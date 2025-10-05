package config

import (
	"os"
	"strings"
	"testing"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config with webhook URLs",
			config: Config{
				SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
				WebhookURLs: []string{"https://example.com"},
			},
			wantErr: false,
		},
		{
			name: "valid config with cloud webhook URLs",
			config: Config{
				SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
				CloudWebhookURLs: []string{"https://cloud.example.com"},
			},
			wantErr: false,
		},
		{
			name: "valid config with enterprise webhook URLs",
			config: Config{
				SNSTopicArn:           "arn:aws:sns:us-east-1:123456789012:test",
				EnterpriseWebhookURLs: []string{"https://enterprise.example.com"},
			},
			wantErr: false,
		},
		{
			name: "valid config with multiple URL types",
			config: Config{
				SNSTopicArn:           "arn:aws:sns:us-east-1:123456789012:test",
				WebhookURLs:           []string{"https://example.com"},
				CloudWebhookURLs:      []string{"https://cloud.example.com"},
				EnterpriseWebhookURLs: []string{"https://enterprise.example.com"},
			},
			wantErr: false,
		},
		{
			name: "missing SNS topic ARN",
			config: Config{
				WebhookURLs: []string{"https://example.com"},
			},
			wantErr: true,
		},
		{
			name: "missing all webhook URLs",
			config: Config{
				SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
			},
			wantErr: true,
		},
		{
			name: "empty webhook URL slices",
			config: Config{
				SNSTopicArn:           "arn:aws:sns:us-east-1:123456789012:test",
				WebhookURLs:           []string{},
				CloudWebhookURLs:      []string{},
				EnterpriseWebhookURLs: []string{},
			},
			wantErr: true,
		},
		{
			name:    "nil config",
			config:  Config{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoad_DevEnvironment(t *testing.T) {
	cfg, err := Load("dev")
	if err != nil {
		t.Fatalf("Failed to load dev config: %v", err)
	}

	if cfg.Environment != "dev" {
		t.Errorf("Expected environment 'dev', got %s", cfg.Environment)
	}

	if cfg.SNSTopicArn == "" {
		t.Error("Expected SNS topic ARN to be set")
	}

	// Dev config should have at least one type of webhook URL configured
	totalWebhooks := len(cfg.WebhookURLs) + len(cfg.CloudWebhookURLs) + len(cfg.EnterpriseWebhookURLs)
	if totalWebhooks == 0 {
		t.Error("Expected at least one webhook URL (of any type)")
	}
}

func TestLoad_EnvironmentOverrides(t *testing.T) {
	// Save original env vars
	origSNS := os.Getenv("SNS_TOPIC_ARN")
	origWebhook := os.Getenv("WEBHOOK_URLS")
	origCloud := os.Getenv("CLOUD_WEBHOOK_URLS")
	origEnterprise := os.Getenv("ENTERPRISE_WEBHOOK_URLS")
	origDebug := os.Getenv("DEBUG")

	// Set test environment variables
	os.Setenv("SNS_TOPIC_ARN", "arn:aws:sns:us-east-1:999999999999:override-topic")
	os.Setenv("WEBHOOK_URLS", "https://override1.com,https://override2.com")
	os.Setenv("CLOUD_WEBHOOK_URLS", "https://cloud-override.com")
	os.Setenv("ENTERPRISE_WEBHOOK_URLS", "https://enterprise-override.com")
	os.Setenv("DEBUG", "true")

	// Restore original env vars after test
	defer func() {
		os.Setenv("SNS_TOPIC_ARN", origSNS)
		os.Setenv("WEBHOOK_URLS", origWebhook)
		os.Setenv("CLOUD_WEBHOOK_URLS", origCloud)
		os.Setenv("ENTERPRISE_WEBHOOK_URLS", origEnterprise)
		os.Setenv("DEBUG", origDebug)
	}()

	cfg, err := Load("dev")
	if err != nil {
		t.Fatalf("Failed to load config with overrides: %v", err)
	}

	// Verify overrides were applied
	if cfg.SNSTopicArn != "arn:aws:sns:us-east-1:999999999999:override-topic" {
		t.Errorf("Expected SNS topic ARN override, got %s", cfg.SNSTopicArn)
	}

	if len(cfg.WebhookURLs) != 2 {
		t.Errorf("Expected 2 webhook URLs, got %d", len(cfg.WebhookURLs))
	}

	if cfg.WebhookURLs[0] != "https://override1.com" {
		t.Errorf("Expected first webhook URL 'https://override1.com', got %s", cfg.WebhookURLs[0])
	}

	if len(cfg.CloudWebhookURLs) != 1 || cfg.CloudWebhookURLs[0] != "https://cloud-override.com" {
		t.Errorf("Expected cloud webhook override, got %v", cfg.CloudWebhookURLs)
	}

	if len(cfg.EnterpriseWebhookURLs) != 1 || cfg.EnterpriseWebhookURLs[0] != "https://enterprise-override.com" {
		t.Errorf("Expected enterprise webhook override, got %v", cfg.EnterpriseWebhookURLs)
	}

	if !cfg.Debug {
		t.Error("Expected Debug to be true")
	}
}

func TestLoad_InvalidEnvironment(t *testing.T) {
	_, err := Load("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent environment")
	}

	if !strings.Contains(err.Error(), "failed to read config file") {
		t.Errorf("Expected error about failed to read config file, got %v", err)
	}
}

func TestLoad_DefaultEnvironment(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Failed to load default config: %v", err)
	}

	if cfg.Environment != "dev" {
		t.Errorf("Expected default environment 'dev', got %s", cfg.Environment)
	}
}

func TestParseURLList(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single URL",
			input:    "https://example.com",
			expected: []string{"https://example.com"},
		},
		{
			name:     "multiple URLs",
			input:    "https://example1.com,https://example2.com,https://example3.com",
			expected: []string{"https://example1.com", "https://example2.com", "https://example3.com"},
		},
		{
			name:     "URLs with spaces",
			input:    "https://example1.com, https://example2.com , https://example3.com",
			expected: []string{"https://example1.com", "https://example2.com", "https://example3.com"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: []string{},
		},
		{
			name:     "empty items",
			input:    "https://example1.com,,https://example2.com",
			expected: []string{"https://example1.com", "https://example2.com"},
		},
		{
			name:     "only commas and spaces",
			input:    " , , ",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseURLList(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d URLs, got %d", len(tt.expected), len(result))
				return
			}

			for i, url := range result {
				if url != tt.expected[i] {
					t.Errorf("Expected URL[%d] = %s, got %s", i, tt.expected[i], url)
				}
			}
		})
	}
}

func TestLoad_BooleanParsing(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		setEnv   bool
		expected bool
	}{
		{"true", "true", true, true},
		{"1", "1", true, true},
		{"false", "false", true, false},
		{"0", "0", true, false},
		{"other", "yes", true, false},
	}

	for _, tt := range tests {
		t.Run("DEBUG_"+tt.name, func(t *testing.T) {
			origDebug := os.Getenv("DEBUG")
			defer func() {
				if origDebug != "" {
					os.Setenv("DEBUG", origDebug)
				} else {
					os.Unsetenv("DEBUG")
				}
			}()

			if tt.setEnv {
				os.Setenv("DEBUG", tt.envValue)
			} else {
				os.Unsetenv("DEBUG")
			}

			cfg, err := Load("dev")
			if err != nil {
				t.Fatalf("Failed to load config: %v", err)
			}

			if tt.setEnv && cfg.Debug != tt.expected {
				t.Errorf("Expected Debug=%v for value '%s', got %v", tt.expected, tt.envValue, cfg.Debug)
			}
		})
	}
}
