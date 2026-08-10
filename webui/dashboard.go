package webui

const dashboardHTML = `<!DOCTYPE html>
<html lang="zh-CN" data-theme="space">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>GeoProxy Gateway Admin</title>
<link rel="stylesheet" href="/assets/dashboard.css">
</head>
<body>
<div class="app" id="app">
<aside class="sidebar preload" id="sidebar">
 <div class="sidebar-brand brand"><div class="mark">GG</div><div class="bt">GeoProxy<small>Admin</small></div></div>
 <nav class="sidebar-nav nav" id="mainNav">
   <div class="lab" data-i18n="nav_control">总控</div>
   <button class="navitem active" data-tab="overview" onclick="switchTab('overview')" data-i18n-title="nav_overview" data-i18n-aria="nav_overview" title="总览" aria-label="总览"><span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="3"/><ellipse cx="12" cy="12" rx="10" ry="4.5"/><ellipse cx="12" cy="12" rx="4.5" ry="10"/></svg></span><span class="lbl t" data-i18n="nav_overview">总览</span></button>
   <button class="navitem" data-tab="nodes" onclick="switchTab('nodes')" data-i18n-title="nav_nodes" data-i18n-aria="nav_nodes" title="节点" aria-label="节点"><span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="5" cy="6" r="2"/><circle cx="5" cy="18" r="2"/><circle cx="19" cy="12" r="2"/><path d="M7 6h6M7 18h6M13 6c0 4 4 3 4 6M13 18c0-4 4-3 4-6"/></svg></span><span class="lbl t" data-i18n="nav_nodes">节点</span></button>
   <button class="navitem" data-tab="subs" onclick="switchTab('subs')" data-i18n-title="nav_subs" data-i18n-aria="nav_subs" title="订阅" aria-label="订阅"><span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 11a9 9 0 0 1 9 9"/><path d="M4 4a16 16 0 0 1 16 16"/><circle cx="5" cy="19" r="1.5" fill="currentColor" stroke="none"/></svg></span><span class="lbl t" data-i18n="nav_subs">订阅</span></button>
    <button class="navitem" data-tab="sessions" onclick="switchTab('sessions')" data-i18n-title="nav_sessions" data-i18n-aria="nav_sessions" title="会话" aria-label="会话"><span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="8" r="4"/><path d="M4 20c0-4 4-6 8-6s8 2 8 6"/></svg></span><span class="lbl t" data-i18n="nav_sessions">会话</span></button>
    <button class="navitem" data-tab="regions" onclick="switchTab('regions')" data-i18n-title="nav_regions" data-i18n-aria="nav_regions" title="地域" aria-label="地域"><span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/></svg></span><span class="lbl t" data-i18n="nav_regions">地域</span></button>
    <div class="lab" data-i18n="nav_ops">运维</div>
   <button class="navitem" data-tab="logs" onclick="switchTab('logs')" data-i18n-title="nav_logs" data-i18n-aria="nav_logs" title="日志" aria-label="日志"><span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M6 3h9l3 3v15H6z"/><path d="M9 9h6M9 13h6M9 17h4"/></svg></span><span class="lbl t" data-i18n="nav_logs">日志</span></button>
   <button class="navitem" data-tab="api" onclick="switchTab('api')" data-i18n-title="nav_api" data-i18n-aria="nav_api" title="API" aria-label="API"><span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M8 3H5a2 2 0 0 0-2 2v3M16 3h3a2 2 0 0 1 2 2v3M8 21H5a2 2 0 0 1-2-2v-3M16 21h3a2 2 0 0 0 2-2v-3"/><circle cx="12" cy="12" r="2.5"/></svg></span><span class="lbl t" data-i18n="nav_api">API</span></button>
   <button class="navitem" data-tab="settings" onclick="switchTab('settings')" data-i18n-title="nav_settings" data-i18n-aria="nav_settings" title="设置" aria-label="设置"><span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg></span><span class="lbl t" data-i18n="nav_settings">设置</span></button>
  </nav>
  <div class="sidebar-foot sidefoot">
   <button class="sidebar-collapse collapse-btn" onclick="toggleSidebar()" data-i18n-title="collapse_menu" data-i18n-aria="collapse_menu" title="折叠 / 展开菜单" aria-label="折叠或展开侧边栏菜单"><span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 6l-6 6 6 6"/></svg></span><span class="lbl t" data-i18n="collapse_menu">收起菜单</span></button>
  </div>
</aside>
<div class="scrim" id="scrim" onclick="closeDrawer()"></div>
<div class="main">
<header class="topbar">
 <button class="hamburger" onclick="openDrawer()" data-i18n-title="menu" data-i18n-aria="menu_open" title="菜单" aria-label="打开导航菜单"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 6h16M4 12h16M4 18h16"/></svg></button>
  <h1 id="pageTitle" data-i18n="nav_overview">总览</h1>
 <div class="topbar-spacer spacer"></div>
  <div class="actions">
   <button class="iconlink iconbtn" onclick="refreshAll()" data-i18n-title="refresh_all" data-i18n-aria="refresh_all" title="全局刷新" aria-label="全局刷新"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 12a9 9 0 1 1-2.6-6.4M21 3v6h-6"/></svg></button>
   <a class="iconlink iconbtn" href="https://github.com/babutree/GeoProxy" target="_blank" rel="noopener" data-i18n-title="github" data-i18n-aria="github" title="GitHub 仓库" aria-label="GitHub 仓库"><svg viewBox="0 0 24 24" aria-hidden="true" fill="currentColor"><path d="M12 1.5A10.5 10.5 0 0 0 1.5 12c0 4.64 3 8.57 7.18 9.96.53.1.72-.23.72-.51 0-.25-.01-1.08-.01-1.96-2.63.48-3.32-.64-3.53-1.23-.12-.3-.63-1.23-1.08-1.48-.37-.2-.9-.68-.01-.69.83-.01 1.42.76 1.62 1.08.95 1.6 2.47 1.15 3.07.88.1-.68.37-1.15.67-1.42-2.33-.26-4.77-1.16-4.77-5.18 0-1.14.41-2.08 1.08-2.81-.11-.27-.47-1.35.1-2.81 0 0 .88-.28 2.88 1.07a9.7 9.7 0 0 1 2.62-.35c.89 0 1.79.12 2.62.35 2-1.35 2.88-1.07 2.88-1.07.57 1.46.21 2.54.1 2.81.67.73 1.08 1.66 1.08 2.81 0 4.03-2.45 4.92-4.79 5.18.38.33.71.96.71 1.94 0 1.4-.01 2.53-.01 2.88 0 .28.19.62.72.51A10.5 10.5 0 0 0 22.5 12 10.5 10.5 0 0 0 12 1.5z"/></svg></a>
   <button class="langbtn iconlink" id="lang-toggle" type="button" onclick="toggleLang()" title="Switch language / 切换语言" aria-label="Switch language"><span class="lang-code" id="lang-code">EN</span></button>
   <button class="iconlink iconbtn" id="theme-toggle" onclick="toggleTheme()" data-i18n-title="theme" data-i18n-aria="theme" title="切换主题" aria-label="切换主题"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/></svg></button>
   <button class="iconlink iconbtn" onclick="logout()" data-i18n-title="logout" data-i18n-aria="logout" title="退出登录" aria-label="退出登录"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9"/></svg></button>
  </div>
</header>
<main class="wrap content">

 <!-- 总览 -->
 <section class="page active" id="page-overview">
   <div class="metrics">
    <div class="metric"><div class="label k" data-i18n="metric_upstreams">上游节点</div><div class="value v num" id="stat-total">--</div><div class="note n" data-i18n="metric_upstreams_note">可用 / 降级</div></div>
     <div class="metric"><div class="label k" data-i18n="metric_http">HTTP 可用</div><div class="value v num" id="stat-http">--</div><div class="note n" data-i18n="metric_mixed_note">含 mixed 双入口</div></div>
     <div class="metric"><div class="label k" data-i18n="metric_socks5">SOCKS5 可用</div><div class="value v num" id="stat-socks5">--</div><div class="note n" data-i18n="metric_mixed_note">含 mixed 双入口</div></div>
    <div class="metric"><div class="label k" data-i18n="metric_subs">订阅可用</div><div class="value v num" id="stat-subscription">--</div><div class="note n" data-i18n="metric_subs_note">订阅来源</div></div>
    <div class="metric"><div class="label k" data-i18n="metric_sessions">活跃会话</div><div class="value v num" id="stat-sessions">--</div><div class="note n" data-i18n="metric_sessions_note">当前绑定</div></div>
   </div>

  <div class="grid overview-grid">
   <div class="card orbit-card">
    <div class="card-head card-h">
      <h3 data-i18n="card_orbit">节点分布</h3>
      <div class="tools"><button class="mini tbtn" type="button" id="orbit-pause-btn" onclick="toggleOrbitPause()" data-i18n="orbit_pause">暂停动画</button></div>
    </div>
    <div class="card-body card-b">
      <div class="orbit-stage stage" id="orbit-stage" role="img" data-i18n-aria="card_orbit" aria-label="节点分布">
      <svg class="orbit-svg" id="orbit-svg"></svg>
      <div class="orbit-sun sun" id="orbit-sun">
       <div class="orbit-sun-ring sun-ring"></div><div class="orbit-sun-halo sun-halo"></div>
        <div class="orbit-sun-lbl lbl"><div class="t" data-i18n="orbit_gateway">网关</div><div class="ip num" id="orbit-gw-ip">--</div></div>
      </div>
      <div class="orbit-sats layer" id="orbit-sats"></div>
     </div>
     <div class="orbit-legend legend">
      <div class="orbit-legend-row legend-row">
       <b><span class="qd s"></span>S ≤200ms</b>
       <b><span class="qd a"></span>A ≤500ms</b>
       <b><span class="qd b"></span>B ≤1000ms</b>
       <b><span class="qd c"></span>C ≤2000ms</b>
        <b><span class="beam-swatch"></span><span data-i18n="orbit_session_beam">会话连线（越粗绑定越多）</span></b>
      </div>
     </div>
    </div>
   </div>
    <div class="overview-side">
     <div class="card">
       <div class="card-head card-h"><h3 data-i18n="card_singbox">sing-box 引擎</h3><div class="tools"><button class="mini" onclick="runAsync(t('err_status_refresh'),loadCustomStatus)" data-i18n="btn_refresh">刷新</button></div></div>
       <div class="card-body card-b"><div id="singbox-status"><div class="empty" data-i18n="loading">加载中</div></div></div>
      </div>
      <div class="card">
       <div class="card-head card-h"><h3 data-i18n="card_connect">如何连接</h3><span class="muted sub" data-i18n="card_connect_sub">网关端口 + 认证</span></div>
      <div class="card-body card-b">
        <div class="conn">
         <div class="conn-item"><div class="k" data-i18n="conn_socks5">SOCKS5 代理</div><div class="v" id="conn-socks5">127.0.0.1:7801</div><div class="desc" data-i18n="conn_socks5_desc">raw TCP，HTTP/HTTPS 目标都可</div></div>
         <div class="conn-item"><div class="k" data-i18n="conn_http">HTTP 代理</div><div class="v" id="conn-http">127.0.0.1:7802</div><div class="desc" data-i18n="conn_http_desc">HTTPS 目标走 CONNECT 隧道</div></div>
         <div class="conn-item"><div class="k" data-i18n="conn_user">用户名（含路由 DSL）</div><div class="v" id="conn-user">username</div><div class="desc" data-i18n="conn_user_desc">见下方 DSL 规则</div></div>
         <div class="conn-item"><div class="k" data-i18n="conn_password">密码</div><div class="v" id="conn-pass">见首次启动日志 / 系统设置</div><div class="desc" id="conn-auth-state" data-i18n="conn_auth_state">代理认证状态</div></div>
        </div>
        <div class="cmd" id="conn-cmd">curl --socks5 username:PASSWORD@127.0.0.1:7801 https://www.gstatic.com/generate_204</div>
        <div class="hint" id="dsl-hint" data-i18n="dsl_hint_static">路由 DSL 说明</div>
        <!-- DSL contract: <base>[-region-<cc>][-unlock-<token>][-node-<host:port|key-<base64url(nodeKey)>>][-session-<id>]; stable configuration identity; compatibility entrance address; not the final exit IP; no fallback; node pin determines routing. -->
        <div id="dsl-examples" hidden></div>
        <div class="notice"><span>⚠️</span><span data-i18n-html="conn_exit_notice"><b>「出口 IP」不是连接地址</b>，须走网关端口 + 认证。</span></div>
      </div>
     </div>
    </div>
   </div>

   <div class="card" style="margin-top:16px">
     <div class="card-head card-h"><h3 data-i18n="card_sessions">活跃会话</h3><span class="muted sub" id="ov-sess-count">--</span><div class="tools"><button class="mini" type="button" onclick="switchTab('sessions')" data-i18n="btn_view_all">查看全部</button></div></div>
     <div class="card-body card-b" style="padding:6px 8px;overflow-x:auto">
      <table class="tbl"><thead><tr><th data-i18n="th_session_id">会话 ID</th><th data-i18n="th_route">路由标签</th><th data-i18n="th_exit_region">出口地域</th><th data-i18n="th_exit_node">出口节点</th><th data-i18n="th_quality">品质</th><th data-i18n="th_latency">延迟</th><th data-i18n="th_ttl">剩余 TTL</th></tr></thead>
      <tbody id="ov-session-rows"><tr><td colspan="7" class="empty" data-i18n="loading">加载中</td></tr></tbody></table>
     </div>
    </div>

    <div class="card" style="margin-top:16px">
     <div class="card-head card-h"><h3 data-i18n="card_regions">地域分布</h3><span class="muted sub" id="region-total">--</span><div class="tools"><button class="mini" type="button" onclick="switchTab('regions')" data-i18n="btn_view_all">查看全部</button></div></div>
     <div class="card-body card-b"><div id="region-list" class="region-panel"><div class="empty" data-i18n="loading">加载中</div></div></div>
    </div>
   </section>

   <!-- 地域（全量） -->
   <section class="page" id="page-regions">
    <div class="card">
     <div class="card-head card-h"><h3 data-i18n="card_all_regions">全部地域</h3><span class="muted sub" id="region-page-total">--</span></div>
     <div class="card-body card-b"><div id="region-page-list" class="region-panel"><div class="empty" data-i18n="loading">加载中</div></div></div>
    </div>
   </section>

   <!-- 节点 -->
   <section class="page" id="page-nodes">
   <div class="card">
    <div class="card-head card-h">
     <h3 data-i18n="card_nodes">节点清单</h3>
     <div class="tools">
      <button class="mini" onclick="document.getElementById('import-modal').classList.add('show')" data-i18n="btn_batch_import">批量导入</button>
      <button class="mini danger" onclick="batchDeleteSelected()" data-i18n="btn_batch_delete">批量删除</button>
     </div>
    </div>
   <div class="card-body card-b">
    <div class="toolbar filters" id="node-filter-toolbar">
     <select class="input sm" id="protocol-filter" onchange="renderProxies()" data-i18n-aria="filter_protocol" aria-label="协议"><option value="" data-i18n="filter_all_protocol">全部协议</option><option value="http">HTTP</option><option value="socks5">SOCKS5</option></select>
     <select class="input sm" id="region-filter" onchange="renderProxies()" data-i18n-aria="filter_region" aria-label="地域"><option value="" data-i18n="filter_all_region">全部地域</option></select>
      <select class="input sm" id="status-filter" onchange="renderProxies()" data-i18n-aria="filter_status" aria-label="状态"><option value="" data-i18n="filter_all_status">全部状态</option><option value="ok" data-i18n="status_ok">可用</option><option value="paused" data-i18n="status_paused">已停用</option><option value="sub_paused" data-i18n="status_sub_paused">订阅已暂停</option><option value="failed" data-i18n="status_failed">不可用</option><option value="pending" data-i18n="status_pending">待验证</option></select>
      <select class="input sm" id="source-filter" onchange="renderProxies()" data-i18n-aria="filter_source" aria-label="来源"><option value="" data-i18n="filter_all_source">全部来源</option><option value="manual" data-i18n="source_manual">手工</option><option value="subscription" data-i18n="source_sub">订阅</option></select>
      <select class="input sm" id="quality-filter" onchange="renderProxies()" data-i18n-aria="filter_quality" aria-label="延迟档"><option value="" data-i18n="filter_all_quality">全部延迟档</option><option value="S">S</option><option value="A">A</option><option value="B">B</option><option value="C">C</option><option value="D">D</option></select>
       <select class="input sm" id="purity-filter" onchange="renderProxies()" data-i18n-aria="filter_purity" aria-label="纯净度" data-i18n-title="filter_purity_title" title="按 ipapi.is abuse score 过滤"><option value="" data-i18n="filter_all_purity">全部纯净度</option><option value="clean" data-i18n="filter_purity_clean">低风险 &lt;0.10</option><option value="caution" data-i18n="filter_purity_caution">中风险 0.10-0.50</option><option value="risky" data-i18n="filter_purity_risky">高风险 &gt;0.50</option><option value="unprobed" data-i18n="filter_unprobed">未探测</option></select>
       <span class="sep" aria-hidden="true" style="width:1px;height:22px;background:var(--line);flex:0 0 auto"></span>
      <select class="hidden-select" id="cf-filter"><option value="" data-i18n="filter_cf_all">全部 Cloudflare</option><option value="unlocked" data-i18n="filter_cf_ok">Cloudflare 畅通</option><option value="blocked" data-i18n="filter_cf_bad">Cloudflare 阻断</option><option value="unknown" data-i18n="filter_cf_unknown">Cloudflare 未知</option></select><button type="button" class="filter-toggle" id="cf-toggle" data-sel="cf-filter" onclick="cycleFilter('cf-filter','cf-toggle')" aria-pressed="false" data-i18n-title="filter_cf_title" title="Cloudflare：全部/畅通/阻断/未知"><span class="tx">Cloudflare</span><span class="st" data-i18n="filter_all">全部</span></button>
      <select class="hidden-select" id="ai-openai-filter"><option value="" data-i18n="filter_gpt_all">ChatGPT 全部</option><option value="unlocked" data-i18n="filter_gpt_ok">ChatGPT 畅通</option><option value="blocked" data-i18n="filter_gpt_bad">ChatGPT 阻断</option><option value="unprobed" data-i18n="filter_gpt_unprobed">ChatGPT 未探测</option></select><button type="button" class="filter-toggle" id="ai-openai-toggle" data-sel="ai-openai-filter" onclick="cycleFilter('ai-openai-filter','ai-openai-toggle')" aria-pressed="false" data-i18n-title="filter_gpt_title" title="ChatGPT：全部/畅通/阻断/未探测"><span class="tx">ChatGPT</span><span class="st" data-i18n="filter_all">全部</span></button>
      <select class="hidden-select" id="ai-claude-filter"><option value="" data-i18n="filter_claude_all">Claude 全部</option><option value="unlocked" data-i18n="filter_claude_ok">Claude 畅通</option><option value="blocked" data-i18n="filter_claude_bad">Claude 阻断</option><option value="unprobed" data-i18n="filter_claude_unprobed">Claude 未探测</option></select><button type="button" class="filter-toggle" id="ai-claude-toggle" data-sel="ai-claude-filter" onclick="cycleFilter('ai-claude-filter','ai-claude-toggle')" aria-pressed="false" data-i18n-title="filter_claude_title" title="Claude：全部/畅通/阻断/未探测"><span class="tx">Claude</span><span class="st" data-i18n="filter_all">全部</span></button>
      <select class="hidden-select" id="ai-gemini-filter"><option value="" data-i18n="filter_gemini_all">Gemini 全部</option><option value="unlocked" data-i18n="filter_gemini_ok">Gemini 畅通</option><option value="blocked" data-i18n="filter_gemini_bad">Gemini 阻断</option><option value="unprobed" data-i18n="filter_gemini_unprobed">Gemini 未探测</option></select><button type="button" class="filter-toggle" id="ai-gemini-toggle" data-sel="ai-gemini-filter" onclick="cycleFilter('ai-gemini-filter','ai-gemini-toggle')" aria-pressed="false" data-i18n-title="filter_gemini_title" title="Gemini：全部/畅通/阻断/未探测"><span class="tx">Gemini</span><span class="st" data-i18n="filter_all">全部</span></button>
      <select class="hidden-select" id="ai-grok-filter"><option value="" data-i18n="filter_grok_all">Grok 全部</option><option value="unlocked" data-i18n="filter_grok_ok">Grok 畅通</option><option value="blocked" data-i18n="filter_grok_bad">Grok 阻断</option><option value="unprobed" data-i18n="filter_grok_unprobed">Grok 未探测</option></select><button type="button" class="filter-toggle" id="ai-grok-toggle" data-sel="ai-grok-filter" onclick="cycleFilter('ai-grok-filter','ai-grok-toggle')" aria-pressed="false" data-i18n-title="filter_grok_title" title="Grok：全部/畅通/阻断/未探测"><span class="tx">Grok</span><span class="st" data-i18n="filter_all">全部</span></button>
    </div>
     <div class="toolbar filters search-row">
     <input class="input narrow" id="latency-min" type="number" min="0" step="1" data-i18n-placeholder="filter_latency_min" placeholder="延迟≥ms" oninput="renderProxies()" aria-label="最小延迟">
     <input class="input narrow" id="latency-max" type="number" min="0" step="1" data-i18n-placeholder="filter_latency_max" placeholder="延迟≤ms" oninput="renderProxies()" aria-label="最大延迟">
     <input class="input grow" id="keyword-filter" type="search" data-i18n-placeholder="filter_keyword" placeholder="搜索地址 / 备注 / 出口 IP" oninput="renderProxies()" aria-label="搜索地址、备注或出口 IP">
    </div>
    <div class="toolbar">
      <input class="input grow" id="manual-link" data-i18n-placeholder="filter_manual_link" placeholder="添加手工节点: http://host:port 或 socks5://host:port" aria-label="手工节点链接">
      <input class="input narrow" id="manual-region" maxlength="2" data-i18n-placeholder="filter_region" placeholder="地域" aria-label="地域">
      <input class="input mid" id="manual-note" data-i18n-placeholder="filter_note" placeholder="备注" aria-label="备注">
      <button class="mini primary" onclick="addManualNode()" data-i18n="btn_add_node">添加</button>
    </div>
      <div class="hint" style="margin:0 0 10px" data-i18n="filter_ai_hint">AI 解锁：ChatGPT / Claude / Grok / Gemini（绿色畅通 / 红色阻断 / 淡灰未探测）</div>
     <div class="table-wrap">
      <table class="tbl">
        <thead><tr><th><input type="checkbox" id="proxy-select-all" onchange="toggleSelectAll(this.checked)" data-i18n-aria="select_all" aria-label="全选"></th><th>★</th><th data-i18n="th_name_note">名称 / 备注</th><th data-i18n="th_protocol">协议</th><th data-i18n="th_region">地域</th><th data-i18n-html="th_exit_ip">出口 IP<span class="muted"> (信息)</span></th><th data-i18n="th_latency">延迟</th><th data-i18n-html="th_abuse">ipapi.is 滥用分<span class="muted"> /1.00</span></th><th data-i18n="th_ipapi_flags">ip-api 标记</th><th><span class="th-ico" data-i18n-title="th_cf_title" title="Cloudflare：畅通 / 阻断 / 未知"><svg viewBox="0 0 24 24" aria-hidden="true" fill="currentColor"><path d="M17.5 15.5c.3 0 .5-.2.5-.5 0-2-1.6-3.6-3.6-3.6-.3 0-.6 0-.9.1A4 4 0 0 0 6 12.5c-1.4.1-2.5 1.3-2.5 2.7 0 .2.2.3.4.3h13.6z"/></svg><span class="tx">CF</span></span></th><th><span class="th-ico" data-i18n-title="filter_ai_hint" title="AI 解锁：ChatGPT / Claude / Grok / Gemini（绿色畅通 / 红色阻断 / 淡灰未探测）"><svg viewBox="0 0 24 24" aria-hidden="true" fill="currentColor"><path d="M12 2l1.9 5.1L19 9l-5.1 1.9L12 16l-1.9-5.1L5 9l5.1-1.9z"/></svg><span class="tx" data-i18n="th_ai">AI 解锁</span></span></th><th data-i18n="th_source">来源</th><th data-i18n="th_status">状态</th><th data-i18n="th_actions">操作</th></tr></thead>
        <tbody id="proxy-rows"><tr><td colspan="14" class="empty" data-i18n="loading">加载中</td></tr></tbody>
       </table>
      </div>
      <div class="pager" id="proxy-pager" data-i18n-aria="pager_nodes" aria-label="节点列表分页">
       <span class="muted" id="proxy-page-info">--</span>
       <div class="pager-actions">
        <button type="button" class="mini" id="proxy-page-prev" onclick="proxyPagePrev()" disabled data-i18n="pager_prev">上一页</button>
        <span class="muted" id="proxy-page-num">1 / 1</span>
        <button type="button" class="mini" id="proxy-page-next" onclick="proxyPageNext()" disabled data-i18n="pager_next">下一页</button>
        <label class="muted" for="proxy-page-size" style="margin-left:8px" data-i18n="pager_per_page">每页</label>
        <select class="input sm" id="proxy-page-size" onchange="proxyPageSizeChange()" data-i18n-aria="pager_per_page" aria-label="每页条数">
        <option value="20" selected>20</option>
        <option value="50">50</option>
        <option value="100">100</option>
       </select>
      </div>
     </div>
    </div>
   </div>
  </section>

 <!-- 订阅 -->
  <section class="page" id="page-subs">
   <div class="card">
    <div class="card-head card-h"><h3 data-i18n="card_subs">订阅管理</h3><div class="tools"><button class="mini" onclick="refreshAllSubs()" data-i18n="btn_refresh_all">刷新全部</button><button class="mini primary" onclick="openSubModal()" data-i18n="btn_add_sub">添加订阅</button></div></div>
    <div class="card-body card-b"><div id="sub-list"><div class="empty" data-i18n="loading">加载中</div></div></div>
   </div>
  </section>

  <!-- 会话 -->
  <section class="page" id="page-sessions">
   <div class="card">
    <div class="card-head card-h">
     <h3 data-i18n="card_session_monitor">Session 监控</h3>
     <span class="muted sub" id="sess-count">--</span>
     <div class="tools">
      <button class="mini" type="button" onclick="expandAllSessions(true)" data-i18n="btn_expand_all">全部展开</button>
      <button class="mini" type="button" onclick="expandAllSessions(false)" data-i18n="btn_collapse_all">全部折叠</button>
      <button class="mini" onclick="runAsync(t('err_session_refresh'),loadSessions)" data-i18n="btn_refresh">刷新</button>
     </div>
    </div>
    <div class="card-body card-b">
     <div class="hint" style="margin-bottom:12px" data-i18n="session_hint">仅展示 sticky 绑定：用户名含 <code>-session-&lt;id&gt;</code> 才进入亲和表；无 session 的请求不出现在此列表。</div>
     <div class="session-list session-grid" id="session-rows"><div class="empty" data-i18n="loading">加载中</div></div>
    </div>
   </div>
  </section>

  <!-- 日志 -->
  <section class="page" id="page-logs">
   <div class="card">
    <div class="card-head card-h"><h3 data-i18n="card_logs">运行日志</h3><div class="tools"><label class="check" for="logs-autoscroll"><input type="checkbox" id="logs-autoscroll" checked> <span data-i18n="logs_autoscroll">自动滚动</span></label><button class="mini" onclick="runAsync(t('err_log_refresh'),loadLogs)" data-i18n="btn_refresh">刷新</button></div></div>
    <div class="card-body card-b"><div class="logs" id="logs-box"><div class="log-line" data-i18n="loading">加载中</div></div></div>
   </div>
  </section>

  <!-- API -->
  <section class="page" id="page-api">
   <div class="grid api-grid">
    <div class="card">
     <div class="card-head card-h"><h3 data-i18n="card_openapi">开放 API 说明</h3><span class="muted sub" data-i18n="card_openapi_sub">只读 · API Key 鉴权</span></div>
     <div class="card-body card-b">
      <div class="guide-row"><b data-i18n="api_endpoints">端点</b><span>GET /api/v1/nodes · GET /api/v1/occupancy · GET /api/v1/ping</span></div>
      <div class="guide-row"><b data-i18n="api_auth">鉴权</b><span>Authorization: Bearer &lt;key&gt; 或 X-API-Key: &lt;key&gt;</span></div>
      <div class="guide-row"><b data-i18n="api_rate">限流</b><span data-i18n="api_rate_desc">默认 60/min/key，推荐轮询 5–10 分钟</span></div>
      <div class="guide-row"><b data-i18n="api_connect">连接模式</b><span data-i18n="api_connect_desc">direct=直连节点地址；gateway=加密节点走网关，不下发 127.0.0.1</span></div>
      <pre class="code-block" id="openapi-curl-nodes">curl -H "Authorization: Bearer YOUR_API_KEY" http://HOST:WEBUI/api/v1/nodes</pre>
      <pre class="code-block" id="openapi-curl-occupancy">curl -H "X-API-Key: YOUR_API_KEY" http://HOST:WEBUI/api/v1/occupancy</pre>
      <div class="notice"><span>⚠️</span><span data-i18n="api_notice">密钥仅存 SHA-256 hash；直连节点客户端可直连，加密节点必须走网关。</span></div>
     </div>
    </div>
    <div class="card">
     <div class="card-head card-h">
      <h3 data-i18n="card_apikeys">API Key 管理</h3>
      <div class="tools">
       <input class="input mid" id="apikey-name" data-i18n-placeholder="ph_apikey_name" placeholder="新 Key 名称">
       <button class="mini primary" onclick="createAPIKey()" data-i18n="btn_create">创建</button>
       <button class="mini" onclick="runAsync(t('err_apikey_refresh'),loadAPIKeys)" data-i18n="btn_refresh">刷新</button>
      </div>
     </div>
     <div class="card-body card-b" style="padding:6px 8px;overflow-x:auto">
      <div class="apikey-section" id="apikey-section" style="margin:0;border:0;padding:0">
       <table class="tbl data"><thead><tr><th data-i18n="th_name">名称</th><th data-i18n="th_created">创建时间</th><th data-i18n="th_last_used">末次使用</th><th data-i18n="th_status">状态</th><th data-i18n="th_actions">操作</th></tr></thead>
       <tbody id="apikey-rows"><tr><td colspan="5" class="empty" data-i18n="loading">加载中</td></tr></tbody></table>
      </div>
     </div>
    </div>
   </div>
  </section>

  <!-- 设置（独立页，非模态） -->
  <section class="page" id="page-settings">
   <div class="card" id="settings-modal">
    <div class="card-head card-h"><h3 data-i18n="card_settings">系统设置</h3><span class="muted sub" data-i18n="card_settings_sub">独立页面</span>
     <div class="tools"><button class="mini primary" onclick="saveConfig()" data-i18n="btn_save">保存</button></div>
    </div>
    <div class="card-body card-b">
     <div class="form-grid">
      <div class="field"><label data-i18n="cfg_http_port">HTTP 端口</label><input id="cfg-http-port" readonly><span class="fh" data-i18n="cfg_readonly_redeploy">只读，改端口需重新部署</span></div>
      <div class="field"><label data-i18n="cfg_socks_port">SOCKS5 端口</label><input id="cfg-socks5-port" readonly><span class="fh" data-i18n="cfg_readonly">只读</span></div>
      <div class="field"><label data-i18n="cfg_webui_port">WebUI 端口</label><input id="cfg-webui-port" readonly><span class="fh" data-i18n="cfg_readonly">只读</span></div>
      <div class="field"><label data-i18n="cfg_auth">代理认证</label><select id="cfg-auth-enabled"><option value="false" data-i18n="opt_off">关闭</option><option value="true" data-i18n="opt_on">开启</option></select></div>
      <div class="field"><label data-i18n="cfg_auth_user">代理认证用户名</label><input id="cfg-auth-username"></div>
      <div class="field"><label data-i18n="cfg_auth_pass">代理认证密码（留空不改）</label><input id="cfg-auth-password" type="password"></div>
      <div class="field"><label data-i18n="cfg_ttl">Session TTL（分钟）</label><input id="cfg-session-ttl" type="number" min="1"></div>
      <div class="field"><label data-i18n="cfg_default_region">默认地域</label><input id="cfg-default-region" maxlength="2" data-i18n-placeholder="ph_region_global" placeholder="空=全局"></div>
      <div class="field"><label data-i18n="cfg_health">健康检查间隔（分钟）</label><input id="cfg-health-interval" type="number" min="1"></div>
      <div class="field"><label data-i18n="cfg_retry">最大重试次数</label><input id="cfg-max-retry" type="number" min="0"></div>
      <div class="field full"><label data-i18n="cfg_singbox">sing-box 路径</label><input id="cfg-singbox-path"></div>
      <div class="field full"><label data-i18n="cfg_allowed">允许国家（逗号分隔，优先）</label><input id="cfg-allowed-countries" placeholder="US,JP,SG"></div>
      <div class="field full"><label data-i18n="cfg_blocked">屏蔽国家（逗号分隔）</label><input id="cfg-blocked-countries" placeholder="CN,RU"></div>
     </div>
     <div class="notice"><span>ℹ️</span><span data-i18n="cfg_notice">保存失败时运行态会回滚，前端会明确报错，不会静默降级。</span></div>
    </div>
   </div>
  </section>
</main>
</div>
</div>

<div class="modal" id="import-modal"><div class="dialog"><h3 data-i18n="modal_import_title">批量导入手工节点</h3><div class="form-grid"><div class="field full"><label data-i18n="modal_import_list">代理列表（每行一条 socks5/http/https URL，支持前缀/行内/行尾说明）</label><textarea id="import-text" rows="12" data-i18n-placeholder="modal_import_list_ph" placeholder="prefix socks5://1.2.3.4:1080 suffix&#10;http://5.6.7.8:8080 note"></textarea></div><div class="field"><label data-i18n="filter_region">地域</label><input id="import-region" maxlength="2" data-i18n-placeholder="ph_optional" placeholder="可选"></div><div class="field"><label data-i18n="filter_note">备注</label><input id="import-note" data-i18n-placeholder="ph_optional" placeholder="可选"></div></div><div class="dialog-actions"><button class="btn" onclick="document.getElementById('import-modal').classList.remove('show')" data-i18n="btn_cancel">取消</button><button class="btn primary" onclick="importManualNodes()" data-i18n="btn_import">导入</button></div></div></div>
<div class="modal" id="node-modal"><div class="dialog"><h3 data-i18n="modal_node_title">节点管理</h3><input type="hidden" id="node-modal-id" value=""><div class="muted mono" id="node-modal-addr" style="margin:-6px 0 12px;font-size:12px;word-break:break-all"></div><div class="form-grid"><div class="field"><label data-i18n="filter_region">地域</label><input id="node-modal-region" maxlength="2" data-i18n-placeholder="ph_region_eg" placeholder="如 us / jp"></div><div class="field full"><label data-i18n="filter_note">备注</label><input id="node-modal-note" data-i18n-placeholder="ph_note_optional" placeholder="可选备注"></div></div><div class="dialog-actions" style="justify-content:space-between"><button class="btn danger" onclick="nodeModalDelete()" data-i18n="btn_delete_node">删除节点</button><span><button class="btn" onclick="closeNodeModal()" data-i18n="btn_cancel">取消</button><button class="btn primary" onclick="nodeModalSave()" data-i18n="btn_save">保存</button></span></div></div></div>
<div class="modal" id="confirm-modal"><div class="dialog"><h3 id="confirm-modal-title" data-i18n="modal_confirm_title">确认</h3><p class="muted" id="confirm-modal-msg" style="word-break:break-all"></p><div class="dialog-actions"><button class="btn" id="confirm-modal-cancel" data-i18n="btn_cancel">取消</button><button class="btn primary" id="confirm-modal-ok" data-i18n="btn_ok">确定</button></div></div></div>
<div class="modal" id="protocol-pick-modal"><div class="dialog"><h3 data-i18n="modal_proto_title">选择复制协议</h3><p class="muted" style="margin:0 0 14px;line-height:1.55" data-i18n="modal_proto_desc">该节点为 mixed 双入口，可同时提供 SOCKS5 与 HTTP。请选择要复制的连接协议：</p><div class="dialog-actions" style="justify-content:flex-end;flex-wrap:wrap;gap:8px"><button class="btn" type="button" id="protocol-pick-cancel" data-i18n="btn_cancel">取消</button><button class="btn" type="button" id="protocol-pick-http" data-i18n="btn_copy_http">复制 HTTP</button><button class="btn primary" type="button" id="protocol-pick-socks" data-i18n="btn_copy_socks">复制 SOCKS5</button></div></div></div>
<div class="modal" id="sub-modal"><div class="dialog"><h3 id="sub-modal-title" data-i18n="modal_sub_add">添加订阅</h3><input type="hidden" id="sub-edit-id" value=""><div class="form-grid"><div class="field"><label data-i18n="th_name">名称</label><input id="sub-name" placeholder="primary subscription"></div><div class="field"><label data-i18n="modal_sub_refresh">刷新间隔（分钟）</label><input id="sub-refresh" type="number" value="60" min="10" step="10"></div><div class="field full"><label data-i18n="modal_sub_url">订阅 URL</label><input id="sub-url" placeholder="https://example.com/sub"></div><div class="field full" id="sub-file-field"><label data-i18n="modal_sub_file">或粘贴配置文件内容</label><textarea id="sub-file-content" placeholder="Clash YAML / V2ray / Base64 / plain text"></textarea></div><div class="field full"><label data-i18n="modal_sub_headers">自定义请求头（可选，JSON）</label><textarea id="sub-headers" placeholder="{&#34;User-Agent&#34;:&#34;clash&#34;}"></textarea></div></div><div class="dialog-actions"><button class="btn" onclick="closeSubModal()" data-i18n="btn_cancel">取消</button><button class="btn primary" id="sub-modal-submit" onclick="submitSubscription()" data-i18n="btn_add">添加</button></div></div></div>
<div class="modal" id="apikey-once-modal"><div class="dialog"><h3 data-i18n="modal_apikey_created">API Key 已创建</h3><p class="muted" data-i18n="modal_apikey_once_hint">明文仅显示一次，请立即复制保存。关闭后无法再次查看。</p><div class="field full"><label data-i18n="th_name">名称</label><input id="apikey-once-name" readonly></div><div class="field full"><label data-i18n="modal_apikey_once_label">Key（仅显示一次）</label><input id="apikey-once-key" readonly style="font-family:monospace"></div><div class="dialog-actions"><button class="btn" onclick="navigator.clipboard.writeText(document.getElementById('apikey-once-key').value).then(()=>showToast(t('toast_copied'))).catch(()=>showToast(t('toast_copy_fail')))" data-i18n="btn_copy">复制</button><button class="btn primary" onclick="document.getElementById('apikey-once-modal').classList.remove('show')" data-i18n="btn_close">关闭</button></div></div></div>
<div class="toast" id="toast"></div>
<script src="/assets/dashboard.js"></script>
</body>
</html>`
