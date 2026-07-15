package webui

import (
	"crypto/sha256"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"goproxy/config"
)

// configSave is the persistence seam used by config handlers.
// Production defaults to config.Save; tests may override it to inject failures.
var configSave = config.Save

// apiConfig 获取配置
func (s *Server) apiConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.configSnapshot()

	jsonOK(w, map[string]interface{}{
		"http_port":           cfg.HTTPPort,
		"socks5_port":         cfg.SOCKS5Port,
		"webui_port":          cfg.WebUIPort,
		"proxy_auth_enabled":  cfg.ProxyAuthEnabled,
		"proxy_auth_username": cfg.ProxyAuthUsername,
		// 代理密码明文下发给已认证 WebUI，供前端拼接含密码的完整代理 URL 供一键复制。
		// 登录密码仍只存哈希、绝不下发；此处仅下发代理认证密码。
		"proxy_auth_password":     cfg.ProxyAuthPassword,
		"session_ttl_minutes":     cfg.SessionTTLMinutes,
		"default_region":          cfg.DefaultRegion,
		"health_check_interval":   cfg.HealthIntervalMinutes,
		"max_retry":               cfg.MaxRetry,
		"singbox_path":            cfg.SingBoxPath,
		"blocked_countries":       cfg.BlockedCountries,
		"allowed_countries":       cfg.AllowedCountries,
		"readonly_fields":         []string{"http_port", "socks5_port", "webui_port"},
		"restart_required_fields": []string{"http_port", "socks5_port", "webui_port"},
	})
}

// apiConfigSave 保存配置
func (s *Server) apiConfigSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ProxyAuthEnabled      bool     `json:"proxy_auth_enabled"`
		ProxyAuthUsername     string   `json:"proxy_auth_username"`
		ProxyAuthPassword     string   `json:"proxy_auth_password"`
		SessionTTLMinutes     int      `json:"session_ttl_minutes"`
		DefaultRegion         string   `json:"default_region"`
		HealthIntervalMinutes int      `json:"health_check_interval"`
		MaxRetry              int      `json:"max_retry"`
		SingBoxPath           string   `json:"singbox_path"`
		BlockedCountries      []string `json:"blocked_countries"`
		AllowedCountries      []string `json:"allowed_countries"`
		HTTPPort              string   `json:"http_port"`
		SOCKS5Port            string   `json:"socks5_port"`
		WebUIPort             string   `json:"webui_port"`
	}

	if err := decodeJSON(r, &req); err != nil {
		jsonDecodeError(w, err)
		return
	}

	username := strings.TrimSpace(req.ProxyAuthUsername)
	if username == "" || req.SessionTTLMinutes <= 0 || req.HealthIntervalMinutes <= 0 || req.MaxRetry < 0 || strings.TrimSpace(req.SingBoxPath) == "" {
		jsonError(w, "invalid config", http.StatusBadRequest)
		return
	}

	// 配置保存和 API Key 操作共用同一个临界区，避免旧配置快照覆盖并发的 Key 变更。
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()

	oldCfg := *s.cfg
	if portChanged(req.HTTPPort, oldCfg.HTTPPort) || portChanged(req.SOCKS5Port, oldCfg.SOCKS5Port) || portChanged(req.WebUIPort, oldCfg.WebUIPort) {
		jsonError(w, "port fields are read-only at runtime and require restart", http.StatusBadRequest)
		return
	}
	newCfg := oldCfg
	newCfg.ProxyAuthEnabled = req.ProxyAuthEnabled
	newCfg.ProxyAuthUsername = username
	// 代理密码保留明文到运行态并落盘，供已认证 WebUI 复制含密码的完整代理 URL。
	// 仅当提交了新密码才更新；留空表示不改，沿用旧明文与旧哈希。
	if req.ProxyAuthPassword != "" {
		newCfg.ProxyAuthPassword = req.ProxyAuthPassword
		newCfg.ProxyAuthPasswordHash = fmt.Sprintf("%x", sha256.Sum256([]byte(req.ProxyAuthPassword)))
	}
	newCfg.SessionTTLMinutes = req.SessionTTLMinutes
	newCfg.DefaultRegion = config.NormalizeCountryCode(req.DefaultRegion)
	newCfg.HealthIntervalMinutes = req.HealthIntervalMinutes
	newCfg.MaxRetry = req.MaxRetry
	newCfg.SingBoxPath = strings.TrimSpace(req.SingBoxPath)
	newCfg.BlockedCountries = config.NormalizeCountryCodes(req.BlockedCountries)
	newCfg.AllowedCountries = config.NormalizeCountryCodes(req.AllowedCountries)

	if err := configSave(&newCfg); err != nil {
		log.Printf("[webui] save config failed: %v", err)
		jsonError(w, "failed to save config", http.StatusInternalServerError)
		return
	}
	s.cfg = &newCfg
	s.affinity.SetTTL(time.Duration(newCfg.SessionTTLMinutes) * time.Minute)
	if err := s.applyCountryFilters(&newCfg); err != nil {
		log.Printf("[webui] apply country filters failed: %v", err)
		if rollbackErr := configSave(&oldCfg); rollbackErr != nil {
			// 磁盘/全局无法回滚时，保留已成功落盘的新配置运行态，避免 s.cfg/affinity 与 config.Get()/磁盘分裂。
			log.Printf("[webui] rollback config after country filter failure failed: %v", rollbackErr)
		} else {
			s.cfg = &oldCfg
			s.affinity.SetTTL(time.Duration(oldCfg.SessionTTLMinutes) * time.Minute)
		}
		jsonError(w, "failed to apply country filters", http.StatusInternalServerError)
		return
	}

	// 通知配置变更
	select {
	case s.configChanged <- struct{}{}:
	default:
	}

	log.Printf("[config] 配置已更新: auth_user=%s ttl=%dmin health=%dmin",
		req.ProxyAuthUsername, req.SessionTTLMinutes, req.HealthIntervalMinutes)
	jsonOK(w, map[string]string{"status": "saved"})
}

func portChanged(requested string, current string) bool {
	requested = strings.TrimSpace(requested)
	return requested != "" && requested != current
}

func (s *Server) applyCountryFilters(cfg *config.Config) error {
	if len(cfg.AllowedCountries) > 0 {
		_, err := s.storage.DisableNotAllowedCountries(cfg.AllowedCountries)
		return err
	}
	if len(cfg.BlockedCountries) > 0 {
		_, err := s.storage.DisableBlockedCountries(cfg.BlockedCountries)
		return err
	}
	return nil
}
