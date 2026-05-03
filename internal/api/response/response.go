package response

import (
	"github.com/gin-gonic/gin"
)

type Envelope struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *APIError   `json:"error,omitempty"`
}

type APIError struct {
	Code    string `json:"code"    example:"internal_error"`
	Message string `json:"message" example:"failed to create short url"`
}

const (
	CodeInvalidInput = "invalid_input"
	CodeNotFound     = "not_found"
	CodeUnavailable  = "unavailable"
	CodeInternal     = "internal_error"
	CodeTimeout      = "timeout"
	CodeRateLimited  = "rate_limited"
)

func OK(c *gin.Context, status int, data interface{}) {
	c.JSON(status, Envelope{Success: true, Data: data})
}

func Error(c *gin.Context, status int, code, message string) {
	c.JSON(status, Envelope{
		Success: false,
		Error:   &APIError{Code: code, Message: message},
	})
}
