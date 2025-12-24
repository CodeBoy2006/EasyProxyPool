package httpproxy

import (
	"net/http"
	"testing"
	"time"

	"github.com/CodeBoy2006/EasyProxyPool/internal/config"
)

func TestParseTraceIDFromTraceparent(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		traceID, ok := parseTraceIDFromTraceparent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00")
		if !ok {
			t.Fatalf("expected ok")
		}
		if traceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
			t.Fatalf("unexpected traceID: %q", traceID)
		}
	})

	t.Run("invalid_all_zero", func(t *testing.T) {
		_, ok := parseTraceIDFromTraceparent("00-00000000000000000000000000000000-00f067aa0ba902b7-00")
		if ok {
			t.Fatalf("expected not ok")
		}
	})

	t.Run("invalid_format", func(t *testing.T) {
		_, ok := parseTraceIDFromTraceparent("not-a-traceparent")
		if ok {
			t.Fatalf("expected not ok")
		}
	})
}

func TestStickyMap_TTLAndEviction(t *testing.T) {
	m := newStickyMap(10*time.Second, 2)
	t0 := time.Unix(1000, 0)

	m.Set("t1", "u1", t0)
	m.Set("t2", "u2", t0)

	if got, ok := m.Get("t1", t0.Add(5*time.Second)); !ok || got != "u1" {
		t.Fatalf("expected t1=u1, got %q ok=%v", got, ok)
	}

	// Touch t1 so t2 becomes least-recently-used.
	_, _ = m.Get("t1", t0.Add(6*time.Second))

	m.Set("t3", "u3", t0.Add(7*time.Second))

	if _, ok := m.Get("t2", t0.Add(7*time.Second)); ok {
		t.Fatalf("expected t2 to be evicted")
	}

	if got, ok := m.Get("t1", t0.Add(7*time.Second)); !ok || got != "u1" {
		t.Fatalf("expected t1=u1 still present, got %q ok=%v", got, ok)
	}

	if got, ok := m.Get("t3", t0.Add(7*time.Second)); !ok || got != "u3" {
		t.Fatalf("expected t3=u3, got %q ok=%v", got, ok)
	}

	if _, ok := m.Get("t1", t0.Add(11*time.Second)); ok {
		t.Fatalf("expected t1 to expire")
	}
}

func TestStickyPolicyFromRequest(t *testing.T) {
	truePtr := func() *bool { b := true; return &b }()
	falsePtr := func() *bool { b := false; return &b }()

	t.Run("override_disabled_ignores_headers", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "http://example.com", nil)
		req.Header.Set(headerSticky, "off")
		req.Header.Set(headerFailover, "hard")
		req.Header.Set(headerUpstream, "node-1")

		p, err := stickyPolicyFromRequest(config.SelectionConfig{
			Sticky: config.StickyConfig{
				HeaderOverride: falsePtr,
				Failover:       "soft",
			},
		}, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.forceSticky != nil || p.failover != "soft" || p.forceKey != "" {
			t.Fatalf("expected headers ignored, got %+v", p)
		}
	})

	t.Run("invalid_sticky_header", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "http://example.com", nil)
		req.Header.Set(headerSticky, "maybe")
		_, err := stickyPolicyFromRequest(config.SelectionConfig{
			Sticky: config.StickyConfig{
				HeaderOverride: truePtr,
				Failover:       "soft",
			},
		}, req)
		if err == nil {
			t.Fatalf("expected error")
		}
	})
}
