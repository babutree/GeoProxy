package storage

import (
	"fmt"
	"math/rand"
	"time"
)

// GetRandom 随机取一个可用代理（优先选择质量高的）
func (s *Storage) GetRandom() (*Proxy, error) {
	rows, err := s.db.Query(
		`SELECT ` + proxyColumns + `
		 FROM proxies
		 WHERE status = 'active' AND fail_count < 3
		 ORDER BY
		   CASE quality_grade
		     WHEN 'S' THEN 1
		     WHEN 'A' THEN 2
		     WHEN 'B' THEN 3
		     ELSE 4
		   END,
		   RANDOM()
		 LIMIT 1`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if rows.Next() {
		return scanProxy(rows)
	}
	return nil, fmt.Errorf("no available proxy")
}

// GetAll 获取所有可用代理
func (s *Storage) GetAll() ([]Proxy, error) {
	return s.GetAllFiltered("")
}

// GetAllFiltered 获取可用节点（可按来源过滤）
// sourceFilter: "" = 全部, "subscription" = 订阅节点, "manual" = 手动节点
func (s *Storage) GetAllFiltered(sourceFilter string) ([]Proxy, error) {
	query := `SELECT ` + proxyColumns + `
		 FROM proxies
		 WHERE status IN ('active', 'degraded') AND fail_count < 3`
	var args []interface{}
	if sourceFilter != "" {
		query += ` AND source = ?`
		args = append(args, sourceFilter)
	}
	query += ` ORDER BY latency ASC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var proxies []Proxy
	for rows.Next() {
		p, err := scanProxy(rows)
		if err != nil {
			return nil, err
		}
		proxies = append(proxies, *p)
	}
	return proxies, nil
}

// GetRandomExclude 排除指定地址随机取一个
func (s *Storage) GetRandomExclude(excludes []string) (*Proxy, error) {
	return s.GetRandomExcludeFiltered(excludes, "")
}

// GetRandomExcludeFiltered 排除指定地址随机取一个（可按来源过滤）
func (s *Storage) GetRandomExcludeFiltered(excludes []string, sourceFilter string) (*Proxy, error) {
	proxies, err := s.GetAllFiltered(sourceFilter)
	if err != nil {
		return nil, err
	}

	excludeMap := make(map[string]bool)
	for _, e := range excludes {
		excludeMap[e] = true
	}

	var available []Proxy
	for _, p := range proxies {
		if !excludeMap[p.Address] {
			available = append(available, p)
		}
	}

	if len(available) == 0 {
		if sourceFilter != "" {
			return nil, fmt.Errorf("no available %s proxy", sourceFilter)
		}
		return nil, fmt.Errorf("no available proxy")
	}

	p := available[rand.Intn(len(available))]
	return &p, nil
}

// GetLowestLatencyExclude 排除指定地址后获取延迟最低的代理
func (s *Storage) GetLowestLatencyExclude(excludes []string) (*Proxy, error) {
	return s.GetLowestLatencyExcludeFiltered(excludes, "")
}

// GetLowestLatencyExcludeFiltered 排除指定地址后获取延迟最低的代理（可按来源过滤）
func (s *Storage) GetLowestLatencyExcludeFiltered(excludes []string, sourceFilter string) (*Proxy, error) {
	proxies, err := s.GetAllFiltered(sourceFilter)
	if err != nil {
		return nil, err
	}

	excludeMap := make(map[string]bool)
	for _, e := range excludes {
		excludeMap[e] = true
	}

	for _, p := range proxies {
		if !excludeMap[p.Address] {
			proxy := p
			return &proxy, nil
		}
	}

	return nil, fmt.Errorf("no available proxy")
}

// GetRandomByProtocolExclude 按协议获取随机代理（排除已尝试的）
func (s *Storage) GetRandomByProtocolExclude(protocol string, excludes []string) (*Proxy, error) {
	return s.GetRandomByProtocolExcludeFiltered(protocol, excludes, "")
}

// GetRandomByProtocolExcludeFiltered 按协议获取随机代理（可按来源过滤）
func (s *Storage) GetRandomByProtocolExcludeFiltered(protocol string, excludes []string, sourceFilter string) (*Proxy, error) {
	proxies, err := s.GetAllFiltered(sourceFilter)
	if err != nil {
		return nil, err
	}

	excludeMap := make(map[string]bool)
	for _, e := range excludes {
		excludeMap[e] = true
	}

	var available []Proxy
	for _, p := range proxies {
		if p.Protocol == protocol && !excludeMap[p.Address] {
			available = append(available, p)
		}
	}

	if len(available) == 0 {
		return nil, fmt.Errorf("no %s proxy available", protocol)
	}

	proxy := available[time.Now().UnixNano()%int64(len(available))]
	return &proxy, nil
}

// GetLowestLatencyByProtocolExclude 按协议获取最低延迟代理（排除已尝试的）
func (s *Storage) GetLowestLatencyByProtocolExclude(protocol string, excludes []string) (*Proxy, error) {
	return s.GetLowestLatencyByProtocolExcludeFiltered(protocol, excludes, "")
}

// GetLowestLatencyByProtocolExcludeFiltered 按协议获取最低延迟代理（可按来源过滤）
func (s *Storage) GetLowestLatencyByProtocolExcludeFiltered(protocol string, excludes []string, sourceFilter string) (*Proxy, error) {
	proxies, err := s.GetAllFiltered(sourceFilter)
	if err != nil {
		return nil, err
	}

	excludeMap := make(map[string]bool)
	for _, e := range excludes {
		excludeMap[e] = true
	}

	for _, p := range proxies {
		if p.Protocol == protocol && !excludeMap[p.Address] {
			proxy := p
			return &proxy, nil
		}
	}

	return nil, fmt.Errorf("no %s proxy available", protocol)
}

// GetBatchForHealthCheck 获取一批需要健康检查的代理
func (s *Storage) GetBatchForHealthCheck(batchSize int, skipSGrade bool) ([]Proxy, error) {
	query := `SELECT ` + proxyColumns + `
		 FROM proxies
		 WHERE status IN ('active', 'degraded') AND fail_count < 3`

	if skipSGrade {
		query += ` AND quality_grade != 'S'`
	}

	query += ` ORDER BY 
		COALESCE(last_check, '1970-01-01') ASC,
		quality_grade DESC
		LIMIT ?`

	rows, err := s.db.Query(query, batchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var proxies []Proxy
	for rows.Next() {
		p, err := scanProxy(rows)
		if err != nil {
			return nil, err
		}
		proxies = append(proxies, *p)
	}
	return proxies, nil
}

// GetByProtocol 按协议获取代理列表
func (s *Storage) GetByProtocol(protocol string) ([]Proxy, error) {
	rows, err := s.db.Query(
		`SELECT `+proxyColumns+`
		 FROM proxies
		 WHERE status IN ('active', 'degraded') AND fail_count < 3 AND protocol = ?
		 ORDER BY latency ASC`, protocol,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var proxies []Proxy
	for rows.Next() {
		p, err := scanProxy(rows)
		if err != nil {
			return nil, err
		}
		proxies = append(proxies, *p)
	}
	return proxies, nil
}

// GetDisabledCustomProxies 获取所有被禁用的订阅代理
func (s *Storage) GetDisabledCustomProxies() ([]Proxy, error) {
	rows, err := s.db.Query(
		`SELECT ` + proxyColumns + `
		 FROM proxies
		 WHERE source = 'subscription' AND status = 'disabled'`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var proxies []Proxy
	for rows.Next() {
		p, err := scanProxy(rows)
		if err != nil {
			return nil, err
		}
		proxies = append(proxies, *p)
	}
	return proxies, nil
}
