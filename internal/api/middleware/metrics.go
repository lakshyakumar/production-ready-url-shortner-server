package middleware

import (
	"strconv"
	"time"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/platform/observability"

	"github.com/gin-gonic/gin"
)

// HTTPMetrics records request count and duration per (method, route, status).
// route is the Gin route template (`/:key`) — never the raw path — so high-
// cardinality URLs don't blow up the metrics database.
//
// For paths that don't match any route (404s), route is set to "unmatched"
// instead of the raw URL for the same reason.
func HTTPMetrics(m *observability.AppMetrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		status := strconv.Itoa(c.Writer.Status())
		labels := []string{c.Request.Method, route, status}

		m.HTTPRequestsTotal.WithLabelValues(labels...).Inc()
		m.HTTPRequestDuration.WithLabelValues(labels...).Observe(time.Since(start).Seconds())
	}
}
