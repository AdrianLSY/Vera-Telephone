package channels

import (
	"encoding/json"
	"fmt"
)

// MessageType represents Phoenix Channel message types.
type MessageType string

// Phoenix Channel message type constants.
const (
	MessageTypeJoin           MessageType = "phx_join"
	MessageTypeLeave          MessageType = "phx_leave"
	MessageTypeReply          MessageType = "phx_reply"
	MessageTypeHeartbeat      MessageType = "heartbeat"
	MessageTypeHeartbeatReply MessageType = "phx_reply"
	MessageTypeProxyRequest   MessageType = "proxy_req"
	MessageTypeProxyResponse  MessageType = "proxy_res"
	MessageTypeRefreshToken   MessageType = "refresh_token"

	// MessageTypeWSConnect and related types are for WebSocket proxy operations.
	MessageTypeWSConnect     MessageType = "ws_connect"      // Client wants to connect to backend WebSocket
	MessageTypeWSFrame       MessageType = "ws_frame"        // WebSocket frame (bidirectional)
	MessageTypeWSClose       MessageType = "ws_close"        // Client closed WebSocket connection
	MessageTypeWSConnected   MessageType = "ws_connected"    // Backend WebSocket connection established
	MessageTypeWSClosed      MessageType = "ws_closed"       // Backend WebSocket connection closed
	MessageTypeWSError       MessageType = "ws_error"        // WebSocket error occurred
	MessageTypeWSCheck       MessageType = "ws_check"        // Pre-flight check for WebSocket support
	MessageTypeWSCheckResult MessageType = "ws_check_result" // Result of WebSocket support check
)

// Message represents a Phoenix Channel message.
// Format: [join_ref, ref, topic, event, payload].
type Message struct {
	JoinRef string                 `json:"join_ref"`
	Ref     string                 `json:"ref"`
	Topic   string                 `json:"topic"`
	Event   string                 `json:"event"`
	Payload map[string]interface{} `json:"payload"`
}

// NewMessage creates a new Phoenix Channel message.
func NewMessage(topic, event, ref string, payload map[string]interface{}) *Message {
	return &Message{
		JoinRef: ref,
		Ref:     ref,
		Topic:   topic,
		Event:   event,
		Payload: payload,
	}
}

// ToJSON serializes the message to Phoenix Channel JSON array format.
// Phoenix expects: [join_ref, ref, topic, event, payload].
func (m *Message) ToJSON() ([]byte, error) {
	arr := []interface{}{
		m.JoinRef,
		m.Ref,
		m.Topic,
		m.Event,
		m.Payload,
	}

	return json.Marshal(arr)
}

// FromJSON parses a Phoenix Channel message from JSON array.
func FromJSON(data []byte) (*Message, error) {
	var arr []interface{}
	if err := json.Unmarshal(data, &arr); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	if len(arr) != 5 {
		return nil, fmt.Errorf("invalid message format: expected 5 elements, got %d", len(arr))
	}

	msg := &Message{}

	// JoinRef (can be nil/null)
	if arr[0] != nil {
		if s, ok := arr[0].(string); ok {
			msg.JoinRef = s
		}
	}

	// Ref (can be nil/null)
	if arr[1] != nil {
		if s, ok := arr[1].(string); ok {
			msg.Ref = s
		}
	}

	// Topic
	topic, ok := arr[2].(string)
	if !ok {
		return nil, fmt.Errorf("topic must be a string")
	}

	msg.Topic = topic

	// Event
	event, ok := arr[3].(string)
	if !ok {
		return nil, fmt.Errorf("event must be a string")
	}

	msg.Event = event

	// Payload
	payload, ok := arr[4].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("payload must be an object")
	}

	msg.Payload = payload

	return msg, nil
}

// IsReply checks if this is a reply message.
func (m *Message) IsReply() bool {
	return m.Event == "phx_reply"
}

// IsError checks if this is an error reply.
func (m *Message) IsError() bool {
	if !m.IsReply() {
		return false
	}

	if status, ok := m.Payload["status"].(string); ok {
		return status == "error"
	}

	return false
}

// GetStatus returns the status from a reply message.
func (m *Message) GetStatus() string {
	if status, ok := m.Payload["status"].(string); ok {
		return status
	}

	return ""
}

// GetResponse returns the response payload from a reply message.
func (m *Message) GetResponse() map[string]interface{} {
	if response, ok := m.Payload["response"].(map[string]interface{}); ok {
		return response
	}

	return nil
}
