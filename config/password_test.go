package config

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func legacySHA256Hex(plain string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(plain)))
}

// TestHashWebUIPasswordIsSaltedAndVerifiable 锁定加盐：同一密码两次哈希必须
// 不同（随机盐），且各自都能校验通过。无盐 SHA-256 的致命弱点就是同密码同哈希，
// 使彩虹表与跨实例比对成为可能。
func TestHashWebUIPasswordIsSaltedAndVerifiable(t *testing.T) {
	const plain = "correct horse battery staple"

	first, err := HashWebUIPassword(plain)
	if err != nil {
		t.Fatalf("HashWebUIPassword() error = %v", err)
	}
	second, err := HashWebUIPassword(plain)
	if err != nil {
		t.Fatalf("HashWebUIPassword() error = %v", err)
	}

	if first == second {
		t.Fatal("two hashes of the same password are identical; the salt is not random")
	}
	if !strings.HasPrefix(first, WebUIPasswordHashScheme+"$") {
		t.Fatalf("hash %q lacks the %q scheme prefix", first, WebUIPasswordHashScheme)
	}
	// 哈希串不得包含明文。
	if strings.Contains(first, plain) {
		t.Fatal("encoded hash leaks the plaintext password")
	}

	for _, encoded := range []string{first, second} {
		ok, needsUpgrade := VerifyWebUIPassword(plain, encoded)
		if !ok {
			t.Fatalf("VerifyWebUIPassword() = false for a hash it just produced (%q)", encoded)
		}
		if needsUpgrade {
			t.Fatal("a freshly produced PBKDF2 hash must not request an upgrade")
		}
	}

	if ok, _ := VerifyWebUIPassword("wrong password", first); ok {
		t.Fatal("VerifyWebUIPassword() accepted a wrong password")
	}
}

// TestVerifyWebUIPasswordMigratesLegacyHash 锁定迁移契约：
// 存量无盐 SHA-256 必须仍能登录，并在校验通过时请求升级。
// 校验失败时绝不能请求升级——否则一次错误的登录尝试就会触发凭据改写。
func TestVerifyWebUIPasswordMigratesLegacyHash(t *testing.T) {
	const plain = "legacy-secret"
	legacy := legacySHA256Hex(plain)

	ok, needsUpgrade := VerifyWebUIPassword(plain, legacy)
	if !ok {
		t.Fatal("VerifyWebUIPassword() rejected a valid legacy SHA-256 hash; existing installs would be locked out")
	}
	if !needsUpgrade {
		t.Fatal("a legacy SHA-256 hash must request an upgrade after a successful verify")
	}

	ok, needsUpgrade = VerifyWebUIPassword("wrong", legacy)
	if ok {
		t.Fatal("VerifyWebUIPassword() accepted a wrong password against a legacy hash")
	}
	if needsUpgrade {
		t.Fatal("a failed verify must never request an upgrade (it would rewrite credentials on a bad guess)")
	}
}

// TestVerifyWebUIPasswordRejectsMalformedEncodings 确认损坏/被手工编辑的哈希
// 一律拒绝，而不是 panic 或意外放行。
func TestVerifyWebUIPasswordRejectsMalformedEncodings(t *testing.T) {
	valid, err := HashWebUIPassword("x")
	if err != nil {
		t.Fatalf("HashWebUIPassword() error = %v", err)
	}
	parts := strings.Split(valid, "$")
	if len(parts) != 4 {
		t.Fatalf("unexpected encoding shape %q", valid)
	}

	for _, tc := range []struct {
		name    string
		encoded string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"scheme only", WebUIPasswordHashScheme},
		{"missing fields", WebUIPasswordHashScheme + "$210000$onlysalt"},
		{"extra fields", valid + "$extra"},
		{"unknown scheme", "scrypt$1$2$3"},
		{"non numeric iterations", WebUIPasswordHashScheme + "$abc$" + parts[2] + "$" + parts[3]},
		{"zero iterations", WebUIPasswordHashScheme + "$0$" + parts[2] + "$" + parts[3]},
		{"negative iterations", WebUIPasswordHashScheme + "$-1$" + parts[2] + "$" + parts[3]},
		{"bad base64 salt", WebUIPasswordHashScheme + "$210000$!!!$" + parts[3]},
		{"bad base64 key", WebUIPasswordHashScheme + "$210000$" + parts[2] + "$!!!"},
		{"empty salt", WebUIPasswordHashScheme + "$210000$$" + parts[3]},
		{"empty key", WebUIPasswordHashScheme + "$210000$" + parts[2] + "$"},
		{"legacy wrong length", legacySHA256Hex("x")[:32]},
		{"legacy non hex", strings.Repeat("z", 64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, needsUpgrade := VerifyWebUIPassword("x", tc.encoded)
			if ok {
				t.Fatalf("VerifyWebUIPassword() accepted malformed encoding %q", tc.encoded)
			}
			if needsUpgrade {
				t.Fatalf("malformed encoding %q must not request an upgrade", tc.encoded)
			}
		})
	}
}

// TestVerifyWebUIPasswordHonoursEncodedIterations 确认迭代次数从哈希串读取，
// 而不是硬编码当前常量——否则调整 pbkdf2Iterations 会让所有存量哈希失效。
func TestVerifyWebUIPasswordHonoursEncodedIterations(t *testing.T) {
	const plain = "iteration-bound"
	salt := []byte("0123456789abcdef")

	// 用一个刻意不同于当前常量的迭代次数生成哈希。
	lowIter := 1000
	if lowIter == pbkdf2Iterations {
		t.Fatal("test iteration count must differ from the production constant")
	}
	encoded, err := encodePBKDF2(plain, salt, lowIter)
	if err != nil {
		t.Fatalf("encodePBKDF2() error = %v", err)
	}

	ok, needsUpgrade := VerifyWebUIPassword(plain, encoded)
	if !ok {
		t.Fatalf("VerifyWebUIPassword() rejected a hash encoded with %d iterations; the count must be read from the string", lowIter)
	}
	if needsUpgrade {
		t.Fatal("a PBKDF2 hash must not request an upgrade merely because its iteration count differs")
	}
}

// TestBootstrapCredentialsUsesSaltedWebUIHash 锁定首启引导：
// WebUI 登录哈希必须是加盐 PBKDF2，代理认证哈希按设计保持无盐 SHA-256
// （每请求校验 + 明文同存，见 password.go 的作用域说明）。
func TestBootstrapCredentialsUsesSaltedWebUIHash(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("DATA_DIR", t.TempDir())

	cfg := Load()
	info := FirstBootCredentials()
	if info == nil || info.WebUIPassword == "" {
		t.Fatal("FirstBootCredentials() did not report a generated WebUI password")
	}

	if !strings.HasPrefix(cfg.WebUIPasswordHash, WebUIPasswordHashScheme+"$") {
		t.Fatalf("WebUIPasswordHash = %q, want a %q hash", cfg.WebUIPasswordHash, WebUIPasswordHashScheme)
	}
	ok, needsUpgrade := VerifyWebUIPassword(info.WebUIPassword, cfg.WebUIPasswordHash)
	if !ok {
		t.Fatal("the bootstrapped WebUI password does not verify against its stored hash")
	}
	if needsUpgrade {
		t.Fatal("a freshly bootstrapped hash must not request an upgrade")
	}

	// 代理认证密码：明文与无盐哈希并存是既定设计，不随本次升级改变。
	if cfg.ProxyAuthPassword == "" {
		t.Fatal("ProxyAuthPassword plaintext is empty; the copy-full-URL feature depends on it")
	}
	if cfg.ProxyAuthPasswordHash != legacySHA256Hex(cfg.ProxyAuthPassword) {
		t.Fatal("ProxyAuthPasswordHash must remain unsalted SHA-256 (verified per request; plaintext coexists)")
	}
}
