package middleware

import (
	contextutil "github/lakshyakumar/production-ready-url-shortner-server/internal/contextUtil"

	"github.com/gin-gonic/gin"
)

func ContextProvider() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		ctx := contextutil.WithClientIP(c.Request.Context(), ip)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
