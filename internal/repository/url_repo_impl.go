package repository

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/model"

	"github.com/jackc/pgx/v5"
)

// Router is the abstraction over read/write pool selection. The repo never
// names a concrete pool — it just asks the router for the right side per
// call. See database.Router for the production implementation.
type Router interface {
	Reader() DBTX
	Writer() DBTX
}

type URLRepositoryImpl struct {
	router Router
}

func NewURLRepository(r Router) URLRepository {
	return &URLRepositoryImpl{router: r}
}

func (r *URLRepositoryImpl) Create(ctx context.Context, url *model.URL) error {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	query := `INSERT INTO urls (id, created_at, updated_at, original_url, short_key, is_active, last_used_at)
              VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := r.router.Writer().Exec(ctx, query, url.ID, url.CreatedAt, url.UpdatedAt, url.URL, url.ShortKey, url.IsActive, url.LastUsedAt)
	if err != nil {
		slog.ErrorContext(ctx, "urls.Create failed",
			slog.String("id", url.ID.String()),
			slog.String("short_key", url.ShortKey),
			slog.String("err", err.Error()),
		)
		return err
	}
	return nil
}

func (r *URLRepositoryImpl) GetByShortKey(ctx context.Context, key string) (*model.URL, error) {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	var u model.URL
	query := `SELECT id, original_url, short_key, is_active, last_used_at FROM urls WHERE short_key = $1 AND deleted_at IS NULL`
	err := r.router.Reader().QueryRow(ctx, query, key).Scan(&u.ID, &u.URL, &u.ShortKey, &u.IsActive, &u.LastUsedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		slog.ErrorContext(ctx, "urls.GetByShortKey failed",
			slog.String("short_key", key),
			slog.String("err", err.Error()),
		)
		return nil, err
	}
	return &u, nil
}

func (r *URLRepositoryImpl) UpdateLastUsed(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	query := `UPDATE urls SET last_used_at = $1 WHERE id = $2`
	_, err := r.router.Writer().Exec(ctx, query, time.Now().UTC(), id)
	if err != nil {
		slog.ErrorContext(ctx, "urls.UpdateLastUsed failed",
			slog.String("id", id),
			slog.String("err", err.Error()),
		)
		return err
	}
	return nil
}

func (r *URLRepositoryImpl) DeleteUnusedSince(ctx context.Context, cutoff time.Time) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	query := `UPDATE urls SET deleted_at = $1, is_active = false WHERE last_used_at < $2 AND deleted_at IS NULL`
	result, err := r.router.Writer().Exec(ctx, query, time.Now().UTC(), cutoff)
	if err != nil {
		slog.ErrorContext(ctx, "urls.DeleteUnusedSince failed",
			slog.Time("cutoff", cutoff),
			slog.String("err", err.Error()),
		)
		return 0, err
	}
	return result.RowsAffected(), nil
}
