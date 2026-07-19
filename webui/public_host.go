package webui

import (
	"net"
	"net/http"
	"strings"

	"github.com/babutree/GeoProxy/config"
)

// resolvePublicHost 为连接视图选择可从外部访问的主机。
// 优先级：cfg.PublicHost > public_ip 缓存 > 请求 Host。
// 跳过回环地址和 localhost，且绝不发起网络探测。
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

// cachedPublicIPOnly 只读取进程内公网 IP 缓存，不发起探测。
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
	// 没有端口，或只有方括号且格式错误（如 "[::1]"）。
	return strings.Trim(hostport, "[]")
}

func normalizePublicHostCandidate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// 允许覆盖值或环境变量中意外包含 host:port。
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
