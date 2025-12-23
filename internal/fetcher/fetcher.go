package fetcher

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type Fetcher struct {
	log    *slog.Logger
	client *http.Client
}

func New(log *slog.Logger) *Fetcher {
	return &Fetcher{
		log: log,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		},
	}
}

func (f *Fetcher) Fetch(ctx context.Context, urls []string) ([]string, error) {
	set := make(map[string]struct{})
	var out []string

	f.log.Info("fetching proxy lists", "sources", len(urls))
	for _, url := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			f.log.Warn("build request failed", "url", url, "err", err)
			continue
		}

		resp, err := f.client.Do(req)
		if err != nil {
			f.log.Warn("fetch failed", "url", url, "err", err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			f.log.Warn("unexpected status", "url", url, "status", resp.StatusCode)
			continue
		}

		scanner := bufio.NewScanner(resp.Body)
		added := 0
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.HasPrefix(line, "socks5://") {
				line = strings.TrimPrefix(line, "socks5://")
			}

			if _, ok := set[line]; ok {
				continue
			}
			set[line] = struct{}{}
			out = append(out, line)
			added++
		}
		if err := scanner.Err(); err != nil {
			f.log.Warn("scan failed", "url", url, "err", err)
		}
		resp.Body.Close()
		f.log.Info("fetched entries", "url", url, "added", added)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no proxies fetched from any source")
	}
	return out, nil
}

func (f *Fetcher) FetchBytes(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return data, nil
}
