package health

import (
	"context"
	"crypto/tls"
	"log/slog"
	"time"

	"golang.org/x/net/proxy"
)

type Checker struct {
	log          *slog.Logger
	targetAddr   string
	serverName   string
	totalTimeout time.Duration
	threshold    time.Duration
}

func New(log *slog.Logger, targetAddr, serverName string, totalTimeout, threshold time.Duration) *Checker {
	return &Checker{
		log:          log,
		targetAddr:   targetAddr,
		serverName:   serverName,
		totalTimeout: totalTimeout,
		threshold:    threshold,
	}
}

func (c *Checker) Check(ctx context.Context, upstreamAddr string, strict bool) (bool, time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, c.totalTimeout)
	defer cancel()

	dialer, err := proxy.SOCKS5("tcp", upstreamAddr, nil, proxy.Direct)
	if err != nil {
		return false, 0
	}

	type result struct {
		ok      bool
		latency time.Duration
	}
	done := make(chan result, 1)

	go func() {
		start := time.Now()
		conn, err := dialer.Dial("tcp", c.targetAddr)
		if err != nil {
			done <- result{ok: false}
			return
		}
		defer conn.Close()

		tlsConn := tls.Client(conn, &tls.Config{
			ServerName:         c.serverName,
			InsecureSkipVerify: !strict,
		})
		if err := tlsConn.Handshake(); err != nil {
			_ = tlsConn.Close()
			done <- result{ok: false}
			return
		}
		_ = tlsConn.Close()

		latency := time.Since(start)
		if latency > c.threshold {
			done <- result{ok: false, latency: latency}
			return
		}
		done <- result{ok: true, latency: latency}
	}()

	select {
	case res := <-done:
		return res.ok, res.latency
	case <-ctx.Done():
		return false, 0
	}
}
