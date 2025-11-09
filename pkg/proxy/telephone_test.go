package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/verastack/telephone/pkg/auth"
	"github.com/verastack/telephone/pkg/config"
)

// TestValidateProxyRequest tests the proxy request validation logic
func TestValidateProxyRequest(t *testing.T) {
	tests := []struct {
		name        string
		payload     map[string]interface{}
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid GET request",
			payload: map[string]interface{}{
				"request_id": "550e8400-e29b-41d4-a716-446655440000",
				"method":     "GET",
				"path":       "/api/users",
				"headers": map[string]interface{}{
					"host": "example.com",
				},
			},
			expectError: false,
		},
		{
			name: "valid POST request with body",
			payload: map[string]interface{}{
				"request_id": "550e8400-e29b-41d4-a716-446655440001",
				"method":     "POST",
				"path":       "/api/users",
				"body":       `{"name":"test"}`,
				"headers": map[string]interface{}{
					"content-type": "application/json",
				},
			},
			expectError: false,
		},
		{
			name: "missing request_id",
			payload: map[string]interface{}{
				"method": "GET",
				"path":   "/api/users",
			},
			expectError: true,
			errorMsg:    "missing or invalid request_id",
		},
		{
			name: "invalid request_id format",
			payload: map[string]interface{}{
				"request_id": "not-a-uuid",
				"method":     "GET",
				"path":       "/api/users",
			},
			expectError: true,
			errorMsg:    "request_id must be a valid UUID",
		},
		{
			name: "missing method",
			payload: map[string]interface{}{
				"request_id": "550e8400-e29b-41d4-a716-446655440000",
				"path":       "/api/users",
			},
			expectError: true,
			errorMsg:    "missing or invalid method",
		},
		{
			name: "invalid HTTP method",
			payload: map[string]interface{}{
				"request_id": "550e8400-e29b-41d4-a716-446655440000",
				"method":     "INVALID",
				"path":       "/api/users",
			},
			expectError: true,
			errorMsg:    "invalid HTTP method",
		},
		{
			name: "missing path",
			payload: map[string]interface{}{
				"request_id": "550e8400-e29b-41d4-a716-446655440000",
				"method":     "GET",
			},
			expectError: true,
			errorMsg:    "missing or invalid path",
		},
		{
			name: "empty path",
			payload: map[string]interface{}{
				"request_id": "550e8400-e29b-41d4-a716-446655440000",
				"method":     "GET",
				"path":       "",
			},
			expectError: true,
			errorMsg:    "path cannot be empty",
		},
		{
			name: "invalid header value type",
			payload: map[string]interface{}{
				"request_id": "550e8400-e29b-41d4-a716-446655440000",
				"method":     "GET",
				"path":       "/api/users",
				"headers": map[string]interface{}{
					"host":  "example.com",
					"count": 123, // Invalid: not a string
				},
			},
			expectError: true,
			errorMsg:    "header value must be string",
		},
		{
			name: "valid PUT request",
			payload: map[string]interface{}{
				"request_id": "550e8400-e29b-41d4-a716-446655440000",
				"method":     "PUT",
				"path":       "/api/users/123",
			},
			expectError: false,
		},
		{
			name: "valid DELETE request",
			payload: map[string]interface{}{
				"request_id": "550e8400-e29b-41d4-a716-446655440000",
				"method":     "DELETE",
				"path":       "/api/users/123",
			},
			expectError: false,
		},
		{
			name: "valid PATCH request",
			payload: map[string]interface{}{
				"request_id": "550e8400-e29b-41d4-a716-446655440000",
				"method":     "PATCH",
				"path":       "/api/users/123",
			},
			expectError: false,
		},
		{
			name: "valid HEAD request",
			payload: map[string]interface{}{
				"request_id": "550e8400-e29b-41d4-a716-446655440000",
				"method":     "HEAD",
				"path":       "/api/users",
			},
			expectError: false,
		},
		{
			name: "valid OPTIONS request",
			payload: map[string]interface{}{
				"request_id": "550e8400-e29b-41d4-a716-446655440000",
				"method":     "OPTIONS",
				"path":       "/api/users",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateProxyRequest(tt.payload)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error containing '%s', got nil", tt.errorMsg)
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestHTTPMethodValidation tests all HTTP method validations
func TestHTTPMethodValidation(t *testing.T) {
	validMethods := []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

	for _, method := range validMethods {
		t.Run(method, func(t *testing.T) {
			payload := map[string]interface{}{
				"request_id": "550e8400-e29b-41d4-a716-446655440000",
				"method":     method,
				"path":       "/test",
			}

			err := validateProxyRequest(payload)
			if err != nil {
				t.Errorf("expected %s to be valid, got error: %v", method, err)
			}
		})
	}

	// Test invalid methods
	invalidMethods := []string{"TRACE", "CONNECT", "INVALID", ""}
	for _, method := range invalidMethods {
		t.Run("invalid_"+method, func(t *testing.T) {
			payload := map[string]interface{}{
				"request_id": "550e8400-e29b-41d4-a716-446655440000",
				"method":     method,
				"path":       "/test",
			}

			err := validateProxyRequest(payload)
			if err == nil {
				t.Errorf("expected %s to be invalid, got no error", method)
			}
		})
	}
}

// TestForwardToBackend tests forwarding requests to backend (unit test with mock server)
func TestForwardToBackend(t *testing.T) {
	// Create a test backend server
	backendCalls := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendCalls++

		// Verify request properties
		if r.Header.Get("X-Test-Header") != "test-value" {
			t.Errorf("expected X-Test-Header=test-value, got %s", r.Header.Get("X-Test-Header"))
		}

		// Return a test response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
	}))
	defer backend.Close()

	// Create minimal telephone instance
	tel := createMinimalTelephoneT(t, backend.URL)

	// Create a test proxy request
	payload := map[string]interface{}{
		"request_id":   "550e8400-e29b-41d4-a716-446655440000",
		"method":       "GET",
		"path":         "/api/test",
		"query_string": "foo=bar",
		"headers": map[string]interface{}{
			"host":          "example.com",
			"x-test-header": "test-value",
		},
		"body": "",
	}

	// Forward to backend
	resp, err := tel.forwardToBackend(payload)
	if err != nil {
		t.Fatalf("failed to forward to backend: %v", err)
	}

	// Verify response
	if resp.RequestID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("expected request_id 550e8400-e29b-41d4-a716-446655440000, got %s", resp.RequestID)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
	if !strings.Contains(resp.Body, "success") {
		t.Errorf("expected body to contain 'success', got %s", resp.Body)
	}
	if backendCalls != 1 {
		t.Errorf("expected 1 backend call, got %d", backendCalls)
	}
}

// TestConcurrentRequests tests handling multiple concurrent requests
func TestConcurrentRequests(t *testing.T) {
	requestCount := 0
	mu := sync.Mutex{}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()

		// Simulate some processing time
		time.Sleep(10 * time.Millisecond)

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer backend.Close()

	tel := createMinimalTelephoneT(t, backend.URL)

	// Send 10 concurrent requests
	numRequests := 10
	var wg sync.WaitGroup
	errors := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			payload := map[string]interface{}{
				"request_id": fmt.Sprintf("550e8400-e29b-41d4-a716-%012d", index),
				"method":     "GET",
				"path":       fmt.Sprintf("/api/test/%d", index),
				"headers":    map[string]interface{}{},
				"body":       "",
			}

			resp, err := tel.forwardToBackend(payload)
			if err != nil {
				errors <- fmt.Errorf("request %d failed: %w", index, err)
				return
			}

			if resp.Status != 200 {
				errors <- fmt.Errorf("request %d got status %d", index, resp.Status)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Error(err)
	}

	mu.Lock()
	finalCount := requestCount
	mu.Unlock()

	if finalCount != numRequests {
		t.Errorf("expected %d backend requests, got %d", numRequests, finalCount)
	}
}

// TestRequestTimeout tests that requests timeout correctly
func TestRequestTimeout(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow backend
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := &config.Config{
		BackendHost:    "localhost",
		BackendPort:    8080,
		RequestTimeout: 100 * time.Millisecond, // Short timeout
	}

	tel := createMinimalTelephoneWithConfig(t, cfg, backend.URL)

	payload := map[string]interface{}{
		"request_id": "550e8400-e29b-41d4-a716-446655440000",
		"method":     "GET",
		"path":       "/slow",
		"headers":    map[string]interface{}{},
		"body":       "",
	}

	start := time.Now()
	_, err := tel.forwardToBackend(payload)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected timeout error, got nil")
	}

	// Should timeout quickly (within 300ms buffer for overhead)
	if elapsed > 300*time.Millisecond {
		t.Errorf("expected timeout within ~100ms, took %v", elapsed)
	}
}

// TestChunkedResponse tests automatic chunking of large responses
func TestChunkedResponse(t *testing.T) {
	// Create a large response (>1MB)
	largeBody := strings.Repeat("x", 1*1024*1024+1000) // 1MB + 1000 bytes

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(largeBody))
	}))
	defer backend.Close()

	tel := createMinimalTelephoneT(t, backend.URL)

	payload := map[string]interface{}{
		"request_id": "550e8400-e29b-41d4-a716-446655440000",
		"method":     "GET",
		"path":       "/large",
		"headers":    map[string]interface{}{},
		"body":       "",
	}

	resp, err := tel.forwardToBackend(payload)
	if err != nil {
		t.Fatalf("failed to forward request: %v", err)
	}

	// Should be chunked
	if !resp.Chunked {
		t.Error("expected response to be chunked for large body")
	}

	if len(resp.Chunks) == 0 {
		t.Error("expected non-empty chunks array")
	}

	if resp.Body != "" {
		t.Error("expected empty body when chunked=true")
	}

	// Verify chunks reconstruct the original body
	var reconstructed strings.Builder
	for _, chunk := range resp.Chunks {
		reconstructed.WriteString(chunk)
	}

	if reconstructed.String() != largeBody {
		t.Errorf("reconstructed body length %d doesn't match original %d",
			reconstructed.Len(), len(largeBody))
	}
}

// TestProxyResponseStructure tests ProxyResponse JSON marshaling
func TestProxyResponseStructure(t *testing.T) {
	resp := &ProxyResponse{
		RequestID: "550e8400-e29b-41d4-a716-446655440000",
		Status:    200,
		Headers: map[string]string{
			"content-type": "application/json",
		},
		Body:    `{"status":"ok"}`,
		Chunked: false,
	}

	// Marshal to JSON
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}

	// Unmarshal back
	var unmarshaled ProxyResponse
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if unmarshaled.RequestID != resp.RequestID {
		t.Errorf("expected request_id %s, got %s", resp.RequestID, unmarshaled.RequestID)
	}

	if unmarshaled.Status != resp.Status {
		t.Errorf("expected status %d, got %d", resp.Status, unmarshaled.Status)
	}

	if unmarshaled.Chunked != resp.Chunked {
		t.Errorf("expected chunked %v, got %v", resp.Chunked, unmarshaled.Chunked)
	}
}

// TestProxyResponseChunked tests chunked response structure
func TestProxyResponseChunked(t *testing.T) {
	resp := &ProxyResponse{
		RequestID: "550e8400-e29b-41d4-a716-446655440000",
		Status:    200,
		Headers: map[string]string{
			"content-type": "text/plain",
		},
		Body:    "",
		Chunked: true,
		Chunks:  []string{"chunk1", "chunk2", "chunk3"},
	}

	// Marshal to JSON
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal chunked response: %v", err)
	}

	// Verify JSON structure
	var jsonMap map[string]interface{}
	if err := json.Unmarshal(data, &jsonMap); err != nil {
		t.Fatalf("failed to unmarshal to map: %v", err)
	}

	if jsonMap["chunked"] != true {
		t.Error("expected chunked to be true in JSON")
	}

	chunks, ok := jsonMap["chunks"].([]interface{})
	if !ok {
		t.Fatal("expected chunks to be array in JSON")
	}

	if len(chunks) != 3 {
		t.Errorf("expected 3 chunks, got %d", len(chunks))
	}
}

// TestBackendErrorHandling tests error responses from backend
func TestBackendErrorHandling(t *testing.T) {
	tests := []struct {
		name           string
		backendStatus  int
		backendBody    string
		expectedStatus int
	}{
		{
			name:           "404 Not Found",
			backendStatus:  404,
			backendBody:    "Not Found",
			expectedStatus: 404,
		},
		{
			name:           "500 Internal Server Error",
			backendStatus:  500,
			backendBody:    "Internal Server Error",
			expectedStatus: 500,
		},
		{
			name:           "401 Unauthorized",
			backendStatus:  401,
			backendBody:    "Unauthorized",
			expectedStatus: 401,
		},
		{
			name:           "403 Forbidden",
			backendStatus:  403,
			backendBody:    "Forbidden",
			expectedStatus: 403,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.backendStatus)
				w.Write([]byte(tt.backendBody))
			}))
			defer backend.Close()

			tel := createMinimalTelephoneT(t, backend.URL)

			payload := map[string]interface{}{
				"request_id": "550e8400-e29b-41d4-a716-446655440000",
				"method":     "GET",
				"path":       "/test",
				"headers":    map[string]interface{}{},
				"body":       "",
			}

			resp, err := tel.forwardToBackend(payload)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if resp.Status != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, resp.Status)
			}

			if !strings.Contains(resp.Body, tt.backendBody) {
				t.Errorf("expected body to contain '%s', got '%s'", tt.backendBody, resp.Body)
			}
		})
	}
}

// TestQueryStringForwarding tests that query strings are properly forwarded
func TestQueryStringForwarding(t *testing.T) {
	receivedQuery := ""
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer backend.Close()

	tel := createMinimalTelephoneT(t, backend.URL)

	payload := map[string]interface{}{
		"request_id":   "550e8400-e29b-41d4-a716-446655440000",
		"method":       "GET",
		"path":         "/api/test",
		"query_string": "foo=bar&baz=qux&page=1",
		"headers":      map[string]interface{}{},
		"body":         "",
	}

	_, err := tel.forwardToBackend(payload)
	if err != nil {
		t.Fatalf("failed to forward request: %v", err)
	}

	if receivedQuery != "foo=bar&baz=qux&page=1" {
		t.Errorf("expected query string 'foo=bar&baz=qux&page=1', got '%s'", receivedQuery)
	}
}

// TestPostRequestWithBody tests POST request with body forwarding
func TestPostRequestWithBody(t *testing.T) {
	receivedBody := ""
	receivedContentType := ""

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":123,"status":"created"}`))
	}))
	defer backend.Close()

	tel := createMinimalTelephoneT(t, backend.URL)

	testBody := `{"name":"Test User","email":"test@example.com"}`
	payload := map[string]interface{}{
		"request_id": "550e8400-e29b-41d4-a716-446655440000",
		"method":     "POST",
		"path":       "/api/users",
		"headers": map[string]interface{}{
			"content-type": "application/json",
		},
		"body": testBody,
	}

	resp, err := tel.forwardToBackend(payload)
	if err != nil {
		t.Fatalf("failed to forward request: %v", err)
	}

	if resp.Status != 201 {
		t.Errorf("expected status 201, got %d", resp.Status)
	}

	if receivedBody != testBody {
		t.Errorf("expected body '%s', got '%s'", testBody, receivedBody)
	}

	if receivedContentType != "application/json" {
		t.Errorf("expected content-type 'application/json', got '%s'", receivedContentType)
	}

	if !strings.Contains(resp.Body, `"id":123`) {
		t.Errorf("expected response to contain id, got: %s", resp.Body)
	}
}

// TestTokenManagement tests token getter and setter
func TestTokenManagement(t *testing.T) {
	tel := createMinimalTelephoneForTokenTest(t)

	// Test getCurrentToken
	initialToken := tel.getCurrentToken()
	if initialToken == "" {
		t.Error("expected non-empty initial token")
	}

	// Test updateToken
	newToken := createTestTokenT(t, time.Now().Add(2*time.Hour))
	newClaims, _ := auth.ParseJWTUnsafe(newToken)
	tel.updateToken(newToken, newClaims)

	updatedToken := tel.getCurrentToken()
	if updatedToken != newToken {
		t.Errorf("expected token to be updated to %s, got %s", newToken, updatedToken)
	}

	if updatedToken == initialToken {
		t.Error("expected token to change after update")
	}
}

// Helper function to create a test JWT token (testing.T version)
func createTestTokenT(t *testing.T, expiry time.Time) string {
	t.Helper()

	claims := &auth.JWTClaims{
		Sub:    "test-subject-uuid",
		JTI:    "test-jti-uuid",
		PathID: "test-path-id-uuid",
		IAT:    time.Now().Unix(),
		Exp:    expiry.Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte("test-secret-key"))
	if err != nil {
		t.Fatalf("failed to generate test token: %v", err)
	}

	return tokenString
}

// Helper function to create a minimal Telephone instance for testing (testing.T version)
func createMinimalTelephoneT(t *testing.T, backendURL string) *Telephone {
	t.Helper()

	cfg := &config.Config{
		RequestTimeout:  5 * time.Second,
		MaxResponseSize: 100 * 1024 * 1024, // 100MB default
		ChunkSize:       1024 * 1024,       // 1MB default
	}

	return createMinimalTelephoneWithConfig(t, cfg, backendURL)
}

// Helper function to create a minimal Telephone instance with custom config
func createMinimalTelephoneWithConfig(t *testing.T, cfg *config.Config, backendURL string) *Telephone {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel() })

	// Parse test server URL - it's like "http://127.0.0.1:12345"
	// We'll extract just the host:port part by removing "http://"
	hostPort := strings.TrimPrefix(backendURL, "http://")

	// Split host and port
	parts := strings.Split(hostPort, ":")
	host := parts[0]
	port := 0
	if len(parts) > 1 {
		port, _ = strconv.Atoi(parts[1])
	}

	// Create config with test backend URL components
	testCfg := &config.Config{
		BackendHost:     host,
		BackendPort:     port,
		BackendScheme:   "http",
		RequestTimeout:  cfg.RequestTimeout,
		MaxResponseSize: cfg.MaxResponseSize,
		ChunkSize:       cfg.ChunkSize,
	}

	// Create minimal telephone without WebSocket connection
	tel := &Telephone{
		config:          testCfg,
		backend:         &http.Client{Timeout: cfg.RequestTimeout},
		pendingRequests: make(map[string]*PendingRequest),
		ctx:             ctx,
		cancel:          cancel,
	}

	return tel
}

// Helper function to create minimal Telephone for token tests
func createMinimalTelephoneForTokenTest(t *testing.T) *Telephone {
	t.Helper()

	token := createTestTokenT(t, time.Now().Add(1*time.Hour))
	claims, _ := auth.ParseJWTUnsafe(token)

	cfg := &config.Config{
		BackendHost:    "localhost",
		BackendPort:    8080,
		RequestTimeout: 5 * time.Second,
		Token:          token,
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel() })

	tel := &Telephone{
		config:          cfg,
		claims:          claims,
		currentToken:    token,
		originalToken:   token,
		backend:         &http.Client{Timeout: cfg.RequestTimeout},
		pendingRequests: make(map[string]*PendingRequest),
		ctx:             ctx,
		cancel:          cancel,
	}

	return tel
}
