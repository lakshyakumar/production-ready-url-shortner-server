package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/api/handlers"

	"github.com/gin-gonic/gin"
)

// Liveness has no DB dependency — it just reports that the process is alive,
// which is exactly what makes it the safe handler for k8s `livenessProbe`. We
// can hit it without standing up a pgxpool, so it's a true unit test.
//
// Readiness needs a real *pgxpool.Pool to call Ping on. Covering it without
// integration infrastructure would require introducing a pinger interface
// just for tests, which isn't worth the abstraction churn — the current
// integration suite (testcontainers Postgres) is the right home for that.
func TestLiveness_Returns200WithStatusAlive(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &handlers.HealthHandler{} // DB pool unused for Liveness
	r.GET("/liveness", h.Liveness)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/liveness", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"alive"`) {
		t.Errorf("body should report alive, got %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Errorf("body should be success envelope, got %s", rec.Body.String())
	}
}
