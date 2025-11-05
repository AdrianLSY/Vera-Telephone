package proxy

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"time"
)

// reconnect handles reconnection logic with exponential backoff
func (t *Telephone) reconnect() error {
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

		// Try to connect
		if err := t.client.Connect(); err != nil {
			log.Printf("Reconnection failed: %v", err)

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

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	consecutiveFailures := 0
	const maxConsecutiveFailures = 12 // 60 seconds of failures before giving up

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			if !t.client.IsConnected() {
				consecutiveFailures++
				log.Printf("Connection lost (failure %d/%d), attempting to reconnect...",
					consecutiveFailures, maxConsecutiveFailures)

				if err := t.reconnect(); err != nil {
					log.Printf("Reconnection failed: %v", err)

					// Check if we should give up
					if consecutiveFailures >= maxConsecutiveFailures {
						log.Printf("Max consecutive failures reached, triggering shutdown")
						t.cancel()
						return
					}

					// Continue monitoring
					continue
				}

				// Successfully reconnected
				consecutiveFailures = 0
				log.Printf("Connection restored successfully")
			} else {
				// Connection is healthy
				if consecutiveFailures > 0 {
					consecutiveFailures = 0
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
