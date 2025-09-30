# Memory: TESTING_VALIDATION

## Critical Test Cases

### TestHandlerReturnType
- Verifies handler returns `any` type
- ALB events → ALBTargetGroupResponse
- SNS success → nil
- SNS failure → error
- Unknown events → ALB response

### TestALBAlwaysReturns200
- **Most critical test**
- Tests all webhooks succeed → 200
- Tests partial failures → 200
- Tests all failures → 200
- Tests timeouts → 200

### TestHealthCheckDetection
- ELB-HealthChecker/2.0 detection
- /health, /healthz, /ping paths
- Verifies no webhook forwarding
- Returns healthy response

### TestWebhookParallelProcessing
- Verifies concurrent execution
- Should complete in ~30ms not 60ms
- Tests webhook isolation

## Test Commands
```bash
# Run all tests
go test -v ./...

# Critical tests only
go test -v -run "TestHandlerReturnType|TestALBAlwaysReturns200" ./internal/handler/

# Benchmarks
go test -bench=. -benchmem ./internal/handler/
```

## Expected Results
- Handler tests: 100% pass
- ALB 200: Must always pass
- Benchmarks: ~10μs per event
- Coverage: >80% for critical paths

## Mock Patterns
- MockSNSForwarder with ForwardFunc
- MockWebhookForwarder with Results map
- httptest.NewServer for webhook testing
- atomic.Bool for concurrency checks