# Memory: IMPLEMENTATION_CODE

## Project Structure
```
lambda-forwarder/
├── main.go
├── internal/
│   ├── handler/forwarder_handler.go
│   └── forwarder/
│       ├── sns_forwarder.go
│       └── webhook_forwarder.go
└── go.mod
```

## Key Code Patterns

### main.go
- Loads AWS config once during cold start
- Creates handler with dependencies
- Calls `lambda.Start(forwarderHandler.Handler)`

### ForwarderHandler
```go
type ForwarderHandler struct {
    snsForwarder     *forwarder.SNSForwarder
    webhookForwarder *forwarder.WebhookForwarder  
    config           *Config
}

func (h *ForwarderHandler) Handler(ctx context.Context, rawEvent json.RawMessage) any {
    // Type detection logic
    // Returns ALBTargetGroupResponse or error
}
```

### Event Detection
- Tries ALB unmarshal first (most common)
- Then tries SNS unmarshal
- Validates required fields (targetGroupArn, MessageID)
- Returns safe ALB 200 for unknown events

### Webhook Forwarder
- Parallel forwarding using goroutines
- HTTP client with 10s timeout
- 3 retries with exponential backoff
- Returns map[url]Result for analysis
- No auth handling - receivers handle own auth

### SNS Forwarder  
- Forwards complete raw event unchanged
- Adds ForwardedAt timestamp attribute
- Simple error propagation for Lambda retry

## Dependencies (go.mod)
- aws-lambda-go v1.46.0
- aws-sdk-go-v2 v1.25.0
- stretchify/testify v1.8.4 (testing only)