package storage

import (
	"testing"
)

// TestUpdateExitInfoDoesNotClearTrustedExitOnPartialObservation 锁定公开出口写回 API
// 的空值保护：部分探测结果（缺 exit_ip/exit_location 或未测得延迟）不得清空已有有效
// 出口身份，也不得把 CalculateQualityGrade(0)=="S" 伪装成最优品质。
// 与 ApplyProbeObservation / RecordProbeFailure 的 CASE WHEN 语义保持一致。
func TestUpdateExitInfoDoesNotClearTrustedExitOnPartialObservation(t *testing.T) {
	store := newTestStorage(t)
	if err := store.AddManualProxy("203.0.113.5:8080", "http", "", "guard"); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	p, err := store.GetProxyByIdentity("203.0.113.5:8080", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() error = %v", err)
	}

	// 先写入一次完整可信观测。
	if err := store.UpdateProxyExitInfo(p.ID, "198.51.100.7", "US Seattle", 180, 0.1, "hosting", true, 0, `{"openai":0}`); err != nil {
		t.Fatalf("UpdateProxyExitInfo(full) error = %v", err)
	}
	full, err := store.GetProxyByID(p.ID)
	if err != nil {
		t.Fatalf("GetProxyByID() error = %v", err)
	}
	if full.ExitIP != "198.51.100.7" || full.ExitLocation != "US Seattle" || full.Latency != 180 || full.QualityGrade != "S" {
		t.Fatalf("baseline = ip:%q loc:%q lat:%d grade:%q", full.ExitIP, full.ExitLocation, full.Latency, full.QualityGrade)
	}

	// 部分观测：出口缺失 + 未测得延迟。既有出口与延迟/评级都必须保留。
	if err := store.UpdateProxyExitInfo(p.ID, "", "", 0, -1, "", false, -1, ""); err != nil {
		t.Fatalf("UpdateProxyExitInfo(partial) error = %v", err)
	}
	after, err := store.GetProxyByID(p.ID)
	if err != nil {
		t.Fatalf("GetProxyByID() error = %v", err)
	}
	if after.ExitIP != "198.51.100.7" {
		t.Fatalf("exit_ip = %q, want preserved 198.51.100.7 (partial observation must not clear it)", after.ExitIP)
	}
	if after.ExitLocation != "US Seattle" {
		t.Fatalf("exit_location = %q, want preserved US Seattle", after.ExitLocation)
	}
	if after.Latency != 180 {
		t.Fatalf("latency = %d, want preserved 180 (latencyMs<=0 means not measured)", after.Latency)
	}
	if after.QualityGrade != "S" {
		t.Fatalf("quality_grade = %q, want preserved S rather than a fabricated grade", after.QualityGrade)
	}

	// 完整观测仍必须能正常改写出口与延迟。
	if err := store.UpdateProxyExitInfo(p.ID, "203.0.113.9", "JP Tokyo", 640, -1, "", false, -1, ""); err != nil {
		t.Fatalf("UpdateProxyExitInfo(second full) error = %v", err)
	}
	updated, err := store.GetProxyByID(p.ID)
	if err != nil {
		t.Fatalf("GetProxyByID() error = %v", err)
	}
	if updated.ExitIP != "203.0.113.9" || updated.ExitLocation != "JP Tokyo" {
		t.Fatalf("trusted observation must overwrite: ip:%q loc:%q", updated.ExitIP, updated.ExitLocation)
	}
	if updated.Latency != 640 || updated.QualityGrade != "B" {
		t.Fatalf("measured latency must overwrite: lat:%d grade:%q, want 640/B", updated.Latency, updated.QualityGrade)
	}
}

// TestGetBatchForHealthCheckRejectsNonPositiveBatch 锁定批量下限。
// SQLite 的 LIMIT -1 语义是"无限制"：不校验会把整表拉进内存，
// 在 6000+ 节点规模下等于一次验证全部节点。
func TestGetBatchForHealthCheckRejectsNonPositiveBatch(t *testing.T) {
	store := newTestStorage(t)
	for _, addr := range []string{"203.0.113.31:8080", "203.0.113.32:8080", "203.0.113.33:8080"} {
		if err := store.AddManualProxy(addr, "http", "us", "batch"); err != nil {
			t.Fatalf("AddManualProxy(%s) error = %v", addr, err)
		}
		p, err := store.GetProxyByIdentity(addr, SourceManual, 0)
		if err != nil {
			t.Fatalf("GetProxyByIdentity(%s) error = %v", addr, err)
		}
		if err := store.EnableProxyByID(p.ID); err != nil {
			t.Fatalf("EnableProxyByID(%s) error = %v", addr, err)
		}
	}

	for _, batch := range []int{0, -1, -100} {
		if _, err := store.GetBatchForHealthCheck(batch); err == nil {
			t.Fatalf("GetBatchForHealthCheck(%d) returned nil error; non-positive batch must be rejected instead of scanning the whole table", batch)
		}
	}

	// 正常批量仍须受 LIMIT 约束。
	rows, err := store.GetBatchForHealthCheck(2)
	if err != nil {
		t.Fatalf("GetBatchForHealthCheck(2) error = %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("GetBatchForHealthCheck(2) returned %d rows, want 2", len(rows))
	}
}
