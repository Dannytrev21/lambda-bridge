# Memory: CONFIGURATION_DEPLOYMENT

## Environment Variables
```bash
# Required
SNS_TOPIC_ARN=arn:aws:sns:us-east-1:123456789012:topic
WEBHOOK_URLS=https://url1.com,https://url2.com,https://url3.com

# Optional
SKIP_HEALTH_CHECKS=true  # default: true
ENVIRONMENT=prod          # default: dev
DEBUG=false              # default: false
AWS_REGION=us-east-1     # default: us-east-1
```

## Build Process
```bash
# Linux binary for Lambda
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bootstrap main.go
zip lambda-deployment.zip bootstrap
```

## Lambda Configuration
- Runtime: provided.al2
- Handler: bootstrap
- Memory: 512MB (sufficient for 200K/hour)
- Timeout: 30 seconds
- Reserved concurrency: Not needed (auto-scales)

## Deployment Commands
```bash
# Create function
aws lambda create-function \
  --function-name lambda-event-forwarder \
  --runtime provided.al2 \
  --role arn:aws:iam::ACCOUNT:role/lambda-execution-role \
  --handler bootstrap \
  --zip-file fileb://lambda-deployment.zip

# Update code
aws lambda update-function-code \
  --function-name lambda-event-forwarder \
  --zip-file fileb://lambda-deployment.zip
```

## IAM Permissions Required
- sns:Publish for topic ARN
- logs:CreateLogGroup
- logs:CreateLogStream
- logs:PutLogEvents

## GitHub Actions CI
- Tests on push to main
- Runs: go test -v -cover ./...
- Benchmarks included
- No complex LocalStack setup needed