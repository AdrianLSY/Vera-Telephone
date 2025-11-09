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
	BackendHost string
	BackendPort int

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
}

// LoadFromEnv loads configuration from environment variables
func LoadFromEnv() (*Config, error) {
	cfg := &Config{
		// Defaults
		PlugboardURL: "ws://localhost:4000/telephone/websocket",
		BackendHost:  "localhost",
		BackendPort:  8080,
		TokenDBPath:  "./telephone.db",
	}

	// Optional: Token (can be loaded from database if not provided)
	cfg.Token = os.Getenv("TELEPHONE_TOKEN")
	if cfg.Token == "" {
		// Try reading from .env file format
		cfg.Token = os.Getenv("token")
		// Note: If still empty, proxy.New() will try to load from database
	}

	// Optional overrides
	if url := os.Getenv("PLUGBOARD_URL"); url != "" {
		cfg.PlugboardURL = url
	}

	if host := os.Getenv("BACKEND_HOST"); host != "" {
		cfg.BackendHost = host
	}

	if portStr := os.Getenv("BACKEND_PORT"); portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("invalid BACKEND_PORT: %w", err)
		}
		cfg.BackendPort = port
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

	if retriesStr := os.Getenv("MAX_RETRIES"); retriesStr != "" {
		retries, err := strconv.Atoi(retriesStr)
		if err != nil {
			return nil, fmt.Errorf("invalid MAX_RETRIES: %w", err)
		}
		cfg.MaxRetries = retries
	} else {
		return nil, fmt.Errorf("MAX_RETRIES environment variable is required")
	}

	if secretKey := os.Getenv("SECRET_KEY_BASE"); secretKey != "" {
		cfg.SecretKeyBase = secretKey
	} else {
		return nil, fmt.Errorf("SECRET_KEY_BASE environment variable is required for token encryption")
	}

	if dbPath := os.Getenv("TOKEN_DB_PATH"); dbPath != "" {
		cfg.TokenDBPath = dbPath
	}

	return cfg, nil
}

// BackendURL returns the full backend URL
func (c *Config) BackendURL() string {
	return fmt.Sprintf("http://%s:%d", c.BackendHost, c.BackendPort)
}
