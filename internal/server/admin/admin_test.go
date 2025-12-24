package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/CodeBoy2006/EasyProxyPool/internal/config"
	"github.com/CodeBoy2006/EasyProxyPool/internal/logging"
	"github.com/CodeBoy2006/EasyProxyPool/internal/orchestrator"
	"github.com/CodeBoy2006/EasyProxyPool/internal/pool"
	"github.com/CodeBoy2006/EasyProxyPool/internal/xray"
)

type sseRecorder struct {
	h    http.Header
	code int
	buf  bytes.Buffer
}

func (r *sseRecorder) Header() http.Header {
	if r.h == nil {
		r.h = make(http.Header)
	}
	return r.h
}

func (r *sseRecorder) WriteHeader(statusCode int) { r.code = statusCode }

func (r *sseRecorder) Write(p []byte) (int, error) {
	if r.code == 0 {
		r.code = http.StatusOK
	}
	return r.buf.Write(p)
}

func (r *sseRecorder) Flush() {}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestAdminAuth_SharedToken(t *testing.T) {
	log := newTestLogger()
	status := orchestrator.NewStatus()
	p := pool.New("pool", log)
	buf := logging.NewLogBuffer(10)

	s := New(log, ":0", status, p, p, Options{
		Auth: config.AdminAuthConfig{
			Mode:  "shared_token",
			Token: "t",
		},
		StartedAt:     time.Unix(0, 0),
		UIEnabled:     true,
		LogBuffer:     buf,
		MaxSSEClients: 1,
	})

	h := s.srv.Handler

	// healthz can be unauthenticated by default
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example/healthz", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /healthz, got %d", rec.Code)
	}

	// api/info requires auth
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "http://example/api/info", nil)
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for /api/info, got %d", rec2.Code)
	}

	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "http://example/api/info", nil)
	req3.Header.Set("Authorization", "Bearer t")
	h.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/info with token, got %d", rec3.Code)
	}
}

func TestAdminNodesEndpoint_UsesStatusSnapshot(t *testing.T) {
	log := newTestLogger()
	status := orchestrator.NewStatus()
	p := pool.New("pool", log)

	status.SetRelaxedNodeHealth(time.Unix(10, 0), map[string]xray.NodeHealth{
		"n1": {Alive: true, Delay: 120 * time.Millisecond, LastSeen: time.Unix(11, 0), LastTry: time.Unix(12, 0)},
		"n2": {Alive: false, Delay: 0},
	})

	s := New(log, ":0", status, p, p, Options{
		Auth: config.AdminAuthConfig{
			Mode:  "shared_token",
			Token: "t",
		},
		UIEnabled: true,
		LogBuffer: logging.NewLogBuffer(10),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example/api/nodes?token=t", nil)
	s.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/nodes, got %d", rec.Code)
	}

	var parsed struct {
		Nodes      []map[string]any `json:"nodes"`
		NodesTotal int              `json:"nodes_total"`
		NodesAlive int              `json:"nodes_alive"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if parsed.NodesTotal != 2 || parsed.NodesAlive != 1 || len(parsed.Nodes) != 2 {
		t.Fatalf("unexpected nodes summary: %#v", parsed)
	}
}

func TestAdminSSE_ConnectionLimit429(t *testing.T) {
	log := newTestLogger()
	status := orchestrator.NewStatus()
	p := pool.New("pool", log)
	buf := logging.NewLogBuffer(10)
	buf.Append(logging.LogEvent{Time: time.Unix(1, 0), Level: slog.LevelInfo, Message: "hello"})

	s := New(log, ":0", status, p, p, Options{
		Auth: config.AdminAuthConfig{
			Mode:  "shared_token",
			Token: "t",
		},
		UIEnabled:     false,
		LogBuffer:     buf,
		MaxSSEClients: 1,
		SSEHeartbeat:  1 * time.Millisecond,
	})

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	req1 := httptest.NewRequest(http.MethodGet, "http://example/api/events/logs?token=t&since=0&level=info", nil).WithContext(ctx1)
	rec1 := &sseRecorder{}

	done1 := make(chan struct{})
	go func() {
		defer close(done1)
		s.srv.Handler.ServeHTTP(rec1, req1)
	}()

	// Wait for the first handler to start and write something.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if rec1.buf.Len() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if rec1.buf.Len() == 0 {
		cancel1()
		<-done1
		t.Fatalf("expected SSE output")
	}
	deadline2 := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline2) {
		if bytes.Contains(rec1.buf.Bytes(), []byte(": ping")) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !bytes.Contains(rec1.buf.Bytes(), []byte(": ping")) {
		cancel1()
		<-done1
		t.Fatalf("expected heartbeat ping")
	}

	// Second connection should be rejected.
	rec2 := &sseRecorder{}
	req2 := httptest.NewRequest(http.MethodGet, "http://example/api/events/logs?token=t&since=0&level=info", nil)
	s.srv.Handler.ServeHTTP(rec2, req2)
	if rec2.code != http.StatusTooManyRequests {
		cancel1()
		<-done1
		t.Fatalf("expected 429, got %d", rec2.code)
	}

	cancel1()
	<-done1
}
