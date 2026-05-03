package service_test

import (
	"context"
	"testing"
	"time"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/service"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newTestLimiter spins up an in-process Redis (miniredis) and returns a
// limiter wired against it. The miniredis instance is returned too so tests
// can advance its clock with FastForward to exercise window rollover.
func newTestLimiter(t *testing.T, limit int, window time.Duration) (service.RateLimiter, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return service.NewRedisRateLimiter(client, limit, window, 100*time.Millisecond), mr
}

func TestRateLimiter_AllowsUnderLimit(t *testing.T) {
	t.Parallel()
	limiter, _ := newTestLimiter(t, 3, time.Minute)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		allowed, remaining, _, err := limiter.Allow(ctx, "1.2.3.4")
		if err != nil {
			t.Fatalf("call %d: Allow returned error: %v", i, err)
		}
		if !allowed {
			t.Errorf("call %d: expected allowed=true under limit", i)
		}
		if want := 3 - i; remaining != want {
			t.Errorf("call %d: remaining got %d want %d", i, remaining, want)
		}
	}
}

func TestRateLimiter_BlocksOverLimit(t *testing.T) {
	t.Parallel()
	limiter, _ := newTestLimiter(t, 2, time.Minute)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if allowed, _, _, _ := limiter.Allow(ctx, "5.6.7.8"); !allowed {
			t.Fatalf("priming call %d should be allowed", i)
		}
	}

	allowed, remaining, retryAfter, err := limiter.Allow(ctx, "5.6.7.8")
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if allowed {
		t.Error("expected denied once over limit")
	}
	if remaining != 0 {
		t.Errorf("remaining: got %d want 0 on deny", remaining)
	}
	if retryAfter <= 0 || retryAfter > time.Minute {
		t.Errorf("retryAfter: got %v, want (0, 1m]", retryAfter)
	}
}

func TestRateLimiter_WindowResets(t *testing.T) {
	t.Parallel()
	limiter, mr := newTestLimiter(t, 1, time.Minute)
	ctx := context.Background()

	if allowed, _, _, _ := limiter.Allow(ctx, "9.9.9.9"); !allowed {
		t.Fatal("first call should be allowed")
	}
	if allowed, _, _, _ := limiter.Allow(ctx, "9.9.9.9"); allowed {
		t.Fatal("second call in same window should be denied")
	}

	// Push miniredis past the TTL — the bucket key expires, the next call
	// hits a fresh bucket. The limiter also bucket-keys by wall-clock seconds,
	// so we sleep a hair past the window boundary too.
	mr.FastForward(time.Minute + time.Second)
	time.Sleep(time.Second + 10*time.Millisecond)

	if allowed, _, _, _ := limiter.Allow(ctx, "9.9.9.9"); !allowed {
		t.Error("expected allowed after window reset")
	}
}

func TestRateLimiter_KeysAreIsolated(t *testing.T) {
	t.Parallel()
	limiter, _ := newTestLimiter(t, 1, time.Minute)
	ctx := context.Background()

	if allowed, _, _, _ := limiter.Allow(ctx, "10.0.0.1"); !allowed {
		t.Fatal("first IP should be allowed")
	}
	// A second call for the same key is denied — proves the limiter actually
	// counts.
	if allowed, _, _, _ := limiter.Allow(ctx, "10.0.0.1"); allowed {
		t.Fatal("first IP should be denied on second hit")
	}
	// A different key gets its own counter.
	if allowed, _, _, _ := limiter.Allow(ctx, "10.0.0.2"); !allowed {
		t.Error("second IP should not be affected by first IP's quota")
	}
}

func TestRateLimiter_RedisDown_ReturnsError(t *testing.T) {
	t.Parallel()
	limiter, mr := newTestLimiter(t, 5, time.Minute)
	mr.Close() // simulate Redis going away mid-flight
	ctx := context.Background()

	allowed, _, _, err := limiter.Allow(ctx, "1.1.1.1")
	if err == nil {
		t.Fatal("expected error when Redis is unreachable")
	}
	// On error the limiter reports !allowed — fail-open is the middleware's
	// job, not the service's.
	if allowed {
		t.Error("expected allowed=false when Redis errors; middleware decides fail-open policy")
	}
}
