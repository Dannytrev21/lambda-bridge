package telemetry

import (
	"os"
	"runtime"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
)

// Config captures runtime configuration for telemetry providers.
type Config struct {
	ServiceName         string
	ServiceVersion      string
	Environment         string
	TracingEnabled      bool
	MetricsEnabled      bool
	OTLPTraceEndpoint   string
	OTLPMetricsEndpoint string
	XRayEndpoint        string
	SampleRatio         float64
}

// DefaultConfig returns sensible defaults for the Lambda Bridge service.
func DefaultConfig() Config {
	cfg := Config{
		ServiceName:         "lambda-bridge",
		ServiceVersion:      os.Getenv("SERVICE_VERSION"),
		Environment:         envOrDefault(os.Getenv("ENV"), os.Getenv("ENVIRONMENT"), "dev"),
		TracingEnabled:      true,
		MetricsEnabled:      true,
		SampleRatio:         1.0,
		OTLPTraceEndpoint:   os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		OTLPMetricsEndpoint: os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"),
		XRayEndpoint:        os.Getenv("OTEL_AWS_XRAY_ENDPOINT"),
	}

	if ratio := os.Getenv("OTEL_TRACES_SAMPLER_RATIO"); ratio != "" {
		if parsed, err := strconv.ParseFloat(ratio, 64); err == nil {
			cfg.SampleRatio = parsed
		}
	}

	return cfg
}

// ResourceAttributes builds the standard resource attributes used by OTel providers.
func (c Config) ResourceAttributes() []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("service.name", c.ServiceName),
		attribute.String("service.version", c.ServiceVersion),
		attribute.String("deployment.environment", c.Environment),
		attribute.String("telemetry.sdk.language", "go"),
		attribute.String("architecture", runtime.GOARCH),
	}
	return attrs
}

func envOrDefault(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
