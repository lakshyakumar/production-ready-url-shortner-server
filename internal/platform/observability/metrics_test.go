package observability_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/platform/observability"
)

func TestNewRegistry_RegistersAppMetrics(t *testing.T) {
	t.Parallel()
	reg, m := observability.NewRegistry()
	if reg == nil {
		t.Fatal("registry is nil")
	}
	if m == nil {
		t.Fatal("AppMetrics is nil")
	}

	// Increment something so the series exists, then scrape /metrics and
	// check the names are exposed.
	m.HTTPRequestsTotal.WithLabelValues("GET", "/x", "200").Inc()
	m.RateLimitDecisions.WithLabelValues("allow").Inc()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	observability.Handler(reg).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"http_requests_total",
		"ratelimit_decisions_total",
		"go_goroutines",       // from GoCollector
		"process_cpu_seconds", // from ProcessCollector
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metric %q missing from /metrics output", want)
		}
	}
}
