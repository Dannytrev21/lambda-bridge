# Lambda Bridge - Architectural Plan

## 1. Project Overview

### High-level Description
Lambda Bridge is a high-performance AWS Lambda function that forwards events between different systems while preventing webhook retry storms. It handles 200K+ messages/hour from ALB and SNS sources, forwarding to SNS topics and webhook endpoints.

### Key Goals
- **Performance**: Handle 200K messages/hour (55/sec) with <100ms ALB response time
- **Reliability**: Prevent webhook retry storms that can 8x the load

### Key Stakeholders
- **Primary**: Event processing systems requiring reliable forwarding
- **Secondary**: Webhook receivers (Github apps/bots)

### Success Criteria
- ✅ ALB always returns HTTP 200 (prevents retry storms)
- ✅ Handler returns `any` type (Lambda runtime compatibility)
- ✅ >90% test coverage with critical path testing
- ✅ Lambda deployment package under 50MB
- ✅ Health check filtering saves ~1000 unnecessary forwards/hour

## 2. Architecture Design

### System Architecture
```mermaid
graph TB
    ALB[Application Load Balancer] --> Lambda[Lambda Bridge Handler]
    SNS_IN[Incoming SNS Events] --> Lambda

    Lambda --> |Type Detection| TypeDetector{Event Type?}

    TypeDetector -->|ALB Event| ALBProcessor[ALB Event Processor]
    TypeDetector -->|SNS Event| SNSProcessor[SNS Event Processor]
    TypeDetector -->|Unknown| SafeResponse[Safe ALB 200 Response]

    ALBProcessor --> |Health Check?| HealthFilter{Health Check Filter}
    HealthFilter -->|Yes| HealthResponse[200 Healthy Response]
    HealthFilter -->|No| AsyncForwarder[Async Webhook Forwarder]

    ALBProcessor --> ImmediateResponse[Immediate 200 Response]
    AsyncForwarder --> |Parallel| Webhook1[Webhook 1]
    AsyncForwarder --> |Parallel| Webhook2[Webhook 2]
    AsyncForwarder --> |Parallel| Webhook3[Webhook 3]

    SNSProcessor --> SNS_OUT[SNS Topic Forward]
    SNSProcessor --> |Error/Nil| LambdaRetry[Lambda Retry Mechanism]

    style Lambda fill:#e1f5fe
    style ImmediateResponse fill:#c8e6c9
    style AsyncForwarder fill:#fff3e0
    style SafeResponse fill:#fce4ec
```

### Component Breakdown
1. **ForwarderHandler** (`internal/handler/forwarder_handler.go`)
   - Event type detection via JSON unmarshaling (ALB vs SNS)
   - Delegates ALB handling to `ALBProcessor`
   - Delegates SNS events to SNS forwarder
   - Return type management (`any`)

2. **SNSForwarder** (`internal/forwarder/`)
   - Forward raw events to SNS topics
   - Add ForwardedAt timestamp metadata
   - Error propagation for Lambda retry

3. **WebhookForwarder** (`internal/forwarder/webhook_forwarder.go`)
   - Parallel HTTP POST to multiple webhooks
   - Prepared request templates per destination (precomputed method/headers/body)
   - Tuned Transport (keep-alives, idle pools) for Lambda latency
   - 3-retry exponential backoff (100ms base delay)
   - 10-second timeout per webhook with context cancellation support
   - Preserves GitHub request fidelity: path/query, multi-value headers, base64 flag, and context headers
   - **NEW**: Request correlation with auto-generated request IDs (`X-Request-ID`)
   - **NEW**: Distributed tracing support with trace ID propagation (`X-Trace-ID`)
   - **NEW**: Context-aware timeout management for individual webhook calls

4. **Configuration** (`internal/config/*`, `cmd/main.go`)
   - Embedded YAML configs for `dev`, `qa`, `prod`; disk-loaded `test` config
   - Fields: `cloud_webhook_urls`, `enterprise_webhook_urls`, (optional) `webhook_urls` fallback
   - Header-based routing: `x-dcp-destination-host` → cloud, `x-github-enterprise-host` → enterprise
   - Env var overrides supported: `ENV`/`ENVIRONMENT`, `SNS_TOPIC_ARN`, `WEBHOOK_URLS`, `SKIP_HEALTH_CHECKS`, `DEBUG`
   - AWS SDK initialization during cold start and DI wiring

5. **ALBProcessor** (`internal/handler/alb_processor.go`)
   - Health check filtering
   - Header-based routing selection (cloud/enterprise/default)
   - Bounded worker pool (default 5 workers, queue size 50) enqueues webhook fan-out
   - Immediate 200 response to ALB while background workers deliver
   - Structured metrics via logs (counts, durations, route)
   - **NEW**: Proper context propagation from request to worker jobs
   - **NEW**: Graceful shutdown support with context cancellation
   - **NEW**: Context-aware worker termination on shutdown signals

### Data Flow
1. **Event Arrival**: ALB or SNS triggers Lambda
2. **Context Setup**: Generate request ID, propagate context with tracing information
3. **Type Detection**: Attempt ALB unmarshal, then SNS unmarshal
4. **ALB Path**: Return 200 immediately → enqueue job to worker pool for webhook forwarding with request context
5. **SNS Path**: Forward to topic → return error/nil for retry
6. **Health Check Path**: Detect and return 200 without forwarding
7. **Unknown Path**: Safe ALB 200 response
8. **Webhook Processing**: Workers receive context, add tracing headers, forward with timeout control

### Technology Stack Justification
- **Go 1.21+**: Performance, concurrency, AWS Lambda support
- **AWS Lambda**: Serverless scaling, pay-per-request pricing
- **AWS SDK v2**: Modern API, better performance, context support
- **Testify**: Comprehensive testing framework with mocks

### Design Patterns
- **Strategy Pattern**: Event type detection and routing
- **Interface Segregation**: SNSClient, WebhookForwarderInterface
- **Dependency Injection**: Constructor-based, testable
- **Bounded Worker Pool**: Queue + workers for controlled fan-out
- **Request Templating**: Precompute headers/body/method per destination
- **Header Canonicalization**: Merge single/multi-value headers; preserve cookies
- **Circuit Breaker**: Panic recovery in async processing
- **NEW: Context Propagation**: Request context flows through all async operations
- **NEW: Correlation Pattern**: Request IDs for distributed tracing
- **NEW: Graceful Shutdown**: Context cancellation for clean worker termination

## 3. Technical Specifications

### Lambda Configuration
```yaml
Runtime: provided.al2            # custom Go 1.21 bootstrap
Architecture: arm64 (cost optimization)
Memory: 128MB (sufficient for current load)
Timeout: 30 seconds (webhook forwarding buffer)
Environment Variables:
  ENV: "dev|qa|prod|test"              # selects embedded/disk config
  SNS_TOPIC_ARN: "arn:aws:sns:..."     # override
  WEBHOOK_URLS: "url1,url2"            # optional fallback/override
  SKIP_HEALTH_CHECKS: "true"
  DEBUG: "false"
```

Config files (embedded for non-test):
- `configs/config.dev.yml`, `configs/config.qa.yml`, `configs/config.prod.yml`
- `configs/config.test.yml` (disk for tests)

Config keys:
- `cloud_webhook_urls`: destination list for GitHub Cloud events (`x-dcp-destination-host`)
- `enterprise_webhook_urls`: destination list for GitHub Enterprise events (`x-github-enterprise-host`)
- `webhook_urls`: optional default/fallback list

### Performance Requirements
- **Throughput**: 200K messages/hour (55/second)
- **Latency**: <100ms ALB response time
- **Memory**: <50MB per invocation

### Security Considerations
- **No Authentication**: Lambda doesn't handle auth (it forwards any auth information, receivers manage own)
- **Network**: VPC configuration optional (public internet access needed)
- **IAM**: Minimal permissions (SNS publish, CloudWatch logs)
- **Secrets**: No sensitive data stored in Lambda

### API Contracts
**Input Events:**
- ALB Target Group Request (AWS events.ALBTargetGroupRequest)
- SNS Event (AWS events.SNSEvent)

**Output Responses:**
- ALB: `events.ALBTargetGroupResponse{StatusCode: 200}`
- SNS: `error` or `nil`

**Context & Tracing:**
- Request correlation via `X-Request-ID` header (format: `req-{16-char-hex}`)
- Distributed tracing via `X-Trace-ID` header (optional)
- Context propagation through all async operations
- Timeout management: 10-second default per webhook call
- Graceful shutdown: configurable timeout for worker termination

## 4. High-Level Implementation Roadmap

### Phase 1: Foundation ✅ COMPLETED
- [x] Task 1: Set up Go project structure with clean architecture
- [x] Task 2: Configure AWS SDK v2 and Lambda runtime
- [x] Task 3: Implement core handler with `any` return type
- [x] Task 4: Create SNS and webhook forwarder interfaces

### Phase 2: Core Features ✅ COMPLETED
- [x] Task 1: Event type detection logic (ALB vs SNS)
- [x] Task 2: ALB 200 response pattern (prevent retry storms)
- [x] Task 3: Async webhook forwarding with goroutines
- [x] Task 4: SNS raw event forwarding with metadata

### Phase 3: Resilience ✅ COMPLETED
- [x] Task 1: Health check filtering (ELB-HealthChecker, paths)
- [x] Task 2: Webhook retry logic with exponential backoff
- [x] Task 3: Panic recovery in async processing
- [x] Task 4: Graceful unknown event handling

### Phase 4: Production Ready  ✅ COMPLETED
- [x] Task 1: Comprehensive test suite (>90% coverage)
- [x] Task 2: Lambda deployment package (`make lambda-build`)
- [x] Task 3: Environment config loader (embedded YAMLs + overrides)
- [x] Task 4: Header-based routing (cloud vs enterprise)
- [x] Task 5: Refactor handler layering with `ALBProcessor` + worker pool
- [x] Task 6: Forwarder transport tuning + prepared requests
- [x] Task 7: Preserve original path/query/base64 and add context headers
- [x] Task 8: Strengthen async tests (channel sync) + add benchmarks
- [x] Task 9: Makefile with dev/test/deploy commands
- [x] **NEW Task 10**: Context propagation and request correlation
- [x] **NEW Task 11**: Graceful shutdown and timeout management

### Phase 5: Operational Excellence 🔄 IN PROGRESS
- [ ] Task 1: CloudWatch EMF integration for webhook metrics
- [ ] Task 2: Performance benchmarking under load (CI benchmark gate)
- [ ] Task 3: Dead Letter Queue or EventBridge for persistent failures
- [ ] Task 4: Cost optimization analysis and ARM64 migration

### Phase 6: Advanced Features 📋 PLANNED
- [ ] Task 1: EventBridge integration for failed webhook forwards
- [ ] Task 2: Webhook signature validation support
- [ ] Task 3: Dynamic webhook URL configuration via Parameter Store
- [ ] Task 4: Circuit breaker pattern for consistently failing webhooks

## 5. Technical Decisions Log

### Decision 1: Handler Returns `any` Type
**Rationale**: Lambda runtime in Go requires flexible return types. ALB events need `ALBTargetGroupResponse`, SNS events need `error`/`nil`. Using `any` allows compile-time type checking while supporting both patterns.

**Alternatives Considered**: `interface{}` (older Go), separate handlers (complexity)

### Decision 2: Always Return ALB 200
**Rationale**: Prevents webhook retry storms. GitHub retries 8x over 24h, Stripe for 72h. At 200K events/hour, failures would create 1.6M retries, increasing costs by 70%.

**Trade-offs**: No immediate failure notification, but async processing maintains reliability.

### Decision 3: Async Webhook Forwarding
**Rationale**: ALB response must be <100ms. Webhook calls can take seconds. Async processing allows immediate ALB response while maintaining webhook delivery.

**Trade-offs**: No immediate webhook failure feedback, but prevents timeout issues.

### Decision 4: No Authentication in Lambda
**Rationale**: Webhook receivers manage their own authentication (GitHub HMAC, Stripe signatures). Lambda stays simple and focused on forwarding.

**Benefits**: Reduced complexity, better separation of concerns, easier testing.

### Decision 5: Bounded Worker Pool for ALB Webhooks
**Rationale**: Avoid unbounded goroutines and protect latency when some endpoints are slow.

**Details**: Default 5 workers, queue size 50; overflow processed in detached goroutine to avoid blocking hot path.

### Decision 6: Preserve Original Request Metadata
**Rationale**: Downstream bots and observability benefit from forwarding path/query, base64 encoding flag, and context headers.

**Details**: Merge multi-value headers; compute raw query from single/multi-value params; inject `X-Original-*` headers.

### Decision 7: Transport Tuning + Prepared Requests
**Rationale**: Reduce per-request overhead and leverage persistent connections in Lambda.

**Details**: Custom `http.Transport` with Keep-Alive; precompute headers/body per target.

### Decision 8: Context Propagation and Request Correlation
**Rationale**: Enable distributed tracing, proper timeout management, and graceful shutdown for production reliability.

**Implementation Details**:
- Generate unique request IDs (`req-{16-char-hex}`) for correlation across async operations
- Propagate context from ALB request through worker pool to webhook calls
- Add tracing headers (`X-Request-ID`, `X-Trace-ID`) to all outbound webhook requests
- Implement context-aware timeouts (10s default) with cancellation support
- Enable graceful shutdown of worker pool via context cancellation

**Benefits**: Enhanced observability, better debugging, proper resource cleanup, production-ready error handling.

## 6. Risk Assessment

### Technical Risks
| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Webhook endpoint failures | Medium | High | 3-retry exponential backoff, panic recovery |
| Lambda cold start latency | Low | Medium | Keep function warm, optimize package size |
| SNS publish failures | High | Low | Lambda retry mechanism, error propagation |
| Memory exhaustion | High | Low | Process events individually, no batching |

### Dependencies and Blockers
- **AWS Lambda Runtime**: Dependency on AWS infrastructure
- **Webhook Endpoints**: External service availability
- **SNS Topic**: Target topic must exist and be accessible

### Backup Plans
- **DLQ Implementation**: Capture failed events for replay (Phase 5)
- **EventBridge Fallback**: Alternative routing for webhook failures (Phase 6)
- **Manual Replay**: Logging sufficient for manual event replay

## 7. Testing Strategy

### Current Coverage ✅
- Forwarders/Handlers: High coverage on critical paths
- Benchmarks included for fan-out scenarios

### Test Categories
1. **Unit Tests**: Individual component behavior
   - Handler return type verification
   - Event type detection accuracy
   - Health check filtering logic
   - Retry mechanism behavior

2. **Integration Tests**: Component interaction
   - End-to-end event processing
   - AWS SDK integration
   - Error propagation paths

3. **Performance Tests**: Load and timing
   - 55 events/second sustained load
   - <100ms ALB response time
   - Memory usage under load
   - Micro-benchmarks for `ForwardToWebhooks` (1/5/10 targets)

4. **Chaos Tests**: Failure scenarios
   - Webhook endpoint failures
   - SNS publish failures
   - Invalid event formats

### Testing Commands
```bash
make test           # Run all tests
make test-coverage  # Generate coverage report
make benchmark      # Performance benchmarks
```

## 8. Documentation Requirements

### Completed ✅
- [x] **Setup Guide**: `README.md` with local development
- [x] **Build Commands**: `Makefile` with clear targets
- [x] **Architecture**: This document

### Needed 📋
- [ ] **Configuration**: `.env.example` with all variables
- [ ] **API Documentation**: Event formats and response schemas
- [ ] **Runbook**: Deployment, monitoring, troubleshooting


## 9. Open Questions

- [ ] **Question 1**: Should we implement webhook signature validation? (Security vs Complexity)
- [ ] **Question 2**: ARM64 vs x86_64 for cost optimization? (Performance impact?)
- [ ] **Question 3**: EventBridge vs SQS for DLQ implementation? (Cost vs Features)
- [ ] **Question 4**: Dynamic webhook configuration via Parameter Store? (Flexibility vs Simplicity)

## 10. Session Notes

### Session 1 (2024-09-29)
- **Completed**: Full implementation from memory context
- **Achievements**:
  - Core architecture implemented with clean Go structure
  - All critical tests passing with >90% coverage
  - Lambda deployment package successfully built
  - Performance requirements met in design
- **Next Priority**: Phase 5 operational excellence tasks

### Session 2 (2025-09-30)
- **Completed**: Context Propagation and Request Correlation Implementation
- **Achievements**:
  - Fixed critical context usage bug in `alb_processor.go:Process()` (line 94)
  - Implemented comprehensive request correlation with auto-generated request IDs
  - Added distributed tracing support with `X-Request-ID` and `X-Trace-ID` headers
  - Enhanced webhook forwarder with context-aware timeout management
  - Implemented graceful shutdown mechanism for ALB processor workers
  - Added context cancellation support throughout async operations
  - Created comprehensive test suite for context functionality (`context_test.go`)
  - Maintained 100% test pass rate and backward compatibility
- **Technical Impact**:
  - Enhanced observability: Every request now traceable across async operations
  - Improved operational control: Graceful shutdown prevents request loss
  - Better debugging: Request correlation enables end-to-end tracing
  - Production readiness: Proper context handling for timeouts and cancellation
- **Next Priority**: Phase 5 operational excellence tasks (CloudWatch EMF, performance benchmarking)

### Session 3 (TBD)
- **Focus**: CloudWatch monitoring and DLQ implementation
- **Goals**: Production monitoring and failure handling

### Session 4 (TBD)
- **Focus**: Performance optimization and cost analysis
- **Goals**: ARM64 migration and benchmark validation

---

## Quick Reference

### Key Commands
```bash
make test           # Run tests
make lambda-build   # Build deployment package
make test-coverage  # Generate coverage report
make clean         # Clean build artifacts
```

### Critical Environment Variables
```bash
SNS_TOPIC_ARN=arn:aws:sns:us-east-1:123456789012:topic
WEBHOOK_URLS=https://url1.com,https://url2.com,https://url3.com
SKIP_HEALTH_CHECKS=true
```

### Current Status: ✅ PRODUCTION READY+
The lambda-bridge implementation is complete and enhanced with enterprise-grade features including:
- **Request Correlation**: Full traceability across async operations
- **Context Propagation**: Proper timeout and cancellation handling
- **Graceful Shutdown**: Clean worker termination prevents request loss
- **Enhanced Observability**: Distributed tracing headers for downstream systems

Ready for AWS Lambda deployment with production reliability and operational excellence.
