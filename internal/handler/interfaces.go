package handler

// WorkerPool defines the interface for worker pool operations.
type WorkerPool interface {
	// Submit submits a job to the worker pool
	Submit(job ALBJob) bool
	// Shutdown gracefully shuts down the worker pool
	Shutdown()
	// Stats returns queue depth and worker count
	Stats() (queueDepth int, workers int)
	// GetHealthScore returns the health score of the pool
	GetHealthScore() float64
	// GetMetrics returns detailed metrics for the pool
	GetMetrics() PoolMetrics
}

// PoolMetrics contains metrics data for a worker pool.
type PoolMetrics struct {
	Workers        int32
	QueueDepth     int32
	HealthScore    float64
	IsHealthy      bool
	Successes      uint64
	Failures       uint64
	SkippedTimeout uint64
}

// WorkerPoolFactory creates worker pool instances.
type WorkerPoolFactory interface {
	// CreatePool creates a new worker pool for the given URL
	CreatePool(url string, queueSize int) WorkerPool
}
