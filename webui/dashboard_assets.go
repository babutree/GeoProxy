package webui

// dashboard_assets.go 将 dashboard 的 CSS/JS 从 HTML 中分离为 Go 常量，
// 由 /assets/dashboard.css 与 /assets/dashboard.js 路由下发（带内容 hash 的 ETag、支持 304）。
// 仍为 Go 内嵌字符串，不落地独立文件、不引入前端构建链。

const dashboardCSS = `/* GeoProxy Gateway 控制台样式
   设计令牌：8px 间距栅格、150ms 微交互 / 250ms 面板过渡、统一缓动 cubic-bezier(0.16,1,0.3,1)、
   暗色底沿用非纯黑 #0d1320、可聚焦元素统一 :focus-visible 2px 描边、prefers-reduced-motion 关闭非必要动画。
   注意：本文件由 /assets/dashboard.css 路由下发；所有既有 class 契约（worldmap/badge/ai-icon 等）保持不变。 */
:root{
  --bg:#eef1f8; --panel:#fff; --ink:#151c2b; --muted:#5b6478; --line:#e2e7f1;
  --soft:#eef2fb; --accent:#3b6ef6; --accent-ink:#fff; --ok:#12a150; --warn:#d0890a;
  --danger:#e0484d; --gray:#8a93a6; --shadow:0 8px 30px rgba(24,38,68,.07); --radius:16px;
  --sidebar-w:220px; --sidebar-w-collapsed:64px; --topbar-h:56px;
  --ease:cubic-bezier(0.16,1,0.3,1); --t-micro:150ms; --t-panel:250ms;
  /* 设计语言 v2 追加令牌（不重命名旧变量，纯增量，避免大面积回归） */
  --panel-2:#f3f6fd; --panel-3:#e8edf8; --hover:#e8edf8;
  --ink-soft:#8a93a6; --accent-strong:#2b57d6; --accent-soft:rgba(59,110,246,.12);
  --signal:#0a9fbf; --hairline:rgba(20,28,48,.05);
  --surface-2:#f3f6fd; --ink-3:#8a93a6; --fs-caption:11px; --t-fast:150ms; --ease-out:cubic-bezier(0.16,1,0.3,1);
  --sh-sm:0 1px 2px rgba(24,38,68,.06),0 1px 1px rgba(24,38,68,.04);
  --sh-md:0 4px 16px rgba(24,38,68,.08),0 2px 6px rgba(24,38,68,.05);
  --sh-lg:0 18px 48px rgba(24,38,68,.14),0 6px 18px rgba(24,38,68,.08);
  --accent-grad:linear-gradient(135deg,var(--accent),#6d5cf7 55%,var(--signal));
  --wm-ocean-1:#e8eefb; --wm-ocean-2:#dbe4f5; --wm-grid:#b9c6e2;
  --wm-land:#cdd8ee; --wm-land-line:#9fb0d4;
}
[data-theme="dark"]{
  --bg:#080d18; --panel:#111a2b; --ink:#e8edf6; --muted:#9aa4ba; --line:#222d44;
  --soft:#172236; --accent:#5b8cff; --accent-ink:#fff; --ok:#2fbf87; --warn:#e5a93b;
  --danger:#f0685f; --gray:#8b95ab; --shadow:0 8px 30px rgba(0,0,0,.32);
  --panel-2:#172236; --panel-3:#1e2b43; --hover:#1e2b43;
  --ink-soft:#6b7590; --accent-strong:#7aa2ff; --accent-soft:rgba(91,140,255,.16);
  --signal:#22d3ee; --hairline:rgba(255,255,255,.06);
  --surface-2:#172236; --ink-3:#6b7590; --fs-caption:11px; --t-fast:150ms; --ease-out:cubic-bezier(0.16,1,0.3,1);
  --sh-sm:0 1px 2px rgba(0,0,0,.4);
  --sh-md:0 8px 24px rgba(0,0,0,.45);
  --sh-lg:0 24px 64px rgba(0,0,0,.55);
  --wm-ocean-1:#0f1b30; --wm-ocean-2:#0a1120; --wm-grid:#2a3a58;
  --wm-land:#243450; --wm-land-line:#3a527a;
}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--ink);
  font-family:"Segoe UI","PingFang SC","Microsoft YaHei",Verdana,sans-serif;font-size:14px;line-height:1.55}
button,input,select,textarea{font:inherit;color:inherit}
a{color:var(--accent)}
/* 统一可见焦点：绝不移除 outline，聚焦元素 2px 描边 */
:focus-visible{outline:2px solid var(--accent);outline-offset:2px;border-radius:4px}

/* ===== 布局骨架：左侧边栏 + 右主区 ===== */
.app{min-height:100vh}
.sidebar{position:fixed;top:0;left:0;bottom:0;width:var(--sidebar-w);z-index:40;
  background:var(--panel);border-right:1px solid var(--line);
  display:flex;flex-direction:column;
  transition:width var(--t-micro) var(--ease),transform var(--t-panel) var(--ease)}
.sidebar.preload{transition:none}
.sidebar-brand{display:flex;align-items:center;gap:8px;height:var(--topbar-h);padding:0 16px;
  border-bottom:1px solid var(--line);overflow:hidden}
.mark{flex:0 0 auto;width:32px;height:32px;border-radius:8px;background:var(--accent);color:var(--accent-ink);
  display:grid;place-items:center;font-weight:800;font-size:13px}
.sidebar-brand .bt{min-width:0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;font-weight:800;font-size:15px;
  transition:opacity var(--t-micro) var(--ease)}
.sidebar-nav{flex:1;padding:8px;display:flex;flex-direction:column;gap:4px;overflow-y:auto}
.navitem{display:flex;align-items:center;gap:12px;width:100%;padding:10px 12px;border-radius:10px;
  background:none;border:0;cursor:pointer;color:var(--muted);font-weight:600;text-align:left;white-space:nowrap;
  transition:background var(--t-micro) var(--ease),color var(--t-micro) var(--ease)}
.navitem .ico{flex:0 0 auto;width:20px;height:20px;display:grid;place-items:center}
.navitem .ico svg{width:20px;height:20px;display:block}
.navitem .lbl{min-width:0;overflow:hidden;text-overflow:ellipsis;transition:opacity var(--t-micro) var(--ease)}
.navitem:hover{background:var(--soft);color:var(--ink)}
.navitem.active{background:color-mix(in srgb,var(--accent) 14%,transparent);color:var(--accent)}
.sidebar-foot{padding:8px;border-top:1px solid var(--line);display:flex;flex-direction:column;gap:4px}
.sidebar-foot .btn{display:flex;align-items:center;gap:12px;width:100%;justify-content:flex-start;white-space:nowrap}
.sidebar-foot .btn .ico{flex:0 0 auto;width:20px;height:20px;display:grid;place-items:center}
.sidebar-foot .btn .lbl{min-width:0;overflow:hidden;text-overflow:ellipsis;transition:opacity var(--t-micro) var(--ease)}

/* 侧边栏内显式折叠开关：整行、图标+文字，PC 常显；折叠态收成居中图标并翻转箭头 */
.sidebar-collapse{display:flex;align-items:center;gap:12px;width:calc(100% - 16px);margin:4px 8px;padding:10px 12px;
  border:1px dashed var(--line);border-radius:10px;background:none;cursor:pointer;color:var(--muted);
  font-weight:600;text-align:left;white-space:nowrap;
  transition:background var(--t-micro) var(--ease),color var(--t-micro) var(--ease),border-color var(--t-micro) var(--ease)}
.sidebar-collapse:hover{background:var(--soft);color:var(--ink);border-color:var(--accent)}
.sidebar-collapse .ico{flex:0 0 auto;width:20px;height:20px;display:grid;place-items:center;
  transition:transform var(--t-panel) var(--ease)}
.sidebar-collapse .ico svg{width:20px;height:20px;display:block}
.sidebar-collapse .lbl{min-width:0;overflow:hidden;text-overflow:ellipsis;transition:opacity var(--t-micro) var(--ease)}

/* 折叠态：仅图标，文字淡出隐藏（文字语义走 title + aria-label） */
body.sidebar-collapsed .sidebar{width:var(--sidebar-w-collapsed)}
body.sidebar-collapsed .navitem .lbl,
body.sidebar-collapsed .sidebar-foot .btn .lbl,
body.sidebar-collapsed .sidebar-collapse .lbl,
body.sidebar-collapsed .sidebar-brand .bt{opacity:0;width:0;pointer-events:none}
body.sidebar-collapsed .navitem,
body.sidebar-collapsed .sidebar-collapse,
body.sidebar-collapsed .sidebar-foot .btn{justify-content:center;gap:0;padding-left:0;padding-right:0}
body.sidebar-collapsed .sidebar-collapse{width:calc(100% - 16px)}
/* 折叠时箭头指向右（展开方向），语义随状态翻转 */
body.sidebar-collapsed .sidebar-collapse .ico{transform:rotate(180deg)}

/* 主区随侧边栏宽度让位 */
.main{margin-left:var(--sidebar-w);transition:margin-left var(--t-micro) var(--ease)}
body.sidebar-collapsed .main{margin-left:var(--sidebar-w-collapsed)}

/* ===== 顶栏 ===== */
.topbar{position:sticky;top:0;z-index:30;height:var(--topbar-h);background:var(--panel);
  border-bottom:1px solid var(--line);display:flex;align-items:center;gap:12px;padding:0 20px}
.hamburger{display:none;width:36px;height:36px;border:1px solid var(--line);background:var(--panel);
  color:var(--muted);border-radius:8px;cursor:pointer;align-items:center;justify-content:center;
  transition:border-color var(--t-micro) var(--ease),color var(--t-micro) var(--ease),background var(--t-micro) var(--ease),transform var(--t-micro) var(--ease)}
.hamburger:hover{border-color:var(--accent);color:var(--accent);background:color-mix(in srgb,var(--accent) 12%,var(--panel))}
.hamburger:active{transform:scale(.96)}
.sidebar-toggle{width:36px;height:36px;border:1px solid var(--line);background:var(--panel);
  border-radius:8px;cursor:pointer;display:inline-flex;align-items:center;justify-content:center;color:var(--muted);
  transition:border-color var(--t-micro) var(--ease),color var(--t-micro) var(--ease),background var(--t-micro) var(--ease),transform var(--t-micro) var(--ease)}
.sidebar-toggle:hover{border-color:var(--accent);color:var(--accent);background:color-mix(in srgb,var(--accent) 12%,var(--panel))}
.sidebar-toggle:active{transform:scale(.96)}
.topbar .status-pill{margin-left:4px}
.topbar-spacer{flex:1}
.topbar .actions{display:flex;align-items:center;gap:8px}
.iconlink{display:inline-flex;align-items:center;justify-content:center;width:36px;height:36px;
  border:1px solid var(--line);border-radius:8px;color:var(--muted);text-decoration:none;background:var(--panel);
  transition:border-color var(--t-micro) var(--ease),color var(--t-micro) var(--ease),background var(--t-micro) var(--ease),transform var(--t-micro) var(--ease)}
.iconlink:hover{border-color:var(--accent);color:var(--accent);background:color-mix(in srgb,var(--accent) 12%,var(--panel))}
.iconlink:active{transform:scale(.96)}
.iconlink svg{width:20px;height:20px;display:block}

.btn{border:1px solid var(--line);background:var(--panel);border-radius:10px;padding:8px 14px;
  cursor:pointer;text-decoration:none;font-weight:600;white-space:nowrap;color:var(--ink);
  transition:border-color var(--t-micro) var(--ease),background var(--t-micro) var(--ease)}
.btn:hover{border-color:var(--accent)}
.btn.primary{background:var(--accent);border-color:var(--accent);color:var(--accent-ink)}
.btn.danger{color:var(--danger)}

.wrap{max-width:1320px;margin:0 auto;padding:24px}
.page{display:none}
.page.active{display:block}

/* ===== 遮罩（移动端抽屉） ===== */
.scrim{position:fixed;inset:0;background:rgba(12,18,30,.5);z-index:35;opacity:0;pointer-events:none;
  transition:opacity var(--t-panel) var(--ease)}
body.drawer-open .scrim{opacity:1;pointer-events:auto}

/* ===== 指标卡 ===== */
.metrics{display:grid;grid-template-columns:repeat(auto-fit,minmax(152px,1fr));gap:16px;margin-bottom:24px}
.metric{background:var(--panel);border:1px solid var(--line);border-radius:var(--radius);padding:16px;box-shadow:var(--shadow)}
.metric .label{font-size:12px;color:var(--muted);font-weight:600}
.metric .value{font-size:28px;font-weight:800;margin:4px 0;letter-spacing:-.02em}
.metric .note{font-size:11px;color:var(--muted)}

.card{background:var(--panel);border:1px solid var(--line);border-radius:var(--radius);box-shadow:var(--shadow);margin-bottom:16px;overflow:hidden}
.card-head{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:16px;border-bottom:1px solid var(--line)}
.card-head h3{margin:0;font-size:15px;letter-spacing:-.01em}
.card-head .tools{display:flex;gap:8px;flex-wrap:wrap;align-items:center}
.card-body{padding:16px}
.two-col{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:16px;align-items:start}
@media(max-width:900px){.two-col{grid-template-columns:1fr}}

/* ===== 连接指引 ===== */
.conn{display:grid;grid-template-columns:repeat(auto-fit,minmax(224px,1fr));gap:16px}
.conn-item{background:var(--soft);border:1px solid var(--line);border-radius:12px;padding:16px}
.conn-item .k{font-size:11px;text-transform:uppercase;letter-spacing:.06em;color:var(--muted);font-weight:700}
.conn-item .v{font-family:"Consolas",monospace;font-size:15px;font-weight:700;margin-top:8px;word-break:break-all}
.conn-item .desc{font-size:12px;color:var(--muted);margin-top:4px}
.cmd{background:#0f1626;color:#cdd8ec;border-radius:10px;padding:16px;font-family:"Consolas",monospace;
  font-size:13px;overflow-x:auto;white-space:pre;margin-top:16px}
.cmd-hint{font-size:12px;color:var(--muted);line-height:1.7;margin-top:8px}
.cmd-hint code{font-family:"Consolas",monospace;font-size:12px;background:var(--soft);color:var(--accent);
  padding:1px 6px;border-radius:6px;border:1px solid var(--line)}
.cmd-hint b{color:var(--ink)}
.notice{display:flex;gap:8px;align-items:flex-start;background:var(--soft);border-left:3px solid var(--warn);
  border-radius:8px;padding:12px;font-size:13px;color:var(--muted);margin-top:16px}

.guide-row{display:flex;flex-wrap:wrap;gap:8px;font-family:"Consolas",monospace;font-size:13px;
  background:var(--soft);border-radius:8px;padding:8px 12px;margin-bottom:8px}
.guide-row b{color:var(--accent)}.guide-row span{color:var(--muted)}
.hint{font-size:12px;color:var(--muted);margin-top:8px}
.code-block{background:#0f1626;color:#cdd8ec;border-radius:10px;padding:16px;font-family:"Consolas",monospace;
  font-size:13px;overflow-x:auto;white-space:pre;margin:8px 0 0}

.toolbar{display:flex;flex-wrap:wrap;gap:8px;margin-bottom:16px}
.input{border:1px solid var(--line);border-radius:8px;padding:8px 12px;background:var(--panel);min-width:0;
  transition:border-color var(--t-micro) var(--ease)}
.input:focus{outline:none;border-color:var(--accent)}
.grow{flex:1;min-width:152px}

.table-wrap{width:100%;overflow-x:auto}
table{width:100%;border-collapse:collapse;font-size:13px}
th,td{padding:8px 10px;text-align:left;border-bottom:1px solid var(--line);white-space:nowrap;vertical-align:middle}
th{font-size:11px;text-transform:uppercase;letter-spacing:.04em;color:var(--muted);font-weight:700}
tbody tr:last-child td{border-bottom:none}
tbody tr:hover{background:color-mix(in srgb,var(--soft) 88%,transparent)}
.mono{font-family:"Consolas",monospace}
.empty{text-align:center;color:var(--muted);padding:24px 0}

.badge{display:inline-block;padding:2px 8px;border-radius:999px;font-size:11px;font-weight:700;background:var(--soft);color:var(--muted)}
.badge.ok{background:rgba(15,159,110,.14);color:var(--ok)}
.badge.blue{background:rgba(47,91,234,.13);color:var(--accent)}
.badge.warn{background:rgba(201,138,18,.15);color:var(--warn)}
.badge.danger{background:rgba(214,69,69,.14);color:var(--danger)}
.badge.gray{background:var(--soft);color:var(--gray)}
.muted{color:var(--muted)}

/* ===== 表头图标（CF / AI 统一图标语言：图标 + 短标签） ===== */
.th-ico{display:inline-flex;align-items:center;gap:5px;color:var(--muted);cursor:default}
.th-ico svg{width:15px;height:15px;display:block;flex:0 0 auto}
.th-ico .tx{font-size:var(--fs-caption,11px);font-weight:700;letter-spacing:.04em}

/* ===== AI 可达性标记（4 服务：✓ 可达 / ✗ 不可达 / – 未探测） ===== */
.ai-marks{display:inline-flex;gap:4px;flex-wrap:wrap;vertical-align:middle}
.ai-mark{display:inline-flex;flex-direction:column;align-items:center;justify-content:center;
  min-width:26px;padding:2px 4px;border-radius:6px;border:1px solid var(--line);
  background:var(--surface-2,var(--soft));line-height:1.1;cursor:default;
  transition:border-color var(--t-fast,150ms) var(--ease-out,ease),background var(--t-fast,150ms) var(--ease-out,ease)}
.ai-mark .nm{font-size:9px;font-weight:700;letter-spacing:.02em;color:var(--ink-3,var(--muted))}
.ai-mark .gl{font-size:11px;font-weight:900;line-height:1}
.ai-mark.ok{border-color:color-mix(in srgb,var(--ok) 40%,transparent);background:rgba(18,161,80,.10)}
.ai-mark.ok .gl{color:var(--ok)}
.ai-mark.ok .nm{color:var(--ok)}
.ai-mark.bad{border-color:color-mix(in srgb,var(--danger) 40%,transparent);background:rgba(229,72,77,.10)}
.ai-mark.bad .gl{color:var(--danger)}
.ai-mark.bad .nm{color:var(--danger)}
.ai-mark.na{opacity:.6}
.ai-mark.na .gl{color:var(--gray)}

/* ===== 节点筛选栏：吸附 ===== */
.filter-toolbar{display:flex;flex-wrap:wrap;gap:8px;align-items:center;
  position:sticky;top:0;z-index:20;background:var(--panel);
  padding:12px 0;margin:0 0 12px;border-bottom:1px solid var(--line)}
.filter-toolbar .input{min-width:0}
.filter-toolbar .input.narrow{width:96px}
.filter-toolbar .input.mid{width:120px}
/* AI/CF 图标切换按钮（隐藏 select 承接原过滤语义） */
.hidden-select{position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0 0 0 0);border:0}
.filter-toggle{display:inline-flex;align-items:center;gap:6px;height:36px;padding:0 10px;border:1px solid var(--line);
  border-radius:8px;background:var(--panel);cursor:pointer;color:var(--muted);font-size:12px;font-weight:700;
  transition:border-color var(--t-micro) var(--ease),color var(--t-micro) var(--ease)}
.filter-toggle:hover{border-color:var(--accent)}
.filter-toggle[aria-pressed="true"]{border-color:var(--accent);color:var(--accent)}
.filter-toggle .ico{width:16px;height:16px;display:grid;place-items:center}
.filter-toggle .ico svg{width:16px;height:16px;display:block}
.filter-toggle .st{font-size:11px}
.check{display:inline-flex;align-items:center;gap:8px;font-size:12px;color:var(--muted);font-weight:600;cursor:pointer;user-select:none}
.check input{accent-color:var(--accent)}
.status-pill{display:inline-flex;align-items:center;gap:8px;font-size:12px;font-weight:700;color:var(--muted);letter-spacing:.04em}
.status-pill .dot{margin:0}

.mini{border:1px solid var(--line);background:var(--panel);border-radius:8px;padding:5px 10px;cursor:pointer;font-size:12px;font-weight:600;color:var(--ink);
  transition:border-color var(--t-micro) var(--ease)}
.mini:hover{border-color:var(--accent)}
.mini.primary{background:var(--accent);border-color:var(--accent);color:var(--accent-ink)}
.mini.danger{color:var(--danger)}

.region-row{display:flex;align-items:center;gap:12px;padding:8px 0}
.region-row strong{width:40px;font-size:13px}
.bar{flex:1;height:8px;background:var(--soft);border-radius:999px;overflow:hidden}
.bar span{display:block;height:100%;background:var(--accent);border-radius:999px}
.region-row .cnt{width:40px;text-align:right;color:var(--muted);font-size:13px}

.kv{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:8px 0;border-bottom:1px solid var(--line);font-size:13px}
.kv:last-child{border-bottom:none}
.kv .k{color:var(--muted)}.kv .v{font-weight:700}
.dot{display:inline-block;width:8px;height:8px;border-radius:50%;margin-right:8px;vertical-align:middle}
.dot.on{background:var(--ok)}.dot.off{background:var(--danger)}.dot.warn{background:var(--warn)}.dot.idle{background:var(--gray)}

.sub-item{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:12px 0;border-bottom:1px solid var(--line);flex-wrap:wrap}
.sub-item:last-child{border-bottom:none}
.sub-item .meta{min-width:0}.sub-item .meta strong{display:block}
.sub-item .meta .muted{font-size:12px;color:var(--muted)}
.mini-actions{display:flex;gap:8px;flex-wrap:wrap}

.session-list{display:grid;grid-template-columns:repeat(auto-fill,minmax(264px,1fr));gap:12px}
.session-card{border:1px solid var(--line);border-radius:12px;padding:12px}
.session-card .top{display:flex;align-items:center;justify-content:space-between;gap:8px}
.session-card .sid{font-family:"Consolas",monospace;font-weight:700;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.session-card .ttl{font-size:12px;color:var(--ok);font-weight:700}
.session-card .node{font-family:"Consolas",monospace;font-size:12px;color:var(--muted);margin-top:8px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}

.logs{background:#0f1626;color:#c9d4e6;border-radius:12px;padding:12px;font-family:"Consolas",monospace;
  font-size:12px;line-height:1.5;max-height:420px;overflow:auto;white-space:pre-wrap;word-break:break-all}
.log-line{padding:1px 0}
.legend{display:flex;gap:16px;flex-wrap:wrap;font-size:12px;color:var(--muted);margin-bottom:12px}
.legend span{display:flex;align-items:center;gap:6px}

/* ===== 骨架墓碑（shimmer） ===== */
.skeleton{display:block;background:linear-gradient(90deg,var(--soft) 25%,color-mix(in srgb,var(--line) 60%,var(--soft)) 37%,var(--soft) 63%);
  background-size:400% 100%;border-radius:8px;animation:sk-shimmer 1.4s ease infinite}
.sk-line{height:12px;margin:8px 0}
.sk-row{height:40px;margin:8px 0;border-radius:10px}
@keyframes sk-shimmer{0%{background-position:100% 0}100%{background-position:0 0}}
.skeleton-wrap{padding:8px 0}

.modal{position:fixed;inset:0;background:rgba(12,18,30,.5);display:none;align-items:flex-start;justify-content:center;padding:44px 16px;z-index:60;overflow:auto}
.modal.show{display:flex}
.dialog{background:var(--panel);border-radius:var(--radius);width:min(560px,100%);padding:24px;box-shadow:0 30px 80px rgba(10,16,30,.4)}
.dialog h3{margin:0 0 16px}
.form-grid{display:grid;grid-template-columns:1fr 1fr;gap:16px}
.field{display:flex;flex-direction:column;gap:8px}
.field.full{grid-column:1 / -1}
.field label{font-size:12px;color:var(--muted);font-weight:600}
.field input,.field select,.field textarea{border:1px solid var(--line);border-radius:8px;padding:8px 12px;background:var(--panel);width:100%}
.field textarea{min-height:120px;resize:vertical;font-family:"Consolas",monospace}
.field .fh{font-size:11px;color:var(--muted)}
.dialog-actions{display:flex;justify-content:flex-end;gap:8px;margin-top:24px}
.apikey-section{margin-top:24px;border-top:1px solid var(--line);padding-top:16px}
.apikey-section h4{margin:0 0 8px}

.toast{position:fixed;left:50%;bottom:24px;transform:translateX(-50%) translateY(20px);background:var(--ink);
  color:var(--bg);padding:12px 20px;border-radius:999px;font-weight:600;opacity:0;pointer-events:none;
  transition:opacity var(--t-panel) var(--ease),transform var(--t-panel) var(--ease);z-index:70}
.toast.show{opacity:1;transform:translateX(-50%) translateY(0)}

/* ===== 世界地图（class 契约与 keyframes 保持不变，勿改动画名/数值） ===== */
.worldmap-svg{width:100%;height:auto;display:block;border-radius:12px;overflow:hidden;border:1px solid var(--line)}
.worldmap-ocean{fill:url(#wm-ocean)}
.worldmap-grid line{stroke:var(--wm-grid);stroke-width:.5;opacity:.5}
.worldmap-land{fill:var(--wm-land);stroke:var(--wm-land-line);stroke-width:.6;opacity:.9}
.worldmap-gw{fill:var(--ok);stroke:#fff;stroke-width:1.2}
.worldmap-gw-pulse{fill:var(--ok);opacity:.35;animation:wm-gw-pulse 2s ease-out infinite}
@keyframes wm-gw-pulse{0%{r:5;opacity:.5}100%{r:16;opacity:0}}
.worldmap-dot{fill:var(--accent);animation:wm-pulse 2.4s ease-in-out infinite;filter:drop-shadow(0 0 4px var(--accent))}
@keyframes wm-pulse{0%,100%{opacity:.4}50%{opacity:1}}
.worldmap-link{fill:none;stroke:var(--signal);stroke-width:1.5;stroke-dasharray:6 6;opacity:.8;animation:wm-flow 1s linear infinite}
@keyframes wm-flow{to{stroke-dashoffset:-24}}

/* ===== 响应式：≤900px 侧边栏改 off-canvas 抽屉 ===== */
@media(max-width:900px){
  .sidebar{transform:translateX(-100%)}
  body.drawer-open .sidebar{transform:translateX(0);width:var(--sidebar-w)}
  body.drawer-open .navitem .lbl,
  body.drawer-open .sidebar-foot .btn .lbl,
  body.drawer-open .sidebar-brand .bt{opacity:1;width:auto}
  .main{margin-left:0}
  body.sidebar-collapsed .main{margin-left:0}
  .hamburger{display:inline-flex}
  .sidebar-toggle{display:none}
  .sidebar-collapse{display:none}
  .wrap{padding:16px}
}

/* ===== 尊重减少动效偏好：关闭非必要动画 ===== */
@media (prefers-reduced-motion:reduce){
  *{animation-duration:.001ms !important;animation-iteration-count:1 !important;transition-duration:.001ms !important}
  .worldmap-dot,.worldmap-gw-pulse,.worldmap-link,.skeleton{animation:none}
}
/* 筛选切换按钮内的服务名与状态点 */
.filter-toggle .nm{font-weight:700;color:var(--ink)}
.filter-toggle[aria-pressed="true"] .nm{color:var(--accent)}
.fdot{width:8px;height:8px;border-radius:50%;background:var(--gray);flex:0 0 auto}
.filter-toggle[aria-pressed="true"] .fdot{background:var(--accent)}`

const dashboardJS = `let allProxies=[];let allRegions=[];let configCache=null;let publicIP='';let worldMapSessions=[];let gatewayCC='';
const COUNTRY_XY={us:[228,142],ca:[206,94],mx:[195,190],br:[330,320],ar:[300,400],gb:[496,100],ie:[482,104],fr:[506,121],de:[529,108],nl:[515,106],es:[490,140],it:[532,133],se:[540,80],ch:[520,122],pl:[548,104],ru:[750,81],tr:[590,145],ae:[650,190],in:[719,193],cn:[790,150],hk:[817,189],tw:[836,184],jp:[883,150],kr:[855,149],sg:[788,246],my:[785,240],th:[770,210],vn:[795,215],id:[820,270],ph:[850,215],au:[869,319],nz:[955,380],za:[560,360],ng:[520,230],eg:[585,175]};
function switchTab(name){document.querySelectorAll('.navitem').forEach(t=>t.classList.toggle('active',t.dataset.tab===name));document.querySelectorAll('.page').forEach(p=>p.classList.toggle('active',p.id==='page-'+name));try{markViewLazy(name)}catch(e){}closeDrawer()}
function showToast(msg){const el=document.getElementById('toast');el.textContent=msg;el.classList.add('show');setTimeout(()=>el.classList.remove('show'),2600)}
async function api(path, options){const res=await fetch(path, Object.assign({headers:{'Content-Type':'application/json'}}, options||{}));if(res.status===401){location.href='/login';return null}const text=await res.text();let data={};if(text){try{data=JSON.parse(text)}catch(err){if(!res.ok)throw new Error(res.statusText||('HTTP '+res.status));throw new Error('响应解析失败')}}if(!res.ok)throw new Error(data.error||res.statusText||('HTTP '+res.status));return data}
function safe(value){return value===undefined||value===null||value===''?'--':String(value)}
function html(value){return safe(value).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
function errorMessage(err){return err&&err.message?err.message:String(err||'操作失败')}
async function runAsync(label, fn){try{return await fn()}catch(err){showToast((label?label+'：':'')+errorMessage(err));return null}}
async function logout(){return runAsync('退出失败',async()=>{const res=await fetch('/logout',{method:'POST'});if(!res.ok)throw new Error(res.statusText||('HTTP '+res.status));location.href='/login'})}
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
function applyTheme(theme){document.documentElement.setAttribute('data-theme',theme);try{localStorage.setItem('gg-theme',theme)}catch(e){}const btn=document.getElementById('theme-toggle');if(btn){const lbl=btn.querySelector('.lbl')||btn;lbl.textContent=theme==='dark'?'☀ 浅色':'🌙 深色'}}
function toggleTheme(){const cur=document.documentElement.getAttribute('data-theme')==='dark'?'dark':'light';applyTheme(cur==='dark'?'light':'dark')}
(function(){let t='light';try{t=localStorage.getItem('gg-theme')||'light'}catch(e){}applyTheme(t)})();
async function loadStats(){const stats=await api('/api/stats');if(!stats)return;document.getElementById('stat-total').textContent=safe(stats.total);document.getElementById('stat-http').textContent=safe(stats.http);document.getElementById('stat-socks5').textContent=safe(stats.socks5);document.getElementById('stat-subscription').textContent=safe(stats.subscription_count);document.getElementById('stat-sessions').textContent=safe(stats.active_sessions)}
async function loadProxies(){const data=await api('/api/proxies');if(!data)return;allProxies=Array.isArray(data)?data:[];allRegions=Array.from(new Set(allProxies.filter(p=>isAvailable(p)&&isKnownRegion(p)).map(regionOf))).sort();renderRegionFilter();renderProxies();renderRegions();renderWorldMap()}
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
// aiBadges: 解析 ai_reachability JSON（形如 {"openai":0,"claude":1,"grok":-1,"gemini":0}），
// 为 4 个服务各渲染一枚状态标记：短名(GPT/Cld/Grk/Gem) + 字形。0=可达(✓绿)、1=不可达(✗红)、
// 其它(-1/缺失/未探测)=未探测(–灰)。title 带服务全名与状态。空/非法 JSON 整体显 "--"。纯字符，无外部图片/SVG。
function aiBadges(v){ const raw=String(v||'').trim(); if(raw===''){return '<span class="muted">--</span>'} let m; try{m=JSON.parse(raw)}catch(e){return '<span class="muted">--</span>'} if(!m||typeof m!=='object'){return '<span class="muted">--</span>'} const defs=[['openai','GPT','OpenAI'],['claude','Cld','Claude'],['grok','Grk','Grok'],['gemini','Gem','Gemini']]; return '<span class="ai-marks">'+defs.map(function(d){const k=d[0],ab=d[1],full=d[2];const n=Number(m[k]);const cls=n===0?'ok':(n===1?'bad':'na');const glyph=n===0?'✓':(n===1?'✗':'–');const title=full+(n===0?' 可达':(n===1?' 不可达':' 未探测'));return '<span class="ai-mark '+cls+'" title="'+html(title)+'"><span class="nm">'+ab+'</span><span class="gl">'+glyph+'</span></span>'}).join('')+'</span>' }
function aiStateOf(p,svc){const raw=String((p&&p.ai_reachability)||'').trim();if(!raw)return 'unprobed';let m;try{m=JSON.parse(raw)}catch(e){return 'unprobed'}if(!m||typeof m!=='object')return 'unprobed';const n=Number(m[svc]);if(n===0)return 'unlocked';if(n===1)return 'blocked';return 'unprobed'}
function cfStateOf(p){const v=Number(p&&p.cf_blocked);if(v===0)return 'unlocked';if(v===1)return 'blocked';return 'unknown'}
function qualityOf(p){return String((p&&p.quality_grade)||'').trim().toUpperCase()}
function filterVal(id){const el=document.getElementById(id);return el?String(el.value||'').trim():''}
// starBtn: 星标切换按钮，★ 已加星 / ☆ 未加星。
function starBtn(p){ const id=proxyIDArg(p); const on=!!(p.starred===true||Number(p.starred)===1); return '<button class="mini" onclick="toggleStar('+id+','+(on?'true':'false')+')" title="星标">'+(on?'★':'☆')+'</button>' }
// randSession: 随机 6 位字母数字，用于复制凭据的 session 段。
function randSession(){ const cs='abcdefghijklmnopqrstuvwxyz0123456789'; let s=''; for(let i=0;i<6;i++)s+=cs[Math.floor(Math.random()*cs.length)]; return s }
// isDualProtocol: 节点是否为 sing-box mixed 入站(单端口同时服务 SOCKS5+HTTP)。
// 读存储层显式下发的 dual_protocol 字段,而非靠地址长相猜测——手动本机 direct socks5 节点
// 地址同为回环但只支持单协议,只有此显式标记能可靠区分。
function isDualProtocol(p){return !!(p&&(p.dual_protocol===true||Number(p.dual_protocol)===1))}
// protocolBadges: 协议列徽章。dual_protocol 节点(mixed 入站)渲染 SOCKS5+HTTP 两个徽章;
// 其余节点按存储的单一 protocol 渲染一个徽章(沿用 html 转义)。
function protocolBadges(p){ if(isDualProtocol(p))return '<span class="badge blue">SOCKS5</span> <span class="badge blue">HTTP</span>'; return '<span class="badge blue">'+html(p.protocol).toUpperCase()+'</span>' }
// isGatewayNode: dual_protocol(mixed 隧道)或回环本地地址必须经网关 DSL 连接；其余为可直连上游。
function isGatewayNode(p){if(isDualProtocol(p))return true;const a=String((p&&p.address)||'');return a.indexOf('127.0.0.1:')===0||a.indexOf('[::1]:')===0||a.indexOf('localhost:')===0}
function isDirectNode(p){return !isGatewayNode(p)}
// copyProxyCred: 直连节点复制 protocol://host:port（无网关密码）；网关节点复制 DSL 凭据到公网入口。
// 用户名/密码编码为 URL userinfo。成功 toast 不回显含真实密码的完整 URL。
function encodeProxyUserInfo(value){return encodeURIComponent(String(value||'')).replace(/[!'()*]/g,c=>'%'+c.charCodeAt(0).toString(16).toUpperCase())}
function copyProxyCred(id){ const p=allProxies.find(x=>Number(x.id)===Number(id)); if(!p)return; const addr=String(p.address||''); const scheme=isDualProtocol(p)?(confirm('确定复制 SOCKS5？取消则复制 HTTP')?'socks5':'http'):String(p.protocol||'socks5'); if(isDirectNode(p)){ const url=scheme+'://'+addr; navigator.clipboard.writeText(url).then(()=>showToast('已复制直连地址')).catch(()=>showToast('复制失败')); return } const base=(configCache&&configCache.proxy_auth_username)?configCache.proxy_auth_username:'acct'; const region=regionOf(p); const user=base+'-region-'+region+'-session-'+randSession(); const rawPass=(configCache&&configCache.proxy_auth_password)?configCache.proxy_auth_password:''; const pass=rawPass||'PASSWORD'; const host=publicIP||location.hostname||'127.0.0.1'; const port=scheme==='http'?(stripColon((configCache&&configCache.http_port)||'7802')):(stripColon((configCache&&configCache.socks5_port)||'7801')); const url=scheme+'://'+encodeProxyUserInfo(user)+':'+encodeProxyUserInfo(pass)+'@'+host+':'+port; const okMsg=rawPass?'已复制':'已复制，请将 PASSWORD 替换为真实密码'; navigator.clipboard.writeText(url).then(()=>showToast(okMsg)).catch(()=>showToast('复制失败')) }
// toggleStar: 加星直接生效；取消星标须 confirm() 确认。
async function toggleStar(id,on){ if(on){ if(!confirm('取消该节点星标？'))return } return runAsync('星标操作失败',async()=>{ await api('/api/proxy/star',{method:'POST',body:JSON.stringify({id,starred:!on})}); await loadProxies(); showToast(on?'已取消星标':'已加星标') }) }
function renderProxies(){const protocol=document.getElementById('protocol-filter').value;const region=document.getElementById('region-filter').value;const sf=document.getElementById('status-filter').value;const srcf=(document.getElementById('source-filter')||{}).value||'';const qf=filterVal('quality-filter');const cff=filterVal('cf-filter');const aif={openai:filterVal('ai-openai-filter'),claude:filterVal('ai-claude-filter'),grok:filterVal('ai-grok-filter'),gemini:filterVal('ai-gemini-filter')};const latMinRaw=filterVal('latency-min');const latMaxRaw=filterVal('latency-max');const latMin=latMinRaw===''?null:Number(latMinRaw);const latMax=latMaxRaw===''?null:Number(latMaxRaw);const kw=filterVal('keyword-filter').toLowerCase();let rows=allProxies.filter(p=>(!protocol||p.protocol===protocol)&&(!region||regionOf(p)===region));if(sf)rows=rows.filter(p=>nodeState(p)===sf);if(srcf==='manual')rows=rows.filter(p=>p.source==='manual');else if(srcf==='subscription')rows=rows.filter(p=>p.source!=='manual');if(qf)rows=rows.filter(p=>qualityOf(p)===qf);if(cff)rows=rows.filter(p=>cfStateOf(p)===cff);['openai','claude','grok','gemini'].forEach(function(svc){const v=aif[svc];if(v)rows=rows.filter(p=>aiStateOf(p,svc)===v)});if(latMin!==null&&Number.isFinite(latMin))rows=rows.filter(p=>Number(p.latency||0)>=latMin);if(latMax!==null&&Number.isFinite(latMax))rows=rows.filter(p=>Number(p.latency||0)<=latMax);if(kw)rows=rows.filter(p=>{const addr=String(p.address||'').toLowerCase();const note=String(p.note||'').toLowerCase();return addr.indexOf(kw)>=0||note.indexOf(kw)>=0});const order={ok:0,pending:1,paused:2,failed:3};rows.sort((a,b)=>{const fa=(nodeState(a)==='ok'&&(a.starred===true||Number(a.starred)===1))?1:0;const fb=(nodeState(b)==='ok'&&(b.starred===true||Number(b.starred)===1))?1:0;if(fa!==fb)return fb-fa;const sa=nodeState(a),sb=nodeState(b);if(order[sa]!==order[sb])return order[sa]-order[sb];return Number(a.latency||1e9)-Number(b.latency||1e9)});const body=document.getElementById('proxy-rows');if(rows.length===0){body.innerHTML='<tr><td colspan="14" class="empty">没有匹配节点</td></tr>';return}proxyRenderRows=rows;proxyRenderCount=0;renderProxyBatch()}
function renderRegions(){const counts={};allProxies.filter(p=>isAvailable(p)&&isKnownRegion(p)).forEach(p=>{const r=regionOf(p);counts[r]=(counts[r]||0)+1});const entries=Object.keys(counts).sort().map(region=>({region,count:counts[region]}));const total=entries.reduce((sum,item)=>sum+item.count,0);document.getElementById('region-total').textContent=total+' available nodes';const list=document.getElementById('region-list');if(entries.length===0){list.innerHTML='<div class="empty">暂无可用地域数据</div>';return}list.innerHTML=entries.map(item=>{const pct=total?Math.round(item.count*100/total):0;return '<div class="region-row"><strong>'+html(item.region).toUpperCase()+'</strong><div class="bar"><span style="width:'+pct+'%"></span></div><span class="cnt">'+html(item.count)+'</span></div>'}).join('')}
// renderWorldMap: 重绘"全球节点分布"。亮点=按 isAvailable&&isKnownRegion 聚合的各国可用节点(半径随数量对数增长，CSS 脉冲闪烁)；
// 连线=每个活跃 session 按其 region 从地图中心(网关 500,250)画到该国坐标(CSS stroke-dasharray 流动)。真实数据，无节点/无 session 均不报错。
function renderWorldMap(){const dots=document.getElementById('worldmap-dots');const links=document.getElementById('worldmap-links');if(!dots||!links)return;const gwcc=String(gatewayCC||(configCache&&configCache.default_region)||'cn').toLowerCase();const gw=COUNTRY_XY[gwcc]||COUNTRY_XY.cn;const cx=gw[0],cy=gw[1];const wmCounts={};allProxies.filter(p=>isAvailable(p)&&isKnownRegion(p)).forEach(p=>{const r=regionOf(p);wmCounts[r]=(wmCounts[r]||0)+1});let dotHTML='';Object.keys(wmCounts).forEach(r=>{const xy=COUNTRY_XY[r];if(!xy)return;const c=wmCounts[r];const radius=(4+3*Math.log(1+c)).toFixed(1);dotHTML+='<circle class="worldmap-dot" cx="'+xy[0]+'" cy="'+xy[1]+'" r="'+radius+'"><title>'+html(r).toUpperCase()+': '+html(c)+' 节点</title></circle>'});dots.innerHTML=dotHTML;let linkHTML='';(Array.isArray(worldMapSessions)?worldMapSessions:[]).forEach(s=>{const r=String((s&&s.region)||'').trim().toLowerCase();const xy=COUNTRY_XY[r];if(!xy)return;const mx=(cx+xy[0])/2,my=(cy+xy[1])/2;const dx=xy[0]-cx,dy=xy[1]-cy;const qx=(mx-dy*0.18).toFixed(1),qy=(my+dx*0.18).toFixed(1);linkHTML+='<path class="worldmap-link" d="M'+cx+' '+cy+' Q'+qx+' '+qy+' '+xy[0]+' '+xy[1]+'"></path>'});links.innerHTML=linkHTML;dots.innerHTML+='<circle class="worldmap-gw-pulse" cx="'+cx+'" cy="'+cy+'" r="9"></circle><circle class="worldmap-gw" cx="'+cx+'" cy="'+cy+'" r="5"><title>网关</title></circle>'}
async function loadSessions(){const sessions=await api('/api/sessions');if(!sessions)return;worldMapSessions=Array.isArray(sessions)?sessions:[];renderWorldMap();const body=document.getElementById('session-rows');if(!Array.isArray(sessions)||sessions.length===0){body.innerHTML='<div class="empty">暂无活跃 session</div>';return}body.innerHTML=sessions.map(s=>{const masked=html(maskAddress(s.node));const region=String(s.region||'').trim().toLowerCase();const regionBadge=region&&region!=='unknown'?'<span class="badge ok">'+html(region).toUpperCase()+'</span> ':'<span class="badge gray">未知</span> ';return '<div class="session-card"><div class="top"><span class="sid" title="'+html(s.session_id)+'">'+html(s.session_id)+'</span><span class="ttl">'+html(formatTTL(s.remaining_ttl_seconds))+'</span></div><div class="node" title="'+masked+'">'+regionBadge+masked+'</div></div>'}).join('')}
function formatTTL(seconds){const value=Number(seconds)||0;const min=Math.floor(value/60);const sec=value%60;return min>0?min+'m '+sec+'s':sec+'s'}
async function addManualNode(){return runAsync('添加失败',async()=>{const payload={link:document.getElementById('manual-link').value.trim(),region:document.getElementById('manual-region').value.trim(),note:document.getElementById('manual-note').value.trim()};if(!payload.link){showToast('请填写节点链接');return}await api('/api/manual-node/add',{method:'POST',body:JSON.stringify(payload)});document.getElementById('manual-link').value='';document.getElementById('manual-region').value='';document.getElementById('manual-note').value='';await Promise.all([loadStats(),loadProxies()]);showToast('手工节点已添加')})}
function toggleSelectAll(on){document.querySelectorAll('.proxy-select').forEach(el=>{el.checked=!!on})}
function selectedProxyIDs(){return Array.from(document.querySelectorAll('.proxy-select:checked')).map(el=>Number(el.value)).filter(n=>Number.isFinite(n)&&n>0)}
async function batchDeleteSelected(){return runAsync('批量删除失败',async()=>{const ids=selectedProxyIDs();if(!ids.length){showToast('请先勾选手工节点');return}if(!confirm('删除选中的 '+ids.length+' 个手工节点？'))return;const r=await api('/api/manual-node/batch-delete',{method:'POST',body:JSON.stringify({ids})});await Promise.all([loadStats(),loadProxies()]);showToast('已删除 '+(r&&r.deleted!=null?r.deleted:ids.length)+' 个'+(r&&r.failed?('，失败 '+r.failed):''))})}
async function importManualNodes(){return runAsync('批量导入失败',async()=>{const text=document.getElementById('import-text').value;const region=document.getElementById('import-region').value.trim();const note=document.getElementById('import-note').value.trim();if(!String(text||'').trim()){showToast('请粘贴代理列表');return}const r=await api('/api/manual-node/import',{method:'POST',body:JSON.stringify({text,region,note})});document.getElementById('import-modal').classList.remove('show');document.getElementById('import-text').value='';await Promise.all([loadStats(),loadProxies()]);showToast('导入完成：新增 '+(r.added||0)+' / 跳过 '+(r.skipped||0)+' / 失败 '+(r.failed||0))})}
async function manageManualNode(id,address){return runAsync('管理失败',async()=>{const current=allProxies.find(p=>Number(p.id)===Number(id))||{};const choice=prompt('手工节点管理：输入 1=改地域，2=改备注，3=删除', '1');if(choice===null)return;if(choice==='1'){const region=prompt('地域',current.region||'');if(region===null)return;await api('/api/manual-node/region',{method:'POST',body:JSON.stringify({id,address,region})});await loadProxies();showToast('地域已更新');return}if(choice==='2'){const note=prompt('备注',current.note||'');if(note===null)return;await api('/api/manual-node/note',{method:'POST',body:JSON.stringify({id,address,note})});await loadProxies();showToast('备注已更新');return}if(choice==='3'){if(!confirm('删除此手工节点？'))return;await api('/api/manual-node/delete',{method:'POST',body:JSON.stringify({id,address})});await Promise.all([loadStats(),loadProxies()]);showToast('手工节点已删除')}})}
async function editManualRegion(id,address){return runAsync('地域更新失败',async()=>{const current=allProxies.find(p=>Number(p.id)===Number(id))||{};const region=prompt('地域',current.region||'');if(region===null)return;await api('/api/manual-node/region',{method:'POST',body:JSON.stringify({id,address,region})});await loadProxies();showToast('地域已更新')})}
async function editManualNote(id,address){return runAsync('备注更新失败',async()=>{const current=allProxies.find(p=>Number(p.id)===Number(id))||{};const note=prompt('备注',current.note||'');if(note===null)return;await api('/api/manual-node/note',{method:'POST',body:JSON.stringify({id,address,note})});await loadProxies();showToast('备注已更新')})}
async function deleteManualNode(id,address){return runAsync('删除失败',async()=>{if(!confirm('删除此手工节点？'))return;await api('/api/manual-node/delete',{method:'POST',body:JSON.stringify({id,address})});await Promise.all([loadStats(),loadProxies()]);showToast('手工节点已删除')})}
async function toggleProxy(id,address,enable){return runAsync('操作失败',async()=>{await api('/api/proxy/toggle',{method:'POST',body:JSON.stringify({id,address,enable})});await Promise.all([loadStats(),loadProxies()]);showToast(enable?'节点已启用':'节点已停用')})}
// testProxy: 触发单节点重新验证（走完整 ValidateOne，含连通 google/openai/github/cloudflare/gstatic），后端异步执行，稍后自动刷新列表。
async function testProxy(id,address){return runAsync('测试失败',async()=>{await api('/api/proxy/refresh',{method:'POST',body:JSON.stringify({id,address})});showToast('测试连通已启动，稍后自动刷新');setTimeout(()=>runAsync('刷新失败',()=>Promise.all([loadStats(),loadProxies()])),4000)})}
async function loadSubscriptions(){const subs=await api('/api/subscriptions');if(!subs)return;const box=document.getElementById('sub-list');if(!Array.isArray(subs)||subs.length===0){box.innerHTML='<div class="empty">暂无订阅，点右上角“添加订阅”</div>';return}box.innerHTML=subs.map(sub=>{const paused=sub.status==='paused';const activeCount=Number(sub.active_count||0);const disabledCount=Number(sub.disabled_count||0);const proxyCount=Number(sub.proxy_count||0);const pausedCount=Number(sub.paused_count??Math.max(0,proxyCount-activeCount-disabledCount));const toggleLabel=paused?'启用':'暂停';const badge=paused?'<span class="badge warn">已暂停</span>':'<span class="badge ok">活跃</span>';const id=Number(sub.id);const idArg=Number.isFinite(id)?String(id):'0';return '<div class="sub-item"><div class="meta"><strong>'+html(sub.name)+' '+badge+'</strong><div class="muted">'+html(activeCount)+' 可用 / '+html(pausedCount)+' 暂停 / '+html(disabledCount)+' 不可用</div></div><div class="mini-actions"><button class="mini" onclick="refreshSub('+idArg+')" title="重新拉取并验证">刷新</button><button class="mini" onclick="toggleSub('+idArg+')" title="启用或暂停该订阅及其节点">'+toggleLabel+'</button><button class="mini danger" onclick="deleteSub('+idArg+')" title="删除订阅及其节点">删除</button></div></div>'}).join('')}
function openSubModal(){document.getElementById('sub-modal').classList.add('show')}function closeSubModal(){document.getElementById('sub-modal').classList.remove('show')}
async function addSubscription(){return runAsync('添加失败',async()=>{const payload={name:document.getElementById('sub-name').value.trim(),url:document.getElementById('sub-url').value.trim(),file_content:document.getElementById('sub-file-content').value.trim(),headers:document.getElementById('sub-headers').value.trim(),refresh_min:Number(document.getElementById('sub-refresh').value)||60};if(!payload.url&&!payload.file_content){showToast('请填写订阅 URL 或粘贴配置内容');return}await api('/api/subscription/add',{method:'POST',body:JSON.stringify(payload)});document.getElementById('sub-name').value='';document.getElementById('sub-url').value='';document.getElementById('sub-file-content').value='';document.getElementById('sub-headers').value='';closeSubModal();await Promise.all([loadSubscriptions(),loadStats(),loadProxies()]);showToast('订阅已添加')})}
async function refreshSub(id){return runAsync('刷新失败',async()=>{await api('/api/subscription/refresh',{method:'POST',body:JSON.stringify({id})});showToast('刷新已启动，稍后自动更新');refreshLater()})}
async function refreshAllSubs(){return runAsync('刷新失败',async()=>{await api('/api/subscription/refresh-all',{method:'POST'});showToast('全部刷新已启动，稍后自动更新');refreshLater()})}
async function toggleSub(id){return runAsync('切换失败',async()=>{await api('/api/subscription/toggle',{method:'POST',body:JSON.stringify({id})});await Promise.all([loadSubscriptions(),loadStats(),loadProxies()]);showToast('已切换启用/暂停状态')})}
async function deleteSub(id){return runAsync('删除失败',async()=>{if(!confirm('删除此订阅及其全部节点？'))return;await api('/api/subscription/delete',{method:'POST',body:JSON.stringify({id})});await Promise.all([loadSubscriptions(),loadStats(),loadProxies()]);showToast('订阅已删除')})}
async function loadLogs(){const data=await api('/api/logs');if(!data)return;const box=document.getElementById('logs-box');if(!box)return;const prevTop=box.scrollTop;const lines=Array.isArray(data.lines)?data.lines:[];box.innerHTML=lines.length?lines.map(line=>'<div class="log-line">'+html(line)+'</div>').join(''):'<div class="log-line">no logs</div>';const auto=document.getElementById('logs-autoscroll');if(auto&&auto.checked){box.scrollTop=box.scrollHeight}else{box.scrollTop=prevTop}}
async function loadConfig(){configCache=await api('/api/config');if(!configCache)return;const hp=stripColon(configCache.http_port),sp=stripColon(configCache.socks5_port),wp=stripColon(configCache.webui_port);document.getElementById('cfg-http-port').value=hp;document.getElementById('cfg-socks5-port').value=sp;document.getElementById('cfg-webui-port').value=wp;document.getElementById('cfg-auth-enabled').value=String(Boolean(configCache.proxy_auth_enabled));document.getElementById('cfg-auth-username').value=configCache.proxy_auth_username||'';document.getElementById('cfg-auth-password').value='';document.getElementById('cfg-session-ttl').value=configCache.session_ttl_minutes||'';document.getElementById('cfg-default-region').value=configCache.default_region||'';document.getElementById('cfg-health-interval').value=configCache.health_check_interval||'';document.getElementById('cfg-max-retry').value=configCache.max_retry??'';document.getElementById('cfg-singbox-path').value=configCache.singbox_path||'';document.getElementById('cfg-allowed-countries').value=(configCache.allowed_countries||[]).join(',');document.getElementById('cfg-blocked-countries').value=(configCache.blocked_countries||[]).join(',');renderConnection();renderDSLExamples()}
async function loadPublicIP(){return runAsync('公网 IP 获取失败',async()=>{const d=await api('/api/public-ip');if(d){if(d.public_ip){publicIP=d.public_ip;renderConnection()}if(d.country){gatewayCC=String(d.country).toLowerCase()}renderWorldMap()}})}
function renderConnection(){if(!configCache)return;const sp=stripColon(configCache.socks5_port)||'7801';const hp=stripColon(configCache.http_port)||'7802';const base=configCache.proxy_auth_username||'acct';const enabled=configCache.proxy_auth_enabled;const host=publicIP||location.hostname||'127.0.0.1';document.getElementById('conn-socks5').textContent=host+':'+(sp||'7801');document.getElementById('conn-http').textContent=host+':'+(hp||'7802');document.getElementById('conn-user').textContent=base;document.getElementById('conn-pass').textContent=enabled?'见首次启动日志 / 系统设置':'（认证已关闭，无需密码）';document.getElementById('conn-auth-state').textContent=enabled?'代理认证：开启':'代理认证：关闭';const cred=enabled?(base+':PASSWORD@'):'';document.getElementById('conn-cmd').textContent='curl --socks5 '+cred+host+':'+(sp||'7801')+' https://www.gstatic.com/generate_204'}
function renderDSLExamples(){const base=(configCache&&configCache.proxy_auth_username)?configCache.proxy_auth_username:'acct';const box=document.getElementById('dsl-examples');if(box){box.innerHTML=['-region-us','-unlock-gpt','-region-jp-unlock-all-session-app01','-session-browser'].map(s=>'<div class="guide-row"><b>'+html(base)+'</b><span>'+html(s)+'</span></div>').join('')}const hint=document.getElementById('dsl-hint');if(hint){hint.textContent=(configCache&&configCache.proxy_auth_enabled)?('前缀 “'+base+'” = 代理认证用户名；-region-XX 地域；-unlock-gpt|claude|gemini|grok|cf|all 解锁过滤；-session-ID 黏连。'):'代理认证当前关闭；启用后前缀须等于代理认证用户名。'}}
async function openSettings(){if(!configCache)await loadConfig();document.getElementById('settings-modal').classList.add('show');runAsync('API Key 加载失败',loadAPIKeys)}function closeSettings(){document.getElementById('settings-modal').classList.remove('show')}function countries(id){return document.getElementById(id).value.split(',').map(v=>v.trim().toUpperCase()).filter(Boolean)}
function formatAPIKeyTime(v){if(!v)return '--';const d=new Date(v);return Number.isNaN(d.getTime())?String(v):d.toLocaleString()}
function renderAPIKeys(keys){const body=document.getElementById('apikey-rows');if(!body)return;const list=Array.isArray(keys)?keys:[];if(!list.length){body.innerHTML='<tr><td colspan="5" class="empty">暂无 API Key</td></tr>';return}body.innerHTML=list.map(k=>{const id=html(k.id);const name=html(k.name);const created=html(formatAPIKeyTime(k.created_at));const last=html(formatAPIKeyTime(k.last_used_at));const disabled=!!(k.disabled===true||Number(k.disabled)===1);const st=disabled?'<span class="badge warn">已吊销</span>':'<span class="badge ok">有效</span>';const revokeBtn=disabled?'':'<button class="mini" onclick="revokeAPIKey(\''+id+'\')">吊销</button> ';return '<tr><td>'+name+'</td><td>'+created+'</td><td>'+last+'</td><td>'+st+'</td><td>'+revokeBtn+'<button class="mini danger" onclick="deleteAPIKey(\''+id+'\')">删除</button></td></tr>'}).join('')}
async function loadAPIKeys(){const data=await api('/api/apikeys');if(!data)return;renderAPIKeys(data.keys||data||[])}
async function createAPIKey(){return runAsync('创建 API Key 失败',async()=>{const name=document.getElementById('apikey-name').value.trim();if(!name){showToast('请填写 Key 名称');return}const r=await api('/api/apikey/create',{method:'POST',body:JSON.stringify({name})});document.getElementById('apikey-name').value='';document.getElementById('apikey-once-name').value=r&&r.name?r.name:name;document.getElementById('apikey-once-key').value=r&&r.key?r.key:'';document.getElementById('apikey-once-modal').classList.add('show');await loadAPIKeys();showToast('API Key 已创建（仅显示一次）')})}
async function revokeAPIKey(id){return runAsync('吊销失败',async()=>{if(!confirm('吊销该 API Key？吊销后立即失效。'))return;await api('/api/apikey/revoke',{method:'POST',body:JSON.stringify({id})});await loadAPIKeys();showToast('已吊销')})}
async function deleteAPIKey(id){return runAsync('删除失败',async()=>{if(!confirm('删除该 API Key？此操作不可恢复。'))return;await api('/api/apikey/delete',{method:'POST',body:JSON.stringify({id})});await loadAPIKeys();showToast('已删除')})}
async function saveConfig(){return runAsync('保存失败',async()=>{if(!configCache)await loadConfig();if(!configCache)throw new Error('配置未加载');const payload={proxy_auth_enabled:document.getElementById('cfg-auth-enabled').value==='true',proxy_auth_username:document.getElementById('cfg-auth-username').value.trim(),proxy_auth_password:document.getElementById('cfg-auth-password').value,session_ttl_minutes:Number(document.getElementById('cfg-session-ttl').value),default_region:document.getElementById('cfg-default-region').value.trim().toLowerCase(),health_check_interval:Number(document.getElementById('cfg-health-interval').value),max_retry:Number(document.getElementById('cfg-max-retry').value),singbox_path:document.getElementById('cfg-singbox-path').value.trim(),allowed_countries:countries('cfg-allowed-countries'),blocked_countries:countries('cfg-blocked-countries')};await api('/api/config/save',{method:'POST',body:JSON.stringify(payload)});closeSettings();await loadConfig();showToast('配置已保存')})}
// ===== 侧边栏折叠持久化 =====
function applySidebar(collapsed){document.body.classList.toggle('sidebar-collapsed',!!collapsed);try{localStorage.setItem('gg-sidebar',collapsed?'1':'0')}catch(e){}}
function toggleSidebar(){applySidebar(!document.body.classList.contains('sidebar-collapsed'))}
function openDrawer(){document.body.classList.add('drawer-open')}
function closeDrawer(){document.body.classList.remove('drawer-open')}
(function(){let c=false;try{c=localStorage.getItem('gg-sidebar')==='1'}catch(e){}applySidebar(c);const sb=document.getElementById('sidebar');if(sb)requestAnimationFrame(function(){sb.classList.remove('preload')})})();
// AI/CF 图标筛选：点击循环 全部->仅可达->仅不可达->仅未探测；值写入隐藏 select，renderProxies 读取不变。
const FILTER_CYCLE={'':'全部','unlocked':'仅可达','blocked':'仅不可达','unprobed':'仅未探测','unknown':'仅未探测'};
function cycleFilter(selId,btnId){const sel=document.getElementById(selId);if(!sel)return;const opts=Array.from(sel.options).map(o=>o.value);let idx=opts.indexOf(sel.value);idx=(idx+1)%opts.length;sel.value=opts[idx];syncFilterToggle(selId,btnId);renderProxies()}
function syncFilterToggle(selId,btnId){const sel=document.getElementById(selId);const btn=document.getElementById(btnId);if(!sel||!btn)return;const v=sel.value;const st=btn.querySelector('.st');if(st)st.textContent=FILTER_CYCLE[v]||'全部';btn.setAttribute('aria-pressed',v?'true':'false')}
function initFilterToggles(){document.querySelectorAll('.filter-toggle[data-sel]').forEach(function(btn){syncFilterToggle(btn.dataset.sel,btn.id)})}
// 节点表分批渲染：首批立即渲染，滚动接近底部再增量，避免上千行一次性 DOM。
let proxyRenderRows=[];let proxyRenderCount=0;const PROXY_BATCH=80;
function proxyRowHTML(p){const addr=addressArg(p.address);const id=proxyIDArg(p);const manual=p.source==='manual';const st=nodeState(p);const label=html(nodeLabel(p));const showRegion=isAvailable(p)&&isKnownRegion(p);const toggleBtn=(st==='paused')?'<button class="mini" onclick="toggleProxy('+id+',decodeURIComponent(\''+addr+'\'),true)">启用</button>':'<button class="mini" onclick="toggleProxy('+id+',decodeURIComponent(\''+addr+'\'),false)">停用</button>';const testBtn='<button class="mini" onclick="testProxy('+id+',decodeURIComponent(\''+addr+'\'))">测试</button>';const copyBtn='<button class="mini" onclick="copyProxyCred('+id+')">复制</button>';const baseActions=testBtn+' '+copyBtn+' '+toggleBtn;const manageBtn=manual?('<button class="mini" onclick="manageManualNode('+id+',decodeURIComponent(\''+addr+'\'))">管理</button>'):'';const actions=baseActions+(manageBtn?(' '+manageBtn):'');const latencyText=Number(p.latency)>0?html(p.latency)+' ms':'--';const sel=manual?'<input type="checkbox" class="proxy-select" value="'+id+'">':'';return '<tr><td>'+sel+'</td><td>'+starBtn(p)+'</td><td title="'+label+'">'+label+'</td><td>'+protocolBadges(p)+'</td><td>'+(showRegion?'<span class="badge ok">'+html(regionOf(p)).toUpperCase()+'</span>':'<span class="muted">--</span>')+'</td><td class="mono">'+html(p.exit_ip)+'</td><td>'+latencyText+'</td><td>'+abuserBadge(p.ipapiis_score)+'</td><td>'+ipapiFlagsBadges(p.ipapi_flags,!!p.ipapi_flags_seen)+'</td><td>'+cfBadge(p.cf_blocked)+'</td><td>'+aiBadges(p.ai_reachability)+'</td><td>'+html(sourceLabel(p))+'</td><td>'+stateBadge(st)+'</td><td>'+actions+'</td></tr>'}
function renderProxyBatch(){const body=document.getElementById('proxy-rows');if(!body)return;const next=Math.min(proxyRenderCount+PROXY_BATCH,proxyRenderRows.length);let h='';for(let i=proxyRenderCount;i<next;i++)h+=proxyRowHTML(proxyRenderRows[i]);if(proxyRenderCount===0)body.innerHTML=h;else body.insertAdjacentHTML('beforeend',h);proxyRenderCount=next}
function onProxyScroll(){if(proxyRenderCount>=proxyRenderRows.length)return;const el=document.documentElement;if(el.scrollTop+el.clientHeight>=el.scrollHeight-320)renderProxyBatch()}
window.addEventListener('scroll',onProxyScroll,{passive:true});
// 骨架墓碑：载入态灰条 shimmer（尊重 prefers-reduced-motion，动画由 CSS 关闭）。
function skeletonRows(n){let h='';for(let i=0;i<(n||3);i++)h+='<div class="skeleton sk-row"></div>';return '<div class="skeleton-wrap">'+h+'</div>'}
function showSkeletons(){['region-list','sub-list','session-rows','singbox-status'].forEach(function(id){const el=document.getElementById(id);if(el)el.innerHTML=skeletonRows(3)})}
// 懒加载：切到总览视图才绘制世界地图（重绘按需）。
let overviewSeen=false;function markViewLazy(name){if(name==='overview'&&!overviewSeen){overviewSeen=true;try{renderWorldMap()}catch(e){}}}
initFilterToggles();showSkeletons();markViewLazy('overview');
refreshAll();
setInterval(()=>runAsync('自动刷新失败',()=>Promise.all([loadStats(),loadProxies(),loadSubscriptions(),loadSessions()])),10000);
setInterval(()=>runAsync('日志刷新失败',loadLogs),5000);`
