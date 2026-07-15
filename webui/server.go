package webui

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
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

	"goproxy/affinity"
	"goproxy/config"
	"goproxy/custom"
	"goproxy/storage"
)

// 简单内存 session
var (
	sessions   = make(map[string]time.Time)
	sessionsMu sync.Mutex

	loginAttempts   = make(map[string]loginAttempt)
	loginAttemptsMu sync.Mutex
)

const (
	sessionTokenBytes = 32
	sessionTTL        = 24 * time.Hour
	maxLoginFailures  = 5
	loginLockout      = time.Minute
	maxLoginBody      = 8 << 10
	maxAPIRequestBody = 2 << 20
)

type loginAttempt struct {
	Failures    int
	LockedUntil time.Time
	LastFailure time.Time
}

func newSession() string {
	raw := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		panic(fmt.Sprintf("webui: generate session token: %v", err))
	}
	token := hex.EncodeToString(raw)
	sessionsMu.Lock()
	pruneExpiredSessionsLocked(time.Now())
	sessions[token] = time.Now().Add(sessionTTL)
	sessionsMu.Unlock()
	return token
}

func pruneExpiredSessionsLocked(now time.Time) {
	for token, expiry := range sessions {
		if !expiry.After(now) {
			delete(sessions, token)
		}
	}
}

func validSession(r *http.Request) bool {
	cookie, err := r.Cookie("session")
	if err != nil {
		return false
	}
	sessionsMu.Lock()
	expiry, ok := sessions[cookie.Value]
	sessionsMu.Unlock()
	return ok && time.Now().Before(expiry)
}

func cookieSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func sessionCookie(r *http.Request, token string) *http.Cookie {
	return &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(sessionTTL),
		HttpOnly: true,
		Secure:   cookieSecure(r),
		SameSite: http.SameSiteLaxMode,
	}
}

func clearSessionCookie(r *http.Request) *http.Cookie {
	return &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   cookieSecure(r),
		SameSite: http.SameSiteLaxMode,
	}
}

// loginClientKey 计算登录限速的客户端标识键。
//
// 口径与 cookieSecure 一致：信任前置反向代理设置的 XFF 头。当存在 X-Forwarded-For 时，
// 取其第一段（最靠近客户端的地址）作为限速键，否则回退到 r.RemoteAddr 的 host。
// 这样部署在反代之后时，不会因所有请求的 RemoteAddr 都是反代地址而把全网用户锁进同一个桶。
//
// 安全说明：XFF 可被客户端伪造。但这里是登录“限速”而非“鉴权”——伪造 XFF 只会让攻击者
// 被独立计数到一个自己控制的键上，无法借此绕过针对真实来源的锁定，也无法冒充他人身份。
// 若未部署可信反代却直接暴露，攻击者可通过轮换伪造 XFF 规避限速；此为已知权衡，需靠前置
// 可信反代正确设置 XFF 来保证。
func loginClientKey(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// XFF 形如 "client, proxy1, proxy2"；第一段是最原始的客户端地址。
		first := xff
		if idx := strings.IndexByte(xff, ','); idx >= 0 {
			first = xff[:idx]
		}
		if first = strings.TrimSpace(first); first != "" {
			return first
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || host == "" {
		return r.RemoteAddr
	}
	return host
}

func loginRateLimited(r *http.Request, now time.Time) bool {
	key := loginClientKey(r)
	loginAttemptsMu.Lock()
	attempt := loginAttempts[key]
	loginAttemptsMu.Unlock()
	return attempt.LockedUntil.After(now)
}

func recordLoginFailure(r *http.Request, now time.Time) {
	key := loginClientKey(r)
	loginAttemptsMu.Lock()
	defer loginAttemptsMu.Unlock()
	pruneStaleLoginAttemptsLocked(now)
	attempt := loginAttempts[key]
	if now.Sub(attempt.LastFailure) > loginLockout {
		attempt.Failures = 0
	}
	attempt.Failures++
	attempt.LastFailure = now
	if attempt.Failures >= maxLoginFailures {
		attempt.LockedUntil = now.Add(loginLockout)
	}
	loginAttempts[key] = attempt
}

func pruneStaleLoginAttemptsLocked(now time.Time) {
	for key, attempt := range loginAttempts {
		if !attempt.LockedUntil.After(now) && now.Sub(attempt.LastFailure) > loginLockout {
			delete(loginAttempts, key)
		}
	}
}

func recordLoginSuccess(r *http.Request) {
	key := loginClientKey(r)
	loginAttemptsMu.Lock()
	delete(loginAttempts, key)
	loginAttemptsMu.Unlock()
}

type Server struct {
	storage       *storage.Storage
	cfg           *config.Config
	cfgMu         sync.RWMutex
	affinity      *affinity.Store
	customMgr     *custom.Manager
	configChanged chan<- struct{}
	apiKeyLimiter *apiKeyRateLimiter
}

// configSnapshot 返回已发布的不可变配置快照。
// 所有运行态配置更新在 cfgMu 内以替换指针的方式完成，读者持有快照后无需继续持锁。
func (s *Server) configSnapshot() *config.Config {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg
}

func New(s *storage.Storage, cfg *config.Config, affinityStore *affinity.Store, cm *custom.Manager, cc chan<- struct{}) *Server {
	rate := 60
	if cfg != nil && cfg.ReadOnlyAPIRatePerMin > 0 {
		rate = cfg.ReadOnlyAPIRatePerMin
	}
	return &Server{
		storage:       s,
		cfg:           cfg,
		affinity:      affinityStore,
		customMgr:     cm,
		configChanged: cc,
		apiKeyLimiter: newAPIKeyRateLimiter(rate, time.Now),
	}
}

func (s *Server) Start() {
	mux := s.routes()
	cfg := s.configSnapshot()

	// 添加日志中间件；跳过前端高频轮询端点与容器健康探活，避免访问日志自我膨胀刷屏。
	loggedMux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isNoiseRequest(r) {
			log.Printf("[webui] %s %s | RemoteAddr: %s", r.Method, r.URL.Path, r.RemoteAddr)
		}
		mux.ServeHTTP(w, r)
	})

	log.Printf("WebUI listening on %s", cfg.WebUIPort)
	go func() {
		if err := http.ListenAndServe(cfg.WebUIPort, loggedMux); err != nil {
			log.Fatalf("webui: %v", err)
		}
	}()
}

// isNoiseRequest 判断请求是否为不值得记录的噪音：
//   - 前端定时轮询端点（/api/logs、/api/stats、/api/sessions）
//   - 来自本机回环地址的健康探活（docker healthcheck 每 30s 请求 GET /）
func isNoiseRequest(r *http.Request) bool {
	switch r.URL.Path {
	case "/api/logs", "/api/stats", "/api/sessions", "/api/proxy-occupancy":
		return true
	}
	// docker healthcheck: GET / from loopback。真人从其它地址访问 / 仍会记录。
	if r.URL.Path == "/" && r.Method == http.MethodGet && isLoopbackRemote(r.RemoteAddr) {
		return true
	}
	return false
}

// isLoopbackRemote 判断 RemoteAddr 是否来自回环地址（127.0.0.1 / ::1）。
func isLoopbackRemote(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.authMiddleware(s.handleLogout))

	// 静态资源：dashboard 的 CSS/JS 从 HTML 合规分离后由这两个路由下发。
	// 基于内容 sha256 的 ETag，支持 If-None-Match 命中返回 304；带 Cache-Control。
	mux.HandleFunc("/assets/dashboard.css", s.handleAssetCSS)
	mux.HandleFunc("/assets/dashboard.js", s.handleAssetJS)

	// Public API: only authentication state, with no business data.
	mux.HandleFunc("/api/auth/check", s.apiAuthCheck)
	mux.HandleFunc("/api/public-ip", s.authMiddleware(s.apiPublicIP))

	// Business APIs require login. There is no guest/read-only role.
	mux.HandleFunc("/api/stats", s.authMiddleware(s.apiStats))
	mux.HandleFunc("/api/proxies", s.authMiddleware(s.apiProxies))
	mux.HandleFunc("/api/logs", s.authMiddleware(s.apiLogs))
	mux.HandleFunc("/api/config", s.authMiddleware(s.apiConfig))
	mux.HandleFunc("/api/sessions", s.authMiddleware(s.apiSessions))
	mux.HandleFunc("/api/proxy-occupancy", s.authMiddleware(s.apiProxyOccupancy))
	mux.HandleFunc("/api/proxy/delete", s.authMiddleware(s.apiDeleteProxy))
	mux.HandleFunc("/api/proxy/toggle", s.authMiddleware(s.apiToggleProxy))
	mux.HandleFunc("/api/proxy/refresh", s.authMiddleware(s.apiRefreshProxy))
	mux.HandleFunc("/api/proxy/star", s.authMiddleware(s.apiStarProxy))
	mux.HandleFunc("/api/refresh-latency", s.authMiddleware(s.apiRefreshLatency))
	mux.HandleFunc("/api/config/save", s.authMiddleware(s.apiConfigSave))

	// 订阅管理 API
	mux.HandleFunc("/api/subscriptions", s.authMiddleware(s.apiSubscriptions))
	mux.HandleFunc("/api/custom/status", s.authMiddleware(s.apiCustomStatus))
	mux.HandleFunc("/api/subscription/add", s.authMiddleware(s.apiSubscriptionAdd))
	mux.HandleFunc("/api/subscription/delete", s.authMiddleware(s.apiSubscriptionDelete))
	mux.HandleFunc("/api/subscription/refresh", s.authMiddleware(s.apiSubscriptionRefresh))
	mux.HandleFunc("/api/subscription/refresh-all", s.authMiddleware(s.apiSubscriptionRefreshAll))
	mux.HandleFunc("/api/subscription/toggle", s.authMiddleware(s.apiSubscriptionToggle))
	mux.HandleFunc("/api/manual-node/add", s.authMiddleware(s.apiManualNodeAdd))
	mux.HandleFunc("/api/manual-node/import", s.authMiddleware(s.apiManualNodeImport))
	mux.HandleFunc("/api/manual-node/batch-delete", s.authMiddleware(s.apiManualNodeBatchDelete))
	mux.HandleFunc("/api/manual-node/region", s.authMiddleware(s.apiManualNodeRegion))
	mux.HandleFunc("/api/manual-node/note", s.authMiddleware(s.apiManualNodeNote))
	mux.HandleFunc("/api/manual-node/delete", s.authMiddleware(s.apiManualNodeDelete))

	// API Key management (session + CSRF only; never accept API Key for privilege escalation).
	mux.HandleFunc("/api/apikeys", s.authMiddleware(s.apiAPIKeysList))
	mux.HandleFunc("/api/apikey/create", s.authMiddleware(s.apiAPIKeyCreate))
	mux.HandleFunc("/api/apikey/revoke", s.authMiddleware(s.apiAPIKeyRevoke))
	mux.HandleFunc("/api/apikey/delete", s.authMiddleware(s.apiAPIKeyDelete))

	// Read-only external API (API Key auth).
	mux.HandleFunc("/api/v1/ping", s.apiKeyMiddleware(s.apiV1Ping))
	mux.HandleFunc("/api/v1/nodes", s.apiKeyMiddleware(s.apiV1Nodes))
	mux.HandleFunc("/api/v1/occupancy", s.apiKeyMiddleware(s.apiV1Occupancy))

	return mux
}

// authMiddleware 管理员权限中间件（必须登录）
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !validSession(r) {
			if len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api" {
				jsonError(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		if len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api" && isUnsafeMethod(r.Method) {
			if !validCSRF(r) {
				jsonError(w, "forbidden", http.StatusForbidden)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxAPIRequestBody)
		}
		next(w, r)
	}
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

func validCSRF(r *http.Request) bool {
	if validCSRFFromHeader(r) {
		return true
	}
	if sameOriginHeader(r.Header.Get("Origin"), r.Host) {
		return true
	}
	return sameOriginHeader(r.Header.Get("Referer"), r.Host)
}

func validCSRFFromHeader(r *http.Request) bool {
	cookie, err := r.Cookie("session")
	if err != nil || cookie.Value == "" {
		return false
	}
	token := r.Header.Get("X-CSRF-Token")
	if token == "" || len(token) != len(cookie.Value) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(cookie.Value)) == 1
}

func sameOriginHeader(raw string, host string) bool {
	if raw == "" || host == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Host == host && (u.Scheme == "http" || u.Scheme == "https")
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if !validSession(r) {
		fmt.Fprint(w, loginHTML)
		return
	}
	fmt.Fprint(w, dashboardHTML)
}

// assetETag 计算内容的强 ETag（基于 sha256，包裹双引号，符合 RFC 7232）。
func assetETag(content string) string {
	sum := sha256.Sum256([]byte(content))
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

// serveAsset 统一下发静态文本资源：正确 Content-Type、基于内容 hash 的 ETag、
// Cache-Control（max-age=3600 + must-revalidate，配合 ETag 保证内容变更即失效），
// 支持 If-None-Match 命中返回 304。仅内嵌 Go 常量，无外链、无独立文件。
func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request, contentType, content string) {
	etag := assetETag(content)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "max-age=3600, must-revalidate")
	if match := r.Header.Get("If-None-Match"); match != "" && etagMatches(match, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	fmt.Fprint(w, content)
}

// etagMatches 判断 If-None-Match 头是否命中给定 ETag（支持逗号分隔的多值与 * 通配）。
func etagMatches(header, etag string) bool {
	header = strings.TrimSpace(header)
	if header == "*" {
		return true
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == etag {
			return true
		}
	}
	return false
}

// handleAssetCSS 下发 dashboard 样式表（text/css）。
func (s *Server) handleAssetCSS(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/assets/dashboard.css" {
		http.NotFound(w, r)
		return
	}
	s.serveAsset(w, r, "text/css; charset=utf-8", dashboardCSS)
}

// handleAssetJS 下发 dashboard 脚本（application/javascript）。
func (s *Server) handleAssetJS(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/assets/dashboard.js" {
		http.NotFound(w, r)
		return
	}
	s.serveAsset(w, r, "application/javascript; charset=utf-8", dashboardJS)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, loginHTML)
		return
	}
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	now := time.Now()
	if loginRateLimited(r, now) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, loginHTMLWithError)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxLoginBody)
	if err := r.ParseForm(); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			jsonError(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	password := r.FormValue("password")
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(password)))
	if hash != s.configSnapshot().WebUIPasswordHash {
		recordLoginFailure(r, now)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, loginHTMLWithError)
		return
	}
	recordLoginSuccess(r)
	token := newSession()
	http.SetCookie(w, sessionCookie(r, token))
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !validCSRF(r) {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}
	if cookie, err := r.Cookie("session"); err == nil {
		sessionsMu.Lock()
		delete(sessions, cookie.Value)
		sessionsMu.Unlock()
	}
	http.SetCookie(w, clearSessionCookie(r))
	http.Redirect(w, r, "/login", http.StatusFound)
}

func decodeJSON(r *http.Request, dst interface{}) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errTrailingJSONValue
	}
	return nil
}

var errTrailingJSONValue = errors.New("trailing JSON value")

func jsonDecodeError(w http.ResponseWriter, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		jsonError(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	jsonError(w, "invalid request", http.StatusBadRequest)
}

func jsonOK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
