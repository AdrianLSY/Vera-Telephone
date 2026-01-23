package proxy

import (
	"context"
	"fmt"
	"time"

	"github.com/verastack/telephone/pkg/channels"
	"github.com/verastack/telephone/pkg/logger"
	"github.com/verastack/telephone/pkg/websocket"
)

// handleWSConnect handles the ws_connect event from Plugboard
func (t *Telephone) handleWSConnect(msg *channels.Message) {
	// Check if we're shutting down
	select {
	case <-t.ctx.Done():
		logger.Debug("ws_connect: ignoring request during shutdown")
		return
	default:
	}

	// Parse payload
	connectionID, ok := msg.Payload["connection_id"].(string)
	if !ok || connectionID == "" {
		logger.Error("ws_connect: missing or invalid connection_id")
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

	logger.Info("WebSocket connect request",
		"connection_id", connectionID,
		"path", path,
	)

	// Build backend WebSocket URL
	backendURL := t.buildWSBackendURL(path, queryString)

	// Attempt connection
	protocol, err := t.wsManager.Connect(connectionID, backendURL, headers)
	if err != nil {
		logger.Error("WebSocket connection failed",
			"connection_id", connectionID,
			"error", err,
		)
		t.sendWSError(connectionID, websocket.ErrConnectionRefused)
		return
	}

	// Send success response
	t.sendWSConnected(connectionID, protocol)
}

// handleWSFrame handles the ws_frame event from Plugboard (client -> backend)
func (t *Telephone) handleWSFrame(msg *channels.Message) {
	// Check if we're shutting down
	select {
	case <-t.ctx.Done():
		logger.Debug("ws_frame: ignoring request during shutdown")
		return
	default:
	}

	connectionID, ok := msg.Payload["connection_id"].(string)
	if !ok || connectionID == "" {
		logger.Error("ws_frame: missing or invalid connection_id")
		return
	}

	opcodeStr, _ := msg.Payload["opcode"].(string)
	opcode := websocket.Opcode(opcodeStr)

	dataB64, _ := msg.Payload["data"].(string)

	// Decode base64 data
	data, err := websocket.DecodeBase64(dataB64)
	if err != nil {
		logger.Error("ws_frame: failed to decode base64 data",
			"connection_id", connectionID,
			"error", err,
		)
		t.sendWSError(connectionID, websocket.ErrInvalidFrameData)
		return
	}

	// Forward frame to backend
	if err := t.wsManager.SendFrame(connectionID, opcode, data); err != nil {
		logger.Error("ws_frame: failed to send frame",
			"connection_id", connectionID,
			"error", err,
		)
		t.sendWSError(connectionID, websocket.ErrBackendError)
		return
	}
}

// handleWSClose handles the ws_close event from Plugboard (client closed)
func (t *Telephone) handleWSClose(msg *channels.Message) {
	// Note: We don't check context here because close requests should be processed
	// even during shutdown to ensure clean connection termination

	connectionID, ok := msg.Payload["connection_id"].(string)
	if !ok || connectionID == "" {
		logger.Error("ws_close: missing or invalid connection_id")
		return
	}

	code := 1000 // Default: normal closure
	if codeFloat, ok := msg.Payload["code"].(float64); ok {
		code = int(codeFloat)
	}

	reason, _ := msg.Payload["reason"].(string)

	logger.Info("WebSocket close request",
		"connection_id", connectionID,
		"code", code,
		"reason", reason,
	)

	// Close the backend connection
	if err := t.wsManager.Close(connectionID, code, reason); err != nil {
		logger.Error("ws_close: failed to close connection",
			"connection_id", connectionID,
			"error", err,
		)
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
	t.sendWSErrorRaw(connectionID, reason)
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
		logger.Error("Failed to send ws_connected",
			"connection_id", connectionID,
			"error", err,
		)
		return
	}

	logger.Debug("Sent ws_connected",
		"connection_id", connectionID,
		"protocol", protocol,
	)
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
		logger.Error("Failed to send ws_frame",
			"connection_id", connectionID,
			"error", err,
		)
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
		logger.Error("Failed to send ws_closed",
			"connection_id", connectionID,
			"error", err,
		)
		return
	}

	logger.Debug("Sent ws_closed",
		"connection_id", connectionID,
		"code", code,
		"reason", reason,
	)
}

// sendWSError sends ws_error event to Plugboard with a typed error reason
func (t *Telephone) sendWSError(connectionID string, reason websocket.ErrorReason) {
	t.sendWSErrorRaw(connectionID, reason.String())
}

// sendWSErrorRaw sends ws_error event to Plugboard with a raw string reason
// This is used for error messages from the underlying WebSocket library
func (t *Telephone) sendWSErrorRaw(connectionID string, reason string) {
	topic := fmt.Sprintf("telephone:%s", t.claims.PathID)
	ref := t.client.NextRef()

	payload := map[string]interface{}{
		"connection_id": connectionID,
		"reason":        reason,
	}

	msg := channels.NewMessage(topic, "ws_error", ref, payload)

	if err := t.client.Send(msg); err != nil {
		logger.Error("Failed to send ws_error",
			"connection_id", connectionID,
			"error", err,
		)
		return
	}

	logger.Debug("Sent ws_error",
		"connection_id", connectionID,
		"reason", reason,
	)
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

// handleWSCheck handles the ws_check event from Plugboard.
// This is a pre-flight check to verify backend WebSocket support before
// Plugboard upgrades the client connection.
func (t *Telephone) handleWSCheck(msg *channels.Message) {
	// Parse payload
	checkID, ok := msg.Payload["check_id"].(string)
	if !ok || checkID == "" {
		logger.Error("ws_check: missing or invalid check_id")
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

	logger.Info("WebSocket check request",
		"check_id", checkID,
		"path", path,
	)

	// Build backend WebSocket URL (reuse existing method)
	backendURL := t.buildWSBackendURL(path, queryString)

	// Create context with timeout (3 seconds to leave buffer for the 5s Plugboard timeout)
	ctx, cancel := context.WithTimeout(t.ctx, 3*time.Second)
	defer cancel()

	// Attempt WebSocket handshake
	protocol, err := t.wsManager.CheckSupport(ctx, backendURL, headers)
	if err != nil {
		logger.Warn("WebSocket check failed",
			"check_id", checkID,
			"path", path,
			"error", err,
		)
		t.sendWSCheckResult(checkID, false, "", err.Error())
		return
	}

	logger.Info("WebSocket check succeeded",
		"check_id", checkID,
		"path", path,
		"protocol", protocol,
	)
	t.sendWSCheckResult(checkID, true, protocol, "")
}

// sendWSCheckResult sends ws_check_result event to Plugboard
func (t *Telephone) sendWSCheckResult(checkID string, supported bool, protocol, reason string) {
	topic := fmt.Sprintf("telephone:%s", t.claims.PathID)
	ref := t.client.NextRef()

	payload := map[string]interface{}{
		"check_id":  checkID,
		"supported": supported,
	}
	if protocol != "" {
		payload["protocol"] = protocol
	}
	if reason != "" {
		payload["reason"] = reason
	}

	msg := channels.NewMessage(topic, "ws_check_result", ref, payload)

	if err := t.client.Send(msg); err != nil {
		logger.Error("Failed to send ws_check_result",
			"check_id", checkID,
			"error", err,
		)
		return
	}

	logger.Debug("Sent ws_check_result",
		"check_id", checkID,
		"supported", supported,
		"protocol", protocol,
	)
}
