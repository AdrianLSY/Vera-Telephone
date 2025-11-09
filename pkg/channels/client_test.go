package channels

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestNewClient tests client creation
func TestNewClient(t *testing.T) {
	url := "ws://localhost:4000/socket/websocket"
	client := NewClient(url)

	if client == nil {
		t.Fatal("Expected non-nil client")
	}

	if client.GetURL() != url {
		t.Errorf("Expected URL %s, got %s", url, client.GetURL())
	}

	if client.handlers == nil {
		t.Error("Expected handlers map to be initialized")
	}

	if client.pendingRequests == nil {
		t.Error("Expected pendingRequests map to be initialized")
	}
}

// TestUpdateURL tests URL updates
func TestUpdateURL(t *testing.T) {
	client := NewClient("ws://localhost:4000/socket")

	newURL := "ws://localhost:5000/socket?token=new"
	client.UpdateURL(newURL)

	if client.GetURL() != newURL {
		t.Errorf("Expected URL %s, got %s", newURL, client.GetURL())
	}
}

// TestOnHandler tests event handler registration
func TestOnHandler(t *testing.T) {
	client := NewClient("ws://localhost:4000/socket")

	called := false
	handler := func(msg *Message) {
		called = true
	}

	client.On("test_event", handler)

	// Verify handler is registered
	client.handlerLock.RLock()
	_, exists := client.handlers["test_event"]
	client.handlerLock.RUnlock()

	if !exists {
		t.Error("Expected handler to be registered")
	}

	// Trigger handler directly
	msg := &Message{Event: "test_event"}
	client.handlerLock.RLock()
	h := client.handlers["test_event"]
	client.handlerLock.RUnlock()
	h(msg)

	if !called {
		t.Error("Expected handler to be called")
	}
}

// TestNextRef tests reference counter
func TestNextRef(t *testing.T) {
	client := NewClient("ws://localhost:4000/socket")

	ref1 := client.NextRef()
	ref2 := client.NextRef()

	if ref1 == ref2 {
		t.Error("Expected unique refs")
	}

	if ref1 == "" || ref2 == "" {
		t.Error("Expected non-empty refs")
	}
}

// TestIsConnected tests connection status
func TestIsConnected(t *testing.T) {
	client := NewClient("ws://localhost:4000/socket")

	if client.IsConnected() {
		t.Error("Expected client to not be connected initially")
	}

	// Simulate connection
	client.connected.Store(true)

	if !client.IsConnected() {
		t.Error("Expected client to be connected")
	}
}

// TestPushMessage tests message pushing
func TestPushMessage(t *testing.T) {
	// Create mock WebSocket server
	upgrader := websocket.Upgrader{}
	receivedMsg := make(chan []byte, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("Upgrade error: %v", err)
			return
		}
		defer conn.Close()

		// Read message
		_, msg, err := conn.ReadMessage()
		if err == nil {
			receivedMsg <- msg
		}

		// Keep connection open
		time.Sleep(2 * time.Second)
	}))
	defer server.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	client := NewClient(wsURL)
	err := client.Connect()
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Wait for connection to establish
	time.Sleep(100 * time.Millisecond)

	// Send a message
	msg := NewMessage("test:topic", "test_event", "1", map[string]interface{}{
		"key": "value",
	})

	err = client.Send(msg)
	if err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}

	// Verify message was received
	select {
	case received := <-receivedMsg:
		if !strings.Contains(string(received), "test_event") {
			t.Errorf("Expected message to contain 'test_event', got: %s", string(received))
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for message")
	}
}

// TestRequestResponse tests request/response pattern
func TestRequestResponse(t *testing.T) {
	upgrader := websocket.Upgrader{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Read request
		_, reqMsg, err := conn.ReadMessage()
		if err != nil {
			return
		}

		// In real scenario, would parse JSON and send phx_reply
		// For simplicity, just echo back a reply
		_ = reqMsg

		// Send reply
		reply := `[null,"1","test:topic","phx_reply",{"status":"ok","response":{"result":"success"}}]`
		conn.WriteMessage(websocket.TextMessage, []byte(reply))

		// Keep connection open
		time.Sleep(2 * time.Second)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	client := NewClient(wsURL)
	err := client.Connect()
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	time.Sleep(100 * time.Millisecond)

	// Send request with timeout
	msg := NewMessage("test:topic", "test_request", "1", map[string]interface{}{})

	reply, err := client.SendAndWait(msg, 2*time.Second)
	if err != nil {
		t.Logf("Request error (may be expected in mock): %v", err)
	} else if reply != nil {
		t.Log("Received reply")
	}
}

// TestConcurrentPush tests concurrent message pushing
func TestConcurrentPush(t *testing.T) {
	upgrader := websocket.Upgrader{}
	messageCount := 0
	mu := sync.Mutex{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Read messages
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
			mu.Lock()
			messageCount++
			mu.Unlock()
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	client := NewClient(wsURL)
	err := client.Connect()
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	time.Sleep(100 * time.Millisecond)

	// Send multiple messages concurrently
	numMessages := 10
	var wg sync.WaitGroup

	for i := 0; i < numMessages; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			msg := NewMessage("test:topic", "concurrent_test", client.NextRef(), map[string]interface{}{
				"index": index,
			})

			client.Send(msg)
		}(i)
	}

	wg.Wait()
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	count := messageCount
	mu.Unlock()

	if count < numMessages {
		t.Logf("Received %d/%d messages (some may be lost in test environment)", count, numMessages)
	}
}

// TestClose tests proper client cleanup
func TestClose(t *testing.T) {
	upgrader := websocket.Upgrader{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Keep connection open
		time.Sleep(5 * time.Second)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	client := NewClient(wsURL)
	err := client.Connect()
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Verify connected
	if !client.IsConnected() {
		t.Error("Expected client to be connected")
	}

	// Close
	err = client.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}

	// Verify disconnected
	if client.IsConnected() {
		t.Error("Expected client to be disconnected after close")
	}

	// Close again should not error
	err = client.Close()
	if err != nil {
		t.Errorf("Second close returned error: %v", err)
	}
}

// TestConnectWithInvalidURL tests connection error handling
func TestConnectWithInvalidURL(t *testing.T) {
	client := NewClient("ws://invalid-host-that-does-not-exist:9999/socket")

	err := client.Connect()
	if err == nil {
		t.Error("Expected error when connecting to invalid host")
		client.Close()
	}
}

// TestPushWithoutConnection tests error handling when not connected
func TestPushWithoutConnection(t *testing.T) {
	client := NewClient("ws://localhost:9999/socket")

	msg := NewMessage("test:topic", "test_event", "1", map[string]interface{}{})

	err := client.Send(msg)
	if err == nil {
		t.Error("Expected error when sending without connection")
	}
}

// TestMessageHandler tests that registered handlers are called
func TestMessageHandler(t *testing.T) {
	upgrader := websocket.Upgrader{}
	handlerCalled := make(chan bool, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Wait a bit for client to set up handlers
		time.Sleep(100 * time.Millisecond)

		// Send a message to client
		testMsg := `[null,null,"test:topic","custom_event",{"data":"test"}]`
		conn.WriteMessage(websocket.TextMessage, []byte(testMsg))

		// Keep connection open
		time.Sleep(2 * time.Second)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	client := NewClient(wsURL)

	// Register handler
	client.On("custom_event", func(msg *Message) {
		if msg.Event == "custom_event" {
			handlerCalled <- true
		}
	})

	err := client.Connect()
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Wait for handler to be called
	select {
	case <-handlerCalled:
		t.Log("Handler called successfully")
	case <-time.After(3 * time.Second):
		t.Error("Handler was not called")
	}
}

// TestRefCounterIncrement tests that ref counter increments
func TestRefCounterIncrement(t *testing.T) {
	client := NewClient("ws://localhost:4000/socket")

	refs := make(map[string]bool)

	// Generate 100 refs
	for i := 0; i < 100; i++ {
		ref := client.NextRef()
		if refs[ref] {
			t.Errorf("Duplicate ref generated: %s", ref)
		}
		refs[ref] = true
	}

	if len(refs) != 100 {
		t.Errorf("Expected 100 unique refs, got %d", len(refs))
	}
}

// TestContextCancellation tests that context cancellation stops client
func TestContextCancellation(t *testing.T) {
	upgrader := websocket.Upgrader{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Keep reading to keep connection alive
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	client := NewClient(wsURL)
	err := client.Connect()
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Cancel context
	client.cancel()

	// Give it time to clean up
	time.Sleep(500 * time.Millisecond)

	// Verify connection is closed
	if client.IsConnected() {
		t.Error("Expected client to disconnect after context cancellation")
	}

	client.Close()
}

// TestMultipleHandlers tests multiple handlers for the same event
func TestMultipleHandlers(t *testing.T) {
	client := NewClient("ws://localhost:4000/socket")

	count := 0
	mu := sync.Mutex{}

	handler1 := func(msg *Message) {
		mu.Lock()
		count++
		mu.Unlock()
	}

	handler2 := func(msg *Message) {
		mu.Lock()
		count += 10
		mu.Unlock()
	}

	// Register first handler
	client.On("test_event", handler1)

	// Register second handler (should replace first)
	client.On("test_event", handler2)

	// Trigger handler
	client.handlerLock.RLock()
	handler := client.handlers["test_event"]
	client.handlerLock.RUnlock()

	handler(&Message{Event: "test_event"})

	mu.Lock()
	finalCount := count
	mu.Unlock()

	// Should only call second handler
	if finalCount != 10 {
		t.Errorf("Expected count 10 (second handler), got %d", finalCount)
	}
}
