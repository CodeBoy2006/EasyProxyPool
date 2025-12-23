package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CodeBoy2006/EasyProxyPool/internal/config"
	"github.com/CodeBoy2006/EasyProxyPool/internal/fetcher"
	"github.com/CodeBoy2006/EasyProxyPool/internal/health"
	"github.com/CodeBoy2006/EasyProxyPool/internal/pool"
	"github.com/CodeBoy2006/EasyProxyPool/internal/sources"
	"github.com/CodeBoy2006/EasyProxyPool/internal/upstream"
	"github.com/CodeBoy2006/EasyProxyPool/internal/xray"
)

type Updater struct {
	log *slog.Logger
	cfg config.Config

	strictPool  *pool.Pool
	relaxedPool *pool.Pool
	status      *Status

	fetcher *fetcher.Fetcher
	checker *health.Checker
	xrayStrict  *xray.Instance
	xrayRelaxed *xray.Instance

	metricsStrict  *xray.MetricsClient
	metricsRelaxed *xray.MetricsClient

	ticker *time.Ticker
	wg     sync.WaitGroup
}

func NewUpdater(log *slog.Logger, cfg config.Config, strictPool, relaxedPool *pool.Pool, status *Status) *Updater {
	u := &Updater{
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

	if cfg.Adapters.Xray.Enabled {
		u.xrayStrict = xray.NewInstance(
			log,
			xray.ModeStrict,
			cfg.Adapters.Xray.BinaryPath,
			cfg.Adapters.Xray.WorkDir,
			cfg.Adapters.Xray.SOCKSListenStrict,
			cfg.Adapters.Xray.MetricsListenStrict,
			time.Duration(cfg.Adapters.Xray.StartTimeoutSeconds)*time.Second,
			nil,
		)
		u.xrayRelaxed = xray.NewInstance(
			log,
			xray.ModeRelaxed,
			cfg.Adapters.Xray.BinaryPath,
			cfg.Adapters.Xray.WorkDir,
			cfg.Adapters.Xray.SOCKSListenRelaxed,
			cfg.Adapters.Xray.MetricsListenRelaxed,
			time.Duration(cfg.Adapters.Xray.StartTimeoutSeconds)*time.Second,
			nil,
		)
		u.metricsStrict = xray.NewMetricsClient(cfg.Adapters.Xray.MetricsListenStrict)
		u.metricsRelaxed = xray.NewMetricsClient(cfg.Adapters.Xray.MetricsListenRelaxed)
	}
	return u
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

	if u.xrayStrict != nil {
		_ = u.xrayStrict.Stop(ctx)
	}
	if u.xrayRelaxed != nil {
		_ = u.xrayRelaxed.Stop(ctx)
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

	if u.cfg.Adapters.Xray.Enabled {
		u.runOnceXray(ctx, start)
		return
	}

	proxies, err := u.loadSOCKS5Upstreams(ctx)
	if err != nil {
		u.status.SetEnd(time.Now(), 0, 0, 0, err, UpdateDetails{Adapter: "legacy"})
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

	u.status.SetEnd(time.Now(), len(proxies), len(strictEntries), len(relaxedEntries), nil, UpdateDetails{Adapter: "legacy"})
	u.log.Info("update complete",
		"fetched", len(proxies),
		"strict", len(strictEntries),
		"relaxed", len(relaxedEntries),
		"took", time.Since(start).String(),
	)
}

func (u *Updater) runOnceXray(ctx context.Context, start time.Time) {
	specs, res, err := u.loadUpstreamSpecs(ctx)
	if err != nil {
		u.status.SetEnd(time.Now(), 0, 0, 0, err, UpdateDetails{Adapter: "xray"})
		u.log.Warn("load specs failed", "err", err)
		return
	}

	genStrict, err := xray.Generate(specs, xray.GenerateOptions{
		Mode:           xray.ModeStrict,
		SOCKSListen:    u.cfg.Adapters.Xray.SOCKSListenStrict,
		MetricsListen:  u.cfg.Adapters.Xray.MetricsListenStrict,
		UserPassword:   u.cfg.Adapters.Xray.UserPassword,
		MaxNodes:       u.cfg.Adapters.Xray.MaxNodes,
		Observatory:    u.cfg.Adapters.Xray.Observatory,
	})
	if err != nil {
		u.status.SetEnd(time.Now(), len(specs), 0, 0, err, UpdateDetails{Adapter: "xray"})
		u.log.Warn("xray config (strict) failed", "err", err)
		return
	}
	genRelaxed, err := xray.Generate(specs, xray.GenerateOptions{
		Mode:           xray.ModeRelaxed,
		SOCKSListen:    u.cfg.Adapters.Xray.SOCKSListenRelaxed,
		MetricsListen:  u.cfg.Adapters.Xray.MetricsListenRelaxed,
		UserPassword:   u.cfg.Adapters.Xray.UserPassword,
		MaxNodes:       u.cfg.Adapters.Xray.MaxNodes,
		Observatory:    u.cfg.Adapters.Xray.Observatory,
	})
	if err != nil {
		u.status.SetEnd(time.Now(), len(specs), 0, 0, err, UpdateDetails{Adapter: "xray"})
		u.log.Warn("xray config (relaxed) failed", "err", err)
		return
	}

	if err := u.xrayStrict.Ensure(ctx, genStrict.ConfigJSON, genStrict.Hash); err != nil {
		u.status.SetEnd(time.Now(), len(specs), 0, 0, err, UpdateDetails{Adapter: "xray", XrayStrictHash: genStrict.Hash, XrayRelaxedHash: genRelaxed.Hash})
		u.log.Warn("xray ensure strict failed", "err", err)
		return
	}
	if err := u.xrayRelaxed.Ensure(ctx, genRelaxed.ConfigJSON, genRelaxed.Hash); err != nil {
		u.status.SetEnd(time.Now(), len(specs), 0, 0, err, UpdateDetails{Adapter: "xray", XrayStrictHash: genStrict.Hash, XrayRelaxedHash: genRelaxed.Hash})
		u.log.Warn("xray ensure relaxed failed", "err", err)
		return
	}

	hs, err := u.metricsStrict.Fetch(ctx)
	if err != nil {
		u.status.SetEnd(time.Now(), len(specs), 0, 0, err, UpdateDetails{Adapter: "xray", XrayStrictHash: genStrict.Hash, XrayRelaxedHash: genRelaxed.Hash})
		u.log.Warn("metrics strict failed", "err", err)
		return
	}
	hr, err := u.metricsRelaxed.Fetch(ctx)
	if err != nil {
		u.status.SetEnd(time.Now(), len(specs), 0, 0, err, UpdateDetails{Adapter: "xray", XrayStrictHash: genStrict.Hash, XrayRelaxedHash: genRelaxed.Hash})
		u.log.Warn("metrics relaxed failed", "err", err)
		return
	}

	now := time.Now()
	strictEntries := make([]pool.Entry, 0, len(genStrict.Included))
	relaxedEntries := make([]pool.Entry, 0, len(genRelaxed.Included))

	for _, id := range genStrict.Included {
		if h, ok := hs[id]; ok && h.Alive {
			strictEntries = append(strictEntries, pool.Entry{
				ID:            id,
				Addr:          u.cfg.Adapters.Xray.SOCKSListenStrict,
				Username:      id,
				Password:      u.cfg.Adapters.Xray.UserPassword,
				Latency:       h.Delay,
				LastCheckedAt: now,
			})
		}
	}
	for _, id := range genRelaxed.Included {
		if h, ok := hr[id]; ok && h.Alive {
			relaxedEntries = append(relaxedEntries, pool.Entry{
				ID:            id,
				Addr:          u.cfg.Adapters.Xray.SOCKSListenRelaxed,
				Username:      id,
				Password:      u.cfg.Adapters.Xray.UserPassword,
				Latency:       h.Delay,
				LastCheckedAt: now,
			})
		}
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

	details := UpdateDetails{
		Adapter:         "xray",
		NodesTotal:      len(specs),
		NodesIncluded:   len(specs),
		ProblemsCount:   len(res.Problems),
		SkippedByType:   mergeSkipped(genStrict.Skipped, genRelaxed.Skipped, res.Skipped),
		XrayStrictHash:  genStrict.Hash,
		XrayRelaxedHash: genRelaxed.Hash,
	}
	u.status.SetEnd(time.Now(), len(specs), len(strictEntries), len(relaxedEntries), nil, details)
	u.log.Info("update complete",
		"adapter", "xray",
		"nodes", len(specs),
		"strict", len(strictEntries),
		"relaxed", len(relaxedEntries),
		"took", time.Since(start).String(),
	)
}

func mergeSkipped(mm ...map[string]int) map[string]int {
	out := make(map[string]int)
	for _, m := range mm {
		for k, v := range m {
			out[k] += v
		}
	}
	return out
}

func (u *Updater) loadUpstreamSpecs(ctx context.Context) ([]upstream.Spec, sources.Result, error) {
	var res sources.Result
	var specs []upstream.Spec

	// 1) Typed sources.
	if len(u.cfg.Sources) > 0 {
		r, err := sources.New(u.log).Load(ctx, u.cfg.Sources)
		if err != nil {
			return nil, sources.Result{}, err
		}
		res = r
		specs = append(specs, r.Specs...)
		for _, a := range r.SOCKS5Addrs {
			if s, ok := socks5SpecFromAddr(a); ok {
				specs = append(specs, s)
			}
		}
	}

	// 2) Legacy URL lists, mapped to SOCKS5 specs.
	if len(u.cfg.ProxyListURLs) > 0 {
		addrs, err := u.fetcher.Fetch(ctx, u.cfg.ProxyListURLs)
		if err != nil {
			return nil, res, err
		}
		for _, a := range addrs {
			if s, ok := socks5SpecFromAddr(a); ok {
				specs = append(specs, s)
			}
		}
	}

	specs = upstream.Deduplicate(specs)
	return specs, res, nil
}

func socks5SpecFromAddr(addr string) (upstream.Spec, bool) {
	addr = strings.TrimSpace(strings.TrimPrefix(addr, "socks5://"))
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return upstream.Spec{}, false
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		return upstream.Spec{}, false
	}
	return upstream.Spec{
		Type:   upstream.TypeSOCKS5,
		Server: host,
		Port:   port,
	}.Normalize(), true
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
