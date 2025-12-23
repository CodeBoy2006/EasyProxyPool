package xray

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type NodeHealth struct {
	Alive bool
	Delay time.Duration

	LastSeen time.Time
	LastTry  time.Time

	OutboundTag string
}

// ParseDebugVars parses xray expvar `/debug/vars` output and returns node health keyed by outbound tag.
// It expects the "observatory" object structure described in xray metrics docs.
func ParseDebugVars(data []byte) (map[string]NodeHealth, error) {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse /debug/vars: %w", err)
	}

	obs, ok := root["observatory"].(map[string]any)
	if !ok {
		// Some builds might not enable observatory; treat as empty.
		return map[string]NodeHealth{}, nil
	}

	out := make(map[string]NodeHealth, len(obs))
	for tag, v := range obs {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		h := NodeHealth{
			OutboundTag: tag,
			Alive:       getBoolAny(m["alive"]),
			Delay:       time.Duration(getInt64Any(m["delay"])) * time.Millisecond,
			LastSeen:    fromUnixSeconds(getInt64Any(m["last_seen_time"])),
			LastTry:     fromUnixSeconds(getInt64Any(m["last_try_time"])),
		}
		out[tag] = h
	}
	return out, nil
}

type MetricsClient struct {
	base string
	http *http.Client
}

func NewMetricsClient(listen string) *MetricsClient {
	return &MetricsClient{
		base: "http://" + strings.TrimSpace(listen),
		http: &http.Client{
			Timeout: 3 * time.Second,
		},
	}
}

func (c *MetricsClient) Fetch(ctx context.Context) (map[string]NodeHealth, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/debug/vars", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return ParseDebugVars(data)
}

func getBoolAny(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	default:
		return false
	}
}

func getInt64Any(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	default:
		return 0
	}
}

func fromUnixSeconds(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	return time.Unix(v, 0)
}

