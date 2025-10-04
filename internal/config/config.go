package config

import (
	"fmt"
	"time"
)

// Config represents the runtime configuration for the Lambda bridge application.
// All duration fields use milliseconds or seconds as indicated in field names for clarity.
type Config struct {
	Environment                   string   `yaml:"environment"`
	SNSTopicArn                   string   `yaml:"sns_topic_arn"`
	WebhookURLs                   []string `yaml:"webhook_urls"`
	CloudWebhookURLs              []string `yaml:"cloud_webhook_urls"`
	EnterpriseWebhookURLs         []string `yaml:"enterprise_webhook_urls"`
	SkipHealthChecks              bool     `yaml:"skip_health_checks"`
	Debug                         bool     `yaml:"debug"`

	// Worker Pool Configuration (per-URL worker pools)
	WorkerCount                   int      `yaml:"worker_count"`           // Default: 10, Range: 1-100
	QueueSize                     int      `yaml:"queue_size"`             // Default: 100, Range: 10-10000

	// Retry Configuration
	MaxRetries                    int      `yaml:"max_retries"`            // Default: 3, Range: 0-10
	EnableJitter                  bool     `yaml:"enable_jitter"`          // Default: true
	BaseRetryDelayMs              int      `yaml:"base_retry_delay_ms"`    // Default: 100ms, Range: 10-5000ms
	MaxRetryDelaySeconds          int      `yaml:"max_retry_delay_seconds"`// Default: 5s, Range: 1-60s

	// Circuit Breaker Configuration
	CircuitBreakerFailThreshold   int      `yaml:"circuit_breaker_fail_threshold"`   // Default: 5, Range: 1-100
	CircuitBreakerWindowSeconds   int      `yaml:"circuit_breaker_window_seconds"`   // Default: 30s, Range: 5-300s
	CircuitBreakerCooldownSeconds int      `yaml:"circuit_breaker_cooldown_seconds"` // Default: 30s, Range: 5-300s

	// HTTP Client Configuration
	WebhookTimeoutMs              int      `yaml:"webhook_timeout_ms"`     // Default: 10000ms (10s), Range: 100-30000ms
	MaxConnectionsPerHost         int      `yaml:"max_connections_per_host"` // Default: 100, Range: 1-1000
	EnableHTTP2                   bool     `yaml:"enable_http2"`           // Default: false
	DisableCompression            bool     `yaml:"disable_compression"`    // Default: false
	ConnectionWarmup              bool     `yaml:"connection_warmup"`      // Default: false
}

// Validate ensures the config contains required values and all numeric ranges are valid.
func (c *Config) Validate() error {
	if c == nil {
		return ErrConfigNil
	}
	if c.SNSTopicArn == "" {
		return ErrMissingSNSTopicArn
	}
	if len(c.WebhookURLs) == 0 && len(c.CloudWebhookURLs) == 0 && len(c.EnterpriseWebhookURLs) == 0 {
		return ErrMissingWebhookURLs
	}

	// Validate worker pool settings (0 values are allowed, will use defaults)
	if c.WorkerCount > 100 {
		return fmt.Errorf("worker_count must be at most 100, got %d", c.WorkerCount)
	}
	if c.QueueSize > 10000 {
		return fmt.Errorf("queue_size must be at most 10000, got %d", c.QueueSize)
	}

	// Validate retry settings (0 values are allowed, will use defaults)
	if c.MaxRetries > 10 {
		return fmt.Errorf("max_retries must be at most 10, got %d", c.MaxRetries)
	}
	if c.BaseRetryDelayMs > 5000 {
		return fmt.Errorf("base_retry_delay_ms must be at most 5000ms, got %d", c.BaseRetryDelayMs)
	}
	if c.MaxRetryDelaySeconds > 60 {
		return fmt.Errorf("max_retry_delay_seconds must be at most 60s, got %d", c.MaxRetryDelaySeconds)
	}

	// Validate circuit breaker settings (0 values are allowed, will use defaults)
	if c.CircuitBreakerFailThreshold > 100 {
		return fmt.Errorf("circuit_breaker_fail_threshold must be at most 100, got %d", c.CircuitBreakerFailThreshold)
	}
	if c.CircuitBreakerWindowSeconds > 300 {
		return fmt.Errorf("circuit_breaker_window_seconds must be at most 300s, got %d", c.CircuitBreakerWindowSeconds)
	}
	if c.CircuitBreakerCooldownSeconds > 300 {
		return fmt.Errorf("circuit_breaker_cooldown_seconds must be at most 300s, got %d", c.CircuitBreakerCooldownSeconds)
	}

	// Validate HTTP client settings (0 values are allowed, will use defaults)
	if c.WebhookTimeoutMs > 30000 {
		return fmt.Errorf("webhook_timeout_ms must be at most 30000ms, got %d", c.WebhookTimeoutMs)
	}
	if c.MaxConnectionsPerHost > 1000 {
		return fmt.Errorf("max_connections_per_host must be at most 1000, got %d", c.MaxConnectionsPerHost)
	}

	return nil
}

// GetRetryConfig returns retry configuration as durations.
func (c *Config) GetRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:     c.MaxRetries,
		EnableJitter:   c.EnableJitter,
		BaseRetryDelay: time.Duration(c.BaseRetryDelayMs) * time.Millisecond,
		MaxRetryDelay:  time.Duration(c.MaxRetryDelaySeconds) * time.Second,
	}
}

// GetCircuitBreakerConfig returns circuit breaker configuration as durations.
func (c *Config) GetCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		Threshold: c.CircuitBreakerFailThreshold,
		Window:    time.Duration(c.CircuitBreakerWindowSeconds) * time.Second,
		Cooldown:  time.Duration(c.CircuitBreakerCooldownSeconds) * time.Second,
	}
}

// GetWebhookTimeout returns the webhook timeout as a duration.
func (c *Config) GetWebhookTimeout() time.Duration {
	return time.Duration(c.WebhookTimeoutMs) * time.Millisecond
}

// RetryConfig contains retry-related configuration.
type RetryConfig struct {
	MaxRetries     int
	EnableJitter   bool
	BaseRetryDelay time.Duration
	MaxRetryDelay  time.Duration
}

// CircuitBreakerConfig contains circuit breaker-related configuration.
type CircuitBreakerConfig struct {
	Threshold int
	Window    time.Duration
	Cooldown  time.Duration
}
