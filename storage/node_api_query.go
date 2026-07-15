package storage

import (
	"database/sql"
	"encoding/json"
	"strings"
)

// NodeAPIFilter 只读节点 API 的存储层过滤条件（参数组合为 AND）。
// Status 空=仅可用（active/degraded && !user_paused && fail_count<3 && 父订阅未暂停）；"all"=全量。
// MaxAbuse 非 nil 时按 ipapiis_score 上限过滤；-1（未探测）不通过。
// CF: "open"=cf_blocked==0，"blocked"=cf_blocked==1，空=不过滤。
// AI: 需全部可达（JSON 中对应键值为 0）的服务名列表；SQLite 先做其他过滤，Go 分批解析 JSON。
// Limit 默认 500、上限 2000；Offset 默认 0。
type NodeAPIFilter struct {
	Region   string
	Protocol string
	Source   string
	Status   string
	MaxAbuse *float64
	CF       string
	AI       []string
	Limit    int
	Offset   int
}

const (
	nodeAPIDefaultLimit = 500
	nodeAPIMaxLimit     = 2000
)

// ListNodesForAPI 按过滤条件列出节点并返回稳定分页结果（latency ASC, id ASC）。
// total 为过滤后的总数（分页前）。
func (s *Storage) ListNodesForAPI(filter NodeAPIFilter) ([]Proxy, int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()

	nodes, total, err := listNodesForAPI(tx, filter)
	if err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	return nodes, total, nil
}

type nodeAPIQueryer interface {
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
}

func listNodesForAPI(queryer nodeAPIQueryer, filter NodeAPIFilter) ([]Proxy, int, error) {
	limit, offset := normalizeNodeAPIPaging(filter.Limit, filter.Offset)
	wantAI := normalizeAIList(filter.AI)
	where, args := buildNodeAPIWhere(filter)

	if len(wantAI) > 0 {
		return listNodesForAPIWithAI(queryer, where, args, wantAI, limit, offset)
	}

	total, err := countNodesForAPI(queryer, where, args)
	if err != nil {
		return nil, 0, err
	}
	if offset >= total {
		return []Proxy{}, total, nil
	}

	query := `SELECT ` + proxyColumns + ` FROM proxies` + where + ` ORDER BY latency ASC, id ASC LIMIT ? OFFSET ?`
	queryArgs := append(append([]interface{}{}, args...), limit, offset)
	rows, err := queryer.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	return scanNodeAPIPage(rows, total)
}

func normalizeNodeAPIPaging(rawLimit, rawOffset int) (int, int) {
	limit := rawLimit
	if limit <= 0 {
		limit = nodeAPIDefaultLimit
	}
	if limit > nodeAPIMaxLimit {
		limit = nodeAPIMaxLimit
	}
	offset := rawOffset
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func buildNodeAPIWhere(filter NodeAPIFilter) (string, []interface{}) {
	where := ` WHERE 1=1`
	var args []interface{}

	status := strings.TrimSpace(strings.ToLower(filter.Status))
	if status != "all" {
		where += ` AND status IN ('active', 'degraded') AND user_paused = 0 AND fail_count < 3
			AND NOT EXISTS (SELECT 1 FROM subscriptions WHERE subscriptions.id = proxies.subscription_id AND subscriptions.status = 'paused')`
	}

	if region := strings.TrimSpace(strings.ToLower(filter.Region)); region != "" {
		where += ` AND region = ?`
		args = append(args, region)
	}
	if protocol := strings.TrimSpace(strings.ToLower(filter.Protocol)); protocol != "" {
		where += ` AND protocol = ?`
		args = append(args, protocol)
	}
	if source := strings.TrimSpace(strings.ToLower(filter.Source)); source != "" {
		where += ` AND source = ?`
		args = append(args, source)
	}

	switch strings.TrimSpace(strings.ToLower(filter.CF)) {
	case "open":
		where += ` AND cf_blocked = 0`
	case "blocked":
		where += ` AND cf_blocked = 1`
	}

	if filter.MaxAbuse != nil {
		// 未探测 (-1) 不通过 max_abuse：要求已探测且分数 <= 上限
		where += ` AND ipapiis_score >= 0 AND ipapiis_score <= ?`
		args = append(args, *filter.MaxAbuse)
	}

	return where, args
}

func countNodesForAPI(queryer nodeAPIQueryer, where string, args []interface{}) (int, error) {
	var total int
	if err := queryer.QueryRow(`SELECT COUNT(*) FROM proxies`+where, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func listNodesForAPIWithAI(queryer nodeAPIQueryer, where string, args []interface{}, wantAI []string, limit, offset int) ([]Proxy, int, error) {
	total := 0
	skipped := 0
	page := make([]Proxy, 0, limit)
	hasCursor := false
	lastLatency := 0
	var lastID int64

	for {
		query := `SELECT ` + proxyColumns + ` FROM proxies` + where
		queryArgs := append([]interface{}{}, args...)
		if hasCursor {
			query += ` AND (latency > ? OR (latency = ? AND id > ?))`
			queryArgs = append(queryArgs, lastLatency, lastLatency, lastID)
		}
		query += ` ORDER BY latency ASC, id ASC LIMIT ?`
		queryArgs = append(queryArgs, nodeAPIMaxLimit)
		rows, err := queryer.Query(query, queryArgs...)
		if err != nil {
			return nil, 0, err
		}

		batchCount := 0
		for rows.Next() {
			batchCount++
			p, err := scanProxy(rows)
			if err != nil {
				rows.Close()
				return nil, 0, err
			}
			lastLatency, lastID, hasCursor = p.Latency, p.ID, true
			if !aiReachableAll(p.AIReachability, wantAI) {
				continue
			}
			total++
			if skipped < offset {
				skipped++
				continue
			}
			if len(page) < limit {
				page = append(page, *p)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, 0, err
		}
		if err := rows.Close(); err != nil {
			return nil, 0, err
		}
		if batchCount < nodeAPIMaxLimit {
			break
		}
	}

	if len(page) == 0 {
		return []Proxy{}, total, nil
	}
	return page, total, nil
}

func scanNodeAPIPage(rows *sql.Rows, total int) ([]Proxy, int, error) {
	nodes := make([]Proxy, 0)
	for rows.Next() {
		p, err := scanProxy(rows)
		if err != nil {
			return nil, 0, err
		}
		nodes = append(nodes, *p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return nodes, total, nil
}

func normalizeAIList(ai []string) []string {
	if len(ai) == 0 {
		return nil
	}
	out := make([]string, 0, len(ai))
	seen := make(map[string]struct{}, len(ai))
	for _, raw := range ai {
		name := strings.TrimSpace(strings.ToLower(raw))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// aiReachableAll 要求 JSON 中每个 name 的值均为 0（可达）。
// 空串、坏 JSON、缺字段、未探测(-1)、不可达(1) 均失败。
func aiReachableAll(raw string, names []string) bool {
	if len(names) == 0 {
		return true
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	var m map[string]int
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return false
	}
	for _, name := range names {
		v, ok := m[name]
		if !ok || v != 0 {
			return false
		}
	}
	return true
}
