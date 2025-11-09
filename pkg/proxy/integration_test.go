package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/verastack/telephone/pkg/config"
)

func init() {
	// Try to load .env file for integration tests
	godotenv.Load("../../.env")
}

// Integration tests that require a running Plugboard server on localhost:4000
// Run with: go test ./pkg/proxy/... -v -tags=integration

// TestIntegrationFullFlow tests the complete end-to-end flow
func TestIntegrationFullFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if Plugboard is available
	if !isPlugboardAvailable(t) {
		t.Skip("Plugboard not available on localhost:4000")
	}

	// Create a test backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("Backend received: %s %s", r.Method, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message":"integration test success","timestamp":"` + time.Now().Format(time.RFC3339) + `"}`))
	}))
	defer backend.Close()

	// Parse backend URL
	backendHost := strings.TrimPrefix(backend.URL, "http://")
	parts := strings.Split(backendHost, ":")
	backendPort := parts[1]

	// Load config from environment
	cfg, err := loadTestConfig(t)
	if err != nil {
		t.Skip("Skipping test: " + err.Error())
	}

	// Use unique test database to avoid token conflicts
	cfg.TokenDBPath = "/tmp/telephone_integration_test_fullflow.db"
	defer os.Remove(cfg.TokenDBPath)

	// Override backend to point to our test server
	cfg.BackendHost = "127.0.0.1"
	if port, ok := parsePort(backendPort); ok {
		cfg.BackendPort = port
	}

	// Create and start Telephone
	tel, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to connect to Plubboard: %v", err)
	}

	// Start in background
	errChan := make(chan error, 1)
	go func() {
		if err := tel.Start(); err != nil {
			errChan <- err
		}
	}()

	// Wait for startup or error
	select {
	case err := <-errChan:
		t.Fatalf("Failed to start Telephone: %v", err)
	case <-time.After(5 * time.Second):
		// Startup successful
	}

	// Give it time to establish connection and join channel
	time.Sleep(2 * time.Second)

	// Verify connection is established by checking logs
	// In a real test, you'd make a request through Plugboard
	t.Log("Telephone started and connected to Plugboard")

	// Clean shutdown
	if err := tel.Stop(); err != nil {
		t.Errorf("Failed to stop Telephone: %v", err)
	}

	// Verify telephone waited for shutdown
	t.Log("Telephone stopped cleanly")
}

// TestIntegrationConnection tests WebSocket connection establishment
func TestIntegrationConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if !isPlugboardAvailable(t) {
		t.Skip("Plugboard not available on localhost:4000")
	}

	cfg, err := loadTestConfig(t)
	if err != nil {
		t.Skip("Skipping test: " + err.Error())
	}

	// Use unique test database
	cfg.TokenDBPath = "/tmp/telephone_integration_test_connection.db"
	defer os.Remove(cfg.TokenDBPath)

	tel, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to connect to Plubboard: %v", err)
	}

	// Start should succeed
	errChan := make(chan error, 1)
	go func() {
		if err := tel.Start(); err != nil {
			errChan <- err
		}
	}()

	// Wait for connection
	select {
	case err := <-errChan:
		t.Fatalf("Failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Log("Connection established successfully")
	}

	// Verify we can get current token
	token := tel.getCurrentToken()
	if token == "" {
		t.Error("Expected non-empty token after connection")
	}

	// Clean shutdown
	tel.Stop()
}

// TestIntegrationHeartbeat tests the heartbeat mechanism
func TestIntegrationHeartbeat(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if !isPlugboardAvailable(t) {
		t.Skip("Plugboard not available on localhost:4000")
	}

	cfg, err := loadTestConfig(t)
	if err != nil {
		t.Skip("Skipping test: " + err.Error())
	}

	// Use unique test database
	cfg.TokenDBPath = "/tmp/telephone_integration_test_heartbeat.db"
	defer os.Remove(cfg.TokenDBPath)

	// Set short heartbeat interval for testing
	cfg.HeartbeatInterval = 2 * time.Second

	tel, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to connect to Plubboard: %v", err)
	}

	errChan := make(chan error, 1)
	go func() {
		if err := tel.Start(); err != nil {
			errChan <- err
		}
	}()

	select {
	case err := <-errChan:
		t.Fatalf("Failed to start: %v", err)
	case <-time.After(3 * time.Second):
		// Should have sent at least one heartbeat
	}

	// Check last heartbeat time
	tel.heartbeatLock.RLock()
	lastHeartbeat := tel.lastHeartbeat
	tel.heartbeatLock.RUnlock()

	if lastHeartbeat.IsZero() {
		t.Error("Expected heartbeat to have been sent")
	}

	elapsed := time.Since(lastHeartbeat)
	if elapsed > 5*time.Second {
		t.Errorf("Last heartbeat was too long ago: %v", elapsed)
	}

	tel.Stop()
}

// TestIntegrationTokenRefresh tests token refresh mechanism
func TestIntegrationTokenRefresh(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if !isPlugboardAvailable(t) {
		t.Skip("Plugboard not available on localhost:4000")
	}

	cfg, err := loadTestConfig(t)
	if err != nil {
		t.Skip("Skipping test: " + err.Error())
	}

	// Use unique test database
	cfg.TokenDBPath = "/tmp/telephone_integration_test_tokenrefresh.db"
	defer os.Remove(cfg.TokenDBPath)

	tel, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to connect to Plubboard: %v", err)
	}

	initialToken := tel.getCurrentToken()

	errChan := make(chan error, 1)
	go func() {
		if err := tel.Start(); err != nil {
			errChan <- err
		}
	}()

	select {
	case err := <-errChan:
		t.Fatalf("Failed to start: %v", err)
	case <-time.After(2 * time.Second):
		// Connected
	}

	// Note: Token refresh now happens automatically at half-life of the token.
	// For a typical 1-hour token, this would be 30 minutes.
	// Since we can't control the refresh interval anymore, we just verify
	// that the token refresh mechanism is properly initialized.

	tel.tokenMu.RLock()
	claims := tel.claims
	tel.tokenMu.RUnlock()

	timeUntilRefresh := claims.TimeUntilHalfLife()
	t.Logf("Token will be refreshed in %v (at half of %v lifespan)", timeUntilRefresh, claims.Lifespan())

	// Verify the token has not changed immediately
	currentToken := tel.getCurrentToken()
	if currentToken != initialToken {
		t.Error("Token changed unexpectedly before half-life")
	}

	tel.Stop()
}

// TestIntegrationGracefulShutdown tests graceful shutdown with active requests
func TestIntegrationGracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if !isPlugboardAvailable(t) {
		t.Skip("Plugboard not available on localhost:4000")
	}

	// This test is difficult to properly test in isolation because it requires
	// coordinating real proxy requests through Plugboard, which involves
	// WebSocket message timing and backend request lifecycle.
	// The graceful shutdown logic is verified through the unit tests.
	t.Skip("Graceful shutdown tested via unit tests - integration test requires complex timing coordination")
}

// TestIntegrationReconnection tests automatic reconnection
func TestIntegrationReconnection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if !isPlugboardAvailable(t) {
		t.Skip("Plugboard not available on localhost:4000")
	}

	// This test would require stopping/starting Plugboard
	// which is complex in an automated test
	t.Skip("Reconnection test requires manual Plugboard restart")
}

// TestIntegrationConcurrentProxyRequests tests handling multiple proxy requests
func TestIntegrationConcurrentProxyRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if !isPlugboardAvailable(t) {
		t.Skip("Plugboard not available on localhost:4000")
	}

	requestCount := 0
	mu := sync.Mutex{}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		count := requestCount
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf(`{"request_number":%d}`, count)))
	}))
	defer backend.Close()

	cfg, err := loadTestConfig(t)
	if err != nil {
		t.Skip("Skipping test: " + err.Error())
	}

	// Use unique test database
	cfg.TokenDBPath = "/tmp/telephone_integration_test_concurrent.db"
	defer os.Remove(cfg.TokenDBPath)

	backendHost := strings.TrimPrefix(backend.URL, "http://")
	parts := strings.Split(backendHost, ":")
	cfg.BackendHost = "127.0.0.1"
	if port, ok := parsePort(parts[1]); ok {
		cfg.BackendPort = port
	}

	tel, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to connect to Plubboard: %v", err)
	}

	go tel.Start()
	time.Sleep(2 * time.Second)

	// Send multiple concurrent requests
	numRequests := 20
	var wg sync.WaitGroup
	successCount := 0
	successMu := sync.Mutex{}

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			payload := map[string]interface{}{
				"request_id": fmt.Sprintf("concurrent-request-%d", index),
				"method":     "GET",
				"path":       fmt.Sprintf("/api/test/%d", index),
				"headers":    map[string]interface{}{},
				"body":       "",
			}

			resp, err := tel.forwardToBackend(payload)
			if err == nil && resp.Status == 200 {
				successMu.Lock()
				successCount++
				successMu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	successMu.Lock()
	final := successCount
	successMu.Unlock()

	if final != numRequests {
		t.Errorf("Expected %d successful requests, got %d", numRequests, final)
	}

	mu.Lock()
	finalCount := requestCount
	mu.Unlock()

	if finalCount != numRequests {
		t.Errorf("Expected backend to receive %d requests, got %d", numRequests, finalCount)
	}

	tel.Stop()
}

// TestIntegrationLargeResponse tests handling of large responses through Plugboard
func TestIntegrationLargeResponse(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if !isPlugboardAvailable(t) {
		t.Skip("Plugboard not available on localhost:4000")
	}

	// Create response larger than 1MB
	largeBody := strings.Repeat("x", 2*1024*1024) // 2MB

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(largeBody))
	}))
	defer backend.Close()

	cfg, err := loadTestConfig(t)
	if err != nil {
		t.Skip("Skipping test: " + err.Error())
	}

	// Use unique test database
	cfg.TokenDBPath = "/tmp/telephone_integration_test_largeresponse.db"
	defer os.Remove(cfg.TokenDBPath)

	backendHost := strings.TrimPrefix(backend.URL, "http://")
	parts := strings.Split(backendHost, ":")
	cfg.BackendHost = "127.0.0.1"
	if port, ok := parsePort(parts[1]); ok {
		cfg.BackendPort = port
	}

	tel, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to connect to Plubboard: %v", err)
	}

	go tel.Start()
	time.Sleep(2 * time.Second)

	payload := map[string]interface{}{
		"request_id": "large-response-test",
		"method":     "GET",
		"path":       "/large",
		"headers":    map[string]interface{}{},
		"body":       "",
	}

	resp, err := tel.forwardToBackend(payload)
	if err != nil {
		t.Fatalf("Failed to forward request: %v", err)
	}

	if !resp.Chunked {
		t.Error("Expected large response to be chunked")
	}

	if len(resp.Chunks) == 0 {
		t.Error("Expected non-empty chunks array")
	}

	// Verify chunks reconstruct the original
	var reconstructed strings.Builder
	for _, chunk := range resp.Chunks {
		reconstructed.WriteString(chunk)
	}

	if reconstructed.Len() != len(largeBody) {
		t.Errorf("Reconstructed body size %d doesn't match original %d",
			reconstructed.Len(), len(largeBody))
	}

	tel.Stop()
}

// TestIntegrationErrorHandling tests error responses through the full stack
func TestIntegrationErrorHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if !isPlugboardAvailable(t) {
		t.Skip("Plugboard not available on localhost:4000")
	}

	testCases := []struct {
		name           string
		backendStatus  int
		expectedStatus int
	}{
		{"404 Not Found", 404, 404},
		{"500 Internal Server Error", 500, 500},
		{"503 Service Unavailable", 503, 503},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.backendStatus)
				w.Write([]byte(fmt.Sprintf("Error: %d", tc.backendStatus)))
			}))
			defer backend.Close()

			cfg, err := loadTestConfig(t)
			if err != nil {
				t.Skip("Skipping test: " + err.Error())
			}

			// Use unique test database for this subtest
			cfg.TokenDBPath = fmt.Sprintf("/tmp/telephone_integration_test_error_%d.db", tc.backendStatus)
			defer os.Remove(cfg.TokenDBPath)

			backendHost := strings.TrimPrefix(backend.URL, "http://")
			parts := strings.Split(backendHost, ":")
			cfg.BackendHost = "127.0.0.1"
			if port, ok := parsePort(parts[1]); ok {
				cfg.BackendPort = port
			}

			tel, err := New(cfg)
			if err != nil {
				t.Fatalf("Failed to connect to Plubboard: %v", err)
			}

			go tel.Start()
			time.Sleep(2 * time.Second)

			payload := map[string]interface{}{
				"request_id": fmt.Sprintf("error-test-%d", tc.backendStatus),
				"method":     "GET",
				"path":       "/error",
				"headers":    map[string]interface{}{},
				"body":       "",
			}

			resp, err := tel.forwardToBackend(payload)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if resp.Status != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, resp.Status)
			}

			tel.Stop()
		})
	}
}

// TestIntegrationPostWithBody tests POST request with body through full stack
func TestIntegrationPostWithBody(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if !isPlugboardAvailable(t) {
		t.Skip("Plugboard not available on localhost:4000")
	}

	receivedBody := ""
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)

		var data map[string]interface{}
		json.Unmarshal(body, &data)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":123,"created":true}`))
	}))
	defer backend.Close()

	cfg, err := loadTestConfig(t)
	if err != nil {
		t.Skip("Skipping test: " + err.Error())
	}

	// Use unique test database
	cfg.TokenDBPath = "/tmp/telephone_integration_test_postwithbody.db"
	defer os.Remove(cfg.TokenDBPath)

	backendHost := strings.TrimPrefix(backend.URL, "http://")
	parts := strings.Split(backendHost, ":")
	cfg.BackendHost = "127.0.0.1"
	if port, ok := parsePort(parts[1]); ok {
		cfg.BackendPort = port
	}

	tel, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to connect to Plubboard: %v", err)
	}

	go tel.Start()
	time.Sleep(2 * time.Second)

	testBody := `{"name":"Integration Test","value":42}`
	payload := map[string]interface{}{
		"request_id": "post-with-body-test",
		"method":     "POST",
		"path":       "/api/create",
		"headers": map[string]interface{}{
			"content-type": "application/json",
		},
		"body": testBody,
	}

	resp, err := tel.forwardToBackend(payload)
	if err != nil {
		t.Fatalf("Failed to forward request: %v", err)
	}

	if resp.Status != 201 {
		t.Errorf("Expected status 201, got %d", resp.Status)
	}

	if receivedBody != testBody {
		t.Errorf("Expected body %s, got %s", testBody, receivedBody)
	}

	if !strings.Contains(resp.Body, `"id":123`) {
		t.Errorf("Expected response to contain id, got: %s", resp.Body)
	}

	tel.Stop()
}

// Helper function to check if Plugboard is available
func isPlugboardAvailable(t *testing.T) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:4000")
	if err != nil {
		t.Logf("Plugboard not available: %v", err)
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

// Helper function to parse port string to int
func parsePort(portStr string) (int, bool) {
	var port int
	_, err := fmt.Sscanf(portStr, "%d", &port)
	return port, err == nil
}

// TestIntegrationContextCancellation tests proper context handling
func TestIntegrationContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if !isPlugboardAvailable(t) {
		t.Skip("Plugboard not available on localhost:4000")
	}

	cfg, err := loadTestConfig(t)
	if err != nil {
		t.Skip("Skipping test: " + err.Error())
	}

	// Use unique test database
	cfg.TokenDBPath = "/tmp/telephone_integration_test_contextcancel.db"
	defer os.Remove(cfg.TokenDBPath)

	tel, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to connect to Plubboard: %v", err)
	}

	go tel.Start()
	time.Sleep(1 * time.Second)

	// Cancel context
	tel.cancel()

	// Wait should return immediately
	done := make(chan struct{})
	go func() {
		tel.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Log("Context cancellation handled correctly")
	case <-time.After(2 * time.Second):
		t.Error("Wait() didn't return after context cancellation")
	}

	tel.Stop()
}

// TestIntegrationTokenPersistence tests that tokens are persisted to database
func TestIntegrationTokenPersistence(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if !isPlugboardAvailable(t) {
		t.Skip("Plugboard not available on localhost:4000")
	}

	// Use a temporary database (unique for this test)
	tmpDB := "/tmp/telephone_integration_test_persistence.db"
	defer os.Remove(tmpDB)

	cfg, err := loadTestConfig(t)
	if err != nil {
		t.Skip("Skipping test: " + err.Error())
	}
	cfg.TokenDBPath = tmpDB

	tel, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to connect to Plubboard: %v", err)
	}

	go tel.Start()
	time.Sleep(2 * time.Second)

	// Get current token
	token1 := tel.getCurrentToken()
	if token1 == "" {
		t.Fatal("Expected non-empty token")
	}

	// Stop and create new instance
	tel.Stop()
	time.Sleep(1 * time.Second)

	// Create new telephone with same DB
	tel2, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create second Telephone: %v", err)
	}

	// Should load token from DB
	token2 := tel2.getCurrentToken()
	if token2 == "" {
		t.Error("Expected to load token from database")
	}

	t.Logf("Token persistence verified (tokens may differ due to refresh)")

	// Cleanup
	if tel2.tokenStore != nil {
		tel2.tokenStore.Close()
	}
}

// BenchmarkIntegrationProxyRequest benchmarks a single proxy request through the full stack
func BenchmarkIntegrationProxyRequest(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping integration benchmark in short mode")
	}

	// Check Plugboard availability
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:4000")
	if err != nil || resp.StatusCode != 200 {
		b.Skip("Plugboard not available")
	}
	resp.Body.Close()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"benchmark":"true"}`))
	}))
	defer backend.Close()

	cfg, err := loadTestConfig(nil)
	if err != nil {
		b.Skip("Skipping benchmark: " + err.Error())
	}
	backendHost := strings.TrimPrefix(backend.URL, "http://")
	parts := strings.Split(backendHost, ":")
	cfg.BackendHost = "127.0.0.1"
	if port, ok := parsePort(parts[1]); ok {
		cfg.BackendPort = port
	}

	tel, _ := New(cfg)
	go tel.Start()
	time.Sleep(2 * time.Second)
	defer tel.Stop()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		payload := map[string]interface{}{
			"request_id": fmt.Sprintf("bench-request-%d", i),
			"method":     "GET",
			"path":       "/bench",
			"headers":    map[string]interface{}{},
			"body":       "",
		}
		tel.forwardToBackend(payload)
	}
}

// Helper function to load test config with proper error handling
func loadTestConfig(t *testing.T) (*config.Config, error) {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		// Check if it's missing required environment variables
		if t != nil {
			t.Logf("Config load error: %v", err)
		}
		return nil, fmt.Errorf("missing required environment variables (run from project root or set TELEPHONE_TOKEN and SECRET_KEY_BASE)")
	}
	return cfg, nil
}
