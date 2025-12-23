package socks5proxy

import (
	"context"
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/CodeBoy2006/EasyProxyPool/internal/config"
	"github.com/CodeBoy2006/EasyProxyPool/internal/pool"
	"github.com/armon/go-socks5"
	"golang.org/x/net/proxy"
)

type Mode string

const (
	ModeStrict  Mode = "STRICT"
	ModeRelaxed Mode = "RELAXED"
)

type Server struct {
	log *slog.Logger

	addr      string
	mode      Mode
	pool      *pool.Pool
	auth      config.AuthConfig
	selection config.SelectionConfig

	srv *socks5.Server
	ln  net.Listener
}

func New(log *slog.Logger, addr string, mode Mode, p *pool.Pool, auth config.AuthConfig, sel config.SelectionConfig) *Server {
	return &Server{
		log:       log.With("component", "socks5", "mode", string(mode)),
		addr:      addr,
		mode:      mode,
		pool:      p,
		auth:      auth,
		selection: sel,
	}
}

func (s *Server) Start(ctx context.Context) {
	conf := &socks5.Config{
		Dial: s.dial,
		Logger: func() *log.Logger {
			// Silence go-socks5 internal logs by default; slog handles our own logs.
			return log.New(io.Discard, "", 0)
		}(),
	}

	if strings.TrimSpace(s.auth.Username) != "" {
		creds := socks5.StaticCredentials{
			s.auth.Username: s.auth.Password,
		}
		conf.Credentials = creds
	}

	server, err := socks5.New(conf)
	if err != nil {
		s.log.Error("create server failed", "err", err)
		return
	}
	s.srv = server

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		s.log.Error("listen failed", "addr", s.addr, "err", err)
		return
	}
	s.ln = ln

	s.log.Info("listening", "addr", s.addr)
	go func() {
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, net.ErrClosed) {
			s.log.Error("serve error", "err", err)
		}
	}()

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
}

func (s *Server) Stop(ctx context.Context) {
	if s.ln != nil {
		_ = s.ln.Close()
	}
}

func (s *Server) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	var lastErr error
	for attempt := 0; attempt <= s.selection.Retries; attempt++ {
		entry, ok := s.pool.Next(s.selection.Strategy, time.Now())
		if !ok {
			return nil, errors.New("no upstreams available")
		}

		c, err := dialViaUpstream(ctx, entry.Addr, network, addr)
		if err != nil {
			lastErr = err
			s.pool.MarkFailure(entry.Addr, time.Now(), time.Duration(s.selection.FailureBackoffSeconds)*time.Second, time.Duration(s.selection.MaxBackoffSeconds)*time.Second)
			continue
		}
		s.pool.MarkSuccess(entry.Addr)
		return c, nil
	}
	return nil, lastErr
}

func dialViaUpstream(ctx context.Context, upstreamAddr, network, addr string) (net.Conn, error) {
	dialer, err := proxy.SOCKS5("tcp", upstreamAddr, nil, proxy.Direct)
	if err != nil {
		return nil, err
	}

	type result struct {
		conn net.Conn
		err  error
	}
	done := make(chan result, 1)
	go func() {
		c, err := dialer.Dial(network, addr)
		done <- result{conn: c, err: err}
	}()

	select {
	case res := <-done:
		return res.conn, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
