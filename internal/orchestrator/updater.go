package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/CodeBoy2006/EasyProxyPool/internal/config"
	"github.com/CodeBoy2006/EasyProxyPool/internal/fetcher"
	"github.com/CodeBoy2006/EasyProxyPool/internal/health"
	"github.com/CodeBoy2006/EasyProxyPool/internal/pool"
	"github.com/CodeBoy2006/EasyProxyPool/internal/sources"
)

type Updater struct {
	log *slog.Logger
	cfg config.Config

	strictPool  *pool.Pool
	relaxedPool *pool.Pool
	status      *Status

	fetcher *fetcher.Fetcher
	checker *health.Checker

	ticker *time.Ticker
	wg     sync.WaitGroup
}

func NewUpdater(log *slog.Logger, cfg config.Config, strictPool, relaxedPool *pool.Pool, status *Status) *Updater {
	return &Updater{
		log:         log,
		cfg:         cfg,
		strictPool:  strictPool,
		relaxedPool: relaxedPool,
		status:      status,
		fetcher:     fetcher.New(log),
		checker: health.New(
			log,
			cfg.HealthCheck.TargetAddress,
			cfg.HealthCheck.TargetServerName,
			time.Duration(cfg.HealthCheck.TotalTimeoutSeconds)*time.Second,
			time.Duration(cfg.HealthCheck.TLSHandshakeThresholdSeconds)*time.Second,
		),
	}
}

func (u *Updater) Start(ctx context.Context) {
	u.runOnce(ctx)

	interval := time.Duration(u.cfg.UpdateIntervalMinutes) * time.Minute
	u.ticker = time.NewTicker(interval)

	u.wg.Add(1)
	go func() {
		defer u.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-u.ticker.C:
				u.runOnce(ctx)
			}
		}
	}()
}

func (u *Updater) Stop(ctx context.Context) {
	if u.ticker != nil {
		u.ticker.Stop()
	}

	done := make(chan struct{})
	go func() {
		u.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
	}
}

func (u *Updater) runOnce(ctx context.Context) {
	if !u.strictPool.UpdatingCAS() {
		u.log.Info("update already in progress; skipping")
		return
	}
	defer u.strictPool.UpdatingClear()

	start := time.Now()
	u.status.SetStart(start)
	u.log.Info("updating proxy pools")

	proxies, err := u.loadSOCKS5Upstreams(ctx)
	if err != nil {
		u.status.SetEnd(time.Now(), 0, 0, 0, err)
		u.log.Warn("fetch failed", "err", err)
		return
	}

	type hc struct {
		addr    string
		strict  bool
		latency time.Duration
	}

	sem := make(chan struct{}, u.cfg.HealthCheckConcurrency)
	results := make(chan hc, len(proxies)*2)

	var wg sync.WaitGroup
	for _, addr := range proxies {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			ok, latency := u.checker.Check(ctx, addr, true)
			if ok {
				results <- hc{addr: addr, strict: true, latency: latency}
				results <- hc{addr: addr, strict: false, latency: latency}
				return
			}

			ok2, latency2 := u.checker.Check(ctx, addr, false)
			if ok2 {
				results <- hc{addr: addr, strict: false, latency: latency2}
			}
		}(addr)
	}

	wg.Wait()
	close(results)

	strictEntries := make([]pool.Entry, 0)
	relaxedEntries := make([]pool.Entry, 0)
	now := time.Now()

	seenStrict := make(map[string]struct{})
	seenRelaxed := make(map[string]struct{})
	for r := range results {
		if r.strict {
			if _, ok := seenStrict[r.addr]; ok {
				continue
			}
			seenStrict[r.addr] = struct{}{}
			strictEntries = append(strictEntries, pool.Entry{
				Addr:          r.addr,
				Latency:       r.latency,
				LastCheckedAt: now,
			})
			continue
		}
		if _, ok := seenRelaxed[r.addr]; ok {
			continue
		}
		seenRelaxed[r.addr] = struct{}{}
		relaxedEntries = append(relaxedEntries, pool.Entry{
			Addr:          r.addr,
			Latency:       r.latency,
			LastCheckedAt: now,
		})
	}

	if len(strictEntries) > 0 {
		u.strictPool.Update(strictEntries)
	} else {
		u.log.Warn("strict pool empty; keeping existing")
	}
	if len(relaxedEntries) > 0 {
		u.relaxedPool.Update(relaxedEntries)
	} else {
		u.log.Warn("relaxed pool empty; keeping existing")
	}

	u.status.SetEnd(time.Now(), len(proxies), len(strictEntries), len(relaxedEntries), nil)
	u.log.Info("update complete",
		"fetched", len(proxies),
		"strict", len(strictEntries),
		"relaxed", len(relaxedEntries),
		"took", time.Since(start).String(),
	)
}

func (u *Updater) loadSOCKS5Upstreams(ctx context.Context) ([]string, error) {
	set := make(map[string]struct{})
	var out []string
	add := func(addr string) {
		if _, ok := set[addr]; ok {
			return
		}
		set[addr] = struct{}{}
		out = append(out, addr)
	}

	// Legacy line-based sources.
	if len(u.cfg.ProxyListURLs) > 0 {
		addrs, err := u.fetcher.Fetch(ctx, u.cfg.ProxyListURLs)
		if err != nil {
			return nil, err
		}
		for _, a := range addrs {
			add(a)
		}
	}

	// Typed sources (currently only SOCKS5 nodes are usable without adapters).
	if len(u.cfg.Sources) > 0 {
		res, err := sources.New(u.log).Load(ctx, u.cfg.Sources)
		if err != nil {
			return nil, err
		}
		for _, a := range res.SOCKS5Addrs {
			add(a)
		}
		for _, p := range res.Problems {
			u.log.Warn("source problem", "msg", p)
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no proxies fetched from any source")
	}
	return out, nil
}

func (u *Updater) String() string {
	return fmt.Sprintf("updater(interval=%dm)", u.cfg.UpdateIntervalMinutes)
}
