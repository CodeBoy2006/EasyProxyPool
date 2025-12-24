package admin

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/CodeBoy2006/EasyProxyPool/internal/config"
	"github.com/CodeBoy2006/EasyProxyPool/internal/logging"
	"github.com/CodeBoy2006/EasyProxyPool/internal/orchestrator"
	"github.com/CodeBoy2006/EasyProxyPool/internal/pool"
)

type Options struct {
	Auth      config.AdminAuthConfig
	StartedAt time.Time

	LogBuffer     *logging.LogBuffer
	MaxSSEClients int
}

type Server struct {
	log *slog.Logger
	srv *http.Server

	status      *orchestrator.Status
	strictPool  *pool.Pool
	relaxedPool *pool.Pool

	auth      config.AdminAuthConfig
	startedAt time.Time

	logBuf *logging.LogBuffer
	sseSem chan struct{}
}

func New(log *slog.Logger, addr string, status *orchestrator.Status, strictPool, relaxedPool *pool.Pool, opt Options) *Server {
	maxSSE := opt.MaxSSEClients
	if maxSSE <= 0 {
		maxSSE = 10
	}
	s := &Server{
		log:         log,
		status:      status,
		strictPool:  strictPool,
		relaxedPool: relaxedPool,
		auth:        opt.Auth,
		startedAt:   opt.StartedAt,
		logBuf:      opt.LogBuffer,
		sseSem:      make(chan struct{}, maxSSE),
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
	mux.Handle("/api/events/logs", s.wrapAuth(http.HandlerFunc(s.handleLogsSSE)))

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

	h, updatedAt := s.status.RelaxedNodeHealthSnapshot()

	type node struct {
		ID          string `json:"id"`
		Alive       bool   `json:"alive"`
		DelayMS     int64  `json:"delay_ms"`
		LastSeenUTC string `json:"last_seen_utc"`
		LastTryUTC  string `json:"last_try_utc"`
	}

	nodes := make([]node, 0, len(h))
	alive := 0
	for id, nh := range h {
		if nh.Alive {
			alive++
		}
		lastSeen := ""
		if !nh.LastSeen.IsZero() {
			lastSeen = nh.LastSeen.UTC().Format(time.RFC3339)
		}
		lastTry := ""
		if !nh.LastTry.IsZero() {
			lastTry = nh.LastTry.UTC().Format(time.RFC3339)
		}
		nodes = append(nodes, node{
			ID:          id,
			Alive:       nh.Alive,
			DelayMS:     int64(nh.Delay / time.Millisecond),
			LastSeenUTC: lastSeen,
			LastTryUTC:  lastTry,
		})
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Alive != nodes[j].Alive {
			return nodes[i].Alive
		}
		if nodes[i].DelayMS != nodes[j].DelayMS {
			return nodes[i].DelayMS < nodes[j].DelayMS
		}
		return nodes[i].ID < nodes[j].ID
	})

	updatedAtUTC := ""
	if !updatedAt.IsZero() {
		updatedAtUTC = updatedAt.UTC().Format(time.RFC3339)
	}

	resp := map[string]any{
		"nodes":           nodes,
		"nodes_total":     len(nodes),
		"nodes_alive":     alive,
		"updated_at_utc":  updatedAtUTC,
		"server_time_utc": now.UTC().Format(time.RFC3339),
	}
	writeJSON(w, resp)
}

func (s *Server) handleLogsSSE(w http.ResponseWriter, r *http.Request) {
	select {
	case s.sseSem <- struct{}{}:
		defer func() { <-s.sseSem }()
	default:
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}
	if s.logBuf == nil {
		http.Error(w, "Log buffer disabled", http.StatusServiceUnavailable)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	q := r.URL.Query()
	since := uint64(0)
	if v := strings.TrimSpace(q.Get("since")); v != "" {
		if parsed, err := strconv.ParseUint(v, 10, 64); err == nil {
			since = parsed
		}
	}
	minLevel := parseSlogLevel(q.Get("level"))

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	for _, e := range s.logBuf.SnapshotSince(since, minLevel) {
		if err := writeSSELog(w, e); err != nil {
			return
		}
		flusher.Flush()
		since = e.ID
	}

	ch, cancel := s.logBuf.Subscribe(256)
	defer cancel()

	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			if e.ID <= since || e.Level < minLevel {
				continue
			}
			if err := writeSSELog(w, e); err != nil {
				return
			}
			flusher.Flush()
			since = e.ID
		}
	}
}

func writeSSELog(w http.ResponseWriter, e logging.LogEvent) error {
	payload := map[string]any{
		"id":       e.ID,
		"time_utc": e.Time.UTC().Format(time.RFC3339Nano),
		"level":    strings.ToLower(e.Level.String()),
		"msg":      e.Message,
		"attrs":    e.Attrs,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: log\ndata: %s\n\n", e.ID, data)
	return err
}

func parseSlogLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "info", "":
		return slog.LevelInfo
	default:
		return slog.LevelInfo
	}
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
