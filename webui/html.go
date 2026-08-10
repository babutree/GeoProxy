package webui

import "strings"

// 登录页 HTML。契约：POST /login、password 字段、.error（错误页加 .show）。
// 语言与后台共用 localStorage 键 gg-lang（zh|en）。

const loginPageCSS = `:root{--bg:#eef3fb;--panel:#fff;--ink:#0f1b2e;--muted:#5f6f8c;--line:#e2e9f4;--soft:#f4f8ff;--accent:#1d6fe0;--accent-ink:#fff;--danger:#e0485f;--shadow:0 8px 30px rgba(30,60,110,.10);--radius:16px;--ease:cubic-bezier(.16,1,.3,1);--bg-canvas:radial-gradient(120% 120% at 50% 0%,#f2f7ff 0%,#e8f0fb 100%);--glow-a:0 4px 14px rgba(29,111,224,.24)}@media (prefers-color-scheme:dark){:root{--bg:#04060e;--panel:#0e1626;--ink:#eaf1ff;--muted:#8fa0bf;--line:rgba(90,160,255,.16);--soft:#152036;--accent:#3b8dff;--danger:#ff5c7a;--shadow:0 12px 40px rgba(0,0,0,.55);--bg-canvas:radial-gradient(120% 120% at 50% 0%,#0b1226 0%,#070c18 46%,#04060e 100%);--glow-a:0 0 12px rgba(59,141,255,.55)}}*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;background:var(--bg-canvas);color:var(--ink);font-family:"Segoe UI","PingFang SC","Microsoft YaHei",system-ui,sans-serif;font-size:14px;line-height:1.55;padding:24px;-webkit-font-smoothing:antialiased}.card{position:relative;width:min(400px,100%);background:linear-gradient(180deg,var(--soft),var(--panel));border:1px solid var(--line);border-radius:var(--radius);box-shadow:var(--shadow);padding:32px}.langbtn{position:absolute;top:14px;right:14px;width:auto;min-width:42px;margin:0;padding:6px 10px;border:1px solid var(--line);border-radius:999px;background:var(--panel);color:var(--ink);font-size:12px;font-weight:800;letter-spacing:.02em;cursor:pointer;transition:border-color 150ms var(--ease),filter 150ms var(--ease)}.langbtn:hover{filter:brightness(1.03);border-color:color-mix(in srgb,var(--accent) 40%,var(--line))}.brand{display:flex;align-items:center;gap:12px;margin-bottom:24px;padding-right:48px}.mark{width:42px;height:42px;border-radius:12px;background:radial-gradient(circle at 35% 30%,#dbeaff,var(--accent) 60%,#1546a8);color:var(--accent-ink);display:grid;place-items:center;font-weight:900;font-size:14px;box-shadow:var(--glow-a)}.brand .bt{font-weight:800;font-size:17px;letter-spacing:-.01em}.title{font-size:20px;font-weight:800;margin:0 0 8px;letter-spacing:-.02em}.copy{color:var(--muted);margin:0 0 24px;line-height:1.6}.field{display:flex;flex-direction:column;gap:8px;margin-top:16px}.field label{font-size:12px;color:var(--muted);font-weight:700;letter-spacing:.04em;text-transform:uppercase}input{width:100%;border:1px solid var(--line);border-radius:10px;padding:10px 12px;font-size:14px;background:var(--panel);color:var(--ink);transition:border-color 150ms var(--ease),box-shadow 150ms var(--ease)}input:focus{outline:none;border-color:var(--accent);box-shadow:0 0 0 3px color-mix(in srgb,var(--accent) 18%,transparent)}:focus-visible{outline:2px solid var(--accent);outline-offset:2px;border-radius:6px}button[type="submit"]{width:100%;border:1px solid var(--accent);border-radius:10px;background:var(--accent);color:var(--accent-ink);padding:11px 16px;margin-top:24px;font-weight:700;cursor:pointer;transition:filter 150ms var(--ease),transform 150ms var(--ease)}button[type="submit"]:hover{filter:brightness(1.05)}button[type="submit"]:active{transform:scale(.98)}.error{display:none;margin:0 0 16px;padding:10px 12px;border-radius:10px;background:color-mix(in srgb,var(--danger) 12%,transparent);border:1px solid color-mix(in srgb,var(--danger) 35%,transparent);color:var(--danger);font-weight:700;font-size:13px}.error.show{display:block}.foot{margin-top:20px;font-size:12px;color:var(--muted);text-align:center}`

const loginPageJS = `const LOGIN_I18N={zh:{doc_title:'GeoProxy Gateway 登录',heading:'管理员登录',copy:'输入管理密码后进入后台。',error:'密码错误，请重试。',password:'管理密码',submit:'登录',foot:'仅限管理员。',lang_code:'EN',lang_aria:'切换语言'},en:{doc_title:'GeoProxy Gateway Login',heading:'Admin login',copy:'Enter the admin password to open the console.',error:'Incorrect password. Please try again.',password:'Admin password',submit:'Sign in',foot:'Administrators only.',lang_code:'中文',lang_aria:'Switch language'}};
function loginT(key){const pack=LOGIN_I18N[document.documentElement.getAttribute('data-lang')==='en'?'en':'zh'];return pack&&pack[key]!=null?pack[key]:key}
function applyLoginLang(lang){const ui=(lang==='en'?'en':'zh');try{localStorage.setItem('gg-lang',ui)}catch(e){}document.documentElement.lang=ui==='en'?'en':'zh-CN';document.documentElement.setAttribute('data-lang',ui);const pack=LOGIN_I18N[ui];document.title=pack.doc_title;document.querySelectorAll('[data-i18n]').forEach(function(el){const k=el.getAttribute('data-i18n');if(k&&pack[k]!=null)el.textContent=pack[k]});const code=document.getElementById('lang-code');if(code)code.textContent=pack.lang_code;const btn=document.getElementById('lang-toggle');if(btn){btn.title=pack.lang_aria;btn.setAttribute('aria-label',pack.lang_aria)}}
function toggleLoginLang(){const cur=document.documentElement.getAttribute('data-lang')==='en'?'en':'zh';applyLoginLang(cur==='zh'?'en':'zh')}
(function(){let lang='zh';try{lang=localStorage.getItem('gg-lang')||'zh'}catch(e){}applyLoginLang(lang)})();`

func buildLoginHTML(showError bool) string {
	errClass := "error"
	if showError {
		errClass = "error show"
	}
	var b strings.Builder
	b.Grow(len(loginPageCSS) + len(loginPageJS) + 1200)
	b.WriteString(`<!DOCTYPE html>
<html lang="zh-CN" data-lang="zh">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>GeoProxy Gateway 登录</title>
<style>
`)
	b.WriteString(loginPageCSS)
	b.WriteString(`
</style>
</head>
<body>
<main class="card">
 <button class="langbtn" id="lang-toggle" type="button" onclick="toggleLoginLang()" title="切换语言" aria-label="切换语言"><span id="lang-code">EN</span></button>
 <div class="brand"><div class="mark">GG</div><span class="bt">GeoProxy Gateway</span></div>
 <h1 class="title" data-i18n="heading">管理员登录</h1>
  <p class="copy" data-i18n="copy">输入管理密码后进入后台。</p>
  <div class="`)
	b.WriteString(errClass)
	b.WriteString(`" data-i18n="error">密码错误，请重试。</div>
  <form method="POST" action="/login">
   <div class="field"><label for="password" data-i18n="password">管理密码</label><input id="password" type="password" name="password" autocomplete="current-password" autofocus required></div>
   <button type="submit" data-i18n="submit">登录</button>
  </form>
  <div class="foot" data-i18n="foot">仅限管理员。</div>
</main>
<script>
`)
	b.WriteString(loginPageJS)
	b.WriteString(`
</script>
</body>
</html>`)
	return b.String()
}

// 保持既有标识符，避免改动 server 与测试中的引用面。
var (
	loginHTML          = buildLoginHTML(false)
	loginHTMLWithError = buildLoginHTML(true)
)
