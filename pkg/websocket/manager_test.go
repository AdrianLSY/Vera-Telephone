package websocket

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// mockEventHandler implements EventHandler for testing
type mockEventHandler struct {
	mu      sync.Mutex
	frames  []frameEvent
	closes  []closeEvent
	errors  []errorEvent
	frameCh chan frameEvent
	closeCh chan closeEvent
	errorCh chan errorEvent
}

type frameEvent struct {
	connectionID string
	opcode       Opcode
	data         []byte
}

type closeEvent struct {
	connectionID string
	code         int
	reason       string
}

type errorEvent struct {
	connectionID string
	reason       string
}

func newMockEventHandler() *mockEventHandler {
	return &mockEventHandler{
		frameCh: make(chan frameEvent, 10),
		closeCh: make(chan closeEvent, 10),
		errorCh: make(chan errorEvent, 10),
	}
}

func (m *mockEventHandler) OnFrame(connectionID string, opcode Opcode, data []byte) {
	m.mu.Lock()
	event := frameEvent{connectionID, opcode, data}
	m.frames = append(m.frames, event)
	m.mu.Unlock()

	select {
	case m.frameCh <- event:
	default:
	}
}

func (m *mockEventHandler) OnClose(connectionID string, code int, reason string) {
	m.mu.Lock()
	event := closeEvent{connectionID, code, reason}
	m.closes = append(m.closes, event)
	m.mu.Unlock()

	select {
	case m.closeCh <- event:
	default:
	}
}

func (m *mockEventHandler) OnError(connectionID string, reason string) {
	m.mu.Lock()
	event := errorEvent{connectionID, reason}
	m.errors = append(m.errors, event)
	m.mu.Unlock()

	select {
	case m.errorCh <- event:
	default:
	}
}

// upgrader for test WebSocket server
var testUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// createTestWSServer creates a test WebSocket server
func createTestWSServer(t *testing.T, handler func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("Upgrade error: %v", err)
			return
		}
		defer conn.Close()
		handler(conn)
	}))
}

func TestNewManager(t *testing.T) {
	handler := newMockEventHandler()
	manager := NewManager(handler)

	if manager == nil {
		t.Fatal("NewManager returned nil")
	}

	if manager.handler != handler {
		t.Error("Handler not set correctly")
	}

	if manager.connections == nil {
		t.Error("Connections map not initialized")
	}
}

func TestManagerConnect(t *testing.T) {
	handler := newMockEventHandler()
	manager := NewManager(handler)
	defer manager.CloseAll()

	// Create test server that accepts connections
	server := createTestWSServer(t, func(conn *websocket.Conn) {
		// Keep connection open until closed
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Test successful connection
	protocol, err := manager.Connect("conn-1", wsURL, nil)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// No subprotocol expected
	if protocol != "" {
		t.Errorf("Expected empty protocol, got: %s", protocol)
	}

	// Verify connection is tracked
	manager.mu.RLock()
	_, exists := manager.connections["conn-1"]
	manager.mu.RUnlock()

	if !exists {
		t.Error("Connection not tracked in manager")
	}
}

func TestManagerConnectDuplicate(t *testing.T) {
	handler := newMockEventHandler()
	manager := NewManager(handler)
	defer manager.CloseAll()

	server := createTestWSServer(t, func(conn *websocket.Conn) {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// First connection should succeed
	_, err := manager.Connect("conn-1", wsURL, nil)
	if err != nil {
		t.Fatalf("First connect failed: %v", err)
	}

	// Second connection with same ID should fail
	_, err = manager.Connect("conn-1", wsURL, nil)
	if err == nil {
		t.Error("Expected error for duplicate connection ID")
	}
}

func TestManagerConnectWithSubprotocols(t *testing.T) {
	handler := newMockEventHandler()
	manager := NewManager(handler)
	defer manager.CloseAll()

	// Create server that selects a subprotocol
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin:  func(r *http.Request) bool { return true },
			Subprotocols: []string{"graphql-ws"},
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	headers := map[string]string{
		"sec-websocket-protocol": "graphql-ws, wamp",
	}

	protocol, err := manager.Connect("conn-1", wsURL, headers)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	if protocol != "graphql-ws" {
		t.Errorf("Expected protocol 'graphql-ws', got: %s", protocol)
	}
}

func TestManagerSendFrame(t *testing.T) {
	handler := newMockEventHandler()
	manager := NewManager(handler)
	defer manager.CloseAll()

	received := make(chan []byte, 1)

	server := createTestWSServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			received <- data
		}
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	_, err := manager.Connect("conn-1", wsURL, nil)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// Send a text frame
	testData := []byte("hello world")
	err = manager.SendFrame("conn-1", OpcodeText, testData)
	if err != nil {
		t.Fatalf("SendFrame failed: %v", err)
	}

	// Verify the frame was received
	select {
	case data := <-received:
		if string(data) != string(testData) {
			t.Errorf("Expected %s, got %s", testData, data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for frame")
	}
}

func TestManagerSendFrameToNonexistent(t *testing.T) {
	handler := newMockEventHandler()
	manager := NewManager(handler)
	defer manager.CloseAll()

	err := manager.SendFrame("nonexistent", OpcodeText, []byte("test"))
	if err == nil {
		t.Error("Expected error for nonexistent connection")
	}
}

func TestManagerReceiveFrame(t *testing.T) {
	handler := newMockEventHandler()
	manager := NewManager(handler)
	defer manager.CloseAll()

	server := createTestWSServer(t, func(conn *websocket.Conn) {
		// Send a message to the client
		conn.WriteMessage(websocket.TextMessage, []byte("hello from server"))
		// Keep connection open
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	_, err := manager.Connect("conn-1", wsURL, nil)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// Wait for the frame to be received
	select {
	case frame := <-handler.frameCh:
		if frame.connectionID != "conn-1" {
			t.Errorf("Expected connection ID 'conn-1', got '%s'", frame.connectionID)
		}
		if frame.opcode != OpcodeText {
			t.Errorf("Expected opcode 'text', got '%s'", frame.opcode)
		}
		if string(frame.data) != "hello from server" {
			t.Errorf("Expected 'hello from server', got '%s'", frame.data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for frame event")
	}
}

func TestManagerClose(t *testing.T) {
	handler := newMockEventHandler()
	manager := NewManager(handler)

	server := createTestWSServer(t, func(conn *websocket.Conn) {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	_, err := manager.Connect("conn-1", wsURL, nil)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// Close the connection
	err = manager.Close("conn-1", 1000, "normal closure")
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Give time for cleanup
	time.Sleep(100 * time.Millisecond)

	// Verify connection is removed
	manager.mu.RLock()
	_, exists := manager.connections["conn-1"]
	manager.mu.RUnlock()

	if exists {
		t.Error("Connection should be removed after close")
	}
}

func TestManagerCloseAll(t *testing.T) {
	handler := newMockEventHandler()
	manager := NewManager(handler)

	server := createTestWSServer(t, func(conn *websocket.Conn) {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Create multiple connections
	for i := 0; i < 3; i++ {
		_, err := manager.Connect(string(rune('a'+i)), wsURL, nil)
		if err != nil {
			t.Fatalf("Connect %d failed: %v", i, err)
		}
	}

	// Close all connections
	manager.CloseAll()

	// Verify all connections are removed
	manager.mu.RLock()
	count := len(manager.connections)
	manager.mu.RUnlock()

	if count != 0 {
		t.Errorf("Expected 0 connections, got %d", count)
	}
}

func TestManagerBackendClose(t *testing.T) {
	handler := newMockEventHandler()
	manager := NewManager(handler)
	defer manager.CloseAll()

	serverReady := make(chan struct{})

	server := createTestWSServer(t, func(conn *websocket.Conn) {
		close(serverReady)
		// Close the connection from server side
		conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(1000, "server closing"),
			time.Now().Add(time.Second),
		)
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	_, err := manager.Connect("conn-1", wsURL, nil)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// Wait for close event
	select {
	case closeEvt := <-handler.closeCh:
		if closeEvt.connectionID != "conn-1" {
			t.Errorf("Expected connection ID 'conn-1', got '%s'", closeEvt.connectionID)
		}
		if closeEvt.code != 1000 {
			t.Errorf("Expected close code 1000, got %d", closeEvt.code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for close event")
	}
}

func TestBase64EncodeDecode(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"simple", []byte("hello world")},
		{"binary", []byte{0x00, 0x01, 0x02, 0xff, 0xfe}},
		{"unicode", []byte("hello")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := EncodeBase64(tt.data)
			decoded, err := DecodeBase64(encoded)
			if err != nil {
				t.Fatalf("DecodeBase64 failed: %v", err)
			}
			if string(decoded) != string(tt.data) {
				t.Errorf("Round-trip failed: expected %v, got %v", tt.data, decoded)
			}
		})
	}
}

func TestDecodeBase64Invalid(t *testing.T) {
	_, err := DecodeBase64("not valid base64!!!")
	if err == nil {
		t.Error("Expected error for invalid base64")
	}
}
