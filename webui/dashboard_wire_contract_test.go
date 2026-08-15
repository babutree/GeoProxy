package webui

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/babutree/GeoProxy/storage"
)

// captureWireResponse 用真实路由 + 真实 handler 采集一个 API 的原样响应。
// 契约断言链上不允许出现手写 JSON：后端字段改名或 nil 切片必须直接反映到前端行为。
func captureWireResponse(t *testing.T, server *Server, path string) dashboardWirePayload {
	t.Helper()
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, authenticatedJSONRequest(http.MethodGet, path, ""))
	return dashboardWirePayload{Status: rec.Code, Body: rec.Body.String()}
}

// seedActiveRegionProxy 造一个已验证通过、可选路的手工节点。
// region 用 region_source='manual' 固定，避免出口写回改写地域后与断言漂移。
func seedActiveRegionProxy(t *testing.T, server *Server, address, region string, latencyMs int) storage.Proxy {
	t.Helper()
	if err := server.storage.AddManualProxy(address, "socks5", region, "wire-node"); err != nil {
		t.Fatalf("AddManualProxy(%s) error = %v", address, err)
	}
	proxy, err := server.storage.GetProxyByIdentity(address, storage.SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(%s) error = %v", address, err)
	}
	// 写回出口观测：设置 latency 与 quality_grade（卫星轨道按品质档分桶）。
	if err := server.storage.UpdateProxyExitInfo(proxy.ID, "203.0.113.90", strings.ToUpper(region)+" Wire", latencyMs, 0, "", true, 0, ""); err != nil {
		t.Fatalf("UpdateProxyExitInfo(%s) error = %v", address, err)
	}
	if err := server.storage.EnableProxyByID(proxy.ID); err != nil {
		t.Fatalf("EnableProxyByID(%s) error = %v", address, err)
	}
	refreshed, err := server.storage.GetProxyByID(proxy.ID)
	if err != nil {
		t.Fatalf("GetProxyByID(%s) error = %v", address, err)
	}
	return *refreshed
}

// TestDashboardListEndpointsNeverEncodeJSONNull 锁定列表端点的空值形态。
// nil 切片会被编码成 "null"，而前端 `if(!data)return` 会当成请求失败提前退出，
// 表现为「删掉最后一条订阅后列表没变」。所有列表端点都必须返回 JSON 数组。
func TestDashboardListEndpointsNeverEncodeJSONNull(t *testing.T) {
	server := newTestServer(t)
	for _, path := range []string{"/api/subscriptions", "/api/proxies", "/api/sessions", "/api/proxy-occupancy"} {
		t.Run(path, func(t *testing.T) {
			payload := captureWireResponse(t, server, path)
			if payload.Status != http.StatusOK {
				t.Fatalf("status = %d, want %d; body=%s", payload.Status, http.StatusOK, payload.Body)
			}
			body := strings.TrimSpace(payload.Body)
			if body == "null" {
				t.Fatalf("%s returns JSON null on an empty table; the frontend treats it as a failed request", path)
			}
			if !strings.HasPrefix(body, "[") {
				t.Fatalf("%s body = %q, want a JSON array", path, body)
			}
		})
	}
}

// TestDashboardEmptySubscriptionWireClearsList 把空订阅表的真实响应喂给生产 JS，
// 断言前端会清空列表并结束骨架态（而不是永久停在灰条上）。
func TestDashboardEmptySubscriptionWireClearsList(t *testing.T) {
	server := newTestServer(t)
	wire := map[string]dashboardWirePayload{
		"/api/subscriptions": captureWireResponse(t, server, "/api/subscriptions"),
	}
	result := requireDashboardBehaviorScenarioWithWire(t, dashboardJS, "subs_empty_wire", wire)
	if result.Assertions == 0 {
		t.Fatal("harness did not execute any behavior assertions")
	}
}

// TestDashboardDeleteLastSubscriptionWireRemovesRow 覆盖端到端删除路径：
// 删除前后各采集一次真实 /api/subscriptions 响应，让生产 JS 走完
// deleteSub → showConfirm → POST → loadSubscriptions → renderSubscriptions。
func TestDashboardDeleteLastSubscriptionWireRemovesRow(t *testing.T) {
	server := newTestServer(t)
	subID, err := server.storage.AddSubscription("wire-sub", "https://example.com/sub", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	before := captureWireResponse(t, server, "/api/subscriptions")
	if !strings.Contains(before.Body, "wire-sub") {
		t.Fatalf("pre-delete body missing subscription: %s", before.Body)
	}

	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, authenticatedJSONRequest(http.MethodPost, "/api/subscription/delete", `{"id":`+strconv.FormatInt(subID, 10)+`}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	after := captureWireResponse(t, server, "/api/subscriptions")
	if strings.Contains(after.Body, "wire-sub") {
		t.Fatalf("post-delete body still contains subscription: %s", after.Body)
	}

	wire := map[string]dashboardWirePayload{
		"/api/subscriptions#before": before,
		"/api/subscriptions#after":  after,
		"/api/subscriptions":        after,
		"/api/subscription/delete":  {Status: http.StatusOK, Body: `{"status":"deleted"}`},
		"/api/stats":                captureWireResponse(t, server, "/api/stats"),
		"/api/proxies":              captureWireResponse(t, server, "/api/proxies"),
	}
	result := requireDashboardBehaviorScenarioWithWire(t, dashboardJS, "subs_delete_last", wire)
	if result.Assertions == 0 {
		t.Fatal("harness did not execute any behavior assertions")
	}
}

// TestDashboardDeleteSubscriptionPartialFailureStillRefreshes 覆盖
// apiSubscriptionDelete 的「订阅已删除但受管文件清理失败」500 分支：
// 服务端状态已经变了，前端不能因为 HTTP 非 2xx 就跳过列表刷新，
// 否则页面继续显示已删除的订阅。同时必须把真实错误报给用户，不谎报成功。
func TestDashboardDeleteSubscriptionPartialFailureStillRefreshes(t *testing.T) {
	server := newTestServer(t)
	subID, err := server.storage.AddSubscription("wire-sub", "https://example.com/sub", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	before := captureWireResponse(t, server, "/api/subscriptions")
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, authenticatedJSONRequest(http.MethodPost, "/api/subscription/delete", `{"id":`+strconv.FormatInt(subID, 10)+`}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	after := captureWireResponse(t, server, "/api/subscriptions")

	wire := map[string]dashboardWirePayload{
		"/api/subscriptions#before": before,
		"/api/subscriptions#after":  after,
		"/api/subscriptions":        after,
		// 真实 handler 在文件清理失败时返回的状态码与错误体。
		"/api/subscription/delete": {
			Status: http.StatusInternalServerError,
			Body:   `{"error":"subscription deleted but file cleanup failed"}`,
		},
		"/api/stats":   captureWireResponse(t, server, "/api/stats"),
		"/api/proxies": captureWireResponse(t, server, "/api/proxies"),
	}
	result := requireDashboardBehaviorScenarioWithWire(t, dashboardJS, "subs_delete_partial_failure", wire)
	if result.Assertions == 0 {
		t.Fatal("harness did not execute any behavior assertions")
	}
}

// TestDashboardOrbitSessionBeamWireRendersBeams 是会话字段契约的直接拦截点。
// 用真实 affinity 绑定 + 真实 /api/sessions、/api/proxies 响应驱动轨道渲染，
// 断言「存在 sticky 会话 ⇒ 至少一条连线 + 卫星点亮 + 地域面板计数 > 0」。
func TestDashboardOrbitSessionBeamWireRendersBeams(t *testing.T) {
	server := newTestServer(t)
	proxy := seedActiveRegionProxy(t, server, "198.51.100.11:1080", "us", 150)
	// 与 selector.Resolve 一致：绑定 Region 取所选节点的 proxy.Region。
	server.affinity.SetProxy("wire-session", proxy.ID, proxy.Address, proxy.Region)

	sessions := captureWireResponse(t, server, "/api/sessions")
	if strings.Contains(sessions.Body, `"region"`) {
		t.Fatalf("/api/sessions unexpectedly exposes a bare region field: %s", sessions.Body)
	}
	wire := map[string]dashboardWirePayload{
		"/api/sessions": sessions,
		"/api/proxies":  captureWireResponse(t, server, "/api/proxies"),
	}
	result := requireDashboardBehaviorScenarioWithWire(t, dashboardJS, "orbit_session_beams", wire)
	if result.Assertions == 0 {
		t.Fatal("harness did not execute any behavior assertions")
	}
}

// TestDashboardOrbitBeamHarnessRejectsLegacyRegionField 是变异测试：
// 把生产 JS 的地域键退回旧的 s.region，契约场景必须失败。
// 这证明上面的断言真的能拦住这次字段改名，而不是恰好通过。
func TestDashboardOrbitBeamHarnessRejectsLegacyRegionField(t *testing.T) {
	server := newTestServer(t)
	proxy := seedActiveRegionProxy(t, server, "198.51.100.12:1080", "us", 150)
	server.affinity.SetProxy("wire-session", proxy.ID, proxy.Address, proxy.Region)

	wire := map[string]dashboardWirePayload{
		"/api/sessions": captureWireResponse(t, server, "/api/sessions"),
		"/api/proxies":  captureWireResponse(t, server, "/api/proxies"),
	}
	mutated := injectDashboardBehaviorOverride(t, dashboardJS,
		"function sessionRegionKey(s){const r=String((s&&s.region)||'').trim().toLowerCase();return (!r||r==='unknown')?'':r}\n")
	_, stderr, err := runDashboardBehaviorScenarioWithWire(t, mutated, "orbit_session_beams", wire)
	if err == nil {
		t.Fatal("behavior harness accepted the legacy s.region regression")
	}
	if !strings.Contains(stderr, "session beam") &&
		!strings.Contains(stderr, "session region key") &&
		!strings.Contains(stderr, "marked live") &&
		!strings.Contains(stderr, "counts sticky sessions") {
		t.Fatalf("mutation failed for an unexpected reason: %v\nstderr:\n%s", err, stderr)
	}
}

// TestDashboardSubscriptionHarnessRejectsNullListRegression 是变异测试：
// 用 "null"（修复前的后端形态）替换空订阅响应，契约场景必须失败。
func TestDashboardSubscriptionHarnessRejectsNullListRegression(t *testing.T) {
	wire := map[string]dashboardWirePayload{
		"/api/subscriptions": {Status: http.StatusOK, Body: "null\n"},
	}
	_, stderr, err := runDashboardBehaviorScenarioWithWire(t, dashboardJS, "subs_empty_wire", wire)
	if err == nil {
		t.Fatal("behavior harness accepted a JSON null subscription list")
	}
	if !strings.Contains(stderr, "empty subscription wire response") {
		t.Fatalf("mutation failed for an unexpected reason: %v\nstderr:\n%s", err, stderr)
	}
}

// TestDashboardDeleteHarnessRejectsSkippedRefreshOnPartialFailure 是变异测试：
// 把 deleteSub 退回「非 2xx 直接抛出、不刷新列表」的旧实现，
// 部分失败场景必须失败——证明该断言真的在保护刷新行为。
func TestDashboardDeleteHarnessRejectsSkippedRefreshOnPartialFailure(t *testing.T) {
	server := newTestServer(t)
	subID, err := server.storage.AddSubscription("wire-sub", "https://example.com/sub", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	before := captureWireResponse(t, server, "/api/subscriptions")
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, authenticatedJSONRequest(http.MethodPost, "/api/subscription/delete", `{"id":`+strconv.FormatInt(subID, 10)+`}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	after := captureWireResponse(t, server, "/api/subscriptions")
	wire := map[string]dashboardWirePayload{
		"/api/subscriptions#before": before,
		"/api/subscriptions#after":  after,
		"/api/subscriptions":        after,
		"/api/subscription/delete": {
			Status: http.StatusInternalServerError,
			Body:   `{"error":"subscription deleted but file cleanup failed"}`,
		},
		"/api/stats":   captureWireResponse(t, server, "/api/stats"),
		"/api/proxies": captureWireResponse(t, server, "/api/proxies"),
	}
	mutated := injectDashboardBehaviorOverride(t, dashboardJS,
		"async function deleteSub(id){return runAsync(t('err_delete'),async()=>{if(!(await showConfirm(t('confirm_delete_sub'))))return;await api('/api/subscription/delete',{method:'POST',body:JSON.stringify({id})});await Promise.all([loadSubscriptions(),loadStats(),loadProxies()]);showToast(t('toast_sub_deleted'))})}\n")
	_, stderr, err := runDashboardBehaviorScenarioWithWire(t, mutated, "subs_delete_partial_failure", wire)
	if err == nil {
		t.Fatal("behavior harness accepted a delete flow that skips refresh on partial failure")
	}
	if !strings.Contains(stderr, "partial delete failure") {
		t.Fatalf("mutation failed for an unexpected reason: %v\nstderr:\n%s", err, stderr)
	}
}
