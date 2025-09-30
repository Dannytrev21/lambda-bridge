package config

// Config represents the runtime configuration for the Lambda bridge application.
type Config struct {
	Environment           string   `yaml:"environment"`
	SNSTopicArn           string   `yaml:"sns_topic_arn"`
	WebhookURLs           []string `yaml:"webhook_urls"`
	CloudWebhookURLs      []string `yaml:"cloud_webhook_urls"`
	EnterpriseWebhookURLs []string `yaml:"enterprise_webhook_urls"`
	SkipHealthChecks      bool     `yaml:"skip_health_checks"`
	Debug                 bool     `yaml:"debug"`
}

// Validate ensures the config contains the required values for normal operation.
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
	return nil
}
