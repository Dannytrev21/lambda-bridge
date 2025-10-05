# Lambda Bridge Integration Tests

This directory contains comprehensive integration and component tests for the Lambda Bridge application.

## Overview

The integration test suite validates the complete end-to-end functionality of the Lambda Bridge, including:
- ALB event handling and webhook forwarding
- Concurrent request processing and burst handling
- Multi-route webhook distribution
- Failure recovery and retry mechanisms
- URL isolation and non-blocking behavior
- Health check filtering
- Graceful shutdown

## Test Suite Summary

### Total Test Coverage
- **Total Tests**: 76 tests across all packages
- **Integration Tests**: 11 comprehensive integration tests
- **Coverage**:
  - Config package: 92.1%
  - Forwarder package: 80.3%
  - Handler package: 90.9%

## Integration Tests

### 1. End-to-End ALB Flow (`TestIntegration_EndToEndALBFlow`)
**Purpose**: Validates the complete flow from ALB event receipt to webhook delivery

**What it tests**:
- ALB event parsing and handling
- Request ID generation and propagation
- Webhook payload delivery
- HTTP headers (Content-Type, User-Agent, X-Request-ID)
- Immediate Lambda response (non-blocking)
- Async webhook forwarding

**Key assertions**:
- Lambda returns 200 status immediately
- Webhook receives the full ALB event payload
- All required headers are present and correct

---

### 2. Concurrent Burst Traffic (`TestIntegration_ConcurrentBurstTraffic`)
**Purpose**: Tests system behavior under high concurrent load

**What it tests**:
- 100 concurrent requests submitted simultaneously
- Worker pool processing with queue buffering
- Non-blocking Lambda responses
- Request latency under load
- Delivery rate and throughput

**Key metrics measured**:
- Average, min, max request latency
- Total requests processed
- Webhook delivery rate (should be ≥90%)
- Throughput (requests/second)

**Key assertions**:
- Average latency < 100ms (verifies non-blocking behavior)
- At least 90% of webhooks delivered
- All requests receive immediate 200 response

---

### 3. Multi-Route Distribution (`TestIntegration_MultiRouteDistribution`)
**Purpose**: Validates webhook routing based on request headers

**What it tests**:
- Default route (no special headers)
- Cloud route (x-dcp-destination-host header)
- Enterprise route (x-github-enterprise-host header)
- Correct webhook server selection

**Test scenarios**:
1. Request without special headers → default webhooks
2. Request with `x-dcp-destination-host` → cloud webhooks
3. Request with `x-github-enterprise-host` → enterprise webhooks

**Key assertions**:
- Each route receives exactly the expected webhooks
- Other routes receive zero webhooks
- Routing is case-insensitive

---

### 4. Failure Recovery (`TestIntegration_FailureRecovery`)
**Purpose**: Tests resilience to webhook endpoint failures

**What it tests**:
- Multiple webhook servers with different behaviors:
  - Always-successful server
  - Always-failing server (5xx errors)
  - Flakey server (fails twice, then succeeds)
- Retry logic with exponential backoff
- Failure isolation (one webhook failure doesn't block others)

**Key assertions**:
- Successful server receives webhook (1 attempt)
- Failing server is retried (4 attempts: initial + 3 retries)
- Flakey server recovers after retries (≥3 attempts)
- Lambda still returns 200 despite webhook failures

---

### 5. URL Isolation (`TestIntegration_URLIsolation`)
**Purpose**: Verifies that slow/failing URLs don't block fast URLs

**What it tests**:
- Fast webhook server (immediate response)
- Slow webhook server (2-second delay)
- Parallel execution of webhook deliveries
- Independent processing of each URL

**Key assertions**:
- Both webhooks are attempted
- Fast and slow webhooks start nearly simultaneously (<500ms difference)
- Slow webhook doesn't block fast webhook

---

### 6. Health Check Filtering (`TestIntegration_HealthCheckFiltering`)
**Purpose**: Tests health check detection and filtering

**What it tests**:
- ELB health checker user-agent detection
- Health check path detection (/health, /healthz, /ping)
- Health check responses don't trigger webhooks
- Regular requests do trigger webhooks

**Key assertions**:
- Health checks return 200 with "healthy" body
- Health checks don't forward to webhooks
- Regular requests forward to webhooks normally

---

### 7. Mixed Traffic Scenario (`TestIntegration_MixedTrafficScenario`)
**Purpose**: Simulates realistic production traffic patterns

**What it tests**:
- 5 concurrent traffic generators
- 3-second sustained load
- Mixed traffic types:
  - 20% health checks
  - 20% cloud webhooks
  - 60% default webhooks
- Variable latency (10-30ms)
- Unreliable server (30% failure rate)
- Variable request rate (50-150ms intervals)

**Metrics reported**:
- Total requests processed
- Health check percentage
- Webhook deliveries by type
- Failed deliveries
- Overall throughput (req/s)

**Key assertions**:
- System handles sustained mixed traffic
- Throughput is reasonable (typically 40-60 req/s)
- All traffic types are processed

---

### 8. Graceful Shutdown (`TestIntegration_GracefulShutdown`)
**Purpose**: Tests handler shutdown and queue draining

**What it tests**:
- Enqueuing multiple jobs
- Initiating graceful shutdown
- Work queue draining
- In-flight job completion

**Key assertions**:
- Shutdown completes without error
- Most/all jobs are processed before shutdown
- Queue is properly drained

---

## Running the Tests

### Run all integration tests
```bash
go test ./test/... -v
```

### Run with timeout (recommended)
```bash
go test ./test/... -v -timeout 60s
```

### Run all tests with coverage
```bash
go test ./... -cover
```

### Generate coverage report
```bash
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html
```

### Run specific integration test
```bash
go test ./test -v -run TestIntegration_EndToEndALBFlow
```

### Run with race detection
```bash
go test ./test/... -race
```

## Benchmark Tests

The suite includes a benchmark for end-to-end throughput:

```bash
go test ./test -bench=BenchmarkIntegration_EndToEndThroughput -benchmem
```

## Test Architecture

### Mock SNS Forwarder
The integration tests use a `mockSNSForwarder` that implements the `SNSForwarder` interface:
- Captures forwarded messages for verification
- Can simulate failures (shouldFail flag)
- Thread-safe with mutex protection

### Test Servers
Tests use `httptest.Server` to create mock webhook endpoints:
- Configurable response codes
- Variable latency simulation
- Request tracking and verification
- Concurrent request handling

### Atomic Counters
Tests use `atomic.Int32` and `atomic.Int64` for thread-safe counting:
- Webhook delivery tracking
- Attempt counting for retries
- Request counting under load

### Synchronization
Tests use proper synchronization primitives:
- `sync.WaitGroup` for concurrent operations
- `sync.Mutex` for protecting shared data
- Time-based waits for async operations

## Test Data

Tests use realistic test data:
- Valid ALB event structures
- Realistic webhook payloads
- Appropriate headers and metadata
- ARN formats following AWS conventions

## Expected Test Duration

- **Integration tests alone**: ~20 seconds
- **All tests**: ~35 seconds
- Individual test durations:
  - End-to-end: ~1s
  - Burst traffic: ~5s
  - Failure recovery: ~3s
  - Mixed traffic: ~5s
  - Others: <1s each

## Key Insights from Tests

### 1. Non-Blocking Behavior
Lambda responds in <100ms while webhooks process asynchronously in the background.

### 2. Burst Handling
With queue size 200 and 25 workers, the system handles 100+ concurrent requests efficiently.

### 3. URL Isolation
Each webhook URL is processed in its own goroutine, preventing cascading failures.

### 4. Retry Resilience
Failed webhooks are retried up to 3 times with exponential backoff, improving delivery reliability.

### 5. Health Check Efficiency
Health checks are filtered early, preventing unnecessary webhook forwards.

## Future Enhancements

Potential additions to the test suite:
- [ ] SNS integration tests with LocalStack
- [ ] Performance regression tests
- [ ] Chaos engineering scenarios
- [ ] Memory leak detection
- [ ] Long-running stability tests
- [ ] CloudWatch metrics validation

## Contributing

When adding new tests:
1. Follow existing naming conventions (`TestIntegration_*`)
2. Add comprehensive comments explaining what is being tested
3. Use atomic counters for concurrency safety
4. Include realistic test data
5. Verify tests are deterministic (not flaky)
6. Add documentation to this README

## Troubleshooting

### Tests timing out
Increase timeout: `go test ./test/... -timeout 120s`

### Flaky tests
Some tests involve async operations with time-based waits. If tests are flaky:
- Increase sleep durations in tests
- Check system load
- Run tests sequentially: `go test ./test/... -p 1`

### Coverage gaps
To identify untested code paths:
```bash
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out | grep -v 100.0%
```
