package repository

import (
	"context"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/model"
	"time"
)

type URLRepository interface {
	Create(ctx context.Context, url *model.URL) error
	GetByShortKey(ctx context.Context, key string) (*model.URL, error)
	UpdateLastUsed(ctx context.Context, id string) error
	DeleteUnusedSince(ctx context.Context, cutoff time.Time) (int64, error)
}
