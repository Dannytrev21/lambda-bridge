package forwarder

import (
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// CircuitBreaker defines the interface for circuit breaker behavior.
type CircuitBreaker interface {
	// AllowRequest checks if a request should be allowed through
	AllowRequest(key string) bool
	// RecordSuccess records a successful request
	RecordSuccess(key string)
	// RecordFailure records a failed request
	RecordFailure(key string)
}

// ErrCircuitBreakerOpen is now defined in errors.go to avoid circular dependencies

// breakerEntry represents the state for a single circuit breaker instance.
type breakerEntry struct {
	failCount   atomic.Int32
	lastFailure atomic.Value // time.Time
	openedUntil atomic.Value // time.Time
}

// WindowedCircuitBreaker implements a time-window based circuit breaker.
type WindowedCircuitBreaker struct {
	threshold      int
	window         time.Duration
	cooldown       time.Duration
	breakers       map[string]*breakerEntry
	mu             sync.Mutex
}

// NewWindowedCircuitBreaker creates a new windowed circuit breaker.
func NewWindowedCircuitBreaker(threshold int, window, cooldown time.Duration) *WindowedCircuitBreaker {
	if threshold <= 0 {
		threshold = 5
	}
	if window <= 0 {
		window = 30 * time.Second
	}
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}

	return &WindowedCircuitBreaker{
		threshold: threshold,
		window:    window,
		cooldown:  cooldown,
		breakers:  make(map[string]*breakerEntry),
	}
}

// AllowRequest checks if a request should be allowed for the given key.
func (cb *WindowedCircuitBreaker) AllowRequest(key string) bool {
	if key == "" {
		return true
	}

	now := time.Now()
	cb.mu.Lock()
	entry := cb.ensureBreaker(key)
	cb.mu.Unlock()

	// Check if circuit is open
	openedUntil := entry.openedUntil.Load().(time.Time)
	if openedUntil.After(now) {
		return false
	}

	// Reset fail count if outside window
	lastFailure := entry.lastFailure.Load().(time.Time)
	if !lastFailure.IsZero() && now.Sub(lastFailure) > cb.window {
		entry.failCount.Store(0)
	}
	entry.openedUntil.Store(time.Time{})
	return true
}

// RecordSuccess records a successful request and resets the failure count.
func (cb *WindowedCircuitBreaker) RecordSuccess(key string) {
	if key == "" {
		return
	}

	cb.mu.Lock()
	entry := cb.ensureBreaker(key)
	cb.mu.Unlock()

	// Reset all failure tracking on success
	entry.failCount.Store(0)
	entry.lastFailure.Store(time.Time{})
	entry.openedUntil.Store(time.Time{})
}

// RecordFailure records a failed request and opens the circuit if threshold is reached.
func (cb *WindowedCircuitBreaker) RecordFailure(key string) {
	if key == "" {
		return
	}

	now := time.Now()
	cb.mu.Lock()
	entry := cb.ensureBreaker(key)
	cb.mu.Unlock()

	// Check if already open
	openedUntil := entry.openedUntil.Load().(time.Time)
	if openedUntil.After(now) {
		return
	}

	// Reset fail count if outside window
	lastFailure := entry.lastFailure.Load().(time.Time)
	if !lastFailure.IsZero() && now.Sub(lastFailure) > cb.window {
		entry.failCount.Store(0)
	}

	// Atomically increment fail count and check threshold
	newFailCount := entry.failCount.Add(1)
	entry.lastFailure.Store(now)

	if newFailCount >= int32(cb.threshold) {
		entry.failCount.Store(0)
		cooldownUntil := now.Add(cb.cooldown)
		entry.openedUntil.Store(cooldownUntil)
		log.Printf("Circuit breaker opened for %s until %s (failures=%d, threshold=%d)",
			key, cooldownUntil.Format(time.RFC3339), newFailCount, cb.threshold)
	}
}

// ensureBreaker gets or creates a breaker entry for the given key.
// Must be called with cb.mu held.
func (cb *WindowedCircuitBreaker) ensureBreaker(key string) *breakerEntry {
	if entry, ok := cb.breakers[key]; ok {
		return entry
	}
	entry := &breakerEntry{}
	entry.lastFailure.Store(time.Time{})
	entry.openedUntil.Store(time.Time{})
	cb.breakers[key] = entry
	return entry
}

// GetStats returns statistics for all circuit breakers.
func (cb *WindowedCircuitBreaker) GetStats() map[string]map[string]interface{} {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	stats := make(map[string]map[string]interface{})
	now := time.Now()

	for key, entry := range cb.breakers {
		openedUntil := entry.openedUntil.Load().(time.Time)
		isOpen := openedUntil.After(now)

		stats[key] = map[string]interface{}{
			"fail_count": entry.failCount.Load(),
			"is_open":    isOpen,
			"opened_until": openedUntil,
		}
	}

	return stats
}
