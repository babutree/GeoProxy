package storage

import (
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// TestRiskColumnsMigrationAndDefault 验证风险列存在且默认值正确
// （未探测：ipapiis_score = -1，ipapi_flags = "", ipapi_flags_seen=false）。
func TestRiskColumnsMigrationAndDefault(t *testing.T) {
	store := newTestStorage(t)
	assertColumnExists(t, store, "ipapiis_score")
	assertColumnExists(t, store, "ipapi_flags")
	assertColumnExists(t, store, "ipapi_flags_seen")

	insertProxy(t, store, "risk-default:8080", "http", "us", SourceManual, 100, "active", 0)
	p, err := store.GetProxyByAddress("risk-default:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	// 未经风险探测的新节点，DB DEFAULT：ipapiis_score=-1、ipapi_flags=""。
	if p.IPAPIIsScore != -1 {
		t.Fatalf("default ipapiis_score = %v, want -1", p.IPAPIIsScore)
	}
	if p.IPAPIFlags != "" {
		t.Fatalf("default ipapi_flags = %q, want empty", p.IPAPIFlags)
	}
	if p.IPAPIFlagsSeen {
		t.Fatal("default ipapi_flags_seen = true, want false")
	}
}

// TestRiskColumnsMigrationIdempotentOnLegacyDB 在旧库（无风险列）上重复 New()，
// 迁移应幂等：第一次补两列，第二次不报错、值不变。
func TestRiskColumnsMigrationIdempotentOnLegacyDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "proxy.db")
	seedLegacyDB(t, dbPath)

	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() first run error = %v", err)
	}
	assertColumnExists(t, store, "ipapiis_score")
	assertColumnExists(t, store, "ipapi_flags")
	assertColumnExists(t, store, "ipapi_flags_seen")
	// 旧行迁移后默认值。
	var score float64
	var flags string
	var flagsSeen int
	if err := store.db.QueryRow(`SELECT ipapiis_score, ipapi_flags, ipapi_flags_seen FROM proxies WHERE address = '1.1.1.1:8080'`).Scan(&score, &flags, &flagsSeen); err != nil {
		t.Fatalf("read migrated risk columns: %v", err)
	}
	if score != -1 || flags != "" || flagsSeen != 0 {
		t.Fatalf("migrated legacy risk = (%v,%q,%d), want (-1,\"\",0)", score, flags, flagsSeen)
	}
	store.Close()

	// 第二次打开同一库：addProxyColumnIfMissing 应识别列已存在，跳过 ALTER，不报错。
	store2, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() second run error = %v", err)
	}
	defer store2.Close()
	assertColumnExists(t, store2, "ipapiis_score")
	assertColumnExists(t, store2, "ipapi_flags")
	assertColumnExists(t, store2, "ipapi_flags_seen")

	// 确认两列各只有一列（未被重复添加）。
	for _, col := range []string{"ipapiis_score", "ipapi_flags", "ipapi_flags_seen"} {
		var count int
		if err := store2.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name = ?`, col).Scan(&count); err != nil {
			t.Fatalf("count %s column: %v", col, err)
		}
		if count != 1 {
			t.Fatalf("%s column count = %d, want 1", col, count)
		}
	}
}

// TestUpdateProxyExitInfoWritesRiskSignals 正向：成功路径写入两源风险信号，scanProxy 正确读回。
func TestUpdateProxyExitInfoWritesRiskSignals(t *testing.T) {
	store := newTestStorage(t)
	insertProxy(t, store, "risk-write:8080", "http", "us", SourceManual, 100, "active", 0)
	p, err := store.GetProxyByAddress("risk-write:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}

	if err := store.UpdateProxyExitInfo(p.ID, "203.0.113.20", "US Ashburn", 42, 0.65, "proxy,hosting", -1, ""); err != nil {
		t.Fatalf("UpdateProxyExitInfo() error = %v", err)
	}
	p, _ = store.GetProxyByAddress("risk-write:8080")
	if p.IPAPIIsScore != 0.65 {
		t.Fatalf("ipapiis_score after update = %v, want 0.65", p.IPAPIIsScore)
	}
	if p.IPAPIFlags != "proxy,hosting" {
		t.Fatalf("ipapi_flags after update = %q, want proxy,hosting", p.IPAPIFlags)
	}
	if !p.IPAPIFlagsSeen {
		t.Fatal("ipapi_flags_seen after update = false, want true")
	}
	// 顺带确认出口信息与 fail_count 同路径写入正确（scanProxy 顺序对齐）。
	if p.ExitIP != "203.0.113.20" || p.Latency != 42 {
		t.Fatalf("exit info not written correctly: %#v", p)
	}
}

// TestUpdateProxyExitInfoNegativeScoreDoesNotOverwrite 负向：ipapiis_score 降级/未知(-1)
// 不得覆盖已有有效分；但 ipapi_flags 随每次成功探测覆盖（含清空，语义有效）。
func TestUpdateProxyExitInfoNegativeScoreDoesNotOverwrite(t *testing.T) {
	store := newTestStorage(t)
	insertProxy(t, store, "risk-keep:8080", "http", "us", SourceManual, 100, "active", 0)
	p, err := store.GetProxyByAddress("risk-keep:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}

	// 先写入有效分 0.80 + flags。
	if err := store.UpdateProxyExitInfo(p.ID, "203.0.113.21", "US Ashburn", 42, 0.80, "proxy", -1, ""); err != nil {
		t.Fatalf("UpdateProxyExitInfo(0.80) error = %v", err)
	}
	// 再以 -1（ipapi.is 探测降级/失败）更新：ipapiis_score 应保持 0.80 不被覆盖。
	if err := store.UpdateProxyExitInfo(p.ID, "203.0.113.21", "US Ashburn", 50, -1, "", -1, ""); err != nil {
		t.Fatalf("UpdateProxyExitInfo(-1) error = %v", err)
	}
	p, _ = store.GetProxyByAddress("risk-keep:8080")
	if p.IPAPIIsScore != 0.80 {
		t.Fatalf("ipapiis_score after -1 update = %v, want 0.80 (must not overwrite)", p.IPAPIIsScore)
	}
	if !p.IPAPIFlagsSeen {
		t.Fatal("ipapi_flags_seen after -1 update = false, want true")
	}
	// 其它字段（如延迟）仍应正常更新，证明只有 ipapiis_score 被条件保护。
	if p.Latency != 50 {
		t.Fatalf("latency after -1 update = %d, want 50", p.Latency)
	}

	// 从 0 分再写 -1 也不得覆盖（0 是有效低分）。
	if err := store.UpdateProxyExitInfo(p.ID, "203.0.113.21", "US Ashburn", 50, 0, "", -1, ""); err != nil {
		t.Fatalf("UpdateProxyExitInfo(0) error = %v", err)
	}
	if err := store.UpdateProxyExitInfo(p.ID, "203.0.113.21", "US Ashburn", 50, -1, "", -1, ""); err != nil {
		t.Fatalf("UpdateProxyExitInfo(-1 after 0) error = %v", err)
	}
	p, _ = store.GetProxyByAddress("risk-keep:8080")
	if p.IPAPIIsScore != 0 {
		t.Fatalf("ipapiis_score after 0 then -1 = %v, want 0 (must not overwrite with -1)", p.IPAPIIsScore)
	}
}

// TestUpdateProxyExitInfoClearsFailCountAndMarksSeenButKeepsStatus 覆盖成功探测写回契约：
// UpdateProxyExitInfo 归零 fail_count、置位 ipapi_flags_seen，但不改 status——
// 状态恢复（disabled→active）由调用点（如 apiRefreshProxy 的 EnableProxyByID）负责，
// 存储层不在此耦合，避免破坏订阅“先 Disable 再 Enable”流程（见回归 TestProxyIdentityConsistency…）。
func TestUpdateProxyExitInfoClearsFailCountAndMarksSeenButKeepsStatus(t *testing.T) {
	store := newTestStorage(t)
	insertProxy(t, store, "risk-reactivate:8080", "http", "us", SourceManual, 100, "disabled", 3)
	p, err := store.GetProxyByAddress("risk-reactivate:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}

	if err := store.UpdateProxyExitInfo(p.ID, "203.0.113.22", "US Ashburn", 35, -1, "", -1, ""); err != nil {
		t.Fatalf("UpdateProxyExitInfo() error = %v", err)
	}
	p, _ = store.GetProxyByAddress("risk-reactivate:8080")
	// fail_count 归零 + flags_seen 置位；status 保持 disabled（恢复由调用点负责，不在存储层）。
	if p.FailCount != 0 {
		t.Fatalf("fail_count after success = %d, want 0", p.FailCount)
	}
	if p.Status != "disabled" {
		t.Fatalf("status after UpdateProxyExitInfo = %q, want disabled (reactivation is caller's job)", p.Status)
	}
	if !p.IPAPIFlagsSeen {
		t.Fatal("ipapi_flags_seen after clean successful probe = false, want true")
	}

	// 调用点契约：EnableProxyByID 恢复 disabled→active，但不动 user_paused。
	if err := store.EnableProxyByID(p.ID); err != nil {
		t.Fatalf("EnableProxyByID() error = %v", err)
	}
	p, _ = store.GetProxyByAddress("risk-reactivate:8080")
	if p.Status != "active" {
		t.Fatalf("status after EnableProxyByID = %q, want active", p.Status)
	}
}

// TestProxyColumnsMatchScanProxy 弥补现有缺口：proxyColumns 与 scanProxy 的列数/顺序
// 必须严格一致。用真实 SELECT proxyColumns 后交给 scanProxy 扫描——若列数或类型不匹配，
// rows.Scan 会报错，从而捕获两处失步。
func TestProxyColumnsMatchScanProxy(t *testing.T) {
	store := newTestStorage(t)
	insertProxy(t, store, "cols-sync:8080", "http", "us", SourceManual, 123, "active", 0)
	if err := store.UpdateProxyExitInfo(mustProxyID(t, store, "cols-sync:8080"), "203.0.113.30", "US Ashburn", 123, 0.55, "hosting", -1, ""); err != nil {
		t.Fatalf("UpdateProxyExitInfo() error = %v", err)
	}

	rows, err := store.db.Query(`SELECT ` + proxyColumns + ` FROM proxies WHERE address = 'cols-sync:8080'`)
	if err != nil {
		t.Fatalf("query proxyColumns: %v", err)
	}
	defer rows.Close()

	// 列数与 scanProxy Scan 目标数须一致：rows.Columns 给出 SELECT 侧列数。
	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("rows.Columns(): %v", err)
	}
	// 当前模型：id..note(20) + ipapiis_score + ipapi_flags + ipapi_flags_seen + starred + cf_blocked + dual_protocol + ai_reachability = 27 列。硬编码断言，防止任一侧漏改。
	if len(cols) != 27 {
		t.Fatalf("proxyColumns yields %d columns, want 27 (id..note + risk columns + starred + cf_blocked + dual_protocol + ai_reachability)", len(cols))
	}
	// 最后四列必须与 scanProxy 末尾新增字段对齐。
	if cols[len(cols)-4] != "starred" || cols[len(cols)-3] != "cf_blocked" || cols[len(cols)-2] != "dual_protocol" || cols[len(cols)-1] != "ai_reachability" {
		t.Fatalf("last four SELECT columns = %q,%q,%q,%q, want starred,cf_blocked,dual_protocol,ai_reachability", cols[len(cols)-4], cols[len(cols)-3], cols[len(cols)-2], cols[len(cols)-1])
	}

	if !rows.Next() {
		t.Fatal("expected one row")
	}
	// 核心断言：scanProxy 能无错扫描全部列——列数/顺序不一致会导致 Scan 报错。
	p, err := scanProxy(rows)
	if err != nil {
		t.Fatalf("scanProxy failed (proxyColumns/scanProxy out of sync?): %v", err)
	}
	if p.Address != "cols-sync:8080" || p.IPAPIIsScore != 0.55 || p.IPAPIFlags != "hosting" || !p.IPAPIFlagsSeen || p.Latency != 123 {
		t.Fatalf("scanned proxy mismatch: %#v", p)
	}
	if rows.Err() != nil {
		t.Fatalf("rows error: %v", rows.Err())
	}
}

func mustProxyID(t *testing.T, store *Storage, address string) int64 {
	t.Helper()
	p, err := store.GetProxyByAddress(address)
	if err != nil {
		t.Fatalf("GetProxyByAddress(%s) error = %v", address, err)
	}
	return p.ID
}

// TestProxyColumnsIncludesRiskColumns 静态断言 proxyColumns 常量包含两个风险列，
// 便于在两处对齐测试之外提供一个直接的字符串防护。
func TestProxyColumnsIncludesRiskColumns(t *testing.T) {
	if !strings.Contains(proxyColumns, "ipapiis_score") {
		t.Fatal("proxyColumns constant missing ipapiis_score")
	}
	if !strings.Contains(proxyColumns, "ipapi_flags") {
		t.Fatal("proxyColumns constant missing ipapi_flags")
	}
	if !strings.Contains(proxyColumns, "ipapi_flags_seen") {
		t.Fatal("proxyColumns constant missing ipapi_flags_seen")
	}
}

// TestStarredAndCFBlockedDefaults 新节点默认 starred=false、cf_blocked=-1（未探测）。
func TestStarredAndCFBlockedDefaults(t *testing.T) {
	store := newTestStorage(t)
	insertProxy(t, store, "star-default:8080", "http", "us", SourceManual, 100, "active", 0)
	p, err := store.GetProxyByAddress("star-default:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if p.Starred {
		t.Fatalf("default starred = true, want false")
	}
	if p.CFBlocked != -1 {
		t.Fatalf("default cf_blocked = %d, want -1", p.CFBlocked)
	}
}

// TestSetProxyStarredTogglesFlag 置位/清位 starred 并被 scanProxy 读回。
func TestSetProxyStarredTogglesFlag(t *testing.T) {
	store := newTestStorage(t)
	insertProxy(t, store, "star-toggle:8080", "http", "us", SourceManual, 100, "active", 0)
	p, err := store.GetProxyByAddress("star-toggle:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}

	if err := store.SetProxyStarred(p.ID, true); err != nil {
		t.Fatalf("SetProxyStarred(true) error = %v", err)
	}
	p, _ = store.GetProxyByAddress("star-toggle:8080")
	if !p.Starred {
		t.Fatalf("starred after set true = false, want true")
	}

	if err := store.SetProxyStarred(p.ID, false); err != nil {
		t.Fatalf("SetProxyStarred(false) error = %v", err)
	}
	p, _ = store.GetProxyByAddress("star-toggle:8080")
	if p.Starred {
		t.Fatalf("starred after set false = true, want false")
	}
}

// TestUpdateProxyExitInfoWritesCFBlocked cf_blocked 仅在有效值(0/1)时写入并被读回；
// -1（本次未能探测/未知）不得覆盖已有有效值。
func TestUpdateProxyExitInfoWritesCFBlocked(t *testing.T) {
	store := newTestStorage(t)
	insertProxy(t, store, "cf-write:8080", "http", "us", SourceManual, 100, "active", 0)
	p, err := store.GetProxyByAddress("cf-write:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}

	if err := store.UpdateProxyExitInfo(p.ID, "203.0.113.40", "US Ashburn", 42, -1, "", 1, ""); err != nil {
		t.Fatalf("UpdateProxyExitInfo(cf=1) error = %v", err)
	}
	p, _ = store.GetProxyByAddress("cf-write:8080")
	if p.CFBlocked != 1 {
		t.Fatalf("cf_blocked after write 1 = %d, want 1", p.CFBlocked)
	}

	// -1 保护：本次未能探测(-1)不得覆盖之前的有效值 1。
	if err := store.UpdateProxyExitInfo(p.ID, "203.0.113.40", "US Ashburn", 42, -1, "", -1, ""); err != nil {
		t.Fatalf("UpdateProxyExitInfo(cf=-1) error = %v", err)
	}
	p, _ = store.GetProxyByAddress("cf-write:8080")
	if p.CFBlocked != 1 {
		t.Fatalf("cf_blocked after write -1 = %d, want 1 (-1 must not overwrite)", p.CFBlocked)
	}

	// 有效值覆盖为 0（未拦截）。
	if err := store.UpdateProxyExitInfo(p.ID, "203.0.113.40", "US Ashburn", 42, -1, "", 0, ""); err != nil {
		t.Fatalf("UpdateProxyExitInfo(cf=0) error = %v", err)
	}
	p, _ = store.GetProxyByAddress("cf-write:8080")
	if p.CFBlocked != 0 {
		t.Fatalf("cf_blocked after write 0 = %d, want 0", p.CFBlocked)
	}
}

// TestUpdateProxyExitInfoNegativeCFBlockedDoesNotOverwrite 负向回归：cf_blocked 的 -1 保护生效。
// -1 代表本次未能探测(未知)，必须保留数据库已有的有效值(0/1)，不得覆盖；有效值(0/1)则正常写入。
func TestUpdateProxyExitInfoNegativeCFBlockedDoesNotOverwrite(t *testing.T) {
	store := newTestStorage(t)
	insertProxy(t, store, "cf-keep:8080", "http", "us", SourceManual, 100, "active", 0)
	p, err := store.GetProxyByAddress("cf-keep:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}

	// 1) 先写入有效值 1（被拦截）。
	if err := store.UpdateProxyExitInfo(p.ID, "203.0.113.41", "US Ashburn", 42, -1, "", 1, ""); err != nil {
		t.Fatalf("UpdateProxyExitInfo(cf=1) error = %v", err)
	}
	p, _ = store.GetProxyByAddress("cf-keep:8080")
	if p.CFBlocked != 1 {
		t.Fatalf("cf_blocked after write 1 = %d, want 1", p.CFBlocked)
	}

	// 2) 以 -1（本次未能探测）更新：cf_blocked 应保持 1 不被覆盖。
	if err := store.UpdateProxyExitInfo(p.ID, "203.0.113.41", "US Ashburn", 50, -1, "", -1, ""); err != nil {
		t.Fatalf("UpdateProxyExitInfo(cf=-1) error = %v", err)
	}
	p, _ = store.GetProxyByAddress("cf-keep:8080")
	if p.CFBlocked != 1 {
		t.Fatalf("cf_blocked after -1 update = %d, want 1 (-1 must not overwrite)", p.CFBlocked)
	}
	// 其它字段（如延迟）仍应正常更新，证明只有 cf_blocked 被条件保护。
	if p.Latency != 50 {
		t.Fatalf("latency after -1 update = %d, want 50", p.Latency)
	}

	// 3) 再以有效值 0（未拦截）更新：cf_blocked 应正常覆盖为 0。
	if err := store.UpdateProxyExitInfo(p.ID, "203.0.113.41", "US Ashburn", 50, -1, "", 0, ""); err != nil {
		t.Fatalf("UpdateProxyExitInfo(cf=0) error = %v", err)
	}
	p, _ = store.GetProxyByAddress("cf-keep:8080")
	if p.CFBlocked != 0 {
		t.Fatalf("cf_blocked after write 0 = %d, want 0 (valid value must overwrite)", p.CFBlocked)
	}
}

// TestAIReachabilityColumnExistsAndDefaultEmpty 验证 ai_reachability 列存在且新节点默认空串
// （空串语义：整体未探测；前端显示 "--"，存储层据此不覆盖已有值）。
func TestAIReachabilityColumnExistsAndDefaultEmpty(t *testing.T) {
	store := newTestStorage(t)
	assertColumnExists(t, store, "ai_reachability")

	insertProxy(t, store, "ai-default:8080", "http", "us", SourceManual, 100, "active", 0)
	p, err := store.GetProxyByAddress("ai-default:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if p.AIReachability != "" {
		t.Fatalf("default ai_reachability = %q, want empty", p.AIReachability)
	}
}

// TestAIReachabilityColumnMigrationIdempotentOnLegacyDB 在旧库（无 ai_reachability 列）上
// 重复 New()：第一次补列，第二次不报错、值不变，且该列只出现一次（未被重复添加）。
func TestAIReachabilityColumnMigrationIdempotentOnLegacyDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "proxy.db")
	seedLegacyDB(t, dbPath)

	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() first run error = %v", err)
	}
	assertColumnExists(t, store, "ai_reachability")
	var ai string
	if err := store.db.QueryRow(`SELECT ai_reachability FROM proxies WHERE address = '1.1.1.1:8080'`).Scan(&ai); err != nil {
		t.Fatalf("read migrated ai_reachability: %v", err)
	}
	if ai != "" {
		t.Fatalf("migrated legacy ai_reachability = %q, want empty", ai)
	}
	store.Close()

	store2, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() second run error = %v", err)
	}
	defer store2.Close()
	assertColumnExists(t, store2, "ai_reachability")
	var count int
	if err := store2.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name = 'ai_reachability'`).Scan(&count); err != nil {
		t.Fatalf("count ai_reachability column: %v", err)
	}
	if count != 1 {
		t.Fatalf("ai_reachability column count = %d, want 1", count)
	}
}

// TestUpdateProxyExitInfoWritesAIReachability 正向：非空 JSON 写入并被 scanProxy 读回；
// 空串（本次整体未探测）不得覆盖已有有效值——与 cf_blocked 的 -1 保护同理。
func TestUpdateProxyExitInfoWritesAIReachability(t *testing.T) {
	store := newTestStorage(t)
	insertProxy(t, store, "ai-write:8080", "http", "us", SourceManual, 100, "active", 0)
	p, err := store.GetProxyByAddress("ai-write:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}

	const aiJSON = `{"claude":1,"gemini":0,"grok":-1,"openai":0}`
	if err := store.UpdateProxyExitInfo(p.ID, "203.0.113.50", "US Ashburn", 42, -1, "", -1, aiJSON); err != nil {
		t.Fatalf("UpdateProxyExitInfo(ai=json) error = %v", err)
	}
	p, _ = store.GetProxyByAddress("ai-write:8080")
	if p.AIReachability != aiJSON {
		t.Fatalf("ai_reachability after write = %q, want %q", p.AIReachability, aiJSON)
	}

	// 空串保护：本次整体未探测（空串）不得覆盖之前的有效 JSON。
	if err := store.UpdateProxyExitInfo(p.ID, "203.0.113.50", "US Ashburn", 50, -1, "", -1, ""); err != nil {
		t.Fatalf("UpdateProxyExitInfo(ai=empty) error = %v", err)
	}
	p, _ = store.GetProxyByAddress("ai-write:8080")
	if p.AIReachability != aiJSON {
		t.Fatalf("ai_reachability after empty update = %q, want %q (empty must not overwrite)", p.AIReachability, aiJSON)
	}
	// 其它字段（延迟）仍正常更新，证明只有 ai_reachability 被条件保护。
	if p.Latency != 50 {
		t.Fatalf("latency after empty update = %d, want 50", p.Latency)
	}

	// 有效值覆盖为新 JSON。
	const aiJSON2 = `{"claude":0,"gemini":0,"grok":0,"openai":0}`
	if err := store.UpdateProxyExitInfo(p.ID, "203.0.113.50", "US Ashburn", 50, -1, "", -1, aiJSON2); err != nil {
		t.Fatalf("UpdateProxyExitInfo(ai=json2) error = %v", err)
	}
	p, _ = store.GetProxyByAddress("ai-write:8080")
	if p.AIReachability != aiJSON2 {
		t.Fatalf("ai_reachability after write json2 = %q, want %q", p.AIReachability, aiJSON2)
	}
}

// TestAddSubscriptionDoesNotTouchAIReachability 边界隔离：ai_reachability 是 proxies 表列，
// 与 subscriptions 表无关。AddSubscription 只写 subscriptions，不应因缺列或误写而失败/串列。
func TestAddSubscriptionDoesNotTouchAIReachability(t *testing.T) {
	store := newTestStorage(t)
	subID, err := store.AddSubscription("sub-ai", "https://example.test/ai.yaml", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if subID <= 0 {
		t.Fatalf("AddSubscription() subID = %d, want > 0", subID)
	}
	// subscriptions 表不应含 ai_reachability 列（该列属于 proxies）。
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name = 'ai_reachability'`).Scan(&count); err != nil {
		t.Fatalf("count subscriptions.ai_reachability: %v", err)
	}
	if count != 0 {
		t.Fatalf("subscriptions has ai_reachability column count = %d, want 0", count)
	}
}
