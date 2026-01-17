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

// Common error reasons
const (
	ErrConnectionRefused = "connection_refused"
	ErrConnectionTimeout = "connection_timeout"
	ErrInvalidUpgrade    = "invalid_upgrade"
	ErrBackendError      = "backend_error"
)
