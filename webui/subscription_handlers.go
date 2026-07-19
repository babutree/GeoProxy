package webui

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/custom"
	"github.com/babutree/GeoProxy/storage"
)

const maxSubscriptionFileContentBytes = 1 << 20

type subscriptionFile interface {
	Name() string
	Chmod(os.FileMode) error
	WriteString(string) (int, error)
	Close() error
}

func writeSubscriptionFile(file subscriptionFile, content string) error {
	var operationErr error
	if err := file.Chmod(0644); err != nil {
		operationErr = err
	} else if _, err := file.WriteString(content); err != nil {
		operationErr = err
	}
	if err := file.Close(); operationErr == nil {
		operationErr = err
	}
	if operationErr != nil {
		_ = os.Remove(file.Name())
	}
	return operationErr
}

// apiSubscriptions 获取订阅列表（含每个订阅的可用/不可用代理数）
func (s *Server) apiSubscriptions(w http.ResponseWriter, r *http.Request) {
	subs, err := s.storage.GetSubscriptions()
	if err != nil {
		log.Printf("[webui] 获取订阅列表失败: %v", err)
		jsonError(w, "failed to list subscriptions", http.StatusInternalServerError)
		return
	}
	if subs == nil {
		subs = []storage.Subscription{}
	}

	// 附加每个订阅的代理统计
	type subWithStats struct {
		storage.Subscription
		ActiveCount   int `json:"active_count"`
		DisabledCount int `json:"disabled_count"`
		PausedCount   int `json:"paused_count"`
	}
	var result []subWithStats
	for _, sub := range subs {
		active, disabled, err := s.storage.CountBySubscriptionID(sub.ID)
		if err != nil {
			log.Printf("[webui] 统计订阅 #%d 节点失败: %v", sub.ID, err)
			jsonError(w, "failed to list subscriptions", http.StatusInternalServerError)
			return
		}
		// 节点级用户暂停计数(user_paused=1)。出错时记 log 并返回错误，不吞错返回假 0，
		// 否则前端会把 paused_count=0 当真值显示，掩盖统计失败。
		paused, err := s.storage.CountPausedBySubscriptionID(sub.ID)
		if err != nil {
			log.Printf("[webui] 统计订阅 #%d 已暂停节点失败: %v", sub.ID, err)
			jsonError(w, "failed to list subscriptions", http.StatusInternalServerError)
			return
		}
		result = append(result, subWithStats{
			Subscription:  sub,
			ActiveCount:   active,
			DisabledCount: disabled,
			PausedCount:   paused,
		})
	}
	jsonOK(w, result)
}

// apiCustomStatus 获取订阅代理状态
func (s *Server) apiCustomStatus(w http.ResponseWriter, r *http.Request) {
	if s.customMgr == nil {
		jsonOK(w, map[string]interface{}{
			"singbox_running":    false,
			"singbox_nodes":      0,
			"subscription_count": 0,
			"disabled_count":     0,
			"subscription_total": 0,
		})
		return
	}
	jsonOK(w, s.customMgr.GetStatus())
}

// apiSubscriptionAdd 添加订阅
func (s *Server) apiSubscriptionAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Name        string `json:"name"`
		URL         string `json:"url"`
		FileContent string `json:"file_content"` // 粘贴的原始订阅内容；解析器会自动识别 YAML、协议链接、Base64 或纯文本。
		RefreshMin  int    `json:"refresh_min"`
		Headers     string `json:"headers"` // 自定义请求头 JSON 对象字符串，如 {"User-Agent":"clash.meta"}；空则用默认 UA。
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonDecodeError(w, err)
		return
	}
	if len(req.FileContent) > maxSubscriptionFileContentBytes {
		jsonError(w, "file_content too large", http.StatusRequestEntityTooLarge)
		return
	}
	if req.URL == "" && req.FileContent == "" {
		jsonError(w, "请填写订阅 URL 或上传配置文件", http.StatusBadRequest)
		return
	}
	if err := custom.ValidateSubscriptionHeaders(req.Headers); err != nil {
		jsonError(w, "invalid headers", http.StatusBadRequest)
		return
	}
	if req.RefreshMin <= 0 {
		req.RefreshMin = config.Get().CustomRefreshInterval
	}
	if req.Name == "" {
		req.Name = "订阅"
	}

	// 如果上传了文件内容，保存到本地
	filePath := ""
	if req.FileContent != "" {
		dataDir, err := config.DataDir()
		if err != nil {
			log.Printf("[webui] 解析订阅数据目录失败: %v", err)
			jsonError(w, "failed to resolve data directory", http.StatusInternalServerError)
			return
		}
		subDir := filepath.Join(dataDir, "subscriptions")
		if err := os.MkdirAll(subDir, 0755); err != nil {
			log.Printf("[webui] 创建订阅目录失败: %v", err)
			jsonError(w, "failed to save subscription file", http.StatusInternalServerError)
			return
		}
		file, err := os.CreateTemp(subDir, "sub_*.yaml")
		if err != nil {
			log.Printf("[webui] 创建订阅文件失败: %v", err)
			jsonError(w, "failed to save subscription file", http.StatusInternalServerError)
			return
		}
		filePath = file.Name()
		if err := writeSubscriptionFile(file, req.FileContent); err != nil {
			log.Printf("[webui] 保存订阅文件失败: %v", err)
			jsonError(w, "failed to save subscription file", http.StatusInternalServerError)
			return
		}
		filePath, err = filepath.Abs(filePath)
		if err != nil {
			_ = os.Remove(filePath)
			log.Printf("[webui] 解析订阅文件路径失败: %v", err)
			jsonError(w, "failed to save subscription file", http.StatusInternalServerError)
			return
		}
	}

	// 先验证：拉取并解析，确认能解析出节点后再入库
	if s.customMgr != nil {
		nodeCount, err := s.customMgr.ValidateSubscription(req.URL, filePath, req.Headers)
		if err != nil {
			// 清理已保存的文件
			if filePath != "" {
				os.Remove(filePath)
			}
			log.Printf("[webui] 订阅验证失败: %v", err)
			jsonError(w, "subscription validation failed", http.StatusBadRequest)
			return
		}
		log.Printf("[webui] 订阅验证通过: %s (%d 个节点)", req.Name, nodeCount)
	}

	id, err := s.storage.AddSubscription(req.Name, req.URL, filePath, "auto", req.RefreshMin, req.Headers)
	if err != nil {
		if filePath != "" {
			_ = os.Remove(filePath)
		}
		log.Printf("[webui] 添加订阅失败: %v", err)
		jsonError(w, "failed to add subscription", http.StatusInternalServerError)
		return
	}

	// 验证已通过，异步执行入池
	if s.customMgr != nil {
		go func() {
			if err := s.customMgr.RefreshSubscription(id); err != nil {
				log.Printf("[webui] 订阅刷新失败: %v", err)
			}
		}()
	}

	log.Printf("[webui] 添加订阅: %s (url=%v file=%v)", req.Name, req.URL != "", filePath != "")
	jsonOK(w, map[string]interface{}{"status": "added", "id": id})
}

// apiSubscriptionDelete 删除订阅
func (s *Server) apiSubscriptionDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID int64 `json:"id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonDecodeError(w, err)
		return
	}
	if req.ID <= 0 {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	if s.customMgr != nil {
		if err := s.customMgr.DeleteSubscription(req.ID); err != nil {
			log.Printf("[webui] 通过 Manager 删除订阅 #%d 失败: %v", req.ID, err)
			jsonError(w, "failed to delete subscription", http.StatusInternalServerError)
			return
		}
		log.Printf("[webui] 删除订阅 #%d", req.ID)
		jsonOK(w, map[string]string{"status": "deleted"})
		return
	}

	// custom manager 缺失的测试或最小服务器使用此回退；仅删除数据库记录无法更新 sing-box 运行态。
	if err := s.storage.DeleteSubscription(req.ID); err != nil {
		log.Printf("[webui] 删除订阅 #%d 失败: %v", req.ID, err)
		jsonError(w, "failed to delete subscription", http.StatusInternalServerError)
		return
	}

	log.Printf("[webui] 删除订阅 #%d", req.ID)
	jsonOK(w, map[string]string{"status": "deleted"})
}

// apiSubscriptionRefresh 刷新单个订阅
func (s *Server) apiSubscriptionRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID int64 `json:"id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonDecodeError(w, err)
		return
	}
	if req.ID <= 0 {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	if s.customMgr != nil {
		go func() {
			if err := s.customMgr.RefreshSubscription(req.ID); err != nil {
				log.Printf("[webui] 订阅 #%d 刷新失败: %v", req.ID, err)
			}
		}()
	}

	jsonOK(w, map[string]string{"status": "refresh started"})
}

// apiSubscriptionRefreshAll 刷新所有订阅
func (s *Server) apiSubscriptionRefreshAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.customMgr != nil {
		go s.customMgr.RefreshAll()
	}

	jsonOK(w, map[string]string{"status": "refresh all started"})
}

// apiSubscriptionToggle 切换订阅状态
func (s *Server) apiSubscriptionToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID int64 `json:"id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonDecodeError(w, err)
		return
	}
	if req.ID <= 0 {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	status, err := s.storage.ToggleSubscription(req.ID)
	if err != nil {
		log.Printf("[webui] 切换订阅 #%d 状态失败: %v", req.ID, err)
		jsonError(w, "failed to toggle subscription", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]string{"status": status})
}

// apiSubscriptionUpdate 修改订阅元数据（名称/URL/刷新间隔/请求头）。
// 保留既有 file_path 与 format，不改动已入库的节点。修改后异步重新拉取。
func (s *Server) apiSubscriptionUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		URL        string `json:"url"`
		RefreshMin int    `json:"refresh_min"`
		Headers    string `json:"headers"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonDecodeError(w, err)
		return
	}
	if req.ID <= 0 {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := custom.ValidateSubscriptionHeaders(req.Headers); err != nil {
		jsonError(w, "invalid headers", http.StatusBadRequest)
		return
	}

	existing, err := s.storage.GetSubscription(req.ID)
	if err != nil {
		log.Printf("[webui] 加载订阅 #%d 失败: %v", req.ID, err)
		jsonError(w, "subscription not found", http.StatusNotFound)
		return
	}
	// URL 订阅须保留 URL；文件订阅保留原 file_path，不允许把两者都清空。
	if req.URL == "" && existing.FilePath == "" {
		jsonError(w, "请填写订阅 URL", http.StatusBadRequest)
		return
	}
	if req.RefreshMin <= 0 {
		req.RefreshMin = existing.RefreshMin
	}
	if req.Name == "" {
		req.Name = existing.Name
	}

	if err := s.storage.UpdateSubscription(req.ID, req.Name, req.URL, existing.FilePath, existing.Format, req.RefreshMin, req.Headers); err != nil {
		log.Printf("[webui] 更新订阅 #%d 失败: %v", req.ID, err)
		jsonError(w, "failed to update subscription", http.StatusInternalServerError)
		return
	}

	// 元数据变更后异步重新拉取验证。
	if s.customMgr != nil {
		go func() {
			if err := s.customMgr.RefreshSubscription(req.ID); err != nil {
				log.Printf("[webui] 订阅刷新失败: %v", err)
			}
		}()
	}

	log.Printf("[webui] 修改订阅 #%d", req.ID)
	jsonOK(w, map[string]string{"status": "updated"})
}
