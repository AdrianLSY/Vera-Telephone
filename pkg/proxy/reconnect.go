package proxy

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/verastack/telephone/pkg/auth"
	"github.com/verastack/telephone/pkg/logger"
)

// reconnect handles reconnection logic with exponential backoff.
func (t *Telephone) reconnect() error {
	// Prevent concurrent reconnection attempts
	t.reconnecting.Lock()
	if t.reconnectFlag {
		t.reconnecting.Unlock()
		return ErrReconnectionInProgress
	}

	t.reconnectFlag = true
	t.reconnecting.Unlock()

	defer func() {
		t.reconnecting.Lock()
		t.reconnectFlag = false
		t.reconnecting.Unlock()
	}()

	backoff := t.config.InitialBackoff
	attempt := 0

	for {
		select {
		case <-t.ctx.Done():
			return ErrReconnectionCancelled
		default:
		}

		attempt++

		logger.Info("Reconnection attempt",
			"attempt", attempt,
			"backoff", backoff,
		)

		// Try to connect (with fallback to original token if needed)
		connected := t.attemptConnection()

		if !connected {
			// Check if we've exceeded max retries (if configured)
			if t.config.MaxRetries > 0 && attempt >= t.config.MaxRetries {
				return fmt.Errorf("%w: %d attempts", ErrMaxRetriesExceeded, t.config.MaxRetries)
			}

			// Calculate next backoff with jitter
			backoff = calculateBackoffWithJitter(attempt, t.config.InitialBackoff, t.config.MaxBackoff)

			// Wait with exponential backoff
			select {
			case <-time.After(backoff):
				// Continue to next attempt
			case <-t.ctx.Done():
				return ErrReconnectionCancelledDuringBackoff
			}

			continue
		}

		// Successfully reconnected - try to rejoin channel
		logger.Info("WebSocket reconnected, attempting to rejoin channel...")

		if err := t.joinChannel(); err != nil {
			logger.Error("Failed to rejoin channel", "error", err)

			// Disconnect and retry
			_ = t.client.Disconnect() //nolint:errcheck // Best-effort disconnect before retry

			// Wait before retrying
			backoff = calculateBackoffWithJitter(attempt, t.config.InitialBackoff, t.config.MaxBackoff)
			select {
			case <-time.After(backoff):
				// Continue to next attempt
			case <-t.ctx.Done():
				return ErrReconnectionCancelledDuringChannelJoin
			}

			continue
		}

		logger.Info("Successfully reconnected and rejoined channel",
			"attempts", attempt,
		)

		// Reset heartbeat tracking
		t.heartbeatLock.Lock()
		t.lastHeartbeat = time.Now()
		t.heartbeatLock.Unlock()

		return nil
	}
}

// attemptConnection tries to connect with the current token, falling back to the
// original token if needed. Returns true if connection was successful.
func (t *Telephone) attemptConnection() bool {
	// Try to connect with current token
	if err := t.client.Connect(); err == nil {
		return true
	}

	logger.Warn("Reconnection failed with current token")

	// If we have an original token that differs from current, try falling back to it
	// This handles server restarts where refreshed tokens may not be recognized
	currentToken := t.getCurrentToken()
	if t.originalToken == "" || t.originalToken == currentToken {
		return false
	}

	logger.Info("Attempting reconnection with original token...")

	// Temporarily update client token to original
	// Token is added as query parameter per Phoenix Socket requirement
	t.client.UpdateToken(t.originalToken)

	if err := t.client.Connect(); err != nil {
		logger.Error("Reconnection failed with original token", "error", err)

		// Restore current token
		t.client.UpdateToken(currentToken)

		return false
	}

	// Successfully connected with original token
	// Update current token to original since that's what worked
	logger.Info("Successfully reconnected with original token")

	// Parse original token to get claims
	if claims, err := auth.ParseJWTUnsafe(t.originalToken); err == nil {
		t.updateToken(t.originalToken, claims)
	}

	return true
}

// monitorConnection monitors the WebSocket connection and reconnects if needed.
func (t *Telephone) monitorConnection() {
	defer t.wg.Done()

	ticker := time.NewTicker(t.config.ConnectionMonitorInterval)
	defer ticker.Stop()

	consecutiveFailures := 0

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			// Check if connection is lost
			if !t.client.IsConnected() {
				consecutiveFailures++
				logger.Warn("Connection lost, attempting to reconnect",
					"consecutive_failures", consecutiveFailures,
				)

				if err := t.reconnect(); err != nil {
					// Ignore "already in progress" errors
					if errors.Is(err, ErrReconnectionInProgress) {
						continue
					}

					logger.Error("Reconnection failed", "error", err)

					// Continue monitoring - will keep retrying indefinitely
					continue
				}

				// Successfully reconnected
				consecutiveFailures = 0

				logger.Info("Connection restored successfully")
			} else {
				// Connection appears healthy, but check heartbeat timeout
				t.heartbeatLock.RLock()
				lastHB := t.lastHeartbeat
				t.heartbeatLock.RUnlock()

				// If we haven't received a heartbeat ack in 3x the heartbeat interval, consider connection dead
				heartbeatTimeout := t.config.HeartbeatInterval * 3
				if !lastHB.IsZero() && time.Since(lastHB) > heartbeatTimeout {
					logger.Warn("Heartbeat timeout detected, reconnecting",
						"last_heartbeat_ago", time.Since(lastHB),
						"threshold", heartbeatTimeout,
					)

					// Force disconnect and reconnect
					_ = t.client.Disconnect() //nolint:errcheck // Best-effort disconnect on heartbeat timeout

					if err := t.reconnect(); err != nil {
						if errors.Is(err, ErrReconnectionInProgress) {
							continue
						}

						logger.Error("Reconnection after heartbeat timeout failed", "error", err)

						continue
					}

					logger.Info("Connection restored after heartbeat timeout")
				} else if consecutiveFailures > 0 {
					// Connection is healthy
					consecutiveFailures = 0
				}
			}
		}
	}
}

// Uses cryptographically secure random for jitter to prevent timing attacks.
func calculateBackoffWithJitter(attempt int, initial, maxBackoff time.Duration) time.Duration {
	// Exponential backoff: initial * 2^attempt
	backoff := time.Duration(float64(initial) * math.Pow(2, float64(attempt-1)))

	// Cap at maximum
	if backoff > maxBackoff {
		backoff = maxBackoff
	}

	// Add jitter: ±25% randomization using cryptographically secure random
	jitterPercent := 0.25
	jitterRange := time.Duration(float64(backoff) * jitterPercent)
	// Generate random value in range [0, 2*jitterRange) then subtract jitterRange to get [-jitterRange, +jitterRange)
	jitter := secureRandomDuration(jitterRange*2) - jitterRange
	backoff += jitter

	// Ensure we don't go below initial or above maxBackoff
	if backoff < initial {
		backoff = initial
	}

	if backoff > maxBackoff {
		backoff = maxBackoff
	}

	return backoff
}
