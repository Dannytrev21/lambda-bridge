package handler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockWorkerPool is a mock for testing
type MockWorkerPool struct {
	mock.Mock
}

func (m *MockWorkerPool) Submit(job ALBJob) bool {
	args := m.Called(job)
	return args.Bool(0)
}

func (m *MockWorkerPool) Shutdown() {
	m.Called()
}

func (m *MockWorkerPool) Stats() (queueDepth int, workers int) {
	args := m.Called()
	return args.Int(0), args.Int(1)
}

func (m *MockWorkerPool) GetHealthScore() float64 {
	args := m.Called()
	return args.Get(0).(float64)
}

func (m *MockWorkerPool) GetMetrics() PoolMetrics {
	args := m.Called()
	return args.Get(0).(PoolMetrics)
}

// MockWorkerPoolFactory is a mock factory
type MockWorkerPoolFactory struct {
	mock.Mock
}

func (m *MockWorkerPoolFactory) CreatePool(url string, queueSize int) WorkerPool {
	args := m.Called(url, queueSize)
	return args.Get(0).(WorkerPool)
}

func TestWorkerPoolManager_Submit(t *testing.T) {
	mockForwarder := new(MockWebhookForwarder)
	manager := NewWorkerPoolManager(mockForwarder)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	payload := json.RawMessage(`{"test":"data"}`)
	route := "default"
	urls := []string{"http://example.com/webhook1", "http://example.com/webhook2"}

	dropped := manager.Submit(ctx, payload, route, urls)

	// Allow time for pools to be created
	time.Sleep(100 * time.Millisecond)

	assert.Empty(t, dropped, "Should not drop any URLs with fresh pools")
	assert.Equal(t, 2, len(manager.pools), "Should create 2 pools")

	// Cleanup
	manager.Shutdown()
}

func TestWorkerPoolManager_SubmitEmptyURL(t *testing.T) {
	mockForwarder := new(MockWebhookForwarder)
	manager := NewWorkerPoolManager(mockForwarder)
	defer manager.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	payload := json.RawMessage(`{"test":"data"}`)
	route := "default"
	urls := []string{"", "http://example.com/webhook"}

	dropped := manager.Submit(ctx, payload, route, urls)
	time.Sleep(50 * time.Millisecond)

	assert.Empty(t, dropped, "Should not drop valid URL")
	assert.Equal(t, 1, len(manager.pools), "Should create only 1 pool (empty URL skipped)")
}

func TestWorkerPoolManager_ReuseExistingPool(t *testing.T) {
	mockForwarder := new(MockWebhookForwarder)
	manager := NewWorkerPoolManager(mockForwarder)
	defer manager.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	payload := json.RawMessage(`{"test":"data"}`)
	route := "default"
	url := "http://example.com/webhook"

	// First submission
	manager.Submit(ctx, payload, route, []string{url})
	time.Sleep(50 * time.Millisecond)
	pool1 := manager.pools[url]

	// Second submission to same URL
	manager.Submit(ctx, payload, route, []string{url})
	pool2 := manager.pools[url]

	assert.Same(t, pool1, pool2, "Should reuse existing pool for same URL")
	assert.Equal(t, 1, len(manager.pools), "Should have only 1 pool")
}

func TestWorkerPoolManager_Shutdown(t *testing.T) {
	mockForwarder := new(MockWebhookForwarder)
	manager := NewWorkerPoolManager(mockForwarder)

	// Create some pools
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	payload := json.RawMessage(`{"test":"data"}`)
	urls := []string{"http://example.com/webhook1", "http://example.com/webhook2"}
	manager.Submit(ctx, payload, "default", urls)
	time.Sleep(100 * time.Millisecond)

	// Shutdown should not panic
	manager.Shutdown()

	// Pools should still exist but be shutdown
	assert.Equal(t, 2, len(manager.pools), "Pools should still exist after shutdown")
}

func TestWorkerPoolManager_GetStats(t *testing.T) {
	mockForwarder := new(MockWebhookForwarder)
	manager := NewWorkerPoolManager(mockForwarder)
	defer manager.Shutdown()

	// Create a pool
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	payload := json.RawMessage(`{"test":"data"}`)
	url := "http://example.com/webhook"
	manager.Submit(ctx, payload, "default", []string{url})

	// Wait a moment for pool to initialize
	time.Sleep(200 * time.Millisecond)

	stats := manager.GetStats()

	assert.NotNil(t, stats, "Stats should not be nil")
	assert.Contains(t, stats, url, "Stats should contain the URL")
	assert.NotNil(t, stats[url], "URL stats should not be nil")
}

func TestWorkerPoolManager_ConcurrentSubmit(t *testing.T) {
	mockForwarder := new(MockWebhookForwarder)
	manager := NewWorkerPoolManager(mockForwarder)
	defer manager.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	payload := json.RawMessage(`{"test":"data"}`)
	url := "http://example.com/webhook"

	// Submit concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			manager.Submit(ctx, payload, "default", []string{url})
			done <- true
		}()
	}

	// Wait for all to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 1, len(manager.pools), "Should have only 1 pool despite concurrent submissions")
}

func TestWorkerPoolFactory_CreatePool(t *testing.T) {
	mockForwarder := new(MockWebhookForwarder)
	factory := NewDefaultWorkerPoolFactory(mockForwarder)

	pool := factory.CreatePool("http://example.com/webhook", 100)

	assert.NotNil(t, pool, "Factory should create a pool")
	assert.IsType(t, &BotWorkerPool{}, pool, "Factory should create BotWorkerPool")
}
