package logger

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// captureOutput captures log output for testing
func captureOutput(t *testing.T, level, format string, logFunc func()) string {
	t.Helper()

	var buf bytes.Buffer

	// Create a handler that writes to our buffer
	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level: parseLevel(level),
	}

	switch strings.ToLower(format) {
	case "json":
		handler = slog.NewJSONHandler(&buf, opts)
	default:
		handler = slog.NewTextHandler(&buf, opts)
	}

	// Temporarily replace the logger
	oldLogger := Logger
	Logger = slog.New(handler)
	defer func() { Logger = oldLogger }()

	logFunc()

	return buf.String()
}

// parseLevel converts string level to slog.Level
func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func TestInit(t *testing.T) {
	tests := []struct {
		name   string
		level  string
		format string
	}{
		{"debug text", "debug", "text"},
		{"info text", "info", "text"},
		{"warn text", "warn", "text"},
		{"warning text", "warning", "text"},
		{"error text", "error", "text"},
		{"debug json", "debug", "json"},
		{"info json", "info", "json"},
		{"unknown level defaults to info", "unknown", "text"},
		{"unknown format defaults to text", "info", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original logger
			oldLogger := Logger

			// Initialize with test settings
			Init(tt.level, tt.format)

			// Verify logger is not nil
			if Logger == nil {
				t.Error("Logger should not be nil after Init")
			}

			// Restore original logger
			Logger = oldLogger
		})
	}
}

func TestInfo(t *testing.T) {
	output := captureOutput(t, "info", "text", func() {
		Info("test message", "key", "value")
	})

	if !strings.Contains(output, "test message") {
		t.Errorf("Expected output to contain 'test message', got: %s", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("Expected output to contain 'key=value', got: %s", output)
	}
	if !strings.Contains(output, "INFO") {
		t.Errorf("Expected output to contain 'INFO', got: %s", output)
	}
}

func TestError(t *testing.T) {
	output := captureOutput(t, "info", "text", func() {
		Error("error message", "error", "something went wrong")
	})

	if !strings.Contains(output, "error message") {
		t.Errorf("Expected output to contain 'error message', got: %s", output)
	}
	if !strings.Contains(output, "ERROR") {
		t.Errorf("Expected output to contain 'ERROR', got: %s", output)
	}
}

func TestDebug(t *testing.T) {
	// Debug should appear when level is debug
	output := captureOutput(t, "debug", "text", func() {
		Debug("debug message", "detail", "verbose")
	})

	if !strings.Contains(output, "debug message") {
		t.Errorf("Expected output to contain 'debug message', got: %s", output)
	}
	if !strings.Contains(output, "DEBUG") {
		t.Errorf("Expected output to contain 'DEBUG', got: %s", output)
	}
}

func TestDebugNotShownAtInfoLevel(t *testing.T) {
	// Debug should NOT appear when level is info
	output := captureOutput(t, "info", "text", func() {
		Debug("debug message", "detail", "verbose")
	})

	if strings.Contains(output, "debug message") {
		t.Errorf("Debug message should not appear at info level, got: %s", output)
	}
}

func TestWarn(t *testing.T) {
	output := captureOutput(t, "info", "text", func() {
		Warn("warning message", "code", "W001")
	})

	if !strings.Contains(output, "warning message") {
		t.Errorf("Expected output to contain 'warning message', got: %s", output)
	}
	if !strings.Contains(output, "WARN") {
		t.Errorf("Expected output to contain 'WARN', got: %s", output)
	}
}

func TestWith(t *testing.T) {
	// Save original logger
	oldLogger := Logger

	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	Logger = slog.New(handler)
	defer func() { Logger = oldLogger }()

	// Create a logger with attributes
	childLogger := With("component", "test", "version", "1.0")

	// Log using the child logger
	childLogger.Info("child message")

	output := buf.String()
	if !strings.Contains(output, "component=test") {
		t.Errorf("Expected output to contain 'component=test', got: %s", output)
	}
	if !strings.Contains(output, "version=1.0") {
		t.Errorf("Expected output to contain 'version=1.0', got: %s", output)
	}
}

func TestJSONFormat(t *testing.T) {
	output := captureOutput(t, "info", "json", func() {
		Info("json test", "key", "value")
	})

	// JSON output should contain these patterns
	if !strings.Contains(output, `"msg":"json test"`) {
		t.Errorf("Expected JSON output to contain msg field, got: %s", output)
	}
	if !strings.Contains(output, `"key":"value"`) {
		t.Errorf("Expected JSON output to contain key field, got: %s", output)
	}
	if !strings.Contains(output, `"level":"INFO"`) {
		t.Errorf("Expected JSON output to contain level field, got: %s", output)
	}
}

func TestLogLevelFiltering(t *testing.T) {
	tests := []struct {
		name          string
		configLevel   string
		logLevel      string
		shouldAppear  bool
		logFunc       func()
		expectedLevel string
	}{
		{
			name:          "debug at debug level",
			configLevel:   "debug",
			logLevel:      "debug",
			shouldAppear:  true,
			logFunc:       func() { Debug("test") },
			expectedLevel: "DEBUG",
		},
		{
			name:          "info at debug level",
			configLevel:   "debug",
			logLevel:      "info",
			shouldAppear:  true,
			logFunc:       func() { Info("test") },
			expectedLevel: "INFO",
		},
		{
			name:          "debug at info level",
			configLevel:   "info",
			logLevel:      "debug",
			shouldAppear:  false,
			logFunc:       func() { Debug("test") },
			expectedLevel: "DEBUG",
		},
		{
			name:          "info at info level",
			configLevel:   "info",
			logLevel:      "info",
			shouldAppear:  true,
			logFunc:       func() { Info("test") },
			expectedLevel: "INFO",
		},
		{
			name:          "warn at info level",
			configLevel:   "info",
			logLevel:      "warn",
			shouldAppear:  true,
			logFunc:       func() { Warn("test") },
			expectedLevel: "WARN",
		},
		{
			name:          "info at warn level",
			configLevel:   "warn",
			logLevel:      "info",
			shouldAppear:  false,
			logFunc:       func() { Info("test") },
			expectedLevel: "INFO",
		},
		{
			name:          "error at warn level",
			configLevel:   "warn",
			logLevel:      "error",
			shouldAppear:  true,
			logFunc:       func() { Error("test") },
			expectedLevel: "ERROR",
		},
		{
			name:          "warn at error level",
			configLevel:   "error",
			logLevel:      "warn",
			shouldAppear:  false,
			logFunc:       func() { Warn("test") },
			expectedLevel: "WARN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureOutput(t, tt.configLevel, "text", tt.logFunc)

			if tt.shouldAppear {
				if !strings.Contains(output, "test") {
					t.Errorf("Expected message to appear, got: %s", output)
				}
			} else {
				if strings.Contains(output, "test") {
					t.Errorf("Expected message to NOT appear, got: %s", output)
				}
			}
		})
	}
}

func TestMultipleAttributes(t *testing.T) {
	output := captureOutput(t, "info", "text", func() {
		Info("multi attr test",
			"string", "value",
			"int", 42,
			"bool", true,
			"float", 3.14,
		)
	})

	expectedParts := []string{
		"multi attr test",
		"string=value",
		"int=42",
		"bool=true",
		"float=3.14",
	}

	for _, part := range expectedParts {
		if !strings.Contains(output, part) {
			t.Errorf("Expected output to contain '%s', got: %s", part, output)
		}
	}
}

func TestEmptyMessage(t *testing.T) {
	output := captureOutput(t, "info", "text", func() {
		Info("")
	})

	// Should still produce output with level
	if !strings.Contains(output, "INFO") {
		t.Errorf("Expected output to contain 'INFO', got: %s", output)
	}
}

func TestNoAttributes(t *testing.T) {
	output := captureOutput(t, "info", "text", func() {
		Info("no attrs")
	})

	if !strings.Contains(output, "no attrs") {
		t.Errorf("Expected output to contain 'no attrs', got: %s", output)
	}
}

// BenchmarkInfo benchmarks Info logging
func BenchmarkInfo(b *testing.B) {
	// Use a no-op handler for benchmarking
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	Logger = slog.New(handler)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Info("benchmark message", "key", "value")
	}
}

// BenchmarkDebugDisabled benchmarks Debug when disabled (should be fast)
func BenchmarkDebugDisabled(b *testing.B) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	Logger = slog.New(handler)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Debug("benchmark message", "key", "value")
	}
}

// BenchmarkWith benchmarks creating child loggers
func BenchmarkWith(b *testing.B) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	Logger = slog.New(handler)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = With("component", "test", "version", "1.0")
	}
}

// BenchmarkJSONFormat benchmarks JSON formatted logging
func BenchmarkJSONFormat(b *testing.B) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	Logger = slog.New(handler)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Info("benchmark message", "key", "value", "count", 42)
	}
}
