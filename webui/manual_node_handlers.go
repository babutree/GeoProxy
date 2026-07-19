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
		log.Printf("[webui] 导入手工节点失败: %v", err)
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
	// 与节点表多选对齐：任意来源（含订阅）均可批量删除，避免勾选订阅节点后全部失败。
	deleted, errs := s.customMgr.DeleteManagedProxies(req.IDs)
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
		log.Printf("[webui] 添加手工节点失败: %v", err)
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
	// 地域覆盖对任意来源开放：订阅节点也允许手工覆盖地域（非破坏性、可逆）。
	// 仅放宽地域与备注两条非破坏性路径；删除仍按来源分流。
	proxy, ok := s.requireNoteEditableRequest(w, r, &req, &req.ID, &req.Address)
	if !ok {
		return
	}
	if err := s.storage.UpdateProxyRegionByID(proxy.ID, req.Region, true); err != nil {
		log.Printf("[webui] 更新节点 %q 地域失败: %v", req.Address, err)
		jsonError(w, "failed to update node region", http.StatusInternalServerError)
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
	// 备注编辑对任意来源开放：订阅节点也允许编辑备注（UpdateProxyNoteByID 本身来源无关）。
	// 仅放宽备注这一路径，region/delete 仍严格限手工节点。
	proxy, ok := s.requireNoteEditableRequest(w, r, &req, &req.ID, &req.Address)
	if !ok {
		return
	}
	if err := s.storage.UpdateProxyNoteByID(proxy.ID, req.Note); err != nil {
		log.Printf("[webui] 更新节点 %q 备注失败: %v", req.Address, err)
		jsonError(w, "failed to update node note", http.StatusInternalServerError)
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
			log.Printf("[webui] 删除手工节点 %q 失败: %v", req.Address, err)
			jsonError(w, "failed to delete manual node", http.StatusInternalServerError)
			return
		}
	} else if err := s.storage.DeleteProxyByID(proxy.ID); err != nil {
		log.Printf("[webui] 删除手工节点 %q 失败: %v", req.Address, err)
		jsonError(w, "failed to delete manual node", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"status": "deleted"})
}

// requireNoteEditableRequest 与 requireManualNodeRequest 一致地解析并定位节点，
// 但不施加“仅手工节点”来源限制——备注编辑对任意来源（含订阅节点）开放。
// 仅供备注这一非破坏性更新使用；region/delete 仍须走 requireManualNodeRequest。
func (s *Server) requireNoteEditableRequest(w http.ResponseWriter, r *http.Request, dst interface{}, id *int64, address *string) (*storage.Proxy, bool) {
	return s.lookupProxyRequest(w, r, dst, id, address, false)
}

func (s *Server) requireManualNodeRequest(w http.ResponseWriter, r *http.Request, dst interface{}, id *int64, address *string) (*storage.Proxy, bool) {
	return s.lookupProxyRequest(w, r, dst, id, address, true)
}

func (s *Server) lookupProxyRequest(w http.ResponseWriter, r *http.Request, dst interface{}, id *int64, address *string, manualOnly bool) (*storage.Proxy, bool) {
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
			log.Printf("[webui] 手工节点地址 %q 不唯一: %v", *address, err)
			jsonError(w, "ambiguous proxy address; use id", http.StatusConflict)
			return nil, false
		}
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "not found") {
			log.Printf("[webui] 未找到手工节点 id=%d address=%q: %v", *id, *address, err)
			jsonError(w, "manual node not found", http.StatusNotFound)
			return nil, false
		}
		log.Printf("[webui] 查询手工节点 id=%d address=%q 失败: %v", *id, *address, err)
		jsonError(w, "failed to lookup manual node", http.StatusInternalServerError)
		return nil, false
	}
	if manualOnly && proxy.Source != storage.SourceManual {
		jsonError(w, "manual nodes only", http.StatusForbidden)
		return nil, false
	}
	return proxy, true
}
