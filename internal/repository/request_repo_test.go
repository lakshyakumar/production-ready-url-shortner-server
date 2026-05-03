package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	contextutil "github/lakshyakumar/production-ready-url-shortner-server/internal/contextUtil"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/repository"

	"github.com/google/uuid"
)

func TestRequestRepository_LogCreateRequest_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx, tx := withTx(t)
	reqRepo := repository.NewRequestRepository(singlePoolRouter(tx))

	u := seedURL(t, tx, "req_test001", time.Now().UTC())
	urlID, err := uuid.Parse(u.ID)
	if err != nil {
		t.Fatalf("parse seeded id: %v", err)
	}

	got, err := reqRepo.LogCreateRequest(ctx, urlID, "test-agent")
	if err != nil {
		t.Fatalf("LogCreateRequest: %v", err)
	}
	if got.URLID != urlID {
		t.Errorf("URLID: got %v want %v", got.URLID, urlID)
	}
	if got.UserAgent != "test-agent" {
		t.Errorf("UserAgent: got %q want %q", got.UserAgent, "test-agent")
	}
	if got.IPAddress == "" {
		t.Errorf("IPAddress: should default to 'unknown', got empty")
	}

	var count int
	if err := tx.QueryRow(ctx,
		"SELECT count(*) FROM incoming_requests WHERE url_id = $1", urlID).Scan(&count); err != nil {
		t.Fatalf("verify count: %v", err)
	}
	if count != 1 {
		t.Errorf("row count: got %d want 1", count)
	}
}

func TestRequestRepository_LogCreateRequest_PicksUpClientIPFromContext(t *testing.T) {
	t.Parallel()
	_, tx := withTx(t)
	reqRepo := repository.NewRequestRepository(singlePoolRouter(tx))

	u := seedURL(t, tx, "req_ip_001", time.Now().UTC())
	urlID, err := uuid.Parse(u.ID)
	if err != nil {
		t.Fatalf("parse seeded id: %v", err)
	}

	ctx := contextutil.WithClientIP(context.Background(), "203.0.113.7")
	got, err := reqRepo.LogCreateRequest(ctx, urlID, "agent")
	if err != nil {
		t.Fatalf("LogCreateRequest: %v", err)
	}
	if got.IPAddress != "203.0.113.7" {
		t.Errorf("IPAddress: got %q want %q", got.IPAddress, "203.0.113.7")
	}
}

func TestRequestRepository_LogCreateRequest_FKViolationOnUnknownURL(t *testing.T) {
	t.Parallel()
	_, tx := withTx(t)
	reqRepo := repository.NewRequestRepository(singlePoolRouter(tx))

	_, err := reqRepo.LogCreateRequest(context.Background(), uuid.New(), "agent")
	if err == nil {
		t.Fatal("expected FK violation error, got nil")
	}
}

func TestRequestRepository_LogCreateRequest_RespectsCancelledContext(t *testing.T) {
	t.Parallel()
	_, tx := withTx(t)
	reqRepo := repository.NewRequestRepository(singlePoolRouter(tx))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := reqRepo.LogCreateRequest(ctx, uuid.New(), "agent"); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// ErrCircuitOpen is now produced by the breakerDB layer (see
// internal/platform/databse/router_test.go), not by a context flag.
