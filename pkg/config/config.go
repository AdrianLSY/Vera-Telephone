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
	ConnectTimeout       time.Duration
	RequestTimeout       time.Duration
	HeartbeatInterval    time.Duration
	TokenRefreshInterval time.Duration

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
		PlugboardURL:         "ws://localhost:4000/telephone/websocket",
		BackendHost:          "localhost",
		BackendPort:          8080,
		ConnectTimeout:       10 * time.Second,
		RequestTimeout:       30 * time.Second,
		HeartbeatInterval:    30 * time.Second,
		TokenRefreshInterval: 25 * time.Minute, // Refresh before 30min expiry
		InitialBackoff:       1 * time.Second,
		MaxBackoff:           30 * time.Second,
		MaxRetries:           -1, // Infinite retries
		TokenDBPath:          "./telephone.db",
		SecretKeyBase:        "", // Must be provided
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
	}

	if timeoutStr := os.Getenv("REQUEST_TIMEOUT"); timeoutStr != "" {
		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return nil, fmt.Errorf("invalid REQUEST_TIMEOUT: %w", err)
		}
		cfg.RequestTimeout = timeout
	}

	if intervalStr := os.Getenv("TOKEN_REFRESH_INTERVAL"); intervalStr != "" {
		interval, err := time.ParseDuration(intervalStr)
		if err != nil {
			return nil, fmt.Errorf("invalid TOKEN_REFRESH_INTERVAL: %w", err)
		}
		cfg.TokenRefreshInterval = interval
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
