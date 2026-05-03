package logger

import (
	"log/slog"
	"os"
	"strings"
)

// Init installs a JSON slog handler as the process default.
// Call once from main before any other component logs.
func Init(level string) {
	var lvl slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     lvl,
		AddSource: true,
	})
	slog.SetDefault(slog.New(h))
}
