package pool

import (
	"log/slog"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

type Entry struct {
	Addr          string
	Latency       time.Duration
	LastCheckedAt time.Time

	failures      int
	disabledUntil time.Time
}

type Pool struct {
	name string
	log  *slog.Logger

	mu      sync.RWMutex
	entries []Entry
	index   map[string]int
	rr      uint64

	updating int32

	rng *rand.Rand
}

func New(name string, log *slog.Logger) *Pool {
	return &Pool{
		name:  name,
		log:   log,
		index: make(map[string]int),
		rng:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (p *Pool) Name() string { return p.name }

func (p *Pool) UpdatingCAS() bool {
	return atomic.CompareAndSwapInt32(&p.updating, 0, 1)
}

func (p *Pool) UpdatingClear() {
	atomic.StoreInt32(&p.updating, 0)
}

func (p *Pool) Update(entries []Entry) {
	p.mu.Lock()
	defer p.mu.Unlock()

	oldCount := len(p.entries)
	p.entries = entries
	p.index = make(map[string]int, len(entries))
	for i := range entries {
		p.index[entries[i].Addr] = i
	}
	atomic.StoreUint64(&p.rr, 0)

	p.log.Info("pool updated", "pool", p.name, "old", oldCount, "new", len(entries))
}

func (p *Pool) Next(strategy string, now time.Time) (Entry, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.entries) == 0 {
		return Entry{}, false
	}

	switch strategy {
	case "random":
		for i := 0; i < len(p.entries); i++ {
			e := p.entries[p.rng.Intn(len(p.entries))]
			if e.disabledUntil.IsZero() || now.After(e.disabledUntil) {
				return e, true
			}
		}
		return Entry{}, false
	default: // round_robin
		start := int(atomic.AddUint64(&p.rr, 1) % uint64(len(p.entries)))
		for i := 0; i < len(p.entries); i++ {
			idx := (start + i) % len(p.entries)
			e := p.entries[idx]
			if e.disabledUntil.IsZero() || now.After(e.disabledUntil) {
				return e, true
			}
		}
		return Entry{}, false
	}
}

func (p *Pool) MarkSuccess(addr string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	idx, ok := p.index[addr]
	if !ok {
		return
	}
	p.entries[idx].failures = 0
	p.entries[idx].disabledUntil = time.Time{}
}

func (p *Pool) MarkFailure(addr string, now time.Time, baseBackoff, maxBackoff time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	idx, ok := p.index[addr]
	if !ok {
		return
	}

	p.entries[idx].failures++
	failures := p.entries[idx].failures

	backoff := baseBackoff
	for i := 1; i < failures; i++ {
		backoff *= 2
		if backoff >= maxBackoff {
			backoff = maxBackoff
			break
		}
	}
	p.entries[idx].disabledUntil = now.Add(backoff)
}

type Stats struct {
	Total        int
	Disabled     int
	LastChecked  time.Time
	HasAnyActive bool
}

func (p *Pool) Stats(now time.Time) Stats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	disabled := 0
	active := 0
	var last time.Time
	for i := range p.entries {
		if p.entries[i].LastCheckedAt.After(last) {
			last = p.entries[i].LastCheckedAt
		}
		if !p.entries[i].disabledUntil.IsZero() && now.Before(p.entries[i].disabledUntil) {
			disabled++
		} else {
			active++
		}
	}
	return Stats{
		Total:        len(p.entries),
		Disabled:     disabled,
		LastChecked:  last,
		HasAnyActive: active > 0,
	}
}
