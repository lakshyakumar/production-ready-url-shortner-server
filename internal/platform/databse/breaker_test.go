package database

import (
	"context"
	"errors"
	"testing"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/repository"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestShouldCount is a pure-function table test. Every error type the
// breaker might see should land in the right bucket — counting it as a
// failure (returns true) or a success (returns false). Misclassifying
// here means a flapping primary either trips too easily or never trips.
func TestShouldCount(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},

		// Business outcomes — never count.
		{"ErrNotFound", repository.ErrNotFound, false},
		{"pgx.ErrNoRows", pgx.ErrNoRows, false},
		{"context.Canceled", context.Canceled, false},
		{"FK violation (23503)", &pgconn.PgError{Code: "23503"}, false},
		{"unique violation (23505)", &pgconn.PgError{Code: "23505"}, false},
		{"syntax error (42601)", &pgconn.PgError{Code: "42601"}, false},
		{"undefined table (42P01)", &pgconn.PgError{Code: "42P01"}, false},

		// Infrastructure failures — always count.
		{"DeadlineExceeded", context.DeadlineExceeded, true},
		{"connection failure (08006)", &pgconn.PgError{Code: "08006"}, true},
		{"out of memory (53200)", &pgconn.PgError{Code: "53200"}, true},
		{"too many connections (53300)", &pgconn.PgError{Code: "53300"}, true},
		{"admin shutdown (57P01)", &pgconn.PgError{Code: "57P01"}, true},
		{"crash shutdown (57P02)", &pgconn.PgError{Code: "57P02"}, true},
		{"system error (58000)", &pgconn.PgError{Code: "58000"}, true},
		{"internal error (XX000)", &pgconn.PgError{Code: "XX000"}, true},

		// Plain Go error (network, pool acquire) — count.
		{"plain error", errors.New("boom"), true},

		// Wrapped errors should unwrap correctly.
		{"wrapped DeadlineExceeded", errors.Join(errors.New("foo"), context.DeadlineExceeded), true},
		{"wrapped ErrNotFound", errors.Join(errors.New("foo"), repository.ErrNotFound), false},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldCount(c.err); got != c.want {
				t.Errorf("shouldCount(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}
