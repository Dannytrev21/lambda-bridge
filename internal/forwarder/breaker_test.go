package forwarder

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewWindowedCircuitBreaker(t *testing.T) {
	breaker := NewWindowedCircuitBreaker(5, 30*time.Second, 30*time.Second)

	assert.NotNil(t, breaker, "Should create circuit breaker")
	assert.Equal(t, 5, breaker.threshold)
	assert.Equal(t, 30*time.Second, breaker.window)
	assert.Equal(t, 30*time.Second, breaker.cooldown)
}

func TestNewWindowedCircuitBreaker_Defaults(t *testing.T) {
	breaker := NewWindowedCircuitBreaker(-1, 0, 0)

	assert.Equal(t, 5, breaker.threshold, "Negative threshold should default to 5")
	assert.Equal(t, 30*time.Second, breaker.window, "Zero window should default to 30s")
	assert.Equal(t, 30*time.Second, breaker.cooldown, "Zero cooldown should default to 30s")
}

func TestCircuitBreaker_AllowRequest_InitiallyOpen(t *testing.T) {
	breaker := NewWindowedCircuitBreaker(5, 30*time.Second, 30*time.Second)

	allowed := breaker.AllowRequest("http://example.com")

	assert.True(t, allowed, "Should allow requests initially")
}

func TestCircuitBreaker_RecordFailure(t *testing.T) {
	breaker := NewWindowedCircuitBreaker(3, 1*time.Second, 1*time.Second)
	key := "http://example.com"

	// Record failures
	breaker.RecordFailure(key)
	breaker.RecordFailure(key)

	// Should still allow requests (under threshold)
	assert.True(t, breaker.AllowRequest(key), "Should allow requests under threshold")

	// One more failure to trip the breaker
	breaker.RecordFailure(key)

	// Should now block requests
	assert.False(t, breaker.AllowRequest(key), "Should block requests when threshold exceeded")
}

func TestCircuitBreaker_RecordSuccess_ResetsFailures(t *testing.T) {
	breaker := NewWindowedCircuitBreaker(3, 30*time.Second, 30*time.Second)
	key := "http://example.com"

	// Record some failures
	breaker.RecordFailure(key)
	breaker.RecordFailure(key)

	// Record success - should reset
	breaker.RecordSuccess(key)

	// Should allow requests (failures reset)
	assert.True(t, breaker.AllowRequest(key), "Success should reset failure count")

	// Would need 3 more failures to trip
	breaker.RecordFailure(key)
	breaker.RecordFailure(key)
	assert.True(t, breaker.AllowRequest(key), "Should still allow after 2 failures post-reset")
}

func TestCircuitBreaker_WindowExpiration(t *testing.T) {
	breaker := NewWindowedCircuitBreaker(2, 100*time.Millisecond, 100*time.Millisecond)
	key := "http://example.com"

	// Record failures
	breaker.RecordFailure(key)
	breaker.RecordFailure(key)

	// Breaker should be open
	assert.False(t, breaker.AllowRequest(key), "Should block requests")

	// Wait for window to expire
	time.Sleep(150 * time.Millisecond)

	// Should allow again (failure count reset by window expiration)
	assert.True(t, breaker.AllowRequest(key), "Should allow after window expiration")
}

func TestCircuitBreaker_CooldownPeriod(t *testing.T) {
	breaker := NewWindowedCircuitBreaker(2, 1*time.Second, 200*time.Millisecond)
	key := "http://example.com"

	// Trip the breaker
	breaker.RecordFailure(key)
	breaker.RecordFailure(key)

	assert.False(t, breaker.AllowRequest(key), "Should block requests")

	// Wait for cooldown
	time.Sleep(250 * time.Millisecond)

	// Should allow again after cooldown
	assert.True(t, breaker.AllowRequest(key), "Should allow after cooldown period")
}

func TestCircuitBreaker_MultipleKeys(t *testing.T) {
	breaker := NewWindowedCircuitBreaker(2, 30*time.Second, 30*time.Second)
	key1 := "http://example1.com"
	key2 := "http://example2.com"

	// Trip breaker for key1
	breaker.RecordFailure(key1)
	breaker.RecordFailure(key1)

	// key1 should be blocked
	assert.False(t, breaker.AllowRequest(key1), "Should block key1")

	// key2 should still be allowed
	assert.True(t, breaker.AllowRequest(key2), "Should allow key2")
}

func TestCircuitBreaker_GetStats(t *testing.T) {
	breaker := NewWindowedCircuitBreaker(3, 30*time.Second, 30*time.Second)
	key := "http://example.com"

	// Record some failures
	breaker.RecordFailure(key)
	breaker.RecordFailure(key)

	stats := breaker.GetStats()

	assert.NotNil(t, stats, "Should return stats")
	assert.Contains(t, stats, key, "Stats should contain key")

	keyStats := stats[key]
	assert.Equal(t, int32(2), keyStats["fail_count"], "Should have 2 failures")
	assert.False(t, keyStats["is_open"].(bool), "Should be closed (under threshold)")
}

func TestCircuitBreaker_GetStats_OpenState(t *testing.T) {
	breaker := NewWindowedCircuitBreaker(2, 1*time.Second, 1*time.Second)
	key := "http://example.com"

	// Trip the breaker
	breaker.RecordFailure(key)
	breaker.RecordFailure(key)

	stats := breaker.GetStats()

	assert.NotNil(t, stats, "Should return stats")
	assert.Contains(t, stats, key, "Stats should contain key")

	keyStats := stats[key]
	assert.True(t, keyStats["is_open"].(bool), "Should be open")
	assert.NotNil(t, keyStats["opened_until"], "Should have opened_until time")
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	breaker := NewWindowedCircuitBreaker(100, 30*time.Second, 30*time.Second)
	key := "http://example.com"

	done := make(chan bool, 20)

	// Concurrent failures
	for i := 0; i < 10; i++ {
		go func() {
			breaker.RecordFailure(key)
			done <- true
		}()
	}

	// Concurrent successes
	for i := 0; i < 10; i++ {
		go func() {
			breaker.RecordSuccess(key)
			done <- true
		}()
	}

	// Wait for all to complete
	for i := 0; i < 20; i++ {
		<-done
	}

	// Should not panic with concurrent access
	stats := breaker.GetStats()
	assert.NotNil(t, stats, "Should handle concurrent access")
}

func TestCircuitBreaker_HalfOpenState(t *testing.T) {
	breaker := NewWindowedCircuitBreaker(2, 1*time.Second, 200*time.Millisecond)
	key := "http://example.com"

	// Trip the breaker
	breaker.RecordFailure(key)
	breaker.RecordFailure(key)
	assert.False(t, breaker.AllowRequest(key), "Should be open")

	// Wait for cooldown to enter half-open
	time.Sleep(250 * time.Millisecond)

	// First request should be allowed (half-open)
	assert.True(t, breaker.AllowRequest(key), "Should allow first request in half-open")

	// If it succeeds, circuit should close
	breaker.RecordSuccess(key)

	// Should now allow all requests
	assert.True(t, breaker.AllowRequest(key), "Should be closed after successful half-open request")
}
