package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/config"
)

func TestAPIKeyManageRequiresAuthentication(t *testing.T) {
	server := newTestServer(t)
	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/apikeys", ""},
		{http.MethodPost, "/api/apikey/create", `{"name":"x"}`},
		{http.MethodPost, "/api/apikey/revoke", `{"id":"k1"}`},
		{http.MethodPost, "/api/apikey/delete", `{"id":"k1"}`},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var req *http.Request
			if tc.body == "" {
				req = httptest.NewRequest(tc.method, tc.path, nil)
			} else {
				req = httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			server.routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
			}
		})
	}
}

func TestAPIKeyWriteRequiresCSRFProof(t *testing.T) {
	server := newTestServer(t)
	setTestGlobalConfig(t, server.cfg)
	token := newSession()

	req := httptest.NewRequest(http.MethodPost, "/api/apikey/create", strings.NewReader(`{"name":"n"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestAPIKeyManageRejectsAPIKeyAuth(t *testing.T) {
	server := newTestServer(t)
	plain := "ro-manage-bypass-key"
	server.cfg.ReadOnlyAPIKeys = []config.APIKey{{
		ID:   "k-bypass",
		Name: "bypass",
		Hash: sha256Hex(plain),
	}}
	setTestGlobalConfig(t, server.cfg)

	for _, path := range []string{"/api/apikeys", "/api/apikey/create"} {
		t.Run(path, func(t *testing.T) {
			var req *http.Request
			if path == "/api/apikeys" {
				req = httptest.NewRequest(http.MethodGet, path, nil)
			} else {
				req = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"name":"x"}`))
				req.Header.Set("Content-Type", "application/json")
			}
			req.Header.Set("Authorization", "Bearer "+plain)
			req.Header.Set("X-API-Key", plain)
			rec := httptest.NewRecorder()
			server.routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d (API key must not manage keys); body=%s",
					rec.Code, http.StatusUnauthorized, rec.Body.String())
			}
		})
	}
}

func TestAPIKeyCreateReturnsPlaintextOnceAndStoresHashOnly(t *testing.T) {
	server := newTestServer(t)
	setTestGlobalConfig(t, server.cfg)

	req := authenticatedJSONRequest(http.MethodPost, "/api/apikey/create", `{"name":" primary "}`)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v body=%s", err, rec.Body.String())
	}
	plain, _ := created["key"].(string)
	id, _ := created["id"].(string)
	name, _ := created["name"].(string)
	if plain == "" || id == "" {
		t.Fatalf("create response missing key/id: %#v", created)
	}
	if name != "primary" {
		t.Fatalf("name = %q, want primary", name)
	}
	if _, ok := created["hash"]; ok {
		t.Fatalf("create response must not include hash: %#v", created)
	}
	if !config.ValidateReadOnlyAPIKey(config.Get(), plain) {
		t.Fatal("created key failed ValidateReadOnlyAPIKey")
	}

	raw, err := os.ReadFile(config.ConfigFile())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	disk := string(raw)
	if strings.Contains(disk, plain) {
		t.Fatalf("plaintext key leaked into config.json: %s", disk)
	}
	if !strings.Contains(disk, sha256Hex(plain)) {
		t.Fatalf("config.json missing key hash for created key: %s", disk)
	}
}

func TestAPIKeyListOmitsPlaintextAndHash(t *testing.T) {
	server := newTestServer(t)
	plain := "list-secret-plain-xyz"
	createdAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	lastUsed := time.Date(2026, 7, 2, 8, 30, 0, 0, time.UTC)
	server.cfg.ReadOnlyAPIKeys = []config.APIKey{{
		ID:         "k-list",
		Name:       "listed",
		Hash:       sha256Hex(plain),
		CreatedAt:  createdAt,
		LastUsedAt: lastUsed,
		Disabled:   false,
	}}
	setTestGlobalConfig(t, server.cfg)

	req := authenticatedJSONRequest(http.MethodGet, "/api/apikeys", "")
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, plain) {
		t.Fatalf("list leaked plaintext: %s", body)
	}
	if strings.Contains(body, sha256Hex(plain)) || strings.Contains(body, `"hash"`) {
		t.Fatalf("list leaked hash: %s", body)
	}

	var payload struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		// also accept bare array
		var arr []map[string]any
		if err2 := json.Unmarshal(rec.Body.Bytes(), &arr); err2 != nil {
			t.Fatalf("decode list: %v / %v body=%s", err, err2, body)
		}
		payload.Keys = arr
	}
	if len(payload.Keys) != 1 {
		t.Fatalf("keys len = %d, want 1; body=%s", len(payload.Keys), body)
	}
	k := payload.Keys[0]
	if k["id"] != "k-list" || k["name"] != "listed" {
		t.Fatalf("key meta = %#v", k)
	}
	if _, ok := k["disabled"]; !ok {
		t.Fatalf("missing disabled: %#v", k)
	}
	if _, ok := k["created_at"]; !ok {
		t.Fatalf("missing created_at: %#v", k)
	}
	if _, ok := k["last_used_at"]; !ok {
		t.Fatalf("missing last_used_at: %#v", k)
	}
	if _, ok := k["hash"]; ok {
		t.Fatalf("hash present in list item: %#v", k)
	}
	if _, ok := k["key"]; ok {
		t.Fatalf("plaintext key present in list item: %#v", k)
	}
}

func TestAPIKeyRevokeDisablesValidation(t *testing.T) {
	server := newTestServer(t)
	plain := "revoke-me-plain"
	server.cfg.ReadOnlyAPIKeys = []config.APIKey{{
		ID:   "k-revoke",
		Name: "to-revoke",
		Hash: sha256Hex(plain),
	}}
	setTestGlobalConfig(t, server.cfg)
	if !config.ValidateReadOnlyAPIKey(config.Get(), plain) {
		t.Fatal("precondition: key should validate before revoke")
	}

	serveAuthenticated(t, server, "/api/apikey/revoke", `{"id":"k-revoke"}`, http.StatusOK)

	if config.ValidateReadOnlyAPIKey(config.Get(), plain) {
		t.Fatal("ValidateReadOnlyAPIKey still true after revoke")
	}
	found := false
	for _, k := range config.Get().ReadOnlyAPIKeys {
		if k.ID == "k-revoke" {
			found = true
			if !k.Disabled {
				t.Fatalf("key not disabled after revoke: %#v", k)
			}
		}
	}
	if !found {
		t.Fatal("revoked key missing from config")
	}

	raw, err := os.ReadFile(config.ConfigFile())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(raw), plain) {
		t.Fatalf("plaintext leaked after revoke: %s", raw)
	}
}

func TestAPIKeyDeleteRemovesKey(t *testing.T) {
	server := newTestServer(t)
	plain := "delete-me-plain"
	server.cfg.ReadOnlyAPIKeys = []config.APIKey{
		{ID: "k-keep", Name: "keep", Hash: sha256Hex("keep-plain")},
		{ID: "k-del", Name: "del", Hash: sha256Hex(plain)},
	}
	setTestGlobalConfig(t, server.cfg)

	serveAuthenticated(t, server, "/api/apikey/delete", `{"id":"k-del"}`, http.StatusOK)

	got := config.Get().ReadOnlyAPIKeys
	if len(got) != 1 || got[0].ID != "k-keep" {
		t.Fatalf("after delete keys = %#v, want only k-keep", got)
	}
	if config.ValidateReadOnlyAPIKey(config.Get(), plain) {
		t.Fatal("deleted key still validates")
	}
}

func TestAPIKeyConfigSaveFailureKeepsLiveConfig(t *testing.T) {
	server := newTestServer(t)
	plain := "save-failure-plain"
	server.cfg.ReadOnlyAPIKeys = []config.APIKey{{
		ID:   "k-fail",
		Name: "original",
		Hash: sha256Hex(plain),
	}}
	setTestGlobalConfig(t, server.cfg)
	origSave := configSave
	t.Cleanup(func() { configSave = origSave })
	configSave = func(*config.Config) error {
		return fmt.Errorf("forced api key save failure")
	}

	serveAuthenticated(t, server, "/api/apikey/create", `{"name":"new"}`, http.StatusInternalServerError)
	if got := server.cfg.ReadOnlyAPIKeys; len(got) != 1 || got[0].ID != "k-fail" || got[0].Disabled {
		t.Fatalf("live config changed after failed create: %#v", got)
	}
	if got := config.Get().ReadOnlyAPIKeys; len(got) != 1 || got[0].ID != "k-fail" || got[0].Disabled {
		t.Fatalf("global config changed after failed create: %#v", got)
	}

	serveAuthenticated(t, server, "/api/apikey/revoke", `{"id":"k-fail"}`, http.StatusInternalServerError)
	if got := server.cfg.ReadOnlyAPIKeys; len(got) != 1 || got[0].ID != "k-fail" || got[0].Disabled {
		t.Fatalf("live config changed after failed revoke: %#v", got)
	}
	if got := config.Get().ReadOnlyAPIKeys; len(got) != 1 || got[0].ID != "k-fail" || got[0].Disabled {
		t.Fatalf("global config changed after failed revoke: %#v", got)
	}

	serveAuthenticated(t, server, "/api/apikey/delete", `{"id":"k-fail"}`, http.StatusInternalServerError)
	if got := server.cfg.ReadOnlyAPIKeys; len(got) != 1 || got[0].ID != "k-fail" || got[0].Disabled {
		t.Fatalf("live config changed after failed delete: %#v", got)
	}
	if got := config.Get().ReadOnlyAPIKeys; len(got) != 1 || got[0].ID != "k-fail" || got[0].Disabled {
		t.Fatalf("global config changed after failed delete: %#v", got)
	}
}

func TestConfigSaveCannotRestoreConcurrentlyRevokedAPIKey(t *testing.T) {
	server := newTestServer(t)
	server.cfg.ReadOnlyAPIKeys = []config.APIKey{{
		ID: "k-race", Name: "race", Hash: sha256Hex("race-key"),
	}}
	setTestGlobalConfig(t, server.cfg)

	originalSave := configSave
	t.Cleanup(func() { configSave = originalSave })
	firstSaveStarted := make(chan struct{})
	releaseFirstSave := make(chan struct{})
	revokeSaveStarted := make(chan struct{})
	var saves atomic.Int32
	configSave = func(*config.Config) error {
		switch saves.Add(1) {
		case 1:
			close(firstSaveStarted)
			<-releaseFirstSave
		case 2:
			close(revokeSaveStarted)
		}
		return nil
	}

	configDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/api/config/save", strings.NewReader(`{"proxy_auth_enabled":true,"proxy_auth_username":"username","proxy_auth_password":"","session_ttl_minutes":10,"default_region":"","health_check_interval":5,"max_retry":3,"singbox_path":"sing-box","allowed_countries":[],"blocked_countries":[]}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		server.apiConfigSave(rec, req)
		configDone <- rec
	}()
	<-firstSaveStarted

	revokeDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/api/apikey/revoke", strings.NewReader(`{"id":"k-race"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		server.apiAPIKeyRevoke(rec, req)
		revokeDone <- rec
	}()

	// 配置保存和 API Key 吊销必须覆盖同一个“读取、持久化、发布”临界区。
	// 旧实现会在首个配置保存尚未发布时执行第二次保存，随后被旧快照覆盖。
	select {
	case <-revokeSaveStarted:
		close(releaseFirstSave)
		<-configDone
		<-revokeDone
		t.Fatal("api key revoke saved while config save was still in progress")
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseFirstSave)
	if rec := <-configDone; rec.Code != http.StatusOK {
		t.Fatalf("config save status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if rec := <-revokeDone; rec.Code != http.StatusOK {
		t.Fatalf("revoke status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	server.cfgMu.RLock()
	key := server.cfg.ReadOnlyAPIKeys[0]
	server.cfgMu.RUnlock()
	if !key.Disabled {
		t.Fatal("config save restored a concurrently revoked API key")
	}
}
