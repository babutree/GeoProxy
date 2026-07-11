package webui

import (
	"strings"
	"testing"
)

func TestDashboardEscapesAPIFieldsBeforeInnerHTML(t *testing.T) {
	checks := []string{
		"function html(value)",
		"html(safe(st.singbox_nodes))",
		"html(safe(st.subscription_count))",
		"html(safe(st.disabled_count))",
		"html(safe(st.subscription_total))",
		"html(p.protocol)",
		"html(p.exit_ip)",
		"html(sourceLabel(p))",
		"html(regionOf(p))",
		// IP 风险分两列：abuserBadge 经 html(n.toFixed(2)) 转义分值，ipapiFlagsBadges 经 html(f) 转义标记。
		"abuserBadge(p.ipapiis_score)",
		"ipapiFlagsBadges(p.ipapi_flags,!!p.ipapi_flags_seen)",
		"html(sub.name)",
		"html(activeCount)",
		"html(pausedCount)",
		"html(disabledCount)",
		"html(line)",
		"html(s.session_id)",
		"html(region)",
		"function addressArg(address){return encodeURIComponent(String(address||'')).replace(/[!'()*]/g,c=>'%'+c.charCodeAt(0).toString(16).toUpperCase())}",
		"function proxyIDArg(proxy){const id=Number(proxy&&proxy.id);return Number.isFinite(id)?String(id):'0'}",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardHTML, check) {
				t.Fatalf("dashboardHTML missing escaped API-field usage %q", check)
			}
		})
	}

	for _, unsafe := range []string{
		"+safe(st.",
		"+safe(sub.",
		"+String(line).replace",
	} {
		t.Run("reject "+unsafe, func(t *testing.T) {
			if strings.Contains(dashboardHTML, unsafe) {
				t.Fatalf("dashboardHTML still contains unsafe innerHTML pattern %q", unsafe)
			}
		})
	}
}

// TestDashboardRiskColumnsAndBadges 验证 IP 风险分两列（分源展示不聚合）：
// 表头含 ipapi.is 滥用分列与 ip-api 标记列、colspan 为 10、abuserBadge 阈值逻辑、
// ipapiFlagsBadges 标记着色/干净/未探测逻辑、行渲染分别引用两列字段。
func TestDashboardRiskColumnsAndBadges(t *testing.T) {
	checks := []string{
		// 表头两列（ipapi.is 分数 + ip-api 标记）。
		"ipapi.is 滥用分",
		"<th>ip-api 标记</th>",
		// 两处 colspan 为 12（加载中 + 无匹配节点）：星标列 + CF 列共新增两列。
		"<td colspan=\"12\" class=\"empty\">加载中</td>",
		"<td colspan=\"12\" class=\"empty\">没有匹配节点</td>",
		// abuserBadge：<0 显示 "--"，否则两位小数 + 三色阈值(0.1/0.5)。
		"function abuserBadge(score){const n=Number(score);if(!Number.isFinite(n)||n<0)return '<span class=\"muted\">--</span>';const cls=n<0.1?'ok':(n<=0.5?'warn':'danger');return '<span class=\"badge '+cls+'\">'+html(n.toFixed(2))+'</span>'}",
		// ipapiFlagsBadges：空+seen 显"干净"、空+未探测显"--"、命中按类型着色。
		"function ipapiFlagsBadges(flags,seen){",
		"return seen?'<span class=\"badge ok\">干净</span>':'<span class=\"muted\">--</span>'",
		// 行渲染分别引用两列字段；ip-api 探测状态用 ipapi_flags_seen 判定。
		"abuserBadge(p.ipapiis_score)",
		"ipapiFlagsBadges(p.ipapi_flags,!!p.ipapi_flags_seen)",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardHTML, check) {
				t.Fatalf("dashboardHTML missing risk-columns invariant %q", check)
			}
		})
	}

	// 回归防护：旧的单列聚合模型（riskBadge/risk_score/9 列 colspan）不应再残留。
	for _, unsafe := range []string{
		"riskBadge(p.risk_score)",
		"<th>IP 风险分</th>",
		"<td colspan=\"9\" class=\"empty\">加载中</td>",
		"<td colspan=\"9\" class=\"empty\">没有匹配节点</td>",
	} {
		t.Run("reject "+unsafe, func(t *testing.T) {
			if strings.Contains(dashboardHTML, unsafe) {
				t.Fatalf("dashboardHTML still has stale aggregated risk model %q", unsafe)
			}
		})
	}
}

// TestDashboardStarCopyAndCFColumns 验证新增：星标列/CF 列（12 列）、cfBadge/copyProxyCred/
// toggleStar/starBtn/randSession 函数、星标可用置顶 sort 片段、/api/proxy/star 路由调用。
func TestDashboardStarCopyAndCFColumns(t *testing.T) {
	checks := []string{
		// 表头新增星标列与 CF 列。
		"<th>★</th>",
		"<th>CF 拦截</th>",
		// 两处 colspan 改为 12。
		"<td colspan=\"12\" class=\"empty\">加载中</td>",
		"<td colspan=\"12\" class=\"empty\">没有匹配节点</td>",
		// 新增 JS 函数。
		"function cfBadge(",
		"function copyProxyCred(",
		"function toggleStar(",
		"function starBtn(",
		"function randSession(",
		// 星标可用置顶 sort 片段：fa/fb 先于原有 order 比较。
		"const fa=(nodeState(a)==='ok'&&(a.starred===true||Number(a.starred)===1))?1:0",
		"const fb=(nodeState(b)==='ok'&&(b.starred===true||Number(b.starred)===1))?1:0",
		"if(fa!==fb)return fb-fa;",
		// 星标 API 路由。
		"/api/proxy/star",
		// 行渲染引用星标/CF/复制。
		"'<tr><td>'+starBtn(p)+'</td>",
		"cfBadge(p.cf_blocked)",
		"copyProxyCred('+id+')",
		// 取消星标须 confirm 确认。
		"if(!confirm('取消该节点星标？'))return",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardHTML, check) {
				t.Fatalf("dashboardHTML missing star/copy/cf invariant %q", check)
			}
		})
	}
}

func TestDashboardNodeStateAndRegionDistributionUseAvailableKnownRegions(t *testing.T) {
	checks := []string{
		// BUG-50: isAvailable 必须排除 user_paused，被暂停节点不算可用。
		"function isUserPaused(p){return !!(p&&(p.user_paused===true||Number(p.user_paused)===1))}",
		"function isAvailable(proxy){return !isUserPaused(proxy)&&(proxy.status==='active'||proxy.status==='degraded')&&Number(proxy.fail_count||0)<3}",
		// BUG-50: nodeState 主判据改为 user_paused（存储层新口径 status 仍为 active）。
		"function nodeState(p){if(isUserPaused(p)||p.status==='paused')return 'paused';if(isAvailable(p))return 'ok';if(p.status==='disabled'||Number(p.fail_count||0)>=3)return 'failed';return 'pending'}",
		// BUG-51: allRegions/地域分布均经 isAvailable 过滤，因此不再计入 user_paused 节点。
		"allRegions=Array.from(new Set(allProxies.filter(p=>isAvailable(p)&&isKnownRegion(p)).map(regionOf))).sort()",
		"allProxies.filter(p=>isAvailable(p)&&isKnownRegion(p)).forEach",
		"available nodes",
		"暂无可用地域数据",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardHTML, check) {
				t.Fatalf("dashboardHTML missing available-region invariant %q", check)
			}
		})
	}

	// 回归防护：isAvailable 必须真正读取 user_paused，不得退回旧口径（只看 status/fail_count）。
	if strings.Contains(dashboardHTML, "function isAvailable(proxy){return (proxy.status==='active'||proxy.status==='degraded')&&Number(proxy.fail_count||0)<3}") {
		t.Fatal("dashboardHTML isAvailable reverted to legacy form that ignores user_paused (BUG-50)")
	}
	// 回归防护：nodeState 不得只靠 status==='paused' 判定暂停（新数据 status 恒为 active，永不命中）。
	if strings.Contains(dashboardHTML, "function nodeState(p){if(p.status==='paused')return 'paused';") {
		t.Fatal("dashboardHTML nodeState reverted to legacy status==='paused' only check (BUG-50)")
	}
}

// TestDashboardPausedNodeTogglesToEnableButton 验证 BUG-50 修复后操作列按钮逻辑：
// user_paused 节点经 nodeState 归为 'paused'，renderProxies 据此显示“启用”按钮（enable=true），
// 非暂停节点显示“停用”按钮（enable=false）。
func TestDashboardPausedNodeTogglesToEnableButton(t *testing.T) {
	checks := []string{
		// toggleBtn 根据 nodeState 结果分支：paused -> 启用(true)，否则 停用(false)。
		"const toggleBtn=(st==='paused')?",
		",true)\">启用</button>'",
		",false)\">停用</button>'",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardHTML, check) {
				t.Fatalf("dashboardHTML missing paused-toggle-button invariant %q", check)
			}
		})
	}
}

func TestDashboardShowsPausedSubscriptionCountsAndLabels(t *testing.T) {
	checks := []string{
		"const pausedCount=Number(sub.paused_count??Math.max(0,proxyCount-activeCount-disabledCount))",
		"' 暂停 / '",
		"html(disabledCount)+' 不可用",
		"const badge=paused?'<span class=\"badge warn\">已暂停</span>'",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardHTML, check) {
				t.Fatalf("dashboardHTML missing paused subscription display %q", check)
			}
		})
	}
}

func TestDashboardShowsExplainedSingBoxStatusInsteadOfBoolFailure(t *testing.T) {
	checks := []string{
		"const status=String(st.singbox_status||(st.singbox_running?'running':'stopped'))",
		"no_tunnel_nodes:'无需运行'",
		"partial:'部分就绪'",
		"failed:'启动失败'",
		"<span class=\"k\">状态原因</span>",
		"html(reason)",
		"html(safe(st.singbox_ready_ports))+'/'+html(safe(st.singbox_total_ports))",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardHTML, check) {
				t.Fatalf("dashboardHTML missing explained sing-box status %q", check)
			}
		})
	}

	if strings.Contains(dashboardHTML, "running?'运行中':'未运行'") {
		t.Fatal("dashboardHTML still maps singbox_running=false directly to 未运行")
	}
}

func TestDashboardAsyncEntrypointsUseUnifiedErrorHandling(t *testing.T) {
	checks := []string{
		"async function runAsync(label, fn)",
		"try{data=JSON.parse(text)}catch(err){if(!res.ok)throw new Error(res.statusText||('HTTP '+res.status));throw new Error('响应解析失败')}",
		"async function refreshAll(){return runAsync('刷新失败'",
		"async function addManualNode(){return runAsync('添加失败'",
		"async function editManualRegion(id,address){return runAsync('地域更新失败'",
		"async function editManualNote(id,address){return runAsync('备注更新失败'",
		"async function deleteManualNode(id,address){return runAsync('删除失败'",
		"async function toggleProxy(id,address,enable){return runAsync('操作失败'",
		"async function addSubscription(){return runAsync('添加失败'",
		"async function refreshSub(id){return runAsync('刷新失败'",
		"async function refreshAllSubs(){return runAsync('刷新失败'",
		"async function toggleSub(id){return runAsync('切换失败'",
		"async function deleteSub(id){return runAsync('删除失败'",
		"async function saveConfig(){return runAsync('保存失败'",
		"setInterval(()=>runAsync('自动刷新失败'",
		"setInterval(()=>runAsync('日志刷新失败'",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardHTML, check) {
				t.Fatalf("dashboardHTML missing unified async handling %q", check)
			}
		})
	}

	if strings.Contains(dashboardHTML, ".catch(err=>showToast(err.message))") {
		t.Fatal("dashboardHTML still uses one-off catch instead of runAsync")
	}
}

func TestDashboardProxyActionsUseProxyIDAsPrimaryIdentity(t *testing.T) {
	checks := []string{
		"const id=proxyIDArg(p)",
		"toggleProxy('+id+',decodeURIComponent",
		"editManualRegion('+id+',decodeURIComponent",
		"editManualNote('+id+',decodeURIComponent",
		"deleteManualNode('+id+',decodeURIComponent",
		"const current=allProxies.find(p=>Number(p.id)===Number(id))||{}",
		"JSON.stringify({id,address,region})",
		"JSON.stringify({id,address,note})",
		"JSON.stringify({id,address})",
		"JSON.stringify({id,address,enable})",
		// 单节点测试连通按钮：走 proxy-id 优先身份，调用后端 /api/proxy/refresh。
		"testProxy('+id+',decodeURIComponent",
		"await api('/api/proxy/refresh',{method:'POST',body:JSON.stringify({id,address})})",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardHTML, check) {
				t.Fatalf("dashboardHTML missing proxy-id action invariant %q", check)
			}
		})
	}

	for _, unsafe := range []string{
		"toggleProxy(decodeURIComponent",
		"editManualRegion(decodeURIComponent",
		"editManualNote(decodeURIComponent",
		"deleteManualNode(decodeURIComponent",
		"allProxies.find(p=>p.address===address)",
		"JSON.stringify({address,region})",
		"JSON.stringify({address,note})",
		"JSON.stringify({address})",
		"JSON.stringify({address,enable})",
	} {
		t.Run("reject "+unsafe, func(t *testing.T) {
			if strings.Contains(dashboardHTML, unsafe) {
				t.Fatalf("dashboardHTML still depends on address-only proxy action pattern %q", unsafe)
			}
		})
	}
}

func TestDashboardDoesNotShowOKBadgeForEmptySessionRegion(t *testing.T) {
	checks := []string{
		"const region=String(s.region||'').trim().toLowerCase()",
		"const regionBadge=region&&region!=='unknown'?'<span class=\"badge ok\">'+html(region).toUpperCase()+'</span> ':'<span class=\"badge gray\">未知</span> '",
		"'+regionBadge+masked+'",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardHTML, check) {
				t.Fatalf("dashboardHTML missing empty-session-region guard %q", check)
			}
		})
	}

	if strings.Contains(dashboardHTML, "'<span class=\"badge ok\">'+html(s.region)") {
		t.Fatal("dashboardHTML still renders an ok badge directly from s.region")
	}
}

func TestDashboardConnectionExampleAvoidsHttpbinSinglePoint(t *testing.T) {
	if strings.Contains(dashboardHTML, "httpbin.org") {
		t.Fatal("dashboardHTML still uses httpbin.org as a connection example target")
	}
	if !strings.Contains(dashboardHTML, "https://www.gstatic.com/generate_204") {
		t.Fatal("dashboardHTML missing stable HTTPS connection example target")
	}
}
