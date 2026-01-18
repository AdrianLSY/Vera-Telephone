package proxy

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/verastack/telephone/pkg/auth"
	"github.com/verastack/telephone/pkg/channels"
	"github.com/verastack/telephone/pkg/config"
	"github.com/verastack/telephone/pkg/logger"
	"github.com/verastack/telephone/pkg/storage"
	"github.com/verastack/telephone/pkg/websocket"
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

// secureRandomDuration returns a cryptographically secure random duration
// in the range [0, max). This is used for jitter to prevent timing attacks
// and thundering herd problems.
func secureRandomDuration(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	var b [8]byte
	_, err := cryptorand.Read(b[:])
	if err != nil {
		// Fallback to zero jitter if crypto/rand fails (very unlikely)
		return 0
	}
	n := binary.LittleEndian.Uint64(b[:])
	return time.Duration(n % uint64(max))
}

// defaultBufferSize is the standard buffer size for the pool (1MB)
const defaultBufferSize = 1024 * 1024

// bufferPool is used to reuse byte buffers for reading large responses
// This reduces memory allocations and GC pressure
var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, defaultBufferSize)
	},
}

// PendingRequest tracks an in-flight proxy request
type PendingRequest struct {
	RequestID    string
	ResponseChan chan *ProxyResponse
	CancelFunc   context.CancelFunc
}

// ProxyResponse represents a response from the backend
type ProxyResponse struct {
	RequestID string            `json:"request_id"`
	Status    int               `json:"status"`
	Headers   map[string]string `json:"headers"`
	Body      string            `json:"body"`
	Chunked   bool              `json:"chunked"`
	Chunks    []string          `json:"chunks,omitempty"`
	Error     error             `json:"-"`
}

// Telephone is the main client that manages the WebSocket connection and proxy logic
type Telephone struct {
	config     *config.Config
	claims     *auth.JWTClaims
	client     ChannelsClient
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

	// WebSocket proxy
	wsManager *websocket.Manager

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

	// Health check server
	healthServer *healthServer
}

// New creates a new Telephone instance
func New(cfg *config.Config) (*Telephone, error) {
	// Initialize token store with configurable timeout
	tokenStore, err := storage.NewTokenStore(cfg.TokenDBPath, cfg.SecretKeyBase, cfg.DBTimeout)
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
		logger.Info("Loaded persisted token from database", "expires", expiresAt)
	} else if cfg.Token != "" {
		// Fall back to environment token
		token = cfg.Token
		logger.Info("Using token from environment")
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

	logger.Info("Token parsed and verified successfully",
		"expires_in", claims.ExpiresIn(),
	)
	// Log detailed token info at DEBUG level to avoid exposing sensitive identifiers
	logger.Debug("Token details",
		"path_id", claims.PathID,
		"subject", claims.Sub,
		"expires", claims.ExpiresAt(),
	)

	ctx, cancel := context.WithCancel(context.Background())

	// Store the initial/original token from environment for reconnection
	// This is needed because after server restart, refreshed tokens may not be recognized
	originalToken := cfg.Token

	// Build WebSocket URL (token will be added as query parameter during connection)
	// Phoenix Socket requires tokens in query params for authentication
	wsURL := cfg.PlugboardURL

	// Configure HTTP client with proper transport settings
	httpTransport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		MaxConnsPerHost:     100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		DisableCompression:  false,
	}

	t := &Telephone{
		config:          cfg,
		claims:          claims,
		client:          channels.NewClient(wsURL, token, cfg.ConnectTimeout),
		originalToken:   originalToken,
		backend:         &http.Client{Timeout: cfg.RequestTimeout, Transport: httpTransport},
		tokenStore:      tokenStore,
		currentToken:    token,
		pendingRequests: make(map[string]*PendingRequest),
		ctx:             ctx,
		cancel:          cancel,
	}

	// Initialize WebSocket manager with Telephone as the event handler
	t.wsManager = websocket.NewManager(t)

	return t, nil
}

// Start connects to Plugboard and starts the main loop
func (t *Telephone) Start() error {
	// Start health check server if configured
	if t.config.HealthCheckPort > 0 {
		t.healthServer = newHealthServer(t, t.config.HealthCheckPort)
		if err := t.healthServer.Start(); err != nil {
			return fmt.Errorf("failed to start health check server: %w", err)
		}
	}

	// Connect to WebSocket
	if err := t.client.Connect(); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	// Register event handlers
	t.client.On("proxy_req", t.handleProxyRequest)
	t.client.On("heartbeat_ack", t.handleHeartbeatAck)

	// Register WebSocket proxy event handlers
	t.client.On("ws_connect", t.handleWSConnect)
	t.client.On("ws_frame", t.handleWSFrame)
	t.client.On("ws_close", t.handleWSClose)

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

	logger.Info("Telephone started successfully")
	return nil
}

// Stop gracefully shuts down the Telephone
func (t *Telephone) Stop() error {
	logger.Info("Stopping Telephone...")

	// Close all backend WebSocket connections first
	if t.wsManager != nil {
		t.wsManager.CloseAll()
	}

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
		logger.Info("All active requests completed")
	case <-time.After(30 * time.Second):
		logger.Warn("Timeout waiting for active requests, forcing shutdown")
	}

	// Wait for background goroutines
	t.wg.Wait()

	// Close WebSocket client (permanent shutdown)
	if err := t.client.Close(); err != nil {
		logger.Warn("Error closing WebSocket client", "error", err)
	}

	// Close token store
	if err := t.tokenStore.Close(); err != nil {
		logger.Warn("Failed to close token store", "error", err)
	}

	// Stop health check server
	if t.healthServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := t.healthServer.Stop(ctx); err != nil {
			logger.Warn("Error stopping health check server", "error", err)
		}
	}

	logger.Info("Telephone stopped")
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

	logger.Info("Joining channel", "topic", topic)

	reply, err := t.client.SendAndWait(msg, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to join channel: %w", err)
	}

	if reply.IsError() {
		response := reply.GetResponse()
		return fmt.Errorf("channel join failed: %v", response["reason"])
	}

	response := reply.GetResponse()
	logger.Info("Channel join successful",
		"status", response["status"],
		"path", response["path"],
		"expires_in", response["expires_in"],
	)

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
				logger.Error("Heartbeat error", "error", err)
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

	logger.Debug("Heartbeat sent", "ref", ref)
	return nil
}

// handleHeartbeatAck handles heartbeat acknowledgment from Plugboard
func (t *Telephone) handleHeartbeatAck(msg *channels.Message) {
	logger.Debug("Heartbeat acknowledged")
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
			logger.Info("Token is already at half-life, refreshing immediately")
			timeUntilRefresh = 1 * time.Second
		} else {
			// Add jitter: ±5% randomization for unpredictability
			// This prevents multiple clients from refreshing at exactly the same time
			if timeUntilRefresh > time.Minute {
				jitterPercent := 0.05
				jitterRange := time.Duration(float64(timeUntilRefresh) * jitterPercent)
				jitter := secureRandomDuration(jitterRange*2) - jitterRange
				timeUntilRefresh += jitter
			}

			logger.Info("Token refresh scheduled",
				"refresh_in", timeUntilRefresh,
				"lifespan", currentClaims.Lifespan(),
			)
		}

		// Wait until half-life or context cancellation
		select {
		case <-t.ctx.Done():
			return
		case <-time.After(timeUntilRefresh):
			// Skip token refresh if not connected
			if !t.client.IsConnected() {
				// If not connected, check again in 5 seconds
				logger.Info("Not connected, will retry token refresh in 5 seconds")
				select {
				case <-t.ctx.Done():
					return
				case <-time.After(5 * time.Second):
					continue
				}
			}

			if err := t.refreshToken(); err != nil {
				logger.Error("Token refresh error", "error", err)
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

	logger.Info("Requesting token refresh...")

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

	// Save encrypted token to database with retry logic
	if err := t.persistTokenWithRetry(newToken, newClaims.ExpiresAt(), 3); err != nil {
		logger.Warn("Failed to persist token to database after retries", "error", err)
		// Don't fail the refresh if persistence fails - token is still valid in memory
	} else {
		logger.Info("Token persisted to database")
	}

	// Update in-memory token with thread safety
	t.updateToken(newToken, newClaims)

	// CRITICAL: Update the client's token for reconnection
	// Plugboard updates the token_hash in its database on refresh,
	// so we must use the new token for reconnection
	// Token is added as query parameter per Phoenix Socket requirement
	t.client.UpdateToken(newToken)

	logger.Info("Token refreshed successfully",
		"expires_in", newClaims.ExpiresIn(),
	)
	logger.Debug("Client token updated for reconnection")

	// Cleanup old expired tokens
	if err := t.tokenStore.CleanupExpiredTokens(); err != nil {
		logger.Warn("Failed to cleanup expired tokens", "error", err)
	}

	return nil
}

// persistTokenWithRetry attempts to save the token to the database with retries
func (t *Telephone) persistTokenWithRetry(token string, expiresAt time.Time, maxRetries int) error {
	var lastErr error
	backoff := 100 * time.Millisecond

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if err := t.tokenStore.SaveToken(token, expiresAt); err != nil {
			lastErr = err
			logger.Warn("Token persistence attempt failed",
				"attempt", attempt,
				"max_retries", maxRetries,
				"error", err,
			)

			// Don't sleep after the last attempt
			if attempt < maxRetries {
				// Check if we're shutting down
				select {
				case <-t.ctx.Done():
					return fmt.Errorf("shutdown during token persistence: %w", lastErr)
				case <-time.After(backoff):
					// Exponential backoff with jitter
					backoff = backoff * 2
					if backoff > 2*time.Second {
						backoff = 2 * time.Second
					}
				}
			}
		} else {
			return nil // Success
		}
	}

	return fmt.Errorf("token persistence failed after %d attempts: %w", maxRetries, lastErr)
}

// validateProxyRequest validates incoming proxy request payload
func validateProxyRequest(payload map[string]interface{}) error {
	// Validate request_id
	requestID, ok := payload["request_id"].(string)
	if !ok || requestID == "" {
		return fmt.Errorf("missing or invalid request_id")
	}
	// Check length first to prevent regex DoS
	// Standard UUID is exactly 36 characters (32 hex + 4 hyphens)
	if len(requestID) != 36 {
		return fmt.Errorf("request_id must be exactly 36 characters (UUID format)")
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
		logger.Error("Invalid proxy request", "error", err)
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

	logger.Info("Received proxy request",
		"request_id", requestID,
		"method", method,
		"path", path,
	)

	// Forward to backend
	resp, err := t.forwardToBackend(msg.Payload)
	if err != nil {
		logger.Error("Error forwarding to backend",
			"request_id", requestID,
			"error", err,
		)
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
		logger.Error("Error sending proxy response",
			"request_id", requestID,
			"error", err,
		)
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
	chunkSize := t.config.ChunkSize
	var chunks []string
	var totalSize int

	// Get buffer from pool and ensure it's the right size
	buf := bufferPool.Get().([]byte)
	bufferFromPool := true
	if len(buf) < chunkSize {
		// Need a larger buffer - create one but don't return it to pool
		buf = make([]byte, chunkSize)
		bufferFromPool = false
	}
	defer func() {
		// Only return standard-sized buffers to the pool to prevent memory growth
		if bufferFromPool && len(buf) == defaultBufferSize {
			bufferPool.Put(buf)
		}
	}()

	for {
		n, err := resp.Body.Read(buf[:chunkSize])
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
		if int64(totalSize) > t.config.MaxResponseSize {
			return nil, fmt.Errorf("response body too large: %d bytes (max: %d bytes)", totalSize, t.config.MaxResponseSize)
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

	logger.Info("Backend response",
		"request_id", requestID,
		"status", resp.StatusCode,
		"bytes", totalSize,
		"chunks", len(chunks),
	)

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
		logger.Debug("Sent chunked proxy response",
			"request_id", resp.RequestID,
			"status", resp.Status,
			"chunks", len(resp.Chunks),
		)
	} else {
		logger.Debug("Sent proxy response",
			"request_id", resp.RequestID,
			"status", resp.Status,
		)
	}
	return nil
}
