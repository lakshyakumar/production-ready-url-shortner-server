// Package observability owns the global tracer and metrics setup. The rest
// of the codebase reads through otel.Tracer / otel.GetTracerProvider and
// prometheus.Registry; this package is the only place that decides what's
// behind those globals.
package observability

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace/noop"
)

// TracingConfig is the subset of config the tracer needs. Kept narrow so this
// package doesn't import the global config and bind everyone who imports it
// to viper.
type TracingConfig struct {
	// ExporterEndpoint is the OTLP HTTP endpoint URL — e.g.
	// "http://localhost:4318" or "https://otel.example.com:4318/v1/traces".
	// When empty, tracing is disabled and a no-op tracer provider is
	// installed so otel.Tracer(...) calls remain safe.
	ExporterEndpoint string
	// Insecure forces the OTLP HTTP client to use plaintext. Default is to
	// derive it from the URL scheme (http vs https).
	Insecure       bool
	ServiceName    string
	ServiceVersion string
	// SampleRatio in [0,1]. 0 = drop everything, 1 = sample every span.
	// Anything outside that range is clamped.
	SampleRatio float64
}

// ShutdownFunc flushes buffered spans and releases exporter resources. Always
// returned (a no-op when tracing is disabled) so callers can defer it without
// a nil check.
type ShutdownFunc func(context.Context) error

// InitTracing installs the global TracerProvider and propagator. When
// cfg.ExporterEndpoint is empty the function returns a no-op provider plus a
// no-op shutdown — useful for tests and dev runs that don't want to stand up
// a collector.
func InitTracing(ctx context.Context, cfg TracingConfig) (ShutdownFunc, error) {
	// W3C propagator goes in either way so context still flows across
	// process boundaries even when this process isn't exporting spans.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if cfg.ExporterEndpoint == "" {
		otel.SetTracerProvider(noop.NewTracerProvider())
		return func(context.Context) error { return nil }, nil
	}

	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpointURL(cfg.ExporterEndpoint),
	}
	if cfg.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("otlp exporter: %w", err)
	}

	// NewSchemaless avoids conflicts with the SDK default resource's
	// schema URL when semconv versions drift between this code and the SDK.
	// resource.Default() supplies sdk/process metadata; we merge our service
	// identity on top.
	custom := resource.NewSchemaless(
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.ServiceVersion),
	)
	res, err := resource.Merge(resource.Default(), custom)
	if err != nil {
		return nil, fmt.Errorf("resource: %w", err)
	}

	ratio := cfg.SampleRatio
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(
			exporter,
			// Modest batch settings — we'd rather pay a slightly higher
			// flush rate than lose spans on a sudden shutdown.
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithMaxExportBatchSize(512),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}
