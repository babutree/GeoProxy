package proxy

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
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
	log.Printf("[socks5] 服务器监听 %s [%s]", s.port, authStatus)

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
				log.Printf("[socks5] Accept 临时错误: %v", err)
				time.Sleep(50 * time.Millisecond)
				continue
			}
			if strings.Contains(err.Error(), "use of closed network connection") {
				return nil
			}
			acceptFailures++
			log.Printf("[socks5] Accept 错误: %v", err)
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
			log.Printf("[socks5] 设置入站协议 deadline 失败: %v", err)
			return
		}
	}

	// SOCKS5 握手
	route, err := s.socks5Handshake(clientConn)
	if err != nil {
		log.Printf("[socks5] 握手失败: %v", err)
		return
	}

	// 读取请求
	target, err := s.readSOCKS5Request(clientConn)
	if err != nil {
		log.Printf("[socks5] 读取请求失败: %v", err)
		return
	}
	if protocolTimeout > 0 {
		if err := clientConn.SetDeadline(time.Time{}); err != nil {
			log.Printf("[socks5] 清除入站协议 deadline 失败: %v", err)
			return
		}
	}

	// 内网/本地目标直连，不经上游节点（等同 NO_PROXY 例外）。
	if isBypassTarget(target) {
		s.socks5Direct(clientConn, target)
		return
	}

	// 带重试的连接上游代理
	// selector 可返回 HTTP 或 SOCKS5 上游；HTTP 上游必须为当前目标建立 CONNECT 隧道。
	tried := []int64{}
	for attempt := 0; attempt <= s.runtimeConfig().MaxRetry; attempt++ {
		p, err := s.selectSOCKS5Proxy(route, tried)
		if err != nil {
			log.Printf("[socks5] 无可用上游代理: %v", err)
			s.sendSOCKS5Reply(clientConn, 0x01) // 通用失败
			return
		}

		tried = append(tried, p.ID)

		// 连接上游代理
		upstreamConn, err := s.dialViaProxy(p, target)
		if err != nil {
			log.Printf("[socks5] 通过节点 %s（%s）拨号 %s 失败: %v", p.Address, p.Protocol, target, err)
			if isHTTPConnectCapabilityRejection(err) {
				// HTTP 上游对目标端口的显式策略拒绝不等于节点失效；只放弃本次候选，
				// 让重试选择其它具备该目标能力的节点，避免污染全局健康计数。
				log.Printf("[socks5] HTTP 上游不具备目标 CONNECT 能力，跳过健康失败计数 target=%s proxy=%s", target, p.Address)
				s.releaseFailedBinding(route, p)
				continue
			}
			recordProxyFailure(s.storage, p)
			s.releaseFailedBinding(route, p)
			continue
		}

		// 发送成功响应
		if err := s.sendSOCKS5Reply(clientConn, 0x00); err != nil {
			upstreamConn.Close()
			s.releaseFailedBinding(route, p)
			return
		}

		log.Printf("[socks5] 已通过节点 %s 建立 %s", p.Address, target)
		var recordSuccess sync.Once
		idleTimeout := s.socks5RelayIdleTimeout()
		relayResult := relaySOCKS5(clientConn, upstreamConn, idleTimeout, func() {
			recordSuccess.Do(func() {
				if err := s.storage.RecordProxyUseByID(p.ID, true); err != nil {
					log.Printf("[socks5] 记录节点成功使用失败 id=%d: %v", p.ID, err)
				}
			})
		})
		logSOCKS5RelayResult(target, idleTimeout, relayResult)
		if relayResult.upstreamToClientBytes == 0 {
			// CONNECT 成功只证明隧道建立，不能证明节点完成了有效转发。
			// 零上游数据时保留 fail_count，仅释放本次粘滞绑定供后续重新选路。
			s.releaseFailedBinding(route, p)
		}
		return
	}

	// 所有重试都失败
	s.sendSOCKS5Reply(clientConn, 0x01) // 通用失败
	log.Printf("[socks5] 所有上游节点均失败 target=%s", target)
}

func (s *SOCKS5Server) releaseFailedBinding(route auth.ParsedUsername, p *storage.Proxy) {
	if route.Session == "" || s.sessions == nil {
		return
	}
	s.sessions.RemoveIfProxyID(route.Session, p.ID)
}

type socks5RelayDirection uint8

const (
	socks5RelayClientToUpstream socks5RelayDirection = iota
	socks5RelayUpstreamToClient
)

const defaultSOCKS5RelayIdleTimeout = 10 * time.Second

type socks5RelayCopyResult struct {
	direction socks5RelayDirection
	bytes     int64
	err       error
	timedOut  bool
}

type socks5RelayResult struct {
	clientToUpstreamBytes    int64
	clientToUpstreamErr      error
	clientToUpstreamTimedOut bool
	clientToUpstreamCanceled bool
	clientToUpstreamDeadline error
	upstreamToClientBytes    int64
	upstreamToClientErr      error
	upstreamToClientTimedOut bool
	upstreamToClientCanceled bool
	upstreamToClientDeadline error
	coordinationDirection    socks5RelayDirection
	coordinationErr          error
}

type socks5RelayObservedWriter struct {
	dst     io.Writer
	onWrite func()
}

func (w *socks5RelayObservedWriter) Write(p []byte) (int, error) {
	n, err := w.dst.Write(p)
	if n > 0 && w.onWrite != nil {
		w.onWrite()
	}
	return n, err
}

type socks5RelayIdleDeadline struct {
	mu            sync.Mutex
	readConn      net.Conn
	writeConn     net.Conn
	timeout       time.Duration
	armed         bool
	diagnosticErr error
}

func (d *socks5RelayIdleDeadline) arm() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	deadline := time.Now().Add(d.timeout)
	if err := d.readConn.SetReadDeadline(deadline); err != nil {
		return err
	}
	if err := d.writeConn.SetWriteDeadline(deadline); err != nil {
		_ = d.readConn.SetReadDeadline(time.Time{})
		return err
	}
	d.armed = true
	return nil
}

func (d *socks5RelayIdleDeadline) progress() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.armed {
		return
	}
	deadline := time.Now().Add(d.timeout)
	if err := d.readConn.SetReadDeadline(deadline); err != nil {
		d.rememberDiagnosticLocked(fmt.Errorf("刷新读 deadline: %w", err))
	}
	if err := d.writeConn.SetWriteDeadline(deadline); err != nil {
		d.rememberDiagnosticLocked(fmt.Errorf("刷新写 deadline: %w", err))
	}
}

func (d *socks5RelayIdleDeadline) clear() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.armed {
		return
	}
	d.armed = false
	if err := d.readConn.SetReadDeadline(time.Time{}); err != nil {
		d.rememberDiagnosticLocked(fmt.Errorf("清除读 deadline: %w", err))
	}
	if err := d.writeConn.SetWriteDeadline(time.Time{}); err != nil {
		d.rememberDiagnosticLocked(fmt.Errorf("清除写 deadline: %w", err))
	}
}

func (d *socks5RelayIdleDeadline) rememberDiagnosticLocked(err error) {
	if d.diagnosticErr == nil {
		d.diagnosticErr = err
	}
}

func (d *socks5RelayIdleDeadline) diagnosticError() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.diagnosticErr
}

// relaySOCKS5 协调双向复制。任一方向正常 EOF 时只向对应目的端传播
// CloseWrite，并为仍工作的另一方向启用空闲 deadline；每次成功转发都会刷新
// deadline。非正常错误快速关闭两端，最终始终等待两个复制任务返回。
func relaySOCKS5(clientConn, upstreamConn net.Conn, idleTimeout time.Duration, onUpstreamWrite func()) socks5RelayResult {
	results := make(chan socks5RelayCopyResult, 2)
	clientToUpstreamIdle := &socks5RelayIdleDeadline{
		readConn:  clientConn,
		writeConn: upstreamConn,
		timeout:   idleTimeout,
	}
	upstreamToClientIdle := &socks5RelayIdleDeadline{
		readConn:  upstreamConn,
		writeConn: clientConn,
		timeout:   idleTimeout,
	}
	observedUpstreamWriter := &socks5RelayObservedWriter{
		dst:     upstreamConn,
		onWrite: clientToUpstreamIdle.progress,
	}
	observedClientWriter := &socks5RelayObservedWriter{
		dst: clientConn,
		onWrite: func() {
			upstreamToClientIdle.progress()
			if onUpstreamWrite != nil {
				onUpstreamWrite()
			}
		},
	}
	go copySOCKS5Relay(observedUpstreamWriter, clientConn, socks5RelayClientToUpstream, results)
	go copySOCKS5Relay(observedClientWriter, upstreamConn, socks5RelayUpstreamToClient, results)

	first := <-results
	coordinationDirection := first.direction
	var coordinationErr error
	fastClose := first.err != nil
	if !fastClose {
		coordinationErr = armRemainingSOCKS5Relay(first.direction, clientConn, upstreamConn, clientToUpstreamIdle, upstreamToClientIdle)
		fastClose = coordinationErr != nil
	}
	if fastClose {
		clientToUpstreamIdle.clear()
		upstreamToClientIdle.clear()
		_ = clientConn.Close()
		_ = upstreamConn.Close()
	}
	second := <-results
	if !fastClose && second.err == nil {
		coordinationDirection = second.direction
		coordinationErr = propagateSOCKS5RelayEOF(second.direction, clientConn, upstreamConn)
	}
	clientToUpstreamIdle.clear()
	upstreamToClientIdle.clear()
	_ = clientConn.Close()
	_ = upstreamConn.Close()

	var result socks5RelayResult
	result.add(first, false)
	result.add(second, fastClose)
	result.clientToUpstreamDeadline = clientToUpstreamIdle.diagnosticError()
	result.upstreamToClientDeadline = upstreamToClientIdle.diagnosticError()
	result.coordinationDirection = coordinationDirection
	result.coordinationErr = coordinationErr
	return result
}

func armRemainingSOCKS5Relay(
	completed socks5RelayDirection,
	clientConn net.Conn,
	upstreamConn net.Conn,
	clientToUpstreamIdle *socks5RelayIdleDeadline,
	upstreamToClientIdle *socks5RelayIdleDeadline,
) error {
	if completed == socks5RelayClientToUpstream {
		if err := upstreamToClientIdle.arm(); err != nil {
			return fmt.Errorf("启用上游到客户端 idle deadline: %w", err)
		}
		if err := closeSOCKS5RelayWrite(upstreamConn); err != nil {
			return fmt.Errorf("向上游传播客户端 EOF: %w", err)
		}
		return nil
	}
	if err := clientToUpstreamIdle.arm(); err != nil {
		return fmt.Errorf("启用客户端到上游 idle deadline: %w", err)
	}
	if err := closeSOCKS5RelayWrite(clientConn); err != nil {
		return fmt.Errorf("向客户端传播上游 EOF: %w", err)
	}
	return nil
}

func propagateSOCKS5RelayEOF(direction socks5RelayDirection, clientConn, upstreamConn net.Conn) error {
	if direction == socks5RelayClientToUpstream {
		if err := closeSOCKS5RelayWrite(upstreamConn); err != nil {
			return fmt.Errorf("向上游传播客户端 EOF: %w", err)
		}
		return nil
	}
	if err := closeSOCKS5RelayWrite(clientConn); err != nil {
		return fmt.Errorf("向客户端传播上游 EOF: %w", err)
	}
	return nil
}

func closeSOCKS5RelayWrite(conn net.Conn) error {
	if closeWriter, ok := conn.(interface{ CloseWrite() error }); ok {
		return closeWriter.CloseWrite()
	}
	if buffered, ok := conn.(*bufferedConn); ok {
		return closeSOCKS5RelayWrite(buffered.Conn)
	}
	return fmt.Errorf("连接 %T 不支持 CloseWrite", conn)
}

func copySOCKS5Relay(dst io.Writer, src io.Reader, direction socks5RelayDirection, results chan<- socks5RelayCopyResult) {
	n, err := io.Copy(dst, src)
	timedOut := false
	if netErr, ok := err.(net.Error); ok {
		timedOut = netErr.Timeout()
	}
	results <- socks5RelayCopyResult{direction: direction, bytes: n, err: err, timedOut: timedOut}
}

func (r *socks5RelayResult) add(result socks5RelayCopyResult, canceled bool) {
	if result.direction == socks5RelayClientToUpstream {
		r.clientToUpstreamBytes = result.bytes
		r.clientToUpstreamErr = result.err
		r.clientToUpstreamTimedOut = result.timedOut
		r.clientToUpstreamCanceled = canceled
		return
	}
	r.upstreamToClientBytes = result.bytes
	r.upstreamToClientErr = result.err
	r.upstreamToClientTimedOut = result.timedOut
	r.upstreamToClientCanceled = canceled
}

func logSOCKS5RelayResult(target string, idleTimeout time.Duration, result socks5RelayResult) {
	logSOCKS5RelayDirection(
		target,
		"客户端->上游",
		result.clientToUpstreamBytes,
		result.clientToUpstreamErr,
		result.clientToUpstreamTimedOut,
		result.clientToUpstreamCanceled,
		idleTimeout,
	)
	logSOCKS5RelayDirection(
		target,
		"上游->客户端",
		result.upstreamToClientBytes,
		result.upstreamToClientErr,
		result.upstreamToClientTimedOut,
		result.upstreamToClientCanceled,
		idleTimeout,
	)
	if result.clientToUpstreamDeadline != nil {
		log.Printf("[socks5] relay deadline 异常 target=%s direction=客户端->上游 idle=%s: %v", target, idleTimeout, result.clientToUpstreamDeadline)
	}
	if result.upstreamToClientDeadline != nil {
		log.Printf("[socks5] relay deadline 异常 target=%s direction=上游->客户端 idle=%s: %v", target, idleTimeout, result.upstreamToClientDeadline)
	}
	if result.coordinationErr != nil {
		log.Printf(
			"[socks5] relay 协调异常 target=%s completed_direction=%s: %v",
			target,
			socks5RelayDirectionName(result.coordinationDirection),
			result.coordinationErr,
		)
	}
}

func logSOCKS5RelayDirection(
	target string,
	direction string,
	bytes int64,
	err error,
	timedOut bool,
	canceled bool,
	idleTimeout time.Duration,
) {
	if err == nil || canceled {
		return
	}
	if timedOut {
		log.Printf("[socks5] relay 空闲超时 target=%s direction=%s bytes=%d idle=%s", target, direction, bytes, idleTimeout)
		return
	}
	log.Printf("[socks5] relay 转发异常 target=%s direction=%s bytes=%d: %v", target, direction, bytes, err)
}

func socks5RelayDirectionName(direction socks5RelayDirection) string {
	if direction == socks5RelayClientToUpstream {
		return "客户端->上游"
	}
	return "上游->客户端"
}

// socks5Direct 为内网/本地目标建立直连，不经上游节点。
func (s *SOCKS5Server) socks5Direct(clientConn net.Conn, target string) {
	timeout := time.Duration(s.runtimeConfig().ValidateTimeout) * time.Second
	upstreamConn, err := net.DialTimeout("tcp", target, timeout)
	if err != nil {
		log.Printf("[socks5] 直连拨号 %s 失败: %v", target, err)
		s.sendSOCKS5Reply(clientConn, 0x01) // 通用失败
		return
	}
	if err := s.sendSOCKS5Reply(clientConn, 0x00); err != nil {
		upstreamConn.Close()
		return
	}
	log.Printf("[socks5] %s 已直连建立（绕过上游）", target)
	idleTimeout := s.socks5RelayIdleTimeout()
	relayResult := relaySOCKS5(clientConn, upstreamConn, idleTimeout, nil)
	logSOCKS5RelayResult(target, idleTimeout, relayResult)
}

// socks5RelayIdleTimeout 将运行时校验超时映射为半关闭后的空闲回收门槛。
// ValidateTimeout<=0 时使用与配置默认值一致的 10 秒，确保 relay 始终有界。
func (s *SOCKS5Server) socks5RelayIdleTimeout() time.Duration {
	if cfg := s.runtimeConfig(); cfg != nil && cfg.ValidateTimeout > 0 {
		return time.Duration(cfg.ValidateTimeout) * time.Second
	}
	return defaultSOCKS5RelayIdleTimeout
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
		s.sendSOCKS5Reply(conn, 0x01) // 通用失败
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
		// 发送 CONNECT 请求。若节点带认证凭据，附加 Proxy-Authorization 头（Basic）。
		// 凭据仅在握手帧中使用，绝不写入日志。
		if p.Username != "" || p.Password != "" {
			cred := base64.StdEncoding.EncodeToString([]byte(p.Username + ":" + p.Password))
			fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: Basic %s\r\n\r\n", target, target, cred)
		} else {
			fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
		}
		proxiedConn, err := readHTTPConnectResponseForTarget(conn, target)
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

		// SOCKS5 握手（无认证或 RFC1929 用户名/密码认证）。凭据绝不写入日志。
		if err := socks5ClientHandshake(proxyConn, p.Username, p.Password); err != nil {
			proxyConn.Close()
			return nil, err
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
	return readHTTPConnectResponseForTarget(conn, "")
}

type httpConnectRejectionError struct {
	target     string
	statusCode int
	status     string
}

func (e *httpConnectRejectionError) Error() string {
	if e.target == "" {
		return fmt.Sprintf("upstream proxy connect failed: %s", e.status)
	}
	return fmt.Sprintf("upstream proxy connect failed: %s (target %s)", e.status, e.target)
}

func isHTTPConnectCapabilityRejection(err error) bool {
	var rejection *httpConnectRejectionError
	if !errors.As(err, &rejection) || rejection.target == "" {
		return false
	}
	_, port, splitErr := net.SplitHostPort(rejection.target)
	if splitErr != nil || port != "80" {
		return false
	}
	// 只豁免能明确表达“策略禁止/方法不支持”的状态。400、407、408、
	// 429 与 5xx 仍可能是请求、认证、限流或节点故障，保持原健康计数。
	switch rejection.statusCode {
	case http.StatusForbidden, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	default:
		return false
	}
}

func readHTTPConnectResponseForTarget(conn net.Conn, target string) (net.Conn, error) {
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &httpConnectRejectionError{
			target:     target,
			statusCode: resp.StatusCode,
			status:     resp.Status,
		}
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
