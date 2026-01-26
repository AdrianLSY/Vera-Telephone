// Package channels provides a Phoenix Channels WebSocket client implementation.
package channels

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// BenchmarkFromJSONSmall benchmarks parsing a small Phoenix message.
func BenchmarkFromJSONSmall(b *testing.B) {
	data := []byte(`["1", "2", "telephone:path", "phx_join", {"key": "value"}]`)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := FromJSON(data)
		if err != nil {
			b.Fatalf("Failed to parse: %v", err)
		}
	}
}

// BenchmarkFromJSONLarge benchmarks parsing a large Phoenix message with complex payload.
func BenchmarkFromJSONLarge(b *testing.B) {
	payload := map[string]interface{}{
		"request_id": "550e8400-e29b-41d4-a716-446655440000",
		"method":     "POST",
		"path":       "/api/users/profile/update",
		"headers": map[string]interface{}{
			"content-type":    "application/json",
			"authorization":   "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
			"user-agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
			"accept":          "application/json",
			"accept-language": "en-US,en;q=0.9",
			"x-request-id":    "req-12345",
		},
		"body": `{"name":"John Doe","email":"john@example.com","preferences":{"theme":"dark","notifications":true}}`,
	}

	arr := []interface{}{"join-ref-1", "ref-123", "telephone:test-path-id", "proxy_req", payload}
	data, _ := json.Marshal(arr)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := FromJSON(data)
		if err != nil {
			b.Fatalf("Failed to parse: %v", err)
		}
	}
}

// BenchmarkFromJSONNullRefs benchmarks parsing messages with null refs (common case).
func BenchmarkFromJSONNullRefs(b *testing.B) {
	data := []byte(`[null, null, "telephone:path", "proxy_req", {"request_id": "abc-123"}]`)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := FromJSON(data)
		if err != nil {
			b.Fatalf("Failed to parse: %v", err)
		}
	}
}

// BenchmarkToJSONSmall benchmarks serializing a small Phoenix message.
func BenchmarkToJSONSmall(b *testing.B) {
	msg := &Message{
		JoinRef: "1",
		Ref:     "2",
		Topic:   "telephone:path",
		Event:   "phx_join",
		Payload: map[string]interface{}{
			"key": "value",
		},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := msg.ToJSON()
		if err != nil {
			b.Fatalf("Failed to serialize: %v", err)
		}
	}
}

// BenchmarkToJSONLarge benchmarks serializing a large Phoenix message.
func BenchmarkToJSONLarge(b *testing.B) {
	msg := &Message{
		JoinRef: "join-ref-1",
		Ref:     "ref-123",
		Topic:   "telephone:test-path-id",
		Event:   "proxy_res",
		Payload: map[string]interface{}{
			"request_id":  "550e8400-e29b-41d4-a716-446655440000",
			"status_code": 200,
			"headers": map[string]interface{}{
				"content-type":   "application/json",
				"content-length": "1024",
				"cache-control":  "no-cache",
				"x-request-id":   "req-12345",
			},
			"body": strings.Repeat("x", 1024), // 1KB body
		},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := msg.ToJSON()
		if err != nil {
			b.Fatalf("Failed to serialize: %v", err)
		}
	}
}

// BenchmarkMessageRoundTrip benchmarks full serialize/deserialize cycle.
func BenchmarkMessageRoundTrip(b *testing.B) {
	msg := &Message{
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

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		data, err := msg.ToJSON()
		if err != nil {
			b.Fatalf("Failed to serialize: %v", err)
		}

		_, err = FromJSON(data)
		if err != nil {
			b.Fatalf("Failed to parse: %v", err)
		}
	}
}

// BenchmarkNewMessage benchmarks message creation.
func BenchmarkNewMessage(b *testing.B) {
	topic := "telephone:test-path"
	event := "proxy_req"
	ref := "123"
	payload := map[string]interface{}{
		"request_id": "abc-123",
		"method":     "GET",
		"path":       "/test",
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = NewMessage(topic, event, ref, payload)
	}
}

// BenchmarkIsReply benchmarks reply checking.
func BenchmarkIsReply(b *testing.B) {
	msg := &Message{Event: "phx_reply"}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = msg.IsReply()
	}
}

// BenchmarkIsError benchmarks error checking.
func BenchmarkIsError(b *testing.B) {
	msg := &Message{
		Event: "phx_reply",
		Payload: map[string]interface{}{
			"status": "error",
		},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = msg.IsError()
	}
}

// BenchmarkGetStatus benchmarks status extraction.
func BenchmarkGetStatus(b *testing.B) {
	msg := &Message{
		Payload: map[string]interface{}{
			"status": "ok",
		},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = msg.GetStatus()
	}
}

// BenchmarkGetResponse benchmarks response extraction.
func BenchmarkGetResponse(b *testing.B) {
	msg := &Message{
		Payload: map[string]interface{}{
			"response": map[string]interface{}{
				"token": "new-token",
				"path":  "/test",
			},
		},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = msg.GetResponse()
	}
}

// BenchmarkFromJSONParallel benchmarks concurrent message parsing.
func BenchmarkFromJSONParallel(b *testing.B) {
	data := []byte(`["1", "2", "telephone:path", "proxy_req", {"request_id": "abc-123", "method": "GET", "path": "/test"}]`)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := FromJSON(data)
			if err != nil {
				b.Fatalf("Failed to parse: %v", err)
			}
		}
	})
}

// BenchmarkToJSONParallel benchmarks concurrent message serialization.
func BenchmarkToJSONParallel(b *testing.B) {
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			msg := &Message{
				JoinRef: fmt.Sprintf("join-%d", i),
				Ref:     fmt.Sprintf("ref-%d", i),
				Topic:   "telephone:path",
				Event:   "proxy_res",
				Payload: map[string]interface{}{
					"request_id":  fmt.Sprintf("req-%d", i),
					"status_code": 200,
					"body":        "response body",
				},
			}

			_, err := msg.ToJSON()
			if err != nil {
				b.Fatalf("Failed to serialize: %v", err)
			}

			i++
		}
	})
}

// BenchmarkFromJSONVeryLarge benchmarks parsing a message with very large body.
func BenchmarkFromJSONVeryLarge(b *testing.B) {
	largeBody := strings.Repeat("x", 100*1024) // 100KB body
	payload := map[string]interface{}{
		"request_id": "550e8400-e29b-41d4-a716-446655440000",
		"method":     "POST",
		"path":       "/api/upload",
		"body":       largeBody,
	}

	arr := []interface{}{"1", "2", "telephone:path", "proxy_req", payload}
	data, _ := json.Marshal(arr)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := FromJSON(data)
		if err != nil {
			b.Fatalf("Failed to parse: %v", err)
		}
	}
}

// BenchmarkMessageTypeComparison benchmarks message type string comparison.
func BenchmarkMessageTypeComparison(b *testing.B) {
	types := []MessageType{
		MessageTypeJoin,
		MessageTypeLeave,
		MessageTypeReply,
		MessageTypeHeartbeat,
		MessageTypeProxyRequest,
		MessageTypeProxyResponse,
		MessageTypeRefreshToken,
		MessageTypeWSConnect,
		MessageTypeWSFrame,
		MessageTypeWSClose,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		mt := types[i%len(types)]
		_ = mt == MessageTypeProxyRequest
	}
}
