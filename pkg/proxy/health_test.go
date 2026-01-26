package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/verastack/telephone/pkg/auth"
	"github.com/verastack/telephone/pkg/channels"
	"github.com/verastack/telephone/pkg/config"
)

// mockHealthClient implements ChannelsClient for health check testing.
type mockHealthClient struct {
	mu        sync.Mutex
	connected bool
}

func (m *mockHealthClient) Connect() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = true

	return nil
}

func (m *mockHealthClient) Disconnect() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false

	return nil
}

func (m *mockHealthClient) Close() error {
	return m.Disconnect()
}

func (m *mockHealthClient) IsConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.connected
}

func (m *mockHealthClient) Send(_ *channels.Message) error {
	return nil
}

func (m *mockHealthClient) SendAndWait(_ *channels.Message, _ time.Duration) (*channels.Message, error) {
	return nil, nil
}

func (m *mockHealthClient) On(_ string, _ channels.MessageHandler) {}

func (m *mockHealthClient) NextRef() string {
	return "1"
}

func (m *mockHealthClient) UpdateURL(_ string) {}

func (m *mockHealthClient) UpdateToken(_ string) {}

// createTestTelephoneForHealth creates a minimal Telephone for health testing.
func createTestTelephoneForHealth(t *testing.T, connected bool, tokenExpiry time.Duration) *Telephone {
	t.Helper()

	token := createTestTokenT(t, time.Now().Add(tokenExpiry))
	claims, _ := auth.ParseJWTUnsafe(token)

	ctx, cancel := context.WithCancel(context.Background())

	t.Cleanup(func() { cancel() })

	mockClient := &mockHealthClient{connected: connected}

	return &Telephone{
		config: &config.Config{
			HealthCheckPort: 0, // Not used in tests
		},
		claims:          claims,
		client:          mockClient,
		currentToken:    token,
		pendingRequests: make(map[string]*PendingRequest),
		ctx:             ctx,
		cancel:          cancel,
	}
}

func TestHealthEndpoint(t *testing.T) {
	tests := []struct {
		name           string
		connected      bool
		tokenExpiry    time.Duration
		expectedStatus int
		expectedHealth string
	}{
		{
			name:           "healthy - connected with valid token",
			connected:      true,
			tokenExpiry:    1 * time.Hour,
			expectedStatus: http.StatusOK,
			expectedHealth: "healthy",
		},
		{
			name:           "degraded - connected but token expired",
			connected:      true,
			tokenExpiry:    -1 * time.Hour, // Expired
			expectedStatus: http.StatusServiceUnavailable,
			expectedHealth: "degraded",
		},
		{
			name:           "unhealthy - not connected",
			connected:      false,
			tokenExpiry:    1 * time.Hour,
			expectedStatus: http.StatusServiceUnavailable,
			expectedHealth: "unhealthy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tel := createTestTelephoneForHealth(t, tt.connected, tt.tokenExpiry)
			hs := newHealthServer(tel, 0)

			req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
			w := httptest.NewRecorder()

			hs.handleHealth(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			var status HealthStatus
			if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if status.Status != tt.expectedHealth {
				t.Errorf("Expected health status %s, got %s", tt.expectedHealth, status.Status)
			}

			if status.Connected != tt.connected {
				t.Errorf("Expected connected %v, got %v", tt.connected, status.Connected)
			}
		})
	}
}

func TestReadyEndpoint(t *testing.T) {
	tests := []struct {
		name           string
		connected      bool
		tokenExpiry    time.Duration
		expectedStatus int
	}{
		{
			name:           "ready - connected with valid token",
			connected:      true,
			tokenExpiry:    1 * time.Hour,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "not ready - not connected",
			connected:      false,
			tokenExpiry:    1 * time.Hour,
			expectedStatus: http.StatusServiceUnavailable,
		},
		{
			name:           "not ready - token expired",
			connected:      true,
			tokenExpiry:    -1 * time.Hour,
			expectedStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tel := createTestTelephoneForHealth(t, tt.connected, tt.tokenExpiry)
			hs := newHealthServer(tel, 0)

			req := httptest.NewRequest(http.MethodGet, "/ready", http.NoBody)
			w := httptest.NewRecorder()

			hs.handleReady(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestLiveEndpoint(t *testing.T) {
	tel := createTestTelephoneForHealth(t, false, 1*time.Hour)
	hs := newHealthServer(tel, 0)

	req := httptest.NewRequest(http.MethodGet, "/live", http.NoBody)
	w := httptest.NewRecorder()

	hs.handleLive(w, req)

	// Liveness should always return OK if the process is running
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	if w.Body.String() != "alive" {
		t.Errorf("Expected body 'alive', got '%s'", w.Body.String())
	}
}

func TestHealthEndpointMethodNotAllowed(t *testing.T) {
	tel := createTestTelephoneForHealth(t, true, 1*time.Hour)
	hs := newHealthServer(tel, 0)

	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/health", http.NoBody)
			w := httptest.NewRecorder()

			hs.handleHealth(w, req)

			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("Expected status %d for %s, got %d", http.StatusMethodNotAllowed, method, w.Code)
			}
		})
	}
}

func TestHealthStatusFields(t *testing.T) {
	tel := createTestTelephoneForHealth(t, true, 1*time.Hour)

	// Set last heartbeat
	tel.heartbeatLock.Lock()
	tel.lastHeartbeat = time.Now().Add(-30 * time.Second)
	tel.heartbeatLock.Unlock()

	hs := newHealthServer(tel, 0)

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	w := httptest.NewRecorder()

	hs.handleHealth(w, req)

	var status HealthStatus
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Check that all expected fields are present
	if status.Uptime == "" {
		t.Error("Expected uptime to be set")
	}

	if status.LastHB == "" {
		t.Error("Expected last_heartbeat to be set")
	}

	if status.TokenExpiry == "" {
		t.Error("Expected token_expiry to be set")
	}

	if status.Version == "" {
		t.Error("Expected version to be set")
	}
}

func TestIsTokenValid(t *testing.T) {
	tests := []struct {
		name        string
		tokenExpiry time.Duration
		expected    bool
	}{
		{
			name:        "valid token",
			tokenExpiry: 1 * time.Hour,
			expected:    true,
		},
		{
			name:        "expired token",
			tokenExpiry: -1 * time.Hour,
			expected:    false,
		},
		{
			name:        "about to expire",
			tokenExpiry: 1 * time.Second,
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tel := createTestTelephoneForHealth(t, true, tt.tokenExpiry)

			result := tel.isTokenValid()
			if result != tt.expected {
				t.Errorf("Expected isTokenValid() = %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestHealthServerStartStop(t *testing.T) {
	tel := createTestTelephoneForHealth(t, true, 1*time.Hour)

	// Use a random high port to avoid conflicts
	hs := newHealthServer(tel, 0)

	// Test that we can't stop a server that hasn't started
	ctx := context.Background()
	// Note: We don't test actual Start() here because it binds to a port
	// and could cause issues in CI. The handler tests above cover the logic.
	if err := hs.Stop(ctx); err != nil {
		t.Errorf("Stop on non-running server should not error: %v", err)
	}
}
