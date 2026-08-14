package webui

// dashboard_assets.go 将 dashboard 的 CSS/JS 从 HTML 中分离为 Go 常量，
// 由 /assets/dashboard.css 与 /assets/dashboard.js 路由下发（带内容 hash 的 ETag、支持 304）。
// 仍为 Go 内嵌字符串，不落地独立文件、不引入前端构建链。

const dashboardCSS = `/* Orbit SSOT tokens+layout from docs/orbit-dashboard.html */

/* ===================== 设计令牌：中性石墨主题（Orbit 蓝仅作信号） ===================== */
:root[data-theme="space"]{
 --space-0:#111315; --space-1:#17191c; --space-2:#1c2024; --space-3:#252a2f;
 --panel:rgba(28,32,36,.96); --panel-solid:#1c2024; --panel-2:#23282d;
 --ink:#f1f3f5; --ink-2:#c3c8ce; --muted:#9098a1;
 --line:rgba(226,232,240,.14); --hairline:rgba(226,232,240,.08);
 --accent:#3b8dff; --accent-ink:#fff;
 --q-s:#7cc4ff; --q-a:#3b8dff; --q-b:#1f56c8; --q-c:#5a6480;
 --ok:#2fbf87; --warn:#f5b544; --danger:#ff5c7a; --gray:#7d858d; --ai-unprobed:#6f7a96;
 --sun-core:#fff; --sun-halo:#9ccaff; --sun-energy:#3b8dff;
 --glow-s:0 0 12px rgba(124,196,255,.6),0 0 34px rgba(124,196,255,.28);
 --glow-a:0 0 12px rgba(59,141,255,.55),0 0 34px rgba(59,141,255,.24);
 --glow-b:0 0 12px rgba(31,86,200,.5),0 0 30px rgba(31,86,200,.2);
 --glow-c:0 0 10px rgba(90,100,128,.4),0 0 22px rgba(90,100,128,.16);
 --glow-ok:0 0 10px rgba(47,191,135,.6);
 --sh-md:0 1px 2px rgba(0,0,0,.28);
 --sh-lg:0 12px 32px rgba(0,0,0,.42);
 --radius:8px; --ease:cubic-bezier(.16,1,.3,1);
 --t-micro:150ms; --t-panel:280ms;
 --bg-canvas:#111315;
}
/* 日间主题同样使用中性冷灰，避免把功能区染成单一强调色。 */
:root[data-theme="day"]{
 --space-0:#f3f4f6; --space-1:#f8f9fa; --space-2:#fff; --space-3:#e5e7eb;
 --panel:#fff; --panel-solid:#fff; --panel-2:#f6f7f8;
 --ink:#181b1f; --ink-2:#41484f; --muted:#68717a;
 --line:#d9dde2; --hairline:rgba(31,35,40,.08);
 --accent:#1d6fe0; --accent-ink:#fff;
 --q-s:#4da3ff; --q-a:#1d6fe0; --q-b:#1546a8; --q-c:#8a93a6;
 --ok:#12a150; --warn:#c98a12; --danger:#e0485f; --gray:#9299a1; --ai-unprobed:#9aa4ba;
 --sun-core:#fff; --sun-halo:#8fc0ff; --sun-energy:#1d6fe0;
 --glow-s:0 4px 14px rgba(77,163,255,.28); --glow-a:0 4px 14px rgba(29,111,224,.24);
 --glow-b:0 4px 12px rgba(21,70,168,.2); --glow-c:0 2px 8px rgba(138,147,166,.28);
 --glow-ok:0 3px 10px rgba(18,161,80,.3);
 --sh-md:0 1px 2px rgba(31,35,40,.10);
 --sh-lg:0 12px 32px rgba(31,35,40,.16);
 --radius:8px; --ease:cubic-bezier(.16,1,.3,1);
 --t-micro:150ms; --t-panel:280ms;
 --bg-canvas:#f3f4f6;
}
*{box-sizing:border-box}
html,body{margin:0;height:100%}
body{
 background:var(--bg-canvas); color:var(--ink); overflow-x:hidden;
 font-family:"Segoe UI","PingFang SC","Microsoft YaHei",system-ui,sans-serif;
 font-size:14px; line-height:1.55; -webkit-font-smoothing:antialiased;
}
.num{font-variant-numeric:tabular-nums;font-feature-settings:"tnum" 1}
h1,h2,h3{margin:0}
a{color:inherit;text-decoration:none}
:focus-visible{outline:2px solid var(--accent);outline-offset:2px;border-radius:6px}

/* ===================== 布局骨架 ===================== */
.app{position:relative;z-index:1;display:grid;grid-template-columns:236px 1fr;min-height:100vh;
 transition:grid-template-columns var(--t-panel) var(--ease)}
.app.nav-collapsed{grid-template-columns:74px 1fr}
.sidebar{position:sticky;top:0;height:100vh;min-width:0;display:flex;flex-direction:column;overflow:hidden;
 background:var(--panel-solid);
 border-right:1px solid var(--line)}
.brand{display:flex;align-items:center;gap:12px;padding:18px;border-bottom:1px solid var(--hairline)}
.app.nav-collapsed .brand{justify-content:center;padding:18px 0}
.brand .mark{position:relative;flex:0 0 auto;width:38px;height:38px;border-radius:11px;display:grid;place-items:center;
 background:var(--accent);box-shadow:none;color:#fff;font-weight:900;font-size:13px;letter-spacing:.02em}
.brand .bt{min-width:0;font-weight:800;letter-spacing:.02em;font-size:15px;white-space:nowrap;
 transition:opacity var(--t-micro),width var(--t-micro)}
.brand .bt small{display:block;font-size:10px;font-weight:600;color:var(--muted);letter-spacing:.18em;text-transform:uppercase}
.app.nav-collapsed .brand .bt{opacity:0;width:0;overflow:hidden}
.nav{flex:1;padding:12px;display:flex;flex-direction:column;gap:4px;overflow-y:auto}
.nav .lab{font-size:10px;letter-spacing:.16em;text-transform:uppercase;color:var(--muted);margin:12px 10px 4px;font-weight:700;
 white-space:nowrap;transition:opacity var(--t-micro)}
.app.nav-collapsed .nav .lab{opacity:0;height:8px;margin:6px 0 0}
.navitem{appearance:none;-webkit-appearance:none;display:flex;align-items:center;gap:12px;padding:10px 12px;border-radius:11px;cursor:pointer;
 background:transparent;color:var(--muted);font:inherit;font-weight:600;border:1px solid transparent;white-space:nowrap;
 transition:background var(--t-micro) var(--ease),color var(--t-micro),border-color var(--t-micro),padding var(--t-panel) var(--ease)}
.navitem .ico{flex:0 0 auto;width:19px;height:19px;display:grid;place-items:center}
.navitem .ico svg{width:19px;height:19px;opacity:.9}
.navitem .t{min-width:0;overflow:hidden;transition:opacity var(--t-micro),width var(--t-micro)}
.app.nav-collapsed .navitem{justify-content:center;padding-left:0;padding-right:0;gap:0}
.app.nav-collapsed .navitem .t{opacity:0;width:0}
.navitem:hover{background:color-mix(in srgb,var(--accent) 12%,transparent);color:var(--ink)}
.navitem.active{color:var(--ink);border-color:color-mix(in srgb,var(--accent) 40%,transparent);
 background:color-mix(in srgb,var(--accent) 12%,transparent)}
.navitem.active .ico svg{color:var(--accent);filter:drop-shadow(0 0 6px color-mix(in srgb,var(--accent) 70%,transparent))}
/* 折叠钮:侧栏左下(D 决策,不再放 logo 前) */
.sidefoot{padding:12px;border-top:1px solid var(--hairline);display:flex;flex-direction:column;gap:8px}
.collapse-btn{display:flex;align-items:center;gap:12px;padding:9px 12px;border-radius:11px;cursor:pointer;
 color:var(--ink-2);font-weight:600;border:1px solid var(--line);background:var(--panel-2);white-space:nowrap;
 transition:border-color var(--t-micro),color var(--t-micro),background var(--t-micro)}
.collapse-btn:hover{border-color:var(--accent);color:var(--accent)}
.collapse-btn .ico{flex:0 0 auto;width:19px;height:19px;display:grid;place-items:center;transition:transform var(--t-panel) var(--ease)}
.app.nav-collapsed .collapse-btn .ico{transform:rotate(180deg)}
.collapse-btn .t{overflow:hidden;transition:opacity var(--t-micro),width var(--t-micro)}
.app.nav-collapsed .collapse-btn{justify-content:center;gap:0;padding-left:0;padding-right:0}
.app.nav-collapsed .collapse-btn .t{opacity:0;width:0}
.sidefoot .pill{justify-content:center}
.app.nav-collapsed .sidefoot .pill .t{opacity:0;width:0}

.main{min-width:0;display:flex;flex-direction:column}
.topbar{position:sticky;top:0;z-index:20;display:flex;align-items:center;gap:14px;padding:0 16px;height:56px;
 background:var(--panel-solid);border-bottom:1px solid var(--line)}
.topbar h1{font-size:15px;font-weight:800;letter-spacing:.01em}
.spacer{flex:1}
.topbar .actions{display:flex;align-items:center;gap:8px;flex:0 0 auto}
.langbtn{min-width:38px;height:38px;padding:0 8px;border-radius:10px;border:1px solid var(--line);background:var(--panel-2);color:var(--ink);font-weight:800;font-size:12px;letter-spacing:.04em;cursor:pointer}
.langbtn:hover{border-color:var(--accent);color:var(--accent)}
.langbtn .lang-code{display:inline-block}
.pill{display:inline-flex;align-items:center;gap:8px;padding:6px 12px;border-radius:999px;font-size:12px;font-weight:700;
 border:1px solid var(--line);background:var(--panel-2);color:var(--ink-2);white-space:nowrap}
.pill .dot{flex:0 0 auto;width:8px;height:8px;border-radius:50%;background:var(--ok);box-shadow:var(--glow-ok);animation:pulseSimple 2.4s ease-in-out infinite}
.iconbtn{flex:0 0 auto;width:38px;height:38px;border-radius:10px;display:grid;place-items:center;cursor:pointer;
 border:1px solid var(--line);background:var(--panel-2);color:var(--ink-2);
 transition:border-color var(--t-micro),color var(--t-micro),transform var(--t-micro),box-shadow var(--t-micro)}
.iconbtn:hover{border-color:var(--accent);color:var(--accent);box-shadow:var(--glow-a)}
.iconbtn:active{transform:scale(.94)}
.iconbtn svg{width:19px;height:19px}
.content{padding:16px;max-width:none;width:100%;margin:0 auto}

/* ===================== 仪表读数卡 ===================== */
.metrics{display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:8px;margin-bottom:12px}
.metric{position:relative;padding:12px 14px;border-radius:var(--radius);overflow:hidden;
 background:var(--panel);border:1px solid var(--line);
 box-shadow:var(--sh-md);transition:border-color var(--t-panel)}
.metric:hover{border-color:color-mix(in srgb,var(--accent) 34%,var(--line))}
.metric::before{content:"";position:absolute;left:0;top:0;height:3px;width:100%;
 background:var(--accent)}
.metric .k{font-size:11px;letter-spacing:.06em;text-transform:uppercase;color:var(--muted);font-weight:700}
.metric .v{font-size:30px;font-weight:800;letter-spacing:-.02em;margin:6px 0 2px;
 text-shadow:0 0 20px color-mix(in srgb,var(--accent) 40%,transparent)}
.metric .n{font-size:11px;color:var(--muted)}

/* ===================== 卡片壳 ===================== */
.grid,.overview-grid{display:grid;grid-template-columns:minmax(0,1.6fr) minmax(280px,1fr);gap:16px;align-items:stretch}
@media(max-width:1100px){.grid,.overview-grid{grid-template-columns:1fr}}
.overview-side{display:flex;flex-direction:column;gap:16px;min-width:0}
.overview-side .card{margin:0}
.card.orbit-card{min-width:0;margin:0}
.card{border-radius:var(--radius);border:1px solid var(--line);overflow:hidden;
 background:var(--panel);box-shadow:var(--sh-md)}
.card-h,.card-head{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:11px 14px;border-bottom:1px solid var(--hairline);flex-wrap:wrap}
.card-h h3{font-size:14px;font-weight:800;letter-spacing:.01em;display:flex;align-items:center;gap:8px}
.card-h .sub{font-size:11px;color:var(--muted)}
.card-h .tools{display:flex;gap:8px;flex-wrap:wrap}
.card-b,.card-body{padding:14px}
/* 轨道卡:被 grid 拉高时,让 card-b 纵向居中其内容(星系+图例),消除底部大片空白 */
.card.orbit-card{display:flex;flex-direction:column}
.orbit-card .card-b,.card-body{flex:1;display:flex;flex-direction:column;justify-content:center}
/* 卡头内小工具按钮(公转控制放这里,不用悬浮条) */
.tbtn{padding:6px 12px;border-radius:6px;border:1px solid var(--line);background:var(--panel-2);color:var(--ink-2);
 cursor:pointer;font-weight:700;font-size:11px;transition:border-color var(--t-micro),color var(--t-micro),transform var(--t-micro)}
.tbtn:hover{border-color:var(--accent);color:var(--accent)}
.tbtn:active{transform:scale(.95)}

/* ===================== 核心:Node Orbit System(rAF 三维椭圆) ===================== */
.stage,.orbit-stage{position:relative;width:100%;margin:0 auto;aspect-ratio:16/9}
/* 舞台内绝对定位层;所有卫星/环/光束/太阳的坐标由 JS 按舞台像素尺寸实时计算 */
.orbit-svg{position:absolute;inset:0;width:100%;height:100%;overflow:visible;pointer-events:none}
.orbit-ring{fill:none;stroke-width:1.2}
/* 光束:柔和单弧丝带(填充)+外层淡晕。无虚线、无末端闪点、无折线感 */
.orbit-beam,.beam-path{stroke:none;fill-rule:nonzero;pointer-events:none}
.orbit-beam-glow,.beam-glow{stroke:none;fill-rule:nonzero;pointer-events:none;opacity:.35;mix-blend-mode:screen}
/* 太阳风:柔和流(连续流线,无硬质粒子点)。类名/渐变 id 与 JS 生成保持一致(orbit- 前缀) */
.orbit-wind-plume{fill:url(#orbitWindPlume);pointer-events:none;mix-blend-mode:screen;filter:blur(1.2px)}
.orbit-wind-stream{fill:none;stroke-linecap:round;pointer-events:none;mix-blend-mode:screen;filter:blur(0.6px)}
.orbit-wind-stream-core{fill:none;stroke-linecap:round;pointer-events:none;mix-blend-mode:screen}
/* 引力透镜:半透明光晕环,柔和扭曲(非折线) */
.orbit-lens-halo{fill:url(#orbitLensFill);pointer-events:none;mix-blend-mode:screen}
.orbit-lens-rim{fill:none;stroke:rgba(180,210,255,.35);stroke-width:1.2;pointer-events:none}
.layer{position:absolute;inset:0;pointer-events:none}
/* 卫星球体 */
.sat,.orbit-sat{position:absolute;top:0;left:0;transform:translate(-50%,-50%);will-change:transform;cursor:pointer;pointer-events:auto}
.sat .ball,.orbit-sat .ball{position:relative;width:100%;height:100%;border-radius:50%;display:grid;place-items:center;line-height:1;
 background:radial-gradient(circle at 34% 28%,#fff 0%,var(--qc,var(--q-a)) 42%,color-mix(in srgb,var(--qc,var(--q-a)) 55%,#111315) 100%);
 box-shadow:inset 0 -3px 8px rgba(0,0,0,.35),inset 2px 2px 6px rgba(255,255,255,.35),0 0 10px color-mix(in srgb,var(--qc,var(--q-a)) 45%,transparent);
 border:1px solid color-mix(in srgb,var(--qc,var(--q-a)) 60%,transparent)}
.sat .cc,.orbit-sat .cc{font-size:10px;font-weight:900;letter-spacing:.02em;color:#fff;text-shadow:0 1px 3px rgba(0,0,0,.6)}
.sat .cnt,.orbit-sat .cnt{position:absolute;top:-5px;right:-5px;min-width:13px;height:13px;padding:0 3px;border-radius:7px;
 display:grid;place-items:center;font-size:8px;font-weight:800;color:var(--ink);
 background:var(--panel-solid);border:1px solid var(--qc,var(--q-a))}
.sat .tip,.orbit-sat .tip{position:absolute;bottom:calc(100% + 8px);left:50%;transform:translateX(-50%);white-space:nowrap;
 padding:6px 10px;border-radius:8px;background:var(--panel-solid);border:1px solid var(--line);
 color:var(--ink);font-size:11px;font-weight:600;box-shadow:var(--sh-md);opacity:0;pointer-events:none;
 transition:opacity var(--t-micro);z-index:20}
.sat:hover .tip,.orbit-sat:hover .tip{opacity:1}
/* 会话中卫星:轻微辉光呼吸(仅辉光,本体不缩放跳动) */
.sat.live .ball,.orbit-sat.live .ball{box-shadow:inset 0 -3px 8px rgba(0,0,0,.35),inset 2px 2px 6px rgba(255,255,255,.4),0 0 16px color-mix(in srgb,var(--qc,var(--q-a)) 75%,transparent)}

/* 太阳:网关(中心,z 居中,可被前景卫星盖住) */
.sun,.orbit-sun{position:absolute;left:50%;top:50%;width:92px;height:92px;transform:translate(-50%,-50%);
 border-radius:50%;display:grid;place-items:center;
 background:radial-gradient(circle at 42% 36%,#fff,var(--sun-halo) 46%,#1f56c8 78%,#0e2a5e);
 box-shadow:0 0 30px rgba(124,196,255,.7),0 0 70px rgba(59,141,255,.42),inset 0 0 20px rgba(255,255,255,.5)}
.sun-ring,.orbit-sun-ring{content:"";position:absolute;inset:-14px;border-radius:50%;z-index:-1;
 background:conic-gradient(from var(--ang,0deg),transparent,var(--sun-energy),transparent 30%,var(--sun-halo),transparent 60%,var(--sun-energy),transparent);
 filter:blur(3px);animation:ringspin 8s linear infinite}
.sun-halo,.orbit-sun-halo{content:"";position:absolute;inset:-4px;border-radius:50%;z-index:-1;
 background:radial-gradient(circle,rgba(59,141,255,.35),transparent 70%);animation:pulseSimple 3s ease-in-out infinite}
.sun .lbl{text-align:center;color:#fff;text-shadow:0 1px 6px rgba(0,0,0,.5);z-index:1}
.sun .lbl .t{font-size:9px;letter-spacing:.14em;font-weight:700;opacity:.85;text-transform:uppercase}
.sun .lbl .ip{font-size:12px;font-weight:800;letter-spacing:.02em}
@property --ang{syntax:'<angle>';initial-value:0deg;inherits:false}
@keyframes ringspin{to{--ang:360deg}}
@keyframes spin{to{transform:rotate(360deg)}}
@keyframes pulseSimple{0%,100%{opacity:.45}50%{opacity:.95}}

/* 图例:上行延迟档,下行光束/事件(各一行居中) */
.legend,.orbit-legend{display:flex;flex-direction:column;align-items:center;gap:10px;margin-top:40px;padding-top:8px;font-size:11px;color:var(--muted)}
.legend-row,.orbit-legend-row{display:flex;flex-wrap:nowrap;gap:16px;justify-content:center;align-items:center}
.legend b{display:inline-flex;align-items:center;gap:6px;font-weight:600;color:var(--ink-2);white-space:nowrap}
.legend .qd{width:10px;height:10px;border-radius:50%}
.qd.s{background:var(--q-s);box-shadow:var(--glow-s)}
.qd.a{background:var(--q-a);box-shadow:var(--glow-a)}
.qd.b{background:var(--q-b);box-shadow:var(--glow-b)}
.qd.c{background:var(--q-c);box-shadow:var(--glow-c)}

/* ===================== 地域分布（宽卡片多指标） / 会话 / 引擎 ===================== */
.region-panel{display:flex;flex-direction:column;gap:10px}
/* 代码|国家/地区 紧邻；结构指标列吃剩余宽度，避免国家与结构栏被拉得过开 */
.region-head{display:grid;grid-template-columns:44px max-content minmax(0,1fr) 72px;column-gap:12px;row-gap:8px;align-items:center;padding:0 14px 6px;color:var(--muted);font-size:11px;font-weight:700;letter-spacing:.04em;text-transform:uppercase}
.region{display:grid;grid-template-columns:44px max-content minmax(0,1fr) 72px;column-gap:12px;row-gap:8px;align-items:center;padding:12px 14px;border:1px solid var(--hairline);border-radius:12px;background:var(--panel-2)}
.region .cc{font-weight:800;font-size:15px;letter-spacing:.04em;line-height:1.1;font-family:"Consolas",monospace}
.region .name{font-weight:700;font-size:13px;color:var(--ink);line-height:1.25;min-width:4.5em;max-width:8em;padding-right:4px;word-break:keep-all}
.region .meta{display:flex;flex-direction:column;gap:8px;min-width:0}
.region .bar{height:8px;border-radius:999px;background:color-mix(in srgb,var(--muted) 22%,transparent);overflow:hidden}
.region .bar i{display:block;height:100%;border-radius:999px;background:var(--accent);box-shadow:none}
.region .chips{display:flex;flex-wrap:wrap;gap:6px;align-items:center}
.region .n{text-align:right;display:flex;flex-direction:column;align-items:flex-end;gap:2px;min-width:72px}
.region .n .big{font-size:18px;font-weight:800;color:var(--ink);line-height:1}
.region .n .sub{font-size:11px;color:var(--muted);font-weight:600}
@media (max-width:720px){.region-head{display:none}.region{grid-template-columns:48px minmax(0,1fr);}.region .name{grid-column:2}.region .meta{grid-column:1/-1}.region .n{grid-column:1/-1;flex-direction:row;justify-content:space-between;align-items:center;text-align:left}}
.sess{display:flex;align-items:center;justify-content:space-between;gap:10px;padding:10px 12px;border-radius:11px;
 border:1px solid var(--hairline);margin-bottom:8px;background:var(--panel-2)}
.sess .sid{font-family:"Consolas",monospace;font-weight:700;font-size:12px;display:flex;align-items:center;gap:8px}
.sess .sid::before{content:"";width:7px;height:7px;border-radius:50%;background:var(--ok);box-shadow:var(--glow-ok);animation:pulseSimple 2s ease-in-out infinite}
.sess .ttl{font-size:11px;color:var(--ok);font-weight:700}
.kv{display:flex;align-items:center;justify-content:space-between;padding:8px 0;border-bottom:1px solid var(--hairline);font-size:13px}
.kv:last-child{border:none}.kv .k{color:var(--muted)}.kv .v{font-weight:700}

/* ===================== 徽章语言 ===================== */
.badge{display:inline-flex;align-items:center;gap:5px;padding:2px 9px;border-radius:999px;font-size:11px;font-weight:700;border:1px solid transparent}
.badge.qs{background:color-mix(in srgb,var(--q-s) 20%,transparent);color:var(--q-s);border-color:color-mix(in srgb,var(--q-s) 40%,transparent);box-shadow:var(--glow-s)}
.badge.qa{background:color-mix(in srgb,var(--q-a) 20%,transparent);color:var(--q-a);border-color:color-mix(in srgb,var(--q-a) 40%,transparent)}
.badge.qb{background:color-mix(in srgb,var(--q-b) 22%,transparent);color:var(--q-s);border-color:color-mix(in srgb,var(--q-b) 46%,transparent)}
.badge.qc{background:color-mix(in srgb,var(--q-c) 22%,transparent);color:var(--ink-2);border-color:color-mix(in srgb,var(--q-c) 46%,transparent)}
.badge.qd{background:color-mix(in srgb,var(--danger) 20%,transparent);color:var(--danger);border-color:color-mix(in srgb,var(--danger) 42%,transparent)}
.badge.ok{background:color-mix(in srgb,var(--ok) 18%,transparent);color:var(--ok);border-color:color-mix(in srgb,var(--ok) 38%,transparent)}
.badge.warn{background:color-mix(in srgb,var(--warn) 18%,transparent);color:var(--warn);border-color:color-mix(in srgb,var(--warn) 38%,transparent)}
.badge.danger{background:color-mix(in srgb,var(--danger) 18%,transparent);color:var(--danger);border-color:color-mix(in srgb,var(--danger) 38%,transparent)}
.badge.blue{background:color-mix(in srgb,var(--accent) 16%,transparent);color:var(--accent);border-color:color-mix(in srgb,var(--accent) 36%,transparent)}
.sdot{display:inline-flex;align-items:center;gap:7px;font-size:12px;font-weight:700}
.sdot i{width:8px;height:8px;border-radius:50%}
.sdot.ok i{background:var(--ok);box-shadow:var(--glow-ok)}
.sdot.off i{background:var(--danger);box-shadow:0 0 8px color-mix(in srgb,var(--danger) 70%,transparent)}
.sdot.idle i{background:var(--gray)}

/* 表格 */
.tbl{width:100%;border-collapse:collapse;font-size:13px}
.tbl th{font-size:10px;letter-spacing:.06em;text-transform:uppercase;color:var(--muted);font-weight:700;
 text-align:left;padding:10px;border-bottom:1px solid var(--line)}
.tbl td{padding:11px 10px;border-bottom:1px solid var(--hairline);white-space:nowrap}
.tbl tr:last-child td{border:none}
/* ip-api 标记(第9列) 与 Cloudflare(第10列) 为窄徽章列：收紧横向内边距，让两枚状态徽章视觉贴近，不再被全局 10px 撑远。仅作用于节点表(含 #proxy-rows)，不影响其它表。 */
.tbl:has(#proxy-rows) th:nth-child(9),.tbl:has(#proxy-rows) td:nth-child(9),.tbl:has(#proxy-rows) th:nth-child(10),.tbl:has(#proxy-rows) td:nth-child(10){padding-left:4px;padding-right:4px}
/* AI 解锁(第11列)：四枚短标记须单行排布，不得挤成 2x2。 */
.tbl:has(#proxy-rows) th:nth-child(11),.tbl:has(#proxy-rows) td:nth-child(11){white-space:nowrap;min-width:132px}
/* 表内复选框:深空主题下原生 checkbox 暗底几乎不可见,显式给出可见外观与配色 */
.tbl input[type=checkbox]{appearance:auto;-webkit-appearance:auto;width:15px;height:15px;margin:0;cursor:pointer;accent-color:var(--accent);vertical-align:middle}
.tbl tbody tr{transition:background var(--t-micro)}
.tbl tbody tr:hover{background:color-mix(in srgb,var(--accent) 8%,transparent)}
.mono{font-family:"Consolas",monospace}
.ops{display:inline-flex;gap:4px;flex-wrap:nowrap;white-space:nowrap}

/* toast */
.toast{position:fixed;left:50%;bottom:24px;transform:translateX(-50%) translateY(20px);background:var(--panel-solid);
 border:1px solid var(--accent);color:var(--ink);padding:12px 20px;border-radius:999px;font-weight:700;opacity:0;
 pointer-events:none;transition:all var(--t-panel) var(--ease);z-index:50;box-shadow:var(--glow-a)}
.toast.show{opacity:1;transform:translateX(-50%) translateY(0)}

/* ===================== 多页壳 / 筛选 / 表单 / 日志 / 会话卡 ===================== */
.page{display:none}.page.active{display:block}
.pager{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:10px;margin-top:12px;padding:8px 10px;border:1px solid var(--hairline);border-radius:6px;background:var(--panel-2)}
.pager-actions{display:flex;flex-wrap:wrap;align-items:center;gap:8px}
.pager .input.sm{width:auto;min-width:72px}
.toolbar{display:flex;flex-wrap:wrap;gap:8px;align-items:center;margin-bottom:12px}
.toolbar.filters{padding:8px 10px;border:1px solid var(--hairline);border-radius:6px;background:var(--panel-2);
 justify-content:space-between;gap:8px}
.toolbar.filters > .input.sm{flex:1 1 0;min-width:0;width:auto}
.toolbar.filters > .filter-toggle{flex:1 1 0;min-width:0;justify-content:center}
.toolbar.filters .sep{width:1px;height:22px;background:var(--line);margin:0;flex:0 0 auto;align-self:center}
.toolbar.filters.search-row{justify-content:flex-start}
.toolbar.filters.search-row > .input.narrow{flex:0 0 110px}
.toolbar.filters.search-row > .search-box{flex:1 1 auto}
@media(max-width:720px){.toolbar.filters > .filter-toggle{flex:0 0 auto;min-width:max-content}}
.search-box{display:flex;align-items:stretch;flex:1;min-width:200px;border:1px solid var(--line);border-radius:10px;background:var(--panel-2);overflow:hidden;
 transition:border-color var(--t-micro),box-shadow var(--t-micro)}
.search-box:focus-within{border-color:var(--accent);box-shadow:0 0 0 3px color-mix(in srgb,var(--accent) 18%,transparent)}
.search-box input{flex:1;min-width:0;border:0;background:transparent;border-radius:0;padding:8px 12px;box-shadow:none!important}
.search-box input:focus{outline:none;box-shadow:none}
.search-box .sbtn{flex:0 0 auto;width:40px;display:grid;place-items:center;border:0;border-left:1px solid var(--line);
 background:color-mix(in srgb,var(--accent) 10%,transparent);color:var(--accent);cursor:pointer}
.search-box .sbtn:hover{background:color-mix(in srgb,var(--accent) 18%,transparent)}
.search-box .sbtn svg{width:16px;height:16px}
.input,.select,textarea,select.input{appearance:none;-webkit-appearance:none;border:1px solid var(--line);background:var(--panel-2);color:var(--ink);
 border-radius:6px;padding:7px 10px;font:inherit;font-size:13px;outline:none;min-width:0;
 transition:border-color var(--t-micro),box-shadow var(--t-micro)}
.input:focus,textarea:focus,select.input:focus{border-color:var(--accent);box-shadow:0 0 0 3px color-mix(in srgb,var(--accent) 18%,transparent)}
.input.grow{flex:1;min-width:160px}.input.narrow{width:100px}.input.mid{width:128px}.input.sm{width:112px}
textarea{width:100%;min-height:110px;resize:vertical;font-family:"Consolas",monospace;line-height:1.45}
.btn,.mini{display:inline-flex;align-items:center;justify-content:center;gap:6px;border-radius:6px;border:1px solid var(--line);
 background:var(--panel-2);color:var(--ink-2);font-weight:700;font-size:12px;padding:8px 14px;cursor:pointer;
 transition:border-color var(--t-micro),color var(--t-micro),background var(--t-micro),transform var(--t-micro)}
.mini{padding:5px 9px;border-radius:6px;font-size:11px}
.btn:hover,.mini:hover{border-color:var(--accent);color:var(--accent)}
.btn:active,.mini:active{transform:scale(.96)}
.btn.primary,.mini.primary{background:color-mix(in srgb,var(--accent) 18%,var(--panel-2));border-color:color-mix(in srgb,var(--accent) 50%,var(--line));color:var(--accent)}
.btn.danger,.mini.danger{border-color:color-mix(in srgb,var(--danger) 40%,var(--line));color:var(--danger)}
.btn.danger:hover,.mini.danger:hover{background:color-mix(in srgb,var(--danger) 12%,transparent)}
.filter-toggle{display:inline-flex;align-items:center;gap:5px;padding:6px 10px;border-radius:999px;border:1px solid var(--line);
 background:var(--panel-2);color:var(--ink-2);font-size:11px;font-weight:700;cursor:pointer;user-select:none}
.filter-toggle[data-state="ok"]{border-color:color-mix(in srgb,var(--ok) 50%,var(--line));color:var(--ok);background:color-mix(in srgb,var(--ok) 12%,transparent)}
.filter-toggle[data-state="bad"]{border-color:color-mix(in srgb,var(--danger) 50%,var(--line));color:var(--danger);background:color-mix(in srgb,var(--danger) 12%,transparent)}
.filter-toggle[data-state="unk"]{border-color:color-mix(in srgb,var(--gray) 50%,var(--line));color:var(--gray);background:color-mix(in srgb,var(--gray) 12%,transparent)}
.filter-toggle[data-sel^="ai-"][data-state="unk"]{border-color:color-mix(in srgb,var(--ai-unprobed) 50%,var(--line));color:var(--ai-unprobed);background:color-mix(in srgb,var(--ai-unprobed) 12%,transparent)}
.filter-toggle .st{color:inherit;opacity:.8;font-weight:600;min-width:2em}
.hidden-select{position:absolute;width:1px;height:1px;opacity:0;pointer-events:none}
.empty{padding:28px 12px;text-align:center;color:var(--muted);font-size:13px}
.muted{color:var(--muted)}
.session-grid{display:flex;flex-direction:column;gap:12px}
.session-card{padding:0;border-radius:8px;border:1px solid var(--hairline);background:var(--panel-2);overflow:hidden;
 transition:border-color var(--t-micro),box-shadow var(--t-micro)}
.session-card.open{border-color:color-mix(in srgb,var(--accent) 40%,var(--line));box-shadow:var(--glow-a)}
.session-card .head{display:flex;align-items:center;gap:12px;padding:14px 16px;cursor:pointer;user-select:none}
.session-card .head:hover{background:color-mix(in srgb,var(--accent) 6%,transparent)}
.session-card .sid{font-family:"Consolas",monospace;font-weight:800;font-size:14px}
.session-card .ttl{color:var(--ok);font-weight:800;font-size:12px;white-space:nowrap}
.session-card .ttl.warn{color:var(--warn)}.session-card .ttl.danger{color:var(--danger)}
.session-card .chips{display:flex;align-items:center;gap:6px;flex-wrap:wrap;flex:1;min-width:0}
.session-card .chev{flex:0 0 auto;width:22px;height:22px;display:grid;place-items:center;color:var(--muted);
 transition:transform var(--t-panel) var(--ease)}
.session-card.open .chev{transform:rotate(90deg);color:var(--accent)}
.session-card .body{display:none;padding:0 16px 16px;border-top:1px solid var(--hairline)}
.session-card.open .body{display:block}
.session-card .detail-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:10px 16px;padding-top:14px}
.session-card .di{display:flex;flex-direction:column;gap:3px;min-width:0}
.session-card .di .k{font-size:10px;font-weight:700;letter-spacing:.06em;text-transform:uppercase;color:var(--muted)}
.session-card .di .v{font-size:13px;font-weight:600;color:var(--ink);word-break:break-all}
.session-card .di .v.mono{font-family:"Consolas",monospace;font-weight:700}
.session-card .route-box{margin-top:12px;padding:10px 12px;border-radius:10px;border:1px solid var(--hairline);
 background:color-mix(in srgb,var(--space-0) 40%,transparent);font-family:"Consolas",monospace;font-size:12px;
 word-break:break-all;color:var(--ink-2);line-height:1.45}
.session-card .route-box b{color:var(--muted);font-weight:700;font-family:inherit;margin-right:8px}
.session-card .occ{margin-top:12px;display:flex;align-items:center;gap:12px;flex-wrap:wrap}
.session-card .occ .bar{flex:1;min-width:120px;height:8px;border-radius:999px;background:color-mix(in srgb,var(--muted) 22%,transparent);overflow:hidden}
.session-card .occ .bar i{display:block;height:100%;border-radius:999px;background:linear-gradient(90deg,var(--q-b),var(--q-s))}
.sub-item{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:12px;border-radius:8px;
 border:1px solid var(--hairline);background:var(--panel-2);margin-bottom:10px}
.sub-item .meta strong{font-size:14px;display:inline-flex;align-items:center;gap:8px}
.sub-item .meta .muted{margin-top:4px;font-size:12px}
.sub-item .mini-actions{display:flex;gap:6px;flex-wrap:wrap}
.logs{background:#121416;border:1px solid var(--line);border-radius:12px;padding:12px 14px;max-height:520px;overflow:auto;overflow-anchor:none;
 font-family:"Consolas","Cascadia Mono",monospace;font-size:12px;line-height:1.55;color:#d7dce1}
[data-theme="day"] .logs{background:#181b1f;color:#eef0f2}
.log-line{padding:2px 4px;border-radius:4px;white-space:pre-wrap;word-break:break-all}
.log-line:hover{background:rgba(59,141,255,.08)}
.form-grid{display:grid;grid-template-columns:1fr 1fr;gap:14px}
@media(max-width:800px){.form-grid{grid-template-columns:1fr}}
.field{display:flex;flex-direction:column;gap:6px}
.field.full{grid-column:1/-1}
.field label{font-size:11px;font-weight:700;letter-spacing:.04em;text-transform:uppercase;color:var(--muted)}
.field .fh{font-size:11px;color:var(--muted)}
.field input,.field select,.field textarea{appearance:none;-webkit-appearance:none;border:1px solid var(--line);background:var(--panel-2);color:var(--ink);border-radius:6px;padding:9px 10px;font:inherit;font-size:13px;outline:none;width:100%;box-sizing:border-box;transition:border-color var(--t-micro),box-shadow var(--t-micro)}
.field input:focus,.field select:focus,.field textarea:focus{border-color:var(--accent);box-shadow:0 0 0 3px color-mix(in srgb,var(--accent) 18%,transparent)}
.field input[readonly]{background:color-mix(in srgb,var(--muted) 14%,var(--panel-2));color:var(--muted);cursor:not-allowed}
.guide-row{display:flex;gap:14px;padding:10px 0;border-bottom:1px solid var(--hairline);font-size:13px}
.guide-row:last-child{border:none}
.guide-row b{flex:0 0 88px;color:var(--muted);font-weight:700}
.guide-row span{color:var(--ink);word-break:break-all}
.code-block{margin:10px 0;padding:12px 14px;border-radius:10px;background:var(--panel-2);
 border:1px solid var(--line);color:var(--ink);font-family:"Consolas",monospace;font-size:12px;overflow:auto;white-space:pre-wrap}
[data-theme="day"] .code-block{background:#fff;color:var(--ink)}
[data-theme="space"] .code-block{background:color-mix(in srgb,var(--space-0) 70%,#000);color:var(--ink)}
.check{display:inline-flex;align-items:center;gap:8px;font-size:12px;font-weight:700;color:var(--ink-2);cursor:pointer}
.modal{position:fixed;inset:0;z-index:40;display:none;place-items:center;background:rgba(10,11,12,.72);backdrop-filter:blur(6px);padding:20px}
.modal.show{display:grid}
.dialog{width:min(560px,100%);background:var(--panel-solid);border:1px solid var(--line);border-radius:8px;box-shadow:var(--sh-lg);padding:20px}
.dialog h3{margin:0 0 14px;font-size:16px}
.dialog-actions{display:flex;justify-content:flex-end;gap:8px;margin-top:16px}
.login-shell{min-height:100vh;display:grid;place-items:center;padding:24px;position:relative;z-index:1}
.login-card{width:min(400px,100%);padding:24px;border-radius:8px;border:1px solid var(--line);
 background:var(--panel);box-shadow:var(--sh-lg)}
.login-card .brand-row{display:flex;align-items:center;gap:12px;margin-bottom:18px}
.login-card h1{font-size:18px;font-weight:800}
.login-card .sub{font-size:12px;color:var(--muted);margin-bottom:18px}
.login-card .error{min-height:18px;color:var(--danger);font-size:12px;font-weight:700;margin:8px 0 0}
.login-card .btn{width:100%;margin-top:12px;padding:11px;font-size:14px}
.hint{font-size:12px;color:var(--muted);line-height:1.5;margin-top:8px}
.notice{display:flex;gap:10px;padding:12px 14px;border-radius:12px;border:1px solid color-mix(in srgb,var(--warn) 35%,var(--line));
 background:color-mix(in srgb,var(--warn) 10%,transparent);color:var(--ink-2);font-size:12px;line-height:1.55;margin-top:12px}
.table-wrap{overflow-x:auto}
.star{cursor:pointer;color:color-mix(in srgb,var(--warn) 72%,var(--ink-2));background:transparent;border:0;padding:2px 8px;font-size:20px;line-height:1;transition:color var(--t-micro),transform var(--t-micro),text-shadow var(--t-micro)}
.star:hover{color:var(--warn);transform:scale(1.18);text-shadow:0 0 10px color-mix(in srgb,var(--warn) 45%,transparent)}
.star.on{color:var(--warn);text-shadow:0 0 8px color-mix(in srgb,var(--warn) 70%,transparent),0 0 18px color-mix(in srgb,var(--warn) 35%,transparent)}

/* production API/compat */
.hidden-select{position:absolute;width:1px;height:1px;opacity:0;pointer-events:none}
.scrim{position:fixed;inset:0;background:rgba(10,11,12,.58);z-index:35;opacity:0;pointer-events:none;transition:opacity .28s}
body.drawer-open .scrim{opacity:1;pointer-events:auto}
.hamburger{display:none}
@media(max-width:900px){
 .app{grid-template-columns:1fr}
 .sidebar{position:fixed;left:0;top:0;bottom:0;width:236px;z-index:40;transform:translateX(-105%);transition:transform .28s}
 body.drawer-open .sidebar{transform:none}
 .hamburger{display:grid;place-items:center;width:38px;height:38px;border-radius:10px;border:1px solid var(--line);background:var(--panel-2);color:var(--ink-2);cursor:pointer}
}
.ai-marks{display:inline-flex;gap:3px;flex-wrap:nowrap;white-space:nowrap;align-items:center}
.ai-mark{display:inline-grid;place-items:center;min-width:20px;height:18px;padding:0 4px;border-radius:5px;border:1px solid transparent;font-size:9px;font-weight:800}
.ai-mark .gl{display:none}
.ai-mark.ok{color:var(--ok);border-color:color-mix(in srgb,var(--ok) 42%,transparent);background:color-mix(in srgb,var(--ok) 14%,transparent)}
.ai-mark.bad{color:var(--danger);border-color:color-mix(in srgb,var(--danger) 42%,transparent);background:color-mix(in srgb,var(--danger) 12%,transparent)}
.ai-mark.na{color:var(--ai-unprobed);border-color:color-mix(in srgb,var(--ai-unprobed) 40%,transparent);background:color-mix(in srgb,var(--ai-unprobed) 10%,transparent);opacity:.75}
.th-ico{display:inline-flex;align-items:center;gap:5px;color:var(--muted)}
.th-ico svg{width:15px;height:15px}
.th-ico .tx{font-size:11px;font-weight:700}
.beam-swatch{width:16px;height:3px;background:var(--q-a);display:inline-block;border-radius:2px;box-shadow:var(--glow-a)}
#page-logs .card{display:flex;flex-direction:column;min-height:calc(100vh - 148px)}
#page-logs .card-b,#page-logs .card-body{flex:1;display:flex;flex-direction:column;min-height:0}
.logs{height:calc(100vh - 220px);min-height:420px;max-height:none}
.conn{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}
@media(max-width:560px){.conn{grid-template-columns:1fr}}
.conn-item{background:var(--panel-2);border:1px solid var(--line);border-radius:8px;padding:10px}
.conn-item .k{font-size:11px;color:var(--muted);font-weight:700;text-transform:uppercase;letter-spacing:.04em}
.conn-item .v{font-family:Consolas,monospace;font-weight:700;margin-top:6px;word-break:break-all}
.conn-item .desc{font-size:12px;color:var(--muted);margin-top:4px}
.cmd{margin-top:10px;padding:12px;border-radius:10px;background:var(--panel-2);color:var(--ink);border:1px solid var(--line);font-family:Consolas,monospace;font-size:12px;overflow:auto;white-space:pre-wrap}
[data-theme="day"] .cmd{background:#fff;color:var(--ink)}
[data-theme="space"] .cmd{background:color-mix(in srgb,var(--space-0) 70%,#000);color:var(--ink)}
.cmd-hint{font-size:12px;color:var(--muted);margin-top:8px;line-height:1.55}
/* 折叠：JS 切 body.sidebar-collapsed，与 .app grid 对齐 */
body.sidebar-collapsed .app{grid-template-columns:74px 1fr}
body.sidebar-collapsed .brand .bt,
body.sidebar-collapsed .navitem .t,
body.sidebar-collapsed .navitem .lbl,
body.sidebar-collapsed .collapse-btn .t,
body.sidebar-collapsed .collapse-btn .lbl,
body.sidebar-collapsed .sidefoot .pill .t,
body.sidebar-collapsed .sidefoot .pill .lbl,
body.sidebar-collapsed .nav .lab{opacity:0;width:0;overflow:hidden}
body.sidebar-collapsed .navitem,
body.sidebar-collapsed .collapse-btn{justify-content:center;gap:0;padding-left:0;padding-right:0}
body.sidebar-collapsed .collapse-btn .ico{transform:rotate(180deg)}
`

const dashboardJS = `let allProxies=[];let allRegions=[];let configCache=null;let publicIP='';let orbitSessions=[];let gatewayCC='';let uiLang='zh';let customStatus=null;let customStatusLoaded=false;let sessionCache=[];let sessionsLoaded=false;let apiKeyCache=[];let apiKeysLoaded=false;
const I18N={zh:{
nav_control:'总控',nav_overview:'总览',nav_nodes:'节点',nav_subs:'订阅',nav_sessions:'会话',nav_regions:'地域',nav_ops:'运维',nav_logs:'日志',nav_api:'API',nav_settings:'设置',collapse_menu:'收起菜单',menu:'菜单',menu_open:'打开导航菜单',
refresh_all:'全局刷新',github:'GitHub 仓库',theme:'切换主题',logout:'退出登录',
metric_upstreams:'上游节点',metric_upstreams_note:'可用 / 降级',metric_http:'HTTP 可用',metric_socks5:'SOCKS5 可用',metric_mixed_note:'含 mixed 双入口',metric_subs:'订阅可用',metric_subs_note:'订阅来源',metric_sessions:'活跃会话',metric_sessions_note:'当前绑定',
card_orbit:'节点分布',orbit_pause:'暂停动画',orbit_resume:'恢复动画',orbit_gateway:'网关',orbit_session_beam:'会话连线（越粗绑定越多）',
card_singbox:'sing-box 引擎',card_connect:'如何连接',card_connect_sub:'网关端口 + 认证',card_sessions:'活跃会话',card_regions:'地域分布',card_all_regions:'全部地域',card_nodes:'节点清单',card_subs:'订阅管理',card_session_monitor:'Session 监控',card_logs:'运行日志',card_openapi:'开放 API 说明',card_openapi_sub:'只读 · API Key 鉴权',card_apikeys:'API Key 管理',card_settings:'系统设置',card_settings_sub:'独立页面',
 btn_refresh:'刷新',btn_view_all:'查看全部',btn_batch_import:'批量导入',btn_batch_delete:'批量删除',btn_refresh_all:'刷新全部',btn_add_sub:'添加订阅',btn_add_node:'添加',btn_expand_all:'全部展开',btn_collapse_all:'全部折叠',btn_create:'创建',btn_save:'保存',btn_cancel:'取消',btn_ok:'确定',btn_copy:'复制',btn_enable:'启用',btn_disable:'停用',btn_test:'测试',btn_manage:'管理',btn_import:'导入',btn_delete_node:'删除节点',btn_copy_http:'复制 HTTP',btn_copy_socks:'复制 SOCKS5',btn_add:'添加',btn_close:'关闭',btn_delete:'删除',btn_unstar:'取消星标',
loading:'加载中',logs_autoscroll:'自动滚动',logs_empty:'暂无日志',session_hint:'仅展示 sticky 绑定：用户名含 -session-<id> 才进入亲和表；无 session 的请求不出现在此列表。',
th_session_id:'会话 ID',th_route:'路由标签',th_exit_region:'出口地域',th_exit_node:'出口节点',th_quality:'品质',th_latency:'延迟',th_ttl:'剩余 TTL',th_name:'名称',th_created:'创建时间',th_last_used:'末次使用',th_status:'状态',th_actions:'操作',
api_endpoints:'端点',api_auth:'鉴权',api_rate:'限流',api_rate_desc:'默认 60/min/key，推荐轮询 5–10 分钟',api_connect:'连接模式',api_connect_desc:'direct=直连节点地址；gateway=加密节点走网关，不下发 127.0.0.1',api_notice:'密钥仅存 SHA-256 hash；直连节点客户端可直连，加密节点必须走网关。',
  cfg_http_port:'HTTP 端口',cfg_socks_port:'SOCKS5 端口',cfg_webui_port:'WebUI 端口',cfg_auth:'代理认证',cfg_auth_user:'代理认证用户名',cfg_auth_pass:'代理认证密码（留空不改）',cfg_ttl:'Session TTL（分钟）',cfg_default_region:'默认地域',cfg_health:'健康检查间隔（分钟）',cfg_retry:'最大重试次数',cfg_singbox:'sing-box 路径（修改需重启）',cfg_allowed:'允许国家（逗号分隔，优先）',cfg_blocked:'屏蔽国家（逗号分隔）',cfg_readonly:'只读',cfg_readonly_redeploy:'只读，改端口需重新部署',cfg_notice:'保存失败时运行态会回滚，前端会明确报错，不会静默降级。',
opt_off:'关闭',opt_on:'开启',ph_apikey_name:'新 Key 名称',ph_region_global:'空=全局',
status_ok:'可用',status_paused:'已停用',status_sub_paused:'订阅已暂停',status_failed:'不可用',status_pending:'待验证',
source_manual:'手工',source_sub:'订阅',source_sub_paused:'（已暂停）',source_sub_unavail:'（不可选）',
copy_blocked_sub:'订阅已暂停，无法复制网关凭据',copy_blocked_paused:'节点已停用，无法复制网关凭据',copy_blocked_pending:'节点未验证，无法复制网关凭据',copy_blocked_failed:'节点不可用，无法复制网关凭据',
toast_refreshed:'数据已刷新',toast_copied_direct:'已复制直连地址',toast_copy_fail:'复制失败',toast_copied_pin:'已复制（锁定节点身份，非出口IP）',toast_copied_pw:'已复制，请将 PASSWORD 替换为真实密码',
err_status_refresh:'状态刷新失败',err_session_refresh:'会话刷新失败',err_log_refresh:'日志刷新失败',err_apikey_refresh:'API Key 刷新失败',err_open_settings:'打开设置失败',err_logout:'退出失败',err_op:'操作失败',
confirm_default:'确定执行此操作？',filter_all:'全部',filter_ok:'畅通',filter_bad:'阻断',filter_unprobed:'未探测',filter_unknown:'未知',
region_empty:'暂无可用地域数据',region_available:'个可用',region_regions:'个地域',region_nodes:'节点',region_code:'代码',region_name:'国家/地区',region_struct:'品质结构 · 平均延迟 · 会话',avg_latency:'平均延迟',sess_chip:'会话',
page_info_zero:'共 0 条',page_info:'共 {total} 条，显示 {from}-{to}',no_match:'没有匹配节点',no_subs:'暂无订阅，点右上角“添加订阅”',no_sessions:'暂无活跃 session',no_apikeys:'暂无 API Key',
 sub_active:'活跃',sub_paused_badge:'已暂停',sub_counts:'{a} 可用 / {p} 暂停 / {d} 不可用',
  aria_latency_min:'最小延迟',aria_latency_max:'最大延迟',aria_keyword_search:'搜索地址、备注或出口 IP',aria_manual_link:'手工节点链接',aria_region:'地域',aria_note:'备注',
  filter_all_protocol:'全部协议',filter_all_region:'全部地域',filter_all_status:'全部状态',filter_all_source:'全部来源',filter_all_quality:'全部延迟档',filter_all_purity:'全部纯净度',filter_protocol:'协议',filter_region:'地域',filter_status:'状态',filter_source:'来源',filter_quality:'延迟档',filter_purity:'纯净度',filter_purity_title:'按 ipapi.is abuse score 过滤',filter_purity_clean:'低风险 <0.10',filter_purity_caution:'中风险 0.10-0.50',filter_purity_risky:'高风险 >0.50',filter_latency_min:'延迟≥ms',filter_latency_max:'延迟≤ms',filter_keyword:'搜索地址 / 备注 / 出口 IP',filter_manual_link:'添加手工节点: http://host:port 或 socks5://host:port',filter_note:'备注',filter_ai_hint:'AI 解锁：ChatGPT / Claude / Grok / Gemini（绿色畅通 / 红色阻断 / 淡灰未探测）',filter_cf_all:'全部 Cloudflare',filter_cf_ok:'Cloudflare 畅通',filter_cf_bad:'Cloudflare 阻断',filter_cf_unknown:'Cloudflare 未知',filter_cf_title:'Cloudflare：全部/畅通/阻断/未知',filter_gpt_all:'ChatGPT 全部',filter_gpt_ok:'ChatGPT 畅通',filter_gpt_bad:'ChatGPT 阻断',filter_gpt_unprobed:'ChatGPT 未探测',filter_gpt_title:'ChatGPT：全部/畅通/阻断/未探测',filter_claude_all:'Claude 全部',filter_claude_ok:'Claude 畅通',filter_claude_bad:'Claude 阻断',filter_claude_unprobed:'Claude 未探测',filter_claude_title:'Claude：全部/畅通/阻断/未探测',filter_gemini_all:'Gemini 全部',filter_gemini_ok:'Gemini 畅通',filter_gemini_bad:'Gemini 阻断',filter_gemini_unprobed:'Gemini 未探测',filter_gemini_title:'Gemini：全部/畅通/阻断/未探测',filter_grok_all:'Grok 全部',filter_grok_ok:'Grok 畅通',filter_grok_bad:'Grok 阻断',filter_grok_unprobed:'Grok 未探测',filter_grok_title:'Grok：全部/畅通/阻断/未探测',
 singbox_engine:'sing-box 引擎',singbox_reason:'状态原因',singbox_nodes:'转换节点',singbox_ports:'端口就绪',singbox_subscriptions:'订阅可用',singbox_disabled:'暂停/不可用节点',singbox_total:'订阅总数',singbox_no_tunnel:'无需运行',singbox_running:'运行中',singbox_stopped:'已停止',singbox_partial:'部分就绪',singbox_failed:'启动失败',
 session_count:'{count} 条 sticky 绑定',session_unknown:'未知',session_id:'会话 ID',session_proxy_id:'绑定节点 Proxy ID',session_exit_node:'出口节点',session_exit_region:'出口地域',session_exit_location:'出口地点',session_exit_checked:'出口检查时间',session_selected_region:'选路地域',session_unlock:'解锁过滤',session_protocol:'协议',session_quality_latency:'品质 / 延迟',session_source:'来源',session_last_active:'最近活跃',session_ttl:'剩余 TTL',session_cooldown:'节点冷却',session_bind:'本机绑定',session_route:'路由标签',session_occupancy:'节点占用',session_occupancy_hint:'活跃 sticky / 最大会话数（0=无限）',session_no_unlock:'—',session_none:'暂无活跃 session',session_no_cooldown:'无',
 sub_edit:'修改',sub_delete:'删除',apikey_active:'有效',apikey_revoked:'已吊销',apikey_revoke:'吊销',apikey_delete:'删除',
 region_total_zero:'0 个可用',region_total:'{total} 个可用 · {regions} 个地域',region_total_top:'{total} 个可用 · Top {limit} / {regions}',region_session:'会话 {count}',region_avg_latency:'平均延迟 {value}',
  conn_socks5:'SOCKS5 代理',conn_socks5_desc:'raw TCP，HTTP/HTTPS 目标都可',conn_http:'HTTP 代理',conn_http_desc:'HTTPS 目标走 CONNECT 隧道',conn_user:'用户名（含路由 DSL）',conn_user_desc:'见下方 DSL 规则',conn_password:'密码',conn_auth_state:'代理认证状态',conn_password_hint:'见首次启动日志 / 系统设置',conn_auth_on:'代理认证：开启',conn_auth_off:'代理认证：关闭',conn_auth_disabled:'（认证已关闭，无需密码）',conn_exit_notice:'<b>「出口 IP」不是连接地址</b>，须走网关端口 + 认证。',dsl_hint_static:'路由 DSL 说明',dsl_enabled:'语法：{syntax}；固定顺序 region → unlock → node → session；前缀 “{base}” 是代理认证用户名。{nodeHint}',dsl_disabled:'代理认证当前关闭；启用后前缀须等于代理认证用户名。',dsl_node_hint:'key-<base64url(nodeKey)> 是稳定配置身份（优先）；host:port 是兼容入口地址。二者都不是最终出口 IP；无匹配节点时显式失败，不回退。node 锁定决定路由，优先于 session 黏连。',
  th_name_note:'名称 / 备注',th_protocol:'协议',th_region:'地域',th_exit_ip:'出口 IP<span class="muted"> (信息)</span>',th_abuse:'ipapi.is 滥用分<span class="muted"> /1.00</span>',th_ipapi_flags:'ip-api 标记',th_cf_title:'Cloudflare：畅通 / 阻断 / 未知',th_ai:'AI 解锁',th_source:'来源',select_all:'全选',pager_nodes:'节点列表分页',pager_prev:'上一页',pager_next:'下一页',pager_per_page:'每页',
  confirm_revoke_key:'吊销该 API Key？吊销后立即失效。',confirm_delete_key:'删除该 API Key？此操作不可恢复。',confirm_delete_sub:'删除此订阅及其全部节点？',confirm_delete_batch:'删除选中的 {n} 个节点？订阅节点删除后可能在下次刷新时重新出现。',confirm_delete_node:'删除此节点？删除后需重新添加或等待订阅刷新。',confirm_unstar:'取消该节点星标？',toast_revoked:'已吊销',toast_deleted:'已删除',toast_sub_deleted:'订阅已删除',toast_config_saved:'配置已保存',toast_copied:'已复制',toast_need_link:'请填写节点链接',toast_manual_added:'手工节点已添加',toast_select_nodes:'请先勾选节点',toast_batch_deleted:'已删除 {n} 个',toast_batch_failed:'，失败 {n}',toast_import_need:'请粘贴代理列表',toast_import_done:'导入完成：新增 {a} / 跳过 {s} / 失败 {f}',toast_invalid_node:'无效节点',toast_node_updated:'节点已更新',toast_node_deleted:'节点已删除',toast_node_enabled:'节点已启用',toast_node_disabled:'节点已停用',toast_test_started:'测试连通已启动，稍后自动刷新',toast_sub_need:'请填写订阅 URL 或粘贴配置内容',toast_sub_added:'订阅已添加',toast_invalid_sub:'无效订阅',toast_sub_updated:'订阅已修改，正在重新拉取',toast_refresh_started:'刷新已启动，稍后自动更新',toast_refresh_all_started:'全部刷新已启动，稍后自动更新',toast_sub_toggled:'已切换启用/暂停状态',toast_need_key_name:'请填写 Key 名称',toast_apikey_created:'API Key 已创建（仅显示一次）',toast_star_on:'已加星标',toast_star_off:'已取消星标',toast_no_nodekey:'无法复制：该网关节点缺少稳定 NodeKey，请刷新订阅或重新导入节点后重试',toast_nodekey_encode:'无法复制：该网关节点 NodeKey 无法编码，请刷新订阅或重新导入节点后重试',err_revoke:'吊销失败',err_delete:'删除失败',err_save:'保存失败',err_config_missing:'配置未加载',err_refresh:'刷新失败',err_add:'添加失败',err_batch_delete:'批量删除失败',err_import:'批量导入失败',err_test:'测试失败',err_toggle:'切换失败',err_star:'星标操作失败',err_public_ip:'公网 IP 获取失败',err_create_key:'创建 API Key 失败',err_update:'修改失败',badge_clean:'干净',badge_blocked:'拦截',badge_normal:'正常',ai_open:'畅通',ai_blocked:'阻断',ai_unprobed:'未探测',star_on:'取消星标',star_off:'加星标',star_aria:'星标',note_edit:'点击编辑备注',note_add:'点击添加备注',modal_import_title:'批量导入手工节点',modal_import_list:'代理列表（每行一条 socks5/http/https URL，支持前缀/行内/行尾说明）',modal_import_list_ph:'prefix socks5://1.2.3.4:1080 suffix\nhttp://5.6.7.8:8080 note',modal_node_title:'节点管理',modal_confirm_title:'确认',modal_proto_title:'选择复制协议',modal_proto_desc:'该节点为 mixed 双入口，可同时提供 SOCKS5 与 HTTP。请选择要复制的连接协议：',modal_sub_add:'添加订阅',modal_sub_edit:'修改订阅',modal_sub_refresh:'刷新间隔（分钟）',modal_sub_url:'订阅 URL',modal_sub_file:'或粘贴配置文件内容',modal_sub_headers:'自定义请求头（可选，JSON）',modal_apikey_created:'API Key 已创建',modal_apikey_once_hint:'明文仅显示一次，请立即复制保存。关闭后无法再次查看。',modal_apikey_once_label:'Key（仅显示一次）',ph_optional:'可选',ph_region_eg:'如 us / jp',ph_note_optional:'可选备注',orbit_nodes:'节点',orbit_grade:'档',orbit_sessions:'会话'
},en:{
nav_control:'Control',nav_overview:'Overview',nav_nodes:'Nodes',nav_subs:'Subscriptions',nav_sessions:'Sessions',nav_regions:'Regions',nav_ops:'Ops',nav_logs:'Logs',nav_api:'API',nav_settings:'Settings',collapse_menu:'Collapse',menu:'Menu',menu_open:'Open navigation menu',
refresh_all:'Refresh all',github:'GitHub repository',theme:'Toggle theme',logout:'Log out',
metric_upstreams:'Upstreams',metric_upstreams_note:'Active / degraded',metric_http:'HTTP available',metric_socks5:'SOCKS5 available',metric_mixed_note:'Includes mixed dual-entry',metric_subs:'Subscription nodes',metric_subs_note:'From subscriptions',metric_sessions:'Active sessions',metric_sessions_note:'Current bindings',
card_orbit:'Node map',orbit_pause:'Pause animation',orbit_resume:'Resume animation',orbit_gateway:'Gateway',orbit_session_beam:'Session links (thicker = more binds)',
card_singbox:'sing-box engine',card_connect:'How to connect',card_connect_sub:'Gateway ports + auth',card_sessions:'Active sessions',card_regions:'Region mix',card_all_regions:'All regions',card_nodes:'Node list',card_subs:'Subscriptions',card_session_monitor:'Session monitor',card_logs:'Runtime logs',card_openapi:'Open API',card_openapi_sub:'Read-only · API key auth',card_apikeys:'API keys',card_settings:'System settings',card_settings_sub:'Dedicated page',
 btn_refresh:'Refresh',btn_view_all:'View all',btn_batch_import:'Bulk import',btn_batch_delete:'Bulk delete',btn_refresh_all:'Refresh all',btn_add_sub:'Add subscription',btn_add_node:'Add',btn_expand_all:'Expand all',btn_collapse_all:'Collapse all',btn_create:'Create',btn_save:'Save',btn_cancel:'Cancel',btn_ok:'OK',btn_copy:'Copy',btn_enable:'Enable',btn_disable:'Disable',btn_test:'Test',btn_manage:'Manage',btn_import:'Import',btn_delete_node:'Delete node',btn_copy_http:'Copy HTTP',btn_copy_socks:'Copy SOCKS5',btn_add:'Add',btn_close:'Close',btn_delete:'Delete',btn_unstar:'Unstar',
loading:'Loading',logs_autoscroll:'Auto-scroll',logs_empty:'No logs',session_hint:'Sticky only: usernames with -session-<id> appear here; requests without session are omitted.',
th_session_id:'Session ID',th_route:'Route label',th_exit_region:'Exit region',th_exit_node:'Exit node',th_quality:'Quality',th_latency:'Latency',th_ttl:'TTL left',th_name:'Name',th_created:'Created',th_last_used:'Last used',th_status:'Status',th_actions:'Actions',
api_endpoints:'Endpoints',api_auth:'Auth',api_rate:'Rate limit',api_rate_desc:'Default 60/min/key; poll every 5–10 minutes',api_connect:'Connect modes',api_connect_desc:'direct=node address; gateway=encrypted via gateway (no 127.0.0.1)',api_notice:'Keys stored as SHA-256 only; direct nodes can be used directly; encrypted nodes must use the gateway.',
  cfg_http_port:'HTTP port',cfg_socks_port:'SOCKS5 port',cfg_webui_port:'WebUI port',cfg_auth:'Proxy auth',cfg_auth_user:'Proxy username',cfg_auth_pass:'Proxy password (leave blank to keep)',cfg_ttl:'Session TTL (minutes)',cfg_default_region:'Default region',cfg_health:'Health interval (minutes)',cfg_retry:'Max retries',cfg_singbox:'sing-box path (restart required)',cfg_allowed:'Allowed countries (comma, priority)',cfg_blocked:'Blocked countries (comma)',cfg_readonly:'Read-only',cfg_readonly_redeploy:'Read-only; redeploy to change ports',cfg_notice:'On save failure runtime rolls back and the UI reports the error (no silent degrade).',
opt_off:'Off',opt_on:'On',ph_apikey_name:'New key name',ph_region_global:'empty = global',
status_ok:'Available',status_paused:'Disabled',status_sub_paused:'Subscription paused',status_failed:'Unavailable',status_pending:'Unverified',
source_manual:'Manual',source_sub:'Subscription',source_sub_paused:' (paused)',source_sub_unavail:' (unavailable)',
copy_blocked_sub:'Subscription paused; cannot copy gateway credentials',copy_blocked_paused:'Node disabled; cannot copy gateway credentials',copy_blocked_pending:'Node unverified; cannot copy gateway credentials',copy_blocked_failed:'Node unavailable; cannot copy gateway credentials',
toast_refreshed:'Data refreshed',toast_copied_direct:'Copied direct address',toast_copy_fail:'Copy failed',toast_copied_pin:'Copied (node identity pin, not exit IP)',toast_copied_pw:'Copied; replace PASSWORD with the real password',
err_status_refresh:'Status refresh failed',err_session_refresh:'Session refresh failed',err_log_refresh:'Log refresh failed',err_apikey_refresh:'API key refresh failed',err_open_settings:'Failed to open settings',err_logout:'Logout failed',err_op:'Operation failed',
confirm_default:'Confirm this action?',filter_all:'All',filter_ok:'Open',filter_bad:'Blocked',filter_unprobed:'Unprobed',filter_unknown:'Unknown',
region_empty:'No available region data',region_available:' available',region_regions:' regions',region_nodes:'nodes',region_code:'Code',region_name:'Country/Region',region_struct:'Quality · avg latency · sessions',avg_latency:'Avg latency',sess_chip:'Sessions',
page_info_zero:'0 items',page_info:'{total} items, showing {from}-{to}',no_match:'No matching nodes',no_subs:'No subscriptions. Use Add subscription.',no_sessions:'No sticky sessions',no_apikeys:'No API keys',
 sub_active:'Active',sub_paused_badge:'Paused',sub_counts:'{a} up / {p} paused / {d} down',
  aria_latency_min:'Min latency',aria_latency_max:'Max latency',aria_keyword_search:'Search address, note, or exit IP',aria_manual_link:'Manual node link',aria_region:'Region',aria_note:'Note',
  filter_all_protocol:'All protocols',filter_all_region:'All regions',filter_all_status:'All statuses',filter_all_source:'All sources',filter_all_quality:'All latency grades',filter_all_purity:'All risk levels',filter_protocol:'Protocol',filter_region:'Region',filter_status:'Status',filter_source:'Source',filter_quality:'Latency grade',filter_purity:'Risk level',filter_purity_title:'Filter by ipapi.is abuse score',filter_purity_clean:'Low risk <0.10',filter_purity_caution:'Medium risk 0.10-0.50',filter_purity_risky:'High risk >0.50',filter_latency_min:'Latency ≥ ms',filter_latency_max:'Latency ≤ ms',filter_keyword:'Search address / note / exit IP',filter_manual_link:'Add manual node: http://host:port or socks5://host:port',filter_note:'Note',filter_ai_hint:'AI access: ChatGPT / Claude / Grok / Gemini (green available / red blocked / gray unprobed)',filter_cf_all:'All Cloudflare',filter_cf_ok:'Cloudflare open',filter_cf_bad:'Cloudflare blocked',filter_cf_unknown:'Cloudflare unknown',filter_cf_title:'Cloudflare: all / open / blocked / unknown',filter_gpt_all:'ChatGPT all',filter_gpt_ok:'ChatGPT open',filter_gpt_bad:'ChatGPT blocked',filter_gpt_unprobed:'ChatGPT unprobed',filter_gpt_title:'ChatGPT: all / open / blocked / unprobed',filter_claude_all:'Claude all',filter_claude_ok:'Claude open',filter_claude_bad:'Claude blocked',filter_claude_unprobed:'Claude unprobed',filter_claude_title:'Claude: all / open / blocked / unprobed',filter_gemini_all:'Gemini all',filter_gemini_ok:'Gemini open',filter_gemini_bad:'Gemini blocked',filter_gemini_unprobed:'Gemini unprobed',filter_gemini_title:'Gemini: all / open / blocked / unprobed',filter_grok_all:'Grok all',filter_grok_ok:'Grok open',filter_grok_bad:'Grok blocked',filter_grok_unprobed:'Grok unprobed',filter_grok_title:'Grok: all / open / blocked / unprobed',
 singbox_engine:'sing-box engine',singbox_reason:'Status reason',singbox_nodes:'Tunnel nodes',singbox_ports:'Ready ports',singbox_subscriptions:'Available subscriptions',singbox_disabled:'Paused/unavailable nodes',singbox_total:'Subscriptions total',singbox_no_tunnel:'Not needed',singbox_running:'Running',singbox_stopped:'Stopped',singbox_partial:'Partially ready',singbox_failed:'Start failed',
 session_count:'{count} sticky bindings',session_unknown:'Unknown',session_id:'Session ID',session_proxy_id:'Bound proxy ID',session_exit_node:'Exit node',session_exit_region:'Exit region',session_exit_location:'Exit location',session_exit_checked:'Exit checked',session_selected_region:'Selected routing region',session_unlock:'Unlock filter',session_protocol:'Protocol',session_quality_latency:'Quality / latency',session_source:'Source',session_last_active:'Last active',session_ttl:'TTL left',session_cooldown:'Node cooldown',session_bind:'Local bind',session_route:'Route label',session_occupancy:'Node occupancy',session_occupancy_hint:'active sticky / max sessions (0 = unlimited)',session_no_unlock:'—',session_none:'No active sessions',session_no_cooldown:'None',
 sub_edit:'Edit',sub_delete:'Delete',apikey_active:'Active',apikey_revoked:'Revoked',apikey_revoke:'Revoke',apikey_delete:'Delete',
 region_total_zero:'0 available',region_total:'{total} available · {regions} regions',region_total_top:'{total} available · Top {limit} / {regions}',region_session:'{count} sessions',region_avg_latency:'Avg latency {value}',
  conn_socks5:'SOCKS5 proxy',conn_socks5_desc:'raw TCP; works for HTTP/HTTPS targets',conn_http:'HTTP proxy',conn_http_desc:'HTTPS targets use CONNECT tunnel',conn_user:'Username (routing DSL)',conn_user_desc:'See DSL rules below',conn_password:'Password',conn_auth_state:'Proxy auth state',conn_password_hint:'See first-start logs / system settings',conn_auth_on:'Proxy auth: on',conn_auth_off:'Proxy auth: off',conn_auth_disabled:'(authentication disabled; no password needed)',conn_exit_notice:'<b>Exit IP is not the connection address</b>; use gateway ports + auth.',dsl_hint_static:'Routing DSL notes',dsl_enabled:'Syntax: {syntax}; fixed order region → unlock → node → session; prefix “{base}” is the proxy username. {nodeHint}',dsl_disabled:'Proxy authentication is disabled. When enabled, the prefix must match the proxy username.',dsl_node_hint:'key-<base64url(nodeKey)> is the stable configuration identity (preferred); host:port is a compatible entry address. Neither is the final exit IP. No matching node fails explicitly with no fallback. Node pinning takes precedence over session affinity.',
  th_name_note:'Name / note',th_protocol:'Protocol',th_region:'Region',th_exit_ip:'Exit IP<span class="muted"> (info)</span>',th_abuse:'ipapi.is abuse<span class="muted"> /1.00</span>',th_ipapi_flags:'ip-api flags',th_cf_title:'Cloudflare: open / blocked / unknown',th_ai:'AI access',th_source:'Source',select_all:'Select all',pager_nodes:'Node list pagination',pager_prev:'Prev',pager_next:'Next',pager_per_page:'Per page',
  confirm_revoke_key:'Revoke this API key? It becomes invalid immediately.',confirm_delete_key:'Delete this API key? This cannot be undone.',confirm_delete_sub:'Delete this subscription and all of its nodes?',confirm_delete_batch:'Delete {n} selected node(s)? Subscription nodes may reappear on the next refresh.',confirm_delete_node:'Delete this node? You must re-add it or wait for subscription refresh.',confirm_unstar:'Remove star from this node?',toast_revoked:'Revoked',toast_deleted:'Deleted',toast_sub_deleted:'Subscription deleted',toast_config_saved:'Settings saved',toast_copied:'Copied',toast_need_link:'Enter a node link',toast_manual_added:'Manual node added',toast_select_nodes:'Select nodes first',toast_batch_deleted:'Deleted {n}',toast_batch_failed:', failed {n}',toast_import_need:'Paste a proxy list',toast_import_done:'Import done: added {a} / skipped {s} / failed {f}',toast_invalid_node:'Invalid node',toast_node_updated:'Node updated',toast_node_deleted:'Node deleted',toast_node_enabled:'Node enabled',toast_node_disabled:'Node disabled',toast_test_started:'Connectivity test started; list will refresh shortly',toast_sub_need:'Enter a subscription URL or paste config content',toast_sub_added:'Subscription added',toast_invalid_sub:'Invalid subscription',toast_sub_updated:'Subscription updated; re-fetching',toast_refresh_started:'Refresh started; updates shortly',toast_refresh_all_started:'Full refresh started; updates shortly',toast_sub_toggled:'Enabled/paused state toggled',toast_need_key_name:'Enter a key name',toast_apikey_created:'API key created (shown once)',toast_star_on:'Starred',toast_star_off:'Unstarred',toast_no_nodekey:'Cannot copy: gateway node has no stable NodeKey; refresh subscription or re-import',toast_nodekey_encode:'Cannot copy: NodeKey encoding failed; refresh subscription or re-import',err_revoke:'Revoke failed',err_delete:'Delete failed',err_save:'Save failed',err_config_missing:'Config not loaded',err_refresh:'Refresh failed',err_add:'Add failed',err_batch_delete:'Bulk delete failed',err_import:'Bulk import failed',err_test:'Test failed',err_toggle:'Toggle failed',err_star:'Star action failed',err_public_ip:'Public IP lookup failed',err_create_key:'Create API key failed',err_update:'Update failed',badge_clean:'Clean',badge_blocked:'Blocked',badge_normal:'OK',ai_open:'open',ai_blocked:'blocked',ai_unprobed:'unprobed',star_on:'Unstar',star_off:'Star',star_aria:'Star',note_edit:'Click to edit note',note_add:'Click to add note',modal_import_title:'Bulk import manual nodes',modal_import_list:'Proxy list (one socks5/http/https URL per line; prefix/inline/trailing notes allowed)',modal_import_list_ph:'prefix socks5://1.2.3.4:1080 suffix\nhttp://5.6.7.8:8080 note',modal_node_title:'Manage node',modal_confirm_title:'Confirm',modal_proto_title:'Choose protocol to copy',modal_proto_desc:'This node is a mixed dual-entry endpoint (SOCKS5 + HTTP). Choose which protocol to copy:',modal_sub_add:'Add subscription',modal_sub_edit:'Edit subscription',modal_sub_refresh:'Refresh interval (minutes)',modal_sub_url:'Subscription URL',modal_sub_file:'Or paste config content',modal_sub_headers:'Custom request headers (optional, JSON)',modal_apikey_created:'API key created',modal_apikey_once_hint:'Plaintext is shown once. Copy it now; it cannot be viewed again after close.',modal_apikey_once_label:'Key (shown once)',ph_optional:'Optional',ph_region_eg:'e.g. us / jp',ph_note_optional:'Optional note',orbit_nodes:'nodes',orbit_grade:'grade',orbit_sessions:'sessions'
}};
function t(key){const pack=I18N[uiLang]||I18N.zh;if(pack[key]!=null)return pack[key];const zh=I18N.zh[key];return zh!=null?zh:key}
function tf(key,vars){let s=t(key);if(vars)Object.keys(vars).forEach(function(k){s=s.split('{'+k+'}').join(String(vars[k]))});return s}
function pageTitleOf(name){const map={overview:'nav_overview',nodes:'nav_nodes',regions:'nav_regions',subs:'nav_subs',sessions:'nav_sessions',logs:'nav_logs',api:'nav_api',settings:'nav_settings'};return t(map[name]||name)}
function applyLang(lang){uiLang=(lang==='en'?'en':'zh');try{localStorage.setItem('gg-lang',uiLang)}catch(e){}document.documentElement.lang=uiLang==='en'?'en':'zh-CN';document.documentElement.setAttribute('data-lang',uiLang);const code=document.getElementById('lang-code');if(code)code.textContent=uiLang==='en'?'中文':'EN';document.querySelectorAll('[data-i18n]').forEach(function(el){const k=el.getAttribute('data-i18n');if(!k)return;const val=t(k);if(el.childElementCount&&!el.getAttribute('data-i18n-force')){/* keep nested HTML for complex hints */}else{el.textContent=val}});document.querySelectorAll('[data-i18n-html]').forEach(function(el){const k=el.getAttribute('data-i18n-html');if(k)el.innerHTML=t(k)});document.querySelectorAll('[data-i18n-title]').forEach(function(el){const k=el.getAttribute('data-i18n-title');if(k)el.title=t(k)});document.querySelectorAll('[data-i18n-aria]').forEach(function(el){const k=el.getAttribute('data-i18n-aria');if(k)el.setAttribute('aria-label',t(k))});document.querySelectorAll('[data-i18n-placeholder]').forEach(function(el){const k=el.getAttribute('data-i18n-placeholder');if(k)el.placeholder=t(k)});const title=document.getElementById('pageTitle');const active=document.querySelector('.navitem.active');if(title&&active&&active.dataset.tab)title.textContent=pageTitleOf(active.dataset.tab);const op=document.getElementById('orbit-pause-btn');if(op)op.textContent=t(orbitPaused?'orbit_resume':'orbit_pause');try{initFilterToggles()}catch(e){}try{renderProxies(true)}catch(e){}try{renderRegions()}catch(e){}try{if(customStatusLoaded)renderCustomStatus(customStatus)}catch(e){}try{if(sessionsLoaded)renderSessions(sessionCache)}catch(e){}try{if(subscriptionsLoaded)renderSubscriptions()}catch(e){}try{if(apiKeysLoaded)renderAPIKeys(apiKeyCache)}catch(e){}try{renderConnection()}catch(e){}}
function toggleLang(){applyLang(uiLang==='zh'?'en':'zh')}
function switchTab(name){document.querySelectorAll('.navitem').forEach(t=>t.classList.toggle('active',t.dataset.tab===name));document.querySelectorAll('.page').forEach(p=>p.classList.toggle('active',p.id==='page-'+name));const title=document.getElementById('pageTitle');if(title)title.textContent=pageTitleOf(name);if(name==='settings'){runAsync(t('err_open_settings'),async()=>{if(!configCache)await loadConfig();await loadAPIKeys()})}if(name==='logs'){runAsync(t('err_log_refresh'),async()=>{await loadLogs();const auto=document.getElementById('logs-autoscroll');if(auto&&auto.checked)stickLogsToBottom(document.getElementById('logs-box'))})}try{markViewLazy(name)}catch(e){}closeDrawer()}
function showToast(msg){const el=document.getElementById('toast');el.textContent=msg;el.classList.add('show');setTimeout(()=>el.classList.remove('show'),2600)}
// showConfirm: 应用内确认弹窗，替换浏览器原生 confirm()。返回 Promise<Boolean>。
function showConfirm(msg,okLabel){return new Promise(function(resolve){const modal=document.getElementById('confirm-modal');const msgEl=document.getElementById('confirm-modal-msg');const okBtn=document.getElementById('confirm-modal-ok');const cancelBtn=document.getElementById('confirm-modal-cancel');if(!modal||!msgEl||!okBtn||!cancelBtn){resolve(false);return}msgEl.textContent=String(msg||t('confirm_default'));okBtn.textContent=String(okLabel||t('btn_ok'));const cancelLabel=document.getElementById('confirm-modal-cancel');if(cancelLabel&&!okLabel){/* keep */}function cleanup(val){modal.classList.remove('show');okBtn.onclick=null;cancelBtn.onclick=null;resolve(val)}okBtn.onclick=function(){cleanup(true)};cancelBtn.onclick=function(){cleanup(false)};modal.classList.add('show')})}
// showProtocolPick: mixed 节点复制时的协议选择，返回 'socks5' | 'http' | ''（取消）。
// 禁止用浏览器 confirm 的「确定/取消」冒充两种协议。
function showProtocolPick(){return new Promise(function(resolve){const modal=document.getElementById('protocol-pick-modal');const socksBtn=document.getElementById('protocol-pick-socks');const httpBtn=document.getElementById('protocol-pick-http');const cancelBtn=document.getElementById('protocol-pick-cancel');if(!modal||!socksBtn||!httpBtn||!cancelBtn){resolve('');return}function cleanup(val){modal.classList.remove('show');socksBtn.onclick=null;httpBtn.onclick=null;cancelBtn.onclick=null;resolve(val)}socksBtn.onclick=function(){cleanup('socks5')};httpBtn.onclick=function(){cleanup('http')};cancelBtn.onclick=function(){cleanup('')};modal.classList.add('show')})}
async function api(path, options){const res=await fetch(path, Object.assign({headers:{'Content-Type':'application/json'}}, options||{}));if(res.status===401){location.href='/login';return null}const text=await res.text();let data={};if(text){try{data=JSON.parse(text)}catch(err){if(!res.ok)throw new Error(res.statusText||('HTTP '+res.status));throw new Error(t('err_op'))}}if(!res.ok)throw new Error(data.error||res.statusText||('HTTP '+res.status));return data}
function safe(value){return value===undefined||value===null||value===''?'--':String(value)}
function html(value){return safe(value).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
function errorMessage(err){return err&&err.message?err.message:String(err||t('err_op'))}
async function runAsync(label, fn){try{return await fn()}catch(err){showToast((label?label+(uiLang==='en'?': ':'：'):'')+errorMessage(err));return null}}
async function logout(){return runAsync(t('err_logout'),async()=>{const res=await fetch('/logout',{method:'POST'});if(!res.ok)throw new Error(res.statusText||('HTTP '+res.status));location.href='/login'})}
function refreshLater(){setTimeout(()=>runAsync(t('err_refresh'),()=>Promise.all([loadSubscriptions(),loadStats(),loadProxies()])),4000)}
function maskAddress(address){if(!address)return '--';const parts=String(address).split(':');const host=parts[0]||address;if(host.length<=8)return host+(parts[1]?':'+parts[1]:'');return host.slice(0,4)+'...'+host.slice(-4)+(parts[1]?':'+parts[1]:'')}
function addressArg(address){return encodeURIComponent(String(address||'')).replace(/[!'()*]/g,c=>'%'+c.charCodeAt(0).toString(16).toUpperCase())}
function proxyIDArg(proxy){const id=Number(proxy&&proxy.id);return Number.isFinite(id)?String(id):'0'}
function regionOf(proxy){const region=String((proxy&&proxy.region)||'').trim().toLowerCase();return region||'unknown'}
function isKnownRegion(proxy){const region=regionOf(proxy);return region&&region!=='unknown'}
function isUserPaused(p){return !!(p&&(p.user_paused===true||Number(p.user_paused)===1))}
// 订阅节点仅在父订阅存在且为 active 时参与「可用」统计；与 CountAll/选路 scope 一致。
function isParentSubscriptionSelectable(p){if(!p||p.source!=='subscription')return true;return String(p.subscription_status||'').trim().toLowerCase()==='active'}
function isParentSubscriptionPaused(p){return !!(p&&p.source==='subscription'&&String(p.subscription_status||'').trim().toLowerCase()==='paused')}
function isAvailable(proxy){return isParentSubscriptionSelectable(proxy)&&!isUserPaused(proxy)&&(proxy.status==='active'||proxy.status==='degraded')&&Number(proxy.fail_count||0)<3}
function stripColon(port){return String(port||'').replace(/^:/,'')}
async function refreshAll(){return runAsync(t('err_op'),async()=>{await Promise.all([loadStats(),loadProxies(),loadSubscriptions(),loadConfig(),loadSessions(),loadLogs(),loadCustomStatus()]);loadPublicIP();showToast(t('toast_refreshed'))})}
function renderCustomStatus(st){const box=document.getElementById('singbox-status');if(!box||!st)return;const status=String(st.singbox_status||(st.singbox_running?'running':'stopped'));const reason=String(st.singbox_reason||status);const statusText={no_tunnel_nodes:t('singbox_no_tunnel'),running:t('singbox_running'),stopped:t('singbox_stopped'),partial:t('singbox_partial'),failed:t('singbox_failed')}[status]||status;const dotClass={no_tunnel_nodes:'idle',running:'on',stopped:'idle',partial:'warn',failed:'off'}[status]||'idle';const dot='<span class="dot '+dotClass+'"></span>';box.innerHTML='<div class="kv"><span>'+dot+html(t('singbox_engine'))+'</span><span class="v">'+html(statusText)+'</span></div>'+'<div class="kv"><span class="k">'+html(t('singbox_reason'))+'</span><span class="v">'+html(reason)+'</span></div>'+'<div class="kv"><span class="k">'+html(t('singbox_nodes'))+'</span><span class="v">'+html(safe(st.singbox_nodes))+'</span></div>'+'<div class="kv"><span class="k">'+html(t('singbox_ports'))+'</span><span class="v">'+html(safe(st.singbox_ready_ports))+'/'+html(safe(st.singbox_total_ports))+'</span></div>'+'<div class="kv"><span class="k">'+html(t('singbox_subscriptions'))+'</span><span class="v">'+html(safe(st.subscription_count))+'</span></div>'+'<div class="kv"><span class="k">'+html(t('singbox_disabled'))+'</span><span class="v">'+html(safe(st.disabled_count))+'</span></div>'+'<div class="kv"><span class="k">'+html(t('singbox_total'))+'</span><span class="v">'+html(safe(st.subscription_total))+'</span></div>'}
async function loadCustomStatus(){const st=await api('/api/custom/status');if(!st)return;customStatus=st;customStatusLoaded=true;renderCustomStatus(customStatus)}
function normalizeTheme(theme){const t=String(theme||'');if(t==='day'||t==='light')return 'day';return 'space'}
function applyTheme(theme){const th=normalizeTheme(theme);document.documentElement.setAttribute('data-theme',th);try{localStorage.setItem('gg-theme',th)}catch(e){}const btn=document.getElementById('theme-toggle');if(btn){btn.title=t('theme');btn.setAttribute('aria-label',t('theme'));const lbl=btn.querySelector('.lbl');if(lbl)lbl.remove();Array.from(btn.childNodes).forEach(function(n){if(n.nodeType===3&&String(n.textContent||'').trim())n.remove()})}try{if(document.getElementById('orbit-stage'))renderOrbitSystem()}catch(e){}}
function toggleTheme(){const cur=normalizeTheme(document.documentElement.getAttribute('data-theme'));applyTheme(cur==='space'?'day':'space')}
(function(){let th='space';try{th=localStorage.getItem('gg-theme')||'space'}catch(e){}applyTheme(th)})();
async function loadStats(){const stats=await api('/api/stats');if(!stats)return;document.getElementById('stat-total').textContent=safe(stats.total);document.getElementById('stat-http').textContent=safe(stats.http);document.getElementById('stat-socks5').textContent=safe(stats.socks5);document.getElementById('stat-subscription').textContent=safe(stats.subscription_count);document.getElementById('stat-sessions').textContent=safe(stats.active_sessions)}
async function loadProxies(){const data=await api('/api/proxies');if(!data)return;allProxies=Array.isArray(data)?data:[];allRegions=Array.from(new Set(allProxies.filter(p=>isAvailable(p)&&isKnownRegion(p)).map(regionOf))).sort();renderRegionFilter();renderProxies(true);renderRegions();renderOrbitSystem()}
function renderRegionFilter(){const select=document.getElementById('region-filter');const current=select.value;select.innerHTML='<option value="">'+html(t('filter_all_region'))+'</option>'+allRegions.map(r=>'<option value="'+html(r)+'">'+html(r).toUpperCase()+'</option>').join('');select.value=allRegions.includes(current)?current:''}
function sourceLabel(p){if(p&&p.source==='manual')return t('source_manual');if(p&&p.source==='subscription'){const name=String((p.subscription_name||'').trim()||t('source_sub'));if(isParentSubscriptionPaused(p))return name+t('source_sub_paused');if(!isParentSubscriptionSelectable(p))return name+t('source_sub_unavail');return name}if(Number(p&&p.subscription_id)>0)return String((p.subscription_name||'').trim()||t('source_sub'));return p&&p.source?String(p.source):t('source_manual')}
function nodeLabel(p){return String((p&&p.note)||'').trim()}
// 节点状态：父订阅 paused→sub_paused；user_paused→paused；可用→ok；disabled 且从未验证→pending；否则不可用。
// last_check 空/零值视为未验证（待验证），有 last_check 或 fail_count≥3 视为验证失败。
function hasLastCheck(p){const v=p&&p.last_check;if(v==null||v===''||v===false)return false;const s=String(v);if(s.indexOf('0001-')===0||s.indexOf('1970-01-01')===0)return false;return true}
function nodeState(p){if(isParentSubscriptionPaused(p))return 'sub_paused';if(p&&p.source==='subscription'&&!isParentSubscriptionSelectable(p))return 'failed';if(isUserPaused(p)||p.status==='paused')return 'paused';if(isAvailable(p))return 'ok';if(Number(p.fail_count||0)>=3)return 'failed';if(p.status==='disabled')return hasLastCheck(p)?'failed':'pending';return 'pending'}
function stateBadge(st){switch(st){case 'ok':return '<span class="badge ok">'+html(t('status_ok'))+'</span>';case 'sub_paused':return '<span class="badge warn">'+html(t('status_sub_paused'))+'</span>';case 'paused':return '<span class="badge warn">'+html(t('status_paused'))+'</span>';case 'failed':return '<span class="badge danger">'+html(t('status_failed'))+'</span>';default:return '<span class="badge gray">'+html(t('status_pending'))+'</span>'}}
// abuserBadge: ipapi.is abuser_score<0 显示 "--"（未探测/查询失败）；否则显示 0.00-1.00 两位小数 + 颜色。
// 阈值：<0.10 绿(ok)、0.10-0.50 黄(warn)、>0.50 红(danger)。两源分开展示，不与 ip-api 聚合。
function abuserBadge(score){const n=Number(score);if(!Number.isFinite(n)||n<0)return '<span class="muted">--</span>';const cls=n<0.1?'ok':(n<=0.5?'warn':'danger');return '<span class="badge '+cls+'">'+html(n.toFixed(2))+'</span>'}
// ipapiFlagsBadges: ip-api 命中标记逗号串。proxy 红、hosting 黄、mobile 灰；seen=true 且无命中显"干净"绿；未探测显 "--"。
function ipapiFlagsBadges(flags,seen){const raw=String(flags||'').trim();if(raw===''){return seen?'<span class="badge ok">'+html(t('badge_clean'))+'</span>':'<span class="muted">--</span>'}const cls={proxy:'danger',hosting:'warn',mobile:'gray'};return raw.split(',').map(f=>f.trim()).filter(Boolean).map(f=>'<span class="badge '+(cls[f]||'gray')+'">'+html(f)+'</span>').join(' ')}
// cfBadge: cf_blocked==1 显"拦截"红、==0 显"正常"绿、其它(-1/未探测)显 "--"。
function cfBadge(v){ v=Number(v); if(v===1)return '<span class="badge danger">'+html(t('badge_blocked'))+'</span>'; if(v===0)return '<span class="badge ok">'+html(t('badge_normal'))+'</span>'; return '<span class="muted">--</span>' }
// AI 状态只接受后端合同中的数字 0/1；缺字段、布尔值、坏 JSON 均保持未探测。
function parseAIReachability(v){if(v&&typeof v==='object'&&!Array.isArray(v))return v;const raw=String(v||'').trim();if(!raw)return null;try{const m=JSON.parse(raw);return m&&typeof m==='object'&&!Array.isArray(m)?m:null}catch(e){return null}}
function aiValueState(v){if(v===0||v==='0')return 'unlocked';if(v===1||v==='1')return 'blocked';return 'unprobed'}
function aiBadges(v){const m=parseAIReachability(v);if(!m)return '<span class="muted">--</span>';const defs=[['openai','GPT','ChatGPT'],['claude','Cld','Claude'],['grok','Grk','Grok'],['gemini','Gem','Gemini']];return '<span class="ai-marks">'+defs.map(function(d){const state=aiValueState(m[d[0]]);const cls=state==='unlocked'?'ok':(state==='blocked'?'bad':'na');const glyph=state==='unlocked'?'✓':(state==='blocked'?'✗':'–');const title=d[2]+' '+(state==='unlocked'?t('ai_open'):(state==='blocked'?t('ai_blocked'):t('ai_unprobed')));return '<span class="ai-mark '+cls+'" role="img" aria-label="'+html(title)+'" title="'+html(title)+'"><span class="nm">'+d[1]+'</span><span class="gl" aria-hidden="true">'+glyph+'</span></span>'}).join('')+'</span>'}
function aiStateOf(p,svc){const m=parseAIReachability(p&&p.ai_reachability);return aiValueState(m&&m[svc])}
function cfStateOf(p){const v=p&&p.cf_blocked;if(v===0||v==='0')return 'unlocked';if(v===1||v==='1')return 'blocked';return 'unknown'}
function qualityOf(p){return String((p&&p.quality_grade)||'').trim().toUpperCase()}
function purityStateOf(p){const raw=p&&p.ipapiis_score;if(raw===null||raw===undefined||raw===''||typeof raw==='boolean')return 'unprobed';const n=Number(raw);if(!Number.isFinite(n)||n<0)return 'unprobed';if(n<0.1)return 'clean';if(n<=0.5)return 'caution';return 'risky'}
function filterVal(id){const el=document.getElementById(id);return el?String(el.value||'').trim():''}
// starBtn: 星标切换按钮，★ 已加星 / ☆ 未加星。
function starBtn(p){ const id=proxyIDArg(p); const on=!!(p.starred===true||Number(p.starred)===1); return '<button class="star'+(on?' on':'')+'" onclick="toggleStar('+id+','+(on?'true':'false')+')" title="'+(on?t('star_on'):t('star_off'))+'" aria-label="'+t('star_aria')+'">'+(on?'★':'☆')+'</button>' }
// randSession: 随机 6 位字母数字，用于复制凭据的 session 段。
function randSession(){ const cs='abcdefghijklmnopqrstuvwxyz0123456789'; let s=''; for(let i=0;i<6;i++)s+=cs[Math.floor(Math.random()*cs.length)]; return s }
// isDualProtocol: 节点是否为 sing-box mixed 入站(单端口同时服务 SOCKS5+HTTP)。
// 读存储层显式下发的 dual_protocol 字段,而非靠地址长相猜测——手动本机 direct socks5 节点
// 地址同为回环但只支持单协议,只有此显式标记能可靠区分。
function isDualProtocol(p){return !!(p&&(p.dual_protocol===true||Number(p.dual_protocol)===1))}
function nodeSupportsInboundProtocol(p,protocol){const wanted=String(protocol||'').trim().toLowerCase();if(!wanted)return true;const actual=String((p&&p.protocol)||'').trim().toLowerCase();const dual=!!(p&&(p.dual_protocol===true||p.dual_protocol===1));if(dual)return wanted==='http'||wanted==='socks5';return actual===wanted}
// protocolBadges: 协议列徽章。dual_protocol 节点(mixed 入站)渲染 SOCKS5+HTTP 两个徽章;
// 其余节点按存储的单一 protocol 渲染一个徽章(沿用 html 转义)。
function protocolBadges(p){ if(isDualProtocol(p))return '<span class="badge blue">SOCKS5</span> <span class="badge blue">HTTP</span>'; return '<span class="badge blue">'+html(p.protocol).toUpperCase()+'</span>' }
// isGatewayNode: dual_protocol(mixed 隧道)或回环本地地址必须经网关 DSL 连接；其余为可直连上游。
function isGatewayNode(p){if(isDualProtocol(p))return true;const a=String((p&&p.address)||'');return a.indexOf('127.0.0.1:')===0||a.indexOf('[::1]:')===0||a.indexOf('localhost:')===0}
function isDirectNode(p){return !isGatewayNode(p)}
// copyProxyCred: 直连节点复制 protocol://host:port（无网关密码）；网关节点必须有稳定 NodeKey 才复制 DSL。
// mixed 节点经应用内弹窗选择 SOCKS5/HTTP，禁止浏览器 confirm 的确定/取消冒充协议。
// 成功 toast 不回显含真实密码的完整 URL。
function encodeProxyUserInfo(value){return encodeURIComponent(String(value||'')).replace(/[!'()*]/g,c=>'%'+c.charCodeAt(0).toString(16).toUpperCase())}
function encodeNodeKeyPin(nkey){nkey=String(nkey||'').trim();if(!nkey)return '';try{return btoa(unescape(encodeURIComponent(nkey))).replace(/\+/g,'-').replace(/\//g,'_').replace(/=+$/,'')}catch(e){return ''}}
function gatewayCopyBlockedReason(p){const st=nodeState(p);if(st==='ok')return '';if(st==='sub_paused')return t('copy_blocked_sub');if(st==='paused')return t('copy_blocked_paused');if(st==='pending')return t('copy_blocked_pending');return t('copy_blocked_failed')}
async function copyProxyCred(id){const p=allProxies.find(x=>Number(x.id)===Number(id));if(!p)return;const addr=String(p.address||'');if(isDirectNode(p)){const scheme=String(p.protocol||'socks5');const u=String(p.username||'');const url=u?(scheme+'://'+encodeProxyUserInfo(u)+':'+encodeProxyUserInfo(String(p.password||''))+'@'+addr):(scheme+'://'+addr);try{await navigator.clipboard.writeText(url);showToast(t('toast_copied_direct'))}catch(e){showToast(t('toast_copy_fail'))}return}const blocked=gatewayCopyBlockedReason(p);if(blocked){showToast(blocked);return}const nkey=String(p.node_key||'').trim();if(!nkey){showToast(t('toast_no_nodekey'));return}let scheme=String(p.protocol||'socks5');if(isDualProtocol(p)){scheme=await showProtocolPick();if(scheme!=='socks5'&&scheme!=='http')return}const base=(configCache&&configCache.proxy_auth_username)?configCache.proxy_auth_username:'username';const pin='key-'+encodeNodeKeyPin(nkey);if(pin==='key-'){showToast(t('toast_nodekey_encode'));return}const user=base+'-node-'+pin;const rawPass=(configCache&&configCache.proxy_auth_password)?configCache.proxy_auth_password:'';const pass=rawPass||'PASSWORD';const host=publicIP||location.hostname||'127.0.0.1';const port=scheme==='http'?(stripColon((configCache&&configCache.http_port)||'7802')):(stripColon((configCache&&configCache.socks5_port)||'7801'));const url=scheme+'://'+encodeProxyUserInfo(user)+':'+encodeProxyUserInfo(pass)+'@'+host+':'+port;const okMsg=rawPass?t('toast_copied_pin'):t('toast_copied_pw');try{await navigator.clipboard.writeText(url);showToast(okMsg)}catch(e){showToast(t('toast_copy_fail'))}}
// toggleStar: 加星直接生效；取消星标须 confirm() 确认。
async function toggleStar(id,on){if(on){const ok=await showConfirm(t('confirm_unstar'),t('btn_unstar'));if(!ok)return}return runAsync(t('err_star'),async()=>{await api('/api/proxy/star',{method:'POST',body:JSON.stringify({id,starred:!on})});await loadProxies();showToast(on?t('toast_star_off'):t('toast_star_on'))})}
// keepPage=true：数据刷新（10s 轮询）保留当前页，避免把用户从第 N 页踢回第 1 页。
// 筛选/搜索 onchange 不传参 → 重置到第 1 页。
function renderProxies(keepPage){if(!keepPage)proxyPage=1;const protocol=document.getElementById('protocol-filter').value;const region=document.getElementById('region-filter').value;const sf=document.getElementById('status-filter').value;const srcf=(document.getElementById('source-filter')||{}).value||'';const qf=filterVal('quality-filter');const pf=filterVal('purity-filter');const cff=filterVal('cf-filter');const aif={openai:filterVal('ai-openai-filter'),claude:filterVal('ai-claude-filter'),grok:filterVal('ai-grok-filter'),gemini:filterVal('ai-gemini-filter')};const latMinRaw=filterVal('latency-min');const latMaxRaw=filterVal('latency-max');const latMin=latMinRaw===''?null:Number(latMinRaw);const latMax=latMaxRaw===''?null:Number(latMaxRaw);const kw=filterVal('keyword-filter').toLowerCase();let rows=allProxies.filter(p=>nodeSupportsInboundProtocol(p,protocol)&&(!region||regionOf(p)===region));if(sf)rows=rows.filter(p=>nodeState(p)===sf);if(srcf==='manual')rows=rows.filter(p=>p.source==='manual');else if(srcf==='subscription')rows=rows.filter(p=>p.source!=='manual');if(qf)rows=rows.filter(p=>qualityOf(p)===qf);if(pf)rows=rows.filter(p=>purityStateOf(p)===pf);if(cff)rows=rows.filter(p=>cfStateOf(p)===cff);['openai','claude','grok','gemini'].forEach(function(svc){const v=aif[svc];if(v)rows=rows.filter(p=>aiStateOf(p,svc)===v)});if(latMin!==null&&Number.isFinite(latMin))rows=rows.filter(p=>{const n=Number(p.latency);return Number.isFinite(n)&&n>0&&n>=latMin});if(latMax!==null&&Number.isFinite(latMax))rows=rows.filter(p=>{const n=Number(p.latency);return Number.isFinite(n)&&n>0&&n<=latMax});if(kw)rows=rows.filter(p=>{const addr=String(p.address||'').toLowerCase();const note=String(p.note||'').toLowerCase();const exitIP=String(p.exit_ip||'').toLowerCase();return addr.indexOf(kw)>=0||note.indexOf(kw)>=0||exitIP.indexOf(kw)>=0});const order={ok:0,pending:1,paused:2,sub_paused:2,failed:3};rows.sort((a,b)=>{const fa=(nodeState(a)==='ok'&&(a.starred===true||Number(a.starred)===1))?1:0;const fb=(nodeState(b)==='ok'&&(b.starred===true||Number(b.starred)===1))?1:0;if(fa!==fb)return fb-fa;const sa=nodeState(a),sb=nodeState(b);if(order[sa]!==order[sb])return order[sa]-order[sb];return Number(a.latency||1e9)-Number(b.latency||1e9)});const body=document.getElementById('proxy-rows');if(rows.length===0){proxyRenderRows=[];proxyPage=1;const bodyEmpty=document.getElementById('proxy-rows');if(bodyEmpty)bodyEmpty.innerHTML='<tr><td colspan="14" class="empty">'+html(t('no_match'))+'</td></tr>';renderProxyPager();return}proxyRenderRows=rows;const pages=proxyTotalPages();if(proxyPage>pages)proxyPage=pages;if(proxyPage<1)proxyPage=1;renderProxyPage()}
// ISO 3166-1 alpha-2 → 中文国家/地区（代理出口常见码；未知码回退大写代码）。
// 含波罗的海/巴尔干/中亚等节点池高频码，避免地域分布只显示 LV/EE。
const REGION_ZH={us:'美国',ca:'加拿大',mx:'墨西哥',br:'巴西',ar:'阿根廷',cl:'智利',co:'哥伦比亚',pe:'秘鲁',uy:'乌拉圭',py:'巴拉圭',ec:'厄瓜多尔',ve:'委内瑞拉',bo:'玻利维亚',pa:'巴拿马',cr:'哥斯达黎加',do:'多米尼加',pr:'波多黎各',jm:'牙买加',cu:'古巴',gt:'危地马拉',hn:'洪都拉斯',sv:'萨尔瓦多',ni:'尼加拉瓜',bz:'伯利兹',bs:'巴哈马',bb:'巴巴多斯',tt:'特立尼达和多巴哥',gy:'圭亚那',sr:'苏里南',cw:'库拉索',aw:'阿鲁巴',gb:'英国',uk:'英国',ie:'爱尔兰',fr:'法国',de:'德国',nl:'荷兰',be:'比利时',lu:'卢森堡',ch:'瑞士',at:'奥地利',li:'列支敦士登',it:'意大利',es:'西班牙',pt:'葡萄牙',ad:'安道尔',mc:'摩纳哥',sm:'圣马力诺',mt:'马耳他',cy:'塞浦路斯',gi:'直布罗陀',gg:'根西',je:'泽西',im:'马恩岛',se:'瑞典',no:'挪威',dk:'丹麦',fi:'芬兰',is:'冰岛',fo:'法罗群岛',gl:'格陵兰',pl:'波兰',cz:'捷克',sk:'斯洛伐克',hu:'匈牙利',ro:'罗马尼亚',bg:'保加利亚',gr:'希腊',si:'斯洛文尼亚',hr:'克罗地亚',ba:'波黑',rs:'塞尔维亚',me:'黑山',mk:'北马其顿',al:'阿尔巴尼亚',xk:'科索沃',md:'摩尔多瓦',by:'白俄罗斯',ua:'乌克兰',ru:'俄罗斯',lv:'拉脱维亚',ee:'爱沙尼亚',lt:'立陶宛',tr:'土耳其',ge:'格鲁吉亚',am:'亚美尼亚',az:'阿塞拜疆',jp:'日本',kr:'韩国',cn:'中国',hk:'香港',tw:'台湾',mo:'澳门',mn:'蒙古',sg:'新加坡',my:'马来西亚',th:'泰国',vn:'越南',id:'印度尼西亚',ph:'菲律宾',bn:'文莱',kh:'柬埔寨',la:'老挝',mm:'缅甸',tl:'东帝汶',in:'印度',pk:'巴基斯坦',bd:'孟加拉',lk:'斯里兰卡',np:'尼泊尔',af:'阿富汗',kz:'哈萨克斯坦',uz:'乌兹别克斯坦',kg:'吉尔吉斯斯坦',tj:'塔吉克斯坦',tm:'土库曼斯坦',au:'澳大利亚',nz:'新西兰',fj:'斐济',pg:'巴布亚新几内亚',nc:'新喀里多尼亚',pf:'法属波利尼西亚',gu:'关岛',mp:'北马里亚纳',as:'美属萨摩亚',vi:'美属维尔京',za:'南非',eg:'埃及',ma:'摩洛哥',tn:'突尼斯',dz:'阿尔及利亚',ly:'利比亚',ng:'尼日利亚',gh:'加纳',ke:'肯尼亚',tz:'坦桑尼亚',ug:'乌干达',et:'埃塞俄比亚',mz:'莫桑比克',ao:'安哥拉',cm:'喀麦隆',sn:'塞内加尔',ci:'科特迪瓦',sc:'塞舌尔',mu:'毛里求斯',re:'留尼汪',il:'以色列',jo:'约旦',lb:'黎巴嫩',iq:'伊拉克',ir:'伊朗',ae:'阿联酋',sa:'沙特阿拉伯',qa:'卡塔尔',kw:'科威特',bh:'巴林',om:'阿曼',ye:'也门'};
function regionDisplayName(code){const c=String(code||'').trim().toLowerCase();if(!c||c==='unknown')return t('session_unknown');if(uiLang==='en')return c.toUpperCase();return REGION_ZH[c]||c.toUpperCase()}
// 地域聚合：按可用节点数倒序；总览最多 TopN，不足则全显；全量页展示全部。
function buildRegionStats(){const map={};allProxies.filter(p=>isAvailable(p)&&isKnownRegion(p)).forEach(p=>{const r=regionOf(p);if(!map[r])map[r]={region:r,count:0,latSum:0,latN:0,s:0,a:0,b:0,c:0,d:0,sess:0};const st=map[r];st.count++;const lat=Number(p.latency||0);if(lat>0){st.latSum+=lat;st.latN++}const g=qualityOf(p);if(g==='S')st.s++;else if(g==='A')st.a++;else if(g==='B')st.b++;else if(g==='C')st.c++;else if(g==='D')st.d++});const sessByR={};(Array.isArray(orbitSessions)?orbitSessions:[]).forEach(s=>{const r=String((s&&s.region)||'').trim().toLowerCase();if(!r||r==='unknown')return;sessByR[r]=(sessByR[r]||0)+1});Object.keys(map).forEach(function(r){map[r].sess=sessByR[r]||0;map[r].avgLat=map[r].latN?Math.round(map[r].latSum/map[r].latN):0});return Object.keys(map).map(function(k){return map[k]}).sort(function(a,b){return b.count-a.count||a.region.localeCompare(b.region)})}
function regionRowHTML(item,maxN){const pct=maxN?Math.round(item.count*100/maxN):0;const avg=item.avgLat>0?(item.avgLat+' ms'):'--';const chips=[];if(item.s)chips.push('<span class="badge qs">S '+html(item.s)+'</span>');if(item.a)chips.push('<span class="badge qa">A '+html(item.a)+'</span>');if(item.b)chips.push('<span class="badge qb">B '+html(item.b)+'</span>');if(item.c)chips.push('<span class="badge qc">C '+html(item.c)+'</span>');if(item.d)chips.push('<span class="badge qd">D '+html(item.d)+'</span>');if(item.sess)chips.push('<span class="badge ok">'+html(tf('region_session',{count:item.sess}))+'</span>');chips.push('<span class="badge gray">'+html(tf('region_avg_latency',{value:avg}))+'</span>');return '<div class="region"><span class="cc">'+html(item.region).toUpperCase()+'</span><span class="name">'+html(regionDisplayName(item.region))+'</span><div class="meta"><div class="bar"><i style="width:'+pct+'%"></i></div><div class="chips">'+chips.join('')+'</div></div><div class="n"><span class="big num">'+html(item.count)+'</span><span class="sub">'+html(t('region_nodes'))+'</span></div></div>'}
function regionPanelHTML(entries,maxN,limit){if(!entries.length)return '<div class="empty">'+html(t('region_empty'))+'</div>';const rows=typeof limit==='number'?entries.slice(0,limit):entries;return '<div class="region-head"><span>'+html(t('region_code'))+'</span><span>'+html(t('region_name'))+'</span><span>'+html(t('region_struct'))+'</span><span>'+html(t('region_nodes'))+'</span></div>'+rows.map(function(item){return regionRowHTML(item,maxN)}).join('')}
// 总览最多展示 REGION_OVERVIEW_LIMIT 个；不足时展示全部，不写死“Top 10”。
const REGION_OVERVIEW_LIMIT=10;
function renderRegions(){const entries=buildRegionStats();const total=entries.reduce(function(sum,item){return sum+item.count},0);const n=entries.length;const showN=Math.min(REGION_OVERVIEW_LIMIT,n);const totalText=!n?t('region_total_zero'):(n<=REGION_OVERVIEW_LIMIT?tf('region_total',{total:total,regions:n}):tf('region_total_top',{total:total,limit:REGION_OVERVIEW_LIMIT,regions:n}));const totalEl=document.getElementById('region-total');if(totalEl)totalEl.textContent=totalText;const pageTotal=document.getElementById('region-page-total');if(pageTotal)pageTotal.textContent=!n?t('region_total_zero'):tf('region_total',{total:total,regions:n});const list=document.getElementById('region-list');const pageList=document.getElementById('region-page-list');const maxN=entries.reduce(function(m,item){return Math.max(m,item.count)},0);if(list)list.innerHTML=regionPanelHTML(entries,maxN,showN);if(pageList)pageList.innerHTML=regionPanelHTML(entries,maxN)}
// 总览节点分布：按地域+延迟档聚合圆点，有 session 的地域画连线。
// renderWorldMap 保留为旧调用名，实际转调 renderOrbitSystem。
const ORBIT_TRACKS={s:{rr:0.42,w:15,dir:1,phase:0},a:{rr:0.60,w:11,dir:-1,phase:40},b:{rr:0.78,w:8.5,dir:1,phase:15},c:{rr:0.96,w:6.5,dir:-1,phase:70}};
const ORBIT_QVAR={s:'var(--q-s)',a:'var(--q-a)',b:'var(--q-b)',c:'var(--q-c)'};
let orbitSats=[];let orbitT=0;let orbitLast=0;let orbitPaused=false;let orbitRAF=0;let orbitBuilt=false;
function orbitQualityTrack(p){const g=qualityOf(p);if(g==='S'||g==='A'||g==='B'||g==='C'||g==='D')return g.toLowerCase();const lat=Number(p&&p.latency||0);if(lat>0&&lat<=200)return 's';if(lat>0&&lat<=500)return 'a';if(lat>0&&lat<=1000)return 'b';if(lat>0&&lat<=2000)return 'c';return 'd'}
// 会话出口品质档：与卫星轨道同一阈值；仅返回 s/a/b/c（D 不上轨，返回空由调用方回退）。
function orbitSessionQualityTrack(s){const g=String((s&&s.quality_grade)||'').trim().toUpperCase();if(g==='S'||g==='A'||g==='B'||g==='C')return g.toLowerCase();if(g==='D')return '';const lat=Number(s&&s.latency||0);if(lat>0&&lat<=200)return 's';if(lat>0&&lat<=500)return 'a';if(lat>0&&lat<=1000)return 'b';if(lat>0&&lat<=2000)return 'c';return ''}
// 优先会话自身品质；否则用 proxy_id 反查节点；仍无则回退到该地区节点最多的 S–C 轨道（避免 US 会话因品质空/D 完全不连线）。
function orbitSessionBeamKey(s,regionTracks){const r=String((s&&s.region)||'').trim().toLowerCase();if(!r||r==='unknown')return '';let q=orbitSessionQualityTrack(s);if(!q&&s&&Number(s.proxy_id)>0){const p=allProxies.find(function(x){return Number(x.id)===Number(s.proxy_id)});if(p){const pq=orbitQualityTrack(p);if(pq&&pq!=='d')q=pq}}if(q&&q!=='d'&&regionTracks[r]&&regionTracks[r][q])return r+'|'+q;if(regionTracks[r]){const order=['s','a','b','c'];let best='',bestN=-1;for(let i=0;i<order.length;i++){const t=order[i];const n=regionTracks[r][t]||0;if(n>bestN){bestN=n;best=t}}if(best&&bestN>0)return r+'|'+best}return ''}
function orbitStageGeom(){const st=document.getElementById('orbit-stage');const w=st?st.clientWidth:600;const h=st?st.clientHeight:338;return {cx:w/2,cy:h/2,halfW:w/2,halfH:h/2}}
function orbitAngAbsDiff(a,b){let d=a-b;while(d>Math.PI)d-=Math.PI*2;while(d<-Math.PI)d+=Math.PI*2;return Math.abs(d)}
function orbitRibbonPath(sx,sy,x,y,baseW,phase,widthScale,wind,lens){const dx=x-sx,dy=y-sy;const len=Math.hypot(dx,dy)||1;const ux=dx/len,uy=dy/len;const nx=-uy,ny=ux;const SEG=20;const swing=Math.min(len*0.038,6.5)*(0.85+0.15*(0.5+0.5*Math.sin(phase*0.32)));const side=Math.sin(phase*0.34);const wScale=widthScale==null?1:widthScale;const top=[],bot=[];let windHitMax=0;for(let i=0;i<=SEG;i++){const tt=i/SEG;let bend=swing*side*Math.sin(tt*Math.PI);let px=sx+ux*len*tt+nx*bend;let py=sy+uy*len*tt+ny*bend;let thin=1;if(wind&&wind.r>0){const rdx=px-wind.ox,rdy=py-wind.oy;const dist=Math.hypot(rdx,rdy)||1;const ang=Math.atan2(rdy,rdx);if(orbitAngAbsDiff(ang,wind.angle)<=wind.halfAperture){const u=(dist-wind.r)/Math.max(8,wind.band);const axis=1-orbitAngAbsDiff(ang,wind.angle)/Math.max(1e-4,wind.halfAperture);const hit=Math.exp(-u*u)*Math.pow(Math.max(0,axis),0.7);if(hit>0.02){if(hit>windHitMax)windHitMax=hit;const push=wind.force*hit;const tx=-wind.wy,ty=wind.wx;px+=wind.wx*push+tx*push*0.18*side;py+=wind.wy*push+ty*push*0.18*side;thin*=1-0.82*hit}}}if(lens&&lens.rx>0&&lens.ry>0){const ldx=px-lens.lx,ldy=py-lens.ly;const nr=Math.hypot(ldx/lens.rx,ldy/lens.ry);if(nr<1.35){const fall=Math.exp(-2.2*nr*nr);const w=fall*lens.strength;const rlen=Math.hypot(ldx,ldy)||1;const radial=Math.sin(lens.phase*0.7)*0.55;px+=(ldx/rlen)*w*radial*lens.rx*0.22;py+=(ldy/rlen)*w*radial*lens.ry*0.22;const txx=-ldy/rlen,tyy=ldx/rlen;const swirl=Math.sin(nr*Math.PI*1.1+lens.phase*0.9)*0.35;px+=txx*w*swirl*lens.rx*0.12;py+=tyy*w*swirl*lens.ry*0.12;thin*=1+0.12*fall*lens.strength}}const envelope=Math.pow(Math.sin(tt*Math.PI),0.95);const travel=0.92+0.08*Math.sin(tt*Math.PI-phase);const breath=0.97+0.03*Math.sin(phase*0.45);const hw=Math.max(0.12,(baseW*0.5)*envelope*travel*breath*wScale*Math.max(0.06,thin));top.push([px+nx*hw,py+ny*hw]);bot.push([px-nx*hw,py-ny*hw])}return {d:orbitRibbonSmooth(top,bot),windHit:windHitMax}}
function orbitRibbonSmooth(top,bot){function append(d,pts,move){if(pts.length<2)return d;if(move)d+='M'+pts[0][0].toFixed(1)+' '+pts[0][1].toFixed(1);for(let i=0;i<pts.length-1;i++){const p0=pts[Math.max(0,i-1)],p1=pts[i],p2=pts[i+1],p3=pts[Math.min(pts.length-1,i+2)];const c1x=p1[0]+(p2[0]-p0[0])/6,c1y=p1[1]+(p2[1]-p0[1])/6;const c2x=p2[0]-(p3[0]-p1[0])/6,c2y=p2[1]-(p3[1]-p1[1])/6;d+=' C'+c1x.toFixed(1)+' '+c1y.toFixed(1)+' '+c2x.toFixed(1)+' '+c2y.toFixed(1)+' '+p2[0].toFixed(1)+' '+p2[1].toFixed(1)}return d}let d=append('',top,true);d=append(d,bot.slice().reverse(),false);return d+' Z'}
// orbitClosedSmooth: 闭合 Catmull-Rom→Bézier，末点回绕首点，得到无折角的平滑闭合轮廓。
function orbitClosedSmooth(pts){const n=pts.length;if(n<3)return '';let d='M'+pts[0][0].toFixed(1)+' '+pts[0][1].toFixed(1);for(let i=0;i<n;i++){const p0=pts[(i-1+n)%n],p1=pts[i],p2=pts[(i+1)%n],p3=pts[(i+2)%n];const c1x=p1[0]+(p2[0]-p0[0])/6,c1y=p1[1]+(p2[1]-p0[1])/6;const c2x=p2[0]-(p3[0]-p1[0])/6,c2y=p2[1]-(p3[1]-p1[1])/6;d+=' C'+c1x.toFixed(1)+' '+c1y.toFixed(1)+' '+c2x.toFixed(1)+' '+c2y.toFixed(1)+' '+p2[0].toFixed(1)+' '+p2[1].toFixed(1)}return d+' Z'}
// orbitLensBlobPath: 采样 N=12 点，半径按固定相位的正弦项微扰成温和鹅卵石轮廓，再平滑闭合。
// seed 相位固定（每次透镜生成时播种一次），故呼吸缩放时轮廓稳定不抖动。
function orbitLensBlobPath(cx,cy,rx,ry,seed){const N=12;const pts=[];const terms=(seed&&seed.length)?seed:[];for(let i=0;i<N;i++){const th=i/N*Math.PI*2;let rf=1;for(let k=0;k<terms.length;k++){rf+=terms[k].a*Math.sin(terms[k].f*th+terms[k].p)}if(rf<0.6)rf=0.6;pts.push([cx+rx*rf*Math.cos(th),cy+ry*rf*Math.sin(th)])}return orbitClosedSmooth(pts)}
function orbitSetGrad(id,sx,sy,x,y){const g=document.getElementById(id);if(!g)return;g.setAttribute('x1',sx.toFixed(1));g.setAttribute('y1',sy.toFixed(1));g.setAttribute('x2',x.toFixed(1));g.setAttribute('y2',y.toFixed(1))}
// 偶发装饰粒子流（约 5 分钟一次，首波约 18 秒）；纯视觉，不表示业务状态。
const SOLAR_WIND={active:false,front:0,duration:4.8,nextIn:18,period:300,strength:1,angle:0,halfAperture:0.18,band:38,streams:null};
function spawnWindStreams(){const n=7+Math.floor(Math.random()*5);const kinds=['spiral','hook','wave','s'];const arr=[];for(let i=0;i<n;i++){const t=(i/(n-1||1))-0.5;arr.push({da:t*SOLAR_WIND.halfAperture*2.1+(Math.random()-0.5)*0.04,w:0.9+Math.random()*1.6,op:0.22+Math.random()*0.38,core:Math.abs(t)<0.1,kind:kinds[Math.floor(Math.random()*kinds.length)],twist:(0.9+1.4*Math.random())*(Math.random()<0.5?-1:1),hook:0.55+0.9*Math.random(),waves:1.2+1.6*Math.random(),amp:0.10+0.18*Math.random(),phase:Math.random()*Math.PI*2,seed:Math.random()*10,curveSide:Math.random()<0.5?-1:1})}return arr}
function windPlumePath(cx,cy,halfW,halfH,angle,halfA,p){const edge=0.1,steps=16;const a0=angle-halfA*1.25,a1=angle+halfA*1.25;const x0=cx+halfW*edge*Math.cos(a0),y0=cy+halfH*edge*Math.sin(a0);const x3=cx+halfW*edge*Math.cos(a1),y3=cy+halfH*edge*Math.sin(a1);let d='M'+x0.toFixed(1)+' '+y0.toFixed(1);const n0x=-Math.sin(a0),n0y=Math.cos(a0);const bulge=Math.min(halfW,halfH)*0.05*p;const m0=0.45;const cx0=cx+halfW*(edge+(p-edge)*m0)*Math.cos(a0)+n0x*bulge;const cy0=cy+halfH*(edge+(p-edge)*m0)*Math.sin(a0)+n0y*bulge;const x1=cx+halfW*p*Math.cos(a0),y1=cy+halfH*p*Math.sin(a0);d+=' Q'+cx0.toFixed(1)+' '+cy0.toFixed(1)+' '+x1.toFixed(1)+' '+y1.toFixed(1);for(let i=1;i<=steps;i++){const tt=a0+(a1-a0)*i/steps;const rp=p*(1+0.03*Math.sin(i*1.7+angle));d+=' L'+(cx+halfW*rp*Math.cos(tt)).toFixed(1)+' '+(cy+halfH*rp*Math.sin(tt)).toFixed(1)}const n1x=-Math.sin(a1),n1y=Math.cos(a1);const cx1=cx+halfW*(edge+(p-edge)*m0)*Math.cos(a1)+n1x*bulge;const cy1=cy+halfH*(edge+(p-edge)*m0)*Math.sin(a1)+n1y*bulge;d+=' Q'+cx1.toFixed(1)+' '+cy1.toFixed(1)+' '+x3.toFixed(1)+' '+y3.toFixed(1)+' Z';return d}
function windStreamCurve(cx,cy,halfW,halfH,angle,p,s,timeP){const a0=angle+s.da;const edge=0.1;const SEG=10;const pts=[];const baseAmp=s.amp*Math.min(halfW,halfH)*Math.max(0.15,p);for(let i=0;i<=SEG;i++){const tt=i/SEG;const r=edge+(p-edge)*tt;let a=a0;let nOff=0;if(s.kind==='spiral'){a=a0+s.twist*tt*tt;nOff=baseAmp*(0.35+0.65*tt)*Math.sin(s.phase+tt*Math.PI*s.waves+timeP*1.8)}else if(s.kind==='hook'){const hookT=Math.max(0,(tt-0.55)/0.45);const hookEase=hookT*hookT*(3-2*hookT);a=a0+(s.curveSide||1)*s.hook*hookEase*1.1;nOff=baseAmp*0.45*Math.sin(s.phase+tt*2)*(1-hookEase*0.3)}else if(s.kind==='s'){a=a0+s.twist*0.25*Math.sin(tt*Math.PI);nOff=baseAmp*Math.sin(tt*Math.PI*2+s.phase+timeP)*(0.5+0.5*tt)}else{a=a0+s.da*0.2*Math.sin(tt*Math.PI);nOff=baseAmp*Math.sin(tt*Math.PI*s.waves+s.phase+timeP*2.1)*(0.4+0.6*tt)}nOff+=baseAmp*0.12*Math.sin(s.seed+tt*5+timeP*3);const ux=Math.cos(a),uy=Math.sin(a);const nx=-uy,ny=ux;pts.push([cx+halfW*r*ux+nx*nOff,cy+halfH*r*uy+ny*nOff])}if(pts.length<2)return '';let d='M'+pts[0][0].toFixed(1)+' '+pts[0][1].toFixed(1);for(let i=0;i<pts.length-1;i++){const p0=pts[Math.max(0,i-1)],p1=pts[i],p2=pts[i+1],p3=pts[Math.min(pts.length-1,i+2)];const c1x=p1[0]+(p2[0]-p0[0])/6,c1y=p1[1]+(p2[1]-p0[1])/6;const c2x=p2[0]-(p3[0]-p1[0])/6,c2y=p2[1]-(p3[1]-p1[1])/6;d+=' C'+c1x.toFixed(1)+' '+c1y.toFixed(1)+' '+c2x.toFixed(1)+' '+c2y.toFixed(1)+' '+p2[0].toFixed(1)+' '+p2[1].toFixed(1)}return d}
function updateSolarWind(dt,halfW,halfH,cx,cy){if(!SOLAR_WIND.active){SOLAR_WIND.nextIn-=dt;if(SOLAR_WIND.nextIn<=0){SOLAR_WIND.active=true;SOLAR_WIND.front=0;SOLAR_WIND.strength=0.85+0.15*Math.random();SOLAR_WIND.angle=Math.random()*Math.PI*2;SOLAR_WIND.halfAperture=(10+Math.random()*8)*Math.PI/180;SOLAR_WIND.band=32+18*Math.random();SOLAR_WIND.streams=spawnWindStreams();SOLAR_WIND.nextIn=SOLAR_WIND.period+(Math.random()*80-40)}}else{SOLAR_WIND.front+=dt/SOLAR_WIND.duration;if(SOLAR_WIND.front>=1){SOLAR_WIND.active=false;SOLAR_WIND.front=0;SOLAR_WIND.streams=null}}const g=document.getElementById('orbit-wind');const streamsG=document.getElementById('orbit-wind-streams');const plume=document.getElementById('orbit-wind-plume');if(g&&streamsG&&plume){if(SOLAR_WIND.active&&SOLAR_WIND.streams){const p=SOLAR_WIND.front;const ease=1-Math.pow(1-Math.min(1,p),1.45);const fade=Math.sin(Math.min(1,p)*Math.PI);const op=(0.4+0.55*fade)*SOLAR_WIND.strength;g.setAttribute('opacity',op.toFixed(3));const maxRx=halfW*1.12,maxRy=halfH*1.12;plume.setAttribute('d',windPlumePath(cx,cy,maxRx,maxRy,SOLAR_WIND.angle,SOLAR_WIND.halfAperture,ease));let streamHtml='';SOLAR_WIND.streams.forEach(s=>{if(s.curveSide==null)s.curveSide=s.twist>=0?1:-1;const d=windStreamCurve(cx,cy,maxRx,maxRy,SOLAR_WIND.angle,ease,s,p);if(!d)return;const sw=(1.1+s.w)*(0.75+0.25*fade);streamHtml+='<path class="orbit-wind-stream" d="'+d+'" stroke="url(#orbitWindStream)" stroke-width="'+(sw*2.2).toFixed(2)+'" stroke-opacity="'+(s.op*0.3*fade).toFixed(2)+'"/>';streamHtml+='<path class="'+(s.core?'orbit-wind-stream-core':'orbit-wind-stream')+'" d="'+d+'" stroke="'+(s.core?'#d8eaff':'url(#orbitWindStream)')+'" stroke-width="'+sw.toFixed(2)+'" stroke-opacity="'+(s.op*fade).toFixed(2)+'"/>'});streamsG.innerHTML=streamHtml;SOLAR_WIND._r=ease*Math.sqrt(halfW*halfH)*1.08;SOLAR_WIND._ox=cx;SOLAR_WIND._oy=cy;SOLAR_WIND._wx=Math.cos(SOLAR_WIND.angle);SOLAR_WIND._wy=Math.sin(SOLAR_WIND.angle)}else{g.setAttribute('opacity','0');streamsG.innerHTML='';plume.setAttribute('d','');SOLAR_WIND._r=0}}if(!SOLAR_WIND.active)return null;return{ox:SOLAR_WIND._ox,oy:SOLAR_WIND._oy,r:SOLAR_WIND._r||0,band:SOLAR_WIND.band,force:28*SOLAR_WIND.strength,angle:SOLAR_WIND.angle,halfAperture:SOLAR_WIND.halfAperture,wx:SOLAR_WIND._wx||0,wy:SOLAR_WIND._wy||0}}
// 偶发光晕扭曲（约 30 分钟一次，首波约 45 秒）；纯视觉，不表示业务状态。
const GRAV_LENS={active:false,life:0,duration:8,nextIn:45,period:1800,strength:0,phase:0,lx:0,ly:0,rx:0,ry:0,seed:null};
// spawnLensSeed: 播种固定相位的 3 项微扰，构成稳定鹅卵石轮廓（呼吸时形状不变，仅整体缩放）。
function spawnLensSeed(){return [{a:0.07+0.05*Math.random(),f:2,p:Math.random()*Math.PI*2},{a:0.05+0.04*Math.random(),f:3,p:Math.random()*Math.PI*2},{a:0.03+0.03*Math.random(),f:5,p:Math.random()*Math.PI*2}]}
function updateGravLens(dt,halfW,halfH,cx,cy){if(!GRAV_LENS.active){GRAV_LENS.nextIn-=dt;if(GRAV_LENS.nextIn<=0){GRAV_LENS.active=true;GRAV_LENS.life=0;GRAV_LENS.phase=Math.random()*Math.PI*2;const a=Math.random()*Math.PI*2;const rr=0.28+0.38*Math.random();GRAV_LENS.lx=cx+halfW*rr*Math.cos(a);GRAV_LENS.ly=cy+halfH*rr*Math.sin(a);GRAV_LENS.rx=halfW*(0.18+0.12*Math.random());GRAV_LENS.ry=halfH*(0.18+0.12*Math.random());GRAV_LENS.strength=0.85+0.15*Math.random();GRAV_LENS.seed=spawnLensSeed();GRAV_LENS.nextIn=GRAV_LENS.period+(Math.random()*240-120)}}else{GRAV_LENS.life+=dt;GRAV_LENS.phase+=dt*0.85;if(GRAV_LENS.life>=GRAV_LENS.duration){GRAV_LENS.active=false;GRAV_LENS.life=0}}const g=document.getElementById('orbit-lens');const halo=document.getElementById('orbit-lens-halo');const rim=document.getElementById('orbit-lens-rim');if(g&&halo&&rim){if(GRAV_LENS.active){const p=GRAV_LENS.life/GRAV_LENS.duration;const env=Math.sin(Math.min(1,p)*Math.PI);const op=0.15+0.55*env*GRAV_LENS.strength;g.setAttribute('opacity',op.toFixed(3));const breathe=1+0.06*Math.sin(GRAV_LENS.phase*0.6);const seed=GRAV_LENS.seed||(GRAV_LENS.seed=spawnLensSeed());halo.setAttribute('d',orbitLensBlobPath(GRAV_LENS.lx,GRAV_LENS.ly,GRAV_LENS.rx*breathe,GRAV_LENS.ry*breathe,seed));rim.setAttribute('d',orbitLensBlobPath(GRAV_LENS.lx,GRAV_LENS.ly,GRAV_LENS.rx*breathe*0.92,GRAV_LENS.ry*breathe*0.92,seed))}else{g.setAttribute('opacity','0')}}if(!GRAV_LENS.active)return null;const p=GRAV_LENS.life/GRAV_LENS.duration;const env=Math.sin(Math.min(1,p)*Math.PI);return{lx:GRAV_LENS.lx,ly:GRAV_LENS.ly,rx:GRAV_LENS.rx,ry:GRAV_LENS.ry,strength:GRAV_LENS.strength*env,phase:GRAV_LENS.phase}}
function buildOrbitSvg(){const svg=document.getElementById('orbit-svg');if(!svg)return;const {cx,cy,halfW,halfH}=orbitStageGeom();let defs='<defs>';['s','a','b','c'].forEach(q=>{const c=getComputedStyle(document.documentElement).getPropertyValue('--q-'+q).trim()||'#3b8dff';const energy=getComputedStyle(document.documentElement).getPropertyValue('--sun-energy').trim()||c;defs+='<linearGradient id="orbitBeam-'+q+'" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="1" y2="0"><stop offset="0%" stop-color="'+energy+'" stop-opacity="0"/><stop offset="12%" stop-color="'+energy+'" stop-opacity="0.75"/><stop offset="55%" stop-color="'+c+'" stop-opacity="0.85"/><stop offset="88%" stop-color="'+c+'" stop-opacity="0.55"/><stop offset="100%" stop-color="'+c+'" stop-opacity="0"/></linearGradient><linearGradient id="orbitGlow-'+q+'" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="1" y2="0"><stop offset="0%" stop-color="'+energy+'" stop-opacity="0"/><stop offset="20%" stop-color="'+c+'" stop-opacity="0.35"/><stop offset="70%" stop-color="'+c+'" stop-opacity="0.22"/><stop offset="100%" stop-color="'+c+'" stop-opacity="0"/></linearGradient>'});defs+='<linearGradient id="orbitWindStream" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="1" y2="0"><stop offset="0%" stop-color="#9ccaff" stop-opacity="0"/><stop offset="12%" stop-color="#b8d8ff" stop-opacity="0.55"/><stop offset="55%" stop-color="#6aa8ff" stop-opacity="0.28"/><stop offset="100%" stop-color="#3b8dff" stop-opacity="0"/></linearGradient>';defs+='<radialGradient id="orbitWindPlume" cx="50%" cy="50%" r="50%"><stop offset="0%" stop-color="#9ccaff" stop-opacity="0.2"/><stop offset="45%" stop-color="#5a9dff" stop-opacity="0.08"/><stop offset="100%" stop-color="#3b8dff" stop-opacity="0"/></radialGradient>';defs+='<radialGradient id="orbitLensFill" cx="50%" cy="50%" r="50%"><stop offset="0%" stop-color="#c8dcff" stop-opacity="0.18"/><stop offset="55%" stop-color="#8eb6ff" stop-opacity="0.08"/><stop offset="100%" stop-color="#3b8dff" stop-opacity="0"/></radialGradient>';defs+='</defs>';let rings='';['s','a','b','c'].forEach(q=>{const tr=ORBIT_TRACKS[q];const rx=halfW*tr.rr,ry=halfH*tr.rr;const c=getComputedStyle(document.documentElement).getPropertyValue('--q-'+q).trim()||'#3b8dff';rings+='<ellipse class="orbit-ring" cx="'+cx+'" cy="'+cy+'" rx="'+rx.toFixed(1)+'" ry="'+ry.toFixed(1)+'" stroke="'+c+'" stroke-opacity="0.34"/>'});const wind='<g id="orbit-wind" opacity="0"><path class="orbit-wind-plume" id="orbit-wind-plume" d=""/><g id="orbit-wind-streams"></g></g>';const lens='<g id="orbit-lens" opacity="0"><path class="orbit-lens-halo" id="orbit-lens-halo" d=""/><path class="orbit-lens-rim" id="orbit-lens-rim" d=""/></g>';svg.innerHTML=defs+rings+wind+lens+'<g id="orbit-beams"></g>'}
function buildOrbitSats(){const layer=document.getElementById('orbit-sats');const beamG=document.getElementById('orbit-beams');if(!layer||!beamG)return;layer.innerHTML='';beamG.innerHTML='';orbitSats=[];// 会话连线按「地区+品质档」计数：仅连到与会话出口品质/延迟匹配的卫星轨道，
// 禁止「该地区任一 session 就把 S/A/B/C 全档卫星都点亮」。
const regionTracks={};const buckets={};allProxies.filter(p=>isAvailable(p)&&isKnownRegion(p)).forEach(p=>{const cc=regionOf(p);const q=orbitQualityTrack(p);if(!q||q==='d')return;const key=cc+'|'+q;if(!buckets[key])buckets[key]={cc:cc,q:q,n:0,k:0};buckets[key].n++;if(!regionTracks[cc])regionTracks[cc]={};regionTracks[cc][q]=(regionTracks[cc][q]||0)+1});const sessCount={};(Array.isArray(orbitSessions)?orbitSessions:[]).forEach(s=>{const key=orbitSessionBeamKey(s,regionTracks);if(!key)return;sessCount[key]=(sessCount[key]||0)+1});Object.keys(buckets).forEach(key=>{const b=buckets[key];b.k=sessCount[b.cc+'|'+b.q]||0});const byQ={s:[],a:[],b:[],c:[]};Object.values(buckets).forEach(b=>{if(byQ[b.q])byQ[b.q].push(b)});const svgns='http://www.w3.org/2000/svg';['s','a','b','c'].forEach(q=>{const arr=byQ[q];if(!arr.length)return;arr.sort((x,y)=>y.n-x.n);const tr=ORBIT_TRACKS[q];const step=360/arr.length;arr.forEach((d,i)=>{const el=document.createElement('div');el.className='orbit-sat'+(d.k>0?' live':'');el.style.setProperty('--qc',ORBIT_QVAR[q]);const SMIN=30,SMAX=60,NLO=1,NHI=40;const norm=Math.max(0,Math.min(1,(Math.sqrt(d.n)-Math.sqrt(NLO))/(Math.sqrt(NHI)-Math.sqrt(NLO))));const size=Math.round(SMIN+(SMAX-SMIN)*norm);el.dataset.size=String(size);el.style.width=size+'px';el.style.height=size+'px';const tip=html(d.cc).toUpperCase()+' · '+html(d.n)+' '+t('orbit_nodes')+' · '+q.toUpperCase()+' '+t('orbit_grade')+(d.k>0?(' · '+html(d.k)+' '+t('orbit_sessions')):'');el.innerHTML='<div class="ball"><span class="cc">'+html(d.cc).toUpperCase()+'</span></div><span class="cnt num">'+html(d.n)+'</span>';el.title=tip;layer.appendChild(el);let beam=null;if(d.k>0){const g=document.createElementNS(svgns,'g');const glow=document.createElementNS(svgns,'path');glow.setAttribute('class','orbit-beam-glow');glow.setAttribute('fill','url(#orbitGlow-'+q+')');const path=document.createElementNS(svgns,'path');path.setAttribute('class','orbit-beam');path.setAttribute('fill','url(#orbitBeam-'+q+')');g.appendChild(glow);g.appendChild(path);beamG.appendChild(g);beam={path:path,glow:glow,phase:Math.random()*Math.PI*2,speed:1.1+0.2*Math.min(5,d.k),baseW:Math.max(2.2,Math.min(5.5,2.0+0.85*Math.min(6,d.k)))}}orbitSats.push({el:el,beam:beam,track:tr,baseAngle:tr.phase+i*step,q:q})})});const ipEl=document.getElementById('orbit-gw-ip');if(ipEl){ipEl.textContent=publicIP||(location&&location.hostname)||'--'}orbitBuilt=true}
function orbitFrame(now){if(!orbitLast)orbitLast=now;const dt=(now-orbitLast)/1000;orbitLast=now;const live=(!orbitPaused&&!document.hidden);if(live)orbitT+=dt;const {cx,cy,halfW,halfH}=orbitStageGeom();const sunR=42;const wind=live?updateSolarWind(dt,halfW,halfH,cx,cy):null;const lens=live?updateGravLens(dt,halfW,halfH,cx,cy):null;orbitSats.forEach(s=>{const tr=s.track;const ang=(s.baseAngle+tr.dir*tr.w*orbitT)*Math.PI/180;const rx=halfW*tr.rr,ry=halfH*tr.rr;const x=cx+rx*Math.cos(ang);const y=cy+ry*Math.sin(ang);const depth=(Math.sin(ang)+1)/2;const scale=0.82+0.30*depth;s.el.style.transform='translate3d('+x+'px,'+y+'px,0) translate(-50%,-50%) scale('+scale+')';const zi=String(Math.round(depth*100)+10);if(s.el.dataset.z!==zi){s.el.dataset.z=zi;s.el.style.zIndex=zi}if(s.beam){const dx=x-cx,dy=y-cy;const len=Math.hypot(dx,dy)||1;const sx=cx+dx/len*sunR,sy=cy+dy/len*sunR;s.beam.phase+=dt*s.beam.speed;const baseOp=0.42+0.48*depth;const main=orbitRibbonPath(sx,sy,x,y,s.beam.baseW,s.beam.phase,1,wind,lens);const hit=main.windHit||0;const op=baseOp*(1-0.78*hit);s.beam.path.setAttribute('d',main.d);s.beam.path.style.opacity=Math.max(0.08,op).toFixed(2);if(s.beam.glow){const g=orbitRibbonPath(sx,sy,x,y,s.beam.baseW,s.beam.phase,2.1,wind,lens);s.beam.glow.setAttribute('d',g.d);s.beam.glow.style.opacity=Math.max(0.04,op*0.5*(1-0.5*hit)).toFixed(2)}orbitSetGrad('orbitBeam-'+s.q,sx,sy,x,y);orbitSetGrad('orbitGlow-'+s.q,sx,sy,x,y)}});orbitRAF=requestAnimationFrame(orbitFrame)}
function ensureOrbitLoop(){if(!orbitRAF){orbitLast=0;orbitRAF=requestAnimationFrame(orbitFrame)}}
function renderOrbitSystem(){const stage=document.getElementById('orbit-stage');if(!stage)return;buildOrbitSvg();buildOrbitSats();ensureOrbitLoop()}
function toggleOrbitPause(){orbitPaused=!orbitPaused;const btn=document.getElementById('orbit-pause-btn');if(btn)btn.textContent=t(orbitPaused?'orbit_resume':'orbit_pause')}
function renderWorldMap(){renderOrbitSystem()}

// 会话卡展开状态：跨 10s 自动刷新保留，避免「全部展开后几秒又折叠」。
let sessionOpenIDs={};let sessionExpandAll=null;
function expandAllSessions(open){if(open){sessionExpandAll=true}else{sessionExpandAll=null;sessionOpenIDs={}}document.querySelectorAll('#session-rows .session-card').forEach(function(el){el.classList.toggle('open',!!open);const sid=el.getAttribute('data-sid');if(sid){if(open)sessionOpenIDs[sid]=true;else delete sessionOpenIDs[sid]}})}
function toggleSessionCard(el){if(!el)return;const open=el.classList.toggle('open');const sid=el.getAttribute('data-sid')||'';// 手动点开/点关后退出「全部展开」粘性模式，改为按 sessionOpenIDs 精确恢复。
sessionExpandAll=null;if(sid){if(open)sessionOpenIDs[sid]=true;else delete sessionOpenIDs[sid]}const head=el.querySelector('.head');if(head)head.setAttribute('aria-expanded',open?'true':'false')}
function sessionIsOpen(sid){if(sessionExpandAll===true)return true;return !!sessionOpenIDs[sid]}
function sessionExitNode(s){const exitIP=String((s&&s.exit_ip)||'').trim();return exitIP||'--'}
function sessionSourceLabel(s){if(s&&s.subscription_name)return s.subscription_name;if(s&&s.source==='manual')return t('source_manual');if(s&&s.source==='subscription')return t('source_sub');return '--'}
function sessionProtoLabel(s){if(s&&(s.dual_protocol===true||Number(s.dual_protocol)===1))return 'SOCKS5+HTTP';const p=String((s&&s.protocol)||'').toLowerCase();if(p==='socks5')return 'SOCKS5';if(p==='http')return 'HTTP';return p?p.toUpperCase():'--'}
// DSL 片段展示：region-ca（不要前导多余 '-'；完整用户名里才是 base-region-ca-...）
function sessionRegionReq(s){const r=String((s&&s.selected_region)||'').trim().toLowerCase();if(!r||r==='unknown')return '—';return 'region-'+r}
function sessionCooldownLabel(sec){const n=Number(sec)||0;if(n>0)return '<span class="badge warn">'+html(n)+'s</span>';return '<span class="muted">'+html(t('session_no_cooldown'))+'</span>'}
function sessionOccPct(s){const active=Number(s&&s.active_sessions_on_proxy||0);const max=Number(s&&s.max_sessions_per_proxy||0);if(max>0)return Math.max(0,Math.min(100,Math.round(active*100/max)));return active>0?35:0}
function sessionOccLabel(s){const active=Number(s&&s.active_sessions_on_proxy||0);const max=Number(s&&s.max_sessions_per_proxy||0);return max>0?(active+' / '+max):(active+' / ∞')}
function sessionCardHTML(s){const sid=html(s.session_id);const sidRaw=String(s.session_id||'');const open=sessionIsOpen(sidRaw);const route=html(s.route_label||'');const exitNode=html(sessionExitNode(s));const bindAddr=html(s.bind_address||s.node||'--');const region=String(s.exit_region||'').trim().toLowerCase();const regionBadge=region&&region!=='unknown'?'<span class="badge blue">'+html(region).toUpperCase()+'</span>':'<span class="badge gray">'+html(t('session_unknown'))+'</span>';const q=sessionQualityBadge(s.quality_grade);const lat=Number(s.latency)>0?('<span class="num">'+html(s.latency)+' ms</span>'):'<span class="muted">--</span>';const proto=html(sessionProtoLabel(s));const src=html(sessionSourceLabel(s));const proxyID=Number(s.proxy_id)>0?('#'+html(s.proxy_id)):'--';const lastActive=html(s.last_active||'--');const ttlSec=Number(s.remaining_ttl_seconds)||0;const ttlCls=ttlSec>0&&ttlSec<60?' danger':(ttlSec>0&&ttlSec<180?' warn':'');const ttlText=html(formatTTL(ttlSec));const regionReq=html(sessionRegionReq(s));const exitLocation=html(s.exit_location||'--');const exitCheckedAt=html(s.exit_checked_at||'--');const cooldown=sessionCooldownLabel(s.cooldown_remaining_seconds);const occ=sessionOccPct(s);const occLab=html(sessionOccLabel(s));return '<div class="session-card'+(open?' open':'')+'" data-sid="'+sid+'"><div class="head" onclick="toggleSessionCard(this.parentElement)" role="button" aria-expanded="'+(open?'true':'false')+'"><span class="sid" title="'+sid+'">'+sid+'</span><span class="chips">'+regionBadge+' '+q+' <span class="badge blue">'+proto+'</span> <span class="muted mono" style="font-size:11px">'+exitNode+'</span></span><span class="ttl'+ttlCls+'">'+ttlText+'</span><span class="chev" aria-hidden="true">›</span></div><div class="body"><div class="detail-grid"><div class="di"><span class="k">'+html(t('session_id'))+'</span><span class="v mono">'+sid+'</span></div><div class="di"><span class="k">'+html(t('session_proxy_id'))+'</span><span class="v mono">'+proxyID+'</span></div><div class="di"><span class="k">'+html(t('session_exit_node'))+'</span><span class="v mono">'+exitNode+'</span></div><div class="di"><span class="k">'+html(t('session_exit_region'))+'</span><span class="v">'+regionBadge+'</span></div><div class="di"><span class="k">'+html(t('session_exit_location'))+'</span><span class="v">'+exitLocation+'</span></div><div class="di"><span class="k">'+html(t('session_exit_checked'))+'</span><span class="v mono">'+exitCheckedAt+'</span></div><div class="di"><span class="k">'+html(t('session_selected_region'))+'</span><span class="v mono">'+regionReq+'</span></div><div class="di"><span class="k">'+html(t('session_unlock'))+'</span><span class="v muted">'+html(t('session_no_unlock'))+'</span></div><div class="di"><span class="k">'+html(t('session_protocol'))+'</span><span class="v"><span class="badge blue">'+proto+'</span></span></div><div class="di"><span class="k">'+html(t('session_quality_latency'))+'</span><span class="v">'+q+' · '+lat+'</span></div><div class="di"><span class="k">'+html(t('session_source'))+'</span><span class="v">'+src+'</span></div><div class="di"><span class="k">'+html(t('session_last_active'))+'</span><span class="v num">'+lastActive+'</span></div><div class="di"><span class="k">'+html(t('session_ttl'))+'</span><span class="v ttl'+ttlCls+'" style="color:inherit">'+ttlText+' <span class="muted">('+html(ttlSec)+'s)</span></span></div><div class="di"><span class="k">'+html(t('session_cooldown'))+'</span><span class="v">'+cooldown+'</span></div><div class="di"><span class="k">'+html(t('session_bind'))+'</span><span class="v mono muted">'+bindAddr+'</span></div></div><div class="route-box"><b>'+html(t('session_route'))+'</b>'+(route||html(t('session_no_unlock')))+'</div><div class="occ"><span class="muted" style="font-size:11px;font-weight:700;letter-spacing:.04em;text-transform:uppercase">'+html(t('session_occupancy'))+'</span><div class="bar"><i style="width:'+occ+'%"></i></div><span class="num" style="font-weight:800">'+occLab+'</span><span class="muted" style="font-size:11px">'+html(t('session_occupancy_hint'))+'</span></div></div></div>'}
function renderSessions(sessions){const list=Array.isArray(sessions)?sessions:[];orbitSessions=list;renderOrbitSystem();const body=document.getElementById('session-rows');const cnt=document.getElementById('sess-count');const countText=Array.isArray(sessions)?tf('session_count',{count:list.length}):'--';if(cnt)cnt.textContent=countText;const ov=document.getElementById('ov-session-rows');const ovc=document.getElementById('ov-sess-count');if(ovc)ovc.textContent=countText;if(!list.length){if(body)body.innerHTML='<div class="empty">'+html(t('session_none'))+'</div>';if(ov)ov.innerHTML='<tr><td colspan="7" class="empty">'+html(t('session_none'))+'</td></tr>';return}if(ov)ov.innerHTML=list.slice(0,8).map(function(s){const sid=html(s.session_id);const route=html(s.route_label||'');const exitNode=html(sessionExitNode(s));const region=String(s.exit_region||'').trim().toLowerCase();const regionBadge=region&&region!=='unknown'?'<span class="badge ok">'+html(region).toUpperCase()+'</span>':'<span class="badge gray">'+html(t('session_unknown'))+'</span>';const q=sessionQualityBadge(s.quality_grade);const lat=Number(s.latency)>0?html(s.latency)+' ms':'--';const ttlSec=Number(s.remaining_ttl_seconds)||0;return '<tr><td class="mono">'+sid+'</td><td class="mono muted">'+route+'</td><td>'+regionBadge+'</td><td class="mono">'+exitNode+'</td><td>'+q+'</td><td>'+lat+'</td><td>'+html(formatTTL(ttlSec))+'</td></tr>'}).join('');if(body)body.innerHTML=list.map(sessionCardHTML).join('')}
async function loadSessions(){const sessions=await api('/api/sessions');if(!sessions)return;sessionCache=Array.isArray(sessions)?sessions:[];sessionsLoaded=true;renderSessions(sessionCache)}
function formatTTL(seconds){const value=Number(seconds)||0;const min=Math.floor(value/60);const sec=value%60;return min>0?min+'m '+sec+'s':sec+'s'}
// sessionQualityBadge: 会话出口节点品质档 S/A/B/C 徽章;无品质数据显示 --。
function sessionQualityBadge(g){const v=String(g||'').trim().toUpperCase();const cls={S:'qs',A:'qa',B:'qb',C:'qc'}[v];if(!cls)return '<span class="muted">--</span>';return '<span class="badge '+cls+'">'+v+'</span>'}
async function addManualNode(){return runAsync(t('err_add'),async()=>{const payload={link:document.getElementById('manual-link').value.trim(),region:document.getElementById('manual-region').value.trim(),note:document.getElementById('manual-note').value.trim()};if(!payload.link){showToast(t('toast_need_link'));return}await api('/api/manual-node/add',{method:'POST',body:JSON.stringify(payload)});document.getElementById('manual-link').value='';document.getElementById('manual-region').value='';document.getElementById('manual-note').value='';await Promise.all([loadStats(),loadProxies()]);showToast(t('toast_manual_added'))})}
function toggleSelectAll(on){document.querySelectorAll('.proxy-select').forEach(el=>{el.checked=!!on})}
function selectedProxyIDs(){return Array.from(document.querySelectorAll('.proxy-select:checked')).map(el=>Number(el.value)).filter(n=>Number.isFinite(n)&&n>0)}
async function batchDeleteSelected(){const ids=selectedProxyIDs();if(!ids.length){showToast(t('toast_select_nodes'));return}const ok=await showConfirm(tf('confirm_delete_batch',{n:ids.length}),t('btn_delete'));if(!ok)return;return runAsync(t('err_batch_delete'),async()=>{const r=await api('/api/manual-node/batch-delete',{method:'POST',body:JSON.stringify({ids})});await Promise.all([loadStats(),loadProxies()]);const deleted=r&&r.deleted!=null?r.deleted:ids.length;showToast(tf('toast_batch_deleted',{n:deleted})+(r&&r.failed?tf('toast_batch_failed',{n:r.failed}):''))})}
async function importManualNodes(){return runAsync(t('err_import'),async()=>{const text=document.getElementById('import-text').value;const region=document.getElementById('import-region').value.trim();const note=document.getElementById('import-note').value.trim();if(!String(text||'').trim()){showToast(t('toast_import_need'));return}const r=await api('/api/manual-node/import',{method:'POST',body:JSON.stringify({text,region,note})});document.getElementById('import-modal').classList.remove('show');document.getElementById('import-text').value='';await Promise.all([loadStats(),loadProxies()]);showToast(tf('toast_import_done',{a:r.added||0,s:r.skipped||0,f:r.failed||0}))})}
// manageNode: 来源无关的统一节点管理弹窗（订阅节点与手工节点都可用）。
// 以 proxy-id 为主身份；地域/备注保存与删除均走应用内弹窗，不再用浏览器 prompt/confirm。
function manageNode(id){const p=allProxies.find(x=>Number(x.id)===Number(id))||{};document.getElementById('node-modal-id').value=String(id);const addrEl=document.getElementById('node-modal-addr');if(addrEl)addrEl.textContent=maskAddress(p.address||'');const rEl=document.getElementById('node-modal-region');if(rEl)rEl.value=String(p.region||'');const nEl=document.getElementById('node-modal-note');if(nEl)nEl.value=String(p.note||'');document.getElementById('node-modal').classList.add('show')}
function closeNodeModal(){document.getElementById('node-modal').classList.remove('show')}
async function nodeModalSave(){return runAsync(t('err_save'),async()=>{const id=Number(document.getElementById('node-modal-id').value);if(!Number.isFinite(id)||id<=0){showToast(t('toast_invalid_node'));return}const region=document.getElementById('node-modal-region').value.trim();const note=document.getElementById('node-modal-note').value;await api('/api/manual-node/region',{method:'POST',body:JSON.stringify({id,region})});await api('/api/manual-node/note',{method:'POST',body:JSON.stringify({id,note})});closeNodeModal();await loadProxies();showToast(t('toast_node_updated'))})}
async function nodeModalDelete(){const id=Number(document.getElementById('node-modal-id').value);if(!Number.isFinite(id)||id<=0){showToast(t('toast_invalid_node'));return}const ok=await showConfirm(t('confirm_delete_node'),t('btn_delete'));if(!ok)return;return runAsync(t('err_delete'),async()=>{await api('/api/proxy/delete',{method:'POST',body:JSON.stringify({id})});closeNodeModal();await Promise.all([loadStats(),loadProxies()]);showToast(t('toast_node_deleted'))})}
// editNote: 来源无关的备注快捷编辑入口（点击名称/备注单元格）。复用统一管理弹窗。
async function editNote(id){manageNode(id)}
async function toggleProxy(id,address,enable){return runAsync(t('err_op'),async()=>{await api('/api/proxy/toggle',{method:'POST',body:JSON.stringify({id,address,enable})});await Promise.all([loadStats(),loadProxies()]);showToast(enable?t('toast_node_enabled'):t('toast_node_disabled'))})}
// testProxy: 触发单节点重新验证（走完整 ValidateOne，含连通 google/openai/github/cloudflare/gstatic），后端异步执行，稍后自动刷新列表。
async function testProxy(id,address){return runAsync(t('err_test'),async()=>{await api('/api/proxy/refresh',{method:'POST',body:JSON.stringify({id,address})});showToast(t('toast_test_started'));setTimeout(()=>runAsync(t('err_refresh'),()=>Promise.all([loadStats(),loadProxies()])),4000)})}
let allSubs=[];let subscriptionsLoaded=false;function renderSubscriptions(){const box=document.getElementById('sub-list');if(!box)return;if(!allSubs.length){box.innerHTML='<div class="empty">'+html(t('no_subs'))+'</div>';return}box.innerHTML=allSubs.map(sub=>{const paused=sub.status==='paused';const activeCount=Number(sub.active_count||0);const disabledCount=Number(sub.disabled_count||0);const proxyCount=Number(sub.proxy_count||0);const pausedCount=Number(sub.paused_count??Math.max(0,proxyCount-activeCount-disabledCount));const toggleLabel=paused?t('btn_enable'):t('btn_disable');const badge=paused?'<span class="badge warn">'+html(t('sub_paused_badge'))+'</span>':'<span class="badge ok">'+html(t('sub_active'))+'</span>';const id=Number(sub.id);const idArg=Number.isFinite(id)?String(id):'0';const url=String(sub.url||'');const urlLine=url?('<div class="muted mono" style="margin-top:4px;font-size:11px;word-break:break-all">'+html(url)+'</div>'):'';return '<div class="sub-item"><div class="meta"><strong>'+html(sub.name)+' '+badge+'</strong><div class="muted">'+html(tf('sub_counts',{a:activeCount,p:pausedCount,d:disabledCount}))+'</div>'+urlLine+'</div><div class="mini-actions"><button class="mini" onclick="openSubModal('+idArg+')">'+html(t('sub_edit'))+'</button><button class="mini" onclick="refreshSub('+idArg+')">'+html(t('btn_refresh'))+'</button><button class="mini" onclick="toggleSub('+idArg+')">'+html(toggleLabel)+'</button><button class="mini danger" onclick="deleteSub('+idArg+')">'+html(t('sub_delete'))+'</button></div></div>'}).join('')}
async function loadSubscriptions(){const subs=await api('/api/subscriptions');if(!subs)return;allSubs=Array.isArray(subs)?subs:[];subscriptionsLoaded=true;renderSubscriptions()}
function openSubModal(id){const editing=id!=null&&id!=='';const sub=editing?allSubs.find(s=>Number(s.id)===Number(id)):null;document.getElementById('sub-edit-id').value=sub?String(sub.id):'';document.getElementById('sub-modal-title').textContent=sub?t('modal_sub_edit'):t('modal_sub_add');document.getElementById('sub-modal-submit').textContent=sub?t('btn_save'):t('btn_add');document.getElementById('sub-name').value=sub?(sub.name||''):'';document.getElementById('sub-refresh').value=sub?(sub.refresh_min||60):60;document.getElementById('sub-url').value=sub?(sub.url||''):'';document.getElementById('sub-headers').value=sub?(sub.headers||''):'';const fileField=document.getElementById('sub-file-field');const fc=document.getElementById('sub-file-content');if(fc)fc.value='';if(fileField)fileField.style.display=sub?'none':'';document.getElementById('sub-modal').classList.add('show')}
function closeSubModal(){document.getElementById('sub-modal').classList.remove('show')}
function submitSubscription(){const editId=document.getElementById('sub-edit-id').value.trim();if(editId){return updateSubscription(editId)}return addSubscription()}
async function addSubscription(){return runAsync(t('err_add'),async()=>{const payload={name:document.getElementById('sub-name').value.trim(),url:document.getElementById('sub-url').value.trim(),file_content:document.getElementById('sub-file-content').value.trim(),headers:document.getElementById('sub-headers').value.trim(),refresh_min:Number(document.getElementById('sub-refresh').value)||60};if(!payload.url&&!payload.file_content){showToast(t('toast_sub_need'));return}await api('/api/subscription/add',{method:'POST',body:JSON.stringify(payload)});document.getElementById('sub-name').value='';document.getElementById('sub-url').value='';document.getElementById('sub-file-content').value='';document.getElementById('sub-headers').value='';closeSubModal();await Promise.all([loadSubscriptions(),loadStats(),loadProxies()]);showToast(t('toast_sub_added'))})}
async function updateSubscription(id){return runAsync(t('err_update'),async()=>{const payload={id:Number(id),name:document.getElementById('sub-name').value.trim(),url:document.getElementById('sub-url').value.trim(),headers:document.getElementById('sub-headers').value.trim(),refresh_min:Number(document.getElementById('sub-refresh').value)||60};if(!Number.isFinite(payload.id)||payload.id<=0){showToast(t('toast_invalid_sub'));return}await api('/api/subscription/update',{method:'POST',body:JSON.stringify(payload)});closeSubModal();await Promise.all([loadSubscriptions(),loadStats(),loadProxies()]);showToast(t('toast_sub_updated'))})}
async function refreshSub(id){return runAsync(t('err_refresh'),async()=>{await api('/api/subscription/refresh',{method:'POST',body:JSON.stringify({id})});showToast(t('toast_refresh_started'));refreshLater()})}
async function refreshAllSubs(){return runAsync(t('err_refresh'),async()=>{await api('/api/subscription/refresh-all',{method:'POST'});showToast(t('toast_refresh_all_started'));refreshLater()})}
async function toggleSub(id){return runAsync(t('err_toggle'),async()=>{await api('/api/subscription/toggle',{method:'POST',body:JSON.stringify({id})});await Promise.all([loadSubscriptions(),loadStats(),loadProxies()]);showToast(t('toast_sub_toggled'))})}
async function deleteSub(id){return runAsync(t('err_delete'),async()=>{if(!(await showConfirm(t('confirm_delete_sub'))))return;await api('/api/subscription/delete',{method:'POST',body:JSON.stringify({id})});await Promise.all([loadSubscriptions(),loadStats(),loadProxies()]);showToast(t('toast_sub_deleted'))})}
function logWindowShift(oldLines,newLines){const old=Array.isArray(oldLines)?oldLines:[];const next=Array.isArray(newLines)?newLines:[];let best=null,bestOverlap=0;for(let shift=0;shift<old.length;shift++){const overlap=Math.min(old.length-shift,next.length);if(overlap<=bestOverlap)continue;let same=true;for(let i=0;i<overlap;i++){if(old[shift+i]!==next[i]){same=false;break}}if(same){best=shift;bestOverlap=overlap}}return best}
// 自动贴底：隐藏页 display:none 时 scrollHeight 不可用，须在可见后再贴；双 rAF 等 flex 布局算完。
function isLogsTabVisible(){const page=document.getElementById('page-logs');return !!(page&&page.classList.contains('active'))}
function stickLogsToBottom(box){if(!box)return;const apply=function(){try{box.scrollTop=box.scrollHeight}catch(e){}};apply();requestAnimationFrame(function(){apply();requestAnimationFrame(apply)})}
async function loadLogs(){const data=await api('/api/logs');if(!data)return;const box=document.getElementById('logs-box');if(!box)return;const auto=document.getElementById('logs-autoscroll');const follow=!!(auto&&auto.checked);const prevTop=box.scrollTop;const oldElements=follow?[]:Array.from(box.children);const oldLines=oldElements.map(el=>el.textContent||'');let anchorIndex=-1,anchorOffset=0;if(!follow&&isLogsTabVisible()){const boxTop=box.getBoundingClientRect().top;anchorIndex=oldElements.findIndex(el=>el.getBoundingClientRect().bottom>boxTop);if(anchorIndex>=0)anchorOffset=oldElements[anchorIndex].getBoundingClientRect().top-boxTop}const lines=Array.isArray(data.lines)?data.lines:[];box.innerHTML=lines.length?lines.map(line=>'<div class="log-line">'+html(line)+'</div>').join(''):'<div class="log-line">'+html(t('logs_empty'))+'</div>';if(follow){if(isLogsTabVisible())stickLogsToBottom(box);return}const shift=logWindowShift(oldLines,lines);const mappedIndex=shift===null?-1:anchorIndex-shift;const fresh=Array.from(box.children);if(mappedIndex>=0&&mappedIndex<fresh.length&&isLogsTabVisible()){const boxTop=box.getBoundingClientRect().top;const currentOffset=fresh[mappedIndex].getBoundingClientRect().top-boxTop;box.scrollTop=Math.max(0,box.scrollTop+currentOffset-anchorOffset);return}if(isLogsTabVisible())box.scrollTop=prevTop}
async function loadConfig(){configCache=await api('/api/config');if(!configCache)return;const hp=stripColon(configCache.http_port),sp=stripColon(configCache.socks5_port),wp=stripColon(configCache.webui_port);document.getElementById('cfg-http-port').value=hp;document.getElementById('cfg-socks5-port').value=sp;document.getElementById('cfg-webui-port').value=wp;document.getElementById('cfg-auth-enabled').value=String(Boolean(configCache.proxy_auth_enabled));document.getElementById('cfg-auth-username').value=configCache.proxy_auth_username||'';document.getElementById('cfg-auth-password').value='';document.getElementById('cfg-session-ttl').value=configCache.session_ttl_minutes||'';document.getElementById('cfg-default-region').value=configCache.default_region||'';document.getElementById('cfg-health-interval').value=configCache.health_check_interval||'';document.getElementById('cfg-max-retry').value=configCache.max_retry??'';document.getElementById('cfg-singbox-path').value=configCache.singbox_path||'';document.getElementById('cfg-allowed-countries').value=(configCache.allowed_countries||[]).join(',');document.getElementById('cfg-blocked-countries').value=(configCache.blocked_countries||[]).join(',');renderConnection();renderDSLExamples()}
async function loadPublicIP(){return runAsync(t('err_public_ip'),async()=>{const d=await api('/api/public-ip');if(d){if(d.public_ip){publicIP=d.public_ip;renderConnection()}if(d.country){gatewayCC=String(d.country).toLowerCase()}renderOrbitSystem()}})}
function renderConnection(){if(!configCache)return;const sp=stripColon(configCache.socks5_port)||'7801';const hp=stripColon(configCache.http_port)||'7802';const base=configCache.proxy_auth_username||'username';const enabled=configCache.proxy_auth_enabled;const host=publicIP||location.hostname||'127.0.0.1';const setText=function(id,v){const el=document.getElementById(id);if(el)el.textContent=v};setText('conn-socks5',host+':'+(sp||'7801'));setText('conn-http',host+':'+(hp||'7802'));setText('conn-user',base);setText('conn-pass',enabled?t('conn_password_hint'):t('conn_auth_disabled'));setText('conn-auth-state',enabled?t('conn_auth_on'):t('conn_auth_off'));const cred=enabled?(base+':PASSWORD@'):'';setText('conn-cmd','curl --socks5 '+cred+host+':'+(sp||'7801')+' https://www.gstatic.com/generate_204');renderDSLExamples()}
function renderDSLExamples(){const base=(configCache&&configCache.proxy_auth_username)?configCache.proxy_auth_username:'username';const hint=document.getElementById('dsl-hint');if(!hint)return;const syntax='<base>[-region-<cc>][-unlock-<token>][-node-<host:port|key-<base64url(nodeKey)>>][-session-<id>]';const nodeHint=t('dsl_node_hint');hint.textContent=(configCache&&configCache.proxy_auth_enabled!==false)?tf('dsl_enabled',{syntax:syntax,base:base,nodeHint:nodeHint}):t('dsl_disabled')}
async function openSettings(){switchTab('settings')}function closeSettings(){}function countries(id){return document.getElementById(id).value.split(',').map(v=>v.trim().toUpperCase()).filter(Boolean)}
// API Key 时间展示：Go 零值 time（0001-01-01…）表示「从未使用」，禁止显示成 1/1/1。
function formatAPIKeyTime(v){if(v==null||v===''||v===false)return '--';const s=String(v).trim();if(!s)return '--';if(s.indexOf('0001-')===0||s.indexOf('0001/')===0||s==='0')return '--';const d=new Date(s);if(Number.isNaN(d.getTime()))return '--';if(d.getUTCFullYear()<1970)return '--';return d.toLocaleString()}
function renderAPIKeys(keys){const body=document.getElementById('apikey-rows');if(!body)return;const list=Array.isArray(keys)?keys:[];if(!list.length){body.innerHTML='<tr><td colspan="5" class="empty">'+html(t('no_apikeys'))+'</td></tr>';return}body.innerHTML=list.map(k=>{const id=html(k.id);const name=html(k.name);const created=html(formatAPIKeyTime(k.created_at));const last=html(formatAPIKeyTime(k.last_used_at));const disabled=!!(k.disabled===true||Number(k.disabled)===1);const st=disabled?'<span class="badge warn">'+html(t('apikey_revoked'))+'</span>':'<span class="badge ok">'+html(t('apikey_active'))+'</span>';const revokeBtn=disabled?'':'<button class="mini" onclick="revokeAPIKey(\''+id+'\')">'+html(t('apikey_revoke'))+'</button> ';return '<tr><td>'+name+'</td><td>'+created+'</td><td>'+last+'</td><td>'+st+'</td><td>'+revokeBtn+'<button class="mini danger" onclick="deleteAPIKey(\''+id+'\')">'+html(t('apikey_delete'))+'</button></td></tr>'}).join('')}
async function loadAPIKeys(){const data=await api('/api/apikeys');if(!data)return;apiKeyCache=data.keys||data||[];apiKeysLoaded=true;renderAPIKeys(apiKeyCache)}
async function createAPIKey(){return runAsync(t('err_create_key'),async()=>{const name=document.getElementById('apikey-name').value.trim();if(!name){showToast(t('toast_need_key_name'));return}const r=await api('/api/apikey/create',{method:'POST',body:JSON.stringify({name})});document.getElementById('apikey-name').value='';document.getElementById('apikey-once-name').value=r&&r.name?r.name:name;document.getElementById('apikey-once-key').value=r&&r.key?r.key:'';document.getElementById('apikey-once-modal').classList.add('show');await loadAPIKeys();showToast(t('toast_apikey_created'))})}
async function revokeAPIKey(id){return runAsync(t('err_revoke'),async()=>{if(!(await showConfirm(t('confirm_revoke_key'))))return;await api('/api/apikey/revoke',{method:'POST',body:JSON.stringify({id})});await loadAPIKeys();showToast(t('toast_revoked'))})}
async function deleteAPIKey(id){return runAsync(t('err_delete'),async()=>{if(!(await showConfirm(t('confirm_delete_key'))))return;await api('/api/apikey/delete',{method:'POST',body:JSON.stringify({id})});await loadAPIKeys();showToast(t('toast_deleted'))})}
async function saveConfig(){return runAsync(t('err_save'),async()=>{if(!configCache)await loadConfig();if(!configCache)throw new Error(t('err_config_missing'));const payload={proxy_auth_enabled:document.getElementById('cfg-auth-enabled').value==='true',proxy_auth_username:document.getElementById('cfg-auth-username').value.trim(),proxy_auth_password:document.getElementById('cfg-auth-password').value,session_ttl_minutes:Number(document.getElementById('cfg-session-ttl').value),default_region:document.getElementById('cfg-default-region').value.trim().toLowerCase(),health_check_interval:Number(document.getElementById('cfg-health-interval').value),max_retry:Number(document.getElementById('cfg-max-retry').value),singbox_path:document.getElementById('cfg-singbox-path').value.trim(),allowed_countries:countries('cfg-allowed-countries'),blocked_countries:countries('cfg-blocked-countries')};await api('/api/config/save',{method:'POST',body:JSON.stringify(payload)});await loadConfig();showToast(t('toast_config_saved'))})}
// ===== 侧边栏折叠持久化 =====
// scheduleOrbitReflow: 侧栏折叠/展开会改变 stage 宽度；在 CSS transition 结束与下一帧各重建一次，
// 避免网关/轨道/S 标记滞后数秒才归位。
function scheduleOrbitReflow(){try{renderOrbitSystem()}catch(e){}requestAnimationFrame(function(){try{renderOrbitSystem()}catch(e){}});setTimeout(function(){try{renderOrbitSystem()}catch(e){}},320)}
function applySidebar(collapsed){document.body.classList.toggle('sidebar-collapsed',!!collapsed);try{localStorage.setItem('gg-sidebar',collapsed?'1':'0')}catch(e){}scheduleOrbitReflow()}
function toggleSidebar(){applySidebar(!document.body.classList.contains('sidebar-collapsed'))}
function openDrawer(){document.body.classList.add('drawer-open')}
function closeDrawer(){document.body.classList.remove('drawer-open')}
(function(){let c=false;try{c=localStorage.getItem('gg-sidebar')==='1'}catch(e){}applySidebar(c);const sb=document.getElementById('sidebar');if(sb)requestAnimationFrame(function(){sb.classList.remove('preload')})})();
// AI/Cloudflare 图标筛选：点击循环 全部->畅通->阻断->未知；值写入隐藏 select，renderProxies 读取不变。
const FILTER_STATE={'':'all','unlocked':'ok','blocked':'bad','unprobed':'unk','unknown':'unk'};
function filterCycleLabel(v){if(v==='unlocked')return t('filter_ok');if(v==='blocked')return t('filter_bad');if(v==='unprobed')return t('filter_unprobed');if(v==='unknown')return t('filter_unknown');return t('filter_all')}
function cycleFilter(selId,btnId){const sel=document.getElementById(selId);if(!sel)return;const opts=Array.from(sel.options).map(o=>o.value);let idx=opts.indexOf(sel.value);idx=(idx+1)%opts.length;sel.value=opts[idx];syncFilterToggle(selId,btnId);renderProxies()}
function syncFilterToggle(selId,btnId){const sel=document.getElementById(selId);const btn=document.getElementById(btnId);if(!sel||!btn)return;const v=sel.value;const st=btn.querySelector('.st');if(st)st.textContent=filterCycleLabel(v);btn.dataset.state=FILTER_STATE[v]||'all';btn.setAttribute('aria-pressed',v?'true':'false')}
function initFilterToggles(){document.querySelectorAll('.filter-toggle[data-sel]').forEach(function(btn){syncFilterToggle(btn.dataset.sel,btn.id)})}
// 节点表分页：默认每页 20，可选 20/50/100；筛选变化回到第 1 页。
let proxyRenderRows=[];let proxyPage=1;let proxyPageSize=20;
function proxyTotalPages(){const n=proxyRenderRows.length;if(n<=0)return 1;return Math.max(1,Math.ceil(n/proxyPageSize))}
function proxyPageSlice(){const start=(proxyPage-1)*proxyPageSize;return proxyRenderRows.slice(start,start+proxyPageSize)}
function renderProxyPage(){const body=document.getElementById('proxy-rows');if(!body)return;const slice=proxyPageSlice();if(!slice.length){body.innerHTML='<tr><td colspan="14" class="empty">'+html(t('no_match'))+'</td></tr>';renderProxyPager();return}body.innerHTML=slice.map(proxyRowHTML).join('');renderProxyPager();const selAll=document.getElementById('proxy-select-all');if(selAll)selAll.checked=false}
function renderProxyPager(){const total=proxyRenderRows.length;const pages=proxyTotalPages();const info=document.getElementById('proxy-page-info');const num=document.getElementById('proxy-page-num');const prev=document.getElementById('proxy-page-prev');const next=document.getElementById('proxy-page-next');const sizeSel=document.getElementById('proxy-page-size');if(sizeSel&&String(proxyPageSize)!==sizeSel.value){sizeSel.value=String(proxyPageSize)}if(info){if(total<=0){info.textContent=t('page_info_zero')}else{const from=(proxyPage-1)*proxyPageSize+1;const to=Math.min(proxyPage*proxyPageSize,total);info.textContent=tf('page_info',{total:total,from:from,to:to})}}if(num)num.textContent=proxyPage+' / '+pages;if(prev)prev.disabled=proxyPage<=1||total<=0;if(next)next.disabled=proxyPage>=pages||total<=0}
function proxyPagePrev(){if(proxyPage<=1)return;proxyPage--;renderProxyPage()}
function proxyPageNext(){if(proxyPage>=proxyTotalPages())return;proxyPage++;renderProxyPage()}
function proxyPageSizeChange(){const sel=document.getElementById('proxy-page-size');const n=sel?Number(sel.value):20;proxyPageSize=(n===50||n===100)?n:20;proxyPage=1;renderProxyPage()}
function proxyRowHTML(p){const addr=addressArg(p.address);const id=proxyIDArg(p);const st=nodeState(p);const noteText=nodeLabel(p);const label=noteText?html(noteText):'';const showRegion=isKnownRegion(p);let toggleBtn;if(st==='sub_paused'){toggleBtn='<button class="mini" disabled title="'+html(t('status_sub_paused'))+'">'+html(t('status_sub_paused'))+'</button>'}else if(st==='paused'){toggleBtn='<button class="mini" onclick="toggleProxy('+id+',decodeURIComponent(\''+addr+'\'),true)">'+html(t('btn_enable'))+'</button>'}else{toggleBtn='<button class="mini" onclick="toggleProxy('+id+',decodeURIComponent(\''+addr+'\'),false)">'+html(t('btn_disable'))+'</button>'}const testBtn='<button class="mini" onclick="testProxy('+id+',decodeURIComponent(\''+addr+'\'))">'+html(t('btn_test'))+'</button>';let copyBtn;if(isGatewayNode(p)&&st!=='ok'){const why=gatewayCopyBlockedReason(p)||t('copy_blocked_failed');copyBtn='<button class="mini" disabled title="'+html(why)+'">'+html(t('btn_copy'))+'</button>'}else{copyBtn='<button class="mini" onclick="copyProxyCred('+id+')">'+html(t('btn_copy'))+'</button>'}const baseActions=testBtn+' '+copyBtn+' '+toggleBtn;const manageBtn='<button class="mini" onclick="manageNode('+id+')">'+html(t('btn_manage'))+'</button>';const actions=baseActions+' '+manageBtn;const latencyText=Number(p.latency)>0?html(p.latency)+' ms':'--';const sel='<input type="checkbox" class="proxy-select" value="'+id+'">';const nameTitle=noteText?(html(noteText)+' · '+t('note_edit')):t('note_add');const nameCell='<td class="note-edit" title="'+nameTitle+'" style="cursor:pointer" onclick="editNote('+id+')">'+label+'</td>';const regionCell=showRegion?'<span class="badge ok">'+html(regionOf(p)).toUpperCase()+'</span>':'<span class="muted">--</span>';const exitCell=String((p&&p.exit_ip)||'').trim()?html(p.exit_ip):'<span class="muted">--</span>';return '<tr><td>'+sel+'</td><td>'+starBtn(p)+'</td>'+nameCell+'<td>'+protocolBadges(p)+'</td><td>'+regionCell+'</td><td class="mono">'+exitCell+'</td><td>'+latencyText+'</td><td>'+abuserBadge(p.ipapiis_score)+'</td><td>'+ipapiFlagsBadges(p.ipapi_flags,!!p.ipapi_flags_seen)+'</td><td>'+cfBadge(p.cf_blocked)+'</td><td>'+aiBadges(p.ai_reachability)+'</td><td>'+html(sourceLabel(p))+'</td><td>'+stateBadge(st)+'</td><td><span class="ops">'+actions+'</span></td></tr>'}
let orbitResizeTimer=null;window.addEventListener('resize',function(){clearTimeout(orbitResizeTimer);orbitResizeTimer=setTimeout(function(){try{renderOrbitSystem()}catch(e){}},180)});
// 骨架墓碑：载入态灰条 shimmer（尊重 prefers-reduced-motion，动画由 CSS 关闭）。
function skeletonRows(n){let h='';for(let i=0;i<(n||3);i++)h+='<div class="skeleton sk-row"></div>';return '<div class="skeleton-wrap">'+h+'</div>'}
function showSkeletons(){['region-list','sub-list','session-rows','singbox-status'].forEach(function(id){const el=document.getElementById(id);if(el)el.innerHTML=skeletonRows(3)})}
// 首次进入总览再画分布图。
// 每次回到总览都即时重建轨道（椭圆底图/卫星/网关），避免返回 dashboard 后底图数秒才出现。
function markViewLazy(name){if(name==='overview'){try{renderOrbitSystem()}catch(e){}requestAnimationFrame(function(){try{renderOrbitSystem()}catch(e){}})}}
(function(){let lang='zh';try{lang=localStorage.getItem('gg-lang')||'zh'}catch(e){}applyLang(lang)})();
initFilterToggles();showSkeletons();markViewLazy('overview');
refreshAll();
setInterval(()=>runAsync(t('err_op'),()=>Promise.all([loadStats(),loadProxies(),loadSubscriptions(),loadSessions()])),10000);
(function(){const auto=document.getElementById('logs-autoscroll');if(auto){auto.addEventListener('change',function(){if(this.checked)stickLogsToBottom(document.getElementById('logs-box'))})}})();
setInterval(()=>runAsync(t('err_log_refresh'),loadLogs),5000);`
