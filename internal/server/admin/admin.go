package admin

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/CodeBoy2006/EasyProxyPool/internal/orchestrator"
	"github.com/CodeBoy2006/EasyProxyPool/internal/pool"
)

type Server struct {
	log *slog.Logger
	srv *http.Server

	status      *orchestrator.Status
	strictPool  *pool.Pool
	relaxedPool *pool.Pool
}

func New(log *slog.Logger, addr string, status *orchestrator.Status, strictPool, relaxedPool *pool.Pool) *Server {
	s := &Server{
		log:         log,
		status:      status,
		strictPool:  strictPool,
		relaxedPool: relaxedPool,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/status", s.handleStatus)

	s.srv = &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	return s
}

func (s *Server) Start(ctx context.Context) {
	s.log.Info("admin listening", "addr", s.srv.Addr)
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.Error("admin server error", "err", err)
		}
	}()

	go func() {
		<-ctx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(ctx)
	}()
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	resp := map[string]any{
		"updater":         s.status.Snapshot(),
		"strict_pool":     s.strictPool.Stats(now),
		"relaxed_pool":    s.relaxedPool.Stats(now),
		"server_time_utc": now.UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(resp)
}
