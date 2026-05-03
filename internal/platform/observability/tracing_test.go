package observability_test

import (
	"context"
	"testing"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/platform/observability"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestInitTracing_NoEndpoint_InstallsNoop(t *testing.T) {
	t.Parallel()
	shutdown, err := observability.InitTracing(context.Background(), observability.TracingConfig{
		ServiceName:    "test",
		ServiceVersion: "0.0.1",
	})
	if err != nil {
		t.Fatalf("InitTracing: %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown func should never be nil")
	}
	// Provider should be the noop implementation; the easy way to check is
	// that a freshly created tracer has the same type as a noop tracer.
	got := otel.GetTracerProvider().Tracer("x")
	want := noop.NewTracerProvider().Tracer("x")
	if got != want {
		// noop.Tracer is a value type and equal across instances; if the
		// provider is real these will differ.
		t.Errorf("expected noop tracer when endpoint empty; got %T", got)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("noop shutdown should never fail: %v", err)
	}
}

func TestInitTracing_RealEndpoint_ReturnsLiveProvider(t *testing.T) {
	t.Parallel()
	// Point at a guaranteed-dead address. The exporter is lazy — it builds
	// a client but doesn't dial until export time, so InitTracing should
	// still succeed. This is the realistic startup path: collector might
	// be down at boot but we don't want to crash the API for it.
	shutdown, err := observability.InitTracing(context.Background(), observability.TracingConfig{
		ExporterEndpoint: "http://127.0.0.1:1",
		Insecure:         true,
		ServiceName:      "test",
		ServiceVersion:   "0.0.1",
		SampleRatio:      1.0,
	})
	if err != nil {
		t.Fatalf("InitTracing: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	gotTracer := otel.GetTracerProvider().Tracer("x")
	noopTracer := noop.NewTracerProvider().Tracer("x")
	if gotTracer == noopTracer {
		t.Error("expected real tracer when endpoint set, got noop")
	}
}
