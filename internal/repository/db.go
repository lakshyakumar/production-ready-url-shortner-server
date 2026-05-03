package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DBTX is the minimum surface every repository needs from pgx. Both
// *pgxpool.Pool and pgx.Tx satisfy it, which lets tests run inside a
// rolled-back transaction without touching production code paths.
//
// Convention borrowed from sqlc-generated code; widely used in pgx codebases.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
