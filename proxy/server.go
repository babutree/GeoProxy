package proxy

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
	"goproxy/affinity"
	"goproxy/auth"
	"goproxy/config"
	"goproxy/selector"
	"goproxy/storage"
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
	DisableProxyByID(id int64) error
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
	authStatus := "无认证"
	if s.cfg.ProxyAuthEnabled {
		authStatus = fmt.Sprintf("需认证 (用户: %s)", s.cfg.ProxyAuthUsername)
	}
	log.Printf("http proxy server listening on %s [lowest latency] [%s]", s.port, authStatus)
	return s.httpServer().ListenAndServe()
}

// httpServer 构造入站 HTTP/CONNECT 服务。正的 ValidateTimeout 映射为
// ReadHeaderTimeout，防止半请求头 Slowloris 无限占用连接；0 保持不设超时。
func (s *Server) httpServer() *http.Server {
	srv := &http.Server{Addr: s.port, Handler: s}
	if timeout := time.Duration(s.cfg.ValidateTimeout) * time.Second; timeout > 0 {
		srv.ReadHeaderTimeout = timeout
	}
	return srv
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	route := auth.ParsedUsername{}
	// 认证检查（如果启用）
	if s.cfg.ProxyAuthEnabled {
		parsed, ok := s.checkAuth(r)
		if !ok {
			w.Header().Set("Proxy-Authenticate", `Basic realm="GoProxy"`)
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
	return parsed, auth.VerifyPassword(parsed.Base, password, s.cfg.ProxyAuthUsername, s.cfg.ProxyAuthPassword, s.cfg.ProxyAuthPasswordHash)
}

func (s *Server) selectProxy(route auth.ParsedUsername, tried []int64) (*storage.Proxy, error) {
	route = withDefaultRegion(route, s.cfg.DefaultRegion)
	return selector.Resolve(s.storage, s.sessions, route, tried)
}

func withDefaultRegion(route auth.ParsedUsername, defaultRegion string) auth.ParsedUsername {
	if route.Region != "" || defaultRegion == "" {
		return route
	}
	route.Region = strings.ToLower(strings.TrimSpace(defaultRegion))
	return route
}

// recordProxyFailure 记录一次失败计数；当累计失败达到阈值时禁用节点，
// 使其从 active 转为 disabled（管理界面可见、可显式恢复），而不是停留在
// “active 但被选路/健康检查静默排除”的僵尸态。见 BUG-53。
// p.FailCount 是本次选路时读取的旧值，本次失败后有效计数为 p.FailCount+1，
// 与健康检查路径 (health_checker.go) 的判断口径一致。
func recordProxyFailure(store proxyStore, p *storage.Proxy) {
	store.RecordProxyUseByID(p.ID, false)
	if p.FailCount+1 >= failDisableThreshold {
		store.DisableProxyByID(p.ID)
	}
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
	for attempt := 0; attempt <= s.cfg.MaxRetry; attempt++ {
		p, err := s.selectProxy(route, tried)
		if err != nil {
			http.Error(w, proxySelectionError(route, err), http.StatusServiceUnavailable)
			return
		}

		tried = append(tried, p.ID)

		client, err := s.buildClient(p)
		if err != nil {
			recordProxyFailure(s.storage, p)
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
			log.Printf("[proxy] %s via %s failed", r.RequestURI, p.Address)
			recordProxyFailure(s.storage, p)
			if !replayable {
				// body 已在本次尝试中被消费，不能重放，直接失败。
				http.Error(w, "all proxies failed", http.StatusBadGateway)
				return
			}
			continue
		}
		defer resp.Body.Close()

		// 写回响应
		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		s.storage.RecordProxyUseByID(p.ID, true)
		if resp.StatusCode == 429 {
			log.Printf("[proxy] ⚠️  429 %s via %s (protocol=%s)", r.RequestURI, p.Address, p.Protocol)
		} else {
			log.Printf("[proxy] %s via %s -> %d", r.RequestURI, p.Address, resp.StatusCode)
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

	client := &http.Client{Timeout: time.Duration(s.cfg.ValidateTimeout) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[proxy] %s direct (bypass upstream) failed: %v", r.RequestURI, err)
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
	log.Printf("[proxy] %s direct (bypass upstream) -> %d", r.RequestURI, resp.StatusCode)
}

// handleTunnel 处理 HTTPS CONNECT 隧道（带自动重试）
func (s *Server) handleTunnel(w http.ResponseWriter, r *http.Request, route auth.ParsedUsername) {
	// 内网/本地目标直连，不经上游节点（等同浏览器代理例外 / NO_PROXY）。
	if isBypassTarget(r.Host) {
		s.tunnelDirect(w, r)
		return
	}

	var tried []int64
	for attempt := 0; attempt <= s.cfg.MaxRetry; attempt++ {
		p, err := s.selectProxy(route, tried)
		if err != nil {
			http.Error(w, proxySelectionError(route, err), http.StatusServiceUnavailable)
			return
		}

		tried = append(tried, p.ID)

		conn, err := s.dialViaProxy(p, r.Host)
		if err != nil {
			log.Printf("[tunnel] dial %s via %s failed", r.Host, p.Address)
			recordProxyFailure(s.storage, p)
			continue
		}

		s.storage.RecordProxyUseByID(p.ID, true)

		// 告知客户端隧道建立
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			conn.Close()
			http.Error(w, "hijack not supported", http.StatusInternalServerError)
			return
		}
		clientConn, _, err := hijacker.Hijack()
		if err != nil {
			conn.Close()
			return
		}

		fmt.Fprintf(clientConn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		log.Printf("[tunnel] %s via %s established", r.Host, p.Address)

		// 双向转发
		go transfer(conn, clientConn)
		go transfer(clientConn, conn)
		return
	}

	http.Error(w, "all proxies failed", http.StatusBadGateway)
}

// tunnelDirect 为内网/本地 CONNECT 目标建立直连隧道，不经上游节点。
func (s *Server) tunnelDirect(w http.ResponseWriter, r *http.Request) {
	timeout := time.Duration(s.cfg.ValidateTimeout) * time.Second
	conn, err := net.DialTimeout("tcp", r.Host, timeout)
	if err != nil {
		log.Printf("[tunnel] direct dial %s failed: %v", r.Host, err)
		http.Error(w, "direct dial failed", http.StatusBadGateway)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		conn.Close()
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		conn.Close()
		return
	}
	fmt.Fprintf(clientConn, "HTTP/1.1 200 Connection Established\r\n\r\n")
	log.Printf("[tunnel] %s direct (bypass upstream)", r.Host)
	go transfer(conn, clientConn)
	go transfer(clientConn, conn)
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

func (s *Server) dialViaProxy(p *storage.Proxy, host string) (net.Conn, error) {
	timeout := time.Duration(s.cfg.ValidateTimeout) * time.Second
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
		// 发送 CONNECT 请求给上游 HTTP 代理
		fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", host, host)
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
		dialer, err := proxy.SOCKS5("tcp", p.Address, nil, proxy.Direct)
		if err != nil {
			return nil, err
		}
		return dialer.Dial("tcp", host)
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
	timeout := time.Duration(s.cfg.ValidateTimeout) * time.Second
	switch p.Protocol {
	case "http":
		proxyURL, err := url.Parse(fmt.Sprintf("http://%s", p.Address))
		if err != nil {
			return nil, err
		}
		return &http.Client{
			Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
			Timeout:   timeout,
		}, nil
	case "socks5":
		dialer, err := proxy.SOCKS5("tcp", p.Address, nil, proxy.Direct)
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

func transfer(dst io.WriteCloser, src io.ReadCloser) {
	defer dst.Close()
	defer src.Close()
	io.Copy(dst, src)
}
