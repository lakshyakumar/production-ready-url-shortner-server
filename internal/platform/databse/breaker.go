package database

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/repository"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sony/gobreaker/v2"
)

// BreakerSettings configures a single pool's breaker. Defaults are
// conservative — see DefaultBreakerSettings.
type BreakerSettings struct {
	Name                string
	MaxRequests         uint32
	Interval            time.Duration
	Timeout             time.Duration
	ConsecutiveFailures uint32
	ErrorRateThreshold  float64
	MinRequestsForRate  uint32
}

// DefaultBreakerSettings returns the conservative defaults agreed in the
// design: trip on 5 consecutive failures OR >50% error rate over 60s with
// at least 20 requests; half-open after 30s with a single probe.
func DefaultBreakerSettings(name string) BreakerSettings {
	return BreakerSettings{
		Name:                name,
		MaxRequests:         1,
		Interval:            60 * time.Second,
		Timeout:             30 * time.Second,
		ConsecutiveFailures: 5,
		ErrorRateThreshold:  0.5,
		MinRequestsForRate:  20,
	}
}

// newBreaker assembles a *gobreaker.CircuitBreaker[any] from BreakerSettings,
// wiring the standard ReadyToTrip / IsSuccessful / OnStateChange hooks.
//
// IsSuccessful classifies the error to decide whether a call counts as a
// failure for tripping purposes — see classifyErr.
func newBreaker(s BreakerSettings) *gobreaker.CircuitBreaker[any] {
	return gobreaker.NewCircuitBreaker[any](gobreaker.Settings{
		Name:        s.Name,
		MaxRequests: s.MaxRequests,
		Interval:    s.Interval,
		Timeout:     s.Timeout,
		ReadyToTrip: func(c gobreaker.Counts) bool {
			if c.ConsecutiveFailures >= s.ConsecutiveFailures {
				return true
			}
			if s.MinRequestsForRate > 0 && c.Requests >= s.MinRequestsForRate {
				rate := float64(c.TotalFailures) / float64(c.Requests)
				if rate > s.ErrorRateThreshold {
					return true
				}
			}
			return false
		},
		IsSuccessful: func(err error) bool {
			return !shouldCount(err)
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			slog.Warn("circuit breaker state change",
				slog.String("name", name),
				slog.String("from", from.String()),
				slog.String("to", to.String()),
			)
		},
	})
}

// shouldCount returns true when err represents a real infrastructure
// failure — connection refused, pool exhaustion, server-side outage,
// statement timeout. It returns false for outcomes that are normal
// control-flow signals (no rows, constraint violations, client cancel).
//
// Anything that returns true contributes to ConsecutiveFailures and
// TotalFailures inside gobreaker; anything that returns false is treated
// as a successful call from the breaker's point of view.
func shouldCount(err error) bool {
	if err == nil {
		return false
	}
	// Repo-level "no rows" — already mapped before reaching the breaker
	// in our codebase, but defend against future callers.
	if errors.Is(err, repository.ErrNotFound) {
		return false
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	// Client gave up; not the DB's fault.
	if errors.Is(err, context.Canceled) {
		return false
	}
	// Our timeout fired — that's an infrastructure signal: the DB took
	// longer than dbCallTimeout. Count it.
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// Server-side errors classified by SQLSTATE class:
	//   23  integrity constraint violation (FK, unique, check, NOT NULL) — caller's fault, don't count
	//   42  syntax / privilege error — caller's fault
	//   53  insufficient resources (out of memory, disk full, too many conns) — count
	//   57  operator intervention (admin shutdown, crash) — count
	//   58  system error (file IO) — count
	//   08  connection exception — count
	//   XX  internal — count
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code[:2] {
		case "23", "42":
			return false
		case "53", "57", "58", "08", "XX":
			return true
		}
		// Other classes: be conservative and don't count by default.
		return false
	}

	// Non-pg error (network, pool acquire, driver-level): count.
	return true
}
