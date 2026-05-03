package api

import (
	_ "github/lakshyakumar/production-ready-url-shortner-server/docs"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/api/handlers"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/api/middleware"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/platform/observability"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/service"

	"github.com/gin-gonic/gin"
	swaggerfiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// rateLimitSkipPrefixes are paths the rate limiter never sees. Health probes
// run on every replica from a load balancer or k8s — counting them would
// burn quota; swagger is internal docs.
var rateLimitSkipPrefixes = []string{"/liveness", "/readiness", "/swagger"}

// RouterDeps groups everything SetupRouter needs. Optional fields can be nil
// (e.g. RateLimiter when no Redis, Metrics when observability is off in
// tests) — the router degrades gracefully.
type RouterDeps struct {
	URL         *handlers.URLHandler
	Health      *handlers.HealthHandler
	RateLimiter service.RateLimiter
	Metrics     *observability.AppMetrics
	ServiceName string
}

func SetupRouter(d RouterDeps) *gin.Engine {
	r := gin.Default()

	// Order matters here. otelgin runs first so every span covers the rest
	// of the chain (rate limiter, handler). HTTPMetrics is next so the
	// recorded latency includes everything the user sees, including any
	// 429 from the limiter.
	r.Use(otelgin.Middleware(d.ServiceName))
	if d.Metrics != nil {
		r.Use(middleware.HTTPMetrics(d.Metrics))
	}
	r.Use(middleware.ContextProvider())
	if d.RateLimiter != nil {
		r.Use(middleware.RateLimit(d.RateLimiter, rateLimitSkipPrefixes, d.Metrics))
	}

	// Health Checks
	r.GET("/liveness", d.Health.Liveness)
	r.GET("/readiness", d.Health.Readiness)

	// API Routes
	r.POST("/shorten", d.URL.Create)
	r.GET("/:key", d.URL.Redirect)

	// Swagger UI
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerfiles.Handler))

	return r
}
