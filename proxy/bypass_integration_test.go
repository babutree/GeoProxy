package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestSocks5BypassDialsLocalTargetDirectly 端到端验证 bypass 直连：
// 上游 store 为空（无任何可用节点），若目标是内网/本地地址，SOCKS5 仍应直连成功。
// 因为没有任何上游节点，连接能建立且回显成功，只可能来自 bypass 直连路径——
// 这直接证明内网目标绕过了上游选路。
func TestSocks5BypassDialsLocalTargetDirectly(t *testing.T) {
	// 本地 echo 服务器，模拟内网服务（127.0.0.1）。
	echoAddr, echoPort := startLocalEcho(t)

	store := newProxyTestStore() // 故意为空：无任何上游节点
	server := newSocks5TestServer(store, proxyTestConfig(0))

	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close() })
	go server.handleConnection(serverConn)

	writeSocks5NoAuthHandshake(t, clientConn)
	writeSocks5DomainRequest(t, clientConn, "127.0.0.1", echoPort)

	// 读 SOCKS5 应答：VER REP RSV ATYP + BND.ADDR(IPv4 4) + BND.PORT(2) = 10 字节。
	reply := make([]byte, 10)
	if err := readFullDeadline(t, clientConn, reply); err != nil {
		t.Fatalf("read socks5 reply: %v", err)
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		t.Fatalf("socks5 reply = %#v, want VER=0x05 REP=0x00 (bypass 直连成功); echoAddr=%s", reply[:2], echoAddr)
	}

	// 通过隧道发数据，本地 echo 应原样返回，证明直连真实可用。
	payload := []byte("ping-bypass")
	if err := writeDeadline(t, clientConn, payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	got := make([]byte, len(payload))
	if err := readFullDeadline(t, clientConn, got); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("echo = %q, want %q", got, payload)
	}
}

// TestHTTPBypassDialsLocalTargetDirectly 空 store 时 HTTP 入站对 127.0.0.1 仍直连成功。
// 无上游节点却能拿到本地目标响应，只能走 httpDirect bypass。
func TestHTTPBypassDialsLocalTargetDirectly(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bypass-ok" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("http-bypass-ok"))
	}))
	t.Cleanup(target.Close)

	store := newProxyTestStore() // 空 store：无任何上游
	gateway := newProxyTestServer(store, proxyTestConfig(0))
	proxySrv := httptest.NewServer(gateway)
	t.Cleanup(proxySrv.Close)

	proxyURL, err := url.Parse(proxySrv.URL)
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}
	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
	}

	resp, err := client.Get(target.URL + "/bypass-ok")
	if err != nil {
		t.Fatalf("HTTP via empty-store proxy to local target: %v (want bypass direct success)", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK || string(body) != "http-bypass-ok" {
		t.Fatalf("status/body = %d %q, want 200 http-bypass-ok", resp.StatusCode, body)
	}
}

// TestHTTPConnectBypassDialsLocalTargetDirectly 空 store 时 CONNECT 对 127.0.0.1 仍建立直连隧道。
func TestHTTPConnectBypassDialsLocalTargetDirectly(t *testing.T) {
	echoAddr, _ := startLocalEcho(t)

	store := newProxyTestStore()
	gateway := newProxyTestServer(store, proxyTestConfig(0))
	proxySrv := httptest.NewServer(gateway)
	t.Cleanup(proxySrv.Close)

	proxyHost := strings.TrimPrefix(proxySrv.URL, "http://")
	conn, err := net.DialTimeout("tcp", proxyHost, 3*time.Second)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	_, err = fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", echoAddr, echoAddr)
	if err != nil {
		t.Fatalf("write CONNECT: %v", err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("read CONNECT response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT status = %d, want 200 (bypass tunnel); echoAddr=%s", resp.StatusCode, echoAddr)
	}

	payload := []byte("connect-bypass")
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write tunnel payload: %v", err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(br, got); err != nil {
		t.Fatalf("read tunnel echo: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("tunnel echo = %q, want %q", got, payload)
	}
}

// TestHTTPBypassReturnsRedirectWithoutFollowing verifies HTTP bypass returns the
// target's redirect to the client instead of following it inside the gateway.
func TestHTTPBypassReturnsRedirectWithoutFollowing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("gateway followed relative redirect during bypass")
	})
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	target := httptest.NewServer(mux)
	t.Cleanup(target.Close)

	store := newProxyTestStore()
	gateway := newProxyTestServer(store, proxyTestConfig(0))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target.URL+"/start", nil)
	gateway.handleHTTP(rec, req, emptyRoute())

	if rec.Code != http.StatusFound {
		t.Fatalf("bypass redirect status = %d, want 302 returned to client", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/final" {
		t.Fatalf("bypass redirect Location = %q, want /final", got)
	}
}

// TestHTTPBypassDoesNotFollowDangerousAbsoluteRedirect covers the metadata SSRF
// case: a bypassed local service must not make the gateway follow an absolute
// redirect to link-local metadata addresses.
func TestHTTPBypassDoesNotFollowDangerousAbsoluteRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	t.Cleanup(target.Close)

	store := newProxyTestStore()
	gateway := newProxyTestServer(store, proxyTestConfig(0))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target.URL, nil)
	gateway.handleHTTP(rec, req, emptyRoute())

	if rec.Code != http.StatusFound {
		t.Fatalf("bypass redirect status = %d, want 302 returned to client", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "http://169.254.169.254/latest/meta-data/" {
		t.Fatalf("bypass redirect Location = %q, want metadata URL returned unchanged", got)
	}
}

// startLocalEcho 起一个 127.0.0.1 上的 TCP echo 服务器，返回其地址与端口。
func startLocalEcho(t *testing.T) (string, uint16) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				io.Copy(conn, conn)
			}(c)
		}
	}()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := net.LookupPort("tcp", portStr)
	return ln.Addr().String(), uint16(port)
}

func readFullDeadline(t *testing.T, conn net.Conn, buf []byte) error {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	_, err := io.ReadFull(conn, buf)
	return err
}

func writeDeadline(t *testing.T, conn net.Conn, buf []byte) error {
	t.Helper()
	_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	defer conn.SetWriteDeadline(time.Time{})
	_, err := conn.Write(buf)
	return err
}
