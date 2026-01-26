package proxy

import "errors"

// Sentinel errors for proxy operations.
var (
	// ErrReconnectionInProgress is returned when a reconnection attempt
	// is already underway.
	ErrReconnectionInProgress = errors.New("reconnection already in progress")

	// ErrReconnectionCancelled is returned when reconnection is canceled
	// due to context cancellation.
	ErrReconnectionCancelled = errors.New("reconnection canceled")

	// ErrReconnectionCancelledDuringBackoff is returned when reconnection is
	// canceled while waiting for backoff.
	ErrReconnectionCancelledDuringBackoff = errors.New("reconnection canceled during backoff")

	// ErrReconnectionCancelledDuringChannelJoin is returned when reconnection
	// is canceled while attempting to rejoin the channel.
	ErrReconnectionCancelledDuringChannelJoin = errors.New("reconnection canceled during channel join retry")

	// ErrMaxRetriesExceeded is returned when maximum reconnection attempts
	// have been exhausted.
	ErrMaxRetriesExceeded = errors.New("maximum reconnection attempts exceeded")
)
