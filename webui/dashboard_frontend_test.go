package webui

import (
	"strings"
	"testing"
)

// dashboardBundle 聚合前端产物：CSS/JS 已从 dashboardHTML 合规分离到 dashboardCSS/dashboardJS，
// 由 /assets/dashboard.css、/assets/dashboard.js 路由下发。前端 invariant 断言针对该聚合串，
// 等价于“最终送达浏览器的 HTML+CSS+JS 中包含该片段”，语义与分离前一致，覆盖不降低。
var dashboardBundle = dashboardHTML + dashboardCSS + dashboardJS

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
		"html(tf('sub_counts',{a:activeCount,p:pausedCount,d:disabledCount}))",
		"html(line)",
		"html(s.session_id)",
		"html(region)",
		"function addressArg(address){return encodeURIComponent(String(address||'')).replace(/[!'()*]/g,c=>'%'+c.charCodeAt(0).toString(16).toUpperCase())}",
		"function proxyIDArg(proxy){const id=Number(proxy&&proxy.id);return Number.isFinite(id)?String(id):'0'}",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
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
			if strings.Contains(dashboardBundle, unsafe) {
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
		`data-i18n="th_ipapi_flags">ip-api 标记</th>`,
		// 两处 colspan 为 14（加载中 + 无匹配节点）：勾选列 + 星标 + CF + AI。
		`class="empty" data-i18n="loading">加载中</td>`,
		"<td colspan=\"14\" class=\"empty\">'+html(t('no_match'))+'</td>",
		// abuserBadge：<0 显示 "--"，否则两位小数 + 三色阈值(0.1/0.5)。
		"function abuserBadge(score){const n=Number(score);if(!Number.isFinite(n)||n<0)return '<span class=\"muted\">--</span>';const cls=n<0.1?'ok':(n<=0.5?'warn':'danger');return '<span class=\"badge '+cls+'\">'+html(n.toFixed(2))+'</span>'}",
		// ipapiFlagsBadges：空+seen 显"干净"、空+未探测显"--"、命中按类型着色。
		"function ipapiFlagsBadges(flags,seen){",
		"return seen?'<span class=\"badge ok\">'+html(t('badge_clean'))+'</span>':'<span class=\"muted\">--</span>'",
		// 行渲染分别引用两列字段；ip-api 探测状态用 ipapi_flags_seen 判定。
		"abuserBadge(p.ipapiis_score)",
		"ipapiFlagsBadges(p.ipapi_flags,!!p.ipapi_flags_seen)",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
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
			if strings.Contains(dashboardBundle, unsafe) {
				t.Fatalf("dashboardHTML still has stale aggregated risk model %q", unsafe)
			}
		})
	}
}

// TestDashboardStarCopyAndCFColumns 验证新增：星标列/CF 列（12 列）、cfBadge/copyProxyCred/
// toggleStar/starBtn/randSession 函数、星标可用置顶 sort 片段、/api/proxy/star 路由调用。
func TestDashboardStarCopyAndCFColumns(t *testing.T) {
	checks := []string{
		// 表头新增星标列与 Cloudflare 列（th-ico 图标+正规短标签）。
		"<th>★</th>",
		`data-i18n-title="th_cf_title"`,
		`<span class="tx">Cloudflare</span>`,
		// 两处 colspan 为 14（含多选勾选列）。
		`class="empty" data-i18n="loading">加载中</td>`,
		"<td colspan=\"14\" class=\"empty\">'+html(t('no_match'))+'</td>",
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
		"starBtn(p)",
		"cfBadge(p.cf_blocked)",
		"copyProxyCred('+id+')",
		// 取消星标须应用内确认弹窗（替换浏览器 confirm）。
		"const ok=await showConfirm(t('confirm_unstar'),t('btn_unstar'))",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboardHTML missing star/copy/cf invariant %q", check)
			}
		})
	}
}

func TestDashboardLogsAutoScrollFollowsWhenVisible(t *testing.T) {
	checks := []string{
		"function isLogsTabVisible(){const page=document.getElementById('page-logs');return !!(page&&page.classList.contains('active'))}",
		"function stickLogsToBottom(box){",
		"requestAnimationFrame(function(){apply();requestAnimationFrame(apply)})",
		"if(follow){if(isLogsTabVisible())stickLogsToBottom(box);return}",
		"if(name==='logs'){runAsync(t('err_log_refresh'),async()=>{await loadLogs();",
		"if(auto&&auto.checked)stickLogsToBottom(document.getElementById('logs-box'))",
		"auto.addEventListener('change',function(){if(this.checked)stickLogsToBottom(document.getElementById('logs-box'))})",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboardHTML missing logs autoscroll invariant %q", check)
			}
		})
	}
	// 不得再使用「仅一次赋值 scrollTop=scrollHeight 且不检查可见性」的旧贴底。
	if strings.Contains(dashboardBundle, "if(follow){box.scrollTop=box.scrollHeight;return}") {
		t.Fatal("dashboardHTML loadLogs still uses layout-unsafe single scrollTop assignment for autoscroll")
	}
}

func TestDashboardNodeStateAndRegionDistributionUseAvailableKnownRegions(t *testing.T) {
	checks := []string{
		// BUG-50: isAvailable 必须排除 user_paused，被暂停节点不算可用。
		"function isUserPaused(p){return !!(p&&(p.user_paused===true||Number(p.user_paused)===1))}",
		// 父订阅 paused/孤儿节点也不得计入可用（与 CountAll/选路 scope 对齐）。
		"function isParentSubscriptionSelectable(p){if(!p||p.source!=='subscription')return true;return String(p.subscription_status||'').trim().toLowerCase()==='active'}",
		"function isParentSubscriptionPaused(p){return !!(p&&p.source==='subscription'&&String(p.subscription_status||'').trim().toLowerCase()==='paused')}",
		"function isAvailable(proxy){return isParentSubscriptionSelectable(proxy)&&!isUserPaused(proxy)&&(proxy.status==='active'||proxy.status==='degraded')&&Number(proxy.fail_count||0)<3}",
		// BUG-50: nodeState 主判据改为 user_paused（存储层新口径 status 仍为 active）。
		"function hasLastCheck(p){",
		"function nodeState(p){if(isParentSubscriptionPaused(p))return 'sub_paused';if(p&&p.source==='subscription'&&!isParentSubscriptionSelectable(p))return 'failed';if(isUserPaused(p)||p.status==='paused')return 'paused';if(isAvailable(p))return 'ok';if(Number(p.fail_count||0)>=3)return 'failed';if(p.status==='disabled')return hasLastCheck(p)?'failed':'pending';return 'pending'}",
		"case 'sub_paused':return '<span class=\"badge warn\">'+html(t('status_sub_paused'))+'</span>'",
		"if(st==='sub_paused'){toggleBtn='<button class=\"mini\" disabled title=\"'+html(t('status_sub_paused'))+'\">'+html(t('status_sub_paused'))+'</button>'}",
		"const showRegion=isKnownRegion(p)",
		// BUG-51: allRegions/地域分布均经 isAvailable 过滤，因此不再计入 user_paused 节点。
		"allRegions=Array.from(new Set(allProxies.filter(p=>isAvailable(p)&&isKnownRegion(p)).map(regionOf))).sort()",
		"allProxies.filter(p=>isAvailable(p)&&isKnownRegion(p)).forEach",
		"region_total_zero:",
		"region_empty:",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboardHTML missing available-region invariant %q", check)
			}
		})
	}

	// 回归防护：isAvailable 必须真正读取 user_paused，不得退回旧口径（只看 status/fail_count）。
	if strings.Contains(dashboardBundle, "function isAvailable(proxy){return (proxy.status==='active'||proxy.status==='degraded')&&Number(proxy.fail_count||0)<3}") {
		t.Fatal("dashboardHTML isAvailable reverted to legacy form that ignores user_paused (BUG-50)")
	}
	if strings.Contains(dashboardBundle, "function isAvailable(proxy){return !isUserPaused(proxy)&&(proxy.status==='active'||proxy.status==='degraded')&&Number(proxy.fail_count||0)<3}") {
		t.Fatal("dashboardHTML isAvailable ignores parent subscription pause (must match CountAll scope)")
	}
	// 回归防护：nodeState 不得只靠 status==='paused' 判定暂停（新数据 status 恒为 active，永不命中）。
	if strings.Contains(dashboardBundle, "function nodeState(p){if(p.status==='paused')return 'paused';") {
		t.Fatal("dashboardHTML nodeState reverted to legacy status==='paused' only check (BUG-50)")
	}
}

// TestDashboardPausedNodeTogglesToEnableButton 验证 BUG-50 修复后操作列按钮逻辑：
// user_paused 节点经 nodeState 归为 'paused'，renderProxies 据此显示“启用”按钮（enable=true），
// 非暂停节点显示“停用”按钮（enable=false）。
func TestDashboardPausedNodeTogglesToEnableButton(t *testing.T) {
	checks := []string{
		// toggleBtn 根据 nodeState 结果分支：paused -> 启用(true)，否则 停用(false)。
		"let toggleBtn;if(st==='sub_paused')",
		",true)\">'+html(t('btn_enable'))+'</button>",
		",false)\">'+html(t('btn_disable'))+'</button>",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboardHTML missing paused-toggle-button invariant %q", check)
			}
		})
	}
}

func TestDashboardShowsPausedSubscriptionCountsAndLabels(t *testing.T) {
	checks := []string{
		"const pausedCount=Number(sub.paused_count??Math.max(0,proxyCount-activeCount-disabledCount))",
		"sub_counts:'{a} 可用 / {p} 暂停 / {d} 不可用'",
		"html(tf('sub_counts',{a:activeCount,p:pausedCount,d:disabledCount}))",
		"const badge=paused?'<span class=\"badge warn\">'+html(t('sub_paused_badge'))+'</span>'",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboardHTML missing paused subscription display %q", check)
			}
		})
	}
}

func TestDashboardShowsExplainedSingBoxStatusInsteadOfBoolFailure(t *testing.T) {
	checks := []string{
		"const status=String(st.singbox_status||(st.singbox_running?'running':'stopped'))",
		"no_tunnel_nodes:t('singbox_no_tunnel')",
		"partial:t('singbox_partial')",
		"failed:t('singbox_failed')",
		"html(t('singbox_reason'))",
		"html(reason)",
		"html(safe(st.singbox_ready_ports))+'/'+html(safe(st.singbox_total_ports))",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboardHTML missing explained sing-box status %q", check)
			}
		})
	}

	if strings.Contains(dashboardBundle, "running?'运行中':'未运行'") {
		t.Fatal("dashboardHTML still maps singbox_running=false directly to 未运行")
	}
}

func TestDashboardAsyncEntrypointsUseUnifiedErrorHandling(t *testing.T) {
	checks := []string{
		"async function runAsync(label, fn)",
		"try{data=JSON.parse(text)}catch(err){if(!res.ok)throw new Error(res.statusText||('HTTP '+res.status));throw new Error(t('err_op'))}",
		"async function refreshAll(){return runAsync(t('err_op')",
		"async function addManualNode(){return runAsync(t('err_add')",
		"async function nodeModalSave(){return runAsync(t('err_save')",
		"async function toggleProxy(id,address,enable){return runAsync(t('err_op')",
		"async function addSubscription(){return runAsync(t('err_add')",
		"async function refreshSub(id){return runAsync(t('err_refresh')",
		"async function refreshAllSubs(){return runAsync(t('err_refresh')",
		"async function toggleSub(id){return runAsync(t('err_toggle')",
		"async function deleteSub(id){return runAsync(t('err_delete')",
		"async function saveConfig(){return runAsync(t('err_save')",
		"setInterval(()=>runAsync(t('err_op')",
		"setInterval(()=>runAsync(t('err_log_refresh')",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboardHTML missing unified async handling %q", check)
			}
		})
	}

	if strings.Contains(dashboardBundle, ".catch(err=>showToast(err.message))") {
		t.Fatal("dashboardHTML still uses one-off catch instead of runAsync")
	}
}

func TestDashboardProxyActionsUseProxyIDAsPrimaryIdentity(t *testing.T) {
	checks := []string{
		"const id=proxyIDArg(p)",
		"toggleProxy('+id+',decodeURIComponent",
		"manageNode('+id+')",
		"const p=allProxies.find(x=>Number(x.id)===Number(id))||{}",
		"JSON.stringify({id,region})",
		"JSON.stringify({id,note})",
		"JSON.stringify({id,address,enable})",
		// 单节点测试连通按钮：走 proxy-id 优先身份，调用后端 /api/proxy/refresh。
		"testProxy('+id+',decodeURIComponent",
		"await api('/api/proxy/refresh',{method:'POST',body:JSON.stringify({id,address})})",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboardHTML missing proxy-id action invariant %q", check)
			}
		})
	}

	for _, unsafe := range []string{
		"toggleProxy(decodeURIComponent",
		"manageManualNode(decodeURIComponent",
		"allProxies.find(p=>p.address===address)",
		"JSON.stringify({address,region})",
		"JSON.stringify({address,note})",
		"JSON.stringify({address})",
		"JSON.stringify({address,enable})",
	} {
		t.Run("reject "+unsafe, func(t *testing.T) {
			if strings.Contains(dashboardBundle, unsafe) {
				t.Fatalf("dashboardHTML still depends on address-only proxy action pattern %q", unsafe)
			}
		})
	}
}

func TestDashboardDoesNotShowOKBadgeForEmptySessionRegion(t *testing.T) {
	checks := []string{
		"const region=String(s.exit_region||'').trim().toLowerCase()",
		// 空/unknown 地域不得渲染 ok 徽章；出口节点单独显示地址（不再拼 regionBadge+masked）。
		"const regionBadge=region&&region!=='unknown'?'<span class=\"badge ok\">'+html(region).toUpperCase()+'</span>':'<span class=\"badge gray\">'+html(t('session_unknown'))+'</span>'",
		"s.route_label",
		"sessionQualityBadge(s.quality_grade)",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboardHTML missing empty-session-region guard %q", check)
			}
		})
	}

	if strings.Contains(dashboardBundle, "'<span class=\"badge ok\">'+html(s.region)") {
		t.Fatal("dashboardHTML still renders an ok badge directly from s.region")
	}
	// 旧的 regionBadge 直接拼进出口节点单元格会把“未知”徽章塞进地址列。
	if strings.Contains(dashboardBundle, "'+regionBadge+masked+'") {
		t.Fatal("dashboardHTML still concatenates regionBadge into exit-node cell")
	}
}

func TestDashboardConnectionExampleAvoidsHttpbinSinglePoint(t *testing.T) {
	if strings.Contains(dashboardBundle, "httpbin.org") {
		t.Fatal("dashboardHTML still uses httpbin.org as a connection example target")
	}
	if !strings.Contains(dashboardBundle, "https://www.gstatic.com/generate_204") {
		t.Fatal("dashboardHTML missing stable HTTPS connection example target")
	}
}

// TestDashboardProtocolBadgesShowMixedNodeDualLabels 验证需求1：协议列改用 protocolBadges(p)，
// dual_protocol=true 的 mixed 节点渲染 SOCKS5+HTTP 两个徽章，其余节点渲染单个协议徽章。
// 方案Y：以存储层显式字段 p.dual_protocol 判定，而非靠地址长相猜测（避免本机 direct socks5 误判）。
func TestDashboardProtocolBadgesShowMixedNodeDualLabels(t *testing.T) {
	checks := []string{
		// 新增封装函数，保持可测。
		"function protocolBadges(p){",
		// mixed 节点（dual_protocol=true）渲染两个徽章。
		"if(isDualProtocol(p))return '<span class=\"badge blue\">SOCKS5</span> <span class=\"badge blue\">HTTP</span>'",
		// 非 mixed 节点渲染单个存储协议徽章（沿用 html(p.protocol) 转义）。
		"return '<span class=\"badge blue\">'+html(p.protocol).toUpperCase()+'</span>'",
		// dual_protocol 显式判定函数（读后端下发的布尔字段）。
		"function isDualProtocol(p){return !!(p&&(p.dual_protocol===true||Number(p.dual_protocol)===1))}",
		// 行渲染协议列改为调用 protocolBadges(p)。
		"<td>'+protocolBadges(p)+'</td>",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboardHTML missing protocol-badges invariant %q", check)
			}
		})
	}

	// 回归防护：协议列不得再内联单徽章，须走 protocolBadges 封装。
	if strings.Contains(dashboardBundle, "<td><span class=\"badge blue\">'+html(p.protocol).toUpperCase()+'</span></td>") {
		t.Fatal("dashboardHTML still inlines single protocol badge in row instead of protocolBadges(p)")
	}
	// 回归防护：不得再靠地址长相猜 mixed（方案A 已被方案Y 取代）。
	if strings.Contains(dashboardBundle, "if(addr.startsWith('127.0.0.1:'))return '<span class=\"badge blue\">SOCKS5</span>") {
		t.Fatal("dashboardHTML still guesses mixed node by 127.0.0.1 address instead of dual_protocol field")
	}
}

// TestDashboardProtocolFilterUsesInboundCapabilities 以完整 predicate 及分支片段锁定
// 可审计真值表；仓库当前没有 JS runtime，本测试不冒充浏览器行为测试。
func TestDashboardProtocolFilterUsesInboundCapabilities(t *testing.T) {
	const predicate = `function nodeSupportsInboundProtocol(p,protocol){const wanted=String(protocol||'').trim().toLowerCase();if(!wanted)return true;const actual=String((p&&p.protocol)||'').trim().toLowerCase();const dual=!!(p&&(p.dual_protocol===true||p.dual_protocol===1));if(dual)return wanted==='http'||wanted==='socks5';return actual===wanted}`
	t.Run("predicate definition", func(t *testing.T) {
		if !strings.Contains(dashboardBundle, predicate) {
			t.Fatalf("dashboard missing auditable inbound-protocol predicate %q", predicate)
		}
	})

	truthTableEvidence := []struct {
		name     string
		evidence string
	}{
		{name: "empty filter matches all", evidence: "if(!wanted)return true"},
		{name: "dual marker is bool true or numeric one", evidence: "const dual=!!(p&&(p.dual_protocol===true||p.dual_protocol===1))"},
		{name: "dual matches only http and socks5", evidence: "if(dual)return wanted==='http'||wanted==='socks5'"},
		{name: "pure nodes match their stored protocol only", evidence: "return actual===wanted"},
	}
	for _, tt := range truthTableEvidence {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, tt.evidence) {
				t.Fatalf("dashboard missing protocol truth-table evidence %q", tt.evidence)
			}
		})
	}

	t.Run("renderProxies uses predicate", func(t *testing.T) {
		const usage = "let rows=allProxies.filter(p=>nodeSupportsInboundProtocol(p,protocol)&&(!region||regionOf(p)===region))"
		if !strings.Contains(dashboardBundle, usage) {
			t.Fatalf("renderProxies missing inbound-protocol predicate usage %q", usage)
		}
	})

	t.Run("legacy exact predicate removed", func(t *testing.T) {
		if strings.Contains(dashboardBundle, "(!protocol||p.protocol===protocol)") {
			t.Fatal("renderProxies still uses exact stored-protocol predicate")
		}
	})
}

// TestDashboardNodeOrbit 验证总览节点分布图：按可用地域+延迟档聚合，session 画连线，rAF 动画。
func TestDashboardNodeOrbit(t *testing.T) {
	checks := []string{
		`data-i18n="card_orbit"`,
		`id="orbit-stage"`,
		`id="orbit-svg"`,
		`id="orbit-sats"`,
		`id="orbit-gw-ip"`,
		`id="orbit-pause-btn"`,
		"function renderOrbitSystem(){",
		"function buildOrbitSats(){",
		"function orbitFrame(",
		"requestAnimationFrame(orbitFrame)",
		"isAvailable(p)&&isKnownRegion(p)",
		"function orbitQualityTrack(",
		"orbitSessions",
		"function renderWorldMap(){renderOrbitSystem()}",
		"renderRegions();renderOrbitSystem()",
		"function renderSessions(sessions){const list=Array.isArray(sessions)?sessions:[];orbitSessions=list;renderOrbitSystem()",
		"S ≤200ms",
		"会话连线（越粗绑定越多）",
		"function updateSolarWind(",
		"function updateGravLens(",
		"function spawnWindStreams(",
		`id="orbit-wind"`,
		`id="orbit-lens"`,
		// 颜色 1:1：beam energy 读 --sun-energy（对齐 orbit-dashboard.html）
		"getPropertyValue('--sun-energy')",
		// 折叠/展开与回到总览即时重算几何，避免网关/轨道数秒错位
		"function scheduleOrbitReflow(",
		"function applySidebar(collapsed){document.body.classList.toggle('sidebar-collapsed',!!collapsed);try{localStorage.setItem('gg-sidebar',collapsed?'1':'0')}catch(e){}scheduleOrbitReflow()}",
		"function markViewLazy(name){if(name==='overview'){try{renderOrbitSystem()}catch(e){}",
		// 星标视觉：.star 类 + 已点亮 .on
		`class="star'+(on?' on':'')+'"`,
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboardHTML missing node-orbit invariant %q", check)
			}
		})
	}
	// 旧世界地图轮廓 path / COUNTRY_XY 不得残留。
	for _, bad := range []string{
		`id="world-map"`,
		"class=\"worldmap-land\"",
		"const COUNTRY_XY={",
		"us:[228,142]",
		"let worldMapSessions=[]",
	} {
		t.Run("reject "+bad, func(t *testing.T) {
			if strings.Contains(dashboardBundle, bad) {
				t.Fatalf("dashboard still has legacy world-map fragment %q", bad)
			}
		})
	}
}

// TestDashboardCopyProxyCredPrefersStableNodeKey 网关节点复制优先 -node-key-<node_key>，
// 而非易变的 127.0.0.1:本地端口（端口回收后不再代表同一上游配置）。
func TestDashboardCopyProxyCredPrefersStableNodeKey(t *testing.T) {
	checks := []string{
		"const nkey=String(p.node_key||'').trim()",
		"function encodeNodeKeyPin(nkey)",
		"btoa(unescape(encodeURIComponent(nkey)))",
		"const pin='key-'+encodeNodeKeyPin(nkey)",
		"const user=base+'-node-'+pin",
		"无法复制：该网关节点缺少稳定 NodeKey，请刷新订阅或重新导入节点后重试",
		"锁定节点身份，非出口IP",
	}
	for _, check := range checks {
		if !strings.Contains(dashboardBundle, check) {
			t.Fatalf("copyProxyCred missing stable identity pin %q", check)
		}
	}

	if strings.Contains(dashboardBundle, "):addr; const user=base+'-node-'+pin") {
		t.Fatal("copyProxyCred must not fall back to a temporary gateway address when node_key is empty")
	}
}

// TestDashboardCopyProxyCredBuildsFullURL 验证 copyProxyCred：
// - 网关节点：协议://用户名DSL:密码@网关入口（密码可为空占位 PASSWORD）
// - 直连节点（手工 HTTP/SOCKS 等非 tunnel）：协议://节点自身 IP:端口，绝不拼网关密码
// mixed 网关节点用应用内协议选择弹窗；成功 toast 不回显含真实密码的完整 URL。
func TestDashboardCopyProxyCredBuildsFullURL(t *testing.T) {
	checks := []string{
		// 直连 vs 网关分支：dual_protocol 或回环本地地址才走网关 DSL。
		"function isGatewayNode(p){",
		"function isDirectNode(p){return !isGatewayNode(p)}",
		// mixed 协议选择必须走应用内弹窗，禁止浏览器 confirm 冒充协议。
		"function showProtocolPick()",
		"id=\"protocol-pick-modal\"",
		"选择复制协议",
		"复制 SOCKS5",
		"复制 HTTP",
		"async function copyProxyCred(id)",
		"scheme=await showProtocolPick()",
		// 直连复制：有 username 则拼 scheme://user:pass@addr，否则 scheme://addr（无 userinfo）。
		"if(isDirectNode(p)){",
		"const u=String(p.username||'')",
		"const url=u?(scheme+'://'+encodeProxyUserInfo(u)+':'+encodeProxyUserInfo(String(p.password||''))+'@'+addr):(scheme+'://'+addr)",
		"showToast(t('toast_copied_direct'))",
		// 网关复制：仍用 DSL + 密码（空则 PASSWORD 占位）。
		"const rawPass=(configCache&&configCache.proxy_auth_password)?configCache.proxy_auth_password:''",
		"const pass=rawPass||'PASSWORD'",
		"const url=scheme+'://'+encodeProxyUserInfo(user)+':'+encodeProxyUserInfo(pass)+'@'+host",
		"function encodeProxyUserInfo(value){return encodeURIComponent(String(value||'')).replace(/[!'()*]/g,c=>'%'+c.charCodeAt(0).toString(16).toUpperCase())}",
		"已复制，请将 PASSWORD 替换为真实密码",
		"锁定节点身份，非出口IP",
		"showToast(okMsg)",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboardHTML missing copy-full-url invariant %q", check)
			}
		})
	}

	// 回归防护：直连节点不得再强制拼网关 DSL 密码到任意 p.address。
	if strings.Contains(dashboardBundle, "const url=scheme+'://'+encodeProxyUserInfo(user)+':'+encodeProxyUserInfo(pass)+'@'+addr") {
		t.Fatal("dashboardHTML copyProxyCred still appends gateway auth to raw node address (breaks open SOCKS/HTTP)")
	}
	if strings.Contains(dashboardBundle, "navigator.clipboard.writeText(cred)") {
		t.Fatal("dashboardHTML copyProxyCred still copies bare username DSL instead of full proxy URL")
	}
	if strings.Contains(dashboardBundle, "showToast('已复制: '+url)") {
		t.Fatal("dashboardHTML copyProxyCred success toast must not echo full proxy URL with password")
	}
	if strings.Contains(dashboardBundle, "showToast(url)") {
		t.Fatal("dashboardHTML copyProxyCred success toast must not echo url variable with password")
	}
	// 回归：禁止用浏览器 confirm 的确定/取消冒充 SOCKS5/HTTP 选择。
	if strings.Contains(dashboardBundle, "confirm('确定复制 SOCKS5") || strings.Contains(dashboardBundle, "confirm(\"确定复制 SOCKS5") {
		t.Fatal("copyProxyCred still uses browser confirm for protocol selection")
	}
	if strings.Contains(dashboardBundle, "取消则复制 HTTP") {
		t.Fatal("copyProxyCred still uses cancel-as-HTTP browser confirm wording")
	}
}

// TestDashboardFormatAPIKeyTimeHidesGoZero 末次使用为零值时显示 --，不得出现 1/1/1。
func TestDashboardFormatAPIKeyTimeHidesGoZero(t *testing.T) {
	checks := []string{
		"function formatAPIKeyTime(v)",
		"if(s.indexOf('0001-')===0",
		"if(d.getUTCFullYear()<1970)return '--'",
		"const last=html(formatAPIKeyTime(k.last_used_at))",
	}
	for _, check := range checks {
		if !strings.Contains(dashboardBundle, check) {
			t.Fatalf("formatAPIKeyTime missing invariant %q", check)
		}
	}
	// 回归：不得对任意非空字符串直接 new Date 后 toLocaleString（会把 0001 显示成 1/1/1）。
	if strings.Contains(dashboardBundle, "function formatAPIKeyTime(v){if(!v)return '--';const d=new Date(v);return Number.isNaN(d.getTime())?String(v):d.toLocaleString()}") {
		t.Fatal("formatAPIKeyTime still uses legacy form that renders Go zero time as year 1")
	}
}

// TestDashboardProxyNameColumnIsNoteOnly 名称/备注列只显示用户备注；
// 无备注必须留空，禁止回退 address（尤其 127.0.0.1 mixed 端口）。
func TestDashboardProxyNameColumnIsNoteOnly(t *testing.T) {
	if !strings.Contains(dashboardBundle, "function nodeLabel(p){return String((p&&p.note)||'').trim()}") {
		t.Fatal("nodeLabel must read only note")
	}
	if !strings.Contains(dashboardBundle, "const noteText=nodeLabel(p);const label=noteText?html(noteText):''") {
		t.Fatal("proxyRowHTML must use note-only label without address fallback")
	}
	if strings.Contains(dashboardBundle, "const labelText=noteText||addrText") {
		t.Fatal("proxyRowHTML must not fall back name column to address")
	}
	if strings.Contains(dashboardBundle, "labelText=noteText||") {
		t.Fatal("proxyRowHTML must not compose name label from non-note fields")
	}
}

// TestDashboardProxyActionsUnifiedAcrossSources：
// 手工与订阅节点共享测试/复制/停用；手工额外「管理」入口（地域/备注/删除），不再两套按钮列。
func TestDashboardProxyActionsUnifiedAcrossSources(t *testing.T) {
	checks := []string{
		"const testBtn=",
		"let copyBtn",
		"let toggleBtn",
		"isGatewayNode(p)&&st!=='ok'",
		"function gatewayCopyBlockedReason(p)",
		"if(blocked){showToast(blocked);return}",
		// 统一基础操作：测试 + 复制 + 停用/启用。
		"const baseActions=testBtn+' '+copyBtn+' '+toggleBtn",
		// 管理入口对所有来源开放（订阅节点与手工节点都可用），走统一应用内弹窗。
		"const manageBtn='<button class=\"mini\" onclick=\"manageNode('+id+')\">'+html(t('btn_manage'))+'</button>'",
		"const actions=baseActions+' '+manageBtn",
		"function manageNode(",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboardHTML missing unified actions invariant %q", check)
			}
		})
	}
	// 旧的手工专属「地域 备注 测试 复制 停用 删除」长串不得残留为唯一 actions 分支。
	if strings.Contains(dashboardBundle, "const actions=manual?('<button class=\"mini\" onclick=\"editManualRegion(") {
		t.Fatal("dashboardHTML still uses divergent manual action column")
	}
}

// TestDashboardHasSettingsEntry：侧栏运维分组设置入口；设置为独立页面（非模态）。
func TestDashboardHasSettingsEntry(t *testing.T) {
	checks := []string{
		`id="page-settings"`,
		`id="settings-modal"`,
		`data-tab="settings"`,
		`title="设置"`,
		`switchTab('settings')`,
		// 主题切换在顶栏。
		`id="theme-toggle"`,
		`id="pageTitle"`,
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboard missing settings entry invariant %q", check)
			}
		})
	}
}

// TestDashboardAIReachabilityColumnAndBadges 验证 AI 解锁列沿用已发布的短标签样式，
// 并用绿色、红色、淡灰色分别表达畅通、阻断、未探测。
func TestDashboardAIReachabilityColumnAndBadges(t *testing.T) {
	checks := []string{
		`AI 解锁：ChatGPT / Claude / Grok / Gemini（绿色畅通 / 红色阻断 / 淡灰未探测）`,
		`data-i18n="th_ai">AI 解锁</span>`,
		// 两处 colspan 为 14（加载中 + 无匹配节点，含勾选列）。
		`class="empty" data-i18n="loading">加载中</td>`,
		"<td colspan=\"14\" class=\"empty\">'+html(t('no_match'))+'</td>",
		// 新增 aiBadges 函数。
		"function aiBadges(",
		// 行渲染引用 ai_reachability 字段。
		"aiBadges(p.ai_reachability)",
		"const defs=[['openai','GPT','ChatGPT'],['claude','Cld','Claude'],['grok','Grk','Grok'],['gemini','Gem','Gemini']]",
		"const glyph=state==='unlocked'?'✓':(state==='blocked'?'✗':'–')",
		`'<span class="ai-marks">'`,
		"<span class=\"ai-mark '+cls+'\"",
		`.ai-mark.ok{color:var(--ok)`,
		`.ai-mark.bad{color:var(--danger)`,
		`--ai-unprobed:#6f7a96`,
		`--ai-unprobed:#9aa4ba`,
		`.ai-mark.na{color:var(--ai-unprobed)`,
		`.filter-toggle{display:inline-flex;align-items:center;gap:5px;padding:6px 10px;border-radius:999px`,
		`.filter-toggle[data-sel^="ai-"][data-state="unk"]{border-color:color-mix(in srgb,var(--ai-unprobed) 50%,var(--line));color:var(--ai-unprobed)`,
		"function filterCycleLabel(v)",
		"return t('filter_all')",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboardHTML missing ai-reachability invariant %q", check)
			}
		})
	}

	// 回归防护：旧的 12/13 列 colspan 不应再残留（现为 14，含勾选列）。
	for _, unsafe := range []string{
		"<td colspan=\"12\" class=\"empty\">加载中</td>",
		"<td colspan=\"12\" class=\"empty\">没有匹配节点</td>",
		"<td colspan=\"13\" class=\"empty\">加载中</td>",
		"<td colspan=\"13\" class=\"empty\">没有匹配节点</td>",
	} {
		if strings.Contains(dashboardBundle, unsafe) {
			t.Fatalf("dashboardHTML still has stale colspan %q (should be 14 with selection column)", unsafe)
		}
	}
	if strings.Contains(dashboardBundle, "function aiIconSVG(") {
		t.Fatal("AI 解锁样式偏离已发布的短标签合同")
	}
}

func TestDashboardAdvancedFilterControls(t *testing.T) {
	checks := []string{
		`id="protocol-filter"`,
		`id="region-filter"`,
		`id="status-filter"`,
		`id="source-filter"`,
		`id="purity-filter"`,
		`value="clean" data-i18n="filter_purity_clean">低风险`,
		`value="caution" data-i18n="filter_purity_caution">中风险`,
		`value="risky" data-i18n="filter_purity_risky">高风险`,
		`<option value="unprobed" data-i18n="filter_unprobed">未探测`,
		`id="cf-filter"`,
		`id="ai-openai-filter"`,
		`id="ai-claude-filter"`,
		`id="ai-grok-filter"`,
		`id="ai-gemini-filter"`,
		`id="latency-min"`,
		`id="latency-max"`,
		`id="keyword-filter"`,
		"function purityStateOf(",
	}
	for _, check := range checks {
		if !strings.Contains(dashboardBundle, check) {
			t.Fatalf("dashboard missing advanced-filter contract %q", check)
		}
	}
}

func TestDashboardMobileFilterControlsKeepIntrinsicHitTargets(t *testing.T) {
	const mobileFilterRule = "@media(max-width:720px){.toolbar.filters > .filter-toggle{flex:0 0 auto;min-width:max-content"
	if !strings.Contains(dashboardBundle, mobileFilterRule) {
		t.Fatalf("dashboard 缺少移动端筛选按钮稳定命中区域合同 %q", mobileFilterRule)
	}
}

// TestDashboardSubscriptionCustomHeaders 验证订阅弹窗含自定义请求头输入框，
// 且 addSubscription() 读取该输入框并随 payload 以 headers 字段发送。
func TestDashboardSubscriptionCustomHeaders(t *testing.T) {
	checks := []string{
		// 弹窗新增 headers 输入框（textarea），带 JSON 示例 placeholder。
		`<textarea id="sub-headers" placeholder="{&#34;User-Agent&#34;:&#34;clash&#34;}"></textarea>`,
		// addSubscription 读取输入框值。
		"headers:document.getElementById('sub-headers').value.trim()",
		// 提交后清空该输入框。
		"document.getElementById('sub-headers').value=''",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboardHTML missing subscription custom-headers invariant %q", check)
			}
		})
	}
}

func TestDashboardManualImportCopyAllowsAnyAnnotationPosition(t *testing.T) {
	checks := []string{
		"代理列表（每行一条 socks5/http/https URL，支持前缀/行内/行尾说明）",
		"prefix socks5://1.2.3.4:1080 suffix",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboardHTML missing manual import copy %q", check)
			}
		})
	}

	if strings.Contains(dashboardBundle, "行尾注释自动忽略") {
		t.Fatal("dashboardHTML still says manual import only ignores trailing comments")
	}
}

func TestDashboardLogoutUsesPostFlow(t *testing.T) {
	checks := []string{
		`onclick="logout()"`,
		"data-i18n-title=\"logout\"",
		"async function logout(){return runAsync(t('err_logout')",
		"fetch('/logout',{method:'POST'})",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboardHTML missing POST logout invariant %q", check)
			}
		})
	}

	if strings.Contains(dashboardBundle, `href="/logout"`) {
		t.Fatal("dashboardHTML still exposes logout as a GET link")
	}
}

// TestDashboardAPIKeyManagementUI 验证任务7：API Key 管理 UI（列表/创建/吊销/删除）。
// 列表展示名称、创建时间、末次使用、状态；创建后一次性明文 +「仅显示一次」；
// 吊销/删除走应用内 showConfirm + i18n；绝不在列表渲染 hash 字段。
func TestDashboardAPIKeyManagementUI(t *testing.T) {
	checks := []string{
		// API 路由调用。
		"/api/apikeys",
		"/api/apikey/create",
		"/api/apikey/revoke",
		"/api/apikey/delete",
		// 列表字段文案。
		"名称",
		"创建时间",
		"末次使用",
		"状态",
		// 一次性明文提示。
		"仅显示一次",
		// 吊销/删除须应用内确认并走字典。
		"showConfirm(t('confirm_revoke_key'))",
		"showConfirm(t('confirm_delete_key'))",
		"confirm_revoke_key:",
		"confirm_delete_key:",
		// 关键函数。
		"function loadAPIKeys(",
		"function createAPIKey(",
		"function revokeAPIKey(",
		"function deleteAPIKey(",
		// 打开设置时加载 keys。
		"loadAPIKeys",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboardHTML missing API Key management invariant %q", check)
			}
		})
	}

	// 负向：列表渲染不得展示 hash / 历史明文。
	for _, unsafe := range []string{
		"k.hash",
		"key_hash",
		"key.hash",
	} {
		t.Run("reject "+unsafe, func(t *testing.T) {
			if strings.Contains(dashboardBundle, unsafe) {
				t.Fatalf("dashboardHTML must not render hash field %q", unsafe)
			}
		})
	}

	// revoke/delete 必须各自带确认动作。
	if !strings.Contains(dashboardBundle, "revoke") || !strings.Contains(dashboardBundle, "delete") {
		t.Fatal("dashboardHTML missing revoke/delete API key actions")
	}
	if strings.Contains(dashboardBundle, "confirm('吊销该 API Key") || strings.Contains(dashboardBundle, "confirm('删除该 API Key") {
		t.Fatal("dashboardHTML still uses browser confirm for API key revoke/delete")
	}
}

// TestDashboardOpenAPITab 验证任务9：开放 API 功能页/tab。
// 含 data-tab=api、page-api、端点列表、鉴权说明、限流、curl 示例、连接模式、链到 Key 管理。
func TestDashboardOpenAPITab(t *testing.T) {
	checks := []string{
		`data-tab="api"`,
		`id="page-api"`,
		"/api/v1/nodes",
		"/api/v1/occupancy",
		"/api/v1/ping",
		"Authorization: Bearer",
		"X-API-Key",
		"60/min",
		"curl",
		"direct",
		"gateway",
		`data-i18n="nav_api"`,
		`data-i18n="card_openapi"`,
		"开放 API 说明",
		"Open API",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboardHTML missing Open API tab invariant %q", check)
			}
		})
	}
}

// TestDashboardOrbitThemeTokens 锁定主题结构，不强制切换到另一套具体配色。
func TestDashboardOrbitThemeTokens(t *testing.T) {
	checks := []string{
		"#3b8dff",
		"#2fbf87",
		"--q-s:",
		"--q-a:",
		"--q-b:",
		"--q-c:",
		`[data-theme="space"]`,
		`[data-theme="day"]`,
		"--space-0:",
		"--bg-canvas:",
		"['openai','GPT','ChatGPT']",
		"['claude','Cld','Claude']",
		"['gemini','Gem','Gemini']",
		"['grok','Grk','Grok']",
		`<span class="tx">Cloudflare</span>`,
		"function normalizeTheme(",
		"localStorage.getItem('gg-theme')||'space'",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboard bundle missing Orbit theme invariant %q", check)
			}
		})
	}
	// 禁止蓝紫/teal 违规渐变残留
	for _, bad := range []string{
		"#6d5cf7",
		"--signal:#0a9fbf",
		"--signal:#22d3ee",
		"['openai','GPT','OpenAI']",
	} {
		t.Run("reject "+bad, func(t *testing.T) {
			if strings.Contains(dashboardBundle, bad) {
				t.Fatalf("dashboard bundle still has non-Orbit token/label %q", bad)
			}
		})
	}
}

// TestDashboardOrbitSessionBeamsMatchQuality 会话连线按地区+品质匹配卫星，
// 禁止仅按地区把 S/A/B/C 全档点亮。D 档不进轨道是产品预期（byQ 仅 s/a/b/c）。
// 会话品质空/D 时须能回退到该地区现有轨道，避免 US 会话完全无连线。
func TestDashboardOrbitSessionBeamsMatchQuality(t *testing.T) {
	checks := []string{
		"function orbitSessionQualityTrack(",
		"function orbitSessionBeamKey(",
		"b.k=sessCount[b.cc+'|'+b.q]||0",
		"const byQ={s:[],a:[],b:[],c:[]}",
		"regionTracks",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboard missing orbit session-quality beam invariant %q", check)
			}
		})
	}
	// 旧逻辑：只按地区计数再赋给该地区所有品质桶。
	if strings.Contains(dashboardBundle, "b.k=sessCount[b.cc]||0") {
		t.Fatal("dashboard still assigns session beams by region only (ignores quality track)")
	}
	// D 档不得进入 byQ（轨道不展示 D 是正确产品行为）。
	if strings.Contains(dashboardBundle, "const byQ={s:[],a:[],b:[],c:[],d:[]}") {
		t.Fatal("dashboard must not place D-grade satellites on orbit tracks")
	}
}

// TestDashboardSessionRegionReqNoExtraDash DSL 地域请求展示为 region-ca，不得出现 -region-ca。
func TestDashboardSessionRegionReqNoExtraDash(t *testing.T) {
	if !strings.Contains(dashboardBundle, "return 'region-'+r") {
		t.Fatal("sessionRegionReq must render region-xx without leading dash")
	}
	if strings.Contains(dashboardBundle, "return '-region-'+r") {
		t.Fatal("sessionRegionReq still has extra leading dash before region-")
	}
}

// TestDashboardRegionDistributionRichPanel 地域分布：倒序、中文国家名列、TopN 不足则全显、查看全部页。
func TestDashboardRegionDistributionRichPanel(t *testing.T) {
	checks := []string{
		"function buildRegionStats(",
		"function regionDisplayName(",
		"function regionPanelHTML(",
		"const REGION_OVERVIEW_LIMIT=10",
		"国家/地区",
		`id="page-regions"`,
		`id="region-page-list"`,
		`switchTab('regions')`,
		"REGION_ZH",
		"ca:'加拿大'",
		"us:'美国'",
		"nl:'荷兰'",
		// 波罗的海等高频码不得回退显示裸 ISO 代码。
		"lv:'拉脱维亚'",
		"ee:'爱沙尼亚'",
		"lt:'立陶宛'",
		"sk:'斯洛伐克'",
		"hr:'克罗地亚'",
		"rs:'塞尔维亚'",
		"margin-top:40px",
		"平均延迟 ",
		"品质结构 · 平均延迟 · 会话",
		// 代码|国家紧邻：国家列用 max-content，结构列吃剩余宽度
		"grid-template-columns:44px max-content minmax(0,1fr) 72px",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) && !strings.Contains(dashboardHTML, check) {
				t.Fatalf("missing region distribution invariant %q", check)
			}
		})
	}
	if strings.Contains(dashboardBundle, "均延 ") {
		t.Fatal("region panel still uses nonstandard label 均延; want 平均延迟")
	}
}

// TestDashboardRegionZHCoversCommonExitCodes 锁定代理节点池常见出口码的中文名，
// 避免仅靠用户报告才补 LV/EE 这类缺口。
func TestDashboardRegionZHCoversCommonExitCodes(t *testing.T) {
	start := strings.Index(dashboardBundle, "const REGION_ZH={")
	if start < 0 {
		t.Fatal("REGION_ZH map missing from dashboard bundle")
	}
	end := strings.Index(dashboardBundle[start:], "};")
	if end < 0 {
		t.Fatal("REGION_ZH map is not closed")
	}
	body := dashboardBundle[start : start+end]

	// 用户已报 / 欧洲节点池高频且原先缺失的码。
	required := map[string]string{
		"lv": "拉脱维亚",
		"ee": "爱沙尼亚",
		"lt": "立陶宛",
		"sk": "斯洛伐克",
		"si": "斯洛文尼亚",
		"hr": "克罗地亚",
		"rs": "塞尔维亚",
		"ba": "波黑",
		"mk": "北马其顿",
		"al": "阿尔巴尼亚",
		"md": "摩尔多瓦",
		"by": "白俄罗斯",
		"is": "冰岛",
		"mt": "马耳他",
		"cy": "塞浦路斯",
		"ge": "格鲁吉亚",
		"am": "亚美尼亚",
		"az": "阿塞拜疆",
		"uz": "乌兹别克斯坦",
		"lk": "斯里兰卡",
		"kh": "柬埔寨",
		"mm": "缅甸",
		"ma": "摩洛哥",
		"tn": "突尼斯",
		"gh": "加纳",
		"pa": "巴拿马",
		"cr": "哥斯达黎加",
		"do": "多米尼加",
		"uy": "乌拉圭",
	}
	for code, name := range required {
		token := code + ":'" + name + "'"
		if !strings.Contains(body, token) {
			t.Fatalf("REGION_ZH missing %s → %s", code, name)
		}
	}

	// 未知码仍须回退大写 ISO，避免把坏数据硬编码成“未知国家名”。
	if !strings.Contains(dashboardBundle, "return REGION_ZH[c]||c.toUpperCase()") {
		t.Fatal("regionDisplayName must fall back to uppercase ISO for unknown codes")
	}
	if !strings.Contains(dashboardBundle, "if(!c||c==='unknown')return t('session_unknown')") {
		t.Fatal("regionDisplayName must map empty/unknown through the active language dictionary")
	}
}

// TestDashboardOverviewRegionConnectionSwap 总览：如何连接放右侧（矮），地域分布放下方，
// 避免地域列表过长把星系卡片撑高。
func TestDashboardOverviewRegionConnectionSwap(t *testing.T) {
	html := dashboardHTML
	conn := strings.Index(html, `data-i18n="card_connect"`)
	if conn < 0 {
		conn = strings.Index(html, `如何连接`)
	}
	region := strings.Index(html, `data-i18n="card_regions"`)
	if region < 0 {
		region = strings.Index(html, `地域分布`)
	}
	orbit := strings.Index(html, `id="orbit-stage"`)
	if conn < 0 || region < 0 || orbit < 0 {
		t.Fatalf("missing overview sections conn=%d region=%d orbit=%d", conn, region, orbit)
	}
	// 如何连接应在 overview-side（orbit 之后、地域分布之前或至少不在把侧栏撑爆的长列表位）。
	side := strings.Index(html, `class="overview-side"`)
	if side < 0 || conn < side {
		t.Fatal("如何连接 should be inside overview-side")
	}
	if region < conn {
		t.Fatal("地域分布 should be below 如何连接 (swap), not above it in the side column")
	}
}

// TestDashboardLangTogglePresent 右上角语言切换：中/英字典 + localStorage + 顶栏按钮。
func TestDashboardLangTogglePresent(t *testing.T) {
	checks := []string{
		`id="lang-toggle"`,
		`onclick="toggleLang()"`,
		"function applyLang(lang)",
		"function toggleLang()",
		"function t(key)",
		"localStorage.setItem('gg-lang'",
		"localStorage.getItem('gg-lang')",
		"nav_overview:'Overview'",
		"nav_overview:'总览'",
		"metric_upstreams:'Upstreams'",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) && !strings.Contains(dashboardHTML, check) {
				t.Fatalf("missing lang toggle invariant %q", check)
			}
		})
	}
}

// TestDashboardSessionCardsMatchDesignFields 会话卡对齐设计：完整详情字段、出口节点优先 exit_ip、
// 刷新保留展开状态（避免全部展开后定时刷新自动折叠）。
func TestDashboardSessionCardsMatchDesignFields(t *testing.T) {
	checks := []string{
		"function sessionCardHTML(",
		"function sessionExitNode(",
		"function sessionIsOpen(",
		"let sessionOpenIDs={}",
		"绑定节点 Proxy ID",
		"选路地域",
		"出口地点",
		"出口检查时间",
		"解锁过滤",
		"最近活跃",
		"节点冷却",
		"节点占用",
		"路由标签",
		"sessionExpandAll",
		"data-sid=",
		// 出口节点不得再把地域徽章拼进地址。
		"sessionExitNode(s)",
		// 全部折叠不得进入 sticky false（否则刷新后手动展开会被压回）。
		"if(open){sessionExpandAll=true}else{sessionExpandAll=null;sessionOpenIDs={}}",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboard missing session-card design invariant %q", check)
			}
		})
	}
	for _, bad := range []string{
		"'+regionBadge+masked+'",
		// 旧 4 字段详情网格（仅会话ID/地域/节点/TTL）不得作为唯一详情。
	} {
		t.Run("reject "+bad, func(t *testing.T) {
			if strings.Contains(dashboardBundle, bad) {
				t.Fatalf("dashboard still has bad session display fragment %q", bad)
			}
		})
	}
}

// TestDashboardProxyListPagination 节点清单分页：默认每页 20，可选 20/50/100，筛选重置到第 1 页。
func TestDashboardProxyListPagination(t *testing.T) {
	checks := []string{
		`id="proxy-pager"`,
		`id="proxy-page-size"`,
		`id="proxy-page-prev"`,
		`id="proxy-page-next"`,
		`id="proxy-page-info"`,
		`<option value="20" selected>20</option>`,
		`<option value="50">50</option>`,
		`<option value="100">100</option>`,
		"let proxyPage=1;let proxyPageSize=20",
		"function proxyTotalPages(",
		"function proxyPageSlice(",
		"function renderProxyPage(",
		"function renderProxyPager(",
		"function proxyPagePrev(",
		"function proxyPageNext(",
		"function proxyPageSizeChange(",
		"function renderProxies(keepPage){if(!keepPage)proxyPage=1;",
		"proxyPageSize=(n===50||n===100)?n:20",
		// 数据轮询 loadProxies 必须 keepPage=true，避免把用户从第 N 页踢回第 1 页。
		"renderProxies(true)",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboard missing proxy pagination invariant %q", check)
			}
		})
	}
	// 旧无限滚动分批不得残留。
	for _, bad := range []string{
		"const PROXY_BATCH=80",
		"function renderProxyBatch(",
		"function onProxyScroll(",
		"window.addEventListener('scroll',onProxyScroll",
		// 错误：无 keepPage 的 loadProxies 会在 10s 刷新时重置分页。
		"renderRegionFilter();renderProxies();renderRegions()",
	} {
		t.Run("reject "+bad, func(t *testing.T) {
			if strings.Contains(dashboardBundle, bad) {
				t.Fatalf("dashboard still has infinite-scroll batching %q", bad)
			}
		})
	}
}
