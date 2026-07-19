package storage

import "database/sql"

// inboundProtocolSQL 返回入口协议能力谓词及其规范化参数。
// mixed 节点仅对 HTTP/SOCKS5 放宽；未知协议仍须精确匹配存储协议。
func inboundProtocolSQL(protocol string) (string, []interface{}) {
	protocol = normalizeProtocol(protocol)
	if dualSupportsInboundProtocol(protocol) {
		return `(protocol = ? OR dual_protocol = 1)`, []interface{}{protocol}
	}
	return `protocol = ?`, []interface{}{protocol}
}

func dualSupportsInboundProtocol(protocol string) bool {
	return protocol == "http" || protocol == "socks5"
}

func proxySupportsInboundProtocol(proxy Proxy, protocol string) bool {
	protocol = normalizeProtocol(protocol)
	return normalizeProtocol(proxy.Protocol) == protocol ||
		(proxy.DualProtocol && dualSupportsInboundProtocol(protocol))
}

// GetAverageLatency 按存储的物理协议获取平均延迟，不展开 dual 入口能力。
func (s *Storage) GetAverageLatency(protocol string) (int, error) {
	var avg sql.NullFloat64
	err := s.db.QueryRow(
		`SELECT AVG(latency) FROM proxies
		 WHERE protocol = ? AND status = 'active' AND user_paused = 0 AND latency > 0
		   AND `+selectableSubscriptionScopeSQL,
		protocol,
	).Scan(&avg)
	if err != nil || !avg.Valid {
		return 0, err
	}
	return int(avg.Float64), nil
}

// GetQualityDistribution 获取质量分布统计
func (s *Storage) GetQualityDistribution() (map[string]int, error) {
	rows, err := s.db.Query(
		`SELECT quality_grade, COUNT(*) as count 
		 FROM proxies 
		 WHERE status = 'active' AND user_paused = 0 AND fail_count < 3
		   AND ` + selectableSubscriptionScopeSQL + `
		 GROUP BY quality_grade`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dist := make(map[string]int)
	for rows.Next() {
		var grade string
		var count int
		if err := rows.Scan(&grade, &count); err != nil {
			return nil, err
		}
		dist[grade] = count
	}
	return dist, nil
}

// CountAll 返回所有可用节点数量
func (s *Storage) CountAll() (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM proxies
		 WHERE status IN ('active', 'degraded') AND user_paused = 0 AND fail_count < 3
		   AND ` + selectableSubscriptionScopeSQL,
	).Scan(&count)
	return count, err
}

// CountAvailableByProtocol 按入口协议统计可用节点数量。
// dual_protocol=1（sing-box mixed）同时计入 http 与 socks5：与节点列表徽章一致，
// 避免「列表显示几百 SOCKS5+HTTP 但顶部 HTTP 可用只有纯 protocol=http 的 2 个」。
func (s *Storage) CountAvailableByProtocol(protocol string) (int, error) {
	protocolWhere, args := inboundProtocolSQL(protocol)
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM proxies
		 WHERE status IN ('active', 'degraded') AND user_paused = 0 AND fail_count < 3
		   AND `+protocolWhere+`
		   AND `+selectableSubscriptionScopeSQL,
		args...,
	).Scan(&count)
	return count, err
}

// CountBySource 按来源统计可用代理数量
func (s *Storage) CountBySource(source string) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM proxies
		 WHERE source = ? AND status IN ('active', 'degraded') AND user_paused = 0 AND fail_count < 3
		   AND `+selectableSubscriptionScopeSQL,
		source,
	).Scan(&count)
	return count, err
}
