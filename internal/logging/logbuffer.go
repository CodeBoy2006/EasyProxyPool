package logging

import (
	"log/slog"
	"sync"
	"time"
)

type LogEvent struct {
	ID      uint64
	Time    time.Time
	Level   slog.Level
	Message string
	Attrs   map[string]string
}

type LogBuffer struct {
	mu sync.Mutex

	capacity int
	start    int
	size     int
	events   []LogEvent

	nextID uint64

	subs      map[uint64]chan LogEvent
	nextSubID uint64
}

func NewLogBuffer(lines int) *LogBuffer {
	if lines <= 0 {
		lines = 0
	}
	b := &LogBuffer{
		capacity: lines,
	}
	if lines > 0 {
		b.events = make([]LogEvent, lines)
	}
	return b
}

func (b *LogBuffer) Append(e LogEvent) LogEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++
	e.ID = b.nextID

	if b.capacity > 0 {
		idx := (b.start + b.size) % b.capacity
		if b.size < b.capacity {
			b.size++
		} else {
			b.start = (b.start + 1) % b.capacity
		}
		b.events[idx] = e
	}

	for id, ch := range b.subs {
		select {
		case ch <- e:
		default:
			close(ch)
			delete(b.subs, id)
		}
	}
	return e
}

func (b *LogBuffer) SnapshotSince(since uint64, minLevel slog.Level) []LogEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make([]LogEvent, 0, b.size)
	if b.capacity == 0 || b.size == 0 {
		return out
	}
	for i := 0; i < b.size; i++ {
		idx := (b.start + i) % b.capacity
		e := b.events[idx]
		if e.ID <= since {
			continue
		}
		if e.Level < minLevel {
			continue
		}
		out = append(out, e)
	}
	return out
}

func (b *LogBuffer) Subscribe(buffer int) (<-chan LogEvent, func()) {
	if buffer <= 0 {
		buffer = 256
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.subs == nil {
		b.subs = make(map[uint64]chan LogEvent)
	}
	b.nextSubID++
	id := b.nextSubID
	ch := make(chan LogEvent, buffer)
	b.subs[id] = ch

	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if c, ok := b.subs[id]; ok {
			close(c)
			delete(b.subs, id)
		}
	}
	return ch, cancel
}
