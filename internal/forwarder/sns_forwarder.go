package forwarder

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"
)

type SNSForwarder struct {
	snsClient SNSClient
}

func NewSNSForwarder(snsClient SNSClient) *SNSForwarder {
	return &SNSForwarder{
		snsClient: snsClient,
	}
}

// Forward sends the raw event to the specified SNS topic
// Returns error for Lambda retry mechanism
func (f *SNSForwarder) Forward(ctx context.Context, topicArn string, rawEvent json.RawMessage) error {
	// Add forwarded timestamp attribute
	attributes := map[string]string{
		"ForwardedAt": strconv.FormatInt(time.Now().Unix(), 10),
	}

	// Convert attributes to SNS format
	messageAttributes := make(map[string]types.MessageAttributeValue)
	for key, value := range attributes {
		messageAttributes[key] = types.MessageAttributeValue{
			DataType:    stringPtr("String"),
			StringValue: stringPtr(value),
		}
	}

	// Publish the raw event to SNS
	_, err := f.snsClient.Publish(ctx, &sns.PublishInput{
		TopicArn:          stringPtr(topicArn),
		Message:           stringPtr(string(rawEvent)),
		MessageAttributes: messageAttributes,
	})

	return err
}

func stringPtr(s string) *string {
	return &s
}