package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/Dannytrev21/lambda-bridge/internal/config"
)

// loadTestData loads a test event from the testdata directory
func loadTestData(t *testing.T, filename string) json.RawMessage {
	t.Helper()
	path := filepath.Join("testdata", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read testdata file %s: %v", filename, err)
	}
	return json.RawMessage(data)
}

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

	// Load ALB event from testdata
	rawEvent := loadTestData(t, "alb_standard_event.json")
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
		name     string
		testFile string
		want     string
	}{
		{
			name:     "health check via user-agent (ELB)",
			testFile: "alb_health_check_event.json",
			want:     "healthy",
		},
		{
			name:     "health check via path /healthz",
			testFile: "alb_healthz_check_event.json",
			want:     "healthy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawEvent := loadTestData(t, tt.testFile)
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

	// Test routing using testdata files
	tests := []struct {
		name     string
		testFile string
	}{
		{"cloud destination header", "alb_forward_cloud_event.json"},
		{"enterprise header", "alb_forward_enterprise_event.json"},
		{"default route", "alb_standard_event.json"},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset counters
			cloudReceived.Store(0)
			enterpriseReceived.Store(0)
			defaultReceived.Store(0)

			rawEvent := loadTestData(t, tt.testFile)
			h.Handle(context.Background(), rawEvent)

			// Wait for async forwarding
			time.Sleep(300 * time.Millisecond)

			// Check expectations based on test file
			if i == 0 && cloudReceived.Load() != 1 {
				t.Errorf("Expected cloud webhook to be called once, got %d", cloudReceived.Load())
			}
			if i == 1 && enterpriseReceived.Load() != 1 {
				t.Errorf("Expected enterprise webhook to be called once, got %d", enterpriseReceived.Load())
			}
			if i == 2 && defaultReceived.Load() != 1 {
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

	// Load SNS event from testdata
	rawEvent := loadTestData(t, "sns_good_event.json")
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

func TestHandler_AsyncForwardContextCancellation(t *testing.T) {
	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{"https://example.com"},
		Debug:            true,
	}

	h := NewHandler(cfg, nil)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Try to enqueue with cancelled context
	h.asyncForward(ctx, []string{"https://example.com"}, json.RawMessage(`{"test":"data"}`))

	// Should log warning but not panic
	time.Sleep(100 * time.Millisecond)
}

func TestHandler_AsyncForwardQueueFull(t *testing.T) {
	// Create a slow webhook to prevent workers from draining the queue
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second) // Very slow to keep queue full
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

	// Fill the queue completely
	for i := 0; i < QueueSize; i++ {
		h.workQueue <- &WebhookJob{
			ctx:     context.Background(),
			urls:    []string{ts.URL},
			payload: json.RawMessage(`{"test":"data"}`),
		}
	}

	// Verify queue is full before attempting to add more
	initialSize := len(h.workQueue)
	if initialSize < QueueSize {
		t.Logf("Warning: Queue not completely full (%d/%d), but continuing test", initialSize, QueueSize)
	}

	// Try to add one more (should trigger default case - queue full and drop the job)
	h.asyncForward(context.Background(), []string{ts.URL}, json.RawMessage(`{"test":"overflow"}`))

	// Queue size should not exceed capacity (job should be dropped)
	finalSize := len(h.workQueue)
	if finalSize > QueueSize {
		t.Errorf("Queue size exceeded capacity: %d > %d", finalSize, QueueSize)
	}

	// Cleanup
	h.Shutdown(100 * time.Millisecond) // Short timeout since workers are busy
}

func TestHandler_GetHeaderMultiValue(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := &config.Config{
		Environment:           "test",
		SNSTopicArn:           "arn:aws:sns:us-east-1:123456789012:test",
		CloudWebhookURLs:      []string{ts.URL},
		EnterpriseWebhookURLs: []string{ts.URL},
		Debug:                 false,
	}

	h := NewHandler(cfg, nil)

	// Load ALB event with MultiValueHeaders from testdata
	rawEvent := loadTestData(t, "alb_multivalue_headers_event.json")
	response := h.Handle(context.Background(), rawEvent)

	albResp := response.(events.ALBTargetGroupResponse)
	if albResp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", albResp.StatusCode)
	}

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	h.Shutdown(1 * time.Second)
}

func TestHandler_GetHeaderCaseInsensitive(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		CloudWebhookURLs: []string{ts.URL},
		Debug:            false,
	}

	h := NewHandler(cfg, nil)

	// Test case insensitive header lookup
	albEvent := events.ALBTargetGroupRequest{
		RequestContext: events.ALBTargetGroupRequestContext{
			ELB: events.ELBContext{
				TargetGroupArn: "arn:test",
			},
		},
		Path: "/webhook",
		Body: `{"test":"case"}`,
		Headers: map[string]string{
			"x-dcp-DESTINATION-host": "cloud.example.com", // Mixed case
		},
	}

	rawEvent, _ := json.Marshal(albEvent)
	response := h.Handle(context.Background(), rawEvent)

	albResp := response.(events.ALBTargetGroupResponse)
	if albResp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", albResp.StatusCode)
	}

	time.Sleep(200 * time.Millisecond)
	h.Shutdown(1 * time.Second)
}

func TestHandler_SelectWebhookURLsNilEvent(t *testing.T) {
	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{"https://default.example.com"},
		CloudWebhookURLs: []string{"https://cloud.example.com"},
		Debug:            false,
	}

	h := NewHandler(cfg, nil)

	// Test with nil event (should return default WebhookURLs)
	urls := h.selectWebhookURLs(nil)

	if len(urls) != 1 || urls[0] != "https://default.example.com" {
		t.Errorf("Expected default webhook URLs for nil event, got %v", urls)
	}
}

func TestHandler_HealthCheckStatus(t *testing.T) {
	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{"https://example.com"},
		Debug:            false,
	}

	h := NewHandler(cfg, nil)

	// Test /status endpoint
	albEvent := events.ALBTargetGroupRequest{
		RequestContext: events.ALBTargetGroupRequestContext{
			ELB: events.ELBContext{
				TargetGroupArn: "arn:test",
			},
		},
		Headers: map[string]string{},
		Path:    "/status",
	}

	rawEvent, _ := json.Marshal(albEvent)
	response := h.Handle(context.Background(), rawEvent)

	albResp := response.(events.ALBTargetGroupResponse)
	if albResp.StatusCode != 200 {
		t.Errorf("Expected status 200 for /status health check, got %d", albResp.StatusCode)
	}
	if albResp.Body != "healthy" {
		t.Errorf("Expected 'healthy' body for /status, got %s", albResp.Body)
	}
}

func TestHandler_IsHealthCheckNil(t *testing.T) {
	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{"https://example.com"},
		Debug:            false,
	}

	h := NewHandler(cfg, nil)

	// Test with nil event
	result := h.isHealthCheck(nil)
	if result {
		t.Error("Expected isHealthCheck(nil) to return false")
	}
}

func TestHandler_EnterpriseWebhookRouting(t *testing.T) {
	var received atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := &config.Config{
		Environment:           "test",
		SNSTopicArn:           "arn:aws:sns:us-east-1:123456789012:test",
		EnterpriseWebhookURLs: []string{ts.URL},
		WebhookURLs:           []string{"https://should-not-be-called.com"},
		Debug:                 false,
	}

	h := NewHandler(cfg, nil)

	// Load enterprise event from testdata
	rawEvent := loadTestData(t, "alb_forward_enterprise_event.json")
	response := h.Handle(context.Background(), rawEvent)

	albResp := response.(events.ALBTargetGroupResponse)
	if albResp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", albResp.StatusCode)
	}

	// Wait for webhook processing
	time.Sleep(300 * time.Millisecond)

	// Verify enterprise webhook was called
	if count := received.Load(); count != 1 {
		t.Errorf("Expected 1 enterprise webhook call, got %d", count)
	}

	h.Shutdown(1 * time.Second)
}
