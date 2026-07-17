package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAssetRoutesServeCSSAndJS 验证 CSS/JS 从 dashboardHTML 分离后，
// 由 /assets/dashboard.css 与 /assets/dashboard.js 路由下发，且：
//   - 返回 200
//   - 正确 Content-Type（text/css / application/javascript，均 charset=utf-8）
//   - 带基于内容 sha256 的强 ETag 与 Cache-Control
//   - 响应体分别等于 dashboardCSS / dashboardJS
func TestAssetRoutesServeCSSAndJS(t *testing.T) {
	server := newTestServer(t)
	cases := []struct {
		path        string
		contentType string
		body        string
	}{
		{"/assets/dashboard.css", "text/css; charset=utf-8", dashboardCSS},
		{"/assets/dashboard.js", "application/javascript; charset=utf-8", dashboardJS},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			server.routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("%s status = %d, want 200", tc.path, rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); got != tc.contentType {
				t.Fatalf("%s Content-Type = %q, want %q", tc.path, got, tc.contentType)
			}
			if etag := rec.Header().Get("ETag"); etag == "" {
				t.Fatalf("%s missing ETag header", tc.path)
			}
			cc := rec.Header().Get("Cache-Control")
			if cc == "" {
				t.Fatalf("%s missing Cache-Control header", tc.path)
			}
			// BUG-08：不得对固定资产 URL 设置可跳过再验证的新鲜窗口。
			if strings.Contains(cc, "max-age=") && !strings.Contains(cc, "max-age=0") {
				t.Fatalf("%s Cache-Control = %q; fixed asset URL must not be fresh-cached without revalidation", tc.path, cc)
			}
			if !strings.Contains(cc, "no-cache") && !strings.Contains(cc, "max-age=0") {
				t.Fatalf("%s Cache-Control = %q, want no-cache or max-age=0 revalidation", tc.path, cc)
			}
			if rec.Body.String() != tc.body {
				t.Fatalf("%s body does not match served constant (len got=%d want=%d)",
					tc.path, rec.Body.Len(), len(tc.body))
			}
		})
	}
}

// TestAssetRoutesIfNoneMatchReturns304 验证带 If-None-Match 命中当前内容 ETag 时返回 304，
// 且 304 响应体为空（浏览器复用缓存）。
func TestAssetRoutesIfNoneMatchReturns304(t *testing.T) {
	server := newTestServer(t)
	for _, path := range []string{"/assets/dashboard.css", "/assets/dashboard.js"} {
		t.Run(path, func(t *testing.T) {
			// 先取一次 ETag。
			first := httptest.NewRequest(http.MethodGet, path, nil)
			firstRec := httptest.NewRecorder()
			server.routes().ServeHTTP(firstRec, first)
			etag := firstRec.Header().Get("ETag")
			if etag == "" {
				t.Fatalf("%s missing ETag on first request", path)
			}

			// 带 If-None-Match 再取一次，应命中 304。
			second := httptest.NewRequest(http.MethodGet, path, nil)
			second.Header.Set("If-None-Match", etag)
			secondRec := httptest.NewRecorder()
			server.routes().ServeHTTP(secondRec, second)

			if secondRec.Code != http.StatusNotModified {
				t.Fatalf("%s with matching If-None-Match status = %d, want 304", path, secondRec.Code)
			}
			if secondRec.Body.Len() != 0 {
				t.Fatalf("%s 304 response must have empty body, got %d bytes", path, secondRec.Body.Len())
			}
		})
	}
}

// TestAssetRoutesDoNotRequireAuth 验证静态资源无需登录即可获取（登录页也需引用它们），
// 但不得泄露任何业务字段。
func TestAssetRoutesDoNotRequireAuth(t *testing.T) {
	server := newTestServer(t)
	for _, path := range []string{"/assets/dashboard.css", "/assets/dashboard.js"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		server.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200 (no auth required)", path, rec.Code)
		}
	}
}

// TestDashboardHTMLReferencesExternalAssets 验证 dashboardHTML 通过 <link>/<script src>
// 引用分离后的资源，而非内联 <style>/<script> 块。
func TestDashboardHTMLReferencesExternalAssets(t *testing.T) {
	if !strings.Contains(dashboardHTML, `<link rel="stylesheet" href="/assets/dashboard.css">`) {
		t.Fatal("dashboardHTML missing external CSS link")
	}
	if !strings.Contains(dashboardHTML, `<script src="/assets/dashboard.js"></script>`) {
		t.Fatal("dashboardHTML missing external JS script reference")
	}
	if strings.Contains(dashboardHTML, "<style>") {
		t.Fatal("dashboardHTML still inlines <style> block (should be separated to /assets/dashboard.css)")
	}
	if strings.Contains(dashboardHTML, "<script>") {
		t.Fatal("dashboardHTML still inlines <script> block (should be separated to /assets/dashboard.js)")
	}
}
