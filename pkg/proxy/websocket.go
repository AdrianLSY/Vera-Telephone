package proxy

import (
	"fmt"
	"log"

	"github.com/verastack/telephone/pkg/channels"
	"github.com/verastack/telephone/pkg/websocket"
)

// handleWSConnect handles the ws_connect event from Plugboard
func (t *Telephone) handleWSConnect(msg *channels.Message) {
	// Parse payload
	connectionID, ok := msg.Payload["connection_id"].(string)
	if !ok || connectionID == "" {
		log.Printf("ws_connect: missing or invalid connection_id")
		return
	}

	path, _ := msg.Payload["path"].(string)
	queryString, _ := msg.Payload["query_string"].(string)

	// Extract headers
	headers := make(map[string]string)
	if rawHeaders, ok := msg.Payload["headers"].(map[string]interface{}); ok {
		for k, v := range rawHeaders {
			if str, ok := v.(string); ok {
				headers[k] = str
			}
		}
	}

	log.Printf("WebSocket connect request [%s]: path=%s", connectionID, path)

	// Build backend WebSocket URL
	backendURL := t.buildWSBackendURL(path, queryString)

	// Attempt connection
	protocol, err := t.wsManager.Connect(connectionID, backendURL, headers)
	if err != nil {
		log.Printf("WebSocket connection failed [%s]: %v", connectionID, err)
		t.sendWSError(connectionID, websocket.ErrConnectionRefused)
		return
	}

	// Send success response
	t.sendWSConnected(connectionID, protocol)
}

// handleWSFrame handles the ws_frame event from Plugboard (client -> backend)
func (t *Telephone) handleWSFrame(msg *channels.Message) {
	connectionID, ok := msg.Payload["connection_id"].(string)
	if !ok || connectionID == "" {
		log.Printf("ws_frame: missing or invalid connection_id")
		return
	}

	opcodeStr, _ := msg.Payload["opcode"].(string)
	opcode := websocket.Opcode(opcodeStr)

	dataB64, _ := msg.Payload["data"].(string)

	// Decode base64 data
	data, err := websocket.DecodeBase64(dataB64)
	if err != nil {
		log.Printf("ws_frame: failed to decode base64 data [%s]: %v", connectionID, err)
		t.sendWSError(connectionID, "invalid_frame_data")
		return
	}

	// Forward frame to backend
	if err := t.wsManager.SendFrame(connectionID, opcode, data); err != nil {
		log.Printf("ws_frame: failed to send frame [%s]: %v", connectionID, err)
		t.sendWSError(connectionID, websocket.ErrBackendError)
		return
	}
}

// handleWSClose handles the ws_close event from Plugboard (client closed)
func (t *Telephone) handleWSClose(msg *channels.Message) {
	connectionID, ok := msg.Payload["connection_id"].(string)
	if !ok || connectionID == "" {
		log.Printf("ws_close: missing or invalid connection_id")
		return
	}

	code := 1000 // Default: normal closure
	if codeFloat, ok := msg.Payload["code"].(float64); ok {
		code = int(codeFloat)
	}

	reason, _ := msg.Payload["reason"].(string)

	log.Printf("WebSocket close request [%s]: code=%d reason=%s", connectionID, code, reason)

	// Close the backend connection
	if err := t.wsManager.Close(connectionID, code, reason); err != nil {
		log.Printf("ws_close: failed to close connection [%s]: %v", connectionID, err)
	}
}

// OnFrame implements websocket.EventHandler - called when backend sends a frame
func (t *Telephone) OnFrame(connectionID string, opcode websocket.Opcode, data []byte) {
	t.sendWSFrame(connectionID, opcode, data)
}

// OnClose implements websocket.EventHandler - called when backend closes connection
func (t *Telephone) OnClose(connectionID string, code int, reason string) {
	t.sendWSClosed(connectionID, code, reason)
}

// OnError implements websocket.EventHandler - called when backend connection errors
func (t *Telephone) OnError(connectionID string, reason string) {
	t.sendWSError(connectionID, reason)
}

// sendWSConnected sends ws_connected event to Plugboard
func (t *Telephone) sendWSConnected(connectionID, protocol string) {
	topic := fmt.Sprintf("telephone:%s", t.claims.PathID)
	ref := t.client.NextRef()

	payload := map[string]interface{}{
		"connection_id": connectionID,
	}
	if protocol != "" {
		payload["protocol"] = protocol
	}

	msg := channels.NewMessage(topic, "ws_connected", ref, payload)

	if err := t.client.Send(msg); err != nil {
		log.Printf("Failed to send ws_connected [%s]: %v", connectionID, err)
		return
	}

	log.Printf("Sent ws_connected [%s] protocol=%s", connectionID, protocol)
}

// sendWSFrame sends ws_frame event to Plugboard (backend -> client)
func (t *Telephone) sendWSFrame(connectionID string, opcode websocket.Opcode, data []byte) {
	topic := fmt.Sprintf("telephone:%s", t.claims.PathID)
	ref := t.client.NextRef()

	payload := map[string]interface{}{
		"connection_id": connectionID,
		"opcode":        string(opcode),
		"data":          websocket.EncodeBase64(data),
	}

	msg := channels.NewMessage(topic, "ws_frame", ref, payload)

	if err := t.client.Send(msg); err != nil {
		log.Printf("Failed to send ws_frame [%s]: %v", connectionID, err)
		return
	}
}

// sendWSClosed sends ws_closed event to Plugboard
func (t *Telephone) sendWSClosed(connectionID string, code int, reason string) {
	topic := fmt.Sprintf("telephone:%s", t.claims.PathID)
	ref := t.client.NextRef()

	payload := map[string]interface{}{
		"connection_id": connectionID,
		"code":          code,
		"reason":        reason,
	}

	msg := channels.NewMessage(topic, "ws_closed", ref, payload)

	if err := t.client.Send(msg); err != nil {
		log.Printf("Failed to send ws_closed [%s]: %v", connectionID, err)
		return
	}

	log.Printf("Sent ws_closed [%s] code=%d reason=%s", connectionID, code, reason)
}

// sendWSError sends ws_error event to Plugboard
func (t *Telephone) sendWSError(connectionID, reason string) {
	topic := fmt.Sprintf("telephone:%s", t.claims.PathID)
	ref := t.client.NextRef()

	payload := map[string]interface{}{
		"connection_id": connectionID,
		"reason":        reason,
	}

	msg := channels.NewMessage(topic, "ws_error", ref, payload)

	if err := t.client.Send(msg); err != nil {
		log.Printf("Failed to send ws_error [%s]: %v", connectionID, err)
		return
	}

	log.Printf("Sent ws_error [%s] reason=%s", connectionID, reason)
}

// buildWSBackendURL constructs the WebSocket URL for the backend
func (t *Telephone) buildWSBackendURL(path, queryString string) string {
	// Determine WebSocket scheme based on backend scheme
	wsScheme := "ws"
	if t.config.BackendScheme == "https" {
		wsScheme = "wss"
	}

	url := fmt.Sprintf("%s://%s:%d%s", wsScheme, t.config.BackendHost, t.config.BackendPort, path)
	if queryString != "" {
		url += "?" + queryString
	}

	return url
}
