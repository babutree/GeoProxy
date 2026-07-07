package proxy

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"testing"

	"goproxy/auth"
	"goproxy/config"
)

func TestCheckAuthParsesDSLAndAuthenticatesBaseUsername(t *testing.T) {
	cfg := authTestConfig()
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

func TestSocks5AuthParsesDSLAndAuthenticatesBaseUsername(t *testing.T) {
	client, serverConn := net.Pipe()
	defer client.Close()
	defer serverConn.Close()

	server := NewSOCKS5(nil, authTestConfig(), ":0")
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

type authResult struct {
	parsed auth.ParsedUsername
	err    error
}

func authTestConfig() *config.Config {
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte("secret")))
	return &config.Config{
		ProxyAuthUsername:     "proxy",
		ProxyAuthPassword:     "secret",
		ProxyAuthPasswordHash: hash,
		ValidateTimeout:       1,
		MaxRetry:              1,
	}
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
