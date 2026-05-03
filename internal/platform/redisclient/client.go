package redisclient

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

func NewClient(ctx context.Context, url string) (*redis.Client, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}

	opt.PoolSize = 20
	opt.MinIdleConns = 5
	opt.ConnMaxIdleTime = 30 * time.Minute

	client := redis.NewClient(opt)

	// redisotel attaches a hook that creates a span per Redis command. With
	// the noop tracer this is a few extra method calls; with a real
	// provider it gives us latency + command labels in traces.
	if err := redisotel.InstrumentTracing(client); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redisotel instrument: %w", err)
	}

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}
