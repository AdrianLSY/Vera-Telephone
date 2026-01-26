// Package config provides configuration loading from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// parseDuration parses a duration from an environment variable.
// Returns an error if the variable is not set or has an invalid format.
func parseDuration(envVar string) (time.Duration, error) {
	value := os.Getenv(envVar)
	if value == "" {
		return 0, fmt.Errorf("%s environment variable is required", envVar)
	}

	duration, parseErr := time.ParseDuration(value)
	if parseErr != nil {
		return 0, fmt.Errorf("invalid %s: %w", envVar, parseErr)
	}

	return duration, nil
}

// parseInt parses an integer from an environment variable.
// Returns an error if the variable is not set or has an invalid format.
func parseInt(envVar string) (int, error) {
	value := os.Getenv(envVar)
	if value == "" {
		return 0, fmt.Errorf("%s environment variable is required", envVar)
	}

	intVal, parseErr := strconv.Atoi(value)
	if parseErr != nil {
		return 0, fmt.Errorf("invalid %s: %w", envVar, parseErr)
	}

	return intVal, nil
}

// parsePositiveInt parses a positive integer from an environment variable.
// Returns an error if the variable is not set, has an invalid format, or is not positive.
func parsePositiveInt(envVar string) (int, error) {
	value, parseErr := parseInt(envVar)
	if parseErr != nil {
		return 0, parseErr
	}

	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", envVar)
	}

	return value, nil
}

// parsePositiveInt64 parses a positive int64 from an environment variable.
// Returns an error if the variable is not set, has an invalid format, or is not positive.
func parsePositiveInt64(envVar string) (int64, error) {
	value := os.Getenv(envVar)
	if value == "" {
		return 0, fmt.Errorf("%s environment variable is required", envVar)
	}

	intVal, parseErr := strconv.ParseInt(value, 10, 64)
	if parseErr != nil {
		return 0, fmt.Errorf("invalid %s: %w", envVar, parseErr)
	}

	if intVal <= 0 {
		return 0, fmt.Errorf("%s must be positive", envVar)
	}

	return intVal, nil
}

// Config holds the application configuration.
type Config struct {
	// Plugboard connection settings
	PlugboardURL string
	Token        string

	// Backend settings
	BackendHost   string
	BackendPort   int
	BackendScheme string // http or https

	// Timeouts and intervals
	ConnectTimeout            time.Duration
	RequestTimeout            time.Duration
	HeartbeatInterval         time.Duration
	ConnectionMonitorInterval time.Duration

	// Reconnection settings
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	MaxRetries     int

	// Token persistence
	SecretKeyBase string
	TokenDBPath   string

	// Proxy limits
	MaxResponseSize int64 // Maximum response size in bytes
	ChunkSize       int   // Chunk size for large responses
	DBTimeout       time.Duration

	// Health check server
	HealthCheckPort int // Port for health check HTTP server (0 = disabled)
}

// LoadFromEnv loads configuration from environment variables.
// All configuration must be explicitly set - no defaults are used.
//
//nolint:gocyclo // Configuration loading is inherently sequential with many required fields
func LoadFromEnv() (*Config, error) {
	cfg := &Config{}

	// Optional: Token (can be loaded from database if not provided).
	// If still empty, proxy.New() will try to load from database.
	cfg.Token = os.Getenv("TELEPHONE_TOKEN")
	if cfg.Token == "" {
		// Try reading from .env file format
		cfg.Token = os.Getenv("token")
	}

	// Required: Plugboard URL
	cfg.PlugboardURL = os.Getenv("PLUGBOARD_URL")
	if cfg.PlugboardURL == "" {
		return nil, fmt.Errorf("PLUGBOARD_URL environment variable is required")
	}

	// Required: Backend Host
	cfg.BackendHost = os.Getenv("BACKEND_HOST")
	if cfg.BackendHost == "" {
		return nil, fmt.Errorf("BACKEND_HOST environment variable is required")
	}

	// Required: Backend Port
	port, err := parseInt("BACKEND_PORT")
	if err != nil {
		return nil, err
	}

	cfg.BackendPort = port

	// Required: Backend Scheme
	cfg.BackendScheme = os.Getenv("BACKEND_SCHEME")
	if cfg.BackendScheme == "" {
		return nil, fmt.Errorf("BACKEND_SCHEME environment variable is required")
	}

	if cfg.BackendScheme != "http" && cfg.BackendScheme != "https" {
		return nil, fmt.Errorf("BACKEND_SCHEME must be 'http' or 'https', got: %s", cfg.BackendScheme)
	}

	// Required: Timeouts and intervals
	cfg.ConnectTimeout, err = parseDuration("CONNECT_TIMEOUT")
	if err != nil {
		return nil, err
	}

	cfg.RequestTimeout, err = parseDuration("REQUEST_TIMEOUT")
	if err != nil {
		return nil, err
	}

	cfg.HeartbeatInterval, err = parseDuration("HEARTBEAT_INTERVAL")
	if err != nil {
		return nil, err
	}

	cfg.ConnectionMonitorInterval, err = parseDuration("CONNECTION_MONITOR_INTERVAL")
	if err != nil {
		return nil, err
	}

	cfg.InitialBackoff, err = parseDuration("INITIAL_BACKOFF")
	if err != nil {
		return nil, err
	}

	cfg.MaxBackoff, err = parseDuration("MAX_BACKOFF")
	if err != nil {
		return nil, err
	}

	// Required: Max Retries (can be negative for infinite retries)
	cfg.MaxRetries, err = parseInt("MAX_RETRIES")
	if err != nil {
		return nil, err
	}

	// Required: Secret key base
	cfg.SecretKeyBase = os.Getenv("SECRET_KEY_BASE")
	if cfg.SecretKeyBase == "" {
		return nil, fmt.Errorf("SECRET_KEY_BASE environment variable is required for token encryption")
	}

	// Required: Token DB Path
	cfg.TokenDBPath = os.Getenv("TOKEN_DB_PATH")
	if cfg.TokenDBPath == "" {
		return nil, fmt.Errorf("TOKEN_DB_PATH environment variable is required")
	}

	// Required: Max response size (must be positive)
	cfg.MaxResponseSize, err = parsePositiveInt64("MAX_RESPONSE_SIZE")
	if err != nil {
		return nil, err
	}

	// Required: Chunk size (must be positive)
	cfg.ChunkSize, err = parsePositiveInt("CHUNK_SIZE")
	if err != nil {
		return nil, err
	}

	// Required: Database operation timeout
	cfg.DBTimeout, err = parseDuration("DB_TIMEOUT")
	if err != nil {
		return nil, err
	}

	// Optional: Health check port (0 = disabled)
	if healthPortStr := os.Getenv("HEALTH_CHECK_PORT"); healthPortStr != "" {
		healthPort, parseErr := strconv.Atoi(healthPortStr)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid HEALTH_CHECK_PORT: %w", parseErr)
		}

		if healthPort < 0 || healthPort > 65535 {
			return nil, fmt.Errorf("HEALTH_CHECK_PORT must be between 0 and 65535")
		}

		cfg.HealthCheckPort = healthPort
	}

	return cfg, nil
}

// BackendURL returns the full backend URL.
func (c *Config) BackendURL() string {
	return fmt.Sprintf("%s://%s:%d", c.BackendScheme, c.BackendHost, c.BackendPort)
}
