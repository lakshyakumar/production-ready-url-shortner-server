package redisclient_test

import (
	"context"
	"testing"
	"time"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/platform/redisclient"

	"github.com/alicebob/miniredis/v2"
)

// miniredis exposes a `redis://host:port` URL via Addr() that NewClient can
// parse, so we can exercise NewClient end-to-end (parse, ping, instrument,
// return) without docker.

func TestNewClient_PingsAndReturnsLiveClient(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)

	client, err := redisclient.NewClient(context.Background(), "redis://"+mr.Addr())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	if setErr := client.Set(context.Background(), "k", "v", 0).Err(); setErr != nil {
		t.Errorf("SET on returned client: %v", setErr)
	}
	got, err := client.Get(context.Background(), "k").Result()
	if err != nil {
		t.Fatalf("GET on returned client: %v", err)
	}
	if got != "v" {
		t.Errorf("GET k: got %q want %q", got, "v")
	}
}

func TestNewClient_InvalidURL_ReturnsError(t *testing.T) {
	t.Parallel()
	_, err := redisclient.NewClient(context.Background(), "not-a-redis-url")
	if err == nil {
		t.Fatal("NewClient should fail on a malformed URL")
	}
}

func TestNewClient_UnreachableServer_ReturnsError(t *testing.T) {
	t.Parallel()
	// Bind a port-of-no-return: 127.0.0.1:1 is conventionally never listening.
	// Use a tight ctx so we don't wait the full default Redis dial timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := redisclient.NewClient(ctx, "redis://127.0.0.1:1")
	if err == nil {
		t.Fatal("NewClient should fail when Redis is unreachable")
	}
}
