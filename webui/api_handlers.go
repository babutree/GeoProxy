package webui

import (
	"context"
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

	// 父订阅状态：统计/选路会排除 paused 与孤儿订阅节点，但管理列表仍返回全部行。
	// 前端必须拿到 subscription_status 才能与顶部「上游节点」可用数对齐。
	statusByID := map[int64]string{}
	for _, sub := range subs {
		statusByID[sub.ID] = sub.Status
	}

	type proxyView struct {
		storage.Proxy
		SubscriptionName   string `json:"subscription_name"`
		SubscriptionStatus string `json:"subscription_status,omitempty"`
	}
	views := make([]proxyView, 0, len(proxies))
	for _, p := range proxies {
		name := ""
		subStatus := ""
		if p.Source == storage.SourceSubscription {
			if n, ok := nameByID[p.SubscriptionID]; ok {
				name = n
			}
			if st, ok := statusByID[p.SubscriptionID]; ok {
				subStatus = st
			}
		}
		views = append(views, proxyView{Proxy: p, SubscriptionName: name, SubscriptionStatus: subStatus})
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
		probeCfg := *cfg
		probeCfg.ValidateConcurrency = 1
		v := validator.NewWithConfig(&probeCfg)

		log.Printf("[webui] 开始刷新节点: %s", req.Address)
		result := v.ValidateOneResult(*targetProxy)
		if err := s.applyProbeResult(result); err != nil {
			log.Printf("[webui] 节点刷新结果写回失败: %s: %v", targetProxy.Address, err)
			return
		}
		if result.Valid {
			log.Printf("[webui] 节点刷新完成: %s latency=%dms grade=%s", targetProxy.Address, result.Latency.Milliseconds(), storage.CalculateQualityGrade(int(result.Latency.Milliseconds())))
		} else {
			log.Printf("[webui] 节点验证失败: %s reason=%s", targetProxy.Address, result.FailureReason)
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
		validate := validator.NewWithConfig(cfg)

		log.Printf("[webui] 正在刷新 %d 个节点的延迟...", len(proxies))
		updated := 0
		// 用户触发的一次性批量刷新，没有比进程更短的生命周期可挂靠；
		// 消费者完整消费 channel，不会让发送方滞留。
		for r := range validate.ValidateStream(context.Background(), proxies) {
			if err := s.applyProbeResult(r); err != nil {
				log.Printf("[webui] 批量刷新结果写回失败: %s: %v", r.Proxy.Address, err)
				continue
			}
			if r.Valid {
				updated++
			}
		}
		log.Printf("[webui] 延迟刷新完成: updated=%d", updated)
	}()
	jsonOK(w, map[string]string{"status": "refresh started"})
}

// applyProbeResult 用完整路由身份原子写回 WebUI 发起的验证结果。
// 结果滞后于节点重绑时不能按旧 ID 覆盖新凭据；地域策略拒绝保留可信出口但不累积系统故障。
func (s *Server) applyProbeResult(result validator.Result) error {
	identity := storage.RouteIdentityFromProxy(result.Proxy)
	observation := storage.ExitObservation{
		ExitIP: result.ExitIP, ExitLocation: result.ExitLocation, LatencyMS: int(result.Latency.Milliseconds()),
		IPAPIIsScore: result.Risk.IPAPIIsScore, IPAPIFlags: result.Risk.Flags,
		IPAPIFlagsKnown: result.Risk.FlagsKnown, CFBlocked: result.Risk.CFBlocked,
		AIReachability: result.Risk.AIReachability,
	}
	if !result.Valid {
		if result.FailureReason == validator.FailureGeoRejected {
			if err := s.storage.DisableRouteForPolicy(identity); err != nil {
				return err
			}
			return s.storage.ApplyProbeObservation(identity, observation)
		}
		if result.Proxy.Status == "disabled" {
			return s.storage.RecordDisabledProbeFailure(identity, observation)
		}
		_, err := s.storage.RecordProbeFailure(identity, observation, 1)
		return err
	}
	if err := s.storage.RecoverProxyFromProbe(identity, observation); err == nil {
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return s.storage.ApplyProbeObservation(identity, observation)
}

func (s *Server) apiLogs(w http.ResponseWriter, r *http.Request) {
	// 与 logger 环形缓冲容量对齐，避免自动滚动只能看到末尾 100 行。
	lines := logger.GetLines(500)
	jsonOK(w, map[string]interface{}{"lines": lines})
}
