.PHONY: build test test-coverage test-bench test-clean clean lambda-build

# Build the binary
build:
	@echo "Building Lambda function..."
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -o forwarder ./cmd/main.go

# Run unit tests
test:
	@echo "Running unit tests..."
	go test -v -race ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"
	@echo ""
	@echo "Summary:"
	go tool cover -func=coverage.out | tail -1

# Run benchmarks
test-bench:
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./...

# Clean test artifacts
test-clean:
	@echo "Cleaning test artifacts..."
	rm -f coverage.out coverage.html *.test

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f forwarder lambda-deployment.zip lambda-deployment-arm64.zip
	go clean -cache

# Build Lambda deployment package
lambda-build: clean build
	@echo "Creating Lambda deployment package..."
	zip lambda-deployment-arm64.zip forwarder bootstrap
	@echo "Lambda package created: lambda-deployment-arm64.zip"
	@echo "Size: $$(du -h lambda-deployment-arm64.zip | cut -f1)"
	@echo ""
	@echo "Deploy with:"
	@echo "  Runtime: provided.al2"
	@echo "  Architecture: arm64"
	@echo "  Environment variables:"
	@echo "    ENV=prod"
	@echo "    SNS_TOPIC_ARN=arn:aws:sns:region:account:topic-name"
	@echo "    WEBHOOK_URLS=https://url1.com,https://url2.com"

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Help
help:
	@echo "Available targets:"
	@echo "  build         - Build the Lambda binary"
	@echo "  test          - Run tests"
	@echo "  test-coverage - Run tests with coverage"
	@echo "  clean         - Clean build artifacts"
	@echo "  lambda-build  - Build Lambda deployment package (ARM64)"
	@echo "  fmt           - Format code"
	@echo "  help          - Show this help"
