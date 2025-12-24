package admin

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/CodeBoy2006/EasyProxyPool/internal/config"
	"github.com/CodeBoy2006/EasyProxyPool/internal/orchestrator"
	"github.com/CodeBoy2006/EasyProxyPool/internal/pool"
)

type Options struct {
	Auth      config.AdminAuthConfig
	StartedAt time.Time
}

type Server struct {
	log *slog.Logger
	srv *http.Server

	status      *orchestrator.Status
	strictPool  *pool.Pool
	relaxedPool *pool.Pool

	auth      config.AdminAuthConfig
	startedAt time.Time
}

func New(log *slog.Logger, addr string, status *orchestrator.Status, strictPool, relaxedPool *pool.Pool, opt Options) *Server {
	s := &Server{
		log:         log,
		status:      status,
		strictPool:  strictPool,
		relaxedPool: relaxedPool,
		auth:        opt.Auth,
		startedAt:   opt.StartedAt,
	}

	mux := http.NewServeMux()
	mux.Handle("/healthz", s.wrapAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})))
	mux.Handle("/status", s.wrapAuth(http.HandlerFunc(s.handleStatus)))

	mux.Handle("/api/status", s.wrapAuth(http.HandlerFunc(s.handleStatus)))
	mux.Handle("/api/info", s.wrapAuth(http.HandlerFunc(s.handleInfo)))
	mux.Handle("/api/nodes", s.wrapAuth(http.HandlerFunc(s.handleNodes)))

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

	writeJSON(w, resp)
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	uptime := time.Duration(0)
	startedAt := ""
	if !s.startedAt.IsZero() {
		uptime = now.Sub(s.startedAt)
		startedAt = s.startedAt.UTC().Format(time.RFC3339)
	}

	info := map[string]any{
		"server_time_utc": now.UTC().Format(time.RFC3339),
		"started_at_utc":  startedAt,
		"uptime_seconds":  int64(uptime.Seconds()),
		"go_version":      runtime.Version(),
		"build":           buildInfo(),
		"admin_auth_mode": strings.ToLower(strings.TrimSpace(s.auth.Mode)),
	}
	writeJSON(w, info)
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	// Placeholder: node health snapshot is added in WEBUI-020.
	resp := map[string]any{
		"nodes":           []any{},
		"nodes_total":     0,
		"nodes_alive":     0,
		"server_time_utc": now.UTC().Format(time.RFC3339),
	}
	writeJSON(w, resp)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func buildInfo() map[string]any {
	bi, ok := debug.ReadBuildInfo()
	if !ok || bi == nil {
		return map[string]any{}
	}
	out := map[string]any{
		"path":    bi.Path,
		"main":    bi.Main.Path,
		"version": bi.Main.Version,
		"sum":     bi.Main.Sum,
	}
	return out
}

func (s *Server) wrapAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mode := strings.ToLower(strings.TrimSpace(s.auth.Mode))
		if mode == "" || mode == "disabled" {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL != nil && r.URL.Path == "/healthz" {
			if s.auth.AllowUnauthenticatedHealthz == nil || *s.auth.AllowUnauthenticatedHealthz {
				next.ServeHTTP(w, r)
				return
			}
		}

		switch mode {
		case "basic":
			user, pass, ok := r.BasicAuth()
			if !ok || user != s.auth.Username || pass != s.auth.Password {
				w.Header().Set("WWW-Authenticate", `Basic realm="EasyProxyPool Admin"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
			return
		case "shared_token":
			token := ""
			if ah := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(strings.ToLower(ah), "bearer ") {
				token = strings.TrimSpace(ah[len("bearer "):])
			}
			if token == "" && r.URL != nil {
				token = strings.TrimSpace(r.URL.Query().Get("token"))
			}
			if token == "" {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			if subtle.ConstantTimeCompare([]byte(token), []byte(s.auth.Token)) != 1 {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
			return
		default:
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	})
}
