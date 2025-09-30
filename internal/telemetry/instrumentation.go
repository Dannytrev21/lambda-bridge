package telemetry

import (
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// HTTPClient returns an HTTP client configured with OTel transport instrumentation.
func HTTPClient(base *http.Client, name string) *http.Client {
	client := &http.Client{}
	if base != nil {
		*client = *base
	}
	client.Transport = otelhttp.NewTransport(
		client.Transport,
		otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string {
			if name != "" {
				return name
			}
			return operation
		}),
	)
	client.Timeout = baseTimeout(base)
	return client
}

// HTTPHandler wraps an http.Handler with OTel tracing middleware.
func HTTPHandler(name string, handler http.Handler) http.Handler {
	return otelhttp.NewHandler(handler, name)
}

// Tracer returns a tracer instance scoped to the provided instrumentation name.
func Tracer(name string) trace.Tracer {
	if name == "" {
		name = "github.com/Dannytrev21/lambda-bridge"
	}
	return otel.Tracer(name)
}

func baseTimeout(base *http.Client) time.Duration {
	if base == nil {
		return 0
	}
	return base.Timeout
}
