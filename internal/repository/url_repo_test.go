package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/model"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/repository"
)

func TestURLRepository_Create_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx, tx := withTx(t)
	repo := repository.NewURLRepository(singlePoolRouter(tx))

	u := model.NewURL("https://example.com/round-trip", "round_trip1")
	u.LastUsedAt = time.Now().UTC()

	if err := repo.Create(ctx, &u); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByShortKey(ctx, u.ShortKey)
	if err != nil {
		t.Fatalf("GetByShortKey: %v", err)
	}
	if got.URL != u.URL {
		t.Errorf("URL mismatch: got %q want %q", got.URL, u.URL)
	}
	if got.ShortKey != u.ShortKey {
		t.Errorf("ShortKey mismatch: got %q want %q", got.ShortKey, u.ShortKey)
	}
	if !got.IsActive {
		t.Errorf("IsActive: got false, want true (default)")
	}
}

func TestURLRepository_GetByShortKey_NotFound(t *testing.T) {
	t.Parallel()
	ctx, tx := withTx(t)
	repo := repository.NewURLRepository(singlePoolRouter(tx))

	_, err := repo.GetByShortKey(ctx, "definitely_missing")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestURLRepository_GetByShortKey_HidesSoftDeleted(t *testing.T) {
	t.Parallel()
	ctx, tx := withTx(t)
	repo := repository.NewURLRepository(singlePoolRouter(tx))

	u := seedURL(t, tx, "soft_del001", time.Now().UTC())
	if _, err := tx.Exec(ctx,
		"UPDATE urls SET deleted_at = NOW() WHERE id = $1", u.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	if _, err := repo.GetByShortKey(ctx, u.ShortKey); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for soft-deleted row, got %v", err)
	}
}

func TestURLRepository_UpdateLastUsed_BumpsTimestamp(t *testing.T) {
	t.Parallel()
	ctx, tx := withTx(t)
	repo := repository.NewURLRepository(singlePoolRouter(tx))

	old := time.Now().Add(-1 * time.Hour).UTC()
	u := seedURL(t, tx, "bump_001", old)

	if err := repo.UpdateLastUsed(ctx, u.ID); err != nil {
		t.Fatalf("UpdateLastUsed: %v", err)
	}

	got, err := repo.GetByShortKey(ctx, u.ShortKey)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if !got.LastUsedAt.After(old) {
		t.Fatalf("LastUsedAt not bumped: was %v, now %v", old, got.LastUsedAt)
	}
}

func TestURLRepository_UpdateLastUsed_UnknownIDIsNoOp(t *testing.T) {
	t.Parallel()
	ctx, tx := withTx(t)
	repo := repository.NewURLRepository(singlePoolRouter(tx))

	if err := repo.UpdateLastUsed(ctx, "00000000-0000-0000-0000-000000000000"); err != nil {
		t.Fatalf("UpdateLastUsed of unknown id: %v", err)
	}
}

func TestURLRepository_DeleteUnusedSince_DeactivatesOldOnly(t *testing.T) {
	t.Parallel()
	ctx, tx := withTx(t)
	repo := repository.NewURLRepository(singlePoolRouter(tx))

	old := seedURL(t, tx, "old_001", time.Now().Add(-48*time.Hour).UTC())
	fresh := seedURL(t, tx, "fresh_001", time.Now().UTC())

	cutoff := time.Now().Add(-24 * time.Hour)
	affected, err := repo.DeleteUnusedSince(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteUnusedSince: %v", err)
	}
	if affected != 1 {
		t.Fatalf("rows affected: got %d want 1", affected)
	}

	if _, err := repo.GetByShortKey(ctx, old.ShortKey); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("old URL should be hidden, got %v", err)
	}
	if _, err := repo.GetByShortKey(ctx, fresh.ShortKey); err != nil {
		t.Errorf("fresh URL should still be visible, got %v", err)
	}
}

func TestURLRepository_DeleteUnusedSince_IdempotentForAlreadyDeleted(t *testing.T) {
	t.Parallel()
	ctx, tx := withTx(t)
	repo := repository.NewURLRepository(singlePoolRouter(tx))

	seedURL(t, tx, "stale_001", time.Now().Add(-48*time.Hour).UTC())
	cutoff := time.Now().Add(-24 * time.Hour)

	first, err := repo.DeleteUnusedSince(ctx, cutoff)
	if err != nil || first != 1 {
		t.Fatalf("first run: affected=%d err=%v", first, err)
	}

	second, err := repo.DeleteUnusedSince(ctx, cutoff)
	if err != nil {
		t.Fatalf("second run err: %v", err)
	}
	if second != 0 {
		t.Fatalf("second run affected: got %d want 0", second)
	}
}

func TestURLRepository_RespectsCancelledContext(t *testing.T) {
	t.Parallel()
	_, tx := withTx(t)
	repo := repository.NewURLRepository(singlePoolRouter(tx))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := repo.GetByShortKey(ctx, "anything"); !errors.Is(err, context.Canceled) {
		t.Errorf("GetByShortKey: expected context.Canceled, got %v", err)
	}
	u := model.NewURL("https://example.com/c", "ctx_test_001")
	if err := repo.Create(ctx, &u); !errors.Is(err, context.Canceled) {
		t.Errorf("Create: expected context.Canceled, got %v", err)
	}
	if err := repo.UpdateLastUsed(ctx, u.ID.String()); !errors.Is(err, context.Canceled) {
		t.Errorf("UpdateLastUsed: expected context.Canceled, got %v", err)
	}
	if _, err := repo.DeleteUnusedSince(ctx, time.Now()); !errors.Is(err, context.Canceled) {
		t.Errorf("DeleteUnusedSince: expected context.Canceled, got %v", err)
	}
}

// Note: ErrCircuitOpen is now produced by the breakerDB layer (see
// internal/platform/databse/router_test.go), not by a context flag at the
// repo. Repos no longer have a per-method circuit-breaker check.

// Foundation test: prove pgx forwards context deadlines to Postgres. Uses the
// raw pool (not a tx) because it doesn't write any data — pg_sleep is the
// whole point.
func TestPool_DeadlinePropagatesToPostgres(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := testPool.Exec(ctx, "SELECT pg_sleep(5)")
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v (elapsed %v)", err, elapsed)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("query ran for %v — cancellation did not reach Postgres", elapsed)
	}
}
