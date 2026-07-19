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

// proxyOccupancyRow 是用于租约可观测性的单节点占用快照。
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

// buildProxyOccupancyRows 将活跃绑定聚合为逐节点占用记录。
// affinity==nil 时返回空切片，且不包含凭据字段。
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

// apiProxyOccupancy 返回已认证管理员可见的逐节点活跃会话数。
// 只包含至少一个未过期绑定的节点，且不包含凭据字段。
func (s *Server) apiProxyOccupancy(w http.ResponseWriter, _ *http.Request) {
	jsonOK(w, s.buildProxyOccupancyRows())
}

// apiV1Occupancy 是只读外部占用 API，使用 API Key 鉴权。
// 私有或内部节点地址会被脱敏，包括回环地址、RFC1918、CGNAT 100.64/10、
// link-local 169.254/16、IPv6 回环 ::1、IPv6 ULA fc00::/7 和
// IPv6 link-local fe80::/10，防止外部 API Key 调用方把内部绑定地址误作
// 直连目标，或据此获知网关的私有拓扑。公网地址保持原样。
//
// 此脱敏仅应用于只读端点。管理员端点 apiProxyOccupancy 有意保留
// buildProxyOccupancyRows 的原始结果，继续显示真实绑定地址。
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

// proxyAddressHost 从代理地址中提取裸主机，去掉 host:port 包装和 IPv6 方括号。
// 如果没有可识别的端口分隔符，则返回去除首尾空白后的输入。
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

// isPrivateOrInternalProxyAddress 判断地址是否应从只读占用 API 中脱敏。
// 覆盖回环地址、localhost，以及代理可能绑定但绝不能交给外部调用方的所有非公网地址：
//   - IPv4 回环（127/8）、RFC1918（10/8、172.16/12、192.168/16）、
//     CGNAT（100.64/10）、link-local（169.254/16）、未指定地址（0.0.0.0）
//   - IPv6 回环（::1）、ULA（fc00::/7）、link-local（fe80::/10）、未指定地址（::）
//
// 公网或全局单播地址返回 false，因此保持可见。
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
		// 非 IP 主机（如主机名）按非公网地址处理并脱敏；
		// 只读 API 只会公开公网 IP:port 目标。
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
	// net.IP.IsPrivate 覆盖 RFC1918 和 IPv6 ULA（fc00::/7），但不覆盖 CGNAT
	// 100.64.0.0/10；后者属于共享地址空间，也不能泄露。
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 100 && v4[1]&0xc0 == 0x40 {
			return true
		}
	}
	return false
}
