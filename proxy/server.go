package proxy

import (
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
	storage  *storage.Storage
	cfg      *config.Config
	port     string
	sessions *affinity.Store
}

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
	return http.ListenAndServe(s.port, s)
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
	return parsed, auth.VerifyPasswordHash(parsed.Base, password, s.cfg.ProxyAuthUsername, s.cfg.ProxyAuthPasswordHash)
}

func (s *Server) selectProxy(route auth.ParsedUsername, tried []string) (*storage.Proxy, error) {
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

// removeOrDisableProxy 记录上游失败并禁用节点，避免请求失败触发隐式池管理删除。
func removeOrDisableProxy(store *storage.Storage, p *storage.Proxy) {
	store.DisableProxy(p.Address)
}

// handleHTTP 处理普通 HTTP 请求（带自动重试）
func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request, route auth.ParsedUsername) {
	var tried []string
	for attempt := 0; attempt <= s.cfg.MaxRetry; attempt++ {
		p, err := s.selectProxy(route, tried)
		if err != nil {
			http.Error(w, proxySelectionError(route, err), http.StatusServiceUnavailable)
			return
		}

		tried = append(tried, p.Address)

		client, err := s.buildClient(p)
		if err != nil {
			removeOrDisableProxy(s.storage, p)
			continue
		}

		// 转发请求（使用完整 URL，上游代理通过 client transport 设置）
		req, err := http.NewRequest(r.Method, r.URL.String(), r.Body)
		if err != nil {
			continue
		}
		req.Header = r.Header.Clone()
		req.Header.Del("Proxy-Connection")

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("[proxy] %s via %s failed, disabling", r.RequestURI, p.Address)
			s.storage.RecordProxyUse(p.Address, false)
			removeOrDisableProxy(s.storage, p)
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
		s.storage.RecordProxyUse(p.Address, true)
		if resp.StatusCode == 429 {
			log.Printf("[proxy] ⚠️  429 %s via %s (protocol=%s)", r.RequestURI, p.Address, p.Protocol)
		} else {
			log.Printf("[proxy] %s via %s -> %d", r.RequestURI, p.Address, resp.StatusCode)
		}
		return
	}

	http.Error(w, "all proxies failed", http.StatusBadGateway)
}

// handleTunnel 处理 HTTPS CONNECT 隧道（带自动重试）
func (s *Server) handleTunnel(w http.ResponseWriter, r *http.Request, route auth.ParsedUsername) {
	var tried []string
	for attempt := 0; attempt <= s.cfg.MaxRetry; attempt++ {
		p, err := s.selectProxy(route, tried)
		if err != nil {
			http.Error(w, proxySelectionError(route, err), http.StatusServiceUnavailable)
			return
		}

		tried = append(tried, p.Address)

		conn, err := s.dialViaProxy(p, r.Host)
		if err != nil {
			log.Printf("[tunnel] dial %s via %s failed, disabling", r.Host, p.Address)
			s.storage.RecordProxyUse(p.Address, false)
			removeOrDisableProxy(s.storage, p)
			continue
		}

		s.storage.RecordProxyUse(p.Address, true)

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
		// 发送 CONNECT 请求给上游 HTTP 代理
		fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", host, host)
		buf := make([]byte, 256)
		n, err := conn.Read(buf)
		if err != nil {
			conn.Close()
			return nil, err
		}
		if n < 12 {
			conn.Close()
			return nil, fmt.Errorf("short response from proxy")
		}
		return conn, nil
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
