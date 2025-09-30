.PHONY: build run test clean lint air lambda-build lambda-deploy

# Local development
build:
	go build -o bin/app cmd/main.go

run:
	go run cmd/main.go

test:
	SNS_TOPIC_ARN=arn:aws:sns:us-east-1:123456789012:test \
	WEBHOOK_URLS=http://test1.com,http://test2.com,http://test3.com \
	go test -v ./...

test-coverage:
	SNS_TOPIC_ARN=arn:aws:sns:us-east-1:123456789012:test \
	WEBHOOK_URLS=http://test1.com,http://test2.com,http://test3.com \
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

clean:
	rm -rf bin/ tmp/ bootstrap lambda-deployment.zip lambda-deployment-arm64.zip lambda-deployment-amd64.zip coverage.out coverage.html

lint:
	golangci-lint run

air:
	air

# Lambda-specific builds
lambda-build-arm64:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o bootstrap cmd/main.go
	zip lambda-deployment-arm64.zip bootstrap
	$(MAKE) verify-arch

lambda-build-amd64:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bootstrap cmd/main.go
	zip lambda-deployment-amd64.zip bootstrap

lambda-build: lambda-build-arm64
	@cp lambda-deployment-arm64.zip lambda-deployment.zip

verify-arch:
	@file bootstrap | grep -q "ARM aarch64" || (echo "ERROR: Expected ARM64 binary" && exit 1)

lambda-deploy: lambda-build
	@echo "Upload lambda-deployment.zip (ARM64 build) to AWS Lambda"
	@echo "Lambda configuration: Runtime=provided.al2 Architecture=arm64"
	@echo "Make sure to set environment variables:"
	@echo "  SNS_TOPIC_ARN=arn:aws:sns:region:account:topic-name"
	@echo "  WEBHOOK_URLS=https://url1.com,https://url2.com,https://url3.com"

# Development tools
install-tools:
	go install github.com/cosmtrek/air@latest
	go install gotest.tools/gotestsum@latest
	go install github.com/golang/mock/mockgen@latest

# Dependencies
deps:
	go mod download
	go mod tidy

# Format code
fmt:
	go fmt ./...

# Benchmarks
benchmark:
	SNS_TOPIC_ARN=arn:aws:sns:us-east-1:123456789012:test \
	WEBHOOK_URLS=http://test1.com,http://test2.com,http://test3.com \
	go test -bench=. -benchmem ./...

# Help
help:
	@echo "Available targets:"
	@echo "  build         - Build the application"
	@echo "  run           - Run locally"
	@echo "  test          - Run tests"
	@echo "  test-coverage - Run tests with coverage"
	@echo "  clean         - Clean build artifacts"
	@echo "  lint          - Run linter"
	@echo "  lambda-build  - Build ARM64 deployment package"
	@echo "  lambda-build-arm64 - Explicit ARM64 package"
	@echo "  lambda-build-amd64 - Legacy AMD64 package"
	@echo "  lambda-deploy - Build and show deployment instructions"
	@echo "  deps          - Download and tidy dependencies"
	@echo "  fmt           - Format code"
	@echo "  benchmark     - Run benchmarks"
	@echo "  help          - Show this help"
