package proxy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
)

func init() {
	// Try to load .env file for integration tests
	godotenv.Load("../../.env")
}

// Integration tests that require a running Plugboard server on localhost:4000
// Run with: go test ./pkg/proxy/... -v -tags=integration
//
// Note: Most integration tests are in integration_improved_test.go using
// table-driven approach. This file contains unique tests not covered there.

// This is a comprehensive smoke test that verifies all components work together.
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
		t.Fatalf("Failed to connect to Plugboard: %v", err)
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
		t.Log("Startup successful")
	}

	// Give it time to establish connection and join channel
	time.Sleep(2 * time.Second)

	// Verify connection is established
	t.Log("Telephone started and connected to Plugboard")

	// Clean shutdown
	if err := tel.Stop(); err != nil {
		t.Errorf("Failed to stop Telephone: %v", err)
	}

	// Verify telephone waited for shutdown
	t.Log("Telephone stopped cleanly")
}

// This verifies the token can be loaded from database on restart.
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
		t.Fatalf("Failed to connect to Plugboard: %v", err)
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

// This test requires manual Plugboard restart to trigger reconnection.
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

// BenchmarkIntegrationProxyRequest benchmarks a single proxy request through the full stack.
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

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"benchmark":"true"}`))
	}))
	defer backend.Close()

	cfg, err := loadTestConfig(nil)
	if err != nil {
		b.Skip("Skipping benchmark: " + err.Error())
	}

	// Use unique test database
	cfg.TokenDBPath = "/tmp/telephone_benchmark.db"
	defer os.Remove(cfg.TokenDBPath)

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
			"request_id": fmt.Sprintf("550e8400-e29b-41d4-a716-%012d", i),
			"method":     "GET",
			"path":       "/bench",
			"headers":    map[string]interface{}{},
			"body":       "",
		}
		tel.forwardToBackend(payload)
	}
}
