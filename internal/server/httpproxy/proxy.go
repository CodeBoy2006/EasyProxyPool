package httpproxy

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/CodeBoy2006/EasyProxyPool/internal/config"
	"github.com/CodeBoy2006/EasyProxyPool/internal/pool"
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
	sticky    *stickyMap

	client *http.Client

	srv *http.Server
}

type upstreamContextKey struct{}

func New(log *slog.Logger, addr string, mode Mode, p *pool.Pool, auth config.AuthConfig, sel config.SelectionConfig) *Server {
	s := &Server{
		log:       log.With("component", "http", "mode", string(mode)),
		addr:      addr,
		mode:      mode,
		pool:      p,
		auth:      auth,
		selection: sel,
	}
	if sel.Sticky.Enabled {
		s.sticky = newStickyMap(time.Duration(sel.Sticky.TTLSeconds)*time.Second, sel.Sticky.MaxEntries)
	}

	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			upstream, _ := ctx.Value(upstreamContextKey{}).(pool.Entry)
			if strings.TrimSpace(upstream.Addr) == "" {
				return nil, errors.New("missing upstream in context")
			}
			return dialThroughSOCKS5(ctx, upstream, network, addr)
		},
		ForceAttemptHTTP2: true,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: mode == ModeRelaxed,
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}

	s.client = &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	s.srv = &http.Server{
		Addr:    addr,
		Handler: http.HandlerFunc(s.handle),
	}
	return s
}

func (s *Server) Start(ctx context.Context) {
	s.log.Info("listening", "addr", s.addr)
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.Error("server error", "err", err)
		}
	}()

	go func() {
		<-ctx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(ctx)
	}()
}

func (s *Server) Stop(ctx context.Context) { _ = s.srv.Shutdown(ctx) }

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeHTTP(w, r) {
		return
	}

	start := time.Now()
	if r.Method == http.MethodConnect {
		err := s.handleConnect(w, r)
		if err != nil {
			s.log.Debug("connect failed", "host", r.Host, "err", err)
		}
		return
	}

	status, err := s.handleForward(w, r)
	s.log.Debug("request",
		"method", r.Method,
		"url", safeURL(r),
		"status", status,
		"took", time.Since(start).String(),
		"err", err,
	)
}

func safeURL(r *http.Request) string {
	if r.URL == nil {
		return ""
	}
	u := r.URL.String()
	if len(u) > 200 {
		return u[:197] + "..."
	}
	return u
}

func (s *Server) authorizeHTTP(w http.ResponseWriter, r *http.Request) bool {
	if strings.TrimSpace(s.auth.Username) == "" {
		return true
	}

	v := r.Header.Get("Proxy-Authorization")
	user, pass, ok := parseBasicAuth(v)
	if !ok || user != s.auth.Username || pass != s.auth.Password {
		w.Header().Set("Proxy-Authenticate", `Basic realm="EasyProxyPool"`)
		http.Error(w, "Proxy authentication required", http.StatusProxyAuthRequired)
		return false
	}
	return true
}

func parseBasicAuth(v string) (string, string, bool) {
	const prefix = "Basic "
	if !strings.HasPrefix(v, prefix) {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(v, prefix)))
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) error {
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return errors.New("hijacking not supported")
	}

	policy, err := stickyPolicyFromRequest(s.selection, r)
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return err
	}

	stickyEnabled := s.selection.Sticky.Enabled && s.sticky != nil && policy.traceID != ""
	if policy.forceSticky != nil {
		stickyEnabled = *policy.forceSticky && s.sticky != nil && policy.traceID != ""
	}

	clientConn, _, err := hj.Hijack()
	if err != nil {
		http.Error(w, "Hijack failed", http.StatusInternalServerError)
		return err
	}
	defer clientConn.Close()

	target := r.Host
	if !strings.Contains(target, ":") {
		target += ":443"
	}

	now := time.Now()
	mappedKey := ""
	hasMapping := false
	if stickyEnabled {
		mappedKey, hasMapping = s.sticky.Get(policy.traceID, now)
	}

	var lastErr error
	for attempt := 0; attempt <= s.selection.Retries; attempt++ {
		now = time.Now()
		var entry pool.Entry
		var ok bool
		if strings.TrimSpace(policy.forceKey) != "" {
			entry, ok = s.pool.Get(policy.forceKey, now)
			if !ok {
				http.Error(w, "Unknown upstream", http.StatusBadRequest)
				return errors.New("unknown upstream")
			}
		} else if stickyEnabled {
			if hasMapping {
				entry, ok = s.pool.Get(mappedKey, now)
				if !ok {
					entry, ok = s.pool.Next(s.selection.Strategy, now)
					if ok && policy.failover == "soft" {
						mappedKey = entry.Key()
						hasMapping = true
						s.sticky.Set(policy.traceID, mappedKey, now)
					}
				}
			} else {
				entry, ok = s.pool.Next(s.selection.Strategy, now)
				if ok {
					mappedKey = entry.Key()
					hasMapping = true
					s.sticky.Set(policy.traceID, mappedKey, now)
				}
			}
		} else {
			entry, ok = s.pool.Next(s.selection.Strategy, now)
		}
		if !ok {
			http.Error(w, "No available proxies", http.StatusServiceUnavailable)
			return errors.New("no upstreams available")
		}

		upstreamConn, err := dialThroughSOCKS5(r.Context(), entry, "tcp", target)
		if err != nil {
			lastErr = err
			s.pool.MarkFailure(entry.Key(), now, time.Duration(s.selection.FailureBackoffSeconds)*time.Second, time.Duration(s.selection.MaxBackoffSeconds)*time.Second)
			if stickyEnabled && policy.failover == "soft" && strings.TrimSpace(policy.forceKey) == "" {
				if replacement, ok := s.pool.Next(s.selection.Strategy, now); ok {
					mappedKey = replacement.Key()
					hasMapping = true
					s.sticky.Set(policy.traceID, mappedKey, now)
				} else {
					s.sticky.Delete(policy.traceID)
					hasMapping = false
				}
			}
			continue
		}
		s.pool.MarkSuccess(entry.Key())

		_, _ = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

		errc := make(chan error, 2)
		go func() {
			_, err := io.Copy(upstreamConn, clientConn)
			errc <- err
		}()
		go func() {
			_, err := io.Copy(clientConn, upstreamConn)
			errc <- err
		}()

		<-errc
		_ = upstreamConn.Close()
		<-errc
		return nil
	}

	http.Error(w, fmt.Sprintf("CONNECT failed: %v", lastErr), http.StatusBadGateway)
	return lastErr
}

func (s *Server) handleForward(w http.ResponseWriter, r *http.Request) (int, error) {
	outReq, err := buildForwardRequest(r)
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return http.StatusBadRequest, err
	}

	policy, err := stickyPolicyFromRequest(s.selection, r)
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return http.StatusBadRequest, err
	}

	stickyEnabled := s.selection.Sticky.Enabled && s.sticky != nil && policy.traceID != ""
	if policy.forceSticky != nil {
		stickyEnabled = *policy.forceSticky && s.sticky != nil && policy.traceID != ""
	}

	stripHopByHopHeaders(outReq.Header)
	outReq.Header.Del(headerSticky)
	outReq.Header.Del(headerFailover)
	outReq.Header.Del(headerUpstream)

	retryable := isRetryableRequest(outReq, s.selection.RetryNonIdempotent)
	var lastErr error

	now := time.Now()
	mappedKey := ""
	hasMapping := false
	if stickyEnabled {
		mappedKey, hasMapping = s.sticky.Get(policy.traceID, now)
	}

	for attempt := 0; attempt <= s.selection.Retries; attempt++ {
		now = time.Now()
		var entry pool.Entry
		var ok bool
		if strings.TrimSpace(policy.forceKey) != "" {
			entry, ok = s.pool.Get(policy.forceKey, now)
			if !ok {
				http.Error(w, "Unknown upstream", http.StatusBadRequest)
				return http.StatusBadRequest, errors.New("unknown upstream")
			}
		} else if stickyEnabled {
			if hasMapping {
				entry, ok = s.pool.Get(mappedKey, now)
				if !ok {
					entry, ok = s.pool.Next(s.selection.Strategy, now)
					if ok && policy.failover == "soft" {
						mappedKey = entry.Key()
						hasMapping = true
						s.sticky.Set(policy.traceID, mappedKey, now)
					}
				}
			} else {
				entry, ok = s.pool.Next(s.selection.Strategy, now)
				if ok {
					mappedKey = entry.Key()
					hasMapping = true
					s.sticky.Set(policy.traceID, mappedKey, now)
				}
			}
		} else {
			entry, ok = s.pool.Next(s.selection.Strategy, now)
		}
		if !ok {
			http.Error(w, "No available proxies", http.StatusServiceUnavailable)
			return http.StatusServiceUnavailable, errors.New("no upstreams available")
		}

		attemptReq := outReq.Clone(context.WithValue(outReq.Context(), upstreamContextKey{}, entry))
		resp, err := s.client.Do(attemptReq)
		if err != nil {
			lastErr = err
			s.pool.MarkFailure(entry.Key(), now, time.Duration(s.selection.FailureBackoffSeconds)*time.Second, time.Duration(s.selection.MaxBackoffSeconds)*time.Second)
			if stickyEnabled && policy.failover == "soft" && strings.TrimSpace(policy.forceKey) == "" {
				if replacement, ok := s.pool.Next(s.selection.Strategy, now); ok {
					mappedKey = replacement.Key()
					hasMapping = true
					s.sticky.Set(policy.traceID, mappedKey, now)
				} else {
					s.sticky.Delete(policy.traceID)
					hasMapping = false
				}
			}
			if retryable && attempt < s.selection.Retries {
				continue
			}
			http.Error(w, fmt.Sprintf("Proxy request failed: %v", err), http.StatusBadGateway)
			return http.StatusBadGateway, err
		}
		defer resp.Body.Close()
		s.pool.MarkSuccess(entry.Key())

		stripHopByHopHeaders(resp.Header)
		copyHeader(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return resp.StatusCode, nil
	}

	http.Error(w, fmt.Sprintf("Proxy request failed: %v", lastErr), http.StatusBadGateway)
	return http.StatusBadGateway, lastErr
}

func dialThroughSOCKS5(ctx context.Context, upstream pool.Entry, network, addr string) (net.Conn, error) {
	var auth *proxy.Auth
	if strings.TrimSpace(upstream.Username) != "" || strings.TrimSpace(upstream.Password) != "" {
		auth = &proxy.Auth{User: upstream.Username, Password: upstream.Password}
	}
	dialer, err := proxy.SOCKS5("tcp", upstream.Addr, auth, proxy.Direct)
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

func isRetryableRequest(r *http.Request, retryNonIdempotent bool) bool {
	if retryNonIdempotent {
		return r.Body == http.NoBody || r.Body == nil
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return r.Body == http.NoBody || r.Body == nil
	default:
		return false
	}
}

func buildForwardRequest(r *http.Request) (*http.Request, error) {
	if r.URL == nil {
		return nil, errors.New("missing url")
	}

	u := *r.URL
	if u.Scheme == "" {
		u.Scheme = "http"
	}
	if u.Host == "" {
		u.Host = r.Host
	}

	out := r.Clone(r.Context())
	out.URL = &u
	out.RequestURI = ""

	// RFC 9110: Proxy requests should use absolute-form. Ensure Host is preserved.
	out.Host = u.Host

	return out, nil
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func stripHopByHopHeaders(h http.Header) {
	// First capture any headers listed by the "Connection" header.
	var connectionTokens []string
	if c := h.Get("Connection"); c != "" {
		for _, f := range strings.Split(c, ",") {
			if t := strings.TrimSpace(f); t != "" {
				connectionTokens = append(connectionTokens, textproto.CanonicalMIMEHeaderKey(t))
			}
		}
	}

	// Standard hop-by-hop headers.
	for _, k := range []string{
		"Connection",
		"Proxy-Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"TE",
		"Trailers",
		"Transfer-Encoding",
		"Upgrade",
	} {
		h.Del(k)
	}

	// Also remove headers listed by the "Connection" header.
	for _, k := range connectionTokens {
		h.Del(k)
	}
}
