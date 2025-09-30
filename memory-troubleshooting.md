# Memory: TROUBLESHOOTING_PATTERNS

## Common Issues & Solutions

### Lambda Not Returning 200
- **Check**: Handler returns `any` not `error`
- **Check**: ALB event detection working
- **Check**: No panics in logs
- **Solution**: Ensure ALBTargetGroupResponse always returned

### Webhook Retry Storms
- **Symptom**: Lambda invocations > incoming events
- **Cause**: Returning non-200 to ALB
- **Impact**: 8x multiplication (GitHub retries)
- **Fix**: Always return 200, handle errors async

### High Memory Usage
- **Check**: Goroutine leaks
- **Check**: HTTP client reuse
- **Solution**: Ensure proper cleanup in async processing

### Webhooks Not Receiving
- **Check**: WEBHOOK_URLS comma-separated
- **Check**: Network from Lambda VPC
- **Check**: Async goroutine panics
- **Debug**: Add more logging in ForwardToAll

## Performance Patterns

### Optimization Insights
- Event detection: ~10μs
- Response to ALB: <100ms
- Webhook timeout: 10s per webhook
- Parallel execution saves ~40ms
- Memory usage: <50MB typical

### Monitoring Setup
```bash
# Key CloudWatch metrics
- Lambda Invocations (should match events)
- Lambda Duration (<1s expected)
- Lambda Errors (near 0%)
- Lambda Concurrent Executions
```

## Debug Commands
```bash
# View logs
aws logs tail /aws/lambda/lambda-event-forwarder --follow

# Filter errors
aws logs filter-log-events \
  --log-group-name /aws/lambda/lambda-event-forwarder \
  --filter-pattern "ERROR"

# Test with sample event
aws lambda invoke \
  --function-name lambda-event-forwarder \
  --payload '{"test":"event"}' \
  response.json
```

## Fixed Issues
- ioutil.ReadFile deprecated → use os.ReadFile
- interface{} → use `any` (Go 1.18+)
- SNS MessageAttributes don't exist in Lambda events
- ALB requestId might be empty
- Base64 body handling for binary data