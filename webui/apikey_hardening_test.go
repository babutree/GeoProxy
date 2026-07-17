package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/babutree/GeoProxy/config"
)

// TestAPIKeyCreateRejectsEmptyName verifies that creating an API key with an
// empty or whitespace-only name must be rejected with HTTP 400 BEFORE any key
// is generated or saved. Positive control (valid name) is covered by
// TestAPIKeyCreateReturnsPlaintextOnceAndStoresHashOnly.
func TestAPIKeyCreateRejectsEmptyName(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"empty_string", `{"name":""}`},
		{"missing_field", `{}`},
		{"whitespace_only", `{"name":"   "}`},
		{"tabs_and_newlines", "{\"name\":\" \\t\\n \"}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newTestServer(t)
			setTestGlobalConfig(t, server.cfg)

			req := authenticatedJSONRequest(http.MethodPost, "/api/apikey/create", tc.body)
			rec := httptest.NewRecorder()
			server.routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}

			// No key must have been generated or persisted on the reject path.
			if got := len(config.Get().ReadOnlyAPIKeys); got != 0 {
				t.Fatalf("keys persisted on empty-name reject = %d, want 0", got)
			}
			if got := len(server.cfg.ReadOnlyAPIKeys); got != 0 {
				t.Fatalf("live keys mutated on empty-name reject = %d, want 0", got)
			}

			// Error body must not echo any generated plaintext secret; it is a
			// fixed validation message only.
			body := rec.Body.String()
			var payload map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode error body: %v body=%s", err, body)
			}
			if _, ok := payload["error"]; !ok {
				t.Fatalf("error response missing 'error' field: %s", body)
			}
			if _, ok := payload["key"]; ok {
				t.Fatalf("error response leaked a key field: %s", body)
			}
		})
	}
}

// TestAPIKeyCreateAcceptsValidNameAfterReject is the positive counterpart:
// after a rejected empty-name attempt, a valid name still succeeds and stores
// exactly one hashed key.
func TestAPIKeyCreateAcceptsValidNameAfterReject(t *testing.T) {
	server := newTestServer(t)
	setTestGlobalConfig(t, server.cfg)

	// Reject first.
	rej := authenticatedJSONRequest(http.MethodPost, "/api/apikey/create", `{"name":"  "}`)
	recRej := httptest.NewRecorder()
	server.routes().ServeHTTP(recRej, rej)
	if recRej.Code != http.StatusBadRequest {
		t.Fatalf("empty-name status = %d, want 400", recRej.Code)
	}

	// Then a valid create.
	ok := authenticatedJSONRequest(http.MethodPost, "/api/apikey/create", `{"name":"prod"}`)
	recOK := httptest.NewRecorder()
	server.routes().ServeHTTP(recOK, ok)
	if recOK.Code != http.StatusOK {
		t.Fatalf("valid create status = %d, want 200; body=%s", recOK.Code, recOK.Body.String())
	}
	if got := len(config.Get().ReadOnlyAPIKeys); got != 1 {
		t.Fatalf("keys after valid create = %d, want 1", got)
	}
}

// TestWebUIAPIKeyHashUsesConfigCanonical proves the webui hashing seam and the
// config canonical helper produce identical output.
func TestWebUIAPIKeyHashUsesConfigCanonical(t *testing.T) {
	for _, plain := range []string{"abc", "another-secret", "  spaced  "} {
		if apiKeySHA256(plain) != config.HashAPIKey(plain) {
			t.Fatalf("apiKeySHA256(%q)=%q != config.HashAPIKey=%q",
				plain, apiKeySHA256(plain), config.HashAPIKey(plain))
		}
	}
	// Adversarial: distinct inputs must not collide.
	if apiKeySHA256("x") == apiKeySHA256("y") {
		t.Fatal("distinct plaintext collided in apiKeySHA256")
	}
}

// TestLegacyBareSHA256KeyStillAuthenticates is the mandatory backward-compat
// proof: a key persisted as a bare SHA-256 hex digest (the pre-change
// scheme) must still authenticate through the read-only API middleware after the
// hash centralization change. The hash is a fixed known-answer vector, NOT
// produced by the code under test, so this test would fail if the scheme drifted.
func TestLegacyBareSHA256KeyStillAuthenticates(t *testing.T) {
	const (
		plain      = "legacy-persisted-key"
		legacyHash = "cdc5c4494fbd5368953db8ca0be17cfb1120e32399d1c2ae710dc873adcba7de"
	)
	server := newReadOnlyAPITestServer(t, []config.APIKey{{
		ID:   "k-legacy",
		Name: "legacy",
		Hash: legacyHash,
	}}, 60)

	// Positive: the legacy key authenticates.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy bare-SHA256 key rejected: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// Negative: a wrong key against the same legacy hash is rejected.
	bad := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	bad.Header.Set("Authorization", "Bearer not-the-legacy-key")
	recBad := httptest.NewRecorder()
	server.routes().ServeHTTP(recBad, bad)
	if recBad.Code != http.StatusUnauthorized {
		t.Fatalf("wrong key accepted against legacy hash: status = %d, want 401", recBad.Code)
	}
}

// TestLegacyEnvImportedKeyPersistsAndAuthenticates proves an env-imported key
// (READONLY_API_KEYS) persisted as bare SHA-256 in config.json still validates
// after reload through config.ValidateReadOnlyAPIKey, and that the on-disk hash
// equals the canonical helper output (no divergent scheme).
func TestLegacyEnvImportedKeyPersistsAndAuthenticates(t *testing.T) {
	server := newTestServer(t)
	setTestGlobalConfig(t, server.cfg)

	plain := "env-style-legacy-key"
	// Simulate a previously persisted env-imported key: stored by hash only.
	server.cfg.ReadOnlyAPIKeys = []config.APIKey{{
		ID:   "env-import",
		Name: "env-import",
		Hash: config.HashAPIKey(plain),
	}}
	if err := config.Save(server.cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	raw, err := os.ReadFile(config.ConfigFile())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	disk := string(raw)
	if strings.Contains(disk, plain) {
		t.Fatalf("plaintext leaked to disk: %s", disk)
	}
	if !strings.Contains(disk, config.HashAPIKey(plain)) {
		t.Fatalf("config.json missing canonical hash for persisted key")
	}
	if !config.ValidateReadOnlyAPIKey(config.Get(), plain) {
		t.Fatal("persisted bare-SHA256 key failed validation after change")
	}
	if config.ValidateReadOnlyAPIKey(config.Get(), "wrong") {
		t.Fatal("wrong key validated against persisted hash")
	}
}
