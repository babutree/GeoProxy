package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/storage"
)

func newNodesAPITestServer(t *testing.T, plainKey string) (*Server, string) {
	t.Helper()
	if plainKey == "" {
		plainKey = "nodes-api-test-key"
	}
	server := newReadOnlyAPITestServer(t, []config.APIKey{{
		ID:   "nodes-k1",
		Name: "nodes",
		Hash: testAPIKeyHash(plainKey),
	}}, 60)
	server.cfg.PublicHost = "203.0.113.50"
	server.cfg.SOCKS5Port = ":7801"
	server.cfg.HTTPPort = ":7802"
	server.cfg.ProxyAuthUsername = "username"
	server.cfg.ProxyAuthPassword = "super-secret-proxy-pass"
	return server, plainKey
}

func nodesAPIRequest(method, path, plainKey string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if plainKey != "" {
		req.Header.Set("Authorization", "Bearer "+plainKey)
	}
	return req
}

func insertNodesAPIProxy(t *testing.T, store *storage.Storage, p storage.Proxy) int64 {
	t.Helper()
	userPaused := 0
	if p.UserPaused {
		userPaused = 1
	}
	starred := 0
	if p.Starred {
		starred = 1
	}
	dual := 0
	if p.DualProtocol {
		dual = 1
	}
	ipapiSeen := 0
	if p.IPAPIFlagsSeen {
		ipapiSeen = 1
	}
	if p.Source == "" {
		p.Source = storage.SourceManual
	}
	if p.Status == "" {
		p.Status = "active"
	}
	if p.Protocol == "" {
		p.Protocol = "socks5"
	}
	res, err := store.GetDB().Exec(
		`INSERT INTO proxies (
			address, protocol, region, region_source, note, exit_ip, exit_location,
			latency, quality_grade, use_count, success_count, fail_count, status, user_paused,
			source, subscription_id, ipapiis_score, ipapi_flags, ipapi_flags_seen, starred,
			cf_blocked, dual_protocol, ai_reachability
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Address, p.Protocol, p.Region, p.RegionSource, p.Note, p.ExitIP, p.ExitLocation,
		p.Latency, p.QualityGrade, p.UseCount, p.SuccessCount, p.FailCount, p.Status, userPaused,
		p.Source, p.SubscriptionID, p.IPAPIIsScore, p.IPAPIFlags, ipapiSeen, starred,
		p.CFBlocked, dual, p.AIReachability,
	)
	if err != nil {
		t.Fatalf("insertNodesAPIProxy %s: %v", p.Address, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId %s: %v", p.Address, err)
	}
	return id
}

func decodeNodesResponse(t *testing.T, body []byte) (total, count int, nodes []map[string]any) {
	t.Helper()
	var resp struct {
		Total int              `json:"total"`
		Count int              `json:"count"`
		Nodes []map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode nodes response: %v body=%s", err, string(body))
	}
	return resp.Total, resp.Count, resp.Nodes
}

func TestApiV1NodesRejectsMissingOrBadKey(t *testing.T) {
	server, _ := newNodesAPITestServer(t, "good-nodes-key")

	cases := []struct {
		name   string
		header func(*http.Request)
	}{
		{name: "missing", header: func(*http.Request) {}},
		{name: "bad_bearer", header: func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer wrong-key")
		}},
		{name: "bad_x_api_key", header: func(r *http.Request) {
			r.Header.Set("X-API-Key", "wrong-key")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
			tc.header(req)
			rec := httptest.NewRecorder()
			server.routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
			}
		})
	}
}

func TestApiV1NodesDirectNodeReportsDirectConnect(t *testing.T) {
	server, key := newNodesAPITestServer(t, "direct-nodes-key")
	id := insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address: "198.51.100.10:1080", Protocol: "socks5", Source: storage.SourceManual,
		Region: "us", RegionSource: "manual",
		ExitIP: "198.51.100.10", ExitLocation: "US / California / Santa Clara",
		Latency: 83, QualityGrade: "A", Status: "active",
		IPAPIIsScore: 0.02, IPAPIFlags: "hosting", IPAPIFlagsSeen: true,
		CFBlocked:      0,
		AIReachability: `{"openai":0,"claude":1,"grok":-1,"gemini":0}`,
	})

	req := nodesAPIRequest(http.MethodGet, "/api/v1/nodes", key)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	total, count, nodes := decodeNodesResponse(t, rec.Body.Bytes())
	if total != 1 || count != 1 || len(nodes) != 1 {
		t.Fatalf("total/count/len = %d/%d/%d, want 1/1/1 body=%s", total, count, len(nodes), rec.Body.String())
	}
	n := nodes[0]
	if int64(n["id"].(float64)) != id {
		t.Fatalf("id = %v, want %d", n["id"], id)
	}
	if n["protocol"] != "socks5" || n["source"] != "manual" || n["region"] != "us" {
		t.Fatalf("identity fields = %#v", n)
	}
	conn, ok := n["connect"].(map[string]any)
	if !ok {
		t.Fatalf("connect missing: %#v", n)
	}
	if conn["mode"] != "direct" {
		t.Fatalf("connect.mode = %v, want direct", conn["mode"])
	}
	if conn["host"] != "198.51.100.10" {
		t.Fatalf("connect.host = %v, want 198.51.100.10", conn["host"])
	}
	if int(conn["port"].(float64)) != 1080 {
		t.Fatalf("connect.port = %v, want 1080", conn["port"])
	}
	if conn["dual_protocol"] != false {
		t.Fatalf("connect.dual_protocol = %v, want false", conn["dual_protocol"])
	}
}

func TestApiV1NodesTunnelNodeReportsGatewayConnect(t *testing.T) {
	server, key := newNodesAPITestServer(t, "gateway-nodes-key")

	// dual_protocol tunnel
	insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address: "127.0.0.1:20001", Protocol: "socks5", Source: storage.SourceSubscription,
		SubscriptionID: 1, Region: "jp", DualProtocol: true, Status: "active",
		ExitIP: "203.0.113.9", Latency: 120, IPAPIIsScore: 0, IPAPIFlagsSeen: true, CFBlocked: 0,
		AIReachability: `{"openai":0}`,
	})
	// loopback without dual_protocol also gateway
	insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address: "127.0.0.1:20002", Protocol: "socks5", Source: storage.SourceSubscription,
		SubscriptionID: 1, Region: "sg", DualProtocol: false, Status: "active",
		ExitIP: "203.0.113.10", Latency: 130, IPAPIIsScore: -1, CFBlocked: -1,
	})

	req := nodesAPIRequest(http.MethodGet, "/api/v1/nodes", key)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "127.0.0.1") {
		t.Fatalf("response must not contain 127.0.0.1: %s", body)
	}

	_, _, nodes := decodeNodesResponse(t, rec.Body.Bytes())
	if len(nodes) != 2 {
		t.Fatalf("len(nodes)=%d, want 2 body=%s", len(nodes), body)
	}
	for _, n := range nodes {
		conn, ok := n["connect"].(map[string]any)
		if !ok {
			t.Fatalf("connect missing: %#v", n)
		}
		if conn["mode"] != "gateway" {
			t.Fatalf("connect.mode = %v, want gateway for %#v", conn["mode"], n)
		}
		if conn["host"] != "203.0.113.50" {
			t.Fatalf("gateway host = %v, want PublicHost 203.0.113.50", conn["host"])
		}
		if int(conn["gateway_socks5_port"].(float64)) != 7801 {
			t.Fatalf("gateway_socks5_port = %v, want 7801", conn["gateway_socks5_port"])
		}
		if int(conn["gateway_http_port"].(float64)) != 7802 {
			t.Fatalf("gateway_http_port = %v, want 7802", conn["gateway_http_port"])
		}
		hint, _ := conn["username_hint"].(string)
		region, _ := n["region"].(string)
		wantPrefix := "username-region-" + region + "-session-"
		if !strings.HasPrefix(hint, wantPrefix) {
			t.Fatalf("username_hint = %q, want prefix %q", hint, wantPrefix)
		}
		if strings.Contains(hint, "super-secret") || strings.Contains(strings.ToLower(hint), "password") {
			t.Fatalf("username_hint must not include password: %q", hint)
		}
	}
}

func TestApiV1NodesPrivateAddressesUseGatewayConnect(t *testing.T) {
	server, key := newNodesAPITestServer(t, "private-connect-key")
	cases := []string{
		"10.23.4.8:1080",
		"172.16.4.8:1080",
		"192.168.4.8:1080",
		"100.64.10.2:1080",
		"169.254.1.10:1080",
		"[fd12::8]:1080",
		"[fe80::8]:1080",
		"node.internal:1080",
	}
	for i, address := range cases {
		insertNodesAPIProxy(t, server.storage, storage.Proxy{
			Address: address, Protocol: "socks5", Region: "us", Status: "active",
			Latency: i + 1, IPAPIIsScore: -1, CFBlocked: -1,
		})
	}

	req := nodesAPIRequest(http.MethodGet, "/api/v1/nodes", key)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	for _, address := range cases {
		host, _ := splitAddressHostPort(address)
		if strings.Contains(body, host) {
			t.Fatalf("response leaked private node host %q: %s", host, body)
		}
	}
	_, _, nodes := decodeNodesResponse(t, rec.Body.Bytes())
	if len(nodes) != len(cases) {
		t.Fatalf("nodes len = %d, want %d", len(nodes), len(cases))
	}
	for _, node := range nodes {
		connect := node["connect"].(map[string]any)
		if connect["mode"] != "gateway" {
			t.Fatalf("private node connect.mode = %v, want gateway; node=%#v", connect["mode"], node)
		}
		if connect["host"] != "203.0.113.50" {
			t.Fatalf("gateway host = %v, want public host", connect["host"])
		}
	}
}

func TestApiV1NodesPurityFieldsFidelity(t *testing.T) {
	server, key := newNodesAPITestServer(t, "purity-nodes-key")
	insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address: "203.0.113.20:1080", Protocol: "socks5", Source: storage.SourceManual,
		Region: "us", Status: "active",
		// unprobed purity
		IPAPIIsScore: -1, IPAPIFlags: "", IPAPIFlagsSeen: false,
		CFBlocked: -1, AIReachability: "",
		Latency: 50, QualityGrade: "B",
	})

	req := nodesAPIRequest(http.MethodGet, "/api/v1/nodes", key)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	_, _, nodes := decodeNodesResponse(t, rec.Body.Bytes())
	if len(nodes) != 1 {
		t.Fatalf("len(nodes)=%d body=%s", len(nodes), rec.Body.String())
	}
	n := nodes[0]
	purity, ok := n["purity"].(map[string]any)
	if !ok {
		t.Fatalf("purity missing: %#v", n)
	}
	if purity["ipapiis_abuse_score"].(float64) != -1 {
		t.Fatalf("ipapiis_abuse_score = %v, want -1", purity["ipapiis_abuse_score"])
	}
	if purity["ipapi_flags_seen"] != false {
		t.Fatalf("ipapi_flags_seen = %v, want false", purity["ipapi_flags_seen"])
	}
	flags, ok := purity["ipapi_flags"].([]any)
	if !ok {
		t.Fatalf("ipapi_flags type = %T, want array", purity["ipapi_flags"])
	}
	if len(flags) != 0 {
		t.Fatalf("ipapi_flags = %#v, want empty", flags)
	}
	if n["cf_blocked"].(float64) != -1 {
		t.Fatalf("cf_blocked = %v, want -1", n["cf_blocked"])
	}
	// required top-level fields present
	for _, k := range []string{"latency_ms", "quality_grade", "status", "last_check", "ai_reachability", "exit_ip", "exit_location"} {
		if _, ok := n[k]; !ok {
			t.Fatalf("missing field %q in %#v", k, n)
		}
	}
}

func TestApiV1NodesRegionFilterPassthrough(t *testing.T) {
	server, key := newNodesAPITestServer(t, "region-nodes-key")
	insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address: "198.51.100.1:1080", Protocol: "socks5", Region: "us", Status: "active",
		IPAPIIsScore: -1, CFBlocked: -1,
	})
	insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address: "198.51.100.2:1080", Protocol: "socks5", Region: "jp", Status: "active",
		IPAPIIsScore: -1, CFBlocked: -1,
	})

	req := nodesAPIRequest(http.MethodGet, "/api/v1/nodes?region=jp", key)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	total, count, nodes := decodeNodesResponse(t, rec.Body.Bytes())
	if total != 1 || count != 1 || len(nodes) != 1 {
		t.Fatalf("total/count/len = %d/%d/%d, want 1/1/1 body=%s", total, count, len(nodes), rec.Body.String())
	}
	if nodes[0]["region"] != "jp" {
		t.Fatalf("region = %v, want jp", nodes[0]["region"])
	}
}

func TestApiV1NodesNeverLeaksSecrets(t *testing.T) {
	server, key := newNodesAPITestServer(t, "secret-nodes-key")
	server.cfg.ProxyAuthPassword = "proxy-auth-password-SECRET"
	insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address: "127.0.0.1:21000", Protocol: "socks5", Source: storage.SourceSubscription,
		SubscriptionID: 1, Region: "de", DualProtocol: true, Status: "active",
		IPAPIIsScore: 0.1, IPAPIFlagsSeen: true, CFBlocked: 0,
	})
	insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address: "203.0.113.88:9050", Protocol: "socks5", Source: storage.SourceManual,
		Region: "us", Status: "active", IPAPIIsScore: -1, CFBlocked: -1,
	})

	req := nodesAPIRequest(http.MethodGet, "/api/v1/nodes", key)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	lower := strings.ToLower(body)
	for _, bad := range []string{
		"proxy-auth-password-secret",
		`"password"`,
		"proxy_auth_password",
		"127.0.0.1",
		key, // plain api key must not echo
	} {
		if strings.Contains(lower, strings.ToLower(bad)) {
			t.Fatalf("response leaked %q: %s", bad, body)
		}
	}
	// structural: no password-like keys in decoded nodes
	_, _, nodes := decodeNodesResponse(t, rec.Body.Bytes())
	raw, _ := json.Marshal(nodes)
	rawLower := strings.ToLower(string(raw))
	if strings.Contains(rawLower, "password") {
		t.Fatalf("nodes JSON contains password field: %s", raw)
	}
}

func TestApiV1NodesConnectFilterDirectAndGateway(t *testing.T) {
	server, key := newNodesAPITestServer(t, "connect-filter-key")
	insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address: "198.51.100.30:1080", Protocol: "socks5", Region: "us", Status: "active",
		IPAPIIsScore: -1, CFBlocked: -1,
	})
	insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address: "127.0.0.1:22000", Protocol: "socks5", Region: "jp", DualProtocol: true, Status: "active",
		IPAPIIsScore: -1, CFBlocked: -1,
	})

	t.Run("direct", func(t *testing.T) {
		req := nodesAPIRequest(http.MethodGet, "/api/v1/nodes?connect=direct", key)
		rec := httptest.NewRecorder()
		server.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		_, _, nodes := decodeNodesResponse(t, rec.Body.Bytes())
		if len(nodes) != 1 {
			t.Fatalf("len=%d want 1 body=%s", len(nodes), rec.Body.String())
		}
		conn := nodes[0]["connect"].(map[string]any)
		if conn["mode"] != "direct" {
			t.Fatalf("mode=%v want direct", conn["mode"])
		}
	})
	t.Run("gateway", func(t *testing.T) {
		req := nodesAPIRequest(http.MethodGet, "/api/v1/nodes?connect=gateway", key)
		rec := httptest.NewRecorder()
		server.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if strings.Contains(body, "127.0.0.1") {
			t.Fatalf("gateway filter response leaked 127.0.0.1: %s", body)
		}
		_, _, nodes := decodeNodesResponse(t, rec.Body.Bytes())
		if len(nodes) != 1 {
			t.Fatalf("len=%d want 1 body=%s", len(nodes), body)
		}
		conn := nodes[0]["connect"].(map[string]any)
		if conn["mode"] != "gateway" {
			t.Fatalf("mode=%v want gateway", conn["mode"])
		}
	})
}

func TestApiV1NodesConnectFilterTotalIsBeforePagination(t *testing.T) {
	server, key := newNodesAPITestServer(t, "connect-total-key")
	insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address: "127.0.0.1:22010", Protocol: "socks5", Region: "jp", DualProtocol: true, Status: "active",
		Latency: 10, IPAPIIsScore: -1, CFBlocked: -1,
	})
	insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address: "198.51.100.40:1080", Protocol: "socks5", Region: "us", Status: "active",
		Latency: 20, IPAPIIsScore: -1, CFBlocked: -1,
	})
	insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address: "127.0.0.1:22011", Protocol: "socks5", Region: "sg", DualProtocol: true, Status: "active",
		Latency: 30, IPAPIIsScore: -1, CFBlocked: -1,
	})

	for _, tc := range []struct {
		name       string
		path       string
		wantRegion string
	}{
		{name: "first_gateway_page", path: "/api/v1/nodes?connect=gateway&limit=1", wantRegion: "jp"},
		{name: "second_gateway_page", path: "/api/v1/nodes?connect=gateway&limit=1&offset=1", wantRegion: "sg"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := nodesAPIRequest(http.MethodGet, tc.path, key)
			rec := httptest.NewRecorder()
			server.routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			total, count, nodes := decodeNodesResponse(t, rec.Body.Bytes())
			if total != 2 || count != 1 || len(nodes) != 1 {
				t.Fatalf("total/count/len=%d/%d/%d, want 2/1/1 body=%s", total, count, len(nodes), rec.Body.String())
			}
			if nodes[0]["region"] != tc.wantRegion {
				t.Fatalf("region=%v want %s", nodes[0]["region"], tc.wantRegion)
			}
			conn := nodes[0]["connect"].(map[string]any)
			if conn["mode"] != "gateway" {
				t.Fatalf("mode=%v want gateway", conn["mode"])
			}
		})
	}
}

func TestApiV1NodesRejectsInvalidQueryParams(t *testing.T) {
	server, key := newNodesAPITestServer(t, "invalid-query-key")
	insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address: "198.51.100.41:1080", Protocol: "socks5", Region: "us", Status: "active",
		IPAPIIsScore: 0.1, CFBlocked: 0,
	})

	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "bad_limit_text", path: "/api/v1/nodes?limit=abc"},
		{name: "bad_limit_zero", path: "/api/v1/nodes?limit=0"},
		{name: "bad_limit_over_max", path: "/api/v1/nodes?limit=2001"},
		{name: "bad_offset_text", path: "/api/v1/nodes?offset=abc"},
		{name: "bad_offset_negative", path: "/api/v1/nodes?offset=-1"},
		{name: "bad_max_abuse_text", path: "/api/v1/nodes?max_abuse=abc"},
		{name: "bad_max_abuse_negative", path: "/api/v1/nodes?max_abuse=-0.1"},
		{name: "bad_max_abuse_over_one", path: "/api/v1/nodes?max_abuse=1.1"},
		{name: "bad_cf", path: "/api/v1/nodes?cf=unknown"},
		{name: "bad_status", path: "/api/v1/nodes?status=active"},
		{name: "bad_connect", path: "/api/v1/nodes?connect=tunnel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := nodesAPIRequest(http.MethodGet, tc.path, key)
			rec := httptest.NewRecorder()
			server.routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

func TestApiV1NodesMethodNotAllowed(t *testing.T) {
	server, key := newNodesAPITestServer(t, "method-nodes-key")
	req := nodesAPIRequest(http.MethodPost, "/api/v1/nodes", key)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusMethodNotAllowed, rec.Body.String())
	}
}

func TestApiV1NodesUsernameHintTemplate(t *testing.T) {
	server, key := newNodesAPITestServer(t, "hint-nodes-key")
	server.cfg.ProxyAuthUsername = "edge"
	insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address: "127.0.0.1:23000", Protocol: "socks5", Region: "kr", DualProtocol: true, Status: "active",
		IPAPIIsScore: -1, CFBlocked: -1,
	})
	req := nodesAPIRequest(http.MethodGet, "/api/v1/nodes", key)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	_, _, nodes := decodeNodesResponse(t, rec.Body.Bytes())
	if len(nodes) != 1 {
		t.Fatalf("len=%d body=%s", len(nodes), rec.Body.String())
	}
	conn := nodes[0]["connect"].(map[string]any)
	hint := fmt.Sprint(conn["username_hint"])
	if hint != "edge-region-kr-session-api" {
		t.Fatalf("username_hint = %q, want edge-region-kr-session-api", hint)
	}
}
