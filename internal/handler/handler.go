package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/Dannytrev21/lambda-bridge/internal/config"
	"github.com/Dannytrev21/lambda-bridge/internal/forwarder"
	"github.com/aws/aws-lambda-go/events"
)

const (
	// MaxWorkers is the number of concurrent workers processing webhook jobs
	MaxWorkers = 25
	// QueueSize is the buffer size for the work queue
	QueueSize = 200
)

// WebhookJob represents a webhook forwarding job
type WebhookJob struct {
	ctx         context.Context
	urls        []string
	payload     json.RawMessage
	method      string
	path        string
	queryParams map[string]string
	headers     map[string]string
}

// SNSForwarder interface for dependency injection
type SNSForwarder interface {
	Forward(ctx context.Context, topicArn string, rawEvent json.RawMessage) error
}

// Handler is the main Lambda handler for forwarding events
type Handler struct {
	snsForwarder     SNSForwarder
	webhookForwarder *forwarder.WebhookForwarder
	config           *config.Config
	workQueue        chan *WebhookJob
	shutdown         chan struct{}
	workerWg         sync.WaitGroup
}

// NewHandler creates a new Lambda handler with worker pool
func NewHandler(cfg *config.Config, snsForwarder SNSForwarder) *Handler {
	h := &Handler{
		snsForwarder:     snsForwarder,
		webhookForwarder: forwarder.NewWebhookForwarder(),
		config:           cfg,
		workQueue:        make(chan *WebhookJob, QueueSize),
		shutdown:         make(chan struct{}),
	}

	// Start worker pool
	h.workerWg.Add(MaxWorkers)
	for i := 0; i < MaxWorkers; i++ {
		go h.worker(i)
	}

	return h
}

// worker processes webhook jobs from the queue
func (h *Handler) worker(id int) {
	defer h.workerWg.Done()

	if h.config.Debug {
		log.Printf("[DEBUG] Worker %d started", id)
	}

	for {
		select {
		case job := <-h.workQueue:
			if job == nil {
				// Queue closed
				if h.config.Debug {
					log.Printf("[DEBUG] Worker %d shutting down", id)
				}
				return
			}

			// Process the job
			if h.config.Debug {
				log.Printf("[DEBUG] Worker %d processing job with %d URLs", id, len(job.urls))
			}

			// ForwardToWebhooks spawns a goroutine per URL, ensuring isolation
			// If one URL fails or hangs, it doesn't block others
			h.webhookForwarder.ForwardToWebhooks(job.ctx, job.urls, job.payload, job.method, job.path, job.queryParams, job.headers)

		case <-h.shutdown:
			if h.config.Debug {
				log.Printf("[DEBUG] Worker %d received shutdown signal", id)
			}
			return
		}
	}
}

// Handle processes Lambda events (ALB or SNS) and returns appropriate response
func (h *Handler) Handle(ctx context.Context, rawEvent json.RawMessage) any {
	// Add request ID to context if not present
	requestID := forwarder.RequestIDFromContext(ctx)
	if requestID == "" {
		requestID = forwarder.GenerateRequestID()
		ctx = forwarder.WithRequestID(ctx, requestID)
	}

	if h.config.Debug {
		log.Printf("[DEBUG] [%s] Received event: %s", requestID, string(rawEvent))
	}

	// Try ALB event first (most common)
	var albEvent events.ALBTargetGroupRequest
	if err := json.Unmarshal(rawEvent, &albEvent); err == nil && albEvent.RequestContext.ELB.TargetGroupArn != "" {
		return h.handleALBEvent(ctx, rawEvent, &albEvent)
	}

	// Try SNS event
	var snsEvent events.SNSEvent
	if err := json.Unmarshal(rawEvent, &snsEvent); err == nil && len(snsEvent.Records) > 0 && snsEvent.Records[0].SNS.MessageID != "" {
		return h.handleSNSEvent(ctx, rawEvent)
	}

	// Unknown event type - return safe ALB response
	log.Printf("[WARN] Unknown event type, returning safe ALB response")
	return events.ALBTargetGroupResponse{
		StatusCode:      200,
		Headers:         map[string]string{"Content-Type": "application/json"},
		Body:            `{"status":"accepted"}`,
		IsBase64Encoded: false,
	}
}

// handleALBEvent processes ALB events
func (h *Handler) handleALBEvent(ctx context.Context, rawEvent json.RawMessage, albEvent *events.ALBTargetGroupRequest) events.ALBTargetGroupResponse {
	// Check if this is a health check
	if h.isHealthCheck(albEvent) {
		return events.ALBTargetGroupResponse{
			StatusCode: 200,
			Body:       "healthy",
		}
	}

	// Determine which webhook URLs to use
	urls := h.selectWebhookURLs(albEvent)
	if len(urls) == 0 {
		return events.ALBTargetGroupResponse{
			StatusCode: 200,
			Body:       `{"status":"no_webhooks_configured"}`,
		}
	}

	// Extract forwarding information from the ALB event
	// This includes body, headers, path, query params, and HTTP method
	payload := h.extractPayload(albEvent)
	method := albEvent.HTTPMethod
	if method == "" {
		method = "POST" // Default to POST if not specified
	}
	path := albEvent.Path
	queryParams := albEvent.QueryStringParameters
	headers := albEvent.Headers

	// Forward asynchronously with semaphore limiting
	// This ensures burst handling and rate limiting
	go h.asyncForward(ctx, urls, payload, method, path, queryParams, headers)

	// Return immediately with 200 to prevent retry storms
	return events.ALBTargetGroupResponse{
		StatusCode:      200,
		Headers:         map[string]string{"Content-Type": "application/json"},
		Body:            `{"status":"accepted"}`,
		IsBase64Encoded: false,
	}
}

// handleSNSEvent processes SNS events
func (h *Handler) handleSNSEvent(ctx context.Context, rawEvent json.RawMessage) any {
	if err := h.snsForwarder.Forward(ctx, h.config.SNSTopicArn, rawEvent); err != nil {
		// Log the error with context
		log.Printf("[ERROR] SNS forwarding failed: %v", err)
		// Return the error for Lambda retry mechanism
		return err
	}
	return nil
}

// extractPayload extracts the body from an ALB event
// If the body is base64 encoded, it decodes it first
// This ensures we forward the original payload, not the ALB event wrapper
func (h *Handler) extractPayload(albEvent *events.ALBTargetGroupRequest) json.RawMessage {
	body := albEvent.Body

	// Decode base64 if needed
	if albEvent.IsBase64Encoded {
		decoded, err := base64.StdEncoding.DecodeString(body)
		if err != nil {
			log.Printf("[WARN] Failed to decode base64 body: %v", err)
			// Fall back to original body
			return json.RawMessage(body)
		}
		return json.RawMessage(decoded)
	}

	return json.RawMessage(body)
}

// asyncForward enqueues webhook jobs for async processing
// The work queue provides burst smoothing by buffering incoming requests
// Workers process jobs from the queue, providing controlled concurrency
func (h *Handler) asyncForward(ctx context.Context, urls []string, payload json.RawMessage, method, path string, queryParams, headers map[string]string) {
	job := &WebhookJob{
		ctx:         ctx,
		urls:        urls,
		payload:     payload,
		method:      method,
		path:        path,
		queryParams: queryParams,
		headers:     headers,
	}

	// Try to enqueue the job (non-blocking)
	select {
	case h.workQueue <- job:
		// Job successfully queued
		if h.config.Debug {
			log.Printf("[DEBUG] Job queued with %d URLs (queue size: %d/%d)",
				len(urls), len(h.workQueue), QueueSize)
		}
	case <-ctx.Done():
		log.Printf("[WARN] Context cancelled before enqueuing job")
	default:
		// Queue is full - drop the job
		// This rarely happens with a large queue (100 items)
		log.Printf("[WARN] Queue full - dropping job (burst exceeded queue capacity)")
	}
}

// isHealthCheck determines if the request is a health check
// Health checks are always filtered to prevent overwhelming webhook endpoints
func (h *Handler) isHealthCheck(event *events.ALBTargetGroupRequest) bool {
	if event == nil {
		return false
	}

	// Check for ELB health checker user agent (case-insensitive)
	for key, value := range event.Headers {
		if strings.ToLower(key) == "user-agent" {
			if strings.Contains(strings.ToLower(value), "elb-healthchecker") {
				return true
			}
		}
	}

	// Check for health check paths
	path := strings.ToLower(event.Path)
	return path == "/health" || path == "/status" || path == "/ping"
}

// selectWebhookURLs determines which webhook URLs to use based on headers
func (h *Handler) selectWebhookURLs(event *events.ALBTargetGroupRequest) []string {
	if event == nil {
		return h.config.WebhookURLs
	}

	// Check for cloud destination (case-insensitive header lookup)
	if dest := getHeader(event, "x-dcp-destination-host"); dest != "" && len(h.config.CloudWebhookURLs) > 0 {
		return h.config.CloudWebhookURLs
	}

	// Check for enterprise destination
	if enterprise := getHeader(event, "x-github-enterprise-host"); enterprise != "" && len(h.config.EnterpriseWebhookURLs) > 0 {
		return h.config.EnterpriseWebhookURLs
	}

	return h.config.WebhookURLs
}

// getHeader retrieves a header value (case-insensitive)
func getHeader(event *events.ALBTargetGroupRequest, name string) string {
	lowerName := strings.ToLower(name)

	// Check regular headers
	for key, value := range event.Headers {
		if strings.ToLower(key) == lowerName {
			return value
		}
	}

	// Check multi-value headers
	for key, values := range event.MultiValueHeaders {
		if strings.ToLower(key) == lowerName && len(values) > 0 {
			return values[0]
		}
	}

	return ""
}

// Shutdown gracefully shuts down the handler and drains the work queue
func (h *Handler) Shutdown(timeout time.Duration) error {
	log.Printf("[INFO] Initiating graceful shutdown (queue size: %d)", len(h.workQueue))

	// Close the work queue to prevent new jobs
	close(h.workQueue)

	// Signal all workers to shut down
	close(h.shutdown)

	// Wait for queue to drain or timeout
	deadline := time.Now().Add(timeout)
	for {
		if len(h.workQueue) == 0 {
			log.Printf("[INFO] Work queue drained successfully")
			break
		}

		if time.Now().After(deadline) {
			remaining := len(h.workQueue)
			log.Printf("[WARN] Shutdown timeout reached with %d jobs remaining", remaining)
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	// Wait for all workers to finish with a separate timeout
	// Workers may be in the middle of HTTP requests with retries, so allow enough time
	workersDone := make(chan struct{})
	go func() {
		h.workerWg.Wait()
		close(workersDone)
	}()

	select {
	case <-workersDone:
		log.Printf("[INFO] All workers stopped successfully")
	case <-time.After(10 * time.Second):
		log.Printf("[WARN] Workers did not stop within 10 seconds")
	}

	return h.webhookForwarder.Close()
}
