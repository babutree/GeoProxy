package custom

import (
	"strings"
	"testing"
)

// TestBuildOutboundUnsupportedTransport 验证 sing-box 不支持的传输层（Xray 私有 xhttp/splithttp）
// 被显式拒绝，返回错误而非静默丢弃 transport 生成假配置。
func TestBuildOutboundUnsupportedTransport(t *testing.T) {
	for _, network := range []string{"xhttp", "splithttp"} {
		node := ParsedNode{
			Name:   "unsupported-" + network,
			Type:   "vless",
			Server: "example.com",
			Port:   443,
			Raw: map[string]interface{}{
				"type":    "vless",
				"uuid":    "347b77a2-dbf7-4755-adb9-64ef05f51e84",
				"tls":     true,
				"sni":     "update.microsoft.com",
				"network": network,
			},
		}
		out, err := buildOutbound(node, "node-0")
		if err == nil {
			t.Fatalf("network=%s: buildOutbound() 期望错误, 得到 nil (out=%v)", network, out)
		}
		if out != nil {
			t.Fatalf("network=%s: 出错时 out 应为 nil, 得到 %v", network, out)
		}
		if !strings.Contains(err.Error(), network) {
			t.Fatalf("network=%s: 错误信息应包含传输层名, 得到 %q", network, err.Error())
		}
	}
}

// TestBuildOutboundSupportedTransport 验证受支持的传输层（ws / 裸 tcp）正常生成出站配置。
func TestBuildOutboundSupportedTransport(t *testing.T) {
	// ws 传输应写入 transport.type=ws
	wsNode := ParsedNode{
		Name:   "ws-node",
		Type:   "trojan",
		Server: "example.com",
		Port:   443,
		Raw: map[string]interface{}{
			"type":     "trojan",
			"password": "secret",
			"tls":      true,
			"sni":      "example.com",
			"network":  "ws",
			"ws-opts":  map[string]interface{}{"path": "/path"},
		},
	}
	out, err := buildOutbound(wsNode, "node-0")
	if err != nil {
		t.Fatalf("ws buildOutbound() error = %v", err)
	}
	transport, ok := out["transport"].(map[string]interface{})
	if !ok {
		t.Fatalf("ws 出站缺少 transport 字段: %v", out)
	}
	if transport["type"] != "ws" {
		t.Fatalf("ws transport.type = %v, want ws", transport["type"])
	}

	// 裸 tcp（无 network 字段）不应带 transport
	tcpNode := ParsedNode{
		Name:   "tcp-node",
		Type:   "trojan",
		Server: "example.com",
		Port:   443,
		Raw: map[string]interface{}{
			"type":     "trojan",
			"password": "secret",
			"tls":      true,
			"sni":      "example.com",
		},
	}
	out, err = buildOutbound(tcpNode, "node-1")
	if err != nil {
		t.Fatalf("tcp buildOutbound() error = %v", err)
	}
	if _, exists := out["transport"]; exists {
		t.Fatalf("裸 tcp 出站不应有 transport 字段: %v", out["transport"])
	}
}
