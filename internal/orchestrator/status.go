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

	LastDetails UpdateDetails
}

type UpdateDetails struct {
	Adapter string

	NodesTotal    int
	NodesIncluded int

	ProblemsCount int
	SkippedByType map[string]int

	XrayStrictHash  string
	XrayRelaxedHash string
}

func NewStatus() *Status {
	return &Status{}
}

func (s *Status) SetStart(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastUpdateStart = t
}

func (s *Status) SetEnd(t time.Time, fetched, strict, relaxed int, err error, details UpdateDetails) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastUpdateEnd = t
	s.LastFetched = fetched
	s.LastStrict = strict
	s.LastRelaxed = relaxed
	s.LastDetails = cloneDetails(details)
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
		LastDetails:     cloneDetails(s.LastDetails),
	}
}

func cloneDetails(d UpdateDetails) UpdateDetails {
	if d.SkippedByType == nil {
		return d
	}
	cp := make(map[string]int, len(d.SkippedByType))
	for k, v := range d.SkippedByType {
		cp[k] = v
	}
	d.SkippedByType = cp
	return d
}
