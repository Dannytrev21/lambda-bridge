package config

import (
	"embed"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed configs/*.yml
var configFiles embed.FS

// Config represents the application configuration
type Config struct {
	Environment           string   `yaml:"environment"`
	SNSTopicArn           string   `yaml:"sns_topic_arn"`
	WebhookURLs           []string `yaml:"webhook_urls"`
	CloudWebhookURLs      []string `yaml:"cloud_webhook_urls"`
	EnterpriseWebhookURLs []string `yaml:"enterprise_webhook_urls"`
	Debug                 bool     `yaml:"debug"`
}

// Load loads configuration for the given environment
func Load(environment string) (*Config, error) {
	if environment == "" {
		environment = "dev"
	}

	// Load base config from embedded files
	filename := fmt.Sprintf("configs/config-%s.yml", environment)
	data, err := configFiles.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", filename, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse YAML config: %w", err)
	}

	// Apply environment variable overrides
	if val := os.Getenv("SNS_TOPIC_ARN"); val != "" {
		cfg.SNSTopicArn = val
	}
	if val := os.Getenv("WEBHOOK_URLS"); val != "" {
		cfg.WebhookURLs = parseURLList(val)
	}
	if val := os.Getenv("CLOUD_WEBHOOK_URLS"); val != "" {
		cfg.CloudWebhookURLs = parseURLList(val)
	}
	if val := os.Getenv("ENTERPRISE_WEBHOOK_URLS"); val != "" {
		cfg.EnterpriseWebhookURLs = parseURLList(val)
	}
	if val := os.Getenv("DEBUG"); val != "" {
		cfg.Debug = val == "true" || val == "1"
	}

	cfg.Environment = environment

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("config is nil")
	}
	if c.SNSTopicArn == "" {
		return fmt.Errorf("SNS topic ARN is required")
	}
	if len(c.WebhookURLs) == 0 && len(c.CloudWebhookURLs) == 0 && len(c.EnterpriseWebhookURLs) == 0 {
		return fmt.Errorf("at least one webhook URL must be configured")
	}
	return nil
}

// parseURLList parses a comma-separated list of URLs
func parseURLList(urls string) []string {
	var result []string
	for _, url := range strings.Split(urls, ",") {
		url = strings.TrimSpace(url)
		if url != "" {
			result = append(result, url)
		}
	}
	return result
}
