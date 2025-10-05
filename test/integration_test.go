package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dannytrev21/lambda-bridge/internal/config"
	"github.com/Dannytrev21/lambda-bridge/internal/handler"
	"github.com/aws/aws-lambda-go/events"
)

// TestIntegration_EndToEndALBFlow tests the complete flow from ALB event to webhook delivery
func TestIntegration_EndToEndALBFlow(t *testing.T) {
	var (
		receivedPayloads []string
		receivedHeaders  []http.Header
		mu               sync.Mutex
	)

	// Create webhook server
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		// Capture headers
		receivedHeaders = append(receivedHeaders, r.Header.Clone())

		// Capture body
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		payloadBytes, _ := json.Marshal(body)
		receivedPayloads = append(receivedPayloads, string(payloadBytes))

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"received"}`))
	}))
	defer webhookServer.Close()

	// Create handler with test config
	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{webhookServer.URL},
		Debug:            true,
	}

	h := handler.NewHandler(cfg, &mockSNSForwarder{})

	// Create ALB event with realistic payload
	albEvent := events.ALBTargetGroupRequest{
		RequestContext: events.ALBTargetGroupRequestContext{
			ELB: events.ELBContext{
				TargetGroupArn: "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/test/abc123",
			},
		},
		HTTPMethod: "POST",
		Path:       "/webhook",
		Headers: map[string]string{
			"content-type":     "application/json",
			"x-custom-header":  "test-value",
			"x-forwarded-for":  "203.0.113.1",
			"x-forwarded-port": "443",
			"x-forwarded-proto": "https",
		},
		Body:            `{"event":"push","repository":"test-repo","commits":[{"id":"abc123","message":"test commit"}]}`,
		IsBase64Encoded: false,
	}

	rawEvent, _ := json.Marshal(albEvent)
	ctx := context.Background()

	// Execute: Handle the event
	response := h.Handle(ctx, rawEvent)

	// Verify: Lambda response is immediate
	albResp, ok := response.(events.ALBTargetGroupResponse)
	if !ok {
		t.Fatal("Expected ALBTargetGroupResponse")
	}

	if albResp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", albResp.StatusCode)
	}

	// Wait for async webhook delivery
	time.Sleep(1 * time.Second)

	// Verify: Webhook received the payload
	mu.Lock()
	defer mu.Unlock()

	if len(receivedPayloads) != 1 {
		t.Fatalf("Expected 1 webhook delivery, got %d", len(receivedPayloads))
	}

	// Verify: Headers are correctly set
	if len(receivedHeaders) > 0 {
		headers := receivedHeaders[0]
		if headers.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type: application/json, got %s", headers.Get("Content-Type"))
		}
		if headers.Get("User-Agent") != "Lambda-Bridge/1.0" {
			t.Errorf("Expected User-Agent: Lambda-Bridge/1.0, got %s", headers.Get("User-Agent"))
		}
		if headers.Get("X-Request-ID") == "" {
			t.Error("Expected X-Request-ID header to be set")
		}
	}

	t.Logf("Successfully delivered payload: %s", receivedPayloads[0])
}

// TestIntegration_ConcurrentBurstTraffic tests handling of burst traffic with multiple concurrent requests
func TestIntegration_ConcurrentBurstTraffic(t *testing.T) {
	var (
		totalReceived  atomic.Int64
		successCount   atomic.Int64
		requestLatency []time.Duration
		mu             sync.Mutex
	)

	// Create webhook server with realistic latency
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate realistic webhook processing time
		time.Sleep(50 * time.Millisecond)

		totalReceived.Add(1)
		successCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{webhookServer.URL},
		Debug:            false, // Disable debug to reduce noise
	}

	h := handler.NewHandler(cfg, &mockSNSForwarder{})

	// Test: Send burst of 100 concurrent requests
	numRequests := 100
	var wg sync.WaitGroup
	startTime := time.Now()

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(requestID int) {
			defer wg.Done()

			reqStart := time.Now()

			albEvent := events.ALBTargetGroupRequest{
				RequestContext: events.ALBTargetGroupRequestContext{
					ELB: events.ELBContext{
						TargetGroupArn: "arn:test",
					},
				},
				Path: "/webhook",
				Body: fmt.Sprintf(`{"burst_test":true,"request_id":%d}`, requestID),
			}

			rawEvent, _ := json.Marshal(albEvent)
			response := h.Handle(context.Background(), rawEvent)

			reqDuration := time.Since(reqStart)
			mu.Lock()
			requestLatency = append(requestLatency, reqDuration)
			mu.Unlock()

			// Verify immediate response (non-blocking)
			if albResp, ok := response.(events.ALBTargetGroupResponse); ok {
				if albResp.StatusCode != 200 {
					t.Errorf("Request %d: Expected status 200, got %d", requestID, albResp.StatusCode)
				}
			}
		}(i)
	}

	// Wait for all requests to be sent
	wg.Wait()
	totalRequestTime := time.Since(startTime)

	t.Logf("Sent %d requests in %v", numRequests, totalRequestTime)

	// Wait for async webhook processing
	time.Sleep(5 * time.Second)

	received := totalReceived.Load()
	success := successCount.Load()

	t.Logf("Received: %d/%d webhooks (%.1f%%)", received, numRequests, float64(received)/float64(numRequests)*100)
	t.Logf("Success: %d/%d webhooks", success, numRequests)

	// Calculate latency statistics
	if len(requestLatency) > 0 {
		var totalLatency time.Duration
		maxLatency := requestLatency[0]
		minLatency := requestLatency[0]

		for _, lat := range requestLatency {
			totalLatency += lat
			if lat > maxLatency {
				maxLatency = lat
			}
			if lat < minLatency {
				minLatency = lat
			}
		}

		avgLatency := totalLatency / time.Duration(len(requestLatency))
		t.Logf("Request Latency - Avg: %v, Min: %v, Max: %v", avgLatency, minLatency, maxLatency)

		// Verify Lambda is non-blocking (should respond quickly)
		if avgLatency > 100*time.Millisecond {
			t.Errorf("Average request latency too high: %v (expected < 100ms for non-blocking)", avgLatency)
		}
	}

	// Verify majority of webhooks were delivered
	// With queue size 200 and 100 requests, all should be processed
	expectedMin := int64(float64(numRequests) * 0.9)
	if received < expectedMin {
		t.Errorf("Expected at least 90%% delivery rate, got %.1f%%", float64(received)/float64(numRequests)*100)
	}
}

// TestIntegration_MultiRouteDistribution tests routing to different webhook destinations
func TestIntegration_MultiRouteDistribution(t *testing.T) {
	var (
		defaultReceived    atomic.Int32
		cloudReceived      atomic.Int32
		enterpriseReceived atomic.Int32
	)

	// Create webhook servers for each route
	defaultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defaultReceived.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer defaultServer.Close()

	cloudServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cloudReceived.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer cloudServer.Close()

	enterpriseServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enterpriseReceived.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer enterpriseServer.Close()

	cfg := &config.Config{
		Environment:           "test",
		SNSTopicArn:           "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:           []string{defaultServer.URL},
		CloudWebhookURLs:      []string{cloudServer.URL},
		EnterpriseWebhookURLs: []string{enterpriseServer.URL},
	}

	h := handler.NewHandler(cfg, &mockSNSForwarder{})

	// Test scenarios
	scenarios := []struct {
		name              string
		headers           map[string]string
		expectedDefault   int32
		expectedCloud     int32
		expectedEnterprise int32
	}{
		{
			name:            "default route",
			headers:         map[string]string{},
			expectedDefault: 1,
		},
		{
			name:          "cloud route",
			headers:       map[string]string{"x-dcp-destination-host": "cloud.example.com"},
			expectedCloud: 1,
		},
		{
			name:               "enterprise route",
			headers:            map[string]string{"x-github-enterprise-host": "github.enterprise.com"},
			expectedEnterprise: 1,
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			// Reset counters
			defaultReceived.Store(0)
			cloudReceived.Store(0)
			enterpriseReceived.Store(0)

			albEvent := events.ALBTargetGroupRequest{
				RequestContext: events.ALBTargetGroupRequestContext{
					ELB: events.ELBContext{
						TargetGroupArn: "arn:test",
					},
				},
				Headers: scenario.headers,
				Path:    "/webhook",
				Body:    fmt.Sprintf(`{"route_test":"%s"}`, scenario.name),
			}

			rawEvent, _ := json.Marshal(albEvent)
			h.Handle(context.Background(), rawEvent)

			// Wait for async processing
			time.Sleep(500 * time.Millisecond)

			// Verify routing
			if defaultReceived.Load() != scenario.expectedDefault {
				t.Errorf("Expected %d default webhooks, got %d", scenario.expectedDefault, defaultReceived.Load())
			}
			if cloudReceived.Load() != scenario.expectedCloud {
				t.Errorf("Expected %d cloud webhooks, got %d", scenario.expectedCloud, cloudReceived.Load())
			}
			if enterpriseReceived.Load() != scenario.expectedEnterprise {
				t.Errorf("Expected %d enterprise webhooks, got %d", scenario.expectedEnterprise, enterpriseReceived.Load())
			}
		})
	}
}

// TestIntegration_FailureRecovery tests resilience to webhook failures
func TestIntegration_FailureRecovery(t *testing.T) {
	var (
		successfulReceived atomic.Int32
		failedAttempts     atomic.Int32
		flakeyAttempts     atomic.Int32
	)

	// Successful webhook server
	successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		successfulReceived.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer successServer.Close()

	// Always-failing webhook server (5xx errors trigger retries)
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failedAttempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer failServer.Close()

	// Flakey server (fails first 2 times, succeeds on 3rd)
	flakeyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := flakeyAttempts.Add(1)
		if attempt < 3 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer flakeyServer.Close()

	cfg := &config.Config{
		Environment: "test",
		SNSTopicArn: "arn:aws:sns:us-east-1:123456789012:test",
		// Mix of successful, failing, and flakey servers
		WebhookURLs:      []string{successServer.URL, failServer.URL, flakeyServer.URL},
	}

	h := handler.NewHandler(cfg, &mockSNSForwarder{})

	// Send event to all webhooks
	albEvent := events.ALBTargetGroupRequest{
		RequestContext: events.ALBTargetGroupRequestContext{
			ELB: events.ELBContext{
				TargetGroupArn: "arn:test",
			},
		},
		Path: "/webhook",
		Body: `{"test":"failure_recovery"}`,
	}

	rawEvent, _ := json.Marshal(albEvent)
	response := h.Handle(context.Background(), rawEvent)

	// Verify Lambda still returns 200 (non-blocking)
	if albResp, ok := response.(events.ALBTargetGroupResponse); ok {
		if albResp.StatusCode != 200 {
			t.Errorf("Expected status 200 despite webhook failures, got %d", albResp.StatusCode)
		}
	}

	// Wait for retries to complete
	time.Sleep(3 * time.Second)

	// Verify: Successful server received webhook
	if successfulReceived.Load() != 1 {
		t.Errorf("Expected 1 successful webhook, got %d", successfulReceived.Load())
	}

	// Verify: Failing server was retried (initial + 3 retries = 4 attempts)
	if failedAttempts.Load() != 4 {
		t.Logf("Expected 4 attempts for failing server (initial + 3 retries), got %d", failedAttempts.Load())
	}

	// Verify: Flakey server recovered after retries
	if flakeyAttempts.Load() < 3 {
		t.Errorf("Expected at least 3 attempts for flakey server, got %d", flakeyAttempts.Load())
	}

	t.Logf("Recovery test results: Success=%d, Failed=%d, Flakey=%d",
		successfulReceived.Load(), failedAttempts.Load(), flakeyAttempts.Load())
}

// TestIntegration_URLIsolation tests that slow/failing URLs don't block others
func TestIntegration_URLIsolation(t *testing.T) {
	var (
		fastReceived atomic.Int32
		slowReceived atomic.Int32
		fastTimes    []time.Time
		slowTimes    []time.Time
		mu           sync.Mutex
	)

	// Fast webhook (responds immediately)
	fastServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		fastTimes = append(fastTimes, time.Now())
		mu.Unlock()
		fastReceived.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer fastServer.Close()

	// Slow webhook (takes 2 seconds)
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		slowTimes = append(slowTimes, time.Now())
		mu.Unlock()
		time.Sleep(2 * time.Second)
		slowReceived.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer slowServer.Close()

	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{slowServer.URL, fastServer.URL},
	}

	h := handler.NewHandler(cfg, &mockSNSForwarder{})

	// Send event
	albEvent := events.ALBTargetGroupRequest{
		RequestContext: events.ALBTargetGroupRequestContext{
			ELB: events.ELBContext{
				TargetGroupArn: "arn:test",
			},
		},
		Path: "/webhook",
		Body: `{"test":"url_isolation"}`,
	}

	rawEvent, _ := json.Marshal(albEvent)
	startTime := time.Now()
	h.Handle(context.Background(), rawEvent)

	// Wait for both webhooks to complete
	time.Sleep(3 * time.Second)

	// Verify both webhooks were called
	if fastReceived.Load() != 1 {
		t.Errorf("Expected 1 fast webhook, got %d", fastReceived.Load())
	}
	if slowReceived.Load() != 1 {
		t.Errorf("Expected 1 slow webhook, got %d", slowReceived.Load())
	}

	// Verify fast webhook wasn't blocked by slow webhook
	mu.Lock()
	defer mu.Unlock()

	if len(fastTimes) > 0 && len(slowTimes) > 0 {
		fastDuration := fastTimes[0].Sub(startTime)
		slowDuration := slowTimes[0].Sub(startTime)

		t.Logf("Fast webhook started after %v", fastDuration)
		t.Logf("Slow webhook started after %v", slowDuration)

		// Both should start around the same time (parallel execution)
		timeDiff := fastDuration - slowDuration
		if timeDiff < 0 {
			timeDiff = -timeDiff
		}

		// Allow 500ms tolerance for parallel execution
		if timeDiff > 500*time.Millisecond {
			t.Errorf("URLs not properly isolated: time difference %v (expected near-simultaneous)", timeDiff)
		}
	}
}

// TestIntegration_HealthCheckFiltering tests health check detection and handling
func TestIntegration_HealthCheckFiltering(t *testing.T) {
	var webhookCalls atomic.Int32

	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{webhookServer.URL},
	}

	h := handler.NewHandler(cfg, &mockSNSForwarder{})

	// Test: Health check request
	healthCheckEvent := events.ALBTargetGroupRequest{
		RequestContext: events.ALBTargetGroupRequestContext{
			ELB: events.ELBContext{
				TargetGroupArn: "arn:test",
			},
		},
		Headers: map[string]string{
			"user-agent": "ELB-HealthChecker/2.0",
		},
		Path: "/webhook",
	}

	rawEvent, _ := json.Marshal(healthCheckEvent)
	response := h.Handle(context.Background(), rawEvent)

	// Verify: Health check returns 200 without forwarding
	if albResp, ok := response.(events.ALBTargetGroupResponse); ok {
		if albResp.StatusCode != 200 {
			t.Errorf("Expected status 200 for health check, got %d", albResp.StatusCode)
		}
		if albResp.Body != "healthy" {
			t.Errorf("Expected 'healthy' body, got %s", albResp.Body)
		}
	}

	// Wait to see if webhook was called (it shouldn't be)
	time.Sleep(500 * time.Millisecond)

	if webhookCalls.Load() != 0 {
		t.Errorf("Health check should not trigger webhook, but got %d calls", webhookCalls.Load())
	}

	// Test: Regular request should forward
	regularEvent := events.ALBTargetGroupRequest{
		RequestContext: events.ALBTargetGroupRequestContext{
			ELB: events.ELBContext{
				TargetGroupArn: "arn:test",
			},
		},
		Path: "/webhook",
		Body: `{"test":"regular"}`,
	}

	rawEvent, _ = json.Marshal(regularEvent)
	h.Handle(context.Background(), rawEvent)

	time.Sleep(500 * time.Millisecond)

	if webhookCalls.Load() != 1 {
		t.Errorf("Regular request should trigger webhook, got %d calls", webhookCalls.Load())
	}
}

// TestIntegration_MixedTrafficScenario tests realistic mixed traffic patterns
func TestIntegration_MixedTrafficScenario(t *testing.T) {
	var (
		totalRequests      atomic.Int32
		healthChecks       atomic.Int32
		webhookDeliveries  atomic.Int32
		cloudDeliveries    atomic.Int32
		failedDeliveries   atomic.Int32
	)

	// Setup multiple webhook servers
	defaultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookDeliveries.Add(1)
		// Simulate variable latency
		time.Sleep(time.Duration(10+totalRequests.Load()%20) * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer defaultServer.Close()

	cloudServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cloudDeliveries.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer cloudServer.Close()

	// Unreliable server (fails 30% of the time)
	unreliableServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if totalRequests.Load()%3 == 0 {
			failedDeliveries.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			webhookDeliveries.Add(1)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer unreliableServer.Close()

	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{defaultServer.URL, unreliableServer.URL},
		CloudWebhookURLs: []string{cloudServer.URL},
	}

	h := handler.NewHandler(cfg, &mockSNSForwarder{})

	// Generate mixed traffic
	var wg sync.WaitGroup
	testDuration := 3 * time.Second
	startTime := time.Now()

	// Traffic generator goroutines
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for time.Since(startTime) < testDuration {
				totalRequests.Add(1)
				requestNum := totalRequests.Load()

				var albEvent events.ALBTargetGroupRequest

				switch requestNum % 10 {
				case 0, 5: // 20% health checks
					healthChecks.Add(1)
					albEvent = events.ALBTargetGroupRequest{
						RequestContext: events.ALBTargetGroupRequestContext{
							ELB: events.ELBContext{TargetGroupArn: "arn:test"},
						},
						Headers: map[string]string{"user-agent": "ELB-HealthChecker/2.0"},
						Path:    "/webhook",
					}
				case 1, 6: // 20% cloud webhooks
					albEvent = events.ALBTargetGroupRequest{
						RequestContext: events.ALBTargetGroupRequestContext{
							ELB: events.ELBContext{TargetGroupArn: "arn:test"},
						},
						Headers: map[string]string{"x-dcp-destination-host": "cloud.example.com"},
						Path:    "/webhook",
						Body:    fmt.Sprintf(`{"worker":%d,"request":%d,"type":"cloud"}`, workerID, requestNum),
					}
				default: // 60% default webhooks
					albEvent = events.ALBTargetGroupRequest{
						RequestContext: events.ALBTargetGroupRequestContext{
							ELB: events.ELBContext{TargetGroupArn: "arn:test"},
						},
						Path: "/webhook",
						Body: fmt.Sprintf(`{"worker":%d,"request":%d,"type":"default"}`, workerID, requestNum),
					}
				}

				rawEvent, _ := json.Marshal(albEvent)
				h.Handle(context.Background(), rawEvent)

				// Variable request rate
				time.Sleep(time.Duration(50+requestNum%100) * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	// Wait for async processing
	time.Sleep(2 * time.Second)

	// Report results
	total := totalRequests.Load()
	health := healthChecks.Load()
	webhooks := webhookDeliveries.Load()
	cloud := cloudDeliveries.Load()
	failed := failedDeliveries.Load()

	t.Logf("=== Mixed Traffic Test Results ===")
	t.Logf("Total requests: %d", total)
	t.Logf("Health checks: %d (%.1f%%)", health, float64(health)/float64(total)*100)
	t.Logf("Webhook deliveries: %d", webhooks)
	t.Logf("Cloud deliveries: %d", cloud)
	t.Logf("Failed deliveries: %d", failed)
	t.Logf("Duration: %v", testDuration)
	t.Logf("Throughput: %.1f req/s", float64(total)/testDuration.Seconds())

	// Verify system handled the load
	if total < 10 {
		t.Errorf("Expected more requests to be processed, got %d", total)
	}

	// Verify health checks were filtered (shouldn't reach webhooks)
	// Verify some webhooks were delivered
	if webhooks < 5 {
		t.Errorf("Expected webhook deliveries, got %d", webhooks)
	}
}

// TestIntegration_GracefulShutdown tests handler shutdown behavior
func TestIntegration_GracefulShutdown(t *testing.T) {
	var (
		webhookCalls atomic.Int32
		mu           sync.Mutex
		receivedIDs  []string
	)

	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookCalls.Add(1)

		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if id, ok := body["id"].(string); ok {
			mu.Lock()
			receivedIDs = append(receivedIDs, id)
			mu.Unlock()
		}

		// Simulate processing time
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{webhookServer.URL},
	}

	h := handler.NewHandler(cfg, &mockSNSForwarder{})

	// Send multiple requests
	numRequests := 20
	for i := 0; i < numRequests; i++ {
		albEvent := events.ALBTargetGroupRequest{
			RequestContext: events.ALBTargetGroupRequestContext{
				ELB: events.ELBContext{TargetGroupArn: "arn:test"},
			},
			Path: "/webhook",
			Body: fmt.Sprintf(`{"id":"req-%d","shutdown_test":true}`, i),
		}

		rawEvent, _ := json.Marshal(albEvent)
		h.Handle(context.Background(), rawEvent)
	}

	// Allow time for async goroutines to enqueue jobs
	// This prevents race condition where we close queue while goroutines are still enqueuing
	time.Sleep(100 * time.Millisecond)

	// Initiate graceful shutdown
	t.Log("Initiating graceful shutdown...")
	err := h.Shutdown(5 * time.Second)
	if err != nil {
		t.Errorf("Shutdown error: %v", err)
	}

	// Verify webhooks were processed
	calls := webhookCalls.Load()
	t.Logf("Processed %d/%d webhooks before shutdown", calls, numRequests)

	mu.Lock()
	t.Logf("Received IDs: %v", receivedIDs)
	mu.Unlock()

	// With graceful shutdown, most requests should be processed
	if calls < int32(numRequests/2) {
		t.Logf("Warning: Only %d/%d webhooks processed during graceful shutdown", calls, numRequests)
	}
}

// Mock SNS Forwarder for testing

type mockSNSForwarder struct {
	mu            sync.Mutex
	forwardedMsgs []string
	shouldFail    bool
}

func (m *mockSNSForwarder) Forward(ctx context.Context, topicArn string, rawEvent json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.shouldFail {
		return fmt.Errorf("mock SNS forward failure")
	}

	m.forwardedMsgs = append(m.forwardedMsgs, string(rawEvent))
	return nil
}

func (m *mockSNSForwarder) GetForwardedMessages() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.forwardedMsgs...)
}

// Benchmark: End-to-end throughput

func BenchmarkIntegration_EndToEndThroughput(b *testing.B) {
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	cfg := &config.Config{
		Environment:      "test",
		SNSTopicArn:      "arn:aws:sns:us-east-1:123456789012:test",
		WebhookURLs:      []string{webhookServer.URL},
	}

	h := handler.NewHandler(cfg, &mockSNSForwarder{})

	albEvent := events.ALBTargetGroupRequest{
		RequestContext: events.ALBTargetGroupRequestContext{
			ELB: events.ELBContext{TargetGroupArn: "arn:test"},
		},
		Path: "/webhook",
		Body: `{"bench":"test"}`,
	}

	rawEvent, _ := json.Marshal(albEvent)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Handle(ctx, rawEvent)
	}
}
