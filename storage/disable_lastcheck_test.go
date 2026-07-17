package storage

import (
	"testing"
	"time"
)

// TestDisableProxyByIDSetsLastCheck 覆盖 bug6 症状「验证失败的手工节点永远显示待验证」的存储层根因：
// DisableProxyByID 仅写 status='disabled' 却不写 last_check，导致前端 nodeState 把
// 「已验证但失败」误判为「从未验证(待验证)」。禁用必须留下验证时间戳。
func TestDisableProxyByIDSetsLastCheck(t *testing.T) {
	store := newTestStorage(t)
	if err := store.AddManualProxy("1.2.3.4:3129", "http", "us", "note"); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	p, err := store.GetProxyByIdentity("1.2.3.4:3129", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() error = %v", err)
	}
	if !p.LastCheck.IsZero() {
		t.Fatalf("新建手工节点 last_check 应为空, 得到 %v", p.LastCheck)
	}

	if err := store.DisableProxyByID(p.ID); err != nil {
		t.Fatalf("DisableProxyByID() error = %v", err)
	}

	got, err := store.GetProxyByID(p.ID)
	if err != nil {
		t.Fatalf("GetProxyByID() error = %v", err)
	}
	if got.Status != "disabled" {
		t.Fatalf("status = %q, want disabled", got.Status)
	}
	if got.LastCheck.IsZero() {
		t.Fatal("DisableProxyByID 后 last_check 仍为空: 验证失败节点会被前端误判为「待验证」而非「不可用」")
	}
	if time.Since(got.LastCheck) > time.Hour {
		t.Fatalf("last_check 时间戳异常: %v", got.LastCheck)
	}
}
