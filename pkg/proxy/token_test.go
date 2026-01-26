package proxy

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/verastack/telephone/pkg/auth"
	"github.com/verastack/telephone/pkg/channels"
	"github.com/verastack/telephone/pkg/config"
)

// TestTokenRefreshSuccess tests successful token refresh.
func TestTokenRefreshSuccess(t *testing.T) {
	oldToken := createTestTokenT(t, time.Now().Add(1*time.Hour))
	newToken := createTestTokenT(t, time.Now().Add(2*time.Hour))
	oldClaims, _ := auth.ParseJWTUnsafe(oldToken)

	cfg := &config.Config{
		BackendHost:     "localhost",
		BackendPort:     8080,
		RequestTimeout:  5 * time.Second,
		Token:           oldToken,
		PlugboardURL:    "ws://localhost:4000/telephone/websocket",
		TokenDBPath:     t.TempDir() + "/test.db",
		SecretKeyBase:   "test-secret-key-base-at-least-64-characters-long-for-security-purposes",
		DBTimeout:       5 * time.Second,
		MaxResponseSize: 100 * 1024 * 1024,
		ChunkSize:       1024 * 1024,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockClient := &MockChannelsClient{
		connected:    true,
		messages:     make(chan *channels.Message, 10),
		sentMessages: make([]*channels.Message, 0),
		refreshToken: newToken,
	}

	tel := &Telephone{
		config:          cfg,
		claims:          oldClaims,
		client:          mockClient,
		currentToken:    oldToken,
		originalToken:   oldToken,
		pendingRequests: make(map[string]*PendingRequest),
		ctx:             ctx,
		cancel:          cancel,
	}

	// Initialize token store
	tokenStore, err := initTestTokenStore(cfg)
	if err != nil {
		t.Fatalf("Failed to create token store: %v", err)
	}

	tel.tokenStore = tokenStore

	defer tokenStore.Close()

	// Refresh token
	err = tel.refreshToken()
	if err != nil {
		t.Fatalf("Expected successful refresh, got error: %v", err)
	}

	// Verify token was updated
	updatedToken := tel.getCurrentToken()
	if updatedToken != newToken {
		t.Errorf("Expected token to be updated to %s, got %s", newToken, updatedToken)
	}

	// Verify client token was updated via UpdateToken (not in URL for security)
	mockClient.mu.Lock()
	if mockClient.lastUpdatedToken != newToken {
		t.Errorf("Expected client token to be updated to %s, got %s", newToken, mockClient.lastUpdatedToken)
	}
	mockClient.mu.Unlock()
}

// TestTokenRefreshFailure tests token refresh failure handling.
func TestTokenRefreshFailure(t *testing.T) {
	oldToken := createTestTokenT(t, time.Now().Add(1*time.Hour))
	oldClaims, _ := auth.ParseJWTUnsafe(oldToken)

	cfg := &config.Config{
		BackendHost:    "localhost",
		BackendPort:    8080,
		RequestTimeout: 5 * time.Second,
		Token:          oldToken,
		PlugboardURL:   "ws://localhost:4000/telephone/websocket",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockClient := &MockChannelsClient{
		connected:    true,
		messages:     make(chan *channels.Message, 10),
		sentMessages: make([]*channels.Message, 0),
		refreshError: fmt.Errorf("refresh failed"),
	}

	tel := &Telephone{
		config:          cfg,
		claims:          oldClaims,
		client:          mockClient,
		currentToken:    oldToken,
		originalToken:   oldToken,
		pendingRequests: make(map[string]*PendingRequest),
		ctx:             ctx,
		cancel:          cancel,
	}

	// Refresh token
	err := tel.refreshToken()
	if err == nil {
		t.Error("Expected refresh to fail, got success")
	}

	// Verify token was NOT updated
	currentToken := tel.getCurrentToken()
	if currentToken != oldToken {
		t.Errorf("Expected token to remain unchanged, got %s", currentToken)
	}
}

// TestGetCurrentTokenThreadSafety tests concurrent access to getCurrentToken.
func TestGetCurrentTokenThreadSafety(t *testing.T) {
	token := createTestTokenT(t, time.Now().Add(1*time.Hour))
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

	// Concurrently read and write token
	var wg sync.WaitGroup

	numReaders := 10
	numWriters := 5

	// Start readers
	for i := 0; i < numReaders; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := 0; j < 100; j++ {
				_ = tel.getCurrentToken()
			}
		}()
	}

	// Start writers
	for i := 0; i < numWriters; i++ {
		wg.Add(1)

		go func(index int) {
			defer wg.Done()

			for j := 0; j < 50; j++ {
				newToken := createTestTokenT(t, time.Now().Add(time.Duration(index+j)*time.Hour))
				newClaims, _ := auth.ParseJWTUnsafe(newToken)
				tel.updateToken(newToken, newClaims)
				time.Sleep(1 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	// If we get here without data races, test passes
	finalToken := tel.getCurrentToken()
	if finalToken == "" {
		t.Error("Token should not be empty after concurrent access")
	}
}

// TestUpdateTokenThreadSafety tests concurrent calls to updateToken.
func TestUpdateTokenThreadSafety(t *testing.T) {
	token := createTestTokenT(t, time.Now().Add(1*time.Hour))
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

	// Concurrently update token
	var wg sync.WaitGroup

	numUpdaters := 10

	for i := 0; i < numUpdaters; i++ {
		wg.Add(1)

		go func(index int) {
			defer wg.Done()

			for j := 0; j < 100; j++ {
				newToken := createTestTokenT(t, time.Now().Add(time.Duration(index+j)*time.Hour))
				newClaims, _ := auth.ParseJWTUnsafe(newToken)
				tel.updateToken(newToken, newClaims)
			}
		}(i)
	}

	wg.Wait()

	// Verify we end up with a valid token
	finalToken := tel.getCurrentToken()
	if finalToken == "" {
		t.Error("Expected valid token after concurrent updates")
	}

	// Verify claims are consistent with token
	tel.tokenMu.RLock()
	finalClaims := tel.claims
	tel.tokenMu.RUnlock()

	if finalClaims == nil {
		t.Error("Expected valid claims after concurrent updates")
	}
}

// createTestJWTToken creates a test JWT token for testing.
//
//nolint:unused // Helper function for future tests
func createTestJWTToken(t *testing.T, expiry time.Time) string {
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
