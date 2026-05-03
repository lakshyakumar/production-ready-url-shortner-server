package middleware

import (
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/api/response"
	contextutil "github/lakshyakumar/production-ready-url-shortner-server/internal/contextUtil"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/platform/observability"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/service"

	"github.com/gin-gonic/gin"
)

// RateLimit returns a Gin middleware that consults the supplied limiter on
// every request. Requests whose path starts with any prefix in skipPrefixes
// (e.g. health probes, swagger) bypass the limiter entirely.
//
// The middleware fails open: if the limiter returns an error (Redis down,
// timeout, etc.) the request is allowed and a warning is logged. Availability
// of the API is more valuable than strict rate limiting during a Redis blip.
//
// metrics is optional — pass nil to disable decision counters (useful in
// tests that don't want to wire a registry).
func RateLimit(limiter service.RateLimiter, skipPrefixes []string, metrics *observability.AppMetrics) gin.HandlerFunc {
	record := func(decision string) {
		if metrics != nil {
			metrics.RateLimitDecisions.WithLabelValues(decision).Inc()
		}
	}

	return func(c *gin.Context) {
		path := c.Request.URL.Path
		for _, prefix := range skipPrefixes {
			if strings.HasPrefix(path, prefix) {
				c.Next()
				return
			}
		}

		ctx := c.Request.Context()
		ip := contextutil.GetClientIP(ctx)

		allowed, remaining, retryAfter, err := limiter.Allow(ctx, ip)
		if err != nil {
			slog.WarnContext(ctx, "rate limiter unavailable, allowing request",
				slog.String("ip", ip),
				slog.String("err", err.Error()),
			)
			record("error")
			c.Next()
			return
		}

		c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))

		if !allowed {
			// Round up so a 1.4s wait doesn't get rendered as "1" and have
			// the client retry too soon.
			retrySec := int(math.Ceil(retryAfter.Seconds()))
			if retrySec < 1 {
				retrySec = 1
			}
			c.Header("Retry-After", strconv.Itoa(retrySec))
			response.Error(c, http.StatusTooManyRequests, response.CodeRateLimited, "rate limit exceeded")
			record("deny")
			c.Abort()
			return
		}

		record("allow")
		c.Next()
	}
}
