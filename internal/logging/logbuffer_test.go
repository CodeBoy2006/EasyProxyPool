package logging

import (
	"fmt"
	"log/slog"
	"testing"
	"time"
)

func TestLogBuffer_CapacityAndOrdering(t *testing.T) {
	b := NewLogBuffer(3)
	for i := 1; i <= 5; i++ {
		b.Append(LogEvent{
			Time:    time.Unix(int64(i), 0),
			Level:   slog.LevelInfo,
			Message: fmt.Sprintf("m%d", i),
		})
	}

	got := b.SnapshotSince(0, slog.LevelDebug)
	if len(got) != 3 {
		t.Fatalf("expected 3 events, got %d", len(got))
	}
	if got[0].Message != "m3" || got[1].Message != "m4" || got[2].Message != "m5" {
		t.Fatalf("unexpected ordering: %q %q %q", got[0].Message, got[1].Message, got[2].Message)
	}
	if got[0].ID != 3 || got[1].ID != 4 || got[2].ID != 5 {
		t.Fatalf("unexpected ids: %d %d %d", got[0].ID, got[1].ID, got[2].ID)
	}

	got2 := b.SnapshotSince(3, slog.LevelDebug)
	if len(got2) != 2 || got2[0].ID != 4 || got2[1].ID != 5 {
		t.Fatalf("unexpected since snapshot: %#v", got2)
	}
}

func TestLogBuffer_SubscribeSlowClientIsDropped(t *testing.T) {
	b := NewLogBuffer(10)
	ch, cancel := b.Subscribe(1)
	defer cancel()

	b.Append(LogEvent{Time: time.Now(), Level: slog.LevelInfo, Message: "m1"})
	b.Append(LogEvent{Time: time.Now(), Level: slog.LevelInfo, Message: "m2"})

	e1, ok := <-ch
	if !ok {
		t.Fatalf("expected first event")
	}
	if e1.Message != "m1" {
		t.Fatalf("unexpected first message: %q", e1.Message)
	}

	select {
	case _, ok2 := <-ch:
		if ok2 {
			t.Fatalf("expected channel to be closed due to slow consumer")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected channel close")
	}
}

func TestLogBuffer_SnapshotFiltersByLevel(t *testing.T) {
	b := NewLogBuffer(10)
	b.Append(LogEvent{Time: time.Now(), Level: slog.LevelDebug, Message: "d"})
	b.Append(LogEvent{Time: time.Now(), Level: slog.LevelInfo, Message: "i"})
	b.Append(LogEvent{Time: time.Now(), Level: slog.LevelWarn, Message: "w"})

	got := b.SnapshotSince(0, slog.LevelWarn)
	if len(got) != 1 || got[0].Message != "w" {
		t.Fatalf("unexpected snapshot: %#v", got)
	}
}
