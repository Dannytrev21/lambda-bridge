package forwarder

import (
	"context"
	"math/rand"
	"time"
)

// RetryStrategy defines the interface for retry behavior.
type RetryStrategy interface {
	// ShouldRetry determines if a request should be retried based on attempt number and error
	ShouldRetry(attempt int, statusCode int, err error) bool
	// NextDelay calculates the delay before the next retry attempt
	NextDelay(attempt int) time.Duration
}

// ExponentialBackoffRetry implements exponential backoff with jitter.
type ExponentialBackoffRetry struct {
	MaxRetries     int
	BaseDelay      time.Duration
	MaxDelay       time.Duration
	EnableJitter   bool
}

// NewExponentialBackoffRetry creates a new exponential backoff retry strategy.
func NewExponentialBackoffRetry(maxRetries int, baseDelay, maxDelay time.Duration, enableJitter bool) *ExponentialBackoffRetry {
	if maxRetries < 0 {
		maxRetries = 0
	}
	if baseDelay <= 0 {
		baseDelay = 100 * time.Millisecond
	}
	if maxDelay <= 0 {
		maxDelay = 5 * time.Second
	}
	return &ExponentialBackoffRetry{
		MaxRetries:   maxRetries,
		BaseDelay:    baseDelay,
		MaxDelay:     maxDelay,
		EnableJitter: enableJitter,
	}
}

// ShouldRetry determines if the request should be retried.
// Uses errors.Is and errors.As for proper error checking.
func (r *ExponentialBackoffRetry) ShouldRetry(attempt int, statusCode int, err error) bool {
	if attempt >= r.MaxRetries {
		return false
	}

	// If no error and status is 2xx or 3xx, don't retry
	if err == nil && statusCode < 400 {
		return false
	}

	// Use the IsRetryable helper for proper error classification
	if err != nil {
		return IsRetryable(err)
	}

	// Don't retry client errors (4xx)
	if statusCode >= 400 && statusCode < 500 {
		return false
	}

	// Retry on server errors (5xx)
	return statusCode >= 500
}

// NextDelay calculates the delay before the next retry with exponential backoff and optional jitter.
func (r *ExponentialBackoffRetry) NextDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}

	// Exponential backoff: baseDelay * 2^(attempt-1)
	delay := r.BaseDelay * time.Duration(1<<uint(attempt-1))
	if delay > r.MaxDelay {
		delay = r.MaxDelay
	}

	// Add jitter ±25%
	if r.EnableJitter && delay > 0 {
		variance := delay / 4
		if variance > 0 {
			jitter := time.Duration(rand.Int63n(int64(variance*2))) - variance
			delay += jitter
			if delay < r.BaseDelay {
				delay = r.BaseDelay
			}
		}
	}

	return delay
}

// WaitForRetry waits for the calculated delay or context cancellation.
func (r *ExponentialBackoffRetry) WaitForRetry(ctx context.Context, attempt int) error {
	delay := r.NextDelay(attempt)
	if delay == 0 {
		return nil
	}

	select {
	case <-time.After(delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// HasTimeForRetry checks if there's enough time remaining for another retry attempt.
func (r *ExponentialBackoffRetry) HasTimeForRetry(ctx context.Context, minTimeRequired time.Duration) bool {
	deadline, ok := ctx.Deadline()
	if !ok {
		return true
	}
	return time.Until(deadline) >= minTimeRequired
}
