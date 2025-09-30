package forwarder

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/textproto"
	"net/url"
	"sync"
	"time"

	"github.com/aws/aws-lambda-go/events"
)

type WebhookResult struct {
	StatusCode int
	Error      error
	Duration   time.Duration
}

type WebhookForwarder struct {
	client *http.Client
}

func NewWebhookForwarder() *WebhookForwarder {
	transport := &http.Transport{
		MaxIdleConns:          128,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &WebhookForwarder{
		client: &http.Client{
			Timeout:   10 * time.Second,
			Transport: transport,
		},
	}
}

// ForwardToWebhooks sends the raw event to all webhook URLs in parallel
// Returns a map of URL to result for analysis
func (f *WebhookForwarder) ForwardToWebhooks(ctx context.Context, webhookURLs []string, rawEvent json.RawMessage) map[string]WebhookResult {
	results := make(map[string]WebhookResult)
	var mu sync.Mutex
	var wg sync.WaitGroup

	payload := newWebhookPayload(rawEvent)

	templates := make([]preparedRequest, len(webhookURLs))
	for i, url := range webhookURLs {
		templates[i] = newPreparedRequest(url, payload)
	}

	for i, url := range webhookURLs {
		wg.Add(1)
		template := templates[i]
		go func(webhookURL string, req preparedRequest) {
			defer wg.Done()
			result := f.forwardToWebhookWithRetry(ctx, req)

			mu.Lock()
			results[webhookURL] = result
			mu.Unlock()
		}(url, template)
	}

	wg.Wait()
	return results
}

func (f *WebhookForwarder) forwardToWebhookWithRetry(ctx context.Context, req preparedRequest) WebhookResult {
	maxRetries := 3
	baseDelay := 100 * time.Millisecond

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			delay := baseDelay * time.Duration(1<<uint(attempt-1))
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return WebhookResult{Error: ctx.Err()}
			}
		}

		result := f.forwardToWebhook(ctx, req)

		// Success or non-retryable error
		if result.Error == nil || result.StatusCode < 500 {
			return result
		}

		// Last attempt, return the error
		if attempt == maxRetries {
			return result
		}
	}

	return WebhookResult{Error: fmt.Errorf("max retries exceeded")}
}

func (f *WebhookForwarder) forwardToWebhook(ctx context.Context, reqTemplate preparedRequest) WebhookResult {
	start := time.Now()

	bodyReader := bytes.NewReader(reqTemplate.body)
	req, err := http.NewRequestWithContext(ctx, reqTemplate.method, reqTemplate.url, bodyReader)
	if err != nil {
		return WebhookResult{
			Error:    fmt.Errorf("failed to create request: %w", err),
			Duration: time.Since(start),
		}
	}

	req.Header = reqTemplate.headers.Clone()
	req.ContentLength = int64(len(reqTemplate.body))

	// Make the request
	resp, err := f.client.Do(req)
	if err != nil {
		return WebhookResult{
			Error:    fmt.Errorf("request failed: %w", err),
			Duration: time.Since(start),
		}
	}
	defer resp.Body.Close()

	result := WebhookResult{
		StatusCode: resp.StatusCode,
		Duration:   time.Since(start),
	}

	// Check for HTTP error status
	if resp.StatusCode >= 400 {
		result.Error = fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return result
}

type webhookPayload struct {
	method        string
	body          []byte
	headers       http.Header
	path          string
	rawQuery      string
	base64Encoded bool
}

type preparedRequest struct {
	method  string
	url     string
	headers http.Header
	body    []byte
}

func newWebhookPayload(rawEvent json.RawMessage) webhookPayload {
	var albEvent events.ALBTargetGroupRequest
	if err := json.Unmarshal(rawEvent, &albEvent); err == nil {
		if payload, ok := buildGithubPayload(&albEvent); ok {
			return payload
		}
	}

	return webhookPayload{
		method:  http.MethodPost,
		body:    append([]byte(nil), rawEvent...),
		headers: defaultHeaders(),
	}
}

func buildGithubPayload(albEvent *events.ALBTargetGroupRequest) (webhookPayload, bool) {
	if albEvent == nil {
		return webhookPayload{}, false
	}

	body, err := extractBody(albEvent)
	if err != nil {
		return webhookPayload{}, false
	}

	headers := mergeHeaders(albEvent)
	if headers.Get("X-GitHub-Event") == "" {
		return webhookPayload{}, false
	}

	if headers.Get("Content-Type") == "" {
		headers.Set("Content-Type", "application/json")
	}

	rawQuery := buildRawQuery(albEvent)
	if albEvent.Path != "" {
		headers.Set("X-Original-Path", albEvent.Path)
	}
	if rawQuery != "" {
		headers.Set("X-Original-Raw-Query", rawQuery)
	}
	if albEvent.RequestContext.ELB.TargetGroupArn != "" {
		headers.Set("X-Original-Target-Group-Arn", albEvent.RequestContext.ELB.TargetGroupArn)
	}
	headers.Set("X-Original-Base64-Encoded", fmt.Sprintf("%t", albEvent.IsBase64Encoded))

	return webhookPayload{
		method:        albEvent.HTTPMethod,
		body:          body,
		headers:       headers,
		path:          albEvent.Path,
		rawQuery:      rawQuery,
		base64Encoded: albEvent.IsBase64Encoded,
	}, true
}

func extractBody(albEvent *events.ALBTargetGroupRequest) ([]byte, error) {
	if albEvent.IsBase64Encoded {
		decoded, err := base64.StdEncoding.DecodeString(albEvent.Body)
		if err != nil {
			return nil, fmt.Errorf("decode base64 body: %w", err)
		}
		return decoded, nil
	}
	return []byte(albEvent.Body), nil
}

func mergeHeaders(albEvent *events.ALBTargetGroupRequest) http.Header {
	headers := http.Header{}
	for key, values := range albEvent.MultiValueHeaders {
		canonicalKey := textproto.CanonicalMIMEHeaderKey(key)
		for _, value := range values {
			headers.Add(canonicalKey, value)
		}
	}

	for key, value := range albEvent.Headers {
		canonicalKey := textproto.CanonicalMIMEHeaderKey(key)
		if len(headers[canonicalKey]) == 0 {
			headers.Set(canonicalKey, value)
		}
	}

	return headers
}

func defaultHeaders() http.Header {
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("User-Agent", "lambda-bridge/1.0")
	return headers
}

func newPreparedRequest(url string, payload webhookPayload) preparedRequest {
	method := payload.method
	if method == "" {
		method = http.MethodPost
	}

	headers := payload.headers.Clone()
	if headers.Get("Content-Type") == "" {
		headers.Set("Content-Type", "application/json")
	}
	if headers.Get("User-Agent") == "" {
		headers.Set("User-Agent", "lambda-bridge/1.0")
	}

	parsedURL, err := urlParse(url)
	if err == nil {
		if payload.rawQuery != "" {
			if parsedURL.RawQuery != "" {
				parsedURL.RawQuery = parsedURL.RawQuery + "&" + payload.rawQuery
			} else {
				parsedURL.RawQuery = payload.rawQuery
			}
		}
		url = parsedURL.String()
	}

	return preparedRequest{
		method:  method,
		url:     url,
		headers: headers,
		body:    payload.body,
	}
}

func buildRawQuery(albEvent *events.ALBTargetGroupRequest) string {
	if albEvent == nil {
		return ""
	}
	values := url.Values{}
	for key, multi := range albEvent.MultiValueQueryStringParameters {
		for _, value := range multi {
			values.Add(key, value)
		}
	}
	for key, value := range albEvent.QueryStringParameters {
		if _, exists := values[key]; !exists {
			values.Add(key, value)
		}
	}
	return values.Encode()
}

func urlParse(raw string) (*url.URL, error) {
	return url.Parse(raw)
}
