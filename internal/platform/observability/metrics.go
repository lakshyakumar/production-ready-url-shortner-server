package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// AppMetrics is the bundle of custom counters/histograms the application
// emits directly. Standard runtime + process metrics come from the registry's
// built-in collectors, not from this struct.
type AppMetrics struct {
	// HTTPRequestsTotal counts every request that traversed the HTTP RED
	// middleware. Cardinality is bounded by route template — never raw URL.
	HTTPRequestsTotal *prometheus.CounterVec
	// HTTPRequestDuration is a histogram of request latencies in seconds,
	// labeled the same as HTTPRequestsTotal so quantile + RED views can
	// be built off the same series.
	HTTPRequestDuration *prometheus.HistogramVec
	// RateLimitDecisions counts each rate-limit middleware decision so we
	// can alert on a sudden spike in denials or fail-open errors.
	RateLimitDecisions *prometheus.CounterVec
}

// NewRegistry returns a fresh registry with Go runtime and process collectors
// already registered, plus the AppMetrics bundle. We don't use the global
// prometheus.DefaultRegisterer — keeping our own makes it trivial to swap in
// a fresh registry for tests.
func NewRegistry() (*prometheus.Registry, *AppMetrics) {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	m := &AppMetrics{
		HTTPRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Total HTTP requests processed, labeled by method, route template and status.",
			},
			[]string{"method", "route", "status"},
		),
		HTTPRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_duration_seconds",
				Help:    "HTTP request latency in seconds.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "route", "status"},
		),
		RateLimitDecisions: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ratelimit_decisions_total",
				Help: "Rate limit middleware decisions: allow, deny, or error (fail-open).",
			},
			[]string{"decision"},
		),
	}
	reg.MustRegister(m.HTTPRequestsTotal, m.HTTPRequestDuration, m.RateLimitDecisions)
	return reg, m
}

// Handler returns the http.Handler that serves /metrics for the supplied
// registry. Wired into the debug server in main.go.
func Handler(reg *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		Registry:          reg,
		EnableOpenMetrics: true,
	})
}
