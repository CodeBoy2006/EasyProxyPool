package httpproxy

import (
	"container/list"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/CodeBoy2006/EasyProxyPool/internal/config"
)

const (
	headerSticky   = "X-EasyProxyPool-Sticky"
	headerFailover = "X-EasyProxyPool-Failover"
	headerUpstream = "X-EasyProxyPool-Upstream"
)

type stickyMap struct {
	ttl        time.Duration
	maxEntries int

	mu    sync.Mutex
	items map[string]*list.Element
	lru   list.List // front = most recently accessed
}

type stickyItem struct {
	traceID     string
	upstreamKey string
	expiresAt   time.Time
}

func newStickyMap(ttl time.Duration, maxEntries int) *stickyMap {
	return &stickyMap{
		ttl:        ttl,
		maxEntries: maxEntries,
		items:      make(map[string]*list.Element, maxEntries),
	}
}

func (m *stickyMap) Get(traceID string, now time.Time) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	el, ok := m.items[traceID]
	if !ok {
		return "", false
	}
	it := el.Value.(stickyItem)
	if !it.expiresAt.IsZero() && !now.Before(it.expiresAt) {
		m.lru.Remove(el)
		delete(m.items, traceID)
		return "", false
	}
	m.lru.MoveToFront(el)
	return it.upstreamKey, true
}

func (m *stickyMap) Set(traceID, upstreamKey string, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	expiresAt := now.Add(m.ttl)
	if el, ok := m.items[traceID]; ok {
		el.Value = stickyItem{traceID: traceID, upstreamKey: upstreamKey, expiresAt: expiresAt}
		m.lru.MoveToFront(el)
	} else {
		m.items[traceID] = m.lru.PushFront(stickyItem{traceID: traceID, upstreamKey: upstreamKey, expiresAt: expiresAt})
	}

	for len(m.items) > m.maxEntries {
		back := m.lru.Back()
		if back == nil {
			break
		}
		it := back.Value.(stickyItem)
		m.lru.Remove(back)
		delete(m.items, it.traceID)
	}
}

func (m *stickyMap) Delete(traceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	el, ok := m.items[traceID]
	if !ok {
		return
	}
	m.lru.Remove(el)
	delete(m.items, traceID)
}

type requestStickyPolicy struct {
	traceID     string
	forceSticky *bool
	failover    string
	forceKey    string
}

func parseTraceIDFromTraceparent(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	if idx := strings.IndexByte(v, ','); idx >= 0 {
		v = strings.TrimSpace(v[:idx])
	}
	parts := strings.Split(v, "-")
	if len(parts) != 4 {
		return "", false
	}
	traceID := strings.TrimSpace(parts[1])
	if len(traceID) != 32 {
		return "", false
	}
	allZero := true
	for _, c := range traceID {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return "", false
		}
		if c != '0' {
			allZero = false
		}
	}
	if allZero {
		return "", false
	}
	return strings.ToLower(traceID), true
}

func parseBoolLike(v string) (bool, bool) {
	v = strings.TrimSpace(strings.ToLower(v))
	switch v {
	case "1", "true", "t", "yes", "y", "on", "enable", "enabled":
		return true, true
	case "0", "false", "f", "no", "n", "off", "disable", "disabled":
		return false, true
	default:
		return false, false
	}
}

func stickyPolicyFromRequest(sel config.SelectionConfig, r *http.Request) (requestStickyPolicy, error) {
	p := requestStickyPolicy{
		failover: sel.Sticky.Failover,
	}

	traceID, ok := parseTraceIDFromTraceparent(r.Header.Get("traceparent"))
	if ok {
		p.traceID = traceID
	}

	headerOverride := sel.Sticky.HeaderOverride == nil || *sel.Sticky.HeaderOverride
	if !headerOverride {
		return p, nil
	}

	if v := strings.TrimSpace(r.Header.Get(headerUpstream)); v != "" {
		p.forceKey = v
	}

	if v := strings.TrimSpace(r.Header.Get(headerSticky)); v != "" {
		b, ok := parseBoolLike(v)
		if !ok {
			return requestStickyPolicy{}, errors.New("invalid X-EasyProxyPool-Sticky (use on/off)")
		}
		p.forceSticky = &b
	}

	if v := strings.TrimSpace(r.Header.Get(headerFailover)); v != "" {
		v = strings.ToLower(v)
		switch v {
		case "soft", "hard":
			p.failover = v
		default:
			return requestStickyPolicy{}, errors.New("invalid X-EasyProxyPool-Failover (use soft/hard)")
		}
	}

	return p, nil
}
