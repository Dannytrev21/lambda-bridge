package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Dannytrev21/lambda-bridge/configs"
)

var embeddedEnvToPath = map[string]string{
	"dev":  "config.dev.yml",
	"qa":   "config.qa.yml",
	"prod": "config.prod.yml",
}

// Load returns the configuration for the provided environment. Embedded
// configurations are used for dev/qa/prod while the test configuration is
// loaded from disk so tests can mutate it on the fly if needed.
func Load(environment string) (*Config, error) {
	env := normalizeEnv(environment)

	data, err := readConfigBytes(env)
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("unmarshal %s config: %w", env, err)
	}

	cfg.Environment = env
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func normalizeEnv(environment string) string {
	if environment == "" {
		return "dev"
	}
	return strings.ToLower(environment)
}

func readConfigBytes(environment string) ([]byte, error) {
	if environment == "test" {
		path := filepath.Join("configs", "config.test.yml")
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s config from disk: %w", environment, err)
		}
		return data, nil
	}

	embeddedPath, ok := embeddedEnvToPath[environment]
	if !ok {
		return nil, fmt.Errorf("unsupported environment: %s", environment)
	}

	data, err := configs.Embedded.ReadFile(embeddedPath)
	if err != nil {
		return nil, fmt.Errorf("read embedded %s config: %w", environment, err)
	}

	return data, nil
}
