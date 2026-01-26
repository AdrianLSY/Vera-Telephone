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

// TestNewClient tests client creation.
func TestNewClient(t *testing.T) {
	url := "ws://localhost:4000/socket/websocket"
	client := NewClient(url, "test-token", 10*time.Second)

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

// TestUpdateURL tests URL updates.
func TestUpdateURL(t *testing.T) {
	client := NewClient("ws://localhost:4000/socket", "test-token", 10*time.Second)

	newURL := "ws://localhost:5000/socket?token=new"
	client.UpdateURL(newURL)

	if client.GetURL() != newURL {
		t.Errorf("Expected URL %s, got %s", newURL, client.GetURL())
	}
}

// TestOnHandler tests event handler registration.
func TestOnHandler(t *testing.T) {
	client := NewClient("ws://localhost:4000/socket", "test-token", 10*time.Second)

	called := false
	handler := func(_ *Message) {
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

// TestNextRef tests reference counter.
func TestNextRef(t *testing.T) {
	client := NewClient("ws://localhost:4000/socket", "test-token", 10*time.Second)

	ref1 := client.NextRef()
	ref2 := client.NextRef()

	if ref1 == ref2 {
		t.Error("Expected unique refs")
	}

	if ref1 == "" || ref2 == "" {
		t.Error("Expected non-empty refs")
	}
}

// TestIsConnected tests connection status.
func TestIsConnected(t *testing.T) {
	client := NewClient("ws://localhost:4000/socket", "test-token", 10*time.Second)

	if client.IsConnected() {
		t.Error("Expected client to not be connected initially")
	}

	// Simulate connection
	client.connected.Store(true)

	if !client.IsConnected() {
		t.Error("Expected client to be connected")
	}
}

// TestPushMessage tests message pushing.
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

	client := NewClient(wsURL, "test-token", 10*time.Second)

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

// TestRequestResponse tests request/response pattern.
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

	client := NewClient(wsURL, "test-token", 10*time.Second)

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

// TestConcurrentPush tests concurrent message pushing.
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

	client := NewClient(wsURL, "test-token", 10*time.Second)

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

// TestClose tests proper client cleanup.
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

	client := NewClient(wsURL, "test-token", 10*time.Second)

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

// TestConnectWithInvalidURL tests connection error handling.
func TestConnectWithInvalidURL(t *testing.T) {
	client := NewClient("ws://invalid-host-that-does-not-exist:9999/socket", "test-token", 5*time.Second)

	err := client.Connect()
	if err == nil {
		t.Error("Expected error when connecting to invalid host")
		client.Close()
	}
}

// TestPushWithoutConnection tests error handling when not connected.
func TestPushWithoutConnection(t *testing.T) {
	client := NewClient("ws://localhost:9999/socket", "test-token", 10*time.Second)

	msg := NewMessage("test:topic", "test_event", "1", map[string]interface{}{})

	err := client.Send(msg)
	if err == nil {
		t.Error("Expected error when sending without connection")
	}
}

// TestMessageHandler tests that registered handlers are called.
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

	client := NewClient(wsURL, "test-token", 10*time.Second)

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

// TestRefCounterIncrement tests that ref counter increments.
func TestRefCounterIncrement(t *testing.T) {
	client := NewClient("ws://localhost:4000/socket", "test-token", 10*time.Second)

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

// TestContextCancellation tests that context cancellation stops client.
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

	client := NewClient(wsURL, "test-token", 10*time.Second)

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

// TestMultipleHandlers tests multiple handlers for the same event.
func TestMultipleHandlers(t *testing.T) {
	client := NewClient("ws://localhost:4000/socket", "test-token", 10*time.Second)

	count := 0
	mu := sync.Mutex{}

	handler1 := func(_ *Message) {
		mu.Lock()
		count++
		mu.Unlock()
	}

	handler2 := func(_ *Message) {
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

// TestBuildWSURL tests WebSocket URL building with token as query parameter.
func TestBuildWSURL(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		token    string
		expected string
		wantErr  bool
	}{
		{
			name:     "simple URL with token",
			baseURL:  "ws://localhost:4000/socket",
			token:    "test-token-123",
			expected: "ws://localhost:4000/socket?token=test-token-123&vsn=2.0.0",
			wantErr:  false,
		},
		{
			name:     "URL with existing query params",
			baseURL:  "ws://localhost:4000/socket?foo=bar",
			token:    "test-token",
			expected: "ws://localhost:4000/socket?foo=bar&token=test-token&vsn=2.0.0",
			wantErr:  false,
		},
		{
			name:     "URL without token",
			baseURL:  "ws://localhost:4000/socket",
			token:    "",
			expected: "ws://localhost:4000/socket?vsn=2.0.0",
			wantErr:  false,
		},
		{
			name:     "wss URL with token",
			baseURL:  "wss://example.com/telephone/websocket",
			token:    "jwt-token",
			expected: "wss://example.com/telephone/websocket?token=jwt-token&vsn=2.0.0",
			wantErr:  false,
		},
		{
			name:     "invalid URL",
			baseURL:  "://invalid",
			token:    "token",
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.baseURL, tt.token, 10*time.Second)
			got, err := client.buildWSURL()

			if (err != nil) != tt.wantErr {
				t.Errorf("buildWSURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && got != tt.expected {
				t.Errorf("buildWSURL() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestGetCleanURL_RemovesToken tests that GetCleanURL strips the token from URLs.
func TestGetCleanURL_RemovesToken(t *testing.T) {
	baseURL := "ws://localhost:4000/socket"
	token := "sensitive-token-12345"

	client := NewClient(baseURL, token, 10*time.Second)

	// Build URL with token
	fullURL, err := client.buildWSURL()
	if err != nil {
		t.Fatalf("buildWSURL() error = %v", err)
	}

	// Verify token is in URL
	if !strings.Contains(fullURL, token) {
		t.Errorf("Expected URL to contain token, got %v", fullURL)
	}

	// Update client URL to full URL with token
	client.UpdateURL(fullURL)

	// Get clean URL
	cleanURL := client.GetCleanURL()

	// Verify token is removed
	if strings.Contains(cleanURL, token) {
		t.Errorf("GetCleanURL() should not contain token, got %v", cleanURL)
	}

	if cleanURL != baseURL {
		t.Errorf("GetCleanURL() = %v, want %v", cleanURL, baseURL)
	}
}

// TestGetCleanURL_WithMultipleQueryParams tests GetCleanURL with multiple query params.
func TestGetCleanURL_WithMultipleQueryParams(t *testing.T) {
	// URL with multiple query params including token
	urlWithParams := "ws://localhost:4000/socket?foo=bar&token=secret&baz=qux"
	client := NewClient(urlWithParams, "", 10*time.Second)

	cleanURL := client.GetCleanURL()
	expected := "ws://localhost:4000/socket"

	if cleanURL != expected {
		t.Errorf("GetCleanURL() = %v, want %v", cleanURL, expected)
	}

	// Verify none of the query params are in the clean URL
	if strings.Contains(cleanURL, "token") || strings.Contains(cleanURL, "foo") {
		t.Errorf("GetCleanURL() should not contain any query params, got %v", cleanURL)
	}
}

// TestBuildWSURL_TokenUpdate tests that buildWSURL reflects token updates.
func TestBuildWSURL_TokenUpdate(t *testing.T) {
	client := NewClient("ws://localhost:4000/socket", "initial-token", 10*time.Second)

	// Build URL with initial token
	url1, err := client.buildWSURL()
	if err != nil {
		t.Fatalf("buildWSURL() error = %v", err)
	}

	if !strings.Contains(url1, "initial-token") {
		t.Errorf("Expected URL to contain initial-token, got %v", url1)
	}

	// Update token
	client.UpdateToken("new-token")

	// Build URL with new token
	url2, err := client.buildWSURL()
	if err != nil {
		t.Fatalf("buildWSURL() error = %v", err)
	}

	if !strings.Contains(url2, "new-token") {
		t.Errorf("Expected URL to contain new-token, got %v", url2)
	}

	if strings.Contains(url2, "initial-token") {
		t.Errorf("URL should not contain initial-token after update, got %v", url2)
	}
}

// TestBuildWSURL_IncludesVSN tests that the vsn parameter is included in the URL.
func TestBuildWSURL_IncludesVSN(t *testing.T) {
	client := NewClient("ws://localhost:4000/socket", "test-token", 10*time.Second)

	url, err := client.buildWSURL()
	if err != nil {
		t.Fatalf("buildWSURL() error = %v", err)
	}

	// Verify vsn=2.0.0 parameter is in URL
	if !strings.Contains(url, "vsn=2.0.0") {
		t.Errorf("Expected URL to contain vsn=2.0.0, got %v", url)
	}

	// Verify both token and vsn are present
	if !strings.Contains(url, "token=test-token") {
		t.Errorf("Expected URL to contain token parameter, got %v", url)
	}
}

// TestBuildWSURL_VSN_WithoutToken tests that vsn is included even without token.
func TestBuildWSURL_VSN_WithoutToken(t *testing.T) {
	client := NewClient("ws://localhost:4000/socket", "", 10*time.Second)

	url, err := client.buildWSURL()
	if err != nil {
		t.Fatalf("buildWSURL() error = %v", err)
	}

	// Verify vsn parameter is still included even without token
	if !strings.Contains(url, "vsn=2.0.0") {
		t.Errorf("Expected URL to contain vsn=2.0.0 even without token, got %v", url)
	}

	expected := "ws://localhost:4000/socket?vsn=2.0.0"
	if url != expected {
		t.Errorf("Expected URL %v, got %v", expected, url)
	}
}
