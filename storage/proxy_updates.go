package storage

import (
	"database/sql"
	"errors"
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
func (s *Storage) UpdateExitInfo(address, exitIP, exitLocation string, latencyMs int, ipapiisScore float64, ipapiFlags string, ipapiFlagsKnown bool, cfBlocked int, aiReachability string) error {
	if err := s.requireUnambiguousAddress(address); err != nil {
		return err
	}
	return s.updateExitInfoWhere(`address = ?`, []interface{}{address}, exitIP, exitLocation, latencyMs, ipapiisScore, ipapiFlags, ipapiFlagsKnown, cfBlocked, aiReachability, true)
}

func (s *Storage) UpdateProxyExitInfo(id int64, exitIP, exitLocation string, latencyMs int, ipapiisScore float64, ipapiFlags string, ipapiFlagsKnown bool, cfBlocked int, aiReachability string) error {
	return s.updateExitInfoWhere(`id = ?`, []interface{}{id}, exitIP, exitLocation, latencyMs, ipapiisScore, ipapiFlags, ipapiFlagsKnown, cfBlocked, aiReachability, true)
}

func (s *Storage) UpdateSubscriptionProxyExitInfo(address string, subscriptionID int64, exitIP, exitLocation string, latencyMs int, ipapiisScore float64, ipapiFlags string, ipapiFlagsKnown bool, cfBlocked int, aiReachability string) error {
	return s.updateExitInfoWhere(`address = ? AND source = ? AND subscription_id = ?`, []interface{}{address, SourceSubscription, subscriptionID}, exitIP, exitLocation, latencyMs, ipapiisScore, ipapiFlags, ipapiFlagsKnown, cfBlocked, aiReachability, true)
}

// UpdateDisabledSubscriptionProxyExitInfo 写回仍需保持禁用的订阅节点元数据。
// last_check 是 disabled 节点的回收起点；地理过滤探测不得续期已有时钟，
// 但历史空值必须初始化，否则节点永远无法达到长期禁用回收阈值。
func (s *Storage) UpdateDisabledSubscriptionProxyExitInfo(address string, subscriptionID int64, exitIP, exitLocation string, latencyMs int, ipapiisScore float64, ipapiFlags string, ipapiFlagsKnown bool, cfBlocked int, aiReachability string) error {
	return s.updateExitInfoWhere(`address = ? AND source = ? AND subscription_id = ? AND status = 'disabled'`, []interface{}{address, SourceSubscription, subscriptionID}, exitIP, exitLocation, latencyMs, ipapiisScore, ipapiFlags, ipapiFlagsKnown, cfBlocked, aiReachability, false)
}

// RecoverSubscriptionProxyWithExitInfo 原子写回探测结果并恢复订阅节点。
// 出口元数据和 active 状态必须在同一条 UPDATE 中提交：任一字段写入失败时，
// SQLite 会回滚整条语句，节点保持 disabled 与旧元数据，避免半提交假健康。
// 仅允许从 disabled 恢复，且父订阅未暂停；调用方可在故障解除后安全重试。
func (s *Storage) RecoverSubscriptionProxyWithExitInfo(address string, subscriptionID int64, exitIP, exitLocation string, latencyMs int, ipapiisScore float64, ipapiFlags string, ipapiFlagsKnown bool, cfBlocked int, aiReachability string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开始探测恢复事务失败: %w", err)
	}
	defer tx.Rollback()

	grade := CalculateQualityGrade(latencyMs)
	region := regionFromExitLocation(exitLocation)
	res, err := tx.Exec(
		`UPDATE proxies
		 SET exit_ip = ?, exit_location = ?, latency = ?, quality_grade = ?,
		     status = 'active', fail_count = 0, last_check = CURRENT_TIMESTAMP,
		     region = CASE WHEN region_source != 'manual' AND ? != '' THEN ? ELSE region END,
		     ipapiis_score = CASE WHEN ? >= 0 THEN ? ELSE ipapiis_score END,
		     ipapi_flags = CASE WHEN ? THEN ? ELSE ipapi_flags END,
		     ipapi_flags_seen = CASE WHEN ? THEN 1 ELSE ipapi_flags_seen END,
		     cf_blocked = CASE WHEN ? >= 0 THEN ? ELSE cf_blocked END,
		     ai_reachability = CASE WHEN ? != '' THEN ? ELSE ai_reachability END
		 WHERE address = ? AND source = ? AND subscription_id = ?
		   AND status = 'disabled'
		   AND EXISTS (
			   SELECT 1 FROM subscriptions
			   WHERE subscriptions.id = proxies.subscription_id
			     AND subscriptions.status != 'paused'
		   )`,
		exitIP, exitLocation, latencyMs, grade,
		region, region,
		ipapiisScore, ipapiisScore,
		ipapiFlagsKnown, ipapiFlags,
		ipapiFlagsKnown,
		cfBlocked, cfBlocked,
		aiReachability, aiReachability,
		address, SourceSubscription, subscriptionID,
	)
	if err != nil {
		return fmt.Errorf("写回探测恢复结果失败: %w", err)
	}
	if err := requireRowsAffected(res.RowsAffected()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交探测恢复结果失败: %w", err)
	}
	return nil
}

// updateExitInfoWhere 写回出口信息与两源风险信号。
// ipapiis_score 仅在 ipapiisScore >= 0 时更新：探测降级/未知(-1)不得覆盖已有有效分。
// ipapiFlagsKnown=true 时覆盖 ipapi_flags（含空串）并置 seen=1；false 时保留旧值与 seen。
// 显式 bool 区分“主源已探测且无命中”和“仅备用源取得出口、主源未知”。
// 注意：本函数不改 status；renewLastCheck=false 保留已有时钟，仅为空值初始化回收起点。
func (s *Storage) updateExitInfoWhere(where string, args []interface{}, exitIP, exitLocation string, latencyMs int, ipapiisScore float64, ipapiFlags string, ipapiFlagsKnown bool, cfBlocked int, aiReachability string, renewLastCheck bool) error {
	grade := CalculateQualityGrade(latencyMs)
	region := regionFromExitLocation(exitLocation)
	queryArgs := []interface{}{exitIP, exitLocation, latencyMs, grade, renewLastCheck, region, region, ipapiisScore, ipapiisScore, ipapiFlagsKnown, ipapiFlags, ipapiFlagsKnown, cfBlocked, cfBlocked, aiReachability, aiReachability}
	queryArgs = append(queryArgs, args...)
	// 健康检查/验证成功时同样清零 fail_count（BUG-53）：只有到达此处才代表
	// 探测通过，之前累积的失败应清除，节点方能重新参与选路/后续检查。
	// 健康检查失败路径仍会累加 fail_count 至阈值并 disable，故持续坏的节点
	// 不会来回横跳——只有真正探测成功才归零。
	// cf_blocked 仅在 cfBlocked >= 0 时更新：-1 代表本次未能探测(未知)，不得覆盖已有有效值(0/1)。
	// ai_reachability 仅在非空串时更新：空串代表本次未探测(未知)，不得覆盖已有有效 JSON（与 cf_blocked 的 -1 不覆盖同理）。
	res, err := s.db.Exec(
		`UPDATE proxies
			 SET exit_ip = ?, exit_location = ?, latency = ?, quality_grade = ?, fail_count = 0,
			     last_check = CASE WHEN ? THEN CURRENT_TIMESTAMP ELSE COALESCE(last_check, CURRENT_TIMESTAMP) END,
		     region = CASE WHEN region_source != 'manual' AND ? != '' THEN ? ELSE region END,
		     ipapiis_score = CASE WHEN ? >= 0 THEN ? ELSE ipapiis_score END,
		     ipapi_flags = CASE WHEN ? THEN ? ELSE ipapi_flags END,
		     ipapi_flags_seen = CASE WHEN ? THEN 1 ELSE ipapi_flags_seen END,
		     cf_blocked = CASE WHEN ? >= 0 THEN ? ELSE cf_blocked END,
		     ai_reachability = CASE WHEN ? != '' THEN ? ELSE ai_reachability END
		 WHERE `+where,
		queryArgs...,
	)
	if err != nil {
		return err
	}
	return requireRowsAffected(res.RowsAffected())
}

// SetProxyDualProtocol 置位/清位节点的双协议能力标记。
// mixed 隧道节点（单端口同时服务 SOCKS5+HTTP）入库时置 true，供前端可靠区分双协议节点。
func (s *Storage) SetProxyDualProtocol(id int64, dual bool) error {
	dualInt := 0
	if dual {
		dualInt = 1
	}
	res, err := s.db.Exec(`UPDATE proxies SET dual_protocol = ? WHERE id = ?`, dualInt, id)
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

// RecordProxyFailureByID 原子累加失败次数，并在达到阈值时禁用节点。
// 计数和状态更新必须在同一条语句中完成，避免并发失败时出现
// fail_count 已达阈值但节点仍处于 active 的状态。
func (s *Storage) RecordProxyFailureByID(id int64, threshold int) error {
	_, err := s.RecordProxyFailureByIDWithStatus(id, threshold)
	return err
}

// RecordProxyFailureByIDWithStatus 原子累加失败次数，并返回写入完成后的
// 数据库权威禁用状态；调用方不得再使用写入前的 Proxy 快照推断结果。
func (s *Storage) RecordProxyFailureByIDWithStatus(id int64, threshold int) (bool, error) {
	if threshold <= 0 {
		return false, fmt.Errorf("failure threshold must be positive, got %d", threshold)
	}
	var status string
	err := s.db.QueryRow(
		`UPDATE proxies
		 SET use_count = use_count + 1,
		     fail_count = fail_count + 1,
		     status = CASE WHEN fail_count + 1 >= ? THEN 'disabled' ELSE status END,
		     last_used = CURRENT_TIMESTAMP,
		     last_check = CURRENT_TIMESTAMP
		 WHERE id = ?
		 RETURNING status`,
		threshold, id,
	).Scan(&status)
	if err != nil {
		return false, err
	}
	return status == "disabled", nil
}

// CalculateQualityGrade 根据延迟计算质量等级
// 阈值：S≤200ms A≤500ms B≤1000ms C≤2000ms；>2000ms 归入 D（视为无法连接）。
func CalculateQualityGrade(latencyMs int) string {
	switch {
	case latencyMs <= 200:
		return "S" // 超快
	case latencyMs <= 500:
		return "A" // 良好
	case latencyMs <= 1000:
		return "B" // 可用
	case latencyMs <= 2000:
		return "C" // 淘汰候选
	default:
		return "D" // 无法连接（>2000ms）
	}
}

// DisableBlockedCountries 禁用属于被屏蔽国家的节点（不删除）
func (s *Storage) DisableBlockedCountries(countryCodes []string) (int64, error) {
	if len(countryCodes) == 0 {
		return 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var total int64
	for _, code := range countryCodes {
		res, err := tx.Exec(
			`UPDATE proxies SET status = 'disabled', last_check = CURRENT_TIMESTAMP WHERE status IN ('active', 'degraded') AND (region = ? OR exit_location = ? OR exit_location LIKE ?)`,
			normalizeRegion(code), strings.ToUpper(code), strings.ToUpper(code)+" %",
		)
		if err != nil {
			return 0, err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		total += affected
	}
	if err := tx.Commit(); err != nil {
		return 0, err
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
	query := `UPDATE proxies SET status = 'disabled', last_check = CURRENT_TIMESTAMP WHERE status IN ('active', 'degraded') AND (region != '' OR exit_location != '') AND NOT (` + strings.Join(conditions, " OR ") + `)`
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
	// 禁用必写 last_check：验证/健康检查失败与地理过滤/策略路径均会写入。
	// 前端 nodeState 以 last_check 是否存在区分「已验证失败(不可用)」与
	// 「从未验证(待验证)」。漏写会让验证失败的节点永远显示为待验证。
	res, err := s.db.Exec(
		`UPDATE proxies
		 SET status = 'disabled',
		     last_check = CASE
		         WHEN status = 'disabled' AND last_check IS NOT NULL THEN last_check
		         ELSE CURRENT_TIMESTAMP
		     END
		 WHERE `+where,
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
		   AND (
			   source != ? OR EXISTS (
				   SELECT 1 FROM subscriptions
				   WHERE subscriptions.id = proxies.subscription_id
				     AND subscriptions.status != 'paused'
			   )
		   )`,
		append(args, SourceSubscription)...,
	)
	if err != nil {
		return err
	}
	return requireRowsAffected(res.RowsAffected())
}

// PauseProxy 用户手动停用节点：写 user_paused=1，不改 status 底色（active/degraded/disabled）。
// user_paused 表示“用户主动不用”，status=disabled 表示“系统判定不可用”。两者都不参与选路。
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

// ErrAmbiguousProxyAddress 在多个节点共享同一地址时返回。
var ErrAmbiguousProxyAddress = errors.New("ambiguous proxy address")

func (s *Storage) requireUnambiguousAddress(address string) error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM proxies WHERE address = ?`, address).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	if count > 1 {
		return fmt.Errorf("%w: %q", ErrAmbiguousProxyAddress, address)
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
