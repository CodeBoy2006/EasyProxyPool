package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/CodeBoy2006/EasyProxyPool/internal/config"
	"github.com/CodeBoy2006/EasyProxyPool/internal/logging"
	"github.com/CodeBoy2006/EasyProxyPool/internal/orchestrator"
	"github.com/CodeBoy2006/EasyProxyPool/internal/pool"
	"github.com/CodeBoy2006/EasyProxyPool/internal/server/admin"
	"github.com/CodeBoy2006/EasyProxyPool/internal/server/httpproxy"
	"github.com/CodeBoy2006/EasyProxyPool/internal/server/socks5proxy"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "config.yaml", "Path to YAML config")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "failed to load config:", err)
		os.Exit(1)
	}

	logger := logging.New(cfg.Logging.Level, os.Getenv("LOG_LEVEL"))
	logger.Info("starting EasyProxyPool", "config", configPath, "update_every_minutes", cfg.UpdateIntervalMinutes)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	strictPool := pool.New("strict", logger)
	relaxedPool := pool.New("relaxed", logger)

	status := orchestrator.NewStatus()

	updater := orchestrator.NewUpdater(logger, cfg, strictPool, relaxedPool, status)
	updater.Start(ctx)

	if cfg.Admin.Enabled && cfg.Admin.Addr != "" {
		adminServer := admin.New(logger, cfg.Admin.Addr, status, strictPool, relaxedPool)
		adminServer.Start(ctx)
	}

	socksStrict := socks5proxy.New(logger, cfg.Ports.SOCKS5Strict, socks5proxy.ModeStrict, strictPool, cfg.Auth, cfg.Selection)
	socksRelaxed := socks5proxy.New(logger, cfg.Ports.SOCKS5Relaxed, socks5proxy.ModeRelaxed, relaxedPool, cfg.Auth, cfg.Selection)
	httpStrict := httpproxy.New(logger, cfg.Ports.HTTPStrict, httpproxy.ModeStrict, strictPool, cfg.Auth, cfg.Selection)
	httpRelaxed := httpproxy.New(logger, cfg.Ports.HTTPRelaxed, httpproxy.ModeRelaxed, relaxedPool, cfg.Auth, cfg.Selection)

	socksStrict.Start(ctx)
	socksRelaxed.Start(ctx)
	httpStrict.Start(ctx)
	httpRelaxed.Start(ctx)

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	updater.Stop(shutdownCtx)
	socksStrict.Stop(shutdownCtx)
	socksRelaxed.Stop(shutdownCtx)
	httpStrict.Stop(shutdownCtx)
	httpRelaxed.Stop(shutdownCtx)
}
