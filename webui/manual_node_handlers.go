package webui

import (
	"encoding/json"
	"net/http"

	"goproxy/storage"
)

func (s *Server) apiManualNodeAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Link   string `json:"link"`
		Region string `json:"region"`
		Note   string `json:"note"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Link == "" {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if s.customMgr == nil {
		jsonError(w, "manual node manager unavailable", http.StatusInternalServerError)
		return
	}
	if err := s.customMgr.AddManualNode(req.Link, req.Region, req.Note); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	jsonOK(w, map[string]string{"status": "added"})
}

func (s *Server) apiManualNodeRegion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Address string `json:"address"`
		Region  string `json:"region"`
	}
	if !s.requireManualNodeRequest(w, r, &req, &req.Address) {
		return
	}
	if err := s.storage.UpdateProxyRegion(req.Address, req.Region, true); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"status": "updated"})
}

func (s *Server) apiManualNodeNote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Address string `json:"address"`
		Note    string `json:"note"`
	}
	if !s.requireManualNodeRequest(w, r, &req, &req.Address) {
		return
	}
	if err := s.storage.UpdateProxyNote(req.Address, req.Note); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"status": "updated"})
}

func (s *Server) apiManualNodeDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Address string `json:"address"`
	}
	if !s.requireManualNodeRequest(w, r, &req, &req.Address) {
		return
	}
	if err := s.storage.DeleteManualProxy(req.Address); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"status": "deleted"})
}

func (s *Server) requireManualNodeRequest(w http.ResponseWriter, r *http.Request, dst interface{}, address *string) bool {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	if err := decodeJSON(r, dst); err != nil || *address == "" {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return false
	}
	proxy, err := s.storage.GetProxyByAddress(*address)
	if err != nil {
		jsonError(w, err.Error(), http.StatusNotFound)
		return false
	}
	if proxy.Source != storage.SourceManual {
		jsonError(w, "manual nodes only", http.StatusForbidden)
		return false
	}
	return true
}

func decodeJSON(r *http.Request, dst interface{}) error {
	return json.NewDecoder(r.Body).Decode(dst)
}
