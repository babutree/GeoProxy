package config

import "testing"

// TestHashAPIKeyMatchesBareSHA256 pins the canonical API-key hash to bare
// SHA-256 hex. This is the backward-compatibility contract: any key persisted
// under the old bare-SHA-256 scheme (config.json or READONLY_API_KEYS env
// import) MUST keep hashing to the same value, otherwise every already-stored
// key would silently stop authenticating.
func TestHashAPIKeyMatchesBareSHA256(t *testing.T) {
	// Known-answer vector computed independently:
	//   printf 'legacy-persisted-key' | sha256sum
	const (
		plain    = "legacy-persisted-key"
		wantHash = "cdc5c4494fbd5368953db8ca0be17cfb1120e32399d1c2ae710dc873adcba7de"
	)
	if got := HashAPIKey(plain); got != wantHash {
		t.Fatalf("HashAPIKey(%q) = %q, want %q (backward-compat with persisted keys broken)", plain, got, wantHash)
	}
}

// TestHashAPIKeyEqualsInternalPasswordHash proves the exported canonical helper
// and the internal passwordHash used by Load/Save/env-import produce identical
// output, so centralizing on HashAPIKey does not change any persisted hash.
func TestHashAPIKeyEqualsInternalPasswordHash(t *testing.T) {
	for _, plain := range []string{"", "a", "ro-key-secret-plain-abc", "env-key-two", "  spaced  "} {
		if HashAPIKey(plain) != passwordHash(plain) {
			t.Fatalf("HashAPIKey(%q)=%q != passwordHash=%q", plain, HashAPIKey(plain), passwordHash(plain))
		}
	}
}

// TestHashAPIKeyDistinctForDistinctInput is the adversarial/negative case:
// different plaintext must not collide to the same hash.
func TestHashAPIKeyDistinctForDistinctInput(t *testing.T) {
	if HashAPIKey("key-one") == HashAPIKey("key-two") {
		t.Fatal("distinct plaintext hashed to identical value")
	}
}
