package httpproxy

import (
	"errors"
	"net/http"
	"strings"

	"github.com/CodeBoy2006/EasyProxyPool/internal/config"
	"github.com/CodeBoy2006/EasyProxyPool/internal/pool"
)

const (
	headerSticky      = "X-EasyProxyPool-Sticky"
	headerFailover    = "X-EasyProxyPool-Failover"
	headerUpstream    = "X-EasyProxyPool-Upstream"
	headerSession     = "X-EasyProxyPool-Session"
	headerTraceparent = "traceparent"
)

type requestStickyPolicy struct {
	sessionKey  string
	forceSticky *bool
	failover    string
	forceKey    string
}

func parseTraceIDFromTraceparent(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	if idx := strings.IndexByte(v, ','); idx >= 0 {
		v = strings.TrimSpace(v[:idx])
	}
	parts := strings.Split(v, "-")
	if len(parts) != 4 {
		return "", false
	}
	traceID := strings.TrimSpace(parts[1])
	if len(traceID) != 32 {
		return "", false
	}
	allZero := true
	for _, c := range traceID {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return "", false
		}
		if c != '0' {
			allZero = false
		}
	}
	if allZero {
		return "", false
	}
	return strings.ToLower(traceID), true
}

func parseBoolLike(v string) (bool, bool) {
	v = strings.TrimSpace(strings.ToLower(v))
	switch v {
	case "1", "true", "t", "yes", "y", "on", "enable", "enabled":
		return true, true
	case "0", "false", "f", "no", "n", "off", "disable", "disabled":
		return false, true
	default:
		return false, false
	}
}

func sessionKeyFromRequest(allowHeaderOverride bool, r *http.Request) string {
	if allowHeaderOverride {
		if v := strings.TrimSpace(r.Header.Get(headerSession)); v != "" {
			return v
		}
	}

	user, _, ok := parseBasicAuth(r.Header.Get("Proxy-Authorization"))
	if ok && strings.TrimSpace(user) != "" {
		return user
	}

	if traceID, ok := parseTraceIDFromTraceparent(r.Header.Get(headerTraceparent)); ok {
		return traceID
	}

	return ""
}

func stickyPolicyFromRequest(sel config.SelectionConfig, r *http.Request) (requestStickyPolicy, error) {
	p := requestStickyPolicy{
		failover: sel.Sticky.Failover,
	}

	headerOverride := sel.Sticky.HeaderOverride == nil || *sel.Sticky.HeaderOverride
	if headerOverride {
		if v := strings.TrimSpace(r.Header.Get(headerUpstream)); v != "" {
			p.forceKey = v
		}
		if v := strings.TrimSpace(r.Header.Get(headerSticky)); v != "" {
			b, ok := parseBoolLike(v)
			if !ok {
				return requestStickyPolicy{}, errors.New("invalid X-EasyProxyPool-Sticky (use on/off)")
			}
			p.forceSticky = &b
		}
		if v := strings.TrimSpace(r.Header.Get(headerFailover)); v != "" {
			v = strings.ToLower(v)
			switch v {
			case "soft", "hard":
				p.failover = v
			default:
				return requestStickyPolicy{}, errors.New("invalid X-EasyProxyPool-Failover (use soft/hard)")
			}
		}
	}

	p.sessionKey = sessionKeyFromRequest(headerOverride, r)
	return p, nil
}

func hrwScore(sessionKey, nodeKey string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	var h uint64 = offset64
	for i := 0; i < len(sessionKey); i++ {
		h ^= uint64(sessionKey[i])
		h *= prime64
	}
	h ^= 0
	h *= prime64
	for i := 0; i < len(nodeKey); i++ {
		h ^= uint64(nodeKey[i])
		h *= prime64
	}
	return h
}

func pickRendezvous(candidates []pool.Entry, sessionKey string, exclude map[string]struct{}) (pool.Entry, bool) {
	var best pool.Entry
	var bestScore uint64
	found := false

	for i := range candidates {
		k := candidates[i].Key()
		if exclude != nil {
			if _, ok := exclude[k]; ok {
				continue
			}
		}
		score := hrwScore(sessionKey, k)
		if !found || score > bestScore {
			best = candidates[i]
			bestScore = score
			found = true
		}
	}

	return best, found
}
