package storage

import (
	"database/sql"
	"fmt"
	"strings"
)

// Delete 立即删除指定代理
func (s *Storage) Delete(address string) error {
	if err := s.requireUnambiguousAddress(address); err != nil {
		return err
	}
	res, err := s.db.Exec(`DELETE FROM proxies WHERE address = ?`, address)
	if err != nil {
		return err
	}
	return requireRowsAffected(res.RowsAffected())
}

func (s *Storage) DeleteProxyByID(id int64) error {
	res, err := s.db.Exec(`DELETE FROM proxies WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return requireRowsAffected(res.RowsAffected())
}

// IncrFail 增加失败次数
func (s *Storage) IncrFail(address string) error {
	if err := s.requireUnambiguousAddress(address); err != nil {
		return err
	}
	res, err := s.db.Exec(
		`UPDATE proxies SET fail_count = fail_count + 1, last_check = CURRENT_TIMESTAMP WHERE address = ?`,
		address,
	)
	if err != nil {
		return err
	}
	return requireRowsAffected(res.RowsAffected())
}

// ResetFail 重置失败次数（验证通过）
func (s *Storage) ResetFail(address string) error {
	if err := s.requireUnambiguousAddress(address); err != nil {
		return err
	}
	res, err := s.db.Exec(
		`UPDATE proxies SET fail_count = 0, last_check = CURRENT_TIMESTAMP WHERE address = ?`,
		address,
	)
	if err != nil {
		return err
	}
	return requireRowsAffected(res.RowsAffected())
}

// UpdateLatency 更新代理的延迟信息（毫秒）
func (s *Storage) UpdateLatency(address string, latencyMs int) error {
	if err := s.requireUnambiguousAddress(address); err != nil {
		return err
	}
	res, err := s.db.Exec(
		`UPDATE proxies SET latency = ? WHERE address = ?`,
		latencyMs, address,
	)
	if err != nil {
		return err
	}
	return requireRowsAffected(res.RowsAffected())
}

func (s *Storage) UpdateLatencyByID(id int64, latencyMs int) error {
	res, err := s.db.Exec(`UPDATE proxies SET latency = ? WHERE id = ?`, latencyMs, id)
	if err != nil {
		return err
	}
	return requireRowsAffected(res.RowsAffected())
}

// UpdateExitInfo 更新出口信息；自动地域可由验证结果回写，手动地域受保护。
func (s *Storage) UpdateExitInfo(address, exitIP, exitLocation string, latencyMs int, ipapiisScore float64, ipapiFlags string, cfBlocked int) error {
	if err := s.requireUnambiguousAddress(address); err != nil {
		return err
	}
	return s.updateExitInfoWhere(`address = ?`, []interface{}{address}, exitIP, exitLocation, latencyMs, ipapiisScore, ipapiFlags, cfBlocked)
}

func (s *Storage) UpdateProxyExitInfo(id int64, exitIP, exitLocation string, latencyMs int, ipapiisScore float64, ipapiFlags string, cfBlocked int) error {
	return s.updateExitInfoWhere(`id = ?`, []interface{}{id}, exitIP, exitLocation, latencyMs, ipapiisScore, ipapiFlags, cfBlocked)
}

func (s *Storage) UpdateSubscriptionProxyExitInfo(address string, subscriptionID int64, exitIP, exitLocation string, latencyMs int, ipapiisScore float64, ipapiFlags string, cfBlocked int) error {
	return s.updateExitInfoWhere(`address = ? AND source = ? AND subscription_id = ?`, []interface{}{address, SourceSubscription, subscriptionID}, exitIP, exitLocation, latencyMs, ipapiisScore, ipapiFlags, cfBlocked)
}

// updateExitInfoWhere 写回出口信息与两源风险信号。
// ipapiis_score 仅在 ipapiisScore >= 0 时更新：探测降级/未知(-1)不得覆盖已有有效分。
// ipapi_flags 随每次成功探测覆盖写入（含空串——空表示本次探测无命中，语义有效）。
// ipapi_flags_seen=1 区分“已探测且无命中”和“旧数据/未探测”。
// 注意：本函数不改 status——订阅流程依赖 Disable/Enable 分离，恢复启用由调用点显式处理。
func (s *Storage) updateExitInfoWhere(where string, args []interface{}, exitIP, exitLocation string, latencyMs int, ipapiisScore float64, ipapiFlags string, cfBlocked int) error {
	grade := CalculateQualityGrade(latencyMs)
	region := regionFromExitLocation(exitLocation)
	queryArgs := []interface{}{exitIP, exitLocation, latencyMs, grade, region, region, ipapiisScore, ipapiisScore, ipapiFlags, cfBlocked, cfBlocked}
	queryArgs = append(queryArgs, args...)
	// 健康检查/验证成功时同样清零 fail_count（BUG-53）：只有到达此处才代表
	// 探测通过，之前累积的失败应清除，节点方能重新参与选路/后续检查。
	// 健康检查失败路径仍会累加 fail_count 至阈值并 disable，故持续坏的节点
	// 不会来回横跳——只有真正探测成功才归零。
	// cf_blocked 仅在 cfBlocked >= 0 时更新：-1 代表本次未能探测(未知)，不得覆盖已有有效值(0/1)。
	res, err := s.db.Exec(
		`UPDATE proxies
		 SET exit_ip = ?, exit_location = ?, latency = ?, quality_grade = ?, fail_count = 0,
		     region = CASE WHEN region_source != 'manual' AND ? != '' THEN ? ELSE region END,
		     ipapiis_score = CASE WHEN ? >= 0 THEN ? ELSE ipapiis_score END,
		     ipapi_flags = ?,
		     ipapi_flags_seen = 1,
		     cf_blocked = CASE WHEN ? >= 0 THEN ? ELSE cf_blocked END
		 WHERE `+where,
		queryArgs...,
	)
	if err != nil {
		return err
	}
	return requireRowsAffected(res.RowsAffected())
}

// SetProxyStarred 置位/清位节点星标。starred 转 0/1 写入 starred 列。
func (s *Storage) SetProxyStarred(id int64, starred bool) error {
	starredInt := 0
	if starred {
		starredInt = 1
	}
	res, err := s.db.Exec(`UPDATE proxies SET starred = ? WHERE id = ?`, starredInt, id)
	if err != nil {
		return err
	}
	return requireRowsAffected(res.RowsAffected())
}

// RecordProxyUse 记录代理使用（成功）
func (s *Storage) RecordProxyUse(address string, success bool) error {
	if err := s.requireUnambiguousAddress(address); err != nil {
		return err
	}
	proxy, err := s.GetProxyByAddress(address)
	if err != nil {
		return err
	}
	return s.RecordProxyUseByID(proxy.ID, success)

}

func (s *Storage) RecordProxyUseByID(id int64, success bool) error {
	if success {
		// 成功即清零 fail_count：一次成功证明节点当前可用，
		// 否则请求失败累积的 fail_count 永不归零，节点会被选路/健康检查
		// 的 fail_count < 3 过滤永久排除（僵尸节点）。见 BUG-53。
		res, err := s.db.Exec(
			`UPDATE proxies SET use_count = use_count + 1, success_count = success_count + 1, 
			 fail_count = 0, last_used = CURRENT_TIMESTAMP WHERE id = ?`,
			id,
		)
		if err != nil {
			return err
		}
		return requireRowsAffected(res.RowsAffected())
	}
	res, err := s.db.Exec(
		`UPDATE proxies SET use_count = use_count + 1, fail_count = fail_count + 1, 
		 last_used = CURRENT_TIMESTAMP WHERE id = ?`,
		id,
	)
	if err != nil {
		return err
	}
	return requireRowsAffected(res.RowsAffected())
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
	if err := s.requireUnambiguousAddress(address); err != nil {
		return err
	}
	res, err := s.db.Exec(
		`UPDATE proxies SET fail_count = fail_count + 1 WHERE address = ?`,
		address,
	)
	if err != nil {
		return err
	}
	return requireRowsAffected(res.RowsAffected())
}

// DeleteBySubscriptionID 删除指定订阅的所有代理
func (s *Storage) DeleteBySubscriptionID(subscriptionID int64) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM proxies WHERE subscription_id = ? AND source = ?`, subscriptionID, SourceSubscription)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DisableProxy 禁用代理（软删除，用于订阅代理）
func (s *Storage) DisableProxy(address string) error {
	if err := s.requireUnambiguousAddress(address); err != nil {
		return err
	}
	return s.disableProxyWhere(`address = ?`, address)
}

func (s *Storage) DisableProxyByID(id int64) error {
	return s.disableProxyWhere(`id = ?`, id)
}

func (s *Storage) DisableSubscriptionProxy(address string, subscriptionID int64) error {
	return s.disableProxyWhere(`address = ? AND source = ? AND subscription_id = ?`, address, SourceSubscription, subscriptionID)
}

func (s *Storage) disableProxyWhere(where string, args ...interface{}) error {
	res, err := s.db.Exec(
		`UPDATE proxies SET status = 'disabled' WHERE `+where,
		args...,
	)
	if err != nil {
		return err
	}
	return requireRowsAffected(res.RowsAffected())
}

// EnableProxy 启用代理（从禁用状态恢复）
func (s *Storage) EnableProxy(address string) error {
	if err := s.requireUnambiguousAddress(address); err != nil {
		return err
	}
	return s.enableProxyWhere(`address = ?`, address)
}

func (s *Storage) EnableProxyByID(id int64) error {
	return s.enableProxyWhere(`id = ?`, id)
}

func (s *Storage) EnableSubscriptionProxy(address string, subscriptionID int64) error {
	return s.enableProxyWhere(`address = ? AND source = ? AND subscription_id = ?`, address, SourceSubscription, subscriptionID)
}

func (s *Storage) enableProxyWhere(where string, args ...interface{}) error {
	res, err := s.db.Exec(
		`UPDATE proxies SET status = 'active', fail_count = 0
		 WHERE `+where+` AND status = 'disabled'
		   AND NOT EXISTS (SELECT 1 FROM subscriptions WHERE subscriptions.id = proxies.subscription_id AND subscriptions.status = 'paused')`,
		args...,
	)
	if err != nil {
		return err
	}
	return requireRowsAffected(res.RowsAffected())
}

// PauseProxy 用户手动停用节点，状态置为 'paused' 以区别于验证失败的 'disabled'。
// paused 表示“用户主动不用”，disabled 表示“系统判定不可用”。两者都不参与选路。
func (s *Storage) PauseProxy(address string) error {
	if err := s.requireUnambiguousAddress(address); err != nil {
		return err
	}
	res, err := s.db.Exec(
		`UPDATE proxies SET user_paused = 1 WHERE address = ?`,
		address,
	)
	if err != nil {
		return err
	}
	return requireRowsAffected(res.RowsAffected())
}

func (s *Storage) PauseProxyByID(id int64) error {
	res, err := s.db.Exec(`UPDATE proxies SET user_paused = 1 WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return requireRowsAffected(res.RowsAffected())
}

// UnpauseProxy 恢复用户手动停用的节点；父订阅暂停时不恢复为可选路节点。
func (s *Storage) UnpauseProxy(address string) error {
	if err := s.requireUnambiguousAddress(address); err != nil {
		return err
	}
	res, err := s.db.Exec(
		`UPDATE proxies SET user_paused = 0, fail_count = 0 WHERE address = ?`,
		address,
	)
	if err != nil {
		return err
	}
	return requireRowsAffected(res.RowsAffected())
}

func (s *Storage) UnpauseProxyByID(id int64) error {
	res, err := s.db.Exec(`UPDATE proxies SET user_paused = 0, fail_count = 0 WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return requireRowsAffected(res.RowsAffected())
}

func (s *Storage) requireUnambiguousAddress(address string) error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM proxies WHERE address = ?`, address).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	if count > 1 {
		return fmt.Errorf("proxy address %q is ambiguous", address)
	}
	return nil
}

// DeleteBySource 删除指定来源的所有代理
func (s *Storage) DeleteBySource(source string) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM proxies WHERE source = ?`, source)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
