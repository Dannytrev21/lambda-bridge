package handler

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/aws/aws-lambda-go/events"

	configpkg "github.com/Dannytrev21/lambda-bridge/internal/config"
	"github.com/Dannytrev21/lambda-bridge/internal/constants"
	"github.com/Dannytrev21/lambda-bridge/internal/forwarder"
)

type ALBJob struct {
	ctx     context.Context
	payload json.RawMessage
	route   string
	url     string
}

type webhookSelection struct {
	urls  []string
	route string
}

type ALBProcessor struct {
	config           *configpkg.Config
	webhookForwarder forwarder.WebhookForwarderInterface
	poolManager      *WorkerPoolManager
	debug            bool
	shutdownCtx      context.Context
	shutdownCancel   context.CancelFunc
	overflowSem      chan struct{}
	overflowDropped  atomic.Uint64
	overflowActive   atomic.Int32
}

func NewALBProcessor(cfg *configpkg.Config, webhookForwarder forwarder.WebhookForwarderInterface) *ALBProcessor {
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())

	processor := &ALBProcessor{
		config:           cfg,
		webhookForwarder: webhookForwarder,
		poolManager:      NewWorkerPoolManager(webhookForwarder),
		debug:            cfg != nil && cfg.Debug,
		shutdownCtx:      shutdownCtx,
		shutdownCancel:   shutdownCancel,
		overflowSem:      make(chan struct{}, constants.MaxConcurrentOverflow),
	}

	// Start metrics reporter
	go func() {
		ticker := time.NewTicker(constants.MetricsReportInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				stats := processor.poolManager.GetStats()
				for url, stat := range stats {
					log.Printf("POOL_STATS: bot=%s workers=%d queue=%d health=%.2f is_healthy=%v skipped_timeout=%d",
						url,
						stat["workers"],
						stat["queue_depth"],
						stat["health_score"],
						stat["is_healthy"],
						stat["skipped_timeout"])
				}
				// Report overflow stats
				if processor.overflowDropped.Load() > 0 || processor.overflowActive.Load() > 0 {
					log.Printf("OVERFLOW_STATS: active=%d dropped=%d",
						processor.overflowActive.Load(),
						processor.overflowDropped.Load())
				}
			case <-shutdownCtx.Done():
				return
			}
		}
	}()

	return processor
}

// Shutdown gracefully shuts down the ALB processor.
func (p *ALBProcessor) Shutdown(timeout time.Duration) error {
	if p.debug {
		log.Printf("Initiating graceful shutdown of ALB processor")
	}

	// Signal shutdown
	if p.shutdownCancel != nil {
		p.shutdownCancel()
	}

	// Shutdown all bot pools
	if p.poolManager != nil {
		p.poolManager.Shutdown()
	}

	// Wait for completion with timeout
	done := make(chan struct{})
	go func() {
		time.Sleep(500 * time.Millisecond) // Allow workers to finish current jobs
		close(done)
	}()

	select {
	case <-done:
		if p.debug {
			log.Printf("ALB processor shutdown completed successfully")
		}
		return nil
	case <-time.After(timeout):
		if p.debug {
			log.Printf("ALB processor shutdown timed out after %v", timeout)
		}
		return nil
	}
}

func (p *ALBProcessor) Process(ctx context.Context, rawEvent json.RawMessage, albEvent *events.ALBTargetGroupRequest) events.ALBTargetGroupResponse {
	if p.config != nil && p.config.SkipHealthChecks && p.isHealthCheck(albEvent) {
		if p.debug {
			log.Printf("Skipping health check: %s %s", albEvent.HTTPMethod, albEvent.Path)
		}
		return events.ALBTargetGroupResponse{
			StatusCode:      constants.HTTPStatusOK,
			Headers:         map[string]string{constants.ContentTypeHeader: constants.DefaultContentType},
			Body:            `{"status":"healthy"}`,
			IsBase64Encoded: false,
		}
	}

	selection := p.selectWebhookURLs(albEvent)
	if len(selection.urls) == 0 {
		return events.ALBTargetGroupResponse{
			StatusCode:      constants.HTTPStatusOK,
			Headers:         map[string]string{constants.ContentTypeHeader: constants.DefaultContentType},
			Body:            `{"status":"accepted"}`,
			IsBase64Encoded: false,
		}
	}

	if p.poolManager != nil {
		// Share immutable payload across all workers - no cloning
		dropped := p.poolManager.Submit(ctx, rawEvent, selection.route, selection.urls)
		for _, url := range dropped {
			// Try to acquire semaphore for overflow processing
			select {
			case p.overflowSem <- struct{}{}:
				// Successfully acquired slot, spawn goroutine
				// Share immutable payload - no cloning
				go p.processOverflow(ctx, rawEvent, url, selection.route)
			default:
				// Semaphore full, drop the overflow request with metrics
				p.overflowDropped.Add(1)
				if p.debug {
					log.Printf("Overflow capacity exceeded, dropped request for url=%s route=%s", url, selection.route)
				}
			}
		}
	}

	return events.ALBTargetGroupResponse{
		StatusCode:      constants.HTTPStatusOK,
		Headers:         map[string]string{constants.ContentTypeHeader: constants.DefaultContentType},
		Body:            `{"status":"accepted"}`,
		IsBase64Encoded: false,
	}
}

func (p *ALBProcessor) isHealthCheck(albEvent *events.ALBTargetGroupRequest) bool {
	if albEvent == nil {
		return false
	}

	if userAgent, exists := albEvent.Headers[strings.ToLower(constants.UserAgentHeader)]; exists {
		if strings.Contains(strings.ToLower(userAgent), strings.ToLower(constants.ELBHealthCheckerUserAgent)) {
			return true
		}
	}

	path := strings.ToLower(albEvent.Path)
	healthPaths := []string{
		constants.HealthCheckPath,
		constants.HealthCheckPathZ,
		constants.PingPath,
	}
	for _, healthPath := range healthPaths {
		if path == healthPath {
			return true
		}
	}

	return false
}

func (p *ALBProcessor) selectWebhookURLs(albEvent *events.ALBTargetGroupRequest) webhookSelection {
	if p.config != nil {
		if albEvent != nil {
			if hasHeader(albEvent, "x-dcp-destination-host") {
				if len(p.config.CloudWebhookURLs) > 0 {
					return webhookSelection{urls: p.config.CloudWebhookURLs, route: "cloud"}
				}
			}
			if hasHeader(albEvent, "x-github-enterprise-host") {
				if len(p.config.EnterpriseWebhookURLs) > 0 {
					return webhookSelection{urls: p.config.EnterpriseWebhookURLs, route: "enterprise"}
				}
			}
		}

		if len(p.config.WebhookURLs) > 0 {
			return webhookSelection{urls: p.config.WebhookURLs, route: "default"}
		}
		if len(p.config.CloudWebhookURLs) > 0 {
			return webhookSelection{urls: p.config.CloudWebhookURLs, route: "cloud"}
		}
		if len(p.config.EnterpriseWebhookURLs) > 0 {
			return webhookSelection{urls: p.config.EnterpriseWebhookURLs, route: "enterprise"}
		}
	}
	return webhookSelection{}
}

func hasHeader(albEvent *events.ALBTargetGroupRequest, headerName string) bool {
	if albEvent == nil {
		return false
	}

	lowerName := strings.ToLower(headerName)
	for key, values := range albEvent.MultiValueHeaders {
		if strings.ToLower(key) == lowerName {
			for _, value := range values {
				if value != "" {
					return true
				}
			}
		}
	}
	for key, value := range albEvent.Headers {
		if strings.ToLower(key) == lowerName && value != "" {
			return true
		}
	}
	return false
}

func (p *ALBProcessor) processOverflow(ctx context.Context, payload json.RawMessage, url, route string) {
	// Track active overflow processing
	p.overflowActive.Add(1)
	defer func() {
		p.overflowActive.Add(-1)
		<-p.overflowSem // Release semaphore slot
	}()

	time.Sleep(100 * time.Millisecond)
	results := p.webhookForwarder.ForwardToWebhooks(ctx, []string{url}, payload)
	if p.debug {
		success := 0
		failure := 0
		for _, r := range results {
			if r.Error != nil {
				failure++
			} else {
				success++
			}
		}
		log.Printf("overflow processing route=%s url=%s success=%d failure=%d", route, url, success, failure)
	}
}

func (p *ALBProcessor) GetPoolStats() map[string]map[string]interface{} {
	if p.poolManager == nil {
		return nil
	}
	return p.poolManager.GetStats()
}
