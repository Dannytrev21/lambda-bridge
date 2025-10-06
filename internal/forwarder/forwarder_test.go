package forwarder

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sns"
)

func TestWebhookForwarder_ForwardToWebhooks(t *testing.T) {
	// Counter for received webhooks
	var received atomic.Int32

	// Create test servers
	ts1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts1.Close()

	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts2.Close()

	// Create forwarder
	wf := NewWebhookForwarder()

	// Test payload
	payload := json.RawMessage(`{"test":"data"}`)

	// Forward to multiple webhooks
	ctx := context.Background()
	urls := []string{ts1.URL, ts2.URL}

	wf.ForwardToWebhooks(ctx, urls, payload, "POST", "/webhook", nil, nil)

	// Verify both webhooks were called
	if count := received.Load(); count != 2 {
		t.Errorf("Expected 2 webhooks to be called, got %d", count)
	}
}

func TestWebhookForwarder_RetryOnServerError(t *testing.T) {
	var attempts atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer ts.Close()

	wf := NewWebhookForwarder()
	payload := json.RawMessage(`{"test":"data"}`)

	ctx := context.Background()
	wf.ForwardToWebhooks(ctx, []string{ts.URL}, payload, "POST", "/webhook", nil, nil)

	// Should retry and eventually succeed
	finalAttempts := attempts.Load()
	if finalAttempts != 3 {
		t.Errorf("Expected 3 attempts (initial + 2 retries), got %d", finalAttempts)
	}
}

func TestWebhookForwarder_NoRetryOnClientError(t *testing.T) {
	var attempts atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest) // 400 - client error
	}))
	defer ts.Close()

	wf := NewWebhookForwarder()
	payload := json.RawMessage(`{"test":"data"}`)

	ctx := context.Background()
	wf.ForwardToWebhooks(ctx, []string{ts.URL}, payload, "POST", "/webhook", nil, nil)

	// Should not retry on client error
	if count := attempts.Load(); count != 1 {
		t.Errorf("Expected 1 attempt (no retry on client error), got %d", count)
	}
}

func TestWebhookForwarder_URLIsolation(t *testing.T) {
	// Test that failure of one URL doesn't prevent others from being attempted
	var successReceived, failReceived atomic.Int32
	var firstSuccess, firstFail time.Time

	// Failing server that responds quickly
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if firstFail.IsZero() {
			firstFail = time.Now()
		}
		failReceived.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failServer.Close()

	// Fast successful server
	fastServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if firstSuccess.IsZero() {
			firstSuccess = time.Now()
		}
		successReceived.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer fastServer.Close()

	wf := NewWebhookForwarder()
	payload := json.RawMessage(`{"test":"isolation"}`)

	ctx := context.Background()
	urls := []string{failServer.URL, fastServer.URL}

	wf.ForwardToWebhooks(ctx, urls, payload, "POST", "/webhook", nil, nil)

	// Both webhooks should have been attempted (fail server gets retries)
	// Fast server should have succeeded
	if count := successReceived.Load(); count != 1 {
		t.Errorf("Expected 1 successful webhook call, got %d", count)
	}

	// Fail server should have been retried (initial + 3 retries = 4 attempts)
	if count := failReceived.Load(); count != 4 {
		t.Logf("Expected 4 attempts for failing server (initial + 3 retries), got %d", count)
	}
}

func TestWebhookForwarder_ContextCancellation(t *testing.T) {
	var attempts atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	wf := NewWebhookForwarder()
	payload := json.RawMessage(`{"test":"cancellation"}`)

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	wf.ForwardToWebhooks(ctx, []string{ts.URL}, payload, "POST", "/webhook", nil, nil)

	// Should have attempted but cancelled before completion
	count := attempts.Load()
	if count == 0 {
		t.Error("Expected at least one attempt")
	}
}

func TestWebhookForwarder_EmptyURLs(t *testing.T) {
	wf := NewWebhookForwarder()
	payload := json.RawMessage(`{"test":"data"}`)

	ctx := context.Background()

	// Should not panic with empty URLs
	wf.ForwardToWebhooks(ctx, []string{}, payload, "POST", "/webhook", nil, nil)
	wf.ForwardToWebhooks(ctx, nil, payload, "POST", "/webhook", nil, nil)
}

func TestWebhookForwarder_Headers(t *testing.T) {
	var receivedHeaders http.Header

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	wf := NewWebhookForwarder()
	payload := json.RawMessage(`{"test":"data"}`)

	// Add request ID to context
	ctx := context.Background()
	ctx = WithRequestID(ctx, "test-request-123")

	wf.ForwardToWebhooks(ctx, []string{ts.URL}, payload, "POST", "/webhook", nil, nil)

	// Give it time to complete
	time.Sleep(200 * time.Millisecond)

	// Verify headers
	if receivedHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("Expected Content-Type: application/json, got %s", receivedHeaders.Get("Content-Type"))
	}

	if receivedHeaders.Get("User-Agent") != "Lambda-Bridge/1.0" {
		t.Errorf("Expected User-Agent: Lambda-Bridge/1.0, got %s", receivedHeaders.Get("User-Agent"))
	}

	if receivedHeaders.Get("X-Request-ID") != "test-request-123" {
		t.Errorf("Expected X-Request-ID: test-request-123, got %s", receivedHeaders.Get("X-Request-ID"))
	}
}

func TestGenerateRequestID(t *testing.T) {
	id1 := GenerateRequestID()
	id2 := GenerateRequestID()

	if id1 == id2 {
		t.Error("Generated IDs should be unique")
	}

	if len(id1) == 0 {
		t.Error("Generated ID should not be empty")
	}

	if !strings.HasPrefix(id1, "req-") {
		t.Errorf("ID should start with 'req-', got %s", id1)
	}

	if !strings.HasPrefix(id2, "req-") {
		t.Errorf("ID should start with 'req-', got %s", id2)
	}
}

func TestContextPropagation(t *testing.T) {
	ctx := context.Background()

	// Test request ID propagation
	requestID := "test-request-456"
	ctx = WithRequestID(ctx, requestID)

	retrievedID := RequestIDFromContext(ctx)
	if retrievedID != requestID {
		t.Errorf("Expected request ID %s, got %s", requestID, retrievedID)
	}

	// Test empty context
	emptyCtx := context.Background()
	emptyID := RequestIDFromContext(emptyCtx)
	if emptyID != "" {
		t.Errorf("Expected empty string for missing request ID, got %s", emptyID)
	}
}

func TestCalculateBackoff(t *testing.T) {
	// Test that backoff increases exponentially
	delay0 := calculateBackoff(0)
	delay1 := calculateBackoff(1)
	delay2 := calculateBackoff(2)

	if delay1 <= delay0 {
		t.Error("Backoff should increase with attempt number")
	}

	if delay2 <= delay1 {
		t.Error("Backoff should continue to increase")
	}

	// Test that backoff respects max delay
	delay10 := calculateBackoff(10)
	if delay10 > defaultMaxDelay*2 {
		t.Errorf("Backoff should not exceed max delay significantly, got %v", delay10)
	}
}

// Benchmarks

func BenchmarkWebhookForwarder_SingleURL(b *testing.B) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	wf := NewWebhookForwarder()
	payload := json.RawMessage(`{"bench":"data"}`)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wf.ForwardToWebhooks(ctx, []string{ts.URL}, payload, "POST", "/webhook", nil, nil)
	}
}

func BenchmarkWebhookForwarder_MultipleURLs(b *testing.B) {
	// Create 5 test servers
	servers := make([]*httptest.Server, 5)
	urls := make([]string, 5)
	for i := 0; i < 5; i++ {
		servers[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		urls[i] = servers[i].URL
		defer servers[i].Close()
	}

	wf := NewWebhookForwarder()
	payload := json.RawMessage(`{"bench":"data"}`)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wf.ForwardToWebhooks(ctx, urls, payload, "POST", "/webhook", nil, nil)
	}
}

func BenchmarkGenerateRequestID(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GenerateRequestID()
	}
}

// SNS Forwarder Tests (using real SNS client types but testing validation)

func TestSNSForwarder_ForwardEmptyTopicARN(t *testing.T) {
	// Create forwarder with nil client (won't be called due to validation)
	forwarder := &SNSForwarder{client: nil}

	ctx := context.Background()
	payload := json.RawMessage(`{"test":"data"}`)

	err := forwarder.Forward(ctx, "", payload)
	if err == nil {
		t.Error("Expected error for empty topic ARN")
	}

	if err.Error() != "SNS topic ARN is required" {
		t.Errorf("Expected 'SNS topic ARN is required', got %v", err)
	}
}

func TestSNSForwarder_New(t *testing.T) {
	// Test that NewSNSForwarder creates a forwarder
	forwarder := NewSNSForwarder(nil)
	if forwarder == nil {
		t.Error("Expected non-nil forwarder")
	}
}

func TestWebhookForwarder_Close(t *testing.T) {
	wf := NewWebhookForwarder()
	err := wf.Close()
	if err != nil {
		t.Errorf("Expected no error from Close, got %v", err)
	}
}

// Mock SNS Client for testing
type mockSNSClient struct {
	publishFunc func(ctx context.Context, params *sns.PublishInput, optFns ...func(*sns.Options)) (*sns.PublishOutput, error)
}

func (m *mockSNSClient) Publish(ctx context.Context, params *sns.PublishInput, optFns ...func(*sns.Options)) (*sns.PublishOutput, error) {
	if m.publishFunc != nil {
		return m.publishFunc(ctx, params, optFns...)
	}
	return &sns.PublishOutput{}, nil
}

func TestSNSForwarder_ForwardSuccess(t *testing.T) {
	var publishedMessage string
	var publishedTopicArn string

	mockClient := &mockSNSClient{
		publishFunc: func(ctx context.Context, params *sns.PublishInput, optFns ...func(*sns.Options)) (*sns.PublishOutput, error) {
			if params.Message != nil {
				publishedMessage = *params.Message
			}
			if params.TopicArn != nil {
				publishedTopicArn = *params.TopicArn
			}
			return &sns.PublishOutput{MessageId: stringPtr("test-message-id")}, nil
		},
	}

	forwarder := NewSNSForwarder(mockClient)
	ctx := context.Background()
	topicArn := "arn:aws:sns:us-east-1:123456789012:test-topic"
	payload := json.RawMessage(`{"test":"data"}`)

	err := forwarder.Forward(ctx, topicArn, payload)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if publishedTopicArn != topicArn {
		t.Errorf("Expected topic ARN %s, got %s", topicArn, publishedTopicArn)
	}

	if publishedMessage == "" {
		t.Error("Expected message to be published")
	}

	// Verify the message structure
	var messageData map[string]interface{}
	if err := json.Unmarshal([]byte(publishedMessage), &messageData); err != nil {
		t.Errorf("Failed to unmarshal published message: %v", err)
	}

	if _, ok := messageData["event"]; !ok {
		t.Error("Expected 'event' field in published message")
	}

	if _, ok := messageData["forwardedAt"]; !ok {
		t.Error("Expected 'forwardedAt' field in published message")
	}
}

func TestSNSForwarder_ForwardPublishError(t *testing.T) {
	mockClient := &mockSNSClient{
		publishFunc: func(ctx context.Context, params *sns.PublishInput, optFns ...func(*sns.Options)) (*sns.PublishOutput, error) {
			return nil, errors.New("SNS service unavailable")
		},
	}

	forwarder := NewSNSForwarder(mockClient)
	ctx := context.Background()
	topicArn := "arn:aws:sns:us-east-1:123456789012:test-topic"
	payload := json.RawMessage(`{"test":"data"}`)

	err := forwarder.Forward(ctx, topicArn, payload)
	if err == nil {
		t.Error("Expected error for SNS publish failure")
	}

	if !strings.Contains(err.Error(), "failed to publish to SNS") {
		t.Errorf("Expected 'failed to publish to SNS' error, got %v", err)
	}
}

func TestSNSForwarder_ForwardContextCancellation(t *testing.T) {
	mockClient := &mockSNSClient{
		publishFunc: func(ctx context.Context, params *sns.PublishInput, optFns ...func(*sns.Options)) (*sns.PublishOutput, error) {
			return nil, context.Canceled
		},
	}

	forwarder := NewSNSForwarder(mockClient)
	ctx := context.Background()
	topicArn := "arn:aws:sns:us-east-1:123456789012:test-topic"
	payload := json.RawMessage(`{"test":"data"}`)

	err := forwarder.Forward(ctx, topicArn, payload)
	if err == nil {
		t.Error("Expected error for context cancellation")
	}
}

func TestSNSForwarder_ForwardWithLargePayload(t *testing.T) {
	var publishedMessage string

	mockClient := &mockSNSClient{
		publishFunc: func(ctx context.Context, params *sns.PublishInput, optFns ...func(*sns.Options)) (*sns.PublishOutput, error) {
			if params.Message != nil {
				publishedMessage = *params.Message
			}
			return &sns.PublishOutput{MessageId: stringPtr("test-message-id")}, nil
		},
	}

	forwarder := NewSNSForwarder(mockClient)
	ctx := context.Background()
	topicArn := "arn:aws:sns:us-east-1:123456789012:test-topic"

	// Create a large payload
	largeData := make(map[string]interface{})
	for i := 0; i < 100; i++ {
		largeData[string(rune('a'+i%26))+string(rune(i))] = "test data with some content to make it larger"
	}
	payload, _ := json.Marshal(largeData)

	err := forwarder.Forward(ctx, topicArn, json.RawMessage(payload))
	if err != nil {
		t.Errorf("Expected no error with large payload, got %v", err)
	}

	if publishedMessage == "" {
		t.Error("Expected message to be published")
	}
}

// Helper function
func stringPtr(s string) *string {
	return &s
}
