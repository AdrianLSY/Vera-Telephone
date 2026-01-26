// Package websocket provides WebSocket connection management for the Telephone reverse proxy.
package websocket

import (
	"encoding/base64"
	"strings"
	"testing"
)

// BenchmarkEncodeBase64Small benchmarks encoding small data.
func BenchmarkEncodeBase64Small(b *testing.B) {
	data := []byte("hello world")

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = EncodeBase64(data)
	}
}

// BenchmarkEncodeBase64Medium benchmarks encoding medium data (1KB).
func BenchmarkEncodeBase64Medium(b *testing.B) {
	data := []byte(strings.Repeat("x", 1024))

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = EncodeBase64(data)
	}
}

// BenchmarkEncodeBase64Large benchmarks encoding large data (100KB).
func BenchmarkEncodeBase64Large(b *testing.B) {
	data := []byte(strings.Repeat("x", 100*1024))

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = EncodeBase64(data)
	}
}

// BenchmarkDecodeBase64Small benchmarks decoding small data.
func BenchmarkDecodeBase64Small(b *testing.B) {
	encoded := base64.StdEncoding.EncodeToString([]byte("hello world"))

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := DecodeBase64(encoded)
		if err != nil {
			b.Fatalf("Failed to decode: %v", err)
		}
	}
}

// BenchmarkDecodeBase64Medium benchmarks decoding medium data (1KB).
func BenchmarkDecodeBase64Medium(b *testing.B) {
	data := []byte(strings.Repeat("x", 1024))
	encoded := base64.StdEncoding.EncodeToString(data)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := DecodeBase64(encoded)
		if err != nil {
			b.Fatalf("Failed to decode: %v", err)
		}
	}
}

// BenchmarkDecodeBase64Large benchmarks decoding large data (100KB).
func BenchmarkDecodeBase64Large(b *testing.B) {
	data := []byte(strings.Repeat("x", 100*1024))
	encoded := base64.StdEncoding.EncodeToString(data)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := DecodeBase64(encoded)
		if err != nil {
			b.Fatalf("Failed to decode: %v", err)
		}
	}
}

// BenchmarkBase64RoundTrip benchmarks full encode/decode cycle.
func BenchmarkBase64RoundTrip(b *testing.B) {
	data := []byte("test data for round trip encoding and decoding")

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		encoded := EncodeBase64(data)

		_, err := DecodeBase64(encoded)
		if err != nil {
			b.Fatalf("Failed to decode: %v", err)
		}
	}
}

// BenchmarkBase64RoundTripParallel benchmarks concurrent encode/decode.
func BenchmarkBase64RoundTripParallel(b *testing.B) {
	data := []byte("test data for parallel round trip encoding")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			encoded := EncodeBase64(data)

			_, err := DecodeBase64(encoded)
			if err != nil {
				b.Fatalf("Failed to decode: %v", err)
			}
		}
	})
}

// BenchmarkConnectPayloadCreation benchmarks creating connect payloads.
func BenchmarkConnectPayloadCreation(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = ConnectPayload{
			ConnectionID: "conn-12345",
			Path:         "/ws/graphql",
			QueryString:  "token=abc123&version=1",
			Headers: map[string]string{
				"origin":                 "https://example.com",
				"sec-websocket-protocol": "graphql-ws",
				"user-agent":             "Mozilla/5.0",
			},
		}
	}
}

// BenchmarkFramePayloadCreation benchmarks creating frame payloads.
func BenchmarkFramePayloadCreation(b *testing.B) {
	data := EncodeBase64([]byte(`{"type":"connection_init"}`))

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = FramePayload{
			ConnectionID: "conn-12345",
			Opcode:       OpcodeText,
			Data:         data,
		}
	}
}

// BenchmarkClosePayloadCreation benchmarks creating close payloads.
func BenchmarkClosePayloadCreation(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = ClosePayload{
			ConnectionID: "conn-12345",
			Code:         1000,
			Reason:       "normal closure",
		}
	}
}

// BenchmarkOpcodeComparison benchmarks opcode string comparison.
func BenchmarkOpcodeComparison(b *testing.B) {
	opcodes := []Opcode{OpcodeText, OpcodeBinary, OpcodePing, OpcodePong}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		op := opcodes[i%len(opcodes)]
		_ = op == OpcodeText
	}
}

// BenchmarkErrorReasonComparison benchmarks error reason comparison.
func BenchmarkErrorReasonComparison(b *testing.B) {
	reasons := []ErrorReason{
		ErrConnectionRefused,
		ErrConnectionTimeout,
		ErrInvalidUpgrade,
		ErrBackendError,
		ErrInvalidFrameData,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		reason := reasons[i%len(reasons)]
		_ = reason == ErrConnectionRefused
	}
}

// BenchmarkErrorReasonString benchmarks converting error reason to string.
func BenchmarkErrorReasonString(b *testing.B) {
	reasons := []ErrorReason{
		ErrConnectionRefused,
		ErrConnectionTimeout,
		ErrInvalidUpgrade,
		ErrBackendError,
		ErrInvalidFrameData,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		reason := reasons[i%len(reasons)]
		_ = reason.String()
	}
}

// BenchmarkEncodeBase64Binary benchmarks encoding binary data with various byte values.
func BenchmarkEncodeBase64Binary(b *testing.B) {
	// Create binary data with all byte values
	data := make([]byte, 256)
	for i := 0; i < 256; i++ {
		data[i] = byte(i)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = EncodeBase64(data)
	}
}

// BenchmarkDecodeBase64Binary benchmarks decoding binary data.
func BenchmarkDecodeBase64Binary(b *testing.B) {
	data := make([]byte, 256)
	for i := 0; i < 256; i++ {
		data[i] = byte(i)
	}

	encoded := base64.StdEncoding.EncodeToString(data)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := DecodeBase64(encoded)
		if err != nil {
			b.Fatalf("Failed to decode: %v", err)
		}
	}
}

// BenchmarkPayloadWithLargeHeaders benchmarks creating payloads with many headers.
func BenchmarkPayloadWithLargeHeaders(b *testing.B) {
	headers := make(map[string]string)
	for i := 0; i < 20; i++ {
		headers[strings.Repeat("header", i+1)] = strings.Repeat("value", i+1)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = ConnectPayload{
			ConnectionID: "conn-12345",
			Path:         "/ws/api",
			QueryString:  "",
			Headers:      headers,
		}
	}
}
