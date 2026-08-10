package custom

import (
	"testing"

	"github.com/babutree/GeoProxy/storage"
	"github.com/babutree/GeoProxy/validator"
)

// 刷新内容含非法端点时必须 fail-closed，不能把解析阶段跳过的旧节点误判为已删除。
func TestRefreshSubscriptionRejectsInvalidEndpointWithoutRemovingOldProxy(t *testing.T) {
	store := newTestStorage(t)
	content := "proxies:\n" +
		"  - name: valid\n" +
		"    type: http\n" +
		"    server: new.example.com\n" +
		"    port: 8080\n" +
		"  - name: invalid\n" +
		"    type: http\n" +
		"    server: old.example.com\n" +
		"    port: 0\n"
	subID, err := store.AddSubscription("invalid-endpoint", "", writeSubscriptionFile(t, content), "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if err := store.AddProxyWithSource("old.example.com:8080", "http", storage.SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}
	if _, err := store.GetDB().Exec(`UPDATE proxies SET status = 'active' WHERE address = 'old.example.com:8080'`); err != nil {
		t.Fatalf("seed old proxy status: %v", err)
	}

	m := &Manager{
		storage:   store,
		validator: validator.New(1, 1, "http://127.0.0.1/validate"),
		singbox:   NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort),
	}
	if err := m.RefreshSubscription(subID); err == nil {
		t.Fatal("RefreshSubscription() error = nil, want invalid endpoint rejection")
	}
	if _, err := store.GetProxyByAddress("old.example.com:8080"); err != nil {
		t.Fatalf("old proxy removed after invalid endpoint refresh: %v", err)
	}
}

func TestRefreshSubscriptionReplacesOldProxyWhenAllEndpointsValid(t *testing.T) {
	store := newTestStorage(t)
	content := "proxies:\n" +
		"  - name: replacement\n" +
		"    type: http\n" +
		"    server: new.example.com\n" +
		"    port: 8080\n"
	subID, err := store.AddSubscription("valid-replacement", "", writeSubscriptionFile(t, content), "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if err := store.AddProxyWithSource("old.example.com:8080", "http", storage.SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}

	m := &Manager{
		storage:   store,
		validator: validator.New(1, 1, "http://127.0.0.1/validate"),
		singbox:   NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort),
	}
	if err := m.RefreshSubscription(subID); err != nil {
		t.Fatalf("RefreshSubscription() error = %v", err)
	}
	if _, err := store.GetProxyByAddress("old.example.com:8080"); err == nil {
		t.Fatal("old proxy still exists after all-valid refresh")
	}
	if _, err := store.GetProxyByAddress("new.example.com:8080"); err != nil {
		t.Fatalf("new proxy missing after all-valid refresh: %v", err)
	}
}

func TestParseRejectsMixedInvalidEndpoints(t *testing.T) {
	cases := []struct {
		name   string
		format string
		data   string
	}{
		{"clash", "clash", "proxies:\n  - {name: good, type: http, server: good.example, port: 8080}\n  - {name: bad, type: http, server: bad.example, port: 0}\n"},
		{"plain", "plain", "http://good.example:8080\nsocks5://bad.example:0\n"},
		{"protocol-links", "links", "vless://123e4567-e89b-12d3-a456-426614174000@good.example:443?security=tls\nvless://123e4567-e89b-12d3-a456-426614174000@bad.example:0?security=tls\n"},
		{"singbox-json", "singbox", `{"outbounds":[{"type":"vless","server": "good.example","server_port":443,"uuid": "u1"},{"type": "vless","server": "bad.example","server_port":0,"uuid": "u2"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nodes, err := Parse([]byte(tc.data), tc.format)
			if err == nil {
				t.Fatalf("Parse() nodes = %+v, error = nil; want fail-closed endpoint error", nodes)
			}
		})
	}
}
