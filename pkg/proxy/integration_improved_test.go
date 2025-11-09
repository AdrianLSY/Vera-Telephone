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
	"sync/atomic"
	"testing"
	"time"
)

// TestIntegrationLifecycle tests basic lifecycle operations using table-driven approach
func TestIntegrationLifecycle(t *testing.T) {
	tests := []struct {
		name         string
		startupWait  time.Duration
		testDuration time.Duration
		assertions   func(*testing.T, *integrationTestSetup)
	}{
		{
			name:         "connection_establishment",
			startupWait:  5 * time.Second,
			testDuration: 2 * time.Second,
			assertions: func(t *testing.T, setup *integrationTestSetup) {
				assertTokenValid(t, setup.Tel)
				t.Log("Connection established successfully")
			},
		},
		{
			name:         "heartbeat_mechanism",
			startupWait:  3 * time.Second,
			testDuration: 5 * time.Second,
			assertions: func(t *testing.T, setup *integrationTestSetup) {
				// Override heartbeat interval for faster testing
				setup.Tel.config.HeartbeatInterval = 2 * time.Second
				assertHeartbeatRecent(t, setup.Tel, 5*time.Second)
			},
		},
		{
			name:         "token_validity",
			startupWait:  2 * time.Second,
			testDuration: 1 * time.Second,
			assertions: func(t *testing.T, setup *integrationTestSetup) {
				initialToken := setup.Tel.getCurrentToken()

				// Verify token doesn't change immediately
				time.Sleep(1 * time.Second)
				currentToken := setup.Tel.getCurrentToken()

				if currentToken != initialToken {
					t.Error("Token changed unexpectedly before half-life")
				}

				// Log time until refresh
				setup.Tel.tokenMu.RLock()
				claims := setup.Tel.claims
				setup.Tel.tokenMu.RUnlock()

				t.Logf("Token will be refreshed in %v", claims.TimeUntilHalfLife())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup := setupIntegrationTest(t, tt.name)
			defer setup.Cleanup()

			setup.startTelephoneAsync(t, tt.startupWait)
			time.Sleep(tt.testDuration)

			tt.assertions(t, setup)
		})
	}
}

// TestIntegrationProxyRequests tests various proxy request scenarios
func TestIntegrationProxyRequests(t *testing.T) {
	tests := []struct {
		name         string
		setupBackend func(*testing.T) *httptest.Server
		payload      func(index int) map[string]interface{}
		validateResp func(*testing.T, *ProxyResponse)
		numRequests  int
		concurrent   bool
	}{
		{
			name: "simple_get_request",
			setupBackend: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{"status":"success"}`))
				}))
			},
			payload: func(i int) map[string]interface{} {
				return map[string]interface{}{
					"request_id": fmt.Sprintf("get-request-%d", i),
					"method":     "GET",
					"path":       "/api/test",
					"headers":    map[string]interface{}{},
					"body":       "",
				}
			},
			validateResp: func(t *testing.T, resp *ProxyResponse) {
				if resp.Status != 200 {
					t.Errorf("Expected status 200, got %d", resp.Status)
				}
				if !strings.Contains(resp.Body, "success") {
					t.Errorf("Expected success in body, got: %s", resp.Body)
				}
			},
			numRequests: 1,
			concurrent:  false,
		},
		{
			name: "post_with_json_body",
			setupBackend: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					var data map[string]interface{}
					if err := json.Unmarshal(body, &data); err != nil {
						t.Errorf("Failed to unmarshal request body: %v", err)
					}

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					w.Write([]byte(`{"id":123,"created":true}`))
				}))
			},
			payload: func(i int) map[string]interface{} {
				return map[string]interface{}{
					"request_id": fmt.Sprintf("post-request-%d", i),
					"method":     "POST",
					"path":       "/api/create",
					"headers": map[string]interface{}{
						"content-type": "application/json",
					},
					"body": `{"name":"test","value":42}`,
				}
			},
			validateResp: func(t *testing.T, resp *ProxyResponse) {
				if resp.Status != 201 {
					t.Errorf("Expected status 201, got %d", resp.Status)
				}
				if !strings.Contains(resp.Body, `"id":123`) {
					t.Errorf("Expected id in response, got: %s", resp.Body)
				}
			},
			numRequests: 1,
			concurrent:  false,
		},
		{
			name: "large_response_chunking",
			setupBackend: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/plain")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(strings.Repeat("x", 2*1024*1024))) // 2MB
				}))
			},
			payload: func(i int) map[string]interface{} {
				return map[string]interface{}{
					"request_id": fmt.Sprintf("large-response-%d", i),
					"method":     "GET",
					"path":       "/large",
					"headers":    map[string]interface{}{},
					"body":       "",
				}
			},
			validateResp: func(t *testing.T, resp *ProxyResponse) {
				if !resp.Chunked {
					t.Error("Expected large response to be chunked")
				}
				if len(resp.Chunks) == 0 {
					t.Error("Expected non-empty chunks array")
				}

				// Verify total size
				totalSize := 0
				for _, chunk := range resp.Chunks {
					totalSize += len(chunk)
				}
				if totalSize != 2*1024*1024 {
					t.Errorf("Expected 2MB total, got %d bytes", totalSize)
				}
			},
			numRequests: 1,
			concurrent:  false,
		},
		{
			name: "concurrent_requests",
			setupBackend: func(t *testing.T) *httptest.Server {
				var counter int32
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					count := atomic.AddInt32(&counter, 1)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(fmt.Sprintf(`{"request_number":%d}`, count)))
				}))
			},
			payload: func(i int) map[string]interface{} {
				return map[string]interface{}{
					"request_id": fmt.Sprintf("concurrent-%d", i),
					"method":     "GET",
					"path":       fmt.Sprintf("/api/test/%d", i),
					"headers":    map[string]interface{}{},
					"body":       "",
				}
			},
			validateResp: func(t *testing.T, resp *ProxyResponse) {
				if resp.Status != 200 {
					t.Errorf("Expected status 200, got %d", resp.Status)
				}
			},
			numRequests: 20,
			concurrent:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup := setupIntegrationTest(t, tt.name)
			defer setup.Cleanup()

			backend := tt.setupBackend(t)
			defer backend.Close()

			if err := configureBackend(setup.Config, backend.URL); err != nil {
				t.Fatalf("Failed to configure backend: %v", err)
			}

			setup.startTelephoneAsync(t, 5*time.Second)

			if tt.concurrent {
				// Run requests concurrently
				var wg sync.WaitGroup
				var mu sync.Mutex
				successCount := 0

				for i := 0; i < tt.numRequests; i++ {
					wg.Add(1)
					go func(index int) {
						defer wg.Done()

						resp, err := setup.Tel.forwardToBackend(tt.payload(index))
						if err != nil {
							t.Errorf("Request %d failed: %v", index, err)
							return
						}

						tt.validateResp(t, resp)

						mu.Lock()
						successCount++
						mu.Unlock()
					}(i)
				}

				wg.Wait()

				mu.Lock()
				if successCount != tt.numRequests {
					t.Errorf("Expected %d successful requests, got %d", tt.numRequests, successCount)
				}
				mu.Unlock()
			} else {
				// Run requests sequentially
				for i := 0; i < tt.numRequests; i++ {
					resp, err := setup.Tel.forwardToBackend(tt.payload(i))
					if err != nil {
						t.Fatalf("Request %d failed: %v", i, err)
					}
					tt.validateResp(t, resp)
				}
			}
		})
	}
}

// TestIntegrationErrorHandling tests error scenarios using table-driven approach
func TestIntegrationErrorHandling(t *testing.T) {
	tests := []struct {
		name           string
		backendStatus  int
		backendBody    string
		expectedStatus int
		validateError  func(*testing.T, *ProxyResponse)
	}{
		{
			name:           "404_not_found",
			backendStatus:  404,
			backendBody:    "Not Found",
			expectedStatus: 404,
			validateError: func(t *testing.T, resp *ProxyResponse) {
				if !strings.Contains(resp.Body, "Not Found") {
					t.Errorf("Expected 'Not Found' in body, got: %s", resp.Body)
				}
			},
		},
		{
			name:           "500_internal_server_error",
			backendStatus:  500,
			backendBody:    "Internal Server Error",
			expectedStatus: 500,
			validateError: func(t *testing.T, resp *ProxyResponse) {
				if !strings.Contains(resp.Body, "Internal Server Error") {
					t.Errorf("Expected error message in body, got: %s", resp.Body)
				}
			},
		},
		{
			name:           "503_service_unavailable",
			backendStatus:  503,
			backendBody:    "Service Unavailable",
			expectedStatus: 503,
			validateError: func(t *testing.T, resp *ProxyResponse) {
				if !strings.Contains(resp.Body, "Service Unavailable") {
					t.Errorf("Expected error message in body, got: %s", resp.Body)
				}
			},
		},
		{
			name:           "401_unauthorized",
			backendStatus:  401,
			backendBody:    "Unauthorized",
			expectedStatus: 401,
			validateError: func(t *testing.T, resp *ProxyResponse) {
				if resp.Status != 401 {
					t.Errorf("Expected status 401, got %d", resp.Status)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup := setupIntegrationTest(t, tt.name)
			defer setup.Cleanup()

			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.backendStatus)
				w.Write([]byte(tt.backendBody))
			}))
			defer backend.Close()

			if err := configureBackend(setup.Config, backend.URL); err != nil {
				t.Fatalf("Failed to configure backend: %v", err)
			}

			setup.startTelephoneAsync(t, 5*time.Second)

			payload := map[string]interface{}{
				"request_id": fmt.Sprintf("error-test-%d", tt.backendStatus),
				"method":     "GET",
				"path":       "/error",
				"headers":    map[string]interface{}{},
				"body":       "",
			}

			resp, err := setup.Tel.forwardToBackend(payload)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if resp.Status != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, resp.Status)
			}

			tt.validateError(t, resp)
		})
	}
}

// TestIntegrationContextAndShutdown tests context cancellation and shutdown scenarios
func TestIntegrationContextAndShutdown(t *testing.T) {
	tests := []struct {
		name      string
		action    func(*testing.T, *integrationTestSetup)
		timeout   time.Duration
		assertion func(*testing.T, *integrationTestSetup)
	}{
		{
			name: "graceful_shutdown",
			action: func(t *testing.T, setup *integrationTestSetup) {
				// Just call Stop()
				err := setup.Tel.Stop()
				if err != nil {
					t.Errorf("Stop returned error: %v", err)
				}
			},
			timeout: 5 * time.Second,
			assertion: func(t *testing.T, setup *integrationTestSetup) {
				t.Log("Graceful shutdown completed")
			},
		},
		{
			name: "context_cancellation",
			action: func(t *testing.T, setup *integrationTestSetup) {
				// Cancel context
				setup.Tel.cancel()

				// Wait should return
				done := make(chan struct{})
				go func() {
					setup.Tel.Wait()
					close(done)
				}()

				select {
				case <-done:
					t.Log("Context cancellation handled correctly")
				case <-time.After(2 * time.Second):
					t.Error("Wait() didn't return after context cancellation")
				}
			},
			timeout: 3 * time.Second,
			assertion: func(t *testing.T, setup *integrationTestSetup) {
				// Context should be done
				select {
				case <-setup.Tel.ctx.Done():
					// Expected
				default:
					t.Error("Context should be done after cancellation")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup := setupIntegrationTest(t, tt.name)
			// Don't defer cleanup since we're testing shutdown

			setup.startTelephoneAsync(t, 5*time.Second)
			time.Sleep(1 * time.Second)

			tt.action(t, setup)
			tt.assertion(t, setup)

			// Manual cleanup
			os.Remove(setup.DBPath)
		})
	}
}
