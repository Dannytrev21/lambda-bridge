package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/Dannytrev21/lambda-bridge/internal/config"
)

func TestHandler_HandleALBEvent(t *testing.T) {
	// Setup test server to receive webhooks
	var received atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type: application/json, got %s", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("User-Agent") != "Lambda-Bridge/1.0" {
			t.Errorf("Expected User-Agent: Lambda-Bridge/1.0, got %s", r.Header.Get("User-Agent"))
		}

		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Create handler with test config
	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{ts.URL},
		Debug:            false,
	}

	h := NewHandler(cfg, nil)

	// Test ALB event
	albEvent := events.ALBTargetGroupRequest{
		RequestContext: events.ALBTargetGroupRequestContext{
			ELB: events.ELBContext{
				TargetGroupArn: "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/test/1234567890",
			},
		},
		Headers: map[string]string{
			"content-type": "application/json",
		},
		Path: "/webhook",
		Body: `{"test":"data"}`,
	}

	rawEvent, _ := json.Marshal(albEvent)
	ctx := context.Background()

	// Handle event
	response := h.Handle(ctx, rawEvent)

	// Verify response is ALB response with 200
	albResp, ok := response.(events.ALBTargetGroupResponse)
	if !ok {
		t.Fatal("Expected ALBTargetGroupResponse")
	}

	if albResp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", albResp.StatusCode)
	}

	if albResp.Body != `{"status":"accepted"}` {
		t.Errorf("Expected status:accepted body, got %s", albResp.Body)
	}

	// Wait for webhook to be received (async)
	time.Sleep(500 * time.Millisecond)

	if count := received.Load(); count != 1 {
		t.Errorf("Expected 1 webhook call, got %d", count)
	}
}

func TestHandler_HandleHealthCheck(t *testing.T) {
	cfg := &config.Config{
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{"https://example.com"},
	}

	h := NewHandler(cfg, nil)

	tests := []struct {
		name    string
		headers map[string]string
		path    string
		want    string
	}{
		{
			name:    "health check via user-agent",
			headers: map[string]string{"user-agent": "ELB-HealthChecker/2.0"},
			path:    "/webhook",
			want:    "healthy",
		},
		{
			name:    "health check via path /health",
			headers: map[string]string{},
			path:    "/health",
			want:    "healthy",
		},
		{
			name:    "health check via path /healthz",
			headers: map[string]string{},
			path:    "/healthz",
			want:    "healthy",
		},
		{
			name:    "health check via path /ping",
			headers: map[string]string{},
			path:    "/ping",
			want:    "healthy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			albEvent := events.ALBTargetGroupRequest{
				RequestContext: events.ALBTargetGroupRequestContext{
					ELB: events.ELBContext{
						TargetGroupArn: "arn:test",
					},
				},
				Headers: tt.headers,
				Path:    tt.path,
			}

			rawEvent, _ := json.Marshal(albEvent)
			response := h.Handle(context.Background(), rawEvent)

			albResp := response.(events.ALBTargetGroupResponse)
			if albResp.StatusCode != 200 {
				t.Errorf("Expected status 200 for health check, got %d", albResp.StatusCode)
			}
			if albResp.Body != tt.want {
				t.Errorf("Expected '%s' body, got %s", tt.want, albResp.Body)
			}
		})
	}
}

func TestHandler_HandleUnknownEvent(t *testing.T) {
	cfg := &config.Config{
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs: []string{"https://example.com"},
	}

	h := NewHandler(cfg, nil)

	// Send invalid JSON that doesn't match any event type
	rawEvent := json.RawMessage(`{"unknown":"event"}`)
	response := h.Handle(context.Background(), rawEvent)

	// Should return safe ALB response
	albResp, ok := response.(events.ALBTargetGroupResponse)
	if !ok {
		t.Fatal("Expected ALBTargetGroupResponse for unknown event")
	}

	if albResp.StatusCode != 200 {
		t.Errorf("Expected status 200 for unknown event, got %d", albResp.StatusCode)
	}
}

func TestHandler_RouteSelection(t *testing.T) {
	var cloudReceived, enterpriseReceived, defaultReceived atomic.Int32

	// Cloud webhook server
	cloudServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cloudReceived.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer cloudServer.Close()

	// Enterprise webhook server
	enterpriseServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enterpriseReceived.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer enterpriseServer.Close()

	// Default webhook server
	defaultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defaultReceived.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer defaultServer.Close()

	cfg := &config.Config{
		Environment:           "test",
		SNSTopicArn:           "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:           []string{defaultServer.URL},
		CloudWebhookURLs:      []string{cloudServer.URL},
		EnterpriseWebhookURLs: []string{enterpriseServer.URL},
	}

	h := NewHandler(cfg, nil)

	tests := []struct {
		name              string
		headers           map[string]string
		expectCloud       bool
		expectEnterprise  bool
		expectDefault     bool
	}{
		{
			name:          "cloud destination header",
			headers:       map[string]string{"x-dcp-destination-host": "cloud.example.com"},
			expectCloud:   true,
			expectDefault: false,
		},
		{
			name:             "enterprise header",
			headers:          map[string]string{"x-github-enterprise-host": "github.enterprise.com"},
			expectEnterprise: true,
			expectDefault:    false,
		},
		{
			name:          "no special headers - default route",
			headers:       map[string]string{},
			expectDefault: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset counters
			cloudReceived.Store(0)
			enterpriseReceived.Store(0)
			defaultReceived.Store(0)

			albEvent := events.ALBTargetGroupRequest{
				RequestContext: events.ALBTargetGroupRequestContext{
					ELB: events.ELBContext{
						TargetGroupArn: "arn:test",
					},
				},
				Headers: tt.headers,
				Path:    "/webhook",
				Body:    `{"test":"routing"}`,
			}

			rawEvent, _ := json.Marshal(albEvent)
			h.Handle(context.Background(), rawEvent)

			// Wait for async forwarding
			time.Sleep(300 * time.Millisecond)

			if tt.expectCloud && cloudReceived.Load() != 1 {
				t.Errorf("Expected cloud webhook to be called once, got %d", cloudReceived.Load())
			}
			if tt.expectEnterprise && enterpriseReceived.Load() != 1 {
				t.Errorf("Expected enterprise webhook to be called once, got %d", enterpriseReceived.Load())
			}
			if tt.expectDefault && defaultReceived.Load() != 1 {
				t.Errorf("Expected default webhook to be called once, got %d", defaultReceived.Load())
			}
		})
	}
}

func TestHandler_BurstHandling(t *testing.T) {
	var received atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		time.Sleep(50 * time.Millisecond) // Simulate slow webhook
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{ts.URL},
	}

	h := NewHandler(cfg, nil)

	// Send 15 concurrent events (more than MaxWorkers=10)
	// Queue (size 100) should buffer them, workers process them
	ctx := context.Background()
	numRequests := 15

	for i := 0; i < numRequests; i++ {
		albEvent := events.ALBTargetGroupRequest{
			RequestContext: events.ALBTargetGroupRequestContext{
				ELB: events.ELBContext{
					TargetGroupArn: "arn:test",
				},
			},
			Path: "/webhook",
			Body: `{"burst":"test"}`,
		}

		rawEvent, _ := json.Marshal(albEvent)
		h.Handle(ctx, rawEvent)
	}

	// Wait for queue to be processed
	// With 10 workers processing 15 jobs at 50ms each, should complete quickly
	time.Sleep(2 * time.Second)

	// All requests should be queued and processed (queue size 100 > 15 requests)
	count := received.Load()
	if count != int32(numRequests) {
		t.Errorf("Expected all %d requests to be processed (queue buffering), got %d", numRequests, count)
	}
}

func TestHandler_QueueOverflow(t *testing.T) {
	var received atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		time.Sleep(200 * time.Millisecond) // Very slow webhook
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{ts.URL},
	}

	h := NewHandler(cfg, nil)

	// Send more requests than queue can hold (QueueSize=100)
	// This tests queue overflow behavior
	ctx := context.Background()
	numRequests := QueueSize + 20 // 120 requests, queue holds 100

	for i := 0; i < numRequests; i++ {
		albEvent := events.ALBTargetGroupRequest{
			RequestContext: events.ALBTargetGroupRequestContext{
				ELB: events.ELBContext{
					TargetGroupArn: "arn:test",
				},
			},
			Path: "/webhook",
			Body: `{"overflow":"test"}`,
		}

		rawEvent, _ := json.Marshal(albEvent)
		h.Handle(ctx, rawEvent)
	}

	// Wait for processing
	time.Sleep(2 * time.Second)

	// Some requests should be processed, but not all (queue overflow)
	count := received.Load()
	if count == 0 {
		t.Error("Expected some requests to be processed")
	}

	// Should process at most QueueSize jobs (some may be dropped)
	t.Logf("Processed %d/%d requests (queue overflow test)", count, numRequests)
}

func TestHandler_HandleSNSEvent(t *testing.T) {
	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{"https://example.com"},
	}

	// Create handler with mock SNS forwarder
	mockForwarder := &mockSNSForwarder{
		forwardFunc: func(ctx context.Context, topicArn string, payload json.RawMessage) error {
			if topicArn != cfg.SNSTopicArn {
				t.Errorf("Expected topic ARN %s, got %s", cfg.SNSTopicArn, topicArn)
			}
			return nil
		},
	}

	h := NewHandler(cfg, mockForwarder)

	// Create SNS event
	snsEvent := events.SNSEvent{
		Records: []events.SNSEventRecord{
			{
				SNS: events.SNSEntity{
					MessageID: "test-message-id",
					Message:   `{"test":"data"}`,
				},
			},
		},
	}

	rawEvent, _ := json.Marshal(snsEvent)
	ctx := context.Background()

	// Handle event
	response := h.Handle(ctx, rawEvent)

	// SNS event should return nil on success
	if response != nil {
		t.Errorf("Expected nil response for successful SNS event, got %v", response)
	}
}

func TestHandler_HandleSNSEventError(t *testing.T) {
	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{"https://example.com"},
	}

	// Create handler with failing SNS forwarder
	expectedError := fmt.Errorf("SNS publish failed")
	mockForwarder := &mockSNSForwarder{
		forwardFunc: func(ctx context.Context, topicArn string, payload json.RawMessage) error {
			return expectedError
		},
	}

	h := NewHandler(cfg, mockForwarder)

	// Create SNS event
	snsEvent := events.SNSEvent{
		Records: []events.SNSEventRecord{
			{
				SNS: events.SNSEntity{
					MessageID: "test-message-id",
					Message:   `{"test":"data"}`,
				},
			},
		},
	}

	rawEvent, _ := json.Marshal(snsEvent)
	ctx := context.Background()

	// Handle event
	response := h.Handle(ctx, rawEvent)

	// SNS event should return the error
	if response != expectedError {
		t.Errorf("Expected error response for failing SNS event, got %v", response)
	}
}

func TestHandler_Shutdown(t *testing.T) {
	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{"https://example.com"},
	}

	h := NewHandler(cfg, nil)

	// Add some jobs to the queue
	for i := 0; i < 5; i++ {
		h.workQueue <- &WebhookJob{
			ctx:     context.Background(),
			urls:    []string{"https://example.com"},
			payload: json.RawMessage(`{"test":"data"}`),
		}
	}

	// Shutdown with timeout
	err := h.Shutdown(2 * time.Second)
	if err != nil {
		t.Errorf("Expected no error from Shutdown, got %v", err)
	}

	// Queue should be drained
	queueSize := len(h.workQueue)
	t.Logf("Queue size after shutdown: %d", queueSize)
}

func TestHandler_ShutdownTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second) // Very slow webhook
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{ts.URL},
	}

	h := NewHandler(cfg, nil)

	// Fill the queue with slow jobs
	for i := 0; i < 50; i++ {
		h.workQueue <- &WebhookJob{
			ctx:     context.Background(),
			urls:    []string{ts.URL},
			payload: json.RawMessage(`{"test":"data"}`),
		}
	}

	// Shutdown with short timeout
	err := h.Shutdown(500 * time.Millisecond)
	if err != nil {
		t.Errorf("Expected no error from Shutdown, got %v", err)
	}

	// Some jobs may remain due to timeout
	remaining := len(h.workQueue)
	t.Logf("Jobs remaining after timeout: %d", remaining)
}

func TestHandler_WorkerWithDebug(t *testing.T) {
	var received atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{ts.URL},
		Debug:            true, // Enable debug mode
	}

	h := NewHandler(cfg, nil)

	// Send a job
	albEvent := events.ALBTargetGroupRequest{
		RequestContext: events.ALBTargetGroupRequestContext{
			ELB: events.ELBContext{
				TargetGroupArn: "arn:test",
			},
		},
		Path: "/webhook",
		Body: `{"debug":"test"}`,
	}

	rawEvent, _ := json.Marshal(albEvent)
	h.Handle(context.Background(), rawEvent)

	// Wait for processing
	time.Sleep(500 * time.Millisecond)

	// Verify webhook was called
	if count := received.Load(); count != 1 {
		t.Errorf("Expected 1 webhook call, got %d", count)
	}
}

// Mock SNS Forwarder

type mockSNSForwarder struct {
	forwardFunc func(ctx context.Context, topicArn string, payload json.RawMessage) error
}

func (m *mockSNSForwarder) Forward(ctx context.Context, topicArn string, payload json.RawMessage) error {
	if m.forwardFunc != nil {
		return m.forwardFunc(ctx, topicArn, payload)
	}
	return nil
}

func TestHandler_NoWebhookURLs(t *testing.T) {
	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{}, // No webhooks configured
	}

	h := NewHandler(cfg, nil)

	albEvent := events.ALBTargetGroupRequest{
		RequestContext: events.ALBTargetGroupRequestContext{
			ELB: events.ELBContext{
				TargetGroupArn: "arn:test",
			},
		},
		Path: "/webhook",
		Body: `{"test":"data"}`,
	}

	rawEvent, _ := json.Marshal(albEvent)
	response := h.Handle(context.Background(), rawEvent)

	albResp, ok := response.(events.ALBTargetGroupResponse)
	if !ok {
		t.Fatal("Expected ALBTargetGroupResponse")
	}

	if albResp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", albResp.StatusCode)
	}

	if !strings.Contains(albResp.Body, "no_webhooks_configured") {
		t.Errorf("Expected no_webhooks_configured in body, got %s", albResp.Body)
	}
}

func TestHandler_HandleDebugMode(t *testing.T) {
	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{"https://example.com"},
		Debug:            true,
	}

	h := NewHandler(cfg, nil)

	// Test with unknown event (triggers debug log)
	rawEvent := json.RawMessage(`{"unknown":"event"}`)
	response := h.Handle(context.Background(), rawEvent)

	albResp, ok := response.(events.ALBTargetGroupResponse)
	if !ok {
		t.Fatal("Expected ALBTargetGroupResponse for unknown event")
	}

	if albResp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", albResp.StatusCode)
	}
}

func TestHandler_QueueDebugLogs(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{ts.URL},
		Debug:            true,
	}

	h := NewHandler(cfg, nil)

	albEvent := events.ALBTargetGroupRequest{
		RequestContext: events.ALBTargetGroupRequestContext{
			ELB: events.ELBContext{
				TargetGroupArn: "arn:test",
			},
		},
		Path: "/webhook",
		Body: `{"debug":"log"}`,
	}

	rawEvent, _ := json.Marshal(albEvent)
	h.Handle(context.Background(), rawEvent)

	time.Sleep(200 * time.Millisecond)
}
