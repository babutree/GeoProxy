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
.logs{background:#121416;border:1px solid var(--line);border-radius:12px;padding:12px 14px;max-height:520px;overflow:auto;
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
.conn{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:12px}
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

const dashboardJS = `let allProxies=[];let allRegions=[];let configCache=null;let publicIP='';let orbitSessions=[];let gatewayCC='';
const PAGE_TITLES={overview:'总览',nodes:'节点',regions:'地域',subs:'订阅',sessions:'会话',logs:'日志',api:'API',settings:'设置'};
function switchTab(name){document.querySelectorAll('.navitem').forEach(t=>t.classList.toggle('active',t.dataset.tab===name));document.querySelectorAll('.page').forEach(p=>p.classList.toggle('active',p.id==='page-'+name));const title=document.getElementById('pageTitle');if(title)title.textContent=PAGE_TITLES[name]||name;if(name==='settings'){runAsync('打开设置失败',async()=>{if(!configCache)await loadConfig();await loadAPIKeys()})}try{markViewLazy(name)}catch(e){}closeDrawer()}
function showToast(msg){const el=document.getElementById('toast');el.textContent=msg;el.classList.add('show');setTimeout(()=>el.classList.remove('show'),2600)}
// showConfirm: 应用内确认弹窗，替换浏览器原生 confirm()。返回 Promise<boolean>。
function showConfirm(msg,okLabel){return new Promise(function(resolve){const modal=document.getElementById('confirm-modal');const msgEl=document.getElementById('confirm-modal-msg');const okBtn=document.getElementById('confirm-modal-ok');const cancelBtn=document.getElementById('confirm-modal-cancel');if(!modal||!msgEl||!okBtn||!cancelBtn){resolve(false);return}msgEl.textContent=String(msg||'确定执行此操作？');okBtn.textContent=String(okLabel||'确定');function cleanup(val){modal.classList.remove('show');okBtn.onclick=null;cancelBtn.onclick=null;resolve(val)}okBtn.onclick=function(){cleanup(true)};cancelBtn.onclick=function(){cleanup(false)};modal.classList.add('show')})}
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
function normalizeTheme(theme){const t=String(theme||'');if(t==='day'||t==='light')return 'day';return 'space'}
function applyTheme(theme){const t=normalizeTheme(theme);document.documentElement.setAttribute('data-theme',t);try{localStorage.setItem('gg-theme',t)}catch(e){}const btn=document.getElementById('theme-toggle');if(btn){btn.title='切换主题';btn.setAttribute('aria-label','切换主题');const lbl=btn.querySelector('.lbl');if(lbl)lbl.remove();Array.from(btn.childNodes).forEach(function(n){if(n.nodeType===3&&String(n.textContent||'').trim())n.remove()})}try{if(document.getElementById('orbit-stage'))renderOrbitSystem()}catch(e){}}
function toggleTheme(){const cur=normalizeTheme(document.documentElement.getAttribute('data-theme'));applyTheme(cur==='space'?'day':'space')}
(function(){let t='space';try{t=localStorage.getItem('gg-theme')||'space'}catch(e){}applyTheme(t)})();
async function loadStats(){const stats=await api('/api/stats');if(!stats)return;document.getElementById('stat-total').textContent=safe(stats.total);document.getElementById('stat-http').textContent=safe(stats.http);document.getElementById('stat-socks5').textContent=safe(stats.socks5);document.getElementById('stat-subscription').textContent=safe(stats.subscription_count);document.getElementById('stat-sessions').textContent=safe(stats.active_sessions)}
async function loadProxies(){const data=await api('/api/proxies');if(!data)return;allProxies=Array.isArray(data)?data:[];allRegions=Array.from(new Set(allProxies.filter(p=>isAvailable(p)&&isKnownRegion(p)).map(regionOf))).sort();renderRegionFilter();renderProxies(true);renderRegions();renderOrbitSystem()}
function renderRegionFilter(){const select=document.getElementById('region-filter');const current=select.value;select.innerHTML='<option value="">全部地域</option>'+allRegions.map(r=>'<option value="'+html(r)+'">'+html(r).toUpperCase()+'</option>').join('');select.value=allRegions.includes(current)?current:''}
function sourceLabel(p){if(p.source==='manual')return '手工';return p.subscription_name?p.subscription_name:'订阅';}
function nodeLabel(p){if(p.note)return p.note;return maskAddress(p.address);}
// 节点状态：user_paused→已停用；可用→ok；disabled 且从未验证→pending；否则不可用。
// last_check 空/零值视为未验证（待验证），有 last_check 或 fail_count≥3 视为验证失败。
function hasLastCheck(p){const v=p&&p.last_check;if(v==null||v===''||v===false)return false;const s=String(v);if(s.indexOf('0001-')===0||s.indexOf('1970-01-01')===0)return false;return true}
function nodeState(p){if(isUserPaused(p)||p.status==='paused')return 'paused';if(isAvailable(p))return 'ok';if(Number(p.fail_count||0)>=3)return 'failed';if(p.status==='disabled')return hasLastCheck(p)?'failed':'pending';return 'pending'}
function stateBadge(st){switch(st){case 'ok':return '<span class="badge ok">可用</span>';case 'paused':return '<span class="badge warn">已停用</span>';case 'failed':return '<span class="badge danger">不可用</span>';default:return '<span class="badge gray">待验证</span>'}}
// abuserBadge: ipapi.is abuser_score<0 显示 "--"（未探测/查询失败）；否则显示 0.00-1.00 两位小数 + 颜色。
// 阈值：<0.10 绿(ok)、0.10-0.50 黄(warn)、>0.50 红(danger)。两源分开展示，不与 ip-api 聚合。
function abuserBadge(score){const n=Number(score);if(!Number.isFinite(n)||n<0)return '<span class="muted">--</span>';const cls=n<0.1?'ok':(n<=0.5?'warn':'danger');return '<span class="badge '+cls+'">'+html(n.toFixed(2))+'</span>'}
// ipapiFlagsBadges: ip-api 命中标记逗号串。proxy 红、hosting 黄、mobile 灰；seen=true 且无命中显"干净"绿；未探测显 "--"。
function ipapiFlagsBadges(flags,seen){const raw=String(flags||'').trim();if(raw===''){return seen?'<span class="badge ok">干净</span>':'<span class="muted">--</span>'}const cls={proxy:'danger',hosting:'warn',mobile:'gray'};return raw.split(',').map(f=>f.trim()).filter(Boolean).map(f=>'<span class="badge '+(cls[f]||'gray')+'">'+html(f)+'</span>').join(' ')}
// cfBadge: cf_blocked==1 显"拦截"红、==0 显"正常"绿、其它(-1/未探测)显 "--"。
function cfBadge(v){ v=Number(v); if(v===1)return '<span class="badge danger">拦截</span>'; if(v===0)return '<span class="badge ok">正常</span>'; return '<span class="muted">--</span>' }
// AI 状态只接受后端合同中的数字 0/1；缺字段、布尔值、坏 JSON 均保持未探测。
function parseAIReachability(v){if(v&&typeof v==='object'&&!Array.isArray(v))return v;const raw=String(v||'').trim();if(!raw)return null;try{const m=JSON.parse(raw);return m&&typeof m==='object'&&!Array.isArray(m)?m:null}catch(e){return null}}
function aiValueState(v){if(v===0||v==='0')return 'unlocked';if(v===1||v==='1')return 'blocked';return 'unprobed'}
function aiBadges(v){const m=parseAIReachability(v);if(!m)return '<span class="muted">--</span>';const defs=[['openai','GPT','ChatGPT'],['claude','Cld','Claude'],['grok','Grk','Grok'],['gemini','Gem','Gemini']];return '<span class="ai-marks">'+defs.map(function(d){const state=aiValueState(m[d[0]]);const cls=state==='unlocked'?'ok':(state==='blocked'?'bad':'na');const glyph=state==='unlocked'?'✓':(state==='blocked'?'✗':'–');const title=d[2]+(state==='unlocked'?' 畅通':(state==='blocked'?' 阻断':' 未探测'));return '<span class="ai-mark '+cls+'" role="img" aria-label="'+html(title)+'" title="'+html(title)+'"><span class="nm">'+d[1]+'</span><span class="gl" aria-hidden="true">'+glyph+'</span></span>'}).join('')+'</span>'}
function aiStateOf(p,svc){const m=parseAIReachability(p&&p.ai_reachability);return aiValueState(m&&m[svc])}
function cfStateOf(p){const v=p&&p.cf_blocked;if(v===0||v==='0')return 'unlocked';if(v===1||v==='1')return 'blocked';return 'unknown'}
function qualityOf(p){return String((p&&p.quality_grade)||'').trim().toUpperCase()}
function purityStateOf(p){const raw=p&&p.ipapiis_score;if(raw===null||raw===undefined||raw===''||typeof raw==='boolean')return 'unprobed';const n=Number(raw);if(!Number.isFinite(n)||n<0)return 'unprobed';if(n<0.1)return 'clean';if(n<=0.5)return 'caution';return 'risky'}
function filterVal(id){const el=document.getElementById(id);return el?String(el.value||'').trim():''}
// starBtn: 星标切换按钮，★ 已加星 / ☆ 未加星。
function starBtn(p){ const id=proxyIDArg(p); const on=!!(p.starred===true||Number(p.starred)===1); return '<button class="star'+(on?' on':'')+'" onclick="toggleStar('+id+','+(on?'true':'false')+')" title="'+(on?'取消星标':'加星标')+'" aria-label="星标">'+(on?'★':'☆')+'</button>' }
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
// 网关缺少稳定身份时显式拒绝，不把会变化的 loopback address 当作 pin；用户名/密码编码为 URL userinfo。
// 成功 toast 不回显含真实密码的完整 URL。
function encodeProxyUserInfo(value){return encodeURIComponent(String(value||'')).replace(/[!'()*]/g,c=>'%'+c.charCodeAt(0).toString(16).toUpperCase())}
function encodeNodeKeyPin(nkey){nkey=String(nkey||'').trim();if(!nkey)return '';try{return btoa(unescape(encodeURIComponent(nkey))).replace(/\+/g,'-').replace(/\//g,'_').replace(/=+$/,'')}catch(e){return ''}}
function copyProxyCred(id){ const p=allProxies.find(x=>Number(x.id)===Number(id)); if(!p)return; const addr=String(p.address||''); if(isDirectNode(p)){ const scheme=String(p.protocol||'socks5'); const u=String(p.username||''); const url=u?(scheme+'://'+encodeProxyUserInfo(u)+':'+encodeProxyUserInfo(String(p.password||''))+'@'+addr):(scheme+'://'+addr); navigator.clipboard.writeText(url).then(()=>showToast('已复制直连地址')).catch(()=>showToast('复制失败')); return } const nkey=String(p.node_key||'').trim(); if(!nkey){showToast('无法复制：该网关节点缺少稳定 NodeKey，请刷新订阅或重新导入节点后重试');return} const scheme=isDualProtocol(p)?(confirm('确定复制 SOCKS5？取消则复制 HTTP')?'socks5':'http'):String(p.protocol||'socks5'); const base=(configCache&&configCache.proxy_auth_username)?configCache.proxy_auth_username:'username'; const pin='key-'+encodeNodeKeyPin(nkey); if(pin==='key-'){showToast('无法复制：该网关节点 NodeKey 无法编码，请刷新订阅或重新导入节点后重试');return} const user=base+'-node-'+pin; const rawPass=(configCache&&configCache.proxy_auth_password)?configCache.proxy_auth_password:''; const pass=rawPass||'PASSWORD'; const host=publicIP||location.hostname||'127.0.0.1'; const port=scheme==='http'?(stripColon((configCache&&configCache.http_port)||'7802')):(stripColon((configCache&&configCache.socks5_port)||'7801')); const url=scheme+'://'+encodeProxyUserInfo(user)+':'+encodeProxyUserInfo(pass)+'@'+host+':'+port; const okMsg=rawPass?'已复制（锁定节点身份，非出口IP）':'已复制，请将 PASSWORD 替换为真实密码'; navigator.clipboard.writeText(url).then(()=>showToast(okMsg)).catch(()=>showToast('复制失败')) }
// toggleStar: 加星直接生效；取消星标须 confirm() 确认。
async function toggleStar(id,on){if(on){const ok=await showConfirm('取消该节点星标？','取消星标');if(!ok)return}return runAsync('星标操作失败',async()=>{await api('/api/proxy/star',{method:'POST',body:JSON.stringify({id,starred:!on})});await loadProxies();showToast(on?'已取消星标':'已加星标')})}
// keepPage=true：数据刷新（10s 轮询）保留当前页，避免把用户从第 N 页踢回第 1 页。
// 筛选/搜索 onchange 不传参 → 重置到第 1 页。
function renderProxies(keepPage){if(!keepPage)proxyPage=1;const protocol=document.getElementById('protocol-filter').value;const region=document.getElementById('region-filter').value;const sf=document.getElementById('status-filter').value;const srcf=(document.getElementById('source-filter')||{}).value||'';const qf=filterVal('quality-filter');const pf=filterVal('purity-filter');const cff=filterVal('cf-filter');const aif={openai:filterVal('ai-openai-filter'),claude:filterVal('ai-claude-filter'),grok:filterVal('ai-grok-filter'),gemini:filterVal('ai-gemini-filter')};const latMinRaw=filterVal('latency-min');const latMaxRaw=filterVal('latency-max');const latMin=latMinRaw===''?null:Number(latMinRaw);const latMax=latMaxRaw===''?null:Number(latMaxRaw);const kw=filterVal('keyword-filter').toLowerCase();let rows=allProxies.filter(p=>nodeSupportsInboundProtocol(p,protocol)&&(!region||regionOf(p)===region));if(sf)rows=rows.filter(p=>nodeState(p)===sf);if(srcf==='manual')rows=rows.filter(p=>p.source==='manual');else if(srcf==='subscription')rows=rows.filter(p=>p.source!=='manual');if(qf)rows=rows.filter(p=>qualityOf(p)===qf);if(pf)rows=rows.filter(p=>purityStateOf(p)===pf);if(cff)rows=rows.filter(p=>cfStateOf(p)===cff);['openai','claude','grok','gemini'].forEach(function(svc){const v=aif[svc];if(v)rows=rows.filter(p=>aiStateOf(p,svc)===v)});if(latMin!==null&&Number.isFinite(latMin))rows=rows.filter(p=>{const n=Number(p.latency);return Number.isFinite(n)&&n>0&&n>=latMin});if(latMax!==null&&Number.isFinite(latMax))rows=rows.filter(p=>{const n=Number(p.latency);return Number.isFinite(n)&&n>0&&n<=latMax});if(kw)rows=rows.filter(p=>{const addr=String(p.address||'').toLowerCase();const note=String(p.note||'').toLowerCase();const exitIP=String(p.exit_ip||'').toLowerCase();return addr.indexOf(kw)>=0||note.indexOf(kw)>=0||exitIP.indexOf(kw)>=0});const order={ok:0,pending:1,paused:2,failed:3};rows.sort((a,b)=>{const fa=(nodeState(a)==='ok'&&(a.starred===true||Number(a.starred)===1))?1:0;const fb=(nodeState(b)==='ok'&&(b.starred===true||Number(b.starred)===1))?1:0;if(fa!==fb)return fb-fa;const sa=nodeState(a),sb=nodeState(b);if(order[sa]!==order[sb])return order[sa]-order[sb];return Number(a.latency||1e9)-Number(b.latency||1e9)});const body=document.getElementById('proxy-rows');if(rows.length===0){proxyRenderRows=[];proxyPage=1;const bodyEmpty=document.getElementById('proxy-rows');if(bodyEmpty)bodyEmpty.innerHTML='<tr><td colspan="14" class="empty">没有匹配节点</td></tr>';renderProxyPager();return}proxyRenderRows=rows;const pages=proxyTotalPages();if(proxyPage>pages)proxyPage=pages;if(proxyPage<1)proxyPage=1;renderProxyPage()}
// ISO 3166-1 alpha-2 → 中文国家/地区（常见出口地域；未知码回退大写代码）。
const REGION_ZH={us:'美国',ca:'加拿大',mx:'墨西哥',br:'巴西',ar:'阿根廷',cl:'智利',co:'哥伦比亚',pe:'秘鲁',gb:'英国',uk:'英国',ie:'爱尔兰',fr:'法国',de:'德国',nl:'荷兰',be:'比利时',lu:'卢森堡',ch:'瑞士',at:'奥地利',it:'意大利',es:'西班牙',pt:'葡萄牙',se:'瑞典',no:'挪威',dk:'丹麦',fi:'芬兰',pl:'波兰',cz:'捷克',ro:'罗马尼亚',hu:'匈牙利',bg:'保加利亚',gr:'希腊',tr:'土耳其',ru:'俄罗斯',ua:'乌克兰',jp:'日本',kr:'韩国',cn:'中国',hk:'香港',tw:'台湾',mo:'澳门',sg:'新加坡',my:'马来西亚',th:'泰国',vn:'越南',id:'印度尼西亚',ph:'菲律宾',in:'印度',pk:'巴基斯坦',bd:'孟加拉',au:'澳大利亚',nz:'新西兰',za:'南非',eg:'埃及',il:'以色列',ae:'阿联酋',sa:'沙特阿拉伯',qa:'卡塔尔',kw:'科威特',ng:'尼日利亚',ke:'肯尼亚',kz:'哈萨克斯坦'};
function regionDisplayName(code){const c=String(code||'').trim().toLowerCase();if(!c||c==='unknown')return '未知';return REGION_ZH[c]||c.toUpperCase()}
// 地域聚合：按可用节点数倒序；总览最多 TopN，不足则全显；全量页展示全部。
function buildRegionStats(){const map={};allProxies.filter(p=>isAvailable(p)&&isKnownRegion(p)).forEach(p=>{const r=regionOf(p);if(!map[r])map[r]={region:r,count:0,latSum:0,latN:0,s:0,a:0,b:0,c:0,d:0,sess:0};const st=map[r];st.count++;const lat=Number(p.latency||0);if(lat>0){st.latSum+=lat;st.latN++}const g=qualityOf(p);if(g==='S')st.s++;else if(g==='A')st.a++;else if(g==='B')st.b++;else if(g==='C')st.c++;else if(g==='D')st.d++});const sessByR={};(Array.isArray(orbitSessions)?orbitSessions:[]).forEach(s=>{const r=String((s&&s.region)||'').trim().toLowerCase();if(!r||r==='unknown')return;sessByR[r]=(sessByR[r]||0)+1});Object.keys(map).forEach(function(r){map[r].sess=sessByR[r]||0;map[r].avgLat=map[r].latN?Math.round(map[r].latSum/map[r].latN):0});return Object.keys(map).map(function(k){return map[k]}).sort(function(a,b){return b.count-a.count||a.region.localeCompare(b.region)})}
function regionRowHTML(item,maxN){const pct=maxN?Math.round(item.count*100/maxN):0;const avg=item.avgLat>0?(item.avgLat+' ms'):'--';const chips=[];if(item.s)chips.push('<span class="badge qs">S '+html(item.s)+'</span>');if(item.a)chips.push('<span class="badge qa">A '+html(item.a)+'</span>');if(item.b)chips.push('<span class="badge qb">B '+html(item.b)+'</span>');if(item.c)chips.push('<span class="badge qc">C '+html(item.c)+'</span>');if(item.d)chips.push('<span class="badge qd">D '+html(item.d)+'</span>');if(item.sess)chips.push('<span class="badge ok">会话 '+html(item.sess)+'</span>');chips.push('<span class="badge gray">平均延迟 '+html(avg)+'</span>');return '<div class="region"><span class="cc">'+html(item.region).toUpperCase()+'</span><span class="name">'+html(regionDisplayName(item.region))+'</span><div class="meta"><div class="bar"><i style="width:'+pct+'%"></i></div><div class="chips">'+chips.join('')+'</div></div><div class="n"><span class="big num">'+html(item.count)+'</span><span class="sub">节点</span></div></div>'}
function regionPanelHTML(entries,maxN,limit){if(!entries.length)return '<div class="empty">暂无可用地域数据</div>';const rows=typeof limit==='number'?entries.slice(0,limit):entries;return '<div class="region-head"><span>代码</span><span>国家/地区</span><span>品质结构 · 平均延迟 · 会话</span><span>节点</span></div>'+rows.map(function(item){return regionRowHTML(item,maxN)}).join('')}
// 总览最多展示 REGION_OVERVIEW_LIMIT 个；不足时展示全部，不写死“Top 10”。
const REGION_OVERVIEW_LIMIT=10;
function renderRegions(){const entries=buildRegionStats();const total=entries.reduce(function(sum,item){return sum+item.count},0);const n=entries.length;const showN=Math.min(REGION_OVERVIEW_LIMIT,n);const totalEl=document.getElementById('region-total');if(totalEl){if(!n)totalEl.textContent='0 个可用';else if(n<=REGION_OVERVIEW_LIMIT)totalEl.textContent=total+' 个可用 · '+n+' 个地域';else totalEl.textContent=total+' 个可用 · Top '+REGION_OVERVIEW_LIMIT+' / '+n}const pageTotal=document.getElementById('region-page-total');if(pageTotal)pageTotal.textContent=!n?'0 个可用':(total+' 个可用 · '+n+' 个地域');const list=document.getElementById('region-list');const pageList=document.getElementById('region-page-list');const maxN=entries.reduce(function(m,item){return Math.max(m,item.count)},0);if(list)list.innerHTML=regionPanelHTML(entries,maxN,showN);if(pageList)pageList.innerHTML=regionPanelHTML(entries,maxN)}
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
const regionTracks={};const buckets={};allProxies.filter(p=>isAvailable(p)&&isKnownRegion(p)).forEach(p=>{const cc=regionOf(p);const q=orbitQualityTrack(p);if(!q||q==='d')return;const key=cc+'|'+q;if(!buckets[key])buckets[key]={cc:cc,q:q,n:0,k:0};buckets[key].n++;if(!regionTracks[cc])regionTracks[cc]={};regionTracks[cc][q]=(regionTracks[cc][q]||0)+1});const sessCount={};(Array.isArray(orbitSessions)?orbitSessions:[]).forEach(s=>{const key=orbitSessionBeamKey(s,regionTracks);if(!key)return;sessCount[key]=(sessCount[key]||0)+1});Object.keys(buckets).forEach(key=>{const b=buckets[key];b.k=sessCount[b.cc+'|'+b.q]||0});const byQ={s:[],a:[],b:[],c:[]};Object.values(buckets).forEach(b=>{if(byQ[b.q])byQ[b.q].push(b)});const svgns='http://www.w3.org/2000/svg';['s','a','b','c'].forEach(q=>{const arr=byQ[q];if(!arr.length)return;arr.sort((x,y)=>y.n-x.n);const tr=ORBIT_TRACKS[q];const step=360/arr.length;arr.forEach((d,i)=>{const el=document.createElement('div');el.className='orbit-sat'+(d.k>0?' live':'');el.style.setProperty('--qc',ORBIT_QVAR[q]);const SMIN=30,SMAX=60,NLO=1,NHI=40;const norm=Math.max(0,Math.min(1,(Math.sqrt(d.n)-Math.sqrt(NLO))/(Math.sqrt(NHI)-Math.sqrt(NLO))));const size=Math.round(SMIN+(SMAX-SMIN)*norm);el.dataset.size=String(size);el.style.width=size+'px';el.style.height=size+'px';const tip=html(d.cc).toUpperCase()+' · '+html(d.n)+' 节点 · '+q.toUpperCase()+' 档'+(d.k>0?(' · '+html(d.k)+' 会话'):'');el.innerHTML='<div class="ball"><span class="cc">'+html(d.cc).toUpperCase()+'</span></div><span class="cnt num">'+html(d.n)+'</span>';el.title=tip;layer.appendChild(el);let beam=null;if(d.k>0){const g=document.createElementNS(svgns,'g');const glow=document.createElementNS(svgns,'path');glow.setAttribute('class','orbit-beam-glow');glow.setAttribute('fill','url(#orbitGlow-'+q+')');const path=document.createElementNS(svgns,'path');path.setAttribute('class','orbit-beam');path.setAttribute('fill','url(#orbitBeam-'+q+')');g.appendChild(glow);g.appendChild(path);beamG.appendChild(g);beam={path:path,glow:glow,phase:Math.random()*Math.PI*2,speed:1.1+0.2*Math.min(5,d.k),baseW:Math.max(2.2,Math.min(5.5,2.0+0.85*Math.min(6,d.k)))}}orbitSats.push({el:el,beam:beam,track:tr,baseAngle:tr.phase+i*step,q:q})})});const ipEl=document.getElementById('orbit-gw-ip');if(ipEl){ipEl.textContent=publicIP||(location&&location.hostname)||'--'}orbitBuilt=true}
function orbitFrame(now){if(!orbitLast)orbitLast=now;const dt=(now-orbitLast)/1000;orbitLast=now;const live=(!orbitPaused&&!document.hidden);if(live)orbitT+=dt;const {cx,cy,halfW,halfH}=orbitStageGeom();const sunR=42;const wind=live?updateSolarWind(dt,halfW,halfH,cx,cy):null;const lens=live?updateGravLens(dt,halfW,halfH,cx,cy):null;orbitSats.forEach(s=>{const tr=s.track;const ang=(s.baseAngle+tr.dir*tr.w*orbitT)*Math.PI/180;const rx=halfW*tr.rr,ry=halfH*tr.rr;const x=cx+rx*Math.cos(ang);const y=cy+ry*Math.sin(ang);const depth=(Math.sin(ang)+1)/2;const scale=0.82+0.30*depth;s.el.style.transform='translate3d('+x+'px,'+y+'px,0) translate(-50%,-50%) scale('+scale+')';const zi=String(Math.round(depth*100)+10);if(s.el.dataset.z!==zi){s.el.dataset.z=zi;s.el.style.zIndex=zi}if(s.beam){const dx=x-cx,dy=y-cy;const len=Math.hypot(dx,dy)||1;const sx=cx+dx/len*sunR,sy=cy+dy/len*sunR;s.beam.phase+=dt*s.beam.speed;const baseOp=0.42+0.48*depth;const main=orbitRibbonPath(sx,sy,x,y,s.beam.baseW,s.beam.phase,1,wind,lens);const hit=main.windHit||0;const op=baseOp*(1-0.78*hit);s.beam.path.setAttribute('d',main.d);s.beam.path.style.opacity=Math.max(0.08,op).toFixed(2);if(s.beam.glow){const g=orbitRibbonPath(sx,sy,x,y,s.beam.baseW,s.beam.phase,2.1,wind,lens);s.beam.glow.setAttribute('d',g.d);s.beam.glow.style.opacity=Math.max(0.04,op*0.5*(1-0.5*hit)).toFixed(2)}orbitSetGrad('orbitBeam-'+s.q,sx,sy,x,y);orbitSetGrad('orbitGlow-'+s.q,sx,sy,x,y)}});orbitRAF=requestAnimationFrame(orbitFrame)}
function ensureOrbitLoop(){if(!orbitRAF){orbitLast=0;orbitRAF=requestAnimationFrame(orbitFrame)}}
function renderOrbitSystem(){const stage=document.getElementById('orbit-stage');if(!stage)return;buildOrbitSvg();buildOrbitSats();ensureOrbitLoop()}
function toggleOrbitPause(){orbitPaused=!orbitPaused;const btn=document.getElementById('orbit-pause-btn');if(btn)btn.textContent=orbitPaused?'恢复动画':'暂停动画'}
function renderWorldMap(){renderOrbitSystem()}

// 会话卡展开状态：跨 10s 自动刷新保留，避免「全部展开后几秒又折叠」。
let sessionOpenIDs={};let sessionExpandAll=null;
function expandAllSessions(open){if(open){sessionExpandAll=true}else{sessionExpandAll=null;sessionOpenIDs={}}document.querySelectorAll('#session-rows .session-card').forEach(function(el){el.classList.toggle('open',!!open);const sid=el.getAttribute('data-sid');if(sid){if(open)sessionOpenIDs[sid]=true;else delete sessionOpenIDs[sid]}})}
function toggleSessionCard(el){if(!el)return;const open=el.classList.toggle('open');const sid=el.getAttribute('data-sid')||'';// 手动点开/点关后退出「全部展开」粘性模式，改为按 sessionOpenIDs 精确恢复。
sessionExpandAll=null;if(sid){if(open)sessionOpenIDs[sid]=true;else delete sessionOpenIDs[sid]}const head=el.querySelector('.head');if(head)head.setAttribute('aria-expanded',open?'true':'false')}
function sessionIsOpen(sid){if(sessionExpandAll===true)return true;return !!sessionOpenIDs[sid]}
function sessionExitNode(s){const exitIP=String((s&&s.exit_ip)||'').trim();if(exitIP)return exitIP;const node=String((s&&s.node)||'').trim();if(node&&node.indexOf('127.0.0.1:')!==0&&node.indexOf('[::1]:')!==0&&node.toLowerCase().indexOf('localhost:')!==0)return node;const bind=String((s&&s.bind_address)||'').trim();if(bind&&bind.indexOf('127.0.0.1:')!==0)return bind;return node||bind||'--'}
function sessionSourceLabel(s){if(s&&s.subscription_name)return s.subscription_name;if(s&&s.source==='manual')return '手工';if(s&&s.source==='subscription')return '订阅';return '--'}
function sessionProtoLabel(s){if(s&&(s.dual_protocol===true||Number(s.dual_protocol)===1))return 'SOCKS5+HTTP';const p=String((s&&s.protocol)||'').toLowerCase();if(p==='socks5')return 'SOCKS5';if(p==='http')return 'HTTP';return p?p.toUpperCase():'--'}
// DSL 片段展示：region-ca（不要前导多余 '-'；完整用户名里才是 base-region-ca-...）
function sessionRegionReq(s){const r=String((s&&(s.region_req||s.region))||'').trim().toLowerCase();if(!r||r==='unknown')return '—';return 'region-'+r}
function sessionCooldownLabel(sec){const n=Number(sec)||0;if(n>0)return '<span class="badge warn">'+html(n)+'s</span>';return '<span class="muted">无</span>'}
function sessionOccPct(s){const active=Number(s&&s.active_sessions_on_proxy||0);const max=Number(s&&s.max_sessions_per_proxy||0);if(max>0)return Math.max(0,Math.min(100,Math.round(active*100/max)));return active>0?35:0}
function sessionOccLabel(s){const active=Number(s&&s.active_sessions_on_proxy||0);const max=Number(s&&s.max_sessions_per_proxy||0);return max>0?(active+' / '+max):(active+' / ∞')}
function sessionCardHTML(s){const sid=html(s.session_id);const sidRaw=String(s.session_id||'');const open=sessionIsOpen(sidRaw);const route=html(s.route_label||'');const exitNode=html(sessionExitNode(s));const bindAddr=html(s.bind_address||s.node||'--');const region=String(s.region||'').trim().toLowerCase();const regionBadge=region&&region!=='unknown'?'<span class="badge blue">'+html(region).toUpperCase()+'</span>':'<span class="badge gray">未知</span>';const q=sessionQualityBadge(s.quality_grade);const lat=Number(s.latency)>0?('<span class="num">'+html(s.latency)+' ms</span>'):'<span class="muted">--</span>';const proto=html(sessionProtoLabel(s));const src=html(sessionSourceLabel(s));const proxyID=Number(s.proxy_id)>0?('#'+html(s.proxy_id)):'--';const lastActive=html(s.last_active||'--');const ttlSec=Number(s.remaining_ttl_seconds)||0;const ttlCls=ttlSec>0&&ttlSec<60?' danger':(ttlSec>0&&ttlSec<180?' warn':'');const ttlText=html(formatTTL(ttlSec));const regionReq=html(sessionRegionReq(s));const cooldown=sessionCooldownLabel(s.cooldown_remaining_seconds);const occ=sessionOccPct(s);const occLab=html(sessionOccLabel(s));return '<div class="session-card'+(open?' open':'')+'" data-sid="'+sid+'"><div class="head" onclick="toggleSessionCard(this.parentElement)" role="button" aria-expanded="'+(open?'true':'false')+'"><span class="sid" title="'+sid+'">'+sid+'</span><span class="chips">'+regionBadge+' '+q+' <span class="badge blue">'+proto+'</span> <span class="muted mono" style="font-size:11px">'+exitNode+'</span></span><span class="ttl'+ttlCls+'">'+ttlText+'</span><span class="chev" aria-hidden="true">›</span></div><div class="body"><div class="detail-grid"><div class="di"><span class="k">会话 ID</span><span class="v mono">'+sid+'</span></div><div class="di"><span class="k">绑定节点 Proxy ID</span><span class="v mono">'+proxyID+'</span></div><div class="di"><span class="k">出口节点</span><span class="v mono">'+exitNode+'</span></div><div class="di"><span class="k">出口地域</span><span class="v">'+regionBadge+'</span></div><div class="di"><span class="k">DSL 地域请求</span><span class="v mono">'+regionReq+'</span></div><div class="di"><span class="k">解锁过滤</span><span class="v muted">—</span></div><div class="di"><span class="k">协议</span><span class="v"><span class="badge blue">'+proto+'</span></span></div><div class="di"><span class="k">品质 / 延迟</span><span class="v">'+q+' · '+lat+'</span></div><div class="di"><span class="k">来源</span><span class="v">'+src+'</span></div><div class="di"><span class="k">最近活跃</span><span class="v num">'+lastActive+'</span></div><div class="di"><span class="k">剩余 TTL</span><span class="v ttl'+ttlCls+'" style="color:inherit">'+ttlText+' <span class="muted">('+html(ttlSec)+'s)</span></span></div><div class="di"><span class="k">节点冷却</span><span class="v">'+cooldown+'</span></div><div class="di"><span class="k">本机绑定</span><span class="v mono muted">'+bindAddr+'</span></div></div><div class="route-box"><b>路由标签</b>'+(route||'—')+'</div><div class="occ"><span class="muted" style="font-size:11px;font-weight:700;letter-spacing:.04em;text-transform:uppercase">节点占用</span><div class="bar"><i style="width:'+occ+'%"></i></div><span class="num" style="font-weight:800">'+occLab+'</span><span class="muted" style="font-size:11px">active sticky / max_sessions（0=无限）</span></div></div></div>'}
async function loadSessions(){const sessions=await api('/api/sessions');if(!sessions)return;orbitSessions=Array.isArray(sessions)?sessions:[];renderOrbitSystem();const body=document.getElementById('session-rows');const cnt=document.getElementById('sess-count');if(cnt)cnt.textContent=Array.isArray(sessions)?(sessions.length+' 条 sticky 绑定'):'--';const ov=document.getElementById('ov-session-rows');const ovc=document.getElementById('ov-sess-count');if(ovc)ovc.textContent=Array.isArray(sessions)?(sessions.length+' 条 sticky 绑定'):'--';if(!Array.isArray(sessions)||sessions.length===0){if(body)body.innerHTML='<div class="empty">暂无活跃 session</div>';if(ov)ov.innerHTML='<tr><td colspan="7" class="empty">暂无活跃 session</td></tr>';return}if(ov)ov.innerHTML=sessions.slice(0,8).map(function(s){const sid=html(s.session_id);const route=html(s.route_label||'');const exitNode=html(sessionExitNode(s));const region=String(s.region||'').trim().toLowerCase();const regionBadge=region&&region!=='unknown'?'<span class="badge ok">'+html(region).toUpperCase()+'</span>':'<span class="badge gray">未知</span>';const q=sessionQualityBadge(s.quality_grade);const lat=Number(s.latency)>0?html(s.latency)+' ms':'--';const ttlSec=Number(s.remaining_ttl_seconds)||0;return '<tr><td class="mono">'+sid+'</td><td class="mono muted">'+route+'</td><td>'+regionBadge+'</td><td class="mono">'+exitNode+'</td><td>'+q+'</td><td>'+lat+'</td><td>'+html(formatTTL(ttlSec))+'</td></tr>'}).join('');if(body)body.innerHTML=sessions.map(sessionCardHTML).join('')}
function formatTTL(seconds){const value=Number(seconds)||0;const min=Math.floor(value/60);const sec=value%60;return min>0?min+'m '+sec+'s':sec+'s'}
// sessionQualityBadge: 会话出口节点品质档 S/A/B/C 徽章;无品质数据显示 --。
function sessionQualityBadge(g){const v=String(g||'').trim().toUpperCase();const cls={S:'qs',A:'qa',B:'qb',C:'qc'}[v];if(!cls)return '<span class="muted">--</span>';return '<span class="badge '+cls+'">'+v+'</span>'}
async function addManualNode(){return runAsync('添加失败',async()=>{const payload={link:document.getElementById('manual-link').value.trim(),region:document.getElementById('manual-region').value.trim(),note:document.getElementById('manual-note').value.trim()};if(!payload.link){showToast('请填写节点链接');return}await api('/api/manual-node/add',{method:'POST',body:JSON.stringify(payload)});document.getElementById('manual-link').value='';document.getElementById('manual-region').value='';document.getElementById('manual-note').value='';await Promise.all([loadStats(),loadProxies()]);showToast('手工节点已添加')})}
function toggleSelectAll(on){document.querySelectorAll('.proxy-select').forEach(el=>{el.checked=!!on})}
function selectedProxyIDs(){return Array.from(document.querySelectorAll('.proxy-select:checked')).map(el=>Number(el.value)).filter(n=>Number.isFinite(n)&&n>0)}
async function batchDeleteSelected(){const ids=selectedProxyIDs();if(!ids.length){showToast('请先勾选节点');return}const ok=await showConfirm('删除选中的 '+ids.length+' 个节点？订阅节点删除后可能在下次刷新时重新出现。','删除');if(!ok)return;return runAsync('批量删除失败',async()=>{const r=await api('/api/manual-node/batch-delete',{method:'POST',body:JSON.stringify({ids})});await Promise.all([loadStats(),loadProxies()]);showToast('已删除 '+(r&&r.deleted!=null?r.deleted:ids.length)+' 个'+(r&&r.failed?('，失败 '+r.failed):''))})}
async function importManualNodes(){return runAsync('批量导入失败',async()=>{const text=document.getElementById('import-text').value;const region=document.getElementById('import-region').value.trim();const note=document.getElementById('import-note').value.trim();if(!String(text||'').trim()){showToast('请粘贴代理列表');return}const r=await api('/api/manual-node/import',{method:'POST',body:JSON.stringify({text,region,note})});document.getElementById('import-modal').classList.remove('show');document.getElementById('import-text').value='';await Promise.all([loadStats(),loadProxies()]);showToast('导入完成：新增 '+(r.added||0)+' / 跳过 '+(r.skipped||0)+' / 失败 '+(r.failed||0))})}
// manageNode: 来源无关的统一节点管理弹窗（订阅节点与手工节点都可用）。
// 以 proxy-id 为主身份；地域/备注保存与删除均走应用内弹窗，不再用浏览器 prompt/confirm。
function manageNode(id){const p=allProxies.find(x=>Number(x.id)===Number(id))||{};document.getElementById('node-modal-id').value=String(id);const addrEl=document.getElementById('node-modal-addr');if(addrEl)addrEl.textContent=maskAddress(p.address||'');const rEl=document.getElementById('node-modal-region');if(rEl)rEl.value=String(p.region||'');const nEl=document.getElementById('node-modal-note');if(nEl)nEl.value=String(p.note||'');document.getElementById('node-modal').classList.add('show')}
function closeNodeModal(){document.getElementById('node-modal').classList.remove('show')}
async function nodeModalSave(){return runAsync('保存失败',async()=>{const id=Number(document.getElementById('node-modal-id').value);if(!Number.isFinite(id)||id<=0){showToast('无效节点');return}const region=document.getElementById('node-modal-region').value.trim();const note=document.getElementById('node-modal-note').value;await api('/api/manual-node/region',{method:'POST',body:JSON.stringify({id,region})});await api('/api/manual-node/note',{method:'POST',body:JSON.stringify({id,note})});closeNodeModal();await loadProxies();showToast('节点已更新')})}
async function nodeModalDelete(){const id=Number(document.getElementById('node-modal-id').value);if(!Number.isFinite(id)||id<=0){showToast('无效节点');return}const ok=await showConfirm('删除此节点？删除后需重新添加或等待订阅刷新。','删除');if(!ok)return;return runAsync('删除失败',async()=>{await api('/api/proxy/delete',{method:'POST',body:JSON.stringify({id})});closeNodeModal();await Promise.all([loadStats(),loadProxies()]);showToast('节点已删除')})}
// editNote: 来源无关的备注快捷编辑入口（点击名称/备注单元格）。复用统一管理弹窗。
function editNote(id){manageNode(id)}
async function toggleProxy(id,address,enable){return runAsync('操作失败',async()=>{await api('/api/proxy/toggle',{method:'POST',body:JSON.stringify({id,address,enable})});await Promise.all([loadStats(),loadProxies()]);showToast(enable?'节点已启用':'节点已停用')})}
// testProxy: 触发单节点重新验证（走完整 ValidateOne，含连通 google/openai/github/cloudflare/gstatic），后端异步执行，稍后自动刷新列表。
async function testProxy(id,address){return runAsync('测试失败',async()=>{await api('/api/proxy/refresh',{method:'POST',body:JSON.stringify({id,address})});showToast('测试连通已启动，稍后自动刷新');setTimeout(()=>runAsync('刷新失败',()=>Promise.all([loadStats(),loadProxies()])),4000)})}
let allSubs=[];async function loadSubscriptions(){const subs=await api('/api/subscriptions');if(!subs)return;allSubs=Array.isArray(subs)?subs:[];const box=document.getElementById('sub-list');if(!Array.isArray(subs)||subs.length===0){box.innerHTML='<div class="empty">暂无订阅，点右上角“添加订阅”</div>';return}box.innerHTML=subs.map(sub=>{const paused=sub.status==='paused';const activeCount=Number(sub.active_count||0);const disabledCount=Number(sub.disabled_count||0);const proxyCount=Number(sub.proxy_count||0);const pausedCount=Number(sub.paused_count??Math.max(0,proxyCount-activeCount-disabledCount));const toggleLabel=paused?'启用':'暂停';const badge=paused?'<span class="badge warn">已暂停</span>':'<span class="badge ok">活跃</span>';const id=Number(sub.id);const idArg=Number.isFinite(id)?String(id):'0';const url=String(sub.url||'');const urlLine=url?('<div class="muted mono" style="margin-top:4px;font-size:11px;word-break:break-all">'+html(url)+'</div>'):'<div class="muted mono" style="margin-top:4px;font-size:11px">（本地文件订阅）</div>';return '<div class="sub-item"><div class="meta"><strong>'+html(sub.name)+' '+badge+'</strong><div class="muted">'+html(activeCount)+' 可用 / '+html(pausedCount)+' 暂停 / '+html(disabledCount)+' 不可用</div>'+urlLine+'</div><div class="mini-actions"><button class="mini" onclick="openSubModal('+idArg+')" title="修改名称/URL/间隔/请求头">修改</button><button class="mini" onclick="refreshSub('+idArg+')" title="重新拉取并验证">刷新</button><button class="mini" onclick="toggleSub('+idArg+')" title="启用或暂停该订阅及其节点">'+toggleLabel+'</button><button class="mini danger" onclick="deleteSub('+idArg+')" title="删除订阅及其节点">删除</button></div></div>'}).join('')}
function openSubModal(id){const editing=id!=null&&id!=='';const sub=editing?allSubs.find(s=>Number(s.id)===Number(id)):null;document.getElementById('sub-edit-id').value=sub?String(sub.id):'';document.getElementById('sub-modal-title').textContent=sub?'修改订阅':'添加订阅';document.getElementById('sub-modal-submit').textContent=sub?'保存':'添加';document.getElementById('sub-name').value=sub?(sub.name||''):'';document.getElementById('sub-refresh').value=sub?(sub.refresh_min||60):60;document.getElementById('sub-url').value=sub?(sub.url||''):'';document.getElementById('sub-headers').value=sub?(sub.headers||''):'';const fileField=document.getElementById('sub-file-field');const fc=document.getElementById('sub-file-content');if(fc)fc.value='';if(fileField)fileField.style.display=sub?'none':'';document.getElementById('sub-modal').classList.add('show')}
function closeSubModal(){document.getElementById('sub-modal').classList.remove('show')}
function submitSubscription(){const editId=document.getElementById('sub-edit-id').value.trim();if(editId){return updateSubscription(editId)}return addSubscription()}
async function addSubscription(){return runAsync('添加失败',async()=>{const payload={name:document.getElementById('sub-name').value.trim(),url:document.getElementById('sub-url').value.trim(),file_content:document.getElementById('sub-file-content').value.trim(),headers:document.getElementById('sub-headers').value.trim(),refresh_min:Number(document.getElementById('sub-refresh').value)||60};if(!payload.url&&!payload.file_content){showToast('请填写订阅 URL 或粘贴配置内容');return}await api('/api/subscription/add',{method:'POST',body:JSON.stringify(payload)});document.getElementById('sub-name').value='';document.getElementById('sub-url').value='';document.getElementById('sub-file-content').value='';document.getElementById('sub-headers').value='';closeSubModal();await Promise.all([loadSubscriptions(),loadStats(),loadProxies()]);showToast('订阅已添加')})}
async function updateSubscription(id){return runAsync('修改失败',async()=>{const payload={id:Number(id),name:document.getElementById('sub-name').value.trim(),url:document.getElementById('sub-url').value.trim(),headers:document.getElementById('sub-headers').value.trim(),refresh_min:Number(document.getElementById('sub-refresh').value)||60};if(!Number.isFinite(payload.id)||payload.id<=0){showToast('无效订阅');return}await api('/api/subscription/update',{method:'POST',body:JSON.stringify(payload)});closeSubModal();await Promise.all([loadSubscriptions(),loadStats(),loadProxies()]);showToast('订阅已修改，正在重新拉取')})}
async function refreshSub(id){return runAsync('刷新失败',async()=>{await api('/api/subscription/refresh',{method:'POST',body:JSON.stringify({id})});showToast('刷新已启动，稍后自动更新');refreshLater()})}
async function refreshAllSubs(){return runAsync('刷新失败',async()=>{await api('/api/subscription/refresh-all',{method:'POST'});showToast('全部刷新已启动，稍后自动更新');refreshLater()})}
async function toggleSub(id){return runAsync('切换失败',async()=>{await api('/api/subscription/toggle',{method:'POST',body:JSON.stringify({id})});await Promise.all([loadSubscriptions(),loadStats(),loadProxies()]);showToast('已切换启用/暂停状态')})}
async function deleteSub(id){return runAsync('删除失败',async()=>{if(!confirm('删除此订阅及其全部节点？'))return;await api('/api/subscription/delete',{method:'POST',body:JSON.stringify({id})});await Promise.all([loadSubscriptions(),loadStats(),loadProxies()]);showToast('订阅已删除')})}
async function loadLogs(){const data=await api('/api/logs');if(!data)return;const box=document.getElementById('logs-box');if(!box)return;const prevTop=box.scrollTop;const lines=Array.isArray(data.lines)?data.lines:[];box.innerHTML=lines.length?lines.map(line=>'<div class="log-line">'+html(line)+'</div>').join(''):'<div class="log-line">no logs</div>';const auto=document.getElementById('logs-autoscroll');if(auto&&auto.checked){box.scrollTop=box.scrollHeight}else{box.scrollTop=prevTop}}
async function loadConfig(){configCache=await api('/api/config');if(!configCache)return;const hp=stripColon(configCache.http_port),sp=stripColon(configCache.socks5_port),wp=stripColon(configCache.webui_port);document.getElementById('cfg-http-port').value=hp;document.getElementById('cfg-socks5-port').value=sp;document.getElementById('cfg-webui-port').value=wp;document.getElementById('cfg-auth-enabled').value=String(Boolean(configCache.proxy_auth_enabled));document.getElementById('cfg-auth-username').value=configCache.proxy_auth_username||'';document.getElementById('cfg-auth-password').value='';document.getElementById('cfg-session-ttl').value=configCache.session_ttl_minutes||'';document.getElementById('cfg-default-region').value=configCache.default_region||'';document.getElementById('cfg-health-interval').value=configCache.health_check_interval||'';document.getElementById('cfg-max-retry').value=configCache.max_retry??'';document.getElementById('cfg-singbox-path').value=configCache.singbox_path||'';document.getElementById('cfg-allowed-countries').value=(configCache.allowed_countries||[]).join(',');document.getElementById('cfg-blocked-countries').value=(configCache.blocked_countries||[]).join(',');renderConnection();renderDSLExamples()}
async function loadPublicIP(){return runAsync('公网 IP 获取失败',async()=>{const d=await api('/api/public-ip');if(d){if(d.public_ip){publicIP=d.public_ip;renderConnection()}if(d.country){gatewayCC=String(d.country).toLowerCase()}renderOrbitSystem()}})}
function renderConnection(){if(!configCache)return;const sp=stripColon(configCache.socks5_port)||'7801';const hp=stripColon(configCache.http_port)||'7802';const base=configCache.proxy_auth_username||'username';const enabled=configCache.proxy_auth_enabled;const host=publicIP||location.hostname||'127.0.0.1';const setText=function(id,v){const el=document.getElementById(id);if(el)el.textContent=v};setText('conn-socks5',host+':'+(sp||'7801'));setText('conn-http',host+':'+(hp||'7802'));setText('conn-user',base);setText('conn-pass',enabled?'见首次启动日志 / 系统设置':'（认证已关闭，无需密码）');setText('conn-auth-state',enabled?'代理认证：开启':'代理认证：关闭');const cred=enabled?(base+':PASSWORD@'):'';setText('conn-cmd','curl --socks5 '+cred+host+':'+(sp||'7801')+' https://www.gstatic.com/generate_204');renderDSLExamples()}
function renderDSLExamples(){const base=(configCache&&configCache.proxy_auth_username)?configCache.proxy_auth_username:'username';const hint=document.getElementById('dsl-hint');if(!hint)return;const syntax='<base>[-region-<cc>][-unlock-<token>][-node-<host:port|key-<base64url(nodeKey)>>][-session-<id>]';const nodeHint='key-<base64url(nodeKey)> 是稳定配置身份（优先）；host:port 是兼容入口地址。二者都不是最终出口 IP；无匹配节点时显式失败，不回退。node 锁定决定路由，优先于 session 黏连。';hint.textContent=(configCache&&configCache.proxy_auth_enabled!==false)?('语法：'+syntax+'；固定顺序 region → unlock → node → session；前缀 “'+base+'” 是代理认证用户名。'+nodeHint):'代理认证当前关闭；启用后前缀须等于代理认证用户名。'}
async function openSettings(){switchTab('settings')}function closeSettings(){}function countries(id){return document.getElementById(id).value.split(',').map(v=>v.trim().toUpperCase()).filter(Boolean)}
function formatAPIKeyTime(v){if(!v)return '--';const d=new Date(v);return Number.isNaN(d.getTime())?String(v):d.toLocaleString()}
function renderAPIKeys(keys){const body=document.getElementById('apikey-rows');if(!body)return;const list=Array.isArray(keys)?keys:[];if(!list.length){body.innerHTML='<tr><td colspan="5" class="empty">暂无 API Key</td></tr>';return}body.innerHTML=list.map(k=>{const id=html(k.id);const name=html(k.name);const created=html(formatAPIKeyTime(k.created_at));const last=html(formatAPIKeyTime(k.last_used_at));const disabled=!!(k.disabled===true||Number(k.disabled)===1);const st=disabled?'<span class="badge warn">已吊销</span>':'<span class="badge ok">有效</span>';const revokeBtn=disabled?'':'<button class="mini" onclick="revokeAPIKey(\''+id+'\')">吊销</button> ';return '<tr><td>'+name+'</td><td>'+created+'</td><td>'+last+'</td><td>'+st+'</td><td>'+revokeBtn+'<button class="mini danger" onclick="deleteAPIKey(\''+id+'\')">删除</button></td></tr>'}).join('')}
async function loadAPIKeys(){const data=await api('/api/apikeys');if(!data)return;renderAPIKeys(data.keys||data||[])}
async function createAPIKey(){return runAsync('创建 API Key 失败',async()=>{const name=document.getElementById('apikey-name').value.trim();if(!name){showToast('请填写 Key 名称');return}const r=await api('/api/apikey/create',{method:'POST',body:JSON.stringify({name})});document.getElementById('apikey-name').value='';document.getElementById('apikey-once-name').value=r&&r.name?r.name:name;document.getElementById('apikey-once-key').value=r&&r.key?r.key:'';document.getElementById('apikey-once-modal').classList.add('show');await loadAPIKeys();showToast('API Key 已创建（仅显示一次）')})}
async function revokeAPIKey(id){return runAsync('吊销失败',async()=>{if(!confirm('吊销该 API Key？吊销后立即失效。'))return;await api('/api/apikey/revoke',{method:'POST',body:JSON.stringify({id})});await loadAPIKeys();showToast('已吊销')})}
async function deleteAPIKey(id){return runAsync('删除失败',async()=>{if(!confirm('删除该 API Key？此操作不可恢复。'))return;await api('/api/apikey/delete',{method:'POST',body:JSON.stringify({id})});await loadAPIKeys();showToast('已删除')})}
async function saveConfig(){return runAsync('保存失败',async()=>{if(!configCache)await loadConfig();if(!configCache)throw new Error('配置未加载');const payload={proxy_auth_enabled:document.getElementById('cfg-auth-enabled').value==='true',proxy_auth_username:document.getElementById('cfg-auth-username').value.trim(),proxy_auth_password:document.getElementById('cfg-auth-password').value,session_ttl_minutes:Number(document.getElementById('cfg-session-ttl').value),default_region:document.getElementById('cfg-default-region').value.trim().toLowerCase(),health_check_interval:Number(document.getElementById('cfg-health-interval').value),max_retry:Number(document.getElementById('cfg-max-retry').value),singbox_path:document.getElementById('cfg-singbox-path').value.trim(),allowed_countries:countries('cfg-allowed-countries'),blocked_countries:countries('cfg-blocked-countries')};await api('/api/config/save',{method:'POST',body:JSON.stringify(payload)});await loadConfig();showToast('配置已保存')})}
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
const FILTER_CYCLE={'':'全部','unlocked':'畅通','blocked':'阻断','unprobed':'未探测','unknown':'未知'};
const FILTER_STATE={'':'all','unlocked':'ok','blocked':'bad','unprobed':'unk','unknown':'unk'};
function cycleFilter(selId,btnId){const sel=document.getElementById(selId);if(!sel)return;const opts=Array.from(sel.options).map(o=>o.value);let idx=opts.indexOf(sel.value);idx=(idx+1)%opts.length;sel.value=opts[idx];syncFilterToggle(selId,btnId);renderProxies()}
function syncFilterToggle(selId,btnId){const sel=document.getElementById(selId);const btn=document.getElementById(btnId);if(!sel||!btn)return;const v=sel.value;const st=btn.querySelector('.st');if(st)st.textContent=FILTER_CYCLE[v]||'全部';btn.dataset.state=FILTER_STATE[v]||'all';btn.setAttribute('aria-pressed',v?'true':'false')}
function initFilterToggles(){document.querySelectorAll('.filter-toggle[data-sel]').forEach(function(btn){syncFilterToggle(btn.dataset.sel,btn.id)})}
// 节点表分页：默认每页 20，可选 20/50/100；筛选变化回到第 1 页。
let proxyRenderRows=[];let proxyPage=1;let proxyPageSize=20;
function proxyTotalPages(){const n=proxyRenderRows.length;if(n<=0)return 1;return Math.max(1,Math.ceil(n/proxyPageSize))}
function proxyPageSlice(){const start=(proxyPage-1)*proxyPageSize;return proxyRenderRows.slice(start,start+proxyPageSize)}
function renderProxyPage(){const body=document.getElementById('proxy-rows');if(!body)return;const slice=proxyPageSlice();if(!slice.length){body.innerHTML='<tr><td colspan="14" class="empty">没有匹配节点</td></tr>';renderProxyPager();return}body.innerHTML=slice.map(proxyRowHTML).join('');renderProxyPager();const selAll=document.getElementById('proxy-select-all');if(selAll)selAll.checked=false}
function renderProxyPager(){const total=proxyRenderRows.length;const pages=proxyTotalPages();const info=document.getElementById('proxy-page-info');const num=document.getElementById('proxy-page-num');const prev=document.getElementById('proxy-page-prev');const next=document.getElementById('proxy-page-next');const sizeSel=document.getElementById('proxy-page-size');if(sizeSel&&String(proxyPageSize)!==sizeSel.value){sizeSel.value=String(proxyPageSize)}if(info){if(total<=0){info.textContent='共 0 条'}else{const from=(proxyPage-1)*proxyPageSize+1;const to=Math.min(proxyPage*proxyPageSize,total);info.textContent='共 '+total+' 条，显示 '+from+'-'+to}}if(num)num.textContent=proxyPage+' / '+pages;if(prev)prev.disabled=proxyPage<=1||total<=0;if(next)next.disabled=proxyPage>=pages||total<=0}
function proxyPagePrev(){if(proxyPage<=1)return;proxyPage--;renderProxyPage()}
function proxyPageNext(){if(proxyPage>=proxyTotalPages())return;proxyPage++;renderProxyPage()}
function proxyPageSizeChange(){const sel=document.getElementById('proxy-page-size');const n=sel?Number(sel.value):20;proxyPageSize=(n===50||n===100)?n:20;proxyPage=1;renderProxyPage()}
function proxyRowHTML(p){const addr=addressArg(p.address);const id=proxyIDArg(p);const manual=p.source==='manual';const st=nodeState(p);const label=html(nodeLabel(p));const showRegion=isAvailable(p)&&isKnownRegion(p);const toggleBtn=(st==='paused')?'<button class="mini" onclick="toggleProxy('+id+',decodeURIComponent(\''+addr+'\'),true)">启用</button>':'<button class="mini" onclick="toggleProxy('+id+',decodeURIComponent(\''+addr+'\'),false)">停用</button>';const testBtn='<button class="mini" onclick="testProxy('+id+',decodeURIComponent(\''+addr+'\'))">测试</button>';const copyBtn='<button class="mini" onclick="copyProxyCred('+id+')">复制</button>';const baseActions=testBtn+' '+copyBtn+' '+toggleBtn;const manageBtn='<button class="mini" onclick="manageNode('+id+')">管理</button>';const actions=baseActions+' '+manageBtn;const latencyText=Number(p.latency)>0?html(p.latency)+' ms':'--';const sel='<input type="checkbox" class="proxy-select" value="'+id+'">';const nameCell='<td class="note-edit" title="'+label+' · 点击编辑备注" style="cursor:pointer" onclick="editNote('+id+')">'+label+'</td>';return '<tr><td>'+sel+'</td><td>'+starBtn(p)+'</td>'+nameCell+'<td>'+protocolBadges(p)+'</td><td>'+(showRegion?'<span class="badge ok">'+html(regionOf(p)).toUpperCase()+'</span>':'<span class="muted">--</span>')+'</td><td class="mono">'+html(p.exit_ip)+'</td><td>'+latencyText+'</td><td>'+abuserBadge(p.ipapiis_score)+'</td><td>'+ipapiFlagsBadges(p.ipapi_flags,!!p.ipapi_flags_seen)+'</td><td>'+cfBadge(p.cf_blocked)+'</td><td>'+aiBadges(p.ai_reachability)+'</td><td>'+html(sourceLabel(p))+'</td><td>'+stateBadge(st)+'</td><td><span class="ops">'+actions+'</span></td></tr>'}
let orbitResizeTimer=null;window.addEventListener('resize',function(){clearTimeout(orbitResizeTimer);orbitResizeTimer=setTimeout(function(){try{renderOrbitSystem()}catch(e){}},180)});
// 骨架墓碑：载入态灰条 shimmer（尊重 prefers-reduced-motion，动画由 CSS 关闭）。
function skeletonRows(n){let h='';for(let i=0;i<(n||3);i++)h+='<div class="skeleton sk-row"></div>';return '<div class="skeleton-wrap">'+h+'</div>'}
function showSkeletons(){['region-list','sub-list','session-rows','singbox-status'].forEach(function(id){const el=document.getElementById(id);if(el)el.innerHTML=skeletonRows(3)})}
// 首次进入总览再画分布图。
// 每次回到总览都即时重建轨道（椭圆底图/卫星/网关），避免返回 dashboard 后底图数秒才出现。
function markViewLazy(name){if(name==='overview'){try{renderOrbitSystem()}catch(e){}requestAnimationFrame(function(){try{renderOrbitSystem()}catch(e){}})}}
initFilterToggles();showSkeletons();markViewLazy('overview');
refreshAll();
setInterval(()=>runAsync('自动刷新失败',()=>Promise.all([loadStats(),loadProxies(),loadSubscriptions(),loadSessions()])),10000);
setInterval(()=>runAsync('日志刷新失败',loadLogs),5000);`
