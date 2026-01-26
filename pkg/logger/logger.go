// Package logger provides structured logging functionality.
package logger

import (
	"log/slog"
	"os"
	"strings"
	"sync"
)

// Logger is the global logger instance.
var Logger *slog.Logger

// initOnce ensures the default logger is initialized only once.
var initOnce sync.Once

// getLogger returns the logger, initializing it with defaults if needed.
func getLogger() *slog.Logger {
	initOnce.Do(func() {
		if Logger == nil {
			// Default to Info level with text handler
			initLogger("info", "text")
		}
	})

	return Logger
}

// initLogger sets up the logger with the given level and format.
func initLogger(level, format string) {
	var logLevel slog.Level

	switch strings.ToLower(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn", "warning":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: logLevel == slog.LevelDebug, // Add source info in debug mode
	}

	var handler slog.Handler

	switch strings.ToLower(format) {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, opts)
	default:
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	Logger = slog.New(handler)
	slog.SetDefault(Logger)
}

// Init initializes the logger with the specified level and format.
// level: "debug", "info", "warn", "error"
// format: "text" or "json"
// This should be called early in main() before any logging occurs.
func Init(level, format string) {
	initOnce.Do(func() {
		initLogger(level, format)
	})
}

// Info logs at INFO level.
func Info(msg string, args ...any) {
	getLogger().Info(msg, args...)
}

// Error logs at ERROR level.
func Error(msg string, args ...any) {
	getLogger().Error(msg, args...)
}

// Debug logs at DEBUG level.
func Debug(msg string, args ...any) {
	getLogger().Debug(msg, args...)
}

// Warn logs at WARN level.
func Warn(msg string, args ...any) {
	getLogger().Warn(msg, args...)
}

// With returns a logger with the given attributes.
func With(args ...any) *slog.Logger {
	return getLogger().With(args...)
}
