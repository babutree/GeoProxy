package proxy

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/affinity"
	"github.com/babutree/GeoProxy/config"
)

// CONNECT 请求头和首个 payload 同写时，net/http 可能把 payload 留在
// Hijack 返回的 bufio.Reader 中；中继必须从该 reader 开始读取。
func TestHTTPConnectPreservesPayloadBufferedByHijack(t *testing.T) {
	payload := []byte("payload-sent-with-connect-headers")
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake HTTP upstream: %v", err)
	}
	t.Cleanup(func() { _ = upstream.Close() })
	type upstreamReadResult struct {
		payload []byte
		err     error
	}
	received := make(chan upstreamReadResult, 1)
	go func() {
		conn, acceptErr := upstream.Accept()
		if acceptErr != nil {
			received <- upstreamReadResult{err: fmt.Errorf("accept fake upstream: %w", acceptErr)}
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		if _, readErr := http.ReadRequest(reader); readErr != nil {
			received <- upstreamReadResult{err: fmt.Errorf("read upstream CONNECT request: %w", readErr)}
			return
		}
		if _, writeErr := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); writeErr != nil {
			received <- upstreamReadResult{err: fmt.Errorf("write upstream CONNECT response: %w", writeErr)}
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		got := make([]byte, len(payload))
		_, readErr := io.ReadFull(&bufferedConn{Conn: conn, reader: reader}, got)
		received <- upstreamReadResult{payload: got, err: readErr}
	}()

	previous := config.Get()
	cfg := proxyTestConfig(0)
	config.SetGlobal(cfg)
	t.Cleanup(func() { config.SetGlobal(previous) })
	store := newProxyTestStore()
	addProxy(t, store, upstream.Addr().String(), "http", 1)
	gateway := httptest.NewServer(&Server{
		storage:  store,
		cfg:      cfg,
		sessions: affinity.New(time.Minute),
	})
	t.Cleanup(gateway.Close)

	conn, err := net.Dial("tcp", strings.TrimPrefix(gateway.URL, "http://"))
	if err != nil {
		t.Fatalf("dial gateway: %v", err)
	}
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		_ = conn.Close()
		t.Fatalf("gateway connection type = %T, want *net.TCPConn", conn)
	}
	t.Cleanup(func() { _ = tcpConn.Close() })
	request := append([]byte("CONNECT relay.example:443 HTTP/1.1\r\nHost: relay.example:443\r\n\r\n"), payload...)
	if _, err := tcpConn.Write(request); err != nil {
		t.Fatalf("write CONNECT headers and payload: %v", err)
	}
	response, err := http.ReadResponse(bufio.NewReader(tcpConn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("read CONNECT response: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT status = %d, want 200", response.StatusCode)
	}

	select {
	case result := <-received:
		if result.err != nil {
			t.Fatalf("upstream failed to read buffered payload: %v", result.err)
		}
		if string(result.payload) != string(payload) {
			t.Fatalf("upstream payload = %q, want %q", result.payload, payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("upstream did not receive CONNECT payload")
	}
}

func TestHTTPConnectPreservesDelayedResponseAfterClientCloseWrite(t *testing.T) {
	requestPayload := []byte("request-before-half-close")
	responsePayload := []byte("delayed-response-after-half-close")
	upstream := startHTTPConnectUpstreamForTest(t, func(conn net.Conn) error {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		got, err := io.ReadAll(conn)
		if err != nil {
			return err
		}
		if string(got) != string(requestPayload) {
			return fmt.Errorf("request payload = %q, want %q", got, requestPayload)
		}
		time.Sleep(250 * time.Millisecond)
		if _, err := conn.Write(responsePayload); err != nil {
			return err
		}
		if tcpConn, ok := conn.(*bufferedConn); ok {
			if tcp, ok := tcpConn.Conn.(*net.TCPConn); ok {
				return tcp.CloseWrite()
			}
		}
		return nil
	})
	gateway, _ := startHTTPConnectGatewayForTest(t, upstream.address)
	client, reader := dialHTTPConnectGatewayForTest(t, gateway)
	writeHTTPConnectRequestForTest(t, client)
	readSuccessfulHTTPConnectResponseForTest(t, reader)
	if _, err := client.Write(requestPayload); err != nil {
		t.Fatalf("write tunneled request: %v", err)
	}
	if err := client.CloseWrite(); err != nil {
		t.Fatalf("client CloseWrite: %v", err)
	}
	if err := client.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set delayed response deadline: %v", err)
	}
	gotResponse := make([]byte, len(responsePayload))
	if _, err := io.ReadFull(reader, gotResponse); err != nil {
		t.Fatalf("read delayed response after CloseWrite: %v", err)
	}
	if string(gotResponse) != string(responsePayload) {
		t.Fatalf("delayed response = %q, want %q", gotResponse, responsePayload)
	}
}

func TestHTTPConnectUnsupportedHijackDoesNotRecordSuccess(t *testing.T) {
	upstream := startHTTPConnectUpstreamForTest(t, func(conn net.Conn) error {
		_, err := io.Copy(io.Discard, conn)
		return err
	})
	server, store := newHTTPConnectTestServerForTest(t, upstream.address)
	route := emptyRoute()
	route.Session = "unsupported-hijack"
	server.handleTunnel(httptest.NewRecorder(), newHTTPConnectRequestForTest(), route)
	assertHTTPConnectSuccessCountForTest(t, store, upstream.address, 0)
	assertNoHTTPConnectBindingForTest(t, server, route.Session)
}

func TestHTTPConnectHijackFailureDoesNotRecordSuccess(t *testing.T) {
	upstream := startHTTPConnectUpstreamForTest(t, func(conn net.Conn) error {
		_, err := io.Copy(io.Discard, conn)
		return err
	})
	server, store := newHTTPConnectTestServerForTest(t, upstream.address)
	route := emptyRoute()
	route.Session = "hijack-error"
	server.handleTunnel(
		&failingHijackWriterForTest{ResponseRecorder: httptest.NewRecorder()},
		newHTTPConnectRequestForTest(),
		route,
	)
	assertHTTPConnectSuccessCountForTest(t, store, upstream.address, 0)
	assertNoHTTPConnectBindingForTest(t, server, route.Session)
}

func TestHTTPConnectResponseFlushFailureDoesNotRecordSuccess(t *testing.T) {
	upstream := startHTTPConnectUpstreamForTest(t, func(conn net.Conn) error {
		_, err := io.Copy(io.Discard, conn)
		return err
	})
	server, store := newHTTPConnectTestServerForTest(t, upstream.address)
	clientConn := newScriptedTunnelClientConnForTest(1)
	route := emptyRoute()
	route.Session = "flush-error"
	server.handleTunnel(newControlledHijackWriterForTest(clientConn), newHTTPConnectRequestForTest(), route)
	select {
	case <-clientConn.writeFailed:
	case <-time.After(time.Second):
		t.Fatal("CONNECT 200 flush 未触发预期写失败")
	}
	assertHTTPConnectSuccessCountForTest(t, store, upstream.address, 0)
	assertNoHTTPConnectBindingForTest(t, server, route.Session)
}

func TestHTTPConnectDataWriteFailureBeforeFirstByteDoesNotRecordSuccess(t *testing.T) {
	upstream := startHTTPConnectUpstreamForTest(t, func(conn net.Conn) error {
		_, err := conn.Write([]byte("X"))
		return err
	})
	server, store := newHTTPConnectTestServerForTest(t, upstream.address)
	clientConn := newScriptedTunnelClientConnForTest(2)
	route := emptyRoute()
	route.Session = "data-write-error"
	server.handleTunnel(newControlledHijackWriterForTest(clientConn), newHTTPConnectRequestForTest(), route)
	select {
	case <-clientConn.writeFailed:
	case <-time.After(time.Second):
		t.Fatal("上游首字节未触发预期客户端写失败")
	}
	assertHTTPConnectSuccessCountForTest(t, store, upstream.address, 0)
	assertNoHTTPConnectBindingForTest(t, server, route.Session)
}

func TestHTTPConnectRecordsSuccessOnceAfterFirstUpstreamByte(t *testing.T) {
	releaseWrites := make(chan struct{}, 2)
	requestSeen := make(chan struct{})
	t.Cleanup(func() {
		for i := 0; i < 2; i++ {
			select {
			case releaseWrites <- struct{}{}:
			default:
			}
		}
	})
	upstream := startHTTPConnectUpstreamForTest(t, func(conn net.Conn) error {
		requestByte := make([]byte, 1)
		if _, err := io.ReadFull(conn, requestByte); err != nil {
			return err
		}
		if string(requestByte) != "Q" {
			return fmt.Errorf("client request byte = %q, want Q", requestByte)
		}
		close(requestSeen)
		<-releaseWrites
		if _, err := conn.Write([]byte("A")); err != nil {
			return err
		}
		<-releaseWrites
		_, err := conn.Write([]byte("B"))
		return err
	})
	gateway, store := startHTTPConnectGatewayForTest(t, upstream.address)
	client, reader := dialHTTPConnectGatewayForTest(t, gateway)
	writeHTTPConnectRequestForTest(t, client)
	readSuccessfulHTTPConnectResponseForTest(t, reader)
	if _, err := client.Write([]byte("Q")); err != nil {
		t.Fatalf("write client request byte: %v", err)
	}
	select {
	case <-requestSeen:
	case <-time.After(time.Second):
		t.Fatal("upstream did not observe client request byte")
	}
	assertHTTPConnectSuccessCountForTest(t, store, upstream.address, 0)

	releaseWrites <- struct{}{}
	readHTTPConnectPayloadForTest(t, client, reader, "A")
	waitHTTPConnectSuccessCountForTest(t, store, upstream.address, 1)
	releaseWrites <- struct{}{}
	readHTTPConnectPayloadForTest(t, client, reader, "B")
	time.Sleep(50 * time.Millisecond)
	assertHTTPConnectSuccessCountForTest(t, store, upstream.address, 1)
}

func TestHTTPConnectDirectPreservesBufferedPayloadAndDelayedResponse(t *testing.T) {
	requestPayload := []byte("direct-buffered-request")
	responsePayload := []byte("direct-delayed-response")
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen direct target: %v", err)
	}
	t.Cleanup(func() { _ = target.Close() })
	targetResult := make(chan error, 1)
	go func() {
		conn, acceptErr := target.Accept()
		if acceptErr != nil {
			targetResult <- acceptErr
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		got, readErr := io.ReadAll(conn)
		if readErr != nil {
			targetResult <- readErr
			return
		}
		if string(got) != string(requestPayload) {
			targetResult <- fmt.Errorf("direct request = %q, want %q", got, requestPayload)
			return
		}
		time.Sleep(100 * time.Millisecond)
		_, writeErr := conn.Write(responsePayload)
		targetResult <- writeErr
	}()

	previous := config.Get()
	cfg := proxyTestConfig(0)
	config.SetGlobal(cfg)
	t.Cleanup(func() { config.SetGlobal(previous) })
	gateway := httptest.NewServer(&Server{
		storage:  newProxyTestStore(),
		cfg:      cfg,
		sessions: affinity.New(time.Minute),
	})
	t.Cleanup(gateway.Close)
	client, reader := dialHTTPConnectGatewayForTest(t, gateway)
	request := append(
		[]byte(fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target.Addr(), target.Addr())),
		requestPayload...,
	)
	if _, err := client.Write(request); err != nil {
		t.Fatalf("write direct CONNECT headers and payload: %v", err)
	}
	if err := client.CloseWrite(); err != nil {
		t.Fatalf("direct client CloseWrite: %v", err)
	}
	readSuccessfulHTTPConnectResponseForTest(t, reader)
	if err := client.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set direct response deadline: %v", err)
	}
	gotResponse := make([]byte, len(responsePayload))
	if _, err := io.ReadFull(reader, gotResponse); err != nil {
		t.Fatalf("read direct delayed response: %v", err)
	}
	if string(gotResponse) != string(responsePayload) {
		t.Fatalf("direct response = %q, want %q", gotResponse, responsePayload)
	}
	select {
	case err := <-targetResult:
		if err != nil {
			t.Fatalf("direct target failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("direct target did not finish")
	}
}

func TestHTTPConnectRelayIdleTimeoutUsesPositiveDefault(t *testing.T) {
	previous := config.Get()
	cfg := proxyTestConfig(0)
	cfg.ValidateTimeout = 0
	config.SetGlobal(cfg)
	t.Cleanup(func() { config.SetGlobal(previous) })
	server := &Server{cfg: cfg}
	if got := server.httpConnectRelayIdleTimeout(); got <= 0 {
		t.Fatalf("idle timeout = %s, want positive default", got)
	}
}

type httpConnectUpstreamForTest struct {
	address string
	done    <-chan error
}

func startHTTPConnectUpstreamForTest(t *testing.T, relay func(net.Conn) error) httpConnectUpstreamForTest {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake HTTP upstream: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	done := make(chan error, 1)
	var acceptedMu sync.Mutex
	var accepted net.Conn
	t.Cleanup(func() {
		acceptedMu.Lock()
		if accepted != nil {
			_ = accepted.Close()
		}
		acceptedMu.Unlock()
	})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		acceptedMu.Lock()
		accepted = conn
		acceptedMu.Unlock()
		defer conn.Close()
		reader := bufio.NewReader(conn)
		request, readErr := http.ReadRequest(reader)
		if readErr != nil {
			done <- readErr
			return
		}
		_ = request.Body.Close()
		if request.Method != http.MethodConnect || request.Host != "relay.example:443" {
			done <- fmt.Errorf("upstream request = %s %q", request.Method, request.Host)
			return
		}
		if _, writeErr := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); writeErr != nil {
			done <- writeErr
			return
		}
		done <- relay(&bufferedConn{Conn: conn, reader: reader})
	}()
	return httpConnectUpstreamForTest{address: listener.Addr().String(), done: done}
}

func startHTTPConnectGatewayForTest(t *testing.T, upstreamAddress string) (*httptest.Server, *fakeProxyStore) {
	t.Helper()
	server, store := newHTTPConnectTestServerForTest(t, upstreamAddress)
	gateway := httptest.NewServer(server)
	t.Cleanup(gateway.Close)
	return gateway, store
}

func newHTTPConnectTestServerForTest(t *testing.T, upstreamAddress string) (*Server, *fakeProxyStore) {
	t.Helper()
	previous := config.Get()
	cfg := proxyTestConfig(0)
	cfg.ValidateTimeout = 1
	config.SetGlobal(cfg)
	t.Cleanup(func() { config.SetGlobal(previous) })
	store := newProxyTestStore()
	addProxy(t, store, upstreamAddress, "http", 1)
	return &Server{storage: store, cfg: cfg, sessions: affinity.New(time.Minute)}, store
}

func dialHTTPConnectGatewayForTest(t *testing.T, gateway *httptest.Server) (*net.TCPConn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("tcp", strings.TrimPrefix(gateway.URL, "http://"))
	if err != nil {
		t.Fatalf("dial gateway: %v", err)
	}
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		_ = conn.Close()
		t.Fatalf("gateway connection type = %T, want *net.TCPConn", conn)
	}
	t.Cleanup(func() { _ = tcpConn.Close() })
	return tcpConn, bufio.NewReader(tcpConn)
}

func writeHTTPConnectRequestForTest(t *testing.T, conn net.Conn) {
	t.Helper()
	if _, err := io.WriteString(conn, "CONNECT relay.example:443 HTTP/1.1\r\nHost: relay.example:443\r\n\r\n"); err != nil {
		t.Fatalf("write CONNECT request: %v", err)
	}
}

func readSuccessfulHTTPConnectResponseForTest(t *testing.T, reader *bufio.Reader) {
	t.Helper()
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("read CONNECT response: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT status = %d, want 200", response.StatusCode)
	}
}

func readHTTPConnectPayloadForTest(t *testing.T, conn net.Conn, reader *bufio.Reader, want string) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set payload deadline: %v", err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(reader, got); err != nil {
		t.Fatalf("read payload %q: %v", want, err)
	}
	if string(got) != want {
		t.Fatalf("payload = %q, want %q", got, want)
	}
	_ = conn.SetReadDeadline(time.Time{})
}

func newHTTPConnectRequestForTest() *http.Request {
	request := httptest.NewRequest(http.MethodConnect, "http://relay.example:443", nil)
	request.Host = "relay.example:443"
	return request
}

func assertHTTPConnectSuccessCountForTest(t *testing.T, store *fakeProxyStore, address string, want int) {
	t.Helper()
	proxy, err := store.GetProxyByAddress(address)
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if proxy.SuccessCount != want {
		t.Fatalf("success_count = %d, want %d", proxy.SuccessCount, want)
	}
}

func assertNoHTTPConnectBindingForTest(t *testing.T, server *Server, session string) {
	t.Helper()
	if binding, ok := server.sessions.Get(session); ok {
		t.Fatalf("session %q still bound after tunnel setup/data failure: %#v", session, binding)
	}
}

func waitHTTPConnectSuccessCountForTest(t *testing.T, store *fakeProxyStore, address string, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		proxy, err := store.GetProxyByAddress(address)
		if err != nil {
			t.Fatalf("GetProxyByAddress() error = %v", err)
		}
		if proxy.SuccessCount == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("success_count = %d, want %d", proxy.SuccessCount, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type controlledHijackWriterForTest struct {
	conn net.Conn
}

type failingHijackWriterForTest struct {
	*httptest.ResponseRecorder
}

func (w *failingHijackWriterForTest) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("scripted hijack failure")
}

func newControlledHijackWriterForTest(conn net.Conn) *controlledHijackWriterForTest {
	return &controlledHijackWriterForTest{conn: conn}
}

func (w *controlledHijackWriterForTest) Header() http.Header { return make(http.Header) }

func (w *controlledHijackWriterForTest) Write(p []byte) (int, error) { return len(p), nil }

func (w *controlledHijackWriterForTest) WriteHeader(int) {}

func (w *controlledHijackWriterForTest) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(bufio.NewReader(w.conn), bufio.NewWriter(w.conn)), nil
}

type scriptedTunnelClientConnForTest struct {
	mu          sync.Mutex
	writes      int
	failAt      int
	writeFailed chan struct{}
	closed      chan struct{}
	failureOnce sync.Once
	closeOnce   sync.Once
}

func newScriptedTunnelClientConnForTest(failAt int) *scriptedTunnelClientConnForTest {
	return &scriptedTunnelClientConnForTest{
		failAt:      failAt,
		writeFailed: make(chan struct{}),
		closed:      make(chan struct{}),
	}
}

func (c *scriptedTunnelClientConnForTest) Read([]byte) (int, error) {
	<-c.closed
	return 0, io.EOF
}

func (c *scriptedTunnelClientConnForTest) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.writes++
	fail := c.writes >= c.failAt
	c.mu.Unlock()
	if fail {
		c.failureOnce.Do(func() { close(c.writeFailed) })
		return 0, errors.New("scripted tunnel write failure")
	}
	return len(p), nil
}

func (c *scriptedTunnelClientConnForTest) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *scriptedTunnelClientConnForTest) CloseWrite() error { return nil }

func (c *scriptedTunnelClientConnForTest) LocalAddr() net.Addr {
	return testTunnelAddrForHTTPConnect("local")
}
func (c *scriptedTunnelClientConnForTest) RemoteAddr() net.Addr {
	return testTunnelAddrForHTTPConnect("remote")
}
func (c *scriptedTunnelClientConnForTest) SetDeadline(time.Time) error      { return nil }
func (c *scriptedTunnelClientConnForTest) SetReadDeadline(time.Time) error  { return nil }
func (c *scriptedTunnelClientConnForTest) SetWriteDeadline(time.Time) error { return nil }

type testTunnelAddrForHTTPConnect string

func (a testTunnelAddrForHTTPConnect) Network() string { return "test" }
func (a testTunnelAddrForHTTPConnect) String() string  { return string(a) }
