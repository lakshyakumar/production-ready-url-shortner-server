package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/api/response"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

const readinessTimeout = 2 * time.Second

type HealthHandler struct {
	DB *pgxpool.Pool
}

// Liveness godoc
// @Summary      Liveness probe
// @Description  Returns 200 if the process is alive. Used by orchestrators to detect a crashed process.
// @Tags         health
// @Produce      json
// @Success      200  {object}  response.Envelope
// @Router       /liveness [get]
func (h *HealthHandler) Liveness(c *gin.Context) {
	response.OK(c, http.StatusOK, gin.H{"status": "alive"})
}

// Readiness godoc
// @Summary      Readiness probe
// @Description  Pings the database with a 2-second timeout. Returns 503 if the database is unreachable.
// @Tags         health
// @Produce      json
// @Success      200  {object}  response.Envelope
// @Failure      503  {object}  response.Envelope
// @Failure      504  {object}  response.Envelope
// @Router       /readiness [get]
func (h *HealthHandler) Readiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), readinessTimeout)
	defer cancel()

	if err := h.DB.Ping(ctx); err != nil {
		if handleContextErr(c, err) {
			slog.WarnContext(ctx, "Readiness: aborted",
				slog.String("err", err.Error()),
			)
			return
		}
		slog.ErrorContext(ctx, "Readiness: ping failed",
			slog.String("err", err.Error()),
		)
		response.Error(c, http.StatusServiceUnavailable, response.CodeUnavailable, "database unreachable")
		return
	}

	response.OK(c, http.StatusOK, gin.H{"status": "ready"})
}
