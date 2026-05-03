package service

import (
	"context"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/model"
)

type URLService interface {
	ShortenURL(ctx context.Context, originalURL, userAgent string) (*model.URL, error)
	Redirect(ctx context.Context, key string) (string, error)
}
