package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAPIStatsReturnsTypedDualProtocolCounts 锁定 /api/stats 的 HTTP JSON 合同：
// mixed 节点分别计入 HTTP 与 SOCKS5，但 total 只计一次，字段类型稳定。
func TestAPIStatsReturnsTypedDualProtocolCounts(t *testing.T) {
	server := newTestServer(t)
	for _, node := range []struct {
		address  string
		protocol string
		dual     bool
	}{
		{address: "stats-http:8080", protocol: "http"},
		{address: "stats-socks:1080", protocol: "socks5"},
		{address: "stats-mixed:2080", protocol: "socks5", dual: true},
	} {
		if err := server.storage.AddProxy(node.address, node.protocol); err != nil {
			t.Fatalf("AddProxy(%s): %v", node.address, err)
		}
		if node.dual {
			proxy, err := server.storage.GetProxyByAddress(node.address)
			if err != nil {
				t.Fatalf("GetProxyByAddress(%s): %v", node.address, err)
			}
			if err := server.storage.SetProxyDualProtocol(proxy.ID, true); err != nil {
				t.Fatalf("SetProxyDualProtocol(%s): %v", node.address, err)
			}
		}
	}

	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, authenticatedJSONRequest(http.MethodGet, "/api/stats", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var body struct {
		Total          int    `json:"total"`
		HTTP           int    `json:"http"`
		SOCKS5         int    `json:"socks5"`
		Subscription   int    `json:"subscription_count"`
		ActiveSessions int    `json:"active_sessions"`
		HTTPPort       string `json:"http_port"`
		SOCKS5Port     string `json:"socks5_port"`
		WebUIPort      string `json:"webui_port"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /api/stats response: %v; body=%s", err, rec.Body.String())
	}
	if body.Total != 3 {
		t.Fatalf("total = %d, want 3", body.Total)
	}
	if body.HTTP != 2 || body.SOCKS5 != 2 {
		t.Fatalf("protocol counts = http:%d socks5:%d, want 2/2 including mixed node", body.HTTP, body.SOCKS5)
	}
	if body.Subscription != 0 || body.ActiveSessions != 0 {
		t.Fatalf("subscription/session counts = %d/%d, want 0/0", body.Subscription, body.ActiveSessions)
	}
	if body.HTTPPort != "" || body.SOCKS5Port != "" || body.WebUIPort != ":0" {
		t.Fatalf("port fields = %q/%q/%q, want empty/empty/:0", body.HTTPPort, body.SOCKS5Port, body.WebUIPort)
	}
}

// TestAPIStatsReturns500WhenStorageFails 防止 storage 错误被伪装成 200 或部分 JSON。
func TestAPIStatsReturns500WhenStorageFails(t *testing.T) {
	server := newTestServer(t)
	if err := server.storage.Close(); err != nil {
		t.Fatalf("close storage: %v", err)
	}

	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, authenticatedJSONRequest(http.MethodGet, "/api/stats", ""))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v; body=%s", err, rec.Body.String())
	}
	if body["error"] != "failed to load stats" {
		t.Fatalf("error = %q, want failed to load stats", body["error"])
	}
}
