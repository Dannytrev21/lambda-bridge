package forwarder

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/textproto"
	"net/url"

	"github.com/aws/aws-lambda-go/events"
)

// webhookPayload contains the extracted payload data for webhooks.
type webhookPayload struct {
	method        string
	body          []byte
	headers       http.Header
	path          string
	rawQuery      string
	base64Encoded bool
}

// preparedRequest represents a prepared HTTP request ready to send.
type preparedRequest struct {
	method  string
	url     string
	headers http.Header
	body    []byte
	urlHash string
}

// newWebhookPayload creates a webhook payload from a raw JSON event.
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

// buildGithubPayload creates a webhook payload from an ALB event for GitHub webhooks.
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

// extractBody extracts the body from an ALB event, decoding base64 if needed.
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

// mergeHeaders merges headers from the ALB event, handling both single and multi-value headers.
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

// buildRawQuery builds the raw query string from ALB event parameters.
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

// defaultHeaders returns default headers for webhook requests.
func defaultHeaders() http.Header {
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("User-Agent", "lambda-bridge/1.0")
	return headers
}
