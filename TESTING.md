# Testing Guide

This document provides comprehensive information about testing the Lambda Bridge application.

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Test Structure](#test-structure)
- [Running Tests](#running-tests)
- [Test Types](#test-types)
- [Writing Tests](#writing-tests)
- [Test Coverage](#test-coverage)
- [CI/CD Integration](#cicd-integration)
- [Troubleshooting](#troubleshooting)

## Overview

Lambda Bridge uses a multi-layered testing strategy to ensure reliability and correctness:

- **Unit Tests**: Test individual components in isolation
- **Integration Tests**: Test component interactions and end-to-end flows
- **Acceptance Tests**: BDD-style tests using Godog for behavior specification
- **Benchmarks**: Performance testing for critical paths

### Test Statistics

```
📊 Test Coverage
├── Unit Tests:        ~85% coverage
├── Integration Tests: 8 scenarios
├── Acceptance Tests:  26 scenarios, 113 steps
└── Total Test Files:  5 packages
```

## Prerequisites

### Required Tools

```bash
# Go 1.21 or higher
go version

# Make (for running test commands)
make --version

# Optional: Coverage visualization
go install golang.org/x/tools/cmd/cover@latest

# Optional: Linting
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Optional: Security scanning
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

### AWS Dependencies

For integration tests that interact with AWS services:

```bash
# AWS CLI configured (for local testing)
aws configure

# Or set environment variables
export AWS_REGION=us-east-1
export AWS_ACCESS_KEY_ID=your_key
export AWS_SECRET_ACCESS_KEY=your_secret
```

## Test Structure

```
lambda-bridge/
├── acceptance/              # BDD acceptance tests
│   ├── features/           # Gherkin feature files
│   │   ├── burst_handling.feature
│   │   ├── failure_recovery.feature
│   │   ├── health_checks.feature
│   │   ├── routing.feature
│   │   └── webhook_forwarding.feature
│   ├── features_test.go    # Godog test runner
│   └── step_definitions.go # Step implementations
│
├── test/                   # Integration tests
│   └── integration_test.go # End-to-end scenarios
│
└── internal/              # Unit tests
    ├── config/
    │   └── config_test.go
    ├── forwarder/
    │   └── forwarder_test.go
    └── handler/
        └── handler_test.go
```

## Running Tests

### Quick Start

```bash
# Run all tests
make test

# Run tests with coverage
make test-coverage

# Run benchmarks
make test-bench
```

### Detailed Commands

#### All Tests

```bash
# Run all tests (unit + integration + acceptance)
go test ./...

# Run with verbose output
go test -v ./...

# Run with race detector
go test -race ./...
```

#### Unit Tests Only

```bash
# Run all unit tests
go test ./internal/...

# Run specific package
go test ./internal/config
go test ./internal/forwarder
go test ./internal/handler

# Run specific test
go test -v -run TestConfig_Validate ./internal/config
```

#### Integration Tests Only

```bash
# Run all integration tests
go test ./test

# Run with verbose output
go test -v ./test

# Run specific integration test
go test -v -run TestIntegration_EndToEndALBFlow ./test
```

#### Acceptance Tests Only

```bash
# Run all BDD acceptance tests
go test ./acceptance

# Run with verbose output
go test -v ./acceptance

# Run specific feature
go test ./acceptance -godog.tags="@health_checks"
```

### Coverage Reports

```bash
# Generate coverage report
make test-coverage

# View HTML coverage report
open coverage.html

# Or manually:
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html

# View coverage summary
go tool cover -func=coverage.out

# Coverage for specific package
go test -coverprofile=coverage.out ./internal/handler
go tool cover -html=coverage.out
```

### Benchmarks

```bash
# Run all benchmarks
make test-bench

# Or manually:
go test -bench=. -benchmem ./...

# Run specific benchmark
go test -bench=BenchmarkWebhookForwarding -benchmem ./internal/forwarder

# Save benchmark results for comparison
go test -bench=. -benchmem ./... > bench_old.txt
# ... make changes ...
go test -bench=. -benchmem ./... > bench_new.txt
benchcmp bench_old.txt bench_new.txt
```

## Test Types

### 1. Unit Tests

Unit tests validate individual functions and components in isolation.

**Location**: `internal/*/` alongside source code
**Naming**: `*_test.go`
**Framework**: Go standard `testing` package

**Example**: `internal/config/config_test.go`

```go
func TestConfig_Validate(t *testing.T) {
    tests := []struct {
        name    string
        config  Config
        wantErr bool
    }{
        {
            name: "valid config",
            config: Config{
                SNSTopicArn: "arn:aws:sns:...",
                WebhookURLs: []string{"https://example.com"},
            },
            wantErr: false,
        },
        // ... more test cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := tt.config.Validate()
            if (err != nil) != tt.wantErr {
                t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

**Key Areas**:
- Configuration loading and validation (`config_test.go`)
- Webhook forwarding logic (`forwarder_test.go`)
- Request handling and routing (`handler_test.go`)

### 2. Integration Tests

Integration tests validate component interactions and end-to-end flows.

**Location**: `test/integration_test.go`
**Framework**: Go standard `testing` package + `httptest`

**Example Scenarios**:
- End-to-end ALB event flow
- Concurrent burst traffic handling
- Multi-route webhook distribution
- Failure recovery with retries
- Health check filtering
- Graceful shutdown

```go
func TestIntegration_EndToEndALBFlow(t *testing.T) {
    // Setup webhook server
    server := httptest.NewServer(...)
    defer server.Close()

    // Create handler with config
    handler := setupHandler(server.URL)

    // Send ALB event
    response := handler.Handle(ctx, albEvent)

    // Verify response and webhook received event
    assert.Equal(t, 200, response.StatusCode)
    assert.True(t, webhookReceived)
}
```

### 3. Acceptance Tests (BDD)

Acceptance tests use Gherkin syntax to describe behavior from a user perspective.

**Location**: `acceptance/`
**Framework**: [Godog](https://github.com/cucumber/godog)
**Files**:
- `features/*.feature` - Gherkin feature files
- `step_definitions.go` - Step implementations
- `features_test.go` - Test runner

**Example Feature** (`features/webhook_forwarding.feature`):

```gherkin
Feature: Webhook Forwarding
  As a Lambda Bridge service
  I want to forward ALB events to configured webhook endpoints
  So that downstream systems can process incoming requests

  Scenario: Successfully forward ALB event to webhook
    Given I have a webhook server listening
    When I receive an ALB event with payload:
      """
      {"event": "push", "repository": "test-repo"}
      """
    Then the Lambda should return status code 200 immediately
    And the response body should contain "accepted"
    And the webhook should receive the event within 1 second
```

**Running Specific Features**:

```bash
# Run all acceptance tests
go test ./acceptance

# Run with Godog format
GODOG_FORMAT=pretty go test ./acceptance

# Available formats: pretty, progress, cucumber, junit
```

### 4. Benchmark Tests

Benchmarks measure performance of critical code paths.

**Example**:

```go
func BenchmarkWebhookForwarding(b *testing.B) {
    forwarder := NewWebhookForwarder()
    payload := []byte(`{"test": "data"}`)

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        forwarder.ForwardToWebhooks(ctx, urls, payload)
    }
}
```

**Reading Benchmark Results**:

```
BenchmarkWebhookForwarding-8    5000    234567 ns/op    1024 B/op    15 allocs/op
                           │      │          │            │           │
                           │      │          │            │           └─ allocations per operation
                           │      │          │            └─ bytes allocated per operation
                           │      │          └─ nanoseconds per operation
                           │      └─ number of iterations
                           └─ GOMAXPROCS value
```

## Writing Tests

### Unit Test Guidelines

1. **Use Table-Driven Tests**:

```go
func TestSomething(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
        wantErr  bool
    }{
        {"valid input", "test", "TEST", false},
        {"empty input", "", "", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := Transform(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("wanted error: %v, got: %v", tt.wantErr, err)
            }
            if result != tt.expected {
                t.Errorf("wanted: %v, got: %v", tt.expected, result)
            }
        })
    }
}
```

2. **Test Error Cases**:

```go
func TestConfig_Validate_Errors(t *testing.T) {
    tests := []struct {
        name      string
        config    Config
        wantError string
    }{
        {
            name:      "missing SNS topic",
            config:    Config{WebhookURLs: []string{"url"}},
            wantError: "SNS topic ARN is required",
        },
    }
    // ...
}
```

3. **Use Subtests for Organization**:

```go
func TestHandler(t *testing.T) {
    t.Run("ALB events", func(t *testing.T) {
        t.Run("health check", func(t *testing.T) {
            // Test health check handling
        })
        t.Run("webhook forwarding", func(t *testing.T) {
            // Test webhook forwarding
        })
    })
}
```

### Integration Test Guidelines

1. **Use httptest for HTTP servers**:

```go
server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
}))
defer server.Close()
```

2. **Clean up resources**:

```go
func TestIntegration(t *testing.T) {
    // Setup
    handler := setupHandler()
    defer handler.Shutdown(5 * time.Second)

    // Test
    // ...
}
```

3. **Test concurrent scenarios**:

```go
func TestConcurrentRequests(t *testing.T) {
    var wg sync.WaitGroup
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            // Send request
        }()
    }
    wg.Wait()
}
```

### BDD Test Guidelines

1. **Write descriptive scenarios**:

```gherkin
Scenario: User receives error on invalid input
  Given I have a webhook server listening
  When I send an invalid ALB event
  Then I should receive a 400 error
  And the error message should be clear
```

2. **Keep scenarios focused**:
   - One scenario per behavior
   - Use Background for common setup
   - Avoid scenario outlines unless testing multiple similar cases

3. **Implement reusable steps**:

```go
func (tc *TestContext) iHaveAWebhookServerListening() error {
    return tc.createWebhookServer("default", func(w http.ResponseWriter, r *http.Request) {
        tc.recordWebhookRequest("default", r)
        w.WriteHeader(http.StatusOK)
    })
}
```

## Test Coverage

### Current Coverage

```bash
# Generate and view current coverage
make test-coverage
open coverage.html
```

### Coverage Goals

- **Overall**: 80%+ coverage
- **Critical paths**: 90%+ coverage (handlers, forwarders)
- **Configuration**: 85%+ coverage
- **Happy paths**: 100% coverage
- **Error paths**: 80%+ coverage

### Improving Coverage

```bash
# Find uncovered code
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | grep -v "100.0%"

# Focus on critical packages
go test -coverprofile=coverage.out ./internal/handler
go tool cover -html=coverage.out
```

## CI/CD Integration

### GitHub Actions Example

```yaml
name: Tests
on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.21'

      - name: Run tests
        run: make test

      - name: Generate coverage
        run: make test-coverage

      - name: Upload coverage
        uses: codecov/codecov-action@v3
        with:
          file: ./coverage.out
```

### Pre-commit Hooks

```bash
# .git/hooks/pre-commit
#!/bin/bash
make test
if [ $? -ne 0 ]; then
    echo "Tests failed. Commit aborted."
    exit 1
fi
```

## Troubleshooting

### Common Issues

#### Tests Hang or Timeout

**Problem**: Tests don't complete
**Solution**: Check for:
- Missing `defer server.Close()` in httptest servers
- Goroutines waiting for channels
- Missing context timeouts

```go
// Add timeouts to tests
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
```

#### Race Conditions

**Problem**: `go test -race` reports data races
**Solution**: Protect shared state with mutexes

```go
type SafeCounter struct {
    mu    sync.Mutex
    count int
}

func (c *SafeCounter) Inc() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.count++
}
```

#### Flaky Tests

**Problem**: Tests pass/fail intermittently
**Solution**:
- Add retry logic for timing-dependent tests
- Use channels instead of sleeps
- Increase timeout values

```go
// Instead of sleep, wait for condition
timeout := time.After(2 * time.Second)
ticker := time.NewTicker(50 * time.Millisecond)
defer ticker.Stop()

for {
    select {
    case <-timeout:
        t.Fatal("timeout waiting for condition")
    case <-ticker.C:
        if condition() {
            return
        }
    }
}
```

#### Coverage Not Updating

**Problem**: Coverage report shows old data
**Solution**:

```bash
# Clean test cache
go clean -testcache

# Regenerate coverage
make test-coverage
```

### Debug Test Failures

```bash
# Run single test with verbose output
go test -v -run TestSpecificTest ./internal/handler

# Show test output even on success
go test -v ./...

# Run with debug logging
DEBUG=true go test -v ./...

# Print stack traces on failure
go test -v -trace ./...
```

### Performance Issues

```bash
# Profile CPU usage
go test -cpuprofile=cpu.prof ./...
go tool pprof cpu.prof

# Profile memory usage
go test -memprofile=mem.prof ./...
go tool pprof mem.prof

# Profile blocking operations
go test -blockprofile=block.prof ./...
go tool pprof block.prof
```

## Best Practices

### General

- ✅ Run tests before committing
- ✅ Write tests for new features
- ✅ Update tests when modifying code
- ✅ Keep tests fast (< 10 seconds for unit tests)
- ✅ Use meaningful test names
- ✅ Test both success and error cases
- ✅ Clean up resources (defer close/cleanup)
- ✅ Use table-driven tests for multiple cases

### Don'ts

- ❌ Don't test implementation details
- ❌ Don't use sleep for synchronization
- ❌ Don't ignore race detector warnings
- ❌ Don't skip flaky tests (fix them)
- ❌ Don't commit code without tests
- ❌ Don't use global state in tests
- ❌ Don't hardcode timeouts (use constants)

## Additional Resources

- [Go Testing Documentation](https://golang.org/pkg/testing/)
- [Godog Documentation](https://github.com/cucumber/godog)
- [Table Driven Tests](https://github.com/golang/go/wiki/TableDrivenTests)
- [Advanced Testing](https://about.sourcegraph.com/go/advanced-testing-in-go)

## Support

For questions about testing:
1. Check this guide
2. Review existing tests for examples
3. See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidelines
4. Open an issue for test-related bugs
