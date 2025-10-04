package forwarder

import (
	"context"
	"encoding/json"

	"github.com/aws/aws-sdk-go-v2/service/sns"
)

// SNSClient interface for mocking
type SNSClient interface {
	Publish(ctx context.Context, input *sns.PublishInput, opts ...func(*sns.Options)) (*sns.PublishOutput, error)
}

// SNSForwarderInterface for dependency injection
type SNSForwarderInterface interface {
	Forward(ctx context.Context, topicArn string, rawEvent json.RawMessage) error
}

// WebhookForwarderInterface for dependency injection
type WebhookForwarderInterface interface {
	ForwardToWebhooks(ctx context.Context, urls []string, rawEvent json.RawMessage) map[string]WebhookResult
	Close() error
}