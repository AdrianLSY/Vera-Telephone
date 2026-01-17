package proxy

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/verastack/telephone/pkg/auth"
	"github.com/verastack/telephone/pkg/channels"
	"github.com/verastack/telephone/pkg/config"
	ws "github.com/verastack/telephone/pkg/websocket"
)

// testUpgrader for test WebSocket server
var wsTestUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// createWSTestServer creates a test WebSocket server
func createWSTestServer(t *testing.T, handler func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsTestUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("Upgrade error: %v", err)
			return
		}
		defer conn.Close()
		handler(conn)
	}))
}

// mockWSChannelsClient implements ChannelsClient for WebSocket testing
type mockWSChannelsClient struct {
	mu           sync.Mutex
	connected    bool
	sentMessages []*channels.Message
	refCounter   uint64
}

func newMockWSChannelsClient() *mockWSChannelsClient {
	return &mockWSChannelsClient{
		connected:    true,
		sentMessages: make([]*channels.Message, 0),
	}
}

func (m *mockWSChannelsClient) Connect() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = true
	return nil
}

func (m *mockWSChannelsClient) Disconnect() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false
	return nil
}

func (m *mockWSChannelsClient) Close() error {
	return m.Disconnect()
}

func (m *mockWSChannelsClient) IsConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected
}

func (m *mockWSChannelsClient) Send(msg *channels.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentMessages = append(m.sentMessages, msg)
	return nil
}

func (m *mockWSChannelsClient) SendAndWait(msg *channels.Message, timeout time.Duration) (*channels.Message, error) {
	return nil, nil
}

func (m *mockWSChannelsClient) On(event string, handler channels.MessageHandler) {}

func (m *mockWSChannelsClient) NextRef() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refCounter++
	return string(rune('0' + m.refCounter))
}

func (m *mockWSChannelsClient) UpdateURL(url string) {}

func (m *mockWSChannelsClient) getLastMessage() *channels.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sentMessages) == 0 {
		return nil
	}
	return m.sentMessages[len(m.sentMessages)-1]
}

func (m *mockWSChannelsClient) getMessagesByEvent(event string) []*channels.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*channels.Message
	for _, msg := range m.sentMessages {
		if msg.Event == event {
			result = append(result, msg)
		}
	}
	return result
}

// createTestTelephoneWithWS creates a Telephone instance configured for WebSocket testing
func createTestTelephoneWithWS(t *testing.T, backendHost string, backendPort int) (*Telephone, *mockWSChannelsClient) {
	t.Helper()

	mockClient := newMockWSChannelsClient()

	// Create test claims
	token := createTestTokenT(t, time.Now().Add(1*time.Hour))
	claims, _ := auth.ParseJWTUnsafe(token)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel() })

	cfg := &config.Config{
		BackendHost:     backendHost,
		BackendPort:     backendPort,
		BackendScheme:   "http",
		RequestTimeout:  5 * time.Second,
		MaxResponseSize: 100 * 1024 * 1024,
		ChunkSize:       1024 * 1024,
	}

	tel := &Telephone{
		config:          cfg,
		claims:          claims,
		client:          mockClient,
		backend:         &http.Client{Timeout: cfg.RequestTimeout},
		pendingRequests: make(map[string]*PendingRequest),
		ctx:             ctx,
		cancel:          cancel,
	}

	// Initialize WebSocket manager
	tel.wsManager = ws.NewManager(tel)

	return tel, mockClient
}

func TestHandleWSConnect(t *testing.T) {
	// Create a test WebSocket server
	server := createWSTestServer(t, func(conn *websocket.Conn) {
		// Keep connection alive
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	// Extract host and port from server URL
	serverURL := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(serverURL, ":")
	host := parts[0]
	port, _ := strconv.Atoi(parts[1])

	tel, mockClient := createTestTelephoneWithWS(t, host, port)
	defer tel.wsManager.CloseAll()

	// Create ws_connect message
	msg := &channels.Message{
		Topic: "telephone:test-path",
		Event: "ws_connect",
		Payload: map[string]interface{}{
			"connection_id": "conn-123",
			"path":          "/websocket",
			"query_string":  "",
			"headers":       map[string]interface{}{},
		},
	}

	// Handle the message
	tel.handleWSConnect(msg)

	// Give time for async operations
	time.Sleep(100 * time.Millisecond)

	// Verify ws_connected was sent
	messages := mockClient.getMessagesByEvent("ws_connected")
	if len(messages) == 0 {
		t.Fatal("Expected ws_connected message to be sent")
	}

	lastMsg := messages[0]
	if lastMsg.Payload["connection_id"] != "conn-123" {
		t.Errorf("Expected connection_id 'conn-123', got '%v'", lastMsg.Payload["connection_id"])
	}
}

func TestHandleWSConnectFailure(t *testing.T) {
	// Create Telephone with invalid backend (nothing listening)
	tel, mockClient := createTestTelephoneWithWS(t, "127.0.0.1", 59999)
	defer tel.wsManager.CloseAll()

	msg := &channels.Message{
		Topic: "telephone:test-path",
		Event: "ws_connect",
		Payload: map[string]interface{}{
			"connection_id": "conn-fail",
			"path":          "/websocket",
			"query_string":  "",
			"headers":       map[string]interface{}{},
		},
	}

	tel.handleWSConnect(msg)

	time.Sleep(100 * time.Millisecond)

	// Verify ws_error was sent
	messages := mockClient.getMessagesByEvent("ws_error")
	if len(messages) == 0 {
		t.Fatal("Expected ws_error message to be sent")
	}

	lastMsg := messages[0]
	if lastMsg.Payload["connection_id"] != "conn-fail" {
		t.Errorf("Expected connection_id 'conn-fail', got '%v'", lastMsg.Payload["connection_id"])
	}
}

func TestHandleWSFrame(t *testing.T) {
	received := make(chan []byte, 1)

	server := createWSTestServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			received <- data
		}
	})
	defer server.Close()

	serverURL := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(serverURL, ":")
	host := parts[0]
	port, _ := strconv.Atoi(parts[1])

	tel, _ := createTestTelephoneWithWS(t, host, port)
	defer tel.wsManager.CloseAll()

	// First establish a connection
	connectMsg := &channels.Message{
		Payload: map[string]interface{}{
			"connection_id": "conn-frame",
			"path":          "/websocket",
			"query_string":  "",
			"headers":       map[string]interface{}{},
		},
	}
	tel.handleWSConnect(connectMsg)
	time.Sleep(100 * time.Millisecond)

	// Send a frame
	testData := "hello from client"
	frameMsg := &channels.Message{
		Payload: map[string]interface{}{
			"connection_id": "conn-frame",
			"opcode":        "text",
			"data":          base64.StdEncoding.EncodeToString([]byte(testData)),
		},
	}
	tel.handleWSFrame(frameMsg)

	// Verify the frame was received by backend
	select {
	case data := <-received:
		if string(data) != testData {
			t.Errorf("Expected '%s', got '%s'", testData, string(data))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for frame at backend")
	}
}

func TestHandleWSClose(t *testing.T) {
	server := createWSTestServer(t, func(conn *websocket.Conn) {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	serverURL := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(serverURL, ":")
	host := parts[0]
	port, _ := strconv.Atoi(parts[1])

	tel, _ := createTestTelephoneWithWS(t, host, port)

	// First establish a connection
	connectMsg := &channels.Message{
		Payload: map[string]interface{}{
			"connection_id": "conn-close",
			"path":          "/websocket",
			"query_string":  "",
			"headers":       map[string]interface{}{},
		},
	}
	tel.handleWSConnect(connectMsg)
	time.Sleep(100 * time.Millisecond)

	// Close the connection
	closeMsg := &channels.Message{
		Payload: map[string]interface{}{
			"connection_id": "conn-close",
			"code":          float64(1000),
			"reason":        "normal closure",
		},
	}
	tel.handleWSClose(closeMsg)
	time.Sleep(100 * time.Millisecond)

	// Verify sending a frame now fails
	frameMsg := &channels.Message{
		Payload: map[string]interface{}{
			"connection_id": "conn-close",
			"opcode":        "text",
			"data":          base64.StdEncoding.EncodeToString([]byte("test")),
		},
	}
	tel.handleWSFrame(frameMsg)
	// Should not panic, just log error
}

func TestHandleWSConnectMissingConnectionID(t *testing.T) {
	tel, _ := createTestTelephoneWithWS(t, "localhost", 8080)
	defer tel.wsManager.CloseAll()

	msg := &channels.Message{
		Payload: map[string]interface{}{
			"path": "/websocket",
		},
	}

	// Should not panic
	tel.handleWSConnect(msg)
}

func TestHandleWSFrameMissingConnectionID(t *testing.T) {
	tel, _ := createTestTelephoneWithWS(t, "localhost", 8080)
	defer tel.wsManager.CloseAll()

	msg := &channels.Message{
		Payload: map[string]interface{}{
			"opcode": "text",
			"data":   "test",
		},
	}

	// Should not panic
	tel.handleWSFrame(msg)
}

func TestHandleWSFrameInvalidBase64(t *testing.T) {
	server := createWSTestServer(t, func(conn *websocket.Conn) {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	serverURL := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(serverURL, ":")
	host := parts[0]
	port, _ := strconv.Atoi(parts[1])

	tel, mockClient := createTestTelephoneWithWS(t, host, port)
	defer tel.wsManager.CloseAll()

	// First establish a connection
	connectMsg := &channels.Message{
		Payload: map[string]interface{}{
			"connection_id": "conn-b64",
			"path":          "/websocket",
			"query_string":  "",
			"headers":       map[string]interface{}{},
		},
	}
	tel.handleWSConnect(connectMsg)
	time.Sleep(100 * time.Millisecond)

	// Send invalid base64
	frameMsg := &channels.Message{
		Payload: map[string]interface{}{
			"connection_id": "conn-b64",
			"opcode":        "text",
			"data":          "not valid base64!!!",
		},
	}
	tel.handleWSFrame(frameMsg)
	time.Sleep(100 * time.Millisecond)

	// Verify ws_error was sent
	messages := mockClient.getMessagesByEvent("ws_error")
	if len(messages) == 0 {
		t.Fatal("Expected ws_error message for invalid base64")
	}
}

func TestBuildWSBackendURL(t *testing.T) {
	tests := []struct {
		name        string
		scheme      string
		host        string
		port        int
		path        string
		queryString string
		expected    string
	}{
		{
			name:        "http backend",
			scheme:      "http",
			host:        "localhost",
			port:        8080,
			path:        "/websocket",
			queryString: "",
			expected:    "ws://localhost:8080/websocket",
		},
		{
			name:        "https backend",
			scheme:      "https",
			host:        "localhost",
			port:        8443,
			path:        "/ws",
			queryString: "",
			expected:    "wss://localhost:8443/ws",
		},
		{
			name:        "with query string",
			scheme:      "http",
			host:        "backend",
			port:        3000,
			path:        "/socket",
			queryString: "token=abc&room=123",
			expected:    "ws://backend:3000/socket?token=abc&room=123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tel := &Telephone{
				config: &config.Config{
					BackendHost:   tt.host,
					BackendPort:   tt.port,
					BackendScheme: tt.scheme,
				},
			}

			result := tel.buildWSBackendURL(tt.path, tt.queryString)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestOnFrameCallback(t *testing.T) {
	tel, mockClient := createTestTelephoneWithWS(t, "localhost", 8080)
	defer tel.wsManager.CloseAll()

	// Directly call OnFrame (simulating backend sending a frame)
	tel.OnFrame("conn-callback", ws.OpcodeText, []byte("hello"))

	time.Sleep(50 * time.Millisecond)

	messages := mockClient.getMessagesByEvent("ws_frame")
	if len(messages) == 0 {
		t.Fatal("Expected ws_frame message")
	}

	msg := messages[0]
	if msg.Payload["connection_id"] != "conn-callback" {
		t.Errorf("Expected connection_id 'conn-callback', got '%v'", msg.Payload["connection_id"])
	}
	if msg.Payload["opcode"] != "text" {
		t.Errorf("Expected opcode 'text', got '%v'", msg.Payload["opcode"])
	}

	// Verify data is base64 encoded
	encodedData := msg.Payload["data"].(string)
	decoded, _ := base64.StdEncoding.DecodeString(encodedData)
	if string(decoded) != "hello" {
		t.Errorf("Expected 'hello', got '%s'", string(decoded))
	}
}

func TestOnCloseCallback(t *testing.T) {
	tel, mockClient := createTestTelephoneWithWS(t, "localhost", 8080)
	defer tel.wsManager.CloseAll()

	tel.OnClose("conn-close-cb", 1000, "normal closure")

	time.Sleep(50 * time.Millisecond)

	messages := mockClient.getMessagesByEvent("ws_closed")
	if len(messages) == 0 {
		t.Fatal("Expected ws_closed message")
	}

	msg := messages[0]
	if msg.Payload["connection_id"] != "conn-close-cb" {
		t.Errorf("Expected connection_id 'conn-close-cb', got '%v'", msg.Payload["connection_id"])
	}
	if msg.Payload["code"] != 1000 {
		t.Errorf("Expected code 1000, got '%v'", msg.Payload["code"])
	}
	if msg.Payload["reason"] != "normal closure" {
		t.Errorf("Expected reason 'normal closure', got '%v'", msg.Payload["reason"])
	}
}

func TestOnErrorCallback(t *testing.T) {
	tel, mockClient := createTestTelephoneWithWS(t, "localhost", 8080)
	defer tel.wsManager.CloseAll()

	tel.OnError("conn-error-cb", "connection_refused")

	time.Sleep(50 * time.Millisecond)

	messages := mockClient.getMessagesByEvent("ws_error")
	if len(messages) == 0 {
		t.Fatal("Expected ws_error message")
	}

	msg := messages[0]
	if msg.Payload["connection_id"] != "conn-error-cb" {
		t.Errorf("Expected connection_id 'conn-error-cb', got '%v'", msg.Payload["connection_id"])
	}
	if msg.Payload["reason"] != "connection_refused" {
		t.Errorf("Expected reason 'connection_refused', got '%v'", msg.Payload["reason"])
	}
}

func TestBackendFrameForwarding(t *testing.T) {
	server := createWSTestServer(t, func(conn *websocket.Conn) {
		// Send a message to the client (Telephone)
		err := conn.WriteMessage(websocket.TextMessage, []byte("hello from backend"))
		if err != nil {
			t.Logf("Failed to send message: %v", err)
		}
		// Keep connection open
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	serverURL := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(serverURL, ":")
	host := parts[0]
	port, _ := strconv.Atoi(parts[1])

	tel, mockClient := createTestTelephoneWithWS(t, host, port)
	defer tel.wsManager.CloseAll()

	// Establish connection
	connectMsg := &channels.Message{
		Payload: map[string]interface{}{
			"connection_id": "conn-recv",
			"path":          "/websocket",
			"query_string":  "",
			"headers":       map[string]interface{}{},
		},
	}
	tel.handleWSConnect(connectMsg)

	// Wait for frame to be received and forwarded
	time.Sleep(200 * time.Millisecond)

	// Verify ws_frame was sent to Plugboard
	messages := mockClient.getMessagesByEvent("ws_frame")
	if len(messages) == 0 {
		t.Fatal("Expected ws_frame message from backend")
	}

	msg := messages[0]
	if msg.Payload["connection_id"] != "conn-recv" {
		t.Errorf("Expected connection_id 'conn-recv', got '%v'", msg.Payload["connection_id"])
	}

	// Decode and verify data
	encodedData := msg.Payload["data"].(string)
	decoded, _ := base64.StdEncoding.DecodeString(encodedData)
	if string(decoded) != "hello from backend" {
		t.Errorf("Expected 'hello from backend', got '%s'", string(decoded))
	}
}

func TestBackendCloseForwarding(t *testing.T) {
	server := createWSTestServer(t, func(conn *websocket.Conn) {
		// Close the connection from the backend side
		conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(1000, "backend closing"),
			time.Now().Add(time.Second),
		)
	})
	defer server.Close()

	serverURL := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(serverURL, ":")
	host := parts[0]
	port, _ := strconv.Atoi(parts[1])

	tel, mockClient := createTestTelephoneWithWS(t, host, port)
	defer tel.wsManager.CloseAll()

	// Establish connection
	connectMsg := &channels.Message{
		Payload: map[string]interface{}{
			"connection_id": "conn-backend-close",
			"path":          "/websocket",
			"query_string":  "",
			"headers":       map[string]interface{}{},
		},
	}
	tel.handleWSConnect(connectMsg)

	// Wait for close event
	time.Sleep(200 * time.Millisecond)

	// Verify ws_closed was sent
	messages := mockClient.getMessagesByEvent("ws_closed")
	if len(messages) == 0 {
		t.Fatal("Expected ws_closed message")
	}

	msg := messages[0]
	if msg.Payload["connection_id"] != "conn-backend-close" {
		t.Errorf("Expected connection_id 'conn-backend-close', got '%v'", msg.Payload["connection_id"])
	}
}
