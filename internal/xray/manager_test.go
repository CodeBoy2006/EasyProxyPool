package xray

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestInstance_Ensure_RestartsOnHashChange(t *testing.T) {
	socksAddr := allocAddr(t)
	metricsAddr := allocAddr(t)

	var starts atomic.Int32
	r := &fakeRunner{
		socksListen:   socksAddr,
		metricsListen: metricsAddr,
		starts:        &starts,
	}

	inst := NewInstance(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		ModeStrict,
		"/bin/false",
		t.TempDir(),
		socksAddr,
		metricsAddr,
		2*time.Second,
		r,
	)

	if err := inst.Ensure(context.Background(), []byte(`{}`), "h1"); err != nil {
		t.Fatalf("ensure1: %v", err)
	}
	if got := starts.Load(); got != 1 {
		t.Fatalf("expected starts=1, got %d", got)
	}

	// Same hash should not restart.
	if err := inst.Ensure(context.Background(), []byte(`{}`), "h1"); err != nil {
		t.Fatalf("ensure2: %v", err)
	}
	if got := starts.Load(); got != 1 {
		t.Fatalf("expected starts=1 after no-op ensure, got %d", got)
	}

	// Different hash triggers restart.
	if err := inst.Ensure(context.Background(), []byte(`{}`), "h2"); err != nil {
		t.Fatalf("ensure3: %v", err)
	}
	if got := starts.Load(); got != 2 {
		t.Fatalf("expected starts=2 after restart, got %d", got)
	}

	_ = inst.Stop(context.Background())
}

func allocAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

type fakeRunner struct {
	socksListen   string
	metricsListen string
	starts        *atomic.Int32
}

func (r *fakeRunner) Start(ctx context.Context, binaryPath string, args []string, workDir string, stdout, stderr io.Writer) (Process, error) {
	r.starts.Add(1)

	socksLn, err := net.Listen("tcp", r.socksListen)
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			c, err := socksLn.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/vars", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	srv := &http.Server{
		Addr:    r.metricsListen,
		Handler: mux,
	}
	go func() { _ = srv.ListenAndServe() }()

	done := make(chan struct{})
	stopOnce := func() {
		_ = socksLn.Close()
		_ = srv.Close()
		select {
		case <-done:
		default:
			close(done)
		}
	}

	return &fakeProcess{stop: stopOnce, done: done}, nil
}

type fakeProcess struct {
	stop func()
	done chan struct{}
}

func (p *fakeProcess) Wait() error {
	<-p.done
	return nil
}

func (p *fakeProcess) Kill() error {
	p.stop()
	return nil
}

