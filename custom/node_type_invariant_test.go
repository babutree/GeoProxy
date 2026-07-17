package custom

import (
	"strings"
	"testing"
)

// TestParsePassesImpliesBuildPasses 是 BUG-1 的核心不变量：
// 一个通过 parseClashProxy 的节点，若其类型属于 sing-box 隧道类型，
// 必须也能通过 buildOutbound——不允许“解析通过但构建失败”的静默错配
// （典型历史缺陷：ssr 解析通过却在 buildOutbound 无对应 case 而被拒）。
//
// 仅用合成节点，绝不含任何真实订阅/凭据数据。
func TestParsePassesImpliesBuildPasses(t *testing.T) {
	// 每条用例是一个合成 Clash 代理 map；期望 parse 通过且（若非直连）build 通过。
	cases := []struct {
		name  string
		proxy map[string]interface{}
	}{
		{
			name: "vmess-ws",
			proxy: map[string]interface{}{
				"type": "vmess", "name": "vmess-ws", "server": "a.example.com", "port": 443,
				"uuid": "11111111-1111-1111-1111-111111111111", "alterId": 0, "cipher": "auto",
				"tls": true, "sni": "a.example.com", "network": "ws",
				"ws-opts": map[string]interface{}{"path": "/ws"},
			},
		},
		{
			name: "vless-reality-no-fingerprint",
			proxy: map[string]interface{}{
				"type": "vless", "name": "vless-reality", "server": "b.example.com", "port": 443,
				"uuid": "22222222-2222-2222-2222-222222222222", "flow": "xtls-rprx-vision",
				"tls": true, "sni": "b.example.com",
				"reality-opts": map[string]interface{}{"public-key": "PUBKEY", "short-id": "abcd"},
			},
		},
		{
			name: "trojan-tcp",
			proxy: map[string]interface{}{
				"type": "trojan", "name": "trojan-tcp", "server": "c.example.com", "port": 443,
				"password": "REDACTED", "sni": "c.example.com",
			},
		},
		{
			name: "shadowsocks",
			proxy: map[string]interface{}{
				"type": "ss", "name": "ss-node", "server": "d.example.com", "port": 8388,
				"cipher": "aes-256-gcm", "password": "REDACTED",
			},
		},
		{
			name: "hysteria2",
			proxy: map[string]interface{}{
				"type": "hysteria2", "name": "hy2", "server": "e.example.com", "port": 443,
				"password": "REDACTED", "sni": "e.example.com",
			},
		},
		{
			name: "tuic",
			proxy: map[string]interface{}{
				"type": "tuic", "name": "tuic", "server": "f.example.com", "port": 443,
				"uuid": "33333333-3333-3333-3333-333333333333", "password": "REDACTED", "sni": "f.example.com",
			},
		},
		{
			name: "vmess-grpc",
			proxy: map[string]interface{}{
				"type": "vmess", "name": "vmess-grpc", "server": "g.example.com", "port": 443,
				"uuid": "44444444-4444-4444-4444-444444444444", "alterId": 0, "cipher": "auto",
				"tls": true, "sni": "g.example.com", "network": "grpc",
				"grpc-opts": map[string]interface{}{"grpc-service-name": "gs"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node, err := parseClashProxy(tc.proxy)
			if err != nil {
				t.Fatalf("parseClashProxy() error = %v (node should parse)", err)
			}
			if node.IsDirect() {
				return // 直连节点不经 buildOutbound
			}
			out, err := buildOutbound(*node, "node-0")
			if err != nil {
				t.Fatalf("节点 %q 解析通过但 buildOutbound 失败: %v (parse-passes-build-fails 不变量被破坏)", tc.name, err)
			}
			if out["type"] == nil {
				t.Fatalf("节点 %q 出站缺少 type 字段: %v", tc.name, out)
			}
		})
	}
}

// TestShadowsocksrRejectedAtParse 验证 ssr 在 parse 阶段被明确拒绝，
// 不再“解析通过后在 buildOutbound 死掉”（sing-box 1.13 无原生 shadowsocksr）。
func TestShadowsocksrRejectedAtParse(t *testing.T) {
	_, err := parseClashProxy(map[string]interface{}{
		"type": "ssr", "name": "ssr-node", "server": "h.example.com", "port": 8388,
		"cipher": "aes-256-cfb", "password": "REDACTED",
		"protocol": "auth_aes128_md5", "obfs": "tls1.2_ticket_auth",
	})
	if err == nil {
		t.Fatal("ssr 节点应在 parse 阶段被拒绝（sing-box 1.13 无原生 shadowsocksr）")
	}
}

// TestRealityNodeGetsDefaultFingerprint 验证含 reality-opts 但缺 client-fingerprint 的节点
// 会被赋予默认 utls fingerprint（避免 sing-box 拒绝）。
func TestRealityNodeGetsDefaultFingerprint(t *testing.T) {
	node, err := parseClashProxy(map[string]interface{}{
		"type": "vless", "name": "reality", "server": "i.example.com", "port": 443,
		"uuid": "55555555-5555-5555-5555-555555555555",
		"tls":  true, "sni": "i.example.com",
		"reality-opts": map[string]interface{}{"public-key": "PK", "short-id": "sid"},
	})
	if err != nil {
		t.Fatalf("parseClashProxy() error = %v", err)
	}
	out, err := buildOutbound(*node, "node-0")
	if err != nil {
		t.Fatalf("buildOutbound() error = %v", err)
	}
	tls, ok := out["tls"].(map[string]interface{})
	if !ok {
		t.Fatalf("reality 出站缺少 tls 段: %v", out)
	}
	utls, ok := tls["utls"].(map[string]interface{})
	if !ok {
		t.Fatalf("reality 出站缺少默认 utls fingerprint: %v", tls)
	}
	if fp := utls["fingerprint"]; fp == "" || fp == nil {
		t.Fatalf("reality 默认 utls fingerprint 为空: %v", utls)
	}
	if _, hasReality := tls["reality"]; !hasReality {
		t.Fatalf("reality 出站缺少 reality 段: %v", tls)
	}
}

// TestUnsupportedTransportStillRejectedAtBuild 确认收紧不变量后，
// 真正不支持的传输层（xhttp）仍在 build 阶段被拒（不被误放行）。
func TestUnsupportedTransportStillRejectedAtBuild(t *testing.T) {
	node, err := parseClashProxy(map[string]interface{}{
		"type": "vless", "name": "xhttp", "server": "j.example.com", "port": 443,
		"uuid": "66666666-6666-6666-6666-666666666666", "network": "xhttp",
	})
	if err != nil {
		t.Fatalf("parseClashProxy() error = %v (xhttp 应能解析，仅在 build 拒绝)", err)
	}
	if _, err := buildOutbound(*node, "node-0"); err == nil {
		t.Fatal("xhttp 传输层应在 buildOutbound 被拒绝")
	} else if !strings.Contains(err.Error(), "xhttp") {
		t.Fatalf("错误信息应包含传输层名 xhttp, 得到: %v", err)
	}
}
