package webui

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/babutree/GeoProxy/storage"
)

func (s *Server) apiManualNodeImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Text   string `json:"text"`
		Region string `json:"region"`
		Note   string `json:"note"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonDecodeError(w, err)
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if s.customMgr == nil {
		jsonError(w, "manual node manager unavailable", http.StatusInternalServerError)
		return
	}
	result, err := s.customMgr.ImportManualLinks(req.Text, req.Region, req.Note)
	if err != nil {
		log.Printf("[webui] import manual nodes failed: %v", err)
		jsonError(w, "failed to import manual nodes", http.StatusBadRequest)
		return
	}
	jsonOK(w, result)
}

func (s *Server) apiManualNodeBatchDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonDecodeError(w, err)
		return
	}
	if len(req.IDs) == 0 {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if s.customMgr == nil {
		jsonError(w, "manual node manager unavailable", http.StatusInternalServerError)
		return
	}
	deleted, errs := s.customMgr.DeleteManualNodes(req.IDs)
	jsonOK(w, map[string]interface{}{
		"deleted": deleted,
		"failed":  len(errs),
		"errors":  errs,
	})
}

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
	if err := decodeJSON(r, &req); err != nil {
		jsonDecodeError(w, err)
		return
	}
	if req.Link == "" {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if s.customMgr == nil {
		jsonError(w, "manual node manager unavailable", http.StatusInternalServerError)
		return
	}
	if err := s.customMgr.AddManualNode(req.Link, req.Region, req.Note); err != nil {
		log.Printf("[webui] add manual node failed: %v", err)
		jsonError(w, "failed to add manual node", http.StatusBadRequest)
		return
	}
	jsonOK(w, map[string]string{"status": "added"})
}

func (s *Server) apiManualNodeRegion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      int64  `json:"id"`
		Address string `json:"address"`
		Region  string `json:"region"`
	}
	proxy, ok := s.requireManualNodeRequest(w, r, &req, &req.ID, &req.Address)
	if !ok {
		return
	}
	if err := s.storage.UpdateProxyRegionByID(proxy.ID, req.Region, true); err != nil {
		log.Printf("[webui] update manual node region %q failed: %v", req.Address, err)
		jsonError(w, "failed to update manual node region", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"status": "updated"})
}

func (s *Server) apiManualNodeNote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      int64  `json:"id"`
		Address string `json:"address"`
		Note    string `json:"note"`
	}
	proxy, ok := s.requireManualNodeRequest(w, r, &req, &req.ID, &req.Address)
	if !ok {
		return
	}
	if err := s.storage.UpdateProxyNoteByID(proxy.ID, req.Note); err != nil {
		log.Printf("[webui] update manual node note %q failed: %v", req.Address, err)
		jsonError(w, "failed to update manual node note", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"status": "updated"})
}

func (s *Server) apiManualNodeDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      int64  `json:"id"`
		Address string `json:"address"`
	}
	proxy, ok := s.requireManualNodeRequest(w, r, &req, &req.ID, &req.Address)
	if !ok {
		return
	}
	if s.customMgr != nil {
		if err := s.customMgr.DeleteManualNode(proxy.ID); err != nil {
			log.Printf("[webui] delete manual node %q failed: %v", req.Address, err)
			jsonError(w, "failed to delete manual node", http.StatusInternalServerError)
			return
		}
	} else if err := s.storage.DeleteProxyByID(proxy.ID); err != nil {
		log.Printf("[webui] delete manual node %q failed: %v", req.Address, err)
		jsonError(w, "failed to delete manual node", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"status": "deleted"})
}

func (s *Server) requireManualNodeRequest(w http.ResponseWriter, r *http.Request, dst interface{}, id *int64, address *string) (*storage.Proxy, bool) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return nil, false
	}
	if err := decodeJSON(r, dst); err != nil {
		jsonDecodeError(w, err)
		return nil, false
	}
	var proxy *storage.Proxy
	var err error
	if *id > 0 {
		proxy, err = s.storage.GetProxyByID(*id)
	} else if *address != "" {
		proxy, err = s.storage.GetProxyByAddress(*address)
	} else {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return nil, false
	}
	if err != nil {
		if errors.Is(err, storage.ErrAmbiguousProxyAddress) || strings.Contains(err.Error(), "ambiguous") {
			log.Printf("[webui] manual node address %q ambiguous: %v", *address, err)
			jsonError(w, "ambiguous proxy address; use id", http.StatusConflict)
			return nil, false
		}
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "not found") {
			log.Printf("[webui] manual node id=%d address=%q not found: %v", *id, *address, err)
			jsonError(w, "manual node not found", http.StatusNotFound)
			return nil, false
		}
		log.Printf("[webui] manual node id=%d address=%q lookup failed: %v", *id, *address, err)
		jsonError(w, "failed to lookup manual node", http.StatusInternalServerError)
		return nil, false
	}
	if proxy.Source != storage.SourceManual {
		jsonError(w, "manual nodes only", http.StatusForbidden)
		return nil, false
	}
	return proxy, true
}
