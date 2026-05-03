package cron_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/cron"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/platform/config"
)

// fakeLocker invokes fn on every Run (no real lock). The cron uses Locker
// only to gate the work; behavior under contention is exercised in
// platform/leader's own tests.
type fakeLocker struct {
	RunFn    func(ctx context.Context, key int64, fn func(context.Context) error) error
	RunCalls atomic.Int32
}

func (f *fakeLocker) Run(ctx context.Context, key int64, fn func(context.Context) error) error {
	f.RunCalls.Add(1)
	if f.RunFn != nil {
		return f.RunFn(ctx, key, fn)
	}
	return fn(ctx)
}

// fakeCleanupService satisfies service.CleanupService.
type fakeCleanupService struct {
	RunCleanupFn    func(ctx context.Context) error
	RunCleanupCalls atomic.Int32
}

func (f *fakeCleanupService) RunCleanup(ctx context.Context) error {
	f.RunCleanupCalls.Add(1)
	if f.RunCleanupFn != nil {
		return f.RunCleanupFn(ctx)
	}
	return nil
}

// startAndWait runs CleanupCron.Start in a goroutine and waits for at least
// `wantTicks` cleanup invocations or the deadline, whichever comes first.
// Returns the number of cleanup calls observed.
func startAndWait(t *testing.T, c *cron.CleanupCron, svc *fakeCleanupService, wantTicks int32, deadline time.Duration) int32 {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		c.Start(ctx)
		close(done)
	}()

	stop := time.After(deadline)
	for {
		select {
		case <-stop:
			cancel()
			<-done
			return svc.RunCleanupCalls.Load()
		default:
			if svc.RunCleanupCalls.Load() >= wantTicks {
				cancel()
				<-done
				return svc.RunCleanupCalls.Load()
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func TestCleanupCron_TicksInvokeServiceThroughLocker(t *testing.T) {
	t.Parallel()
	svc := &fakeCleanupService{}
	loc := &fakeLocker{}
	cfg := &config.Config{CleanUpInterval: 20 * time.Millisecond}

	c := cron.NewCleanupCron(svc, cfg, loc)

	got := startAndWait(t, c, svc, 2, 500*time.Millisecond)
	if got < 2 {
		t.Errorf("RunCleanup calls: got %d, want at least 2", got)
	}
	if loc.RunCalls.Load() < got {
		t.Errorf("Locker.Run calls (%d) should be ≥ cleanup calls (%d)", loc.RunCalls.Load(), got)
	}
}

func TestCleanupCron_LockerSkippingDoesNotCallService(t *testing.T) {
	t.Parallel()
	svc := &fakeCleanupService{}
	// fakeLocker.Run that never invokes fn — simulates "another replica
	// is the leader." Cron should still tick, but the service must stay idle.
	loc := &fakeLocker{
		RunFn: func(ctx context.Context, key int64, fn func(context.Context) error) error {
			return nil
		},
	}
	cfg := &config.Config{CleanUpInterval: 10 * time.Millisecond}

	c := cron.NewCleanupCron(svc, cfg, loc)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Start(ctx); close(done) }()
	time.Sleep(60 * time.Millisecond)
	cancel()
	<-done

	if loc.RunCalls.Load() < 2 {
		t.Errorf("Locker.Run calls: got %d, want at least 2", loc.RunCalls.Load())
	}
	if svc.RunCleanupCalls.Load() != 0 {
		t.Errorf("RunCleanup should not be called when locker skips, got %d", svc.RunCleanupCalls.Load())
	}
}

func TestCleanupCron_LockerErrorIsLoggedAndLoopContinues(t *testing.T) {
	t.Parallel()
	svc := &fakeCleanupService{}
	loc := &fakeLocker{
		RunFn: func(ctx context.Context, key int64, fn func(context.Context) error) error {
			return errors.New("infra blip")
		},
	}
	cfg := &config.Config{CleanUpInterval: 10 * time.Millisecond}

	c := cron.NewCleanupCron(svc, cfg, loc)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Start(ctx); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	// Multiple errors should not crash the loop — we should see > 1 attempt.
	if loc.RunCalls.Load() < 2 {
		t.Errorf("loop should keep ticking after errors; got only %d Run calls", loc.RunCalls.Load())
	}
}

func TestCleanupCron_StopsOnContextCancel(t *testing.T) {
	t.Parallel()
	svc := &fakeCleanupService{}
	loc := &fakeLocker{}
	cfg := &config.Config{CleanUpInterval: 50 * time.Millisecond}

	c := cron.NewCleanupCron(svc, cfg, loc)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Start(ctx); close(done) }()

	cancel()
	select {
	case <-done:
		// good
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Start did not return after ctx cancel")
	}
}
