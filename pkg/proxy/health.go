package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/verastack/telephone/pkg/logger"
)

// HealthStatus represents the health check response.
type HealthStatus struct {
	Status      string `json:"status"`
	Connected   bool   `json:"connected"`
	Uptime      string `json:"uptime"`
	LastHB      string `json:"last_heartbeat,omitempty"`
	TokenExpiry string `json:"token_expiry,omitempty"`
	Version     string `json:"version,omitempty"`
}

// healthServer manages the HTTP health check endpoint.
type healthServer struct {
	server    *http.Server
	telephone *Telephone
	startTime time.Time
	running   atomic.Bool
}

// newHealthServer creates a new health check server.
func newHealthServer(tel *Telephone, port int) *healthServer {
	hs := &healthServer{
		telephone: tel,
		startTime: time.Now(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", hs.handleHealth)
	mux.HandleFunc("/healthz", hs.handleHealth) // Kubernetes-style endpoint
	mux.HandleFunc("/ready", hs.handleReady)
	mux.HandleFunc("/readyz", hs.handleReady) // Kubernetes-style endpoint
	mux.HandleFunc("/live", hs.handleLive)
	mux.HandleFunc("/livez", hs.handleLive) // Kubernetes-style endpoint

	hs.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	return hs
}

// Start starts the health check server.
func (hs *healthServer) Start() error {
	if hs.running.Load() {
		return fmt.Errorf("health server already running")
	}

	hs.running.Store(true)
	logger.Info("Starting health check server", "addr", hs.server.Addr)

	go func() {
		if err := hs.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Health check server error", "error", err)
		}

		hs.running.Store(false)
	}()

	return nil
}

// Stop gracefully stops the health check server.
func (hs *healthServer) Stop(ctx context.Context) error {
	if !hs.running.Load() {
		return nil
	}

	logger.Info("Stopping health check server...")

	return hs.server.Shutdown(ctx)
}

// handleHealth returns the overall health status.
func (hs *healthServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := hs.getHealthStatus()

	w.Header().Set("Content-Type", "application/json")

	if status.Status == "healthy" {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	if err := json.NewEncoder(w).Encode(status); err != nil {
		logger.Error("Failed to encode health response", "error", err)
	}
}

// handleReady returns readiness status (is the service ready to accept traffic?)
func (hs *healthServer) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Ready if connected to Plugboard and token is valid
	connected := hs.telephone.client.IsConnected()
	tokenValid := hs.telephone.isTokenValid()

	if connected && tokenValid {
		w.WriteHeader(http.StatusOK)

		if _, err := w.Write([]byte("ready")); err != nil {
			logger.Error("Failed to write ready response", "error", err)
		}
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)

		if !connected {
			if _, err := w.Write([]byte("not connected to Plugboard")); err != nil {
				logger.Error("Failed to write not connected response", "error", err)
			}
		} else {
			if _, err := w.Write([]byte("token expired or invalid")); err != nil {
				logger.Error("Failed to write token invalid response", "error", err)
			}
		}
	}
}

// handleLive returns liveness status (is the service alive?)
func (hs *healthServer) handleLive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Live if the process is running (always true if we can respond)
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write([]byte("alive")); err != nil {
		logger.Error("Failed to write alive response", "error", err)
	}
}

// getHealthStatus builds the health status response.
func (hs *healthServer) getHealthStatus() HealthStatus {
	connected := hs.telephone.client.IsConnected()
	uptime := time.Since(hs.startTime)

	status := HealthStatus{
		Connected: connected,
		Uptime:    uptime.Round(time.Second).String(),
		Version:   "1.0.0", // Could be injected at build time
	}

	// Get last heartbeat time
	hs.telephone.heartbeatLock.RLock()
	lastHB := hs.telephone.lastHeartbeat
	hs.telephone.heartbeatLock.RUnlock()

	if !lastHB.IsZero() {
		status.LastHB = time.Since(lastHB).Round(time.Second).String() + " ago"
	}

	// Get token expiry
	hs.telephone.tokenMu.RLock()
	claims := hs.telephone.claims
	hs.telephone.tokenMu.RUnlock()

	if claims != nil {
		expiresIn := claims.ExpiresIn()
		if expiresIn > 0 {
			status.TokenExpiry = expiresIn.Round(time.Second).String()
		} else {
			status.TokenExpiry = "expired"
		}
	}

	// Determine overall status
	switch {
	case connected && hs.telephone.isTokenValid():
		status.Status = "healthy"
	case connected:
		status.Status = "degraded" // Connected but token issues
	default:
		status.Status = "unhealthy"
	}

	return status
}

// isTokenValid checks if the current token is still valid.
func (t *Telephone) isTokenValid() bool {
	t.tokenMu.RLock()
	claims := t.claims
	t.tokenMu.RUnlock()

	if claims == nil {
		return false
	}

	return claims.ExpiresIn() > 0
}
