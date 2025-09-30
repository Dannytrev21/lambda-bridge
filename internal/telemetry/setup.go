package telemetry

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

// Providers groups the initialized telemetry providers and shutdown function.
type Providers struct {
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
	Shutdown       func(context.Context) error
}

// Setup configures the global telemetry providers based on the supplied config.
func Setup(ctx context.Context, cfg Config) (Providers, error) {
	var providers Providers

	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		cfg.ResourceAttributes()...,
	))
	if err != nil {
		return providers, fmt.Errorf("telemetry resource: %w", err)
	}

	var tp *sdktrace.TracerProvider
	if cfg.TracingEnabled {
		exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpoint(cfgEndpoint(cfg.OTLPTraceEndpoint)), otlptracehttp.WithInsecure())
		if err != nil {
			return providers, fmt.Errorf("otel trace exporter: %w", err)
		}

		tp = sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exporter),
			sdktrace.WithResource(res),
			sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.SampleRatio)),
		)

		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))
	} else {
		otel.SetTracerProvider(trace.NewNoopTracerProvider())
	}

	var mp *sdkmetric.MeterProvider
	if cfg.MetricsEnabled {
		mp = sdkmetric.NewMeterProvider(sdkmetric.WithResource(res))
		otel.SetMeterProvider(mp)
	} else {
		otel.SetMeterProvider(noop.NewMeterProvider())
	}

	providers = Providers{
		TracerProvider: tp,
		MeterProvider:  mp,
		Shutdown: func(ctx context.Context) error {
			var err error
			if tp != nil {
				err = errors.Join(err, tp.Shutdown(ctx))
			}
			if mp != nil {
				err = errors.Join(err, mp.Shutdown(ctx))
			}
			return err
		},
	}

	return providers, nil
}

func cfgEndpoint(value string) string {
	if value != "" {
		return value
	}
	return "localhost:4318"
}
