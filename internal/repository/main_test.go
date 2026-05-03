// Package repository_test runs the repository integration suite against a
// throwaway PostgreSQL container.
//
// Lifecycle:
//   - TestMain spins up a fresh `postgres:16-alpine` container, applies every
//     migration in ./migrations, and exposes a *pgxpool.Pool as `testPool`.
//   - Each test calls withTx(t) to grab a transaction; t.Cleanup rolls it back,
//     so cases never share state and can run with t.Parallel().
//
// Requirements: a running Docker daemon. No DATABASE_URL, no .env.test, no
// pre-applied schema — testcontainers handles all of it.
package repository_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/repository"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var testPool *pgxpool.Pool

// TestMain is a thin wrapper around testRun so deferred cleanup actually
// runs — calling os.Exit directly inside testRun would skip every defer.
func TestMain(m *testing.M) {
	os.Exit(testRun(m))
}

func testRun(m *testing.M) int {
	// Skip silently when Docker isn't available so this doesn't break the
	// default `go test ./...` flow on machines without it.
	if os.Getenv("CI") == "" && !dockerAvailable() {
		fmt.Fprintln(os.Stderr, "[repository_test] docker not running — skipping integration tests")
		return 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	container, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("urlshortner_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[repository_test] container start: %v\n", err)
		return 1
	}
	defer func() {
		if termErr := container.Terminate(context.Background()); termErr != nil {
			fmt.Fprintf(os.Stderr, "[repository_test] container terminate: %v\n", termErr)
		}
	}()

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[repository_test] connection string: %v\n", err)
		return 1
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[repository_test] pool init: %v\n", err)
		return 1
	}
	defer pool.Close()

	if err := applyMigrations(ctx, pool); err != nil {
		fmt.Fprintf(os.Stderr, "[repository_test] migrations: %v\n", err)
		return 1
	}

	testPool = pool
	return m.Run()
}

// withTx returns a transaction whose Rollback is registered with t.Cleanup,
// so every test gets a fresh slate without truncating tables. The returned
// pgx.Tx satisfies repository.DBTX, so repos can be constructed against it
// directly. Tests using withTx are safe to mark t.Parallel().
func withTx(t *testing.T) (context.Context, pgx.Tx) {
	t.Helper()
	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() {
		// Rollback is a no-op if the tx already committed or aborted; we
		// never commit, so this always wins. context.Background ensures the
		// rollback isn't itself canceled by test cleanup.
		_ = tx.Rollback(context.Background())
	})
	return ctx, tx
}

// txRouter implements repository.Router by serving the same underlying
// DBTX (a pgx.Tx, in our test setup) for both reads and writes. It carries
// no breaker — repository tests aren't testing breaker behavior, just SQL.
type txRouter struct{ db repository.DBTX }

func (r txRouter) Reader() repository.DBTX { return r.db }
func (r txRouter) Writer() repository.DBTX { return r.db }

// singlePoolRouter wraps a DBTX as a Router that serves both sides from the
// same underlying handle. Convenient for repo integration tests where the
// tx is the unit of isolation.
func singlePoolRouter(db repository.DBTX) repository.Router {
	return txRouter{db: db}
}

// applyMigrations executes every *.up.sql in lexical order. Plain glob+exec
// keeps the test binary free of the golang-migrate library — the SQL is
// already idempotent enough for our needs (CREATE TABLE IF NOT EXISTS, etc.).
func applyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return errors.New("runtime.Caller failed — cannot locate migrations dir")
	}
	migrationDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")

	files, err := filepath.Glob(filepath.Join(migrationDir, "*.up.sql"))
	if err != nil {
		return fmt.Errorf("glob: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no migrations found in %s", migrationDir)
	}
	sort.Strings(files)

	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read %s: %w", filepath.Base(f), err)
		}
		if _, err := pool.Exec(ctx, string(b)); err != nil {
			return fmt.Errorf("apply %s: %w", filepath.Base(f), err)
		}
	}
	return nil
}

func dockerAvailable() bool {
	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return false
	}
	defer provider.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return provider.Health(ctx) == nil
}

// seedURL inserts a urls row directly through the supplied DBTX so URL-repo
// tests don't have to trust Create() to set up their preconditions.
func seedURL(t *testing.T, db repository.DBTX, shortKey string, lastUsed time.Time) seededURL {
	t.Helper()
	now := time.Now().UTC()
	id := newUUID(t, db)
	originalURL := "https://example.com/" + shortKey
	_, err := db.Exec(context.Background(),
		`INSERT INTO urls (id, created_at, updated_at, original_url, short_key, is_active, last_used_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		id, now, now, originalURL, shortKey, true, lastUsed.UTC())
	if err != nil {
		t.Fatalf("seedURL: %v", err)
	}
	return seededURL{ID: id, ShortKey: shortKey, OriginalURL: originalURL}
}

type seededURL struct {
	ID          string
	ShortKey    string
	OriginalURL string
}

// newUUID asks Postgres for a uuid so callers don't need to import gofrs/uuid
// just for seed helpers. Cheap one-row query — fine in tests.
func newUUID(t *testing.T, db repository.DBTX) string {
	t.Helper()
	var id string
	err := db.QueryRow(context.Background(), "SELECT gen_random_uuid()::text").Scan(&id)
	if err != nil {
		t.Fatalf("gen_random_uuid: %v", err)
	}
	return id
}
