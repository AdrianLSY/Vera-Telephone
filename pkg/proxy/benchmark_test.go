// Package proxy provides the reverse proxy sidecar implementation.
package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/verastack/telephone/pkg/auth"
	"github.com/verastack/telephone/pkg/config"
)

// BenchmarkValidateProxyRequest benchmarks request validation.
func BenchmarkValidateProxyRequest(b *testing.B) {
	payload := map[string]interface{}{
		"request_id": "550e8400-e29b-41d4-a716-446655440000",
		"method":     "POST",
		"path":       "/api/users",
		"headers": map[string]interface{}{
			"content-type": "application/json",
			"user-agent":   "Test/1.0",
			"accept":       "application/json",
		},
		"body": `{"name":"test user","email":"test@example.com"}`,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = validateProxyRequest(payload)
	}
}

// BenchmarkForwardToBackendSmallResponse benchmarks forwarding small responses.
func BenchmarkForwardToBackendSmallResponse(b *testing.B) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","data":{"id":123}}`))
	}))
	defer backend.Close()

	tel := createMinimalTelephone(b, backend.URL)

	payload := map[string]interface{}{
		"request_id": "550e8400-e29b-41d4-a716-446655440000",
		"method":     "GET",
		"path":       "/api/test",
		"headers":    map[string]interface{}{},
		"body":       "",
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := tel.forwardToBackend(payload)
		if err != nil {
			b.Fatalf("Failed to forward: %v", err)
		}
	}
}

// BenchmarkForwardToBackendLargeResponse benchmarks forwarding large responses.
func BenchmarkForwardToBackendLargeResponse(b *testing.B) {
	largeBody := strings.Repeat("x", 1024*1024) // 1MB

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(largeBody))
	}))
	defer backend.Close()

	tel := createMinimalTelephone(b, backend.URL)

	payload := map[string]interface{}{
		"request_id": "550e8400-e29b-41d4-a716-446655440000",
		"method":     "GET",
		"path":       "/api/large",
		"headers":    map[string]interface{}{},
		"body":       "",
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := tel.forwardToBackend(payload)
		if err != nil {
			b.Fatalf("Failed to forward: %v", err)
		}
	}
}

// BenchmarkTokenUpdate benchmarks concurrent token updates.
func BenchmarkTokenUpdate(b *testing.B) {
	token := createTestToken(b, time.Now().Add(1*time.Hour))
	claims, _ := auth.ParseJWTUnsafe(token)

	cfg := &config.Config{
		BackendHost:    "localhost",
		BackendPort:    8080,
		RequestTimeout: 5 * time.Second,
		Token:          token,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tel := &Telephone{
		config:          cfg,
		claims:          claims,
		currentToken:    token,
		originalToken:   token,
		pendingRequests: make(map[string]*PendingRequest),
		ctx:             ctx,
		cancel:          cancel,
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			newToken := createTestToken(b, time.Now().Add(time.Duration(i)*time.Hour))
			newClaims, _ := auth.ParseJWTUnsafe(newToken)
			tel.updateToken(newToken, newClaims)

			i++
		}
	})
}

// BenchmarkGetCurrentToken benchmarks concurrent token reads.
func BenchmarkGetCurrentToken(b *testing.B) {
	token := createTestToken(b, time.Now().Add(1*time.Hour))
	claims, _ := auth.ParseJWTUnsafe(token)

	cfg := &config.Config{
		BackendHost:    "localhost",
		BackendPort:    8080,
		RequestTimeout: 5 * time.Second,
		Token:          token,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tel := &Telephone{
		config:          cfg,
		claims:          claims,
		currentToken:    token,
		originalToken:   token,
		pendingRequests: make(map[string]*PendingRequest),
		ctx:             ctx,
		cancel:          cancel,
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = tel.getCurrentToken()
		}
	})
}

// BenchmarkProxyResponseMarshaling would require mock client infrastructure
// This functionality is tested via integration tests

// BenchmarkProxyResponseMarshalingChunked would require mock client infrastructure
// This functionality is tested via integration tests

// BenchmarkCalculateBackoffWithJitter benchmarks backoff calculation.
func BenchmarkCalculateBackoffWithJitter(b *testing.B) {
	initial := 1 * time.Second
	maxDuration := 30 * time.Second

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		attempt := (i % 10) + 1
		_ = calculateBackoffWithJitter(attempt, initial, maxDuration)
	}
}

// BenchmarkConcurrentRequestHandling benchmarks concurrent request processing.
func BenchmarkConcurrentRequestHandling(b *testing.B) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer backend.Close()

	tel := createMinimalTelephone(b, backend.URL)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			payload := map[string]interface{}{
				"request_id": fmt.Sprintf("550e8400-e29b-41d4-a716-%012d", i),
				"method":     "GET",
				"path":       "/api/test",
				"headers":    map[string]interface{}{},
				"body":       "",
			}

			_, err := tel.forwardToBackend(payload)
			if err != nil {
				b.Fatalf("Failed to forward: %v", err)
			}

			i++
		}
	})
}

// BenchmarkHTTPMethodValidation benchmarks HTTP method validation.
func BenchmarkHTTPMethodValidation(b *testing.B) {
	methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		method := methods[i%len(methods)]
		payload := map[string]interface{}{
			"request_id": "550e8400-e29b-41d4-a716-446655440000",
			"method":     method,
			"path":       "/test",
		}
		_ = validateProxyRequest(payload)
	}
}

// BenchmarkUUIDValidation benchmarks UUID validation.
func BenchmarkUUIDValidation(b *testing.B) {
	payload := map[string]interface{}{
		"request_id": "550e8400-e29b-41d4-a716-446655440000",
		"method":     "GET",
		"path":       "/test",
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = validateProxyRequest(payload)
	}
}

// Helper for benchmarks that need testing.B instead of testing.T.
func createMinimalTelephone(tb testing.TB, backendURL string) *Telephone {
	tb.Helper()

	cfg := &config.Config{
		RequestTimeout:  5 * time.Second,
		MaxResponseSize: 100 * 1024 * 1024,
		ChunkSize:       1024 * 1024,
	}

	ctx, cancel := context.WithCancel(context.Background())

	tb.Cleanup(func() { cancel() })

	// Parse test server URL
	hostPort := strings.TrimPrefix(backendURL, "http://")
	parts := strings.Split(hostPort, ":")
	host := parts[0]

	port := 0
	if len(parts) > 1 {
		fmt.Sscanf(parts[1], "%d", &port)
	}

	testCfg := &config.Config{
		BackendHost:     host,
		BackendPort:     port,
		BackendScheme:   "http",
		RequestTimeout:  cfg.RequestTimeout,
		MaxResponseSize: cfg.MaxResponseSize,
		ChunkSize:       cfg.ChunkSize,
	}

	tel := &Telephone{
		config:          testCfg,
		backend:         &http.Client{Timeout: cfg.RequestTimeout},
		pendingRequests: make(map[string]*PendingRequest),
		ctx:             ctx,
		cancel:          cancel,
	}

	return tel
}

// createTestToken creates a test JWT token for benchmarks.
func createTestToken(tb testing.TB, expiry time.Time) string {
	tb.Helper()

	// Use a simpler token generation for benchmarks.
	// Build a fake JWT with iat and exp timestamps using strconv for efficiency.
	iat := time.Now().Unix()
	exp := expiry.Unix()

	// Build token parts separately to avoid staticcheck SA5009 false positive
	// Header: {"alg":"HS256","typ":"JWT"} base64 encoded
	header := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"

	// Payload with dynamic timestamps
	payload := `{"sub":"test-subject","jti":"test-jti","path_id":"test-path-id","iat":` +
		strconv.FormatInt(iat, 10) + `,"exp":` + strconv.FormatInt(exp, 10) + `}`

	// For benchmarks, we don't need actual base64 encoding - just a valid-looking token
	signature := "test-signature"

	return header + "." + payload + "." + signature
}
