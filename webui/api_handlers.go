package webui

import (
	"database/sql"
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
	activeSessions := 0
	if s.affinity != nil {
		activeSessions = s.affinity.Count()
	}
	jsonOK(w, map[string]interface{}{
		"total":              total,
		"http":               httpCount,
		"socks5":             socks5Count,
		"subscription_count": subscriptionCount,
		"active_sessions":    activeSessions,
		"http_port":          s.cfg.HTTPPort,
		"socks5_port":        s.cfg.SOCKS5Port,
		"webui_port":         s.cfg.WebUIPort,
	})
}

func (s *Server) apiProxies(w http.ResponseWriter, r *http.Request) {
	// 返回全部节点（含 disabled/paused），以便前端展示并对停用节点执行启用操作。
	// 协议筛选交由前端处理，避免停用节点从列表消失后无法再启用。
	proxies, err := s.storage.GetAllForAdmin()
	if err != nil {
		log.Printf("[webui] list proxies failed: %v", err)
		jsonError(w, "failed to list proxies", http.StatusInternalServerError)
		return
	}

	// 构建 subscription_id -> name 映射，供前端以订阅名称区分节点来源，
	// 而非笼统地显示 "subscription"。
	nameByID := map[int64]string{}
	if subs, subErr := s.storage.GetSubscriptions(); subErr == nil {
		for _, sub := range subs {
			nameByID[sub.ID] = sub.Name
		}
	}

	type proxyView struct {
		storage.Proxy
		SubscriptionName string `json:"subscription_name"`
	}
	views := make([]proxyView, 0, len(proxies))
	for _, p := range proxies {
		name := ""
		if p.Source == storage.SourceSubscription {
			if n, ok := nameByID[p.SubscriptionID]; ok {
				name = n
			}
		}
		views = append(views, proxyView{Proxy: p, SubscriptionName: name})
	}
	jsonOK(w, views)
}

func (s *Server) apiDeleteProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID      int64  `json:"id"`
		Address string `json:"address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.ID <= 0 && req.Address == "") {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	var err error
	if req.ID > 0 {
		err = s.storage.DeleteProxyByID(req.ID)
	} else {
		err = s.storage.Delete(req.Address)
	}
	if err != nil {
		if err == sql.ErrNoRows {
			jsonOK(w, map[string]string{"status": "deleted"})
			return
		}
		log.Printf("[webui] delete proxy failed: id=%d address=%q err=%v", req.ID, req.Address, err)
		jsonError(w, "failed to delete proxy", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"status": "deleted"})
}

// apiToggleProxy 停用/启用单个节点，供用户手动屏蔽不想使用的节点。
func (s *Server) apiToggleProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID      int64  `json:"id"`
		Address string `json:"address"`
		Enable  bool   `json:"enable"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.ID <= 0 && req.Address == "") {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	// 手动停用用 paused（区别于验证失败的 disabled），启用用 UnpauseProxy 恢复。
	var err error
	if req.ID > 0 && req.Enable {
		err = s.storage.UnpauseProxyByID(req.ID)
	} else if req.ID > 0 {
		err = s.storage.PauseProxyByID(req.ID)
	} else if req.Enable {
		err = s.storage.UnpauseProxy(req.Address)
	} else {
		err = s.storage.PauseProxy(req.Address)
	}
	if err != nil {
		log.Printf("[webui] toggle proxy %q failed: %v", req.Address, err)
		jsonError(w, "failed to toggle proxy", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"status": "toggled"})
}

// apiStarProxy 切换节点星标。加星直接生效；取消星标由前端 confirm 保护。
func (s *Server) apiStarProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID      int64  `json:"id"`
		Address string `json:"address"`
		Starred bool   `json:"starred"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.ID <= 0 && req.Address == "") {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	id := req.ID
	if id <= 0 {
		proxy, err := s.storage.GetProxyByAddress(req.Address)
		if err != nil {
			jsonError(w, "proxy not found", http.StatusNotFound)
			return
		}
		id = proxy.ID
	}
	if err := s.storage.SetProxyStarred(id, req.Starred); err != nil {
		log.Printf("[webui] star proxy failed: id=%d address=%q err=%v", req.ID, req.Address, err)
		jsonError(w, "failed to star proxy", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"status": "starred"})
}

func (s *Server) apiRefreshProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID      int64  `json:"id"`
		Address string `json:"address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.ID <= 0 && req.Address == "") {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	// 从数据库获取代理信息
	var targetProxy *storage.Proxy
	if req.ID > 0 {
		proxy, err := s.storage.GetProxyByID(req.ID)
		if err != nil {
			jsonError(w, "proxy not found", http.StatusNotFound)
			return
		}
		targetProxy = proxy
	} else {
		proxy, err := s.storage.GetProxyByAddress(req.Address)
		if err != nil {
			jsonError(w, "proxy not found", http.StatusNotFound)
			return
		}
		targetProxy = proxy
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
		valid, latency, exitIP, exitLocation, risk := v.ValidateOne(*targetProxy)

		if valid {
			latencyMs := int(latency.Milliseconds())
			// 单节点“测试”成功：若此前因验证失败被 disabled，恢复为 active 重新参与选路。
			// EnableProxyByID 仅对 status='disabled' 生效，且尊重父订阅暂停，不影响 user_paused。
			if targetProxy.Status == "disabled" {
				if err := s.storage.EnableProxyByID(targetProxy.ID); err != nil {
					log.Printf("[webui] re-enable proxy %s after successful test failed: %v", targetProxy.Address, err)
				}
			}
			s.storage.UpdateProxyExitInfo(targetProxy.ID, exitIP, exitLocation, latencyMs, risk.IPAPIIsScore, risk.Flags, risk.CFBlocked)
			log.Printf("[webui] proxy refreshed: %s latency=%dms grade=%s", targetProxy.Address, latencyMs, storage.CalculateQualityGrade(latencyMs))
		} else {
			s.storage.DisableProxyByID(targetProxy.ID)
			log.Printf("[webui] proxy validation failed, disabled: %s", targetProxy.Address)
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
				s.storage.UpdateProxyExitInfo(r.Proxy.ID, r.ExitIP, r.ExitLocation, latencyMs, r.Risk.IPAPIIsScore, r.Risk.Flags, r.Risk.CFBlocked)
				updated++
			} else {
				s.storage.DisableProxyByID(r.Proxy.ID)
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
