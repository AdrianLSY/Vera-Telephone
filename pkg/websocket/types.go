package websocket

// Opcode represents WebSocket frame types
type Opcode string

const (
	OpcodeText   Opcode = "text"
	OpcodeBinary Opcode = "binary"
	OpcodePing   Opcode = "ping"
	OpcodePong   Opcode = "pong"
)

// ConnectPayload represents the payload for ws_connect event from Plugboard
type ConnectPayload struct {
	ConnectionID string            `json:"connection_id"`
	Path         string            `json:"path"`
	QueryString  string            `json:"query_string"`
	Headers      map[string]string `json:"headers"`
}

// FramePayload represents the payload for ws_frame event (bidirectional)
type FramePayload struct {
	ConnectionID string `json:"connection_id"`
	Opcode       Opcode `json:"opcode"`
	Data         string `json:"data"` // Base64-encoded
}

// ClosePayload represents the payload for ws_close event from Plugboard
type ClosePayload struct {
	ConnectionID string `json:"connection_id"`
	Code         int    `json:"code"`
	Reason       string `json:"reason"`
}

// ConnectedPayload represents the payload for ws_connected event to Plugboard
type ConnectedPayload struct {
	ConnectionID string `json:"connection_id"`
	Protocol     string `json:"protocol,omitempty"` // Selected subprotocol (if any)
}

// ClosedPayload represents the payload for ws_closed event to Plugboard
type ClosedPayload struct {
	ConnectionID string `json:"connection_id"`
	Code         int    `json:"code"`
	Reason       string `json:"reason"`
}

// ErrorPayload represents the payload for ws_error event to Plugboard
type ErrorPayload struct {
	ConnectionID string `json:"connection_id"`
	Reason       string `json:"reason"`
}

// ErrorReason represents a WebSocket error reason sent to Plugboard.
// These are protocol-level error codes, not Go error types.
// They are sent as string values in the ws_error event payload.
type ErrorReason string

// Common error reasons for WebSocket connections.
// These are sent to Plugboard as part of the ws_error event payload
// to indicate why a WebSocket operation failed.
const (
	// ErrConnectionRefused indicates the backend refused the WebSocket connection
	ErrConnectionRefused ErrorReason = "connection_refused"
	// ErrConnectionTimeout indicates the connection to the backend timed out
	ErrConnectionTimeout ErrorReason = "connection_timeout"
	// ErrInvalidUpgrade indicates the WebSocket upgrade handshake failed
	ErrInvalidUpgrade ErrorReason = "invalid_upgrade"
	// ErrBackendError indicates a general error communicating with the backend
	ErrBackendError ErrorReason = "backend_error"
	// ErrInvalidFrameData indicates the frame data could not be decoded
	ErrInvalidFrameData ErrorReason = "invalid_frame_data"
)

// String returns the string representation of the error reason
func (e ErrorReason) String() string {
	return string(e)
}

// CheckPayload represents the payload for ws_check event from Plugboard
type CheckPayload struct {
	CheckID     string            `json:"check_id"`
	Path        string            `json:"path"`
	QueryString string            `json:"query_string"`
	Headers     map[string]string `json:"headers"`
}

// CheckResultPayload represents the payload for ws_check_result event to Plugboard
type CheckResultPayload struct {
	CheckID   string `json:"check_id"`
	Supported bool   `json:"supported"`
	Protocol  string `json:"protocol,omitempty"` // Selected subprotocol (if supported)
	Reason    string `json:"reason,omitempty"`   // Error reason (if not supported)
}
