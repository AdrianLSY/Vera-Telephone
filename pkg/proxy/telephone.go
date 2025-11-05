package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/verastack/telephone/pkg/auth"
	"github.com/verastack/telephone/pkg/channels"
	"github.com/verastack/telephone/pkg/config"
	"github.com/verastack/telephone/pkg/storage"
)

// PendingRequest tracks an in-flight proxy request
type PendingRequest struct {
	RequestID    string
	ResponseChan chan *ProxyResponse
	CancelFunc   context.CancelFunc
}

// ProxyResponse represents a response from the backend
type ProxyResponse struct {
	RequestID string                 `json:"request_id"`
	Status    int                    `json:"status"`
	Headers   map[string]string      `json:"headers"`
	Body      string                 `json:"body"`
	Chunked   bool                   `json:"chunked"`
	Chunks    []string               `json:"chunks,omitempty"`
	Error     error                  `json:"-"`
}

// Telephone is the main client that manages the WebSocket connection and proxy logic
type Telephone struct {
	config     *config.Config
	claims     *auth.JWTClaims
	client     *channels.Client
	backend    *http.Client
	tokenStore *storage.TokenStore

	// Request correlation
	pendingRequests map[string]*PendingRequest
	requestLock     sync.RWMutex
	activeRequests  sync.WaitGroup

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Heartbeat
	lastHeartbeat time.Time
	heartbeatLock sync.RWMutex
}

// New creates a new Telephone instance
func New(cfg *config.Config) (*Telephone, error) {
	// Initialize token store
	tokenStore, err := storage.NewTokenStore(cfg.TokenDBPath, cfg.SecretKeyBase)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize token store: %w", err)
	}

	// Try to load persisted token first
	var token string
	var claims *auth.JWTClaims

	persistedToken, expiresAt, err := tokenStore.LoadToken()
	if err == nil && expiresAt.After(time.Now()) {
		// Use persisted token if it's still valid
		token = persistedToken
		log.Printf("Loaded persisted token from database (expires: %s)", expiresAt)
	} else {
		// Fall back to environment token
		token = cfg.Token
		log.Printf("Using token from environment")
	}

	// Parse JWT to get claims
	claims, err = auth.ParseJWT(token)
	if err != nil {
		tokenStore.Close()
		return nil, fmt.Errorf("failed to parse JWT: %w", err)
	}

	// Check if token is expired
	if claims.IsExpired() {
		tokenStore.Close()
		return nil, fmt.Errorf("token is expired (expired at %s)", claims.ExpiresAt())
	}

	// Update config with the token we're using (might be from DB)
	cfg.Token = token

	log.Printf("Token parsed successfully:")
	log.Printf("  Path ID: %s", claims.PathID)
	log.Printf("  Subject: %s", claims.Sub)
	log.Printf("  Expires: %s (in %s)", claims.ExpiresAt(), claims.ExpiresIn())

	ctx, cancel := context.WithCancel(context.Background())

	// Build WebSocket URL with token
	wsURL := fmt.Sprintf("%s?token=%s&vsn=2.0.0", cfg.PlugboardURL, cfg.Token)

	return &Telephone{
		config:          cfg,
		claims:          claims,
		client:          channels.NewClient(wsURL),
		backend:         &http.Client{Timeout: cfg.RequestTimeout},
		tokenStore:      tokenStore,
		pendingRequests: make(map[string]*PendingRequest),
		ctx:             ctx,
		cancel:          cancel,
	}, nil
}

// Start connects to Plugboard and starts the main loop
func (t *Telephone) Start() error {
	// Start health check server
	if err := t.StartHealthServer(t.config.HealthPort); err != nil {
		return fmt.Errorf("failed to start health server: %w", err)
	}

	// Connect to WebSocket
	if err := t.client.Connect(); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	// Register event handlers
	t.client.On("proxy_req", t.handleProxyRequest)
	t.client.On("heartbeat_ack", t.handleHeartbeatAck)

	// Join the channel
	if err := t.joinChannel(); err != nil {
		return fmt.Errorf("failed to join channel: %w", err)
	}

	// Start heartbeat
	t.wg.Add(1)
	go t.heartbeatLoop()

	// Start token refresh
	t.wg.Add(1)
	go t.tokenRefreshLoop()

	// Start connection monitor
	t.wg.Add(1)
	go t.monitorConnection()

	log.Println("Telephone started successfully")
	return nil
}

// Stop gracefully shuts down the Telephone
func (t *Telephone) Stop() error {
	log.Println("Stopping Telephone...")

	// Cancel context to stop background goroutines
	t.cancel()

	// Wait for active requests to complete (with timeout)
	done := make(chan struct{})
	go func() {
		t.activeRequests.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("All active requests completed")
	case <-time.After(30 * time.Second):
		log.Println("Timeout waiting for active requests, forcing shutdown")
	}

	// Wait for background goroutines
	t.wg.Wait()

	// Disconnect from WebSocket
	if err := t.client.Disconnect(); err != nil {
		return err
	}

	// Close token store
	if err := t.tokenStore.Close(); err != nil {
		log.Printf("Warning: failed to close token store: %v", err)
	}

	log.Println("Telephone stopped")
	return nil
}

// Wait blocks until the Telephone is stopped
func (t *Telephone) Wait() {
	<-t.ctx.Done()
}

// joinChannel joins the telephone channel
func (t *Telephone) joinChannel() error {
	topic := fmt.Sprintf("telephone:%s", t.claims.PathID)
	ref := t.client.NextRef()

	msg := channels.NewMessage(topic, "phx_join", ref, map[string]interface{}{})

	log.Printf("Joining channel: %s", topic)

	reply, err := t.client.SendAndWait(msg, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to join channel: %w", err)
	}

	if reply.IsError() {
		response := reply.GetResponse()
		return fmt.Errorf("channel join failed: %v", response["reason"])
	}

	response := reply.GetResponse()
	log.Printf("Channel join successful:")
	log.Printf("  Status: %s", response["status"])
	log.Printf("  Path: %s", response["path"])
	log.Printf("  Expires in: %v seconds", response["expires_in"])

	return nil
}

// heartbeatLoop sends periodic heartbeats
func (t *Telephone) heartbeatLoop() {
	defer t.wg.Done()

	ticker := time.NewTicker(t.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			if err := t.sendHeartbeat(); err != nil {
				log.Printf("Heartbeat error: %v", err)
			}
		}
	}
}

// sendHeartbeat sends a heartbeat message
func (t *Telephone) sendHeartbeat() error {
	topic := fmt.Sprintf("telephone:%s", t.claims.PathID)
	ref := t.client.NextRef()

	payload := map[string]interface{}{
		"ts": time.Now().Unix(),
	}

	msg := channels.NewMessage(topic, "heartbeat", ref, payload)

	if err := t.client.Send(msg); err != nil {
		return err
	}

	t.heartbeatLock.Lock()
	t.lastHeartbeat = time.Now()
	t.heartbeatLock.Unlock()

	log.Printf("Heartbeat sent (ref: %s)", ref)
	return nil
}

// handleHeartbeatAck handles heartbeat acknowledgment from Plugboard
func (t *Telephone) handleHeartbeatAck(msg *channels.Message) {
	// Just log that we received it - the connection is healthy
	log.Printf("Heartbeat acknowledged")
}

// tokenRefreshLoop periodically refreshes the JWT token
func (t *Telephone) tokenRefreshLoop() {
	defer t.wg.Done()

	ticker := time.NewTicker(t.config.TokenRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			if err := t.refreshToken(); err != nil {
				log.Printf("Token refresh error: %v", err)
			}
		}
	}
}

// refreshToken requests a new JWT token
func (t *Telephone) refreshToken() error {
	topic := fmt.Sprintf("telephone:%s", t.claims.PathID)
	ref := t.client.NextRef()

	msg := channels.NewMessage(topic, "refresh_token", ref, map[string]interface{}{})

	log.Printf("Requesting token refresh...")

	reply, err := t.client.SendAndWait(msg, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to refresh token: %w", err)
	}

	if reply.IsError() {
		response := reply.GetResponse()
		return fmt.Errorf("token refresh failed: %v", response["reason"])
	}

	response := reply.GetResponse()
	newToken, ok := response["token"].(string)
	if !ok {
		return fmt.Errorf("no token in refresh response")
	}

	// Re-parse claims to get new expiry
	newClaims, err := auth.ParseJWT(newToken)
	if err != nil {
		return fmt.Errorf("failed to parse new token: %w", err)
	}

	// Save encrypted token to database
	if err := t.tokenStore.SaveToken(newToken, newClaims.ExpiresAt()); err != nil {
		log.Printf("Warning: failed to persist token to database: %v", err)
		// Don't fail the refresh if persistence fails
	} else {
		log.Printf("Token persisted to database")
	}

	// Update in-memory token
	t.config.Token = newToken
	t.claims = newClaims

	log.Printf("Token refreshed successfully (expires in %v)", newClaims.ExpiresIn())

	// Cleanup old expired tokens
	if err := t.tokenStore.CleanupExpiredTokens(); err != nil {
		log.Printf("Warning: failed to cleanup expired tokens: %v", err)
	}

	return nil
}

// handleProxyRequest handles an incoming proxy request from Plugboard
func (t *Telephone) handleProxyRequest(msg *channels.Message) {
	// Track active request
	t.activeRequests.Add(1)
	defer t.activeRequests.Done()

	log.Printf("Received proxy request: %s %s", msg.Payload["method"], msg.Payload["path"])

	requestID, ok := msg.Payload["request_id"].(string)
	if !ok {
		log.Printf("Missing request_id in proxy request")
		return
	}

	// Forward to backend
	resp, err := t.forwardToBackend(msg.Payload)
	if err != nil {
		log.Printf("Error forwarding to backend: %v", err)
		// Send error response
		t.sendProxyResponse(&ProxyResponse{
			RequestID: requestID,
			Status:    500,
			Headers:   map[string]string{"Content-Type": "text/plain"},
			Body:      fmt.Sprintf("Internal error: %v", err),
			Chunked:   false,
		})
		return
	}

	// Send response back
	if err := t.sendProxyResponse(resp); err != nil {
		log.Printf("Error sending proxy response: %v", err)
	}
}

// forwardToBackend forwards the request to the local backend
func (t *Telephone) forwardToBackend(payload map[string]interface{}) (*ProxyResponse, error) {
	requestID := payload["request_id"].(string)
	method := payload["method"].(string)
	path := payload["path"].(string)

	// Build backend URL
	backendURL := fmt.Sprintf("%s%s", t.config.BackendURL(), path)

	// Add query string if present
	if queryString, ok := payload["query_string"].(string); ok && queryString != "" {
		backendURL += "?" + queryString
	}

	log.Printf("Forwarding %s %s to backend", method, backendURL)

	// Handle request body for POST/PUT/PATCH
	var bodyReader io.Reader
	if body, ok := payload["body"].(string); ok && body != "" {
		bodyReader = strings.NewReader(body)
	}

	// Create request with timeout context
	ctx, cancel := context.WithTimeout(t.ctx, t.config.RequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, backendURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Copy headers
	if headers, ok := payload["headers"].(map[string]interface{}); ok {
		for k, v := range headers {
			if str, ok := v.(string); ok {
				req.Header.Set(k, str)
			}
		}
	}

	// Execute request
	resp, err := t.backend.Do(req)
	if err != nil {
		return nil, fmt.Errorf("backend request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	var body []byte
	if resp.Body != nil {
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}
	}

	// Build response headers
	responseHeaders := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			responseHeaders[k] = v[0]
		}
	}

	// Check if response should be chunked (> 1MB)
	const maxChunkSize = 1024 * 1024 // 1MB
	bodyStr := string(body)

	if len(body) > maxChunkSize {
		// Split into chunks
		chunks := make([]string, 0)
		for i := 0; i < len(bodyStr); i += maxChunkSize {
			end := i + maxChunkSize
			if end > len(bodyStr) {
				end = len(bodyStr)
			}
			chunks = append(chunks, bodyStr[i:end])
		}

		return &ProxyResponse{
			RequestID: requestID,
			Status:    resp.StatusCode,
			Headers:   responseHeaders,
			Body:      "",
			Chunked:   true,
			Chunks:    chunks,
		}, nil
	}

	return &ProxyResponse{
		RequestID: requestID,
		Status:    resp.StatusCode,
		Headers:   responseHeaders,
		Body:      bodyStr,
		Chunked:   false,
	}, nil
}

// sendProxyResponse sends a proxy response back to Plugboard
func (t *Telephone) sendProxyResponse(resp *ProxyResponse) error {
	topic := fmt.Sprintf("telephone:%s", t.claims.PathID)
	ref := t.client.NextRef()

	payload := map[string]interface{}{
		"request_id": resp.RequestID,
		"status":     resp.Status,
		"headers":    resp.Headers,
		"body":       resp.Body,
		"chunked":    resp.Chunked,
	}

	// Add chunks if present
	if resp.Chunked && len(resp.Chunks) > 0 {
		payload["chunks"] = resp.Chunks
	}

	msg := channels.NewMessage(topic, "proxy_res", ref, payload)

	if err := t.client.Send(msg); err != nil {
		return fmt.Errorf("failed to send proxy response: %w", err)
	}

	if resp.Chunked {
		log.Printf("Sent chunked proxy response for request %s: %d (%d chunks)", resp.RequestID, resp.Status, len(resp.Chunks))
	} else {
		log.Printf("Sent proxy response for request %s: %d", resp.RequestID, resp.Status)
	}
	return nil
}

// Helper to read body as JSON
func readJSON(body string, v interface{}) error {
	return json.Unmarshal([]byte(body), v)
}
