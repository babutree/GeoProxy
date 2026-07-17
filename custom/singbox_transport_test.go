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

// TestBuildOutboundSupportedTransport 验证受支持的传输层（ws / 裸 tcp / http / raw）正常生成出站配置。
// 大量 clash-meta 订阅使用 network=http（HTTP 传输）与 network=raw（=tcp）；
// 此前被 default 分支误拒为「不支持的传输层」，导致可用节点被成批跳过。
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

	// clash-meta network=http → sing-box transport.type=http（读 http-opts；path/host 可能是列表）
	httpNode := ParsedNode{
		Name:   "http-transport",
		Type:   "vless",
		Server: "example.com",
		Port:   443,
		Raw: map[string]interface{}{
			"type":    "vless",
			"uuid":    "347b77a2-dbf7-4755-adb9-64ef05f51e84",
			"tls":     true,
			"sni":     "example.com",
			"network": "http",
			"http-opts": map[string]interface{}{
				"path": []interface{}{"/"},
				"host": []interface{}{"example.com"},
			},
		},
	}
	out, err = buildOutbound(httpNode, "node-2")
	if err != nil {
		t.Fatalf("network=http buildOutbound() error = %v (clash-meta HTTP 传输应被支持)", err)
	}
	httpTransport, ok := out["transport"].(map[string]interface{})
	if !ok {
		t.Fatalf("network=http 出站缺少 transport: %v", out)
	}
	if httpTransport["type"] != "http" {
		t.Fatalf("network=http transport.type = %v, want http", httpTransport["type"])
	}
	if httpTransport["path"] != "/" {
		t.Fatalf("network=http transport.path = %v, want /", httpTransport["path"])
	}
	if hosts, ok := httpTransport["host"].([]string); !ok || len(hosts) != 1 || hosts[0] != "example.com" {
		t.Fatalf("network=http transport.host = %v, want [example.com]", httpTransport["host"])
	}

	// 仅 headers.Host、无 host 字段时也应映射
	httpHdrNode := ParsedNode{
		Name:   "http-headers-host",
		Type:   "vless",
		Server: "example.com",
		Port:   443,
		Raw: map[string]interface{}{
			"type":    "vless",
			"uuid":    "347b77a2-dbf7-4755-adb9-64ef05f51e84",
			"tls":     true,
			"sni":     "example.com",
			"network": "http",
			"http-opts": map[string]interface{}{
				"path":    "/",
				"headers": map[string]interface{}{"Host": "cdn.example.com"},
			},
		},
	}
	out, err = buildOutbound(httpHdrNode, "node-2b")
	if err != nil {
		t.Fatalf("network=http headers.Host buildOutbound() error = %v", err)
	}
	httpHdrTransport, ok := out["transport"].(map[string]interface{})
	if !ok {
		t.Fatalf("headers.Host 出站缺少 transport: %v", out)
	}
	if hosts, ok := httpHdrTransport["host"].([]string); !ok || len(hosts) != 1 || hosts[0] != "cdn.example.com" {
		t.Fatalf("headers.Host transport.host = %v, want [cdn.example.com]", httpHdrTransport["host"])
	}

	// Xray/v2rayN network=raw 语义等同裸 TCP
	rawNode := ParsedNode{
		Name:   "raw-tcp",
		Type:   "vless",
		Server: "example.com",
		Port:   443,
		Raw: map[string]interface{}{
			"type":    "vless",
			"uuid":    "347b77a2-dbf7-4755-adb9-64ef05f51e84",
			"tls":     true,
			"sni":     "example.com",
			"network": "raw",
		},
	}
	out, err = buildOutbound(rawNode, "node-3")
	if err != nil {
		t.Fatalf("network=raw buildOutbound() error = %v (raw 应映射为裸 TCP)", err)
	}
	if _, exists := out["transport"]; exists {
		t.Fatalf("network=raw 不应有 transport 字段: %v", out["transport"])
	}
}

// TestIncompletePortAllocationErrorDiagnosesBuildFailure 验证 buildOutbound
// 失败不能被误报为端口段满或配置跳过。
func TestIncompletePortAllocationErrorDiagnosesBuildFailure(t *testing.T) {
	s := NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort)
	good := tunnelNode("good", "good.example.com", "good-password")
	bad := unsupportedXHTTPNode()

	_, portMap := s.assembleConfig([]ParsedNode{good, bad})
	err := incompletePortAllocationError([]ParsedNode{good, bad}, portMap)
	if err == nil {
		t.Fatal("坏节点未分配端口时应返回错误")
	}
	msg := err.Error()
	for _, want := range []string{"坏 transport 节点", "vless", "xhttp"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("buildOutbound 诊断应包含 %q，实际: %s", want, msg)
		}
	}
	if strings.Contains(msg, "段满") {
		t.Fatalf("buildOutbound 失败不应被误报为段满，实际: %s", msg)
	}
}

// TestReloadAllRejectedNodesDiagnosesBuildFailureBeforeBinaryLookup：
// 全部节点构建失败时必须显式失败并报告构建原因，不得被缺失二进制掩盖。
func TestReloadAllRejectedNodesDiagnosesBuildFailureBeforeBinaryLookup(t *testing.T) {
	s := NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort)
	err := s.Reload([]ParsedNode{unsupportedXHTTPNode()})
	if err == nil {
		t.Fatal("全部坏节点应使 Reload 失败")
	}
	msg := err.Error()
	for _, want := range []string{"坏 transport 节点", "vless", "xhttp"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("Reload 诊断应包含 %q，实际: %s", want, msg)
		}
	}
	if strings.Contains(msg, "未找到") || strings.Contains(msg, "binary") {
		t.Fatalf("构建失败应先于二进制检查暴露，实际: %s", msg)
	}
}

// TestReloadMixedBuildFailureDoesNotBlockGoodNodes：单坏节点不得阻断同批可构建节点。
// 缺二进制时错误应来自 good 节点启动路径，而非把 bad 当 incomplete 阻断。
func TestReloadMixedBuildFailureDoesNotBlockGoodNodes(t *testing.T) {
	s := NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort)
	good := tunnelNode("good", "good.example.com", "good-password")
	bad := unsupportedXHTTPNode()
	err := s.Reload([]ParsedNode{good, bad})
	if err == nil {
		t.Fatal("缺二进制时 Reload 应失败")
	}
	// 应推进到二进制检查：说明 bad 未把整批判为端口不完整。
	if !strings.Contains(err.Error(), "未找到") {
		t.Fatalf("混合批应允许 good 进入启动路径，得到: %v", err)
	}
	if strings.Contains(err.Error(), "端口分配不完整") {
		t.Fatalf("坏节点不应把同批 good 判为 incomplete，得到: %v", err)
	}
}

func unsupportedXHTTPNode() ParsedNode {
	return ParsedNode{
		Name:   "坏 transport 节点",
		Type:   "vless",
		Server: "bad.example.com",
		Port:   443,
		Raw: map[string]interface{}{
			"type":    "vless",
			"uuid":    "347b77a2-dbf7-4755-adb9-64ef05f51e84",
			"network": "xhttp",
		},
	}
}
