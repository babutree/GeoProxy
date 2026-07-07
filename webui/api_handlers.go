package webui

import (
	"encoding/json"
	"log"
	"net/http"

	"goproxy/config"
	"goproxy/logger"
	"goproxy/storage"
	"goproxy/validator"
)

// apiAuthCheck 检查当前用户是否为管理员
func (s *Server) apiAuthCheck(w http.ResponseWriter, r *http.Request) {
	isAdmin := validSession(r)
	jsonOK(w, map[string]interface{}{
		"isAdmin": isAdmin,
	})
}

func (s *Server) apiStats(w http.ResponseWriter, r *http.Request) {
	total, _ := s.storage.CountAll()
	httpCount, _ := s.storage.CountAvailableByProtocol("http")
	socks5Count, _ := s.storage.CountAvailableByProtocol("socks5")
	subscriptionCount, _ := s.storage.CountBySource(storage.SourceSubscription)
	jsonOK(w, map[string]interface{}{
		"total":              total,
		"http":               httpCount,
		"socks5":             socks5Count,
		"subscription_count": subscriptionCount,
		"http_port":          s.cfg.HTTPPort,
		"socks5_port":        s.cfg.SOCKS5Port,
		"webui_port":         s.cfg.WebUIPort,
	})
}

func (s *Server) apiProxies(w http.ResponseWriter, r *http.Request) {
	protocol := r.URL.Query().Get("protocol")
	var proxies []storage.Proxy
	var err error
	if protocol != "" {
		proxies, err = s.storage.GetByProtocol(protocol)
	} else {
		proxies, err = s.storage.GetAll()
	}
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, proxies)
}

func (s *Server) apiDeleteProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Address string `json:"address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Address == "" {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	s.storage.Delete(req.Address)
	jsonOK(w, map[string]string{"status": "deleted"})
}

func (s *Server) apiRefreshProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Address string `json:"address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Address == "" {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	// 从数据库获取代理信息
	proxies, err := s.storage.GetAll()
	if err != nil {
		jsonError(w, "failed to get proxy", http.StatusInternalServerError)
		return
	}

	var targetProxy *storage.Proxy
	for i := range proxies {
		if proxies[i].Address == req.Address {
			targetProxy = &proxies[i]
			break
		}
	}

	if targetProxy == nil {
		jsonError(w, "proxy not found", http.StatusNotFound)
		return
	}

	// 异步验证并更新
	go func() {
		cfg := config.Get()
		v := validator.New(1, cfg.ValidateTimeout, cfg.ValidateURL)

		log.Printf("[webui] refreshing proxy: %s", req.Address)
		valid, latency, exitIP, exitLocation := v.ValidateOne(*targetProxy)

		if valid {
			latencyMs := int(latency.Milliseconds())
			s.storage.UpdateExitInfo(req.Address, exitIP, exitLocation, latencyMs)
			log.Printf("[webui] proxy refreshed: %s latency=%dms grade=%s", req.Address, latencyMs, storage.CalculateQualityGrade(latencyMs))
		} else {
			s.storage.DisableProxy(req.Address)
			log.Printf("[webui] proxy validation failed, disabled: %s", req.Address)
		}
	}()

	jsonOK(w, map[string]string{"status": "refresh started"})
}

func (s *Server) apiRefreshLatency(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	go func() {
		log.Println("[webui] refreshing latency for all proxies...")
		proxies, err := s.storage.GetAll()
		if err != nil {
			log.Printf("[webui] get proxies error: %v", err)
			return
		}
		if len(proxies) == 0 {
			log.Println("[webui] no proxies to refresh")
			return
		}

		cfg := config.Get()
		validate := validator.New(cfg.ValidateConcurrency, cfg.ValidateTimeout, cfg.ValidateURL)

		log.Printf("[webui] refreshing latency for %d proxies...", len(proxies))
		updated := 0
		for r := range validate.ValidateStream(proxies) {
			if r.Valid {
				latencyMs := int(r.Latency.Milliseconds())
				s.storage.UpdateExitInfo(r.Proxy.Address, r.ExitIP, r.ExitLocation, latencyMs)
				updated++
			} else {
				s.storage.DisableProxy(r.Proxy.Address)
			}
		}
		log.Printf("[webui] latency refresh done: updated=%d", updated)
	}()
	jsonOK(w, map[string]string{"status": "refresh started"})
}

func (s *Server) apiLogs(w http.ResponseWriter, r *http.Request) {
	lines := logger.GetLines(100)
	jsonOK(w, map[string]interface{}{"lines": lines})
}
