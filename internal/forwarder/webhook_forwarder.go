package forwarder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/Dannytrev21/lambda-bridge/internal/constants"
)

// Config captures knobs for webhook forwarding behaviour.
type Config struct {
	MaxRetries              int
	EnableJitter            bool
	BaseRetryDelay          time.Duration
	MaxRetryDelay           time.Duration
	CircuitBreakerThreshold int
	CircuitBreakerWindow    time.Duration
	CircuitBreakerCooldown  time.Duration
	WebhookTimeout          time.Duration
}

type WebhookResult struct {
	StatusCode int
	Error      error
	Duration   time.Duration
	RequestID  string
}

type WebhookForwarder struct {
	clientManager  *ClientManager
	config         Config
	retryStrategy  RetryStrategy
	circuitBreaker CircuitBreaker
	shutdownChan   chan struct{}
	shutdownOnce   sync.Once
}

// ErrCircuitOpen is deprecated: use ErrCircuitBreakerOpen directly.
var ErrCircuitOpen = ErrCircuitBreakerOpen

// WithWebhookTimeout adds a timeout to the context for webhook operations.
func WithWebhookTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = constants.DefaultWebhookTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

func NewWebhookForwarder(cfg Config) *WebhookForwarder {
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = constants.DefaultMaxRetries
	}
	if cfg.BaseRetryDelay <= 0 {
		cfg.BaseRetryDelay = constants.DefaultBaseRetryDelay
	}
	if cfg.MaxRetryDelay <= 0 {
		cfg.MaxRetryDelay = constants.DefaultMaxRetryDelay
	}
	if cfg.CircuitBreakerThreshold <= 0 {
		cfg.CircuitBreakerThreshold = constants.DefaultCircuitBreakerThreshold
	}
	if cfg.CircuitBreakerWindow <= 0 {
		cfg.CircuitBreakerWindow = constants.DefaultCircuitBreakerWindow
	}
	if cfg.CircuitBreakerCooldown <= 0 {
		cfg.CircuitBreakerCooldown = constants.DefaultCircuitBreakerCooldown
	}
	if cfg.WebhookTimeout <= 0 {
		cfg.WebhookTimeout = constants.DefaultWebhookTimeout
	}

	// Create ClientConfig based on environment
	clientConfig := DefaultClientConfig()

	// Override with environment-specific settings if needed
	if maxConns := os.Getenv("MAX_CONNECTIONS_PER_HOST"); maxConns != "" {
		if val, err := strconv.Atoi(maxConns); err == nil {
			clientConfig.MaxConnsPerHost = val
			clientConfig.MaxIdleConnsPerHost = val
		}
	}

	if os.Getenv("ENABLE_HTTP2") == "true" {
		clientConfig.EnableHTTP2 = true
	}

	if os.Getenv("DISABLE_COMPRESSION") == "true" {
		clientConfig.DisableCompression = true
	}

	// Create retry strategy
	retryStrategy := NewExponentialBackoffRetry(
		cfg.MaxRetries,
		cfg.BaseRetryDelay,
		cfg.MaxRetryDelay,
		cfg.EnableJitter,
	)

	// Create circuit breaker
	circuitBreaker := NewWindowedCircuitBreaker(
		cfg.CircuitBreakerThreshold,
		cfg.CircuitBreakerWindow,
		cfg.CircuitBreakerCooldown,
	)

	f := &WebhookForwarder{
		clientManager:  NewClientManager(clientConfig),
		config:         cfg,
		retryStrategy:  retryStrategy,
		circuitBreaker: circuitBreaker,
		shutdownChan:   make(chan struct{}),
	}

	// Start periodic idle connection cleanup
	go func() {
		ticker := time.NewTicker(constants.IdleConnectionCleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				f.clientManager.CloseIdleConnections()
			case <-f.shutdownChan:
				return
			}
		}
	}()

	return f
}

// Close gracefully shuts down the WebhookForwarder and stops background goroutines.
func (f *WebhookForwarder) Close() error {
	f.shutdownOnce.Do(func() {
		close(f.shutdownChan)
	})
	return nil
}

// ForwardToWebhooks sends the raw event to all webhook URLs in parallel
// Returns a map of URL to result for analysis
func (f *WebhookForwarder) ForwardToWebhooks(ctx context.Context, webhookURLs []string, rawEvent json.RawMessage) map[string]WebhookResult {
	results := make(map[string]WebhookResult)
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Ensure we have a request ID for tracing
	requestID := RequestIDFromContext(ctx)
	if requestID == "" {
		requestID = GenerateRequestID()
		ctx = WithRequestID(ctx, requestID)
	}

	payload := newWebhookPayload(rawEvent)

	templates := make([]preparedRequest, len(webhookURLs))
	for i, url := range webhookURLs {
		templates[i] = newPreparedRequest(ctx, url, payload)
	}

	for i, url := range webhookURLs {
		wg.Add(1)
		template := templates[i]
		go func(webhookURL string, req preparedRequest) {
			defer wg.Done()

			if !f.circuitBreaker.AllowRequest(req.urlHash) {
				result := WebhookResult{
					StatusCode: http.StatusServiceUnavailable,
					Error:      ErrCircuitOpen,
					RequestID:  requestID,
				}
				mu.Lock()
				results[webhookURL] = result
				mu.Unlock()
				return
			}

			// Create a timeout context for this webhook call
			webhookCtx, cancel := WithWebhookTimeout(ctx, f.config.WebhookTimeout)
			defer cancel()

			result := f.forwardToWebhookWithRetry(webhookCtx, req)
			result.RequestID = requestID
			if result.Error != nil {
				f.circuitBreaker.RecordFailure(req.urlHash)
			} else {
				f.circuitBreaker.RecordSuccess(req.urlHash)
			}

			mu.Lock()
			results[webhookURL] = result
			mu.Unlock()
		}(url, template)
	}

	wg.Wait()
	return results
}

func (f *WebhookForwarder) forwardToWebhookWithRetry(ctx context.Context, req preparedRequest) WebhookResult {
	for attempt := 0; attempt <= f.retryStrategy.(*ExponentialBackoffRetry).MaxRetries; attempt++ {
		// Wait for retry delay if this is a retry attempt
		if attempt > 0 {
			if err := f.retryStrategy.(*ExponentialBackoffRetry).WaitForRetry(ctx, attempt); err != nil {
				return WebhookResult{Error: err}
			}
		}

		result := f.forwardToWebhook(ctx, req)

		// Check if we should retry
		if !f.retryStrategy.ShouldRetry(attempt, result.StatusCode, result.Error) {
			return result
		}

		// Check if we have time for another retry
		if !f.retryStrategy.(*ExponentialBackoffRetry).HasTimeForRetry(ctx, 5*time.Second) {
			return result
		}

		// If this was the last attempt, return the result
		if attempt == f.retryStrategy.(*ExponentialBackoffRetry).MaxRetries {
			return result
		}
	}

	return WebhookResult{Error: fmt.Errorf("max retries exceeded")}
}

func (f *WebhookForwarder) forwardToWebhook(ctx context.Context, reqTemplate preparedRequest) WebhookResult {
	start := time.Now()
	requestID := RequestIDFromContext(ctx)

	bodyReader := bytes.NewReader(reqTemplate.body)
	req, err := http.NewRequestWithContext(ctx, reqTemplate.method, reqTemplate.url, bodyReader)
	if err != nil {
		return WebhookResult{
			Error: &WebhookError{
				Op:        "create_request",
				URL:       reqTemplate.urlHash,
				RequestID: requestID,
				Err:       err,
			},
			Duration: time.Since(start),
		}
	}

	req.Header = reqTemplate.headers.Clone()
	req.ContentLength = int64(len(reqTemplate.body))

	// Get bot-specific client for connection pooling
	client := f.clientManager.GetClient(reqTemplate.url)

	// Make the request with bot-specific client
	resp, err := client.Do(req)
	if err != nil {
		// Check if it's a timeout error
		if errors.Is(err, context.DeadlineExceeded) {
			return WebhookResult{
				Error: &WebhookError{
					Op:        "forward",
					URL:       reqTemplate.urlHash,
					RequestID: requestID,
					Err:       ErrTimeout,
				},
				Duration: time.Since(start),
			}
		}

		return WebhookResult{
			Error: &WebhookError{
				Op:        "forward",
				URL:       reqTemplate.urlHash,
				RequestID: requestID,
				Err:       err,
			},
			Duration: time.Since(start),
		}
	}
	defer resp.Body.Close()

	result := WebhookResult{
		StatusCode: resp.StatusCode,
		Duration:   time.Since(start),
	}

	// Check for HTTP error status
	if resp.StatusCode >= 400 {
		result.Error = &WebhookError{
			Op:        "forward",
			URL:       reqTemplate.urlHash,
			RequestID: requestID,
			Err:       NewHTTPError(resp.StatusCode, reqTemplate.urlHash, requestID),
		}
	}

	return result
}

