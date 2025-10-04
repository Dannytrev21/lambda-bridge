package constants

import "time"

// Worker Pool Configuration
const (
	// DefaultWorkerMin is the minimum number of workers per pool
	DefaultWorkerMin = 1

	// DefaultWorkerMax is the maximum number of workers per pool
	DefaultWorkerMax = 10

	// DefaultQueueSize is the default job queue size per worker pool
	DefaultQueueSize = 100

	// DefaultWorkerUtilizationScaleUp is the utilization threshold for scaling up (70%)
	DefaultWorkerUtilizationScaleUp = 0.7

	// DefaultWorkerUtilizationScaleDown is the utilization threshold for scaling down (20%)
	DefaultWorkerUtilizationScaleDown = 0.2
)

// Timing and Intervals
const (
	// MetricsReportInterval is how often metrics are reported
	MetricsReportInterval = 10 * time.Second

	// AutoScaleInterval is how often worker pool auto-scaling is evaluated
	AutoScaleInterval = 5 * time.Second

	// IdleConnectionCleanupInterval is how often idle connections are cleaned up
	IdleConnectionCleanupInterval = 60 * time.Second

	// MinimumLambdaTimeRemaining is the minimum time required to process a job
	MinimumLambdaTimeRemaining = 2 * time.Second

	// LambdaTimeMargin is the safety margin for Lambda timeout checks
	LambdaTimeMargin = 1 * time.Second
)

// HTTP Client Configuration
const (
	// DefaultWebhookTimeout is the default timeout for webhook HTTP requests
	DefaultWebhookTimeout = 10 * time.Second

	// MaxConnectionsPerHost is the maximum HTTP connections per host
	MaxConnectionsPerHost = 100

	// MaxIdleConnsPerHost is the maximum idle HTTP connections per host
	MaxIdleConnsPerHost = 100

	// MaxClientPoolSize is the maximum number of HTTP clients to cache
	MaxClientPoolSize = 100

	// ConnectionWarmTimeout is the timeout for connection warming
	ConnectionWarmTimeout = 5 * time.Second
)

// Retry Configuration
const (
	// DefaultMaxRetries is the default number of retry attempts
	DefaultMaxRetries = 3

	// DefaultBaseRetryDelay is the base delay for exponential backoff
	DefaultBaseRetryDelay = 100 * time.Millisecond

	// DefaultMaxRetryDelay is the maximum delay between retries
	DefaultMaxRetryDelay = 5 * time.Second

	// JitterVariancePercent is the percentage variance for retry jitter (25%)
	JitterVariancePercent = 0.25
)

// Circuit Breaker Configuration
const (
	// DefaultCircuitBreakerThreshold is the number of failures before opening
	DefaultCircuitBreakerThreshold = 5

	// DefaultCircuitBreakerWindow is the time window for failure counting
	DefaultCircuitBreakerWindow = 30 * time.Second

	// DefaultCircuitBreakerCooldown is how long the breaker stays open
	DefaultCircuitBreakerCooldown = 30 * time.Second
)

// HTTP Status Codes
const (
	// HTTPStatusOK is the success status code
	HTTPStatusOK = 200

	// HTTPStatusBadRequest is the client error status code
	HTTPStatusBadRequest = 400

	// HTTPStatusInternalServerError is the server error status code
	HTTPStatusInternalServerError = 500

	// HTTPStatusServiceUnavailable is returned when circuit breaker is open
	HTTPStatusServiceUnavailable = 503
)

// Overflow Processing
const (
	// MaxConcurrentOverflow is the maximum number of concurrent overflow goroutines
	MaxConcurrentOverflow = 10
)

// Health Check Paths
const (
	// HealthCheckPath is the standard health check endpoint
	HealthCheckPath = "/health"

	// HealthCheckPathZ is the alternative health check endpoint
	HealthCheckPathZ = "/healthz"

	// PingPath is the ping endpoint
	PingPath = "/ping"
)

// Header Names
const (
	// RequestIDHeader is the header name for request correlation
	RequestIDHeader = "X-Request-ID"

	// TraceIDHeader is the header name for distributed tracing
	TraceIDHeader = "X-Trace-ID"

	// UserAgentHeader is the User-Agent header name
	UserAgentHeader = "User-Agent"

	// ContentTypeHeader is the Content-Type header name
	ContentTypeHeader = "Content-Type"

	// GitHubEventHeader is the GitHub event type header
	GitHubEventHeader = "X-GitHub-Event"

	// ELBHealthCheckerUserAgent is the ALB health checker user agent
	ELBHealthCheckerUserAgent = "ELB-HealthChecker"

	// DefaultUserAgent is the default user agent for webhook requests
	DefaultUserAgent = "lambda-bridge/1.0"

	// DefaultContentType is the default content type for webhook requests
	DefaultContentType = "application/json"
)

// Shutdown Timeouts
const (
	// GracefulShutdownTimeout is the timeout for graceful shutdown
	GracefulShutdownTimeout = 5 * time.Second

	// TelemetryShutdownTimeout is the timeout for telemetry shutdown
	TelemetryShutdownTimeout = 5 * time.Second
)

// Request ID Format
const (
	// RequestIDPrefix is the prefix for generated request IDs
	RequestIDPrefix = "req-"

	// RequestIDEntropyBytes is the number of random bytes in request IDs
	RequestIDEntropyBytes = 8
)
