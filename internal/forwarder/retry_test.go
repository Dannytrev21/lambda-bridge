package forwarder

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestExponentialBackoffRetry_ShouldRetry(t *testing.T) {
	retry := NewExponentialBackoffRetry(3, 100*time.Millisecond, 5*time.Second, false)

	tests := []struct {
		name       string
		attempt    int
		statusCode int
		err        error
		expected   bool
	}{
		{
			name:       "should retry on server error",
			attempt:    0,
			statusCode: http.StatusInternalServerError,
			err:        nil,
			expected:   true,
		},
		{
			name:       "should not retry on client error",
			attempt:    0,
			statusCode: http.StatusBadRequest,
			err:        nil,
			expected:   false,
		},
		{
			name:       "should not retry when max attempts reached",
			attempt:    3,
			statusCode: http.StatusInternalServerError,
			err:        nil,
			expected:   false,
		},
		{
			name:       "should retry on timeout error",
			attempt:    0,
			statusCode: 0,
			err:        ErrTimeout,
			expected:   true,
		},
		{
			name:       "should not retry on circuit breaker error",
			attempt:    0,
			statusCode: 0,
			err:        ErrCircuitBreakerOpen,
			expected:   false,
		},
		{
			name:       "should not retry on success",
			attempt:    0,
			statusCode: http.StatusOK,
			err:        nil,
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := retry.ShouldRetry(tt.attempt, tt.statusCode, tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExponentialBackoffRetry_NextDelay(t *testing.T) {
	retry := NewExponentialBackoffRetry(3, 100*time.Millisecond, 5*time.Second, false)

	tests := []struct {
		name     string
		attempt  int
		expected time.Duration
	}{
		{
			name:     "no delay for attempt 0",
			attempt:  0,
			expected: 0,
		},
		{
			name:     "base delay for attempt 1",
			attempt:  1,
			expected: 100 * time.Millisecond,
		},
		{
			name:     "doubled delay for attempt 2",
			attempt:  2,
			expected: 200 * time.Millisecond,
		},
		{
			name:     "quadrupled delay for attempt 3",
			attempt:  3,
			expected: 400 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := retry.NextDelay(tt.attempt)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExponentialBackoffRetry_NextDelayWithJitter(t *testing.T) {
	retry := NewExponentialBackoffRetry(3, 100*time.Millisecond, 5*time.Second, true)

	delay1 := retry.NextDelay(1)
	delay2 := retry.NextDelay(1)

	// With jitter, delays should vary but be in reasonable range
	assert.GreaterOrEqual(t, delay1, 75*time.Millisecond, "Delay should be at least base - 25%")
	assert.LessOrEqual(t, delay1, 125*time.Millisecond, "Delay should be at most base + 25%")

	// Delays might be different due to jitter
	// But we can't guarantee they're different in every run
	_ = delay2
}

func TestExponentialBackoffRetry_MaxDelay(t *testing.T) {
	retry := NewExponentialBackoffRetry(10, 100*time.Millisecond, 500*time.Millisecond, false)

	// Attempt 5 would be 100ms * 2^4 = 1600ms without max cap
	delay := retry.NextDelay(5)

	assert.Equal(t, 500*time.Millisecond, delay, "Delay should be capped at MaxDelay")
}

func TestExponentialBackoffRetry_WaitForRetry(t *testing.T) {
	retry := NewExponentialBackoffRetry(3, 10*time.Millisecond, 100*time.Millisecond, false)

	ctx := context.Background()

	start := time.Now()
	err := retry.WaitForRetry(ctx, 1)
	duration := time.Since(start)

	assert.NoError(t, err, "Should not error on successful wait")
	assert.GreaterOrEqual(t, duration, 10*time.Millisecond, "Should wait for at least base delay")
}

func TestExponentialBackoffRetry_WaitForRetryContextCancelled(t *testing.T) {
	retry := NewExponentialBackoffRetry(3, 1*time.Second, 5*time.Second, false)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := retry.WaitForRetry(ctx, 1)

	assert.Error(t, err, "Should error when context is cancelled")
	assert.Equal(t, context.Canceled, err, "Error should be context.Canceled")
}

func TestExponentialBackoffRetry_HasTimeForRetry(t *testing.T) {
	retry := NewExponentialBackoffRetry(3, 100*time.Millisecond, 5*time.Second, false)

	tests := []struct {
		name            string
		timeout         time.Duration
		minTimeRequired time.Duration
		expected        bool
	}{
		{
			name:            "has time",
			timeout:         10 * time.Second,
			minTimeRequired: 1 * time.Second,
			expected:        true,
		},
		{
			name:            "not enough time",
			timeout:         100 * time.Millisecond,
			minTimeRequired: 5 * time.Second,
			expected:        false,
		},
		{
			name:            "no deadline",
			timeout:         0, // No timeout = no deadline
			minTimeRequired: 5 * time.Second,
			expected:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ctx context.Context
			var cancel context.CancelFunc

			if tt.timeout > 0 {
				ctx, cancel = context.WithTimeout(context.Background(), tt.timeout)
				defer cancel()
			} else {
				ctx = context.Background()
			}

			result := retry.HasTimeForRetry(ctx, tt.minTimeRequired)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewExponentialBackoffRetry_Defaults(t *testing.T) {
	retry := NewExponentialBackoffRetry(-1, 0, 0, false)

	assert.Equal(t, 0, retry.MaxRetries, "Negative max retries should default to 0")
	assert.Equal(t, 100*time.Millisecond, retry.BaseDelay, "Zero base delay should default to 100ms")
	assert.Equal(t, 5*time.Second, retry.MaxDelay, "Zero max delay should default to 5s")
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error not retryable",
			err:      nil,
			expected: false,
		},
		{
			name:     "circuit breaker open not retryable",
			err:      ErrCircuitBreakerOpen,
			expected: false,
		},
		{
			name:     "timeout error retryable",
			err:      ErrTimeout,
			expected: true,
		},
		{
			name:     "server error retryable",
			err:      NewHTTPError(500, "http://example.com", "req-123"),
			expected: true,
		},
		{
			name:     "client error not retryable",
			err:      NewHTTPError(400, "http://example.com", "req-123"),
			expected: false,
		},
		{
			name:     "unknown error retryable by default",
			err:      errors.New("unknown error"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsRetryable(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}
