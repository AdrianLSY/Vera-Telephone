package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/verastack/telephone/pkg/auth"
	"github.com/verastack/telephone/pkg/channels"
	"github.com/verastack/telephone/pkg/config"
	"github.com/verastack/telephone/pkg/storage"
)

// Valid HTTP methods
var validHTTPMethods = map[string]bool{
	"GET":     true,
	"POST":    true,
	"PUT":     true,
	"PATCH":   true,
	"DELETE":  true,
	"HEAD":    true,
	"OPTIONS": true,
}

// UUID validation regex
var uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

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

	// Token management with thread-safety
	originalToken string // Token from environment (never changes, used for reconnection after server restart)
	currentToken  string // Current token (may be refreshed)
	tokenMu       sync.RWMutex

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

	// Reconnection
	reconnecting  sync.Mutex
	reconnectFlag bool
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
	} else if cfg.Token != "" {
		// Fall back to environment token
		token = cfg.Token
		log.Printf("Using token from environment")
	} else {
		// No token available
		tokenStore.Close()
		return nil, fmt.Errorf("no valid token available: please provide TELEPHONE_TOKEN environment variable or ensure database has a valid token")
	}

	// Parse JWT without signature verification (tokens come from trusted sources)
	claims, err = auth.ParseJWTUnsafe(token)
	if err != nil {
		tokenStore.Close()
		return nil, fmt.Errorf("failed to parse JWT: %w", err)
	}

	// Validate claims
	if err := claims.Validate(); err != nil {
		tokenStore.Close()
		return nil, fmt.Errorf("invalid token claims: %w", err)
	}

	log.Printf("Token parsed and verified successfully:")
	log.Printf("  Path ID: %s", claims.PathID)
	log.Printf("  Subject: %s", claims.Sub)
	log.Printf("  Expires: %s (in %s)", claims.ExpiresAt(), claims.ExpiresIn())

	ctx, cancel := context.WithCancel(context.Background())

	// Store the initial/original token from environment for reconnection
	// This is needed because after server restart, refreshed tokens may not be recognized
	originalToken := cfg.Token

	// Build WebSocket URL with token
	wsURL := fmt.Sprintf("%s?token=%s&vsn=2.0.0", cfg.PlugboardURL, token)

	return &Telephone{
		config:          cfg,
		claims:          claims,
		client:          channels.NewClient(wsURL),
		originalToken:   originalToken,
		backend:         &http.Client{Timeout: cfg.RequestTimeout},
		tokenStore:      tokenStore,
		currentToken:    token,
		pendingRequests: make(map[string]*PendingRequest),
		ctx:             ctx,
		cancel:          cancel,
	}, nil
}

// Start connects to Plugboard and starts the main loop
func (t *Telephone) Start() error {
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

	// Close WebSocket client (permanent shutdown)
	if err := t.client.Close(); err != nil {
		log.Printf("Warning: error closing WebSocket client: %v", err)
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

// getCurrentToken safely retrieves the current token
func (t *Telephone) getCurrentToken() string {
	t.tokenMu.RLock()
	defer t.tokenMu.RUnlock()
	return t.currentToken
}

// updateToken safely updates the current token and claims
func (t *Telephone) updateToken(newToken string, newClaims *auth.JWTClaims) {
	t.tokenMu.Lock()
	defer t.tokenMu.Unlock()
	t.currentToken = newToken
	t.claims = newClaims
	t.config.Token = newToken
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
			// Skip heartbeat if not connected
			if !t.client.IsConnected() {
				continue
			}

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
	log.Printf("Heartbeat acknowledged")
}

// tokenRefreshLoop refreshes the JWT token at half of its lifespan
func (t *Telephone) tokenRefreshLoop() {
	defer t.wg.Done()

	for {
		// Calculate time until half-life of current token
		t.tokenMu.RLock()
		currentClaims := t.claims
		t.tokenMu.RUnlock()

		timeUntilRefresh := currentClaims.TimeUntilHalfLife()

		// If token is already past half-life, refresh immediately
		if timeUntilRefresh == 0 {
			log.Printf("Token is already at half-life, refreshing immediately")
			timeUntilRefresh = 1 * time.Second
		} else {
			log.Printf("Token will be refreshed in %s (at half-life of %s lifespan)",
				timeUntilRefresh, currentClaims.Lifespan())
		}

		// Wait until half-life or context cancellation
		select {
		case <-t.ctx.Done():
			return
		case <-time.After(timeUntilRefresh):
			// Skip token refresh if not connected
			if !t.client.IsConnected() {
				// If not connected, check again in 5 seconds
				log.Printf("Not connected, will retry token refresh in 5 seconds")
				select {
				case <-t.ctx.Done():
					return
				case <-time.After(5 * time.Second):
					continue
				}
			}

			if err := t.refreshToken(); err != nil {
				log.Printf("Token refresh error: %v", err)
				// On error, retry in 5 seconds
				select {
				case <-t.ctx.Done():
					return
				case <-time.After(5 * time.Second):
					continue
				}
			}
			// After successful refresh, loop will recalculate based on new token
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

	// Parse new token without signature verification (comes from authenticated WebSocket)
	newClaims, err := auth.ParseJWTUnsafe(newToken)
	if err != nil {
		return fmt.Errorf("failed to parse new token: %w", err)
	}

	// Validate new claims
	if err := newClaims.Validate(); err != nil {
		return fmt.Errorf("invalid new token claims: %w", err)
	}

	// Save encrypted token to database
	if err := t.tokenStore.SaveToken(newToken, newClaims.ExpiresAt()); err != nil {
		log.Printf("Warning: failed to persist token to database: %v", err)
		// Don't fail the refresh if persistence fails
	} else {
		log.Printf("Token persisted to database")
	}

	// Update in-memory token with thread safety
	t.updateToken(newToken, newClaims)

	// CRITICAL: Update the WebSocket URL with the new token
	// Plugboard updates the token_hash in its database on refresh,
	// so we must use the new token for reconnection
	newWsURL := fmt.Sprintf("%s?token=%s&vsn=2.0.0", t.config.PlugboardURL, newToken)
	t.client.UpdateURL(newWsURL)

	log.Printf("Token refreshed successfully (expires in %v)", newClaims.ExpiresIn())
	log.Printf("WebSocket URL updated with new token for reconnection")

	// Cleanup old expired tokens
	if err := t.tokenStore.CleanupExpiredTokens(); err != nil {
		log.Printf("Warning: failed to cleanup expired tokens: %v", err)
	}

	return nil
}

// validateProxyRequest validates incoming proxy request payload
func validateProxyRequest(payload map[string]interface{}) error {
	// Validate request_id
	requestID, ok := payload["request_id"].(string)
	if !ok || requestID == "" {
		return fmt.Errorf("missing or invalid request_id")
	}
	if !uuidRegex.MatchString(requestID) {
		return fmt.Errorf("request_id must be a valid UUID")
	}

	// Validate method
	method, ok := payload["method"].(string)
	if !ok || method == "" {
		return fmt.Errorf("missing or invalid method")
	}
	if !validHTTPMethods[strings.ToUpper(method)] {
		return fmt.Errorf("invalid HTTP method: %s", method)
	}

	// Validate path
	path, ok := payload["path"].(string)
	if !ok {
		return fmt.Errorf("missing or invalid path")
	}
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}

	// Validate headers if present
	if headers, ok := payload["headers"].(map[string]interface{}); ok {
		for k, v := range headers {
			if k == "" {
				return fmt.Errorf("header name cannot be empty")
			}
			if _, ok := v.(string); !ok {
				return fmt.Errorf("header value must be string for key: %s", k)
			}
		}
	}

	return nil
}

// handleProxyRequest handles an incoming proxy request from Plugboard
func (t *Telephone) handleProxyRequest(msg *channels.Message) {
	// Track active request
	t.activeRequests.Add(1)
	defer t.activeRequests.Done()

	// Validate request
	if err := validateProxyRequest(msg.Payload); err != nil {
		log.Printf("Invalid proxy request: %v", err)
		requestID, _ := msg.Payload["request_id"].(string)
		if requestID != "" {
			t.sendProxyResponse(&ProxyResponse{
				RequestID: requestID,
				Status:    400,
				Headers:   map[string]string{"Content-Type": "text/plain"},
				Body:      fmt.Sprintf("Bad request: %v", err),
				Chunked:   false,
			})
		}
		return
	}

	requestID := msg.Payload["request_id"].(string)
	method := msg.Payload["method"].(string)
	path := msg.Payload["path"].(string)

	log.Printf("Received proxy request [%s]: %s %s", requestID, method, path)

	// Forward to backend with retry logic
	resp, err := t.forwardToBackendWithRetry(msg.Payload)
	if err != nil {
		log.Printf("Error forwarding to backend [%s]: %v", requestID, err)
		t.sendProxyResponse(&ProxyResponse{
			RequestID: requestID,
			Status:    502,
			Headers:   map[string]string{"Content-Type": "text/plain"},
			Body:      fmt.Sprintf("Bad Gateway: %v", err),
			Chunked:   false,
		})
		return
	}

	// Send response back
	if err := t.sendProxyResponse(resp); err != nil {
		log.Printf("Error sending proxy response [%s]: %v", requestID, err)
	}
}

// forwardToBackendWithRetry forwards the request with retry logic for transient failures
func (t *Telephone) forwardToBackendWithRetry(payload map[string]interface{}) (*ProxyResponse, error) {
	method := payload["method"].(string)
	maxRetries := 0

	// Only retry idempotent methods
	if method == "GET" || method == "HEAD" || method == "OPTIONS" || method == "PUT" {
		maxRetries = 2
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 100ms, 200ms
			backoff := time.Duration(100*attempt) * time.Millisecond
			log.Printf("Retrying backend request (attempt %d/%d) after %v", attempt+1, maxRetries+1, backoff)
			time.Sleep(backoff)
		}

		resp, err := t.forwardToBackend(payload)
		if err == nil {
			// Check if we should retry based on status code
			if resp.Status >= 500 && resp.Status < 600 && attempt < maxRetries {
				lastErr = fmt.Errorf("server error: status %d", resp.Status)
				continue
			}
			return resp, nil
		}

		lastErr = err

		// Don't retry on context cancellation
		if t.ctx.Err() != nil {
			return nil, fmt.Errorf("request cancelled: %w", err)
		}
	}

	return nil, fmt.Errorf("backend request failed after %d attempts: %w", maxRetries+1, lastErr)
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

	// Read response body with streaming for large responses
	const maxChunkSize = 1024 * 1024 // 1MB
	var chunks []string
	var totalSize int
	buf := make([]byte, maxChunkSize)

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			chunks = append(chunks, string(buf[:n]))
			totalSize += n
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}

		// Prevent excessive memory usage
		if totalSize > 100*1024*1024 { // 100MB limit
			return nil, fmt.Errorf("response body too large: %d bytes", totalSize)
		}
	}

	// Build response headers
	responseHeaders := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			responseHeaders[k] = v[0]
		}
	}

	// Determine if response should be chunked
	chunked := len(chunks) > 1

	var body string
	if !chunked && len(chunks) > 0 {
		body = chunks[0]
	}

	log.Printf("Backend response [%s]: %d (%d bytes, %d chunks)", requestID, resp.StatusCode, totalSize, len(chunks))

	return &ProxyResponse{
		RequestID: requestID,
		Status:    resp.StatusCode,
		Headers:   responseHeaders,
		Body:      body,
		Chunked:   chunked,
		Chunks:    chunks,
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
		log.Printf("Sent chunked proxy response [%s]: %d (%d chunks)", resp.RequestID, resp.Status, len(resp.Chunks))
	} else {
		log.Printf("Sent proxy response [%s]: %d", resp.RequestID, resp.Status)
	}
	return nil
}

// Helper to read body as JSON
func readJSON(body string, v interface{}) error {
	return json.Unmarshal([]byte(body), v)
}
