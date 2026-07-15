package webui

import (
	"net"
	"net/http"
	"strings"
)

type sessionRow struct {
	SessionID           string `json:"session_id"`
	Node                string `json:"node"`
	Region              string `json:"region"`
	RemainingTTLSeconds int64  `json:"remaining_ttl_seconds"`
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
	bindings := s.affinity.List()
	rows := make([]sessionRow, 0, len(bindings))
	for _, binding := range bindings {
		rows = append(rows, sessionRow{
			SessionID:           binding.SessionID,
			Node:                binding.NodeAddress,
			Region:              binding.Region,
			RemainingTTLSeconds: int64(s.affinity.RemainingTTL(binding).Seconds()),
		})
	}
	jsonOK(w, rows)
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
