package forwarder

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestConnectionWarmer_WarmConnections(t *testing.T) {
	// Create test server (even though ConnectionWarmer uses TCP, having a server listening helps)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	urls := []string{server.URL}
	warmer := NewConnectionWarmer(urls)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Should not panic - connection warming opens TCP connections, not HTTP
	warmer.WarmConnections(ctx)
}

func TestConnectionWarmer_EmptyURLs(t *testing.T) {
	warmer := NewConnectionWarmer([]string{})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Should not panic with empty URLs
	warmer.WarmConnections(ctx)
}

func TestConnectionWarmer_InvalidURL(t *testing.T) {
	urls := []string{"ht!tp://invalid url", "http://valid.com/webhook"}
	warmer := NewConnectionWarmer(urls)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Should not panic with invalid URL
	warmer.WarmConnections(ctx)
}

func TestConnectionWarmer_ContextCancellation(t *testing.T) {
	// Create server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	urls := []string{server.URL}
	warmer := NewConnectionWarmer(urls)

	// Context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	warmer.WarmConnections(ctx)
	duration := time.Since(start)

	// Should return quickly due to context cancellation
	assert.Less(t, duration, 200*time.Millisecond, "Should respect context timeout")
}

func TestConnectionWarmer_MultipleURLs(t *testing.T) {
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server2.Close()

	urls := []string{server1.URL, server2.URL}
	warmer := NewConnectionWarmer(urls)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Should not panic with multiple URLs
	warmer.WarmConnections(ctx)
}

func TestConnectionWarmer_ServerError(t *testing.T) {
	// Create server that returns errors
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	urls := []string{server.URL}
	warmer := NewConnectionWarmer(urls)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Should not panic even if server returns errors
	warmer.WarmConnections(ctx)
}

func TestNewConnectionWarmer(t *testing.T) {
	urls := []string{"http://example.com/webhook1", "http://example.com/webhook2"}
	warmer := NewConnectionWarmer(urls)

	assert.NotNil(t, warmer, "Should create ConnectionWarmer")
}
