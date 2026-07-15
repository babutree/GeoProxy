package proxy

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"goproxy/auth"
	"goproxy/config"
	"goproxy/storage"
)

func TestHTTPForwardingStripsProxyAuthorizationAndHopByHopHeaders(t *testing.T) {
	var got http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	store := newProxyTestStore()
	addProxy(t, store, upstreamAddr(t, upstream.URL), "http", 1)
	server := newProxyTestServer(store, proxyTestConfig(0))
	req := httptest.NewRequest(http.MethodGet, "http://example.test/resource", nil)
	req.Header.Set("Proxy-Authorization", "Basic secret")
	req.Header.Set("Proxy-Connection", "keep-alive")
	req.Header.Set("Connection", "Keep-Alive, X-Hop")
	req.Header.Set("Keep-Alive", "timeout=5")
	req.Header.Set("TE", "trailers")
	req.Header.Set("X-Hop", "remove-me")
	req.Header.Set("X-End-To-End", "keep-me")

	server.handleHTTP(httptest.NewRecorder(), req, emptyRoute())

	for _, name := range []string{"Proxy-Authorization", "Proxy-Connection", "Connection", "Keep-Alive", "TE", "X-Hop"} {
		if got.Get(name) != "" {
			t.Fatalf("forwarded header %s = %q, want empty", name, got.Get(name))
		}
	}
	if got.Get("X-End-To-End") != "keep-me" {
		t.Fatalf("X-End-To-End = %q, want keep-me", got.Get("X-End-To-End"))
	}
}

func TestHTTPBodyIsReplayedOnRetry(t *testing.T) {
	var firstBody string
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		firstBody = string(data)
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("response writer does not support hijack")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}
		conn.Close()
	}))
	defer first.Close()

	var secondBody string
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		secondBody = string(data)
		w.WriteHeader(http.StatusOK)
	}))
	defer second.Close()

	store := newProxyTestStore()
	addProxy(t, store, upstreamAddr(t, first.URL), "http", 1)
	addProxy(t, store, upstreamAddr(t, second.URL), "http", 2)
	server := newProxyTestServer(store, proxyTestConfig(1))
	body := "retry-safe-body"
	req := httptest.NewRequest(http.MethodPost, "http://example.test/post", strings.NewReader(body))

	server.handleHTTP(httptest.NewRecorder(), req, emptyRoute())

	if firstBody != body {
		t.Fatalf("first upstream body = %q, want %q", firstBody, body)
	}
	if secondBody != body {
		t.Fatalf("second upstream body = %q, want %q", secondBody, body)
	}
}

func TestHTTPConnectRejectsNon200UpstreamResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Fatalf("method = %s, want CONNECT", r.Method)
		}
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	defer upstream.Close()

	server := New(nil, proxyTestConfig(0), ":0")
	conn, err := server.dialViaProxy(&storage.Proxy{Address: upstreamAddr(t, upstream.URL), Protocol: "http"}, "target.test:443")
	if err == nil {
		conn.Close()
		t.Fatal("dialViaProxy() succeeded for upstream 407, want error")
	}
}

func TestProxyFailureIncrementsFailCountWithoutImmediateDisable(t *testing.T) {
	closedAddr := reserveClosedAddr(t)
	store := newProxyTestStore()
	addProxy(t, store, closedAddr, "http", 1)
	server := newProxyTestServer(store, proxyTestConfig(0))
	req := httptest.NewRequest(http.MethodGet, "http://example.test/fail", nil)

	server.handleHTTP(httptest.NewRecorder(), req, emptyRoute())

	proxy, err := store.GetProxyByAddress(closedAddr)
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if proxy.Status != "active" || proxy.FailCount != 1 {
		t.Fatalf("proxy after failure = status %q fail_count %d, want active/1", proxy.Status, proxy.FailCount)
	}
}

// TestProxyFailureDisablesNodeAtThreshold 覆盖 BUG-53 请求失败路径：连续失败达到
// 阈值时，节点应从 active 转为 disabled（可见/可恢复），而非停留在 active 僵尸态。
func TestProxyFailureDisablesNodeAtThreshold(t *testing.T) {
	closedAddr := reserveClosedAddr(t)
	store := newProxyTestStore()
	addProxy(t, store, closedAddr, "http", 1)
	// 预置 fail_count=2，使本次失败达到阈值 3。
	store.mu.Lock()
	id := store.addressID[closedAddr]
	p := store.proxies[id]
	p.FailCount = 2
	store.proxies[id] = p
	store.mu.Unlock()

	server := newProxyTestServer(store, proxyTestConfig(0))
	req := httptest.NewRequest(http.MethodGet, "http://example.test/fail", nil)
	server.handleHTTP(httptest.NewRecorder(), req, emptyRoute())

	proxy, err := store.GetProxyByAddress(closedAddr)
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if proxy.FailCount != 3 || proxy.Status != "disabled" {
		t.Fatalf("proxy after threshold failure = status %q fail_count %d, want disabled/3", proxy.Status, proxy.FailCount)
	}
}

// TestProxySuccessResetsFailCount 覆盖 BUG-53 请求成功路径：成功使用后 fail_count 归零。
func TestProxySuccessResetsFailCount(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	store := newProxyTestStore()
	addProxy(t, store, upstreamAddr(t, upstream.URL), "http", 1)
	store.mu.Lock()
	id := store.addressID[upstreamAddr(t, upstream.URL)]
	p := store.proxies[id]
	p.FailCount = 2
	store.proxies[id] = p
	store.mu.Unlock()

	server := newProxyTestServer(store, proxyTestConfig(0))
	req := httptest.NewRequest(http.MethodGet, "http://example.test/ok", nil)
	server.handleHTTP(httptest.NewRecorder(), req, emptyRoute())

	proxy, err := store.GetProxyByAddress(upstreamAddr(t, upstream.URL))
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if proxy.FailCount != 0 {
		t.Fatalf("fail_count after success = %d, want 0", proxy.FailCount)
	}
}

// TestHTTPOverCapBodyStreamsOnceWithoutRetry 覆盖 BUG-54：超过 maxReplayBodyBytes 的
// body 不被整体缓存重放；即使配置了重试，也只发送一次（body 不可重放）。
func TestHTTPOverCapBodyStreamsOnceWithoutRetry(t *testing.T) {
	var firstAttempt int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&firstAttempt, 1)
		io.Copy(io.Discard, r.Body)
		// 关闭连接制造一次转发失败，触发潜在重试。
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("response writer does not support hijack")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}
		conn.Close()
	}))
	defer first.Close()

	var secondAttempt int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&secondAttempt, 1)
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer second.Close()

	store := newProxyTestStore()
	addProxy(t, store, upstreamAddr(t, first.URL), "http", 1)
	addProxy(t, store, upstreamAddr(t, second.URL), "http", 2)
	server := newProxyTestServer(store, proxyTestConfig(1)) // MaxRetry=1

	bodyLen := maxReplayBodyBytes + (256 << 10) // 超限：1 MiB + 256 KiB
	req := httptest.NewRequest(http.MethodPost, "http://example.test/big", &countingReader{n: int64(bodyLen)})
	server.handleHTTP(httptest.NewRecorder(), req, emptyRoute())

	// 只应尝试一次（第一个上游），不因失败重放到第二个上游。
	if got := atomic.LoadInt32(&firstAttempt); got != 1 {
		t.Fatalf("first upstream attempts = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&secondAttempt); got != 0 {
		t.Fatalf("second upstream attempts = %d, want 0 (over-cap body must not be replayed)", got)
	}
}

// TestReadReusableBodyDoesNotBufferOverCapBody 直接验证 BUG-54 的内存语义：
// 超限 body 不被整体读入内存（只预读 cap+1），但仍可通过返回的流完整读出。
func TestReadReusableBodyDoesNotBufferOverCapBody(t *testing.T) {
	total := int64(maxReplayBodyBytes) + (512 << 10) // 超限
	cr := &countingReader{n: total}
	req := httptest.NewRequest(http.MethodPost, "http://example.test/big", cr)

	buffered, stream, replayable, err := readReusableBody(req)
	if err != nil {
		t.Fatalf("readReusableBody() error = %v", err)
	}
	if replayable {
		t.Fatal("over-cap body reported replayable, want false")
	}
	if buffered != nil {
		t.Fatalf("over-cap body buffered %d bytes into memory, want nil", len(buffered))
	}
	if stream == nil {
		t.Fatal("over-cap body stream = nil, want non-nil")
	}
	// 判定超限时最多预读 cap+1 字节，绝不整体入内存。
	if consumed := cr.consumed(); consumed > int64(maxReplayBodyBytes)+1 {
		t.Fatalf("consumed %d bytes to detect over-cap, want <= %d", consumed, maxReplayBodyBytes+1)
	}
	// 流仍可完整读出（前缀 + 剩余）。
	streamed, err := io.Copy(io.Discard, stream)
	if err != nil {
		t.Fatalf("draining stream error = %v", err)
	}
	if streamed != total {
		t.Fatalf("streamed %d bytes, want %d", streamed, total)
	}
}

// TestReadReusableBodySmallBodyIsBufferedAndReplayable 验证 BUG-54：上限内的小 body
// 被完整缓存且标记可重放。
func TestReadReusableBodySmallBodyIsBufferedAndReplayable(t *testing.T) {
	body := "small-replayable-body"
	req := httptest.NewRequest(http.MethodPost, "http://example.test/small", strings.NewReader(body))

	buffered, stream, replayable, err := readReusableBody(req)
	if err != nil {
		t.Fatalf("readReusableBody() error = %v", err)
	}
	if !replayable {
		t.Fatal("small body reported not replayable, want true")
	}
	if stream != nil {
		t.Fatal("small body stream != nil, want nil (fully buffered)")
	}
	if string(buffered) != body {
		t.Fatalf("buffered = %q, want %q", string(buffered), body)
	}
}

func TestSocks5AuthAcceptsHashOnlyConfigAndRejectsWrongPassword(t *testing.T) {
	cfg := proxyTestConfig(0)
	cfg.ProxyAuthEnabled = true
	cfg.ProxyAuthUsername = "proxy"
	cfg.ProxyAuthPassword = ""
	cfg.ProxyAuthPasswordHash = fmt.Sprintf("%x", sha256.Sum256([]byte("secret")))
	server := NewSOCKS5(nil, cfg, ":0")

	parsed := runSocks5Auth(t, server, "proxy-region-us-session-a", "secret")
	if parsed.Base != "proxy" || parsed.Region != "us" || parsed.Session != "a" {
		t.Fatalf("parsed username = %#v", parsed)
	}
	if result := runSocks5AuthExpectFailure(t, server, "proxy", "wrong"); result.err == nil {
		t.Fatal("socks5Auth() accepted wrong password")
	}
}

func TestReadSOCKS5RequestParsesFragmentedAddressTypes(t *testing.T) {
	server := NewSOCKS5(nil, proxyTestConfig(0), ":0")
	tests := []struct {
		name   string
		chunks [][]byte
		want   string
	}{
		{
			name:   "ipv4",
			chunks: [][]byte{{0x05, 0x01, 0x00, 0x01}, {127, 0, 0, 1}, {0x1f, 0x90}},
			want:   "127.0.0.1:8080",
		},
		{
			name:   "domain",
			chunks: [][]byte{{0x05, 0x01, 0x00, 0x03}, {0x0b}, []byte("example.com"), {0x01, 0xbb}},
			want:   "example.com:443",
		},
		{
			name:   "ipv6",
			chunks: [][]byte{{0x05, 0x01, 0x00, 0x04}, net.ParseIP("2001:db8::1").To16(), {0x00, 0x50}},
			want:   "[2001:db8::1]:80",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, serverConn := net.Pipe()
			defer client.Close()
			defer serverConn.Close()
			done := make(chan requestResult, 1)
			go func() {
				target, err := server.readSOCKS5Request(serverConn)
				done <- requestResult{target: target, err: err}
			}()
			for _, chunk := range tt.chunks {
				if _, err := client.Write(chunk); err != nil {
					t.Fatalf("write chunk: %v", err)
				}
			}
			result := <-done
			if result.err != nil || result.target != tt.want {
				t.Fatalf("readSOCKS5Request() = %q, %v; want %q, nil", result.target, result.err, tt.want)
			}
		})
	}
}

func TestReadSOCKS5RequestRejectsUnsupportedAddressType(t *testing.T) {
	server := NewSOCKS5(nil, proxyTestConfig(0), ":0")
	client, serverConn := net.Pipe()
	defer client.Close()
	defer serverConn.Close()
	done := make(chan error, 1)
	go func() {
		_, err := server.readSOCKS5Request(serverConn)
		done <- err
	}()
	if _, err := client.Write([]byte{0x05, 0x01, 0x00, 0x09}); err != nil {
		t.Fatalf("write request: %v", err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if reply[1] != 0x08 {
		t.Fatalf("reply code = %#x, want 0x08", reply[1])
	}
	if err := <-done; err == nil {
		t.Fatal("readSOCKS5Request() accepted unsupported address type")
	}
}

func TestSOCKS5ConnectReplyConsumesFullDomainAndIPv6Address(t *testing.T) {
	for _, reply := range [][]byte{
		append(append([]byte{0x05, 0x00, 0x00, 0x03, 0x0b}, []byte("example.com")...), 0x00, 0x00),
		append(append([]byte{0x05, 0x00, 0x00, 0x04}, net.ParseIP("2001:db8::2").To16()...), 0x00, 0x00),
	} {
		buf := bytes.NewBuffer(append(reply, []byte("APP")...))
		if err := readSOCKS5ConnectReply(buf); err != nil {
			t.Fatalf("readSOCKS5ConnectReply() error = %v", err)
		}
		if got := buf.String(); got != "APP" {
			t.Fatalf("remaining bytes = %q, want APP", got)
		}
	}
}

func TestDialViaSocks5ProxySupportsIPv6TargetAndConsumesIPv6Reply(t *testing.T) {
	upstream, gotReq := startFakeSocks5Upstream(t)
	server := NewSOCKS5(nil, proxyTestConfig(0), ":0")
	conn, err := server.dialViaProxy(&storage.Proxy{Address: upstream, Protocol: "socks5"}, "[2001:db8::1]:443")
	if err != nil {
		t.Fatalf("dialViaProxy() error = %v", err)
	}
	defer conn.Close()
	data := make([]byte, 3)
	if _, err := io.ReadFull(conn, data); err != nil {
		t.Fatalf("read app data: %v", err)
	}
	if string(data) != "APP" {
		t.Fatalf("app data = %q, want APP", string(data))
	}
	req := <-gotReq
	if req[3] != 0x04 {
		t.Fatalf("request ATYP = %#x, want IPv6", req[3])
	}
}

func TestSocks5MaxRetryZeroAttemptsOnceAndDoesNotDisable(t *testing.T) {
	firstAddr := reserveClosedAddr(t)
	secondAddr := reserveClosedAddr(t)
	store := newProxyTestStore()
	addProxy(t, store, firstAddr, "socks5", 1)
	addProxy(t, store, secondAddr, "socks5", 2)
	server := newSocks5TestServer(store, proxyTestConfig(0))

	client, serverConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		server.handleConnection(serverConn)
		close(done)
	}()
	writeSocks5NoAuthHandshake(t, client)
	writeSocks5DomainRequest(t, client, "example.com", 443)
	reply := make([]byte, 10)
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatalf("read failure reply: %v", err)
	}
	client.Close()
	<-done

	first, err := store.GetProxyByAddress(firstAddr)
	if err != nil {
		t.Fatalf("GetProxyByAddress(first) error = %v", err)
	}
	second, err := store.GetProxyByAddress(secondAddr)
	if err != nil {
		t.Fatalf("GetProxyByAddress(second) error = %v", err)
	}
	if first.Status != "active" || first.FailCount != 1 {
		t.Fatalf("first proxy = status %q fail_count %d, want active/1", first.Status, first.FailCount)
	}
	if second.FailCount != 0 {
		t.Fatalf("second proxy fail_count = %d, want 0", second.FailCount)
	}
}

func TestSOCKS5HandshakePreservesPipelinedRequest(t *testing.T) {
	server := NewSOCKS5(nil, proxyTestConfig(0), ":0")
	client, serverConn := net.Pipe()
	defer client.Close()
	defer serverConn.Close()

	done := make(chan requestResult, 1)
	go func() {
		_, err := server.socks5Handshake(serverConn)
		if err != nil {
			done <- requestResult{err: err}
			return
		}
		target, err := server.readSOCKS5Request(serverConn)
		done <- requestResult{target: target, err: err}
	}()

	request := []byte{0x05, 0x01, 0x00, 0x03, 0x0b}
	request = append(request, []byte("example.com")...)
	request = append(request, 0x01, 0xbb)
	pipelined := append([]byte{0x05, 0x01, 0x00}, request...)
	writeDone := make(chan error, 1)
	go func() {
		_, err := client.Write(pipelined)
		writeDone <- err
	}()

	methodReply := make([]byte, 2)
	if err := readFullDeadline(t, client, methodReply); err != nil {
		t.Fatalf("read method reply: %v", err)
	}
	if methodReply[0] != 0x05 || methodReply[1] != 0x00 {
		t.Fatalf("method reply = %#v, want [0x05 0x00]", methodReply)
	}
	select {
	case result := <-done:
		if result.err != nil || result.target != "example.com:443" {
			t.Fatalf("pipelined request = %q, %v; want example.com:443, nil", result.target, result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("pipelined SOCKS5 request was consumed by handshake")
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("write pipelined greeting and request: %v", err)
	}
}

func TestSOCKS5AuthPreservesBytesAfterAuthFrame(t *testing.T) {
	cfg := proxyTestConfig(0)
	cfg.ProxyAuthEnabled = true
	server := NewSOCKS5(nil, cfg, ":0")
	client, serverConn := net.Pipe()
	defer client.Close()
	defer serverConn.Close()

	authDone := make(chan error, 1)
	go func() {
		_, err := server.socks5Auth(serverConn)
		authDone <- err
	}()
	authFrame := []byte{0x01, 0x05}
	authFrame = append(authFrame, []byte("proxy")...)
	authFrame = append(authFrame, 0x06)
	authFrame = append(authFrame, []byte("secret")...)
	pipelined := append(authFrame, []byte("NEXT")...)
	writeDone := make(chan error, 1)
	go func() {
		_, err := client.Write(pipelined)
		writeDone <- err
	}()

	authReply := make([]byte, 2)
	if err := readFullDeadline(t, client, authReply); err != nil {
		t.Fatalf("read auth reply: %v", err)
	}
	if authReply[0] != 0x01 || authReply[1] != 0x00 {
		t.Fatalf("auth reply = %#v, want [0x01 0x00]", authReply)
	}
	if err := <-authDone; err != nil {
		t.Fatalf("socks5Auth() error = %v", err)
	}
	remaining := make([]byte, 4)
	_ = serverConn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(serverConn, remaining); err != nil {
		t.Fatalf("read bytes after auth frame: %v (auth parser consumed pipelined bytes)", err)
	}
	if string(remaining) != "NEXT" {
		t.Fatalf("bytes after auth frame = %q, want NEXT", remaining)
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("write pipelined auth frame: %v", err)
	}
}

func TestReadSOCKS5RequestRejectsNonZeroReservedByte(t *testing.T) {
	server := NewSOCKS5(nil, proxyTestConfig(0), ":0")
	request := []byte{0x05, 0x01, 0x01, 0x01, 127, 0, 0, 1, 0x00, 0x50}
	if target, err := server.readSOCKS5Request(&bufferConn{Reader: bytes.NewReader(request)}); err == nil {
		t.Fatalf("readSOCKS5Request() accepted non-zero RSV with target %q", target)
	}
}

func TestHTTPConnectDialTimesOutWhenUpstreamDoesNotRespond(t *testing.T) {
	upstream := startSilentTCPServer(t)
	server := New(nil, proxyTestConfig(1), ":0")

	done := make(chan error, 1)
	go func() {
		conn, err := server.dialViaProxy(&storage.Proxy{Address: upstream, Protocol: "http"}, "target.test:443")
		if conn != nil {
			conn.Close()
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("dialViaProxy() succeeded against silent HTTP proxy, want timeout error")
		}
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("dialViaProxy() did not time out while waiting for HTTP CONNECT response")
	}
}

func TestSocks5UpstreamDialTimesOutWhenHandshakeDoesNotRespond(t *testing.T) {
	upstream := startSilentTCPServer(t)
	server := NewSOCKS5(nil, proxyTestConfig(1), ":0")

	done := make(chan error, 1)
	go func() {
		conn, err := server.dialViaProxy(&storage.Proxy{Address: upstream, Protocol: "socks5"}, "target.test:443")
		if conn != nil {
			conn.Close()
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("dialViaProxy() succeeded against silent SOCKS5 proxy, want timeout error")
		}
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("dialViaProxy() did not time out while waiting for SOCKS5 handshake")
	}
}

func TestHTTPInboundSocks5UpstreamDialTimesOutWhenHandshakeDoesNotRespond(t *testing.T) {
	upstream := startSilentTCPServer(t)
	server := New(nil, proxyTestConfig(1), ":0")

	done := make(chan error, 1)
	go func() {
		conn, err := server.dialViaProxy(&storage.Proxy{Address: upstream, Protocol: "socks5"}, "target.test:443")
		if conn != nil {
			conn.Close()
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("dialViaProxy() succeeded against silent SOCKS5 proxy, want timeout error")
		}
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("dialViaProxy() did not time out while waiting for SOCKS5 handshake")
	}
}

func TestHTTPInboundSocks5UpstreamDialWithZeroTimeoutDoesNotExpireImmediately(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// SOCKS5 greeting: VER=5, METHOD=no-auth
		greeting := make([]byte, 3)
		if _, err := io.ReadFull(conn, greeting); err != nil {
			return
		}
		if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
			return
		}
		// Read CONNECT request header (VER CMD RSV ATYP) then address+port.
		header := make([]byte, 4)
		if _, err := io.ReadFull(conn, header); err != nil {
			return
		}
		switch header[3] {
		case 0x01:
			if _, err := io.ReadFull(conn, make([]byte, 4+2)); err != nil {
				return
			}
		case 0x03:
			lenBuf := make([]byte, 1)
			if _, err := io.ReadFull(conn, lenBuf); err != nil {
				return
			}
			if _, err := io.ReadFull(conn, make([]byte, int(lenBuf[0])+2)); err != nil {
				return
			}
		case 0x04:
			if _, err := io.ReadFull(conn, make([]byte, 16+2)); err != nil {
				return
			}
		default:
			return
		}
		time.Sleep(50 * time.Millisecond)
		// Success reply: VER REP RSV ATYP=IPv4 0.0.0.0:0
		_, _ = conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		time.Sleep(50 * time.Millisecond)
	}()

	cfg := proxyTestConfig(0)
	cfg.ValidateTimeout = 0
	server := New(nil, cfg, ":0")
	conn, err := server.dialViaProxy(&storage.Proxy{Address: listener.Addr().String(), Protocol: "socks5"}, "target.test:443")
	if err != nil {
		t.Fatalf("dialViaProxy() with zero timeout error = %v, want no immediate deadline", err)
	}
	conn.Close()
}

func TestHTTPConnectDialWithZeroTimeoutDoesNotExpireImmediately(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		if _, err := http.ReadRequest(reader); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
		_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		time.Sleep(50 * time.Millisecond)
	}()

	cfg := proxyTestConfig(0)
	cfg.ValidateTimeout = 0
	server := New(nil, cfg, ":0")
	conn, err := server.dialViaProxy(&storage.Proxy{Address: listener.Addr().String(), Protocol: "http"}, "target.test:443")
	if err != nil {
		t.Fatalf("dialViaProxy() with zero timeout error = %v, want no immediate deadline", err)
	}
	conn.Close()
}

func TestHTTPInboundPartialHeaderTimesOut(t *testing.T) {
	cfg := proxyTestConfig(0)
	cfg.ValidateTimeout = 1
	server := New(nil, cfg, "127.0.0.1:0")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := server.httpServer()
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	t.Cleanup(func() {
		_ = srv.Close()
		<-done
	})

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	// 半请求头：故意不发送最终 \r\n\r\n，模拟 HTTP/CONNECT Slowloris。
	if _, err := conn.Write([]byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n")); err != nil {
		t.Fatalf("write partial headers: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
	buf := make([]byte, 1)
	_, readErr := conn.Read(buf)
	if readErr == nil {
		t.Fatal("partial HTTP CONNECT headers stayed open past ValidateTimeout")
	}
	if ne, ok := readErr.(net.Error); ok && ne.Timeout() {
		t.Fatalf("client read timed out waiting for server to close partial headers: %v", readErr)
	}
}

func TestSOCKS5InboundPartialGreetingTimesOut(t *testing.T) {
	cfg := proxyTestConfig(0)
	cfg.ValidateTimeout = 1
	server := newSocks5TestServer(newProxyTestStore(), cfg)
	client, serverConn := net.Pipe()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		server.handleConnection(serverConn)
		close(done)
	}()
	if _, err := client.Write([]byte{0x05}); err != nil {
		t.Fatalf("write partial greeting: %v", err)
	}

	select {
	case <-done:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("partial SOCKS5 greeting kept the inbound connection open past ValidateTimeout")
	}
}

func TestSOCKS5InboundDeadlineIsClearedAfterRequest(t *testing.T) {
	_, echoPort := startLocalEcho(t)
	cfg := proxyTestConfig(0)
	cfg.ValidateTimeout = 1
	server := newSocks5TestServer(newProxyTestStore(), cfg)
	client, serverConn := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	go server.handleConnection(serverConn)

	writeSocks5NoAuthHandshake(t, client)
	writeSocks5DomainRequest(t, client, "127.0.0.1", echoPort)
	reply := make([]byte, 10)
	if err := readFullDeadline(t, client, reply); err != nil {
		t.Fatalf("read SOCKS5 reply: %v", err)
	}
	if reply[1] != 0x00 {
		t.Fatalf("SOCKS5 reply code = %#x, want success", reply[1])
	}

	time.Sleep(1100 * time.Millisecond)
	payload := []byte("after-handshake-deadline")
	if err := writeDeadline(t, client, payload); err != nil {
		t.Fatalf("write after handshake timeout: %v", err)
	}
	got := make([]byte, len(payload))
	if err := readFullDeadline(t, client, got); err != nil {
		t.Fatalf("read after handshake timeout: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("echo = %q, want %q", got, payload)
	}
}

func TestSOCKS5InboundPartialAuthAndRequestTimeOut(t *testing.T) {
	tests := []struct {
		name        string
		authEnabled bool
		write       func(*testing.T, net.Conn)
	}{
		{
			name:        "partial auth",
			authEnabled: true,
			write: func(t *testing.T, conn net.Conn) {
				if _, err := conn.Write([]byte{0x05, 0x01, 0x02}); err != nil {
					t.Fatalf("write greeting: %v", err)
				}
				reply := make([]byte, 2)
				if err := readFullDeadline(t, conn, reply); err != nil {
					t.Fatalf("read method reply: %v", err)
				}
				if _, err := conn.Write([]byte{0x01, 0x05, 'p'}); err != nil {
					t.Fatalf("write partial auth: %v", err)
				}
			},
		},
		{
			name: "partial request",
			write: func(t *testing.T, conn net.Conn) {
				writeSocks5NoAuthHandshake(t, conn)
				if _, err := conn.Write([]byte{0x05, 0x01}); err != nil {
					t.Fatalf("write partial request: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := proxyTestConfig(0)
			cfg.ValidateTimeout = 1
			cfg.ProxyAuthEnabled = tt.authEnabled
			server := newSocks5TestServer(newProxyTestStore(), cfg)
			client, serverConn := net.Pipe()
			defer client.Close()
			done := make(chan struct{})
			go func() {
				server.handleConnection(serverConn)
				close(done)
			}()

			tt.write(t, client)
			select {
			case <-done:
			case <-time.After(1500 * time.Millisecond):
				t.Fatal("partial SOCKS5 frame kept the inbound connection open past ValidateTimeout")
			}
		})
	}
}

// countingReader 产生指定字节数的零填充数据，并记录被实际读取的字节数，
// 用于验证超限 body 不会被整体读入内存（BUG-54）。
type countingReader struct {
	n    int64 // 剩余待产生字节
	read int64 // 已被读取字节（原子累加）
}

func (c *countingReader) Read(p []byte) (int, error) {
	if c.n <= 0 {
		return 0, io.EOF
	}
	n := len(p)
	if int64(n) > c.n {
		n = int(c.n)
	}
	for i := 0; i < n; i++ {
		p[i] = 'x'
	}
	c.n -= int64(n)
	atomic.AddInt64(&c.read, int64(n))
	if c.n <= 0 {
		return n, io.EOF
	}
	return n, nil
}

func (c *countingReader) consumed() int64 {
	return atomic.LoadInt64(&c.read)
}

type requestResult struct {
	target string
	err    error
}

type bufferConn struct {
	io.Reader
}

func (c *bufferConn) Write(p []byte) (int, error)      { return len(p), nil }
func (c *bufferConn) Close() error                     { return nil }
func (c *bufferConn) LocalAddr() net.Addr              { return nil }
func (c *bufferConn) RemoteAddr() net.Addr             { return nil }
func (c *bufferConn) SetDeadline(time.Time) error      { return nil }
func (c *bufferConn) SetReadDeadline(time.Time) error  { return nil }
func (c *bufferConn) SetWriteDeadline(time.Time) error { return nil }

type socks5AuthResult struct {
	parsedBase    string
	parsedRegion  string
	parsedSession string
	err           error
}

type fakeProxyStore struct {
	mu        sync.Mutex
	proxies   map[int64]storage.Proxy
	nextID    int64
	addressID map[string]int64
}

func newProxyTestStore() *fakeProxyStore {
	return &fakeProxyStore{proxies: map[int64]storage.Proxy{}, addressID: map[string]int64{}}
}

func newProxyTestServer(store *fakeProxyStore, cfg *config.Config) *Server {
	return &Server{storage: store, cfg: cfg, port: ":0", sessions: SessionStore(cfg)}
}

func newSocks5TestServer(store *fakeProxyStore, cfg *config.Config) *SOCKS5Server {
	return &SOCKS5Server{storage: store, cfg: cfg, port: ":0", sessions: SessionStore(cfg)}
}

func (s *fakeProxyStore) GetByRegion(region string, excludes []int64) ([]storage.Proxy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	excluded := make(map[int64]bool, len(excludes))
	for _, id := range excludes {
		excluded[id] = true
	}
	var proxies []storage.Proxy
	for _, proxy := range s.proxies {
		if excluded[proxy.ID] {
			continue
		}
		if region != "" && proxy.Region != region {
			continue
		}
		if (proxy.Status == "active" || proxy.Status == "degraded") && proxy.FailCount < 3 {
			proxies = append(proxies, proxy)
		}
	}
	return proxies, nil
}

func (s *fakeProxyStore) GetProxyByID(id int64) (*storage.Proxy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	proxy, ok := s.proxies[id]
	if !ok {
		return nil, fmt.Errorf("proxy id %d not found", id)
	}
	return &proxy, nil
}

func (s *fakeProxyStore) GetProxyByAddress(address string) (*storage.Proxy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.addressID[address]
	if !ok {
		return nil, fmt.Errorf("proxy %s not found", address)
	}
	proxy, ok := s.proxies[id]
	if !ok {
		return nil, fmt.Errorf("proxy %s not found", address)
	}
	return &proxy, nil
}

func (s *fakeProxyStore) RecordProxyUseByID(id int64, success bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	proxy, ok := s.proxies[id]
	if !ok {
		return fmt.Errorf("proxy id %d not found", id)
	}
	proxy.UseCount++
	if success {
		proxy.SuccessCount++
		// 镜像 storage.RecordProxyUseByID 的成功清零语义（BUG-53）。
		proxy.FailCount = 0
	} else {
		proxy.FailCount++
	}
	s.proxies[id] = proxy
	return nil
}

func (s *fakeProxyStore) RecordProxyFailureByID(id int64, threshold int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	proxy, ok := s.proxies[id]
	if !ok {
		return fmt.Errorf("proxy id %d not found", id)
	}
	proxy.UseCount++
	proxy.FailCount++
	if proxy.FailCount >= threshold {
		proxy.Status = "disabled"
	}
	s.proxies[id] = proxy
	return nil
}

func (s *fakeProxyStore) DisableProxyByID(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	proxy, ok := s.proxies[id]
	if !ok {
		return fmt.Errorf("proxy id %d not found", id)
	}
	proxy.Status = "disabled"
	s.proxies[id] = proxy
	return nil
}

func proxyTestConfig(maxRetry int) *config.Config {
	return &config.Config{
		ValidateTimeout:       1,
		MaxRetry:              maxRetry,
		SessionTTLMinutes:     1,
		ProxyAuthUsername:     "proxy",
		ProxyAuthPassword:     "secret",
		ProxyAuthEnabled:      false,
		ProxyAuthPasswordHash: fmt.Sprintf("%x", sha256.Sum256([]byte("secret"))),
	}
}

func emptyRoute() auth.ParsedUsername {
	return auth.ParsedUsername{}
}

func addProxy(t *testing.T, store *fakeProxyStore, address string, protocol string, latency int) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	store.nextID++
	id := store.nextID
	store.proxies[id] = storage.Proxy{ID: id, Address: address, Protocol: protocol, Latency: latency, Status: "active"}
	store.addressID[address] = id
}

func upstreamAddr(t *testing.T, rawURL string) string {
	t.Helper()
	addr := strings.TrimPrefix(rawURL, "http://")
	if addr == rawURL {
		t.Fatalf("unexpected upstream URL %q", rawURL)
	}
	return addr
}

func reserveClosedAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return addr
}

func runSocks5Auth(t *testing.T, server *SOCKS5Server, username string, password string) auth.ParsedUsername {
	t.Helper()
	result := runSocks5AuthExpectFailure(t, server, username, password)
	if result.err != nil {
		t.Fatalf("socks5Auth() error = %v", result.err)
	}
	return auth.ParsedUsername{Base: result.parsedBase, Region: result.parsedRegion, Session: result.parsedSession}
}

func runSocks5AuthExpectFailure(t *testing.T, server *SOCKS5Server, username string, password string) socks5AuthResult {
	t.Helper()
	client, serverConn := net.Pipe()
	defer client.Close()
	defer serverConn.Close()
	done := make(chan socks5AuthResult, 1)
	go func() {
		parsed, err := server.socks5Auth(serverConn)
		done <- socks5AuthResult{parsedBase: parsed.Base, parsedRegion: parsed.Region, parsedSession: parsed.Session, err: err}
	}()
	writeSocks5Auth(t, client, username, password)
	reader := bufio.NewReader(client)
	if _, err := reader.ReadByte(); err != nil {
		t.Fatalf("read auth version: %v", err)
	}
	if _, err := reader.ReadByte(); err != nil {
		t.Fatalf("read auth status: %v", err)
	}
	return <-done
}

func writeSocks5NoAuthHandshake(t *testing.T, conn net.Conn) {
	t.Helper()
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatalf("write greeting: %v", err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("read method selection: %v", err)
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		t.Fatalf("method selection = %#v, want [0x05 0x00]", reply)
	}
}

func writeSocks5DomainRequest(t *testing.T, conn net.Conn, host string, port uint16) {
	t.Helper()
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	req = append(req, []byte(host)...)
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, port)
	req = append(req, portBytes...)
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("write request: %v", err)
	}
}

func startFakeSocks5Upstream(t *testing.T) (string, <-chan []byte) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	reqCh := make(chan []byte, 1)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		greeting := make([]byte, 3)
		if _, err := io.ReadFull(conn, greeting); err != nil {
			return
		}
		_, _ = conn.Write([]byte{0x05, 0x00})
		req := make([]byte, 22)
		if _, err := io.ReadFull(conn, req); err != nil {
			return
		}
		reqCh <- req
		reply := append([]byte{0x05, 0x00, 0x00, 0x04}, net.ParseIP("2001:db8::2").To16()...)
		reply = append(reply, 0x00, 0x00)
		reply = append(reply, []byte("APP")...)
		_, _ = conn.Write(reply)
		time.Sleep(50 * time.Millisecond)
	}()
	return listener.Addr().String(), reqCh
}

func startSilentTCPServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var conns sync.Map
	t.Cleanup(func() {
		_ = listener.Close()
		conns.Range(func(key, _ any) bool {
			_ = key.(net.Conn).Close()
			return true
		})
	})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conns.Store(conn, struct{}{})
		}
	}()
	return listener.Addr().String()
}
