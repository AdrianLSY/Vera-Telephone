package channels

import (
	"encoding/json"
	"testing"
)

func TestNewMessage(t *testing.T) {
	topic := "telephone:test-path"
	event := "proxy_req"
	ref := "123"
	payload := map[string]interface{}{
		"request_id": "abc-123",
		"method":     "GET",
		"path":       "/test",
	}

	msg := NewMessage(topic, event, ref, payload)

	if msg.Topic != topic {
		t.Errorf("expected topic %s, got %s", topic, msg.Topic)
	}
	if msg.Event != event {
		t.Errorf("expected event %s, got %s", event, msg.Event)
	}
	if msg.Ref != ref {
		t.Errorf("expected ref %s, got %s", ref, msg.Ref)
	}
	if msg.JoinRef != ref {
		t.Errorf("expected joinRef %s, got %s", ref, msg.JoinRef)
	}
	if len(msg.Payload) != len(payload) {
		t.Errorf("expected payload length %d, got %d", len(payload), len(msg.Payload))
	}
}

func TestMessageToJSON(t *testing.T) {
	msg := &Message{
		JoinRef: "1",
		Ref:     "2",
		Topic:   "telephone:path",
		Event:   "phx_join",
		Payload: map[string]interface{}{
			"key": "value",
		},
	}

	data, err := msg.ToJSON()
	if err != nil {
		t.Fatalf("failed to convert to JSON: %v", err)
	}

	// Parse back to verify format
	var arr []interface{}
	err = json.Unmarshal(data, &arr)
	if err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if len(arr) != 5 {
		t.Errorf("expected 5 elements in array, got %d", len(arr))
	}

	// Verify structure [join_ref, ref, topic, event, payload]
	if arr[0].(string) != "1" {
		t.Errorf("expected join_ref '1', got %v", arr[0])
	}
	if arr[1].(string) != "2" {
		t.Errorf("expected ref '2', got %v", arr[1])
	}
	if arr[2].(string) != "telephone:path" {
		t.Errorf("expected topic 'telephone:path', got %v", arr[2])
	}
	if arr[3].(string) != "phx_join" {
		t.Errorf("expected event 'phx_join', got %v", arr[3])
	}
}

func TestMessageToJSONWithNilRefs(t *testing.T) {
	msg := &Message{
		JoinRef: "",
		Ref:     "",
		Topic:   "telephone:path",
		Event:   "heartbeat_ack",
		Payload: map[string]interface{}{},
	}

	data, err := msg.ToJSON()
	if err != nil {
		t.Fatalf("failed to convert to JSON: %v", err)
	}

	var arr []interface{}
	err = json.Unmarshal(data, &arr)
	if err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if len(arr) != 5 {
		t.Errorf("expected 5 elements, got %d", len(arr))
	}
}

func TestFromJSON(t *testing.T) {
	tests := []struct {
		name        string
		jsonData    string
		expectError bool
		validate    func(*testing.T, *Message)
	}{
		{
			name:        "valid message",
			jsonData:    `["1", "2", "telephone:path", "phx_join", {"key": "value"}]`,
			expectError: false,
			validate: func(t *testing.T, msg *Message) {
				if msg.JoinRef != "1" {
					t.Errorf("expected JoinRef '1', got %s", msg.JoinRef)
				}
				if msg.Ref != "2" {
					t.Errorf("expected Ref '2', got %s", msg.Ref)
				}
				if msg.Topic != "telephone:path" {
					t.Errorf("expected Topic 'telephone:path', got %s", msg.Topic)
				}
				if msg.Event != "phx_join" {
					t.Errorf("expected Event 'phx_join', got %s", msg.Event)
				}
				if msg.Payload["key"] != "value" {
					t.Errorf("expected payload key 'value', got %v", msg.Payload["key"])
				}
			},
		},
		{
			name:        "null join_ref and ref",
			jsonData:    `[null, null, "telephone:path", "proxy_req", {}]`,
			expectError: false,
			validate: func(t *testing.T, msg *Message) {
				if msg.JoinRef != "" {
					t.Errorf("expected empty JoinRef, got %s", msg.JoinRef)
				}
				if msg.Ref != "" {
					t.Errorf("expected empty Ref, got %s", msg.Ref)
				}
			},
		},
		{
			name:        "reply message",
			jsonData:    `["1", "2", "telephone:path", "phx_reply", {"status": "ok", "response": {}}]`,
			expectError: false,
			validate: func(t *testing.T, msg *Message) {
				if msg.Event != "phx_reply" {
					t.Errorf("expected Event 'phx_reply', got %s", msg.Event)
				}
			},
		},
		{
			name:        "invalid JSON",
			jsonData:    `{not valid json}`,
			expectError: true,
		},
		{
			name:        "wrong array length",
			jsonData:    `["1", "2", "topic"]`,
			expectError: true,
		},
		{
			name:        "missing topic",
			jsonData:    `["1", "2", 123, "event", {}]`,
			expectError: true,
		},
		{
			name:        "missing event",
			jsonData:    `["1", "2", "topic", 456, {}]`,
			expectError: true,
		},
		{
			name:        "invalid payload type",
			jsonData:    `["1", "2", "topic", "event", "not an object"]`,
			expectError: true,
		},
		{
			name:        "empty array",
			jsonData:    `[]`,
			expectError: true,
		},
		{
			name:        "too many elements",
			jsonData:    `["1", "2", "topic", "event", {}, "extra"]`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := FromJSON([]byte(tt.jsonData))

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				if msg != nil {
					t.Errorf("expected nil message but got %+v", msg)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if msg == nil {
					t.Errorf("expected message but got nil")
				} else if tt.validate != nil {
					tt.validate(t, msg)
				}
			}
		})
	}
}

func TestMessageIsReply(t *testing.T) {
	tests := []struct {
		name     string
		event    string
		expected bool
	}{
		{
			name:     "phx_reply event",
			event:    "phx_reply",
			expected: true,
		},
		{
			name:     "phx_join event",
			event:    "phx_join",
			expected: false,
		},
		{
			name:     "proxy_req event",
			event:    "proxy_req",
			expected: false,
		},
		{
			name:     "heartbeat event",
			event:    "heartbeat",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{Event: tt.event}
			result := msg.IsReply()
			if result != tt.expected {
				t.Errorf("expected IsReply() = %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestMessageIsError(t *testing.T) {
	tests := []struct {
		name     string
		msg      *Message
		expected bool
	}{
		{
			name: "error reply",
			msg: &Message{
				Event: "phx_reply",
				Payload: map[string]interface{}{
					"status": "error",
				},
			},
			expected: true,
		},
		{
			name: "ok reply",
			msg: &Message{
				Event: "phx_reply",
				Payload: map[string]interface{}{
					"status": "ok",
				},
			},
			expected: false,
		},
		{
			name: "non-reply message",
			msg: &Message{
				Event: "proxy_req",
				Payload: map[string]interface{}{
					"status": "error",
				},
			},
			expected: false,
		},
		{
			name: "reply without status",
			msg: &Message{
				Event:   "phx_reply",
				Payload: map[string]interface{}{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.msg.IsError()
			if result != tt.expected {
				t.Errorf("expected IsError() = %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestMessageGetStatus(t *testing.T) {
	tests := []struct {
		name     string
		msg      *Message
		expected string
	}{
		{
			name: "ok status",
			msg: &Message{
				Payload: map[string]interface{}{
					"status": "ok",
				},
			},
			expected: "ok",
		},
		{
			name: "error status",
			msg: &Message{
				Payload: map[string]interface{}{
					"status": "error",
				},
			},
			expected: "error",
		},
		{
			name: "no status",
			msg: &Message{
				Payload: map[string]interface{}{},
			},
			expected: "",
		},
		{
			name: "non-string status",
			msg: &Message{
				Payload: map[string]interface{}{
					"status": 123,
				},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.msg.GetStatus()
			if result != tt.expected {
				t.Errorf("expected GetStatus() = %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestMessageGetResponse(t *testing.T) {
	tests := []struct {
		name        string
		msg         *Message
		expectNil   bool
		validateKey string
		validateVal interface{}
	}{
		{
			name: "valid response",
			msg: &Message{
				Payload: map[string]interface{}{
					"response": map[string]interface{}{
						"token": "new-token",
						"path":  "/test",
					},
				},
			},
			expectNil:   false,
			validateKey: "token",
			validateVal: "new-token",
		},
		{
			name: "no response",
			msg: &Message{
				Payload: map[string]interface{}{},
			},
			expectNil: true,
		},
		{
			name: "response not a map",
			msg: &Message{
				Payload: map[string]interface{}{
					"response": "not a map",
				},
			},
			expectNil: true,
		},
		{
			name: "empty response",
			msg: &Message{
				Payload: map[string]interface{}{
					"response": map[string]interface{}{},
				},
			},
			expectNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.msg.GetResponse()

			if tt.expectNil {
				if result != nil {
					t.Errorf("expected nil response, got %+v", result)
				}
			} else {
				if result == nil {
					t.Errorf("expected non-nil response, got nil")
				} else if tt.validateKey != "" {
					if result[tt.validateKey] != tt.validateVal {
						t.Errorf("expected response[%s] = %v, got %v",
							tt.validateKey, tt.validateVal, result[tt.validateKey])
					}
				}
			}
		})
	}
}

func TestMessageRoundTrip(t *testing.T) {
	// Create a message
	original := &Message{
		JoinRef: "join-1",
		Ref:     "ref-2",
		Topic:   "telephone:test-path",
		Event:   "proxy_req",
		Payload: map[string]interface{}{
			"request_id": "uuid-123",
			"method":     "POST",
			"path":       "/api/users",
			"headers": map[string]interface{}{
				"Content-Type": "application/json",
			},
			"body": `{"name":"test"}`,
		},
	}

	// Convert to JSON
	data, err := original.ToJSON()
	if err != nil {
		t.Fatalf("failed to convert to JSON: %v", err)
	}

	// Parse back
	parsed, err := FromJSON(data)
	if err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	// Verify fields match
	if parsed.JoinRef != original.JoinRef {
		t.Errorf("JoinRef mismatch: got %s, want %s", parsed.JoinRef, original.JoinRef)
	}
	if parsed.Ref != original.Ref {
		t.Errorf("Ref mismatch: got %s, want %s", parsed.Ref, original.Ref)
	}
	if parsed.Topic != original.Topic {
		t.Errorf("Topic mismatch: got %s, want %s", parsed.Topic, original.Topic)
	}
	if parsed.Event != original.Event {
		t.Errorf("Event mismatch: got %s, want %s", parsed.Event, original.Event)
	}
	if parsed.Payload["request_id"] != original.Payload["request_id"] {
		t.Errorf("Payload mismatch")
	}
}

func TestMessageTypes(t *testing.T) {
	tests := []struct {
		messageType MessageType
		expected    string
	}{
		{MessageTypeJoin, "phx_join"},
		{MessageTypeLeave, "phx_leave"},
		{MessageTypeReply, "phx_reply"},
		{MessageTypeHeartbeat, "heartbeat"},
		{MessageTypeHeartbeatReply, "phx_reply"},
		{MessageTypeProxyRequest, "proxy_req"},
		{MessageTypeProxyResponse, "proxy_res"},
		{MessageTypeRefreshToken, "refresh_token"},
	}

	for _, tt := range tests {
		t.Run(string(tt.messageType), func(t *testing.T) {
			if string(tt.messageType) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, string(tt.messageType))
			}
		})
	}
}
