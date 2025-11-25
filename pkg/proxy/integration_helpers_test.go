package proxy

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/verastack/telephone/pkg/config"
)

// integrationTestSetup encapsulates common integration test setup
type integrationTestSetup struct {
	Config  *config.Config
	Tel     *Telephone
	DBPath  string
	Cleanup func()
	ErrorCh chan error
}

// setupIntegrationTest performs common integration test setup
func setupIntegrationTest(t *testing.T, testName string) *integrationTestSetup {
	t.Helper()

	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if !isPlugboardAvailable(t) {
		t.Skip("Plugboard not available on localhost:4000")
	}

	cfg, err := loadTestConfig(t)
	if err != nil {
		t.Skip("Skipping test: " + err.Error())
	}

	// Use unique test database
	dbPath := fmt.Sprintf("/tmp/telephone_integration_test_%s_%d.db", testName, time.Now().Unix())
	cfg.TokenDBPath = dbPath

	tel, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to connect to Plugboard: %v", err)
	}

	errChan := make(chan error, 1)

	setup := &integrationTestSetup{
		Config:  cfg,
		Tel:     tel,
		DBPath:  dbPath,
		ErrorCh: errChan,
		Cleanup: func() {
			tel.Stop()
			os.Remove(dbPath)
		},
	}

	return setup
}

// startTelephoneAsync starts Telephone in background and waits for startup
func (s *integrationTestSetup) startTelephoneAsync(t *testing.T, startupWait time.Duration) {
	t.Helper()

	go func() {
		if err := s.Tel.Start(); err != nil {
			s.ErrorCh <- err
		}
	}()

	// Wait for startup or error
	select {
	case err := <-s.ErrorCh:
		t.Fatalf("Failed to start Telephone: %v", err)
	case <-time.After(startupWait):
		// Startup successful
	}
}

// isPlugboardAvailable checks if Plugboard is reachable
func isPlugboardAvailable(t *testing.T) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:4000")
	if err != nil {
		t.Logf("Plugboard not available: %v", err)
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

// parsePort converts port string to int
func parsePort(portStr string) (int, bool) {
	var port int
	_, err := fmt.Sscanf(portStr, "%d", &port)
	return port, err == nil
}

// loadTestConfig loads config with proper error handling
func loadTestConfig(t testing.TB) (*config.Config, error) {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		if t != nil {
			t.Logf("Config load error: %v", err)
		}
		return nil, fmt.Errorf("missing required environment variables (run from project root or set TELEPHONE_TOKEN and SECRET_KEY_BASE)")
	}
	return cfg, nil
}

// configureBackend updates config to point to test backend
func configureBackend(cfg *config.Config, backendURL string) error {
	backendHost := strings.TrimPrefix(backendURL, "http://")
	parts := strings.Split(backendHost, ":")
	if len(parts) != 2 {
		return fmt.Errorf("invalid backend URL: %s", backendURL)
	}

	cfg.BackendHost = "127.0.0.1"
	if port, ok := parsePort(parts[1]); ok {
		cfg.BackendPort = port
		return nil
	}
	return fmt.Errorf("failed to parse backend port: %s", parts[1])
}

// assertTokenValid checks that token is non-empty
func assertTokenValid(t *testing.T, tel *Telephone) {
	t.Helper()
	token := tel.getCurrentToken()
	if token == "" {
		t.Error("Expected non-empty token")
	}
}

// assertHeartbeatRecent checks that last heartbeat was recent
func assertHeartbeatRecent(t *testing.T, tel *Telephone, maxAge time.Duration) {
	t.Helper()
	tel.heartbeatLock.RLock()
	lastHeartbeat := tel.lastHeartbeatAck
	tel.heartbeatLock.RUnlock()

	if lastHeartbeat.IsZero() {
		t.Error("Expected heartbeat to have been acknowledged")
		return
	}

	elapsed := time.Since(lastHeartbeat)
	if elapsed > maxAge {
		t.Errorf("Last heartbeat was too long ago: %v (max: %v)", elapsed, maxAge)
	}
}

// waitForCondition waits for a condition to be true or times out
func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool, description string) {
	t.Helper()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	deadline := time.Now().Add(timeout)
	for {
		if condition() {
			return
		}

		select {
		case <-ticker.C:
			if time.Now().After(deadline) {
				t.Fatalf("Timeout waiting for: %s", description)
			}
		}
	}
}
