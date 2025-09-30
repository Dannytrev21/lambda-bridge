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
	rm -rf bin/ tmp/ bootstrap lambda-deployment.zip coverage.out coverage.html

lint:
	golangci-lint run

air:
	air

# Lambda-specific builds
lambda-build:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bootstrap cmd/main.go
	zip lambda-deployment.zip bootstrap

lambda-deploy: lambda-build
	@echo "Upload lambda-deployment.zip to AWS Lambda"
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
	@echo "  lambda-build  - Build for Lambda deployment"
	@echo "  lambda-deploy - Build and show deployment instructions"
	@echo "  deps          - Download and tidy dependencies"
	@echo "  fmt           - Format code"
	@echo "  benchmark     - Run benchmarks"
	@echo "  help          - Show this help"
