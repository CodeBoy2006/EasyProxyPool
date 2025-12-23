package orchestrator

import (
	"sync"
	"time"
)

type Status struct {
	mu sync.RWMutex

	LastUpdateStart time.Time
	LastUpdateEnd   time.Time
	LastUpdateErr   string
	LastFetched     int
	LastStrict      int
	LastRelaxed     int
}

func NewStatus() *Status {
	return &Status{}
}

func (s *Status) SetStart(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastUpdateStart = t
}

func (s *Status) SetEnd(t time.Time, fetched, strict, relaxed int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastUpdateEnd = t
	s.LastFetched = fetched
	s.LastStrict = strict
	s.LastRelaxed = relaxed
	if err != nil {
		s.LastUpdateErr = err.Error()
	} else {
		s.LastUpdateErr = ""
	}
}

func (s *Status) Snapshot() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Status{
		LastUpdateStart: s.LastUpdateStart,
		LastUpdateEnd:   s.LastUpdateEnd,
		LastUpdateErr:   s.LastUpdateErr,
		LastFetched:     s.LastFetched,
		LastStrict:      s.LastStrict,
		LastRelaxed:     s.LastRelaxed,
	}
}
