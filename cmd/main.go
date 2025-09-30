package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"

	appconfig "github.com/Dannytrev21/lambda-bridge/internal/config"
	"github.com/Dannytrev21/lambda-bridge/internal/forwarder"
	"github.com/Dannytrev21/lambda-bridge/internal/handler"
	"github.com/Dannytrev21/lambda-bridge/internal/telemetry"
)

func main() {
	ctx := context.Background()

	// Load AWS config during cold start
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatal("Failed to load AWS config:", err)
	}

	// Create AWS clients
	snsClient := sns.NewFromConfig(cfg)

	// Determine environment and load config
	environment := os.Getenv("ENV")
	if environment == "" {
		environment = os.Getenv("ENVIRONMENT")
	}

	handlerConfig, err := appconfig.Load(environment)
	if err != nil {
		log.Fatal("Failed to load application config:", err)
	}

	applyOverrides(handlerConfig)

	if err := handlerConfig.Validate(); err != nil {
		log.Fatal("Configuration invalid:", err)
	}

	telemetryCfg := telemetry.DefaultConfig()
	telemetryCfg.Environment = handlerConfig.Environment
	providers, err := telemetry.Setup(ctx, telemetryCfg)
	if err != nil {
		log.Fatal("Failed to configure telemetry:", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := providers.Shutdown(shutdownCtx); shutdownErr != nil {
			log.Printf("Telemetry shutdown error: %v", shutdownErr)
		}
	}()

	// Create forwarders
	snsForwarder := forwarder.NewSNSForwarder(snsClient)
	webhookForwarder := forwarder.NewWebhookForwarder()

	// Create handler
	forwarderHandler := handler.NewForwarderHandler(snsForwarder, webhookForwarder, handlerConfig)

	// Start Lambda
	lambda.Start(forwarderHandler.Handler)
}

func applyOverrides(cfg *appconfig.Config) {
	if cfg == nil {
		return
	}

	if value := os.Getenv("SNS_TOPIC_ARN"); value != "" {
		cfg.SNSTopicArn = value
	}

	if value := os.Getenv("WEBHOOK_URLS"); value != "" {
		cfg.WebhookURLs = parseWebhookURLs(value)
	}

	if value := os.Getenv("SKIP_HEALTH_CHECKS"); value != "" {
		cfg.SkipHealthChecks = getEnvBool("SKIP_HEALTH_CHECKS", cfg.SkipHealthChecks)
	}

	if value := os.Getenv("DEBUG"); value != "" {
		cfg.Debug = getEnvBool("DEBUG", cfg.Debug)
	}

	if value := os.Getenv("ENV"); value != "" {
		cfg.Environment = value
	} else if value := os.Getenv("ENVIRONMENT"); value != "" {
		cfg.Environment = value
	}
}

func parseWebhookURLs(urls string) []string {
	if urls == "" {
		return nil
	}
	var result []string
	for _, url := range parseCommaSeparated(urls) {
		if url != "" {
			result = append(result, url)
		}
	}
	return result
}

func parseCommaSeparated(input string) []string {
	if input == "" {
		return nil
	}
	var result []string
	current := ""
	for _, char := range input {
		if char == ',' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(char)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return value == "true" || value == "1"
	}
	return defaultValue
}
