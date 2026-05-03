// Package leader provides single-leader gating across replicas backed by
// Postgres advisory locks. Use it from a cron tick or any periodic task that
// must run on exactly one replica per fire — see Locker.Run.
package leader

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Locker awards a session-scoped Postgres advisory lock to one caller at a
// time. It owns no state beyond the connection pool; multiple Lockers built
// against the same pool are equivalent.
type Locker struct {
	pool *pgxpool.Pool
}

// New builds a Locker over the given pool. The pool must be the primary
// (writer) — replicas can't take advisory locks against the primary's state
// because they're a different physical database.
func New(pool *pgxpool.Pool) *Locker {
	return &Locker{pool: pool}
}

// Run acquires a session-scoped advisory lock keyed by `key`, runs `fn`, and
// releases the lock when fn returns. If another process already holds the
// lock, Run returns nil immediately — fn is not invoked, and the caller
// treats the work as "covered by someone else this tick."
//
// The lock is bound to a single connection that's held for fn's lifetime.
// If this process dies (panic, kill -9, network drop), the connection
// closes and Postgres releases the lock automatically — no stuck-leader
// scenario.
//
// Run propagates errors from fn unchanged. Lock acquisition / release
// failures are returned wrapped so callers can distinguish.
func (l *Locker) Run(ctx context.Context, key int64, fn func(context.Context) error) error {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire lock conn: %w", err)
	}
	defer conn.Release()

	var got bool
	if err := conn.QueryRow(ctx,
		"SELECT pg_try_advisory_lock($1)", key).Scan(&got); err != nil {
		return fmt.Errorf("try advisory lock: %w", err)
	}
	if !got {
		slog.DebugContext(ctx, "leader: lock held by another process, skipping",
			slog.Int64("key", key))
		return nil
	}

	slog.DebugContext(ctx, "leader: lock acquired", slog.Int64("key", key))
	defer func() {
		// Use context.Background here so a canceled `ctx` (e.g., fn timed
		// out and we want to clean up anyway) doesn't prevent the unlock.
		// Even if the unlock errors, the lock auto-releases when the
		// connection closes via conn.Release().
		if _, err := conn.Exec(context.Background(),
			"SELECT pg_advisory_unlock($1)", key); err != nil {
			slog.Error("leader: unlock failed (will auto-release on session end)",
				slog.Int64("key", key),
				slog.String("err", err.Error()))
		}
	}()

	return fn(ctx)
}
