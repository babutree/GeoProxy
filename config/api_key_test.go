package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDefaultReadOnlyAPIRatePerMinIs60(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("DATA_DIR", t.TempDir())
	cfg := DefaultConfig()
	if cfg.ReadOnlyAPIRatePerMin != 60 {
		t.Fatalf("ReadOnlyAPIRatePerMin = %d, want 60", cfg.ReadOnlyAPIRatePerMin)
	}
	if len(cfg.ReadOnlyAPIKeys) != 0 {
		t.Fatalf("ReadOnlyAPIKeys = %#v, want empty", cfg.ReadOnlyAPIKeys)
	}
	if cfg.PublicHost != "" {
		t.Fatalf("PublicHost = %q, want empty", cfg.PublicHost)
	}
}

func TestReadOnlyAPIKeyHashRoundTrip(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("DATA_DIR", t.TempDir())

	plain := "ro-key-secret-plain-abc"
	cfg := Load()
	cfg.PublicHost = "api.example.com"
	cfg.ReadOnlyAPIRatePerMin = 120
	cfg.ReadOnlyAPIKeys = []APIKey{{
		ID:        "k1",
		Name:      "primary",
		Hash:      passwordHash(plain),
		CreatedAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Disabled:  false,
	}}

	if err := Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	data, err := os.ReadFile(ConfigFile())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	raw := string(data)
	if strings.Contains(raw, plain) {
		t.Fatalf("plain API key leaked into config.json: %s", raw)
	}
	if !strings.Contains(raw, passwordHash(plain)) {
		t.Fatalf("config.json missing key hash: %s", raw)
	}

	t.Setenv("READONLY_API_KEYS", "")
	t.Setenv("PUBLIC_HOST", "")
	t.Setenv("READONLY_API_RATE_PER_MIN", "")
	reloaded := Load()
	if reloaded.PublicHost != "api.example.com" {
		t.Fatalf("PublicHost after reload = %q, want api.example.com", reloaded.PublicHost)
	}
	if reloaded.ReadOnlyAPIRatePerMin != 120 {
		t.Fatalf("ReadOnlyAPIRatePerMin after reload = %d, want 120", reloaded.ReadOnlyAPIRatePerMin)
	}
	if len(reloaded.ReadOnlyAPIKeys) != 1 {
		t.Fatalf("ReadOnlyAPIKeys len = %d, want 1", len(reloaded.ReadOnlyAPIKeys))
	}
	got := reloaded.ReadOnlyAPIKeys[0]
	if got.ID != "k1" || got.Name != "primary" || got.Hash != passwordHash(plain) || got.Disabled {
		t.Fatalf("reloaded key = %#v", got)
	}
	wantCreated := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if !got.CreatedAt.UTC().Equal(wantCreated) {
		t.Fatalf("CreatedAt = %v, want %v", got.CreatedAt, wantCreated)
	}

	if !ValidateReadOnlyAPIKey(reloaded, plain) {
		t.Fatal("ValidateReadOnlyAPIKey(valid) = false, want true")
	}
	if ValidateReadOnlyAPIKey(reloaded, "wrong-key") {
		t.Fatal("ValidateReadOnlyAPIKey(wrong) = true, want false")
	}
}

func TestReadOnlyAPIKeyDisabledFailsValidation(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("DATA_DIR", t.TempDir())

	plain := "disabled-key-plain"
	cfg := Load()
	cfg.ReadOnlyAPIKeys = []APIKey{{
		ID:       "k-disabled",
		Name:     "revoked",
		Hash:     passwordHash(plain),
		Disabled: true,
	}}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded := Load()
	if ValidateReadOnlyAPIKey(reloaded, plain) {
		t.Fatal("disabled key validated as true, want false")
	}
}

func TestReadOnlyAPIKeysEnvImportsAsHashOnly(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("DATA_DIR", t.TempDir())
	plain1 := "env-key-one"
	plain2 := "env-key-two"
	t.Setenv("READONLY_API_KEYS", plain1+","+plain2)
	t.Setenv("PUBLIC_HOST", "pub.example.net")
	t.Setenv("READONLY_API_RATE_PER_MIN", "90")

	cfg := Load()
	if cfg.PublicHost != "pub.example.net" {
		t.Fatalf("PublicHost from env = %q, want pub.example.net", cfg.PublicHost)
	}
	if cfg.ReadOnlyAPIRatePerMin != 90 {
		t.Fatalf("ReadOnlyAPIRatePerMin from env = %d, want 90", cfg.ReadOnlyAPIRatePerMin)
	}
	if len(cfg.ReadOnlyAPIKeys) != 2 {
		t.Fatalf("imported keys len = %d, want 2; keys=%#v", len(cfg.ReadOnlyAPIKeys), cfg.ReadOnlyAPIKeys)
	}
	if !ValidateReadOnlyAPIKey(cfg, plain1) || !ValidateReadOnlyAPIKey(cfg, plain2) {
		t.Fatalf("env-imported keys failed validation; keys=%#v", cfg.ReadOnlyAPIKeys)
	}

	data, err := os.ReadFile(ConfigFile())
	if err != nil {
		t.Fatalf("read config after import: %v", err)
	}
	raw := string(data)
	if strings.Contains(raw, plain1) || strings.Contains(raw, plain2) {
		t.Fatalf("plain env keys leaked into config.json: %s", raw)
	}
	if !strings.Contains(raw, passwordHash(plain1)) || !strings.Contains(raw, passwordHash(plain2)) {
		t.Fatalf("config.json missing env key hashes: %s", raw)
	}

	// Clear env; reloaded from disk must still validate via hash.
	t.Setenv("READONLY_API_KEYS", "")
	t.Setenv("PUBLIC_HOST", "")
	t.Setenv("READONLY_API_RATE_PER_MIN", "")
	reloaded := Load()
	if reloaded.PublicHost != "pub.example.net" {
		t.Fatalf("PublicHost after reload = %q", reloaded.PublicHost)
	}
	if reloaded.ReadOnlyAPIRatePerMin != 90 {
		t.Fatalf("ReadOnlyAPIRatePerMin after reload = %d", reloaded.ReadOnlyAPIRatePerMin)
	}
	if !ValidateReadOnlyAPIKey(reloaded, plain1) || !ValidateReadOnlyAPIKey(reloaded, plain2) {
		t.Fatalf("disk-persisted env keys failed validation; keys=%#v", reloaded.ReadOnlyAPIKeys)
	}
}

func TestExistingConfigIgnoresNewReadOnlyAPIKeysFromEnv(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("DATA_DIR", t.TempDir())

	plain := "already-stored-key"
	cfg := Load()
	cfg.ReadOnlyAPIKeys = []APIKey{{
		ID:   "existing",
		Name: "existing",
		Hash: passwordHash(plain),
	}}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	t.Setenv("READONLY_API_KEYS", plain+",new-key-xyz")
	reloaded := Load()
	if len(reloaded.ReadOnlyAPIKeys) != 1 {
		t.Fatalf("keys after reload = %d, want 1; %#v", len(reloaded.ReadOnlyAPIKeys), reloaded.ReadOnlyAPIKeys)
	}
	if !ValidateReadOnlyAPIKey(reloaded, plain) {
		t.Fatal("persisted key failed validation")
	}
	if ValidateReadOnlyAPIKey(reloaded, "new-key-xyz") {
		t.Fatal("new env key was imported into an existing config")
	}

	data, err := os.ReadFile(ConfigFile())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var parsed struct {
		Keys []APIKey `json:"readonly_api_keys"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json: %v", err)
	}
	seen := map[string]int{}
	for _, k := range parsed.Keys {
		seen[k.Hash]++
	}
	for h, n := range seen {
		if n != 1 {
			t.Fatalf("hash %s appears %d times", h, n)
		}
	}
	if seen[passwordHash("new-key-xyz")] != 0 {
		t.Fatal("new env key hash was persisted into existing config")
	}
}

func TestLoadExistingConfigDoesNotRestoreRevokedAPIKeyFromEnv(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("DATA_DIR", t.TempDir())

	const revokedKey = "revoked-env-key"
	t.Setenv("READONLY_API_KEYS", revokedKey)
	t.Setenv("PUBLIC_HOST", "bootstrap.example.com")
	t.Setenv("READONLY_API_RATE_PER_MIN", "90")
	bootstrapped := Load()
	if !ValidateReadOnlyAPIKey(bootstrapped, revokedKey) {
		t.Fatal("bootstrap key did not validate")
	}

	bootstrapped.ReadOnlyAPIKeys = nil
	bootstrapped.PublicHost = "saved.example.com"
	bootstrapped.ReadOnlyAPIRatePerMin = 120
	if err := Save(bootstrapped); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	t.Setenv("PUBLIC_HOST", "stale-env.example.com")
	t.Setenv("READONLY_API_RATE_PER_MIN", "30")
	reloaded := Load()
	if ValidateReadOnlyAPIKey(reloaded, revokedKey) {
		t.Fatal("revoked API key was restored from READONLY_API_KEYS")
	}
	if reloaded.PublicHost != "saved.example.com" {
		t.Fatalf("PublicHost = %q, want saved.example.com", reloaded.PublicHost)
	}
	if reloaded.ReadOnlyAPIRatePerMin != 120 {
		t.Fatalf("ReadOnlyAPIRatePerMin = %d, want 120", reloaded.ReadOnlyAPIRatePerMin)
	}
}

func TestPublicHostAndRateEnvDefaults(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("PUBLIC_HOST", "  host.example  ")
	loaded := Load()
	if loaded.PublicHost != "host.example" {
		t.Fatalf("Load PublicHost = %q, want host.example", loaded.PublicHost)
	}
}
