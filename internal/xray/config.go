package xray

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/CodeBoy2006/EasyProxyPool/internal/config"
	"github.com/CodeBoy2006/EasyProxyPool/internal/upstream"
)

type Mode string

const (
	ModeStrict  Mode = "strict"
	ModeRelaxed Mode = "relaxed"
)

type GenerateOptions struct {
	Mode Mode

	SOCKSListen  string
	MetricsListen string
	UserPassword string

	MaxNodes    int
	Observatory config.ObservatoryConfig
}

type Generated struct {
	ConfigJSON []byte
	Hash       string

	Included []string
	Skipped  map[string]int
	Problems []string
}

func Generate(specs []upstream.Spec, opt GenerateOptions) (Generated, error) {
	if strings.TrimSpace(opt.SOCKSListen) == "" {
		return Generated{}, fmt.Errorf("missing socks listen")
	}
	if strings.TrimSpace(opt.MetricsListen) == "" {
		return Generated{}, fmt.Errorf("missing metrics listen")
	}
	if strings.TrimSpace(opt.UserPassword) == "" {
		return Generated{}, fmt.Errorf("missing user password")
	}
	if opt.MaxNodes <= 0 {
		opt.MaxNodes = 2000
	}

	gen := Generated{Skipped: make(map[string]int)}

	// Normalize and filter to supported types.
	var nodes []upstream.Spec
	for _, s := range specs {
		if strings.TrimSpace(s.ID) == "" {
			s = s.Normalize()
		}
		if _, ok := buildOutbound(s, opt.Mode); !ok {
			gen.Skipped[string(s.Type)]++
			continue
		}
		nodes = append(nodes, s)
	}

	if len(nodes) > opt.MaxNodes {
		return Generated{}, fmt.Errorf("too many nodes: %d > max_nodes=%d", len(nodes), opt.MaxNodes)
	}

	// Deterministic ordering for stable config hashes.
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	inboundTag := "socks-in"

	accounts := make([]map[string]any, 0, len(nodes))
	outbounds := make([]map[string]any, 0, len(nodes)+1)
	rules := make([]map[string]any, 0, len(nodes))

	for _, n := range nodes {
		accounts = append(accounts, map[string]any{
			"user": n.ID,
			"pass": opt.UserPassword,
		})
		out, _ := buildOutbound(n, opt.Mode)
		outbounds = append(outbounds, out)
		rules = append(rules, map[string]any{
			"type":       "field",
			"inboundTag": []string{inboundTag},
			"user":       []string{n.ID},
			"outboundTag": n.ID,
		})
		gen.Included = append(gen.Included, n.ID)
	}

	// Fallback for unmatched users.
	outbounds = append(outbounds, map[string]any{
		"tag":      "direct",
		"protocol": "freedom",
	})
	rules = append(rules, map[string]any{
		"type":       "field",
		"inboundTag": []string{inboundTag},
		"outboundTag": "direct",
	})

	root := map[string]any{
		"log": map[string]any{
			"loglevel": "warning",
		},
		"inbounds": []map[string]any{
			{
				"listen":   hostPart(opt.SOCKSListen),
				"port":     portPart(opt.SOCKSListen),
				"protocol": "socks",
				"tag":      inboundTag,
				"settings": map[string]any{
					"auth":     "password",
					"udp":      true,
					"accounts": accounts,
				},
			},
		},
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules":          rules,
		},
		"outbounds": outbounds,
		"metrics": map[string]any{
			"tag":    "metrics",
			"listen": opt.MetricsListen,
		},
	}

	switch strings.ToLower(strings.TrimSpace(opt.Observatory.Mode)) {
	case "observatory":
		root["observatory"] = map[string]any{
			"subjectSelector": []string{"n-"},
			"probeUrl":        opt.Observatory.Destination,
			"probeInterval":   durationString(opt.Observatory.IntervalSeconds, 30) + "s",
		}
	default: // burst
		ping := map[string]any{
			"destination": opt.Observatory.Destination,
			"interval":    durationString(opt.Observatory.IntervalSeconds, 30) + "s",
			"sampling":    opt.Observatory.Sampling,
			"timeout":     durationString(opt.Observatory.TimeoutSeconds, 5) + "s",
		}
		if strings.TrimSpace(opt.Observatory.Connectivity) != "" {
			ping["connectivity"] = opt.Observatory.Connectivity
		}
		root["burstObservatory"] = map[string]any{
			"subjectSelector": []string{"n-"},
			"pingConfig":      ping,
		}
	}

	raw, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return Generated{}, fmt.Errorf("marshal xray config: %w", err)
	}
	sum := sha256.Sum256(raw)
	gen.Hash = hex.EncodeToString(sum[:])[:12]
	gen.ConfigJSON = raw
	return gen, nil
}

func buildOutbound(s upstream.Spec, mode Mode) (map[string]any, bool) {
	switch s.Type {
	case upstream.TypeSOCKS5:
		cfg := s.SOCKS5
		if cfg == nil {
			cfg = &upstream.SOCKS5Config{}
		}
		server := map[string]any{
			"address": s.Server,
			"port":    s.Port,
		}
		if strings.TrimSpace(cfg.Username) != "" || strings.TrimSpace(cfg.Password) != "" {
			server["users"] = []map[string]any{
				{"user": cfg.Username, "pass": cfg.Password},
			}
		}
		return map[string]any{
			"tag":      s.ID,
			"protocol": "socks",
			"settings": map[string]any{
				"servers": []map[string]any{server},
			},
		}, true

	case upstream.TypeHTTP:
		cfg := s.HTTP
		if cfg == nil {
			cfg = &upstream.HTTPConfig{}
		}
		server := map[string]any{
			"address": s.Server,
			"port":    s.Port,
		}
		if strings.TrimSpace(cfg.Username) != "" || strings.TrimSpace(cfg.Password) != "" {
			server["users"] = []map[string]any{
				{"user": cfg.Username, "pass": cfg.Password},
			}
		}
		return map[string]any{
			"tag":      s.ID,
			"protocol": "http",
			"settings": map[string]any{
				"servers": []map[string]any{server},
			},
		}, true

	case upstream.TypeShadowsocks:
		if s.Shadowsocks == nil {
			return nil, false
		}
		return map[string]any{
			"tag":      s.ID,
			"protocol": "shadowsocks",
			"settings": map[string]any{
				"servers": []map[string]any{
					{
						"address":  s.Server,
						"port":     s.Port,
						"method":   s.Shadowsocks.Method,
						"password": s.Shadowsocks.Password,
					},
				},
			},
		}, true

	case upstream.TypeTrojan:
		if s.Trojan == nil {
			return nil, false
		}
		out := map[string]any{
			"tag":      s.ID,
			"protocol": "trojan",
			"settings": map[string]any{
				"servers": []map[string]any{
					{
						"address":  s.Server,
						"port":     s.Port,
						"password": s.Trojan.Password,
					},
				},
			},
		}
		out["streamSettings"] = buildStreamSettings(true, s.Trojan.SkipCertVerify, s.Trojan.ServerName, s.Trojan.Network, s.Trojan.WSPath, s.Trojan.Headers, mode)
		return out, true

	case upstream.TypeVMess:
		if s.VMess == nil {
			return nil, false
		}
		user := map[string]any{
			"id":       s.VMess.UUID,
			"alterId":  s.VMess.AlterID,
			"security": s.VMess.Security,
		}
		out := map[string]any{
			"tag":      s.ID,
			"protocol": "vmess",
			"settings": map[string]any{
				"vnext": []map[string]any{
					{
						"address": s.Server,
						"port":    s.Port,
						"users":   []map[string]any{user},
					},
				},
			},
		}
		out["streamSettings"] = buildStreamSettings(s.VMess.TLS, s.VMess.SkipCertVerify, s.VMess.ServerName, s.VMess.Network, s.VMess.WSPath, s.VMess.Headers, mode)
		return out, true

	case upstream.TypeVLESS:
		if s.VLESS == nil {
			return nil, false
		}
		user := map[string]any{
			"id":         s.VLESS.UUID,
			"encryption": "none",
		}
		if strings.TrimSpace(s.VLESS.Flow) != "" {
			user["flow"] = s.VLESS.Flow
		}
		out := map[string]any{
			"tag":      s.ID,
			"protocol": "vless",
			"settings": map[string]any{
				"vnext": []map[string]any{
					{
						"address": s.Server,
						"port":    s.Port,
						"users":   []map[string]any{user},
					},
				},
			},
		}
		out["streamSettings"] = buildStreamSettings(s.VLESS.TLS, s.VLESS.SkipCertVerify, s.VLESS.ServerName, s.VLESS.Network, s.VLESS.WSPath, s.VLESS.Headers, mode)
		return out, true

	default:
		return nil, false
	}
}

func buildStreamSettings(tlsEnabled bool, skipCertVerify bool, serverName string, network string, wsPath string, headers map[string]string, mode Mode) map[string]any {
	network = strings.ToLower(strings.TrimSpace(network))
	if network == "" {
		network = "tcp"
	}

	out := map[string]any{
		"network": network,
	}

	if network == "ws" {
		ws := map[string]any{
			"path": wsPath,
		}
		if len(headers) > 0 {
			ws["headers"] = headers
		}
		out["wsSettings"] = ws
	}

	if tlsEnabled {
		out["security"] = "tls"
		allowInsecure := skipCertVerify || mode == ModeRelaxed
		tlsSettings := map[string]any{
			"allowInsecure": allowInsecure,
		}
		if strings.TrimSpace(serverName) != "" {
			tlsSettings["serverName"] = serverName
		}
		out["tlsSettings"] = tlsSettings
	}

	return out
}

func hostPart(addr string) string {
	if strings.Contains(addr, ":") {
		parts := strings.Split(addr, ":")
		if len(parts) > 1 {
			return strings.Join(parts[:len(parts)-1], ":")
		}
	}
	return addr
}

func portPart(addr string) int {
	if strings.Contains(addr, ":") {
		parts := strings.Split(addr, ":")
		p := parts[len(parts)-1]
		i, _ := strconv.Atoi(p)
		return i
	}
	return 0
}

func durationString(v, def int) string {
	if v <= 0 {
		v = def
	}
	d := time.Duration(v) * time.Second
	// xray expects durations like "30s".
	return strconv.FormatInt(int64(d/time.Second), 10)
}
