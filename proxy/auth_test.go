package proxy

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"testing"

	"github.com/babutree/GeoProxy/auth"
	"github.com/babutree/GeoProxy/config"
)

func TestCheckAuthParsesDSLAndAuthenticatesBaseUsername(t *testing.T) {
	cfg := authTestConfig(t)
	server := New(nil, cfg, ":0")
	req := &http.Request{Header: http.Header{}}
	credentials := base64.StdEncoding.EncodeToString([]byte("proxy-region-us-session-x:secret"))
	req.Header.Set("Proxy-Authorization", "Basic "+credentials)

	parsed, ok := server.checkAuth(req)

	if !ok {
		t.Fatal("checkAuth() rejected DSL username with valid base credentials")
	}
	if parsed.Base != "proxy" || parsed.Region != "us" || parsed.Session != "x" {
		t.Fatalf("parsed username = %#v", parsed)
	}
}

// Save 后入站认证必须使用新配置快照，不得继续用启动时的 s.cfg。
func TestHTTPAuthUsesConfigAfterSave(t *testing.T) {
	clearProxyConfigEnv(t)
	t.Setenv("DATA_DIR", t.TempDir())

	boot := config.Load()
	boot.ProxyAuthEnabled = true
	boot.ProxyAuthUsername = "olduser"
	boot.ProxyAuthPassword = "oldpass"
	boot.ProxyAuthPasswordHash = fmt.Sprintf("%x", sha256.Sum256([]byte("oldpass")))
	if err := config.Save(boot); err != nil {
		t.Fatalf("Save(boot) error = %v", err)
	}

	// 构造时故意持有旧快照指针，模拟进程启动后未重建 server 的场景。
	stale := *boot
	server := New(nil, &stale, ":0")

	updated := *config.Get()
	updated.ProxyAuthUsername = "newuser"
	updated.ProxyAuthPassword = "newpass"
	updated.ProxyAuthPasswordHash = fmt.Sprintf("%x", sha256.Sum256([]byte("newpass")))
	if err := config.Save(&updated); err != nil {
		t.Fatalf("Save(updated) error = %v", err)
	}

	oldReq := &http.Request{Header: http.Header{}}
	oldReq.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("olduser:oldpass")))
	if _, ok := server.checkAuth(oldReq); ok {
		t.Fatal("checkAuth still accepted old credentials after Save")
	}

	newReq := &http.Request{Header: http.Header{}}
	newReq.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("newuser:newpass")))
	if _, ok := server.checkAuth(newReq); !ok {
		t.Fatal("checkAuth rejected new credentials after Save")
	}
}

func clearProxyConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"WEBUI_PASSWORD", "PROXY_AUTH_ENABLED", "PROXY_AUTH_USERNAME", "PROXY_AUTH_PASSWORD",
		"HTTP_PORT", "SOCKS5_PORT", "WEBUI_PORT", "SESSION_TTL_MINUTES", "MAX_SESSIONS_PER_PROXY", "PROXY_COOLDOWN_MINUTES",
		"DEFAULT_REGION", "ALLOWED_COUNTRIES", "BLOCKED_COUNTRIES", "HEALTH_CHECK_INTERVAL",
		"MAX_RETRY", "SINGBOX_PATH",
		"READONLY_API_KEYS", "PUBLIC_HOST", "READONLY_API_RATE_PER_MIN",
	} {
		t.Setenv(key, "")
	}
}

func TestSocks5AuthParsesDSLAndAuthenticatesBaseUsername(t *testing.T) {
	client, serverConn := net.Pipe()
	defer client.Close()
	defer serverConn.Close()

	server := NewSOCKS5(nil, authTestConfig(t), ":0")
	done := make(chan authResult, 1)
	go func() {
		parsed, err := server.socks5Auth(serverConn)
		done <- authResult{parsed: parsed, err: err}
	}()

	writeSocks5Auth(t, client, "proxy-region-jp-session-y", "secret")
	reader := bufio.NewReader(client)
	status, err := reader.ReadByte()
	if err != nil {
		t.Fatalf("read auth version: %v", err)
	}
	code, err := reader.ReadByte()
	if err != nil {
		t.Fatalf("read auth status: %v", err)
	}
	result := <-done

	if status != 0x01 || code != 0x00 || result.err != nil {
		t.Fatalf("auth reply = [%#x %#x], err = %v", status, code, result.err)
	}
	if result.parsed.Base != "proxy" || result.parsed.Region != "jp" || result.parsed.Session != "y" {
		t.Fatalf("parsed username = %#v", result.parsed)
	}
}

func TestSocks5AuthAcceptsHashOnlyProxyPassword(t *testing.T) {
	client, serverConn := net.Pipe()
	defer client.Close()
	defer serverConn.Close()

	cfg := authTestConfig(t)
	cfg.ProxyAuthPassword = ""
	server := NewSOCKS5(nil, cfg, ":0")
	done := make(chan authResult, 1)
	go func() {
		parsed, err := server.socks5Auth(serverConn)
		done <- authResult{parsed: parsed, err: err}
	}()

	writeSocks5Auth(t, client, "proxy-region-jp-session-y", "secret")
	reader := bufio.NewReader(client)
	status, err := reader.ReadByte()
	if err != nil {
		t.Fatalf("read auth version: %v", err)
	}
	code, err := reader.ReadByte()
	if err != nil {
		t.Fatalf("read auth status: %v", err)
	}
	result := <-done

	if status != 0x01 || code != 0x00 || result.err != nil {
		t.Fatalf("hash-only auth reply = [%#x %#x], err = %v", status, code, result.err)
	}
}

type authResult struct {
	parsed auth.ParsedUsername
	err    error
}

func authTestConfig(t *testing.T) *config.Config {
	t.Helper()
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte("secret")))
	cfg := &config.Config{
		ProxyAuthEnabled:      true,
		ProxyAuthUsername:     "proxy",
		ProxyAuthPassword:     "secret",
		ProxyAuthPasswordHash: hash,
		ValidateTimeout:       1,
		MaxRetry:              1,
	}
	prev := config.Get()
	config.SetGlobal(cfg)
	t.Cleanup(func() { config.SetGlobal(prev) })
	return cfg
}

func writeSocks5Auth(t *testing.T, conn net.Conn, username string, password string) {
	t.Helper()
	msg := []byte{0x01, byte(len(username))}
	msg = append(msg, []byte(username)...)
	msg = append(msg, byte(len(password)))
	msg = append(msg, []byte(password)...)
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write auth request: %v", err)
	}
}
