package forwarder

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// RequestIDHeader is the header name for request correlation ID
	RequestIDHeader = "X-Request-ID"
	// TraceIDHeader is the header name for distributed tracing
	TraceIDHeader = "X-Trace-ID"
)

type contextKey string

const (
	requestIDKey contextKey = "request-id"
	traceIDKey   contextKey = "trace-id"
)

// GenerateRequestID creates a new request correlation ID.
func GenerateRequestID() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID if random generation fails
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("req-%s", hex.EncodeToString(bytes))
}

// WithRequestID adds a request ID to the context.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// RequestIDFromContext extracts the request ID from context.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return GenerateRequestID()
}

// WithTraceID adds a trace ID to the context.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

// TraceIDFromContext extracts the trace ID from context.
func TraceIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(traceIDKey).(string); ok {
		return id
	}
	return ""
}

// newPreparedRequest creates a prepared HTTP request from a webhook payload.
func newPreparedRequest(ctx context.Context, targetURL string, payload webhookPayload) preparedRequest {
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

	// Add tracing headers from context
	if requestID := RequestIDFromContext(ctx); requestID != "" {
		headers.Set(RequestIDHeader, requestID)
	}
	if traceID := TraceIDFromContext(ctx); traceID != "" {
		headers.Set(TraceIDHeader, traceID)
	}

	parsedURL, err := url.Parse(targetURL)
	urlHash := targetURL
	if err == nil {
		if payload.rawQuery != "" {
			if parsedURL.RawQuery != "" {
				parsedURL.RawQuery = parsedURL.RawQuery + "&" + payload.rawQuery
			} else {
				parsedURL.RawQuery = payload.rawQuery
			}
		}
		targetURL = parsedURL.String()
		urlHash = strings.ToLower(parsedURL.Host + parsedURL.Path)
	}

	return preparedRequest{
		method:  method,
		url:     targetURL,
		headers: headers,
		body:    payload.body,
		urlHash: urlHash,
	}
}
