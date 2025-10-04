package forwarder

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebhookForwarder_ForwardToWebhooks_Success(t *testing.T) {
	// Create test servers
	var mu sync.Mutex
	receivedBodies := make(map[string]string)

	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		mu.Lock()
		receivedBodies["server1"] = string(body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		mu.Lock()
		receivedBodies["server2"] = string(body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server2.Close()

	forwarder := NewWebhookForwarder(Config{})
	rawEvent := json.RawMessage(`{"test": "webhook event", "data": "value"}`)
	webhookURLs := []string{server1.URL, server2.URL}

	results := forwarder.ForwardToWebhooks(context.Background(), webhookURLs, rawEvent)

	// Verify results
	assert.Len(t, results, 2)

	for _, url := range webhookURLs {
		result, exists := results[url]
		assert.True(t, exists, "Result should exist for URL: %s", url)
		assert.NoError(t, result.Error, "Should not have error for URL: %s", url)
		assert.Equal(t, 200, result.StatusCode, "Should have 200 status for URL: %s", url)
		assert.Greater(t, result.Duration, time.Duration(0), "Should have positive duration")
	}

	// Verify both servers received the event
	mu.Lock()
	assert.Equal(t, string(rawEvent), receivedBodies["server1"])
	assert.Equal(t, string(rawEvent), receivedBodies["server2"])
	mu.Unlock()
}

func TestWebhookForwarder_ForwardToWebhooks_ParallelExecution(t *testing.T) {
	// Create servers with delays to test parallel execution
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server2.Close()

	forwarder := NewWebhookForwarder(Config{})
	rawEvent := json.RawMessage(`{"test": "event"}`)
	webhookURLs := []string{server1.URL, server2.URL}

	start := time.Now()
	results := forwarder.ForwardToWebhooks(context.Background(), webhookURLs, rawEvent)
	duration := time.Since(start)

	// Should complete in ~100ms (parallel) not ~200ms (sequential)
	assert.Less(t, duration, 200*time.Millisecond, "Should execute in parallel")
	assert.Len(t, results, 2)

	for _, result := range results {
		assert.NoError(t, result.Error)
		assert.Equal(t, 200, result.StatusCode)
	}
}

func TestWebhookForwarder_ForwardToWebhooks_ErrorHandling(t *testing.T) {
	// Create servers that return errors
	server500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server500.Close()

	server404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server404.Close()

	forwarder := NewWebhookForwarder(Config{})
	rawEvent := json.RawMessage(`{"test": "event"}`)
	webhookURLs := []string{server500.URL, server404.URL}

	results := forwarder.ForwardToWebhooks(context.Background(), webhookURLs, rawEvent)

	assert.Len(t, results, 2)

	result500 := results[server500.URL]
	assert.Error(t, result500.Error)
	assert.Equal(t, 500, result500.StatusCode)
	assert.Contains(t, result500.Error.Error(), "HTTP 500")

	result404 := results[server404.URL]
	assert.Error(t, result404.Error)
	assert.Equal(t, 404, result404.StatusCode)
	assert.Contains(t, result404.Error.Error(), "HTTP 404")
}

func TestWebhookForwarder_RetryLogic(t *testing.T) {
	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		if attemptCount < 3 {
			w.WriteHeader(http.StatusInternalServerError) // Trigger retry
		} else {
			w.WriteHeader(http.StatusOK) // Succeed on 3rd attempt
		}
	}))
	defer server.Close()

	forwarder := NewWebhookForwarder(Config{})
	rawEvent := json.RawMessage(`{"test": "retry event"}`)

	payload := newWebhookPayload(rawEvent)
	req := newPreparedRequest(context.Background(), server.URL, payload)
	result := forwarder.forwardToWebhookWithRetry(context.Background(), req)

	assert.NoError(t, result.Error, "Should succeed after retries")
	assert.Equal(t, 200, result.StatusCode, "Should have 200 status after retries")
	assert.Equal(t, 3, attemptCount, "Should have made 3 attempts")
}

func TestWebhookForwarder_MaxRetries(t *testing.T) {
	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		w.WriteHeader(http.StatusInternalServerError) // Always fail
	}))
	defer server.Close()

	forwarder := NewWebhookForwarder(Config{})
	rawEvent := json.RawMessage(`{"test": "max retry event"}`)

	payload := newWebhookPayload(rawEvent)
	req := newPreparedRequest(context.Background(), server.URL, payload)
	result := forwarder.forwardToWebhookWithRetry(context.Background(), req)

	assert.Error(t, result.Error, "Should fail after max retries")
	assert.Equal(t, 500, result.StatusCode, "Should have 500 status")
	assert.Equal(t, 4, attemptCount, "Should have made 4 attempts (1 initial + 3 retries)")
}

func TestWebhookForwarder_NoRetryOn4xx(t *testing.T) {
	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		w.WriteHeader(http.StatusBadRequest) // 400 - should not retry
	}))
	defer server.Close()

	forwarder := NewWebhookForwarder(Config{})
	rawEvent := json.RawMessage(`{"test": "no retry event"}`)

	payload := newWebhookPayload(rawEvent)
	req := newPreparedRequest(context.Background(), server.URL, payload)
	result := forwarder.forwardToWebhookWithRetry(context.Background(), req)

	assert.Error(t, result.Error, "Should fail")
	assert.Equal(t, 400, result.StatusCode, "Should have 400 status")
	assert.Equal(t, 1, attemptCount, "Should have made only 1 attempt (no retry for 4xx)")
}

func TestWebhookForwarder_RequestHeaders(t *testing.T) {
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	forwarder := NewWebhookForwarder(Config{})
	rawEvent := json.RawMessage(`{"test": "headers"}`)

	payload := newWebhookPayload(rawEvent)
	req := newPreparedRequest(context.Background(), server.URL, payload)
	result := forwarder.forwardToWebhook(context.Background(), req)

	assert.NoError(t, result.Error)
	assert.Equal(t, "application/json", receivedHeaders.Get("Content-Type"))
	assert.Equal(t, "lambda-bridge/1.0", receivedHeaders.Get("User-Agent"))
}

func TestWebhookForwarder_InvalidURL(t *testing.T) {
	forwarder := NewWebhookForwarder(Config{})
	rawEvent := json.RawMessage(`{"test": "event"}`)

	payload := newWebhookPayload(rawEvent)
	req := newPreparedRequest(context.Background(), "invalid-url", payload)
	result := forwarder.forwardToWebhook(context.Background(), req)

	assert.Error(t, result.Error)
	assert.Contains(t, strings.ToLower(result.Error.Error()), "request")
	assert.Greater(t, result.Duration, time.Duration(0))
}

func TestWebhookForwarder_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond) // Longer than context timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	forwarder := NewWebhookForwarder(Config{})
	rawEvent := json.RawMessage(`{"test": "event"}`)

	payload := newWebhookPayload(rawEvent)
	req := newPreparedRequest(context.Background(), server.URL, payload)
	result := forwarder.forwardToWebhook(ctx, req)

	assert.Error(t, result.Error)
	// The error should indicate timeout (either "context" or "timeout")
	errMsg := strings.ToLower(result.Error.Error())
	assert.True(t, strings.Contains(errMsg, "context") || strings.Contains(errMsg, "timeout"),
		"expected error to mention context or timeout, got: %s", result.Error.Error())
}

func TestWebhookForwarder_CircuitBreaker(t *testing.T) {
	failureServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failureServer.Close()

	fwd := NewWebhookForwarder(Config{
		MaxRetries:              0,
		CircuitBreakerThreshold: 2,
		CircuitBreakerWindow:    30 * time.Second,
		CircuitBreakerCooldown:  30 * time.Second,
	})

	ctx := context.Background()
	payload := json.RawMessage(`{"test":"cb"}`)

	for i := 0; i < 2; i++ {
		results := fwd.ForwardToWebhooks(ctx, []string{failureServer.URL}, payload)
		res := results[failureServer.URL]
		assert.NotNil(t, res.Error)
	}

	results := fwd.ForwardToWebhooks(ctx, []string{failureServer.URL}, payload)
	res := results[failureServer.URL]
	require.ErrorIs(t, res.Error, ErrCircuitOpen)
	assert.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
}

func TestWebhookForwarder_ForwardsGitHubEventWithALBHeaders(t *testing.T) {
	t.Parallel()

	var (
		receivedHeaders http.Header
		receivedBody    []byte
		receivedMethod  string
		receivedPath    string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		receivedHeaders = r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		receivedBody = body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	payload := `{"action":"opened","issue":{"number":1347},"repository":{"id":1296269,"full_name":"octocat/Hello-World","owner":{"login":"octocat","id":1}},"sender":{"login":"octocat","id":1}}`

	albEvent := events.ALBTargetGroupRequest{
		HTTPMethod: "POST",
		Path:       "/payload",
		Headers: map[string]string{
			"Content-Type":                           "application/json",
			"User-Agent":                             "GitHub-Hookshot/044aadd",
			"X-GitHub-Delivery":                      "72d3162e-cc78-11e3-81ab-4c9367dc0958",
			"X-Hub-Signature":                        "sha1=7d38cdd689735b008b3c702edd92eea23791c5f6",
			"X-Hub-Signature-256":                    "sha256=d57c68ca6f92289e6987922ff26938930f6e66a2d161ef06abdf1859230aa23c",
			"X-GitHub-Event":                         "pull_request",
			"X-GitHub-Hook-ID":                       "292430182",
			"X-GitHub-Hook-Installation-Target-ID":   "79929171",
			"X-GitHub-Hook-Installation-Target-Type": "repository",
			"X-Dcp-Destination-Host":                 "bots.internal",
		},
		Body: payload,
	}

	rawEventBytes, err := json.Marshal(albEvent)
	if err != nil {
		t.Fatalf("marshal alb event: %v", err)
	}

	forwarder := NewWebhookForwarder(Config{})
	webhookURL := server.URL + "/payload"
	results := forwarder.ForwardToWebhooks(context.Background(), []string{webhookURL}, json.RawMessage(rawEventBytes))

	result, ok := results[webhookURL]
	if !ok {
		t.Fatalf("missing result for webhook URL")
	}
	assert.NoError(t, result.Error)
	assert.Equal(t, http.StatusOK, result.StatusCode)

	assert.Equal(t, http.MethodPost, receivedMethod)
	assert.Equal(t, "/payload", receivedPath)
	assert.JSONEq(t, payload, string(receivedBody))

	expectedHeaders := map[string]string{
		"X-GitHub-Delivery":                      "72d3162e-cc78-11e3-81ab-4c9367dc0958",
		"X-Hub-Signature":                        "sha1=7d38cdd689735b008b3c702edd92eea23791c5f6",
		"X-Hub-Signature-256":                    "sha256=d57c68ca6f92289e6987922ff26938930f6e66a2d161ef06abdf1859230aa23c",
		"User-Agent":                             "GitHub-Hookshot/044aadd",
		"Content-Type":                           "application/json",
		"X-GitHub-Event":                         "pull_request",
		"X-GitHub-Hook-ID":                       "292430182",
		"X-GitHub-Hook-Installation-Target-ID":   "79929171",
		"X-GitHub-Hook-Installation-Target-Type": "repository",
		"X-Original-Path":                        "/payload",
		"X-Original-Base64-Encoded":              "false",
	}

	assert.Equal(t, "", receivedHeaders.Get("X-Original-Raw-Query"))
	assert.Equal(t, "", receivedHeaders.Get("X-Original-Target-Group-Arn"))

	for key, expected := range expectedHeaders {
		assert.Equalf(t, expected, receivedHeaders.Get(key), "header %s should match", key)
	}

	enterpriseHost := receivedHeaders.Get("X-Github-Enterprise-Host")
	dcpHost := receivedHeaders.Get("X-Dcp-Destination-Host")
	if enterpriseHost == "" && dcpHost == "" {
		t.Fatalf("expected either X-Github-Enterprise-Host or X-Dcp-Destination-Host header to be present")
	}

	allowedEvents := map[string]struct{}{
		"pull_request":        {},
		"installation":        {},
		"pull_request_review": {},
		"issue_comment":       {},
		"status":              {},
		"check_run":           {},
	}

	eventName := receivedHeaders.Get("X-GitHub-Event")
	if _, ok := allowedEvents[eventName]; !ok {
		t.Fatalf("event %s not allowed", eventName)
	}
}
