package upstream

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
)

type Type string

const (
	TypeSOCKS5      Type = "socks5"
	TypeHTTP        Type = "http"
	TypeShadowsocks Type = "shadowsocks"
	TypeVMess       Type = "vmess"
	TypeVLESS       Type = "vless"
	TypeTrojan      Type = "trojan"
)

type Spec struct {
	ID   string
	Name string
	Type Type

	Server string
	Port   int

	SOCKS5      *SOCKS5Config
	HTTP        *HTTPConfig
	Shadowsocks *ShadowsocksConfig
	VMess       *VMessConfig
	VLESS       *VLESSConfig
	Trojan      *TrojanConfig
}

type SOCKS5Config struct {
	Username string
	Password string
}

type HTTPConfig struct {
	Username string
	Password string
}

type ShadowsocksConfig struct {
	Method   string
	Password string
}

type VMessConfig struct {
	UUID           string
	AlterID        int
	Security       string
	TLS            bool
	SkipCertVerify bool
	ServerName     string

	Network string // tcp | ws | grpc
	WSPath  string
	Headers map[string]string
}

type VLESSConfig struct {
	UUID           string
	Flow           string
	TLS            bool
	SkipCertVerify bool
	ServerName     string

	Network string // tcp | ws | grpc
	WSPath  string
	Headers map[string]string
}

type TrojanConfig struct {
	Password       string
	TLS            bool
	SkipCertVerify bool
	ServerName     string

	Network string // tcp | ws | grpc
	WSPath  string
	Headers map[string]string
}

// Normalize computes a stable ID for the spec and returns a copy.
// ID is prefixed with "n-" for convenient xray tag selection.
func (s Spec) Normalize() Spec {
	s.ID = StableNodeID(s)
	return s
}

func StableNodeID(s Spec) string {
	sum := sha256.Sum256([]byte(canonicalString(s)))
	hex12 := hex.EncodeToString(sum[:])[:12]
	return "n-" + hex12
}

func canonicalString(s Spec) string {
	var parts []string
	parts = append(parts, "type="+string(s.Type))
	parts = append(parts, "server="+strings.TrimSpace(strings.ToLower(s.Server)))
	parts = append(parts, "port="+strconv.Itoa(s.Port))

	switch s.Type {
	case TypeSOCKS5:
		if s.SOCKS5 != nil {
			parts = append(parts, "user="+s.SOCKS5.Username)
			parts = append(parts, "pass="+s.SOCKS5.Password)
		}
	case TypeHTTP:
		if s.HTTP != nil {
			parts = append(parts, "user="+s.HTTP.Username)
			parts = append(parts, "pass="+s.HTTP.Password)
		}
	case TypeShadowsocks:
		if s.Shadowsocks != nil {
			parts = append(parts, "method="+strings.ToLower(strings.TrimSpace(s.Shadowsocks.Method)))
			parts = append(parts, "pass="+s.Shadowsocks.Password)
		}
	case TypeVMess:
		parts = append(parts, canonicalStream("vmess", s.VMess))
	case TypeVLESS:
		parts = append(parts, canonicalStream("vless", s.VLESS))
	case TypeTrojan:
		parts = append(parts, canonicalStream("trojan", s.Trojan))
	}

	return strings.Join(parts, "|")
}

func canonicalStream(prefix string, anyCfg any) string {
	switch cfg := anyCfg.(type) {
	case *VMessConfig:
		return canonicalStreamCommon(prefix,
			"uuid="+cfg.UUID,
			"aid="+strconv.Itoa(cfg.AlterID),
			"sec="+cfg.Security,
			"tls="+strconv.FormatBool(cfg.TLS),
			"skip="+strconv.FormatBool(cfg.SkipCertVerify),
			"sni="+cfg.ServerName,
			"net="+cfg.Network,
			"wspath="+cfg.WSPath,
			"hdr="+canonicalHeaders(cfg.Headers),
		)
	case *VLESSConfig:
		return canonicalStreamCommon(prefix,
			"uuid="+cfg.UUID,
			"flow="+cfg.Flow,
			"tls="+strconv.FormatBool(cfg.TLS),
			"skip="+strconv.FormatBool(cfg.SkipCertVerify),
			"sni="+cfg.ServerName,
			"net="+cfg.Network,
			"wspath="+cfg.WSPath,
			"hdr="+canonicalHeaders(cfg.Headers),
		)
	case *TrojanConfig:
		return canonicalStreamCommon(prefix,
			"pass="+cfg.Password,
			"tls="+strconv.FormatBool(cfg.TLS),
			"skip="+strconv.FormatBool(cfg.SkipCertVerify),
			"sni="+cfg.ServerName,
			"net="+cfg.Network,
			"wspath="+cfg.WSPath,
			"hdr="+canonicalHeaders(cfg.Headers),
		)
	default:
		return prefix + ":nil"
	}
}

func canonicalStreamCommon(prefix string, kv ...string) string {
	var out []string
	out = append(out, prefix)
	out = append(out, kv...)
	return strings.Join(out, ",")
}

func canonicalHeaders(h map[string]string) string {
	if len(h) == 0 {
		return ""
	}
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, strings.ToLower(k))
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(h[k])
	}
	return b.String()
}

func (s Spec) SafeSummary() map[string]any {
	out := map[string]any{
		"id":     s.ID,
		"name":   s.Name,
		"type":   string(s.Type),
		"server": s.Server,
		"port":   s.Port,
	}

	switch s.Type {
	case TypeSOCKS5:
		if s.SOCKS5 != nil && strings.TrimSpace(s.SOCKS5.Username) != "" {
			out["username"] = s.SOCKS5.Username
		}
	case TypeHTTP:
		if s.HTTP != nil && strings.TrimSpace(s.HTTP.Username) != "" {
			out["username"] = s.HTTP.Username
		}
	case TypeShadowsocks:
		if s.Shadowsocks != nil {
			out["method"] = s.Shadowsocks.Method
		}
	case TypeVMess:
		if s.VMess != nil {
			out["network"] = s.VMess.Network
			out["tls"] = s.VMess.TLS
			out["skip_cert_verify"] = s.VMess.SkipCertVerify
			if s.VMess.ServerName != "" {
				out["sni"] = s.VMess.ServerName
			}
		}
	case TypeVLESS:
		if s.VLESS != nil {
			out["network"] = s.VLESS.Network
			out["tls"] = s.VLESS.TLS
			out["skip_cert_verify"] = s.VLESS.SkipCertVerify
			if s.VLESS.ServerName != "" {
				out["sni"] = s.VLESS.ServerName
			}
			if s.VLESS.Flow != "" {
				out["flow"] = s.VLESS.Flow
			}
		}
	case TypeTrojan:
		if s.Trojan != nil {
			out["network"] = s.Trojan.Network
			out["tls"] = s.Trojan.TLS
			out["skip_cert_verify"] = s.Trojan.SkipCertVerify
			if s.Trojan.ServerName != "" {
				out["sni"] = s.Trojan.ServerName
			}
		}
	}
	return out
}

func Deduplicate(in []Spec) []Spec {
	seen := make(map[string]struct{}, len(in))
	out := make([]Spec, 0, len(in))
	for _, s := range in {
		if strings.TrimSpace(s.ID) == "" {
			s = s.Normalize()
		}
		if _, ok := seen[s.ID]; ok {
			continue
		}
		seen[s.ID] = struct{}{}
		out = append(out, s)
	}
	return out
}

