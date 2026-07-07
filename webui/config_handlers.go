package webui

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"goproxy/config"
)

// apiConfig 获取配置
func (s *Server) apiConfig(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()

	jsonOK(w, map[string]interface{}{
		"http_port":             cfg.HTTPPort,
		"socks5_port":           cfg.SOCKS5Port,
		"webui_port":            cfg.WebUIPort,
		"proxy_auth_enabled":    cfg.ProxyAuthEnabled,
		"proxy_auth_username":   cfg.ProxyAuthUsername,
		"session_ttl_minutes":   cfg.SessionTTLMinutes,
		"default_region":        cfg.DefaultRegion,
		"health_check_interval": cfg.HealthIntervalMinutes,
		"max_retry":             cfg.MaxRetry,
		"singbox_path":          cfg.SingBoxPath,
		"blocked_countries":     cfg.BlockedCountries,
		"allowed_countries":     cfg.AllowedCountries,
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
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	username := strings.TrimSpace(req.ProxyAuthUsername)
	if username == "" || req.SessionTTLMinutes <= 0 || req.HealthIntervalMinutes <= 0 || req.MaxRetry < 0 || strings.TrimSpace(req.SingBoxPath) == "" {
		jsonError(w, "invalid config", http.StatusBadRequest)
		return
	}

	// 更新配置
	oldCfg := config.Get()
	newCfg := *oldCfg
	newCfg.ProxyAuthEnabled = req.ProxyAuthEnabled
	newCfg.ProxyAuthUsername = username
	if req.ProxyAuthPassword != "" {
		newCfg.ProxyAuthPassword = req.ProxyAuthPassword
		newCfg.ProxyAuthPasswordHash = fmt.Sprintf("%x", sha256.Sum256([]byte(req.ProxyAuthPassword)))
	}
	newCfg.SessionTTLMinutes = req.SessionTTLMinutes
	newCfg.DefaultRegion = strings.ToLower(strings.TrimSpace(req.DefaultRegion))
	newCfg.HealthIntervalMinutes = req.HealthIntervalMinutes
	newCfg.MaxRetry = req.MaxRetry
	newCfg.SingBoxPath = strings.TrimSpace(req.SingBoxPath)
	newCfg.BlockedCountries = req.BlockedCountries
	newCfg.AllowedCountries = req.AllowedCountries

	if err := config.Save(&newCfg); err != nil {
		jsonError(w, "save config error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	*s.cfg = newCfg

	// 通知配置变更
	select {
	case s.configChanged <- struct{}{}:
	default:
	}

	log.Printf("[config] 配置已更新: auth_user=%s ttl=%dmin health=%dmin",
		req.ProxyAuthUsername, req.SessionTTLMinutes, req.HealthIntervalMinutes)
	jsonOK(w, map[string]string{"status": "saved"})
}
