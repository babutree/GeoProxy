package proxy

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/babutree/GeoProxy/affinity"
	"github.com/babutree/GeoProxy/auth"
	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/selector"
	"github.com/babutree/GeoProxy/storage"
	"golang.org/x/net/proxy"
)

var (
	sharedSessions   *affinity.Store
	sharedSessionsMu sync.Mutex
)

type Server struct {
	storage  proxyStore
	cfg      *config.Config
	port     string
	sessions *affinity.Store
}

type proxyStore interface {
	selector.Store
	RecordProxyUseByID(id int64, success bool) error
	RecordProxyFailureByID(id int64, threshold int) error
}

// failDisableThreshold 与健康检查一致：连续失败累计到该阈值即禁用节点。
// 请求路径与健康检查路径共用同一阈值，避免请求失败只累加不禁用而产生
// “status=active 但 fail_count>=3”的僵尸节点（被选路和健康检查同时排除、
// 又永远得不到成功来归零）。禁用后节点在管理界面可见，可显式恢复。见 BUG-53。
const failDisableThreshold = 3

func New(s *storage.Storage, cfg *config.Config, port string) *Server {
	return &Server{
		storage:  s,
		cfg:      cfg,
		port:     port,
		sessions: SessionStore(cfg),
	}
}

func SessionStore(cfg *config.Config) *affinity.Store {
	sharedSessionsMu.Lock()
	defer sharedSessionsMu.Unlock()
	if sharedSessions == nil {
		sharedSessions = affinity.New(time.Duration(cfg.SessionTTLMinutes) * time.Minute)
	}
	return sharedSessions
}

func (s *Server) Start() error {
	cfg := s.runtimeConfig()
	authStatus := "无认证"
	if cfg.ProxyAuthEnabled {
		authStatus = fmt.Sprintf("需认证 (用户: %s)", cfg.ProxyAuthUsername)
	}
	log.Printf("[proxy] HTTP 代理服务器监听 %s [%s]", s.port, authStatus)
	return s.httpServer().ListenAndServe()
}

// runtimeConfig 读取当前已发布配置快照。
// config.Save 会替换全局指针；请求路径不得继续使用启动时缓存的 s.cfg。
func (s *Server) runtimeConfig() *config.Config {
	if live := config.Get(); live != nil {
		return live
	}
	return s.cfg
}

// httpServer 构造入站 HTTP/CONNECT 服务。正的 ValidateTimeout 映射为
// ReadHeaderTimeout，防止半请求头 Slowloris 无限占用连接；0 保持不设超时。
func (s *Server) httpServer() *http.Server {
	cfg := s.runtimeConfig()
	srv := &http.Server{Addr: s.port, Handler: s}
	if timeout := time.Duration(cfg.ValidateTimeout) * time.Second; timeout > 0 {
		srv.ReadHeaderTimeout = timeout
	}
	return srv
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cfg := s.runtimeConfig()
	route := auth.ParsedUsername{}
	// 认证检查（如果启用）
	if cfg.ProxyAuthEnabled {
		parsed, ok := s.checkAuth(r)
		if !ok {
			w.Header().Set("Proxy-Authenticate", `Basic realm="GeoProxy"`)
			http.Error(w, "Proxy Authentication Required", http.StatusProxyAuthRequired)
			return
		}
		route = parsed
	}

	if r.Method == http.MethodConnect {
		s.handleTunnel(w, r, route)
	} else {
		s.handleHTTP(w, r, route)
	}
}

// checkAuth 验证代理 Basic Auth
func (s *Server) checkAuth(r *http.Request) (auth.ParsedUsername, bool) {
	authHeader := r.Header.Get("Proxy-Authorization")
	if authHeader == "" {
		return auth.ParsedUsername{}, false
	}

	// 解析 Basic Auth
	const prefix = "Basic "
	if !strings.HasPrefix(authHeader, prefix) {
		return auth.ParsedUsername{}, false
	}

	decoded, err := base64.StdEncoding.DecodeString(authHeader[len(prefix):])
	if err != nil {
		return auth.ParsedUsername{}, false
	}

	credentials := strings.SplitN(string(decoded), ":", 2)
	if len(credentials) != 2 {
		return auth.ParsedUsername{}, false
	}

	parsed, err := auth.ParseUsername(credentials[0])
	if err != nil {
		return auth.ParsedUsername{}, false
	}
	password := credentials[1]

	// 验证用户名和密码
	return parsed, auth.VerifyPassword(parsed.Base, password, s.runtimeConfig().ProxyAuthUsername, s.runtimeConfig().ProxyAuthPassword, s.runtimeConfig().ProxyAuthPasswordHash)
}

func (s *Server) selectProxy(route auth.ParsedUsername, tried []int64) (*storage.Proxy, error) {
	route = withDefaultRegion(route, s.runtimeConfig().DefaultRegion)
	return selector.Resolve(s.storage, s.sessions, route, tried)
}

func withDefaultRegion(route auth.ParsedUsername, defaultRegion string) auth.ParsedUsername {
	if route.Region != "" || defaultRegion == "" {
		return route
	}
	route.Region = strings.ToLower(strings.TrimSpace(defaultRegion))
	return route
}

func recordProxyFailure(store proxyStore, p *storage.Proxy) {
	if err := store.RecordProxyFailureByID(p.ID, failDisableThreshold); err != nil {
		log.Printf("[proxy] 记录节点失败次数失败 id=%d: %v", p.ID, err)
	}
}

// releaseFailedBinding 在拨号/转发失败后，若该 session 仍绑定到刚失败的节点，
// 则释放绑定，使后续请求重新选路并尊重 cooldown/健康过滤，而不是经 stickyBoundProxy
// 粘死在坏节点上。
// 只在绑定确实指向本次失败节点时释放，避免误删已被其它请求重绑的会话。
func (s *Server) releaseFailedBinding(route auth.ParsedUsername, p *storage.Proxy) {
	if route.Session == "" || s.sessions == nil {
		return
	}
	s.sessions.RemoveIfProxyID(route.Session, p.ID)
}

// handleHTTP 处理普通 HTTP 请求（带自动重试）
func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request, route auth.ParsedUsername) {
	buffered, stream, replayable, err := readReusableBody(r)
	if err != nil {
		http.Error(w, "read request body failed", http.StatusBadRequest)
		return
	}
	// 超限流式 body：r.Body 未在 readReusableBody 内关闭，转发后统一关闭。
	if !replayable && stream != nil {
		defer r.Body.Close()
	}

	// 内网/本地目标直连，不经上游节点（等同浏览器代理例外 / NO_PROXY）。
	if isBypassTarget(r.Host) {
		s.httpDirect(w, r, buffered, stream, replayable)
		return
	}

	var tried []int64
	for attempt := 0; attempt <= s.runtimeConfig().MaxRetry; attempt++ {
		p, err := s.selectProxy(route, tried)
		if err != nil {
			http.Error(w, proxySelectionError(route, err), http.StatusServiceUnavailable)
			return
		}

		tried = append(tried, p.ID)

		client, err := s.buildClient(p)
		if err != nil {
			recordProxyFailure(s.storage, p)
			s.releaseFailedBinding(route, p)
			if !replayable {
				// body 不可重放（已消费/流式），无法重试。
				http.Error(w, "all proxies failed", http.StatusBadGateway)
				return
			}
			continue
		}

		// 转发请求（使用完整 URL，上游代理通过 client transport 设置）
		req, err := http.NewRequest(r.Method, r.URL.String(), forwardBody(buffered, stream, replayable))
		if err != nil {
			client.CloseIdleConnections()
			continue
		}
		// 超限流式 body 长度未知，显式标记为分块传输，避免被当作 0 长度。
		if !replayable && stream != nil {
			req.ContentLength = -1
		}
		req.Header = r.Header.Clone()
		cleanForwardHeaders(req.Header)

		resp, err := client.Do(req)
		if err != nil {
			client.CloseIdleConnections()
			log.Printf("[proxy] 请求 %s 通过节点 %s 失败", r.RequestURI, p.Address)
			recordProxyFailure(s.storage, p)
			s.releaseFailedBinding(route, p)
			if !replayable {
				// body 已在本次尝试中被消费，不能重放，直接失败。
				http.Error(w, "all proxies failed", http.StatusBadGateway)
				return
			}
			continue
		}

		// 写回响应
		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		_ = resp.Body.Close()
		client.CloseIdleConnections()
		s.storage.RecordProxyUseByID(p.ID, true)
		if resp.StatusCode == 429 {
			log.Printf("[proxy] 节点返回 429 request=%s proxy=%s protocol=%s", r.RequestURI, p.Address, p.Protocol)
		} else {
			log.Printf("[proxy] 请求完成 request=%s proxy=%s status=%d", r.RequestURI, p.Address, resp.StatusCode)
		}
		return
	}

	http.Error(w, "all proxies failed", http.StatusBadGateway)
}

// httpDirect 为内网/本地 HTTP 目标直连转发，不经上游节点、不重试（本地目标无需故障转移）。
func (s *Server) httpDirect(w http.ResponseWriter, r *http.Request, buffered []byte, stream io.Reader, replayable bool) {
	req, err := http.NewRequest(r.Method, r.URL.String(), forwardBody(buffered, stream, replayable))
	if err != nil {
		http.Error(w, "build direct request failed", http.StatusBadGateway)
		return
	}
	if !replayable && stream != nil {
		req.ContentLength = -1
	}
	req.Header = r.Header.Clone()
	cleanForwardHeaders(req.Header)

	client := &http.Client{
		Timeout: time.Duration(s.runtimeConfig().ValidateTimeout) * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[proxy] 直连请求失败 request=%s: %v", r.RequestURI, err)
		http.Error(w, "direct request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
	log.Printf("[proxy] 直连请求完成 request=%s status=%d", r.RequestURI, resp.StatusCode)
}

// handleTunnel 处理 HTTPS CONNECT 隧道（带自动重试）
func (s *Server) handleTunnel(w http.ResponseWriter, r *http.Request, route auth.ParsedUsername) {
	// 内网/本地目标直连，不经上游节点（等同浏览器代理例外 / NO_PROXY）。
	if isBypassTarget(r.Host) {
		s.tunnelDirect(w, r)
		return
	}

	var tried []int64
	for attempt := 0; attempt <= s.runtimeConfig().MaxRetry; attempt++ {
		p, err := s.selectProxy(route, tried)
		if err != nil {
			http.Error(w, proxySelectionError(route, err), http.StatusServiceUnavailable)
			return
		}

		tried = append(tried, p.ID)

		conn, err := s.dialViaProxy(p, r.Host)
		if err != nil {
			log.Printf("[tunnel] 通过节点 %s 拨号 %s 失败: %v", p.Address, r.Host, err)
			recordProxyFailure(s.storage, p)
			s.releaseFailedBinding(route, p)
			continue
		}

		s.relayHTTPConnect(w, conn, r.Host, p, route)
		return
	}

	http.Error(w, "all proxies failed", http.StatusBadGateway)
}

// tunnelDirect 为内网/本地 CONNECT 目标建立直连隧道，不经上游节点。
func (s *Server) tunnelDirect(w http.ResponseWriter, r *http.Request) {
	timeout := time.Duration(s.runtimeConfig().ValidateTimeout) * time.Second
	conn, err := net.DialTimeout("tcp", r.Host, timeout)
	if err != nil {
		log.Printf("[tunnel] 直连拨号 %s 失败: %v", r.Host, err)
		http.Error(w, "direct dial failed", http.StatusBadGateway)
		return
	}
	s.relayHTTPConnect(w, conn, r.Host, nil, auth.ParsedUsername{})
}

// maxReplayBodyBytes 限制为“可重放”而缓存进内存的请求体上限。
// 选 1 MiB：足以覆盖绝大多数普通 API / 表单 POST 的重试重放需求，同时避免
// 任意大 body（如大文件上传）被 io.ReadAll 整体读入内存造成内存放大 / OOM，
// 也避免持续的流式 body 在缓存阶段阻塞。超过上限的 body 退回单次流式转发
// （不缓存、不重试）。见 BUG-54。
const maxReplayBodyBytes = 1 << 20 // 1 MiB

// readReusableBody 为转发准备请求体，返回三种形态：
//   - 无 body：buffered=nil, stream=nil, replayable=true。
//   - body 在 maxReplayBodyBytes 上限内：buffered 保存完整内容、replayable=true，
//     r.Body 已在此处关闭；每次重试用 bytes.NewReader(buffered) 重放。
//   - body 超过上限：不整体入内存，stream = (已读前缀 + 剩余 r.Body) 的单次流，
//     replayable=false，r.Body 交由调用方关闭；该 body 只能被转发一次。
//
// 关键点：最多只预读 maxReplayBodyBytes+1 字节即可判定是否超限，超限 body 绝不
// 被整体读入内存。
func readReusableBody(r *http.Request) (buffered []byte, stream io.Reader, replayable bool, err error) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, nil, true, nil
	}
	// 只预读到上限+1 字节：读满 (cap+1) 即说明超限。
	prefix, err := io.ReadAll(io.LimitReader(r.Body, maxReplayBodyBytes+1))
	if err != nil {
		r.Body.Close()
		return nil, nil, false, err
	}
	if len(prefix) <= maxReplayBodyBytes {
		// 完整读入，可安全重放。
		r.Body.Close()
		return prefix, nil, true, nil
	}
	// 超限：前缀 + 剩余 body 拼成单次流，body 由调用方在转发后关闭。
	return nil, io.MultiReader(bytes.NewReader(prefix), r.Body), false, nil
}

// forwardBody 为单次转发构造请求体读取器。可重放时每次返回一个新的
// bytes.Reader（无 body 时返回 nil）；不可重放时返回底层单次流。
func forwardBody(buffered []byte, stream io.Reader, replayable bool) io.Reader {
	if !replayable {
		return stream
	}
	if buffered == nil {
		return nil
	}
	return bytes.NewReader(buffered)
}

func cleanForwardHeaders(header http.Header) {
	for _, value := range header.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			header.Del(strings.TrimSpace(token))
		}
	}
	for _, name := range []string{
		"Proxy-Authorization",
		"Proxy-Connection",
		"Connection",
		"Keep-Alive",
		"TE",
		"Trailer",
		"Upgrade",
	} {
		header.Del(name)
	}
}

func proxySelectionError(route auth.ParsedUsername, err error) string {
	if errors.Is(err, selector.ErrNoNode) && route.Region != "" {
		return fmt.Sprintf("no available node for region: %s", route.Region)
	}
	return "no available proxy"
}

// socks5ClientHandshake 执行出站 SOCKS5 认证协商（RFC1928 + RFC1929）。
// 无凭据时只提供 no-auth；有凭据时同时提供 no-auth 与 user/pass，按上游选择的方法处理。
// 凭据仅在握手帧中传输，绝不写入日志或错误串。
func socks5ClientHandshake(conn net.Conn, username, password string) error {
	if username != "" || password != "" {
		if _, err := conn.Write([]byte{0x05, 0x02, 0x00, 0x02}); err != nil {
			return err
		}
	} else {
		if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
			return err
		}
	}

	handshake := make([]byte, 2)
	if _, err := io.ReadFull(conn, handshake); err != nil {
		return err
	}
	if handshake[0] != 0x05 {
		return fmt.Errorf("socks5 handshake failed")
	}
	switch handshake[1] {
	case 0x00:
		return nil
	case 0x02:
		return socks5UserPassAuth(conn, username, password)
	default:
		return fmt.Errorf("socks5 handshake failed")
	}
}

// socks5UserPassAuth 执行 RFC1929 用户名/密码子协商。凭据绝不写入日志。
func socks5UserPassAuth(conn net.Conn, username, password string) error {
	if len(username) > 255 || len(password) > 255 {
		return fmt.Errorf("socks5 credential too long")
	}
	req := []byte{0x01}
	req = append(req, byte(len(username)))
	req = append(req, []byte(username)...)
	req = append(req, byte(len(password)))
	req = append(req, []byte(password)...)
	if _, err := conn.Write(req); err != nil {
		return err
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return err
	}
	if resp[1] != 0x00 {
		return fmt.Errorf("socks5 authentication failed")
	}
	return nil
}

func (s *Server) dialViaProxy(p *storage.Proxy, host string) (net.Conn, error) {
	timeout := time.Duration(s.runtimeConfig().ValidateTimeout) * time.Second
	switch p.Protocol {
	case "http":
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
		// 发送 CONNECT 请求给上游 HTTP 代理。若节点带认证凭据，附加
		// Proxy-Authorization 头（Basic）。凭据仅在握手帧中使用，绝不写入日志。
		if p.Username != "" || p.Password != "" {
			cred := base64.StdEncoding.EncodeToString([]byte(p.Username + ":" + p.Password))
			fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: Basic %s\r\n\r\n", host, host, cred)
		} else {
			fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", host, host)
		}
		reader := bufio.NewReader(conn)
		resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
		if err != nil {
			conn.Close()
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			conn.Close()
			return nil, fmt.Errorf("upstream proxy connect failed: %s", resp.Status)
		}
		if err := conn.SetDeadline(time.Time{}); err != nil {
			conn.Close()
			return nil, err
		}
		return &bufferedConn{Conn: conn, reader: reader}, nil
	case "socks5":
		// 出站 SOCKS5 域名字段长度只有 1 字节（最大 255）。域名 >255 时
		// byte(len(host)) 会截断，向上游发出长度字段错误的损坏帧。
		// 在拨号前显式拒绝，返回明确错误而非静默截断。
		if targetHost, _, splitErr := net.SplitHostPort(host); splitErr == nil {
			if net.ParseIP(targetHost) == nil && len(targetHost) > 255 {
				return nil, fmt.Errorf("socks5 domain too long: %d bytes (max 255)", len(targetHost))
			}
		}
		// 手动执行 SOCKS5 握手，使 ValidateTimeout 能限制无响应上游；
		// golang.org/x/net/proxy 不提供握手阶段的独立 SetDeadline。
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

		if err := socks5ClientHandshake(proxyConn, p.Username, p.Password); err != nil {
			proxyConn.Close()
			return nil, err
		}

		targetHost, port, err := net.SplitHostPort(host)
		if err != nil {
			proxyConn.Close()
			return nil, err
		}

		req := []byte{0x05, 0x01, 0x00} // VER, CMD=CONNECT, RSV
		if ip := net.ParseIP(targetHost); ip != nil {
			if ip4 := ip.To4(); ip4 != nil {
				req = append(req, 0x01)
				req = append(req, ip4...)
			} else {
				req = append(req, 0x04)
				req = append(req, ip...)
			}
		} else {
			req = append(req, 0x03)
			req = append(req, byte(len(targetHost)))
			req = append(req, []byte(targetHost)...)
		}

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

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func (s *Server) buildClient(p *storage.Proxy) (*http.Client, error) {
	timeout := time.Duration(s.runtimeConfig().ValidateTimeout) * time.Second
	// 每次转发尝试都创建独立客户端；调用方须在尝试结束后释放其空闲连接。
	switch p.Protocol {
	case "http":
		proxyURL, err := url.Parse(fmt.Sprintf("http://%s", p.Address))
		if err != nil {
			return nil, err
		}
		if p.Username != "" || p.Password != "" {
			proxyURL.User = url.UserPassword(p.Username, p.Password)
		}
		return &http.Client{
			Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
			Timeout:   timeout,
		}, nil
	case "socks5":
		var socksAuth *proxy.Auth
		if p.Username != "" || p.Password != "" {
			socksAuth = &proxy.Auth{User: p.Username, Password: p.Password}
		}
		dialer, err := proxy.SOCKS5("tcp", p.Address, socksAuth, proxy.Direct)
		if err != nil {
			return nil, err
		}
		return &http.Client{
			Transport: &http.Transport{Dial: dialer.Dial},
			Timeout:   timeout,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", p.Protocol)
	}
}

const defaultHTTPConnectRelayIdleTimeout = defaultSOCKS5RelayIdleTimeout

// relayHTTPConnect 完成 Hijack 后的 HTTP CONNECT 数据面。
// Hijack 返回的 Reader 可能已缓存请求头之后的首包，必须把它交给 relay；
// 成功记账只在首个上游字节实际写到客户端后触发，避免把握手成功冒充数据面成功。
func (s *Server) relayHTTPConnect(
	w http.ResponseWriter,
	upstreamConn net.Conn,
	target string,
	selected *storage.Proxy,
	route auth.ParsedUsername,
) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		_ = upstreamConn.Close()
		if selected != nil {
			s.releaseFailedBinding(route, selected)
		}
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		log.Printf("[tunnel] 目标 %s 不支持 Hijack", target)
		return
	}

	clientConn, hijackedRW, err := hijacker.Hijack()
	if err != nil {
		_ = upstreamConn.Close()
		if clientConn != nil {
			_ = clientConn.Close()
		}
		if selected != nil {
			s.releaseFailedBinding(route, selected)
		}
		log.Printf("[tunnel] Hijack 目标 %s 失败: %v", target, err)
		return
	}
	if clientConn == nil || hijackedRW == nil || hijackedRW.Reader == nil || hijackedRW.Writer == nil {
		_ = upstreamConn.Close()
		if clientConn != nil {
			_ = clientConn.Close()
		}
		if selected != nil {
			s.releaseFailedBinding(route, selected)
		}
		log.Printf("[tunnel] Hijack 目标 %s 返回空连接或缓冲读写器", target)
		return
	}

	const establishedResponse = "HTTP/1.1 200 Connection Established\r\n\r\n"
	n, err := hijackedRW.WriteString(establishedResponse)
	if err == nil && n != len(establishedResponse) {
		err = io.ErrShortWrite
	}
	if err != nil {
		_ = upstreamConn.Close()
		_ = clientConn.Close()
		if selected != nil {
			s.releaseFailedBinding(route, selected)
		}
		log.Printf("[tunnel] 写入 CONNECT 成功响应失败 target=%s: %v", target, err)
		return
	}
	if err := hijackedRW.Flush(); err != nil {
		_ = upstreamConn.Close()
		_ = clientConn.Close()
		if selected != nil {
			s.releaseFailedBinding(route, selected)
		}
		log.Printf("[tunnel] 刷新 CONNECT 成功响应失败 target=%s: %v", target, err)
		return
	}

	if selected != nil {
		log.Printf("[tunnel] 已通过节点 %s 建立 %s", selected.Address, target)
	} else {
		log.Printf("[tunnel] 已直连建立 %s（绕过上游）", target)
	}

	// 用 bufferedConn 保留 Hijack Reader 中已经预读的首包；Write 仍使用底层
	// TCP 连接，响应已 Flush，不会把后续数据滞留在 HTTP writer 缓冲中。
	bufferedClientConn := &bufferedConn{Conn: clientConn, reader: hijackedRW.Reader}
	var recordSuccess sync.Once
	onUpstreamWrite := func() {
		if selected == nil {
			return
		}
		recordSuccess.Do(func() {
			if err := s.storage.RecordProxyUseByID(selected.ID, true); err != nil {
				log.Printf("[tunnel] 记录节点成功使用失败 id=%d: %v", selected.ID, err)
			}
		})
	}
	idleTimeout := s.httpConnectRelayIdleTimeout()
	result := relaySOCKS5(bufferedClientConn, upstreamConn, idleTimeout, onUpstreamWrite)
	logHTTPConnectRelayResult(target, idleTimeout, result)
	if selected != nil && result.upstreamToClientBytes == 0 {
		// 无有效上游数据不记成功；释放本次粘滞绑定，避免后续请求继续命中
		// 已建立但未完成有效转发的节点。
		s.releaseFailedBinding(route, selected)
	}
}

func (s *Server) httpConnectRelayIdleTimeout() time.Duration {
	if cfg := s.runtimeConfig(); cfg != nil && cfg.ValidateTimeout > 0 {
		return time.Duration(cfg.ValidateTimeout) * time.Second
	}
	return defaultHTTPConnectRelayIdleTimeout
}

func logHTTPConnectRelayResult(target string, idleTimeout time.Duration, result socks5RelayResult) {
	logHTTPConnectRelayDirection(target, "客户端->上游", result.clientToUpstreamBytes, result.clientToUpstreamErr, result.clientToUpstreamTimedOut, result.clientToUpstreamCanceled, idleTimeout)
	logHTTPConnectRelayDirection(target, "上游->客户端", result.upstreamToClientBytes, result.upstreamToClientErr, result.upstreamToClientTimedOut, result.upstreamToClientCanceled, idleTimeout)
	if result.clientToUpstreamDeadline != nil {
		log.Printf("[tunnel] relay deadline 异常 target=%s direction=客户端->上游 idle=%s: %v", target, idleTimeout, result.clientToUpstreamDeadline)
	}
	if result.upstreamToClientDeadline != nil {
		log.Printf("[tunnel] relay deadline 异常 target=%s direction=上游->客户端 idle=%s: %v", target, idleTimeout, result.upstreamToClientDeadline)
	}
	if result.coordinationErr != nil {
		log.Printf("[tunnel] relay 协调异常 target=%s: %v", target, result.coordinationErr)
	}
}

func logHTTPConnectRelayDirection(
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
		log.Printf("[tunnel] relay 空闲超时 target=%s direction=%s bytes=%d idle=%s", target, direction, bytes, idleTimeout)
		return
	}
	log.Printf("[tunnel] relay 转发异常 target=%s direction=%s bytes=%d: %v", target, direction, bytes, err)
}
