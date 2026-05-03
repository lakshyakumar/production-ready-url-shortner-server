package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/api/response"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/repository"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/service"

	"github.com/gin-gonic/gin"
)

// Per-endpoint SLAs. Tighter than any single DB-call timeout in the repo so
// the handler is the binding deadline for the request as a whole.
const (
	createTimeout   = 10 * time.Second
	redirectTimeout = 3 * time.Second
)

type URLHandler struct {
	Service service.URLService
}

type ShortenRequest struct {
	URL string `json:"url" binding:"required,url" example:"https://example.com/very/long/path"`
}

// Create godoc
// @Summary      Create a short URL
// @Description  Accepts a long URL and returns a shortened version with a generated short key.
// @Tags         urls
// @Accept       json
// @Produce      json
// @Param        request  body      ShortenRequest      true  "URL to shorten"
// @Success      201      {object}  response.Envelope
// @Failure      400      {object}  response.Envelope
// @Failure      500      {object}  response.Envelope
// @Failure      504      {object}  response.Envelope
// @Router       /shorten [post]
func (h *URLHandler) Create(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), createTimeout)
	defer cancel()

	var input ShortenRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		slog.WarnContext(ctx, "Create: bind json failed",
			slog.String("err", err.Error()),
		)
		response.Error(c, http.StatusBadRequest, response.CodeInvalidInput, err.Error())
		return
	}

	res, err := h.Service.ShortenURL(ctx, input.URL, c.Request.UserAgent())
	if err != nil {
		if handleContextErr(c, err) {
			slog.WarnContext(ctx, "Create: aborted",
				slog.String("url", input.URL),
				slog.String("err", err.Error()),
			)
			return
		}
		slog.ErrorContext(ctx, "Create: shorten url failed",
			slog.String("url", input.URL),
			slog.String("err", err.Error()),
		)
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "failed to create short url")
		return
	}
	response.OK(c, http.StatusCreated, res)
}

// Redirect godoc
// @Summary      Redirect to original URL
// @Description  Looks up the short key and issues a 301 redirect to the original URL.
// @Tags         urls
// @Produce      json
// @Param        key  path      string  true  "Short key"
// @Success      301  "Moved Permanently"
// @Failure      404  {object}  response.Envelope
// @Failure      500  {object}  response.Envelope
// @Failure      504  {object}  response.Envelope
// @Router       /{key} [get]
func (h *URLHandler) Redirect(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), redirectTimeout)
	defer cancel()

	key := c.Param("key")
	original, err := h.Service.Redirect(ctx, key)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			response.Error(c, http.StatusNotFound, response.CodeNotFound, "short url not found")
			return
		}
		if handleContextErr(c, err) {
			slog.WarnContext(ctx, "Redirect: aborted",
				slog.String("short_key", key),
				slog.String("err", err.Error()),
			)
			return
		}
		slog.ErrorContext(ctx, "Redirect: resolve failed",
			slog.String("short_key", key),
			slog.String("err", err.Error()),
		)
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "failed to resolve short url")
		return
	}
	c.Redirect(http.StatusMovedPermanently, original)
}
