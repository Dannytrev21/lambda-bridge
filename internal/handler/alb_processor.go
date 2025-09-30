package handler

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"

	configpkg "github.com/Dannytrev21/lambda-bridge/internal/config"
	"github.com/Dannytrev21/lambda-bridge/internal/forwarder"
)

const (
	defaultWorkerCount = 5
	defaultQueueSize   = 50
)

type albJob struct {
	ctx         context.Context
	payload     json.RawMessage
	webhookURLs []string
	route       string
}

type webhookSelection struct {
	urls  []string
	route string
}

type ALBProcessor struct {
	config           *configpkg.Config
	webhookForwarder forwarder.WebhookForwarderInterface
	jobs             chan albJob
	workerCount      int
	debug            bool
}

func NewALBProcessor(cfg *configpkg.Config, webhookForwarder forwarder.WebhookForwarderInterface) *ALBProcessor {
	processor := &ALBProcessor{
		config:           cfg,
		webhookForwarder: webhookForwarder,
		jobs:             make(chan albJob, defaultQueueSize),
		workerCount:      defaultWorkerCount,
		debug:            cfg != nil && cfg.Debug,
	}

	processor.start()
	return processor
}

func (p *ALBProcessor) start() {
	if p.workerCount <= 0 {
		p.workerCount = defaultWorkerCount
	}

	for i := 0; i < p.workerCount; i++ {
		go p.worker()
	}
}

func (p *ALBProcessor) worker() {
	for job := range p.jobs {
		p.processJob(job)
	}
}

func (p *ALBProcessor) Process(ctx context.Context, rawEvent json.RawMessage, albEvent *events.ALBTargetGroupRequest) events.ALBTargetGroupResponse {
	if p.config != nil && p.config.SkipHealthChecks && p.isHealthCheck(albEvent) {
		if p.debug {
			log.Printf("Skipping health check: %s %s", albEvent.HTTPMethod, albEvent.Path)
		}
		return events.ALBTargetGroupResponse{
			StatusCode:      200,
			Headers:         map[string]string{"Content-Type": "application/json"},
			Body:            `{"status":"healthy"}`,
			IsBase64Encoded: false,
		}
	}

	selection := p.selectWebhookURLs(albEvent)
	if len(selection.urls) == 0 {
		return events.ALBTargetGroupResponse{
			StatusCode:      200,
			Headers:         map[string]string{"Content-Type": "application/json"},
			Body:            `{"status":"accepted"}`,
			IsBase64Encoded: false,
		}
	}

	job := albJob{
		ctx:         context.Background(),
		payload:     cloneRawMessage(rawEvent),
		webhookURLs: append([]string(nil), selection.urls...),
		route:       selection.route,
	}

	select {
	case p.jobs <- job:
	default:
		go p.processJob(job)
	}

	return events.ALBTargetGroupResponse{
		StatusCode:      200,
		Headers:         map[string]string{"Content-Type": "application/json"},
		Body:            `{"status":"accepted"}`,
		IsBase64Encoded: false,
	}
}

func (p *ALBProcessor) isHealthCheck(albEvent *events.ALBTargetGroupRequest) bool {
	if albEvent == nil {
		return false
	}

	if userAgent, exists := albEvent.Headers["user-agent"]; exists {
		if strings.Contains(strings.ToLower(userAgent), "elb-healthchecker") {
			return true
		}
	}

	path := strings.ToLower(albEvent.Path)
	healthPaths := []string{"/health", "/healthz", "/ping"}
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

func (p *ALBProcessor) processJob(job albJob) {
	results := p.webhookForwarder.ForwardToWebhooks(job.ctx, job.webhookURLs, job.payload)
	successCount := 0
	failureCount := 0
	var totalDuration time.Duration
	var maxDuration time.Duration
	for url, result := range results {
		if result.Error != nil {
			failureCount++
			if p.debug {
				log.Printf("Webhook %s failed: %v", url, result.Error)
			}
		} else {
			successCount++
			if p.debug {
				log.Printf("Webhook %s succeeded: %d", url, result.StatusCode)
			}
		}
		totalDuration += result.Duration
		if result.Duration > maxDuration {
			maxDuration = result.Duration
		}
	}
	log.Printf(
		"metrics=webhook_forward route=%s total=%d success=%d failure=%d total_duration_ms=%d max_duration_ms=%d",
		job.route,
		len(job.webhookURLs),
		successCount,
		failureCount,
		int64(totalDuration/time.Millisecond),
		int64(maxDuration/time.Millisecond),
	)
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

func cloneRawMessage(rawEvent json.RawMessage) json.RawMessage {
	if rawEvent == nil {
		return nil
	}
	buf := make([]byte, len(rawEvent))
	copy(buf, rawEvent)
	return json.RawMessage(buf)
}
