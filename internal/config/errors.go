package config

import "errors"

var (
	ErrConfigNil          = errors.New("config is nil")
	ErrMissingSNSTopicArn = errors.New("sns topic arn is required")
	ErrMissingWebhookURLs = errors.New("at least one webhook url is required")
)
