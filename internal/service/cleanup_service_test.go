package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/platform/config"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/service"
)

func TestRunCleanup_PassesCorrectCutoffToRepo(t *testing.T) {
	t.Parallel()
	threshold := 24 * time.Hour
	cfg := &config.Config{URLUnusedThreshold: threshold}

	var captured time.Time
	urlRepo := &fakeURLRepo{
		DeleteUnusedSinceFn: func(ctx context.Context, cutoff time.Time) (int64, error) {
			captured = cutoff
			return 5, nil
		},
	}
	svc := service.NewCleanupService(urlRepo, cfg)

	before := time.Now()
	if err := svc.RunCleanup(context.Background()); err != nil {
		t.Fatalf("RunCleanup: %v", err)
	}
	after := time.Now()

	// cutoff should sit between (before-threshold) and (after-threshold).
	expectedMin := before.Add(-threshold).Add(-time.Second)
	expectedMax := after.Add(-threshold).Add(time.Second)
	if captured.Before(expectedMin) || captured.After(expectedMax) {
		t.Errorf("cutoff out of expected window: got %v (window %v..%v)",
			captured, expectedMin, expectedMax)
	}
	if urlRepo.DeleteUnusedSinceCalls.Load() != 1 {
		t.Errorf("DeleteUnusedSince calls: got %d want 1", urlRepo.DeleteUnusedSinceCalls.Load())
	}
}

func TestRunCleanup_RepoError_Propagates(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{URLUnusedThreshold: 24 * time.Hour}
	dbErr := errors.New("connection lost")
	urlRepo := &fakeURLRepo{
		DeleteUnusedSinceFn: func(ctx context.Context, cutoff time.Time) (int64, error) {
			return 0, dbErr
		},
	}
	svc := service.NewCleanupService(urlRepo, cfg)

	err := svc.RunCleanup(context.Background())
	if !errors.Is(err, dbErr) {
		t.Errorf("expected dbErr in chain, got %v", err)
	}
}

func TestRunCleanup_RespectsCancelledContext(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{URLUnusedThreshold: 24 * time.Hour}
	urlRepo := &fakeURLRepo{
		DeleteUnusedSinceFn: func(ctx context.Context, cutoff time.Time) (int64, error) {
			return 0, ctx.Err()
		},
	}
	svc := service.NewCleanupService(urlRepo, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := svc.RunCleanup(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
