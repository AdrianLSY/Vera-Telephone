// Package websocket provides WebSocket connection management for proxy functionality.
package websocket

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/verastack/telephone/pkg/logger"
)

// hopByHopHeaders are WebSocket handshake headers that should not be forwarded
// to the backend. The gorilla/websocket dialer generates its own values for these.
var hopByHopHeaders = map[string]bool{
	"sec-websocket-key":        true,
	"sec-websocket-version":    true,
	"sec-websocket-extensions": true,
	"upgrade":                  true,
	"connection":               true,
}

// isHopByHopHeader checks if a header should be stripped before forwarding.
func isHopByHopHeader(name string) bool {
	return hopByHopHeaders[strings.ToLower(name)]
}

// EventHandler is called when events occur on backend WebSocket connections.
type EventHandler interface {
	// OnFrame is called when a frame is received from the backend
	OnFrame(connectionID string, opcode Opcode, data []byte)
	// OnClose is called when the backend closes the connection
	OnClose(connectionID string, code int, reason string)
	// OnError is called when an error occurs on the connection
	OnError(connectionID string, reason string)
}

// connection represents a single backend WebSocket connection.
type connection struct {
	id       string
	conn     *websocket.Conn
	ctx      context.Context
	cancel   context.CancelFunc
	writeMu  sync.Mutex // Protects concurrent writes
	closed   bool
	closedMu sync.RWMutex
}

// Manager manages backend WebSocket connections.
type Manager struct {
	connections map[string]*connection
	mu          sync.RWMutex
	handler     EventHandler
	wg          sync.WaitGroup

	// Parent context for all connections
	ctx    context.Context
	cancel context.CancelFunc
}

// NewManager creates a new WebSocket connection manager.
func NewManager(handler EventHandler) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	return &Manager{
		connections: make(map[string]*connection),
		handler:     handler,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Connect establishes a WebSocket connection to the backend.
func (m *Manager) Connect(connectionID, url string, headers map[string]string) (string, error) {
	// Check if connection already exists
	m.mu.RLock()

	if _, exists := m.connections[connectionID]; exists {
		m.mu.RUnlock()
		return "", fmt.Errorf("connection %s already exists", connectionID)
	}

	m.mu.RUnlock()

	// Build HTTP headers for the WebSocket handshake
	httpHeaders := http.Header{}

	var subprotocols []string

	for k, v := range headers {
		// Extract subprotocols from sec-websocket-protocol header
		if strings.EqualFold(k, "sec-websocket-protocol") {
			// Parse comma-separated subprotocols
			for _, proto := range strings.Split(v, ",") {
				proto = strings.TrimSpace(proto)
				if proto != "" {
					subprotocols = append(subprotocols, proto)
				}
			}
		} else if !isHopByHopHeader(k) {
			// Forward header (skip hop-by-hop headers that the dialer will set)
			httpHeaders.Set(k, v)
		}
	}

	// Configure dialer with subprotocols
	dialer := websocket.Dialer{
		Subprotocols: subprotocols,
	}

	// Connect to backend
	conn, resp, err := dialer.DialContext(m.ctx, url, httpHeaders)
	if resp != nil && resp.Body != nil {
		//nolint:errcheck // Best-effort close of HTTP response body
		defer resp.Body.Close()
	}

	if err != nil {
		if resp != nil {
			return "", fmt.Errorf("websocket dial failed (status %d): %w", resp.StatusCode, err)
		}

		return "", fmt.Errorf("websocket dial failed: %w", err)
	}

	// Get selected subprotocol
	selectedProtocol := conn.Subprotocol()

	// Create connection context
	connCtx, connCancel := context.WithCancel(m.ctx)

	c := &connection{
		id:     connectionID,
		conn:   conn,
		ctx:    connCtx,
		cancel: connCancel,
	}

	// Store connection
	m.mu.Lock()
	m.connections[connectionID] = c
	m.mu.Unlock()

	// Start receiver goroutine
	m.wg.Add(1)

	go m.receiveLoop(c)

	logger.Info("WebSocket connection established",
		"connection_id", connectionID,
		"url", url,
		"protocol", selectedProtocol,
	)

	return selectedProtocol, nil
}

// SendFrame sends a frame to the backend WebSocket.
func (m *Manager) SendFrame(connectionID string, opcode Opcode, data []byte) error {
	m.mu.RLock()
	c, exists := m.connections[connectionID]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("connection %s not found", connectionID)
	}

	c.closedMu.RLock()

	if c.closed {
		c.closedMu.RUnlock()
		return fmt.Errorf("connection %s is closed", connectionID)
	}

	c.closedMu.RUnlock()

	// Map opcode to websocket message type
	var messageType int

	switch opcode {
	case OpcodeText:
		messageType = websocket.TextMessage
	case OpcodeBinary:
		messageType = websocket.BinaryMessage
	case OpcodePing:
		// Send ping using WriteControl
		c.writeMu.Lock()
		err := c.conn.WriteControl(websocket.PingMessage, data, noDeadline)
		c.writeMu.Unlock()

		return err
	case OpcodePong:
		// Send pong using WriteControl
		c.writeMu.Lock()
		err := c.conn.WriteControl(websocket.PongMessage, data, noDeadline)
		c.writeMu.Unlock()

		return err
	default:
		return fmt.Errorf("unknown opcode: %s", opcode)
	}

	// Send message
	c.writeMu.Lock()
	err := c.conn.WriteMessage(messageType, data)
	c.writeMu.Unlock()

	return err
}

// Close closes a backend WebSocket connection.
func (m *Manager) Close(connectionID string, code int, reason string) error {
	m.mu.RLock()
	c, exists := m.connections[connectionID]
	m.mu.RUnlock()

	if !exists {
		return nil // Already closed or never existed
	}

	return m.closeConnection(c, code, reason)
}

// CloseAll closes all backend WebSocket connections.
func (m *Manager) CloseAll() {
	logger.Info("Closing all WebSocket connections...")

	// Cancel parent context to stop all receive loops
	m.cancel()

	// Get all connections
	m.mu.Lock()

	connections := make([]*connection, 0, len(m.connections))
	for _, c := range m.connections {
		connections = append(connections, c)
	}
	m.mu.Unlock()

	// Close all connections with "going away" code
	for _, c := range connections {
		//nolint:errcheck,gosec // Best-effort close during shutdown
		m.closeConnection(c, websocket.CloseGoingAway, "Telephone shutting down")
	}

	// Wait for all receiver goroutines to exit
	m.wg.Wait()

	logger.Info("All WebSocket connections closed")
}

// closeConnection closes a single connection.
func (m *Manager) closeConnection(c *connection, code int, reason string) error {
	c.closedMu.Lock()
	if c.closed {
		c.closedMu.Unlock()
		return nil
	}

	c.closed = true
	c.closedMu.Unlock()

	// Cancel connection context to stop receiver
	c.cancel()

	// Send close frame
	c.writeMu.Lock()
	err := c.conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(code, reason),
		noDeadline,
	)
	c.writeMu.Unlock()

	// Close the underlying connection (best-effort, error already captured from WriteControl)
	//nolint:errcheck,gosec // Close is best-effort; WriteControl error is returned
	c.conn.Close()

	// Remove from map
	m.mu.Lock()
	delete(m.connections, c.id)
	m.mu.Unlock()

	logger.Debug("WebSocket connection closed",
		"connection_id", c.id,
		"code", code,
		"reason", reason,
	)

	return err
}

// receiveLoop reads frames from the backend WebSocket and forwards them.
func (m *Manager) receiveLoop(c *connection) {
	defer m.wg.Done()
	defer func() {
		// Ensure connection is cleaned up
		m.mu.Lock()
		delete(m.connections, c.id)
		m.mu.Unlock()
	}()

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		messageType, data, err := c.conn.ReadMessage()
		if err != nil {
			// Check if this is a clean shutdown
			select {
			case <-c.ctx.Done():
				return
			default:
			}

			// Handle close error
			var closeErr *websocket.CloseError
			if errors.As(err, &closeErr) {
				logger.Info("Backend WebSocket closed",
					"connection_id", c.id,
					"code", closeErr.Code,
					"reason", closeErr.Text,
				)
				m.handler.OnClose(c.id, closeErr.Code, closeErr.Text)
			} else {
				logger.Error("Backend WebSocket error",
					"connection_id", c.id,
					"error", err,
				)
				m.handler.OnError(c.id, err.Error())
			}

			return
		}

		// Convert message type to opcode
		var opcode Opcode

		switch messageType {
		case websocket.TextMessage:
			opcode = OpcodeText
		case websocket.BinaryMessage:
			opcode = OpcodeBinary
		case websocket.PingMessage:
			opcode = OpcodePing
		case websocket.PongMessage:
			opcode = OpcodePong
		default:
			logger.Warn("Unknown message type from backend",
				"connection_id", c.id,
				"message_type", messageType,
			)

			continue
		}

		// Forward frame to handler
		m.handler.OnFrame(c.id, opcode, data)
	}
}

// DecodeBase64 decodes a base64-encoded string to bytes.
func DecodeBase64(data string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(data)
}

// EncodeBase64 encodes bytes to a base64 string.
func EncodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// noDeadline is a zero time used for WebSocket control messages (no deadline).
var noDeadline time.Time

// CheckSupport attempts a WebSocket connection to verify backend support.
// It returns the selected protocol on success, or an error if the backend
// doesn't support WebSocket. The connection is immediately closed after
// the handshake completes. This is used for pre-flight checks before
// upgrading the client connection.
func (m *Manager) CheckSupport(ctx context.Context, url string, headers map[string]string) (string, error) {
	// Build HTTP headers for the WebSocket handshake
	httpHeaders := http.Header{}

	var subprotocols []string

	for k, v := range headers {
		// Extract subprotocols from sec-websocket-protocol header
		if strings.EqualFold(k, "sec-websocket-protocol") {
			// Parse comma-separated subprotocols
			for _, proto := range strings.Split(v, ",") {
				proto = strings.TrimSpace(proto)
				if proto != "" {
					subprotocols = append(subprotocols, proto)
				}
			}
		} else if !isHopByHopHeader(k) {
			// Forward header (skip hop-by-hop headers that the dialer will set)
			httpHeaders.Set(k, v)
		}
	}

	// Configure dialer with subprotocols and handshake timeout
	dialer := websocket.Dialer{
		Subprotocols:     subprotocols,
		HandshakeTimeout: 10 * time.Second,
	}

	// Attempt connection with context timeout
	conn, resp, err := dialer.DialContext(ctx, url, httpHeaders)
	if resp != nil && resp.Body != nil {
		//nolint:errcheck // Best-effort close of HTTP response body
		defer resp.Body.Close()
	}

	if err != nil {
		if resp != nil {
			return "", fmt.Errorf("websocket check failed (status %d): %w", resp.StatusCode, err)
		}

		return "", fmt.Errorf("websocket check failed: %w", err)
	}

	// Get selected subprotocol
	protocol := conn.Subprotocol()

	// Close immediately - we only needed to verify support
	//nolint:errcheck,gosec // Best-effort close for check operation
	conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "check complete"),
		time.Now().Add(time.Second),
	)
	//nolint:errcheck,gosec // Best-effort close for check operation
	conn.Close()

	logger.Debug("WebSocket check successful",
		"url", url,
		"protocol", protocol,
	)

	return protocol, nil
}
