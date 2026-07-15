package webui

import (
	"net"
	"net/http"
	"strings"

	"goproxy/config"
)

// resolvePublicHost picks an externally reachable host for connect views.
// Priority: cfg.PublicHost > public_ip cache > request Host.
// Loopback / localhost values are skipped. Never probes the network.
// PUBLIC_HOST 仅在首次启动时导入；运行期读取会让过期环境变量覆盖已持久化的 config.json。
func resolvePublicHost(cfg *config.Config, r *http.Request) (host string, unresolved bool) {
	candidates := make([]string, 0, 3)
	if cfg != nil {
		candidates = append(candidates, strings.TrimSpace(cfg.PublicHost))
	}
	candidates = append(candidates, cachedPublicIPOnly())
	if r != nil {
		candidates = append(candidates, hostFromRequest(r))
	}

	for _, c := range candidates {
		h := normalizePublicHostCandidate(c)
		if h == "" || isUnusablePublicHost(h) {
			continue
		}
		return h, false
	}
	return "", true
}

// cachedPublicIPOnly reads the in-process public IP cache without issuing probes.
func cachedPublicIPOnly() string {
	pubIP.mu.Lock()
	v := pubIP.value
	pubIP.mu.Unlock()
	return v
}

func hostFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	return stripHostPort(r.Host)
}

func stripHostPort(hostport string) string {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" {
		return ""
	}
	h, _, err := net.SplitHostPort(hostport)
	if err == nil {
		return h
	}
	// No port, or malformed with brackets only (e.g. "[::1]").
	return strings.Trim(hostport, "[]")
}

func normalizePublicHostCandidate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Allow accidental host:port in override/env.
	if strings.Contains(raw, ":") {
		if h, _, err := net.SplitHostPort(raw); err == nil {
			return h
		}
	}
	return strings.Trim(raw, "[]")
}

func isUnusablePublicHost(h string) bool {
	h = strings.TrimSpace(h)
	if h == "" {
		return true
	}
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	if ip != nil {
		return isPrivateOrInternalIP(ip)
	}
	return false
}
