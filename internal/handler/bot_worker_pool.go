package handler

import (
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Dannytrev21/lambda-bridge/internal/constants"
	"github.com/Dannytrev21/lambda-bridge/internal/forwarder"
)

type BotWorkerPool struct {
	url             string
	queue           chan ALBJob
	forwarder       forwarder.WebhookForwarderInterface
	metrics         *BotMetrics
	shutdownChan    chan struct{}
	scaleDownSignal chan struct{}
	wg              sync.WaitGroup

	minWorkers    int
	maxWorkers    int
	targetWorkers atomic.Int32 // Target worker count for deterministic scaling

	workers atomic.Int32
	healthy atomic.Bool
}

type BotMetrics struct {
	successCount      atomic.Uint64
	failureCount      atomic.Uint64
	totalDuration     atomic.Uint64
	queueDepth        atomic.Int32
	skippedByTimeout  atomic.Uint64

	lastSuccess atomic.Value // time.Time
	lastFailure atomic.Value // time.Time
}

func NewBotWorkerPool(url string, forwarder forwarder.WebhookForwarderInterface, queueSize int) *BotWorkerPool {
	if queueSize <= 0 {
		queueSize = constants.DefaultQueueSize
	}

	pool := &BotWorkerPool{
		url:             url,
		queue:           make(chan ALBJob, queueSize),
		forwarder:       forwarder,
		metrics:         &BotMetrics{},
		shutdownChan:    make(chan struct{}),
		scaleDownSignal: make(chan struct{}, constants.DefaultWorkerMax),
		minWorkers:      constants.DefaultWorkerMin,
		maxWorkers:      constants.DefaultWorkerMax,
	}

	pool.metrics.lastSuccess.Store(time.Time{})
	pool.metrics.lastFailure.Store(time.Time{})

	initialWorkers := 5
	pool.workers.Store(int32(initialWorkers))
	pool.targetWorkers.Store(int32(initialWorkers))
	for i := 0; i < initialWorkers; i++ {
		pool.wg.Add(1)
		go pool.worker()
	}

	pool.wg.Add(1)
	go pool.autoScale()

	pool.healthy.Store(true)
	return pool
}

func (p *BotWorkerPool) worker() {
	defer p.wg.Done()
	for {
		// Check if we're above target and should self-terminate
		current := p.workers.Load()
		target := p.targetWorkers.Load()
		if current > target && current > int32(p.minWorkers) {
			p.workers.Add(-1)
			return
		}

		select {
		case job := <-p.queue:
			p.metrics.queueDepth.Add(-1)

			// Check time budget before processing
			if deadline, ok := job.ctx.Deadline(); ok {
				remaining := time.Until(deadline)
				if remaining < constants.MinimumLambdaTimeRemaining {
					p.metrics.skippedByTimeout.Add(1)
					log.Printf("Skipping job for %s: insufficient time remaining=%v", p.url, remaining)
					continue
				}
			}

			p.processJob(job)
		case <-p.scaleDownSignal:
			if current := p.workers.Load(); current > int32(p.minWorkers) {
				p.workers.Add(-1)
				return
			}
		case <-p.shutdownChan:
			return
		}
	}
}

func (p *BotWorkerPool) processJob(job ALBJob) {
	start := time.Now()

	results := p.forwarder.ForwardToWebhooks(job.ctx, []string{p.url}, job.payload)
	duration := time.Since(start)

	for _, result := range results {
		if result.Error != nil {
			failCount := p.metrics.failureCount.Add(1)
			p.metrics.lastFailure.Store(time.Now())

			if errors.Is(result.Error, forwarder.ErrCircuitOpen) {
				log.Printf("Circuit breaker open for bot %s", p.url)
			}

			// Mark unhealthy only if consecutive failures exceed threshold
			if failCount > 10 {
				p.healthy.Store(false)
			}
		} else {
			p.metrics.successCount.Add(1)
			p.metrics.totalDuration.Add(uint64(duration.Microseconds()))
			p.metrics.lastSuccess.Store(time.Now())

			// Reset failure count on success and mark healthy
			p.metrics.failureCount.Store(0)
			p.healthy.Store(true)
		}
	}
}

func (p *BotWorkerPool) autoScale() {
	defer p.wg.Done()
	ticker := time.NewTicker(constants.AutoScaleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.evaluateScaling()
		case <-p.shutdownChan:
			return
		}
	}
}

func (p *BotWorkerPool) evaluateScaling() {
	queueDepth := len(p.queue)
	queueCap := cap(p.queue)
	utilization := float64(queueDepth) / float64(queueCap)
	currentWorkers := p.workers.Load()

	if utilization > constants.DefaultWorkerUtilizationScaleUp && int(currentWorkers) < p.maxWorkers {
		workersToAdd := min(p.maxWorkers-int(currentWorkers), 5)
		newTarget := int32(int(currentWorkers) + workersToAdd)
		p.targetWorkers.Store(newTarget)

		for i := 0; i < workersToAdd; i++ {
			p.workers.Add(1)
			p.wg.Add(1)
			go p.worker()
		}
		log.Printf("Scaled up bot pool %s to %d workers, target=%d (utilization %.2f)",
			p.url, p.workers.Load(), newTarget, utilization)
	}

	if utilization < constants.DefaultWorkerUtilizationScaleDown && int(currentWorkers) > p.minWorkers {
		workersToRemove := min(int(currentWorkers)-p.minWorkers, 2)
		newTarget := int32(int(currentWorkers) - workersToRemove)
		if newTarget < int32(p.minWorkers) {
			newTarget = int32(p.minWorkers)
		}
		p.targetWorkers.Store(newTarget)

		// Workers will self-terminate when they check targetWorkers
		log.Printf("Scaling down bot pool %s target=%d (utilization %.2f)", p.url, newTarget, utilization)
	}
}

func (p *BotWorkerPool) Submit(job ALBJob) bool {
	// Check time budget before queuing
	if deadline, ok := job.ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		// Need at least 3 seconds: queue wait + processing + margin
		if remaining < 3*time.Second {
			p.metrics.skippedByTimeout.Add(1)
			log.Printf("Rejecting job for %s at submission: insufficient time remaining=%v", p.url, remaining)
			return false
		}
	}

	select {
	case p.queue <- job:
		p.metrics.queueDepth.Add(1)
		return true
	default:
		return false
	}
}

func (p *BotWorkerPool) Shutdown() {
	// Set target to 0 to signal all workers to terminate
	p.targetWorkers.Store(0)
	close(p.shutdownChan)
	p.wg.Wait()
}

func (p *BotWorkerPool) Stats() (queueDepth int, workers int) {
	return len(p.queue), int(p.workers.Load())
}

func (p *BotWorkerPool) GetHealthScore() float64 {
	total := p.metrics.successCount.Load() + p.metrics.failureCount.Load()
	if total == 0 {
		return 1.0
	}
	return float64(p.metrics.successCount.Load()) / float64(total)
}

func (p *BotWorkerPool) GetMetrics() PoolMetrics {
	return PoolMetrics{
		Workers:        p.workers.Load(),
		QueueDepth:     p.metrics.queueDepth.Load(),
		HealthScore:    p.GetHealthScore(),
		IsHealthy:      p.healthy.Load(),
		Successes:      p.metrics.successCount.Load(),
		Failures:       p.metrics.failureCount.Load(),
		SkippedTimeout: p.metrics.skippedByTimeout.Load(),
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
