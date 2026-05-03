package repository

import (
	"context"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/model"

	"github.com/google/uuid"
)

type RequestRepository interface {
	LogCreateRequest(ctx context.Context, urlId uuid.UUID, agent string) (*model.IncomingRequest, error)
}
