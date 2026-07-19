package webui

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/babutree/GeoProxy/config"
)

type apiKeyPublicView struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	Disabled   bool      `json:"disabled"`
}

func (s *Server) apiAPIKeysList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.cfgMu.RLock()
	keys := append([]config.APIKey(nil), s.cfg.ReadOnlyAPIKeys...)
	s.cfgMu.RUnlock()
	views := make([]apiKeyPublicView, 0, len(keys))
	for _, k := range keys {
		views = append(views, apiKeyPublicView{
			ID:         k.ID,
			Name:       k.Name,
			CreatedAt:  k.CreatedAt,
			LastUsedAt: k.LastUsedAt,
			Disabled:   k.Disabled,
		})
	}
	jsonOK(w, map[string]interface{}{"keys": views})
}

func (s *Server) apiAPIKeyCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonDecodeError(w, err)
		return
	}
	// 生成或保存密钥前先拒绝空名称或纯空白名称。
	// 此处提前返回可确保拒绝路径不会创建任何密钥材料；错误信息使用固定字符串，
	// 绝不回显调用方输入或生成的密钥。
	name := strings.TrimSpace(req.Name)
	if name == "" {
		jsonError(w, "name is required", http.StatusBadRequest)
		return
	}
	plain, err := newAPIKeySecret()
	if err != nil {
		log.Printf("[webui] 生成 API Key 失败: %v", err)
		jsonError(w, "failed to create api key", http.StatusInternalServerError)
		return
	}
	id, err := newAPIKeySecret()
	if err != nil {
		log.Printf("[webui] 生成 API Key ID 失败: %v", err)
		jsonError(w, "failed to create api key", http.StatusInternalServerError)
		return
	}
	now := time.Now().UTC()
	entry := config.APIKey{
		ID:        id,
		Name:      name,
		Hash:      config.HashAPIKey(plain),
		CreatedAt: now,
	}

	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()

	oldCfg := *s.cfg
	newCfg := oldCfg
	newKeys := make([]config.APIKey, len(oldCfg.ReadOnlyAPIKeys)+1)
	copy(newKeys, oldCfg.ReadOnlyAPIKeys)
	newKeys[len(oldCfg.ReadOnlyAPIKeys)] = entry
	newCfg.ReadOnlyAPIKeys = newKeys

	if err := configSave(&newCfg); err != nil {
		log.Printf("[webui] 保存 API Key 失败: %v", err)
		jsonError(w, "failed to save config", http.StatusInternalServerError)
		return
	}
	s.cfg = &newCfg

	jsonOK(w, map[string]interface{}{
		"id":         entry.ID,
		"name":       entry.Name,
		"key":        plain,
		"created_at": entry.CreatedAt,
	})
}

func (s *Server) apiAPIKeyRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonDecodeError(w, err)
		return
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()

	oldCfg := *s.cfg
	newCfg := oldCfg
	keys := make([]config.APIKey, len(oldCfg.ReadOnlyAPIKeys))
	copy(keys, oldCfg.ReadOnlyAPIKeys)
	found := false
	for i := range keys {
		if keys[i].ID == id {
			keys[i].Disabled = true
			found = true
			break
		}
	}
	if !found {
		jsonError(w, "api key not found", http.StatusNotFound)
		return
	}
	newCfg.ReadOnlyAPIKeys = keys
	if err := configSave(&newCfg); err != nil {
		log.Printf("[webui] 撤销 API Key 失败: %v", err)
		jsonError(w, "failed to save config", http.StatusInternalServerError)
		return
	}
	s.cfg = &newCfg
	jsonOK(w, map[string]string{"status": "revoked"})
}

func (s *Server) apiAPIKeyDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonDecodeError(w, err)
		return
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()

	oldCfg := *s.cfg
	newCfg := oldCfg
	src := oldCfg.ReadOnlyAPIKeys
	out := make([]config.APIKey, 0, len(src))
	found := false
	for _, k := range src {
		if k.ID == id {
			found = true
			continue
		}
		out = append(out, k)
	}
	if !found {
		jsonError(w, "api key not found", http.StatusNotFound)
		return
	}
	newCfg.ReadOnlyAPIKeys = out
	if err := configSave(&newCfg); err != nil {
		log.Printf("[webui] 删除 API Key 失败: %v", err)
		jsonError(w, "failed to save config", http.StatusInternalServerError)
		return
	}
	s.cfg = &newCfg
	jsonOK(w, map[string]string{"status": "deleted"})
}

func newAPIKeySecret() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
