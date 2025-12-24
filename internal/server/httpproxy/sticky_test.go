package httpproxy

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CodeBoy2006/EasyProxyPool/internal/config"
	"github.com/CodeBoy2006/EasyProxyPool/internal/pool"
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

func TestStickyPolicyFromRequest(t *testing.T) {
	truePtr := func() *bool { b := true; return &b }()
	falsePtr := func() *bool { b := false; return &b }()

	t.Run("override_disabled_ignores_override_headers", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "http://example.com", nil)
		req.Header.Set(headerSticky, "off")
		req.Header.Set(headerFailover, "hard")
		req.Header.Set(headerUpstream, "node-1")
		req.Header.Set(headerSession, "hdr-1")

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
			t.Fatalf("expected override headers ignored, got %+v", p)
		}
		if p.sessionKey == "hdr-1" {
			t.Fatalf("expected session header to be ignored when header_override=false")
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

	t.Run("session_key_from_proxy_auth_username", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "http://example.com", nil)
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("sess-1:pw")))
		p, err := stickyPolicyFromRequest(config.SelectionConfig{
			Sticky: config.StickyConfig{
				HeaderOverride: truePtr,
				Failover:       "soft",
			},
		}, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.sessionKey != "sess-1" {
			t.Fatalf("unexpected sessionKey: %q", p.sessionKey)
		}
	})

	t.Run("session_key_from_header", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "http://example.com", nil)
		req.Header.Set(headerSession, "hdr-2")
		p, err := stickyPolicyFromRequest(config.SelectionConfig{
			Sticky: config.StickyConfig{
				HeaderOverride: truePtr,
				Failover:       "soft",
			},
		}, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.sessionKey != "hdr-2" {
			t.Fatalf("unexpected sessionKey: %q", p.sessionKey)
		}
	})
}

func TestPickRendezvous_StableAndExclude(t *testing.T) {
	candidates := []pool.Entry{
		{ID: "n1"},
		{ID: "n2"},
		{ID: "n3"},
	}

	best1, ok := pickRendezvous(candidates, "sess", nil)
	if !ok {
		t.Fatalf("expected ok")
	}
	best2, ok := pickRendezvous(candidates, "sess", nil)
	if !ok {
		t.Fatalf("expected ok")
	}
	if best1.Key() != best2.Key() {
		t.Fatalf("expected stable pick, got %q vs %q", best1.Key(), best2.Key())
	}

	exclude := map[string]struct{}{best1.Key(): {}}
	second, ok := pickRendezvous(candidates, "sess", exclude)
	if !ok {
		t.Fatalf("expected ok")
	}
	if second.Key() == best1.Key() {
		t.Fatalf("expected different pick when excluding best")
	}
}

func TestAuthorizeHTTP_SharedPassword(t *testing.T) {
	truePtr := func() *bool { b := true; return &b }()
	s := &Server{
		auth: config.AuthConfig{
			Mode:     "shared_password",
			Password: "p",
		},
		selection: config.SelectionConfig{
			Sticky: config.StickyConfig{
				HeaderOverride: truePtr,
				Failover:       "soft",
			},
		},
	}

	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("anyuser:p")))
	rr := httptest.NewRecorder()
	if ok := s.authorizeHTTP(rr, req); !ok {
		t.Fatalf("expected authorized")
	}
}

func TestAuthorizeHTTP_Basic(t *testing.T) {
	s := &Server{
		auth: config.AuthConfig{
			Mode:     "basic",
			Username: "u",
			Password: "p",
		},
	}

	okReq, _ := http.NewRequest("GET", "http://example.com", nil)
	okReq.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("u:p")))
	rr := httptest.NewRecorder()
	if ok := s.authorizeHTTP(rr, okReq); !ok {
		t.Fatalf("expected authorized")
	}

	badReq, _ := http.NewRequest("GET", "http://example.com", nil)
	badReq.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("u:bad")))
	rr = httptest.NewRecorder()
	if ok := s.authorizeHTTP(rr, badReq); ok {
		t.Fatalf("expected unauthorized")
	}
	if rr.Code != http.StatusProxyAuthRequired {
		t.Fatalf("expected 407, got %d", rr.Code)
	}
}

func TestAuthorizeHTTP_Disabled(t *testing.T) {
	s := &Server{
		auth: config.AuthConfig{
			Mode: "disabled",
		},
	}
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	rr := httptest.NewRecorder()
	if ok := s.authorizeHTTP(rr, req); !ok {
		t.Fatalf("expected authorized")
	}
}
