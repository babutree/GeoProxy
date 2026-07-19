package proxy

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/affinity"
	"github.com/babutree/GeoProxy/storage"
)

// TestSOCKS5HTTPUpstreamPort80RejectionDoesNotPoisonHealth
// 验证 HTTP 上游拒绝 CONNECT :80 属于目标能力/策略拒绝，不应把节点累计为健康失败。
func TestSOCKS5HTTPUpstreamPort80RejectionDoesNotPoisonHealth(t *testing.T) {
	store := newProxyTestStore()
	server := newAuthenticatedCapabilityServer(t, store, 0)
	boundBeforeReject := make(chan int64, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Errorf("upstream method = %s, want CONNECT", r.Method)
		}
		if r.Host != "example.test:80" {
			t.Errorf("upstream host = %q, want example.test:80", r.Host)
		}
		binding, ok := server.sessions.Get("cap80")
		if !ok {
			boundBeforeReject <- 0
		} else {
			boundBeforeReject <- binding.ProxyID
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(upstream.Close)

	addProxy(t, store, upstreamAddr(t, upstream.URL), "http", 1)

	client, serverConn := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	done := make(chan struct{})
	go func() {
		server.handleConnection(serverConn)
		close(done)
	}()

	writeSocks5AuthenticatedHandshake(t, client, "proxy-session-cap80", "secret")
	writeSocks5DomainRequest(t, client, "example.test", 80)
	select {
	case proxyID := <-boundBeforeReject:
		if proxyID != 1 {
			t.Fatalf("session binding observed before capability rejection = proxy %d, want 1", proxyID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not observe selector binding before HTTP 403 response")
	}
	reply := make([]byte, 10)
	if err := readFullDeadline(t, client, reply); err != nil {
		t.Fatalf("read SOCKS5 failure reply: %v", err)
	}
	if reply[1] != 0x01 {
		t.Fatalf("SOCKS5 reply code = %#x, want general failure", reply[1])
	}

	<-done
	proxy, err := store.GetProxyByID(1)
	if err != nil {
		t.Fatalf("GetProxyByID() error = %v", err)
	}
	if proxy.FailCount != 0 {
		t.Fatalf("HTTP CONNECT :80 policy rejection incremented fail_count to %d, want 0", proxy.FailCount)
	}
	if binding, ok := server.sessions.Get("cap80"); ok {
		t.Fatalf("capability rejection retained failed session binding: %#v", binding)
	}
}

func TestHTTPConnectPort80Success(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != "target.test:80" {
			t.Errorf("CONNECT request = method %s host %q, want target.test:80", r.Method, r.Host)
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("HTTP test server does not support hijacking")
			return
		}
		conn, rw, err := hijacker.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		defer conn.Close()
		if _, err := io.WriteString(rw, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return
		}
		if err := rw.Flush(); err != nil {
			return
		}
		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return
		}
		_, _ = conn.Write([]byte("pong"))
	}))
	t.Cleanup(upstream.Close)

	server := New(nil, proxyTestConfig(0), ":0")
	conn, err := server.dialViaProxy(&storage.Proxy{Address: upstreamAddr(t, upstream.URL), Protocol: "http"}, "target.test:80")
	if err != nil {
		t.Fatalf("dialViaProxy() CONNECT :80 error = %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write tunneled payload: %v", err)
	}
	got := make([]byte, 4)
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read tunneled response: %v", err)
	}
	if string(got) != "pong" {
		t.Fatalf("tunneled response = %q, want pong", got)
	}
}

func TestSOCKS5RetriesAfterHTTPPort80CapabilityRejection(t *testing.T) {
	rejector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Errorf("rejector method = %s, want CONNECT", r.Method)
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(rejector.Close)
	accepted := startCapabilitySOCKS5Upstream(t)

	store := newProxyTestStore()
	addProxy(t, store, upstreamAddr(t, rejector.URL), "http", 1)
	addProxy(t, store, accepted, "socks5", 2)
	server := newAuthenticatedCapabilityServer(t, store, 1)

	client, serverConn := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	done := make(chan struct{})
	go func() {
		server.handleConnection(serverConn)
		close(done)
	}()

	writeSocks5AuthenticatedHandshake(t, client, "proxy-session-cap80", "secret")
	writeSocks5DomainRequest(t, client, "target.test", 80)
	reply := make([]byte, 10)
	if err := readFullDeadline(t, client, reply); err != nil {
		t.Fatalf("read SOCKS5 retry reply: %v", err)
	}
	if reply[1] != 0x00 {
		t.Fatalf("SOCKS5 retry reply code = %#x, want success", reply[1])
	}
	got := make([]byte, 4)
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatalf("read response through retried node: %v", err)
	}
	if string(got) != "pong" {
		t.Fatalf("response through retried node = %q, want pong", got)
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SOCKS5 handler did not finish after client close")
	}

	first, err := store.GetProxyByID(1)
	if err != nil {
		t.Fatalf("GetProxyByID(first) error = %v", err)
	}
	if first.FailCount != 0 {
		t.Fatalf("rejected HTTP node fail_count = %d, want 0", first.FailCount)
	}
	second, err := store.GetProxyByID(2)
	if err != nil {
		t.Fatalf("GetProxyByID(second) error = %v", err)
	}
	if second.SuccessCount == 0 {
		t.Fatalf("retried SOCKS5 node success_count = %d, want positive", second.SuccessCount)
	}
	binding, ok := server.sessions.Get("cap80")
	if !ok || binding.ProxyID != 2 {
		t.Fatalf("retry session binding = %#v, ok=%v; want compatible proxy id=2", binding, ok)
	}
}

func TestSOCKS5HTTPPort443RejectionStillCountsHealthFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(upstream.Close)
	store := newProxyTestStore()
	addProxy(t, store, upstreamAddr(t, upstream.URL), "http", 1)
	server := newAuthenticatedCapabilityServer(t, store, 0)
	client, serverConn := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	done := make(chan struct{})
	go func() {
		server.handleConnection(serverConn)
		close(done)
	}()
	writeSocks5AuthenticatedHandshake(t, client, "proxy-session-cap80", "secret")
	writeSocks5DomainRequest(t, client, "target.test", 443)
	reply := make([]byte, 10)
	if err := readFullDeadline(t, client, reply); err != nil {
		t.Fatalf("read SOCKS5 failure reply: %v", err)
	}
	<-done
	proxy, err := store.GetProxyByID(1)
	if err != nil {
		t.Fatalf("GetProxyByID() error = %v", err)
	}
	if proxy.FailCount != 1 {
		t.Fatalf("HTTP CONNECT :443 rejection fail_count = %d, want 1", proxy.FailCount)
	}
	if binding, ok := server.sessions.Get("cap80"); ok {
		t.Fatalf("CONNECT :443 failure retained session binding: %#v", binding)
	}
}

func TestSOCKS5HTTPPort80AuthenticationRejectionCountsHealthFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	t.Cleanup(upstream.Close)
	store := newProxyTestStore()
	addProxy(t, store, upstreamAddr(t, upstream.URL), "http", 1)
	server := newAuthenticatedCapabilityServer(t, store, 0)
	client, serverConn := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	done := make(chan struct{})
	go func() {
		server.handleConnection(serverConn)
		close(done)
	}()
	writeSocks5AuthenticatedHandshake(t, client, "proxy-session-cap80", "secret")
	writeSocks5DomainRequest(t, client, "target.test", 80)
	reply := make([]byte, 10)
	if err := readFullDeadline(t, client, reply); err != nil {
		t.Fatalf("read SOCKS5 failure reply: %v", err)
	}
	<-done
	proxy, err := store.GetProxyByID(1)
	if err != nil {
		t.Fatalf("GetProxyByID() error = %v", err)
	}
	if proxy.FailCount != 1 {
		t.Fatalf("HTTP CONNECT :80 authentication rejection fail_count = %d, want 1", proxy.FailCount)
	}
	if binding, ok := server.sessions.Get("cap80"); ok {
		t.Fatalf("CONNECT :80 authentication failure retained session binding: %#v", binding)
	}
}

func TestHTTPConnectCapabilityRejectionClassification(t *testing.T) {
	cases := []struct {
		name   string
		target string
		status int
		want   bool
	}{
		{name: "forbidden port 80", target: "target.test:80", status: http.StatusForbidden, want: true},
		{name: "method not allowed port 80", target: "target.test:80", status: http.StatusMethodNotAllowed, want: true},
		{name: "not implemented port 80", target: "target.test:80", status: http.StatusNotImplemented, want: true},
		{name: "proxy auth required port 80", target: "target.test:80", status: http.StatusProxyAuthRequired, want: false},
		{name: "bad request port 80", target: "target.test:80", status: http.StatusBadRequest, want: false},
		{name: "request timeout port 80", target: "target.test:80", status: http.StatusRequestTimeout, want: false},
		{name: "rate limited port 80", target: "target.test:80", status: http.StatusTooManyRequests, want: false},
		{name: "bad gateway port 80", target: "target.test:80", status: http.StatusBadGateway, want: false},
		{name: "forbidden port 443", target: "target.test:443", status: http.StatusForbidden, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := &httpConnectRejectionError{target: tc.target, statusCode: tc.status, status: http.StatusText(tc.status)}
			if got := isHTTPConnectCapabilityRejection(err); got != tc.want {
				t.Fatalf("isHTTPConnectCapabilityRejection() = %v, want %v", got, tc.want)
			}
		})
	}
}

func startCapabilitySOCKS5Upstream(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen SOCKS5 upstream: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		greetingHeader := make([]byte, 2)
		if _, err := io.ReadFull(conn, greetingHeader); err != nil {
			return
		}
		methods := make([]byte, int(greetingHeader[1]))
		if _, err := io.ReadFull(conn, methods); err != nil {
			return
		}
		if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
			return
		}
		requestHeader := make([]byte, 4)
		if _, err := io.ReadFull(conn, requestHeader); err != nil {
			return
		}
		if _, err := readSOCKS5Address(conn, requestHeader[3]); err != nil {
			return
		}
		port := make([]byte, 2)
		if _, err := io.ReadFull(conn, port); err != nil {
			return
		}
		reply := []byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
		if _, err := conn.Write(append(reply, []byte("pong")...)); err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, conn)
	}()
	return listener.Addr().String()
}

func newAuthenticatedCapabilityServer(t *testing.T, store *fakeProxyStore, maxRetry int) *SOCKS5Server {
	t.Helper()
	cfg := proxyTestConfig(maxRetry)
	cfg.ProxyAuthEnabled = true
	cfg.ProxyAuthUsername = "proxy"
	cfg.ProxyAuthPassword = "secret"
	cfg.ProxyAuthPasswordHash = ""
	server := newSocks5TestServer(store, cfg)
	server.sessions = affinity.New(time.Minute)
	return server
}

func writeSocks5AuthenticatedHandshake(t *testing.T, conn net.Conn, username, password string) {
	t.Helper()
	if _, err := conn.Write([]byte{0x05, 0x01, 0x02}); err != nil {
		t.Fatalf("write authenticated greeting: %v", err)
	}
	methodReply := make([]byte, 2)
	if err := readFullDeadline(t, conn, methodReply); err != nil {
		t.Fatalf("read authenticated method reply: %v", err)
	}
	if methodReply[0] != 0x05 || methodReply[1] != 0x02 {
		t.Fatalf("authenticated method reply = %#v, want [0x05 0x02]", methodReply)
	}
	writeSocks5Auth(t, conn, username, password)
	authReply := make([]byte, 2)
	if err := readFullDeadline(t, conn, authReply); err != nil {
		t.Fatalf("read authenticated subnegotiation reply: %v", err)
	}
	if authReply[0] != 0x01 || authReply[1] != 0x00 {
		t.Fatalf("authenticated subnegotiation reply = %#v, want [0x01 0x00]", authReply)
	}
}
