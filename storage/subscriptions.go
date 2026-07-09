package storage

import (
	"database/sql"
	"fmt"
)

// AddSubscription 添加订阅（自动去重：相同 URL 或 file_path 不重复添加）
func (s *Storage) AddSubscription(name, url, filePath, format string, refreshMin int) (int64, error) {
	// 去重检查
	if url != "" {
		var existID int64
		err := s.db.QueryRow(`SELECT id FROM subscriptions WHERE url = ? AND url != ''`, url).Scan(&existID)
		if err == nil {
			return 0, fmt.Errorf("该订阅 URL 已存在")
		}
	}
	if filePath != "" {
		var existID int64
		err := s.db.QueryRow(`SELECT id FROM subscriptions WHERE file_path = ? AND file_path != ''`, filePath).Scan(&existID)
		if err == nil {
			return 0, fmt.Errorf("该订阅文件已存在")
		}
	}

	res, err := s.db.Exec(
		`INSERT INTO subscriptions (name, url, file_path, format, refresh_min) VALUES (?, ?, ?, ?, ?)`,
		name, url, filePath, format, refreshMin,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// CountBySubscriptionID 统计指定订阅的可用/禁用代理数
func (s *Storage) CountBySubscriptionID(subID int64) (active int, disabled int) {
	s.db.QueryRow(
		`SELECT COUNT(*) FROM proxies WHERE subscription_id = ? AND status IN ('active', 'degraded') AND fail_count < 3`,
		subID,
	).Scan(&active)
	s.db.QueryRow(
		`SELECT COUNT(*) FROM proxies WHERE subscription_id = ? AND status = 'disabled'`,
		subID,
	).Scan(&disabled)
	return
}

// AddContributedSubscription 添加访客贡献的订阅
func (s *Storage) AddContributedSubscription(name, url string, refreshMin int) (int64, error) {
	if url == "" {
		return 0, fmt.Errorf("URL 不能为空")
	}
	// 去重
	var existID int64
	err := s.db.QueryRow(`SELECT id FROM subscriptions WHERE url = ? AND url != ''`, url).Scan(&existID)
	if err == nil {
		return 0, fmt.Errorf("该订阅 URL 已存在")
	}

	res, err := s.db.Exec(
		`INSERT INTO subscriptions (name, url, format, refresh_min, contributed) VALUES (?, ?, 'auto', ?, 1)`,
		name, url, refreshMin,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateSubscription 更新订阅
func (s *Storage) UpdateSubscription(id int64, name, url, filePath, format string, refreshMin int) error {
	_, err := s.db.Exec(
		`UPDATE subscriptions SET name = ?, url = ?, file_path = ?, format = ?, refresh_min = ? WHERE id = ?`,
		name, url, filePath, format, refreshMin, id,
	)
	return err
}

// DeleteSubscription 删除订阅
func (s *Storage) DeleteSubscription(id int64) error {
	_, err := s.db.Exec(`DELETE FROM subscriptions WHERE id = ?`, id)
	return err
}

// GetSubscriptions 获取所有订阅
func (s *Storage) GetSubscriptions() ([]Subscription, error) {
	rows, err := s.db.Query(
		`SELECT ` + subColumns + `
		 FROM subscriptions ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []Subscription
	for rows.Next() {
		sub, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		subs = append(subs, *sub)
	}
	return subs, nil
}

// GetSubscription 获取单个订阅
func (s *Storage) GetSubscription(id int64) (*Subscription, error) {
	rows, err := s.db.Query(
		`SELECT `+subColumns+`
		 FROM subscriptions WHERE id = ?`, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if rows.Next() {
		return scanSubscription(rows)
	}
	return nil, fmt.Errorf("subscription %d not found", id)
}

// UpdateSubscriptionFetch 更新订阅的最后拉取时间和代理数
func (s *Storage) UpdateSubscriptionFetch(id int64, proxyCount int) error {
	_, err := s.db.Exec(
		`UPDATE subscriptions SET last_fetch = CURRENT_TIMESTAMP, proxy_count = ? WHERE id = ?`,
		proxyCount, id,
	)
	return err
}

// UpdateSubscriptionSuccess 记录订阅最后一次有可用节点的时间
func (s *Storage) UpdateSubscriptionSuccess(id int64) error {
	_, err := s.db.Exec(
		`UPDATE subscriptions SET last_success = CURRENT_TIMESTAMP WHERE id = ?`, id,
	)
	return err
}

// GetStaleSubscriptions 获取连续 N 天无可用节点的订阅
func (s *Storage) GetStaleSubscriptions(staleDays int) ([]Subscription, error) {
	rows, err := s.db.Query(
		`SELECT `+subColumns+`
		 FROM subscriptions
		 WHERE status = 'active'
		   AND (last_success IS NULL OR JULIANDAY('now') - JULIANDAY(last_success) > ?)
		   AND JULIANDAY('now') - JULIANDAY(created_at) > ?`,
		staleDays, staleDays,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []Subscription
	for rows.Next() {
		sub, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		subs = append(subs, *sub)
	}
	return subs, nil
}

// ToggleSubscription 切换订阅状态，并联动该订阅下所有节点的启用/禁用。
// 返回切换后的订阅状态（"active" 或 "paused"）。
// 暂停订阅时禁用其节点（不参与选路），启用订阅时恢复其节点。
func (s *Storage) ToggleSubscription(id int64) (string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var current string
	if err := tx.QueryRow(`SELECT status FROM subscriptions WHERE id = ?`, id).Scan(&current); err != nil {
		return "", err
	}

	newStatus := "paused"
	if current != "active" {
		newStatus = "active"
	}

	if _, err := tx.Exec(`UPDATE subscriptions SET status = ? WHERE id = ?`, newStatus, id); err != nil {
		return "", err
	}

	// 联动节点：暂停→禁用；启用→恢复为 active 并重置失败计数。
	if newStatus == "paused" {
		if _, err := tx.Exec(`UPDATE proxies SET status = 'disabled' WHERE subscription_id = ?`, id); err != nil {
			return "", err
		}
	} else {
		if _, err := tx.Exec(
			`UPDATE proxies SET status = 'active', fail_count = 0 WHERE subscription_id = ? AND status = 'disabled'`,
			id,
		); err != nil {
			return "", err
		}
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return newStatus, nil
}

// PauseSubscription 暂停订阅但保留订阅和节点记录。
func (s *Storage) PauseSubscription(id int64) error {
	_, err := s.db.Exec(`UPDATE subscriptions SET status = 'paused' WHERE id = ?`, id)
	return err
}

// scanSubscription 扫描订阅行数据
// subColumns 订阅表查询列
const subColumns = `id, name, url, file_path, format, refresh_min, last_fetch, last_success, status, proxy_count, created_at, contributed`

func scanSubscription(rows *sql.Rows) (*Subscription, error) {
	sub := &Subscription{}
	var lastFetch, lastSuccess sql.NullTime
	var contributed int
	if err := rows.Scan(&sub.ID, &sub.Name, &sub.URL, &sub.FilePath, &sub.Format,
		&sub.RefreshMin, &lastFetch, &lastSuccess, &sub.Status, &sub.ProxyCount, &sub.CreatedAt, &contributed); err != nil {
		return nil, err
	}
	if lastFetch.Valid {
		sub.LastFetch = lastFetch.Time
	}
	if lastSuccess.Valid {
		sub.LastSuccess = lastSuccess.Time
	}
	sub.Contributed = contributed == 1
	return sub, nil
}
