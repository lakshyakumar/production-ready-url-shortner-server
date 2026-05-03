package database

import (
	"context"
	"errors"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/repository"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sony/gobreaker/v2"
)

// Router is the seam between repositories and database pools. A repository
// asks for the right side of the split per call:
//
//	r.router.Reader().QueryRow(ctx, ...)   // for SELECTs
//	r.router.Writer().Exec(ctx, ...)       // for INSERT/UPDATE/DELETE
//
// Implementations decide which underlying pool answers — typically replica
// for Reader (with primary fallback) and primary for Writer.
type Router interface {
	Reader() repository.DBTX
	Writer() repository.DBTX
}

// poolRouter is the production Router. It owns a primary pool's breakerDB
// and (optionally) a replica's. When the replica is absent, the primary
// breakerDB serves both Reader and Writer.
type poolRouter struct {
	primary *breakerDB
	replica *breakerDB // nil when no replica configured
}

// Reader prefers the replica when its breaker is closed; otherwise falls
// back to the primary. If the primary's breaker is also open, callers will
// get ErrCircuitOpen from the breakerDB on the next operation.
func (r *poolRouter) Reader() repository.DBTX {
	if r.replica != nil && r.replica.cb.State() != gobreaker.StateOpen {
		return r.replica
	}
	return r.primary
}

// Writer is always the primary. There is no write fallback.
func (r *poolRouter) Writer() repository.DBTX {
	return r.primary
}

// NewRouter constructs a production Router. primaryDSN is required;
// replicaDSN is optional — pass an empty string to run with a single pool
// where reads and writes share the primary.
//
// The returned *pgxpool.Pool is the primary pool, exposed for callers that
// need direct access (e.g. the health handler's Ping). Most code should
// route through the Router.
//
// Each pool gets its own breakerDB with default conservative settings
// (DefaultBreakerSettings). For test setups that need custom breaker
// timing, see NewRouterWithSettings.
func NewRouter(ctx context.Context, primaryDSN, replicaDSN string) (Router, *pgxpool.Pool, func(), error) {
	return NewRouterWithSettings(ctx, primaryDSN, replicaDSN,
		DefaultBreakerSettings("primary-db"),
		DefaultBreakerSettings("replica-db"),
	)
}

// NewRouterWithSettings is the same as NewRouter but lets callers override
// breaker tuning per pool. The cleanup function closes both pools.
func NewRouterWithSettings(
	ctx context.Context,
	primaryDSN, replicaDSN string,
	primarySettings, replicaSettings BreakerSettings,
) (Router, *pgxpool.Pool, func(), error) {
	primaryPool, err := NewPool(ctx, primaryDSN)
	if err != nil {
		return nil, nil, nil, err
	}

	r := &poolRouter{
		primary: newBreakerDB(primaryPool, primarySettings),
	}
	cleanup := func() { primaryPool.Close() }

	if replicaDSN != "" {
		replicaPool, err := NewPool(ctx, replicaDSN)
		if err != nil {
			primaryPool.Close()
			return nil, nil, nil, err
		}
		r.replica = newBreakerDB(replicaPool, replicaSettings)
		prevCleanup := cleanup
		cleanup = func() {
			prevCleanup()
			replicaPool.Close()
		}
	}

	return r, primaryPool, cleanup, nil
}

// breakerDB wraps a repository.DBTX with a circuit breaker. Every Exec/
// Query/QueryRow call goes through the breaker:
//   - if the breaker is open, return repository.ErrCircuitOpen synchronously
//     (no DB hit)
//   - if it's closed or half-open, run the call and report the outcome to
//     the breaker (via shouldCount classification)
//
// breakerDB takes DBTX rather than *pgxpool.Pool so tests can swap in a
// fake without standing up a second test container.
type breakerDB struct {
	db repository.DBTX
	cb *gobreaker.CircuitBreaker[any]
}

func newBreakerDB(db repository.DBTX, s BreakerSettings) *breakerDB {
	return &breakerDB{
		db: db,
		cb: newBreaker(s),
	}
}

// State returns the breaker's current state. Useful for tests and metrics.
func (b *breakerDB) State() gobreaker.State { return b.cb.State() }

func (b *breakerDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	out, err := b.cb.Execute(func() (any, error) {
		return b.db.Exec(ctx, sql, args...)
	})
	if err != nil {
		if isBreakerOpen(err) {
			return pgconn.CommandTag{}, repository.ErrCircuitOpen
		}
		return pgconn.CommandTag{}, err
	}
	return out.(pgconn.CommandTag), nil
}

func (b *breakerDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	out, err := b.cb.Execute(func() (any, error) {
		return b.db.Query(ctx, sql, args...)
	})
	if err != nil {
		if isBreakerOpen(err) {
			return nil, repository.ErrCircuitOpen
		}
		return nil, err
	}
	return out.(pgx.Rows), nil
}

func (b *breakerDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	// QueryRow doesn't return an error directly — it defers to Scan. To
	// participate in the breaker we run Query underneath and wrap the
	// resulting row (or breaker error) so Scan surfaces it.
	out, err := b.cb.Execute(func() (any, error) {
		return b.db.Query(ctx, sql, args...)
	})
	if err != nil {
		if isBreakerOpen(err) {
			return errRow{err: repository.ErrCircuitOpen}
		}
		return errRow{err: err}
	}
	rows := out.(pgx.Rows)
	return rowsToRow{rows: rows}
}

// isBreakerOpen reports whether err is one of gobreaker's "the breaker
// blocked the call" sentinel errors.
func isBreakerOpen(err error) bool {
	return errors.Is(err, gobreaker.ErrOpenState) ||
		errors.Is(err, gobreaker.ErrTooManyRequests)
}

// errRow is a pgx.Row whose Scan always returns the stored error.
// Used when the breaker rejected the QueryRow call before any DB hit.
type errRow struct{ err error }

func (r errRow) Scan(_ ...any) error { return r.err }

// rowsToRow adapts a pgx.Rows (returned by pool.Query) into a pgx.Row, so
// breakerDB.QueryRow can keep its expected return type. This mirrors what
// pgxpool.Pool.QueryRow does internally.
type rowsToRow struct{ rows pgx.Rows }

func (r rowsToRow) Scan(dest ...any) error {
	defer r.rows.Close()
	if !r.rows.Next() {
		if err := r.rows.Err(); err != nil {
			return err
		}
		return pgx.ErrNoRows
	}
	if err := r.rows.Scan(dest...); err != nil {
		return err
	}
	return r.rows.Err()
}
