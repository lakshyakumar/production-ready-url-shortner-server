package cron

import (
	"context"
	"log/slog"
	"time"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/platform/config"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/service"
)

// cleanupLockKey gates the cleanup cron across replicas via Postgres
// advisory locks. Pick once, never change — changing it lets two replicas
// run cleanup concurrently during a deploy that mixes old + new keys.
const cleanupLockKey = int64(0xC1EA_F00D_C0DE)

// Locker is the minimum surface CleanupCron needs from a leader gate. The
// production implementation is *leader.Locker, which uses Postgres advisory
// locks; tests pass a fake. Declaring the interface here (consumer side)
// keeps the cron package free of a leader-package import.
type Locker interface {
	Run(ctx context.Context, key int64, fn func(context.Context) error) error
}

type CleanupCron struct {
	svc    service.CleanupService
	cfg    *config.Config
	locker Locker
}

// NewCleanupCron wires the cron with a Locker so only one replica per tick
// actually performs cleanup. The locker is required — pass leader.New(pool)
// from main.go even in single-replica deployments (it adds ~one DB round
// trip per tick and keeps the contract uniform).
func NewCleanupCron(svc service.CleanupService, cfg *config.Config, locker Locker) *CleanupCron {
	return &CleanupCron{
		svc:    svc,
		cfg:    cfg,
		locker: locker,
	}
}

func (c *CleanupCron) Start(ctx context.Context) {
	ticker := time.NewTicker(c.cfg.CleanUpInterval)
	defer ticker.Stop()

	slog.InfoContext(ctx, "cleanup cron started",
		slog.Duration("interval", c.cfg.CleanUpInterval),
	)

	for {
		select {
		case <-ticker.C:
			// locker.Run is a no-op when another replica already holds the
			// lock — that's the point. Errors here are infra-level (DB
			// unreachable, lock-acquire failure), not the cleanup itself.
			if err := c.locker.Run(ctx, cleanupLockKey, c.svc.RunCleanup); err != nil {
				slog.ErrorContext(ctx, "cleanup tick failed",
					slog.String("err", err.Error()),
				)
			}
		case <-ctx.Done():
			slog.InfoContext(ctx, "cleanup cron stopping")
			return
		}
	}
}
