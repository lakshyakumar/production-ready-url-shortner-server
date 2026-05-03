package database

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/repository"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sony/gobreaker/v2"
)

// fakeDB is a controllable repository.DBTX for breakerDB tests.
//
// Set ExecErr / QueryErr to make the next call fail; clear them to make it
// succeed. CallCount is atomic so the breaker's internal goroutines
// (none currently, but defense) can race safely.
type fakeDB struct {
	ExecErr   error
	QueryErr  error
	CallCount atomic.Int32
}

func (f *fakeDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.CallCount.Add(1)
	return pgconn.CommandTag{}, f.ExecErr
}
func (f *fakeDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.CallCount.Add(1)
	if f.QueryErr != nil {
		return nil, f.QueryErr
	}
	return nil, errors.New("fakeDB: Query without QueryErr is not implemented (tests don't need rows)")
}
func (f *fakeDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	f.CallCount.Add(1)
	return errRow{err: f.QueryErr}
}

// fastSettings trips the breaker after 2 consecutive failures and recovers
// after 50ms — keeps state-transition tests sub-second.
func fastSettings(name string) BreakerSettings {
	return BreakerSettings{
		Name:                name,
		MaxRequests:         1,
		Interval:            time.Hour, // counts never reset by interval during a test
		Timeout:             50 * time.Millisecond,
		ConsecutiveFailures: 2,
		ErrorRateThreshold:  0.5,
		MinRequestsForRate:  100, // effectively disabled — only consecutive matters
	}
}

func TestBreakerDB_TripsAfterConsecutiveFailures(t *testing.T) {
	t.Parallel()
	fake := &fakeDB{ExecErr: context.DeadlineExceeded}
	bdb := newBreakerDB(fake, fastSettings("test"))

	// Trip threshold = 2 → 2 failing calls should leave the breaker Open.
	for i := 0; i < 2; i++ {
		if _, err := bdb.Exec(context.Background(), "SELECT 1"); err == nil {
			t.Fatalf("call %d: expected error, got nil", i)
		}
	}
	if got := bdb.State(); got != gobreaker.StateOpen {
		t.Fatalf("breaker state: got %v, want Open", got)
	}
}

func TestBreakerDB_OpenRejectsCallsWithErrCircuitOpen(t *testing.T) {
	t.Parallel()
	fake := &fakeDB{ExecErr: context.DeadlineExceeded}
	bdb := newBreakerDB(fake, fastSettings("test"))

	// Trip the breaker.
	for i := 0; i < 2; i++ {
		_, _ = bdb.Exec(context.Background(), "SELECT 1")
	}

	beforeOpen := fake.CallCount.Load()
	_, err := bdb.Exec(context.Background(), "SELECT 1")
	if !errors.Is(err, repository.ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
	if fake.CallCount.Load() != beforeOpen {
		t.Errorf("breakerDB called underlying DB while open — should short-circuit")
	}
}

func TestBreakerDB_BusinessErrorsDoNotTrip(t *testing.T) {
	t.Parallel()
	fake := &fakeDB{ExecErr: repository.ErrNotFound}
	bdb := newBreakerDB(fake, fastSettings("test"))

	// 10 calls with business errors — breaker should remain Closed because
	// shouldCount classifies these as non-failures.
	for i := 0; i < 10; i++ {
		_, _ = bdb.Exec(context.Background(), "SELECT 1")
	}
	if got := bdb.State(); got != gobreaker.StateClosed {
		t.Fatalf("breaker state: got %v, want Closed (business errors should not count)", got)
	}
}

func TestBreakerDB_RecoversAfterTimeout(t *testing.T) {
	t.Parallel()
	fake := &fakeDB{ExecErr: context.DeadlineExceeded}
	bdb := newBreakerDB(fake, fastSettings("test"))

	for i := 0; i < 2; i++ {
		_, _ = bdb.Exec(context.Background(), "SELECT 1")
	}
	if got := bdb.State(); got != gobreaker.StateOpen {
		t.Fatalf("setup: breaker should be Open, got %v", got)
	}

	// Heal the underlying DB and wait past the breaker's Timeout.
	fake.ExecErr = nil
	time.Sleep(80 * time.Millisecond)

	// First call after Timeout is the half-open probe; success closes the
	// breaker and the call should propagate normally.
	if _, err := bdb.Exec(context.Background(), "SELECT 1"); err != nil {
		t.Fatalf("probe call: expected success, got %v", err)
	}
	if got := bdb.State(); got != gobreaker.StateClosed {
		t.Errorf("breaker should be Closed after successful probe, got %v", got)
	}
}

func TestRouter_HappyPath_ReaderIsReplica_WriterIsPrimary(t *testing.T) {
	t.Parallel()
	primary := newBreakerDB(&fakeDB{}, fastSettings("primary"))
	replica := newBreakerDB(&fakeDB{}, fastSettings("replica"))
	r := &poolRouter{primary: primary, replica: replica}

	if r.Reader() != repository.DBTX(replica) {
		t.Errorf("Reader: expected replica, got something else")
	}
	if r.Writer() != repository.DBTX(primary) {
		t.Errorf("Writer: expected primary, got something else")
	}
}

func TestRouter_NoReplica_BothSidesUsePrimary(t *testing.T) {
	t.Parallel()
	primary := newBreakerDB(&fakeDB{}, fastSettings("primary"))
	r := &poolRouter{primary: primary}

	if r.Reader() != repository.DBTX(primary) {
		t.Errorf("Reader: expected primary (no replica configured)")
	}
	if r.Writer() != repository.DBTX(primary) {
		t.Errorf("Writer: expected primary")
	}
}

func TestRouter_ReplicaBreakerOpen_ReadsFallBackToPrimary(t *testing.T) {
	t.Parallel()
	primary := newBreakerDB(&fakeDB{}, fastSettings("primary"))
	replicaFake := &fakeDB{ExecErr: context.DeadlineExceeded}
	replica := newBreakerDB(replicaFake, fastSettings("replica"))

	// Trip the replica's breaker.
	for i := 0; i < 2; i++ {
		_, _ = replica.Exec(context.Background(), "SELECT 1")
	}
	if replica.State() != gobreaker.StateOpen {
		t.Fatalf("setup: replica breaker should be Open")
	}

	r := &poolRouter{primary: primary, replica: replica}
	if r.Reader() != repository.DBTX(primary) {
		t.Errorf("Reader: expected primary fallback when replica breaker is Open")
	}
	if r.Writer() != repository.DBTX(primary) {
		t.Errorf("Writer: expected primary (writes never fall back)")
	}
}

func TestRouter_BothBreakersOpen_ReadsReturnErrCircuitOpen(t *testing.T) {
	t.Parallel()
	primaryFake := &fakeDB{ExecErr: context.DeadlineExceeded}
	primary := newBreakerDB(primaryFake, fastSettings("primary"))
	replicaFake := &fakeDB{ExecErr: context.DeadlineExceeded}
	replica := newBreakerDB(replicaFake, fastSettings("replica"))

	// Trip both breakers.
	for i := 0; i < 2; i++ {
		_, _ = primary.Exec(context.Background(), "SELECT 1")
		_, _ = replica.Exec(context.Background(), "SELECT 1")
	}

	r := &poolRouter{primary: primary, replica: replica}
	// Reader returns the primary (replica is Open) and primary's breaker
	// is also Open, so the next call short-circuits to ErrCircuitOpen.
	_, err := r.Reader().Exec(context.Background(), "SELECT 1")
	if !errors.Is(err, repository.ErrCircuitOpen) {
		t.Errorf("Reader().Exec when both Open: expected ErrCircuitOpen, got %v", err)
	}
	_, err = r.Writer().Exec(context.Background(), "SELECT 1")
	if !errors.Is(err, repository.ErrCircuitOpen) {
		t.Errorf("Writer().Exec when primary Open: expected ErrCircuitOpen, got %v", err)
	}
}
