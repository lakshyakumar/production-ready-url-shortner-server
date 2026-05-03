package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/api/middleware"

	"github.com/gin-gonic/gin"
)

// fakeLimiter implements service.RateLimiter with per-method function fields
// so each test can shape the response it cares about. Mirrors the fake-repo
// pattern used in the service tests.
type fakeLimiter struct {
	AllowFn    func(ctx context.Context, key string) (bool, int, time.Duration, error)
	AllowCalls atomic.Int32
}

func (f *fakeLimiter) Allow(ctx context.Context, key string) (bool, int, time.Duration, error) {
	f.AllowCalls.Add(1)
	if f.AllowFn != nil {
		return f.AllowFn(ctx, key)
	}
	return true, 99, 0, nil
}

// newRouter returns a bare gin engine with the rate-limit middleware
// installed and a couple of trivial handlers behind it.
func newRouter(t *testing.T, lim *fakeLimiter, skip []string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RateLimit(lim, skip, nil))
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })
	r.GET("/liveness", func(c *gin.Context) { c.String(http.StatusOK, "alive") })
	r.GET("/swagger/index.html", func(c *gin.Context) { c.String(http.StatusOK, "docs") })
	return r
}

func do(r http.Handler, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestRateLimit_Allowed_PassesThrough(t *testing.T) {
	t.Parallel()
	lim := &fakeLimiter{
		AllowFn: func(ctx context.Context, key string) (bool, int, time.Duration, error) {
			return true, 42, 0, nil
		},
	}
	r := newRouter(t, lim, nil)

	rec := do(r, http.MethodGet, "/ping")

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d want 200", rec.Code)
	}
	if got := rec.Header().Get("X-RateLimit-Remaining"); got != "42" {
		t.Errorf("X-RateLimit-Remaining: got %q want %q", got, "42")
	}
	if lim.AllowCalls.Load() != 1 {
		t.Errorf("Allow calls: got %d want 1", lim.AllowCalls.Load())
	}
}

func TestRateLimit_Denied_Returns429WithRetryAfter(t *testing.T) {
	t.Parallel()
	lim := &fakeLimiter{
		AllowFn: func(ctx context.Context, key string) (bool, int, time.Duration, error) {
			return false, 0, 7 * time.Second, nil
		},
	}
	r := newRouter(t, lim, nil)

	rec := do(r, http.MethodGet, "/ping")

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status: got %d want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "7" {
		t.Errorf("Retry-After: got %q want %q", got, "7")
	}
	if !strings.Contains(rec.Body.String(), `"code":"rate_limited"`) {
		t.Errorf("body should contain rate_limited code, got %s", rec.Body.String())
	}
}

func TestRateLimit_LimiterError_FailsOpen(t *testing.T) {
	t.Parallel()
	lim := &fakeLimiter{
		AllowFn: func(ctx context.Context, key string) (bool, int, time.Duration, error) {
			return false, 0, 0, errors.New("redis unreachable")
		},
	}
	r := newRouter(t, lim, nil)

	rec := do(r, http.MethodGet, "/ping")

	if rec.Code != http.StatusOK {
		t.Errorf("expected fail-open 200 on limiter error, got %d", rec.Code)
	}
	if rec.Body.String() != "pong" {
		t.Errorf("handler should still run on fail-open, body=%q", rec.Body.String())
	}
}

func TestRateLimit_SkipPrefixes_BypassLimiter(t *testing.T) {
	t.Parallel()
	lim := &fakeLimiter{
		// If this is ever invoked the test should fail — set up a deny so
		// the 200 below is unambiguous proof the limiter was skipped.
		AllowFn: func(ctx context.Context, key string) (bool, int, time.Duration, error) {
			return false, 0, time.Minute, nil
		},
	}
	skip := []string{"/liveness", "/readiness", "/swagger"}
	r := newRouter(t, lim, skip)

	for _, path := range []string{"/liveness", "/swagger/index.html"} {
		rec := do(r, http.MethodGet, path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: expected bypass 200, got %d", path, rec.Code)
		}
	}
	if lim.AllowCalls.Load() != 0 {
		t.Errorf("limiter should not be invoked for skipped paths, got %d calls", lim.AllowCalls.Load())
	}

	// Sanity: a non-skipped path still goes through the limiter (and gets denied here).
	rec := do(r, http.MethodGet, "/ping")
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("/ping should still be limited: got %d want 429", rec.Code)
	}
}
