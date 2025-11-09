package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoadFromEnv(t *testing.T) {
	// Save original env and restore after test
	originalEnv := os.Environ()
	defer func() {
		os.Clearenv()
		for _, env := range originalEnv {
			// Split only on first =
			if idx := strings.IndexByte(env, '='); idx > 0 {
				os.Setenv(env[:idx], env[idx+1:])
			}
		}
	}()

	tests := []struct {
		name        string
		setupEnv    func()
		expectError bool
		validate    func(*testing.T, *Config)
	}{
		{
			name: "all required env vars set",
			setupEnv: func() {
				os.Clearenv()
				os.Setenv("SECRET_KEY_BASE", "test-secret-key-base")
				os.Setenv("PLUGBOARD_URL", "ws://localhost:4000/telephone/websocket")
				os.Setenv("BACKEND_HOST", "localhost")
				os.Setenv("BACKEND_PORT", "8080")
				os.Setenv("BACKEND_SCHEME", "http")
				os.Setenv("CONNECT_TIMEOUT", "10s")
				os.Setenv("REQUEST_TIMEOUT", "30s")
				os.Setenv("HEARTBEAT_INTERVAL", "30s")
				os.Setenv("CONNECTION_MONITOR_INTERVAL", "5s")
				os.Setenv("INITIAL_BACKOFF", "1s")
				os.Setenv("MAX_BACKOFF", "30s")
				os.Setenv("MAX_RETRIES", "100")
				os.Setenv("TOKEN_DB_PATH", "./telephone.db")
				os.Setenv("MAX_RESPONSE_SIZE", "104857600")
				os.Setenv("CHUNK_SIZE", "1048576")
				os.Setenv("DB_TIMEOUT", "10s")
			},
			expectError: false,
			validate: func(t *testing.T, cfg *Config) {
				if cfg.ConnectTimeout != 10*time.Second {
					t.Errorf("expected ConnectTimeout 10s, got %v", cfg.ConnectTimeout)
				}
				if cfg.MaxRetries != 100 {
					t.Errorf("expected MaxRetries 100, got %d", cfg.MaxRetries)
				}
			},
		},
		{
			name: "missing PLUGBOARD_URL",
			setupEnv: func() {
				os.Clearenv()
				os.Setenv("SECRET_KEY_BASE", "test-secret-key-base")
				os.Setenv("BACKEND_HOST", "localhost")
				os.Setenv("BACKEND_PORT", "8080")
				os.Setenv("BACKEND_SCHEME", "http")
				os.Setenv("CONNECT_TIMEOUT", "10s")
				os.Setenv("REQUEST_TIMEOUT", "30s")
				os.Setenv("HEARTBEAT_INTERVAL", "30s")
				os.Setenv("CONNECTION_MONITOR_INTERVAL", "5s")
				os.Setenv("INITIAL_BACKOFF", "1s")
				os.Setenv("MAX_BACKOFF", "30s")
				os.Setenv("MAX_RETRIES", "100")
				os.Setenv("TOKEN_DB_PATH", "./telephone.db")
				os.Setenv("MAX_RESPONSE_SIZE", "104857600")
				os.Setenv("CHUNK_SIZE", "1048576")
				os.Setenv("DB_TIMEOUT", "10s")
			},
			expectError: true,
		},
		{
			name: "invalid BACKEND_SCHEME",
			setupEnv: func() {
				os.Clearenv()
				os.Setenv("SECRET_KEY_BASE", "test-secret-key-base")
				os.Setenv("PLUGBOARD_URL", "ws://localhost:4000/telephone/websocket")
				os.Setenv("BACKEND_HOST", "localhost")
				os.Setenv("BACKEND_PORT", "8080")
				os.Setenv("BACKEND_SCHEME", "ftp")
				os.Setenv("CONNECT_TIMEOUT", "10s")
				os.Setenv("REQUEST_TIMEOUT", "30s")
				os.Setenv("HEARTBEAT_INTERVAL", "30s")
				os.Setenv("CONNECTION_MONITOR_INTERVAL", "5s")
				os.Setenv("INITIAL_BACKOFF", "1s")
				os.Setenv("MAX_BACKOFF", "30s")
				os.Setenv("MAX_RETRIES", "100")
				os.Setenv("TOKEN_DB_PATH", "./telephone.db")
				os.Setenv("MAX_RESPONSE_SIZE", "104857600")
				os.Setenv("CHUNK_SIZE", "1048576")
				os.Setenv("DB_TIMEOUT", "10s")
			},
			expectError: true,
		},
		{
			name: "valid https backend",
			setupEnv: func() {
				os.Clearenv()
				os.Setenv("SECRET_KEY_BASE", "test-secret-key-base")
				os.Setenv("PLUGBOARD_URL", "ws://localhost:4000/telephone/websocket")
				os.Setenv("BACKEND_SCHEME", "https")
				os.Setenv("BACKEND_HOST", "api.example.com")
				os.Setenv("BACKEND_PORT", "443")
				os.Setenv("CONNECT_TIMEOUT", "10s")
				os.Setenv("REQUEST_TIMEOUT", "30s")
				os.Setenv("HEARTBEAT_INTERVAL", "30s")
				os.Setenv("CONNECTION_MONITOR_INTERVAL", "5s")
				os.Setenv("INITIAL_BACKOFF", "1s")
				os.Setenv("MAX_BACKOFF", "30s")
				os.Setenv("MAX_RETRIES", "100")
				os.Setenv("TOKEN_DB_PATH", "./telephone.db")
				os.Setenv("MAX_RESPONSE_SIZE", "104857600")
				os.Setenv("CHUNK_SIZE", "1048576")
				os.Setenv("DB_TIMEOUT", "10s")
			},
			expectError: false,
			validate: func(t *testing.T, cfg *Config) {
				if cfg.BackendScheme != "https" {
					t.Errorf("expected BackendScheme https, got %s", cfg.BackendScheme)
				}
				if cfg.BackendURL() != "https://api.example.com:443" {
					t.Errorf("expected backend URL https://api.example.com:443, got %s", cfg.BackendURL())
				}
			},
		},
		{
			name: "custom MAX_RESPONSE_SIZE",
			setupEnv: func() {
				os.Clearenv()
				os.Setenv("SECRET_KEY_BASE", "test-secret-key-base")
				os.Setenv("PLUGBOARD_URL", "ws://localhost:4000/telephone/websocket")
				os.Setenv("BACKEND_HOST", "localhost")
				os.Setenv("BACKEND_PORT", "8080")
				os.Setenv("BACKEND_SCHEME", "http")
				os.Setenv("MAX_RESPONSE_SIZE", "52428800")
				os.Setenv("CONNECT_TIMEOUT", "10s")
				os.Setenv("REQUEST_TIMEOUT", "30s")
				os.Setenv("HEARTBEAT_INTERVAL", "30s")
				os.Setenv("CONNECTION_MONITOR_INTERVAL", "5s")
				os.Setenv("INITIAL_BACKOFF", "1s")
				os.Setenv("MAX_BACKOFF", "30s")
				os.Setenv("MAX_RETRIES", "100")
				os.Setenv("TOKEN_DB_PATH", "./telephone.db")
				os.Setenv("CHUNK_SIZE", "1048576")
				os.Setenv("DB_TIMEOUT", "10s")
			},
			expectError: false,
			validate: func(t *testing.T, cfg *Config) {
				if cfg.MaxResponseSize != 52428800 {
					t.Errorf("expected MaxResponseSize 52428800, got %d", cfg.MaxResponseSize)
				}
			},
		},
		{
			name: "custom CHUNK_SIZE",
			setupEnv: func() {
				os.Clearenv()
				os.Setenv("SECRET_KEY_BASE", "test-secret-key-base")
				os.Setenv("PLUGBOARD_URL", "ws://localhost:4000/telephone/websocket")
				os.Setenv("BACKEND_HOST", "localhost")
				os.Setenv("BACKEND_PORT", "8080")
				os.Setenv("BACKEND_SCHEME", "http")
				os.Setenv("CHUNK_SIZE", "524288")
				os.Setenv("CONNECT_TIMEOUT", "10s")
				os.Setenv("REQUEST_TIMEOUT", "30s")
				os.Setenv("HEARTBEAT_INTERVAL", "30s")
				os.Setenv("CONNECTION_MONITOR_INTERVAL", "5s")
				os.Setenv("INITIAL_BACKOFF", "1s")
				os.Setenv("MAX_BACKOFF", "30s")
				os.Setenv("MAX_RETRIES", "100")
				os.Setenv("TOKEN_DB_PATH", "./telephone.db")
				os.Setenv("MAX_RESPONSE_SIZE", "104857600")
				os.Setenv("DB_TIMEOUT", "10s")
			},
			expectError: false,
			validate: func(t *testing.T, cfg *Config) {
				if cfg.ChunkSize != 524288 {
					t.Errorf("expected ChunkSize 524288, got %d", cfg.ChunkSize)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupEnv()

			cfg, err := LoadFromEnv()

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if tt.validate != nil {
					tt.validate(t, cfg)
				}
			}
		})
	}
}

func TestBackendURL(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *Config
		expected string
	}{
		{
			name: "http backend",
			cfg: &Config{
				BackendScheme: "http",
				BackendHost:   "localhost",
				BackendPort:   8080,
			},
			expected: "http://localhost:8080",
		},
		{
			name: "https backend",
			cfg: &Config{
				BackendScheme: "https",
				BackendHost:   "api.example.com",
				BackendPort:   443,
			},
			expected: "https://api.example.com:443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.BackendURL()
			if got != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, got)
			}
		})
	}
}
