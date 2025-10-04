package handler

import (
	"context"
	"encoding/json"
	"log"
	"sync"

	"github.com/Dannytrev21/lambda-bridge/internal/constants"
	"github.com/Dannytrev21/lambda-bridge/internal/forwarder"
)

type WorkerPoolManager struct {
	factory WorkerPoolFactory
	pools   map[string]WorkerPool
	mu      sync.RWMutex
}

func NewWorkerPoolManager(f forwarder.WebhookForwarderInterface) *WorkerPoolManager {
	return &WorkerPoolManager{
		factory: NewDefaultWorkerPoolFactory(f),
		pools:   make(map[string]WorkerPool),
	}
}

// Submit submits jobs to worker pools for the given URLs.
// The payload is treated as immutable and shared across all workers.
// DO NOT modify the payload after calling this function.
func (m *WorkerPoolManager) Submit(ctx context.Context, payload json.RawMessage, route string, urls []string) []string {
	var dropped []string
	for _, url := range urls {
		if url == "" {
			continue
		}
		pool := m.getOrCreatePool(url)
		job := ALBJob{
			ctx:     ctx,
			payload: payload, // Share immutable payload, no cloning
			route:   route,
			url:     url,
		}
		if !pool.Submit(job) {
			log.Printf("worker queue drop for url=%s", url)
			dropped = append(dropped, url)
		}
	}
	return dropped
}

func (m *WorkerPoolManager) getOrCreatePool(url string) WorkerPool {
	m.mu.RLock()
	if pool, ok := m.pools[url]; ok {
		m.mu.RUnlock()
		return pool
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	if pool, ok := m.pools[url]; ok {
		return pool
	}
	pool := m.factory.CreatePool(url, constants.DefaultQueueSize)
	m.pools[url] = pool
	return pool
}

func (m *WorkerPoolManager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, pool := range m.pools {
		pool.Shutdown()
	}
}

func (m *WorkerPoolManager) GetStats() map[string]map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]map[string]interface{})
	for url, pool := range m.pools {
		metrics := pool.GetMetrics()
		stats[url] = map[string]interface{}{
			"workers":         metrics.Workers,
			"queue_depth":     metrics.QueueDepth,
			"health_score":    metrics.HealthScore,
			"is_healthy":      metrics.IsHealthy,
			"successes":       metrics.Successes,
			"failures":        metrics.Failures,
			"skipped_timeout": metrics.SkippedTimeout,
		}
	}
	return stats
}
