package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/api/middleware"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/platform/observability"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestHTTPMetrics_RecordsCountAndDuration(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	_, m := observability.NewRegistry()

	r := gin.New()
	r.Use(middleware.HTTPMetrics(m))
	r.GET("/things/:id", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/things/42", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status: got %d want 200", rec.Code)
		}
	}

	// Counter should reflect 3 requests, labeled by route template (not /things/42).
	got := testutil.ToFloat64(m.HTTPRequestsTotal.WithLabelValues("GET", "/things/:id", "200"))
	if got != 3 {
		t.Errorf("http_requests_total: got %v want 3", got)
	}

	// Histogram count for the same labels should also be 3.
	if c := testutil.CollectAndCount(m.HTTPRequestDuration); c == 0 {
		t.Error("http_request_duration_seconds was not collected")
	}
}

func TestHTTPMetrics_UnmatchedRoute_LabelledAsUnmatched(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	_, m := observability.NewRegistry()

	r := gin.New()
	r.Use(middleware.HTTPMetrics(m))
	// no routes registered → every request is a 404

	req := httptest.NewRequest(http.MethodGet, "/no-such-path", nil)
	r.ServeHTTP(httptest.NewRecorder(), req)

	got := testutil.ToFloat64(m.HTTPRequestsTotal.WithLabelValues("GET", "unmatched", "404"))
	if got != 1 {
		t.Errorf("expected unmatched-label counter to be 1, got %v", got)
	}
}
