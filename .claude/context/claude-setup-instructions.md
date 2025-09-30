# Lambda Event Forwarder - Complete Setup Instructions for Claude

## Project Overview
I need you to create a Lambda function that forwards events from ALB and SNS to multiple destinations. The handler MUST return `any` type (not `error`) and ALWAYS return HTTP 200 to ALB to prevent webhook retry storms.

## Step 1: Create Project Structure
Please create the following directory structure:
```
lambda-forwarder/
├── main.go
├── internal/
│   ├── handler/
│   │   ├── forwarder_handler.go
│   │   ├── forwarder_handler_test.go
│   │   └── benchmark_test.go
│   └── forwarder/
│       ├── sns_forwarder.go
│       ├── sns_forwarder_test.go
│       ├── webhook_forwarder.go
│       └── webhook_forwarder_test.go
├── test/
│   └── integration/
│       └── integration_test.go
├── .github/
│   └── workflows/
│       └── test.yml
├── go.mod
├── Makefile
├── test.sh
├── .env.example
└── README.md
```

## Step 2: Core Requirements
The implementation MUST follow these critical patterns:

### Handler Signature (CRITICAL)
```go
func (h *ForwarderHandler) Handler(ctx context.Context, rawEvent json.RawMessage) any
```
- Returns `any` (not `interface{}` or `error`)
- For ALB events: returns `events.ALBTargetGroupResponse` with StatusCode 200
- For SNS events: returns error or nil

### ALB Response (ALWAYS 200)
```go
return events.ALBTargetGroupResponse{
    StatusCode: 200,  // ALWAYS 200, never anything else
    Headers: map[string]string{"Content-Type": "application/json"},
    Body: `{"status":"accepted"}`,
    IsBase64Encoded: false,
}
```

## Step 3: Implementation Requirements

### main.go
- Load AWS config once during cold start
- Create handler with dependencies
- Call `lambda.Start(forwarderHandler.Handler)`

### ForwarderHandler Requirements
1. Detect event type (ALB vs SNS) via unmarshaling
2. ALB events: Return 200 immediately, forward to webhooks asynchronously
3. SNS events: Forward to SNS topic, return error/nil for Lambda retry
4. Unknown events: Return ALB 200 response as safe default
5. Health check filtering: Skip forwarding if path is /health, /healthz, /ping or user-agent contains "ELB-HealthChecker"

### Configuration (Environment Variables)
```bash
SNS_TOPIC_ARN=arn:aws:sns:us-east-1:123456789012:topic  # Required
WEBHOOK_URLS=https://url1.com,https://url2.com,https://url3.com  # Required
SKIP_HEALTH_CHECKS=true  # Optional, default: true
ENVIRONMENT=dev  # Optional, default: dev
DEBUG=false  # Optional, default: false
```

### No Authentication
- Lambda does NOT handle any authentication/tokens
- Webhook receivers handle their own authentication
- No auth headers needed in webhook forwarding

### Event Forwarding
- Preserve exact event structure (no transformation)
- SNS events: Forward complete raw event to SNS topic
- ALB events: Forward complete raw event to all webhooks in parallel
- Use goroutines for async processing
- Implement 3 retries with exponential backoff for webhooks

## Step 4: Test Cases to Implement

### Critical Tests (MUST PASS)
1. `TestHandlerReturnType` - Verify handler returns `any` type
2. `TestALBAlwaysReturns200` - Verify ALB always gets 200 even when webhooks fail
3. `TestHealthCheckDetection` - Verify health checks are filtered
4. `TestWebhookParallelProcessing` - Verify webhooks are called in parallel
5. `TestSNSEventForwarding` - Verify SNS events are forwarded correctly

## Step 5: Build and Deployment Files

### Makefile
```makefile
test:
	SNS_TOPIC_ARN=arn:aws:sns:us-east-1:123456789012:test \
	WEBHOOK_URLS=http://test1.com,http://test2.com,http://test3.com \
	go test -v ./...

build:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bootstrap main.go
	zip lambda-deployment.zip bootstrap

clean:
	rm -f bootstrap lambda-deployment.zip
```

### go.mod
```go
module lambda-forwarder

go 1.21

require (
    github.com/aws/aws-lambda-go v1.46.0
    github.com/aws/aws-sdk-go-v2 v1.25.0
    github.com/aws/aws-sdk-go-v2/config v1.27.0
    github.com/aws/aws-sdk-go-v2/service/sns v1.28.0
    github.com/stretchr/testify v1.8.4
)
```

## Step 6: Performance Requirements
- Handle 200K messages/hour (55/second)
- Response to ALB must be <100ms (don't wait for webhooks)
- Memory usage should be <50MB per invocation
- Webhook forwarding timeout: 10 seconds per webhook

## Step 7: Error Handling
- Panic recovery in async webhook forwarding
- Context cancellation handled gracefully
- Always return 200 to ALB even on errors
- Log errors but don't affect response

## Step 8: Validation
After creating all files, verify:
1. Run `go mod tidy` to get dependencies
2. Run `make test` - all tests should pass
3. Handler signature uses `any` return type
4. ALB always gets 200 response
5. No authentication code exists

## Important Architecture Notes
This implementation prevents webhook retry storms by always returning 200 to ALB. Without this:
- GitHub retries 8 times over 24 hours
- Stripe retries for 72 hours
- 200K events/hour becomes 1.6M with retries
- Costs increase by 70%

The handler returns `any` (not `error`) because:
- ALB events need ALBTargetGroupResponse
- SNS events need error/nil for retry
- Lambda runtime handles the type assertion

## Expected Test Output
```bash
$ make test
=== RUN   TestHandlerReturnType
--- PASS: TestHandlerReturnType (0.00s)
=== RUN   TestALBAlwaysReturns200
--- PASS: TestALBAlwaysReturns200 (0.10s)
=== RUN   TestHealthCheckDetection
--- PASS: TestHealthCheckDetection (0.05s)
PASS
ok      lambda-forwarder/internal/handler      0.15s
```

Please implement all files following these exact specifications. The most critical aspects are:
1. Handler returns `any` type
2. ALB always gets 200 response
3. No authentication handling
4. Exact event forwarding without transformation