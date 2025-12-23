package logging

import (
	"log/slog"
	"os"
	"strings"
)

func New(configLevel string, envOverride string) *slog.Logger {
	level := parseLevel(configLevel)
	if strings.TrimSpace(envOverride) != "" {
		level = parseLevel(envOverride)
	}

	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})
	return slog.New(handler)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "info", "":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
