package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"goproxy/affinity"
	"goproxy/config"
	"goproxy/custom"
	"goproxy/storage"
	"goproxy/validator"
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
	wantKeys := []string{
		"allowed_countries", "blocked_countries", "default_region", "health_check_interval", "http_port", "max_retry",
		"proxy_auth_enabled", "proxy_auth_username", "session_ttl_minutes", "singbox_path", "socks5_port", "webui_port",
	}
	if !reflect.DeepEqual(sortedKeys(got), wantKeys) {
		t.Fatalf("config keys = %#v, want %#v; body=%s", sortedKeys(got), wantKeys, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret") || strings.Contains(rec.Body.String(), "proxy_auth_password") {
		t.Fatalf("config response leaked password: %s", rec.Body.String())
	}
	assertNoLegacyConfigFields(t, rec.Body.String())
}

func TestConfigSavePersistsActiveEditableFields(t *testing.T) {
	server := newTestServer(t)
	server.cfg.ProxyAuthPassword = "old-secret"
	server.cfg.ProxyAuthPasswordHash = "old-hash"
	setTestGlobalConfig(t, server.cfg)
	payload := `{"proxy_auth_enabled":true,"proxy_auth_username":"edge","proxy_auth_password":"","session_ttl_minutes":25,"default_region":"us","health_check_interval":8,"max_retry":0,"singbox_path":"D:/tools/sing-box.exe","allowed_countries":["US","JP"],"blocked_countries":["CN"]}`

	serveAuthenticated(t, server, "/api/config/save", payload, http.StatusOK)

	if server.cfg.ProxyAuthEnabled != true || server.cfg.ProxyAuthUsername != "edge" {
		t.Fatalf("auth config = enabled:%v username:%q", server.cfg.ProxyAuthEnabled, server.cfg.ProxyAuthUsername)
	}
	if server.cfg.ProxyAuthPassword != "old-secret" || server.cfg.ProxyAuthPasswordHash != "old-hash" {
		t.Fatalf("empty password changed stored password/hash: %q/%q", server.cfg.ProxyAuthPassword, server.cfg.ProxyAuthPasswordHash)
	}
	if server.cfg.SessionTTLMinutes != 25 || server.cfg.DefaultRegion != "us" || server.cfg.HealthIntervalMinutes != 8 || server.cfg.MaxRetry != 0 {
		t.Fatalf("runtime config = ttl:%d region:%q health:%d retry:%d", server.cfg.SessionTTLMinutes, server.cfg.DefaultRegion, server.cfg.HealthIntervalMinutes, server.cfg.MaxRetry)
	}
	if server.cfg.SingBoxPath != "D:/tools/sing-box.exe" {
		t.Fatalf("SingBoxPath = %q", server.cfg.SingBoxPath)
	}
	if !reflect.DeepEqual(server.cfg.AllowedCountries, []string{"US", "JP"}) || !reflect.DeepEqual(server.cfg.BlockedCountries, []string{"CN"}) {
		t.Fatalf("countries = allowed:%#v blocked:%#v", server.cfg.AllowedCountries, server.cfg.BlockedCountries)
	}
	assertConfigJSONOmitsLegacyFields(t, config.ConfigFile())
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

func TestManualNodeRejectsSubscriptionSourceMutations(t *testing.T) {
	server := newTestServer(t)
	if err := server.storage.AddProxyWithSource("198.51.100.10:8080", "http", storage.SourceSubscription); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}

	for _, tc := range []struct {
		name string
		path string
		body string
	}{
		{"region", "/api/manual-node/region", `{"address":"198.51.100.10:8080","region":"jp"}`},
		{"note", "/api/manual-node/note", `{"address":"198.51.100.10:8080","note":"blocked"}`},
		{"delete", "/api/manual-node/delete", `{"address":"198.51.100.10:8080"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := authenticatedJSONRequest(http.MethodPost, tc.path, tc.body)
			rec := httptest.NewRecorder()

			server.routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
			}
		})
	}

	proxy, err := server.storage.GetProxyByAddress("198.51.100.10:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if proxy.Source != storage.SourceSubscription {
		t.Fatalf("source = %q, want %q", proxy.Source, storage.SourceSubscription)
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
	if !strings.Contains(body, "Admin Sign In") {
		t.Fatalf("body does not contain neutral login title: %s", body)
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
		Node                string `json:"node"`
		Region              string `json:"region"`
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
	if rows[0].RemainingTTLSeconds <= 0 || rows[0].RemainingTTLSeconds > int64((10*time.Minute).Seconds()) {
		t.Fatalf("remaining_ttl_seconds = %d, want within ttl", rows[0].RemainingTTLSeconds)
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

func newTestServer(t *testing.T) *Server {
	t.Helper()
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
	req.AddCookie(&http.Cookie{Name: "session", Value: newSession()})
	return req
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
}
