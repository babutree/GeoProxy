package webui

const loginHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Admin Login</title>
<style>
:root{--bg:#f7f8fb;--panel:#fff;--ink:#172033;--muted:#6d7688;--line:#e6e9ef;--accent:#2557d6;--shadow:0 24px 80px rgba(32,45,73,.12)}
*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;background:radial-gradient(circle at 20% 10%,#e8efff 0,transparent 32%),linear-gradient(145deg,#fff 0%,#f6f8fc 54%,#eef3fa 100%);color:var(--ink);font-family:"Segoe UI",Verdana,sans-serif;padding:24px}.card{width:min(440px,100%);background:var(--panel);border:1px solid var(--line);border-radius:30px;box-shadow:var(--shadow);padding:34px}.mark{width:52px;height:52px;border-radius:18px;background:var(--ink);color:#fff;display:grid;place-items:center;font-weight:800;letter-spacing:.06em;margin-bottom:24px}.eyebrow{font-size:11px;letter-spacing:.18em;text-transform:uppercase;color:var(--muted);font-weight:800}.title{font-size:34px;line-height:1;margin:8px 0 10px;letter-spacing:-.04em}.copy{color:var(--muted);margin:0 0 28px;line-height:1.6}.field{display:grid;gap:8px;margin-top:18px}.field label{font-size:12px;color:var(--muted);font-weight:800;letter-spacing:.1em;text-transform:uppercase}input{width:100%;border:1px solid var(--line);border-radius:16px;padding:14px 15px;font-size:16px;outline:none;background:#fff;color:var(--ink)}input:focus{border-color:var(--accent);box-shadow:0 0 0 4px rgba(37,87,214,.10)}button{width:100%;border:0;border-radius:999px;background:var(--accent);color:#fff;padding:14px 18px;margin-top:22px;font-weight:800;cursor:pointer;box-shadow:0 14px 34px rgba(37,87,214,.22)}button:hover{filter:brightness(.96)}.error{display:none;margin:0 0 18px;padding:12px 14px;border-radius:14px;background:#fff1f0;color:#b42318;border:1px solid #ffd6d1;font-weight:700}.error.show{display:block}.foot{margin-top:22px;color:var(--muted);font-size:12px;text-align:center}
</style>
</head>
<body>
<main class="card">
  <div class="mark">A</div>
  <div class="eyebrow">Private Admin Surface</div>
  <h1 class="title">Admin Sign In</h1>
  <p class="copy">请先完成管理员验证。未登录状态只提供身份验证入口。</p>
  <div class="error">密码错误，请重试。</div>
  <form method="POST" action="/login">
    <div class="field"><label>管理密码</label><input type="password" name="password" autocomplete="current-password" autofocus required></div>
    <button type="submit">进入管理台</button>
  </form>
  <div class="foot">Administrative access is restricted.</div>
</main>
</body>
</html>`

const loginHTMLWithError = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Admin Login</title>
<style>
:root{--bg:#f7f8fb;--panel:#fff;--ink:#172033;--muted:#6d7688;--line:#e6e9ef;--accent:#2557d6;--shadow:0 24px 80px rgba(32,45,73,.12)}
*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;background:radial-gradient(circle at 20% 10%,#e8efff 0,transparent 32%),linear-gradient(145deg,#fff 0%,#f6f8fc 54%,#eef3fa 100%);color:var(--ink);font-family:"Segoe UI",Verdana,sans-serif;padding:24px}.card{width:min(440px,100%);background:var(--panel);border:1px solid var(--line);border-radius:30px;box-shadow:var(--shadow);padding:34px}.mark{width:52px;height:52px;border-radius:18px;background:var(--ink);color:#fff;display:grid;place-items:center;font-weight:800;letter-spacing:.06em;margin-bottom:24px}.eyebrow{font-size:11px;letter-spacing:.18em;text-transform:uppercase;color:var(--muted);font-weight:800}.title{font-size:34px;line-height:1;margin:8px 0 10px;letter-spacing:-.04em}.copy{color:var(--muted);margin:0 0 28px;line-height:1.6}.field{display:grid;gap:8px;margin-top:18px}.field label{font-size:12px;color:var(--muted);font-weight:800;letter-spacing:.1em;text-transform:uppercase}input{width:100%;border:1px solid var(--line);border-radius:16px;padding:14px 15px;font-size:16px;outline:none;background:#fff;color:var(--ink)}input:focus{border-color:var(--accent);box-shadow:0 0 0 4px rgba(37,87,214,.10)}button{width:100%;border:0;border-radius:999px;background:var(--accent);color:#fff;padding:14px 18px;margin-top:22px;font-weight:800;cursor:pointer;box-shadow:0 14px 34px rgba(37,87,214,.22)}button:hover{filter:brightness(.96)}.error{margin:0 0 18px;padding:12px 14px;border-radius:14px;background:#fff1f0;color:#b42318;border:1px solid #ffd6d1;font-weight:700}.foot{margin-top:22px;color:var(--muted);font-size:12px;text-align:center}
</style>
</head>
<body>
<main class="card">
  <div class="mark">A</div>
  <div class="eyebrow">Private Admin Surface</div>
  <h1 class="title">Admin Sign In</h1>
  <p class="copy">请先完成管理员验证。未登录状态只提供身份验证入口。</p>
  <div class="error">密码错误，请重试。</div>
  <form method="POST" action="/login">
    <div class="field"><label>管理密码</label><input type="password" name="password" autocomplete="current-password" autofocus required></div>
    <button type="submit">进入管理台</button>
  </form>
  <div class="foot">Administrative access is restricted.</div>
</main>
</body>
</html>`
