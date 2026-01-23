package proxy

import (
	"time"

	"github.com/verastack/telephone/pkg/channels"
)

// ChannelsClient defines the interface for WebSocket client operations
// This interface allows for dependency injection and easier testing
type ChannelsClient interface {
	// Connect establishes the WebSocket connection
	Connect() error

	// Disconnect closes the WebSocket connection gracefully
	Disconnect() error

	// Close permanently closes the client
	Close() error

	// IsConnected returns the current connection status
	IsConnected() bool

	// Send sends a message without waiting for a reply
	Send(msg *channels.Message) error

	// SendAndWait sends a message and waits for a reply
	SendAndWait(msg *channels.Message, timeout time.Duration) (*channels.Message, error)

	// On registers an event handler for a specific event type
	On(event string, handler channels.MessageHandler)

	// NextRef generates the next reference ID for messages
	NextRef() string

	// UpdateURL updates the WebSocket URL (useful for reconnection)
	UpdateURL(url string)

	// UpdateToken updates the authentication token (used after token refresh)
	// Token is sent as query parameter per Phoenix Socket requirement
	UpdateToken(token string)
}
