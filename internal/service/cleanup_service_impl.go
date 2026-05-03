package service

import (
	"context"
	"log/slog"
	"time"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/platform/config"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/repository"
)

type CleanupServiceImpl struct {
	urlRepo repository.URLRepository
	cfg     *config.Config
}

func NewCleanupService(repo repository.URLRepository, cfg *config.Config) CleanupService {
	return &CleanupServiceImpl{
		urlRepo: repo,
		cfg:     cfg,
	}
}

func (s *CleanupServiceImpl) RunCleanup(ctx context.Context) error {
	cutoff := time.Now().Add(-s.cfg.URLUnusedThreshold)

	slog.InfoContext(ctx, "cleanup starting",
		slog.Time("cutoff", cutoff),
	)

	count, err := s.urlRepo.DeleteUnusedSince(ctx, cutoff)
	if err != nil {
		slog.ErrorContext(ctx, "RunCleanup: delete unused failed",
			slog.Time("cutoff", cutoff),
			slog.String("err", err.Error()),
		)
		return err
	}

	slog.InfoContext(ctx, "cleanup complete",
		slog.Int64("deactivated", count),
	)
	return nil
}
