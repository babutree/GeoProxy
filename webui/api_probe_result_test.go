package webui

import (
	"testing"
	"time"

	"github.com/babutree/GeoProxy/validator"
)

// WebUI 的批量刷新会包含已禁用节点。复检失败只能更新探测观测，不能因管理员重复点击而累加失败计数。
func TestApplyProbeResultDoesNotIncreaseFailureCountForDisabledRoute(t *testing.T) {
	server := newTestServer(t)
	if err := server.storage.AddManualProxy("disabled-refresh.example:1080", "socks5", "us", ""); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	proxy, err := server.storage.GetProxyByAddress("disabled-refresh.example:1080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if err := server.storage.DisableProxyByID(proxy.ID); err != nil {
		t.Fatalf("DisableProxyByID() error = %v", err)
	}

	err = server.applyProbeResult(validator.Result{
		Proxy:         *proxy,
		Latency:       25 * time.Millisecond,
		FailureReason: validator.FailureConnectivity,
	})
	if err != nil {
		t.Fatalf("applyProbeResult() error = %v", err)
	}

	after, err := server.storage.GetProxyByAddress("disabled-refresh.example:1080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() after probe error = %v", err)
	}
	if after.Status != "disabled" {
		t.Fatalf("status = %q, want disabled", after.Status)
	}
	if after.FailCount != 0 {
		t.Fatalf("fail_count = %d, want 0 for disabled-route recheck", after.FailCount)
	}
}

// 成功探测必须在同一路由身份上恢复系统禁用节点，并记录可信出口快照。
func TestApplyProbeResultRecoversDisabledRouteWithTrustedExit(t *testing.T) {
	server := newTestServer(t)
	if err := server.storage.AddManualProxy("recover-refresh.example:1080", "socks5", "us", ""); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	proxy, err := server.storage.GetProxyByAddress("recover-refresh.example:1080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if err := server.storage.DisableProxyByID(proxy.ID); err != nil {
		t.Fatalf("DisableProxyByID() error = %v", err)
	}

	err = server.applyProbeResult(validator.Result{
		Proxy:        *proxy,
		Valid:        true,
		Latency:      25 * time.Millisecond,
		ExitIP:       "203.0.113.71",
		ExitLocation: "GB London",
		Risk:         validator.UnknownRisk(),
	})
	if err != nil {
		t.Fatalf("applyProbeResult() error = %v", err)
	}

	after, err := server.storage.GetProxyByAddress("recover-refresh.example:1080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() after probe error = %v", err)
	}
	if after.Status != "active" || after.ExitIP != "203.0.113.71" || after.ExitLocation != "GB London" || after.ExitCheckedAt.IsZero() {
		t.Fatalf("recovered proxy = %#v", after)
	}
	if after.FailCount != 0 || !after.DisabledAt.IsZero() {
		t.Fatalf("recovered clocks = fail_count:%d disabled_at:%v", after.FailCount, after.DisabledAt)
	}
}

// 结果所属路由已重绑时，异步探测不得把旧出口证据写入新端点。
func TestApplyProbeResultRejectsReboundRoute(t *testing.T) {
	server := newTestServer(t)
	const nodeKey = "manual:webui-probe-cas"
	if err := server.storage.AddManualProxyWithNodeKey("old-refresh.example:1080", "socks5", "us", "", nodeKey); err != nil {
		t.Fatalf("AddManualProxyWithNodeKey(old) error = %v", err)
	}
	stale, err := server.storage.GetProxyByNodeKey(nodeKey)
	if err != nil {
		t.Fatalf("GetProxyByNodeKey(old) error = %v", err)
	}
	if err := server.storage.AddManualProxyWithNodeKey("new-refresh.example:1081", "http", "gb", "", nodeKey); err != nil {
		t.Fatalf("AddManualProxyWithNodeKey(new) error = %v", err)
	}

	err = server.applyProbeResult(validator.Result{
		Proxy:        *stale,
		Valid:        true,
		Latency:      25 * time.Millisecond,
		ExitIP:       "203.0.113.72",
		ExitLocation: "US Ashburn",
		Risk:         validator.UnknownRisk(),
	})
	if err == nil {
		t.Fatal("applyProbeResult() error = nil, want stale route rejection")
	}

	current, err := server.storage.GetProxyByNodeKey(nodeKey)
	if err != nil {
		t.Fatalf("GetProxyByNodeKey(new) error = %v", err)
	}
	if current.Address != "new-refresh.example:1081" || current.ExitIP != "" || current.ExitLocation != "" {
		t.Fatalf("stale result changed rebound route: %#v", current)
	}
}

// 地域策略拒绝不是上游故障：保留可信出口快照，但不建立系统禁用时钟或失败计数。
func TestApplyProbeResultGeoRejectionKeepsExitWithoutSystemFailureClock(t *testing.T) {
	server := newTestServer(t)
	if err := server.storage.AddManualProxy("policy-refresh.example:1080", "socks5", "gb", ""); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	proxy, err := server.storage.GetProxyByAddress("policy-refresh.example:1080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if err := server.storage.EnableProxyByID(proxy.ID); err != nil {
		t.Fatalf("EnableProxyByID() error = %v", err)
	}
	if err := server.storage.DisableProxyByID(proxy.ID); err != nil {
		t.Fatalf("DisableProxyByID() error = %v", err)
	}

	err = server.applyProbeResult(validator.Result{
		Proxy:         *proxy,
		Latency:       25 * time.Millisecond,
		ExitIP:        "203.0.113.73",
		ExitLocation:  "US Ashburn",
		Risk:          validator.UnknownRisk(),
		FailureReason: validator.FailureGeoRejected,
	})
	if err != nil {
		t.Fatalf("applyProbeResult() error = %v", err)
	}

	after, err := server.storage.GetProxyByAddress("policy-refresh.example:1080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() after probe error = %v", err)
	}
	if after.Status != "disabled" || after.ExitIP != "203.0.113.73" || after.ExitLocation != "US Ashburn" || after.ExitCheckedAt.IsZero() {
		t.Fatalf("policy-rejected proxy = %#v", after)
	}
	if after.FailCount != 0 || !after.DisabledAt.IsZero() {
		t.Fatalf("policy rejection created system failure clocks: fail_count:%d disabled_at:%v", after.FailCount, after.DisabledAt)
	}
}
