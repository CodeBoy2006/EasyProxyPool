package orchestrator

import (
	"sync"
	"time"

	"github.com/CodeBoy2006/EasyProxyPool/internal/xray"
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

	LastNodeHealthRelaxedAt time.Time
	LastNodeHealthRelaxed   map[string]xray.NodeHealth
}

type UpdateDetails struct {
	Adapter string

	NodesTotal    int
	NodesIncluded int

	ProblemsCount int
	SkippedByType map[string]int

	XrayStrictHash  string
	XrayRelaxedHash string

	FallbackUsed bool
	FallbackErr  string
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

		LastNodeHealthRelaxedAt: s.LastNodeHealthRelaxedAt,
		LastNodeHealthRelaxed:   cloneNodeHealth(s.LastNodeHealthRelaxed),
	}
}

func (s *Status) SetRelaxedNodeHealth(t time.Time, h map[string]xray.NodeHealth) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastNodeHealthRelaxedAt = t
	s.LastNodeHealthRelaxed = cloneNodeHealth(h)
}

func (s *Status) RelaxedNodeHealthSnapshot() (map[string]xray.NodeHealth, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneNodeHealth(s.LastNodeHealthRelaxed), s.LastNodeHealthRelaxedAt
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

func cloneNodeHealth(h map[string]xray.NodeHealth) map[string]xray.NodeHealth {
	if h == nil {
		return nil
	}
	cp := make(map[string]xray.NodeHealth, len(h))
	for k, v := range h {
		cp[k] = v
	}
	return cp
}
