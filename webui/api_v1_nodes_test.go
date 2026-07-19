package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/affinity"
	"github.com/babutree/GeoProxy/auth"
	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/selector"
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
			cf_blocked, dual_protocol, ai_reachability, proxy_username, proxy_password, node_key
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Address, p.Protocol, p.Region, p.RegionSource, p.Note, p.ExitIP, p.ExitLocation,
		p.Latency, p.QualityGrade, p.UseCount, p.SuccessCount, p.FailCount, p.Status, userPaused,
		p.Source, p.SubscriptionID, p.IPAPIIsScore, p.IPAPIFlags, ipapiSeen, starred,
		p.CFBlocked, dual, p.AIReachability, p.Username, p.Password, p.NodeKey,
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
	subscriptionID, err := server.storage.AddSubscription(
		"gateway-nodes",
		"https://example.test/gateway-nodes",
		"",
		"auto",
		60,
		"",
	)
	if err != nil {
		t.Fatalf("AddSubscription(): %v", err)
	}

	// dual_protocol tunnel
	insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address: "127.0.0.1:20001", Protocol: "socks5", Source: storage.SourceSubscription,
		SubscriptionID: subscriptionID, Region: "jp", DualProtocol: true, Status: "active", NodeKey: "gateway-jp-a",
		ExitIP: "203.0.113.9", Latency: 120, IPAPIIsScore: 0, IPAPIFlagsSeen: true, CFBlocked: 0,
		AIReachability: `{"openai":0}`,
	})
	// loopback without dual_protocol also gateway
	insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address: "127.0.0.1:20002", Protocol: "socks5", Source: storage.SourceSubscription,
		SubscriptionID: subscriptionID, Region: "sg", DualProtocol: false, Status: "active", NodeKey: "gateway-sg-b",
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
		parsed, err := auth.ParseUsername(hint)
		if err != nil {
			t.Fatalf("auth.ParseUsername(%q): %v", hint, err)
		}
		wantNodeKey := "gateway-" + region
		if region == "jp" {
			wantNodeKey += "-a"
		} else {
			wantNodeKey += "-b"
		}
		if parsed.Region != region || parsed.Node != "key-"+wantNodeKey || parsed.Session != "api" {
			t.Fatalf("parsed username_hint = %#v, want Region=%s Node=key-%s Session=api", parsed, region, wantNodeKey)
		}
		if strings.Contains(hint, "super-secret") || strings.Contains(strings.ToLower(hint), "password") {
			t.Fatalf("username_hint must not include password: %q", hint)
		}
	}
}

func TestApiV1NodesSubscriptionNodesRequireActiveParent(t *testing.T) {
	tests := []struct {
		name             string
		invalidateParent func(*testing.T, *storage.Storage, int64)
	}{
		{
			name: "missing",
			invalidateParent: func(t *testing.T, store *storage.Storage, subscriptionID int64) {
				t.Helper()
				if _, err := store.GetDB().Exec(`DELETE FROM subscriptions WHERE id = ?`, subscriptionID); err != nil {
					t.Fatalf("delete parent subscription: %v", err)
				}
			},
		},
		{
			name: "paused",
			invalidateParent: func(t *testing.T, store *storage.Storage, subscriptionID int64) {
				t.Helper()
				if err := store.PauseSubscription(subscriptionID); err != nil {
					t.Fatalf("PauseSubscription(): %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, key := newNodesAPITestServer(t, "parent-scope-"+test.name)
			subscriptionID, err := server.storage.AddSubscription(
				"parent-scope-"+test.name,
				"https://example.test/parent-scope-"+test.name,
				"",
				"auto",
				60,
				"",
			)
			if err != nil {
				t.Fatalf("AddSubscription(): %v", err)
			}
			insertNodesAPIProxy(t, server.storage, storage.Proxy{
				Address:        "127.0.0.1:20100",
				Protocol:       "socks5",
				Source:         storage.SourceSubscription,
				SubscriptionID: subscriptionID,
				Region:         "jp",
				Status:         "active",
				NodeKey:        "parent-scope-" + test.name,
				IPAPIIsScore:   -1,
				CFBlocked:      -1,
			})
			test.invalidateParent(t, server.storage, subscriptionID)

			rec := httptest.NewRecorder()
			server.routes().ServeHTTP(rec, nodesAPIRequest(http.MethodGet, "/api/v1/nodes", key))
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
			}
			total, count, nodes := decodeNodesResponse(t, rec.Body.Bytes())
			if total != 0 || count != 0 || len(nodes) != 0 {
				t.Fatalf("inactive parent leaked nodes: total/count/len=%d/%d/%d body=%s", total, count, len(nodes), rec.Body.String())
			}
		})
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
	subscriptionID, err := server.storage.AddSubscription(
		"secret-node-parent",
		"https://example.test/secret-node-parent",
		"",
		"auto",
		60,
		"",
	)
	if err != nil {
		t.Fatalf("AddSubscription(): %v", err)
	}
	subscriptionNodeID := insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address: "127.0.0.1:21000", Protocol: "socks5", Source: storage.SourceSubscription,
		SubscriptionID: subscriptionID, Region: "de", DualProtocol: true, Status: "active",
		IPAPIIsScore: 0.1, IPAPIFlagsSeen: true, CFBlocked: 0, NodeKey: "stable-secret-test-key",
		Username: "upstream-user-SECRET", Password: "upstream-password-SECRET",
		Note: "https://subscription-secret.invalid/token",
	})
	manualNodeID := insertNodesAPIProxy(t, server.storage, storage.Proxy{
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
		"upstream-user-secret",
		"upstream-password-secret",
		"subscription-secret.invalid",
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
	total, count, nodes := decodeNodesResponse(t, rec.Body.Bytes())
	if total != 2 || count != 2 || len(nodes) != 2 {
		t.Fatalf("total/count/len=%d/%d/%d, want 2/2/2 body=%s", total, count, len(nodes), body)
	}
	nodesByID := make(map[int64]map[string]any, len(nodes))
	for _, node := range nodes {
		nodesByID[int64(node["id"].(float64))] = node
	}
	subscriptionNode, ok := nodesByID[subscriptionNodeID]
	if !ok {
		t.Fatalf("subscription node id=%d missing from response: %#v", subscriptionNodeID, nodes)
	}
	if subscriptionNode["region"] != "de" || subscriptionNode["source"] != storage.SourceSubscription {
		t.Fatalf("subscription node identity=%#v, want region=de source=subscription", subscriptionNode)
	}
	if connect, ok := subscriptionNode["connect"].(map[string]any); !ok || connect["mode"] != "gateway" {
		t.Fatalf("subscription node connect=%#v, want gateway mode", subscriptionNode["connect"])
	}
	manualNode, ok := nodesByID[manualNodeID]
	if !ok {
		t.Fatalf("manual node id=%d missing from response: %#v", manualNodeID, nodes)
	}
	if manualNode["region"] != "us" || manualNode["source"] != storage.SourceManual {
		t.Fatalf("manual node identity=%#v, want region=us source=manual", manualNode)
	}
	if connect, ok := manualNode["connect"].(map[string]any); !ok || connect["mode"] != "direct" {
		t.Fatalf("manual node connect=%#v, want direct mode", manualNode["connect"])
	}
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

func TestApiV1NodesGatewayHintsPinSameRegionNodesByStableNodeKey(t *testing.T) {
	server, key := newNodesAPITestServer(t, "stable-node-key-hints")
	server.cfg.ProxyAuthUsername = "edge"
	wants := make(map[int64]string, 2)
	for i, nodeKey := range []string{"tunnel/jp/provider-a", "tunnel/jp/provider-b"} {
		id := insertNodesAPIProxy(t, server.storage, storage.Proxy{
			Address:      fmt.Sprintf("127.0.0.1:%d", 24001+i),
			Protocol:     "socks5",
			Region:       "jp",
			DualProtocol: true,
			Status:       "active",
			NodeKey:      nodeKey,
			IPAPIIsScore: -1,
			CFBlocked:    -1,
		})
		wants[id] = nodeKey
	}

	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, nodesAPIRequest(http.MethodGet, "/api/v1/nodes?connect=gateway", key))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	_, _, nodes := decodeNodesResponse(t, rec.Body.Bytes())
	if len(nodes) != len(wants) {
		t.Fatalf("len(nodes)=%d, want %d body=%s", len(nodes), len(wants), rec.Body.String())
	}
	seen := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		id := int64(node["id"].(float64))
		nodeKey, ok := wants[id]
		if !ok {
			t.Fatalf("unexpected node id=%d: %#v", id, node)
		}
		connect := node["connect"].(map[string]any)
		hint, ok := connect["username_hint"].(string)
		if !ok {
			t.Fatalf("gateway node missing username_hint: %#v", node)
		}
		parsed, err := auth.ParseUsername(hint)
		if err != nil {
			t.Fatalf("auth.ParseUsername(%q): %v", hint, err)
		}
		if parsed.Base != "edge" || parsed.Region != "jp" || parsed.Session != "api" {
			t.Fatalf("parsed hint=%#v, want Base=edge Region=jp Session=api", parsed)
		}
		wantNode := "key-" + nodeKey
		wantHint := "edge-region-jp-node-key-" + auth.EncodeNodeKeyPin(nodeKey) + "-session-api"
		if hint != wantHint {
			t.Fatalf("username_hint=%q, want %q", hint, wantHint)
		}
		if parsed.Node != wantNode {
			t.Fatalf("parsed Node=%q, want %q", parsed.Node, wantNode)
		}
		seen[parsed.Node] = true
	}
	if len(seen) != len(wants) {
		t.Fatalf("distinct parsed node pins=%#v, want %d", seen, len(wants))
	}
}

func TestApiV1NodesGatewayHintsResolveOnlyToTheirPinnedNodeKey(t *testing.T) {
	server, key := newNodesAPITestServer(t, "gateway-selector-pin")
	server.cfg.ProxyAuthUsername = "edge"
	wantByID := make(map[int64]string, 2)
	for i, nodeKey := range []string{"selector/jp/a", "selector/jp/b"} {
		id := insertNodesAPIProxy(t, server.storage, storage.Proxy{
			Address:      fmt.Sprintf("127.0.0.1:%d", 24301+i),
			Protocol:     "socks5",
			Region:       "jp",
			DualProtocol: true,
			Status:       "active",
			NodeKey:      nodeKey,
			IPAPIIsScore: -1,
			CFBlocked:    -1,
		})
		wantByID[id] = nodeKey
	}

	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, nodesAPIRequest(http.MethodGet, "/api/v1/nodes?connect=gateway", key))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	_, _, nodes := decodeNodesResponse(t, rec.Body.Bytes())
	if len(nodes) != len(wantByID) {
		t.Fatalf("len(nodes)=%d, want %d body=%s", len(nodes), len(wantByID), rec.Body.String())
	}
	for _, node := range nodes {
		id := int64(node["id"].(float64))
		wantNodeKey, ok := wantByID[id]
		if !ok {
			t.Fatalf("unexpected node id=%d: %#v", id, node)
		}
		connect := node["connect"].(map[string]any)
		hint, ok := connect["username_hint"].(string)
		if !ok {
			t.Fatalf("node id=%d missing username_hint: %#v", id, node)
		}
		route, err := auth.ParseUsername(hint)
		if err != nil {
			t.Fatalf("node id=%d parse hint %q: %v", id, hint, err)
		}
		// 使用真实 selector.Resolve，而不是只比较 DSL 文本；每个 hint 都必须
		// 在数据库中唯一命中其 NodeKey，不能退回同地域随机选路。
		picked, err := selector.Resolve(server.storage, affinity.New(10*time.Minute), route, nil)
		if err != nil {
			t.Fatalf("node id=%d selector.Resolve(%q): %v", id, hint, err)
		}
		if route.Node != "key-"+wantNodeKey {
			t.Fatalf("node id=%d parsed Node=%q, want key-%s", id, route.Node, wantNodeKey)
		}
		if picked.NodeKey != wantNodeKey {
			t.Fatalf("node id=%d hint=%q selected NodeKey=%q, want %q", id, hint, picked.NodeKey, wantNodeKey)
		}
	}
}

func TestApiV1NodesGatewayWithoutNodeKeyReturnsStableHintError(t *testing.T) {
	server, key := newNodesAPITestServer(t, "missing-node-key-hint")
	server.cfg.ProxyAuthUsername = "edge"
	insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address:      "127.0.0.1:24100",
		Protocol:     "socks5",
		Region:       "jp",
		DualProtocol: true,
		Status:       "active",
		Username:     "missing-key-upstream-user",
		Password:     "missing-key-upstream-password",
		Note:         "https://missing-key-subscription.invalid/token",
		IPAPIIsScore: -1,
		CFBlocked:    -1,
	})

	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, nodesAPIRequest(http.MethodGet, "/api/v1/nodes", key))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	_, _, nodes := decodeNodesResponse(t, rec.Body.Bytes())
	if len(nodes) != 1 {
		t.Fatalf("len(nodes)=%d, want 1 body=%s", len(nodes), rec.Body.String())
	}
	connect := nodes[0]["connect"].(map[string]any)
	if hint, ok := connect["username_hint"]; ok {
		t.Fatalf("username_hint=%q, want omitted without stable node key", hint)
	}
	const wantError = "cannot generate username hint: gateway node has no stable node key"
	if got := fmt.Sprint(connect["username_hint_error"]); got != wantError {
		t.Fatalf("username_hint_error=%q, want %q", got, wantError)
	}
	for _, secret := range []string{"127.0.0.1", "24100", "missing-key-upstream-user", "missing-key-upstream-password", "missing-key-subscription.invalid"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Fatalf("missing-key response leaked %q: %s", secret, rec.Body.String())
		}
	}
}

func TestApiV1NodesGatewayHintSurvivesTemporaryPortChange(t *testing.T) {
	server, key := newNodesAPITestServer(t, "port-drift-node-key-hint")
	server.cfg.ProxyAuthUsername = "edge"
	const nodeKey = "tunnel/stable/port-drift"
	id := insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address:      "127.0.0.1:24200",
		Protocol:     "socks5",
		Region:       "jp",
		DualProtocol: true,
		Status:       "active",
		NodeKey:      nodeKey,
		IPAPIIsScore: -1,
		CFBlocked:    -1,
	})

	requestHintAndResolve := func(wantAddress string) string {
		t.Helper()
		rec := httptest.NewRecorder()
		server.routes().ServeHTTP(rec, nodesAPIRequest(http.MethodGet, "/api/v1/nodes", key))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "127.0.0.1") || strings.Contains(rec.Body.String(), "24200") || strings.Contains(rec.Body.String(), "24299") {
			t.Fatalf("response leaked temporary internal address: %s", rec.Body.String())
		}
		_, _, nodes := decodeNodesResponse(t, rec.Body.Bytes())
		if len(nodes) != 1 {
			t.Fatalf("len(nodes)=%d, want 1 body=%s", len(nodes), rec.Body.String())
		}
		hint := fmt.Sprint(nodes[0]["connect"].(map[string]any)["username_hint"])
		route, err := auth.ParseUsername(hint)
		if err != nil {
			t.Fatalf("auth.ParseUsername(%q): %v", hint, err)
		}
		picked, err := selector.Resolve(server.storage, affinity.New(10*time.Minute), route, nil)
		if err != nil {
			t.Fatalf("selector.Resolve(%q): %v", hint, err)
		}
		if picked.NodeKey != nodeKey || picked.Address != wantAddress {
			t.Fatalf("selector.Resolve(%q) = NodeKey %q Address %q, want %q %q", hint, picked.NodeKey, picked.Address, nodeKey, wantAddress)
		}
		return hint
	}

	before := requestHintAndResolve("127.0.0.1:24200")
	if _, err := server.storage.GetDB().Exec(`UPDATE proxies SET address = ? WHERE id = ?`, "127.0.0.1:24299", id); err != nil {
		t.Fatalf("update temporary mixed port: %v", err)
	}
	after := requestHintAndResolve("127.0.0.1:24299")
	want := "edge-region-jp-node-key-" + auth.EncodeNodeKeyPin(nodeKey) + "-session-api"
	if before != want || after != want {
		t.Fatalf("username_hint before/after=%q/%q, want stable %q", before, after, want)
	}
	parsed, err := auth.ParseUsername(after)
	if err != nil || parsed.Node != "key-"+nodeKey {
		t.Fatalf("parsed stable hint=%#v err=%v, want Node=key-%s", parsed, err, nodeKey)
	}
}

func TestGatewayUsernameHintProducesParseableDSL(t *testing.T) {
	const nodeKey = "gateway/helper/stable-key"
	tests := []struct {
		name       string
		region     string
		wantRegion string
		wantError  bool
	}{
		{name: "empty region", region: ""},
		{name: "whitespace region", region: " \t "},
		{name: "non-empty region", region: "KR", wantRegion: "kr"},
		{name: "syntactically valid unassigned code", region: "ZZ", wantRegion: "zz"},
		{name: "legacy unknown region", region: "unknown", wantError: true},
		{name: "three-letter region", region: "USA", wantError: true},
		{name: "alphanumeric region", region: "u1", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hint, err := gatewayUsernameHint("edge", tt.region, nodeKey)
			if tt.wantError {
				if err == nil {
					t.Fatalf("gatewayUsernameHint(edge, %q) returned copyable hint %q; want explicit error", tt.region, hint)
				}
				if hint != "" {
					t.Fatalf("gatewayUsernameHint(edge, %q) hint = %q, want empty on error", tt.region, hint)
				}
				const wantError = "cannot generate username hint: node region must be empty or a 2-letter country code"
				if err.Error() != wantError {
					t.Fatalf("gatewayUsernameHint(edge, %q) error = %q, want %q", tt.region, err, wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("gatewayUsernameHint(edge, %q): %v", tt.region, err)
			}
			wantHint := "edge-node-key-" + auth.EncodeNodeKeyPin(nodeKey) + "-session-api"
			if tt.wantRegion != "" {
				wantHint = "edge-region-" + tt.wantRegion + "-node-key-" + auth.EncodeNodeKeyPin(nodeKey) + "-session-api"
			}
			if hint != wantHint {
				t.Fatalf("gatewayUsernameHint(edge, %q) = %q, want %q", tt.region, hint, wantHint)
			}
			parsed, err := auth.ParseUsername(hint)
			if err != nil {
				t.Fatalf("auth.ParseUsername(%q): %v", hint, err)
			}
			if parsed.Base != "edge" || parsed.Region != tt.wantRegion || parsed.Node != "key-"+nodeKey || parsed.Session != "api" {
				t.Fatalf("parsed hint = %#v, want Base=edge Region=%q Node=key-%s Session=api", parsed, tt.wantRegion, nodeKey)
			}
		})
	}
}

func TestApiV1NodesGatewayHintReportsInvalidStoredRegionWithoutAffectingValidNeighbor(t *testing.T) {
	server, key := newNodesAPITestServer(t, "invalid-region-hint-key")
	server.cfg.ProxyAuthUsername = "edge"
	insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address: "127.0.0.1:22998", Protocol: "socks5", Region: "unknown", DualProtocol: true, Status: "active",
		IPAPIIsScore: -1, CFBlocked: -1, NodeKey: "invalid-region-node",
	})
	insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address: "127.0.0.1:22997", Protocol: "socks5", Region: "kr", DualProtocol: true, Status: "active",
		IPAPIIsScore: -1, CFBlocked: -1, NodeKey: "valid-region-neighbor",
	})

	req := nodesAPIRequest(http.MethodGet, "/api/v1/nodes?connect=gateway", key)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	total, count, nodes := decodeNodesResponse(t, rec.Body.Bytes())
	if total != 2 || count != 2 || len(nodes) != 2 {
		t.Fatalf("total/count/len=%d/%d/%d, want 2/2/2 body=%s", total, count, len(nodes), rec.Body.String())
	}
	var invalidNode, validNode map[string]any
	for _, node := range nodes {
		switch node["region"] {
		case "unknown":
			invalidNode = node
		case "kr":
			validNode = node
		}
	}
	if invalidNode == nil || validNode == nil {
		t.Fatalf("missing expected unknown/kr nodes: %#v", nodes)
	}
	invalidConnect, ok := invalidNode["connect"].(map[string]any)
	if !ok {
		t.Fatalf("invalid node connect missing: %#v", invalidNode)
	}
	if invalidConnect["mode"] != "gateway" {
		t.Fatalf("invalid connect.mode=%v, want gateway", invalidConnect["mode"])
	}
	if hint, ok := invalidConnect["username_hint"]; ok {
		t.Fatalf("connect.username_hint=%q, want omitted for invalid stored region", hint)
	}
	const wantError = "cannot generate username hint: node region must be empty or a 2-letter country code"
	if got := fmt.Sprint(invalidConnect["username_hint_error"]); got != wantError {
		t.Fatalf("connect.username_hint_error=%q, want %q", got, wantError)
	}
	if strings.Contains(rec.Body.String(), `"username_hint":"edge-node-key-`) {
		t.Fatalf("invalid stored region silently fell back to global hint: %s", rec.Body.String())
	}
	validConnect, ok := validNode["connect"].(map[string]any)
	if !ok {
		t.Fatalf("valid neighbor connect missing: %#v", validNode)
	}
	validHint, ok := validConnect["username_hint"].(string)
	wantValidHint := "edge-region-kr-node-key-" + auth.EncodeNodeKeyPin("valid-region-neighbor") + "-session-api"
	if !ok || validHint != wantValidHint {
		t.Fatalf("valid neighbor username_hint=%q, want %q", validHint, wantValidHint)
	}
	parsed, err := auth.ParseUsername(validHint)
	if err != nil {
		t.Fatalf("valid neighbor username_hint=%q is not parseable: %v", validHint, err)
	}
	if parsed.Node != "key-valid-region-neighbor" {
		t.Fatalf("valid neighbor parsed Node=%q, want key-valid-region-neighbor", parsed.Node)
	}
}

func TestApiV1NodesGatewayHintWithoutRegionIsParseable(t *testing.T) {
	server, key := newNodesAPITestServer(t, "empty-region-hint-key")
	server.cfg.ProxyAuthUsername = "edge"
	insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address: "127.0.0.1:22999", Protocol: "socks5", DualProtocol: true, Status: "active",
		IPAPIIsScore: -1, CFBlocked: -1, NodeKey: "empty-region-node",
	})

	req := nodesAPIRequest(http.MethodGet, "/api/v1/nodes?connect=gateway", key)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	_, _, nodes := decodeNodesResponse(t, rec.Body.Bytes())
	if len(nodes) != 1 {
		t.Fatalf("len=%d body=%s", len(nodes), rec.Body.String())
	}
	connect := nodes[0]["connect"].(map[string]any)
	hint := fmt.Sprint(connect["username_hint"])
	parsed, err := auth.ParseUsername(hint)
	if err != nil {
		t.Fatalf("auth.ParseUsername(%q): %v", hint, err)
	}
	if strings.Contains(hint, "-region-any") {
		t.Fatalf("username_hint must omit empty region: %q", hint)
	}
	if parsed.Base != "edge" || parsed.Region != "" || parsed.Node != "key-empty-region-node" || parsed.Session != "api" {
		t.Fatalf("parsed hint = %#v, want Base=edge Region=empty Node=key-empty-region-node Session=api", parsed)
	}
}

func TestApiV1NodesUsernameHintTemplate(t *testing.T) {
	server, key := newNodesAPITestServer(t, "hint-nodes-key")
	server.cfg.ProxyAuthUsername = "edge"
	insertNodesAPIProxy(t, server.storage, storage.Proxy{
		Address: "127.0.0.1:23000", Protocol: "socks5", Region: "kr", DualProtocol: true, Status: "active",
		IPAPIIsScore: -1, CFBlocked: -1, NodeKey: "template-node-key",
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
	wantHint := "edge-region-kr-node-key-" + auth.EncodeNodeKeyPin("template-node-key") + "-session-api"
	if hint != wantHint {
		t.Fatalf("username_hint = %q, want %q", hint, wantHint)
	}
}
