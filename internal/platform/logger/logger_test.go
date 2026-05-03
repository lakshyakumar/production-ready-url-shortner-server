package logger_test

import (
	"context"
	"log/slog"
	"testing"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/platform/logger"
)

// Init takes a level string and installs a JSON handler as the slog default.
// We can't easily assert on the JSON output (it goes to os.Stdout), but we
// can prove that:
//  1. Init doesn't panic on any documented level (or unknown levels).
//  2. After Init, the default logger respects the level (Debug logs are
//     suppressed under "info", visible under "debug", etc.).
func TestInit_LevelGates(t *testing.T) {
	cases := []struct {
		level        string
		debugEnabled bool
		warnEnabled  bool
		errorEnabled bool
	}{
		{"debug", true, true, true},
		{"info", false, true, true},
		{"INFO", false, true, true},     // case-insensitive
		{"  Info  ", false, true, true}, // trimmed
		{"warn", false, true, true},
		{"warning", false, true, true}, // alias
		{"error", false, false, true},
		{"unknown", false, true, true}, // falls back to info
	}

	for _, c := range cases {
		c := c
		t.Run(c.level, func(t *testing.T) {
			logger.Init(c.level)
			ctx := context.Background()
			l := slog.Default()

			if got := l.Enabled(ctx, slog.LevelDebug); got != c.debugEnabled {
				t.Errorf("Debug enabled at level %q: got %v want %v", c.level, got, c.debugEnabled)
			}
			if got := l.Enabled(ctx, slog.LevelWarn); got != c.warnEnabled {
				t.Errorf("Warn enabled at level %q: got %v want %v", c.level, got, c.warnEnabled)
			}
			if got := l.Enabled(ctx, slog.LevelError); got != c.errorEnabled {
				t.Errorf("Error enabled at level %q: got %v want %v", c.level, got, c.errorEnabled)
			}
		})
	}
}
