package custom

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// singBoxProxyTypes 是 sing-box outbound 中被视为"真实代理节点"的 type 白名单（BUG-61）。
// selector/urltest/direct/block/dns 等非节点类型不在此列，会被跳过。
var singBoxProxyTypes = map[string]bool{
	"vmess":       true,
	"vless":       true,
	"trojan":      true,
	"shadowsocks": true,
	"hysteria2":   true,
	"hysteria":    true,
	"tuic":        true,
	"anytls":      true,
	"socks":       true,
	"http":        true,
}

// singBoxConfig 只关心 outbounds 数组，其余字段忽略。
type singBoxConfig struct {
	Outbounds []map[string]interface{} `json:"outbounds"`
}

// parseSingBoxJSON 解析 sing-box JSON 配置，从 outbounds 数组提取真实代理节点。
//
// BUG-59: sing-box outbound 字段名与 Clash YAML 不同（server_port 而非 port、
// transport 结构而非 network+ws-opts、method 而非 cipher、tls 对象而非 tls 布尔 + sni）。
// 本函数将 sing-box 字段映射回本项目内部使用的 Clash 风格 Raw，
// 以复用已测试的 buildOutbound（singbox.go）无需改动其字段读取逻辑。
//
// BUG-60/63: JSON 合法但无 outbounds 数组时给明确错误，而非笼统"无法识别"。
func parseSingBoxJSON(data []byte) ([]ParsedNode, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, fmt.Errorf("sing-box JSON 内容为空")
	}

	var cfg singBoxConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("JSON 解析失败（内容以 { 开头但不是合法 JSON）: %w", err)
	}

	if cfg.Outbounds == nil {
		// 顶层是合法 JSON 但没有 outbounds：不是 sing-box 配置结构。
		return nil, fmt.Errorf("JSON 结构不支持：顶层缺少 outbounds 数组（不是 sing-box 配置）")
	}

	total := len(cfg.Outbounds)
	var nodes []ParsedNode
	skippedNonProxy := 0
	for _, ob := range cfg.Outbounds {
		typ := strings.ToLower(getStr(ob, "type"))
		if !singBoxProxyTypes[typ] {
			skippedNonProxy++
			continue
		}
		server := getStr(ob, "server")
		port, err := parseProxyPort(ob["server_port"])
		if err != nil {
			return nil, fmt.Errorf("sing-box 代理端点无效: %w", err)
		}
		if err := validateProxyEndpoint(server, port); err != nil {
			return nil, fmt.Errorf("sing-box 代理端点无效: %w", err)
		}
		node := singBoxOutboundToNode(ob, typ)
		if node == nil {
			return nil, fmt.Errorf("sing-box 代理节点无法转换: %s", typ)
		}
		nodes = append(nodes, *node)
	}

	log.Printf("[custom] sing-box JSON 解析：outbounds 共 %d 项，提取代理 %d 个，跳过非代理 %d",
		total, len(nodes), skippedNonProxy)

	if len(nodes) == 0 {
		return nil, fmt.Errorf("sing-box outbounds 中未找到有效代理节点（共 %d 项，均为非代理类型）", total)
	}

	return nodes, nil
}

// singBoxOutboundToNode 将单个 sing-box outbound map 转换为 ParsedNode，
// 并把 sing-box 字段映射为 Clash 风格 Raw 供 buildOutbound 消费。
// tag 作为节点名。返回 nil 表示服务器或端口端点无效。
func singBoxOutboundToNode(ob map[string]interface{}, typ string) *ParsedNode {
	server := getStr(ob, "server")
	port, err := parseProxyPort(ob["server_port"])
	if err != nil {
		return nil
	}
	if err := validateProxyEndpoint(server, port); err != nil {
		return nil
	}
	name := getStr(ob, "tag")
	if name == "" {
		name = server
	}

	// 内部类型归一：sing-box 的 "socks" → 本项目 "socks5"（IsDirect/DirectProtocol 依赖）。
	nodeType := typ
	if nodeType == "socks" {
		nodeType = "socks5"
	}

	raw := map[string]interface{}{
		"type":   nodeType,
		"name":   name,
		"server": server,
		"port":   port,
	}

	switch typ {
	case "vmess":
		raw["uuid"] = getStr(ob, "uuid")
		raw["alterId"] = getInt(ob, "alter_id")
		if sec := getStr(ob, "security"); sec != "" {
			raw["cipher"] = sec
		}
		applySingBoxTLS(ob, raw)
		applySingBoxTransport(ob, raw)

	case "vless":
		raw["uuid"] = getStr(ob, "uuid")
		if flow := getStr(ob, "flow"); flow != "" {
			raw["flow"] = flow
		}
		applySingBoxTLS(ob, raw)
		applySingBoxTransport(ob, raw)

	case "trojan":
		raw["password"] = getStr(ob, "password")
		applySingBoxTLS(ob, raw)
		applySingBoxTransport(ob, raw)

	case "shadowsocks":
		// sing-box 用 method，Clash/buildOutbound 用 cipher。
		raw["cipher"] = getStr(ob, "method")
		raw["password"] = getStr(ob, "password")
		if plugin := getStr(ob, "plugin"); plugin != "" {
			raw["plugin"] = plugin
			if opts := getStr(ob, "plugin_opts"); opts != "" {
				raw["plugin-opts-raw"] = opts // 保留原始串，由 buildOutbound 直接传入 sing-box
			}
		}

	case "hysteria2":
		raw["password"] = getStr(ob, "password")
		applySingBoxTLS(ob, raw)

	case "hysteria":
		if v := getStr(ob, "auth_str"); v != "" {
			raw["auth-str"] = v
		}
		applySingBoxTLS(ob, raw)

	case "tuic":
		raw["uuid"] = getStr(ob, "uuid")
		raw["password"] = getStr(ob, "password")
		if cc := getStr(ob, "congestion_control"); cc != "" {
			raw["congestion-controller"] = cc
		}
		applySingBoxTLS(ob, raw)

	case "anytls":
		raw["password"] = getStr(ob, "password")
		applySingBoxTLS(ob, raw)

	case "http":
		// 直连类型，buildOutbound 不处理；manager 走 DirectAddress/DirectProtocol。
		if u := getStr(ob, "username"); u != "" {
			raw["username"] = u
		}
		if p := getStr(ob, "password"); p != "" {
			raw["password"] = p
		}

	case "socks":
		if u := getStr(ob, "username"); u != "" {
			raw["username"] = u
		}
		if p := getStr(ob, "password"); p != "" {
			raw["password"] = p
		}
	}

	return &ParsedNode{
		Name:   name,
		Type:   nodeType,
		Server: server,
		Port:   port,
		Raw:    raw,
	}
}

// applySingBoxTLS 将 sing-box 的 tls 对象映射为 Clash 风格字段。
// sing-box: tls{enabled, server_name, insecure, alpn, utls{fingerprint}, reality{public_key, short_id}}
// Clash:    tls(bool), sni, skip-cert-verify, alpn, client-fingerprint, reality-opts{public-key, short-id}
func applySingBoxTLS(ob map[string]interface{}, raw map[string]interface{}) {
	tlsObj, ok := ob["tls"].(map[string]interface{})
	if !ok {
		return
	}
	if !getBool(tlsObj, "enabled") {
		return
	}

	raw["tls"] = true

	if sni := getStr(tlsObj, "server_name"); sni != "" {
		raw["sni"] = sni
	}
	if getBool(tlsObj, "insecure") {
		raw["skip-cert-verify"] = true
	}
	if alpn, ok := tlsObj["alpn"].([]interface{}); ok && len(alpn) > 0 {
		raw["alpn"] = alpn
	}
	if utls, ok := tlsObj["utls"].(map[string]interface{}); ok {
		if fp := getStr(utls, "fingerprint"); fp != "" {
			raw["client-fingerprint"] = fp
		}
	}
	if reality, ok := tlsObj["reality"].(map[string]interface{}); ok {
		realityOpts := map[string]interface{}{}
		if pk := getStr(reality, "public_key"); pk != "" {
			realityOpts["public-key"] = pk
		}
		if sid := getStr(reality, "short_id"); sid != "" {
			realityOpts["short-id"] = sid
		}
		raw["reality-opts"] = realityOpts
	}
}

// applySingBoxTransport 将 sing-box 的 transport 对象映射为 Clash 风格 network + *-opts。
// sing-box: transport{type=ws/grpc/http/httpupgrade, path, headers, service_name, host}
// Clash:    network + ws-opts{path,headers} / grpc-opts{grpc-service-name} / h2-opts{path,host}
// 无 transport 表示裸 TCP，network 保持默认（buildOutbound 默认 tcp）。
func applySingBoxTransport(ob map[string]interface{}, raw map[string]interface{}) {
	transport, ok := ob["transport"].(map[string]interface{})
	if !ok {
		return
	}
	ttype := strings.ToLower(getStr(transport, "type"))
	switch ttype {
	case "ws":
		raw["network"] = "ws"
		wsOpts := map[string]interface{}{}
		if path := getStr(transport, "path"); path != "" {
			wsOpts["path"] = path
		}
		if headers, ok := transport["headers"].(map[string]interface{}); ok {
			wsOpts["headers"] = headers
		}
		raw["ws-opts"] = wsOpts

	case "grpc":
		raw["network"] = "grpc"
		grpcOpts := map[string]interface{}{}
		if sn := getStr(transport, "service_name"); sn != "" {
			grpcOpts["grpc-service-name"] = sn
		}
		raw["grpc-opts"] = grpcOpts

	case "http":
		// sing-box http transport ↔ Clash h2（buildOutbound 将 h2 映射为 sing-box http transport）。
		raw["network"] = "h2"
		h2Opts := map[string]interface{}{}
		if path := getStr(transport, "path"); path != "" {
			h2Opts["path"] = path
		}
		if hostArr, ok := transport["host"].([]interface{}); ok && len(hostArr) > 0 {
			h2Opts["host"] = hostArr
		} else if host := getStr(transport, "host"); host != "" {
			h2Opts["host"] = []interface{}{host}
		}
		raw["h2-opts"] = h2Opts

	case "httpupgrade":
		raw["network"] = "httpupgrade"
		wsOpts := map[string]interface{}{}
		if path := getStr(transport, "path"); path != "" {
			wsOpts["path"] = path
		}
		if headers, ok := transport["headers"].(map[string]interface{}); ok {
			wsOpts["headers"] = headers
		}
		raw["ws-opts"] = wsOpts

	default:
		// 未知/其他 transport：保留 network 供 buildOutbound 判定（可能因不支持而失败，属预期）。
		if ttype != "" {
			raw["network"] = ttype
		}
	}
}
