package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds the application configuration
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

// LoadFromEnv loads configuration from environment variables
// All configuration must be explicitly set - no defaults are used
func LoadFromEnv() (*Config, error) {
	cfg := &Config{}

	// Optional: Token (can be loaded from database if not provided)
	cfg.Token = os.Getenv("TELEPHONE_TOKEN")
	if cfg.Token == "" {
		// Try reading from .env file format
		cfg.Token = os.Getenv("token")
		// Note: If still empty, proxy.New() will try to load from database
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
	portStr := os.Getenv("BACKEND_PORT")
	if portStr == "" {
		return nil, fmt.Errorf("BACKEND_PORT environment variable is required")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid BACKEND_PORT: %w", err)
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

	if timeoutStr := os.Getenv("CONNECT_TIMEOUT"); timeoutStr != "" {
		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return nil, fmt.Errorf("invalid CONNECT_TIMEOUT: %w", err)
		}
		cfg.ConnectTimeout = timeout
	} else {
		return nil, fmt.Errorf("CONNECT_TIMEOUT environment variable is required")
	}

	if timeoutStr := os.Getenv("REQUEST_TIMEOUT"); timeoutStr != "" {
		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return nil, fmt.Errorf("invalid REQUEST_TIMEOUT: %w", err)
		}
		cfg.RequestTimeout = timeout
	} else {
		return nil, fmt.Errorf("REQUEST_TIMEOUT environment variable is required")
	}

	if intervalStr := os.Getenv("HEARTBEAT_INTERVAL"); intervalStr != "" {
		interval, err := time.ParseDuration(intervalStr)
		if err != nil {
			return nil, fmt.Errorf("invalid HEARTBEAT_INTERVAL: %w", err)
		}
		cfg.HeartbeatInterval = interval
	} else {
		return nil, fmt.Errorf("HEARTBEAT_INTERVAL environment variable is required")
	}

	if intervalStr := os.Getenv("CONNECTION_MONITOR_INTERVAL"); intervalStr != "" {
		interval, err := time.ParseDuration(intervalStr)
		if err != nil {
			return nil, fmt.Errorf("invalid CONNECTION_MONITOR_INTERVAL: %w", err)
		}
		cfg.ConnectionMonitorInterval = interval
	} else {
		return nil, fmt.Errorf("CONNECTION_MONITOR_INTERVAL environment variable is required")
	}

	if backoffStr := os.Getenv("INITIAL_BACKOFF"); backoffStr != "" {
		backoff, err := time.ParseDuration(backoffStr)
		if err != nil {
			return nil, fmt.Errorf("invalid INITIAL_BACKOFF: %w", err)
		}
		cfg.InitialBackoff = backoff
	} else {
		return nil, fmt.Errorf("INITIAL_BACKOFF environment variable is required")
	}

	if backoffStr := os.Getenv("MAX_BACKOFF"); backoffStr != "" {
		backoff, err := time.ParseDuration(backoffStr)
		if err != nil {
			return nil, fmt.Errorf("invalid MAX_BACKOFF: %w", err)
		}
		cfg.MaxBackoff = backoff
	} else {
		return nil, fmt.Errorf("MAX_BACKOFF environment variable is required")
	}

	// Required: Max Retries
	retriesStr := os.Getenv("MAX_RETRIES")
	if retriesStr == "" {
		return nil, fmt.Errorf("MAX_RETRIES environment variable is required")
	}
	retries, err := strconv.Atoi(retriesStr)
	if err != nil {
		return nil, fmt.Errorf("invalid MAX_RETRIES: %w", err)
	}
	cfg.MaxRetries = retries

	if secretKey := os.Getenv("SECRET_KEY_BASE"); secretKey != "" {
		cfg.SecretKeyBase = secretKey
	} else {
		return nil, fmt.Errorf("SECRET_KEY_BASE environment variable is required for token encryption")
	}

	// Required: Token DB Path
	cfg.TokenDBPath = os.Getenv("TOKEN_DB_PATH")
	if cfg.TokenDBPath == "" {
		return nil, fmt.Errorf("TOKEN_DB_PATH environment variable is required")
	}

	// Required: Max response size
	maxSizeStr := os.Getenv("MAX_RESPONSE_SIZE")
	if maxSizeStr == "" {
		return nil, fmt.Errorf("MAX_RESPONSE_SIZE environment variable is required")
	}
	maxSize, err := strconv.ParseInt(maxSizeStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid MAX_RESPONSE_SIZE: %w", err)
	}
	if maxSize <= 0 {
		return nil, fmt.Errorf("MAX_RESPONSE_SIZE must be positive")
	}
	cfg.MaxResponseSize = maxSize

	// Required: Chunk size
	chunkSizeStr := os.Getenv("CHUNK_SIZE")
	if chunkSizeStr == "" {
		return nil, fmt.Errorf("CHUNK_SIZE environment variable is required")
	}
	chunkSize, err := strconv.Atoi(chunkSizeStr)
	if err != nil {
		return nil, fmt.Errorf("invalid CHUNK_SIZE: %w", err)
	}
	if chunkSize <= 0 {
		return nil, fmt.Errorf("CHUNK_SIZE must be positive")
	}
	cfg.ChunkSize = chunkSize

	// Required: Database operation timeout
	dbTimeoutStr := os.Getenv("DB_TIMEOUT")
	if dbTimeoutStr == "" {
		return nil, fmt.Errorf("DB_TIMEOUT environment variable is required")
	}
	dbTimeout, err := time.ParseDuration(dbTimeoutStr)
	if err != nil {
		return nil, fmt.Errorf("invalid DB_TIMEOUT: %w", err)
	}
	cfg.DBTimeout = dbTimeout

	// Optional: Health check port (0 = disabled)
	if healthPortStr := os.Getenv("HEALTH_CHECK_PORT"); healthPortStr != "" {
		healthPort, err := strconv.Atoi(healthPortStr)
		if err != nil {
			return nil, fmt.Errorf("invalid HEALTH_CHECK_PORT: %w", err)
		}
		if healthPort < 0 || healthPort > 65535 {
			return nil, fmt.Errorf("HEALTH_CHECK_PORT must be between 0 and 65535")
		}
		cfg.HealthCheckPort = healthPort
	}

	return cfg, nil
}

// BackendURL returns the full backend URL
func (c *Config) BackendURL() string {
	return fmt.Sprintf("%s://%s:%d", c.BackendScheme, c.BackendHost, c.BackendPort)
}
