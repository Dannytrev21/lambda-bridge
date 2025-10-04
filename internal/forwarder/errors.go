package forwarder

import (
	"errors"
	"fmt"
	"net/http"
)

// Sentinel errors for common failure modes
var (
	// ErrTimeout indicates a request timed out
	ErrTimeout = errors.New("request timeout")

	// ErrInvalidURL indicates a malformed webhook URL
	ErrInvalidURL = errors.New("invalid webhook url")

	// ErrHTTPError indicates an HTTP error status code
	ErrHTTPError = errors.New("http error")
)

// WebhookError wraps errors with webhook-specific context.
type WebhookError struct {
	Op        string // Operation that failed (e.g., "forward", "retry", "parse_url")
	URL       string // Webhook URL (may be hashed for privacy)
	RequestID string // Request correlation ID
	Err       error  // Underlying error
}

func (e *WebhookError) Error() string {
	if e.RequestID != "" {
		return fmt.Sprintf("%s %s (request_id=%s): %v", e.Op, e.URL, e.RequestID, e.Err)
	}
	return fmt.Sprintf("%s %s: %v", e.Op, e.URL, e.Err)
}

func (e *WebhookError) Unwrap() error {
	return e.Err
}

// HTTPError represents an HTTP error response with status code.
type HTTPError struct {
	StatusCode int
	Status     string
	URL        string
	RequestID  string
}

func (e *HTTPError) Error() string {
	if e.RequestID != "" {
		return fmt.Sprintf("HTTP %d %s (request_id=%s)", e.StatusCode, e.Status, e.RequestID)
	}
	return fmt.Sprintf("HTTP %d %s", e.StatusCode, e.Status)
}

func (e *HTTPError) Is(target error) bool {
	return target == ErrHTTPError
}

// NewHTTPError creates an HTTPError from a status code.
func NewHTTPError(statusCode int, url, requestID string) *HTTPError {
	return &HTTPError{
		StatusCode: statusCode,
		Status:     http.StatusText(statusCode),
		URL:        url,
		RequestID:  requestID,
	}
}

// ErrCircuitBreakerOpen is defined in breaker.go but referenced here for error checking.
var ErrCircuitBreakerOpen = errors.New("circuit breaker open")

// IsRetryable returns true if the error indicates a retryable condition.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Circuit breaker open is not retryable
	if errors.Is(err, ErrCircuitBreakerOpen) {
		return false
	}

	// Timeout errors are retryable
	if errors.Is(err, ErrTimeout) {
		return true
	}

	// HTTP errors: retry 5xx, don't retry 4xx
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode >= 500
	}

	// Default: retry on unknown errors
	return true
}

// IsClientError returns true if the error is a 4xx client error.
func IsClientError(err error) bool {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode >= 400 && httpErr.StatusCode < 500
	}
	return false
}

// IsServerError returns true if the error is a 5xx server error.
func IsServerError(err error) bool {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode >= 500
	}
	return false
}
