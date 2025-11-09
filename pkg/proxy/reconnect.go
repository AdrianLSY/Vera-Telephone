package proxy

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"time"

	"github.com/verastack/telephone/pkg/auth"
)

// reconnect handles reconnection logic with exponential backoff
func (t *Telephone) reconnect() error {
	// Prevent concurrent reconnection attempts
	t.reconnecting.Lock()
	if t.reconnectFlag {
		t.reconnecting.Unlock()
		return fmt.Errorf("reconnection already in progress")
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
			return fmt.Errorf("reconnection cancelled")
		default:
		}

		attempt++

		log.Printf("Reconnection attempt %d (backoff: %v)", attempt, backoff)

		// Try to connect with current token
		if err := t.client.Connect(); err != nil {
			log.Printf("Reconnection failed with current token: %v", err)

			// If we have an original token that differs from current, try falling back to it
			// This handles server restarts where refreshed tokens may not be recognized
			currentToken := t.getCurrentToken()
			if t.originalToken != "" && t.originalToken != currentToken {
				log.Printf("Attempting reconnection with original token...")

				// Temporarily update URL with original token
				originalURL := fmt.Sprintf("%s?token=%s&vsn=2.0.0", t.config.PlugboardURL, t.originalToken)
				t.client.UpdateURL(originalURL)

				if err := t.client.Connect(); err != nil {
					log.Printf("Reconnection failed with original token: %v", err)

					// Restore current token URL
					currentURL := fmt.Sprintf("%s?token=%s&vsn=2.0.0", t.config.PlugboardURL, currentToken)
					t.client.UpdateURL(currentURL)
				} else {
					// Successfully connected with original token
					// Update current token to original since that's what worked
					log.Printf("Successfully reconnected with original token")

					// Parse original token to get claims
					if claims, err := auth.ParseJWTUnsafe(t.originalToken); err == nil {
						t.updateToken(t.originalToken, claims)
					}

					// Continue to channel join below
					goto channelJoin
				}
			}

			// Check if we've exceeded max retries (if configured)
			if t.config.MaxRetries > 0 && attempt >= t.config.MaxRetries {
				return fmt.Errorf("max reconnection attempts (%d) exceeded", t.config.MaxRetries)
			}

			// Calculate next backoff with jitter
			backoff = calculateBackoffWithJitter(attempt, t.config.InitialBackoff, t.config.MaxBackoff)

			// Wait with exponential backoff
			select {
			case <-time.After(backoff):
				// Continue to next attempt
			case <-t.ctx.Done():
				return fmt.Errorf("reconnection cancelled during backoff")
			}
			continue
		}

	channelJoin:

		// Successfully reconnected - try to rejoin channel
		log.Printf("WebSocket reconnected, attempting to rejoin channel...")

		if err := t.joinChannel(); err != nil {
			log.Printf("Failed to rejoin channel: %v", err)

			// Disconnect and retry
			t.client.Disconnect()

			// Wait before retrying
			backoff = calculateBackoffWithJitter(attempt, t.config.InitialBackoff, t.config.MaxBackoff)
			select {
			case <-time.After(backoff):
				// Continue to next attempt
			case <-t.ctx.Done():
				return fmt.Errorf("reconnection cancelled during channel join retry")
			}
			continue
		}

		log.Printf("Successfully reconnected and rejoined channel after %d attempts", attempt)

		// Reset heartbeat tracking
		t.heartbeatLock.Lock()
		t.lastHeartbeat = time.Now()
		t.heartbeatLock.Unlock()

		return nil
	}
}

// monitorConnection monitors the WebSocket connection and reconnects if needed
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
				log.Printf("Connection lost (failure %d), attempting to reconnect...",
					consecutiveFailures)

				if err := t.reconnect(); err != nil {
					// Ignore "already in progress" errors
					if err.Error() == "reconnection already in progress" {
						continue
					}
					log.Printf("Reconnection failed: %v", err)

					// Continue monitoring - will keep retrying indefinitely
					continue
				}

				// Successfully reconnected
				consecutiveFailures = 0
				log.Printf("Connection restored successfully")
			} else {
				// Connection appears healthy, but check heartbeat timeout
				t.heartbeatLock.RLock()
				lastHB := t.lastHeartbeat
				t.heartbeatLock.RUnlock()

				// If we haven't received a heartbeat ack in 3x the heartbeat interval, consider connection dead
				heartbeatTimeout := t.config.HeartbeatInterval * 3
				if !lastHB.IsZero() && time.Since(lastHB) > heartbeatTimeout {
					log.Printf("Heartbeat timeout detected (last: %v ago, threshold: %v), reconnecting...",
						time.Since(lastHB), heartbeatTimeout)

					// Force disconnect and reconnect
					t.client.Disconnect()

					if err := t.reconnect(); err != nil {
						if err.Error() == "reconnection already in progress" {
							continue
						}
						log.Printf("Reconnection after heartbeat timeout failed: %v", err)
						continue
					}

					log.Printf("Connection restored after heartbeat timeout")
				} else {
					// Connection is healthy
					if consecutiveFailures > 0 {
						consecutiveFailures = 0
					}
				}
			}
		}
	}
}

// calculateBackoffWithJitter calculates exponential backoff with jitter to prevent thundering herd
func calculateBackoffWithJitter(attempt int, initial, max time.Duration) time.Duration {
	// Exponential backoff: initial * 2^attempt
	backoff := time.Duration(float64(initial) * math.Pow(2, float64(attempt-1)))

	// Cap at maximum
	if backoff > max {
		backoff = max
	}

	// Add jitter: ±25% randomization
	jitterPercent := 0.25
	jitter := time.Duration(float64(backoff) * jitterPercent * (rand.Float64()*2 - 1))
	backoff += jitter

	// Ensure we don't go below initial or above max
	if backoff < initial {
		backoff = initial
	}
	if backoff > max {
		backoff = max
	}

	return backoff
}
