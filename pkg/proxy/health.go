package proxy

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// HealthStatus represents the health check response
type HealthStatus struct {
	Status      string    `json:"status"`
	Connected   bool      `json:"connected"`
	LastHeartbeat string  `json:"last_heartbeat,omitempty"`
	PathID      string    `json:"path_id"`
	TokenExpiry string    `json:"token_expiry"`
	Uptime      string    `json:"uptime"`
	Version     string    `json:"version"`
}

// StartHealthServer starts the health check HTTP server
func (t *Telephone) StartHealthServer(port string) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", t.healthHandler)
	mux.HandleFunc("/ready", t.readyHandler)
	mux.HandleFunc("/live", t.liveHandler)

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	go func() {
		log.Printf("Health check server listening on :%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Health server error: %v", err)
		}
	}()

	return nil
}

// healthHandler returns detailed health information
func (t *Telephone) healthHandler(w http.ResponseWriter, r *http.Request) {
	t.heartbeatLock.RLock()
	lastHeartbeat := t.lastHeartbeat
	t.heartbeatLock.RUnlock()

	status := "healthy"
	if !t.client.IsConnected() {
		status = "unhealthy"
	}

	health := HealthStatus{
		Status:      status,
		Connected:   t.client.IsConnected(),
		PathID:      t.claims.PathID,
		TokenExpiry: t.claims.ExpiresAt().Format(time.RFC3339),
		Version:     "dev",
	}

	if !lastHeartbeat.IsZero() {
		health.LastHeartbeat = lastHeartbeat.Format(time.RFC3339)
	}

	w.Header().Set("Content-Type", "application/json")

	if status == "unhealthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	json.NewEncoder(w).Encode(health)
}

// readyHandler returns 200 if ready to accept traffic
func (t *Telephone) readyHandler(w http.ResponseWriter, r *http.Request) {
	if t.client.IsConnected() {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("not ready"))
	}
}

// liveHandler returns 200 if the process is alive
func (t *Telephone) liveHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("alive"))
}
