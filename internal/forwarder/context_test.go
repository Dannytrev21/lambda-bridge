package forwarder

import (
	"context"
	"strings"
	"testing"

	"github.com/Dannytrev21/lambda-bridge/internal/constants"
	"github.com/stretchr/testify/assert"
)

func TestRequestIDGeneration(t *testing.T) {
	id1 := GenerateRequestID()
	id2 := GenerateRequestID()

	assert.NotEmpty(t, id1, "Should generate non-empty request ID")
	assert.NotEmpty(t, id2, "Should generate non-empty request ID")
	assert.NotEqual(t, id1, id2, "Should generate unique request IDs")
	assert.True(t, strings.HasPrefix(id1, "req-"), "Should have req- prefix")
	assert.True(t, strings.HasPrefix(id2, "req-"), "Should have req- prefix")
}

func TestContextPropagation(t *testing.T) {
	ctx := context.Background()

	// Test request ID propagation
	requestID := "test-request-123"
	ctx = WithRequestID(ctx, requestID)

	retrievedID := RequestIDFromContext(ctx)
	assert.Equal(t, requestID, retrievedID, "Should retrieve the same request ID")

	// Test trace ID propagation
	traceID := "trace-abc-456"
	ctx = WithTraceID(ctx, traceID)

	retrievedTrace := TraceIDFromContext(ctx)
	assert.Equal(t, traceID, retrievedTrace, "Should retrieve the same trace ID")
}

func TestContextWithoutIDs(t *testing.T) {
	ctx := context.Background()

	// Should generate a new request ID if none exists
	requestID := RequestIDFromContext(ctx)
	assert.NotEmpty(t, requestID, "Should generate request ID when none exists")
	assert.True(t, strings.HasPrefix(requestID, "req-"), "Should have req- prefix")

	// Should return empty string for trace ID if none exists
	traceID := TraceIDFromContext(ctx)
	assert.Empty(t, traceID, "Should return empty trace ID when none exists")
}

func TestWebhookTimeoutContext(t *testing.T) {
	ctx := context.Background()

	ctxWithTimeout, cancel := WithWebhookTimeout(ctx, constants.DefaultWebhookTimeout)
	defer cancel()

	deadline, ok := ctxWithTimeout.Deadline()
	assert.True(t, ok, "Context should have a deadline")
	assert.NotZero(t, deadline, "Deadline should be set")
}