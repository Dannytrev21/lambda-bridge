package forwarder

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkForwardToWebhooks(b *testing.B) {
	counts := []int{1, 5, 10}

	payload := json.RawMessage(`{"benchmark":true}`)

	for _, count := range counts {
		b.Run(fmt.Sprintf("webhooks=%d", count), func(b *testing.B) {
			servers := make([]*httptest.Server, count)
			urls := make([]string, count)

			for i := 0; i < count; i++ {
				servers[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				urls[i] = servers[i].URL
			}
			defer func() {
				for _, server := range servers {
					server.Close()
				}
			}()

			forwarder := NewWebhookForwarder()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				forwarder.ForwardToWebhooks(context.Background(), urls, payload)
			}
		})
	}
}
