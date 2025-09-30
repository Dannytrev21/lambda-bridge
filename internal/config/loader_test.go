package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEmbeddedEnvironments(t *testing.T) {
	t.Parallel()

	environments := []string{"dev", "qa", "prod"}
	for _, env := range environments {
		env := env
		t.Run(env, func(t *testing.T) {
			t.Parallel()

			cfg, err := Load(env)
			if err != nil {
				t.Fatalf("load %s config: %v", env, err)
			}

			if cfg.Environment != env {
				t.Fatalf("expected environment %s, got %s", env, cfg.Environment)
			}

			if err := cfg.Validate(); err != nil {
				t.Fatalf("validate %s config: %v", env, err)
			}

			if len(cfg.CloudWebhookURLs) == 0 {
				t.Fatalf("expected cloud webhook urls for %s", env)
			}
			if len(cfg.EnterpriseWebhookURLs) == 0 {
				t.Fatalf("expected enterprise webhook urls for %s", env)
			}
		})
	}
}

func TestLoadTestEnvironmentFromDisk(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configsDir := filepath.Join(tmpDir, "configs")
	if err := os.Mkdir(configsDir, 0o755); err != nil {
		t.Fatalf("mkdir configs dir: %v", err)
	}

	testConfigPath := filepath.Join(configsDir, "config.test.yml")
	content := []byte("sns_topic_arn: test\nskip_health_checks: false\ndebug: true\ncloud_webhook_urls:\n  - http://localhost:8081/cloud\nenterprise_webhook_urls:\n  - http://localhost:8082/enterprise\n")
	if err := os.WriteFile(testConfigPath, content, 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	// Point the working directory to the temp dir so Load("test") reads our file.
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(originalWd) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}

	cfg, err := Load("test")
	if err != nil {
		t.Fatalf("load test config: %v", err)
	}

	if cfg.SNSTopicArn != "test" {
		t.Fatalf("expected sns_topic_arn 'test', got %q", cfg.SNSTopicArn)
	}

	if len(cfg.CloudWebhookURLs) != 1 {
		t.Fatalf("expected 1 cloud webhook url, got %d", len(cfg.CloudWebhookURLs))
	}
	if len(cfg.EnterpriseWebhookURLs) != 1 {
		t.Fatalf("expected 1 enterprise webhook url, got %d", len(cfg.EnterpriseWebhookURLs))
	}
}

func TestLoadUnsupportedEnvironment(t *testing.T) {
	t.Parallel()

	if _, err := Load("staging"); err == nil {
		t.Fatal("expected error for unsupported environment")
	}
}
