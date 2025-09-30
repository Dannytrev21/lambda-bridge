# Memory: CORE_ARCHITECTURE

## Handler Design Pattern
- **Critical**: Handler returns `any` type (Go 1.18+ best practice), NOT `interface{}` or `error`
- Signature: `func Handler(ctx context.Context, rawEvent json.RawMessage) any`
- Returns `events.ALBTargetGroupResponse` for ALB events
- Returns `error` or `nil` for SNS events
- Lambda runtime handles type assertion automatically

## ALB 200 Response Architecture
**Problem Solved**: Prevents webhook retry storms that multiply load by 8x
- GitHub retries 8 times, Stripe 72 hours, causing cascade failures
- 200K events/hour becomes 1.6M with retries if errors returned
- Cost increases 70% without this pattern

**Implementation**:
```go
// ALWAYS for ALB events:
return events.ALBTargetGroupResponse{
    StatusCode: 200,  // NEVER change
    Body: `{"status":"accepted"}`,
}
```

## Event Flow Architecture
1. Event arrives (ALB or SNS)
2. Type detection via unmarshaling attempts
3. ALB events → Return 200 immediately → Async webhook forwarding
4. SNS events → Forward to SNS topic → Return error/nil for Lambda retry
5. Unknown events → Return safe ALB 200 response

## Async Processing Pattern
- Webhook forwarding happens in goroutine
- Response returns in <100ms without waiting
- Parallel fan-out to 3 webhooks
- Panic recovery in async processing
- Future: EventBridge/DLQ for failed forwards

## Health Check Filtering
- Detects ELB-HealthChecker user agent
- Recognizes /health, /healthz, /ping paths
- Returns 200 without forwarding
- Saves ~1000 unnecessary forwards/hour