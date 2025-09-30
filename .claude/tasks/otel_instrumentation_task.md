# OpenTelemetry Instrumentation Task Checklist

## Overview
Add comprehensive OpenTelemetry (OTel) instrumentation to Lambda Bridge for distributed tracing and metrics collection. Integrate with AWS X-Ray and CloudWatch using ARM64-compatible OTel collector extensions to enhance observability beyond current request correlation.

## Implementation Plan

### Step 1: Add OpenTelemetry Dependencies
- [x] **Add OTel Go SDK dependencies to `go.mod`**
  - [x] Added `go.opentelemetry.io/otel v1.21.0`
  - [x] Added `go.opentelemetry.io/otel/trace v1.21.0`
  - [x] Added `go.opentelemetry.io/otel/metric v1.21.0`
  - [x] Added `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.21.0`
  - [ ] Investigate `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp` – package not published in v1.21.0 (consider upgrading to 1.22+ or using gRPC exporter)
  - [ ] Find AWS X-Ray exporter module (likely under `go.opentelemetry.io/contrib/exporters/aws/xray`; update dependency once confirmed)
  - [x] Added `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.46.0`
  - [x] Verified all added dependencies are pure Go (ARM64 compatible)

### Step 2: Create OTel Configuration Package
- [x] **Create `internal/telemetry/` package structure**
  - [x] Added `config.go` with service/resource configuration helpers
  - [x] Added `instrumentation.go` with HTTP client/handler wrappers
  - [x] Added `metrics.go` defining core instruments and helpers
  - [x] Added `collector_arm64.yaml` template for Lambda-compatible collector layer
  - [x] Resource helpers expose service name/version/environment attributes

### Step 3: Initialize OTel Providers in Main Function
- [x] **Integrate OTel setup in `cmd/main.go`**
  - [x] Added telemetry setup using OTLP HTTP exporter (Collector translates to X-Ray/CloudWatch)
  - [x] Meter provider initialized with SDK meter provider (noop fallback when disabled)
  - [x] Global tracer/meter providers and propagators configured with service resource attributes
  - [x] Graceful shutdown on Lambda exit with timeout handling
  - [x] Implementation uses pure-Go OTLP exporter compatible with ARM64 Lambda (`provided.al2`)

### Step 4: Instrument Request Handler with Tracing
- [ ] **Add tracing to `internal/handler/forwarder_handler.go`**
  - Wrap main `Handler()` function with OTel span
  - Extract trace context from Lambda event (if present)
  - Propagate OTel trace ID to existing request correlation system
  - Add span attributes for event type (ALB vs SNS), request ID, and routing decisions
  - Record span events for major processing steps
  - Replace manual `X-Trace-ID` generation with OTel trace ID

### Step 5: Instrument ALB Processor with Metrics and Tracing
- [ ] **Enhance `internal/handler/alb_processor.go` with OTel**
  - Add span for ALB processing with webhook routing information
  - Create custom metrics for worker pool utilization (queue depth, active workers)
  - Add histogram metrics for webhook processing latency by route (cloud/enterprise/default)
  - Add counter metrics for health check filtering and bypass operations
  - Instrument worker pool jobs with child spans
  - Add span attributes for worker ID and job metadata

### Step 6: Instrument Webhook Forwarder with HTTP Tracing
- [ ] **Add HTTP instrumentation to `internal/forwarder/webhook_forwarder.go`**
  - Wrap HTTP client with `otelhttp.NewTransport()` for automatic HTTP span creation
  - Add custom spans for retry logic and exponential backoff
  - Create histogram metrics for webhook response times by destination URL
  - Add counter metrics for webhook success/failure rates by HTTP status code
  - Instrument parallel webhook execution with span links
  - Add span attributes for webhook URL (sanitized), retry count, and timeout

### Step 7: Instrument SNS Forwarder with AWS SDK Tracing
- [ ] **Add OTel instrumentation to `internal/forwarder/sns_forwarder.go`**
  - Instrument AWS SDK SNS client with OTel middleware
  - Add span for SNS publish operations with topic ARN and message metadata
  - Create counter metrics for SNS publish success/failure
  - Add histogram metrics for SNS publish latency
  - Include message size and forwarding metadata in span attributes

### Step 8: Configure OTel Collector Layer Extension
- [ ] **Set up AWS Lambda Layer with ARM64 OTel Collector**
  - Create or reference ARM64-compatible OTel collector Lambda layer
  - Configure collector to export traces to AWS X-Ray
  - Configure collector to export metrics to CloudWatch via EMF
  - Set up OTLP HTTP endpoints for trace and metric collection
  - Add layer ARN to Lambda deployment configuration
  - Configure collector to handle high-throughput (200K+ messages/hour)

### Step 9: Add OTel-Aware Testing and Benchmarking
- [ ] **Update test suite for OTel integration**
  - Add OTel test helpers in `internal/telemetry/testing.go`
  - Update existing tests to handle OTel context propagation
  - Create benchmarks measuring OTel instrumentation overhead
  - Add integration tests validating trace propagation end-to-end
  - Test ARM64 builds with OTel dependencies
  - Verify performance impact stays within <5ms overhead per request

### Step 10: Update Documentation and Deployment
- [ ] **Complete OTel integration documentation**
  - Update `lambda_bridge_plan.md` with OTel architecture details
  - Add OTel configuration section to deployment guide
  - Document trace correlation with existing request ID system
  - Add troubleshooting guide for OTel/X-Ray integration
  - Update Makefile to include OTel layer deployment steps
  - Add CloudWatch dashboard templates for OTel metrics
  - Document cost implications of enhanced telemetry

## Technical Implementation Details

### OTel Configuration Structure
```go
// internal/telemetry/config.go
type Config struct {
    ServiceName     string
    ServiceVersion  string
    Environment     string
    TracingEnabled  bool
    MetricsEnabled  bool
    XRayEndpoint    string
    OTLPEndpoint    string
}
```

### Key Metrics to Collect
- **Request Metrics**: Total requests, latency percentiles, error rates
- **Webhook Metrics**: Success rates, retry counts, destination latency
- **Worker Pool Metrics**: Queue depth, worker utilization, job processing time
- **SNS Metrics**: Publish success/failure, message size, latency

### Integration with Existing Tracing
- Preserve existing `X-Request-ID` correlation system
- Map OTel trace IDs to existing request correlation
- Maintain backward compatibility with current tracing headers
- Enhance context propagation without breaking changes

## Success Criteria
- [ ] OTel traces visible in AWS X-Ray console with <100ms Lambda cold start impact
- [ ] Custom metrics flowing to CloudWatch with <5% performance overhead
- [ ] End-to-end trace correlation from ALB → Lambda → Webhooks
- [ ] ARM64 compatibility verified with all OTel components
- [ ] Zero test failures with OTel instrumentation enabled
- [ ] Documentation complete for operations team

## Expected Benefits
- **Enhanced Observability**: Detailed distributed tracing across all components
- **Performance Insights**: Granular metrics for optimization opportunities
- **Debugging Capabilities**: Complete request flow visibility in X-Ray
- **Operational Excellence**: CloudWatch dashboards with business metrics
- **Standardization**: Industry-standard telemetry with OTel ecosystem
