# Memory: REQUIREMENTS_CONSTRAINTS

## Original Context
- Migrating from Go service with transport-agnostic event processing
- Similar pattern to existing architecture in internal/handler/forwarder_handler.go
- Lambda started via lambda.Start(forwarder.Handler)

## Performance Requirements
- **Volume**: 200K messages/hour (55/second)
- **Combined**: SNS + SQS sources
- **Latency**: Faster is better, no hard requirement
- **Cost conscious**: ~$40/day target

## Event Forwarding Requirements
- **SNS → SNS**: Single topic (SNS_TOPIC_ARN)
- **ALB → Webhooks**: Fan-out to 3 webhooks
- **Exact forwarding**: No transformation, preserve structure
- **Raw message delivery**: For SNS forwarding

## Critical Constraints
- **NO authentication handling** in Lambda
- Webhook receivers handle their own auth
- Always return 200 to ALB (prevent retry storms)
- Skip health check forwarding
- No guaranteed delivery requirement initially
- DLQ setup planned for future

## Technical Constraints  
- Go 1.21+ required
- Use `any` not `interface{}`
- Handler must return `any` type
- Mono-repo preference
- ADHD-friendly (small, manageable pieces)