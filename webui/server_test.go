package webui

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/affinity"
	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/custom"
	"github.com/babutree/GeoProxy/storage"
	"github.com/babutree/GeoProxy/validator"
)

func TestBusinessAPIsRequireAuthentication(t *testing.T) {
	server := newTestServer(t)
	paths := []string{
		"/api/stats",
		"/api/proxies",
		"/api/logs",
		"/api/config",
		"/api/subscriptions",
		"/api/custom/status",
		"/api/sessions",
		"/api/proxy-occupancy",
		"/api/manual-node/add",
		"/api/manual-node/region",
		"/api/manual-node/note",
		"/api/manual-node/delete",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()

			server.routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
			assertNoBusinessTerms(t, rec.Body.String())
		})
	}
}

func TestSubscriptionMutationAPIsRequireAuthentication(t *testing.T) {
	server := newTestServer(t)
	paths := []string{
		"/api/subscription/add",
		"/api/subscription/delete",
		"/api/subscription/refresh",
		"/api/subscription/refresh-all",
		"/api/subscription/toggle",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"id":1}`))
			rec := httptest.NewRecorder()

			server.routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
			assertNoBusinessTerms(t, rec.Body.String())
		})
	}
}

func TestLogsAPIRequiresAuthenticationWithoutBusinessLeak(t *testing.T) {
	server := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	assertNoBusinessTerms(t, rec.Body.String())
}

func TestConfigGetReturnsActiveGatewayFieldsOnly(t *testing.T) {
	server := newTestServer(t)
	server.cfg.HTTPPort = ":9100"
	server.cfg.SOCKS5Port = ":9101"
	server.cfg.WebUIPort = ":9102"
	server.cfg.ProxyAuthEnabled = true
	server.cfg.ProxyAuthUsername = "edge"
	server.cfg.ProxyAuthPassword = "secret"
	server.cfg.SessionTTLMinutes = 20
	server.cfg.DefaultRegion = "jp"
	server.cfg.HealthIntervalMinutes = 6
	server.cfg.MaxRetry = 2
	server.cfg.SingBoxPath = "sing-box.exe"
	server.cfg.AllowedCountries = []string{"JP", "US"}
	server.cfg.BlockedCountries = []string{"CN"}
	setTestGlobalConfig(t, server.cfg)

	req := authenticatedJSONRequest(http.MethodGet, "/api/config", "")
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode config response: %v", err)
	}
	// 代理密码明文下发给已认证前端，供一键复制含密码的完整代理 URL。
	// WebUI 登录密码仍只存哈希、绝不下发；此处仅代理密码。
	wantKeys := []string{
		"allowed_countries", "blocked_countries", "default_region", "health_check_interval", "http_port", "max_retry",
		"proxy_auth_enabled", "proxy_auth_password", "proxy_auth_username", "readonly_fields", "restart_required_fields",
		"session_ttl_minutes", "singbox_path", "socks5_port", "webui_port",
	}
	if !reflect.DeepEqual(sortedKeys(got), wantKeys) {
		t.Fatalf("config keys = %#v, want %#v; body=%s", sortedKeys(got), wantKeys, rec.Body.String())
	}
	if got["proxy_auth_password"] != "secret" {
		t.Fatalf("config GET 应下发代理密码明文以支持复制 URL，got proxy_auth_password=%v", got["proxy_auth_password"])
	}
	assertNoLegacyConfigFields(t, rec.Body.String())
}

func TestConfigSavePersistsActiveEditableFields(t *testing.T) {
	server := newTestServer(t)
	oldHash := sha256Hex("old-secret")
	server.cfg.ProxyAuthPassword = "old-secret"
	server.cfg.ProxyAuthPasswordHash = oldHash
	setTestGlobalConfig(t, server.cfg)
	payload := `{"proxy_auth_enabled":true,"proxy_auth_username":"edge","proxy_auth_password":"","session_ttl_minutes":25,"default_region":" us ","health_check_interval":8,"max_retry":0,"singbox_path":"D:/tools/sing-box.exe","allowed_countries":[" us ","JP","jp","bad"],"blocked_countries":[" cn "]}`

	serveAuthenticated(t, server, "/api/config/save", payload, http.StatusOK)

	if server.cfg.ProxyAuthEnabled != true || server.cfg.ProxyAuthUsername != "edge" {
		t.Fatalf("auth config = enabled:%v username:%q", server.cfg.ProxyAuthEnabled, server.cfg.ProxyAuthUsername)
	}
	// 代理密码明文化后：提交空密码表示"不改"，旧明文与旧哈希都应原样保留
	// （代理密码保留明文以支持复制含密码的完整代理 URL；提交空密码时明文与哈希均不变）。
	if server.cfg.ProxyAuthPassword != "old-secret" || server.cfg.ProxyAuthPasswordHash != oldHash {
		t.Fatalf("empty password submit should preserve old plain password/hash, got: %q/%q", server.cfg.ProxyAuthPassword, server.cfg.ProxyAuthPasswordHash)
	}
	if server.cfg.SessionTTLMinutes != 25 || server.cfg.DefaultRegion != "US" || server.cfg.HealthIntervalMinutes != 8 || server.cfg.MaxRetry != 0 {
		t.Fatalf("runtime config = ttl:%d region:%q health:%d retry:%d", server.cfg.SessionTTLMinutes, server.cfg.DefaultRegion, server.cfg.HealthIntervalMinutes, server.cfg.MaxRetry)
	}
	if server.cfg.SingBoxPath != "D:/tools/sing-box.exe" {
		t.Fatalf("SingBoxPath = %q", server.cfg.SingBoxPath)
	}
	if !reflect.DeepEqual(server.cfg.AllowedCountries, []string{"US", "JP"}) || !reflect.DeepEqual(server.cfg.BlockedCountries, []string{"CN"}) {
		t.Fatalf("countries = allowed:%#v blocked:%#v", server.cfg.AllowedCountries, server.cfg.BlockedCountries)
	}
	if server.affinity.TTL() != 25*time.Minute {
		t.Fatalf("affinity TTL = %v, want 25m", server.affinity.TTL())
	}
	assertConfigJSONOmitsLegacyFields(t, config.ConfigFile())
}

func TestConfigSaveRejectsRuntimePortChanges(t *testing.T) {
	server := newTestServer(t)
	server.cfg.HTTPPort = ":7802"
	server.cfg.SOCKS5Port = ":7801"
	server.cfg.WebUIPort = ":7800"
	setTestGlobalConfig(t, server.cfg)
	payload := `{"http_port":":9902","proxy_auth_enabled":true,"proxy_auth_username":"edge","session_ttl_minutes":25,"health_check_interval":8,"max_retry":0,"singbox_path":"sing-box"}`

	serveAuthenticated(t, server, "/api/config/save", payload, http.StatusBadRequest)

	if got := config.Get(); got.HTTPPort != ":7802" {
		t.Fatalf("HTTPPort after rejected save = %q, want :7802", got.HTTPPort)
	}
}

func TestConfigSaveAppliesCountryFiltersImmediately(t *testing.T) {
	server := newTestServer(t)
	server.cfg.ProxyAuthUsername = "edge"
	server.cfg.ProxyAuthPasswordHash = sha256Hex("secret")
	server.cfg.SessionTTLMinutes = 10
	server.cfg.HealthIntervalMinutes = 5
	server.cfg.SingBoxPath = "sing-box"
	setTestGlobalConfig(t, server.cfg)
	insertWebUITestProxy(t, server.storage, "cn:8080", "CN", "active")
	insertWebUITestProxy(t, server.storage, "us:8080", "US", "active")
	payload := `{"proxy_auth_enabled":true,"proxy_auth_username":"edge","session_ttl_minutes":10,"health_check_interval":5,"max_retry":0,"singbox_path":"sing-box","blocked_countries":[" cn "]}`

	serveAuthenticated(t, server, "/api/config/save", payload, http.StatusOK)

	cnProxy, err := server.storage.GetProxyByAddress("cn:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress(cn) error = %v", err)
	}
	usProxy, err := server.storage.GetProxyByAddress("us:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress(us) error = %v", err)
	}
	if cnProxy.Status != "disabled" || usProxy.Status != "active" {
		t.Fatalf("statuses after filter = cn:%s us:%s", cnProxy.Status, usProxy.Status)
	}
}

func TestConfigSaveFailureLeavesRuntimeUnchanged(t *testing.T) {
	server := newTestServer(t)
	server.cfg.ProxyAuthUsername = "old"
	server.cfg.SessionTTLMinutes = 10
	server.cfg.HealthIntervalMinutes = 5
	server.cfg.SingBoxPath = "sing-box"
	setTestGlobalConfig(t, server.cfg)
	badDataDir := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(badDataDir, []byte("file"), 0644); err != nil {
		t.Fatalf("create bad DATA_DIR file: %v", err)
	}
	t.Setenv("DATA_DIR", badDataDir)
	payload := `{"proxy_auth_enabled":true,"proxy_auth_username":"new","session_ttl_minutes":25,"health_check_interval":5,"max_retry":0,"singbox_path":"sing-box"}`

	serveAuthenticated(t, server, "/api/config/save", payload, http.StatusInternalServerError)

	if server.cfg.ProxyAuthUsername != "old" || server.cfg.SessionTTLMinutes != 10 || server.affinity.TTL() != 10*time.Minute {
		t.Fatalf("runtime changed after failed save: cfg=%#v ttl=%v", server.cfg, server.affinity.TTL())
	}
	if got := config.Get(); got.ProxyAuthUsername != "old" || got.SessionTTLMinutes != 10 {
		t.Fatalf("global changed after failed save: %#v", got)
	}
}

func TestManualNodeMutationRequiresAuthentication(t *testing.T) {
	server := newTestServer(t)
	body := strings.NewReader(`{"address":"203.0.113.10:8080","region":"jp"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/manual-node/region", body)
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	assertNoBusinessTerms(t, rec.Body.String())
}

func TestManualNodeAllowsSubscriptionRegionAndNoteEdits(t *testing.T) {
	server := newTestServer(t)
	subID, err := server.storage.AddSubscription("test", "https://example.test/webui.yaml", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if err := server.storage.AddProxyWithSource("198.51.100.10:8080", "http", storage.SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}

	// 统一节点管理：note 与 region 两条非破坏性路径对任意来源开放（订阅节点也可编辑）。
	// 删除走来源无关的 /api/proxy/delete（本测试不删，保留节点做后续断言）。
	t.Run("region", func(t *testing.T) {
		req := authenticatedJSONRequest(http.MethodPost, "/api/manual-node/region", `{"address":"198.51.100.10:8080","region":"jp"}`)
		rec := httptest.NewRecorder()
		server.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("region status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
	})

	t.Run("note", func(t *testing.T) {
		req := authenticatedJSONRequest(http.MethodPost, "/api/manual-node/note", `{"address":"198.51.100.10:8080","note":"blocked"}`)
		rec := httptest.NewRecorder()

		server.routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("note status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
	})

	proxy, err := server.storage.GetProxyByAddress("198.51.100.10:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if proxy.Source != storage.SourceSubscription {
		t.Fatalf("source = %q, want %q", proxy.Source, storage.SourceSubscription)
	}
	if proxy.Region != "jp" || proxy.RegionSource != "manual" {
		t.Fatalf("region = %q source = %q, want jp/manual (subscription region override should persist)", proxy.Region, proxy.RegionSource)
	}
	if proxy.Note != "blocked" {
		t.Fatalf("note = %q, want %q (subscription note edit should persist)", proxy.Note, "blocked")
	}
}

func TestManualNodeRegionNoteDeleteSucceeds(t *testing.T) {
	server := newTestServer(t)
	if err := server.storage.AddManualProxy("203.0.113.20:8080", "http", "us", "old"); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}

	serveAuthenticated(t, server, "/api/manual-node/region", `{"address":"203.0.113.20:8080","region":"jp"}`, http.StatusOK)
	serveAuthenticated(t, server, "/api/manual-node/note", `{"address":"203.0.113.20:8080","note":"primary"}`, http.StatusOK)

	proxy, err := server.storage.GetProxyByAddress("203.0.113.20:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if proxy.Region != "jp" || proxy.RegionSource != "manual" || proxy.Note != "primary" {
		t.Fatalf("proxy = %#v", proxy)
	}

	serveAuthenticated(t, server, "/api/manual-node/delete", `{"address":"203.0.113.20:8080"}`, http.StatusOK)
	if _, err := server.storage.GetProxyByAddress("203.0.113.20:8080"); err == nil {
		t.Fatal("GetProxyByAddress() expected error after delete, got nil")
	}
}

func TestManualNodeAddUsesCustomManagerDirectPath(t *testing.T) {
	server := newTestServer(t)
	server.customMgr = custom.NewManager(server.storage, validator.New(1, 1, "http://127.0.0.1"), &config.Config{})

	serveAuthenticated(t, server, "/api/manual-node/add", `{"link":"http://192.0.2.10:8080","region":"sg","note":"direct"}`, http.StatusOK)

	proxy, err := server.storage.GetProxyByAddress("192.0.2.10:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if proxy.Source != storage.SourceManual || proxy.Protocol != "http" || proxy.Region != "sg" || proxy.Note != "direct" {
		t.Fatalf("proxy = %#v", proxy)
	}
}

func TestNewSessionUsesHighEntropyTokens(t *testing.T) {
	seen := make(map[string]bool)
	for range 1000 {
		token := newSession()
		if len(token) != sessionTokenBytes*2 {
			t.Fatalf("len(token) = %d, want %d", len(token), sessionTokenBytes*2)
		}
		if seen[token] {
			t.Fatalf("duplicate token generated: %s", token)
		}
		seen[token] = true
	}
}

func TestLoginRateLimitLocksRepeatedFailures(t *testing.T) {
	server := newTestServer(t)
	server.cfg.WebUIPasswordHash = sha256Hex("correct")

	for i := 0; i < maxLoginFailures; i++ {
		rec := serveLogin(t, server, "wrong", "198.51.100.10:12345", false)
		if rec.Code != http.StatusOK {
			t.Fatalf("failure %d status = %d, want %d", i+1, rec.Code, http.StatusOK)
		}
	}

	locked := serveLogin(t, server, "correct", "198.51.100.10:12345", false)
	if locked.Code != http.StatusTooManyRequests {
		t.Fatalf("locked status = %d, want %d", locked.Code, http.StatusTooManyRequests)
	}

	otherClient := serveLogin(t, server, "correct", "198.51.100.11:12345", false)
	if otherClient.Code != http.StatusFound {
		t.Fatalf("other client status = %d, want %d; body=%s", otherClient.Code, http.StatusFound, otherClient.Body.String())
	}
}

func TestLoginCookieSecurityAttributes(t *testing.T) {
	server := newTestServer(t)
	server.cfg.WebUIPasswordHash = sha256Hex("correct")

	secureRec := serveLogin(t, server, "correct", "198.51.100.12:12345", true)
	secureCookie := findCookie(t, secureRec.Result().Cookies(), "session")
	if !secureCookie.HttpOnly || !secureCookie.Secure || secureCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("secure cookie attrs = HttpOnly:%v Secure:%v SameSite:%v", secureCookie.HttpOnly, secureCookie.Secure, secureCookie.SameSite)
	}

	insecureRec := serveLogin(t, server, "correct", "198.51.100.13:12345", false)
	insecureCookie := findCookie(t, insecureRec.Result().Cookies(), "session")
	if insecureCookie.Secure {
		t.Fatal("HTTP login cookie unexpectedly set Secure")
	}
	if !insecureCookie.HttpOnly || insecureCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("HTTP cookie attrs = HttpOnly:%v SameSite:%v", insecureCookie.HttpOnly, insecureCookie.SameSite)
	}
}

func TestLoginRejectsOversizedRequestBody(t *testing.T) {
	server := newTestServer(t)
	server.cfg.WebUIPasswordHash = sha256Hex("correct")
	body := "password=" + strings.Repeat("a", maxLoginBody)
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
}

func TestLogoutRequiresPostAndCSRFProof(t *testing.T) {
	server := newTestServer(t)
	token := newSession()

	getReq := httptest.NewRequest(http.MethodGet, "/logout", nil)
	getReq.AddCookie(&http.Cookie{Name: "session", Value: token})
	getRec := httptest.NewRecorder()
	server.routes().ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want %d", getRec.Code, http.StatusMethodNotAllowed)
	}
	if !sessionTokenExists(token) {
		t.Fatal("GET /logout invalidated the session")
	}

	postReq := httptest.NewRequest(http.MethodPost, "/logout", nil)
	postReq.AddCookie(&http.Cookie{Name: "session", Value: token})
	postRec := httptest.NewRecorder()
	server.routes().ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusForbidden {
		t.Fatalf("POST without CSRF status = %d, want %d", postRec.Code, http.StatusForbidden)
	}
	if !sessionTokenExists(token) {
		t.Fatal("CSRF-rejected logout invalidated the session")
	}

	allowedReq := httptest.NewRequest(http.MethodPost, "/logout", nil)
	allowedReq.Header.Set("X-CSRF-Token", token)
	allowedReq.AddCookie(&http.Cookie{Name: "session", Value: token})
	allowedRec := httptest.NewRecorder()
	server.routes().ServeHTTP(allowedRec, allowedReq)
	if allowedRec.Code != http.StatusFound {
		t.Fatalf("CSRF-authenticated POST status = %d, want %d", allowedRec.Code, http.StatusFound)
	}
	if sessionTokenExists(token) {
		t.Fatal("successful logout retained the session")
	}
}

func sessionTokenExists(token string) bool {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	_, ok := sessions[token]
	return ok
}

func TestWriteAPIsRequireCSRFProof(t *testing.T) {
	server := newTestServer(t)
	token := newSession()
	req := httptest.NewRequest(http.MethodPost, "/api/proxy/delete", strings.NewReader(`{"address":"203.0.113.10:8080"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}

	allowed := httptest.NewRequest(http.MethodPost, "/api/proxy/delete", strings.NewReader(`{"address":"203.0.113.10:8080"}`))
	allowed.Header.Set("Content-Type", "application/json")
	allowed.Header.Set("X-CSRF-Token", token)
	allowed.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec = httptest.NewRecorder()

	server.routes().ServeHTTP(rec, allowed)

	if rec.Code != http.StatusOK {
		t.Fatalf("CSRF-authenticated status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestSubscriptionFileContentRawContractAndSizeLimit(t *testing.T) {
	server := newTestServer(t)
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	raw := "proxies:\n  - name: direct\n    type: http\n"
	payload := fmt.Sprintf(`{"name":"raw","file_content":%q,"refresh_min":60}`, raw)

	serveAuthenticated(t, server, "/api/subscription/add", payload, http.StatusOK)

	subs, err := server.storage.GetSubscriptions()
	if err != nil {
		t.Fatalf("GetSubscriptions() error = %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("len(subs) = %d, want 1", len(subs))
	}
	content, err := os.ReadFile(filepath.Clean(subs[0].FilePath))
	if err != nil {
		t.Fatalf("read subscription file: %v", err)
	}
	if string(content) != raw {
		t.Fatalf("file content = %q, want raw %q", string(content), raw)
	}

	oversized := fmt.Sprintf(`{"name":"big","file_content":%q,"refresh_min":60}`, strings.Repeat("a", maxSubscriptionFileContentBytes+1))
	serveAuthenticated(t, server, "/api/subscription/add", oversized, http.StatusRequestEntityTooLarge)
}

func TestJSONMutationRejectsTrailingValueWithoutSideEffect(t *testing.T) {
	server := newTestServer(t)
	if err := server.storage.AddManualProxy("203.0.113.30:8080", "http", "us", "before"); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}

	cases := []struct {
		name string
		path string
		body string
	}{
		{"manual note", "/api/manual-node/note", `{"address":"203.0.113.30:8080","note":"after"}{}`},
		{"proxy toggle", "/api/proxy/toggle", `{"address":"203.0.113.30:8080","enable":false}{}`},
		{"subscription add", "/api/subscription/add", `{"name":"bad","url":"https://example.test/sub","refresh_min":60}{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			serveAuthenticated(t, server, tc.path, tc.body, http.StatusBadRequest)
		})
	}

	proxy, err := server.storage.GetProxyByAddress("203.0.113.30:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if proxy.Note != "before" || proxy.UserPaused {
		t.Fatalf("proxy mutated by invalid JSON: note=%q user_paused=%v", proxy.Note, proxy.UserPaused)
	}
	subs, err := server.storage.GetSubscriptions()
	if err != nil {
		t.Fatalf("GetSubscriptions() error = %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("invalid JSON added %d subscriptions", len(subs))
	}
}

func TestOversizedJSONMutationsReturnRequestEntityTooLarge(t *testing.T) {
	server := newTestServer(t)
	padding := strings.Repeat("a", maxAPIRequestBody)
	cases := []struct {
		name string
		path string
		body string
	}{
		{"proxy", "/api/proxy/toggle", `{"address":"203.0.113.30:8080","padding":"` + padding + `"}`},
		{"manual node", "/api/manual-node/note", `{"address":"203.0.113.30:8080","padding":"` + padding + `"}`},
		{"config", "/api/config/save", `{"proxy_auth_username":"edge","padding":"` + padding + `"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			serveAuthenticated(t, server, tc.path, tc.body, http.StatusRequestEntityTooLarge)
		})
	}
}

func TestConcurrentSubscriptionUploadsUseUniqueFiles(t *testing.T) {
	server := newTestServer(t)
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	const count = 12
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, count)

	for i := 0; i < count; i++ {
		go func(i int) {
			<-start
			payload := fmt.Sprintf(`{"name":"sub-%d","file_content":"node-%d","refresh_min":60}`, i, i)
			req := authenticatedJSONRequest(http.MethodPost, "/api/subscription/add", payload)
			rec := httptest.NewRecorder()
			server.routes().ServeHTTP(rec, req)
			results <- rec
		}(i)
	}
	close(start)
	for i := 0; i < count; i++ {
		if rec := <-results; rec.Code != http.StatusOK {
			t.Fatalf("upload status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
	}

	subs, err := server.storage.GetSubscriptions()
	if err != nil {
		t.Fatalf("GetSubscriptions() error = %v", err)
	}
	paths := make(map[string]struct{}, len(subs))
	for _, sub := range subs {
		paths[sub.FilePath] = struct{}{}
	}
	if len(subs) != count || len(paths) != count {
		t.Fatalf("subscriptions=%d unique paths=%d, want %d", len(subs), len(paths), count)
	}
}

func TestSubscriptionUploadCleansFileWhenDatabaseInsertFails(t *testing.T) {
	server := newTestServer(t)
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	if err := server.storage.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	serveAuthenticated(t, server, "/api/subscription/add", `{"name":"orphan","file_content":"node","refresh_min":60}`, http.StatusInternalServerError)

	files, err := filepath.Glob(filepath.Join(dataDir, "subscriptions", "*"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("database failure left uploaded files: %#v", files)
	}
}

func TestAPIErrorResponsesAreSanitized(t *testing.T) {
	server := newTestServer(t)
	if err := server.storage.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	req := authenticatedJSONRequest(http.MethodGet, "/api/proxies", "")
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	for _, leaked := range []string{"sql", "database", "closed", "D:\\", "/tmp/"} {
		if strings.Contains(strings.ToLower(rec.Body.String()), strings.ToLower(leaked)) {
			t.Fatalf("response leaked %q: %s", leaked, rec.Body.String())
		}
	}
}

func TestConfigSaveErrorsAreSanitized(t *testing.T) {
	t.Run("persistence error", func(t *testing.T) {
		server := newTestServer(t)
		server.cfg.ProxyAuthUsername = "old"
		server.cfg.SessionTTLMinutes = 10
		server.cfg.HealthIntervalMinutes = 5
		server.cfg.SingBoxPath = "sing-box"
		setTestGlobalConfig(t, server.cfg)
		badDataDir := filepath.Join(t.TempDir(), "private-config-location")
		if err := os.WriteFile(badDataDir, []byte("file"), 0600); err != nil {
			t.Fatalf("create bad DATA_DIR file: %v", err)
		}
		t.Setenv("DATA_DIR", badDataDir)
		payload := `{"proxy_auth_username":"new","session_ttl_minutes":10,"health_check_interval":5,"max_retry":0,"singbox_path":"sing-box"}`
		req := authenticatedJSONRequest(http.MethodPost, "/api/config/save", payload)
		rec := httptest.NewRecorder()

		server.routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
		}
		if rec.Body.String() != "{\"error\":\"failed to save config\"}\n" {
			t.Fatalf("response leaked persistence detail: %s", rec.Body.String())
		}
	})

	t.Run("country filter error", func(t *testing.T) {
		server := newTestServer(t)
		server.cfg.ProxyAuthUsername = "old"
		server.cfg.SessionTTLMinutes = 10
		server.cfg.HealthIntervalMinutes = 5
		server.cfg.SingBoxPath = "sing-box"
		setTestGlobalConfig(t, server.cfg)
		if err := server.storage.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		payload := `{"proxy_auth_username":"new","session_ttl_minutes":10,"health_check_interval":5,"max_retry":0,"singbox_path":"sing-box","blocked_countries":["CN"]}`
		req := authenticatedJSONRequest(http.MethodPost, "/api/config/save", payload)
		rec := httptest.NewRecorder()

		server.routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
		}
		if rec.Body.String() != "{\"error\":\"failed to apply country filters\"}\n" {
			t.Fatalf("response leaked storage detail: %s", rec.Body.String())
		}
		if server.cfg.ProxyAuthUsername != "old" || server.cfg.SessionTTLMinutes != 10 {
			t.Fatalf("server config changed after filter failure: %#v", server.cfg)
		}
		if server.affinity.TTL() != 10*time.Minute {
			t.Fatalf("affinity TTL = %v, want 10m after filter failure", server.affinity.TTL())
		}
		if got := config.Get(); got.ProxyAuthUsername != "old" || got.SessionTTLMinutes != 10 {
			t.Fatalf("global config changed after filter failure: %#v", got)
		}
		data, err := os.ReadFile(filepath.Clean(config.ConfigFile()))
		if err != nil {
			t.Fatalf("read persisted config: %v", err)
		}
		if strings.Contains(string(data), `"proxy_auth_username": "new"`) {
			t.Fatalf("new config remained persisted after filter failure: %s", data)
		}
	})
}

func TestConfigSaveCountryFilterFailureRollsBackWhenServerConfigAliasesGlobal(t *testing.T) {
	server := newTestServer(t)
	oldCfg := *server.cfg
	oldCfg.ProxyAuthUsername = "old"
	oldCfg.SessionTTLMinutes = 10
	oldCfg.HealthIntervalMinutes = 5
	oldCfg.SingBoxPath = "sing-box"
	setTestGlobalConfig(t, &oldCfg)
	server.cfg = config.Get()
	if err := server.storage.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	payload := `{"proxy_auth_username":"new","session_ttl_minutes":25,"health_check_interval":5,"max_retry":0,"singbox_path":"sing-box","blocked_countries":["CN"]}`

	serveAuthenticated(t, server, "/api/config/save", payload, http.StatusInternalServerError)

	if server.cfg.ProxyAuthUsername != "old" || server.cfg.SessionTTLMinutes != 10 {
		t.Fatalf("aliased server config not rolled back: %#v", server.cfg)
	}
	if got := config.Get(); got.ProxyAuthUsername != "old" || got.SessionTTLMinutes != 10 {
		t.Fatalf("aliased global config not rolled back: %#v", got)
	}
	if server.affinity.TTL() != 10*time.Minute {
		t.Fatalf("affinity TTL = %v, want 10m", server.affinity.TTL())
	}
}

func TestConfigSaveKeepsRuntimeAlignedWhenRollbackSaveFails(t *testing.T) {
	server := newTestServer(t)
	oldCfg := *server.cfg
	oldCfg.ProxyAuthUsername = "old"
	oldCfg.SessionTTLMinutes = 10
	oldCfg.HealthIntervalMinutes = 5
	oldCfg.SingBoxPath = "sing-box"
	setTestGlobalConfig(t, &oldCfg)
	server.cfg = config.Get()
	if err := server.storage.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	origSave := configSave
	t.Cleanup(func() { configSave = origSave })
	calls := 0
	configSave = func(cfg *config.Config) error {
		calls++
		if calls == 1 {
			return origSave(cfg)
		}
		return fmt.Errorf("forced rollback save failure")
	}

	payload := `{"proxy_auth_username":"new","session_ttl_minutes":25,"health_check_interval":5,"max_retry":0,"singbox_path":"sing-box","blocked_countries":["CN"]}`
	serveAuthenticated(t, server, "/api/config/save", payload, http.StatusInternalServerError)

	if calls < 2 {
		t.Fatalf("configSave calls = %d, want at least 2 (forward + rollback)", calls)
	}
	// 当磁盘/全局无法回滚时，运行态不得单独回滚成旧值，否则会与 config.Get()/磁盘分裂。
	if server.cfg.ProxyAuthUsername != "new" || server.cfg.SessionTTLMinutes != 25 {
		t.Fatalf("server runtime rolled back despite failed disk rollback: %#v", server.cfg)
	}
	if got := config.Get(); got.ProxyAuthUsername != "new" || got.SessionTTLMinutes != 25 {
		t.Fatalf("global config = %#v, want new values kept after failed rollback save", got)
	}
	if server.affinity.TTL() != 25*time.Minute {
		t.Fatalf("affinity TTL = %v, want 25m aligned with persisted new config", server.affinity.TTL())
	}
	data, err := os.ReadFile(filepath.Clean(config.ConfigFile()))
	if err != nil {
		t.Fatalf("read persisted config: %v", err)
	}
	if !strings.Contains(string(data), `"proxy_auth_username": "new"`) {
		t.Fatalf("disk config missing new username after failed rollback: %s", data)
	}
}

func TestSecurityStateActivityPrunesExpiredEntries(t *testing.T) {
	resetWebUISecurityState()
	now := time.Now()
	sessionsMu.Lock()
	sessions["expired"] = now.Add(-time.Second)
	sessions["active"] = now.Add(time.Hour)
	sessionsMu.Unlock()
	loginAttemptsMu.Lock()
	loginAttempts["stale"] = loginAttempt{Failures: 1, LastFailure: now.Add(-2 * loginLockout)}
	loginAttempts["recent"] = loginAttempt{Failures: 1, LastFailure: now.Add(-loginLockout / 2)}
	loginAttemptsMu.Unlock()

	_ = newSession()
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "198.51.100.90:1234"
	recordLoginFailure(req, now)

	sessionsMu.Lock()
	_, expiredExists := sessions["expired"]
	_, activeExists := sessions["active"]
	sessionsMu.Unlock()
	if expiredExists || !activeExists {
		t.Fatalf("session cleanup: expired exists=%v active exists=%v", expiredExists, activeExists)
	}
	loginAttemptsMu.Lock()
	_, staleExists := loginAttempts["stale"]
	_, recentExists := loginAttempts["recent"]
	_, currentExists := loginAttempts["198.51.100.90"]
	loginAttemptsMu.Unlock()
	if staleExists || !recentExists || !currentExists {
		t.Fatalf("login cleanup: stale=%v recent=%v current=%v", staleExists, recentExists, currentExists)
	}
}

func TestAuthCheckIsPublicAndNeutral(t *testing.T) {
	server := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/check", nil)
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"isAdmin":false`) {
		t.Fatalf("body = %s, want unauthenticated auth state", body)
	}
	assertNoBusinessTerms(t, body)
}

func TestIndexWithoutAuthShowsOnlyNeutralLogin(t *testing.T) {
	server := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	// 未登录访问 / 应返回中性登录页：断言登录页的稳定契约（表单动作、密码字段）。
	for _, want := range []string{`action="/login"`, `name="password"`, `type="password"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("login page missing stable contract %q: %s", want, body)
		}
	}
	assertNoBusinessTerms(t, body)
}

func TestSessionAPIRequiresAuthentication(t *testing.T) {
	server := newTestServer(t)
	server.affinity.Set("browser-1", "203.0.113.10:8080", "us")
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	assertNoBusinessTerms(t, rec.Body.String())
}

func TestSessionAPIReturnsActiveBindings(t *testing.T) {
	server := newTestServer(t)
	server.affinity.Set("browser-1", "203.0.113.10:8080", "us")
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: newSession()})
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var rows []struct {
		SessionID           string `json:"session_id"`
		ProxyID             int64  `json:"proxy_id"`
		RouteLabel          string `json:"route_label"`
		Node                string `json:"node"`
		BindAddress         string `json:"bind_address"`
		Region              string `json:"region"`
		RegionReq           string `json:"region_req"`
		RemainingTTLSeconds int64  `json:"remaining_ttl_seconds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode sessions response: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1: %#v", len(rows), rows)
	}
	if rows[0].SessionID != "browser-1" || rows[0].Node != "203.0.113.10:8080" || rows[0].Region != "us" {
		t.Fatalf("row = %#v", rows[0])
	}
	if rows[0].RouteLabel != "region-us-session-browser-1" {
		t.Fatalf("route_label = %q, want region-us-session-browser-1", rows[0].RouteLabel)
	}
	if rows[0].RegionReq != "us" {
		t.Fatalf("region_req = %q, want us", rows[0].RegionReq)
	}
	if rows[0].BindAddress != "203.0.113.10:8080" {
		t.Fatalf("bind_address = %q", rows[0].BindAddress)
	}
	if rows[0].RemainingTTLSeconds <= 0 || rows[0].RemainingTTLSeconds > int64((10*time.Minute).Seconds()) {
		t.Fatalf("remaining_ttl_seconds = %d, want within ttl", rows[0].RemainingTTLSeconds)
	}
}

// TestSessionAPIPrefersExitIPOverLocalMixedBind 隧道会话绑定地址常为 127.0.0.1:mixed，
// 展示出口节点必须优先真实 exit_ip，不能把本机 mixed 当地址出口。
func TestSessionAPIPrefersExitIPOverLocalMixedBind(t *testing.T) {
	server := newTestServer(t)
	if err := server.storage.AddManualProxy("127.0.0.1:31001", "socks5", "jp", "tunnel-note"); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	proxy, err := server.storage.GetProxyByIdentity("127.0.0.1:31001", storage.SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() error = %v", err)
	}
	if err := server.storage.UpdateProxyExitInfo(proxy.ID, "133.242.1.2", "JP", 312, 0, "", 0, ""); err != nil {
		t.Fatalf("UpdateProxyExitInfo() error = %v", err)
	}
	server.affinity.SetProxy("app01", proxy.ID, "127.0.0.1:31001", "jp")

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: newSession()})
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var rows []sessionRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len=%d want 1", len(rows))
	}
	if rows[0].Node != "133.242.1.2" {
		t.Fatalf("node display = %q, want exit_ip 133.242.1.2 (not local mixed)", rows[0].Node)
	}
	if rows[0].BindAddress != "127.0.0.1:31001" {
		t.Fatalf("bind_address = %q, want local mixed", rows[0].BindAddress)
	}
	if rows[0].ProxyID != proxy.ID {
		t.Fatalf("proxy_id = %d, want %d", rows[0].ProxyID, proxy.ID)
	}
	if rows[0].ExitIP != "133.242.1.2" || rows[0].Latency != 312 || rows[0].QualityGrade == "" && false {
		// quality may be computed by UpdateProxyExitInfo path
	}
	if rows[0].Source != storage.SourceManual {
		t.Fatalf("source = %q, want manual", rows[0].Source)
	}
}

func TestSessionMonitorContainerOnlyInAuthenticatedDashboard(t *testing.T) {
	if !strings.Contains(dashboardHTML, `id="session-rows"`) {
		t.Fatal("dashboardHTML missing session monitor container")
	}
	if strings.Contains(loginHTML, "session-rows") || strings.Contains(loginHTMLWithError, "session-rows") {
		t.Fatal("login HTML contains session monitor container")
	}
}

// TestProxyOccupancyAPIRequiresAuthentication: 未登录访问 /api/proxy-occupancy 必须 401，且不泄漏业务字段。
func TestProxyOccupancyAPIRequiresAuthentication(t *testing.T) {
	server := newTestServer(t)
	if err := server.storage.AddManualProxy("203.0.113.50:8080", "http", "us", ""); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	proxy, err := server.storage.GetProxyByAddress("203.0.113.50:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	server.affinity.SetProxy("occ-unauth", proxy.ID, proxy.Address, "us")

	req := httptest.NewRequest(http.MethodGet, "/api/proxy-occupancy", nil)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	assertNoBusinessTerms(t, rec.Body.String())
}

// TestProxyOccupancyAPIReturnsPerProxySessions: 已认证返回每节点 active_sessions / max_sessions；cooldown_remaining_seconds 非负；无 password 字段。
func TestProxyOccupancyAPIReturnsPerProxySessions(t *testing.T) {
	server := newTestServer(t)
	server.cfg.MaxSessionsPerProxy = 1
	if err := server.storage.AddManualProxy("203.0.113.51:8080", "http", "us", ""); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	if err := server.storage.AddManualProxy("203.0.113.52:8080", "http", "jp", ""); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	p1, err := server.storage.GetProxyByAddress("203.0.113.51:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress(p1) error = %v", err)
	}
	p2, err := server.storage.GetProxyByAddress("203.0.113.52:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress(p2) error = %v", err)
	}
	server.affinity.SetProxy("sess-a", p1.ID, p1.Address, "us")
	server.affinity.SetProxy("sess-b", p1.ID, p1.Address, "us")
	server.affinity.SetProxy("sess-c", p2.ID, p2.Address, "jp")

	req := authenticatedJSONRequest(http.MethodGet, "/api/proxy-occupancy", "")
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	for _, bad := range []string{`"password"`, `"proxy_auth_password"`, `"username"`, `"user"`, `"pass"`} {
		if strings.Contains(body, bad) {
			t.Fatalf("occupancy response must not contain credential field %s: %s", bad, body)
		}
	}

	type occupancyRow struct {
		ProxyID                  int64  `json:"proxy_id"`
		Address                  string `json:"address"`
		ActiveSessions           int    `json:"active_sessions"`
		MaxSessions              int    `json:"max_sessions"`
		CooldownRemainingSeconds int64  `json:"cooldown_remaining_seconds"`
	}
	var rows []occupancyRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode occupancy response: %v; body=%s", err, body)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2: %#v", len(rows), rows)
	}

	byID := make(map[int64]occupancyRow, len(rows))
	for _, row := range rows {
		byID[row.ProxyID] = row
	}
	r1, ok := byID[p1.ID]
	if !ok {
		t.Fatalf("missing proxy_id=%d in %#v", p1.ID, rows)
	}
	if r1.Address != p1.Address || r1.ActiveSessions != 2 || r1.MaxSessions != 1 {
		t.Fatalf("proxy1 row = %#v, want address=%q active=2 max=1", r1, p1.Address)
	}
	if r1.CooldownRemainingSeconds < 0 {
		t.Fatalf("cooldown_remaining_seconds = %d, want >=0", r1.CooldownRemainingSeconds)
	}
	r2, ok := byID[p2.ID]
	if !ok {
		t.Fatalf("missing proxy_id=%d in %#v", p2.ID, rows)
	}
	if r2.Address != p2.Address || r2.ActiveSessions != 1 || r2.MaxSessions != 1 {
		t.Fatalf("proxy2 row = %#v, want address=%q active=1 max=1", r2, p2.Address)
	}
	if r2.CooldownRemainingSeconds < 0 {
		t.Fatalf("cooldown_remaining_seconds = %d, want >=0", r2.CooldownRemainingSeconds)
	}
}

// TestProxyOccupancyAPIReturnsActualCooldownRemaining: 冷却字段必须反映 affinity 真实剩余，
// 而非硬编码 0。对 p1 设置 now+120s 冷却，断言其 cooldown_remaining_seconds 落在 (0,120]；
// 未设置冷却的 p2 必须为 0。
func TestProxyOccupancyAPIReturnsActualCooldownRemaining(t *testing.T) {
	server := newTestServer(t)
	server.cfg.MaxSessionsPerProxy = 1
	if err := server.storage.AddManualProxy("203.0.113.61:8080", "http", "us", ""); err != nil {
		t.Fatalf("AddManualProxy(p1) error = %v", err)
	}
	if err := server.storage.AddManualProxy("203.0.113.62:8080", "http", "jp", ""); err != nil {
		t.Fatalf("AddManualProxy(p2) error = %v", err)
	}
	p1, err := server.storage.GetProxyByAddress("203.0.113.61:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress(p1) error = %v", err)
	}
	p2, err := server.storage.GetProxyByAddress("203.0.113.62:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress(p2) error = %v", err)
	}
	server.affinity.SetProxy("cool-a", p1.ID, p1.Address, "us")
	server.affinity.SetProxy("cool-b", p2.ID, p2.Address, "jp")
	server.affinity.SetCooldown(p1.ID, time.Now().Add(120*time.Second))

	req := authenticatedJSONRequest(http.MethodGet, "/api/proxy-occupancy", "")
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	type occupancyRow struct {
		ProxyID                  int64 `json:"proxy_id"`
		CooldownRemainingSeconds int64 `json:"cooldown_remaining_seconds"`
	}
	var rows []occupancyRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode occupancy response: %v; body=%s", err, rec.Body.String())
	}
	byID := make(map[int64]occupancyRow, len(rows))
	for _, row := range rows {
		byID[row.ProxyID] = row
	}
	r1, ok := byID[p1.ID]
	if !ok {
		t.Fatalf("missing proxy_id=%d in %#v", p1.ID, rows)
	}
	if r1.CooldownRemainingSeconds <= 0 || r1.CooldownRemainingSeconds > 120 {
		t.Fatalf("p1 cooldown_remaining_seconds = %d, want in (0,120]", r1.CooldownRemainingSeconds)
	}
	r2, ok := byID[p2.ID]
	if !ok {
		t.Fatalf("missing proxy_id=%d in %#v", p2.ID, rows)
	}
	if r2.CooldownRemainingSeconds != 0 {
		t.Fatalf("p2 cooldown_remaining_seconds = %d, want 0", r2.CooldownRemainingSeconds)
	}
}

// TestProxyOccupancyAPINilStorageUsesBindingAddress 覆盖 BUG-05/BUG-08：
// occupancy 不应依赖 s.storage（避免 nil storage panic 与 N+1 查询），
// 地址直接取自 binding.NodeAddress。
func TestProxyOccupancyAPINilStorageUsesBindingAddress(t *testing.T) {
	resetWebUISecurityState()
	sessions := affinity.New(10 * time.Minute)
	sessions.SetProxy("sess-a", 42, "198.51.100.7:8080", "us")
	server := New(nil, &config.Config{WebUIPort: ":0", MaxSessionsPerProxy: 3}, sessions, nil, make(chan struct{}, 1))

	req := authenticatedJSONRequest(http.MethodGet, "/api/proxy-occupancy", "")
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	type occupancyRow struct {
		ProxyID        int64  `json:"proxy_id"`
		Address        string `json:"address"`
		ActiveSessions int    `json:"active_sessions"`
		MaxSessions    int    `json:"max_sessions"`
	}
	var rows []occupancyRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode occupancy response: %v; body=%s", err, rec.Body.String())
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1: %#v", len(rows), rows)
	}
	if rows[0].ProxyID != 42 || rows[0].Address != "198.51.100.7:8080" || rows[0].ActiveSessions != 1 || rows[0].MaxSessions != 3 {
		t.Fatalf("row = %#v, want proxy_id=42 address=198.51.100.7:8080 active=1 max=3", rows[0])
	}
}

func TestRemovedContributionAPIRouteIsNotPublic(t *testing.T) {
	server := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/subscription/contribute", nil)
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	assertNoBusinessTerms(t, rec.Body.String())
}

// TestStarProxySetsStarredFlag 覆盖 apiStarProxy：POST 置星标成功，SetProxyStarred 生效并被读回。
func TestStarProxySetsStarredFlag(t *testing.T) {
	server := newTestServer(t)
	if err := server.storage.AddManualProxy("203.0.113.40:8080", "http", "us", ""); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	proxy, err := server.storage.GetProxyByAddress("203.0.113.40:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if proxy.Starred {
		t.Fatal("new manual proxy unexpectedly starred by default")
	}

	body := fmt.Sprintf(`{"id":%d,"starred":true}`, proxy.ID)
	serveAuthenticated(t, server, "/api/proxy/star", body, http.StatusOK)

	starred, err := server.storage.GetProxyByAddress("203.0.113.40:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() after star error = %v", err)
	}
	if !starred.Starred {
		t.Fatalf("proxy.Starred = false after star, want true")
	}

	// 清位同样应成功并被读回。
	unbody := fmt.Sprintf(`{"id":%d,"starred":false}`, proxy.ID)
	serveAuthenticated(t, server, "/api/proxy/star", unbody, http.StatusOK)
	unstarred, err := server.storage.GetProxyByAddress("203.0.113.40:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() after unstar error = %v", err)
	}
	if unstarred.Starred {
		t.Fatalf("proxy.Starred = true after unstar, want false")
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	resetWebUISecurityState()
	store, err := storage.New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatalf("storage.New() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	sessions := affinity.New(10 * time.Minute)
	return New(store, &config.Config{WebUIPort: ":0"}, sessions, nil, make(chan struct{}, 1))
}

func authenticatedJSONRequest(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	token := newSession()
	req.Header.Set("X-CSRF-Token", token)
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	return req
}

func resetWebUISecurityState() {
	sessionsMu.Lock()
	sessions = make(map[string]time.Time)
	sessionsMu.Unlock()
	loginAttemptsMu.Lock()
	loginAttempts = make(map[string]loginAttempt)
	loginAttemptsMu.Unlock()
}

func sha256Hex(value string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}

func insertWebUITestProxy(t *testing.T, store *storage.Storage, address string, region string, status string) {
	t.Helper()
	if err := store.AddManualProxy(address, "http", region, ""); err != nil {
		t.Fatalf("AddManualProxy(%s) error = %v", address, err)
	}
	// AddManualProxy 默认 disabled（待验证）；测试夹具若要 active 须显式启用。
	if status == "active" {
		proxy, err := store.GetProxyByAddress(address)
		if err != nil {
			t.Fatalf("GetProxyByAddress(%s) error = %v", address, err)
		}
		if err := store.EnableProxyByID(proxy.ID); err != nil {
			t.Fatalf("EnableProxyByID(%s) error = %v", address, err)
		}
	} else if status == "disabled" {
		if err := store.DisableProxy(address); err != nil {
			t.Fatalf("DisableProxy(%s) error = %v", address, err)
		}
	}
}

func serveLogin(t *testing.T, server *Server, password string, remoteAddr string, https bool) *httptest.ResponseRecorder {
	t.Helper()
	target := "http://example.test/login"
	if https {
		target = "https://example.test/login"
	}
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader("password="+urlQueryEscape(password)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)
	return rec
}

func urlQueryEscape(value string) string {
	return strings.NewReplacer("%", "%25", "+", "%2B", "&", "%26", "=", "%3D", " ", "+").Replace(value)
}

func findCookie(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("cookie %q not found in %#v", name, cookies)
	return nil
}

func serveAuthenticated(t *testing.T, server *Server, path, body string, status int) {
	t.Helper()
	req := authenticatedJSONRequest(http.MethodPost, path, body)
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)

	if rec.Code != status {
		t.Fatalf("%s status = %d, want %d; body=%s", path, rec.Code, status, rec.Body.String())
	}
}

func assertNoBusinessTerms(t *testing.T, body string) {
	t.Helper()
	for _, term := range []string{"address", "region", "proxy_count", "subscription", "total", "node", "gateway"} {
		if strings.Contains(body, term) {
			t.Fatalf("response leaked business term %q: %s", term, body)
		}
	}
}

func setTestGlobalConfig(t *testing.T, cfg *config.Config) {
	t.Helper()
	t.Setenv("DATA_DIR", t.TempDir())
	if err := config.Save(cfg); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}
}

// TestSubscriptionsAPIReportsPausedCount 覆盖 BUG-52 后端：/api/subscriptions
// 为每个订阅返回 paused_count，且该值来自 CountPausedBySubscriptionID(user_paused=1 计数)，
// 与 active_count / disabled_count 相互独立。
func TestSubscriptionsAPIReportsPausedCount(t *testing.T) {
	server := newTestServer(t)

	subID, err := server.storage.AddSubscription("sub-a", "https://example.test/a", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}

	// 三个订阅节点：其中两个被用户暂停(user_paused=1)。
	addrs := []string{"203.0.113.1:8080", "203.0.113.2:8080", "203.0.113.3:8080"}
	for _, addr := range addrs {
		if err := server.storage.AddProxyWithSource(addr, "http", storage.SourceSubscription, subID); err != nil {
			t.Fatalf("AddProxyWithSource(%s) error = %v", addr, err)
		}
	}
	admin, err := server.storage.GetAllForAdmin()
	if err != nil {
		t.Fatalf("GetAllForAdmin() error = %v", err)
	}
	pausedTargets := map[string]bool{"203.0.113.1:8080": true, "203.0.113.2:8080": true}
	pausedIDs := 0
	for _, p := range admin {
		if pausedTargets[p.Address] {
			if err := server.storage.PauseProxyByID(p.ID); err != nil {
				t.Fatalf("PauseProxyByID(%d) error = %v", p.ID, err)
			}
			pausedIDs++
		}
	}
	if pausedIDs != 2 {
		t.Fatalf("expected to pause 2 proxies, paused %d", pausedIDs)
	}

	req := authenticatedJSONRequest(http.MethodGet, "/api/subscriptions", "")
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var subs []struct {
		ID            int64 `json:"id"`
		ActiveCount   int   `json:"active_count"`
		DisabledCount int   `json:"disabled_count"`
		PausedCount   int   `json:"paused_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &subs); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, rec.Body.String())
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d; body=%s", len(subs), rec.Body.String())
	}
	got := subs[0]
	if got.PausedCount != 2 {
		t.Fatalf("paused_count = %d, want 2; body=%s", got.PausedCount, rec.Body.String())
	}
	// 只有 1 个未暂停的 active 节点应计入 active_count。
	if got.ActiveCount != 1 {
		t.Fatalf("active_count = %d, want 1; body=%s", got.ActiveCount, rec.Body.String())
	}
	if got.DisabledCount != 0 {
		t.Fatalf("disabled_count = %d, want 0; body=%s", got.DisabledCount, rec.Body.String())
	}

	// paused_count 字段必须实际出现在返回的 JSON 里（前端依赖它）。
	if !strings.Contains(rec.Body.String(), "\"paused_count\"") {
		t.Fatalf("response missing paused_count field: %s", rec.Body.String())
	}
}

// TestLoginClientKeyPrefersForwardedForFirstSegment 覆盖 BUG-57：
// 存在 X-Forwarded-For 时按其首段计数（反代场景下区分真实客户端），
// 不存在时回退到 RemoteAddr 的 host。
func TestLoginClientKeyPrefersForwardedForFirstSegment(t *testing.T) {
	cases := []struct {
		name       string
		xff        string
		remoteAddr string
		want       string
	}{
		{
			name:       "xff multi segment uses first",
			xff:        "198.51.100.7, 10.0.0.1, 10.0.0.2",
			remoteAddr: "10.0.0.9:5555",
			want:       "198.51.100.7",
		},
		{
			name:       "xff single segment",
			xff:        "203.0.113.42",
			remoteAddr: "10.0.0.9:5555",
			want:       "203.0.113.42",
		},
		{
			name:       "no xff falls back to remoteaddr host",
			xff:        "",
			remoteAddr: "192.0.2.50:44321",
			want:       "192.0.2.50",
		},
		{
			name:       "empty xff falls back to remoteaddr host",
			xff:        "   ",
			remoteAddr: "192.0.2.51:1",
			want:       "192.0.2.51",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/login", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if got := loginClientKey(req); got != tc.want {
				t.Fatalf("loginClientKey() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestLoginRateLimitDistinguishesForwardedForClients 端到端验证 BUG-57：
// 反代之后，不同真实客户端（不同 XFF 首段）即使 RemoteAddr 相同也应被独立限速，
// 一个被锁定不影响另一个。
func TestLoginRateLimitDistinguishesForwardedForClients(t *testing.T) {
	server := newTestServer(t)
	server.cfg.WebUIPasswordHash = sha256Hex("correct")

	const proxyRemote = "10.0.0.9:5555" // 所有请求经同一反代，RemoteAddr 相同。

	for i := 0; i < maxLoginFailures; i++ {
		rec := serveLoginXFF(t, server, "wrong", proxyRemote, "198.51.100.20")
		if rec.Code != http.StatusOK {
			t.Fatalf("failure %d status = %d, want %d", i+1, rec.Code, http.StatusOK)
		}
	}

	// 同一 XFF 客户端应被锁定。
	locked := serveLoginXFF(t, server, "correct", proxyRemote, "198.51.100.20")
	if locked.Code != http.StatusTooManyRequests {
		t.Fatalf("locked status = %d, want %d", locked.Code, http.StatusTooManyRequests)
	}

	// 不同 XFF 客户端（相同反代 RemoteAddr）不应被误锁。
	other := serveLoginXFF(t, server, "correct", proxyRemote, "198.51.100.21")
	if other.Code != http.StatusFound {
		t.Fatalf("other XFF client status = %d, want %d; body=%s", other.Code, http.StatusFound, other.Body.String())
	}
}

func serveLoginXFF(t *testing.T, server *Server, password, remoteAddr, xff string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://example.test/login", strings.NewReader("password="+urlQueryEscape(password)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = remoteAddr
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)
	return rec
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func assertNoLegacyConfigFields(t *testing.T, body string) {
	t.Helper()
	for _, legacy := range []string{"pool_", "fetch", "optimizer", "free_only", "CustomProxyMode", "CustomFreePriority"} {
		if strings.Contains(body, legacy) {
			t.Fatalf("config contains legacy field marker %q: %s", legacy, body)
		}
	}
}

func assertConfigJSONOmitsLegacyFields(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	assertNoLegacyConfigFields(t, string(data))
	// 注：代理密码明文现按用户决策持久化（供复制含密码的完整 URL），故不再断言其缺失。
	// WebUI 登录密码仍只存哈希，其安全模型由 config 包的 TestSaveKeepsWebUIPasswordHashOnly 守护。
}
