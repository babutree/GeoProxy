package webui

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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
)

func newSession() string {
	token := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano()))))
	sessionsMu.Lock()
	sessions[token] = time.Now().Add(24 * time.Hour)
	sessionsMu.Unlock()
	return token
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

type Server struct {
	storage       *storage.Storage
	cfg           *config.Config
	affinity      *affinity.Store
	customMgr     *custom.Manager
	configChanged chan<- struct{}
}

func New(s *storage.Storage, cfg *config.Config, affinityStore *affinity.Store, cm *custom.Manager, cc chan<- struct{}) *Server {
	return &Server{
		storage:       s,
		cfg:           cfg,
		affinity:      affinityStore,
		customMgr:     cm,
		configChanged: cc,
	}
}

func (s *Server) Start() {
	mux := s.routes()

	// 添加日志中间件
	loggedMux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[webui] %s %s | Host: %s | RemoteAddr: %s",
			r.Method, r.URL.Path, r.Host, r.RemoteAddr)
		mux.ServeHTTP(w, r)
	})

	log.Printf("WebUI listening on %s", s.cfg.WebUIPort)
	go func() {
		if err := http.ListenAndServe(s.cfg.WebUIPort, loggedMux); err != nil {
			log.Fatalf("webui: %v", err)
		}
	}()
}

func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.handleLogout)

	// Public API: only authentication state, with no business data.
	mux.HandleFunc("/api/auth/check", s.apiAuthCheck)

	// Business APIs require login. There is no guest/read-only role.
	mux.HandleFunc("/api/stats", s.authMiddleware(s.apiStats))
	mux.HandleFunc("/api/proxies", s.authMiddleware(s.apiProxies))
	mux.HandleFunc("/api/logs", s.authMiddleware(s.apiLogs))
	mux.HandleFunc("/api/config", s.authMiddleware(s.apiConfig))
	mux.HandleFunc("/api/sessions", s.authMiddleware(s.apiSessions))
	mux.HandleFunc("/api/proxy/delete", s.authMiddleware(s.apiDeleteProxy))
	mux.HandleFunc("/api/proxy/refresh", s.authMiddleware(s.apiRefreshProxy))
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
	mux.HandleFunc("/api/manual-node/region", s.authMiddleware(s.apiManualNodeRegion))
	mux.HandleFunc("/api/manual-node/note", s.authMiddleware(s.apiManualNodeNote))
	mux.HandleFunc("/api/manual-node/delete", s.authMiddleware(s.apiManualNodeDelete))

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
		next(w, r)
	}
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

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, loginHTML)
		return
	}
	password := r.FormValue("password")
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(password)))
	if hash != s.cfg.WebUIPasswordHash {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, loginHTMLWithError)
		return
	}
	token := newSession()
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session"); err == nil {
		sessionsMu.Lock()
		delete(sessions, cookie.Value)
		sessionsMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusFound)
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
