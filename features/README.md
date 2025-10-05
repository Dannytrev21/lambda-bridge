# Lambda Bridge BDD Tests

This directory contains Behavior-Driven Development (BDD) tests for the Lambda Bridge application using [Godog](https://github.com/cucumber/godog), the official Cucumber BDD framework for Go.

## Overview

BDD tests are written in Gherkin syntax (Given/When/Then) and provide human-readable specifications that double as automated tests. These tests focus on business behavior and user outcomes rather than implementation details.

## Directory Structure

```
features/
├── webhook_forwarding.feature     # Core webhook forwarding behavior
├── health_checks.feature          # Health check filtering
├── routing.feature                # Multi-route webhook distribution
├── burst_handling.feature         # Concurrency and burst handling
├── failure_recovery.feature       # Retry logic and failure recovery
├── step_definitions.go            # Go implementation of test steps
├── features_test.go               # Test runner
└── README.md                      # This file
```

## Feature Files

### 1. webhook_forwarding.feature (6 scenarios)
Tests the core functionality of receiving ALB events and forwarding them to webhook endpoints.

**Key Scenarios**:
- Successfully forward ALB event to webhook
- Forward event with request ID tracking
- Lambda returns immediately without blocking
- Forward to multiple webhook URLs
- Handle empty webhook URLs gracefully
- Preserve complete ALB event structure

**Business Value**: Ensures reliable event delivery to downstream systems with proper metadata tracking.

---

### 2. health_checks.feature (8 scenarios)
Tests the system's ability to detect and filter ALB health checks to prevent overwhelming webhook endpoints.

**Key Scenarios**:
- Detect health check by user agent (ELB-HealthChecker)
- Detect health check by path (/health, /healthz, /ping)
- Case-insensitive health check detection
- Regular requests should forward despite health check paths
- Scenario outlines for various health check paths

**Business Value**: Reduces unnecessary traffic to webhook endpoints and prevents false positives in monitoring.

---

### 3. routing.feature (8 scenarios)
Tests header-based routing to different webhook destinations (default, cloud, enterprise).

**Key Scenarios**:
- Route to default webhooks (no special headers)
- Route to cloud webhooks (x-dcp-destination-host)
- Route to enterprise webhooks (x-github-enterprise-host)
- Case-insensitive header matching
- Priority of routing headers (cloud takes precedence)
- Fallback to default when specific URLs not configured
- Multiple webhooks per route

**Business Value**: Enables multi-tenant architectures and flexible event routing based on request metadata.

---

### 4. burst_handling.feature (9 scenarios + scenario outline)
Tests the system's ability to handle high concurrent load and burst traffic efficiently.

**Key Scenarios**:
- Handle moderate burst (50 concurrent requests)
- Handle large burst within queue capacity (100 requests)
- Queue overflow with excessive burst (250 requests)
- Worker pool processes jobs concurrently
- Burst smoothing with queue buffering
- Queue size monitoring
- Graceful degradation under extreme load
- Fair processing across multiple webhook URLs
- Performance under various load levels (scenario outline)

**Business Value**: Ensures system stability and responsiveness under production traffic patterns including sudden spikes.

---

### 5. failure_recovery.feature (11 scenarios + scenario outline)
Tests retry logic, exponential backoff, and failure isolation mechanisms.

**Key Scenarios**:
- Retry on webhook server error (5xx)
- No retry on client error (4xx)
- Exponential backoff between retries
- URL isolation during failures
- Lambda doesn't block waiting for retries
- Maximum retry limit respected
- Jitter in retry backoff (prevents thundering herd)
- Timeout handling
- Success on final retry attempt
- Mixed success and failure across multiple URLs
- Retry behavior for different HTTP status codes (scenario outline)

**Business Value**: Maximizes event delivery reliability while preventing cascade failures and system overload.

---

## Running BDD Tests

### Run all scenarios
```bash
go test ./features -v
```

### Run specific feature
```bash
go test ./features -v -godog.tags=@webhook_forwarding
```

### Run with different output format
```bash
GODOG_FORMAT=progress go test ./features -v
GODOG_FORMAT=cucumber go test ./features -v
```

### Run and generate report
```bash
go test ./features -v -godog.format=cucumber:cucumber.json
```

### Available formats
- `pretty` - Colored output with step details (default)
- `progress` - Dots for steps
- `junit` - JUnit XML output
- `cucumber` - Cucumber JSON output

## Writing New Scenarios

### 1. Add scenario to feature file

```gherkin
Feature: My Feature

  Scenario: My new scenario
    Given I have some precondition
    When I perform some action
    Then I should see some result
```

### 2. Implement step definitions

Add step implementations to `step_definitions.go`:

```go
sc.Step(`^I have some precondition$`, ctx.iHaveSomePrecondition)

func (tc *TestContext) iHaveSomePrecondition() error {
    // Implementation
    return nil
}
```

### 3. Run tests to verify

```bash
go test ./features -v -run "TestFeatures/My_new_scenario"
```

## Gherkin Syntax Guide

### Given (Preconditions)
Sets up the initial state before the action.
```gherkin
Given the Lambda Bridge is configured with webhook URLs
Given I have a webhook server listening
Given the Lambda Bridge has health check filtering enabled
```

### When (Actions)
Describes the action being tested.
```gherkin
When I receive an ALB event
When I receive 50 concurrent ALB events
When I receive an ALB event with header "User-Agent" = "ELB-HealthChecker"
```

### Then (Assertions)
Verifies the expected outcome.
```gherkin
Then the Lambda should return status code 200
Then the webhook should receive the event within 1 second
Then the webhook should receive header "Content-Type" with value "application/json"
```

### And/But
Combines multiple steps of the same type.
```gherkin
Given I have a webhook server listening
And the Lambda is configured with all 3 webhook URLs
When I receive an ALB event
Then all 3 webhooks should receive the event
And each webhook should receive the same payload
```

### Scenario Outlines (Data-driven tests)
```gherkin
Scenario Outline: Various health check paths
  Given I have a webhook server listening
  When I receive an ALB event to path "<path>"
  Then the webhook should NOT receive any request

  Examples:
    | path     |
    | /health  |
    | /healthz |
    | /ping    |
```

### Tables
```gherkin
When I receive an ALB event with:
  | field      | value         |
  | httpMethod | POST          |
  | path       | /webhook      |
  | body       | {"test":"data"} |
```

### Doc Strings
```gherkin
When I receive an ALB event with payload:
  """
  {
    "event": "push",
    "repository": "test-repo"
  }
  """
```

## Test Context

The `TestContext` struct maintains state between steps:

```go
type TestContext struct {
    // Configuration
    config  *config.Config
    handler *handler.Handler

    // Test servers
    webhookServers map[string]*httptest.Server

    // Request/Response tracking
    lastALBEvent    events.ALBTargetGroupRequest
    lastResponse    interface{}
    responseTime    time.Duration

    // Webhook tracking
    webhookRequests map[string][]*http.Request
    webhookBodies   map[string][]string
    webhookHeaders  map[string][]http.Header
    webhookAttempts map[string]*atomic.Int32
}
```

## Benefits of BDD

### 1. Living Documentation
Feature files serve as up-to-date documentation that's always in sync with the code.

### 2. Collaboration
Non-technical stakeholders can read and understand test scenarios.

### 3. Behavior Focus
Tests describe *what* the system does, not *how* it does it.

### 4. Regression Safety
Comprehensive scenarios prevent regressions when refactoring.

### 5. Design Tool
Writing scenarios before code helps clarify requirements.

## Best Practices

### Write Declarative Steps
❌ **Bad** (Imperative - too implementation-focused):
```gherkin
Given I create a new HTTP server on port 8080
And I start the Lambda handler with config file "test.yml"
When I send a POST request to "/webhook" with body '{"test":"data"}'
Then I check the webhook_requests array has length 1
```

✅ **Good** (Declarative - behavior-focused):
```gherkin
Given I have a webhook server listening
When I receive an ALB event
Then the webhook should receive the event
```

### One Scenario, One Behavior
Each scenario should test exactly one business behavior.

### Use Background for Common Setup
```gherkin
Background:
  Given the Lambda Bridge is configured with webhook URLs
  And health check filtering is enabled
```

### Make Scenarios Independent
Each scenario should work in isolation and not depend on previous scenarios.

### Use Meaningful Scenario Names
❌ `Scenario: Test 1`
✅ `Scenario: Lambda returns immediately without blocking`

## Troubleshooting

### Undefined steps
```
Step undefined: I have something
```
**Solution**: Implement the step in `step_definitions.go`

### Panic during tests
Check that TestContext is properly initialized in Before hook and cleaned up in After hook.

### Tests timeout
Increase timeout: `go test ./features -v -timeout 60s`

### Step not matching
Check regex pattern in step definition:
```go
sc.Step(`^I receive (\d+) concurrent ALB events$`, ...)  // Matches numbers
sc.Step(`^I receive an ALB event with header "([^"]*)"$`, ...)  // Matches quoted strings
```

## CI/CD Integration

### GitHub Actions Example
```yaml
- name: Run BDD Tests
  run: |
    go test ./features -v -timeout 60s
    go test ./features -godog.format=cucumber:cucumber.json

- name: Upload Test Results
  uses: actions/upload-artifact@v2
  with:
    name: cucumber-results
    path: cucumber.json
```

### Generate HTML Report
```bash
go test ./features -godog.format=cucumber:cucumber.json
# Use cucumber-html-reporter or similar tool to generate HTML from JSON
```

## Comparison with Unit/Integration Tests

| Aspect | Unit Tests | Integration Tests | BDD Tests |
|--------|------------|-------------------|-----------|
| **Focus** | Individual functions | Component interactions | Business behavior |
| **Audience** | Developers | Developers | Everyone |
| **Language** | Go | Go | Gherkin |
| **Speed** | Very fast | Fast | Moderate |
| **Scope** | Single function | Multiple components | End-to-end behavior |
| **When to use** | TDD, refactoring | API testing | Requirements validation |

## Coverage

Current BDD test coverage:

- ✅ **Webhook Forwarding**: 6 scenarios
- ✅ **Health Check Filtering**: 8 scenarios
- ✅ **Multi-Route Distribution**: 8 scenarios
- ✅ **Burst Handling**: 10 scenarios
- ✅ **Failure Recovery**: 12 scenarios

**Total**: 44 scenarios covering all major business behaviors

## Future Enhancements

Potential additions:
- [ ] SNS forwarding scenarios
- [ ] Metrics and monitoring scenarios
- [ ] Configuration validation scenarios
- [ ] Error message validation
- [ ] Performance benchmarking scenarios
- [ ] Security scenarios (authentication, rate limiting)

## Resources

- [Godog Documentation](https://github.com/cucumber/godog)
- [Gherkin Reference](https://cucumber.io/docs/gherkin/reference/)
- [BDD Best Practices](https://cucumber.io/docs/bdd/)
- [Writing Good Gherkin](https://cucumber.io/docs/bdd/better-gherkin/)

## Contributing

When adding new scenarios:
1. Write the scenario in Gherkin first
2. Run tests to see which steps are undefined
3. Implement step definitions
4. Verify scenario passes
5. Update this README with new scenarios
