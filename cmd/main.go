package main

import (
	"context"
	"log"
	"os"
	"strconv"
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

	// Load and validate configuration
	cfg := mustLoadConfig()

	// Setup AWS services
	snsClient := mustSetupAWS(ctx)

	// Setup telemetry with cleanup
	telemetryProviders := mustSetupTelemetry(ctx, cfg)
	defer shutdownTelemetry(telemetryProviders)

	// Create forwarders with cleanup
	webhookForwarder := createWebhookForwarder(cfg)
	defer shutdownForwarder(webhookForwarder)

	// Warm connections if enabled
	warmConnections(ctx, cfg)

	// Create and start handler
	snsForwarder := forwarder.NewSNSForwarder(snsClient)
	forwarderHandler := handler.NewForwarderHandler(snsForwarder, webhookForwarder, cfg)
	defer shutdownHandler(forwarderHandler)

	lambda.Start(forwarderHandler.Handler)
}

// mustLoadConfig loads and validates application configuration or exits.
func mustLoadConfig() *appconfig.Config {
	environment := os.Getenv("ENV")
	if environment == "" {
		environment = os.Getenv("ENVIRONMENT")
	}

	cfg, err := appconfig.Load(environment)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	applyOverrides(cfg)

	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	return cfg
}

// mustSetupAWS initializes AWS SDK configuration or exits.
func mustSetupAWS(ctx context.Context) *sns.Client {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("Failed to load AWS config: %v", err)
	}
	return sns.NewFromConfig(cfg)
}

// mustSetupTelemetry initializes OpenTelemetry providers or exits.
func mustSetupTelemetry(ctx context.Context, cfg *appconfig.Config) telemetry.Providers {
	telemetryCfg := telemetry.DefaultConfig()
	telemetryCfg.Environment = cfg.Environment

	providers, err := telemetry.Setup(ctx, telemetryCfg)
	if err != nil {
		log.Fatalf("Failed to setup telemetry: %v", err)
	}

	return providers
}

// createWebhookForwarder creates a webhook forwarder from application config.
func createWebhookForwarder(cfg *appconfig.Config) *forwarder.WebhookForwarder {
	retryConfig := cfg.GetRetryConfig()
	breakerConfig := cfg.GetCircuitBreakerConfig()

	forwarderConfig := forwarder.Config{
		MaxRetries:              retryConfig.MaxRetries,
		EnableJitter:            retryConfig.EnableJitter,
		BaseRetryDelay:          retryConfig.BaseRetryDelay,
		MaxRetryDelay:           retryConfig.MaxRetryDelay,
		CircuitBreakerThreshold: breakerConfig.Threshold,
		CircuitBreakerWindow:    breakerConfig.Window,
		CircuitBreakerCooldown:  breakerConfig.Cooldown,
		WebhookTimeout:          cfg.GetWebhookTimeout(),
	}

	return forwarder.NewWebhookForwarder(forwarderConfig)
}

// warmConnections pre-establishes HTTP connections to webhook endpoints.
func warmConnections(ctx context.Context, cfg *appconfig.Config) {
	if !cfg.ConnectionWarmup {
		return
	}

	allURLs := append([]string{}, cfg.WebhookURLs...)
	allURLs = append(allURLs, cfg.CloudWebhookURLs...)
	allURLs = append(allURLs, cfg.EnterpriseWebhookURLs...)

	if len(allURLs) == 0 {
		return
	}

	warmer := forwarder.NewConnectionWarmer(allURLs)
	warmCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	warmer.WarmConnections(warmCtx)
	log.Printf("Warmed connections to %d webhook endpoints", len(allURLs))
}

// shutdownTelemetry gracefully shuts down telemetry providers.
func shutdownTelemetry(providers telemetry.Providers) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := providers.Shutdown(ctx); err != nil {
		log.Printf("Telemetry shutdown error: %v", err)
	}
}

// shutdownForwarder gracefully shuts down the webhook forwarder.
func shutdownForwarder(wf *forwarder.WebhookForwarder) {
	if err := wf.Close(); err != nil {
		log.Printf("Webhook forwarder shutdown error: %v", err)
	}
}

// shutdownHandler gracefully shuts down the handler.
func shutdownHandler(h *handler.ForwarderHandler) {
	if err := h.Shutdown(5 * time.Second); err != nil {
		log.Printf("Handler shutdown error: %v", err)
	}
}

// applyOverrides applies environment variable overrides to the configuration.
// Environment variables take precedence over YAML config values.
func applyOverrides(cfg *appconfig.Config) {
	if cfg == nil {
		return
	}

	// String overrides
	applyStringOverride(&cfg.SNSTopicArn, "SNS_TOPIC_ARN")
	applyStringOverride(&cfg.Environment, "ENV", "ENVIRONMENT")

	// Boolean overrides
	applyBoolOverride(&cfg.SkipHealthChecks, "SKIP_HEALTH_CHECKS")
	applyBoolOverride(&cfg.Debug, "DEBUG")
	applyBoolOverride(&cfg.EnableJitter, "ENABLE_JITTER")

	// Integer overrides
	applyIntOverride(&cfg.WorkerCount, "WORKER_COUNT")
	applyIntOverride(&cfg.QueueSize, "QUEUE_SIZE")
	applyIntOverride(&cfg.MaxRetries, "MAX_RETRIES")
	applyIntOverride(&cfg.BaseRetryDelayMs, "BASE_RETRY_DELAY_MS")
	applyIntOverride(&cfg.MaxRetryDelaySeconds, "MAX_RETRY_DELAY_SECONDS")
	applyIntOverride(&cfg.CircuitBreakerFailThreshold, "CB_FAIL_THRESHOLD")
	applyIntOverride(&cfg.CircuitBreakerWindowSeconds, "CB_WINDOW_SEC")
	applyIntOverride(&cfg.CircuitBreakerCooldownSeconds, "CB_COOLDOWN_SEC")
	applyIntOverride(&cfg.WebhookTimeoutMs, "WEBHOOK_TIMEOUT_MS")

	// Special case: webhook URLs (comma-separated list)
	if value := os.Getenv("WEBHOOK_URLS"); value != "" {
		cfg.WebhookURLs = parseWebhookURLs(value)
	}
}

// applyStringOverride applies string environment variable override.
// Checks multiple env var names in order, uses first non-empty value.
func applyStringOverride(target *string, envVars ...string) {
	for _, envVar := range envVars {
		if value := os.Getenv(envVar); value != "" {
			*target = value
			return
		}
	}
}

// applyBoolOverride applies boolean environment variable override.
func applyBoolOverride(target *bool, envVar string) {
	if value := os.Getenv(envVar); value != "" {
		*target = getEnvBool(envVar, *target)
	}
}

// applyIntOverride applies integer environment variable override.
func applyIntOverride(target *int, envVar string) {
	if value := os.Getenv(envVar); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			*target = intValue
		}
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
