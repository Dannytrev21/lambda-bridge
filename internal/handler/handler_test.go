package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dannytrev21/lambda-bridge/internal/config"
	"github.com/aws/aws-lambda-go/events"
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
	var receivedBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type: application/json, got %s", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("User-Agent") != "Lambda-Bridge/1.0" {
			t.Errorf("Expected User-Agent: Lambda-Bridge/1.0, got %s", r.Header.Get("User-Agent"))
		}

		// Read the body
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)

		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Create handler with test config
	cfg := &config.Config{
		Environment: "test",
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs: []string{ts.URL},
		Debug:       false,
	}

	h := NewHandler(cfg, nil)

	// Load ALB event from testdata
	rawEvent := loadTestData(t, "alb_standard_event.json")
	ctx := context.Background()

    // Handle event
    response, err := h.Handle(ctx, rawEvent)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

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

	// Verify the webhook received the body from the ALB event, not the entire ALB event
	expectedBody := `{"event":"push","repository":"test-repo","ref":"refs/heads/main","commits":[{"id":"abc123","message":"Initial commit"}]}`
	if receivedBody != expectedBody {
		t.Errorf("Expected webhook to receive body payload:\n%s\n\nBut got:\n%s", expectedBody, receivedBody)
	}
}

func TestHandler_HandleALBEventBase64(t *testing.T) {
	// Setup test server to receive webhooks
	var received atomic.Int32
	var receivedBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the body
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)

		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Create handler with test config
	cfg := &config.Config{
		Environment: "test",
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs: []string{ts.URL},
		Debug:       false,
	}

	h := NewHandler(cfg, nil)

	// Create ALB event with base64 encoded body
	// The body is {"test":"data"} encoded as base64
	albEvent := events.ALBTargetGroupRequest{
		RequestContext: events.ALBTargetGroupRequestContext{
			ELB: events.ELBContext{
				TargetGroupArn: "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/test/abc",
			},
		},
		HTTPMethod:      "POST",
		Path:            "/webhook",
		Body:            "eyJ0ZXN0IjoiZGF0YSJ9",
		IsBase64Encoded: true,
	}

	rawEvent, _ := json.Marshal(albEvent)
	ctx := context.Background()

    // Handle event
    response, err := h.Handle(ctx, rawEvent)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

	// Verify response
	albResp, ok := response.(events.ALBTargetGroupResponse)
	if !ok {
		t.Fatal("Expected ALBTargetGroupResponse")
	}

	if albResp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", albResp.StatusCode)
	}

	// Wait for webhook to be received (async)
	time.Sleep(500 * time.Millisecond)

	if count := received.Load(); count != 1 {
		t.Errorf("Expected 1 webhook call, got %d", count)
	}

	// Verify the webhook received the decoded body
	expectedBody := `{"test":"data"}`
	if receivedBody != expectedBody {
		t.Errorf("Expected webhook to receive decoded body:\n%s\n\nBut got:\n%s", expectedBody, receivedBody)
	}
}

func TestHandler_ALBForwardingPreservesHeadersAndBody(t *testing.T) {
	capCh := make(chan struct {
		headers http.Header
		body    string
		method  string
		path    string
		query   map[string][]string
	}, 1)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)

		capCh <- struct {
			headers http.Header
			body    string
			method  string
			path    string
			query   map[string][]string
		}{
			headers: r.Header.Clone(),
			body:    string(body),
			method:  r.Method,
			path:    r.URL.Path,
			query:   r.URL.Query(),
		}
	}))
	defer ts.Close()

	cfg := &config.Config{
		Environment: "test",
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs: []string{ts.URL},
	}

	h := NewHandler(cfg, nil)

	albEvent := events.ALBTargetGroupRequest{
		RequestContext: events.ALBTargetGroupRequestContext{
			ELB: events.ELBContext{TargetGroupArn: "arn:test"},
		},
		HTTPMethod: "PUT",
		Path:       "/api/v1/hooks",
		Headers: map[string]string{
			"accept":            "application/vnd.github+json",
			"content-type":      "application/json",
			"x-custom-header":   "custom-value",
			"x-forwarded-for":   "198.51.100.2",
			"x-forwarded-proto": "https",
			"x-forwarded-port":  "443",
			"another-trace-id":  "trace-123",
		},
		QueryStringParameters: map[string]string{"foo": "bar", "baz": "qux"},
		Body:                  `{"event":"ping"}`,
	}

	rawEvent, err := json.Marshal(albEvent)
	if err != nil {
		t.Fatalf("failed to marshal ALB event: %v", err)
	}

    resp, err := h.Handle(context.Background(), rawEvent)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

	albResp, ok := resp.(events.ALBTargetGroupResponse)
	if !ok {
		t.Fatal("expected ALBTargetGroupResponse")
	}

	if albResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 response, got %d", albResp.StatusCode)
	}

	select {
	case captured := <-capCh:
		if captured.method != "PUT" {
			t.Errorf("expected method PUT, got %s", captured.method)
		}

		if captured.path != "/api/v1/hooks" {
			t.Errorf("expected path /api/v1/hooks, got %s", captured.path)
		}

		if captured.body != `{"event":"ping"}` {
			t.Errorf("expected body {\"event\":\"ping\"}, got %s", captured.body)
		}

		if values := captured.query["foo"]; len(values) == 0 || values[0] != "bar" {
			t.Errorf("expected query foo=bar, got %v", values)
		}

		if values := captured.query["baz"]; len(values) == 0 || values[0] != "qux" {
			t.Errorf("expected query baz=qux, got %v", values)
		}

		headers := captured.headers
		if headers.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("expected Accept header to match, got %s", headers.Get("Accept"))
		}

		if headers.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", headers.Get("Content-Type"))
		}

		if headers.Get("X-Custom-Header") != "custom-value" {
			t.Errorf("expected X-Custom-Header custom-value, got %s", headers.Get("X-Custom-Header"))
		}

		if headers.Get("Another-Trace-Id") != "trace-123" {
			t.Errorf("expected Another-Trace-Id trace-123, got %s", headers.Get("Another-Trace-Id"))
		}

		if headers.Get("X-Request-ID") == "" {
			t.Error("expected X-Request-ID header to be set")
		}

		if headers.Get("User-Agent") != "Lambda-Bridge/1.0" {
			t.Errorf("expected User-Agent Lambda-Bridge/1.0, got %s", headers.Get("User-Agent"))
		}

		if headers.Get("X-Forwarded-For") != "" {
			t.Errorf("expected X-Forwarded-For header to be stripped, got %s", headers.Get("X-Forwarded-For"))
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for forwarded webhook request")
	}
}

func TestHandler_HandleHealthCheck(t *testing.T) {
	cfg := &config.Config{
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs: []string{"https://example.com"},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawEvent := loadTestData(t, tt.testFile)
                response, err := h.Handle(context.Background(), rawEvent)
                if err != nil {
                    t.Fatalf("unexpected error: %v", err)
                }

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
    response, err := h.Handle(context.Background(), rawEvent)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

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
    _, _ = h.Handle(context.Background(), rawEvent)

			// Wait for async forwarding
			time.Sleep(300 * time.Millisecond)

			// Check expectations based on test file
			if i == 0 {
				// Cloud routing test
				if cloudReceived.Load() != 1 {
					t.Errorf("Expected cloud webhook to be called once, got %d", cloudReceived.Load())
				}
				if enterpriseReceived.Load() != 0 {
					t.Errorf("Expected enterprise webhook NOT to be called, got %d", enterpriseReceived.Load())
				}
				if defaultReceived.Load() != 0 {
					t.Errorf("Expected default webhook NOT to be called, got %d", defaultReceived.Load())
				}
			}
			if i == 1 {
				// Enterprise routing test
				if cloudReceived.Load() != 0 {
					t.Errorf("Expected cloud webhook NOT to be called, got %d", cloudReceived.Load())
				}
				if enterpriseReceived.Load() != 1 {
					t.Errorf("Expected enterprise webhook to be called once, got %d", enterpriseReceived.Load())
				}
				if defaultReceived.Load() != 0 {
					t.Errorf("Expected default webhook NOT to be called, got %d", defaultReceived.Load())
				}
			}
			if i == 2 {
				// Default routing test
				if cloudReceived.Load() != 0 {
					t.Errorf("Expected cloud webhook NOT to be called, got %d", cloudReceived.Load())
				}
				if enterpriseReceived.Load() != 0 {
					t.Errorf("Expected enterprise webhook NOT to be called, got %d", enterpriseReceived.Load())
				}
				if defaultReceived.Load() != 1 {
					t.Errorf("Expected default webhook to be called once, got %d", defaultReceived.Load())
				}
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
		Environment: "test",
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs: []string{ts.URL},
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
        _, _ = h.Handle(ctx, rawEvent)
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
		Environment: "test",
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs: []string{ts.URL},
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
        _, _ = h.Handle(ctx, rawEvent)
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
		Environment: "test",
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs: []string{"https://example.com"},
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
    response, err := h.Handle(ctx, rawEvent)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

    // SNS event should return nil response on success
    if response != nil {
        t.Errorf("Expected nil response for successful SNS event, got %v", response)
    }
}

func TestHandler_HandleSNSEventError(t *testing.T) {
	cfg := &config.Config{
		Environment: "test",
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs: []string{"https://example.com"},
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
    response, err := h.Handle(ctx, rawEvent)

    // SNS event should return the error
    if err != expectedError {
        t.Errorf("Expected error %v, got %v", expectedError, err)
    }
    if response != nil {
        t.Errorf("Expected nil response when SNS event fails, got %v", response)
    }
}

func TestHandler_Shutdown(t *testing.T) {
	cfg := &config.Config{
		Environment: "test",
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs: []string{"https://example.com"},
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
		Environment: "test",
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs: []string{ts.URL},
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
		Environment: "test",
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs: []string{ts.URL},
		Debug:       true, // Enable debug mode
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
    _, _ = h.Handle(context.Background(), rawEvent)

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
		Environment: "test",
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs: []string{}, // No webhooks configured
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
    response, err := h.Handle(context.Background(), rawEvent)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

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
		Environment: "test",
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs: []string{"https://example.com"},
		Debug:       true,
	}

	h := NewHandler(cfg, nil)

	// Test with unknown event (triggers debug log)
	rawEvent := json.RawMessage(`{"unknown":"event"}`)
    response, err := h.Handle(context.Background(), rawEvent)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

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
		Environment: "test",
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs: []string{ts.URL},
		Debug:       true,
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
    _, _ = h.Handle(context.Background(), rawEvent)

	time.Sleep(200 * time.Millisecond)
}

func TestHandler_AsyncForwardContextCancellation(t *testing.T) {
	cfg := &config.Config{
		Environment: "test",
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs: []string{"https://example.com"},
		Debug:       true,
	}

	h := NewHandler(cfg, nil)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Try to enqueue with cancelled context
	h.asyncForward(ctx, []string{"https://example.com"}, json.RawMessage(`{"test":"data"}`), "POST", "/webhook", nil, nil)

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
		Environment: "test",
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs: []string{ts.URL},
		Debug:       true,
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
	h.asyncForward(context.Background(), []string{ts.URL}, json.RawMessage(`{"test":"overflow"}`), "POST", "/webhook", nil, nil)

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
    response, err := h.Handle(context.Background(), rawEvent)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

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
    response, err := h.Handle(context.Background(), rawEvent)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

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
		Environment: "test",
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs: []string{"https://example.com"},
		Debug:       false,
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
    response, err := h.Handle(context.Background(), rawEvent)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

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
		Environment: "test",
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs: []string{"https://example.com"},
		Debug:       false,
	}

	h := NewHandler(cfg, nil)

	// Test with nil event
	result := h.isHealthCheck(nil)
	if result {
		t.Error("Expected isHealthCheck(nil) to return false")
	}
}

func TestHandler_EnterpriseWebhookRouting(t *testing.T) {
	var enterpriseReceived, defaultReceived atomic.Int32

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
		EnterpriseWebhookURLs: []string{enterpriseServer.URL},
		WebhookURLs:           []string{defaultServer.URL},
		Debug:                 false,
	}

	h := NewHandler(cfg, nil)

	// Load enterprise event from testdata
	rawEvent := loadTestData(t, "alb_forward_enterprise_event.json")
    response, err := h.Handle(context.Background(), rawEvent)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

	albResp := response.(events.ALBTargetGroupResponse)
	if albResp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", albResp.StatusCode)
	}

	// Wait for webhook processing
	time.Sleep(300 * time.Millisecond)

	// Verify enterprise webhook was called
	if count := enterpriseReceived.Load(); count != 1 {
		t.Errorf("Expected 1 enterprise webhook call, got %d", count)
	}

	// Verify default webhook was NOT called
	if count := defaultReceived.Load(); count != 0 {
		t.Errorf("Expected default webhook NOT to be called, got %d calls", count)
	}

	h.Shutdown(1 * time.Second)
}

func TestHandler_ForwardMetadata(t *testing.T) {
	// Capture forwarded request details
	var mu sync.Mutex
	var receivedMethod, receivedPath, receivedQuery string
	var receivedHeaders map[string]string
	var receivedBody string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		receivedMethod = r.Method
		receivedPath = r.URL.Path
		receivedQuery = r.URL.RawQuery

		// Capture headers
		receivedHeaders = make(map[string]string)
		for k, v := range r.Header {
			if len(v) > 0 {
				receivedHeaders[k] = v[0]
			}
		}

		// Capture body
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)

		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Create handler
	cfg := &config.Config{
		Environment: "test",
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs: []string{ts.URL},
		Debug:       false,
	}

	h := NewHandler(cfg, nil)

	// Create ALB event with metadata
	albEvent := events.ALBTargetGroupRequest{
		RequestContext: events.ALBTargetGroupRequestContext{
			ELB: events.ELBContext{
				TargetGroupArn: "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/test/abc",
			},
		},
		HTTPMethod: "PUT",
		Path:       "/api/webhooks/test",
		QueryStringParameters: map[string]string{
			"source": "github",
			"event":  "push",
		},
		Headers: map[string]string{
			"X-GitHub-Event":    "push",
			"X-GitHub-Delivery": "12345",
			"Content-Type":      "application/json",
			"X-Custom-Header":   "test-value",
		},
		Body:            `{"action":"opened","number":123}`,
		IsBase64Encoded: false,
	}

	rawEvent, _ := json.Marshal(albEvent)
	ctx := context.Background()

	// Handle event
    response, err := h.Handle(ctx, rawEvent)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

	// Verify response
	albResp, ok := response.(events.ALBTargetGroupResponse)
	if !ok {
		t.Fatal("Expected ALBTargetGroupResponse")
	}

	if albResp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", albResp.StatusCode)
	}

	// Wait for webhook to be received (async)
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Debug: print all received headers
	t.Logf("Received headers: %+v", receivedHeaders)

	// Verify HTTP method
	if receivedMethod != "PUT" {
		t.Errorf("Expected method PUT, got %s", receivedMethod)
	}

	// Verify path
	if receivedPath != "/api/webhooks/test" {
		t.Errorf("Expected path /api/webhooks/test, got %s", receivedPath)
	}

	// Verify query parameters
	if !strings.Contains(receivedQuery, "source=github") {
		t.Errorf("Expected query to contain source=github, got %s", receivedQuery)
	}
	if !strings.Contains(receivedQuery, "event=push") {
		t.Errorf("Expected query to contain event=push, got %s", receivedQuery)
	}

	// Verify headers were forwarded (except filtered ones)
	// Note: HTTP canonicalizes header names (X-GitHub-Event becomes X-Github-Event)
	if receivedHeaders["X-Github-Event"] != "push" {
		t.Errorf("Expected X-Github-Event header to be push, got %s", receivedHeaders["X-Github-Event"])
	}
	if receivedHeaders["X-Github-Delivery"] != "12345" {
		t.Errorf("Expected X-Github-Delivery header to be 12345, got %s", receivedHeaders["X-Github-Delivery"])
	}
	if receivedHeaders["X-Custom-Header"] != "test-value" {
		t.Errorf("Expected X-Custom-Header to be test-value, got %s", receivedHeaders["X-Custom-Header"])
	}

	// Verify Content-Type was forwarded
	if receivedHeaders["Content-Type"] != "application/json" {
		t.Errorf("Expected Content-Type to be application/json, got %s", receivedHeaders["Content-Type"])
	}

	// Verify User-Agent is Lambda Bridge
	if receivedHeaders["User-Agent"] != "Lambda-Bridge/1.0" {
		t.Errorf("Expected User-Agent to be Lambda-Bridge/1.0, got %s", receivedHeaders["User-Agent"])
	}

	// Verify body
	expectedBody := `{"action":"opened","number":123}`
	if receivedBody != expectedBody {
		t.Errorf("Expected body %s, got %s", expectedBody, receivedBody)
	}

	h.Shutdown(1 * time.Second)
}

func TestHandler_ContextIsolation(t *testing.T) {
	// Track webhook calls
	var received atomic.Int32
	var errors atomic.Int32

	// Channel to signal when request is received
	requestStarted := make(chan struct{}, 1)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Signal that request started
		select {
		case requestStarted <- struct{}{}:
		default:
		}

		// Check if context is cancelled
		select {
		case <-r.Context().Done():
			errors.Add(1)
			t.Log("ERROR: Webhook request context was cancelled")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		default:
			// Context not cancelled, proceed normally
		}

		// Simulate some processing time
		time.Sleep(100 * time.Millisecond)

		// Check again after processing
		select {
		case <-r.Context().Done():
			errors.Add(1)
			t.Log("ERROR: Webhook request context was cancelled during processing")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		default:
			received.Add(1)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer ts.Close()

	cfg := &config.Config{
		Environment: "test",
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs: []string{ts.URL},
		Debug:       true,
	}

	h := NewHandler(cfg, nil)
	defer h.Shutdown(2 * time.Second)

	// Create a context that we'll cancel immediately after Handle returns
	// This simulates Lambda context cancellation
	ctx, cancel := context.WithCancel(context.Background())

	albEvent := events.ALBTargetGroupRequest{
		RequestContext: events.ALBTargetGroupRequestContext{
			ELB: events.ELBContext{
				TargetGroupArn: "arn:test",
			},
		},
		Path: "/webhook",
		Body: `{"test":"context_isolation"}`,
	}

	rawEvent, _ := json.Marshal(albEvent)

	// Handle the event
	response, err := h.Handle(ctx, rawEvent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify we got immediate 200 response
	albResp, ok := response.(events.ALBTargetGroupResponse)
	if !ok {
		t.Fatal("Expected ALBTargetGroupResponse")
	}
	if albResp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", albResp.StatusCode)
	}

	// Cancel the context immediately after Handle returns
	// This simulates Lambda cancelling the context after the function returns
	cancel()

	// Wait for the webhook request to start
	select {
	case <-requestStarted:
		// Request started, good
	case <-time.After(1 * time.Second):
		t.Fatal("Webhook request did not start within 1 second")
	}

	// Wait for webhook processing to complete
	time.Sleep(500 * time.Millisecond)

	// Check results
	if errorCount := errors.Load(); errorCount > 0 {
		t.Errorf("Context cancellation leaked to webhook: %d errors", errorCount)
	}

	if receivedCount := received.Load(); receivedCount != 1 {
		t.Errorf("Expected 1 successful webhook call, got %d", receivedCount)
	}
}

func TestHandler_SimulateLambdaContextCancellation(t *testing.T) {
	// Track successful and failed webhook calls
	var successCount atomic.Int32
	var cancelCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate network delay
		time.Sleep(200 * time.Millisecond)

		// Check if the request context was cancelled
		select {
		case <-r.Context().Done():
			cancelCount.Add(1)
			t.Logf("Request cancelled: %v", r.Context().Err())
			// Don't write response if context is cancelled
			return
		default:
			successCount.Add(1)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer ts.Close()

	cfg := &config.Config{
		Environment: "test",
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs: []string{ts.URL},
		Debug:       true,
	}

	h := NewHandler(cfg, nil)
	defer h.Shutdown(2 * time.Second)

	// Simulate multiple rapid Lambda invocations
	for i := 0; i < 3; i++ {
		// Create a context that simulates Lambda's request context
		lambdaCtx, cancel := context.WithCancel(context.Background())

		albEvent := events.ALBTargetGroupRequest{
			RequestContext: events.ALBTargetGroupRequestContext{
				ELB: events.ELBContext{
					TargetGroupArn: "arn:test",
				},
			},
			Path: "/webhook",
			Body: fmt.Sprintf(`{"request":%d}`, i),
		}

		rawEvent, _ := json.Marshal(albEvent)

		// Handle the event (simulating Lambda invocation)
		response, err := h.Handle(lambdaCtx, rawEvent)
		if err != nil {
			t.Fatalf("Request %d: unexpected error: %v", i, err)
		}

		// Verify immediate response
		if albResp, ok := response.(events.ALBTargetGroupResponse); !ok || albResp.StatusCode != 200 {
			t.Errorf("Request %d: Expected 200 response", i)
		}

		// Cancel context immediately (simulating Lambda finishing)
		cancel()

		// Small delay between invocations
		time.Sleep(50 * time.Millisecond)
	}

	// Wait for all webhooks to complete
	time.Sleep(1 * time.Second)

	// Check results
	success := successCount.Load()
	cancelled := cancelCount.Load()

	t.Logf("Successful webhook calls: %d", success)
	t.Logf("Cancelled webhook calls: %d", cancelled)

	// With the bug, we expect cancelled calls
	// After the fix, all calls should succeed
	if cancelled > 0 {
		t.Errorf("Context cancellation affected webhook forwarding: %d calls cancelled", cancelled)
	}

	if success != 3 {
		t.Errorf("Expected 3 successful webhook calls, got %d", success)
	}
}
