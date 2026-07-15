package webui

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"goproxy/storage"
)

const (
	nodesAPIDefaultLimit = 500
	nodesAPIMaxLimit     = 2000
)

// apiV1Nodes serves GET /api/v1/nodes (read-only API key auth via middleware).
func (s *Server) apiV1Nodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	filter, connectMode, err := parseNodeAPIFilter(r)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	cfg := s.configSnapshot()
	publicHost, hostUnresolved := resolvePublicHost(cfg, r)
	socksPort := stripLeadingColonPort(cfg.SOCKS5Port)
	httpPort := stripLeadingColonPort(cfg.HTTPPort)
	usernameBase := "acct"
	if strings.TrimSpace(cfg.ProxyAuthUsername) != "" {
		usernameBase = strings.TrimSpace(cfg.ProxyAuthUsername)
	}

	if connectMode != "" {
		total, nodes, err := s.listNodesAPIViewWithConnectFilter(filter, connectMode, publicHost, hostUnresolved, socksPort, httpPort, usernameBase)
		if err != nil {
			jsonError(w, "failed to list nodes", http.StatusInternalServerError)
			return
		}
		jsonOK(w, map[string]any{
			"total": total,
			"count": len(nodes),
			"nodes": nodes,
		})
		return
	}

	proxies, total, err := s.storage.ListNodesForAPI(filter)
	if err != nil {
		jsonError(w, "failed to list nodes", http.StatusInternalServerError)
		return
	}
	nodes := make([]map[string]any, 0, len(proxies))
	for _, p := range proxies {
		nodes = append(nodes, buildNodeAPIView(p, publicHost, hostUnresolved, socksPort, httpPort, usernameBase))
	}

	jsonOK(w, map[string]any{
		"total": total,
		"count": len(nodes),
		"nodes": nodes,
	})
}

func parseNodeAPIFilter(r *http.Request) (storage.NodeAPIFilter, string, error) {
	q := r.URL.Query()
	filter := storage.NodeAPIFilter{
		Region:   strings.ToLower(strings.TrimSpace(q.Get("region"))),
		Protocol: strings.ToLower(strings.TrimSpace(q.Get("protocol"))),
		Source:   strings.ToLower(strings.TrimSpace(q.Get("source"))),
	}
	status := strings.ToLower(strings.TrimSpace(q.Get("status")))
	if status != "" && status != "all" {
		return filter, "", fmt.Errorf("invalid status")
	}
	filter.Status = status
	cf := strings.ToLower(strings.TrimSpace(q.Get("cf")))
	switch cf {
	case "", "open", "blocked":
		filter.CF = cf
	default:
		return filter, "", fmt.Errorf("invalid cf")
	}
	if raw := strings.TrimSpace(q.Get("max_abuse")); raw != "" {
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || v < 0 || v > 1 {
			return filter, "", fmt.Errorf("invalid max_abuse")
		}
		filter.MaxAbuse = &v
	}
	if raw := strings.TrimSpace(q.Get("ai")); raw != "" {
		parts := strings.Split(raw, ",")
		filter.AI = make([]string, 0, len(parts))
		for _, p := range parts {
			if t := strings.TrimSpace(p); t != "" {
				filter.AI = append(filter.AI, t)
			}
		}
	}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 || v > nodesAPIMaxLimit {
			return filter, "", fmt.Errorf("invalid limit")
		}
		filter.Limit = v
	}
	if raw := strings.TrimSpace(q.Get("offset")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			return filter, "", fmt.Errorf("invalid offset")
		}
		filter.Offset = v
	}
	connect := strings.ToLower(strings.TrimSpace(q.Get("connect")))
	switch connect {
	case "", "direct", "gateway":
		return filter, connect, nil
	}
	return filter, "", fmt.Errorf("invalid connect")
}

func (s *Server) listNodesAPIViewWithConnectFilter(filter storage.NodeAPIFilter, connectMode, publicHost string, hostUnresolved bool, socksPort, httpPort int, usernameBase string) (int, []map[string]any, error) {
	pageLimit := filter.Limit
	if pageLimit == 0 {
		pageLimit = nodesAPIDefaultLimit
	}
	pageOffset := filter.Offset

	scanFilter := filter
	scanFilter.Limit = nodesAPIMaxLimit
	scanFilter.Offset = 0

	total := 0
	nodes := make([]map[string]any, 0, pageLimit)
	for {
		proxies, filteredTotal, err := s.storage.ListNodesForAPI(scanFilter)
		if err != nil {
			return 0, nil, err
		}
		for _, p := range proxies {
			if connectModeForProxy(p) != connectMode {
				continue
			}
			total++
			if total <= pageOffset || len(nodes) >= pageLimit {
				continue
			}
			nodes = append(nodes, buildNodeAPIView(p, publicHost, hostUnresolved, socksPort, httpPort, usernameBase))
		}
		if len(proxies) == 0 || scanFilter.Offset+len(proxies) >= filteredTotal {
			break
		}
		scanFilter.Offset += len(proxies)
	}

	return total, nodes, nil
}

func buildNodeAPIView(p storage.Proxy, publicHost string, hostUnresolved bool, socksPort, httpPort int, usernameBase string) map[string]any {
	connect := buildConnectView(p, publicHost, hostUnresolved, socksPort, httpPort, usernameBase)
	lastCheck := ""
	if !p.LastCheck.IsZero() {
		lastCheck = p.LastCheck.UTC().Format(time.RFC3339)
	}
	return map[string]any{
		"id":              p.ID,
		"protocol":        p.Protocol,
		"source":          p.Source,
		"region":          p.Region,
		"region_source":   p.RegionSource,
		"connect":         connect,
		"exit_ip":         p.ExitIP,
		"exit_location":   p.ExitLocation,
		"latency_ms":      p.Latency,
		"quality_grade":   p.QualityGrade,
		"purity":          buildPurityView(p),
		"cf_blocked":      p.CFBlocked,
		"ai_reachability": parseAIReachabilityObject(p.AIReachability),
		"last_check":      lastCheck,
		"status":          p.Status,
	}
}

func buildConnectView(p storage.Proxy, publicHost string, hostUnresolved bool, socksPort, httpPort int, usernameBase string) map[string]any {
	if isGatewayNode(p) {
		conn := map[string]any{
			"mode":                "gateway",
			"host":                publicHost,
			"gateway_socks5_port": socksPort,
			"gateway_http_port":   httpPort,
			"username_hint":       gatewayUsernameHint(usernameBase, p.Region),
			"note":                "需网关代理认证；密码见部署配置。host 为空时请用部署域名/请求 Host。",
		}
		if hostUnresolved || publicHost == "" {
			conn["host_unresolved"] = true
		}
		return conn
	}
	host, port := splitAddressHostPort(p.Address)
	return map[string]any{
		"mode":          "direct",
		"host":          host,
		"port":          port,
		"dual_protocol": p.DualProtocol,
	}
}

func isGatewayNode(p storage.Proxy) bool {
	if p.DualProtocol {
		return true
	}
	return isPrivateOrInternalProxyAddress(p.Address)
}

func connectModeForProxy(p storage.Proxy) string {
	if isGatewayNode(p) {
		return "gateway"
	}
	return "direct"
}

func splitAddressHostPort(address string) (string, int) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", 0
	}
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		// bare host without port
		return strings.Trim(address, "[]"), 0
	}
	port, _ := strconv.Atoi(portStr)
	return host, port
}

func stripLeadingColonPort(raw string) int {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, ":")
	if raw == "" {
		return 0
	}
	n, _ := strconv.Atoi(raw)
	return n
}

func buildPurityView(p storage.Proxy) map[string]any {
	flags := []string{}
	if strings.TrimSpace(p.IPAPIFlags) != "" {
		for _, part := range strings.Split(p.IPAPIFlags, ",") {
			if t := strings.TrimSpace(part); t != "" {
				flags = append(flags, t)
			}
		}
	}
	return map[string]any{
		"ipapiis_abuse_score": p.IPAPIIsScore,
		"ipapi_flags":         flags,
		"ipapi_flags_seen":    p.IPAPIFlagsSeen,
	}
}

func parseAIReachabilityObject(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}
	}
	var m map[string]int
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func gatewayUsernameHint(base, region string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "acct"
	}
	region = strings.ToLower(strings.TrimSpace(region))
	if region == "" {
		region = "any"
	}
	return base + "-region-" + region + "-session-api"
}
