package database

import (
	"context"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(ctx context.Context, connString string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, err
	}

	// Production Pool Tuning
	config.MaxConns = 20
	config.MinConns = 5
	config.MaxConnIdleTime = 30 * time.Minute

	// otelpgx hooks every Query/Exec into the global TracerProvider. When
	// tracing is disabled (noop provider), this is essentially free; when
	// it's enabled, every DB call shows up as a child span of the inbound
	// HTTP request.
	config.ConnConfig.Tracer = otelpgx.NewTracer()

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}

	return pool, pool.Ping(ctx)
}
