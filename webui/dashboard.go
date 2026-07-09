package webui

const dashboardHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>GeoProxy Gateway Admin</title>
<style>
:root{
  --bg:#f6f8fc; --panel:#fff; --ink:#1a2436; --muted:#6b7488; --line:#e5e9f0;
  --soft:#eef3ff; --accent:#2557d6; --accent-2:#0f9f7a; --danger:#c2413a;
  --warn:#b8860b; --shadow:0 10px 34px rgba(28,42,71,.08); --radius:18px;
}
[data-theme="dark"]{
  --bg:#0f1626; --panel:#161f31; --ink:#e6ecf6; --muted:#8a96ab; --line:#26314a;
  --soft:#1b2740; --accent:#5b8cff; --accent-2:#2bc39a; --danger:#f0685f;
  --warn:#e0a93b; --shadow:0 10px 34px rgba(0,0,0,.35);
}
[data-theme="dark"] .logs{background:#080d17}
[data-theme="dark"] tbody tr:hover{background:#1b2740}
[data-theme="dark"] .mini{background:#1b2740}
[data-theme="dark"] .mini:hover{background:#22304c;border-color:#3a4a6b}

[data-theme="dark"] .bar{background:#26314a}
[data-theme="dark"] .badge{background:#22304c;color:#c3cee2}
[data-theme="dark"] .badge.green{background:#12352a;color:#4fcfa2}
[data-theme="dark"] .badge.blue{background:#1a2a4d;color:#7ea6ff}
[data-theme="dark"] .badge.gray{background:#22304c;color:#9aa6bd}
[data-theme="dark"] .badge.warn{background:#3a2f14;color:#e0a93b}
[data-theme="dark"] input,[data-theme="dark"] select,[data-theme="dark"] textarea{background:#0f1626;color:var(--ink)}
[data-theme="dark"] .dialog{background:#161f31}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--ink);
  font-family:"Segoe UI","PingFang SC","Microsoft YaHei",Verdana,sans-serif;
  font-size:14px;line-height:1.55}
button,input,select,textarea{font:inherit}
.shell{min-height:100vh;display:flex;flex-direction:column}

/* Top bar */
.topbar{position:sticky;top:0;z-index:20;background:var(--panel);
  backdrop-filter:blur(14px);border-bottom:1px solid var(--line)}
.topbar-inner{max-width:1360px;margin:0 auto;padding:14px 24px;display:flex;
  align-items:center;justify-content:space-between;gap:16px;flex-wrap:wrap}
.brand{display:flex;align-items:center;gap:12px}
.mark{width:38px;height:38px;border-radius:12px;background:var(--ink);color:#fff;
  display:grid;place-items:center;font-weight:800;letter-spacing:.04em}
.eyebrow{font-size:10px;letter-spacing:.16em;text-transform:uppercase;
  color:var(--muted);font-weight:700}
.brand h1{margin:1px 0 0;font-size:19px;line-height:1.1}
.actions{display:flex;align-items:center;gap:8px;flex-wrap:wrap}
.btn{border:1px solid var(--line);background:#fff;color:var(--ink);border-radius:999px;
  padding:9px 15px;cursor:pointer;text-decoration:none;font-weight:600;white-space:nowrap}
.btn:hover{border-color:#c3ccdd;background:#fafbfe}
.btn.primary{background:var(--accent);border-color:var(--accent);color:#fff}
.btn.primary:hover{background:#1f4dc0}
.btn.danger{color:var(--danger)}

.wrap{width:100%;max-width:1360px;margin:0 auto;padding:24px;flex:1}

/* Metric cards */
.metrics{display:grid;grid-template-columns:repeat(auto-fit,minmax(150px,1fr));
  gap:14px;margin-bottom:22px}
.metric{background:var(--panel);border:1px solid var(--line);border-radius:var(--radius);
  padding:18px 20px;box-shadow:var(--shadow)}
.metric .label{font-size:12px;color:var(--muted);font-weight:600}
.metric .value{font-size:30px;font-weight:800;letter-spacing:-.02em;margin:4px 0 2px}
.metric .note{font-size:11px;color:var(--muted)}

/* Two-column responsive layout */
.layout{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);
  gap:20px;align-items:start}
@media(max-width:1080px){.layout{grid-template-columns:1fr}}

.card{background:var(--panel);border:1px solid var(--line);border-radius:var(--radius);
  box-shadow:var(--shadow);margin-bottom:20px;overflow:hidden}
.card-head{display:flex;align-items:center;justify-content:space-between;gap:12px;
  padding:16px 20px;border-bottom:1px solid var(--line)}
.card-head h3{margin:0;font-size:15px}
.card-head .tools{display:flex;align-items:center;gap:8px;flex-wrap:wrap}
.card-body{padding:16px 20px}

/* DSL guide */
.guide-row{display:flex;flex-wrap:wrap;gap:6px;font-family:"Consolas",monospace;
  font-size:13px;background:var(--soft);border-radius:10px;padding:8px 12px;margin-bottom:6px}
.guide-row b{color:var(--accent)}
.guide-row span{color:var(--muted)}
.hint{font-size:12px;color:var(--muted);margin-top:8px}

/* Filters + manual add */
.toolbar{display:flex;flex-wrap:wrap;gap:8px;margin-bottom:14px}
.input{border:1px solid var(--line);border-radius:10px;padding:8px 11px;background:#fff;
  color:var(--ink);min-width:0}
.input:focus{outline:none;border-color:var(--accent);box-shadow:0 0 0 3px rgba(37,87,214,.12)}
.grow{flex:1;min-width:140px}

/* Tables */
.table-wrap{width:100%;overflow-x:auto}
table{width:100%;border-collapse:collapse;font-size:13px}
th,td{padding:9px 10px;text-align:left;border-bottom:1px solid var(--line);
  white-space:nowrap;vertical-align:middle}
th{font-size:11px;text-transform:uppercase;letter-spacing:.04em;color:var(--muted);
  font-weight:700}
tbody tr:last-child td{border-bottom:none}
tbody tr:hover{background:#fafbfe}
td.mono,.mono{font-family:"Consolas",monospace}
.empty{text-align:center;color:var(--muted);padding:22px 0}

.badge{display:inline-block;padding:2px 9px;border-radius:999px;font-size:11px;
  font-weight:700;background:#eef1f6;color:#42506a}
.badge.green{background:#e7f6ef;color:#0f7a58}
.badge.blue{background:#e8efff;color:#2557d6}
.badge.gray{background:#eef1f6;color:#5a6478}
.badge.warn{background:#fdf3e0;color:#a5730a}

.mini{border:1px solid var(--line);background:#fff;border-radius:8px;padding:5px 10px;
  cursor:pointer;font-size:12px;font-weight:600}
.mini:hover{border-color:#c3ccdd;background:#fafbfe}
.mini.danger{color:var(--danger)}

/* Region distribution */
.region-row{display:flex;align-items:center;gap:12px;padding:7px 0}
.region-row strong{width:42px;font-size:13px}
.bar{flex:1;height:8px;background:#eef1f6;border-radius:999px;overflow:hidden}
.bar span{display:block;height:100%;background:var(--accent);border-radius:999px}
.region-row .cnt{width:36px;text-align:right;color:var(--muted);font-size:13px}

/* Status key-value rows */
.kv{display:flex;align-items:center;justify-content:space-between;gap:12px;
  padding:7px 0;border-bottom:1px solid var(--line);font-size:13px}
.kv:last-child{border-bottom:none}
.kv .k{color:var(--muted)}
.kv .v{font-weight:700}
.dot{display:inline-block;width:8px;height:8px;border-radius:50%;margin-right:6px;
  vertical-align:middle}
.dot.on{background:var(--accent-2)}
.dot.off{background:var(--danger)}

/* Subscription list */
.sub-item{display:flex;align-items:center;justify-content:space-between;gap:12px;
  padding:12px 0;border-bottom:1px solid var(--line);flex-wrap:wrap}
.sub-item:last-child{border-bottom:none}
.sub-item .meta{min-width:0}
.sub-item .meta strong{display:block}
.sub-item .meta .muted{font-size:12px;color:var(--muted)}
.mini-actions{display:flex;gap:6px;flex-wrap:wrap}

/* Session cards (no wide table -> no horizontal scroll) */
.session-list{display:grid;grid-template-columns:1fr;gap:10px}
.session-card{border:1px solid var(--line);border-radius:12px;padding:10px 12px}
.session-card .top{display:flex;align-items:center;justify-content:space-between;gap:8px}
.session-card .sid{font-family:"Consolas",monospace;font-weight:700;font-size:13px;
  overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.session-card .node{font-family:"Consolas",monospace;font-size:12px;color:var(--muted);
  margin-top:4px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.session-card .ttl{font-size:12px;color:var(--accent-2);font-weight:700;white-space:nowrap}

/* Logs */
.logs{background:#0f1626;color:#c9d4e6;border-radius:12px;padding:12px;
  font-family:"Consolas",monospace;font-size:12px;line-height:1.5;
  max-height:340px;overflow:auto;white-space:pre-wrap;word-break:break-all}
.log-line{padding:1px 0}

.muted{color:var(--muted)}

/* Modal */
.modal{position:fixed;inset:0;background:rgba(20,28,45,.45);display:none;
  align-items:flex-start;justify-content:center;padding:40px 16px;z-index:50;overflow:auto}
.modal.show{display:flex}
.dialog{background:#fff;border-radius:var(--radius);width:min(560px,100%);
  padding:24px;box-shadow:0 30px 80px rgba(20,30,50,.28)}
.dialog h3{margin:0 0 16px}
.form-grid{display:grid;grid-template-columns:1fr 1fr;gap:14px}
.field{display:flex;flex-direction:column;gap:5px}
.field.full{grid-column:1 / -1}
.field label{font-size:12px;color:var(--muted);font-weight:600}
.field input,.field select,.field textarea{border:1px solid var(--line);border-radius:10px;
  padding:9px 11px;background:#fff;color:var(--ink);width:100%}
.field textarea{min-height:120px;resize:vertical;font-family:"Consolas",monospace}
.dialog-actions{display:flex;justify-content:flex-end;gap:10px;margin-top:20px}

/* Toast */
.toast{position:fixed;left:50%;bottom:28px;transform:translateX(-50%) translateY(20px);
  background:var(--ink);color:#fff;padding:11px 20px;border-radius:999px;font-weight:600;
  opacity:0;pointer-events:none;transition:.25s;z-index:60}
.toast.show{opacity:1;transform:translateX(-50%) translateY(0)}
</style>
</head>
<body>
<div class="shell">
  <header class="topbar">
    <div class="topbar-inner">
      <div class="brand">
        <div class="mark">GG</div>
        <div><div class="eyebrow">Authenticated Admin</div><h1>GeoProxy Gateway</h1></div>
      </div>
      <div class="actions">
        <button class="btn" id="theme-toggle" onclick="toggleTheme()" title="切换深色/浅色主题">🌙 深色</button>
        <button class="btn" onclick="refreshAll()">刷新数据</button>
        <button class="btn primary" onclick="openSubModal()">添加订阅</button>
        <button class="btn" onclick="openSettings()">系统设置</button>
        <a class="btn danger" href="/logout">退出</a>
      </div>
    </div>
  </header>
  <main class="wrap">
    <section class="metrics">
      <div class="metric"><div class="label">上游节点</div><div class="value" id="stat-total">--</div><div class="note">active / degraded</div></div>
      <div class="metric"><div class="label">HTTP</div><div class="value" id="stat-http">--</div><div class="note">可用入口节点</div></div>
      <div class="metric"><div class="label">SOCKS5</div><div class="value" id="stat-socks5">--</div><div class="note">可用入口节点</div></div>
      <div class="metric"><div class="label">订阅节点</div><div class="value" id="stat-subscription">--</div><div class="note">订阅来源可用数</div></div>
      <div class="metric"><div class="label">活跃会话</div><div class="value" id="stat-sessions">--</div><div class="note">当前绑定数</div></div>
    </section>

    <div class="layout">
      <div class="col">
        <div class="card">
          <div class="card-head">
            <h3>节点清单</h3>
            <div class="tools">
              <select class="input" id="protocol-filter" onchange="renderProxies()"><option value="">全部协议</option><option value="http">HTTP</option><option value="socks5">SOCKS5</option></select>
              <select class="input" id="region-filter" onchange="renderProxies()"><option value="">全部地域</option></select>
            </div>
          </div>
          <div class="card-body">
            <div class="toolbar">
              <input class="input grow" id="manual-link" placeholder="http://host:port / socks5://host:port">
              <input class="input" id="manual-region" maxlength="2" placeholder="地域" style="width:70px">
              <input class="input" id="manual-note" placeholder="备注" style="width:120px">
              <button class="mini" onclick="addManualNode()">添加手工节点</button>
            </div>
            <div class="table-wrap">
              <table>
                <thead><tr><th>名称</th><th>协议</th><th>地域</th><th>出口 IP</th><th>延迟</th><th>来源</th><th>状态</th><th>操作</th></tr></thead>
                <tbody id="proxy-rows"><tr><td colspan="8" class="empty">加载中</td></tr></tbody>
              </table>
            </div>
          </div>
        </div>

        <div class="card">
          <div class="card-head"><h3>订阅管理</h3><div class="tools"><button class="mini" onclick="refreshAllSubs()">刷新所有</button></div></div>
          <div class="card-body"><div id="sub-list"><div class="empty">加载中</div></div></div>
        </div>
      </div>

      <div class="col">
        <div class="card">
          <div class="card-head"><h3>sing-box 引擎</h3><div class="tools"><button class="mini" onclick="loadCustomStatus()">刷新</button></div></div>
          <div class="card-body"><div id="singbox-status"><div class="empty">加载中</div></div></div>
        </div>

        <div class="card">
          <div class="card-head"><h3>用户名 DSL</h3></div>
          <div class="card-body">
            <div id="dsl-examples">
              <div class="guide-row"><b>acct</b><span>-region-us</span></div>
              <div class="guide-row"><b>acct</b><span>-region-jp-session-app01</span></div>
              <div class="guide-row"><b>acct</b><span>-session-browser</span></div>
            </div>
            <div class="hint" id="dsl-hint">前缀为你在系统设置中的“代理认证用户名”。</div>
          </div>
        </div>

        <div class="card">
          <div class="card-head"><h3>地域分布</h3><span class="muted" id="region-total">--</span></div>
          <div class="card-body"><div id="region-list"><div class="empty">加载中</div></div></div>
        </div>

        <div class="card" id="session-monitor">
          <div class="card-head"><h3>Session 监控</h3><div class="tools"><button class="mini" onclick="loadSessions()">刷新</button></div></div>
          <div class="card-body"><div class="session-list" id="session-rows"><div class="empty">加载中</div></div></div>
        </div>

        <div class="card">
          <div class="card-head"><h3>运行日志</h3><div class="tools"><button class="mini" onclick="loadLogs()">刷新</button></div></div>
          <div class="card-body"><div class="logs" id="logs-box"><div class="log-line">loading...</div></div></div>
        </div>
      </div>
    </div>
  </main>
</div>

<div class="modal" id="sub-modal"><div class="dialog"><h3>添加订阅</h3><div class="form-grid"><div class="field"><label>名称</label><input id="sub-name" placeholder="primary subscription"></div><div class="field"><label>刷新间隔（分钟）</label><input id="sub-refresh" type="number" value="60" min="10" step="10"></div><div class="field full"><label>订阅 URL</label><input id="sub-url" placeholder="https://example.com/sub"></div><div class="field full"><label>或粘贴配置文件内容</label><textarea id="sub-file-content" placeholder="Clash YAML / V2ray / Base64 / plain text"></textarea></div></div><div class="dialog-actions"><button class="btn" onclick="closeSubModal()">取消</button><button class="btn primary" onclick="addSubscription()">添加</button></div></div></div>
<div class="modal" id="settings-modal"><div class="dialog"><h3>系统设置</h3><div class="form-grid"><div class="field"><label>HTTP 端口（只读）</label><input id="cfg-http-port" readonly></div><div class="field"><label>SOCKS5 端口（只读）</label><input id="cfg-socks5-port" readonly></div><div class="field"><label>WebUI 端口（只读）</label><input id="cfg-webui-port" readonly></div><div class="field"><label>代理认证</label><select id="cfg-auth-enabled"><option value="false">关闭</option><option value="true">开启</option></select></div><div class="field"><label>代理认证用户名</label><input id="cfg-auth-username"></div><div class="field"><label>代理认证密码（留空不改）</label><input id="cfg-auth-password" type="password"></div><div class="field"><label>Session TTL（分钟）</label><input id="cfg-session-ttl" type="number" min="1"></div><div class="field"><label>默认地域</label><input id="cfg-default-region" maxlength="2" placeholder="空=全局"></div><div class="field"><label>健康检查间隔（分钟）</label><input id="cfg-health-interval" type="number" min="1"></div><div class="field"><label>最大重试次数</label><input id="cfg-max-retry" type="number" min="0"></div><div class="field full"><label>sing-box 路径</label><input id="cfg-singbox-path"></div><div class="field full"><label>允许国家（逗号分隔）</label><input id="cfg-allowed-countries" placeholder="US,JP,SG"></div><div class="field full"><label>屏蔽国家（逗号分隔）</label><input id="cfg-blocked-countries" placeholder="CN,RU"></div></div><div class="dialog-actions"><button class="btn" onclick="closeSettings()">取消</button><button class="btn primary" onclick="saveConfig()">保存</button></div></div></div>
<div class="toast" id="toast"></div>
<script>
let allProxies=[];let allRegions=[];let configCache=null;
function showToast(msg){const el=document.getElementById('toast');el.textContent=msg;el.classList.add('show');setTimeout(()=>el.classList.remove('show'),2600)}
async function api(path, options){const res=await fetch(path, Object.assign({headers:{'Content-Type':'application/json'}}, options||{}));if(res.status===401){location.href='/login';return null}const text=await res.text();const data=text?JSON.parse(text):{};if(!res.ok)throw new Error(data.error||res.statusText);return data}
function safe(value){return value===undefined||value===null||value===''?'--':String(value)}
function html(value){return safe(value).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
function maskAddress(address){if(!address)return '--';const parts=String(address).split(':');const host=parts[0]||address;if(host.length<=8)return host+(parts[1]?':'+parts[1]:'');return host.slice(0,4)+'...'+host.slice(-4)+(parts[1]?':'+parts[1]:'')}
function addressArg(address){return encodeURIComponent(String(address||''))}
function regionOf(proxy){return (proxy.region||'unknown').toLowerCase()}
function stripColon(port){return String(port||'').replace(/^:/,'')}
async function refreshAll(){await Promise.all([loadStats(),loadProxies(),loadSubscriptions(),loadConfig(),loadSessions(),loadLogs(),loadCustomStatus()]);showToast('数据已刷新')}
async function loadCustomStatus(){const st=await api('/api/custom/status');if(!st)return;const box=document.getElementById('singbox-status');if(!box)return;const running=st.singbox_running;const dot='<span class="dot '+(running?'on':'off')+'"></span>';box.innerHTML='<div class="kv">'+dot+'<span>sing-box '+(running?'运行中':'未运行')+'</span></div>'+'<div class="kv"><span class="k">转换节点</span><span class="v">'+safe(st.singbox_nodes)+'</span></div>'+'<div class="kv"><span class="k">订阅可用</span><span class="v">'+safe(st.subscription_count)+'</span></div>'+'<div class="kv"><span class="k">禁用节点</span><span class="v">'+safe(st.disabled_count)+'</span></div>'+'<div class="kv"><span class="k">订阅总数</span><span class="v">'+safe(st.subscription_total)+'</span></div>'}
function applyTheme(theme){document.documentElement.setAttribute('data-theme',theme);try{localStorage.setItem('gg-theme',theme)}catch(e){}const btn=document.getElementById('theme-toggle');if(btn)btn.textContent=theme==='dark'?'☀ 浅色':'🌙 深色'}
function toggleTheme(){const cur=document.documentElement.getAttribute('data-theme')==='dark'?'dark':'light';applyTheme(cur==='dark'?'light':'dark')}
(function(){let t='light';try{t=localStorage.getItem('gg-theme')||'light'}catch(e){}applyTheme(t)})();
async function loadStats(){const stats=await api('/api/stats');if(!stats)return;document.getElementById('stat-total').textContent=safe(stats.total);document.getElementById('stat-http').textContent=safe(stats.http);document.getElementById('stat-socks5').textContent=safe(stats.socks5);document.getElementById('stat-subscription').textContent=safe(stats.subscription_count);document.getElementById('stat-sessions').textContent=safe(stats.active_sessions)}
async function loadProxies(){const data=await api('/api/proxies');if(!data)return;allProxies=Array.isArray(data)?data:[];allRegions=Array.from(new Set(allProxies.map(regionOf))).sort();renderRegionFilter();renderProxies();renderRegions()}
function renderRegionFilter(){const select=document.getElementById('region-filter');const current=select.value;select.innerHTML='<option value="">全部地域</option>'+allRegions.map(r=>'<option value="'+r+'">'+r.toUpperCase()+'</option>').join('');select.value=current}
function sourceLabel(p){if(p.source==='manual')return '手工';return p.subscription_name?p.subscription_name:'订阅';}
function nodeLabel(p){if(p.source==='manual')return maskAddress(p.address);return p.subscription_name?p.subscription_name:'订阅节点';}
function renderProxies(){const protocol=document.getElementById('protocol-filter').value;const region=document.getElementById('region-filter').value;const rows=allProxies.filter(p=>(!protocol||p.protocol===protocol)&&(!region||regionOf(p)===region));const body=document.getElementById('proxy-rows');if(rows.length===0){body.innerHTML='<tr><td colspan="8" class="empty">没有匹配节点</td></tr>';return}body.innerHTML=rows.map(p=>{const addr=addressArg(p.address);const manual=p.source==='manual';const paused=p.status==='disabled';const label=html(nodeLabel(p));const actions=manual?'<button class="mini" onclick="editManualRegion(decodeURIComponent(\''+addr+'\'))">地域</button> <button class="mini" onclick="editManualNote(decodeURIComponent(\''+addr+'\'))">备注</button> <button class="mini danger" onclick="deleteManualNode(decodeURIComponent(\''+addr+'\'))">删除</button>':('<button class="mini" onclick="toggleProxy(decodeURIComponent(\''+addr+'\'),'+(paused?'true':'false')+')">'+(paused?'启用':'停用')+'</button>');const statusBadge=paused?'<span class="badge warn">已停用</span>':'<span class="badge green">'+html(p.status)+'</span>';return '<tr><td title="'+label+'">'+label+'</td><td><span class="badge blue">'+html(p.protocol).toUpperCase()+'</span></td><td><span class="badge green">'+html(regionOf(p)).toUpperCase()+'</span></td><td class="mono">'+html(p.exit_ip)+'</td><td>'+html(p.latency)+' ms</td><td>'+html(sourceLabel(p))+'</td><td title="'+html(p.note)+'">'+statusBadge+'</td><td>'+actions+'</td></tr>'}).join('')}
function renderRegions(){const counts={};allProxies.forEach(p=>{const r=regionOf(p);counts[r]=(counts[r]||0)+1});const entries=Object.keys(counts).sort().map(region=>({region,count:counts[region]}));const total=entries.reduce((sum,item)=>sum+item.count,0);document.getElementById('region-total').textContent=total+' nodes';const list=document.getElementById('region-list');if(entries.length===0){list.innerHTML='<div class="empty">暂无地域数据</div>';return}list.innerHTML=entries.map(item=>{const pct=total?Math.round(item.count*100/total):0;return '<div class="region-row"><strong>'+item.region.toUpperCase()+'</strong><div class="bar"><span style="width:'+pct+'%"></span></div><span class="cnt">'+item.count+'</span></div>'}).join('')}
async function loadSessions(){const sessions=await api('/api/sessions');if(!sessions)return;const body=document.getElementById('session-rows');if(!Array.isArray(sessions)||sessions.length===0){body.innerHTML='<div class="empty">暂无活跃 session</div>';return}body.innerHTML=sessions.map(s=>{const masked=html(maskAddress(s.node));return '<div class="session-card"><div class="top"><span class="sid" title="'+html(s.session_id)+'">'+html(s.session_id)+'</span><span class="ttl">'+html(formatTTL(s.remaining_ttl_seconds))+'</span></div><div class="node" title="'+masked+'"><span class="badge green">'+html(s.region).toUpperCase()+'</span> '+masked+'</div></div>'}).join('')}
function formatTTL(seconds){const value=Number(seconds)||0;const min=Math.floor(value/60);const sec=value%60;return min>0?min+'m '+sec+'s':sec+'s'}
async function addManualNode(){const payload={link:document.getElementById('manual-link').value.trim(),region:document.getElementById('manual-region').value.trim(),note:document.getElementById('manual-note').value.trim()};if(!payload.link){showToast('请填写节点链接');return}try{await api('/api/manual-node/add',{method:'POST',body:JSON.stringify(payload)});document.getElementById('manual-link').value='';document.getElementById('manual-region').value='';document.getElementById('manual-note').value='';await Promise.all([loadStats(),loadProxies()]);showToast('手工节点已添加')}catch(err){showToast('添加失败：'+err.message)}}
async function editManualRegion(address){const current=allProxies.find(p=>p.address===address)||{};const region=prompt('地域',current.region||'');if(region===null)return;await api('/api/manual-node/region',{method:'POST',body:JSON.stringify({address,region})});await loadProxies();showToast('地域已更新')}
async function editManualNote(address){const current=allProxies.find(p=>p.address===address)||{};const note=prompt('备注',current.note||'');if(note===null)return;await api('/api/manual-node/note',{method:'POST',body:JSON.stringify({address,note})});await loadProxies();showToast('备注已更新')}
async function deleteManualNode(address){if(!confirm('删除此手工节点？'))return;await api('/api/manual-node/delete',{method:'POST',body:JSON.stringify({address})});await Promise.all([loadStats(),loadProxies()]);showToast('手工节点已删除')}
async function toggleProxy(address,enable){try{await api('/api/proxy/toggle',{method:'POST',body:JSON.stringify({address,enable})});await Promise.all([loadStats(),loadProxies()]);showToast(enable?'节点已启用':'节点已停用')}catch(err){showToast('操作失败：'+err.message)}}
async function loadSubscriptions(){const subs=await api('/api/subscriptions');if(!subs)return;const box=document.getElementById('sub-list');if(!Array.isArray(subs)||subs.length===0){box.innerHTML='<div class="empty">暂无订阅</div>';return}box.innerHTML=subs.map(sub=>{const paused=sub.status==='paused';const toggleLabel=paused?'启用':'暂停';const badge=paused?'<span class="badge warn">已暂停</span>':'<span class="badge green">活跃</span>';return '<div class="sub-item"><div class="meta"><strong>'+html(sub.name)+' '+badge+'</strong><div class="muted">'+safe(sub.active_count)+' 可用 / '+safe(sub.disabled_count)+' 禁用</div></div><div class="mini-actions"><button class="mini" onclick="refreshSub('+sub.id+')" title="重新拉取该订阅并验证节点">刷新</button><button class="mini" onclick="toggleSub('+sub.id+')" title="启用或暂停该订阅">'+toggleLabel+'</button><button class="mini danger" onclick="deleteSub('+sub.id+')" title="删除订阅及其节点">删除</button></div></div>'}).join('')}
function openSubModal(){document.getElementById('sub-modal').classList.add('show')}function closeSubModal(){document.getElementById('sub-modal').classList.remove('show')}
async function addSubscription(){try{const payload={name:document.getElementById('sub-name').value.trim(),url:document.getElementById('sub-url').value.trim(),file_content:document.getElementById('sub-file-content').value.trim(),refresh_min:Number(document.getElementById('sub-refresh').value)||60};if(!payload.url&&!payload.file_content){showToast('请填写订阅 URL 或粘贴配置内容');return}await api('/api/subscription/add',{method:'POST',body:JSON.stringify(payload)});document.getElementById('sub-name').value='';document.getElementById('sub-url').value='';document.getElementById('sub-file-content').value='';closeSubModal();await Promise.all([loadSubscriptions(),loadStats(),loadProxies()]);showToast('订阅已添加')}catch(err){showToast('添加失败：'+err.message)}}
async function refreshSub(id){await api('/api/subscription/refresh',{method:'POST',body:JSON.stringify({id})});showToast('刷新已启动，稍后自动更新');setTimeout(()=>Promise.all([loadSubscriptions(),loadStats(),loadProxies()]),4000)}
async function refreshAllSubs(){await api('/api/subscription/refresh-all',{method:'POST'});showToast('全部刷新已启动，稍后自动更新');setTimeout(()=>Promise.all([loadSubscriptions(),loadStats(),loadProxies()]),4000)}
async function toggleSub(id){await api('/api/subscription/toggle',{method:'POST',body:JSON.stringify({id})});await Promise.all([loadSubscriptions(),loadStats(),loadProxies()]);showToast('已切换启用/暂停状态')}
async function deleteSub(id){if(!confirm('删除此订阅及其全部节点？'))return;await api('/api/subscription/delete',{method:'POST',body:JSON.stringify({id})});await Promise.all([loadSubscriptions(),loadStats(),loadProxies()]);showToast('订阅已删除')}
async function loadLogs(){const data=await api('/api/logs');if(!data)return;const lines=Array.isArray(data.lines)?data.lines:[];document.getElementById('logs-box').innerHTML=lines.length?lines.map(line=>'<div class="log-line">'+String(line).replace(/[&<>]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;'}[c]))+'</div>').join(''):'<div class="log-line">no logs</div>'}
async function loadConfig(){configCache=await api('/api/config');if(!configCache)return;document.getElementById('cfg-http-port').value=stripColon(configCache.http_port);document.getElementById('cfg-socks5-port').value=stripColon(configCache.socks5_port);document.getElementById('cfg-webui-port').value=stripColon(configCache.webui_port);document.getElementById('cfg-auth-enabled').value=String(Boolean(configCache.proxy_auth_enabled));document.getElementById('cfg-auth-username').value=configCache.proxy_auth_username||'';document.getElementById('cfg-auth-password').value='';document.getElementById('cfg-session-ttl').value=configCache.session_ttl_minutes||'';document.getElementById('cfg-default-region').value=configCache.default_region||'';document.getElementById('cfg-health-interval').value=configCache.health_check_interval||'';document.getElementById('cfg-max-retry').value=configCache.max_retry??'';document.getElementById('cfg-singbox-path').value=configCache.singbox_path||'';document.getElementById('cfg-allowed-countries').value=(configCache.allowed_countries||[]).join(',');document.getElementById('cfg-blocked-countries').value=(configCache.blocked_countries||[]).join(',');renderDSLExamples()}
function renderDSLExamples(){const base=(configCache&&configCache.proxy_auth_username)?configCache.proxy_auth_username:'acct';const box=document.getElementById('dsl-examples');if(box){box.innerHTML=['-region-us','-region-jp-session-app01','-session-browser'].map(s=>'<div class="guide-row"><b>'+html(base)+'</b><span>'+html(s)+'</span></div>').join('')}const hint=document.getElementById('dsl-hint');if(hint){hint.textContent=(configCache&&configCache.proxy_auth_enabled)?('前缀 “'+base+'” 为系统设置中的代理认证用户名。'):'代理认证当前关闭；启用后前缀须等于代理认证用户名。'}}
async function openSettings(){if(!configCache)await loadConfig();document.getElementById('settings-modal').classList.add('show')}function closeSettings(){document.getElementById('settings-modal').classList.remove('show')}function countries(id){return document.getElementById(id).value.split(',').map(v=>v.trim().toUpperCase()).filter(Boolean)}
async function saveConfig(){if(!configCache)await loadConfig();if(!configCache)throw new Error('配置未加载');const payload={proxy_auth_enabled:document.getElementById('cfg-auth-enabled').value==='true',proxy_auth_username:document.getElementById('cfg-auth-username').value.trim(),proxy_auth_password:document.getElementById('cfg-auth-password').value,session_ttl_minutes:Number(document.getElementById('cfg-session-ttl').value),default_region:document.getElementById('cfg-default-region').value.trim().toLowerCase(),health_check_interval:Number(document.getElementById('cfg-health-interval').value),max_retry:Number(document.getElementById('cfg-max-retry').value),singbox_path:document.getElementById('cfg-singbox-path').value.trim(),allowed_countries:countries('cfg-allowed-countries'),blocked_countries:countries('cfg-blocked-countries')};await api('/api/config/save',{method:'POST',body:JSON.stringify(payload)});closeSettings();await loadConfig();showToast('配置已保存')}
refreshAll().catch(err=>showToast(err.message));
// 10 秒自动刷新核心数据（统计/节点/订阅/会话）；日志单独 5 秒滚动。
setInterval(()=>{loadStats();loadProxies();loadSubscriptions();loadSessions()},10000);
setInterval(loadLogs,5000);
</script>
</body>
</html>`
