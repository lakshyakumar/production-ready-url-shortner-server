package service

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter decides whether a key (typically a client IP) may make another
// request right now. Implementations are expected to be safe for concurrent
// use across many goroutines and many replicas — the Redis-backed impl is the
// reason this lives in the service layer rather than as an in-process counter.
type RateLimiter interface {
	// Allow returns whether the request is permitted, how many requests
	// remain in the current window, and (when denied) how long until the
	// next window opens. A non-nil error means the limiter could not make
	// a decision; callers decide whether to fail open or closed.
	Allow(ctx context.Context, key string) (allowed bool, remaining int, retryAfter time.Duration, err error)
}

// redisRateLimiter is a fixed-window counter backed by Redis. Keys live for
// exactly one window — the first INCR on a fresh key triggers an EXPIRE so
// the bucket evaporates on its own without a sweeper.
type redisRateLimiter struct {
	client    *redis.Client
	limit     int
	window    time.Duration
	opTimeout time.Duration
}

// NewRedisRateLimiter wires up the limiter. limit and window come from config;
// opTimeout caps each Redis call so a slow Redis cannot stall request handling.
func NewRedisRateLimiter(client *redis.Client, limit int, window, opTimeout time.Duration) RateLimiter {
	return &redisRateLimiter{
		client:    client,
		limit:     limit,
		window:    window,
		opTimeout: opTimeout,
	}
}

func (r *redisRateLimiter) Allow(ctx context.Context, key string) (bool, int, time.Duration, error) {
	if r.opTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.opTimeout)
		defer cancel()
	}

	now := time.Now()
	// Bucket the key by the floor of (now / window). Two requests in the same
	// window land on the same Redis key; the first request after the window
	// rolls over lands on a fresh key.
	windowSec := int64(r.window / time.Second)
	if windowSec <= 0 {
		windowSec = 1
	}
	bucket := now.Unix() / windowSec
	redisKey := fmt.Sprintf("rl:%s:%d", key, bucket)

	count, err := r.client.Incr(ctx, redisKey).Result()
	if err != nil {
		return false, 0, 0, err
	}

	// Only the request that created the key sets its TTL. Setting EXPIRE on
	// every INCR would push the expiry forward and turn this into a sliding
	// window by accident.
	if count == 1 {
		if err := r.client.Expire(ctx, redisKey, r.window).Err(); err != nil {
			return false, 0, 0, err
		}
	}

	if count > int64(r.limit) {
		// Time until the next bucket starts.
		nextBucketStart := time.Unix((bucket+1)*windowSec, 0)
		retryAfter := time.Until(nextBucketStart)
		if retryAfter < 0 {
			retryAfter = 0
		}
		return false, 0, retryAfter, nil
	}

	remaining := r.limit - int(count)
	if remaining < 0 {
		remaining = 0
	}
	return true, remaining, 0, nil
}
