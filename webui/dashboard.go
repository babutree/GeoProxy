package webui

const dashboardHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>GeoProxy Gateway Admin</title>
<style>
:root{
  --bg:#f4f6fb; --panel:#fff; --ink:#18202f; --muted:#6b7488; --line:#e4e8f0;
  --soft:#eef2fb; --accent:#2f5bea; --accent-ink:#fff; --ok:#0f9f6e; --warn:#c98a12;
  --danger:#d64545; --gray:#8a93a6; --shadow:0 8px 30px rgba(24,38,68,.07); --radius:16px;
}
[data-theme="dark"]{
  --bg:#0d1320; --panel:#151d2e; --ink:#e7ecf5; --muted:#8b95ab; --line:#243046;
  --soft:#1a2438; --accent:#5b83ff; --accent-ink:#fff; --ok:#2bbd87; --warn:#e0a93b;
  --danger:#f0685f; --gray:#8b95ab; --shadow:0 8px 30px rgba(0,0,0,.32);
}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--ink);
  font-family:"Segoe UI","PingFang SC","Microsoft YaHei",Verdana,sans-serif;font-size:14px;line-height:1.55}
button,input,select,textarea{font:inherit;color:inherit}
a{color:var(--accent)}

.topbar{position:sticky;top:0;z-index:30;background:var(--panel);border-bottom:1px solid var(--line)}
.topbar-inner{max-width:1320px;margin:0 auto;padding:12px 22px;display:flex;align-items:center;
  justify-content:space-between;gap:14px;flex-wrap:wrap}
.brand{display:flex;align-items:center;gap:11px}
.mark{width:36px;height:36px;border-radius:10px;background:var(--accent);color:var(--accent-ink);
  display:grid;place-items:center;font-weight:800}
.eyebrow{font-size:10px;letter-spacing:.15em;text-transform:uppercase;color:var(--muted);font-weight:700}
.brand h1{margin:1px 0 0;font-size:18px}
.actions{display:flex;align-items:center;gap:8px;flex-wrap:wrap}
.btn{border:1px solid var(--line);background:var(--panel);border-radius:10px;padding:8px 14px;
  cursor:pointer;text-decoration:none;font-weight:600;white-space:nowrap;color:var(--ink)}
.btn:hover{border-color:var(--accent)}
.btn.primary{background:var(--accent);border-color:var(--accent);color:var(--accent-ink)}
.btn.danger{color:var(--danger)}

/* Tabs */
.tabs{max-width:1320px;margin:0 auto;padding:0 22px;display:flex;gap:4px;border-bottom:1px solid var(--line)}
.tab{padding:13px 18px;cursor:pointer;font-weight:600;color:var(--muted);border-bottom:2px solid transparent;
  background:none;border-top:none;border-left:none;border-right:none}
.tab:hover{color:var(--ink)}
.tab.active{color:var(--accent);border-bottom-color:var(--accent)}

.wrap{max-width:1320px;margin:0 auto;padding:22px}
.page{display:none}
.page.active{display:block}

/* Metrics */
.metrics{display:grid;grid-template-columns:repeat(auto-fit,minmax(150px,1fr));gap:13px;margin-bottom:20px}
.metric{background:var(--panel);border:1px solid var(--line);border-radius:var(--radius);padding:16px 18px;box-shadow:var(--shadow)}
.metric .label{font-size:12px;color:var(--muted);font-weight:600}
.metric .value{font-size:28px;font-weight:800;margin:3px 0 1px}
.metric .note{font-size:11px;color:var(--muted)}

.card{background:var(--panel);border:1px solid var(--line);border-radius:var(--radius);box-shadow:var(--shadow);margin-bottom:18px;overflow:hidden}
.card-head{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:15px 18px;border-bottom:1px solid var(--line)}
.card-head h3{margin:0;font-size:15px}
.card-head .tools{display:flex;gap:8px;flex-wrap:wrap;align-items:center}
.card-body{padding:16px 18px}
.two-col{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:18px;align-items:start}
@media(max-width:900px){.two-col{grid-template-columns:1fr}}

/* Connection guide */
.conn{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:14px}
.conn-item{background:var(--soft);border:1px solid var(--line);border-radius:12px;padding:14px}
.conn-item .k{font-size:11px;text-transform:uppercase;letter-spacing:.06em;color:var(--muted);font-weight:700}
.conn-item .v{font-family:"Consolas",monospace;font-size:15px;font-weight:700;margin-top:5px;word-break:break-all}
.conn-item .desc{font-size:12px;color:var(--muted);margin-top:4px}
.cmd{background:#0f1626;color:#cdd8ec;border-radius:10px;padding:12px 14px;font-family:"Consolas",monospace;
  font-size:12.5px;overflow-x:auto;white-space:pre;margin-top:12px}
.notice{display:flex;gap:8px;align-items:flex-start;background:var(--soft);border-left:3px solid var(--warn);
  border-radius:8px;padding:10px 12px;font-size:12.5px;color:var(--muted);margin-top:12px}

.guide-row{display:flex;flex-wrap:wrap;gap:5px;font-family:"Consolas",monospace;font-size:13px;
  background:var(--soft);border-radius:9px;padding:7px 11px;margin-bottom:6px}
.guide-row b{color:var(--accent)}.guide-row span{color:var(--muted)}
.hint{font-size:12px;color:var(--muted);margin-top:8px}

.toolbar{display:flex;flex-wrap:wrap;gap:8px;margin-bottom:14px}
.input{border:1px solid var(--line);border-radius:9px;padding:8px 11px;background:var(--panel);min-width:0}
.input:focus{outline:none;border-color:var(--accent)}
.grow{flex:1;min-width:150px}

.table-wrap{width:100%;overflow-x:auto}
table{width:100%;border-collapse:collapse;font-size:13px}
th,td{padding:9px 10px;text-align:left;border-bottom:1px solid var(--line);white-space:nowrap;vertical-align:middle}
th{font-size:11px;text-transform:uppercase;letter-spacing:.04em;color:var(--muted);font-weight:700}
tbody tr:last-child td{border-bottom:none}
tbody tr:hover{background:var(--soft)}
.mono{font-family:"Consolas",monospace}
.empty{text-align:center;color:var(--muted);padding:22px 0}

.badge{display:inline-block;padding:2px 9px;border-radius:999px;font-size:11px;font-weight:700;background:var(--soft);color:var(--muted)}
.badge.ok{background:rgba(15,159,110,.14);color:var(--ok)}
.badge.blue{background:rgba(47,91,234,.13);color:var(--accent)}
.badge.warn{background:rgba(201,138,18,.15);color:var(--warn)}
.badge.danger{background:rgba(214,69,69,.14);color:var(--danger)}
.badge.gray{background:var(--soft);color:var(--gray)}

.mini{border:1px solid var(--line);background:var(--panel);border-radius:8px;padding:5px 10px;cursor:pointer;font-size:12px;font-weight:600;color:var(--ink)}
.mini:hover{border-color:var(--accent)}
.mini.danger{color:var(--danger)}

.region-row{display:flex;align-items:center;gap:12px;padding:6px 0}
.region-row strong{width:42px;font-size:13px}
.bar{flex:1;height:8px;background:var(--soft);border-radius:999px;overflow:hidden}
.bar span{display:block;height:100%;background:var(--accent);border-radius:999px}
.region-row .cnt{width:36px;text-align:right;color:var(--muted);font-size:13px}

.kv{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:7px 0;border-bottom:1px solid var(--line);font-size:13px}
.kv:last-child{border-bottom:none}
.kv .k{color:var(--muted)}.kv .v{font-weight:700}
.dot{display:inline-block;width:8px;height:8px;border-radius:50%;margin-right:7px;vertical-align:middle}
.dot.on{background:var(--ok)}.dot.off{background:var(--danger)}.dot.warn{background:var(--warn)}.dot.idle{background:var(--gray)}

.sub-item{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:12px 0;border-bottom:1px solid var(--line);flex-wrap:wrap}
.sub-item:last-child{border-bottom:none}
.sub-item .meta{min-width:0}.sub-item .meta strong{display:block}
.sub-item .meta .muted{font-size:12px;color:var(--muted)}
.mini-actions{display:flex;gap:6px;flex-wrap:wrap}

.session-list{display:grid;grid-template-columns:repeat(auto-fill,minmax(260px,1fr));gap:10px}
.session-card{border:1px solid var(--line);border-radius:11px;padding:11px 13px}
.session-card .top{display:flex;align-items:center;justify-content:space-between;gap:8px}
.session-card .sid{font-family:"Consolas",monospace;font-weight:700;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.session-card .ttl{font-size:12px;color:var(--ok);font-weight:700}
.session-card .node{font-family:"Consolas",monospace;font-size:12px;color:var(--muted);margin-top:5px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}

.logs{background:#0f1626;color:#c9d4e6;border-radius:11px;padding:12px;font-family:"Consolas",monospace;
  font-size:12px;line-height:1.5;max-height:420px;overflow:auto;white-space:pre-wrap;word-break:break-all}
.log-line{padding:1px 0}
.muted{color:var(--muted)}
.legend{display:flex;gap:14px;flex-wrap:wrap;font-size:12px;color:var(--muted);margin-bottom:12px}
.legend span{display:flex;align-items:center;gap:5px}

.modal{position:fixed;inset:0;background:rgba(12,18,30,.5);display:none;align-items:flex-start;justify-content:center;padding:44px 16px;z-index:60;overflow:auto}
.modal.show{display:flex}
.dialog{background:var(--panel);border-radius:var(--radius);width:min(560px,100%);padding:24px;box-shadow:0 30px 80px rgba(10,16,30,.4)}
.dialog h3{margin:0 0 16px}
.form-grid{display:grid;grid-template-columns:1fr 1fr;gap:14px}
.field{display:flex;flex-direction:column;gap:5px}
.field.full{grid-column:1 / -1}
.field label{font-size:12px;color:var(--muted);font-weight:600}
.field input,.field select,.field textarea{border:1px solid var(--line);border-radius:9px;padding:9px 11px;background:var(--panel);width:100%}
.field textarea{min-height:120px;resize:vertical;font-family:"Consolas",monospace}
.field .fh{font-size:11px;color:var(--muted)}
.dialog-actions{display:flex;justify-content:flex-end;gap:10px;margin-top:20px}

.toast{position:fixed;left:50%;bottom:26px;transform:translateX(-50%) translateY(20px);background:var(--ink);
  color:var(--bg);padding:11px 20px;border-radius:999px;font-weight:600;opacity:0;pointer-events:none;transition:.25s;z-index:70}
.toast.show{opacity:1;transform:translateX(-50%) translateY(0)}
</style>
</head>
<body>
<header class="topbar">
  <div class="topbar-inner">
    <div class="brand"><div class="mark">GG</div>
      <div><div class="eyebrow">Authenticated Admin</div><h1>GeoProxy Gateway</h1></div>
    </div>
    <div class="actions">
      <button class="btn" id="theme-toggle" onclick="toggleTheme()">🌙 深色</button>
      <button class="btn" onclick="refreshAll()">刷新</button>
      <button class="btn" onclick="runAsync('打开设置失败',openSettings)">系统设置</button>
      <a class="btn danger" href="/logout">退出</a>
    </div>
  </div>
  <nav class="tabs">
    <button class="tab active" data-tab="overview" onclick="switchTab('overview')">总览</button>
    <button class="tab" data-tab="nodes" onclick="switchTab('nodes')">节点</button>
    <button class="tab" data-tab="subs" onclick="switchTab('subs')">订阅</button>
    <button class="tab" data-tab="sessions" onclick="switchTab('sessions')">会话</button>
    <button class="tab" data-tab="logs" onclick="switchTab('logs')">日志</button>
  </nav>
</header>
<main class="wrap">

  <!-- 总览 -->
  <section class="page active" id="page-overview">
    <div class="metrics">
      <div class="metric"><div class="label">上游节点</div><div class="value" id="stat-total">--</div><div class="note">可用 / 降级</div></div>
      <div class="metric"><div class="label">HTTP 可用</div><div class="value" id="stat-http">--</div><div class="note">入口节点</div></div>
      <div class="metric"><div class="label">SOCKS5 可用</div><div class="value" id="stat-socks5">--</div><div class="note">入口节点</div></div>
      <div class="metric"><div class="label">订阅可用</div><div class="value" id="stat-subscription">--</div><div class="note">订阅来源</div></div>
      <div class="metric"><div class="label">活跃会话</div><div class="value" id="stat-sessions">--</div><div class="note">当前绑定</div></div>
    </div>

    <div class="card">
      <div class="card-head"><h3>如何连接</h3><span class="muted">用网关端口 + 认证，不是直连节点</span></div>
      <div class="card-body">
        <div class="conn">
          <div class="conn-item"><div class="k">SOCKS5 代理</div><div class="v" id="conn-socks5">127.0.0.1:7801</div><div class="desc">raw TCP，HTTP/HTTPS 目标都可</div></div>
          <div class="conn-item"><div class="k">HTTP 代理</div><div class="v" id="conn-http">127.0.0.1:7802</div><div class="desc">HTTPS 目标走 CONNECT 隧道</div></div>
          <div class="conn-item"><div class="k">用户名（含路由 DSL）</div><div class="v" id="conn-user">acct</div><div class="desc">见下方 DSL 规则</div></div>
          <div class="conn-item"><div class="k">密码</div><div class="v" id="conn-pass">见首次启动日志 / 系统设置</div><div class="desc" id="conn-auth-state">代理认证状态</div></div>
        </div>
        <div class="cmd" id="conn-cmd">curl --socks5 acct:PASSWORD@127.0.0.1:7801 https://www.gstatic.com/generate_204</div>
        <div class="notice"><span>⚠️</span><span><b>节点清单里的“出口 IP”只是信息展示</b>，表示选中该节点后你的出口地址；<b>不能</b>用出口 IP + 节点账号直连。所有流量都通过上面的网关端口转发。</span></div>
      </div>
    </div>

    <div class="two-col">
      <div class="card">
        <div class="card-head"><h3>用户名 DSL</h3></div>
        <div class="card-body">
          <div id="dsl-examples">
            <div class="guide-row"><b>acct</b><span>-region-us</span></div>
            <div class="guide-row"><b>acct</b><span>-region-jp-session-app01</span></div>
            <div class="guide-row"><b>acct</b><span>-session-browser</span></div>
          </div>
          <div class="hint" id="dsl-hint">前缀 = 代理认证用户名；-region-XX 指定地域；-session-ID 保持出口黏连。</div>
        </div>
      </div>
      <div class="card">
        <div class="card-head"><h3>sing-box 引擎</h3><div class="tools"><button class="mini" onclick="runAsync('状态刷新失败',loadCustomStatus)">刷新</button></div></div>
        <div class="card-body"><div id="singbox-status"><div class="empty">加载中</div></div></div>
      </div>
    </div>

    <div class="card">
      <div class="card-head"><h3>地域分布</h3><span class="muted" id="region-total">--</span></div>
      <div class="card-body"><div id="region-list"><div class="empty">加载中</div></div></div>
    </div>
  </section>

  <!-- 节点 -->
  <section class="page" id="page-nodes">
    <div class="card">
      <div class="card-head">
        <h3>节点清单</h3>
        <div class="tools">
          <select class="input" id="protocol-filter" onchange="renderProxies()"><option value="">全部协议</option><option value="http">HTTP</option><option value="socks5">SOCKS5</option></select>
          <select class="input" id="region-filter" onchange="renderProxies()"><option value="">全部地域</option></select>
          <select class="input" id="status-filter" onchange="renderProxies()"><option value="">全部状态</option><option value="ok">可用</option><option value="paused">已停用</option><option value="failed">不可用</option><option value="pending">待验证</option></select>
        </div>
      </div>
      <div class="card-body">
        <div class="legend">
          <span><span class="badge ok">可用</span> 验证通过</span>
          <span><span class="badge gray">待验证</span> 尚未验证</span>
          <span><span class="badge warn">已停用</span> 你手动停用</span>
          <span><span class="badge danger">不可用</span> 验证失败</span>
        </div>
        <div class="toolbar">
          <input class="input grow" id="manual-link" placeholder="添加手工节点: http://host:port 或 socks5://host:port">
          <input class="input" id="manual-region" maxlength="2" placeholder="地域" style="width:70px">
          <input class="input" id="manual-note" placeholder="备注" style="width:130px">
          <button class="mini" onclick="addManualNode()">添加</button>
        </div>
        <div class="table-wrap">
          <table>
            <thead><tr><th>★</th><th>名称 / 备注</th><th>协议</th><th>地域</th><th>出口 IP<span class="muted"> (信息)</span></th><th>延迟</th><th>ipapi.is 滥用分<span class="muted"> /1.00</span></th><th>ip-api 标记</th><th>CF 拦截</th><th>来源</th><th>状态</th><th>操作</th></tr></thead>
            <tbody id="proxy-rows"><tr><td colspan="12" class="empty">加载中</td></tr></tbody>
          </table>
        </div>
      </div>
    </div>
  </section>

  <!-- 订阅 -->
  <section class="page" id="page-subs">
    <div class="card">
      <div class="card-head"><h3>订阅管理</h3><div class="tools"><button class="mini" onclick="refreshAllSubs()">刷新所有</button><button class="btn primary" onclick="openSubModal()">添加订阅</button></div></div>
      <div class="card-body"><div id="sub-list"><div class="empty">加载中</div></div></div>
    </div>
  </section>

  <!-- 会话 -->
  <section class="page" id="page-sessions">
    <div class="card">
      <div class="card-head"><h3>Session 监控</h3><div class="tools"><button class="mini" onclick="runAsync('会话刷新失败',loadSessions)">刷新</button></div></div>
      <div class="card-body"><div class="session-list" id="session-rows"><div class="empty">加载中</div></div></div>
    </div>
  </section>

  <!-- 日志 -->
  <section class="page" id="page-logs">
    <div class="card">
      <div class="card-head"><h3>运行日志</h3><div class="tools"><button class="mini" onclick="runAsync('日志刷新失败',loadLogs)">刷新</button></div></div>
      <div class="card-body"><div class="logs" id="logs-box"><div class="log-line">loading...</div></div></div>
    </div>
  </section>
</main>

<div class="modal" id="sub-modal"><div class="dialog"><h3>添加订阅</h3><div class="form-grid"><div class="field"><label>名称</label><input id="sub-name" placeholder="primary subscription"></div><div class="field"><label>刷新间隔（分钟）</label><input id="sub-refresh" type="number" value="60" min="10" step="10"></div><div class="field full"><label>订阅 URL</label><input id="sub-url" placeholder="https://example.com/sub"></div><div class="field full"><label>或粘贴配置文件内容</label><textarea id="sub-file-content" placeholder="Clash YAML / V2ray / Base64 / plain text"></textarea></div></div><div class="dialog-actions"><button class="btn" onclick="closeSubModal()">取消</button><button class="btn primary" onclick="addSubscription()">添加</button></div></div></div>
<div class="modal" id="settings-modal"><div class="dialog"><h3>系统设置</h3><div class="form-grid"><div class="field"><label>HTTP 端口</label><input id="cfg-http-port" readonly><span class="fh">只读，改端口需重新部署</span></div><div class="field"><label>SOCKS5 端口</label><input id="cfg-socks5-port" readonly><span class="fh">只读</span></div><div class="field"><label>WebUI 端口</label><input id="cfg-webui-port" readonly><span class="fh">只读</span></div><div class="field"><label>代理认证</label><select id="cfg-auth-enabled"><option value="false">关闭</option><option value="true">开启</option></select></div><div class="field"><label>代理认证用户名</label><input id="cfg-auth-username"></div><div class="field"><label>代理认证密码（留空不改）</label><input id="cfg-auth-password" type="password"></div><div class="field"><label>Session TTL（分钟）</label><input id="cfg-session-ttl" type="number" min="1"></div><div class="field"><label>默认地域</label><input id="cfg-default-region" maxlength="2" placeholder="空=全局"></div><div class="field"><label>健康检查间隔（分钟）</label><input id="cfg-health-interval" type="number" min="1"></div><div class="field"><label>最大重试次数</label><input id="cfg-max-retry" type="number" min="0"></div><div class="field full"><label>sing-box 路径</label><input id="cfg-singbox-path"></div><div class="field full"><label>允许国家（逗号分隔，优先）</label><input id="cfg-allowed-countries" placeholder="US,JP,SG"></div><div class="field full"><label>屏蔽国家（逗号分隔）</label><input id="cfg-blocked-countries" placeholder="CN,RU"></div></div><div class="dialog-actions"><button class="btn" onclick="closeSettings()">取消</button><button class="btn primary" onclick="saveConfig()">保存</button></div></div></div>
<div class="toast" id="toast"></div>
<script>
let allProxies=[];let allRegions=[];let configCache=null;let publicIP='';
function switchTab(name){document.querySelectorAll('.tab').forEach(t=>t.classList.toggle('active',t.dataset.tab===name));document.querySelectorAll('.page').forEach(p=>p.classList.toggle('active',p.id==='page-'+name))}
function showToast(msg){const el=document.getElementById('toast');el.textContent=msg;el.classList.add('show');setTimeout(()=>el.classList.remove('show'),2600)}
async function api(path, options){const res=await fetch(path, Object.assign({headers:{'Content-Type':'application/json'}}, options||{}));if(res.status===401){location.href='/login';return null}const text=await res.text();let data={};if(text){try{data=JSON.parse(text)}catch(err){if(!res.ok)throw new Error(res.statusText||('HTTP '+res.status));throw new Error('响应解析失败')}}if(!res.ok)throw new Error(data.error||res.statusText||('HTTP '+res.status));return data}
function safe(value){return value===undefined||value===null||value===''?'--':String(value)}
function html(value){return safe(value).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
function errorMessage(err){return err&&err.message?err.message:String(err||'操作失败')}
async function runAsync(label, fn){try{return await fn()}catch(err){showToast((label?label+'：':'')+errorMessage(err));return null}}
function refreshLater(){setTimeout(()=>runAsync('刷新失败',()=>Promise.all([loadSubscriptions(),loadStats(),loadProxies()])),4000)}
function maskAddress(address){if(!address)return '--';const parts=String(address).split(':');const host=parts[0]||address;if(host.length<=8)return host+(parts[1]?':'+parts[1]:'');return host.slice(0,4)+'...'+host.slice(-4)+(parts[1]?':'+parts[1]:'')}
function addressArg(address){return encodeURIComponent(String(address||'')).replace(/[!'()*]/g,c=>'%'+c.charCodeAt(0).toString(16).toUpperCase())}
function proxyIDArg(proxy){const id=Number(proxy&&proxy.id);return Number.isFinite(id)?String(id):'0'}
function regionOf(proxy){const region=String((proxy&&proxy.region)||'').trim().toLowerCase();return region||'unknown'}
function isKnownRegion(proxy){const region=regionOf(proxy);return region&&region!=='unknown'}
function isUserPaused(p){return !!(p&&(p.user_paused===true||Number(p.user_paused)===1))}
function isAvailable(proxy){return !isUserPaused(proxy)&&(proxy.status==='active'||proxy.status==='degraded')&&Number(proxy.fail_count||0)<3}
function stripColon(port){return String(port||'').replace(/^:/,'')}
async function refreshAll(){return runAsync('刷新失败',async()=>{await Promise.all([loadStats(),loadProxies(),loadSubscriptions(),loadConfig(),loadSessions(),loadLogs(),loadCustomStatus()]);loadPublicIP();showToast('数据已刷新')})}
async function loadCustomStatus(){const st=await api('/api/custom/status');if(!st)return;const box=document.getElementById('singbox-status');if(!box)return;const status=String(st.singbox_status||(st.singbox_running?'running':'stopped'));const reason=String(st.singbox_reason||status);const statusText={no_tunnel_nodes:'无需运行',running:'运行中',stopped:'已停止',partial:'部分就绪',failed:'启动失败'}[status]||status;const dotClass={no_tunnel_nodes:'idle',running:'on',stopped:'idle',partial:'warn',failed:'off'}[status]||'idle';const dot='<span class="dot '+dotClass+'"></span>';box.innerHTML='<div class="kv"><span>'+dot+'sing-box 引擎</span><span class="v">'+html(statusText)+'</span></div>'+'<div class="kv"><span class="k">状态原因</span><span class="v">'+html(reason)+'</span></div>'+'<div class="kv"><span class="k">转换节点</span><span class="v">'+html(safe(st.singbox_nodes))+'</span></div>'+'<div class="kv"><span class="k">端口就绪</span><span class="v">'+html(safe(st.singbox_ready_ports))+'/'+html(safe(st.singbox_total_ports))+'</span></div>'+'<div class="kv"><span class="k">订阅可用</span><span class="v">'+html(safe(st.subscription_count))+'</span></div>'+'<div class="kv"><span class="k">暂停/不可用节点</span><span class="v">'+html(safe(st.disabled_count))+'</span></div>'+'<div class="kv"><span class="k">订阅总数</span><span class="v">'+html(safe(st.subscription_total))+'</span></div>'}
function applyTheme(theme){document.documentElement.setAttribute('data-theme',theme);try{localStorage.setItem('gg-theme',theme)}catch(e){}const btn=document.getElementById('theme-toggle');if(btn)btn.textContent=theme==='dark'?'☀ 浅色':'🌙 深色'}
function toggleTheme(){const cur=document.documentElement.getAttribute('data-theme')==='dark'?'dark':'light';applyTheme(cur==='dark'?'light':'dark')}
(function(){let t='light';try{t=localStorage.getItem('gg-theme')||'light'}catch(e){}applyTheme(t)})();
async function loadStats(){const stats=await api('/api/stats');if(!stats)return;document.getElementById('stat-total').textContent=safe(stats.total);document.getElementById('stat-http').textContent=safe(stats.http);document.getElementById('stat-socks5').textContent=safe(stats.socks5);document.getElementById('stat-subscription').textContent=safe(stats.subscription_count);document.getElementById('stat-sessions').textContent=safe(stats.active_sessions)}
async function loadProxies(){const data=await api('/api/proxies');if(!data)return;allProxies=Array.isArray(data)?data:[];allRegions=Array.from(new Set(allProxies.filter(p=>isAvailable(p)&&isKnownRegion(p)).map(regionOf))).sort();renderRegionFilter();renderProxies();renderRegions()}
function renderRegionFilter(){const select=document.getElementById('region-filter');const current=select.value;select.innerHTML='<option value="">全部地域</option>'+allRegions.map(r=>'<option value="'+html(r)+'">'+html(r).toUpperCase()+'</option>').join('');select.value=allRegions.includes(current)?current:''}
function sourceLabel(p){if(p.source==='manual')return '手工';return p.subscription_name?p.subscription_name:'订阅';}
function nodeLabel(p){if(p.source==='manual')return maskAddress(p.address);if(p.note)return p.note;return p.subscription_name?p.subscription_name:'订阅节点';}
// 节点状态归类与后端可用统计保持一致: 主判据是 user_paused(存储层新口径, status 仍为 active),
// 其次 active/degraded 且 fail_count < 3 为可用。保留 status==='paused' 兜底以兼容任何未迁移的旧记录。
function nodeState(p){if(isUserPaused(p)||p.status==='paused')return 'paused';if(isAvailable(p))return 'ok';if(p.status==='disabled'||Number(p.fail_count||0)>=3)return 'failed';return 'pending'}
function stateBadge(st){switch(st){case 'ok':return '<span class="badge ok">可用</span>';case 'paused':return '<span class="badge warn">已停用</span>';case 'failed':return '<span class="badge danger">不可用</span>';default:return '<span class="badge gray">待验证</span>'}}
// abuserBadge: ipapi.is abuser_score<0 显示 "--"（未探测/查询失败）；否则显示 0.00-1.00 两位小数 + 颜色。
// 阈值：<0.10 绿(ok)、0.10-0.50 黄(warn)、>0.50 红(danger)。两源分开展示，不与 ip-api 聚合。
function abuserBadge(score){const n=Number(score);if(!Number.isFinite(n)||n<0)return '<span class="muted">--</span>';const cls=n<0.1?'ok':(n<=0.5?'warn':'danger');return '<span class="badge '+cls+'">'+html(n.toFixed(2))+'</span>'}
// ipapiFlagsBadges: ip-api 命中标记逗号串。proxy 红、hosting 黄、mobile 灰；seen=true 且无命中显"干净"绿；未探测显 "--"。
function ipapiFlagsBadges(flags,seen){const raw=String(flags||'').trim();if(raw===''){return seen?'<span class="badge ok">干净</span>':'<span class="muted">--</span>'}const cls={proxy:'danger',hosting:'warn',mobile:'gray'};return raw.split(',').map(f=>f.trim()).filter(Boolean).map(f=>'<span class="badge '+(cls[f]||'gray')+'">'+html(f)+'</span>').join(' ')}
// cfBadge: cf_blocked==1 显"拦截"红、==0 显"正常"绿、其它(-1/未探测)显 "--"。
function cfBadge(v){ v=Number(v); if(v===1)return '<span class="badge danger">拦截</span>'; if(v===0)return '<span class="badge ok">正常</span>'; return '<span class="muted">--</span>' }
// starBtn: 星标切换按钮，★ 已加星 / ☆ 未加星。
function starBtn(p){ const id=proxyIDArg(p); const on=!!(p.starred===true||Number(p.starred)===1); return '<button class="mini" onclick="toggleStar('+id+','+(on?'true':'false')+')" title="星标">'+(on?'★':'☆')+'</button>' }
// randSession: 随机 6 位字母数字，用于复制凭据的 session 段。
function randSession(){ const cs='abcdefghijklmnopqrstuvwxyz0123456789'; let s=''; for(let i=0;i<6;i++)s+=cs[Math.floor(Math.random()*cs.length)]; return s }
// copyProxyCred: 复制该节点代理用户名 <base>-region-<region>-session-<rand6>。
function copyProxyCred(id){ const p=allProxies.find(x=>Number(x.id)===Number(id)); if(!p)return; const base=(configCache&&configCache.proxy_auth_username)?configCache.proxy_auth_username:'acct'; const region=regionOf(p); const cred=base+'-region-'+region+'-session-'+randSession(); navigator.clipboard.writeText(cred).then(()=>showToast('已复制: '+cred)).catch(()=>showToast('复制失败')) }
// toggleStar: 加星直接生效；取消星标须 confirm() 确认。
async function toggleStar(id,on){ if(on){ if(!confirm('取消该节点星标？'))return } return runAsync('星标操作失败',async()=>{ await api('/api/proxy/star',{method:'POST',body:JSON.stringify({id,starred:!on})}); await loadProxies(); showToast(on?'已取消星标':'已加星标') }) }
function renderProxies(){const protocol=document.getElementById('protocol-filter').value;const region=document.getElementById('region-filter').value;const sf=document.getElementById('status-filter').value;let rows=allProxies.filter(p=>(!protocol||p.protocol===protocol)&&(!region||regionOf(p)===region));if(sf)rows=rows.filter(p=>nodeState(p)===sf);const order={ok:0,pending:1,paused:2,failed:3};rows.sort((a,b)=>{const fa=(nodeState(a)==='ok'&&(a.starred===true||Number(a.starred)===1))?1:0;const fb=(nodeState(b)==='ok'&&(b.starred===true||Number(b.starred)===1))?1:0;if(fa!==fb)return fb-fa;const sa=nodeState(a),sb=nodeState(b);if(order[sa]!==order[sb])return order[sa]-order[sb];return Number(a.latency||1e9)-Number(b.latency||1e9)});const body=document.getElementById('proxy-rows');if(rows.length===0){body.innerHTML='<tr><td colspan="12" class="empty">没有匹配节点</td></tr>';return}body.innerHTML=rows.map(p=>{const addr=addressArg(p.address);const id=proxyIDArg(p);const manual=p.source==='manual';const st=nodeState(p);const label=html(nodeLabel(p));const showRegion=isAvailable(p)&&isKnownRegion(p);const toggleBtn=(st==='paused')?'<button class="mini" onclick="toggleProxy('+id+',decodeURIComponent(\''+addr+'\'),true)">启用</button>':'<button class="mini" onclick="toggleProxy('+id+',decodeURIComponent(\''+addr+'\'),false)">停用</button>';const testBtn='<button class="mini" onclick="testProxy('+id+',decodeURIComponent(\''+addr+'\'))">测试</button>';const copyBtn='<button class="mini" onclick="copyProxyCred('+id+')">复制</button>';const actions=manual?('<button class="mini" onclick="editManualRegion('+id+',decodeURIComponent(\''+addr+'\'))">地域</button> <button class="mini" onclick="editManualNote('+id+',decodeURIComponent(\''+addr+'\'))">备注</button> '+testBtn+' '+copyBtn+' '+toggleBtn+' <button class="mini danger" onclick="deleteManualNode('+id+',decodeURIComponent(\''+addr+'\'))">删除</button>'):(testBtn+' '+copyBtn+' '+toggleBtn);const latencyText=Number(p.latency)>0?html(p.latency)+' ms':'--';return '<tr><td>'+starBtn(p)+'</td><td title="'+label+'">'+label+'</td><td><span class="badge blue">'+html(p.protocol).toUpperCase()+'</span></td><td>'+(showRegion?'<span class="badge ok">'+html(regionOf(p)).toUpperCase()+'</span>':'<span class="muted">--</span>')+'</td><td class="mono">'+html(p.exit_ip)+'</td><td>'+latencyText+'</td><td>'+abuserBadge(p.ipapiis_score)+'</td><td>'+ipapiFlagsBadges(p.ipapi_flags,!!p.ipapi_flags_seen)+'</td><td>'+cfBadge(p.cf_blocked)+'</td><td>'+html(sourceLabel(p))+'</td><td>'+stateBadge(st)+'</td><td>'+actions+'</td></tr>'}).join('')}
function renderRegions(){const counts={};allProxies.filter(p=>isAvailable(p)&&isKnownRegion(p)).forEach(p=>{const r=regionOf(p);counts[r]=(counts[r]||0)+1});const entries=Object.keys(counts).sort().map(region=>({region,count:counts[region]}));const total=entries.reduce((sum,item)=>sum+item.count,0);document.getElementById('region-total').textContent=total+' available nodes';const list=document.getElementById('region-list');if(entries.length===0){list.innerHTML='<div class="empty">暂无可用地域数据</div>';return}list.innerHTML=entries.map(item=>{const pct=total?Math.round(item.count*100/total):0;return '<div class="region-row"><strong>'+html(item.region).toUpperCase()+'</strong><div class="bar"><span style="width:'+pct+'%"></span></div><span class="cnt">'+html(item.count)+'</span></div>'}).join('')}
async function loadSessions(){const sessions=await api('/api/sessions');if(!sessions)return;const body=document.getElementById('session-rows');if(!Array.isArray(sessions)||sessions.length===0){body.innerHTML='<div class="empty">暂无活跃 session</div>';return}body.innerHTML=sessions.map(s=>{const masked=html(maskAddress(s.node));const region=String(s.region||'').trim().toLowerCase();const regionBadge=region&&region!=='unknown'?'<span class="badge ok">'+html(region).toUpperCase()+'</span> ':'<span class="badge gray">未知</span> ';return '<div class="session-card"><div class="top"><span class="sid" title="'+html(s.session_id)+'">'+html(s.session_id)+'</span><span class="ttl">'+html(formatTTL(s.remaining_ttl_seconds))+'</span></div><div class="node" title="'+masked+'">'+regionBadge+masked+'</div></div>'}).join('')}
function formatTTL(seconds){const value=Number(seconds)||0;const min=Math.floor(value/60);const sec=value%60;return min>0?min+'m '+sec+'s':sec+'s'}
async function addManualNode(){return runAsync('添加失败',async()=>{const payload={link:document.getElementById('manual-link').value.trim(),region:document.getElementById('manual-region').value.trim(),note:document.getElementById('manual-note').value.trim()};if(!payload.link){showToast('请填写节点链接');return}await api('/api/manual-node/add',{method:'POST',body:JSON.stringify(payload)});document.getElementById('manual-link').value='';document.getElementById('manual-region').value='';document.getElementById('manual-note').value='';await Promise.all([loadStats(),loadProxies()]);showToast('手工节点已添加')})}
async function editManualRegion(id,address){return runAsync('地域更新失败',async()=>{const current=allProxies.find(p=>Number(p.id)===Number(id))||{};const region=prompt('地域',current.region||'');if(region===null)return;await api('/api/manual-node/region',{method:'POST',body:JSON.stringify({id,address,region})});await loadProxies();showToast('地域已更新')})}
async function editManualNote(id,address){return runAsync('备注更新失败',async()=>{const current=allProxies.find(p=>Number(p.id)===Number(id))||{};const note=prompt('备注',current.note||'');if(note===null)return;await api('/api/manual-node/note',{method:'POST',body:JSON.stringify({id,address,note})});await loadProxies();showToast('备注已更新')})}
async function deleteManualNode(id,address){return runAsync('删除失败',async()=>{if(!confirm('删除此手工节点？'))return;await api('/api/manual-node/delete',{method:'POST',body:JSON.stringify({id,address})});await Promise.all([loadStats(),loadProxies()]);showToast('手工节点已删除')})}
async function toggleProxy(id,address,enable){return runAsync('操作失败',async()=>{await api('/api/proxy/toggle',{method:'POST',body:JSON.stringify({id,address,enable})});await Promise.all([loadStats(),loadProxies()]);showToast(enable?'节点已启用':'节点已停用')})}
// testProxy: 触发单节点重新验证（走完整 ValidateOne，含连通 google/openai/github/cloudflare/gstatic），后端异步执行，稍后自动刷新列表。
async function testProxy(id,address){return runAsync('测试失败',async()=>{await api('/api/proxy/refresh',{method:'POST',body:JSON.stringify({id,address})});showToast('测试连通已启动，稍后自动刷新');setTimeout(()=>runAsync('刷新失败',()=>Promise.all([loadStats(),loadProxies()])),4000)})}
async function loadSubscriptions(){const subs=await api('/api/subscriptions');if(!subs)return;const box=document.getElementById('sub-list');if(!Array.isArray(subs)||subs.length===0){box.innerHTML='<div class="empty">暂无订阅，点右上角“添加订阅”</div>';return}box.innerHTML=subs.map(sub=>{const paused=sub.status==='paused';const activeCount=Number(sub.active_count||0);const disabledCount=Number(sub.disabled_count||0);const proxyCount=Number(sub.proxy_count||0);const pausedCount=Number(sub.paused_count??Math.max(0,proxyCount-activeCount-disabledCount));const toggleLabel=paused?'启用':'暂停';const badge=paused?'<span class="badge warn">已暂停</span>':'<span class="badge ok">活跃</span>';const id=Number(sub.id);const idArg=Number.isFinite(id)?String(id):'0';return '<div class="sub-item"><div class="meta"><strong>'+html(sub.name)+' '+badge+'</strong><div class="muted">'+html(activeCount)+' 可用 / '+html(pausedCount)+' 暂停 / '+html(disabledCount)+' 不可用</div></div><div class="mini-actions"><button class="mini" onclick="refreshSub('+idArg+')" title="重新拉取并验证">刷新</button><button class="mini" onclick="toggleSub('+idArg+')" title="启用或暂停该订阅及其节点">'+toggleLabel+'</button><button class="mini danger" onclick="deleteSub('+idArg+')" title="删除订阅及其节点">删除</button></div></div>'}).join('')}
function openSubModal(){document.getElementById('sub-modal').classList.add('show')}function closeSubModal(){document.getElementById('sub-modal').classList.remove('show')}
async function addSubscription(){return runAsync('添加失败',async()=>{const payload={name:document.getElementById('sub-name').value.trim(),url:document.getElementById('sub-url').value.trim(),file_content:document.getElementById('sub-file-content').value.trim(),refresh_min:Number(document.getElementById('sub-refresh').value)||60};if(!payload.url&&!payload.file_content){showToast('请填写订阅 URL 或粘贴配置内容');return}await api('/api/subscription/add',{method:'POST',body:JSON.stringify(payload)});document.getElementById('sub-name').value='';document.getElementById('sub-url').value='';document.getElementById('sub-file-content').value='';closeSubModal();await Promise.all([loadSubscriptions(),loadStats(),loadProxies()]);showToast('订阅已添加')})}
async function refreshSub(id){return runAsync('刷新失败',async()=>{await api('/api/subscription/refresh',{method:'POST',body:JSON.stringify({id})});showToast('刷新已启动，稍后自动更新');refreshLater()})}
async function refreshAllSubs(){return runAsync('刷新失败',async()=>{await api('/api/subscription/refresh-all',{method:'POST'});showToast('全部刷新已启动，稍后自动更新');refreshLater()})}
async function toggleSub(id){return runAsync('切换失败',async()=>{await api('/api/subscription/toggle',{method:'POST',body:JSON.stringify({id})});await Promise.all([loadSubscriptions(),loadStats(),loadProxies()]);showToast('已切换启用/暂停状态')})}
async function deleteSub(id){return runAsync('删除失败',async()=>{if(!confirm('删除此订阅及其全部节点？'))return;await api('/api/subscription/delete',{method:'POST',body:JSON.stringify({id})});await Promise.all([loadSubscriptions(),loadStats(),loadProxies()]);showToast('订阅已删除')})}
async function loadLogs(){const data=await api('/api/logs');if(!data)return;const lines=Array.isArray(data.lines)?data.lines:[];document.getElementById('logs-box').innerHTML=lines.length?lines.map(line=>'<div class="log-line">'+html(line)+'</div>').join(''):'<div class="log-line">no logs</div>'}
async function loadConfig(){configCache=await api('/api/config');if(!configCache)return;const hp=stripColon(configCache.http_port),sp=stripColon(configCache.socks5_port),wp=stripColon(configCache.webui_port);document.getElementById('cfg-http-port').value=hp;document.getElementById('cfg-socks5-port').value=sp;document.getElementById('cfg-webui-port').value=wp;document.getElementById('cfg-auth-enabled').value=String(Boolean(configCache.proxy_auth_enabled));document.getElementById('cfg-auth-username').value=configCache.proxy_auth_username||'';document.getElementById('cfg-auth-password').value='';document.getElementById('cfg-session-ttl').value=configCache.session_ttl_minutes||'';document.getElementById('cfg-default-region').value=configCache.default_region||'';document.getElementById('cfg-health-interval').value=configCache.health_check_interval||'';document.getElementById('cfg-max-retry').value=configCache.max_retry??'';document.getElementById('cfg-singbox-path').value=configCache.singbox_path||'';document.getElementById('cfg-allowed-countries').value=(configCache.allowed_countries||[]).join(',');document.getElementById('cfg-blocked-countries').value=(configCache.blocked_countries||[]).join(',');renderConnection();renderDSLExamples()}
async function loadPublicIP(){return runAsync('公网 IP 获取失败',async()=>{const d=await api('/api/public-ip');if(d&&d.public_ip){publicIP=d.public_ip;renderConnection()}})}
function renderConnection(){if(!configCache)return;const sp=stripColon(configCache.socks5_port)||'7801';const hp=stripColon(configCache.http_port)||'7802';const base=configCache.proxy_auth_username||'acct';const enabled=configCache.proxy_auth_enabled;const host=publicIP||location.hostname||'127.0.0.1';document.getElementById('conn-socks5').textContent=host+':'+(sp||'7801');document.getElementById('conn-http').textContent=host+':'+(hp||'7802');document.getElementById('conn-user').textContent=base;document.getElementById('conn-pass').textContent=enabled?'见首次启动日志 / 系统设置':'（认证已关闭，无需密码）';document.getElementById('conn-auth-state').textContent=enabled?'代理认证：开启':'代理认证：关闭';const cred=enabled?(base+':PASSWORD@'):'';document.getElementById('conn-cmd').textContent='curl --socks5 '+cred+host+':'+(sp||'7801')+' https://www.gstatic.com/generate_204'}
function renderDSLExamples(){const base=(configCache&&configCache.proxy_auth_username)?configCache.proxy_auth_username:'acct';const box=document.getElementById('dsl-examples');if(box){box.innerHTML=['-region-us','-region-jp-session-app01','-session-browser'].map(s=>'<div class="guide-row"><b>'+html(base)+'</b><span>'+html(s)+'</span></div>').join('')}const hint=document.getElementById('dsl-hint');if(hint){hint.textContent=(configCache&&configCache.proxy_auth_enabled)?('前缀 “'+base+'” = 代理认证用户名；-region-XX 指定地域；-session-ID 保持出口黏连。'):'代理认证当前关闭；启用后前缀须等于代理认证用户名。'}}
async function openSettings(){if(!configCache)await loadConfig();document.getElementById('settings-modal').classList.add('show')}function closeSettings(){document.getElementById('settings-modal').classList.remove('show')}function countries(id){return document.getElementById(id).value.split(',').map(v=>v.trim().toUpperCase()).filter(Boolean)}
async function saveConfig(){return runAsync('保存失败',async()=>{if(!configCache)await loadConfig();if(!configCache)throw new Error('配置未加载');const payload={proxy_auth_enabled:document.getElementById('cfg-auth-enabled').value==='true',proxy_auth_username:document.getElementById('cfg-auth-username').value.trim(),proxy_auth_password:document.getElementById('cfg-auth-password').value,session_ttl_minutes:Number(document.getElementById('cfg-session-ttl').value),default_region:document.getElementById('cfg-default-region').value.trim().toLowerCase(),health_check_interval:Number(document.getElementById('cfg-health-interval').value),max_retry:Number(document.getElementById('cfg-max-retry').value),singbox_path:document.getElementById('cfg-singbox-path').value.trim(),allowed_countries:countries('cfg-allowed-countries'),blocked_countries:countries('cfg-blocked-countries')};await api('/api/config/save',{method:'POST',body:JSON.stringify(payload)});closeSettings();await loadConfig();showToast('配置已保存')})}
refreshAll();
setInterval(()=>runAsync('自动刷新失败',()=>Promise.all([loadStats(),loadProxies(),loadSubscriptions(),loadSessions()])),10000);
setInterval(()=>runAsync('日志刷新失败',loadLogs),5000);
</script>
</body>
</html>`
