package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Metrics groups the instruments used across the service.
type Metrics struct {
	meter metric.Meter

	WebhookLatency metric.Float64Histogram
	WebhookSuccess metric.Int64Counter
	WebhookFailure metric.Int64Counter
	WebhookRetry   metric.Int64Counter

	WorkerQueueDepth metric.Int64UpDownCounter
	WorkerActiveJobs metric.Int64UpDownCounter

	SNSPublishLatency metric.Float64Histogram
	SNSPublishSuccess metric.Int64Counter
	SNSPublishFailure metric.Int64Counter
}

// NewMetrics constructs the standard metric instruments using the provided meter.
func NewMetrics(meter metric.Meter) (Metrics, error) {
	if meter == nil {
		return Metrics{}, nil
	}

	var err error
	m := Metrics{meter: meter}

	if m.WebhookLatency, err = meter.Float64Histogram("webhook.latency", metric.WithUnit("ms")); err != nil {
		return Metrics{}, err
	}
	if m.WebhookSuccess, err = meter.Int64Counter("webhook.success"); err != nil {
		return Metrics{}, err
	}
	if m.WebhookFailure, err = meter.Int64Counter("webhook.failure"); err != nil {
		return Metrics{}, err
	}
	if m.WebhookRetry, err = meter.Int64Counter("webhook.retry"); err != nil {
		return Metrics{}, err
	}

	if m.WorkerQueueDepth, err = meter.Int64UpDownCounter("worker.queue_depth"); err != nil {
		return Metrics{}, err
	}
	if m.WorkerActiveJobs, err = meter.Int64UpDownCounter("worker.active_jobs"); err != nil {
		return Metrics{}, err
	}

	if m.SNSPublishLatency, err = meter.Float64Histogram("sns.publish.latency", metric.WithUnit("ms")); err != nil {
		return Metrics{}, err
	}
	if m.SNSPublishSuccess, err = meter.Int64Counter("sns.publish.success"); err != nil {
		return Metrics{}, err
	}
	if m.SNSPublishFailure, err = meter.Int64Counter("sns.publish.failure"); err != nil {
		return Metrics{}, err
	}

	return m, nil
}

// RecordWebhookLatency records a latency sample for a webhook destination.
func (m Metrics) RecordWebhookLatency(ctx context.Context, durationMs float64, attrs ...attribute.KeyValue) {
	if m.WebhookLatency == nil {
		return
	}
	m.WebhookLatency.Record(ctx, durationMs, metric.WithAttributes(attrs...))
}

// AddWebhookResult increments success/failure counters for a webhook invocation.
func (m Metrics) AddWebhookResult(ctx context.Context, success bool, attrs ...attribute.KeyValue) {
	if success {
		if m.WebhookSuccess != nil {
			m.WebhookSuccess.Add(ctx, 1, metric.WithAttributes(attrs...))
		}
		return
	}
	if m.WebhookFailure != nil {
		m.WebhookFailure.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

// AddWebhookRetry increments the retry counter for a webhook.
func (m Metrics) AddWebhookRetry(ctx context.Context, attrs ...attribute.KeyValue) {
	if m.WebhookRetry == nil {
		return
	}
	m.WebhookRetry.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// SetWorkerQueueDepth records the current queue depth for the worker pool.
func (m Metrics) SetWorkerQueueDepth(ctx context.Context, depth int64, attrs ...attribute.KeyValue) {
	if m.WorkerQueueDepth == nil {
		return
	}
	m.WorkerQueueDepth.Add(ctx, depth, metric.WithAttributes(attrs...))
}

// AddSNSPublishResult records SNS publish latency and success/failure counts.
func (m Metrics) AddSNSPublishResult(ctx context.Context, latencyMs float64, err error, attrs ...attribute.KeyValue) {
	if m.SNSPublishLatency != nil {
		m.SNSPublishLatency.Record(ctx, latencyMs, metric.WithAttributes(attrs...))
	}
	if err == nil {
		if m.SNSPublishSuccess != nil {
			m.SNSPublishSuccess.Add(ctx, 1, metric.WithAttributes(attrs...))
		}
		return
	}
	if m.SNSPublishFailure != nil {
		m.SNSPublishFailure.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

// Enabled reports whether the metrics struct has an initialized meter.
func (m Metrics) Enabled() bool {
	return m.meter != nil
}
