package proxy

import (
	"fmt"
	"log"
	"math"
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

			// Check if we've exceeded max retries
			if t.config.MaxRetries > 0 && attempt >= t.config.MaxRetries {
				return fmt.Errorf("max reconnection attempts (%d) exceeded", t.config.MaxRetries)
			}

			// Wait with exponential backoff
			select {
			case <-time.After(backoff):
				// Exponential backoff with jitter
				backoff = time.Duration(float64(backoff) * 2)
				if backoff > t.config.MaxBackoff {
					backoff = t.config.MaxBackoff
				}
			case <-t.ctx.Done():
				return fmt.Errorf("reconnection cancelled")
			}
			continue
		}

		// Successfully reconnected - try to rejoin channel
		if err := t.joinChannel(); err != nil {
			log.Printf("Failed to rejoin channel: %v", err)
			t.client.Disconnect()

			// Wait before retrying
			select {
			case <-time.After(backoff):
			case <-t.ctx.Done():
				return fmt.Errorf("reconnection cancelled")
			}
			continue
		}

		log.Printf("Successfully reconnected and rejoined channel after %d attempts", attempt)
		return nil
	}
}

// monitorConnection monitors the WebSocket connection and reconnects if needed
func (t *Telephone) monitorConnection() {
	defer t.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			if !t.client.IsConnected() {
				log.Println("Connection lost, attempting to reconnect...")

				if err := t.reconnect(); err != nil {
					log.Printf("Reconnection failed permanently: %v", err)
					// Trigger shutdown
					t.cancel()
					return
				}
			}
		}
	}
}

// calculateBackoff calculates exponential backoff with jitter
func calculateBackoff(attempt int, initial, max time.Duration) time.Duration {
	backoff := time.Duration(float64(initial) * math.Pow(2, float64(attempt)))
	if backoff > max {
		backoff = max
	}

	// Add up to 10% jitter to prevent thundering herd
	jitter := time.Duration(float64(backoff) * 0.1 * (0.5 - (float64(time.Now().UnixNano()%100) / 100.0)))
	return backoff + jitter
}
