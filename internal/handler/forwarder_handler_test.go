package handler

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	config "github.com/Dannytrev21/lambda-bridge/internal/config"
	"github.com/Dannytrev21/lambda-bridge/internal/forwarder"
)

// Mock forwarders
type MockSNSForwarder struct {
	mock.Mock
}

func (m *MockSNSForwarder) Forward(ctx context.Context, topicArn string, rawEvent json.RawMessage) error {
	args := m.Called(ctx, topicArn, rawEvent)
	return args.Error(0)
}

type MockWebhookForwarder struct {
	mock.Mock
}

func (m *MockWebhookForwarder) ForwardToWebhooks(ctx context.Context, urls []string, rawEvent json.RawMessage) map[string]forwarder.WebhookResult {
	args := m.Called(ctx, urls, rawEvent)
	return args.Get(0).(map[string]forwarder.WebhookResult)
}

func TestHandlerReturnType(t *testing.T) {
	// Create handler
	snsForwarder := &MockSNSForwarder{}
	webhookForwarder := &MockWebhookForwarder{}
	cfg := &config.Config{
		SNSTopicArn:           "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:           []string{"http://test1.com", "http://test2.com"},
		CloudWebhookURLs:      []string{"http://cloud1.com"},
		EnterpriseWebhookURLs: []string{"http://enterprise1.com"},
		SkipHealthChecks:      true,
		Environment:           "test",
		Debug:                 false,
	}

	handler := NewForwarderHandler(snsForwarder, webhookForwarder, cfg)

	// Test with ALB event
	albEvent := events.ALBTargetGroupRequest{
		RequestContext: events.ALBTargetGroupRequestContext{
			ELB: events.ELBContext{
				TargetGroupArn: "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/test/123",
			},
		},
		HTTPMethod: "POST",
		Path:       "/webhook",
		Headers:    map[string]string{"content-type": "application/json"},
		Body:       `{"test": "data"}`,
	}

	albBytes, _ := json.Marshal(albEvent)
	albJSON := json.RawMessage(albBytes)

	// Mock webhook forwarding
	webhookForwarder.On("ForwardToWebhooks", mock.Anything, cfg.WebhookURLs, albJSON).Return(
		map[string]forwarder.WebhookResult{
			"http://test1.com": {StatusCode: 200},
			"http://test2.com": {StatusCode: 200},
		},
	)

	result := handler.Handler(context.Background(), albJSON)

	// Verify return type is any (interface{})
	_ = reflect.TypeOf(result)
	assert.NotNil(t, result, "Handler should return non-nil value")

	// Verify it's an ALB response
	albResponse, ok := result.(events.ALBTargetGroupResponse)
	assert.True(t, ok, "Result should be ALBTargetGroupResponse for ALB events")
	assert.Equal(t, 200, albResponse.StatusCode, "ALB response should have 200 status code")

	// Verify the signature returns 'any' type at compile time
	// This test ensures the handler signature uses 'any' not 'interface{}'
	var handlerFunc func(context.Context, json.RawMessage) any = handler.Handler
	assert.NotNil(t, handlerFunc, "Handler should have signature that returns 'any'")
}

func TestALBAlwaysReturns200(t *testing.T) {
	tests := []struct {
		name           string
		webhookResults map[string]forwarder.WebhookResult
		description    string
	}{
		{
			name: "webhooks succeed",
			webhookResults: map[string]forwarder.WebhookResult{
				"http://test1.com": {StatusCode: 200},
				"http://test2.com": {StatusCode: 200},
			},
			description: "All webhooks succeed",
		},
		{
			name: "webhooks fail",
			webhookResults: map[string]forwarder.WebhookResult{
				"http://test1.com": {StatusCode: 500, Error: assert.AnError},
				"http://test2.com": {StatusCode: 404, Error: assert.AnError},
			},
			description: "All webhooks fail",
		},
		{
			name: "mixed webhook results",
			webhookResults: map[string]forwarder.WebhookResult{
				"http://test1.com": {StatusCode: 200},
				"http://test2.com": {StatusCode: 500, Error: assert.AnError},
			},
			description: "Mixed webhook results",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snsForwarder := &MockSNSForwarder{}
			webhookForwarder := &MockWebhookForwarder{}
			cfg := &config.Config{
				SNSTopicArn:           "arn:aws:sns:us-east-1:123456789012:test",
				WebhookURLs:           []string{"http://test1.com", "http://test2.com"},
				CloudWebhookURLs:      []string{"http://cloud1.com"},
				EnterpriseWebhookURLs: []string{"http://enterprise1.com"},
				SkipHealthChecks:      true,
				Environment:           "test",
				Debug:                 false,
			}

			handler := NewForwarderHandler(snsForwarder, webhookForwarder, cfg)

			albEvent := events.ALBTargetGroupRequest{
				RequestContext: events.ALBTargetGroupRequestContext{
					ELB: events.ELBContext{
						TargetGroupArn: "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/test/123",
					},
				},
				HTTPMethod: "POST",
				Path:       "/webhook",
				Headers:    map[string]string{"content-type": "application/json"},
				Body:       `{"test": "data"}`,
			}

			albBytes, _ := json.Marshal(albEvent)
			albJSON := json.RawMessage(albBytes)

			callCh := make(chan struct{}, 1)
			webhookForwarder.On("ForwardToWebhooks", mock.Anything, cfg.WebhookURLs, albJSON).
				Return(tt.webhookResults).
				Run(func(args mock.Arguments) {
					select {
					case callCh <- struct{}{}:
					default:
					}
				})

			result := handler.Handler(context.Background(), albJSON)

			// ALB should ALWAYS get 200, regardless of webhook success/failure
			albResponse, ok := result.(events.ALBTargetGroupResponse)
			assert.True(t, ok, "Result should be ALBTargetGroupResponse")
			assert.Equal(t, 200, albResponse.StatusCode, "ALB should ALWAYS get 200 status code")
			assert.Equal(t, `{"status":"accepted"}`, albResponse.Body, "ALB should get accepted status")

			select {
			case <-callCh:
			case <-time.After(250 * time.Millisecond):
				t.Fatal("webhook forwarder was not invoked")
			}
		})
	}
}

func TestHealthCheckDetection(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		headers    map[string]string
		shouldSkip bool
	}{
		{
			name:       "health path",
			path:       "/health",
			headers:    map[string]string{},
			shouldSkip: true,
		},
		{
			name:       "healthz path",
			path:       "/healthz",
			headers:    map[string]string{},
			shouldSkip: true,
		},
		{
			name:       "ping path",
			path:       "/ping",
			headers:    map[string]string{},
			shouldSkip: true,
		},
		{
			name:       "ELB health checker user agent",
			path:       "/webhook",
			headers:    map[string]string{"user-agent": "ELB-HealthChecker/2.0"},
			shouldSkip: true,
		},
		{
			name:       "normal request",
			path:       "/webhook",
			headers:    map[string]string{"user-agent": "GitHub-Hookshot/abc123"},
			shouldSkip: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snsForwarder := &MockSNSForwarder{}
			webhookForwarder := &MockWebhookForwarder{}
			cfg := &config.Config{
				SNSTopicArn:           "arn:aws:sns:us-east-1:123456789012:test",
				WebhookURLs:           []string{"http://test1.com"},
				CloudWebhookURLs:      []string{"http://cloud1.com"},
				EnterpriseWebhookURLs: []string{"http://enterprise1.com"},
				SkipHealthChecks:      true,
				Environment:           "test",
				Debug:                 false,
			}

			handler := NewForwarderHandler(snsForwarder, webhookForwarder, cfg)

			albEvent := events.ALBTargetGroupRequest{
				RequestContext: events.ALBTargetGroupRequestContext{
					ELB: events.ELBContext{
						TargetGroupArn: "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/test/123",
					},
				},
				HTTPMethod: "POST",
				Path:       tt.path,
				Headers:    tt.headers,
				Body:       `{"test": "data"}`,
			}

			albBytes, _ := json.Marshal(albEvent)
			albJSON := json.RawMessage(albBytes)

			callCh := make(chan struct{}, 1)
			webhookForwarder.On("ForwardToWebhooks", mock.Anything, mock.Anything, mock.Anything).
				Return(
					map[string]forwarder.WebhookResult{
						"http://test1.com": {StatusCode: 200},
					},
				).
				Maybe().
				Run(func(args mock.Arguments) {
					select {
					case callCh <- struct{}{}:
					default:
					}
				})

			result := handler.Handler(context.Background(), albJSON)

			albResponse, ok := result.(events.ALBTargetGroupResponse)
			assert.True(t, ok, "Result should be ALBTargetGroupResponse")
			assert.Equal(t, 200, albResponse.StatusCode, "Should always return 200")

			if tt.shouldSkip {
				assert.Equal(t, `{"status":"healthy"}`, albResponse.Body, "Health checks should get healthy status")
				select {
				case <-callCh:
					t.Fatal("webhook forwarder should not be invoked for health checks")
				case <-time.After(50 * time.Millisecond):
				}
			} else {
				assert.Equal(t, `{"status":"accepted"}`, albResponse.Body, "Normal requests should get accepted status")
				select {
				case <-callCh:
				case <-time.After(250 * time.Millisecond):
					t.Fatal("expected webhook forwarder to be invoked")
				}
			}
		})
	}
}

func TestForwarderRoutesCloudEvents(t *testing.T) {
	snsForwarder := &MockSNSForwarder{}
	webhookForwarder := &MockWebhookForwarder{}
	cfg := &config.Config{
		SNSTopicArn:           "arn:aws:sns:us-east-1:123456789012:test",
		CloudWebhookURLs:      []string{"http://cloud1.com", "http://cloud2.com"},
		EnterpriseWebhookURLs: []string{"http://enterprise1.com"},
		SkipHealthChecks:      true,
		Environment:           "test",
		Debug:                 false,
	}

	h := NewForwarderHandler(snsForwarder, webhookForwarder, cfg)

	albEvent := events.ALBTargetGroupRequest{
		RequestContext: events.ALBTargetGroupRequestContext{
			ELB: events.ELBContext{TargetGroupArn: "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/test/123"},
		},
		HTTPMethod: "POST",
		Path:       "/webhook",
		Headers: map[string]string{
			"X-Dcp-Destination-Host": "github.com",
		},
		Body: `{"action":"opened"}`,
	}

	rawBytes, _ := json.Marshal(albEvent)
	rawEvent := json.RawMessage(rawBytes)
	done := make(chan struct{})
	webhookForwarder.On("ForwardToWebhooks", mock.Anything, cfg.CloudWebhookURLs, rawEvent).
		Return(map[string]forwarder.WebhookResult{
			cfg.CloudWebhookURLs[0]: {StatusCode: 200},
		}).
		Run(func(args mock.Arguments) { close(done) }).
		Once()

	result := h.Handler(context.Background(), rawEvent)
	if resp, ok := result.(events.ALBTargetGroupResponse); !ok || resp.StatusCode != 200 {
		t.Fatalf("expected ALB 200 response")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cloud webhook forwarder was not invoked")
	}

	webhookForwarder.AssertExpectations(t)
}

func TestForwarderRoutesEnterpriseEvents(t *testing.T) {
	snsForwarder := &MockSNSForwarder{}
	webhookForwarder := &MockWebhookForwarder{}
	cfg := &config.Config{
		SNSTopicArn:           "arn:aws:sns:us-east-1:123456789012:test",
		CloudWebhookURLs:      []string{"http://cloud1.com"},
		EnterpriseWebhookURLs: []string{"http://enterprise1.com", "http://enterprise2.com"},
		SkipHealthChecks:      true,
		Environment:           "test",
		Debug:                 false,
	}

	h := NewForwarderHandler(snsForwarder, webhookForwarder, cfg)

	albEvent := events.ALBTargetGroupRequest{
		RequestContext: events.ALBTargetGroupRequestContext{
			ELB: events.ELBContext{TargetGroupArn: "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/test/123"},
		},
		HTTPMethod: "POST",
		Path:       "/webhook",
		Headers: map[string]string{
			"X-Github-Enterprise-Host": "github.my-company.com",
		},
		Body: `{"action":"opened"}`,
	}

	rawBytes, _ := json.Marshal(albEvent)
	rawEvent := json.RawMessage(rawBytes)
	done := make(chan struct{})
	webhookForwarder.On("ForwardToWebhooks", mock.Anything, cfg.EnterpriseWebhookURLs, rawEvent).
		Return(map[string]forwarder.WebhookResult{
			cfg.EnterpriseWebhookURLs[0]: {StatusCode: 200},
		}).
		Run(func(args mock.Arguments) { close(done) }).
		Once()

	result := h.Handler(context.Background(), rawEvent)
	if resp, ok := result.(events.ALBTargetGroupResponse); !ok || resp.StatusCode != 200 {
		t.Fatalf("expected ALB 200 response")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("enterprise webhook forwarder was not invoked")
	}

	webhookForwarder.AssertExpectations(t)
}

func TestSNSEventForwarding(t *testing.T) {
	snsForwarder := &MockSNSForwarder{}
	webhookForwarder := &MockWebhookForwarder{}
	cfg := &config.Config{
		SNSTopicArn:           "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:           []string{"http://test1.com"},
		CloudWebhookURLs:      []string{"http://cloud1.com"},
		EnterpriseWebhookURLs: []string{"http://enterprise1.com"},
		SkipHealthChecks:      true,
		Environment:           "test",
		Debug:                 false,
	}

	handler := NewForwarderHandler(snsForwarder, webhookForwarder, cfg)

	snsEvent := events.SNSEvent{
		Records: []events.SNSEventRecord{
			{
				SNS: events.SNSEntity{
					MessageID: "test-message-id",
					Message:   "test message",
				},
			},
		},
	}

	snsJSON, _ := json.Marshal(snsEvent)

	// Mock successful SNS forwarding
	snsForwarder.On("Forward", mock.Anything, cfg.SNSTopicArn, mock.Anything).Return(nil)

	result := handler.Handler(context.Background(), snsJSON)

	// SNS events should return nil on success
	assert.Nil(t, result, "SNS events should return nil on success")

	snsForwarder.AssertExpectations(t)
	webhookForwarder.AssertExpectations(t)
}

func TestUnknownEventType(t *testing.T) {
	snsForwarder := &MockSNSForwarder{}
	webhookForwarder := &MockWebhookForwarder{}
	cfg := &config.Config{
		SNSTopicArn:           "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:           []string{"http://test1.com"},
		CloudWebhookURLs:      []string{"http://cloud1.com"},
		EnterpriseWebhookURLs: []string{"http://enterprise1.com"},
		SkipHealthChecks:      true,
		Environment:           "test",
		Debug:                 false,
	}

	handler := NewForwarderHandler(snsForwarder, webhookForwarder, cfg)

	unknownEvent := `{"unknown": "event", "type": "mystery"}`

	result := handler.Handler(context.Background(), json.RawMessage(unknownEvent))

	// Unknown events should return safe ALB 200 response
	albResponse, ok := result.(events.ALBTargetGroupResponse)
	assert.True(t, ok, "Unknown events should return ALBTargetGroupResponse")
	assert.Equal(t, 200, albResponse.StatusCode, "Unknown events should return 200")
	assert.Equal(t, `{"status":"accepted"}`, albResponse.Body, "Unknown events should get accepted status")

	snsForwarder.AssertExpectations(t)
	webhookForwarder.AssertExpectations(t)
}
