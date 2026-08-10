package custom

import (
	"os"
	"path/filepath"
	"testing"
)

// loadSingBoxFixture 读取真实 sing-box 订阅样本（testdata/singbox.json）。
func loadSingBoxFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "singbox.json"))
	if err != nil {
		t.Fatalf("读取 fixture 失败: %v", err)
	}
	return data
}

// TestParseSingBoxJSON_RealFixture_ExactCount 硬性验收：
// 合成 sing-box 配置样本恰好解析出 7 个代理节点，且不含 direct/selector。
func TestParseSingBoxJSON_RealFixture_ExactCount(t *testing.T) {
	data := loadSingBoxFixture(t)

	nodes, err := parseSingBoxJSON(data)
	if err != nil {
		t.Fatalf("parseSingBoxJSON 出错: %v", err)
	}

	// 硬指标：9 个 outbounds - 1 selector - 1 direct = 7。
	if len(nodes) != 7 {
		t.Fatalf("期望 7 个节点，实际 %d 个", len(nodes))
	}
	t.Logf("PASS: sing-box fixture 解析出恰好 %d 个节点", len(nodes))

	// 断言：不含 direct / selector（以及其他非代理类型）。
	for _, n := range nodes {
		if n.Type == "direct" || n.Type == "selector" || n.Type == "urltest" ||
			n.Type == "block" || n.Type == "dns" {
			t.Errorf("解析结果不应包含非代理类型，但发现 type=%s name=%s", n.Type, n.Name)
		}
	}

	// 断言：每个节点 Server / Port 非空，Name(tag) 非空。
	for _, n := range nodes {
		if n.Server == "" {
			t.Errorf("节点 %s Server 为空", n.Name)
		}
		if n.Port == 0 {
			t.Errorf("节点 %s Port 为 0", n.Name)
		}
		if n.Name == "" {
			t.Errorf("发现 Name(tag) 为空的节点: server=%s", n.Server)
		}
	}

	// 断言：type 分布符合 fixture 构成
	// vless=3, shadowsocks=1, http=1, trojan=1, socks(→socks5)=1。
	counts := map[string]int{}
	for _, n := range nodes {
		counts[n.Type]++
	}
	expected := map[string]int{
		"vless":       3,
		"shadowsocks": 1,
		"http":        1,
		"trojan":      1,
		"socks5":      1, // sing-box "socks" 归一为 socks5
	}
	for typ, want := range expected {
		if counts[typ] != want {
			t.Errorf("type=%s 期望 %d 个，实际 %d 个", typ, want, counts[typ])
		}
	}
	t.Logf("type 分布: %+v", counts)
}

// TestParseSingBoxJSON_FieldMapping 验证字段映射正确（tag→Name、server_port→Port、
// transport/tls 映射为 Clash 风格 Raw）。
func TestParseSingBoxJSON_FieldMapping(t *testing.T) {
	data := loadSingBoxFixture(t)
	nodes, err := parseSingBoxJSON(data)
	if err != nil {
		t.Fatalf("parseSingBoxJSON 出错: %v", err)
	}

	byName := map[string]ParsedNode{}
	for _, n := range nodes {
		byName[n.Name] = n
	}

	// vless + ws transport（无 TLS）
	if n, ok := byName["US-East Los Angeles-42816513-tyte"]; ok {
		if n.Type != "vless" || n.Server != "node1.example.com" || n.Port != 8880 {
			t.Errorf("vless 节点基础字段错误: %+v", n)
		}
		if n.Raw["uuid"] != "00000000-0000-4000-8000-000000000001" {
			t.Errorf("vless uuid 映射错误: %v", n.Raw["uuid"])
		}
		if n.Raw["network"] != "ws" {
			t.Errorf("transport type 应映射为 network=ws，实际: %v", n.Raw["network"])
		}
		wsOpts, ok := n.Raw["ws-opts"].(map[string]interface{})
		if !ok {
			t.Fatalf("ws-opts 缺失或类型错误: %v", n.Raw["ws-opts"])
		}
		if wsOpts["path"] != "/path1" {
			t.Errorf("ws path 映射错误: %v", wsOpts["path"])
		}
	} else {
		t.Error("未找到 vless ws 节点 US-East Los Angeles-42816513-tyte")
	}

	// vless + reality（NL-Meppel）
	if n, ok := byName["NL-Meppel-h-7122721-tuqg"]; ok {
		if n.Raw["tls"] != true {
			t.Errorf("reality 节点应有 tls=true: %v", n.Raw["tls"])
		}
		if n.Raw["flow"] != "xtls-rprx-vision" {
			t.Errorf("flow 映射错误: %v", n.Raw["flow"])
		}
		ro, ok := n.Raw["reality-opts"].(map[string]interface{})
		if !ok {
			t.Fatalf("reality-opts 缺失: %v", n.Raw["reality-opts"])
		}
		if ro["public-key"] != "FAKEPUBLICKEY48" {
			t.Errorf("reality public-key 映射错误: %v", ro["public-key"])
		}
		if n.Raw["client-fingerprint"] != "firefox" {
			t.Errorf("utls fingerprint 映射错误: %v", n.Raw["client-fingerprint"])
		}
	} else {
		t.Error("未找到 reality 节点 NL-Meppel-h-7122721-tuqg")
	}

	// shadowsocks：method→cipher
	if n, ok := byName["US-Columbus-h-11916410-teuz"]; ok {
		if n.Type != "shadowsocks" {
			t.Errorf("类型错误: %v", n.Type)
		}
		if n.Raw["cipher"] != "aes-256-cfb" {
			t.Errorf("method 应映射为 cipher: %v", n.Raw["cipher"])
		}
		if n.Raw["password"] != "test-password-23" {
			t.Errorf("password 映射错误: %v", n.Raw["password"])
		}
	} else {
		t.Error("未找到 shadowsocks 节点 US-Columbus-h-11916410-teuz")
	}

	// http：应为直连类型
	if n, ok := byName["US-New York City-6146591-tyqu"]; ok {
		if n.Type != "http" {
			t.Errorf("类型错误: %v", n.Type)
		}
		if !n.IsDirect() {
			t.Errorf("http 节点 IsDirect() 应为 true")
		}
		if n.Server != "node2.example.com" || n.Port != 9002 {
			t.Errorf("http server/port 错误: %s:%d", n.Server, n.Port)
		}
	} else {
		t.Error("未找到 http 节点 US-New York City-6146591-tyqu")
	}

	// socks：归一为 socks5 且 IsDirect
	if n, ok := byName["HK-Hong Kong-194331-txlu"]; ok {
		if n.Type != "socks5" {
			t.Errorf("socks 应归一为 socks5，实际: %v", n.Type)
		}
		if !n.IsDirect() {
			t.Errorf("socks5 节点 IsDirect() 应为 true")
		}
		if n.DirectProtocol() != "socks5" {
			t.Errorf("DirectProtocol 错误: %v", n.DirectProtocol())
		}
	} else {
		t.Error("未找到 socks 节点 HK-Hong Kong-194331-txlu")
	}

	// vless + grpc transport
	if n, ok := byName["DE-Frankfurt am Main-h-12442539-tuyl"]; ok {
		if n.Raw["network"] != "grpc" {
			t.Errorf("grpc transport 应映射为 network=grpc: %v", n.Raw["network"])
		}
		go2, ok := n.Raw["grpc-opts"].(map[string]interface{})
		if !ok {
			t.Fatalf("grpc-opts 缺失: %v", n.Raw["grpc-opts"])
		}
		if go2["grpc-service-name"] != "media.session.poll" {
			t.Errorf("grpc service_name 映射错误: %v", go2["grpc-service-name"])
		}
	} else {
		t.Error("未找到 grpc 节点 DE-Frankfurt am Main-h-12442539-tuyl")
	}
}

// TestParseSingBoxJSON_BuildOutboundConvertible 验证提取出的 tunnel 节点
// 能被 buildOutbound 无错转换（纯函数，不依赖 sing-box 二进制）。
// 这证明字段映射真正对接下游，而非仅"解析入库"。
func TestParseSingBoxJSON_BuildOutboundConvertible(t *testing.T) {
	data := loadSingBoxFixture(t)
	nodes, err := parseSingBoxJSON(data)
	if err != nil {
		t.Fatalf("parseSingBoxJSON 出错: %v", err)
	}

	convertible := 0
	var failures []string
	for i, n := range nodes {
		if n.IsDirect() {
			continue // http/socks5 走直连路径，不经 buildOutbound
		}
		out, err := buildOutbound(n, "test")
		if err != nil {
			failures = append(failures, n.Name+": "+err.Error())
			continue
		}
		if out["server"] != n.Server {
			t.Errorf("节点 #%d server 未正确写入 outbound", i)
		}
		if out["server_port"] != n.Port {
			t.Errorf("节点 #%d server_port 未正确写入 outbound", i)
		}
		convertible++
	}

	// fixture 中所有 tunnel 节点（vless/trojan/shadowsocks）均为 sing-box 支持的
	// 传输层（ws/grpc/tcp），应全部可转换。
	if len(failures) > 0 {
		t.Errorf("以下 tunnel 节点 buildOutbound 转换失败:\n%v", failures)
	}
	t.Logf("PASS: %d 个 tunnel 节点成功通过 buildOutbound 转换（生成 sing-box outbound map）", convertible)
}

// --- 负向 / 边界用例 ---

func TestParseSingBoxJSON_InvalidJSON(t *testing.T) {
	_, err := parseSingBoxJSON([]byte(`{ "outbounds": [ this is not json `))
	if err == nil {
		t.Fatal("非法 JSON 应返回错误")
	}
	t.Logf("非法 JSON 错误信息: %v", err)
}

func TestParseSingBoxJSON_NoOutbounds(t *testing.T) {
	_, err := parseSingBoxJSON([]byte(`{"log":{"level":"info"},"inbounds":[]}`))
	if err == nil {
		t.Fatal("无 outbounds 的 JSON 应返回明确错误")
	}
	t.Logf("无 outbounds 错误信息: %v", err)
}

func TestParseSingBoxJSON_OnlyNonProxy(t *testing.T) {
	// outbounds 全是 selector/direct/block → 0 代理，应返回错误。
	cfg := `{"outbounds":[
		{"type":"selector","tag":"sel","outbounds":["a","b"]},
		{"type":"direct","tag":"direct"},
		{"type":"block","tag":"block"}
	]}`
	nodes, err := parseSingBoxJSON([]byte(cfg))
	if err == nil {
		t.Fatalf("全非代理 outbounds 应返回错误，却得到 %d 个节点", len(nodes))
	}
	if len(nodes) != 0 {
		t.Fatalf("全非代理 outbounds 应得到 0 节点，实际 %d", len(nodes))
	}
	t.Logf("全非代理错误信息: %v", err)
}

func TestParseSingBoxJSONRejectsMissingServer(t *testing.T) {
	cfg := `{"outbounds":[
		{"type":"vless","tag":"good","server":"a.com","server_port":443,"uuid":"u1"},
		{"type":"vless","tag":"noserver","server_port":443,"uuid":"u2"}
	]}`
	if _, err := parseSingBoxJSON([]byte(cfg)); err == nil {
		t.Fatal("recognized proxy without server must fail closed")
	}
}

func TestParseSingBoxJSON_EmptyContent(t *testing.T) {
	_, err := parseSingBoxJSON([]byte("   "))
	if err == nil {
		t.Fatal("空内容应返回错误")
	}
	t.Logf("空内容错误信息: %v", err)
}

// --- 自动检测 & Parse 分派 ---

func TestParseAutoDetect_SingBoxJSON(t *testing.T) {
	data := loadSingBoxFixture(t)
	nodes, err := parseAutoDetect(data)
	if err != nil {
		t.Fatalf("parseAutoDetect 对 sing-box JSON 出错: %v", err)
	}
	if len(nodes) != 7 {
		t.Fatalf("parseAutoDetect 期望 7 个节点，实际 %d", len(nodes))
	}
	t.Logf("PASS: parseAutoDetect 自动识别 sing-box JSON，得到 %d 个节点", len(nodes))
}

func TestParse_ExplicitSingBoxFormat(t *testing.T) {
	data := loadSingBoxFixture(t)
	nodes, err := Parse(data, "singbox")
	if err != nil {
		t.Fatalf("Parse(singbox) 出错: %v", err)
	}
	if len(nodes) != 7 {
		t.Fatalf("Parse(singbox) 期望 7 个节点，实际 %d", len(nodes))
	}
	t.Logf("PASS: Parse(data, \"singbox\") 得到 %d 个节点", len(nodes))
}

func TestParse_AutoFormatFallsToSingBox(t *testing.T) {
	data := loadSingBoxFixture(t)
	// 模拟历史调用方传 "auto"（migration/前端默认）。
	nodes, err := Parse(data, "auto")
	if err != nil {
		t.Fatalf("Parse(auto) 出错: %v", err)
	}
	if len(nodes) != 7 {
		t.Fatalf("Parse(auto) 期望 7 个节点，实际 %d", len(nodes))
	}
}

func TestParseAutoDetect_EmptyContent(t *testing.T) {
	_, err := parseAutoDetect([]byte("   "))
	if err == nil {
		t.Fatal("空内容应返回错误")
	}
	t.Logf("空内容错误信息: %v", err)
}

func TestParseAutoDetect_UnknownJSON(t *testing.T) {
	// JSON 合法但非 sing-box 结构 → 明确错误（BUG-63）。
	_, err := parseAutoDetect([]byte(`{"foo":"bar","baz":123}`))
	if err == nil {
		t.Fatal("未知 JSON 结构应返回错误")
	}
	t.Logf("未知 JSON 结构错误信息: %v", err)
}

// --- 回归：Clash YAML / 协议链接 / base64 不受影响 ---

func TestParse_ClashYAMLRegression(t *testing.T) {
	yaml := `proxies:
  - name: ss-node
    type: ss
    server: 1.2.3.4
    port: 8388
    cipher: aes-256-gcm
    password: pass123
  - name: trojan-node
    type: trojan
    server: example.com
    port: 443
    password: tj-pass
`
	nodes, err := Parse([]byte(yaml), "clash")
	if err != nil {
		t.Fatalf("Clash YAML 解析出错: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("期望 2 个 Clash 节点，实际 %d", len(nodes))
	}
	if nodes[0].Type != "shadowsocks" {
		t.Errorf("ss 应归一为 shadowsocks，实际 %s", nodes[0].Type)
	}
	t.Logf("PASS: Clash YAML 回归正常，%d 个节点", len(nodes))
}

func TestParse_ProxyLinksRegression(t *testing.T) {
	links := "trojan://mypass@example.com:443#MyTrojan\n" +
		"vless://uuid-1234@vless.example.com:443?security=tls&sni=a.com#MyVless"
	nodes, err := Parse([]byte(links), "links")
	if err != nil {
		t.Fatalf("协议链接解析出错: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("期望 2 个链接节点，实际 %d", len(nodes))
	}
	t.Logf("PASS: 协议链接回归正常，%d 个节点", len(nodes))
}

func TestParse_AutoDetectClashRegression(t *testing.T) {
	// auto 模式对 Clash YAML 仍正常（不被 JSON 分支误判）。
	yaml := "proxies:\n  - {name: n1, type: vmess, server: s.com, port: 443, uuid: u, alterId: 0, cipher: auto}\n"
	nodes, err := parseAutoDetect([]byte(yaml))
	if err != nil {
		t.Fatalf("auto Clash 出错: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("期望 1 个节点，实际 %d", len(nodes))
	}
}
