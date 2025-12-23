package clash

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/CodeBoy2006/EasyProxyPool/internal/upstream"
	"gopkg.in/yaml.v3"
)

type ParseReport struct {
	Specs        []upstream.Spec
	SkippedByType map[string]int
	Problems     []string
}

type ClashConfig struct {
	Proxies []map[string]any `yaml:"proxies"`
}

func ParseYAML(data []byte) (ParseReport, error) {
	var cfg ClashConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return ParseReport{}, fmt.Errorf("parse clash yaml: %w", err)
	}

	report := ParseReport{
		SkippedByType: make(map[string]int),
	}

	for _, raw := range cfg.Proxies {
		name := getString(raw, "name")
		typ := strings.ToLower(getString(raw, "type"))
		if strings.TrimSpace(typ) == "" {
			report.Problems = append(report.Problems, fmt.Sprintf("proxy(name=%q): missing type", name))
			continue
		}

		spec, ok := parseProxy(raw, name, typ, &report)
		if !ok {
			report.SkippedByType[typ]++
			continue
		}
		report.Specs = append(report.Specs, spec.Normalize())
	}

	report.Specs = upstream.Deduplicate(report.Specs)
	return report, nil
}

func parseProxy(raw map[string]any, name, typ string, report *ParseReport) (upstream.Spec, bool) {
	server := getString(raw, "server")
	port := getInt(raw, "port")

	switch typ {
	case "socks5":
		if server == "" || port <= 0 {
			report.Problems = append(report.Problems, fmt.Sprintf("proxy(name=%q,type=%s): missing server/port", name, typ))
			return upstream.Spec{}, false
		}
		return upstream.Spec{
			Name:   name,
			Type:   upstream.TypeSOCKS5,
			Server: server,
			Port:   port,
			SOCKS5: &upstream.SOCKS5Config{
				Username: getString(raw, "username"),
				Password: getString(raw, "password"),
			},
		}, true

	case "http":
		if server == "" || port <= 0 {
			report.Problems = append(report.Problems, fmt.Sprintf("proxy(name=%q,type=%s): missing server/port", name, typ))
			return upstream.Spec{}, false
		}
		return upstream.Spec{
			Name:   name,
			Type:   upstream.TypeHTTP,
			Server: server,
			Port:   port,
			HTTP: &upstream.HTTPConfig{
				Username: getString(raw, "username"),
				Password: getString(raw, "password"),
			},
		}, true

	case "ss":
		method := getString(raw, "cipher")
		pass := getString(raw, "password")
		if server == "" || port <= 0 || method == "" || pass == "" {
			report.Problems = append(report.Problems, fmt.Sprintf("proxy(name=%q,type=%s): missing server/port/cipher/password", name, typ))
			return upstream.Spec{}, false
		}
		return upstream.Spec{
			Name:   name,
			Type:   upstream.TypeShadowsocks,
			Server: server,
			Port:   port,
			Shadowsocks: &upstream.ShadowsocksConfig{
				Method:   method,
				Password: pass,
			},
		}, true

	case "vmess":
		uuid := getString(raw, "uuid")
		if server == "" || port <= 0 || uuid == "" {
			report.Problems = append(report.Problems, fmt.Sprintf("proxy(name=%q,type=%s): missing server/port/uuid", name, typ))
			return upstream.Spec{}, false
		}
		alterID := getInt(raw, "alterId", "alter_id", "aid")
		security := getString(raw, "cipher")
		if security == "" {
			security = getString(raw, "security")
		}
		if security == "" {
			security = "auto"
		}

		network := getString(raw, "network")
		wsPath, wsHeaders := parseWSOpts(raw)
		return upstream.Spec{
			Name:   name,
			Type:   upstream.TypeVMess,
			Server: server,
			Port:   port,
			VMess: &upstream.VMessConfig{
				UUID:           uuid,
				AlterID:        alterID,
				Security:       security,
				TLS:            getBool(raw, "tls"),
				SkipCertVerify: getBool(raw, "skip-cert-verify", "skip_cert_verify"),
				ServerName:     firstNonEmpty(getString(raw, "servername"), getString(raw, "sni")),
				Network:        network,
				WSPath:         wsPath,
				Headers:        wsHeaders,
			},
		}, true

	case "vless":
		uuid := getString(raw, "uuid")
		if server == "" || port <= 0 || uuid == "" {
			report.Problems = append(report.Problems, fmt.Sprintf("proxy(name=%q,type=%s): missing server/port/uuid", name, typ))
			return upstream.Spec{}, false
		}
		network := getString(raw, "network")
		wsPath, wsHeaders := parseWSOpts(raw)
		return upstream.Spec{
			Name:   name,
			Type:   upstream.TypeVLESS,
			Server: server,
			Port:   port,
			VLESS: &upstream.VLESSConfig{
				UUID:           uuid,
				Flow:           getString(raw, "flow"),
				TLS:            getBool(raw, "tls"),
				SkipCertVerify: getBool(raw, "skip-cert-verify", "skip_cert_verify"),
				ServerName:     firstNonEmpty(getString(raw, "servername"), getString(raw, "sni")),
				Network:        network,
				WSPath:         wsPath,
				Headers:        wsHeaders,
			},
		}, true

	case "trojan":
		pass := getString(raw, "password")
		if server == "" || port <= 0 || pass == "" {
			report.Problems = append(report.Problems, fmt.Sprintf("proxy(name=%q,type=%s): missing server/port/password", name, typ))
			return upstream.Spec{}, false
		}
		network := getString(raw, "network")
		wsPath, wsHeaders := parseWSOpts(raw)
		return upstream.Spec{
			Name:   name,
			Type:   upstream.TypeTrojan,
			Server: server,
			Port:   port,
			Trojan: &upstream.TrojanConfig{
				Password:       pass,
				TLS:            true, // trojan implies tls
				SkipCertVerify: getBool(raw, "skip-cert-verify", "skip_cert_verify"),
				ServerName:     firstNonEmpty(getString(raw, "sni"), getString(raw, "servername")),
				Network:        network,
				WSPath:         wsPath,
				Headers:        wsHeaders,
			},
		}, true

	default:
		// Parsed but not supported by our adapter MVP.
		return upstream.Spec{}, false
	}
}

func parseWSOpts(raw map[string]any) (path string, headers map[string]string) {
	v, ok := raw["ws-opts"]
	if !ok {
		return "", nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return "", nil
	}
	path = getString(m, "path")
	h := make(map[string]string)
	if hv, ok := m["headers"]; ok {
		switch hm := hv.(type) {
		case map[string]any:
			for k, v := range hm {
				if s, ok := v.(string); ok {
					h[k] = s
				}
			}
		case map[string]string:
			for k, v := range hm {
				h[k] = v
			}
		}
	}
	if len(h) == 0 {
		h = nil
	}
	return path, h
}

func getString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		switch x := v.(type) {
		case string:
			return strings.TrimSpace(x)
		default:
			// YAML may decode numbers/bools; stringify carefully.
			if s := fmt.Sprint(x); s != "" && s != "<nil>" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func getInt(m map[string]any, keys ...string) int {
	for _, k := range keys {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		switch x := v.(type) {
		case int:
			return x
		case int64:
			return int(x)
		case float64:
			return int(x)
		case string:
			i, _ := strconv.Atoi(strings.TrimSpace(x))
			if i != 0 {
				return i
			}
		default:
			i, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprint(x)))
			if i != 0 {
				return i
			}
		}
	}
	return 0
}

func getBool(m map[string]any, keys ...string) bool {
	for _, k := range keys {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		switch x := v.(type) {
		case bool:
			return x
		case string:
			switch strings.ToLower(strings.TrimSpace(x)) {
			case "true", "1", "yes", "y":
				return true
			}
		}
	}
	return false
}

func firstNonEmpty(vv ...string) string {
	for _, v := range vv {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

