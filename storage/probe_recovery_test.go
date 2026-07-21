package storage

import (
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestRecoverSubscriptionProxyWithExitInfoRollsBackOnMetadataFailure
// 验证探测恢复必须把出口元数据和 active 状态作为同一原子操作：
// SQLite 触发器拒绝元数据写入时，节点仍保持 disabled 且旧元数据不变，
// 后续移除故障后可以重试恢复。
func TestRecoverSubscriptionProxyWithExitInfoRollsBackOnMetadataFailure(t *testing.T) {
	store := newTestStorage(t)
	subID, err := store.AddSubscription("probe-recovery", "https://example.test/probe", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	const address = "probe-recovery:8080"
	if err := store.AddProxyWithSource(address, "http", SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}
	if err := store.UpdateSubscriptionProxyExitInfo(address, subID, "198.51.100.10", "US Ashburn", 321, 0.2, "hosting", true, 0, "{}"); err != nil {
		t.Fatalf("seed exit info: %v", err)
	}
	if err := store.DisableSubscriptionProxy(address, subID); err != nil {
		t.Fatalf("DisableSubscriptionProxy() error = %v", err)
	}
	if _, err := store.GetDB().Exec(
		`UPDATE proxies
		 SET region = ?, region_source = 'manual', fail_count = ?, last_check = ?,
		     quality_grade = ?, ipapiis_score = ?, ipapi_flags = ?,
		     ipapi_flags_seen = 1, cf_blocked = ?, ai_reachability = ?
		 WHERE address = ? AND source = ? AND subscription_id = ?`,
		"manual-region", 7, "2024-01-02 03:04:05", "C", 0.2, "hosting", 1,
		`{"openai":1}`, address, SourceSubscription, subID,
	); err != nil {
		t.Fatalf("seed exact disabled snapshot: %v", err)
	}
	before, err := store.GetProxyByIdentity(address, SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() before recovery: %v", err)
	}

	if _, err := store.GetDB().Exec(`
		CREATE TRIGGER fail_probe_recovery_metadata
		BEFORE UPDATE OF exit_ip ON proxies
		WHEN NEW.exit_ip = '203.0.113.99'
		BEGIN
			SELECT RAISE(ABORT, 'injected probe metadata failure');
		END`); err != nil {
		t.Fatalf("create metadata failure trigger: %v", err)
	}

	err = store.RecoverSubscriptionProxyWithExitInfo(address, subID, "203.0.113.99", "JP Tokyo", 42, 0.01, "", true, 0, "{}")
	if err == nil {
		t.Fatal("RecoverSubscriptionProxyWithExitInfo() expected injected metadata failure, got nil")
	}
	if !strings.Contains(err.Error(), "injected probe metadata failure") {
		t.Fatalf("recovery error = %v, want injected metadata failure", err)
	}

	proxy, err := store.GetProxyByIdentity(address, SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() after failed recovery: %v", err)
	}
	if !reflect.DeepEqual(*proxy, *before) {
		t.Fatalf("metadata failure changed proxy:\n before=%#v\n after=%#v", *before, *proxy)
	}

	if _, err := store.GetDB().Exec(`DROP TRIGGER fail_probe_recovery_metadata`); err != nil {
		t.Fatalf("drop metadata failure trigger: %v", err)
	}
	if err := store.RecoverSubscriptionProxyWithExitInfo(address, subID, "203.0.113.99", "JP Tokyo", 42, -1, "", false, -1, ""); err != nil {
		t.Fatalf("recovery retry error = %v", err)
	}
	proxy, err = store.GetProxyByIdentity(address, SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() after recovery retry: %v", err)
	}
	if proxy.Status != "active" || proxy.ExitIP != "203.0.113.99" || proxy.ExitLocation != "JP Tokyo" || proxy.Latency != 42 {
		t.Fatalf("metadata after recovery retry = %#v, want active/new values", proxy)
	}
	if proxy.Region != before.Region || proxy.RegionSource != "manual" {
		t.Fatalf("manual region after retry = (%q,%q), want (%q,manual)", proxy.Region, proxy.RegionSource, before.Region)
	}
	if proxy.IPAPIIsScore != before.IPAPIIsScore || proxy.IPAPIFlags != before.IPAPIFlags || proxy.IPAPIFlagsSeen != before.IPAPIFlagsSeen || proxy.CFBlocked != before.CFBlocked || proxy.AIReachability != before.AIReachability {
		t.Fatalf("unknown risk inputs overwrote prior values: before=%#v after=%#v", *before, *proxy)
	}
	if proxy.FailCount != 0 || proxy.QualityGrade != "S" || proxy.LastCheck.Equal(before.LastCheck) {
		t.Fatalf("recovery health fields = fail:%d grade:%q last:%v; before last:%v", proxy.FailCount, proxy.QualityGrade, proxy.LastCheck, before.LastCheck)
	}
}

// TestRecoverSubscriptionProxyWithExitInfoRespectsPausedSubscription
// 父订阅暂停时不得恢复节点，也不得写入本次探测的出口元数据。
func TestRecoverSubscriptionProxyWithExitInfoRespectsPausedSubscription(t *testing.T) {
	store := newTestStorage(t)
	subID, err := store.AddSubscription("paused-probe", "https://example.test/paused", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	const address = "paused-probe:8080"
	if err := store.AddProxyWithSource(address, "http", SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}
	if err := store.UpdateSubscriptionProxyExitInfo(address, subID, "198.51.100.20", "US Ashburn", 123, -1, "", false, -1, ""); err != nil {
		t.Fatalf("seed exit info: %v", err)
	}
	if err := store.DisableSubscriptionProxy(address, subID); err != nil {
		t.Fatalf("DisableSubscriptionProxy() error = %v", err)
	}
	if err := store.PauseSubscription(subID); err != nil {
		t.Fatalf("PauseSubscription() error = %v", err)
	}
	before, err := store.GetProxyByIdentity(address, SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() before paused recovery: %v", err)
	}

	if err := store.RecoverSubscriptionProxyWithExitInfo(address, subID, "203.0.113.20", "JP Tokyo", 44, 0.01, "proxy", true, 1, "{}"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("paused recovery error = %v, want sql.ErrNoRows", err)
	}
	proxy, err := store.GetProxyByIdentity(address, SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() after paused recovery: %v", err)
	}
	if !reflect.DeepEqual(*proxy, *before) {
		t.Fatalf("paused recovery changed proxy:\n before=%#v\n after=%#v", *before, *proxy)
	}
}

// TestRecoverSubscriptionProxyWithExitInfoRollsBackOnActivationFailure
// 模拟 active 状态写入失败；同一语句中的出口元数据也必须一起回滚。
func TestRecoverSubscriptionProxyWithExitInfoRollsBackOnActivationFailure(t *testing.T) {
	store := newTestStorage(t)
	subID, err := store.AddSubscription("activation-failure", "https://example.test/activation", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	const address = "activation-failure:8080"
	if err := store.AddProxyWithSource(address, "http", SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}
	if err := store.UpdateSubscriptionProxyExitInfo(address, subID, "198.51.100.30", "US Ashburn", 222, 0.3, "proxy", true, 1, `{"openai":1}`); err != nil {
		t.Fatalf("seed exit info: %v", err)
	}
	if err := store.DisableSubscriptionProxy(address, subID); err != nil {
		t.Fatalf("DisableSubscriptionProxy() error = %v", err)
	}
	before, err := store.GetProxyByIdentity(address, SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() before activation failure: %v", err)
	}
	if _, err := store.GetDB().Exec(`
		CREATE TRIGGER fail_probe_recovery_activation
		BEFORE UPDATE OF status ON proxies
		WHEN NEW.status = 'active'
		BEGIN
			SELECT RAISE(ABORT, 'injected probe activation failure');
		END`); err != nil {
		t.Fatalf("create activation failure trigger: %v", err)
	}

	err = store.RecoverSubscriptionProxyWithExitInfo(address, subID, "203.0.113.30", "JP Tokyo", 40, 0.01, "", true, 0, "{}")
	if err == nil || !strings.Contains(err.Error(), "injected probe activation failure") {
		t.Fatalf("activation recovery error = %v, want injected activation failure", err)
	}
	after, err := store.GetProxyByIdentity(address, SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() after activation failure: %v", err)
	}
	if !reflect.DeepEqual(*after, *before) {
		t.Fatalf("activation failure changed proxy:\n before=%#v\n after=%#v", *before, *after)
	}
}

// TestRecoverSubscriptionProxyWithExitInfoRejectsAlreadyActive
// 重复恢复 active 节点必须返回零行错误，不能用新探测值覆盖现有元数据。
func TestRecoverSubscriptionProxyWithExitInfoRejectsAlreadyActive(t *testing.T) {
	store := newTestStorage(t)
	subID, err := store.AddSubscription("active-probe", "https://example.test/active", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	const address = "active-probe:8080"
	if err := store.AddProxyWithSource(address, "http", SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}
	if err := store.RecoverSubscriptionProxyWithExitInfo(address, subID, "203.0.113.40", "JP Tokyo", 40, 0.01, "", true, 0, "{}"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("already-active recovery error = %v, want sql.ErrNoRows", err)
	}
}

// TestRecoverSubscriptionProxyWithExitInfoRejectsMissingSubscription
// 孤儿订阅节点不得利用“没有 paused 行”绕过父订阅边界恢复为 active。
func TestRecoverSubscriptionProxyWithExitInfoRejectsMissingSubscription(t *testing.T) {
	store := newTestStorage(t)
	subID, err := store.AddSubscription("missing-parent", "https://example.test/missing-parent", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	const address = "missing-parent:8080"
	if err := store.AddProxyWithSource(address, "http", SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}
	if err := store.UpdateSubscriptionProxyExitInfo(address, subID, "198.51.100.50", "US Ashburn", 155, 0.4, "hosting", true, 1, `{"openai":1}`); err != nil {
		t.Fatalf("seed exit info: %v", err)
	}
	if err := store.DisableSubscriptionProxy(address, subID); err != nil {
		t.Fatalf("DisableSubscriptionProxy() error = %v", err)
	}
	if _, err := store.GetDB().Exec(`DELETE FROM subscriptions WHERE id = ?`, subID); err != nil {
		t.Fatalf("delete parent subscription directly: %v", err)
	}

	before, err := store.GetProxyByIdentity(address, SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() before orphan recovery: %v", err)
	}
	if err := store.RecoverSubscriptionProxyWithExitInfo(address, subID, "203.0.113.50", "JP Tokyo", 45, 0.01, "proxy", true, 0, "{}"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("orphan recovery error = %v, want sql.ErrNoRows", err)
	}
	after, err := store.GetProxyByIdentity(address, SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() after orphan recovery: %v", err)
	}
	if !reflect.DeepEqual(*after, *before) {
		t.Fatalf("orphan recovery changed proxy:\n before=%#v\n after=%#v", *before, *after)
	}
}

// TestUpdateDisabledSubscriptionProxyExitInfoInitializesMissingRetentionClock
// 验证历史 disabled 订阅节点缺失 last_check 时，首次地理过滤写回会初始化回收起点；
// 已有回收起点则必须保持不变，避免周期探测续期长期禁用保留窗口。
func TestUpdateDisabledSubscriptionProxyExitInfoInitializesMissingRetentionClock(t *testing.T) {
	store := newTestStorage(t)
	subID, err := store.AddSubscription("disabled-retention-clock", "", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	const address = "disabled-retention-clock:8080"
	if err := store.AddProxyWithSource(address, "http", SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}
	if err := store.DisableSubscriptionProxy(address, subID); err != nil {
		t.Fatalf("DisableSubscriptionProxy() error = %v", err)
	}
	if _, err := store.GetDB().Exec(
		"UPDATE proxies SET last_check = NULL WHERE address = ? AND source = ? AND subscription_id = ?",
		address, SourceSubscription, subID,
	); err != nil {
		t.Fatalf("clear last_check: %v", err)
	}

	if err := store.UpdateDisabledSubscriptionProxyExitInfo(address, subID, "203.0.113.70", "JP Tokyo", 70, -1, "", true, -1, ""); err != nil {
		t.Fatalf("initial disabled metadata write: %v", err)
	}
	proxy, err := store.GetProxyByIdentity(address, SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() after NULL write: %v", err)
	}
	if proxy.LastCheck.IsZero() {
		t.Fatal("disabled metadata write left NULL last_check; node would never reach retention cutoff")
	}

	checkedAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	if _, err := store.GetDB().Exec(
		"UPDATE proxies SET last_check = ? WHERE address = ? AND source = ? AND subscription_id = ?",
		checkedAt.Format("2006-01-02 15:04:05"), address, SourceSubscription, subID,
	); err != nil {
		t.Fatalf("seed existing last_check: %v", err)
	}
	if err := store.UpdateDisabledSubscriptionProxyExitInfo(address, subID, "203.0.113.71", "JP Osaka", 71, -1, "", true, -1, ""); err != nil {
		t.Fatalf("existing disabled metadata write: %v", err)
	}
	proxy, err = store.GetProxyByIdentity(address, SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() after existing clock write: %v", err)
	}
	if !proxy.LastCheck.Equal(checkedAt) {
		t.Fatalf("existing disabled retention clock renewed: got %s, want %s", proxy.LastCheck, checkedAt)
	}
}

func TestUpdateDisabledSubscriptionProxyExitInfoRejectsActiveTarget(t *testing.T) {
	store := newTestStorage(t)
	subID, err := store.AddSubscription("active-retention-race", "", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	const address = "active-retention-race:8080"
	if err := store.AddProxyWithSource(address, "http", SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}
	if err := store.UpdateSubscriptionProxyExitInfo(address, subID, "198.51.100.80", "US Ashburn", 80, 0.2, "hosting", true, 0, "{}"); err != nil {
		t.Fatalf("seed active metadata: %v", err)
	}
	before, err := store.GetProxyByIdentity(address, SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() before special write: %v", err)
	}

	err = store.UpdateDisabledSubscriptionProxyExitInfo(address, subID, "203.0.113.80", "JP Tokyo", 20, 0.9, "proxy", true, 1, `{"openai":1}`)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("active special write error = %v, want sql.ErrNoRows", err)
	}
	after, err := store.GetProxyByIdentity(address, SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() after special write: %v", err)
	}
	if !reflect.DeepEqual(*after, *before) {
		t.Fatalf("active special write changed proxy:\n before=%#v\n after=%#v", *before, *after)
	}
}
