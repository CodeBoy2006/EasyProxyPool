package sources

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/CodeBoy2006/EasyProxyPool/internal/clash"
	"github.com/CodeBoy2006/EasyProxyPool/internal/config"
	"github.com/CodeBoy2006/EasyProxyPool/internal/fetcher"
	"github.com/CodeBoy2006/EasyProxyPool/internal/upstream"
)

type Loader struct {
	log     *slog.Logger
	fetcher *fetcher.Fetcher
}

func New(log *slog.Logger) *Loader {
	return &Loader{
		log:     log.With("component", "sources"),
		fetcher: fetcher.New(log),
	}
}

type Result struct {
	Specs     []upstream.Spec
	SOCKS5Addrs []string

	Problems []string
	Skipped  map[string]int
}

func (l *Loader) Load(ctx context.Context, sources []config.SourceConfig) (Result, error) {
	var out Result
	out.Skipped = make(map[string]int)

	set := make(map[string]struct{})
	addAddr := func(addr string) {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			return
		}
		if strings.HasPrefix(addr, "socks5://") {
			addr = strings.TrimPrefix(addr, "socks5://")
		}
		if _, ok := set[addr]; ok {
			return
		}
		set[addr] = struct{}{}
		out.SOCKS5Addrs = append(out.SOCKS5Addrs, addr)
	}

	for _, src := range sources {
		typ := strings.ToLower(strings.TrimSpace(src.Type))
		switch typ {
		case "raw_list":
			addrs, problems, err := l.loadRawList(ctx, src)
			if err != nil {
				return Result{}, err
			}
			out.Problems = append(out.Problems, problems...)
			for _, a := range addrs {
				addAddr(a)
			}
		case "clash_yaml":
			rep, err := l.loadClashYAML(ctx, src)
			if err != nil {
				return Result{}, err
			}
			out.Problems = append(out.Problems, rep.Problems...)
			for k, v := range rep.SkippedByType {
				out.Skipped[k] += v
			}
			out.Specs = append(out.Specs, rep.Specs...)
			for _, s := range rep.Specs {
				// For the legacy (non-xray) pipeline, we can only use SOCKS5 nodes.
				// Nodes with auth are skipped until the pool/dialer layer supports credentials.
				if s.Type != upstream.TypeSOCKS5 || s.SOCKS5 == nil {
					continue
				}
				if strings.TrimSpace(s.SOCKS5.Username) != "" || strings.TrimSpace(s.SOCKS5.Password) != "" {
					out.Problems = append(out.Problems, fmt.Sprintf("proxy(name=%q,type=socks5): auth not supported in legacy mode; skipped", s.Name))
					continue
				}
				addAddr(fmt.Sprintf("%s:%d", s.Server, s.Port))
			}
		default:
			out.Problems = append(out.Problems, fmt.Sprintf("source(type=%q): unsupported (use raw_list or clash_yaml)", src.Type))
		}
	}

	out.Specs = upstream.Deduplicate(out.Specs)
	if len(out.SOCKS5Addrs) == 0 && len(out.Specs) == 0 {
		return Result{}, fmt.Errorf("no usable entries from sources")
	}
	return out, nil
}

func (l *Loader) loadRawList(ctx context.Context, src config.SourceConfig) ([]string, []string, error) {
	if src.URL != "" && src.Path != "" {
		return nil, nil, fmt.Errorf("raw_list: set only one of url or path")
	}
	if src.URL != "" {
		addrs, err := l.fetcher.Fetch(ctx, []string{src.URL})
		if err != nil {
			return nil, nil, fmt.Errorf("raw_list fetch url: %w", err)
		}
		return addrs, nil, nil
	}
	if src.Path != "" {
		f, err := os.Open(src.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("raw_list open file: %w", err)
		}
		defer f.Close()
		var out []string
		var problems []string
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			out = append(out, line)
		}
		if err := sc.Err(); err != nil {
			problems = append(problems, fmt.Sprintf("raw_list scan file: %v", err))
		}
		return out, problems, nil
	}
	return nil, nil, fmt.Errorf("raw_list: missing url/path")
}

func (l *Loader) loadClashYAML(ctx context.Context, src config.SourceConfig) (clash.ParseReport, error) {
	if src.URL != "" && src.Path != "" {
		return clash.ParseReport{}, fmt.Errorf("clash_yaml: set only one of url or path")
	}
	if src.URL != "" {
		data, err := l.fetcher.FetchBytes(ctx, src.URL)
		if err != nil {
			return clash.ParseReport{}, fmt.Errorf("clash_yaml fetch url: %w", err)
		}
		return clash.ParseYAML(data)
	}
	if src.Path != "" {
		data, err := os.ReadFile(src.Path)
		if err != nil {
			return clash.ParseReport{}, fmt.Errorf("clash_yaml read file: %w", err)
		}
		return clash.ParseYAML(data)
	}
	return clash.ParseReport{}, fmt.Errorf("clash_yaml: missing url/path")
}

