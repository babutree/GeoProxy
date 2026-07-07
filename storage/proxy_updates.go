package storage

import "strings"

// Delete 立即删除指定代理
func (s *Storage) Delete(address string) error {
	_, err := s.db.Exec(`DELETE FROM proxies WHERE address = ?`, address)
	return err
}

// IncrFail 增加失败次数
func (s *Storage) IncrFail(address string) error {
	_, err := s.db.Exec(
		`UPDATE proxies SET fail_count = fail_count + 1, last_check = CURRENT_TIMESTAMP WHERE address = ?`,
		address,
	)
	return err
}

// ResetFail 重置失败次数（验证通过）
func (s *Storage) ResetFail(address string) error {
	_, err := s.db.Exec(
		`UPDATE proxies SET fail_count = 0, last_check = CURRENT_TIMESTAMP WHERE address = ?`,
		address,
	)
	return err
}

// UpdateLatency 更新代理的延迟信息（毫秒）
func (s *Storage) UpdateLatency(address string, latencyMs int) error {
	_, err := s.db.Exec(
		`UPDATE proxies SET latency = ? WHERE address = ?`,
		latencyMs, address,
	)
	return err
}

// UpdateExitInfo 更新出口信息；自动地域可由验证结果回写，手动地域受保护。
func (s *Storage) UpdateExitInfo(address, exitIP, exitLocation string, latencyMs int) error {
	grade := CalculateQualityGrade(latencyMs)
	region := regionFromExitLocation(exitLocation)
	_, err := s.db.Exec(
		`UPDATE proxies
		 SET exit_ip = ?, exit_location = ?, latency = ?, quality_grade = ?,
		     region = CASE WHEN region_source != 'manual' AND ? != '' THEN ? ELSE region END
		 WHERE address = ?`,
		exitIP, exitLocation, latencyMs, grade, region, region, address,
	)
	return err
}

// RecordProxyUse 记录代理使用（成功）
func (s *Storage) RecordProxyUse(address string, success bool) error {
	if success {
		_, err := s.db.Exec(
			`UPDATE proxies SET use_count = use_count + 1, success_count = success_count + 1, 
			 last_used = CURRENT_TIMESTAMP WHERE address = ?`,
			address,
		)
		return err
	}
	_, err := s.db.Exec(
		`UPDATE proxies SET use_count = use_count + 1, fail_count = fail_count + 1, 
		 last_used = CURRENT_TIMESTAMP WHERE address = ?`,
		address,
	)
	return err
}

// CalculateQualityGrade 根据延迟计算质量等级
func CalculateQualityGrade(latencyMs int) string {
	switch {
	case latencyMs <= 500:
		return "S" // 超快
	case latencyMs <= 1000:
		return "A" // 良好
	case latencyMs <= 2000:
		return "B" // 可用
	default:
		return "C" // 淘汰候选
	}
}

// DisableBlockedCountries 禁用属于被屏蔽国家的节点（不删除）
func (s *Storage) DisableBlockedCountries(countryCodes []string) (int64, error) {
	if len(countryCodes) == 0 {
		return 0, nil
	}
	var total int64
	for _, code := range countryCodes {
		res, err := s.db.Exec(
			`UPDATE proxies SET status = 'disabled' WHERE status = 'active' AND (region = ? OR exit_location = ? OR exit_location LIKE ?)`,
			normalizeRegion(code), strings.ToUpper(code), strings.ToUpper(code)+" %",
		)
		if err != nil {
			return total, err
		}
		affected, _ := res.RowsAffected()
		total += affected
	}
	return total, nil
}

// DisableNotAllowedCountries 禁用不在白名单的节点（不删除）
func (s *Storage) DisableNotAllowedCountries(allowedCodes []string) (int64, error) {
	if len(allowedCodes) == 0 {
		return 0, nil
	}
	conditions := make([]string, 0, len(allowedCodes)*3)
	args := make([]interface{}, 0, len(allowedCodes)*3)
	for _, code := range allowedCodes {
		upper := strings.ToUpper(code)
		conditions = append(conditions, "region = ?", "exit_location = ?", "exit_location LIKE ?")
		args = append(args, normalizeRegion(code), upper, upper+" %")
	}
	query := `UPDATE proxies SET status = 'disabled' WHERE status = 'active' AND (region != '' OR exit_location != '') AND NOT (` + strings.Join(conditions, " OR ") + `)`
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// IncrementFailCount 增加失败次数
func (s *Storage) IncrementFailCount(address string) error {
	_, err := s.db.Exec(
		`UPDATE proxies SET fail_count = fail_count + 1 WHERE address = ?`,
		address,
	)
	return err
}

// DeleteBySubscriptionID 删除指定订阅的所有代理
func (s *Storage) DeleteBySubscriptionID(subscriptionID int64) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM proxies WHERE subscription_id = ?`, subscriptionID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DisableProxy 禁用代理（软删除，用于订阅代理）
func (s *Storage) DisableProxy(address string) error {
	_, err := s.db.Exec(
		`UPDATE proxies SET status = 'disabled' WHERE address = ?`,
		address,
	)
	return err
}

// EnableProxy 启用代理（从禁用状态恢复）
func (s *Storage) EnableProxy(address string) error {
	_, err := s.db.Exec(
		`UPDATE proxies SET status = 'active', fail_count = 0 WHERE address = ?`,
		address,
	)
	return err
}

// DeleteBySource 删除指定来源的所有代理
func (s *Storage) DeleteBySource(source string) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM proxies WHERE source = ?`, source)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteCustomProxiesNotIn 删除不在给定地址列表中的订阅代理
func (s *Storage) DeleteCustomProxiesNotIn(addresses []string) (int64, error) {
	if len(addresses) == 0 {
		return s.DeleteBySource(SourceSubscription)
	}
	placeholders := make([]string, len(addresses))
	args := make([]interface{}, len(addresses))
	for i, addr := range addresses {
		placeholders[i] = "?"
		args[i] = addr
	}
	query := `DELETE FROM proxies WHERE source = 'subscription' AND address NOT IN (` + strings.Join(placeholders, ",") + `)`
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
