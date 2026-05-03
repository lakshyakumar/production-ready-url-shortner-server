package handlers

import (
	"context"
	"errors"
	"net/http"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/api/response"

	"github.com/gin-gonic/gin"
)

// handleContextErr writes the right response for context-derived errors and
// returns true if it handled them. Callers should:
//
//	if err != nil {
//	    if handleContextErr(c, err) { return }
//	    // ... regular error handling
//	}
//
// Why two cases:
//   - DeadlineExceeded: our timeout fired → tell the client (504) so they can
//     retry or back off intelligently.
//   - Canceled: client disconnected before we finished → no point writing a
//     body, the socket is gone. We just stop work.
func handleContextErr(c *gin.Context, err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		response.Error(c, http.StatusGatewayTimeout, response.CodeTimeout, "request timed out")
		return true
	}
	if errors.Is(err, context.Canceled) {
		c.Abort()
		return true
	}
	return false
}
