package forwarder

import (
	"context"
	"net"
	"net/url"
	"sync"
	"time"
)

type ConnectionWarmer struct {
	urls []string
}

func NewConnectionWarmer(urls []string) *ConnectionWarmer {
	return &ConnectionWarmer{
		urls: urls,
	}
}

func (w *ConnectionWarmer) WarmConnections(ctx context.Context) {
	if len(w.urls) == 0 {
		return
	}

	var wg sync.WaitGroup
	timeout := 2 * time.Second

	for _, botURL := range w.urls {
		wg.Add(1)
		go func(targetURL string) {
			defer wg.Done()
			w.warmConnection(ctx, targetURL, timeout)
		}(botURL)
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All connections warmed
	case <-time.After(5 * time.Second):
		// Timeout, continue anyway
	}
}

func (w *ConnectionWarmer) warmConnection(ctx context.Context, targetURL string, timeout time.Duration) {
	u, err := url.Parse(targetURL)
	if err != nil {
		return
	}

	// Determine port
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	// Create TCP connection
	dialer := &net.Dialer{
		Timeout: timeout,
	}

	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if err == nil {
		conn.Close()
	}
}
