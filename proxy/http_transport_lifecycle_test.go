package proxy

import (
	"bufio"
	"encoding/base64"
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
)

const oneShotTransportCloseTimeout = 2 * time.Second

func TestHandleHTTPClosesOneShotUpstreamConnection(t *testing.T) {
	const (
		username = "lifecycle-user"
		password = "lifecycle-pass"
	)
	body := strings.Repeat("完整响应体-", 128)

	tests := []struct {
		name     string
		protocol string
		start    func(*testing.T, string, string, string) *trackedOneShotProxy
	}{
		{name: "HTTP", protocol: "http", start: startTrackedHTTPProxy},
		{name: "SOCKS5", protocol: "socks5", start: startTrackedSOCKS5Proxy},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := tt.start(t, username, password, body)
			store := newProxyTestStore()
			addProxy(t, store, upstream.address, tt.protocol, 1)
			setTrackedProxyCredentials(t, store, upstream.address, username, password)
			server := newProxyTestServer(store, proxyTestConfig(0))
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "http://origin.example/lifecycle", nil)
			server.handleHTTP(recorder, req, emptyRoute())
			if recorder.Code != http.StatusOK {
				t.Fatalf("响应状态=%d，期望=%d；body=%q", recorder.Code, http.StatusOK, recorder.Body.String())
			}
			if recorder.Body.String() != body {
				t.Fatalf("响应体长度=%d，期望=%d", recorder.Body.Len(), len(body))
			}

			upstream.waitForClientClose(t)
		})
	}
}

func setTrackedProxyCredentials(t *testing.T, store *fakeProxyStore, address, username, password string) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	id, ok := store.addressID[address]
	if !ok {
		t.Fatalf("测试节点地址不存在: %s", address)
	}
	proxy := store.proxies[id]
	proxy.Username = username
	proxy.Password = password
	store.proxies[id] = proxy
}

func TestHTTPRetryClosesSuccessfulOneShotUpstreamConnection(t *testing.T) {
	first := startFailingHTTPProxy(t)
	secondBody := "retry-success-body"
	second := startTrackedHTTPProxy(t, "", "", secondBody)

	store := newProxyTestStore()
	addProxy(t, store, first.address, "http", 1)
	addProxy(t, store, second.address, "http", 2)
	server := newProxyTestServer(store, proxyTestConfig(1))
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://origin.example/retry", nil)

	server.handleHTTP(recorder, req, emptyRoute())

	if recorder.Code != http.StatusOK {
		t.Fatalf("重试响应状态=%d，期望=%d；body=%q", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if recorder.Body.String() != secondBody {
		t.Fatalf("重试响应体=%q，期望=%q", recorder.Body.String(), secondBody)
	}
	select {
	case <-first.requestSeen:
	default:
		t.Fatal("首个失败上游未收到请求")
	}
	second.waitForClientClose(t)
}

type trackedOneShotProxy struct {
	address      string
	requestSeen  <-chan struct{}
	clientClosed <-chan struct{}
	serverErr    <-chan error
}

func (p *trackedOneShotProxy) waitForClientClose(t *testing.T) {
	t.Helper()
	select {
	case <-p.clientClosed:
		select {
		case err := <-p.serverErr:
			if err != nil {
				t.Fatalf("上游处理失败: %v", err)
			}
		default:
		}
	case err := <-p.serverErr:
		if err != nil {
			t.Fatalf("上游处理失败: %v", err)
		}
		select {
		case <-p.clientClosed:
		default:
			t.Fatal("上游处理已结束，但未观察到客户端关闭连接")
		}
	case <-time.After(oneShotTransportCloseTimeout):
		t.Fatal("一次性 Transport 在响应完成后仍保留上游空闲连接")
	}
}

func startTrackedHTTPProxy(t *testing.T, username, password, body string) *trackedOneShotProxy {
	t.Helper()
	return startTrackedProxy(t, func(conn net.Conn, reader *bufio.Reader, requestSeen chan<- struct{}, clientClosed chan<- struct{}) error {
		req, err := http.ReadRequest(reader)
		if err != nil {
			return fmt.Errorf("读取 HTTP 代理请求: %w", err)
		}
		close(requestSeen)
		defer req.Body.Close()
		if _, err := io.Copy(io.Discard, req.Body); err != nil {
			return fmt.Errorf("读取 HTTP 代理请求体: %w", err)
		}
		if username != "" || password != "" {
			want := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
			if got := req.Header.Get("Proxy-Authorization"); got != want {
				return fmt.Errorf("Proxy-Authorization=%q，期望=%q", got, want)
			}
		}
		return writeTrackedHTTPResponse(conn, reader, body, clientClosed)
	})
}

func startTrackedSOCKS5Proxy(t *testing.T, username, password, body string) *trackedOneShotProxy {
	t.Helper()
	return startTrackedProxy(t, func(conn net.Conn, reader *bufio.Reader, requestSeen chan<- struct{}, clientClosed chan<- struct{}) error {
		if err := completeTrackedSOCKS5Handshake(conn, reader, username, password); err != nil {
			return err
		}
		req, err := http.ReadRequest(reader)
		if err != nil {
			return fmt.Errorf("读取 SOCKS5 隧道内 HTTP 请求: %w", err)
		}
		close(requestSeen)
		defer req.Body.Close()
		if _, err := io.Copy(io.Discard, req.Body); err != nil {
			return fmt.Errorf("读取 SOCKS5 隧道内请求体: %w", err)
		}
		return writeTrackedHTTPResponse(conn, reader, body, clientClosed)
	})
}

func startFailingHTTPProxy(t *testing.T) *trackedOneShotProxy {
	t.Helper()
	return startTrackedProxy(t, func(_ net.Conn, reader *bufio.Reader, requestSeen chan<- struct{}, _ chan<- struct{}) error {
		req, err := http.ReadRequest(reader)
		if err != nil {
			return fmt.Errorf("读取预期失败的 HTTP 代理请求: %w", err)
		}
		close(requestSeen)
		_ = req.Body.Close()
		return nil
	})
}

type trackedProxyHandler func(net.Conn, *bufio.Reader, chan<- struct{}, chan<- struct{}) error

func startTrackedProxy(t *testing.T, handler trackedProxyHandler) *trackedOneShotProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听测试上游失败: %v", err)
	}

	requestSeen := make(chan struct{})
	clientClosed := make(chan struct{})
	serverErr := make(chan error, 1)
	var (
		connMu sync.Mutex
		conn   net.Conn
	)
	t.Cleanup(func() {
		_ = listener.Close()
		connMu.Lock()
		if conn != nil {
			_ = conn.Close()
		}
		connMu.Unlock()
	})

	go func() {
		accepted, acceptErr := listener.Accept()
		if acceptErr != nil {
			if !errors.Is(acceptErr, net.ErrClosed) {
				serverErr <- fmt.Errorf("接受测试上游连接: %w", acceptErr)
			}
			return
		}
		connMu.Lock()
		conn = accepted
		connMu.Unlock()
		defer accepted.Close()
		serverErr <- handler(accepted, bufio.NewReader(accepted), requestSeen, clientClosed)
	}()

	return &trackedOneShotProxy{
		address:      listener.Addr().String(),
		requestSeen:  requestSeen,
		clientClosed: clientClosed,
		serverErr:    serverErr,
	}
}

func writeTrackedHTTPResponse(conn net.Conn, reader *bufio.Reader, body string, clientClosed chan<- struct{}) error {
	header := fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Length: %d\r\nContent-Type: text/plain\r\n\r\n", len(body))
	if _, err := io.WriteString(conn, header); err != nil {
		return fmt.Errorf("写入测试响应头: %w", err)
	}
	middle := len(body) / 2
	if _, err := io.WriteString(conn, body[:middle]); err != nil {
		return fmt.Errorf("写入测试响应体前半段: %w", err)
	}
	if _, err := io.WriteString(conn, body[middle:]); err != nil {
		return fmt.Errorf("写入测试响应体后半段: %w", err)
	}
	if _, err := reader.ReadByte(); err == nil {
		return errors.New("响应完成后收到非预期的额外请求数据")
	}
	close(clientClosed)
	return nil
}

func completeTrackedSOCKS5Handshake(conn net.Conn, reader *bufio.Reader, username, password string) error {
	var greeting [2]byte
	if _, err := io.ReadFull(reader, greeting[:]); err != nil {
		return fmt.Errorf("读取 SOCKS5 greeting: %w", err)
	}
	if greeting[0] != 0x05 {
		return fmt.Errorf("SOCKS5 greeting version=%#x", greeting[0])
	}
	methods := make([]byte, int(greeting[1]))
	if _, err := io.ReadFull(reader, methods); err != nil {
		return fmt.Errorf("读取 SOCKS5 methods: %w", err)
	}
	method := byte(0x00)
	if username != "" || password != "" {
		method = 0x02
	}
	if !trackedContainsByte(methods, method) {
		return fmt.Errorf("SOCKS5 methods=%#v，缺少认证方法 %#x", methods, method)
	}
	if _, err := conn.Write([]byte{0x05, method}); err != nil {
		return fmt.Errorf("写入 SOCKS5 method: %w", err)
	}
	if method == 0x02 {
		if err := verifyTrackedSOCKS5Credentials(conn, reader, username, password); err != nil {
			return err
		}
	}

	var request [4]byte
	if _, err := io.ReadFull(reader, request[:]); err != nil {
		return fmt.Errorf("读取 SOCKS5 CONNECT: %w", err)
	}
	if request[0] != 0x05 || request[1] != 0x01 || request[2] != 0x00 {
		return fmt.Errorf("SOCKS5 CONNECT header=%#v", request)
	}
	if err := discardTrackedSOCKS5Address(reader, request[3]); err != nil {
		return err
	}
	var port [2]byte
	if _, err := io.ReadFull(reader, port[:]); err != nil {
		return fmt.Errorf("读取 SOCKS5 CONNECT port: %w", err)
	}
	if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, 0, 80}); err != nil {
		return fmt.Errorf("写入 SOCKS5 CONNECT reply: %w", err)
	}
	return nil
}

func verifyTrackedSOCKS5Credentials(conn net.Conn, reader *bufio.Reader, username, password string) error {
	var authHeader [2]byte
	if _, err := io.ReadFull(reader, authHeader[:]); err != nil {
		return fmt.Errorf("读取 SOCKS5 auth header: %w", err)
	}
	if authHeader[0] != 0x01 {
		return fmt.Errorf("SOCKS5 auth version=%#x", authHeader[0])
	}
	gotUsername := make([]byte, int(authHeader[1]))
	if _, err := io.ReadFull(reader, gotUsername); err != nil {
		return fmt.Errorf("读取 SOCKS5 username: %w", err)
	}
	passwordLength, err := reader.ReadByte()
	if err != nil {
		return fmt.Errorf("读取 SOCKS5 password length: %w", err)
	}
	gotPassword := make([]byte, int(passwordLength))
	if _, err := io.ReadFull(reader, gotPassword); err != nil {
		return fmt.Errorf("读取 SOCKS5 password: %w", err)
	}
	if string(gotUsername) != username || string(gotPassword) != password {
		return errors.New("SOCKS5 用户名或密码不匹配")
	}
	if _, err := conn.Write([]byte{0x01, 0x00}); err != nil {
		return fmt.Errorf("写入 SOCKS5 auth reply: %w", err)
	}
	return nil
}

func discardTrackedSOCKS5Address(reader *bufio.Reader, addressType byte) error {
	length := 0
	switch addressType {
	case 0x01:
		length = net.IPv4len
	case 0x03:
		domainLength, err := reader.ReadByte()
		if err != nil {
			return fmt.Errorf("读取 SOCKS5 domain length: %w", err)
		}
		length = int(domainLength)
	case 0x04:
		length = net.IPv6len
	default:
		return fmt.Errorf("SOCKS5 address type=%#x", addressType)
	}
	if _, err := io.CopyN(io.Discard, reader, int64(length)); err != nil {
		return fmt.Errorf("读取 SOCKS5 address: %w", err)
	}
	return nil
}

func trackedContainsByte(values []byte, want byte) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
