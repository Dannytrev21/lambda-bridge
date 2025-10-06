package forwarder

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"math"
	mathrand "math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
)

// Context keys
type contextKey string

const (
	ContextKeyRequestID contextKey = "request-id"
	defaultMaxRetries              = 3
	defaultBaseDelay               = 100 * time.Millisecond
	defaultMaxDelay                = 5 * time.Second
)

var (
	// Single shared HTTP client for all webhook forwarding
	// Connection pooling and reuse for efficiency
	sharedHTTPClient = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  false,
			DisableKeepAlives:   false,
		},
	}
)

// GenerateRequestID generates a unique request ID
func GenerateRequestID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("req-%x", b)
}

// WithRequestID adds a request ID to the context
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, ContextKeyRequestID, requestID)
}

// RequestIDFromContext retrieves the request ID from context
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(ContextKeyRequestID).(string); ok {
		return id
	}
	return ""
}

// WebhookForwarder handles forwarding events to webhook endpoints
type WebhookForwarder struct {
	client *http.Client
}

// NewWebhookForwarder creates a new webhook forwarder
func NewWebhookForwarder() *WebhookForwarder {
	return &WebhookForwarder{
		client: sharedHTTPClient,
	}
}

// ForwardToWebhooks sends the payload to multiple webhook URLs concurrently
// Each URL is processed in its own goroutine to ensure isolation:
// - If one URL is slow, it doesn't block others
// - If one URL fails, others continue
// - Retry logic is per-URL, not shared
func (wf *WebhookForwarder) ForwardToWebhooks(ctx context.Context, urls []string, payload json.RawMessage) {
	if len(urls) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, url := range urls {
		if url == "" {
			continue
		}

		// Each URL gets its own goroutine - ensures isolation
		wg.Add(1)
		go func(targetURL string) {
			defer wg.Done()
			wf.forwardWithRetry(ctx, targetURL, payload)
		}(url)
	}

	// Wait for all URLs to complete or context to cancel
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All URLs completed
	case <-ctx.Done():
		// Context cancelled, but goroutines will finish on their own
		log.Printf("[WARN] Context cancelled during webhook forwarding")
	}
}

// forwardWithRetry sends payload to a single webhook with exponential backoff retry
// This function ensures that failures don't block other URLs
func (wf *WebhookForwarder) forwardWithRetry(ctx context.Context, url string, payload json.RawMessage) {
	var lastErr error

	for attempt := 0; attempt <= defaultMaxRetries; attempt++ {
		// Create fresh request for each attempt
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
		if err != nil {
			log.Printf("[ERROR] Failed to create request for %s: %v", url, err)
			return
		}

		// Set headers
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Lambda-Bridge/1.0")
		if reqID := RequestIDFromContext(ctx); reqID != "" {
			req.Header.Set("X-Request-ID", reqID)
		}

		// Perform the request
		resp, err := wf.client.Do(req)

		// Handle network errors
		if err != nil {
			lastErr = fmt.Errorf("network error: %w", err)
			// Check if context was cancelled
			if ctx.Err() != nil {
				log.Printf("[WARN] Request cancelled for %s: %v", url, ctx.Err())
				return
			}
			// Continue to retry for network errors
		} else {
			defer resp.Body.Close()

			// Success
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				log.Printf("[INFO] Successfully forwarded to %s (status: %d)", url, resp.StatusCode)
				return
			}

			// Client error - don't retry
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				log.Printf("[WARN] Client error from %s (status: %d) - not retrying", url, resp.StatusCode)
				return
			}

			// Server error - will retry
			lastErr = fmt.Errorf("server error: status %d", resp.StatusCode)
		}

		// Don't retry on last attempt
		if attempt == defaultMaxRetries {
			break
		}

		// Calculate backoff delay
		delay := calculateBackoff(attempt)

		// Wait before retry, respecting context cancellation
		select {
		case <-time.After(delay):
			// Continue to next retry
		case <-ctx.Done():
			log.Printf("[WARN] Context cancelled during retry for %s", url)
			return
		}
	}

	log.Printf("[ERROR] Failed to forward to %s after %d retries: %v", url, defaultMaxRetries, lastErr)
}

// calculateBackoff computes exponential backoff delay with jitter
func calculateBackoff(attempt int) time.Duration {
	delay := time.Duration(math.Min(
		float64(defaultBaseDelay)*math.Pow(2, float64(attempt)),
		float64(defaultMaxDelay),
	))
	// Add jitter (±25%)
	jitter := time.Duration(float64(delay) * (mathrand.Float64()*0.5 - 0.25))
	return delay + jitter
}

// Close closes the forwarder
func (wf *WebhookForwarder) Close() error {
	return nil
}

// SNSPublisher defines the interface for SNS publishing operations
type SNSPublisher interface {
	Publish(ctx context.Context, params *sns.PublishInput, optFns ...func(*sns.Options)) (*sns.PublishOutput, error)
}

// SNSForwarder handles forwarding events to SNS topics
type SNSForwarder struct {
	client SNSPublisher
}

// NewSNSForwarder creates a new SNS forwarder
func NewSNSForwarder(client SNSPublisher) *SNSForwarder {
	return &SNSForwarder{client: client}
}

// Forward sends a raw event to an SNS topic
func (sf *SNSForwarder) Forward(ctx context.Context, topicArn string, rawEvent json.RawMessage) error {
	if topicArn == "" {
		return fmt.Errorf("SNS topic ARN is required")
	}

	// Create message with metadata
	message := map[string]interface{}{
		"event":       rawEvent,
		"forwardedAt": time.Now().UTC().Format(time.RFC3339),
	}

	messageJSON, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal SNS message: %w", err)
	}

	// Publish to SNS
	_, err = sf.client.Publish(ctx, &sns.PublishInput{
		TopicArn: aws.String(topicArn),
		Message:  aws.String(string(messageJSON)),
	})

	if err != nil {
		return fmt.Errorf("failed to publish to SNS: %w", err)
	}

	log.Printf("[INFO] Successfully published to SNS topic: %s", topicArn)
	return nil
}
