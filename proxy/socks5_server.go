package proxy

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/babutree/GeoProxy/affinity"
	"github.com/babutree/GeoProxy/auth"
	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/selector"
	"github.com/babutree/GeoProxy/storage"
)

// SOCKS5Server SOCKS5 协议服务器
type SOCKS5Server struct {
	storage  proxyStore
	cfg      *config.Config
	port     string
	sessions *affinity.Store
}

// NewSOCKS5 创建 SOCKS5 服务器
func NewSOCKS5(s *storage.Storage, cfg *config.Config, port string) *SOCKS5Server {
	return &SOCKS5Server{
		storage:  s,
		cfg:      cfg,
		port:     port,
		sessions: SessionStore(cfg),
	}
}

// runtimeConfig 读取当前已发布配置快照。
// config.Save 会替换全局指针；请求路径不得继续使用启动时缓存的 s.cfg。
func (s *SOCKS5Server) runtimeConfig() *config.Config {
	if live := config.Get(); live != nil {
		return live
	}
	return s.cfg
}

// Start 启动 SOCKS5 服务器
func (s *SOCKS5Server) Start() error {
	cfg := s.runtimeConfig()
	authStatus := "无认证"
	if cfg.ProxyAuthEnabled {
		authStatus = fmt.Sprintf("需认证 (用户: %s)", cfg.ProxyAuthUsername)
	}
	log.Printf("socks5 server listening on %s [lowest latency] [%s]", s.port, authStatus)

	listener, err := net.Listen("tcp", s.port)
	if err != nil {
		return err
	}
	defer listener.Close()

	var acceptFailures int
	for {
		conn, err := listener.Accept()
		if err != nil {
			// listener 关闭是正常退出；其它持续错误退避，避免忙循环（RISK-04）。
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				log.Printf("[socks5] accept temporary error: %v", err)
				time.Sleep(50 * time.Millisecond)
				continue
			}
			if strings.Contains(err.Error(), "use of closed network connection") {
				return nil
			}
			acceptFailures++
			log.Printf("[socks5] accept error: %v", err)
			if acceptFailures >= 20 {
				return fmt.Errorf("socks5 accept 持续失败: %w", err)
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}
		acceptFailures = 0
		go s.handleConnection(conn)
	}
}

// handleConnection 处理 SOCKS5 连接
func (s *SOCKS5Server) handleConnection(clientConn net.Conn) {
	defer clientConn.Close()
	protocolTimeout := time.Duration(s.runtimeConfig().ValidateTimeout) * time.Second
	if protocolTimeout > 0 {
		if err := clientConn.SetDeadline(time.Now().Add(protocolTimeout)); err != nil {
			log.Printf("[socks5] set inbound protocol deadline failed: %v", err)
			return
		}
	}

	// SOCKS5 握手
	route, err := s.socks5Handshake(clientConn)
	if err != nil {
		log.Printf("[socks5] handshake failed: %v", err)
		return
	}

	// 读取请求
	target, err := s.readSOCKS5Request(clientConn)
	if err != nil {
		log.Printf("[socks5] read request failed: %v", err)
		return
	}
	if protocolTimeout > 0 {
		if err := clientConn.SetDeadline(time.Time{}); err != nil {
			log.Printf("[socks5] clear inbound protocol deadline failed: %v", err)
			return
		}
	}

	// 内网/本地目标直连，不经上游节点（等同 NO_PROXY 例外）。
	if isBypassTarget(target) {
		s.socks5Direct(clientConn, target)
		return
	}

	// 带重试的连接上游代理
	// 重试机制：只使用 SOCKS5 协议的上游代理（天然支持 HTTPS）
	tried := []int64{}
	for attempt := 0; attempt <= s.runtimeConfig().MaxRetry; attempt++ {
		p, err := s.selectSOCKS5Proxy(route, tried)
		if err != nil {
			log.Printf("[socks5] no available socks5 upstream proxy: %v", err)
			s.sendSOCKS5Reply(clientConn, 0x01) // General failure
			return
		}

		tried = append(tried, p.ID)

		// 连接上游代理
		upstreamConn, err := s.dialViaProxy(p, target)
		if err != nil {
			log.Printf("[socks5] dial %s via %s (%s) failed: %v", target, p.Address, p.Protocol, err)
			recordProxyFailure(s.storage, p)
			s.releaseFailedBinding(route, p)
			continue
		}

		// 发送成功响应
		if err := s.sendSOCKS5Reply(clientConn, 0x00); err != nil {
			upstreamConn.Close()
			return
		}

		s.storage.RecordProxyUseByID(p.ID, true)
		log.Printf("[socks5] %s via %s established", target, p.Address)

		// 双向转发数据
		go io.Copy(upstreamConn, clientConn)
		io.Copy(clientConn, upstreamConn)

		// 转发完成，关闭连接
		upstreamConn.Close()
		return
	}

	// 所有重试都失败
	s.sendSOCKS5Reply(clientConn, 0x01) // General failure
	log.Printf("[socks5] all proxies failed for %s", target)
}

func (s *SOCKS5Server) releaseFailedBinding(route auth.ParsedUsername, p *storage.Proxy) {
	if route.Session == "" || s.sessions == nil {
		return
	}
	s.sessions.RemoveIfProxyID(route.Session, p.ID)
}

// socks5Direct 为内网/本地目标建立直连，不经上游节点。
func (s *SOCKS5Server) socks5Direct(clientConn net.Conn, target string) {
	timeout := time.Duration(s.runtimeConfig().ValidateTimeout) * time.Second
	upstreamConn, err := net.DialTimeout("tcp", target, timeout)
	if err != nil {
		log.Printf("[socks5] direct dial %s failed: %v", target, err)
		s.sendSOCKS5Reply(clientConn, 0x01) // General failure
		return
	}
	if err := s.sendSOCKS5Reply(clientConn, 0x00); err != nil {
		upstreamConn.Close()
		return
	}
	log.Printf("[socks5] %s direct (bypass upstream)", target)
	go io.Copy(upstreamConn, clientConn)
	io.Copy(clientConn, upstreamConn)
	upstreamConn.Close()
}

// selectSOCKS5Proxy 根据使用模式选择 SOCKS5 上游代理
func (s *SOCKS5Server) selectSOCKS5Proxy(route auth.ParsedUsername, tried []int64) (*storage.Proxy, error) {
	route = withDefaultRegion(route, s.runtimeConfig().DefaultRegion)
	return selector.Resolve(s.storage, s.sessions, route, tried)
}

// socks5Handshake 处理 SOCKS5 握手
func (s *SOCKS5Server) socks5Handshake(conn net.Conn) (auth.ParsedUsername, error) {
	buf := make([]byte, 257)

	// 读取客户端问候: [VER(1), NMETHODS(1), METHODS(1-255)]
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return auth.ParsedUsername{}, err
	}

	version := buf[0]
	if version != 0x05 {
		return auth.ParsedUsername{}, fmt.Errorf("unsupported SOCKS version: %d", version)
	}

	nmethods := int(buf[1])
	if _, err := io.ReadFull(conn, buf[2:2+nmethods]); err != nil {
		return auth.ParsedUsername{}, err
	}

	// 检查是否需要认证
	needAuth := s.runtimeConfig().ProxyAuthEnabled
	methods := buf[2 : 2+nmethods]

	// 选择认证方式
	var selectedMethod byte = 0xFF // No acceptable methods
	if needAuth {
		// 需要用户名/密码认证 (0x02)
		for _, method := range methods {
			if method == 0x02 {
				selectedMethod = 0x02
				break
			}
		}
	} else {
		// 无需认证 (0x00)
		for _, method := range methods {
			if method == 0x00 {
				selectedMethod = 0x00
				break
			}
		}
	}

	// 发送方法选择: [VER(1), METHOD(1)]
	if _, err := conn.Write([]byte{0x05, selectedMethod}); err != nil {
		return auth.ParsedUsername{}, err
	}

	if selectedMethod == 0xFF {
		return auth.ParsedUsername{}, fmt.Errorf("no acceptable authentication method")
	}

	// 如果需要认证，进行用户名/密码认证
	if selectedMethod == 0x02 {
		return s.socks5Auth(conn)
	}

	return auth.ParsedUsername{}, nil
}

// socks5Auth 处理 SOCKS5 用户名/密码认证
func (s *SOCKS5Server) socks5Auth(conn net.Conn) (auth.ParsedUsername, error) {
	buf := make([]byte, 513)

	// 读取认证请求: [VER(1), ULEN(1), UNAME(1-255), PLEN(1), PASSWD(1-255)]
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return auth.ParsedUsername{}, err
	}

	if buf[0] != 0x01 {
		return auth.ParsedUsername{}, fmt.Errorf("unsupported auth version: %d", buf[0])
	}

	ulen := int(buf[1])
	if _, err := io.ReadFull(conn, buf[2:2+ulen]); err != nil {
		return auth.ParsedUsername{}, err
	}

	parsed, err := auth.ParseUsername(string(buf[2 : 2+ulen]))
	if err != nil {
		conn.Write([]byte{0x01, 0x01})
		return auth.ParsedUsername{}, err
	}

	// 读取密码长度和密码
	if _, err := io.ReadFull(conn, buf[2+ulen:2+ulen+1]); err != nil {
		return auth.ParsedUsername{}, err
	}

	plen := int(buf[2+ulen])
	if _, err := io.ReadFull(conn, buf[2+ulen+1:2+ulen+1+plen]); err != nil {
		return auth.ParsedUsername{}, err
	}

	password := string(buf[2+ulen+1 : 2+ulen+1+plen])

	// 验证用户名和密码
	if !auth.VerifyPassword(parsed.Base, password, s.runtimeConfig().ProxyAuthUsername, s.runtimeConfig().ProxyAuthPassword, s.runtimeConfig().ProxyAuthPasswordHash) {
		// 认证失败: [VER(1), STATUS(1)]
		conn.Write([]byte{0x01, 0x01})
		return auth.ParsedUsername{}, fmt.Errorf("authentication failed")
	}

	// 认证成功: [VER(1), STATUS(1)]
	if _, err := conn.Write([]byte{0x01, 0x00}); err != nil {
		return auth.ParsedUsername{}, err
	}

	return parsed, nil
}

// readSOCKS5Request 读取 SOCKS5 请求
func (s *SOCKS5Server) readSOCKS5Request(conn net.Conn) (string, error) {
	// 读取请求: [VER(1), CMD(1), RSV(1), ATYP(1), DST.ADDR(variable), DST.PORT(2)]
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return "", err
	}

	if header[0] != 0x05 {
		return "", fmt.Errorf("invalid version: %d", header[0])
	}

	cmd := header[1]
	if cmd != 0x01 { // 只支持 CONNECT
		s.sendSOCKS5Reply(conn, 0x07) // Command not supported
		return "", fmt.Errorf("unsupported command: %d", cmd)
	}
	if header[2] != 0x00 {
		s.sendSOCKS5Reply(conn, 0x01) // General failure
		return "", fmt.Errorf("invalid reserved byte: %d", header[2])
	}

	atyp := header[3]
	if !validSOCKS5AddressType(atyp) {
		s.sendSOCKS5Reply(conn, 0x08) // Address type not supported
		return "", fmt.Errorf("unsupported address type: %d", atyp)
	}
	host, err := readSOCKS5Address(conn, atyp)
	if err != nil {
		return "", err
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBytes); err != nil {
		return "", err
	}
	port := binary.BigEndian.Uint16(portBytes)

	return net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}

func validSOCKS5AddressType(atyp byte) bool {
	return atyp == 0x01 || atyp == 0x03 || atyp == 0x04
}

func readSOCKS5Address(conn io.Reader, atyp byte) (string, error) {
	switch atyp {
	case 0x01: // IPv4
		addr := make([]byte, 4)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return "", err
		}
		return net.IP(addr).String(), nil
	case 0x03: // Domain name
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return "", err
		}
		addr := make([]byte, int(lenBuf[0]))
		if _, err := io.ReadFull(conn, addr); err != nil {
			return "", err
		}
		return string(addr), nil
	case 0x04: // IPv6
		addr := make([]byte, 16)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return "", err
		}
		return net.IP(addr).String(), nil
	default:
		return "", fmt.Errorf("unsupported address type: %d", atyp)
	}
}

// sendSOCKS5Reply 发送 SOCKS5 响应
func (s *SOCKS5Server) sendSOCKS5Reply(conn net.Conn, rep byte) error {
	// [VER(1), REP(1), RSV(1), ATYP(1), BND.ADDR(variable), BND.PORT(2)]
	// 简化：使用 0.0.0.0:0
	reply := []byte{
		0x05,       // VER
		rep,        // REP: 0x00=成功, 0x01=一般失败, 0x07=命令不支持, 0x08=地址类型不支持
		0x00,       // RSV
		0x01,       // ATYP: IPv4
		0, 0, 0, 0, // BND.ADDR: 0.0.0.0
		0, 0, // BND.PORT: 0
	}
	_, err := conn.Write(reply)
	return err
}

// dialViaProxy 通过上游代理连接目标
func (s *SOCKS5Server) dialViaProxy(p *storage.Proxy, target string) (net.Conn, error) {
	timeout := time.Duration(s.runtimeConfig().ValidateTimeout) * time.Second

	switch p.Protocol {
	case "http":
		// 连接到 HTTP 代理
		conn, err := net.DialTimeout("tcp", p.Address, timeout)
		if err != nil {
			return nil, err
		}
		if timeout > 0 {
			if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
				conn.Close()
				return nil, err
			}
		}
		// 发送 CONNECT 请求
		fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
		proxiedConn, err := readHTTPConnectResponse(conn)
		if err != nil {
			conn.Close()
			return nil, err
		}
		if err := conn.SetDeadline(time.Time{}); err != nil {
			conn.Close()
			return nil, err
		}
		return proxiedConn, nil

	case "socks5":
		// 使用 SOCKS5 代理
		dialer := &net.Dialer{Timeout: timeout}
		proxyConn, err := dialer.Dial("tcp", p.Address)
		if err != nil {
			return nil, err
		}
		if timeout > 0 {
			if err := proxyConn.SetDeadline(time.Now().Add(timeout)); err != nil {
				proxyConn.Close()
				return nil, err
			}
		}

		// SOCKS5 握手（无认证）
		if _, err := proxyConn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
			proxyConn.Close()
			return nil, err
		}

		handshake := make([]byte, 2)
		if _, err := io.ReadFull(proxyConn, handshake); err != nil {
			proxyConn.Close()
			return nil, err
		}

		if handshake[0] != 0x05 || handshake[1] != 0x00 {
			proxyConn.Close()
			return nil, fmt.Errorf("socks5 handshake failed")
		}

		// 发送 CONNECT 请求
		host, port, err := net.SplitHostPort(target)
		if err != nil {
			proxyConn.Close()
			return nil, err
		}

		// 构建请求
		req := []byte{0x05, 0x01, 0x00} // VER, CMD=CONNECT, RSV

		// 判断是 IP 还是域名
		if ip := net.ParseIP(host); ip != nil {
			if ip4 := ip.To4(); ip4 != nil {
				req = append(req, 0x01) // IPv4
				req = append(req, ip4...)
			} else {
				req = append(req, 0x04) // IPv6
				req = append(req, ip...)
			}
		} else {
			// 域名长度字段只有 1 字节（最大 255）。域名 >255 时 byte(len(host))
			// 会截断，向上游发出长度字段错误的损坏帧。
			// host 来自上面的 net.SplitHostPort(target)，已是纯 host（无端口）。
			// 在写入前显式拒绝，返回明确错误而非静默截断。
			if len(host) > 255 {
				proxyConn.Close()
				return nil, fmt.Errorf("socks5 domain too long: %d bytes (max 255)", len(host))
			}
			req = append(req, 0x03) // Domain
			req = append(req, byte(len(host)))
			req = append(req, []byte(host)...)
		}

		// 添加端口
		portUint, err := strconv.ParseUint(port, 10, 16)
		if err != nil {
			proxyConn.Close()
			return nil, err
		}
		portBytes := make([]byte, 2)
		binary.BigEndian.PutUint16(portBytes, uint16(portUint))
		req = append(req, portBytes...)

		if _, err := proxyConn.Write(req); err != nil {
			proxyConn.Close()
			return nil, err
		}

		// 读取响应
		if err := readSOCKS5ConnectReply(proxyConn); err != nil {
			proxyConn.Close()
			return nil, err
		}
		if err := proxyConn.SetDeadline(time.Time{}); err != nil {
			proxyConn.Close()
			return nil, err
		}

		return proxyConn, nil

	default:
		return nil, fmt.Errorf("unsupported protocol: %s", p.Protocol)
	}
}

func readHTTPConnectResponse(conn net.Conn) (net.Conn, error) {
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream proxy connect failed: %s", resp.Status)
	}
	return &bufferedConn{Conn: conn, reader: reader}, nil
}

func readSOCKS5ConnectReply(conn io.Reader) error {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return err
	}
	if header[0] != 0x05 {
		return fmt.Errorf("invalid socks5 reply version: %d", header[0])
	}
	if _, err := readSOCKS5Address(conn, header[3]); err != nil {
		return err
	}
	port := make([]byte, 2)
	if _, err := io.ReadFull(conn, port); err != nil {
		return err
	}
	if header[1] != 0x00 {
		return fmt.Errorf("socks5 connect failed, code: %d", header[1])
	}
	return nil
}
