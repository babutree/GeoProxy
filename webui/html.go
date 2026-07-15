package webui

// html.go：登录页，与主控台共用设计语言。
// 契约保留：<form method="POST" action="/login">、<input type="password" name="password">、
// 错误态元素 .error；loginHTMLWithError 中 .error 带 .show 常显，loginHTML 中不带 show。

const loginHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>GeoProxy Gateway 登录</title>
<style>
:root{--bg:#f4f6fb;--panel:#fff;--ink:#18202f;--muted:#6b7488;--line:#e4e8f0;--soft:#eef2fb;--accent:#2f5bea;--accent-ink:#fff;--danger:#d64545;--shadow:0 8px 30px rgba(24,38,68,.10);--radius:16px;--ease:cubic-bezier(0.16,1,0.3,1)}@media (prefers-color-scheme:dark){:root{--bg:#0d1320;--panel:#151d2e;--ink:#e7ecf5;--muted:#8b95ab;--line:#243046;--soft:#1a2438;--accent:#5b83ff;--danger:#f0685f;--shadow:0 8px 30px rgba(0,0,0,.36)}}*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;background:var(--bg);color:var(--ink);font-family:"Segoe UI","PingFang SC","Microsoft YaHei",Verdana,sans-serif;font-size:14px;line-height:1.55;padding:24px}.card{width:min(400px,100%);background:var(--panel);border:1px solid var(--line);border-radius:var(--radius);box-shadow:var(--shadow);padding:32px}.brand{display:flex;align-items:center;gap:12px;margin-bottom:24px}.mark{width:40px;height:40px;border-radius:10px;background:var(--accent);color:var(--accent-ink);display:grid;place-items:center;font-weight:800;font-size:15px}.brand .bt{font-weight:800;font-size:17px;letter-spacing:-.01em}.title{font-size:20px;font-weight:800;margin:0 0 8px;letter-spacing:-.02em}.copy{color:var(--muted);margin:0 0 24px;line-height:1.6}.field{display:flex;flex-direction:column;gap:8px;margin-top:16px}.field label{font-size:12px;color:var(--muted);font-weight:600}input{width:100%;border:1px solid var(--line);border-radius:8px;padding:10px 12px;font-size:14px;background:var(--panel);color:var(--ink);transition:border-color 150ms var(--ease)}input:focus{outline:none;border-color:var(--accent)}:focus-visible{outline:2px solid var(--accent);outline-offset:2px;border-radius:4px}button{width:100%;border:1px solid var(--accent);border-radius:10px;background:var(--accent);color:var(--accent-ink);padding:11px 16px;margin-top:24px;font-weight:700;cursor:pointer;transition:background 150ms var(--ease)}button:hover{filter:brightness(.96)}.error{display:none;margin:0 0 16px;padding:10px 12px;border-radius:8px;background:rgba(214,69,69,.12);color:var(--danger);border:1px solid rgba(214,69,69,.28);font-weight:600;font-size:13px}.error.show{display:block}.foot{margin-top:24px;color:var(--muted);font-size:12px;text-align:center}@media (prefers-reduced-motion:reduce){*{transition-duration:.001ms!important}}
</style>
</head>
<body>
<main class="card">
  <div class="brand"><div class="mark">GG</div><span class="bt">GeoProxy Gateway</span></div>
  <h1 class="title">管理员登录</h1>
  <p class="copy">请输入管理密码进入控制台。未登录状态仅提供身份验证入口。</p>
  <div class="error">密码错误，请重试。</div>
  <form method="POST" action="/login">
    <div class="field"><label for="password">管理密码</label><input id="password" type="password" name="password" autocomplete="current-password" autofocus required></div>
    <button type="submit">进入控制台</button>
  </form>
  <div class="foot">仅限授权管理员访问。</div>
</main>
</body>
</html>`

const loginHTMLWithError = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>GeoProxy Gateway 登录</title>
<style>
:root{--bg:#f4f6fb;--panel:#fff;--ink:#18202f;--muted:#6b7488;--line:#e4e8f0;--soft:#eef2fb;--accent:#2f5bea;--accent-ink:#fff;--danger:#d64545;--shadow:0 8px 30px rgba(24,38,68,.10);--radius:16px;--ease:cubic-bezier(0.16,1,0.3,1)}@media (prefers-color-scheme:dark){:root{--bg:#0d1320;--panel:#151d2e;--ink:#e7ecf5;--muted:#8b95ab;--line:#243046;--soft:#1a2438;--accent:#5b83ff;--danger:#f0685f;--shadow:0 8px 30px rgba(0,0,0,.36)}}*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;background:var(--bg);color:var(--ink);font-family:"Segoe UI","PingFang SC","Microsoft YaHei",Verdana,sans-serif;font-size:14px;line-height:1.55;padding:24px}.card{width:min(400px,100%);background:var(--panel);border:1px solid var(--line);border-radius:var(--radius);box-shadow:var(--shadow);padding:32px}.brand{display:flex;align-items:center;gap:12px;margin-bottom:24px}.mark{width:40px;height:40px;border-radius:10px;background:var(--accent);color:var(--accent-ink);display:grid;place-items:center;font-weight:800;font-size:15px}.brand .bt{font-weight:800;font-size:17px;letter-spacing:-.01em}.title{font-size:20px;font-weight:800;margin:0 0 8px;letter-spacing:-.02em}.copy{color:var(--muted);margin:0 0 24px;line-height:1.6}.field{display:flex;flex-direction:column;gap:8px;margin-top:16px}.field label{font-size:12px;color:var(--muted);font-weight:600}input{width:100%;border:1px solid var(--line);border-radius:8px;padding:10px 12px;font-size:14px;background:var(--panel);color:var(--ink);transition:border-color 150ms var(--ease)}input:focus{outline:none;border-color:var(--accent)}:focus-visible{outline:2px solid var(--accent);outline-offset:2px;border-radius:4px}button{width:100%;border:1px solid var(--accent);border-radius:10px;background:var(--accent);color:var(--accent-ink);padding:11px 16px;margin-top:24px;font-weight:700;cursor:pointer;transition:background 150ms var(--ease)}button:hover{filter:brightness(.96)}.error{display:none;margin:0 0 16px;padding:10px 12px;border-radius:8px;background:rgba(214,69,69,.12);color:var(--danger);border:1px solid rgba(214,69,69,.28);font-weight:600;font-size:13px}.error.show{display:block}.foot{margin-top:24px;color:var(--muted);font-size:12px;text-align:center}@media (prefers-reduced-motion:reduce){*{transition-duration:.001ms!important}}
</style>
</head>
<body>
<main class="card">
  <div class="brand"><div class="mark">GG</div><span class="bt">GeoProxy Gateway</span></div>
  <h1 class="title">管理员登录</h1>
  <p class="copy">请输入管理密码进入控制台。未登录状态仅提供身份验证入口。</p>
  <div class="error show">密码错误，请重试。</div>
  <form method="POST" action="/login">
    <div class="field"><label for="password">管理密码</label><input id="password" type="password" name="password" autocomplete="current-password" autofocus required></div>
    <button type="submit">进入控制台</button>
  </form>
  <div class="foot">仅限授权管理员访问。</div>
</main>
</body>
</html>`
