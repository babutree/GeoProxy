package proxy

import (
	"io"
	"net"
	"strings"
	"testing"

	"github.com/babutree/GeoProxy/storage"
)

// SOCKS5Server.dialViaProxy 出站 SOCKS5 域名长度边界。
//
// socks5_server.go dialViaProxy 的 socks5 分支用 byte(len(host)) 写入域名长度。
// host 来自 net.SplitHostPort(target)，已是纯 host（无端口）。域名 >255 字节时
// byte() 截断，向上游发出长度字段错误的损坏帧（静默数据损坏）。
// 修复目标：在写入长度字节前显式拒绝超长域名并返回明确错误，而非静默截断。

// TestSocks5ServerDialViaProxyRejectsOverlongDomain 反例1：超长域名被拒。
// 使用能完成握手的 fake 上游，使代码到达域名分支；修复后应在写入前以
// “too long”返回明确错误，而不是静默截断成损坏帧后误判为成功。
func TestSocks5ServerDialViaProxyRejectsOverlongDomain(t *testing.T) {
	upstream, _ := startFakeSocks5Upstream(t)
	server := NewSOCKS5(nil, proxyTestConfig(0), ":0")
	overlong := strings.Repeat("a", 256) + ".com" // 纯 host 260 字节 > 255
	target := net.JoinHostPort(overlong, "443")

	conn, err := server.dialViaProxy(&storage.Proxy{Address: upstream, Protocol: "socks5"}, target)
	if conn != nil {
		conn.Close()
	}
	if err == nil {
		t.Fatal("dialViaProxy accepted overlong socks5 domain, want explicit length error")
	}
	if !strings.Contains(err.Error(), "too long") {
		t.Fatalf("error = %v, want an explicit domain-length error (not a silent truncation)", err)
	}
}

// TestSocks5ServerDialViaProxyAcceptsNormalDomain 反例2：正常域名仍可用。
// 校验域名 CONNECT 路径 ATYP=0x03、长度字节正确、能走到握手并读到应用数据。
func TestSocks5ServerDialViaProxyAcceptsNormalDomain(t *testing.T) {
	const host = "example.com"
	upstream, gotReq := startFakeSocks5DomainUpstream(t)
	server := NewSOCKS5(nil, proxyTestConfig(0), ":0")

	conn, err := server.dialViaProxy(&storage.Proxy{Address: upstream, Protocol: "socks5"}, net.JoinHostPort(host, "443"))
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
	if req[3] != 0x03 {
		t.Fatalf("request ATYP = %#x, want domain (0x03)", req[3])
	}
	if int(req[4]) != len(host) {
		t.Fatalf("domain length byte = %d, want %d", req[4], len(host))
	}
	if got := string(req[5 : 5+len(host)]); got != host {
		t.Fatalf("domain = %q, want %q", got, host)
	}
}

// startFakeSocks5DomainUpstream 是一个只处理一次连接的 fake SOCKS5 上游，
// 能正确解析域名(ATYP=0x03) CONNECT 请求并回帧+应用数据，用于正例断言。
func startFakeSocks5DomainUpstream(t *testing.T) (string, <-chan []byte) {
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
		if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
			return
		}
		header := make([]byte, 4) // VER, CMD, RSV, ATYP
		if _, err := io.ReadFull(conn, header); err != nil {
			return
		}
		if header[3] != 0x03 {
			return
		}
		lenByte := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenByte); err != nil {
			return
		}
		addr := make([]byte, int(lenByte[0]))
		if _, err := io.ReadFull(conn, addr); err != nil {
			return
		}
		port := make([]byte, 2)
		if _, err := io.ReadFull(conn, port); err != nil {
			return
		}
		full := append([]byte{}, header...)
		full = append(full, lenByte...)
		full = append(full, addr...)
		full = append(full, port...)
		reqCh <- full

		reply := append([]byte{0x05, 0x00, 0x00, 0x04}, net.ParseIP("2001:db8::2").To16()...)
		reply = append(reply, 0x00, 0x00)
		reply = append(reply, []byte("APP")...)
		_, _ = conn.Write(reply)
	}()
	return listener.Addr().String(), reqCh
}
