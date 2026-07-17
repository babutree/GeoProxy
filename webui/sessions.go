package webui

import (
	"net"
	"net/http"
	"strings"

	"github.com/babutree/GeoProxy/storage"
)

type sessionRow struct {
	SessionID                string `json:"session_id"`
	ProxyID                  int64  `json:"proxy_id"`
	RouteLabel               string `json:"route_label"`
	Node                     string `json:"node"` // 展示用出口节点：优先出口 IP，本机 mixed 地址不直接当出口
	BindAddress              string `json:"bind_address"`
	Region                   string `json:"region"`
	RegionReq                string `json:"region_req"`
	ExitIP                   string `json:"exit_ip"`
	Protocol                 string `json:"protocol"`
	Source                   string `json:"source"`
	SubscriptionName         string `json:"subscription_name"`
	Note                     string `json:"note"`
	QualityGrade             string `json:"quality_grade"`
	Latency                  int    `json:"latency"`
	DualProtocol             bool   `json:"dual_protocol"`
	LastActive               string `json:"last_active"`
	RemainingTTLSeconds      int64  `json:"remaining_ttl_seconds"`
	CooldownRemainingSeconds int64  `json:"cooldown_remaining_seconds"`
	ActiveSessionsOnProxy    int    `json:"active_sessions_on_proxy"`
	MaxSessionsPerProxy      int    `json:"max_sessions_per_proxy"`
}

// sessionRouteLabel 依据绑定的地域与会话 ID 还原路由标签的 DSL 展示形式
// （region-<region>-session-<sid>）。这是从可用绑定字段派生的展示值，
// 不是登录时的原始用户名串；绑定层未持久化原始路由 DSL。
func sessionRouteLabel(region, sessionID string) string {
	s := strings.TrimSpace(sessionID)
	if s == "" {
		return ""
	}
	if r := strings.ToLower(strings.TrimSpace(region)); r != "" && r != "unknown" {
		return "region-" + r + "-session-" + s
	}
	return "session-" + s
}

// proxyOccupancyRow is the per-node occupancy snapshot for lease observability.
type proxyOccupancyRow struct {
	ProxyID                  int64  `json:"proxy_id"`
	Address                  string `json:"address"`
	ActiveSessions           int    `json:"active_sessions"`
	MaxSessions              int    `json:"max_sessions"`
	CooldownRemainingSeconds int64  `json:"cooldown_remaining_seconds"`
	Note                     string `json:"note,omitempty"`
}

func (s *Server) apiSessions(w http.ResponseWriter, _ *http.Request) {
	// 与 occupancy 一致：非标准装配下 affinity 可能为 nil，不得 panic（RISK-03）。
	if s.affinity == nil {
		jsonOK(w, []sessionRow{})
		return
	}
	bindings := s.affinity.List()
	// 订阅名映射（来源展示）；失败不阻断会话列表。
	subNameByID := map[int64]string{}
	if s.storage != nil {
		if subs, err := s.storage.GetSubscriptions(); err == nil {
			for _, sub := range subs {
				subNameByID[sub.ID] = sub.Name
			}
		}
	}
	maxSessions := 0
	if cfg := s.configSnapshot(); cfg != nil {
		maxSessions = cfg.MaxSessionsPerProxy
	}
	// 每个 proxy 上的活跃 session 数（供节点占用条）。
	activeByProxy := map[int64]int{}
	for _, binding := range bindings {
		if binding.ProxyID > 0 {
			activeByProxy[binding.ProxyID]++
		}
	}
	rows := make([]sessionRow, 0, len(bindings))
	for _, binding := range bindings {
		region := strings.TrimSpace(binding.Region)
		row := sessionRow{
			SessionID:                binding.SessionID,
			ProxyID:                  binding.ProxyID,
			RouteLabel:               sessionRouteLabel(region, binding.SessionID),
			Node:                     binding.NodeAddress,
			BindAddress:              binding.NodeAddress,
			Region:                   region,
			RegionReq:                strings.ToLower(region),
			RemainingTTLSeconds:      int64(s.affinity.RemainingTTL(binding).Seconds()),
			MaxSessionsPerProxy:      maxSessions,
			ActiveSessionsOnProxy:    activeByProxy[binding.ProxyID],
			CooldownRemainingSeconds: int64(s.affinity.CooldownRemaining(binding.ProxyID).Seconds()),
		}
		if !binding.LastActive.IsZero() {
			row.LastActive = binding.LastActive.Local().Format("2006-01-02 15:04:05")
		}
		// 用 ProxyID 补全出口/协议/来源/品质/延迟。隧道绑定地址常为 127.0.0.1:mixed，
		// 展示出口节点时优先 exit_ip（真实出口），避免把本机 mixed 当成出口节点。
		if binding.ProxyID > 0 && s.storage != nil {
			if p, err := s.storage.GetProxyByID(binding.ProxyID); err == nil && p != nil {
				row.ExitIP = p.ExitIP
				row.QualityGrade = p.QualityGrade
				row.Latency = p.Latency
				row.Protocol = p.Protocol
				row.Source = p.Source
				row.Note = p.Note
				row.DualProtocol = p.DualProtocol
				if p.Source == storage.SourceSubscription {
					if name := subNameByID[p.SubscriptionID]; name != "" {
						row.SubscriptionName = name
					} else {
						row.SubscriptionName = "订阅"
					}
				} else if p.Source == storage.SourceManual {
					row.SubscriptionName = "手工"
				}
				// 出口节点展示：真实 exit_ip > 非本机 address > bind_address
				if p.ExitIP != "" {
					row.Node = p.ExitIP
				} else if p.Address != "" && !isLocalMixedDisplayAddress(p.Address) {
					row.Node = p.Address
				} else if isLocalMixedDisplayAddress(binding.NodeAddress) && p.Address != "" {
					// 仍是本地 mixed：至少展示存储 address（仍可能是 127.0.0.1），并依赖 exit_ip 字段
					row.Node = p.Address
				}
			}
		}
		rows = append(rows, row)
	}
	jsonOK(w, rows)
}

// isLocalMixedDisplayAddress 判断是否为本机 tunnel/mixed 绑定地址（不可当“出口节点”展示）。
func isLocalMixedDisplayAddress(addr string) bool {
	host := proxyAddressHost(addr)
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// buildProxyOccupancyRows aggregates active bindings into per-proxy occupancy rows.
// affinity==nil yields an empty slice. No credential fields.
func (s *Server) buildProxyOccupancyRows() []proxyOccupancyRow {
	if s.affinity == nil {
		return []proxyOccupancyRow{}
	}
	bindings := s.affinity.List()
	counts := make(map[int64]int)
	addressByID := make(map[int64]string)
	for _, binding := range bindings {
		if binding.ProxyID <= 0 {
			continue
		}
		counts[binding.ProxyID]++
		if _, ok := addressByID[binding.ProxyID]; !ok {
			addressByID[binding.ProxyID] = binding.NodeAddress
		}
	}
	maxSessions := 0
	if cfg := s.configSnapshot(); cfg != nil {
		maxSessions = cfg.MaxSessionsPerProxy
	}
	// 使用聚合阶段已记录的 binding.NodeAddress，避免逐节点 GetProxyByID 的 N+1 查询，
	// 同时消除对 s.storage 的依赖（occupancy 快照的地址即绑定时的节点地址）。
	rows := make([]proxyOccupancyRow, 0, len(counts))
	for proxyID, active := range counts {
		rows = append(rows, proxyOccupancyRow{
			ProxyID:                  proxyID,
			Address:                  addressByID[proxyID],
			ActiveSessions:           active,
			MaxSessions:              maxSessions,
			CooldownRemainingSeconds: int64(s.affinity.CooldownRemaining(proxyID).Seconds()),
		})
	}
	return rows
}

// apiProxyOccupancy returns per-proxy active session counts for authenticated admins.
// Only proxies with at least one non-expired binding are included. No credential fields.
func (s *Server) apiProxyOccupancy(w http.ResponseWriter, _ *http.Request) {
	jsonOK(w, s.buildProxyOccupancyRows())
}

// apiV1Occupancy is the read-only external occupancy API (API key auth).
// Private/internal node addresses (loopback, RFC1918, CGNAT 100.64/10,
// link-local 169.254/16, IPv6 loopback ::1, IPv6 ULA fc00::/7 and IPv6
// link-local fe80::/10) are redacted so external API-key callers cannot misuse
// an internal bind address as a direct dial target or learn the gateway's
// private topology. Public addresses are returned unchanged.
//
// This masking applies ONLY to the read-only endpoint. The admin endpoint
// (apiProxyOccupancy) intentionally leaves buildProxyOccupancyRows untouched
// and continues to show the real bind address.
func (s *Server) apiV1Occupancy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rows := s.buildProxyOccupancyRows()
	for i := range rows {
		if isPrivateOrInternalProxyAddress(rows[i].Address) {
			rows[i].Address = "gateway-local"
			rows[i].Note = "private/internal address redacted"
		}
	}
	jsonOK(w, rows)
}

// proxyAddressHost extracts the bare host from a proxy address, stripping any
// "host:port" wrapper and IPv6 brackets. Returns the trimmed input if it has no
// recognizable port separator.
func proxyAddressHost(addr string) string {
	host := strings.TrimSpace(addr)
	if host == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.Trim(host, "[]")
}

// isPrivateOrInternalProxyAddress reports whether the address should be redacted
// from the read-only occupancy API. It covers loopback/localhost plus every
// non-public range that a proxy could bind to but that must never be handed to
// an external caller:
//   - IPv4 loopback (127/8), RFC1918 (10/8, 172.16/12, 192.168/16),
//     CGNAT (100.64/10), link-local (169.254/16), unspecified (0.0.0.0)
//   - IPv6 loopback (::1), ULA (fc00::/7), link-local (fe80::/10),
//     unspecified (::)
//
// Public/global-unicast addresses return false so they remain visible.
func isPrivateOrInternalProxyAddress(addr string) bool {
	host := proxyAddressHost(addr)
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// Non-IP host (e.g. a hostname). Treat as non-public and redact to be
		// safe: the read-only API only ever exposes public IP:port targets.
		return true
	}
	return isPrivateOrInternalIP(ip)
}

func isPrivateOrInternalIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() {
		return true
	}
	// net.IP.IsPrivate covers RFC1918 and IPv6 ULA (fc00::/7) but NOT CGNAT
	// 100.64.0.0/10, which is shared address space that must not leak either.
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 100 && v4[1]&0xc0 == 0x40 {
			return true
		}
	}
	return false
}
