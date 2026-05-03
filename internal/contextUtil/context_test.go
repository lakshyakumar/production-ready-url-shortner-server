package contextutil_test

import (
	"context"
	"testing"

	contextutil "github/lakshyakumar/production-ready-url-shortner-server/internal/contextUtil"
)

func TestClientIP_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := contextutil.WithClientIP(context.Background(), "10.0.0.42")
	if got := contextutil.GetClientIP(ctx); got != "10.0.0.42" {
		t.Errorf("GetClientIP: got %q want %q", got, "10.0.0.42")
	}
}

func TestClientIP_MissingReturnsUnknown(t *testing.T) {
	t.Parallel()
	if got := contextutil.GetClientIP(context.Background()); got != "unknown" {
		t.Errorf(`GetClientIP on bare ctx: got %q want "unknown"`, got)
	}
}

func TestCircuitOpen_RoundTrip(t *testing.T) {
	t.Parallel()
	openCtx := contextutil.WithCircuitOpen(context.Background(), true)
	if !contextutil.IsCircuitOpen(openCtx) {
		t.Error("IsCircuitOpen: got false, want true")
	}

	closedCtx := contextutil.WithCircuitOpen(context.Background(), false)
	if contextutil.IsCircuitOpen(closedCtx) {
		t.Error("IsCircuitOpen: got true, want false (explicitly false)")
	}

	if contextutil.IsCircuitOpen(context.Background()) {
		t.Error("IsCircuitOpen on bare ctx: got true, want false")
	}
}
