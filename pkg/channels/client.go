package channels

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/verastack/telephone/pkg/logger"
)

// Client represents a Phoenix Channels WebSocket client
type Client struct {
	url      string
	token    string // JWT token for authentication (sent via header, not URL)
	conn     *websocket.Conn
	connLock sync.RWMutex
	urlMu    sync.RWMutex // Protects URL updates
	tokenMu  sync.RWMutex // Protects token updates

	// Connection settings
	connectTimeout time.Duration

	// Message handling
	handlers    map[string]MessageHandler
	handlerLock sync.RWMutex

	// Request tracking for request/response pattern
	pendingRequests map[string]chan *Message
	requestLock     sync.RWMutex

	refCounter uint64

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Per-connection context for graceful readLoop shutdown
	readCtx    context.Context
	readCancel context.CancelFunc

	connected atomic.Bool
}

// MessageHandler is a function that handles incoming messages
type MessageHandler func(*Message)

// NewClient creates a new Phoenix Channels client using V2 protocol (JSON array format)
// The token is passed separately and appended as a query parameter during connection
// (Phoenix Socket requires tokens in query params, not HTTP headers)
// Protocol version (vsn=2.0.0) is sent as query parameter to select V2 serializer on server
// Token leakage is mitigated via GetCleanURL() for logging, short-lived tokens, and TLS in production
func NewClient(url string, token string, connectTimeout time.Duration) *Client {
	ctx, cancel := context.WithCancel(context.Background())

	return &Client{
		url:             url,
		token:           token,
		connectTimeout:  connectTimeout,
		handlers:        make(map[string]MessageHandler),
		pendingRequests: make(map[string]chan *Message),
		ctx:             ctx,
		cancel:          cancel,
	}
}

// UpdateURL updates the WebSocket URL (useful for reconnection)
func (c *Client) UpdateURL(newURL string) {
	c.urlMu.Lock()
	defer c.urlMu.Unlock()
	c.url = newURL
}

// UpdateToken updates the authentication token (used after token refresh)
func (c *Client) UpdateToken(newToken string) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	c.token = newToken
}

// GetToken returns the current token
func (c *Client) GetToken() string {
	c.tokenMu.RLock()
	defer c.tokenMu.RUnlock()
	return c.token
}

// GetURL returns the current WebSocket URL
func (c *Client) GetURL() string {
	c.urlMu.RLock()
	defer c.urlMu.RUnlock()
	return c.url
}

// buildWSURL constructs the WebSocket URL with token as query parameter
// Phoenix Socket requires tokens in query params (not HTTP headers) for authentication
func (c *Client) buildWSURL() (string, error) {
	c.urlMu.RLock()
	baseURL := c.url
	c.urlMu.RUnlock()

	c.tokenMu.RLock()
	token := c.token
	c.tokenMu.RUnlock()

	// Parse the base URL
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid WebSocket URL: %w", err)
	}

	// Add token and protocol version as query parameters
	// Phoenix Socket requires both for proper authentication and serializer selection
	query := parsedURL.Query()

	if token != "" {
		query.Set("token", token)
	}

	// Protocol version - tells Phoenix to use V2 serializer (JSON arrays)
	// Phoenix.js default: DEFAULT_VSN = "2.0.0"
	query.Set("vsn", "2.0.0")

	parsedURL.RawQuery = query.Encode()

	return parsedURL.String(), nil
}

// Connect establishes a WebSocket connection
func (c *Client) Connect() error {
	c.connLock.Lock()
	defer c.connLock.Unlock()

	// Clean up any stale connection first
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	// Wait for previous readLoop to finish if it exists
	if c.readCancel != nil {
		c.readCancel()
		// Note: We can't wait here because we hold the lock
	}

	// Build WebSocket URL with token as query parameter
	// Phoenix Socket requires tokens in query params (not HTTP headers) for authentication
	wsURL, err := c.buildWSURL()
	if err != nil {
		return fmt.Errorf("failed to build WebSocket URL: %w", err)
	}

	// Create dialer with timeout
	dialer := websocket.Dialer{
		HandshakeTimeout: c.connectTimeout,
	}

	// Protocol version (vsn=2.0.0) is included in wsURL query params
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", c.GetCleanURL(), err)
	}

	c.conn = conn
	c.connected.Store(true)

	// Create new context for this connection's readLoop
	c.readCtx, c.readCancel = context.WithCancel(c.ctx)

	// Start read loop
	c.wg.Add(1)
	go c.readLoop()

	// Use clean URL for logging to avoid token leakage
	logger.Info("Connected to WebSocket", "url", c.GetCleanURL())
	return nil
}

// Disconnect closes the WebSocket connection
func (c *Client) Disconnect() error {
	// Cancel readLoop context first
	if c.readCancel != nil {
		c.readCancel()
	}

	// Hold lock during entire disconnect to prevent race with readLoop cleanup
	c.connLock.Lock()
	defer c.connLock.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	c.connected.Store(false)

	// Note: We don't cancel the main context here anymore
	// That's only done on final shutdown via Close()

	return nil
}

// Close permanently shuts down the client (for final cleanup)
func (c *Client) Close() error {
	c.Disconnect()
	c.cancel()
	c.wg.Wait()
	return nil
}

// IsConnected returns true if the client is connected
func (c *Client) IsConnected() bool {
	return c.connected.Load()
}

// Send sends a message to the server
func (c *Client) Send(msg *Message) error {
	c.connLock.Lock()
	defer c.connLock.Unlock()

	if c.conn == nil {
		return fmt.Errorf("not connected")
	}

	data, err := msg.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

// SendAndWait sends a message and waits for a reply with the same ref
func (c *Client) SendAndWait(msg *Message, timeout time.Duration) (*Message, error) {
	// Create channel for reply
	replyChan := make(chan *Message, 1)

	c.requestLock.Lock()
	c.pendingRequests[msg.Ref] = replyChan
	c.requestLock.Unlock()

	// Clean up on return
	defer func() {
		c.requestLock.Lock()
		delete(c.pendingRequests, msg.Ref)
		c.requestLock.Unlock()
		close(replyChan)
	}()

	// Send message
	if err := c.Send(msg); err != nil {
		return nil, err
	}

	// Wait for reply
	select {
	case reply := <-replyChan:
		return reply, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for reply to ref %s", msg.Ref)
	case <-c.ctx.Done():
		return nil, fmt.Errorf("client disconnected")
	}
}

// On registers a message handler for a specific event
func (c *Client) On(event string, handler MessageHandler) {
	c.handlerLock.Lock()
	defer c.handlerLock.Unlock()
	c.handlers[event] = handler
}

// NextRef generates a unique reference ID
func (c *Client) NextRef() string {
	ref := atomic.AddUint64(&c.refCounter, 1)
	return fmt.Sprintf("%d", ref)
}

// readLoop continuously reads messages from the WebSocket
func (c *Client) readLoop() {
	defer c.wg.Done()
	defer func() {
		// Clean up connection when readLoop exits
		c.connLock.Lock()
		if c.conn != nil {
			c.conn.Close()
			c.conn = nil
		}
		c.connLock.Unlock()
		c.connected.Store(false)
		logger.Debug("readLoop exited, connection cleaned up")
	}()

	// Start a goroutine to monitor context cancellation and close connection
	// This ensures ReadMessage() is unblocked when context is cancelled
	// Use a WaitGroup to ensure this goroutine exits before readLoop returns
	var contextWg sync.WaitGroup
	contextDone := make(chan struct{})
	contextWg.Add(1)
	go func() {
		defer contextWg.Done()
		select {
		case <-c.readCtx.Done():
			logger.Debug("readLoop context cancelled, closing connection")
		case <-c.ctx.Done():
			logger.Debug("main context cancelled, closing connection")
		case <-contextDone:
			return
		}

		// Close connection to unblock ReadMessage()
		c.connLock.Lock()
		if c.conn != nil {
			c.conn.Close()
		}
		c.connLock.Unlock()
	}()
	defer func() {
		close(contextDone)
		contextWg.Wait() // Ensure context monitoring goroutine exits
	}()

	for {
		c.connLock.RLock()
		conn := c.conn
		c.connLock.RUnlock()

		if conn == nil {
			logger.Debug("readLoop: connection is nil")
			return
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			// Check if we're shutting down gracefully
			select {
			case <-c.readCtx.Done():
				logger.Debug("readLoop cancelled")
				return
			case <-c.ctx.Done():
				logger.Debug("readLoop main context cancelled")
				return
			default:
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					logger.Error("WebSocket read error", "error", err)
				}
				return
			}
		}

		// Parse message
		msg, err := FromJSON(data)
		if err != nil {
			logger.Error("Failed to parse message", "error", err)
			continue
		}

		// Handle the message
		c.handleMessage(msg)
	}
}

// handleMessage routes a message to the appropriate handler
func (c *Client) handleMessage(msg *Message) {
	// Check if this is a reply to a pending request
	if msg.IsReply() && msg.Ref != "" {
		c.requestLock.RLock()
		replyChan, exists := c.pendingRequests[msg.Ref]
		c.requestLock.RUnlock()

		if exists {
			select {
			case replyChan <- msg:
			default:
				logger.Warn("Reply channel full", "ref", msg.Ref)
			}
			return
		}
	}

	// Route to event handler
	c.handlerLock.RLock()
	handler, exists := c.handlers[msg.Event]
	c.handlerLock.RUnlock()

	if exists {
		go handler(msg)
	} else {
		logger.Debug("No handler for event", "event", msg.Event)
	}
}

// GetCleanURL returns the WebSocket URL without query parameters
// Used for logging to prevent token leakage (tokens are in query params per Phoenix Socket requirement)
func (c *Client) GetCleanURL() string {
	c.urlMu.RLock()
	defer c.urlMu.RUnlock()

	parsedURL, err := url.Parse(c.url)
	if err != nil {
		return c.url
	}

	// Remove query parameters for clean logging
	parsedURL.RawQuery = ""
	return parsedURL.String()
}
