package webui

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/logger"
	"github.com/babutree/GeoProxy/storage"
	"github.com/babutree/GeoProxy/validator"
)

// apiAuthCheck 检查当前用户是否为管理员
func (s *Server) apiAuthCheck(w http.ResponseWriter, r *http.Request) {
	isAdmin := validSession(r)
	jsonOK(w, map[string]interface{}{
		"isAdmin": isAdmin,
	})
}

func (s *Server) apiStats(w http.ResponseWriter, r *http.Request) {
	total, err := s.storage.CountAll()
	if err != nil {
		log.Printf("[webui] 统计全部节点失败: %v", err)
		jsonError(w, "failed to load stats", http.StatusInternalServerError)
		return
	}
	httpCount, err := s.storage.CountAvailableByProtocol("http")
	if err != nil {
		log.Printf("[webui] 统计 HTTP 可用节点失败: %v", err)
		jsonError(w, "failed to load stats", http.StatusInternalServerError)
		return
	}
	socks5Count, err := s.storage.CountAvailableByProtocol("socks5")
	if err != nil {
		log.Printf("[webui] 统计 SOCKS5 可用节点失败: %v", err)
		jsonError(w, "failed to load stats", http.StatusInternalServerError)
		return
	}
	subscriptionCount, err := s.storage.CountBySource(storage.SourceSubscription)
	if err != nil {
		log.Printf("[webui] 按来源统计节点失败: %v", err)
		jsonError(w, "failed to load stats", http.StatusInternalServerError)
		return
	}
	activeSessions := 0
	if s.affinity != nil {
		activeSessions = s.affinity.Count()
	}
	cfg := s.configSnapshot()
	jsonOK(w, map[string]interface{}{
		"total":              total,
		"http":               httpCount,
		"socks5":             socks5Count,
		"subscription_count": subscriptionCount,
		"active_sessions":    activeSessions,
		"http_port":          cfg.HTTPPort,
		"socks5_port":        cfg.SOCKS5Port,
		"webui_port":         cfg.WebUIPort,
	})
}

func (s *Server) apiProxies(w http.ResponseWriter, r *http.Request) {
	// 返回全部节点（含 disabled/paused），以便前端展示并对停用节点执行启用操作。
	// 协议筛选交由前端处理，避免停用节点从列表消失后无法再启用。
	proxies, err := s.storage.GetAllForAdmin()
	if err != nil {
		log.Printf("[webui] 获取节点列表失败: %v", err)
		jsonError(w, "failed to list proxies", http.StatusInternalServerError)
		return
	}

	// 构建 subscription_id -> name 映射，供前端以订阅名称区分节点来源，
	// 而非笼统地显示 "subscription"。
	nameByID := map[int64]string{}
	subs, subErr := s.storage.GetSubscriptions()
	if subErr != nil {
		log.Printf("[webui] 获取节点订阅名称失败: %v", subErr)
		jsonError(w, "failed to list proxies", http.StatusInternalServerError)
		return
	}
	for _, sub := range subs {
		nameByID[sub.ID] = sub.Name
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
	if err := decodeJSON(r, &req); err != nil {
		jsonDecodeError(w, err)
		return
	}
	if req.ID <= 0 && req.Address == "" {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	// 解析节点身份，确保删除手工隧道节点时通过 Manager 同步运行态和数据库。
	var proxy *storage.Proxy
	var lookupErr error
	if req.ID > 0 {
		proxy, lookupErr = s.storage.GetProxyByID(req.ID)
	} else {
		proxy, lookupErr = s.storage.GetProxyByAddress(req.Address)
	}
	if lookupErr != nil {
		// 找不到节点身份时视为已删除，使删除操作保持幂等。
		if lookupErr == sql.ErrNoRows || strings.Contains(lookupErr.Error(), "not found") {
			jsonOK(w, map[string]string{"status": "deleted"})
			return
		}
		if errors.Is(lookupErr, storage.ErrAmbiguousProxyAddress) || strings.Contains(lookupErr.Error(), "ambiguous") {
			jsonError(w, "ambiguous proxy address; use id", http.StatusConflict)
			return
		}
		log.Printf("[webui] 删除节点前查询失败: id=%d address=%q err=%v", req.ID, req.Address, lookupErr)
		jsonError(w, "failed to delete proxy", http.StatusInternalServerError)
		return
	}

	var err error
	// 任意来源删除都必须经 Manager：隧道节点需同步卸载 sing-box 运行态（BUG-06）。
	if s.customMgr != nil {
		err = s.customMgr.DeleteManagedProxy(proxy.ID)
	} else if req.ID > 0 {
		err = s.storage.DeleteProxyByID(req.ID)
	} else {
		err = s.storage.Delete(req.Address)
	}
	if err != nil {
		if err == sql.ErrNoRows {
			jsonOK(w, map[string]string{"status": "deleted"})
			return
		}
		log.Printf("[webui] 删除节点失败: id=%d address=%q err=%v", req.ID, req.Address, err)
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
	if err := decodeJSON(r, &req); err != nil {
		jsonDecodeError(w, err)
		return
	}
	if req.ID <= 0 && req.Address == "" {
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
		log.Printf("[webui] 切换节点 %q 状态失败: %v", req.Address, err)
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
	if err := decodeJSON(r, &req); err != nil {
		jsonDecodeError(w, err)
		return
	}
	if req.ID <= 0 && req.Address == "" {
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
		log.Printf("[webui] 切换节点星标失败: id=%d address=%q err=%v", req.ID, req.Address, err)
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
	if err := decodeJSON(r, &req); err != nil {
		jsonDecodeError(w, err)
		return
	}
	if req.ID <= 0 && req.Address == "" {
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

		log.Printf("[webui] 开始刷新节点: %s", req.Address)
		valid, latency, exitIP, exitLocation, risk := v.ValidateOne(*targetProxy)

		if valid {
			latencyMs := int(latency.Milliseconds())
			// 单节点“测试”成功：若此前因验证失败被 disabled，恢复为 active 重新参与选路。
			// EnableProxyByID 仅对 status='disabled' 生效，且尊重父订阅暂停，不影响 user_paused。
			if targetProxy.Status == "disabled" {
				if err := s.storage.EnableProxyByID(targetProxy.ID); err != nil {
					log.Printf("[webui] 节点测试成功后重新启用失败: %s: %v", targetProxy.Address, err)
				}
			}
			s.storage.UpdateProxyExitInfo(targetProxy.ID, exitIP, exitLocation, latencyMs, risk.IPAPIIsScore, risk.Flags, risk.FlagsKnown, risk.CFBlocked, risk.AIReachability)
			log.Printf("[webui] 节点刷新完成: %s latency=%dms grade=%s", targetProxy.Address, latencyMs, storage.CalculateQualityGrade(latencyMs))
		} else {
			s.storage.DisableProxyByID(targetProxy.ID)
			log.Printf("[webui] 节点验证失败，已禁用: %s", targetProxy.Address)
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
		log.Println("[webui] 开始刷新全部节点延迟...")
		proxies, err := s.storage.GetAll()
		if err != nil {
			log.Printf("[webui] 获取节点失败: %v", err)
			return
		}
		if len(proxies) == 0 {
			log.Println("[webui] 没有需要刷新延迟的节点")
			return
		}

		cfg := config.Get()
		validate := validator.New(cfg.ValidateConcurrency, cfg.ValidateTimeout, cfg.ValidateURL)

		log.Printf("[webui] 正在刷新 %d 个节点的延迟...", len(proxies))
		updated := 0
		for r := range validate.ValidateStream(proxies) {
			if r.Valid {
				latencyMs := int(r.Latency.Milliseconds())
				s.storage.UpdateProxyExitInfo(r.Proxy.ID, r.ExitIP, r.ExitLocation, latencyMs, r.Risk.IPAPIIsScore, r.Risk.Flags, r.Risk.FlagsKnown, r.Risk.CFBlocked, r.Risk.AIReachability)
				updated++
			} else {
				s.storage.DisableProxyByID(r.Proxy.ID)
			}
		}
		log.Printf("[webui] 延迟刷新完成: updated=%d", updated)
	}()
	jsonOK(w, map[string]string{"status": "refresh started"})
}

func (s *Server) apiLogs(w http.ResponseWriter, r *http.Request) {
	lines := logger.GetLines(100)
	jsonOK(w, map[string]interface{}{"lines": lines})
}
