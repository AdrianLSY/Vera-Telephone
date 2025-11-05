package channels

import (
	"context"
	"fmt"
	"log"
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

	// Message handling
	handlers map[string]MessageHandler
	handlerLock sync.RWMutex

	// Request tracking for request/response pattern
	pendingRequests map[string]chan *Message
	requestLock     sync.RWMutex

	refCounter uint64

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

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

// Connect establishes a WebSocket connection
func (c *Client) Connect() error {
	c.connLock.Lock()
	defer c.connLock.Unlock()

	if c.conn != nil {
		return fmt.Errorf("already connected")
	}

	conn, _, err := websocket.DefaultDialer.Dial(c.url, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", c.url, err)
	}

	c.conn = conn
	c.connected.Store(true)

	// Start read loop
	c.wg.Add(1)
	go c.readLoop()

	log.Printf("Connected to %s", c.url)
	return nil
}

// Disconnect closes the WebSocket connection
func (c *Client) Disconnect() error {
	c.connLock.Lock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.connLock.Unlock()

	c.connected.Store(false)
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
	c.connLock.RLock()
	defer c.connLock.RUnlock()

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

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		c.connLock.RLock()
		conn := c.conn
		c.connLock.RUnlock()

		if conn == nil {
			return
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			c.connected.Store(false)
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
