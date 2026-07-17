package custom

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// portRangeSpan 保留给既有配置兼容；当前 Reload 保持已加载节点端口稳定，避免入库地址漂移。
const portRangeSpan = 5000

// SingBoxProcess 管理 sing-box 子进程
type SingBoxProcess struct {
	cmd        *exec.Cmd
	waitDone   chan struct{}
	binPath    string
	configDir  string
	configFile string
	basePort   int
	portOffset int            // 端口范围偏移；已加载节点保持原端口，新节点从当前范围后续端口追加
	portMap    map[string]int // nodeKey → 本地 mixed 端口（单端口同时服务 SOCKS5 与 HTTP）
	nodes      []ParsedNode
	mu         sync.Mutex
	running    bool
	status     string
	reason     string
	readyPorts int
	assembly   assemblyDiagnostics
}

// assemblyDiagnostics records the outcome of one in-memory configuration
// assembly. Rejections are safe to omit from a commit; allocation failures are
// not, because they mean an otherwise buildable target did not reach runtime.
type assemblyDiagnostics struct {
	accepted    []ParsedNode
	rejected    []assemblyRejectedNode
	segmentFull []ParsedNode
}

type assemblyRejectedNode struct {
	node ParsedNode
	err  error
}

func (d assemblyDiagnostics) rejectedKeys() map[string]bool {
	keys := make(map[string]bool, len(d.rejected))
	for _, rejected := range d.rejected {
		keys[rejected.node.NodeKey()] = true
	}
	return keys
}

func (d assemblyDiagnostics) acceptedKeys() map[string]bool {
	keys := make(map[string]bool, len(d.accepted))
	for _, node := range d.accepted {
		keys[node.NodeKey()] = true
	}
	return keys
}

func (d assemblyDiagnostics) hasEvidenceForSegmentFull(key string) bool {
	for _, node := range d.segmentFull {
		if node.NodeKey() == key {
			return true
		}
	}
	return false
}

const (
	SingBoxStatusNoTunnelNodes = "no_tunnel_nodes"
	SingBoxStatusRunning       = "running"
	SingBoxStatusStopped       = "stopped"
	SingBoxStatusPartial       = "partial"
	SingBoxStatusFailed        = "failed"
)

// SingBoxRuntimeStatus 是 WebUI/API 使用的可解释 sing-box 状态快照。
type SingBoxRuntimeStatus struct {
	Running    bool
	Status     string
	Reason     string
	Nodes      int
	ReadyPorts int
	TotalPorts int
}

// portRangeSize 保留给历史配置说明，实际端口稳定性由 assembleConfig 维护。
const portRangeSize = 5000

// NewSingBoxProcess 创建 sing-box 进程管理器
func NewSingBoxProcess(binPath, dataDir string, basePort int) *SingBoxProcess {
	if dataDir == "" {
		// 没设置 DATA_DIR 时，使用当前工作目录下的 singbox/
		wd, _ := os.Getwd()
		dataDir = wd
	}
	configDir, _ := filepath.Abs(filepath.Join(dataDir, "singbox"))
	os.MkdirAll(configDir, 0755)

	return &SingBoxProcess{
		binPath:    binPath,
		configDir:  configDir,
		configFile: filepath.Join(configDir, "config.json"),
		basePort:   basePort,
		portMap:    make(map[string]int),
		status:     SingBoxStatusNoTunnelNodes,
		reason:     SingBoxStatusNoTunnelNodes,
	}
}

// Reload 重新加载节点配置并重启 sing-box
func (s *SingBoxProcess) Reload(nodes []ParsedNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 过滤出需要 sing-box 转换的节点
	var tunnelNodes []ParsedNode
	for _, n := range nodes {
		if !n.IsDirect() {
			tunnelNodes = append(tunnelNodes, n)
		}
	}

	if len(tunnelNodes) == 0 {
		log.Println("[custom] 无需 sing-box 转换的节点，停止进程")
		s.stopLocked()
		s.nodes = nil
		s.portMap = make(map[string]int)
		s.assembly = assemblyDiagnostics{}
		s.setStatusLocked(SingBoxStatusNoTunnelNodes, SingBoxStatusNoTunnelNodes, 0)
		return nil
	}

	// 保持当前端口范围，assembleConfig 会复用已加载节点端口并为新节点追加端口。
	nextPortOffset := s.portOffset

	// 生成配置
	oldPortOffset := s.portOffset
	oldPortMap := s.portMap
	oldNodes := s.nodes
	oldAssembly := s.assembly
	oldConfig, oldConfigErr := os.ReadFile(s.configFile)
	restoreState := func() {
		s.portOffset = oldPortOffset
		s.portMap = oldPortMap
		s.nodes = oldNodes
		s.assembly = oldAssembly
		if oldConfigErr == nil {
			if err := os.WriteFile(s.configFile, oldConfig, 0644); err != nil {
				log.Printf("[custom] ⚠️ 恢复旧 sing-box 配置文件失败: %v", err)
			}
		}
	}
	s.portOffset = nextPortOffset
	diagnostics, err := s.generateConfig(tunnelNodes)
	if err != nil {
		restoreState()
		s.setStatusLocked(SingBoxStatusFailed, "config_generation_failed", 0)
		return fmt.Errorf("生成 sing-box 配置失败: %w", err)
	}
	s.assembly = diagnostics
	if len(diagnostics.accepted) == 0 {
		restoreState()
		s.setStatusLocked(SingBoxStatusFailed, "all_tunnel_nodes_rejected", 0)
		return fmt.Errorf("所有隧道节点均被拒绝: %w", incompletePortAllocationErrorWithDiagnostics(tunnelNodes, s.portMap, diagnostics))
	}
	if len(diagnostics.segmentFull) > 0 {
		restoreState()
		s.setStatusLocked(SingBoxStatusFailed, "ports_incomplete", 0)
		return incompletePortAllocationErrorWithDiagnostics(tunnelNodes, s.portMap, diagnostics)
	}
	// 仅端口分配器实际报告的 segmentFull 才能判为段满；构建失败节点已被
	// 明确拒绝，不应阻断其余可构建节点启动。
	if err := incompletePortAllocationErrorWithDiagnostics(diagnostics.accepted, s.portMap, diagnostics); err != nil {
		restoreState()
		s.setStatusLocked(SingBoxStatusFailed, "ports_incomplete", 0)
		return err
	}
	if _, err := exec.LookPath(s.binPath); err != nil {
		restoreState()
		s.setStatusLocked(SingBoxStatusFailed, "binary_not_found", 0)
		return fmt.Errorf("sing-box 未找到: %s（请安装 sing-box 或设置 SINGBOX_PATH）", s.binPath)
	}

	// 健壮性：整份配置若校验失败，二分剔除坏节点，用剩余可用节点重建，
	// 避免单个非法节点拖垮全部订阅（一坏全灭）。
	goodNodes := s.pruneInvalidNodes(diagnostics.accepted)
	if len(goodNodes) == 0 {
		restoreState()
		s.setStatusLocked(SingBoxStatusFailed, "all_tunnel_nodes_invalid", 0)
		return fmt.Errorf("所有隧道节点均无法通过 sing-box 校验")
	}
	if len(goodNodes) != len(diagnostics.accepted) {
		log.Printf("[custom] ⚠️ 剔除 %d 个非法节点，保留 %d 个可用节点",
			len(diagnostics.accepted)-len(goodNodes), len(goodNodes))
		diagnostics, err = s.generateConfig(goodNodes)
		if err != nil {
			restoreState()
			s.setStatusLocked(SingBoxStatusFailed, "config_rebuild_failed", 0)
			return fmt.Errorf("重建 sing-box 配置失败: %w", err)
		}
		s.assembly = diagnostics
		if err := incompletePortAllocationErrorWithDiagnostics(diagnostics.accepted, s.portMap, diagnostics); err != nil {
			restoreState()
			s.setStatusLocked(SingBoxStatusFailed, "ports_incomplete", 0)
			return err
		}
	}

	// 重启进程
	s.stopLocked()
	if err := s.startLocked(); err != nil {
		// Partial：新进程已在跑且配置/portMap 已是本次目标，不可 restore 回旧映射（会与进程脱节）。
		// 仍返回 error，使 RefreshSubscription 保留旧代理。
		if s.status == SingBoxStatusPartial {
			s.nodes = goodNodes
			return fmt.Errorf("启动 sing-box 失败: %w", err)
		}
		restoreState()
		s.setStatusLocked(SingBoxStatusFailed, classifySingBoxStartError(err), 0)
		return fmt.Errorf("启动 sing-box 失败: %w", err)
	}

	s.nodes = goodNodes
	s.assembly.accepted = append([]ParsedNode(nil), goodNodes...)
	return nil
}

// incompletePortAllocationError preserves the strict fallback for callers
// without assembler evidence: a build failure is explicit, any other missing
// port remains unknown rather than being guessed as a full segment.
func incompletePortAllocationError(nodes []ParsedNode, portMap map[string]int) error {
	return incompletePortAllocationErrorWithDiagnostics(nodes, portMap, assemblyDiagnostics{})
}

// incompletePortAllocationErrorWithDiagnostics distinguishes explicit build
// rejections, allocator-proven segment exhaustion, and unknown missing ports.
func incompletePortAllocationErrorWithDiagnostics(nodes []ParsedNode, portMap map[string]int, diagnostics assemblyDiagnostics) error {
	var missing []ParsedNode
	for _, n := range nodes {
		if _, ok := portMap[n.NodeKey()]; !ok {
			missing = append(missing, n)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	var buildFailures []string
	var segmentFull []string
	var unknown []string
	rejectedErr := map[string]error{}
	for _, r := range diagnostics.rejected {
		rejectedErr[r.node.NodeKey()] = r.err
	}
	for _, node := range missing {
		if err, ok := rejectedErr[node.NodeKey()]; ok {
			if err != nil {
				buildFailures = append(buildFailures,
					fmt.Sprintf("节点 %q (%s) 构建失败: %v", node.Name, node.Type, err))
			} else {
				buildFailures = append(buildFailures,
					fmt.Sprintf("节点 %q (%s) 构建失败", node.Name, node.Type))
			}
			continue
		}
		if _, err := buildOutbound(node, "diagnostic"); err != nil {
			buildFailures = append(buildFailures,
				fmt.Sprintf("节点 %q (%s) 构建失败: %v", node.Name, node.Type, err))
			continue
		}
		if diagnostics.hasEvidenceForSegmentFull(node.NodeKey()) {
			segmentFull = append(segmentFull, fmt.Sprintf("%q (%s)", node.Name, node.Type))
			continue
		}
		unknown = append(unknown, fmt.Sprintf("%q (%s)", node.Name, node.Type))
	}

	parts := []string{fmt.Sprintf("sing-box 端口分配不完整: %d/%d 节点未分配端口", len(missing), len(nodes))}
	if len(segmentFull) > 0 {
		parts = append(parts, "端口段已满，未分配节点: "+strings.Join(segmentFull, ", "))
	}
	if len(buildFailures) > 0 {
		parts = append(parts, "节点构建失败: "+strings.Join(buildFailures, "; "))
	}
	if len(unknown) > 0 {
		parts = append(parts, "未知端口缺失: "+strings.Join(unknown, ", "))
	}
	return fmt.Errorf("%s", strings.Join(parts, "; "))
}

// checkNodes 生成一份仅含给定节点的临时配置并运行 sing-box check，返回是否通过。
// 不改动 s.portMap / s.configFile 等运行态，仅用于校验探测。
func (s *SingBoxProcess) checkNodes(nodes []ParsedNode) bool {
	if len(nodes) == 0 {
		return true
	}
	data, err := s.buildConfigBytes(nodes)
	if err != nil {
		return false
	}
	tmp, err := os.CreateTemp(s.configDir, "check-*.json")
	if err != nil {
		return false
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return false
	}
	tmp.Close()

	binPath, err := exec.LookPath(s.binPath)
	if err != nil {
		return false
	}
	cmd := exec.Command(binPath, "check", "-c", tmpPath, "-D", s.configDir)
	return cmd.Run() == nil
}

// pruneInvalidNodes 返回能通过 sing-box 校验的节点子集。
// 若整体已通过则原样返回；否则二分递归定位并剔除坏节点。
func (s *SingBoxProcess) pruneInvalidNodes(nodes []ParsedNode) []ParsedNode {
	if s.checkNodes(nodes) {
		return nodes
	}
	if len(nodes) == 1 {
		log.Printf("[custom] 节点 %s (%s) 无法通过 sing-box 校验，剔除", nodes[0].Name, nodes[0].Type)
		return nil
	}
	mid := len(nodes) / 2
	left := s.pruneInvalidNodes(nodes[:mid])
	right := s.pruneInvalidNodes(nodes[mid:])
	return append(left, right...)
}

// generateConfig 生成 sing-box JSON 配置并写入运行态（更新 s.portMap、写 configFile）。
func (s *SingBoxProcess) generateConfig(nodes []ParsedNode) (assemblyDiagnostics, error) {
	config, portMap, diagnostics := s.assembleConfigWithDiagnostics(nodes)
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return assemblyDiagnostics{}, err
	}
	s.portMap = portMap
	if err := os.WriteFile(s.configFile, data, 0644); err != nil {
		return assemblyDiagnostics{}, err
	}
	return diagnostics, nil
}

// buildConfigBytes 生成配置 JSON 字节，但不改动运行态；供 checkNodes 探测校验使用。
func (s *SingBoxProcess) buildConfigBytes(nodes []ParsedNode) ([]byte, error) {
	config, _ := s.assembleConfig(nodes)
	return json.MarshalIndent(config, "", "  ")
}

// assembleConfig 组装 sing-box 配置 map 及对应的 nodeKey→port 映射，无副作用（不写状态/文件）。
// 端口合并（缺陷5，双端口2x→单端口）：每个 tunnel 节点只生成一个 sing-box `mixed` 本地入站，
// 该单端口同时接受 SOCKS4/4a/5 与 HTTP 连接（mixed 为 sing-box 官方入站类型），路由到同一出站。
// 返回 (config, portMap)。portMap 沿用旧映射保持稳定（不破坏既有入库地址）。
func (s *SingBoxProcess) assembleConfig(nodes []ParsedNode) (map[string]interface{}, map[string]int) {
	config, portMap, _ := s.assembleConfigWithDiagnostics(nodes)
	return config, portMap
}

// assembleConfigWithDiagnostics produces the configuration together with the
// exact acceptance and allocation decisions made by this assembler.
func (s *SingBoxProcess) assembleConfigWithDiagnostics(nodes []ParsedNode) (map[string]interface{}, map[string]int, assemblyDiagnostics) {
	oldPortMap := make(map[string]int, len(s.portMap))
	for key, port := range s.portMap {
		oldPortMap[key] = port
	}
	portMap := make(map[string]int, len(nodes))
	// 仅以"将保留的节点"（本次仍在且此前已分配端口者）为基准计算端口占用与高水位。
	// 被移除节点的端口随即释放、maxPort 回落，避免端口只增不减而最终耗尽分片段（缺陷4：端口泄漏）。
	incoming := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		incoming[n.NodeKey()] = true
	}
	usedPorts := make(map[int]bool, len(oldPortMap))
	for key, port := range oldPortMap {
		if !incoming[key] {
			continue
		}
		usedPorts[port] = true
	}
	orderedNodes := append([]ParsedNode(nil), nodes...)
	sort.SliceStable(orderedNodes, func(i, j int) bool {
		return orderedNodes[i].NodeKey() < orderedNodes[j].NodeKey()
	})
	// 本分片实例独占端口段 [basePort, basePort+portRangeSpan)，越界会侵入下一分片段。
	// 扫描从 basePort 起（含），与历史“port++ 从 maxPort 起”对齐：首端口为 basePort+1。
	segmentStart := s.basePort + 1
	segmentEnd := s.basePort + portRangeSpan

	// allocPort 复用旧端口以保持稳定；新节点从 basePort 起扫描段内空洞复用，
	// 不得从高水位单调爬升导致中间空洞浪费；段满返回 0（跳过，不跨分片段）。
	allocPort := func(key string, oldMap map[string]int) int {
		if p, ok := oldMap[key]; ok {
			return p
		}
		for p := segmentStart; p < segmentEnd; p++ {
			if usedPorts[p] {
				continue
			}
			usedPorts[p] = true
			return p
		}
		return 0
	}

	var inbounds []map[string]interface{}
	var outbounds []map[string]interface{}
	var rules []map[string]interface{}
	diagnostics := assemblyDiagnostics{}

	for i, node := range orderedNodes {
		key := node.NodeKey()
		tag := fmt.Sprintf("node-%d", i)

		// 出站：根据节点类型生成（失败节点跳过，不占端口）
		outbound, err := buildOutbound(node, tag)
		if err != nil {
			log.Printf("[custom] 跳过节点 %s (%s): %v", node.Name, node.Type, err)
			diagnostics.rejected = append(diagnostics.rejected, assemblyRejectedNode{node: node, err: err})
			continue
		}

		mixedPort := allocPort(key, oldPortMap)
		if mixedPort == 0 {
			// 分片端口段已满：跳过该节点（不生成入站/出站/路由，不占端口），与 buildOutbound 失败同一处理。
			log.Printf("[custom] 分片端口段已满，跳过节点 %s (%s)", node.Name, node.Type)
			diagnostics.segmentFull = append(diagnostics.segmentFull, node)
			continue
		}
		portMap[key] = mixedPort
		diagnostics.accepted = append(diagnostics.accepted, node)

		// 入站：本地 mixed 监听（单端口同时服务 SOCKS5 与 HTTP），路由到该节点出站。
		inbounds = append(inbounds, map[string]interface{}{
			"type":        "mixed",
			"tag":         fmt.Sprintf("in-%s", tag),
			"listen":      "127.0.0.1",
			"listen_port": mixedPort,
		})
		outbounds = append(outbounds, outbound)

		// 路由规则：mixed 入站 → 同一出站
		rules = append(rules, map[string]interface{}{
			"inbound":  []string{fmt.Sprintf("in-%s", tag)},
			"outbound": fmt.Sprintf("out-%s", tag),
		})
	}

	// 添加 direct 出站作为默认
	outbounds = append(outbounds, map[string]interface{}{
		"type": "direct",
		"tag":  "direct",
	})

	config := map[string]interface{}{
		"log": map[string]interface{}{
			"level": "warn",
		},
		// 本地 DNS server + default_domain_resolver 优先 IPv4 解析节点服务器域名。
		// 多数容器/宿主无 IPv6 出网，若解析到 IPv6 会导致 "network is unreachable"
		// 而误判节点不可用。这是 sing-box 1.12+ 对已废弃 outbound.domain_strategy
		// 的官方替代写法（见 sing-box migration 文档）。
		"dns": map[string]interface{}{
			"servers": []map[string]interface{}{
				{"type": "local", "tag": "local"},
			},
		},
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"route": map[string]interface{}{
			"rules": rules,
			"final": "direct",
			"default_domain_resolver": map[string]interface{}{
				"server":   "local",
				"strategy": "prefer_ipv4",
			},
		},
	}
	return config, portMap, diagnostics
}

// buildOutbound 根据节点类型构建 sing-box 出站配置。
// 返回 error 而非静默丢弃：不支持的协议类型或传输层（如 Xray 私有的 xhttp）必须显式失败，
// 避免生成"能过 sing-box check 但实际连不上"的假配置。
func buildOutbound(node ParsedNode, tag string) (map[string]interface{}, error) {
	raw := node.Raw
	out := map[string]interface{}{
		"tag":    fmt.Sprintf("out-%s", tag),
		"server": node.Server,
	}

	// sing-box 使用 server_port 而不是 port
	out["server_port"] = node.Port

	switch node.Type {
	case "vmess":
		out["type"] = "vmess"
		out["uuid"] = getStr(raw, "uuid")
		out["alter_id"] = getInt(raw, "alterId")
		out["security"] = getStrDefault(raw, "cipher", "auto")
		applyTLS(raw, out)
		if err := applyTransport(raw, out); err != nil {
			return nil, err
		}

	case "vless":
		out["type"] = "vless"
		out["uuid"] = getStr(raw, "uuid")
		out["flow"] = getStr(raw, "flow")
		applyTLS(raw, out)
		if err := applyTransport(raw, out); err != nil {
			return nil, err
		}

	case "trojan":
		out["type"] = "trojan"
		out["password"] = getStr(raw, "password")
		// 标准 trojan 强制走 TLS；源配置常省略 tls 标记，需强制生成 TLS 段。
		forceTLS(raw, out)
		if err := applyTransport(raw, out); err != nil {
			return nil, err
		}

	case "shadowsocks":
		out["type"] = "shadowsocks"
		out["method"] = getStr(raw, "cipher")
		out["password"] = getStr(raw, "password")
		if plugin := getStr(raw, "plugin"); plugin != "" {
			out["plugin"] = plugin
			if pluginOpts, ok := raw["plugin-opts"].(map[string]interface{}); ok {
				out["plugin_opts"] = convertPluginOpts(plugin, pluginOpts)
			}
		}

	case "hysteria2":
		out["type"] = "hysteria2"
		out["password"] = getStr(raw, "password")
		// hysteria2 基于 QUIC/TLS，sing-box 强制要求 TLS 段。
		forceTLS(raw, out)

	case "hysteria":
		out["type"] = "hysteria"
		out["auth_str"] = getStr(raw, "auth-str")
		if up := getStr(raw, "up"); up != "" {
			out["up_mbps"] = parseSpeed(up)
		}
		if down := getStr(raw, "down"); down != "" {
			out["down_mbps"] = parseSpeed(down)
		}
		// hysteria 基于 QUIC/TLS，sing-box 强制要求 TLS 段。
		forceTLS(raw, out)

	case "tuic":
		out["type"] = "tuic"
		out["uuid"] = getStr(raw, "uuid")
		out["password"] = getStr(raw, "password")
		out["congestion_control"] = getStrDefault(raw, "congestion-controller", "bbr")
		// tuic 基于 QUIC/TLS，sing-box 强制要求 TLS 段。
		forceTLS(raw, out)

	case "anytls":
		out["type"] = "anytls"
		out["password"] = getStr(raw, "password")
		// anytls 强制启用 TLS
		forceTLS(raw, out)

	default:
		return nil, fmt.Errorf("不支持的节点类型: %s", node.Type)
	}

	return out, nil
}

// forceTLS 强制应用 TLS 配置（用于 anytls 等必须 TLS 的协议）
func forceTLS(raw map[string]interface{}, out map[string]interface{}) {
	withTLS := make(map[string]interface{}, len(raw)+1)
	for key, value := range raw {
		withTLS[key] = value
	}
	withTLS["tls"] = true
	applyTLS(withTLS, out)
}

// applyTLS 应用 TLS 配置
func applyTLS(raw map[string]interface{}, out map[string]interface{}) {
	tls := getBool(raw, "tls")
	// 如果有 sni/alpn/client-fingerprint 也视为需要 TLS
	if !tls && getStr(raw, "sni") == "" && getStr(raw, "client-fingerprint") == "" {
		return
	}

	tlsConfig := map[string]interface{}{
		"enabled": true,
	}

	if sni := getStr(raw, "sni"); sni != "" {
		tlsConfig["server_name"] = sni
	} else if servername := getStr(raw, "servername"); servername != "" {
		tlsConfig["server_name"] = servername
	}

	if getBool(raw, "skip-cert-verify") {
		tlsConfig["insecure"] = true
	}

	if alpn, ok := raw["alpn"].([]interface{}); ok {
		var alpnStrs []string
		for _, a := range alpn {
			if s, ok := a.(string); ok {
				alpnStrs = append(alpnStrs, s)
			}
		}
		if len(alpnStrs) > 0 {
			tlsConfig["alpn"] = alpnStrs
		}
	}

	fp := getStr(raw, "client-fingerprint")
	if fp != "" {
		tlsConfig["utls"] = map[string]interface{}{
			"enabled":     true,
			"fingerprint": fp,
		}
	}

	// reality 配置
	if realityOpts, ok := raw["reality-opts"].(map[string]interface{}); ok {
		tlsConfig["reality"] = map[string]interface{}{
			"enabled":    true,
			"public_key": getStr(realityOpts, "public-key"),
			"short_id":   getStr(realityOpts, "short-id"),
		}
		// sing-box reality 出站要求 utls 指纹；源配置常省略 client-fingerprint。
		// 缺失时补一个合理默认（chrome），否则 sing-box 会拒绝该 reality 出站。
		if fp == "" {
			tlsConfig["utls"] = map[string]interface{}{
				"enabled":     true,
				"fingerprint": "chrome",
			}
		}
	}

	out["tls"] = tlsConfig
}

// applyTransport 应用传输层配置。
// 对 sing-box 支持的传输层写入 out["transport"]；tcp/空表示裸 TCP，无需 transport。
// 对 sing-box 明确不支持的传输层（如 Xray 私有的 xhttp/splithttp）返回错误，
// 由调用方跳过该节点，避免生成"能过 check 但连不上"的静默降级配置。
func applyTransport(raw map[string]interface{}, out map[string]interface{}) error {
	network := getStrDefault(raw, "network", "tcp")

	switch network {
	case "tcp", "":
		// 裸 TCP，sing-box 默认即为此，无需 transport 字段。
		return nil

	case "ws":
		transport := map[string]interface{}{
			"type": "ws",
		}
		if wsOpts, ok := raw["ws-opts"].(map[string]interface{}); ok {
			if path := getStr(wsOpts, "path"); path != "" {
				transport["path"] = path
			}
			if headers, ok := wsOpts["headers"].(map[string]interface{}); ok {
				transport["headers"] = headers
			}
		}
		out["transport"] = transport
		return nil

	case "grpc":
		transport := map[string]interface{}{
			"type": "grpc",
		}
		if grpcOpts, ok := raw["grpc-opts"].(map[string]interface{}); ok {
			if sn := getStr(grpcOpts, "grpc-service-name"); sn != "" {
				transport["service_name"] = sn
			}
		}
		out["transport"] = transport
		return nil

	case "h2":
		transport := map[string]interface{}{
			"type": "http",
		}
		if h2Opts, ok := raw["h2-opts"].(map[string]interface{}); ok {
			if path := getStr(h2Opts, "path"); path != "" {
				transport["path"] = path
			}
			if host, ok := h2Opts["host"].([]interface{}); ok && len(host) > 0 {
				if h, ok := host[0].(string); ok {
					transport["host"] = []string{h}
				}
			}
		}
		out["transport"] = transport
		return nil

	case "httpupgrade":
		transport := map[string]interface{}{
			"type": "httpupgrade",
		}
		if wsOpts, ok := raw["ws-opts"].(map[string]interface{}); ok {
			if path := getStr(wsOpts, "path"); path != "" {
				transport["path"] = path
			}
			if headers, ok := wsOpts["headers"].(map[string]interface{}); ok {
				if host, ok := headers["Host"].(string); ok {
					transport["host"] = host
				}
			}
		}
		out["transport"] = transport
		return nil

	default:
		// xhttp / splithttp 等 Xray 私有传输层 sing-box 不支持；显式失败而非静默丢弃。
		return fmt.Errorf("sing-box 不支持的传输层: %s", network)
	}
}

// convertPluginOpts 转换 shadowsocks 插件选项
func convertPluginOpts(plugin string, opts map[string]interface{}) string {
	var parts []string
	for k, v := range opts {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return strings.Join(parts, ";")
}

// startLocked 启动 sing-box（需持有锁）
func (s *SingBoxProcess) startLocked() error {
	binPath, err := exec.LookPath(s.binPath)
	if err != nil {
		s.setStatusLocked(SingBoxStatusFailed, "binary_not_found", 0)
		return fmt.Errorf("sing-box 未找到: %s（请安装 sing-box 或设置 SINGBOX_PATH）", s.binPath)
	}

	// 先检查配置是否有效
	checkCmd := exec.Command(binPath, "check", "-c", s.configFile, "-D", s.configDir)
	if checkOutput, err := checkCmd.CombinedOutput(); err != nil {
		log.Printf("[custom] ❌ sing-box 配置检查失败:\n%s", string(checkOutput))
		s.setStatusLocked(SingBoxStatusFailed, "config_invalid", 0)
		return fmt.Errorf("sing-box 配置无效: %s", string(checkOutput))
	}

	// 启动前确认本次配置要监听的端口已释放：Reload 重启时端口被稳定复用，
	// 旧进程被 stopLocked 终止后其 listen socket 可能尚未被内核回收，
	// 若立即 bind 同一端口会触发 "address already in use" 而启动失败。
	// 轮询等待旧端口释放（带上限超时）后再启动，消除重启竞态窗口。
	if busy := s.waitPortsFreeLocked(portReleaseTimeout); busy > 0 {
		log.Printf("[custom] ⚠️ 启动前仍有 %d 个端口被占用（超时 %s），可能与旧进程残留监听冲突，仍尝试启动",
			busy, portReleaseTimeout)
	}

	s.cmd = exec.Command(binPath, "run", "-c", s.configFile, "-D", s.configDir)

	// 捕获 stderr 用于错误诊断
	stderrPipe, _ := s.cmd.StderrPipe()
	s.cmd.Stdout = os.Stdout

	if err := s.cmd.Start(); err != nil {
		s.setStatusLocked(SingBoxStatusFailed, "start_failed", 0)
		return fmt.Errorf("sing-box 启动失败: %w", err)
	}
	s.running = true

	// 异步读取 stderr 并输出到日志
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stderrPipe.Read(buf)
			if n > 0 {
				log.Printf("[sing-box] %s", strings.TrimSpace(string(buf[:n])))
			}
			if err != nil {
				break
			}
		}
	}()

	// 监控进程退出
	exitCh := make(chan struct{})
	cmd := s.cmd
	s.waitDone = exitCh
	go func() {
		if cmd != nil && cmd.Process != nil {
			cmd.Wait()
			close(exitCh)
			s.mu.Lock()
			if s.cmd == cmd {
				s.running = false
				if s.status != SingBoxStatusFailed {
					s.setStatusLocked(SingBoxStatusStopped, "process_exited", 0)
				}
				s.cmd = nil
				s.waitDone = nil
			}
			s.mu.Unlock()
		}
	}()

	// 等待端口就绪（最多 10 秒）
	log.Printf("[custom] sing-box 启动中，等待端口就绪（配置: %s）...", s.configFile)
	totalPorts := len(s.portMap)
	readyPorts := 0
	for i := 0; i < 20; i++ {
		// 检查进程是否已退出
		select {
		case <-exitCh:
			s.setStatusLocked(SingBoxStatusFailed, "process_exited_on_start", 0)
			return fmt.Errorf("sing-box 进程启动后立即退出，请检查日志")
		default:
		}

		time.Sleep(500 * time.Millisecond)
		readyPorts = s.countReadyPortsLocked(100 * time.Millisecond)
		if totalPorts > 0 && readyPorts == totalPorts {
			break
		}
	}

	if totalPorts == 0 {
		s.setStatusLocked(SingBoxStatusFailed, "no_ports_allocated", 0)
		return fmt.Errorf("sing-box 未分配本地监听端口")
	}
	if readyPorts < totalPorts {
		s.setStatusLocked(SingBoxStatusPartial, "ports_not_ready", readyPorts)
		log.Printf("[custom] ⚠️ sing-box 端口未完全就绪，部分节点可能不可用（%d/%d）", readyPorts, totalPorts)
		// Partial 不得 return nil：否则 RefreshSubscription 会删旧代理并入库不完整配置。
		return fmt.Errorf("sing-box 端口未完全就绪（%d/%d）", readyPorts, totalPorts)
	}
	s.setStatusLocked(SingBoxStatusRunning, SingBoxStatusRunning, readyPorts)
	log.Printf("[custom] ✅ sing-box 启动成功，管理 %d 个节点", totalPorts)
	return nil
}

func (s *SingBoxProcess) setStatusLocked(status, reason string, readyPorts int) {
	s.status = status
	s.reason = reason
	s.readyPorts = readyPorts
}

// portReleaseTimeout 是启动新进程前等待旧端口释放的上限。
// 取值权衡：足够覆盖内核回收 listen socket 的常见延迟，又不至于让重启长时间挂起。
const portReleaseTimeout = 5 * time.Second

// waitPortsFreeLocked 在启动新 sing-box 进程前，轮询确认本次配置要监听的端口
// （s.portMap，因端口稳定复用，正是新进程将 bind 的端口）已释放。
// 返回超时后仍被占用的端口数量（0 表示全部已释放）。
// 说明：这是"确认旧监听已消失"的探测——能连上说明旧进程仍在监听该端口，需继续等待；
// 连不上（连接被拒）说明端口已释放，可安全 bind。带上限超时，避免无限等待。
func (s *SingBoxProcess) waitPortsFreeLocked(timeout time.Duration) int {
	ports := make([]int, 0, len(s.portMap))
	for _, port := range s.portMap {
		ports = append(ports, port)
	}
	if len(ports) == 0 {
		return 0
	}

	portBusy := func(port int) bool {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err != nil {
			return false // 连接失败 = 端口已释放
		}
		conn.Close()
		return true // 能连上 = 旧监听仍在
	}

	deadline := time.Now().Add(timeout)
	for {
		busy := 0
		for _, port := range ports {
			if portBusy(port) {
				busy++
			}
		}
		if busy == 0 {
			return 0
		}
		if time.Now().After(deadline) {
			return busy
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func (s *SingBoxProcess) countReadyPortsLocked(timeout time.Duration) int {
	ready := 0
	dial := func(port int) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), timeout)
		if err == nil {
			conn.Close()
			ready++
		}
	}
	for _, port := range s.portMap {
		dial(port)
	}
	return ready
}

func classifySingBoxStartError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "未找到"):
		return "binary_not_found"
	case strings.Contains(msg, "配置无效"):
		return "config_invalid"
	case strings.Contains(msg, "立即退出"):
		return "process_exited_on_start"
	case strings.Contains(msg, "未分配"):
		return "no_ports_allocated"
	case strings.Contains(msg, "未完全就绪"):
		return "ports_not_ready"
	case strings.Contains(msg, "不完整"):
		return "ports_incomplete"
	default:
		return "start_failed"
	}
}

// stopGracePeriod 是 Unix 上先发 Interrupt 后等待进程自行退出的上限。
// Windows 上 os.Interrupt 未实现，必须直接 Kill，否则会空等并在测试超时后泄漏子进程。
const stopGracePeriod = 2 * time.Second

// stopLocked 停止 sing-box（需持有锁）。
// 必须尽量在超时前真正回收子进程：真实集成测试的 t.Cleanup 若卡住或空等，
// 会留下监听 mixed 端口的 sing-box，阻断本机其它 API/代理探测。
func (s *SingBoxProcess) stopLocked() {
	if s.cmd == nil || s.cmd.Process == nil || !s.running {
		return
	}
	log.Println("[custom] 停止 sing-box 进程...")
	cmd := s.cmd
	done := s.waitDone
	if done == nil {
		done = make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(done)
		}()
	}

	// Windows: Signal(Interrupt) 固定返回 error 且不投递信号，直接 Kill。
	// Unix: 先尝试 Interrupt，超时再 Kill。
	if runtime.GOOS == "windows" {
		_ = cmd.Process.Kill()
	} else if err := cmd.Process.Signal(os.Interrupt); err != nil {
		_ = cmd.Process.Kill()
	}

	select {
	case <-done:
	case <-time.After(stopGracePeriod):
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(stopGracePeriod):
			log.Println("[custom] ⚠️ 等待 sing-box 退出超时，进程可能仍残留")
		}
	}

	s.running = false
	if s.cmd == cmd {
		s.cmd = nil
		s.waitDone = nil
	}
	if len(s.portMap) > 0 {
		s.setStatusLocked(SingBoxStatusStopped, SingBoxStatusStopped, 0)
	} else {
		s.setStatusLocked(SingBoxStatusNoTunnelNodes, SingBoxStatusNoTunnelNodes, 0)
	}
}

// Stop 停止 sing-box
func (s *SingBoxProcess) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

// IsRunning 检查进程是否运行中
func (s *SingBoxProcess) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// GetRuntimeStatus 获取可解释的 sing-box 运行态。
func (s *SingBoxProcess) GetRuntimeStatus() SingBoxRuntimeStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := s.status
	reason := s.reason
	if status == "" {
		switch {
		case len(s.portMap) == 0:
			status = SingBoxStatusNoTunnelNodes
			reason = SingBoxStatusNoTunnelNodes
		case s.running:
			status = SingBoxStatusRunning
			reason = SingBoxStatusRunning
		default:
			status = SingBoxStatusStopped
			reason = SingBoxStatusStopped
		}
	}
	return SingBoxRuntimeStatus{
		Running:    s.running,
		Status:     status,
		Reason:     reason,
		Nodes:      len(s.portMap),
		ReadyPorts: s.readyPorts,
		TotalPorts: len(s.portMap),
	}
}

// GetLocalAddress 获取节点的本地 SOCKS5 地址
func (s *SingBoxProcess) GetLocalAddress(nodeKey string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if port, ok := s.portMap[nodeKey]; ok {
		return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	}
	return ""
}

// GetPortMap 获取所有 SOCKS5 端口映射
func (s *SingBoxProcess) GetPortMap() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[string]int, len(s.portMap))
	for k, v := range s.portMap {
		result[k] = v
	}
	return result
}

// GetAssemblyDiagnostics returns a snapshot of the latest configuration
// assembly so orchestrators can distinguish rejected nodes from incomplete
// allocation without inferring reasons from a missing port.
func (s *SingBoxProcess) GetAssemblyDiagnostics() assemblyDiagnostics {
	s.mu.Lock()
	defer s.mu.Unlock()
	diagnostics := assemblyDiagnostics{
		accepted:    append([]ParsedNode(nil), s.assembly.accepted...),
		segmentFull: append([]ParsedNode(nil), s.assembly.segmentFull...),
		rejected:    append([]assemblyRejectedNode(nil), s.assembly.rejected...),
	}
	return diagnostics
}

// GetNodes 返回当前已加载的 tunnel 节点快照。
func (s *SingBoxProcess) GetNodes() []ParsedNode {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]ParsedNode, len(s.nodes))
	copy(result, s.nodes)
	return result
}

// GetNodeCount 获取管理的节点数
func (s *SingBoxProcess) GetNodeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.portMap)
}

// 辅助函数

func getStr(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getStrDefault(m map[string]interface{}, key, def string) string {
	if s := getStr(m, key); s != "" {
		return s
	}
	return def
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case float64:
			return int(val)
		case string:
			n, _ := strconv.Atoi(val)
			return n
		}
	}
	return 0
}

func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case bool:
			return val
		case string:
			return val == "true"
		}
	}
	return false
}

func parseSpeed(s string) int {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, " Mbps")
	s = strings.TrimSuffix(s, "Mbps")
	n, _ := strconv.Atoi(s)
	if n == 0 {
		n = 100 // 默认 100 Mbps
	}
	return n
}
