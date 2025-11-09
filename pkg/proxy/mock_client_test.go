package proxy

import (
	"fmt"
	"sync"
	"time"

	"github.com/verastack/telephone/pkg/channels"
)

// MockChannelsClient is a mock implementation of ChannelsClient for testing
type MockChannelsClient struct {
	mu                         sync.Mutex
	connected                  bool
	messages                   chan *channels.Message
	sentMessages               []*channels.Message
	handlers                   map[string]channels.MessageHandler
	connectAttempts            int
	connectSucceedOn           int // Connect succeeds on this attempt number
	connectDelay               time.Duration
	disconnectCalled           bool
	lastUpdatedURL             string
	refreshToken               string // Token to return on refresh
	refreshError               error  // Error to return on refresh
	failCurrentSucceedOriginal bool   // For testing original token fallback
	originalToken              string
	nextRef                    int
}

// Connect simulates connecting to WebSocket
func (m *MockChannelsClient) Connect() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.connectDelay > 0 {
		time.Sleep(m.connectDelay)
	}

	m.connectAttempts++

	// Special case: fail with current token, succeed with original
	if m.failCurrentSucceedOriginal {
		// Check if URL contains original token
		if m.lastUpdatedURL != "" && m.originalToken != "" {
			if contains(m.lastUpdatedURL, m.originalToken) {
				m.connected = true
				return nil
			}
		}
		// Fail with current token
		return fmt.Errorf("connection failed")
	}

	// Normal case: succeed on specific attempt
	if m.connectSucceedOn > 0 && m.connectAttempts >= m.connectSucceedOn {
		m.connected = true
		return nil
	}

	return fmt.Errorf("connection failed")
}

// Disconnect simulates disconnecting
func (m *MockChannelsClient) Disconnect() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false
	m.disconnectCalled = true
	return nil
}

// Close permanently closes the client
func (m *MockChannelsClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false
	return nil
}

// IsConnected returns connection status
func (m *MockChannelsClient) IsConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected
}

// Send sends a message
func (m *MockChannelsClient) Send(msg *channels.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return fmt.Errorf("not connected")
	}

	m.sentMessages = append(m.sentMessages, msg)

	// Handle special events
	if msg.Event == "phx_join" {
		// Auto-respond to joins
		go func() {
			reply := channels.NewMessage(
				msg.Topic,
				"phx_reply",
				msg.Ref,
				map[string]interface{}{
					"status": "ok",
					"response": map[string]interface{}{
						"status":     "joined",
						"path":       "/test/path",
						"expires_in": 3600,
					},
				},
			)
			m.messages <- reply
		}()
	}

	if msg.Event == "refresh_token" {
		// Auto-respond to token refresh
		go func() {
			time.Sleep(10 * time.Millisecond)

			m.mu.Lock()
			refreshToken := m.refreshToken
			refreshError := m.refreshError
			m.mu.Unlock()

			if refreshError != nil {
				reply := channels.NewMessage(
					msg.Topic,
					"phx_reply",
					msg.Ref,
					map[string]interface{}{
						"status": "error",
						"response": map[string]interface{}{
							"reason": refreshError.Error(),
						},
					},
				)
				m.messages <- reply
			} else if refreshToken != "" {
				reply := channels.NewMessage(
					msg.Topic,
					"phx_reply",
					msg.Ref,
					map[string]interface{}{
						"status": "ok",
						"response": map[string]interface{}{
							"token": refreshToken,
						},
					},
				)
				m.messages <- reply
			}
		}()
	}

	return nil
}

// SendAndWait sends a message and waits for reply
func (m *MockChannelsClient) SendAndWait(msg *channels.Message, timeout time.Duration) (*channels.Message, error) {
	if err := m.Send(msg); err != nil {
		return nil, err
	}

	// Wait for reply
	select {
	case reply := <-m.messages:
		return reply, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for reply")
	}
}

// On registers event handler
func (m *MockChannelsClient) On(event string, handler channels.MessageHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.handlers == nil {
		m.handlers = make(map[string]channels.MessageHandler)
	}
	m.handlers[event] = handler
}

// UpdateURL updates the WebSocket URL
func (m *MockChannelsClient) UpdateURL(url string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastUpdatedURL = url
}

// NextRef returns next reference number
func (m *MockChannelsClient) NextRef() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextRef++
	return fmt.Sprintf("%d", m.nextRef)
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findInString(s, substr))
}

func findInString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
