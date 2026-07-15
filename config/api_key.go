package config

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"os"
	"strings"
	"time"
)

// HashAPIKey is the single canonical hash for read-only API keys.
//
// It is intentionally bare SHA-256 hex and MUST stay that way: every API key
// already persisted — in config.json and via READONLY_API_KEYS env import — is
// stored as this exact digest, and the read-only API authenticates by comparing
// against it. Changing the algorithm, for example by adding a salt or KDF, would
// silently invalidate every stored key, locking out existing integrations.
// Backward compatibility is therefore mandatory; see docs/READONLY_API_DESIGN.md
// §3.2 for the rejected-alternatives analysis. Both the config package and the
// webui package hash API keys through this one function so no divergent second
// implementation can drift out of sync.
func HashAPIKey(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// APIKey is a read-only API credential stored by SHA-256 hash only.
type APIKey struct {
	ID         string    `json:"id"`
	Name       string    `json:"name,omitempty"`
	Hash       string    `json:"hash"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
	LastUsedAt time.Time `json:"last_used_at,omitempty"`
	Disabled   bool      `json:"disabled,omitempty"`
}

// ValidateReadOnlyAPIKey reports whether plain matches any non-disabled key hash.
func ValidateReadOnlyAPIKey(cfg *Config, plain string) bool {
	if cfg == nil || plain == "" {
		return false
	}
	want := HashAPIKey(plain)
	for _, key := range cfg.ReadOnlyAPIKeys {
		if key.Disabled || key.Hash == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(key.Hash), []byte(want)) == 1 {
			return true
		}
	}
	return false
}

func parseReadOnlyAPIKeysEnv(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		plain := strings.TrimSpace(part)
		if plain == "" {
			continue
		}
		if _, ok := seen[plain]; ok {
			continue
		}
		seen[plain] = struct{}{}
		out = append(out, plain)
	}
	return out
}

// importReadOnlyAPIKeysFromEnv hashes READONLY_API_KEYS plains and appends missing hashes.
// Plaintext is never stored on Config or disk.
func importReadOnlyAPIKeysFromEnv(cfg *Config) (changed bool) {
	plains := parseReadOnlyAPIKeysEnv(os.Getenv("READONLY_API_KEYS"))
	if len(plains) == 0 {
		return false
	}
	existing := make(map[string]struct{}, len(cfg.ReadOnlyAPIKeys))
	for _, k := range cfg.ReadOnlyAPIKeys {
		if k.Hash != "" {
			existing[k.Hash] = struct{}{}
		}
	}
	now := time.Now().UTC()
	for i, plain := range plains {
		h := HashAPIKey(plain)
		if _, ok := existing[h]; ok {
			continue
		}
		cfg.ReadOnlyAPIKeys = append(cfg.ReadOnlyAPIKeys, APIKey{
			ID:        generateCredential(),
			Name:      "env-import",
			Hash:      h,
			CreatedAt: now.Add(time.Duration(i) * time.Nanosecond),
		})
		existing[h] = struct{}{}
		changed = true
	}
	return changed
}
