package webui

import (
	"net/http"
	"strings"
	"testing"

	"github.com/babutree/GeoProxy/config"
)

// TestLoginUpgradesLegacyPasswordHashOnSuccess 锁定透明迁移：
// 存量无盐 SHA-256 哈希的实例，用户用原密码登录成功后哈希应被重写为加盐 PBKDF2。
// 登录成功是唯一能同时拿到明文与既有哈希的时机，别处无法迁移。
func TestLoginUpgradesLegacyPasswordHashOnSuccess(t *testing.T) {
	server := newTestServer(t)
	const password = "legacy-login-secret"
	legacy := sha256Hex(password)

	saved := make(chan *config.Config, 4)
	restore := stubConfigSave(t, func(cfg *config.Config) error {
		saved <- cfg
		return nil
	})
	defer restore()

	server.cfgMu.Lock()
	server.cfg.WebUIPasswordHash = legacy
	server.cfgMu.Unlock()

	rec := serveLogin(t, server, password, "198.51.100.20:12345", false)
	if rec.Code != http.StatusFound {
		t.Fatalf("login status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}

	server.cfgMu.RLock()
	current := server.cfg.WebUIPasswordHash
	server.cfgMu.RUnlock()
	if current == legacy {
		t.Fatal("WebUIPasswordHash was not upgraded after a successful legacy login")
	}
	if !strings.HasPrefix(current, config.WebUIPasswordHashScheme+"$") {
		t.Fatalf("upgraded hash = %q, want the %q scheme", current, config.WebUIPasswordHashScheme)
	}

	// 升级必须落盘，否则重启后又退回旧哈希。
	select {
	case cfg := <-saved:
		if !strings.HasPrefix(cfg.WebUIPasswordHash, config.WebUIPasswordHashScheme+"$") {
			t.Fatalf("persisted hash = %q, want the upgraded scheme", cfg.WebUIPasswordHash)
		}
	default:
		t.Fatal("the upgraded hash was never persisted")
	}

	// 升级后原密码仍必须能登录。
	again := serveLogin(t, server, password, "198.51.100.21:12345", false)
	if again.Code != http.StatusFound {
		t.Fatalf("post-upgrade login status = %d, want %d", again.Code, http.StatusFound)
	}
}

// 密码错误时绝不能触发升级：否则一次错误猜测就会改写已存储的凭据。
func TestLoginDoesNotUpgradeHashOnFailure(t *testing.T) {
	server := newTestServer(t)
	legacy := sha256Hex("real-password")

	restore := stubConfigSave(t, func(*config.Config) error {
		t.Error("configSave must not be called on a failed login")
		return nil
	})
	defer restore()

	server.cfgMu.Lock()
	server.cfg.WebUIPasswordHash = legacy
	server.cfgMu.Unlock()

	rec := serveLogin(t, server, "wrong-password", "198.51.100.22:12345", false)
	if rec.Code == http.StatusFound {
		t.Fatal("a wrong password was accepted")
	}

	server.cfgMu.RLock()
	current := server.cfg.WebUIPasswordHash
	server.cfgMu.RUnlock()
	if current != legacy {
		t.Fatalf("WebUIPasswordHash changed to %q after a failed login; credentials must be untouched", current)
	}
}

// 落盘失败不得阻断登录：用户凭据是对的，升级只是尽力而为的加固。
func TestLoginSucceedsWhenHashUpgradePersistFails(t *testing.T) {
	server := newTestServer(t)
	const password = "persist-fail-secret"
	legacy := sha256Hex(password)

	restore := stubConfigSave(t, func(*config.Config) error {
		return errSaveFailed
	})
	defer restore()

	server.cfgMu.Lock()
	server.cfg.WebUIPasswordHash = legacy
	server.cfgMu.Unlock()

	rec := serveLogin(t, server, password, "198.51.100.23:12345", false)
	if rec.Code != http.StatusFound {
		t.Fatalf("login status = %d, want %d even when the hash upgrade cannot be persisted", rec.Code, http.StatusFound)
	}

	// 落盘失败时运行态必须保留旧哈希，避免内存与磁盘分裂
	// （内存新哈希 + 磁盘旧哈希，重启后行为不一致）。
	server.cfgMu.RLock()
	current := server.cfg.WebUIPasswordHash
	server.cfgMu.RUnlock()
	if current != legacy {
		t.Fatalf("WebUIPasswordHash = %q after a failed persist, want the legacy hash retained to stay consistent with disk", current)
	}
}

// 已是 PBKDF2 的实例不得在每次登录时重复重写哈希（无谓的磁盘写入）。
func TestLoginDoesNotRewriteAlreadyUpgradedHash(t *testing.T) {
	server := newTestServer(t)
	const password = "already-upgraded"
	hashed, err := config.HashWebUIPassword(password)
	if err != nil {
		t.Fatalf("HashWebUIPassword() error = %v", err)
	}

	restore := stubConfigSave(t, func(*config.Config) error {
		t.Error("configSave must not be called when the hash is already PBKDF2")
		return nil
	})
	defer restore()

	server.cfgMu.Lock()
	server.cfg.WebUIPasswordHash = hashed
	server.cfgMu.Unlock()

	rec := serveLogin(t, server, password, "198.51.100.24:12345", false)
	if rec.Code != http.StatusFound {
		t.Fatalf("login status = %d, want %d", rec.Code, http.StatusFound)
	}

	server.cfgMu.RLock()
	current := server.cfg.WebUIPasswordHash
	server.cfgMu.RUnlock()
	if current != hashed {
		t.Fatal("an already-upgraded hash was rewritten")
	}
}

// stubConfigSave 替换持久化切入点，测试结束后恢复。
func stubConfigSave(t *testing.T, fn func(*config.Config) error) func() {
	t.Helper()
	original := configSave
	configSave = fn
	return func() { configSave = original }
}

var errSaveFailed = errStubSave("config save failed")

type errStubSave string

func (e errStubSave) Error() string { return string(e) }
