package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

func New(configLevel string, envOverride string) *slog.Logger {
	l, _ := NewWithBuffer(configLevel, envOverride, 0)
	return l
}

func NewWithBuffer(configLevel string, envOverride string, bufferLines int) (*slog.Logger, *LogBuffer) {
	level := parseLevel(configLevel)
	if strings.TrimSpace(envOverride) != "" {
		level = parseLevel(envOverride)
	}

	base := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})

	var buf *LogBuffer
	var handler slog.Handler = base
	if bufferLines > 0 {
		buf = NewLogBuffer(bufferLines)
		handler = &bufferHandler{next: base, buf: buf}
	}

	return slog.New(handler), buf
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

type bufferHandler struct {
	next slog.Handler
	buf  *LogBuffer
}

func (h *bufferHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *bufferHandler) Handle(ctx context.Context, r slog.Record) error {
	if h.buf != nil {
		attrs := make(map[string]string)
		r.Attrs(func(a slog.Attr) bool {
			attrs[a.Key] = a.Value.String()
			return true
		})
		h.buf.Append(LogEvent{
			Time:    r.Time,
			Level:   r.Level,
			Message: r.Message,
			Attrs:   attrs,
		})
	}
	return h.next.Handle(ctx, r)
}

func (h *bufferHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &bufferHandler{next: h.next.WithAttrs(attrs), buf: h.buf}
}

func (h *bufferHandler) WithGroup(name string) slog.Handler {
	return &bufferHandler{next: h.next.WithGroup(name), buf: h.buf}
}
