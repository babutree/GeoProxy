package proxy

import (
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/affinity"
	"github.com/babutree/GeoProxy/config"
)

func TestSOCKS5ClientCloseStopsSilentUpstreamRelay(t *testing.T) {
	upstreamListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake SOCKS5 upstream: %v", err)
	}
	var upstreamMu sync.Mutex
	var upstreamConn net.Conn
	upstreamClosed := make(chan struct{})
	t.Cleanup(func() {
		_ = upstreamListener.Close()
		upstreamMu.Lock()
		if upstreamConn != nil {
			_ = upstreamConn.Close()
		}
		upstreamMu.Unlock()
	})
	go func() {
		conn, acceptErr := upstreamListener.Accept()
		if acceptErr != nil {
			return
		}
		upstreamMu.Lock()
		upstreamConn = conn
		upstreamMu.Unlock()
		defer conn.Close()
		defer close(upstreamClosed)

		greeting := make([]byte, 3)
		if _, readErr := io.ReadFull(conn, greeting); readErr != nil {
			return
		}
		if _, writeErr := conn.Write([]byte{0x05, 0x00}); writeErr != nil {
			return
		}
		requestHeader := make([]byte, 4)
		if _, readErr := io.ReadFull(conn, requestHeader); readErr != nil {
			return
		}
		if _, readErr := readSOCKS5Address(conn, requestHeader[3]); readErr != nil {
			return
		}
		port := make([]byte, 2)
		if _, readErr := io.ReadFull(conn, port); readErr != nil {
			return
		}
		if _, writeErr := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); writeErr != nil {
			return
		}
		_, _ = io.Copy(io.Discard, conn)
	}()

	store := newProxyTestStore()
	addProxy(t, store, upstreamListener.Addr().String(), "socks5", 1)
	server := newSocks5TestServer(store, proxyTestConfig(0))

	inboundListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen inbound TCP: %v", err)
	}
	clientConn, err := net.Dial("tcp", inboundListener.Addr().String())
	if err != nil {
		_ = inboundListener.Close()
		t.Fatalf("dial inbound TCP: %v", err)
	}
	serverConn, err := inboundListener.Accept()
	_ = inboundListener.Close()
	if err != nil {
		_ = clientConn.Close()
		t.Fatalf("accept inbound TCP: %v", err)
	}
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
	})

	handlerDone := make(chan struct{})
	go func() {
		server.handleConnection(serverConn)
		close(handlerDone)
	}()
	if err := clientConn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set client deadline: %v", err)
	}
	writeSocks5NoAuthHandshake(t, clientConn)
	writeSocks5DomainRequest(t, clientConn, "relay.example", 443)
	reply := make([]byte, 10)
	if _, err := io.ReadFull(clientConn, reply); err != nil {
		t.Fatalf("read SOCKS5 CONNECT reply: %v", err)
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		t.Fatalf("SOCKS5 CONNECT reply = %#v, want VER=0x05 REP=0x00", reply[:2])
	}
	if err := clientConn.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}

	select {
	case <-handlerDone:
	case <-time.After(1500 * time.Millisecond):
		t.Error("客户端关闭后 handleConnection 未在 1 秒内结束")
	}
	select {
	case <-upstreamClosed:
	case <-time.After(200 * time.Millisecond):
		t.Error("客户端关闭后静默上游连接仍未关闭")
	}
}

func TestSOCKS5RelayRecordsSuccessBeforeLongLivedTunnelCloses(t *testing.T) {
	upstream := startAccountingSOCKS5Upstream(t, []byte("APP"))
	store := newProxyTestStore()
	addProxy(t, store, upstream.address, "socks5", 1)
	server := newSocks5TestServer(store, proxyTestConfig(0))
	clientConn, serverConn := newSOCKS5RelayTCPPair(t)
	handlerDone := startSOCKS5RelayHandler(server, serverConn)

	establishNoAuthSOCKS5Relay(t, clientConn)
	appData := make([]byte, 3)
	if _, err := io.ReadFull(clientConn, appData); err != nil {
		t.Fatalf("read relayed APP data: %v", err)
	}
	if string(appData) != "APP" {
		t.Fatalf("relayed data = %q, want APP", appData)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		usedProxy, err := store.GetProxyByAddress(upstream.address)
		if err != nil {
			t.Fatalf("GetProxyByAddress() error = %v", err)
		}
		if usedProxy.SuccessCount == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Errorf("客户端收到 APP 后长连接仍未记成功: success_count=%d", usedProxy.SuccessCount)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case <-handlerDone:
		t.Error("上游保持连接时 handler 提前结束")
	default:
	}
	if err := clientConn.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}
	waitSOCKS5RelayHandler(t, handlerDone)

	usedProxy, err := store.GetProxyByAddress(upstream.address)
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if usedProxy.SuccessCount != 1 || usedProxy.FailCount != 0 {
		t.Fatalf("正常转发计数 = success %d fail %d, want 1/0", usedProxy.SuccessCount, usedProxy.FailCount)
	}
}

func TestSOCKS5RelayWithNoUpstreamDataDoesNotRecordSuccessAndReleasesSession(t *testing.T) {
	upstream := startAccountingSOCKS5Upstream(t, nil)
	server, store := newSessionSOCKS5RelayServer(t, upstream.address)
	setSOCKS5RelayFailCount(t, store, upstream.address, 2)
	clientConn, serverConn := newSOCKS5RelayTCPPair(t)
	handlerDone := startSOCKS5RelayHandler(server, serverConn)

	establishSessionSOCKS5Relay(t, clientConn, "zerodata")
	request := []byte("GET / HTTP/1.1\r\nHost: relay.example\r\nConnection: close\r\n\r\n")
	if _, err := clientConn.Write(request); err != nil {
		t.Fatalf("write tunneled HTTP request: %v", err)
	}
	if err := clientConn.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}
	waitSOCKS5RelayHandler(t, handlerDone)

	unusedProxy, err := store.GetProxyByAddress(upstream.address)
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if unusedProxy.UseCount != 0 || unusedProxy.SuccessCount != 0 || unusedProxy.FailCount != 2 {
		t.Fatalf(
			"零上游数据计数 = use %d success %d fail %d, want 0/0/2",
			unusedProxy.UseCount,
			unusedProxy.SuccessCount,
			unusedProxy.FailCount,
		)
	}
	if binding, ok := server.sessions.Get("zerodata"); ok {
		t.Fatalf("零上游数据的 session 仍被绑定: %#v", binding)
	}
}

func TestSOCKS5RelayPreservesPromptResponseAfterClientCloseWrite(t *testing.T) {
	request := []byte("GET / HTTP/1.1\r\nHost: relay.example\r\nConnection: close\r\n\r\n")
	response := []byte("HTTP/1.1 204 No Content\r\nContent-Length: 0\r\n\r\n")
	upstream := startControlledSOCKS5Upstream(t, func(conn net.Conn) error {
		gotRequest, err := io.ReadAll(conn)
		if err != nil {
			return err
		}
		if string(gotRequest) != string(request) {
			return fmt.Errorf("request = %q, want %q", gotRequest, request)
		}
		_, err = conn.Write(response)
		return err
	})
	store := newProxyTestStore()
	addProxy(t, store, upstream.address, "socks5", 1)
	server := newSocks5TestServer(store, proxyTestConfig(0))
	clientConn, serverConn := newSOCKS5RelayTCPPair(t)
	handlerDone := startSOCKS5RelayHandler(server, serverConn)

	establishNoAuthSOCKS5Relay(t, clientConn)
	if _, err := clientConn.Write(request); err != nil {
		t.Fatalf("write tunneled HTTP request: %v", err)
	}
	tcpClient, ok := clientConn.(*net.TCPConn)
	if !ok {
		t.Fatalf("client connection type = %T, want *net.TCPConn", clientConn)
	}
	if err := tcpClient.CloseWrite(); err != nil {
		t.Fatalf("client CloseWrite: %v", err)
	}
	if err := clientConn.SetReadDeadline(time.Now().Add(1500 * time.Millisecond)); err != nil {
		t.Fatalf("set response deadline: %v", err)
	}
	gotResponse := make([]byte, len(response))
	if _, err := io.ReadFull(clientConn, gotResponse); err != nil {
		t.Fatalf("read response after CloseWrite: %v", err)
	}
	if string(gotResponse) != string(response) {
		t.Fatalf("response = %q, want %q", gotResponse, response)
	}
	waitSOCKS5RelayHandler(t, handlerDone)
}

func TestSOCKS5RelayAllowsDelayedResponseWithinConfiguredIdleTimeout(t *testing.T) {
	request := []byte("GET /slow HTTP/1.1\r\nHost: relay.example\r\nConnection: close\r\n\r\n")
	response := []byte("HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\nSLOW")
	upstream := startControlledSOCKS5Upstream(t, func(conn net.Conn) error {
		gotRequest, err := io.ReadAll(conn)
		if err != nil {
			return err
		}
		if string(gotRequest) != string(request) {
			return fmt.Errorf("request = %q, want %q", gotRequest, request)
		}
		time.Sleep(700 * time.Millisecond)
		_, err = conn.Write(response)
		return err
	})
	store := newProxyTestStore()
	addProxy(t, store, upstream.address, "socks5", 1)
	server := newSocks5TestServer(store, proxyTestConfig(0))
	clientConn, serverConn := newSOCKS5RelayTCPPair(t)
	handlerDone := startSOCKS5RelayHandler(server, serverConn)

	establishNoAuthSOCKS5Relay(t, clientConn)
	if _, err := clientConn.Write(request); err != nil {
		t.Fatalf("write delayed request: %v", err)
	}
	if err := clientConn.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatalf("client CloseWrite: %v", err)
	}
	if err := clientConn.SetReadDeadline(time.Now().Add(1500 * time.Millisecond)); err != nil {
		t.Fatalf("set delayed response deadline: %v", err)
	}
	gotResponse := make([]byte, len(response))
	if _, err := io.ReadFull(clientConn, gotResponse); err != nil {
		t.Fatalf("read delayed response: %v", err)
	}
	if string(gotResponse) != string(response) {
		t.Fatalf("response = %q, want %q", gotResponse, response)
	}
	waitSOCKS5RelayHandler(t, handlerDone)
}

func TestSOCKS5RelayPreservesUpstreamFirstHalfClose(t *testing.T) {
	payload := []byte("remaining-client-payload")
	received := make(chan []byte, 1)
	upstream := startControlledSOCKS5Upstream(t, func(conn net.Conn) error {
		tcpConn, ok := conn.(*net.TCPConn)
		if !ok {
			return fmt.Errorf("upstream connection type = %T, want *net.TCPConn", conn)
		}
		if err := tcpConn.CloseWrite(); err != nil {
			return err
		}
		gotPayload := make([]byte, len(payload))
		if _, err := io.ReadFull(conn, gotPayload); err != nil {
			return err
		}
		received <- gotPayload
		return nil
	})
	store := newProxyTestStore()
	addProxy(t, store, upstream.address, "socks5", 1)
	server := newSocks5TestServer(store, proxyTestConfig(0))
	clientConn, serverConn := newSOCKS5RelayTCPPair(t)
	handlerDone := startSOCKS5RelayHandler(server, serverConn)

	establishNoAuthSOCKS5Relay(t, clientConn)
	if err := clientConn.SetReadDeadline(time.Now().Add(1500 * time.Millisecond)); err != nil {
		t.Fatalf("set upstream EOF deadline: %v", err)
	}
	one := make([]byte, 1)
	if n, err := clientConn.Read(one); n != 0 || err != io.EOF {
		t.Fatalf("read upstream half-close = %d, %v; want 0, EOF", n, err)
	}
	if _, err := clientConn.Write(payload); err != nil {
		t.Fatalf("write after upstream half-close: %v", err)
	}
	if err := clientConn.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatalf("client CloseWrite: %v", err)
	}
	select {
	case gotPayload := <-received:
		if string(gotPayload) != string(payload) {
			t.Fatalf("upstream payload = %q, want %q", gotPayload, payload)
		}
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("上游 half-close 后未收到客户端剩余 payload")
	}
	waitSOCKS5RelayHandler(t, handlerDone)
}

func TestSOCKS5RelayReapsFirstByteThenIdle(t *testing.T) {
	request := []byte("GET /stream HTTP/1.1\r\nHost: relay.example\r\n\r\n")
	releaseUpstream := make(chan struct{})
	t.Cleanup(func() { close(releaseUpstream) })
	upstream := startControlledSOCKS5Upstream(t, func(conn net.Conn) error {
		gotRequest, err := io.ReadAll(conn)
		if err != nil {
			return err
		}
		if string(gotRequest) != string(request) {
			return fmt.Errorf("request = %q, want %q", gotRequest, request)
		}
		if _, err := conn.Write([]byte("H")); err != nil {
			return err
		}
		<-releaseUpstream
		return nil
	})
	store := newProxyTestStore()
	addProxy(t, store, upstream.address, "socks5", 1)
	server := newSocks5TestServer(store, proxyTestConfig(0))
	clientConn, serverConn := newSOCKS5RelayTCPPair(t)
	handlerDone := startSOCKS5RelayHandler(server, serverConn)

	establishNoAuthSOCKS5Relay(t, clientConn)
	if _, err := clientConn.Write(request); err != nil {
		t.Fatalf("write streaming request: %v", err)
	}
	if err := clientConn.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatalf("client CloseWrite: %v", err)
	}
	if err := clientConn.SetReadDeadline(time.Now().Add(1500 * time.Millisecond)); err != nil {
		t.Fatalf("set first-byte deadline: %v", err)
	}
	one := make([]byte, 1)
	if _, err := io.ReadFull(clientConn, one); err != nil {
		t.Fatalf("read first response byte: %v", err)
	}
	if string(one) != "H" {
		t.Fatalf("first response byte = %q, want H", one)
	}
	select {
	case <-handlerDone:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("首字节后上游静默，handler 未在配置 idle 时间内结束")
	}
}

type accountingSOCKS5Upstream struct {
	address string
	done    <-chan error
}

func startAccountingSOCKS5Upstream(t *testing.T, appData []byte) accountingSOCKS5Upstream {
	t.Helper()
	return startControlledSOCKS5Upstream(t, func(conn net.Conn) error {
		if len(appData) > 0 {
			if _, err := conn.Write(appData); err != nil {
				return err
			}
		}
		_, err := io.Copy(io.Discard, conn)
		return err
	})
}

func startControlledSOCKS5Upstream(t *testing.T, relay func(net.Conn) error) accountingSOCKS5Upstream {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen accounting SOCKS5 upstream: %v", err)
	}
	done := make(chan error, 1)
	var connMu sync.Mutex
	var accepted net.Conn
	t.Cleanup(func() {
		_ = listener.Close()
		connMu.Lock()
		if accepted != nil {
			_ = accepted.Close()
		}
		connMu.Unlock()
	})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		connMu.Lock()
		accepted = conn
		connMu.Unlock()
		defer conn.Close()
		if handshakeErr := completeAccountingSOCKS5Handshake(conn); handshakeErr != nil {
			done <- handshakeErr
			return
		}
		done <- relay(conn)
	}()
	return accountingSOCKS5Upstream{address: listener.Addr().String(), done: done}
}

func completeAccountingSOCKS5Handshake(conn net.Conn) error {
	greetingHeader := make([]byte, 2)
	if _, err := io.ReadFull(conn, greetingHeader); err != nil {
		return err
	}
	methods := make([]byte, int(greetingHeader[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}
	if greetingHeader[0] != 0x05 {
		return fmt.Errorf("upstream greeting version = %#x, want 0x05", greetingHeader[0])
	}
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return err
	}
	requestHeader := make([]byte, 4)
	if _, err := io.ReadFull(conn, requestHeader); err != nil {
		return err
	}
	if requestHeader[0] != 0x05 || requestHeader[1] != 0x01 {
		return fmt.Errorf("upstream CONNECT header = %#v", requestHeader)
	}
	if _, err := readSOCKS5Address(conn, requestHeader[3]); err != nil {
		return err
	}
	port := make([]byte, 2)
	if _, err := io.ReadFull(conn, port); err != nil {
		return err
	}
	_, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	return err
}

func newSessionSOCKS5RelayServer(t *testing.T, upstreamAddress string) (*SOCKS5Server, *fakeProxyStore) {
	t.Helper()
	previousConfig := config.Get()
	cfg := proxyTestConfig(0)
	cfg.ProxyAuthEnabled = true
	config.SetGlobal(cfg)
	t.Cleanup(func() { config.SetGlobal(previousConfig) })
	store := newProxyTestStore()
	addProxy(t, store, upstreamAddress, "socks5", 1)
	return &SOCKS5Server{
		storage:  store,
		cfg:      cfg,
		port:     ":0",
		sessions: affinity.New(time.Minute),
	}, store
}

func setSOCKS5RelayFailCount(t *testing.T, store *fakeProxyStore, address string, failCount int) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	id, ok := store.addressID[address]
	if !ok {
		t.Fatalf("proxy %s not found", address)
	}
	proxy := store.proxies[id]
	proxy.FailCount = failCount
	store.proxies[id] = proxy
}

func newSOCKS5RelayTCPPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen SOCKS5 relay inbound: %v", err)
	}
	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		_ = listener.Close()
		t.Fatalf("dial SOCKS5 relay inbound: %v", err)
	}
	serverConn, err := listener.Accept()
	_ = listener.Close()
	if err != nil {
		_ = clientConn.Close()
		t.Fatalf("accept SOCKS5 relay inbound: %v", err)
	}
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
	})
	return clientConn, serverConn
}

func startSOCKS5RelayHandler(server *SOCKS5Server, serverConn net.Conn) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		server.handleConnection(serverConn)
		close(done)
	}()
	return done
}

func waitSOCKS5RelayHandler(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("SOCKS5 relay handler 未在 1 秒内结束")
	}
}

func establishNoAuthSOCKS5Relay(t *testing.T, conn net.Conn) {
	t.Helper()
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set SOCKS5 client deadline: %v", err)
	}
	writeSocks5NoAuthHandshake(t, conn)
	writeSocks5DomainRequest(t, conn, "relay.example", 443)
	readSOCKS5RelaySuccessReply(t, conn)
	if err := conn.SetDeadline(time.Time{}); err != nil {
		t.Fatalf("clear SOCKS5 client deadline: %v", err)
	}
}

func establishSessionSOCKS5Relay(t *testing.T, conn net.Conn, session string) {
	t.Helper()
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set SOCKS5 client deadline: %v", err)
	}
	if _, err := conn.Write([]byte{0x05, 0x01, 0x02}); err != nil {
		t.Fatalf("write SOCKS5 greeting: %v", err)
	}
	method := make([]byte, 2)
	if _, err := io.ReadFull(conn, method); err != nil {
		t.Fatalf("read SOCKS5 auth method: %v", err)
	}
	if method[0] != 0x05 || method[1] != 0x02 {
		t.Fatalf("SOCKS5 auth method = %#v, want [0x05 0x02]", method)
	}
	username := "proxy-session-" + session
	authRequest := []byte{0x01, byte(len(username))}
	authRequest = append(authRequest, username...)
	authRequest = append(authRequest, byte(len("secret")))
	authRequest = append(authRequest, "secret"...)
	if _, err := conn.Write(authRequest); err != nil {
		t.Fatalf("write SOCKS5 auth request: %v", err)
	}
	authReply := make([]byte, 2)
	if _, err := io.ReadFull(conn, authReply); err != nil {
		t.Fatalf("read SOCKS5 auth reply: %v", err)
	}
	if authReply[0] != 0x01 || authReply[1] != 0x00 {
		t.Fatalf("SOCKS5 auth reply = %#v, want success", authReply)
	}
	writeSocks5DomainRequest(t, conn, "relay.example", 443)
	readSOCKS5RelaySuccessReply(t, conn)
	if err := conn.SetDeadline(time.Time{}); err != nil {
		t.Fatalf("clear SOCKS5 client deadline: %v", err)
	}
}

func readSOCKS5RelaySuccessReply(t *testing.T, conn net.Conn) {
	t.Helper()
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("read SOCKS5 CONNECT reply: %v", err)
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		t.Fatalf("SOCKS5 CONNECT reply = %#v, want VER=0x05 REP=0x00", reply[:2])
	}
}
