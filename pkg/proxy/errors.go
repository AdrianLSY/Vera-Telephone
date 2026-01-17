package proxy

import "errors"

// Sentinel errors for proxy operations
var (
	// ErrReconnectionInProgress is returned when a reconnection attempt
	// is already underway
	ErrReconnectionInProgress = errors.New("reconnection already in progress")

	// ErrReconnectionCancelled is returned when reconnection is cancelled
	// due to context cancellation
	ErrReconnectionCancelled = errors.New("reconnection cancelled")

	// ErrReconnectionCancelledDuringBackoff is returned when reconnection is
	// cancelled while waiting for backoff
	ErrReconnectionCancelledDuringBackoff = errors.New("reconnection cancelled during backoff")

	// ErrReconnectionCancelledDuringChannelJoin is returned when reconnection
	// is cancelled while attempting to rejoin the channel
	ErrReconnectionCancelledDuringChannelJoin = errors.New("reconnection cancelled during channel join retry")

	// ErrMaxRetriesExceeded is returned when maximum reconnection attempts
	// have been exhausted
	ErrMaxRetriesExceeded = errors.New("maximum reconnection attempts exceeded")
)
