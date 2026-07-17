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
		"<th>ip-api 标记</th>",
		// 两处 colspan 为 14（加载中 + 无匹配节点）：勾选列 + 星标 + CF + AI。
		"<td colspan=\"14\" class=\"empty\">加载中</td>",
		"<td colspan=\"14\" class=\"empty\">没有匹配节点</td>",
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
		`<span class="th-ico" title="Cloudflare`,
		`<span class="tx">Cloudflare</span>`,
		// 两处 colspan 为 14（含多选勾选列）。
		"<td colspan=\"14\" class=\"empty\">加载中</td>",
		"<td colspan=\"14\" class=\"empty\">没有匹配节点</td>",
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
		// 取消星标须 confirm 确认。
		"if(!confirm('取消该节点星标？'))return",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
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
		"function hasLastCheck(p){",
		"function nodeState(p){if(isUserPaused(p)||p.status==='paused')return 'paused';if(isAvailable(p))return 'ok';if(Number(p.fail_count||0)>=3)return 'failed';if(p.status==='disabled')return hasLastCheck(p)?'failed':'pending';return 'pending'}",
		// BUG-51: allRegions/地域分布均经 isAvailable 过滤，因此不再计入 user_paused 节点。
		"allRegions=Array.from(new Set(allProxies.filter(p=>isAvailable(p)&&isKnownRegion(p)).map(regionOf))).sort()",
		"allProxies.filter(p=>isAvailable(p)&&isKnownRegion(p)).forEach",
		"个可用节点",
		"暂无可用地域数据",
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
		"const toggleBtn=(st==='paused')?",
		",true)\">启用</button>'",
		",false)\">停用</button>'",
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
		"' 暂停 / '",
		"html(disabledCount)+' 不可用",
		"const badge=paused?'<span class=\"badge warn\">已暂停</span>'",
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
		"no_tunnel_nodes:'无需运行'",
		"partial:'部分就绪'",
		"failed:'启动失败'",
		"<span class=\"k\">状态原因</span>",
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
		"manageManualNode('+id+',decodeURIComponent",
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
		"const region=String(s.region||'').trim().toLowerCase()",
		"const regionBadge=region&&region!=='unknown'?'<span class=\"badge ok\">'+html(region).toUpperCase()+'</span> ':'<span class=\"badge gray\">未知</span> '",
		"'+regionBadge+masked+'",
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

// TestDashboardNodeOrbit 验证总览节点分布图：按可用地域+延迟档聚合，session 画连线，rAF 动画。
func TestDashboardNodeOrbit(t *testing.T) {
	checks := []string{
		"<h3>节点分布</h3>",
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
		"orbitSessions=Array.isArray(sessions)?sessions:[];renderOrbitSystem()",
		"S ≤200ms",
		"会话连线（越粗绑定越多）",
		"function updateSolarWind(",
		"function updateGravLens(",
		"function spawnWindStreams(",
		`id="orbit-wind"`,
		`id="orbit-lens"`,
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

// TestDashboardCopyProxyCredBuildsFullURL 验证 copyProxyCred：
// - 网关节点：协议://用户名DSL:密码@网关入口（密码可为空占位 PASSWORD）
// - 直连节点（手工 HTTP/SOCKS 等非 tunnel）：协议://节点自身 IP:端口，绝不拼网关密码
// mixed 网关节点 confirm 选 socks5/http；成功 toast 不回显含真实密码的完整 URL。
func TestDashboardCopyProxyCredBuildsFullURL(t *testing.T) {
	checks := []string{
		// 直连 vs 网关分支：dual_protocol 或回环本地地址才走网关 DSL。
		"function isGatewayNode(p){",
		"function isDirectNode(p){return !isGatewayNode(p)}",
		// 直连复制：有 username 则拼 scheme://user:pass@addr，否则 scheme://addr（无 userinfo）。
		"if(isDirectNode(p)){",
		"const u=String(p.username||'')",
		"const url=u?(scheme+'://'+encodeProxyUserInfo(u)+':'+encodeProxyUserInfo(String(p.password||''))+'@'+addr):(scheme+'://'+addr)",
		"navigator.clipboard.writeText(url).then(()=>showToast('已复制直连地址'))",
		// 网关复制：仍用 DSL + 密码（空则 PASSWORD 占位）。
		"const rawPass=(configCache&&configCache.proxy_auth_password)?configCache.proxy_auth_password:''",
		"const pass=rawPass||'PASSWORD'",
		"const url=scheme+'://'+encodeProxyUserInfo(user)+':'+encodeProxyUserInfo(pass)+'@'+host",
		"function encodeProxyUserInfo(value){return encodeURIComponent(String(value||'')).replace(/[!'()*]/g,c=>'%'+c.charCodeAt(0).toString(16).toUpperCase())}",
		"const okMsg=rawPass?'已复制':'已复制，请将 PASSWORD 替换为真实密码'",
		"navigator.clipboard.writeText(url).then(()=>showToast(okMsg))",
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
}

// TestDashboardProxyActionsUnifiedAcrossSources：
// 手工与订阅节点共享测试/复制/停用；手工额外「管理」入口（地域/备注/删除），不再两套按钮列。
func TestDashboardProxyActionsUnifiedAcrossSources(t *testing.T) {
	checks := []string{
		"const testBtn=",
		"const copyBtn=",
		"const toggleBtn=",
		// 统一基础操作：测试 + 复制 + 停用/启用。
		"const baseActions=testBtn+' '+copyBtn+' '+toggleBtn",
		// 手工附加管理入口，而不是单独一整套不同按钮。
		"const manageBtn=manual?('<button class=\"mini\" onclick=\"manageManualNode('+id+',decodeURIComponent(\\''+addr+'\\'))\">管理</button>'):''",
		"const actions=baseActions+(manageBtn?(' '+manageBtn):'')",
		"function manageManualNode(",
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

// TestDashboardAIReachabilityColumnAndBadges 验证 AI 解锁列（openai/claude/grok/gemini）：
// 表头 AI 解锁；colspan 14；aiBadges 渲染正规短标签（ChatGPT/Claude/Grok/Gemini），绿畅通/红阻断/灰未知。
func TestDashboardAIReachabilityColumnAndBadges(t *testing.T) {
	checks := []string{
		// AI 解锁表头（Orbit 正规名）。
		`<span class="tx">AI 解锁</span>`,
		// 两处 colspan 为 14（加载中 + 无匹配节点，含勾选列）。
		"<td colspan=\"14\" class=\"empty\">加载中</td>",
		"<td colspan=\"14\" class=\"empty\">没有匹配节点</td>",
		// 新增 aiBadges 函数。
		"function aiBadges(",
		// 行渲染引用 ai_reachability 字段。
		"aiBadges(p.ai_reachability)",
		// AI 列 body 用 ✓/✗/– 标记取代坏掉的品牌 SVG。
		"const glyph=n===0?'✓':(n===1?'✗':'–')",
		// 四服务短标签（设计稿风格：GPT/Cld/Grk/Gem，title 保全称）。
		"['openai','GPT','ChatGPT']",
		// 四服务紧凑标记容器。
		`'<span class="ai-marks">'`,
		"<span class=\"ai-mark '+cls+'\"",
	}
	// 回归防护：不得再残留坏掉的 AI 品牌图标函数 aiIconSVG。
	if strings.Contains(dashboardBundle, "function aiIconSVG(") {
		t.Fatal("dashboardHTML still defines broken brand aiIconSVG; AI column should render ✓/✗/– marks")
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
		`onclick="logout()" title="退出登录" aria-label="退出登录"`,
		"async function logout(){return runAsync('退出失败'",
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
// 吊销/删除走 confirm；绝不在列表渲染 hash 字段。
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
		// 吊销/删除须 confirm。
		"confirm(",
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

	// revoke/delete 必须各自带 confirm（不只是页面别处的 confirm）。
	if !strings.Contains(dashboardBundle, "revoke") || !strings.Contains(dashboardBundle, "delete") {
		t.Fatal("dashboardHTML missing revoke/delete API key actions")
	}
	// 至少两处 confirm 用于吊销/删除文案（中文）。
	revokeConfirm := strings.Contains(dashboardBundle, "吊销") && strings.Contains(dashboardBundle, "confirm(")
	deleteConfirm := strings.Contains(dashboardBundle, "删除") && strings.Contains(dashboardBundle, "confirm(")
	if !revokeConfirm || !deleteConfirm {
		t.Fatal("dashboardHTML missing confirm for API key revoke/delete")
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
		`class="lbl t">API</span>`,
		"<h3>开放 API 说明</h3>",
	}
	for _, check := range checks {
		t.Run(check, func(t *testing.T) {
			if !strings.Contains(dashboardBundle, check) {
				t.Fatalf("dashboardHTML missing Open API tab invariant %q", check)
			}
		})
	}
}

// TestDashboardOrbitThemeTokens 锁定生产 CSS 主题 token 与 AI 短标签。
func TestDashboardOrbitThemeTokens(t *testing.T) {
	checks := []string{
		"#04060e",
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
