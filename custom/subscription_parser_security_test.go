package custom

import (
	"bytes"
	"encoding/base64"
	"log"
	"strings"
	"testing"
)

// 订阅内容可携带节点密码；解析日志只能记录结构性信息，不能回显原文。
func TestParseClashDoesNotLogCredentialBearingContent(t *testing.T) {
	const secret = "never-log-this-subscription-password"
	data := []byte("proxies:\n" +
		"  - name: credential-node\n" +
		"    type: ss\n" +
		"    password: " + secret + "\n" +
		"    server: 198.51.100.10\n" +
		"    port: 8388\n" +
		"    cipher: aes-128-gcm\n")

	oldWriter, oldFlags, oldPrefix := log.Writer(), log.Flags(), log.Prefix()
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
		log.SetPrefix(oldPrefix)
	})

	nodes, err := Parse(data, "clash")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(nodes))
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatalf("解析日志泄漏订阅凭据: %q", logs.String())
	}
}

// VLESS 的 type 是传输层，不是 TLS security；ws 明文链接不得被擅自改写为 TLS。
func TestParseStandardVLESSWebSocketWithoutSecurityDoesNotEnableTLS(t *testing.T) {
	node, err := parseStandardLink("vless://123e4567-e89b-12d3-a456-426614174000@edge.example.com:80?type=ws&path=%2Fproxy#ws-clear", "vless")
	if err != nil {
		t.Fatalf("parseStandardLink() error = %v", err)
	}
	if node.Raw["network"] != "ws" {
		t.Fatalf("network = %v, want ws", node.Raw["network"])
	}
	if node.Raw["tls"] == true {
		t.Fatalf("type=ws 被错误解释为 TLS security: raw=%v", node.Raw)
	}
}

func TestParseDirectPlainLineRejectsOutOfRangePort(t *testing.T) {
	_, _, _, _, _, err := parseDirectPlainLine("socks5://proxy.example:65536")
	if err == nil {
		t.Fatal("parseDirectPlainLine() error = nil, want out-of-range port rejection")
	}
}

func TestParseStandardLinkRejectsMissingHost(t *testing.T) {
	_, err := parseStandardLink("vless://123e4567-e89b-12d3-a456-426614174000@:443?security=tls", "vless")
	if err == nil {
		t.Fatal("parseStandardLink() error = nil, want missing-host rejection")
	}
}

func TestParseClashProxyRejectsOutOfRangePort(t *testing.T) {
	_, err := parseClashProxy(map[string]interface{}{
		"name":   "bad-port",
		"type":   "http",
		"server": "proxy.example",
		"port":   65536,
	})
	if err == nil {
		t.Fatal("parseClashProxy() error = nil, want out-of-range port rejection")
	}
}

func TestParseSingBoxJSONRejectsInvalidPort(t *testing.T) {
	_, err := parseSingBoxJSON([]byte(`{"outbounds":[
		{"type":"vless","tag":"good","server":"good.example","server_port":443,"uuid":"u1"},
		{"type":"vless","tag":"bad","server":"bad.example","server_port":65536,"uuid":"u2"}
	]}`))
	if err == nil {
		t.Fatal("parseSingBoxJSON() error = nil, want invalid port rejection")
	}
}

// SIP002 的 plugin 参数决定 Shadowsocks 出站行为；丢失后节点通常无法连通。
func TestParseShadowsocksLinkPreservesPluginOptionsForOutbound(t *testing.T) {
	auth := base64.RawStdEncoding.EncodeToString([]byte("aes-128-gcm:password"))
	link := "ss://" + auth + "@ss.example.com:8388?plugin=v2ray-plugin%3Bobfs%3Dweb%3Bobfs-host%3Dcdn.example.com"

	node, err := parseShadowsocksLink(link)
	if err != nil {
		t.Fatalf("parseShadowsocksLink() error = %v", err)
	}
	out, err := buildOutbound(*node, "test")
	if err != nil {
		t.Fatalf("buildOutbound() error = %v", err)
	}
	if out["plugin"] != "v2ray-plugin" {
		t.Fatalf("plugin = %v, want v2ray-plugin", out["plugin"])
	}
	if out["plugin_opts"] != "obfs=web;obfs-host=cdn.example.com" {
		t.Fatalf("plugin_opts = %v, want preserved SIP002 options", out["plugin_opts"])
	}
}

// 订阅解析不得报告或依赖固定调试转储；固定路径会跨用户、跨进程暴露原始密码。
func TestParseClashDoesNotEmitCredentialDumpArtifact(t *testing.T) {
	oldWriter, oldFlags, oldPrefix := log.Writer(), log.Flags(), log.Prefix()
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
		log.SetPrefix(oldPrefix)
	})

	_, err := Parse([]byte("proxies:\n  - name: secure-node\n    type: ss\n    server: 198.51.100.11\n    port: 8388\n    cipher: aes-128-gcm\n    password: confidential\n"), "clash")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if strings.Contains(logs.String(), "geoproxy_debug_proxies.yaml") {
		t.Fatalf("解析仍声明固定敏感调试转储: %q", logs.String())
	}
}

func TestParserErrorsDoNotExposeCredentialBearingInput(t *testing.T) {
	const secret = "leak"
	input := "unsupported://user:" + secret + "@example.invalid:bad"
	for name, parse := range map[string]func() error{
		"single link": func() error {
			_, err := ParseSingleLink(input)
			return err
		},
		"subscription": func() error {
			_, err := Parse([]byte(input), "auto")
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := parse()
			if err == nil {
				t.Fatal("parse error = nil, want invalid input rejection")
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error leaks input credential: %q", err)
			}
		})
	}
}

func TestSingBoxOutboundToNodeRejectsFractionalPort(t *testing.T) {
	node := singBoxOutboundToNode(map[string]interface{}{
		"server":      "bad.example",
		"server_port": 443.5,
	}, "vless")
	if node != nil {
		t.Fatalf("fractional port yielded node: %+v", node)
	}
}

// 解析失败错误也必须脱敏，不能通过 format/auto 的内容预览回显节点密码。
func TestParseFailureDoesNotExposeCredentials(t *testing.T) {
	const secret = "parse-error-secret"
	_, err := Parse([]byte("socks5://alice:"+secret+"@proxy.example:1080 ???"), "auto")
	if err == nil {
		t.Fatal("Parse() error = nil, want malformed-subscription error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("Parse() error leaked credential: %q", err)
	}
}

func TestParseSingleLinkUnsupportedProtocolDoesNotExposeCredentials(t *testing.T) {
	const secret = "leak"
	_, err := ParseSingleLink("ssr://" + secret + "@proxy.example:443")
	if err == nil {
		t.Fatal("ParseSingleLink() error = nil, want unsupported-protocol rejection")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("unsupported protocol error leaked credential: %q", err)
	}
}
