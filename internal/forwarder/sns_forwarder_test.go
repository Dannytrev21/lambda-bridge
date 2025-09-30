package forwarder

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock SNS Client
type MockSNSClient struct {
	mock.Mock
}

func (m *MockSNSClient) Publish(ctx context.Context, input *sns.PublishInput, opts ...func(*sns.Options)) (*sns.PublishOutput, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*sns.PublishOutput), args.Error(1)
}

func TestSNSForwarder_Forward_Success(t *testing.T) {
	mockClient := &MockSNSClient{}
	forwarder := NewSNSForwarder(mockClient)

	topicArn := "arn:aws:sns:us-east-1:123456789012:test-topic"
	rawEvent := json.RawMessage(`{"test": "event", "data": "value"}`)

	// Mock successful publish
	mockClient.On("Publish", mock.Anything, mock.MatchedBy(func(input *sns.PublishInput) bool {
		return *input.TopicArn == topicArn &&
			*input.Message == string(rawEvent) &&
			input.MessageAttributes != nil &&
			len(input.MessageAttributes) > 0
	})).Return(&sns.PublishOutput{MessageId: stringPtr("message-123")}, nil)

	err := forwarder.Forward(context.Background(), topicArn, rawEvent)

	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestSNSForwarder_Forward_Error(t *testing.T) {
	mockClient := &MockSNSClient{}
	forwarder := NewSNSForwarder(mockClient)

	topicArn := "arn:aws:sns:us-east-1:123456789012:test-topic"
	rawEvent := json.RawMessage(`{"test": "event"}`)

	// Mock publish error
	mockClient.On("Publish", mock.Anything, mock.Anything).Return(
		nil,
		assert.AnError,
	)

	err := forwarder.Forward(context.Background(), topicArn, rawEvent)

	assert.Error(t, err)
	mockClient.AssertExpectations(t)
}

func TestSNSForwarder_Forward_MessageAttributes(t *testing.T) {
	mockClient := &MockSNSClient{}
	forwarder := NewSNSForwarder(mockClient)

	topicArn := "arn:aws:sns:us-east-1:123456789012:test-topic"
	rawEvent := json.RawMessage(`{"test": "event"}`)

	// Capture the publish input to verify attributes
	var capturedInput *sns.PublishInput
	mockClient.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		capturedInput = args.Get(1).(*sns.PublishInput)
	}).Return(&sns.PublishOutput{MessageId: stringPtr("message-123")}, nil)

	err := forwarder.Forward(context.Background(), topicArn, rawEvent)

	assert.NoError(t, err)
	assert.NotNil(t, capturedInput)
	assert.Contains(t, capturedInput.MessageAttributes, "ForwardedAt")

	forwardedAtAttr, exists := capturedInput.MessageAttributes["ForwardedAt"]
	assert.True(t, exists, "ForwardedAt attribute should exist")
	assert.Equal(t, "String", *forwardedAtAttr.DataType)
	assert.NotEmpty(t, *forwardedAtAttr.StringValue)

	mockClient.AssertExpectations(t)
}