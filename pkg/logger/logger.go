// Package logger provides structured logging using log/slog.
// It wraps slog with convenience functions and configuration options.
package logger

import (
	"log/slog"
	"os"
	"strings"
)

// Logger is the global logger instance
var Logger *slog.Logger

// init initializes the default logger
func init() {
	// Default to Info level with text handler
	Init("info", "text")
}

// Init initializes the logger with the specified level and format.
// level: "debug", "info", "warn", "error"
// format: "text" or "json"
func Init(level, format string) {
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

// Info logs at INFO level
func Info(msg string, args ...any) {
	Logger.Info(msg, args...)
}

// Error logs at ERROR level
func Error(msg string, args ...any) {
	Logger.Error(msg, args...)
}

// Debug logs at DEBUG level
func Debug(msg string, args ...any) {
	Logger.Debug(msg, args...)
}

// Warn logs at WARN level
func Warn(msg string, args ...any) {
	Logger.Warn(msg, args...)
}

// With returns a logger with the given attributes
func With(args ...any) *slog.Logger {
	return Logger.With(args...)
}
