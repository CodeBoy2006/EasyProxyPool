package xray

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type Runner interface {
	Start(ctx context.Context, binaryPath string, args []string, workDir string, stdout, stderr io.Writer) (Process, error)
}

type Process interface {
	Wait() error
	Kill() error
}

type OSRunner struct{}

func (OSRunner) Start(ctx context.Context, binaryPath string, args []string, workDir string, stdout, stderr io.Writer) (Process, error) {
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Dir = workDir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &osProcess{cmd: cmd}, nil
}

type osProcess struct {
	cmd *exec.Cmd
}

func (p *osProcess) Wait() error { return p.cmd.Wait() }
func (p *osProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

type Instance struct {
	log *slog.Logger

	mode   Mode
	binary string
	workDir string

	socksListen  string
	metricsListen string
	startTimeout time.Duration

	runner Runner

	mu       sync.Mutex
	proc     Process
	lastHash string
}

func NewInstance(log *slog.Logger, mode Mode, binary, workDir, socksListen, metricsListen string, startTimeout time.Duration, runner Runner) *Instance {
	if runner == nil {
		runner = OSRunner{}
	}
	if startTimeout <= 0 {
		startTimeout = 10 * time.Second
	}
	return &Instance{
		log:           log.With("component", "xray", "mode", string(mode)),
		mode:          mode,
		binary:        binary,
		workDir:       workDir,
		socksListen:   socksListen,
		metricsListen: metricsListen,
		startTimeout:  startTimeout,
		runner:        runner,
	}
}

func (i *Instance) Ensure(ctx context.Context, configJSON []byte, hash string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.proc != nil && i.lastHash == hash {
		return nil
	}

	if i.proc != nil {
		_ = i.proc.Kill()
		i.proc = nil
	}

	if err := os.MkdirAll(i.workDir, 0o700); err != nil {
		return fmt.Errorf("create work_dir: %w", err)
	}

	configPath := filepath.Join(i.workDir, fmt.Sprintf("xray-%s.json", i.mode))
	if err := os.WriteFile(configPath, configJSON, 0o600); err != nil {
		return fmt.Errorf("write xray config: %w", err)
	}

	args := []string{"run", "-c", configPath}
	proc, err := i.runner.Start(ctx, i.binary, args, i.workDir, io.Discard, io.Discard)
	if err != nil {
		return fmt.Errorf("start xray: %w", err)
	}
	i.proc = proc

	readyCtx, cancel := context.WithTimeout(ctx, i.startTimeout)
	defer cancel()
	if err := waitReady(readyCtx, i.socksListen, i.metricsListen); err != nil {
		_ = i.proc.Kill()
		i.proc = nil
		return err
	}

	i.lastHash = hash
	i.log.Info("xray ready", "hash", hash, "socks", i.socksListen, "metrics", i.metricsListen)
	return nil
}

func (i *Instance) Stop(ctx context.Context) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.proc == nil {
		return nil
	}
	_ = ctx // reserved for graceful shutdown if needed
	err := i.proc.Kill()
	i.proc = nil
	i.lastHash = ""
	return err
}

func waitReady(ctx context.Context, socksListen, metricsListen string) error {
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()

	for {
		if err := checkSOCKSPort(socksListen); err == nil {
			if err2 := checkMetrics(ctx, metricsListen); err2 == nil {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("xray not ready before timeout: %w", ctx.Err())
		case <-t.C:
		}
	}
}

func checkSOCKSPort(addr string) error {
	c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return err
	}
	_ = c.Close()
	return nil
}

func checkMetrics(ctx context.Context, listen string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+listen+"/debug/vars", nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("metrics not ok")
	}
	return nil
}

