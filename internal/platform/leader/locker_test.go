package leader_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/platform/leader"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Advisory locks don't require any application tables, so this TestMain is
// simpler than the repository suite's: spin a container, give us a pool,
// done. No migrations.
var testPool *pgxpool.Pool

// TestMain is a thin wrapper around testRun so deferred cleanup actually
// runs — calling os.Exit directly inside testRun would skip every defer.
func TestMain(m *testing.M) {
	os.Exit(testRun(m))
}

func testRun(m *testing.M) int {
	if !dockerAvailable() {
		fmt.Fprintln(os.Stderr, "[leader_test] docker not running — skipping integration tests")
		return 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	container, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("leader_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[leader_test] container start: %v\n", err)
		return 1
	}
	defer func() {
		if termErr := container.Terminate(context.Background()); termErr != nil {
			fmt.Fprintf(os.Stderr, "[leader_test] container terminate: %v\n", termErr)
		}
	}()

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[leader_test] connection string: %v\n", err)
		return 1
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[leader_test] pool init: %v\n", err)
		return 1
	}
	defer pool.Close()

	testPool = pool
	return m.Run()
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

// TestLocker_OnlyOneRunsPerKey races N goroutines on the same key with
// concurrent Run calls. Exactly one fn invocation must occur — the losers'
// Run calls return nil without invoking fn.
//
// This is the core correctness property that the cleanup cron relies on.
//
// Determinism: a naive sleep-based version is racy (a slow-to-schedule
// goroutine can hit pg_try_advisory_lock *after* the winner has released,
// picking up the lock and falsely incrementing the count). We instead
// hold the winner inside fn until every loser has *finished* its Run call,
// guaranteeing that no further Run can race with the winner's release.
func TestLocker_OnlyOneRunsPerKey(t *testing.T) {
	const key int64 = 0xA1
	const concurrency = 5

	l := leader.New(testPool)

	var invocations atomic.Int32
	winnerEntered := make(chan struct{}, 1)
	releaseWinner := make(chan struct{})
	loserDone := make(chan struct{}, concurrency-1)

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var fnRan bool
			err := l.Run(context.Background(), key, func(ctx context.Context) error {
				fnRan = true
				invocations.Add(1)
				winnerEntered <- struct{}{}
				<-releaseWinner
				return nil
			})
			if err != nil {
				t.Errorf("Run: %v", err)
			}
			if !fnRan {
				loserDone <- struct{}{}
			}
		}()
	}

	// Wait for the winner to enter fn …
	<-winnerEntered
	// … then wait for every loser to finish its Run (ErrCircuitOpen-free
	// path returning nil). Once all losers are accounted for, no goroutine
	// can still be in flight to race the winner's release.
	for i := 0; i < concurrency-1; i++ {
		<-loserDone
	}

	close(releaseWinner)
	wg.Wait()

	if got := invocations.Load(); got != 1 {
		t.Fatalf("fn invocations: got %d, want 1", got)
	}
}

func TestLocker_DifferentKeysDoNotInterfere(t *testing.T) {
	l := leader.New(testPool)

	var aRan, bRan bool
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = l.Run(context.Background(), 0xB1, func(ctx context.Context) error {
			aRan = true
			time.Sleep(20 * time.Millisecond)
			return nil
		})
	}()
	go func() {
		defer wg.Done()
		_ = l.Run(context.Background(), 0xB2, func(ctx context.Context) error {
			bRan = true
			time.Sleep(20 * time.Millisecond)
			return nil
		})
	}()
	wg.Wait()

	if !aRan || !bRan {
		t.Errorf("expected both keys to run; aRan=%v bRan=%v", aRan, bRan)
	}
}

func TestLocker_ReleasesAfterFn(t *testing.T) {
	const key int64 = 0xC1
	l := leader.New(testPool)

	if err := l.Run(context.Background(), key, func(ctx context.Context) error { return nil }); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	var ran bool
	if err := l.Run(context.Background(), key, func(ctx context.Context) error {
		ran = true
		return nil
	}); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if !ran {
		t.Fatal("second Run should have executed fn (lock should have been released)")
	}
}

func TestLocker_PropagatesFnError(t *testing.T) {
	const key int64 = 0xD1
	l := leader.New(testPool)

	want := errors.New("fn boom")
	got := l.Run(context.Background(), key, func(ctx context.Context) error { return want })
	if !errors.Is(got, want) {
		t.Errorf("Run: got %v, want %v", got, want)
	}

	// And the lock is still released after a failing fn.
	var ran bool
	if err := l.Run(context.Background(), key, func(ctx context.Context) error {
		ran = true
		return nil
	}); err != nil {
		t.Fatalf("follow-up Run: %v", err)
	}
	if !ran {
		t.Fatal("lock should have been released after fn returned an error")
	}
}

func TestLocker_LoserIsNoOp(t *testing.T) {
	const key int64 = 0xE1
	l := leader.New(testPool)

	// Hold the lock with a long-running fn in a goroutine.
	holding := make(chan struct{})
	releaseHolder := make(chan struct{})
	go func() {
		_ = l.Run(context.Background(), key, func(ctx context.Context) error {
			close(holding)
			<-releaseHolder
			return nil
		})
	}()
	<-holding

	// Concurrent Run on the same key must return nil and NOT invoke fn.
	var ran bool
	err := l.Run(context.Background(), key, func(ctx context.Context) error {
		ran = true
		return nil
	})
	if err != nil {
		t.Fatalf("loser Run: got error %v, want nil", err)
	}
	if ran {
		t.Fatal("loser Run should not invoke fn when the lock is held elsewhere")
	}

	close(releaseHolder)
}
