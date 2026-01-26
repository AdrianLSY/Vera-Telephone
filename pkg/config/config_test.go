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
				t.Helper()
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
				t.Helper()
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
				t.Helper()
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
				t.Helper()
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

// TestLoadFromEnvMissingVariables tests individual missing required variables.
func TestLoadFromEnvMissingVariables(t *testing.T) {
	requiredVars := []string{
		"PLUGBOARD_URL",
		"SECRET_KEY_BASE",
		"BACKEND_HOST",
		"BACKEND_PORT",
		"BACKEND_SCHEME",
		"CONNECT_TIMEOUT",
		"REQUEST_TIMEOUT",
		"HEARTBEAT_INTERVAL",
		"CONNECTION_MONITOR_INTERVAL",
		"INITIAL_BACKOFF",
		"MAX_BACKOFF",
		"MAX_RETRIES",
		"TOKEN_DB_PATH",
		"MAX_RESPONSE_SIZE",
		"CHUNK_SIZE",
		"DB_TIMEOUT",
	}

	// Setup complete environment
	setupCompleteEnv := func() {
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
	}

	for _, varName := range requiredVars {
		t.Run("missing_"+varName, func(t *testing.T) {
			setupCompleteEnv()
			os.Unsetenv(varName)

			_, err := LoadFromEnv()
			if err == nil {
				t.Errorf("Expected error when %s is missing", varName)
			}

			if err != nil && !strings.Contains(err.Error(), varName) {
				t.Errorf("Expected error message to mention %s, got: %v", varName, err)
			}
		})
	}
}

// TestLoadFromEnvInvalidValues tests invalid values for various fields.
func TestLoadFromEnvInvalidValues(t *testing.T) {
	setupCompleteEnv := func() {
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
	}

	tests := []struct {
		name        string
		varName     string
		value       string
		expectError bool
	}{
		{"invalid_backend_port", "BACKEND_PORT", "not-a-number", true},
		{"invalid_connect_timeout", "CONNECT_TIMEOUT", "not-a-duration", true},
		{"invalid_request_timeout", "REQUEST_TIMEOUT", "invalid", true},
		{"invalid_heartbeat_interval", "HEARTBEAT_INTERVAL", "abc", true},
		{"invalid_connection_monitor_interval", "CONNECTION_MONITOR_INTERVAL", "xyz", true},
		{"invalid_initial_backoff", "INITIAL_BACKOFF", "bad", true},
		{"invalid_max_backoff", "MAX_BACKOFF", "wrong", true},
		{"invalid_max_retries", "MAX_RETRIES", "not-a-number", true},
		{"invalid_max_response_size", "MAX_RESPONSE_SIZE", "not-a-number", true},
		{"invalid_chunk_size", "CHUNK_SIZE", "not-a-number", true},
		{"invalid_db_timeout", "DB_TIMEOUT", "not-a-duration", true},
		{"negative_max_retries", "MAX_RETRIES", "-10", false}, // -1 is valid for infinite
		{"negative_max_response_size", "MAX_RESPONSE_SIZE", "-1", true},
		{"negative_chunk_size", "CHUNK_SIZE", "-1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupCompleteEnv()
			os.Setenv(tt.varName, tt.value)

			_, err := LoadFromEnv()
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for %s=%s", tt.varName, tt.value)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for %s=%s: %v", tt.varName, tt.value, err)
				}
			}
		})
	}
}

// TestLoadFromEnvBoundaryValues tests boundary values.
func TestLoadFromEnvBoundaryValues(t *testing.T) {
	setupCompleteEnv := func() {
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
	}

	tests := []struct {
		name        string
		varName     string
		value       string
		expectError bool
		validate    func(*testing.T, *Config)
	}{
		{
			name:        "min_backend_port",
			varName:     "BACKEND_PORT",
			value:       "1",
			expectError: false,
			validate: func(t *testing.T, cfg *Config) {
				t.Helper()
				if cfg.BackendPort != 1 {
					t.Errorf("Expected port 1, got %d", cfg.BackendPort)
				}
			},
		},
		{
			name:        "max_backend_port",
			varName:     "BACKEND_PORT",
			value:       "65535",
			expectError: false,
			validate: func(t *testing.T, cfg *Config) {
				t.Helper()
				if cfg.BackendPort != 65535 {
					t.Errorf("Expected port 65535, got %d", cfg.BackendPort)
				}
			},
		},
		{
			name:        "max_retries_minus_one",
			varName:     "MAX_RETRIES",
			value:       "-1",
			expectError: false,
			validate: func(t *testing.T, cfg *Config) {
				t.Helper()
				if cfg.MaxRetries != -1 {
					t.Errorf("Expected MaxRetries -1, got %d", cfg.MaxRetries)
				}
			},
		},
		{
			name:        "zero_max_retries",
			varName:     "MAX_RETRIES",
			value:       "0",
			expectError: false,
			validate: func(t *testing.T, cfg *Config) {
				t.Helper()
				if cfg.MaxRetries != 0 {
					t.Errorf("Expected MaxRetries 0, got %d", cfg.MaxRetries)
				}
			},
		},
		{
			name:        "very_large_max_response_size",
			varName:     "MAX_RESPONSE_SIZE",
			value:       "10737418240", // 10GB
			expectError: false,
			validate: func(t *testing.T, cfg *Config) {
				t.Helper()
				if cfg.MaxResponseSize != 10737418240 {
					t.Errorf("Expected MaxResponseSize 10737418240, got %d", cfg.MaxResponseSize)
				}
			},
		},
		{
			name:        "very_small_timeout",
			varName:     "CONNECT_TIMEOUT",
			value:       "1ms",
			expectError: false,
			validate: func(t *testing.T, cfg *Config) {
				t.Helper()
				if cfg.ConnectTimeout != 1*time.Millisecond {
					t.Errorf("Expected ConnectTimeout 1ms, got %v", cfg.ConnectTimeout)
				}
			},
		},
		{
			name:        "very_large_timeout",
			varName:     "REQUEST_TIMEOUT",
			value:       "1h",
			expectError: false,
			validate: func(t *testing.T, cfg *Config) {
				t.Helper()
				if cfg.RequestTimeout != 1*time.Hour {
					t.Errorf("Expected RequestTimeout 1h, got %v", cfg.RequestTimeout)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupCompleteEnv()
			os.Setenv(tt.varName, tt.value)

			cfg, err := LoadFromEnv()
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for %s=%s", tt.varName, tt.value)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for %s=%s: %v", tt.varName, tt.value, err)
				}

				if tt.validate != nil && cfg != nil {
					tt.validate(t, cfg)
				}
			}
		})
	}
}

// TestLoadFromEnvEmptyValues tests empty string values.
func TestLoadFromEnvEmptyValues(t *testing.T) {
	setupCompleteEnv := func() {
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
	}

	emptyTestVars := []string{
		"PLUGBOARD_URL",
		"SECRET_KEY_BASE",
		"BACKEND_HOST",
		"BACKEND_SCHEME",
		"TOKEN_DB_PATH",
	}

	for _, varName := range emptyTestVars {
		t.Run("empty_"+varName, func(t *testing.T) {
			setupCompleteEnv()
			os.Setenv(varName, "") // Set to empty string

			_, err := LoadFromEnv()
			if err == nil {
				t.Errorf("Expected error when %s is empty", varName)
			}
		})
	}
}

// TestBackendURLEdgeCases tests edge cases in BackendURL generation.
func TestBackendURLEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *Config
		expected string
	}{
		{
			name: "ipv4_address",
			cfg: &Config{
				BackendScheme: "http",
				BackendHost:   "192.168.1.1",
				BackendPort:   8080,
			},
			expected: "http://192.168.1.1:8080",
		},
		{
			name: "domain_with_subdomain",
			cfg: &Config{
				BackendScheme: "https",
				BackendHost:   "api.staging.example.com",
				BackendPort:   443,
			},
			expected: "https://api.staging.example.com:443",
		},
		{
			name: "standard_http_port",
			cfg: &Config{
				BackendScheme: "http",
				BackendHost:   "example.com",
				BackendPort:   80,
			},
			expected: "http://example.com:80",
		},
		{
			name: "standard_https_port",
			cfg: &Config{
				BackendScheme: "https",
				BackendHost:   "example.com",
				BackendPort:   443,
			},
			expected: "https://example.com:443",
		},
		{
			name: "non_standard_port",
			cfg: &Config{
				BackendScheme: "http",
				BackendHost:   "localhost",
				BackendPort:   3000,
			},
			expected: "http://localhost:3000",
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
