package storage

import (
	"database/sql"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
)

var alpha2RegionPattern = regexp.MustCompile(`^[A-Za-z]{2}$`)

type RegionCount struct {
	Region string `json:"region"`
	Count  int    `json:"count"`
}

func (s *Storage) GetByRegion(region string, excludes []int64) ([]Proxy, error) {
	query := `SELECT ` + proxyColumns + `
		 FROM proxies
		 WHERE status IN ('active', 'degraded') AND user_paused = 0 AND fail_count < 3
		   AND ` + selectableSubscriptionScopeSQL
	args := []interface{}{}
	if normalized := normalizeRegion(region); normalized != "" {
		query += ` AND region = ?`
		args = append(args, normalized)
	}
	excludeMap := makeExcludeMap(excludes)
	query += ` ORDER BY CASE WHEN latency <= 0 THEN 1 ELSE 0 END, latency ASC, address ASC, id ASC`
	return s.queryProxies(query, args, excludeMap)
}

func (s *Storage) GetRandomByRegion(region string, excludes []int64) (*Proxy, error) {
	proxies, err := s.GetByRegion(region, excludes)
	if err != nil {
		return nil, err
	}
	if len(proxies) == 0 {
		return nil, fmt.Errorf("no available proxy for region: %s", normalizeRegion(region))
	}
	proxy := proxies[rand.Intn(len(proxies))]
	return &proxy, nil
}

func (s *Storage) CountByRegion() (map[string]int, error) {
	rows, err := s.db.Query(`
		SELECT region, COUNT(*)
		FROM proxies
		WHERE status IN ('active', 'degraded') AND user_paused = 0 AND fail_count < 3 AND region != ''
		  AND ` + selectableSubscriptionScopeSQL + `
		GROUP BY region`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var region string
		var count int
		if err := rows.Scan(&region, &count); err != nil {
			return nil, err
		}
		counts[region] = count
	}
	return counts, rows.Err()
}

func (s *Storage) GetRegionsWithCount() ([]RegionCount, error) {
	rows, err := s.db.Query(`
		SELECT region, COUNT(*)
		FROM proxies
		WHERE status IN ('active', 'degraded') AND user_paused = 0 AND fail_count < 3 AND region != ''
		  AND ` + selectableSubscriptionScopeSQL + `
		GROUP BY region
		ORDER BY region ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	regions := []RegionCount{}
	for rows.Next() {
		var item RegionCount
		if err := rows.Scan(&item.Region, &item.Count); err != nil {
			return nil, err
		}
		regions = append(regions, item)
	}
	return regions, rows.Err()
}

func (s *Storage) queryProxies(query string, args []interface{}, excludes map[int64]bool) ([]Proxy, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	proxies := []Proxy{}
	for rows.Next() {
		p, err := scanProxy(rows)
		if err != nil {
			return nil, err
		}
		if !excludes[p.ID] {
			proxies = append(proxies, *p)
		}
	}
	return proxies, rows.Err()
}

func makeExcludeMap(excludes []int64) map[int64]bool {
	excludeMap := make(map[int64]bool, len(excludes))
	for _, id := range excludes {
		excludeMap[id] = true
	}
	return excludeMap
}

func (s *Storage) GetProxyByAddress(address string) (*Proxy, error) {
	if err := s.requireUnambiguousAddress(address); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("proxy %s not found", address)
		}
		return nil, err
	}
	row := s.db.QueryRow(`SELECT `+proxyColumns+` FROM proxies WHERE address = ?`, address)
	proxy, err := scanProxy(row)
	if err == nil {
		return proxy, nil
	}
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("proxy %s not found", address)
	}
	return nil, err
}

func (s *Storage) GetProxyByIdentity(address, source string, subscriptionID int64) (*Proxy, error) {
	if source == SourceManual {
		subscriptionID = 0
	}
	row := s.db.QueryRow(
		`SELECT `+proxyColumns+` FROM proxies WHERE address = ? AND source = ? AND subscription_id = ?`,
		address, source, subscriptionID,
	)
	proxy, err := scanProxy(row)
	if err == nil {
		return proxy, nil
	}
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("proxy %s/%s/%d not found", address, source, subscriptionID)
	}
	return nil, err
}

func (s *Storage) GetProxyByID(id int64) (*Proxy, error) {
	row := s.db.QueryRow(`SELECT `+proxyColumns+` FROM proxies WHERE id = ?`, id)
	proxy, err := scanProxy(row)
	if err == nil {
		return proxy, nil
	}
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("proxy id %d not found", id)
	}
	return nil, err
}

// GetProxyByNodeKey 按稳定节点身份键查询。node_key 为空返回 not found。
// 多行同 key：显式歧义失败（与 GetProxyByAddress 一致，禁止静默替身）。
func (s *Storage) GetProxyByNodeKey(nodeKey string) (*Proxy, error) {
	nodeKey = strings.TrimSpace(nodeKey)
	if nodeKey == "" {
		return nil, fmt.Errorf("proxy node_key empty")
	}
	rows, err := s.db.Query(
		`SELECT `+proxyColumns+` FROM proxies WHERE node_key = ? ORDER BY id ASC LIMIT 2`,
		nodeKey,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	matches := make([]*Proxy, 0, 2)
	for rows.Next() {
		proxy, err := scanProxy(rows)
		if err != nil {
			return nil, err
		}
		matches = append(matches, proxy)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("proxy node_key %s not found", nodeKey)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("proxy node_key %s is ambiguous (at least 2 rows)", nodeKey)
	}
	return matches[0], nil
}

// HasOtherSubscriptionProxyOwner 判断某订阅移除后，运行态 key/address 是否仍有其它 owner。
// 允许跨订阅共享 NodeKey；空 key 的旧数据按相同 address 保守保留运行态。
func (s *Storage) HasOtherSubscriptionProxyOwner(nodeKey, address string, subscriptionID int64) (bool, error) {
	nodeKey = strings.TrimSpace(nodeKey)
	address = strings.TrimSpace(address)
	if nodeKey == "" && address == "" {
		return false, nil
	}
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM proxies
		 WHERE NOT (source = ? AND subscription_id = ?)
		   AND ((? != '' AND node_key = ?)
		        OR (? != '' AND node_key = '' AND address = ?))`,
		SourceSubscription, subscriptionID, nodeKey, nodeKey, address, address,
	).Scan(&count)
	return count > 0, err
}

// HasOtherProxyOwner 判断删除单行后，运行态 key/address 是否仍有其它 owner。
// 查询失败必须由调用方上抛，禁止在无法确认所有权时回收运行态。
func (s *Storage) HasOtherProxyOwner(nodeKey, address string, proxyID int64) (bool, error) {
	nodeKey = strings.TrimSpace(nodeKey)
	address = strings.TrimSpace(address)
	if nodeKey == "" && address == "" {
		return false, nil
	}
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM proxies
		 WHERE id != ?
		   AND ((? != '' AND node_key = ?)
		        OR (? != '' AND node_key = '' AND address = ?))`,
		proxyID, nodeKey, nodeKey, address, address,
	).Scan(&count)
	return count > 0, err
}

// IsSubscriptionPaused 报告父订阅是否暂停。id<=0 表示手工节点，无父订阅。
func (s *Storage) IsSubscriptionPaused(id int64) (bool, error) {
	if id <= 0 {
		return false, nil
	}
	var status string
	err := s.db.QueryRow(`SELECT status FROM subscriptions WHERE id = ?`, id).Scan(&status)
	if err == sql.ErrNoRows {
		return false, fmt.Errorf("subscription %d not found", id)
	}
	if err != nil {
		return false, err
	}
	return status == "paused", nil
}

func normalizeProtocol(protocol string) string {
	return strings.ToLower(strings.TrimSpace(protocol))
}

// normalizeManualRegion 规范化用户手动输入的 region：小写去空白后，必须匹配
// alpha2（两位字母），否则视为「未知地域/自动」返回 ""（与既有 region="" 语义一致）。
// 用于手动节点写入入口（AddManualProxy / UpdateProxyRegion / UpdateProxyRegionByID），
// 作为前端转义之外的后端兜底，防止恶意客户端绕过前端直接写入非法 region（如 <script>）。
//
// 不复用/不改动 normalizeRegion：后者被 GetByRegion 查询过滤及
// DisableBlockedCountries / DisableNotAllowedCountries 复用，那些路径依赖保留
// 非 alpha2 原值——若在 normalizeRegion 里把非法值改成空串，disable 路径的
// region = ” 会误匹配所有 region 为空（auto 地域未知）的节点，造成大范围误禁。
func normalizeManualRegion(region string) string {
	normalized := normalizeRegion(region)
	if !alpha2RegionPattern.MatchString(normalized) {
		return ""
	}
	return normalized
}

func regionFromExitLocation(exitLocation string) string {
	fields := strings.Fields(exitLocation)
	if len(fields) == 0 || !alpha2RegionPattern.MatchString(fields[0]) {
		return ""
	}
	return strings.ToLower(fields[0])
}
