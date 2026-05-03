package service_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/model"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/repository"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/service"

	"github.com/google/uuid"
)

func TestShortenURL_NewURL_CreatesAndLogs(t *testing.T) {
	t.Parallel()
	urlRepo := &fakeURLRepo{}
	reqRepo := &fakeRequestRepo{}
	svc := service.NewURLService(urlRepo, reqRepo)

	got, err := svc.ShortenURL(context.Background(), "https://example.com/page", "test-agent")
	if err != nil {
		t.Fatalf("ShortenURL: %v", err)
	}
	if got.URL != "https://example.com/page" {
		t.Errorf("URL: got %q want %q", got.URL, "https://example.com/page")
	}
	if got.ShortKey == "" {
		t.Error("ShortKey is empty")
	}
	if !got.IsActive {
		t.Error("IsActive: got false, want true")
	}
	if urlRepo.CreateCalls.Load() != 1 {
		t.Errorf("Create calls: got %d want 1", urlRepo.CreateCalls.Load())
	}
	if reqRepo.LogCreateRequestCalls.Load() != 1 {
		t.Errorf("LogCreateRequest calls: got %d want 1", reqRepo.LogCreateRequestCalls.Load())
	}
}

func TestShortenURL_ExistingURL_ReturnsExistingWithoutCreate(t *testing.T) {
	t.Parallel()
	existing := &model.URL{
		Base:     model.NewBase(),
		URL:      "https://example.com/page",
		ShortKey: "stub_key_01",
		IsActive: true,
	}
	urlRepo := &fakeURLRepo{
		GetByShortKeyFn: func(ctx context.Context, key string) (*model.URL, error) {
			return existing, nil
		},
	}
	reqRepo := &fakeRequestRepo{}
	svc := service.NewURLService(urlRepo, reqRepo)

	got, err := svc.ShortenURL(context.Background(), existing.URL, "test-agent")
	if err != nil {
		t.Fatalf("ShortenURL: %v", err)
	}
	if got.ID != existing.ID {
		t.Errorf("returned a new URL instead of the existing one")
	}
	if urlRepo.CreateCalls.Load() != 0 {
		t.Errorf("Create should not be called for existing URL, got %d", urlRepo.CreateCalls.Load())
	}
	if reqRepo.LogCreateRequestCalls.Load() != 1 {
		t.Errorf("LogCreateRequest should still log the hit, got %d", reqRepo.LogCreateRequestCalls.Load())
	}
}

func TestShortenURL_HashCollision_RetriesAndCreates(t *testing.T) {
	t.Parallel()
	conflict := &model.URL{
		Base:     model.NewBase(),
		URL:      "https://different.example.com/other",
		ShortKey: "stub_key_01",
	}

	var attempts atomic.Int32 // first call: collision; rest: not found
	urlRepo := &fakeURLRepo{
		GetByShortKeyFn: func(ctx context.Context, key string) (*model.URL, error) {
			if attempts.Add(1) == 1 {
				return conflict, nil
			}
			return nil, repository.ErrNotFound
		},
	}
	reqRepo := &fakeRequestRepo{}
	svc := service.NewURLService(urlRepo, reqRepo)

	got, err := svc.ShortenURL(context.Background(), "https://example.com/me", "agent")
	if err != nil {
		t.Fatalf("ShortenURL: %v", err)
	}
	if got.URL != "https://example.com/me" {
		t.Errorf("URL: got %q", got.URL)
	}
	if attempts.Load() < 2 {
		t.Errorf("expected at least 2 GetByShortKey calls (collision + retry), got %d", attempts.Load())
	}
	if urlRepo.CreateCalls.Load() != 1 {
		t.Errorf("Create calls: got %d want 1", urlRepo.CreateCalls.Load())
	}
}

func TestShortenURL_LookupError_PropagatesWrapped(t *testing.T) {
	t.Parallel()
	dbErr := errors.New("connection refused")
	urlRepo := &fakeURLRepo{
		GetByShortKeyFn: func(ctx context.Context, key string) (*model.URL, error) {
			return nil, dbErr
		},
	}
	svc := service.NewURLService(urlRepo, &fakeRequestRepo{})

	_, err := svc.ShortenURL(context.Background(), "https://example.com", "agent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("error chain should include dbErr, got %v", err)
	}
	if !strings.Contains(err.Error(), "lookup short key") {
		t.Errorf("error should be wrapped with 'lookup short key' context, got %q", err.Error())
	}
}

func TestShortenURL_CreateError_PropagatesWrapped(t *testing.T) {
	t.Parallel()
	dbErr := errors.New("disk full")
	urlRepo := &fakeURLRepo{
		CreateFn: func(ctx context.Context, u *model.URL) error { return dbErr },
	}
	svc := service.NewURLService(urlRepo, &fakeRequestRepo{})

	_, err := svc.ShortenURL(context.Background(), "https://example.com", "agent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("error chain should include dbErr, got %v", err)
	}
	if !strings.Contains(err.Error(), "create url") {
		t.Errorf("error should be wrapped with 'create url' context, got %q", err.Error())
	}
}

func TestShortenURL_LogRequestError_DoesNotFailOperation(t *testing.T) {
	t.Parallel()
	urlRepo := &fakeURLRepo{}
	reqRepo := &fakeRequestRepo{
		LogCreateRequestFn: func(ctx context.Context, id uuid.UUID, agent string) (*model.IncomingRequest, error) {
			return nil, errors.New("logging is broken")
		},
	}
	svc := service.NewURLService(urlRepo, reqRepo)

	got, err := svc.ShortenURL(context.Background(), "https://example.com", "agent")
	if err != nil {
		t.Fatalf("logging failure should not propagate, got %v", err)
	}
	if got == nil {
		t.Fatal("expected URL even when logging failed")
	}
}

func TestShortenURL_RespectsCancelledContext(t *testing.T) {
	t.Parallel()
	urlRepo := &fakeURLRepo{
		GetByShortKeyFn: func(ctx context.Context, key string) (*model.URL, error) {
			return nil, ctx.Err()
		},
	}
	svc := service.NewURLService(urlRepo, &fakeRequestRepo{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.ShortenURL(ctx, "https://example.com", "agent")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestRedirect_Found_ReturnsOriginalURL(t *testing.T) {
	t.Parallel()
	existing := &model.URL{
		Base:     model.NewBase(),
		URL:      "https://example.com/destination",
		ShortKey: "found_key01",
	}
	urlRepo := &fakeURLRepo{
		GetByShortKeyFn: func(ctx context.Context, key string) (*model.URL, error) {
			return existing, nil
		},
	}
	svc := service.NewURLService(urlRepo, &fakeRequestRepo{})

	got, err := svc.Redirect(context.Background(), "found_key01")
	if err != nil {
		t.Fatalf("Redirect: %v", err)
	}
	if got != existing.URL {
		t.Errorf("URL: got %q want %q", got, existing.URL)
	}
	// UpdateLastUsed runs in a goroutine — give it a beat to be observed.
	// Test passes as long as the call eventually shows up; we don't fail
	// on a race because this is fire-and-forget.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if urlRepo.UpdateLastUsedCalls.Load() == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("UpdateLastUsed was never called: count=%d", urlRepo.UpdateLastUsedCalls.Load())
}

func TestRedirect_NotFound_PropagatesError(t *testing.T) {
	t.Parallel()
	urlRepo := &fakeURLRepo{} // default returns ErrNotFound
	svc := service.NewURLService(urlRepo, &fakeRequestRepo{})

	_, err := svc.Redirect(context.Background(), "missing")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// Benchmark the happy path through the service layer with stubbed repos.
// Measures the cost of: short-key generation, repo round-trips (no I/O), and
// the goroutine spawn for the request log.
func BenchmarkShortenURL_NewEntry(b *testing.B) {
	urlRepo := &fakeURLRepo{} // default GetByShortKey returns ErrNotFound
	reqRepo := &fakeRequestRepo{}
	svc := service.NewURLService(urlRepo, reqRepo)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = svc.ShortenURL(ctx, "https://example.com/some/long/path?with=query", "ua")
	}
}
