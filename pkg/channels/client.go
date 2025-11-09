package channels

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Client represents a Phoenix Channels WebSocket client
type Client struct {
	url      string
	conn     *websocket.Conn
	connLock sync.RWMutex
	urlMu    sync.RWMutex // Protects URL updates

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

// NewClient creates a new Phoenix Channels client
func NewClient(url string) *Client {
	ctx, cancel := context.WithCancel(context.Background())

	return &Client{
		url:             url,
		handlers:        make(map[string]MessageHandler),
		pendingRequests: make(map[string]chan *Message),
		ctx:             ctx,
		cancel:          cancel,
	}
}

// UpdateURL updates the WebSocket URL (useful for reconnection with new tokens)
func (c *Client) UpdateURL(newURL string) {
	c.urlMu.Lock()
	defer c.urlMu.Unlock()
	c.url = newURL
}

// GetURL returns the current WebSocket URL
func (c *Client) GetURL() string {
	c.urlMu.RLock()
	defer c.urlMu.RUnlock()
	return c.url
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

	// Get current URL (may have been updated for reconnection)
	currentURL := c.GetURL()

	// Extract token from URL if present and add as header instead
	// This is more secure than query parameters which get logged
	headers := c.extractAndBuildHeaders(currentURL)

	conn, _, err := websocket.DefaultDialer.Dial(currentURL, headers)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", c.url, err)
	}

	c.conn = conn
	c.connected.Store(true)

	// Create new context for this connection's readLoop
	c.readCtx, c.readCancel = context.WithCancel(c.ctx)

	// Start read loop
	c.wg.Add(1)
	go c.readLoop()

	// Use clean URL for logging to avoid token leakage
	log.Printf("Connected to %s", c.GetCleanURL())
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
		log.Printf("readLoop exited, connection cleaned up")
	}()

	for {
		select {
		case <-c.readCtx.Done():
			log.Printf("readLoop cancelled")
			return
		case <-c.ctx.Done():
			log.Printf("readLoop main context cancelled")
			return
		default:
		}

		c.connLock.RLock()
		conn := c.conn
		c.connLock.RUnlock()

		if conn == nil {
			log.Printf("readLoop: connection is nil")
			return
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			return
		}

		// Parse message
		msg, err := FromJSON(data)
		if err != nil {
			log.Printf("Failed to parse message: %v", err)
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
				log.Printf("Reply channel full for ref %s", msg.Ref)
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
		log.Printf("No handler for event: %s", msg.Event)
	}
}

// extractAndBuildHeaders extracts token from URL query params and builds HTTP headers
// This prevents token leakage in logs by using headers instead of query parameters
func (c *Client) extractAndBuildHeaders(wsURL string) http.Header {
	headers := http.Header{}

	parsedURL, err := url.Parse(wsURL)
	if err != nil {
		// If URL parsing fails, return empty headers
		return headers
	}

	// Extract token from query parameters
	queryParams := parsedURL.Query()
	if token := queryParams.Get("token"); token != "" {
		// Add token as Authorization header instead of query param
		headers.Set("Authorization", "Bearer "+token)
	}

	// Add vsn if present
	if vsn := queryParams.Get("vsn"); vsn != "" {
		headers.Set("X-Phoenix-VSN", vsn)
	}

	return headers
}

// GetCleanURL returns the WebSocket URL without query parameters containing sensitive data
// Used for logging purposes to avoid token leakage
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
