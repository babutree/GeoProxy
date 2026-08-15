package webui

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/babutree/GeoProxy/storage"
)

// TestAPIManualNodeRegionRejectsMalformedRegion 锁定 API 边界对非法地域的显式拒绝。
//
// 存储层 normalizeManualRegion 会把非 alpha-2 值归一化为空串。若 handler 不拦截，
// 用户填 "XYZ" 会拿到 200 {"status":"updated"} 而地域被静默清空——既误导用户，
// 也违反项目「不静默回退」约定。空串仍必须被接受（语义为清除手工地域覆盖）。
func TestAPIManualNodeRegionRejectsMalformedRegion(t *testing.T) {
	server := newTestServer(t)
	if err := server.storage.AddManualProxy("203.0.113.40:8080", "http", "us", "region-guard"); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	proxy, err := server.storage.GetProxyByIdentity("203.0.113.40:8080", storage.SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() error = %v", err)
	}
	id := strconv.FormatInt(proxy.ID, 10)

	for _, tc := range []struct {
		name   string
		region string
	}{
		{"three letters", "XYZ"},
		{"one letter", "u"},
		{"digits", "12"},
		{"symbols", "u!"},
		{"whitespace only", "  "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"id":` + id + `,"region":"` + tc.region + `"}`
			rec := httptest.NewRecorder()
			server.routes().ServeHTTP(rec, authenticatedJSONRequest(http.MethodPost, "/api/manual-node/region", body))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d for region %q; body=%s", rec.Code, http.StatusBadRequest, tc.region, rec.Body.String())
			}
			// 拒绝路径不得触碰存储：既有地域必须原样保留。
			current, err := server.storage.GetProxyByID(proxy.ID)
			if err != nil {
				t.Fatalf("GetProxyByID() error = %v", err)
			}
			if current.Region != "us" {
				t.Fatalf("region = %q after rejected update, want preserved us", current.Region)
			}
		})
	}

	// 合法 alpha-2 仍必须成功写入（大小写不敏感）。
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, authenticatedJSONRequest(http.MethodPost, "/api/manual-node/region", `{"id":`+id+`,"region":"JP"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("valid region status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	updated, err := server.storage.GetProxyByID(proxy.ID)
	if err != nil {
		t.Fatalf("GetProxyByID() error = %v", err)
	}
	if updated.Region != "jp" {
		t.Fatalf("region = %q, want jp", updated.Region)
	}

	// 空串是合法输入：清除手工地域覆盖。
	rec2 := httptest.NewRecorder()
	server.routes().ServeHTTP(rec2, authenticatedJSONRequest(http.MethodPost, "/api/manual-node/region", `{"id":`+id+`,"region":""}`))
	if rec2.Code != http.StatusOK {
		t.Fatalf("empty region status = %d, want %d; body=%s", rec2.Code, http.StatusOK, rec2.Body.String())
	}
	cleared, err := server.storage.GetProxyByID(proxy.ID)
	if err != nil {
		t.Fatalf("GetProxyByID() error = %v", err)
	}
	if cleared.Region != "" {
		t.Fatalf("region = %q, want cleared", cleared.Region)
	}
}

// TestDashboardAPIKeyActionsUseJSStringEscaping 锁定 onclick 里的 JS 字符串上下文转义。
//
// html() 是 HTML 文本转义器：它把 ' 变成 &#39;，但浏览器解析属性值时会先把
// &#39; 解码回 ' 再交给 JS 解析器，所以用在 onclick="f('...')" 里等于没转义。
// 必须用 jsArg()（JSON.stringify + HTML 属性转义）生成 JS 字面量。
func TestDashboardAPIKeyActionsUseJSStringEscaping(t *testing.T) {
	if !strings.Contains(dashboardJS, "function jsArg(value){return html(JSON.stringify(") {
		t.Fatal("dashboard missing jsArg() JS-string-context escaper")
	}
	for _, legacy := range []string{
		`onclick="revokeAPIKey(\''+id+'\')"`,
		`onclick="deleteAPIKey(\''+id+'\')"`,
	} {
		if strings.Contains(dashboardJS, legacy) {
			t.Fatalf("dashboard still interpolates an API key id into a JS string literal: %s", legacy)
		}
	}
	for _, want := range []string{
		`onclick="revokeAPIKey('+id+')"`,
		`onclick="deleteAPIKey('+id+')"`,
		"const id=jsArg(k.id)",
	} {
		if !strings.Contains(dashboardJS, want) {
			t.Fatalf("dashboard missing JS-escaped API key action %q", want)
		}
	}
}
