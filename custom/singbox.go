package custom

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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
	portMap    map[string]int // nodeKey → 本地 SOCKS5 端口
	httpPortMap map[string]int // nodeKey → 本地 HTTP 端口（方案 B：同一 tunnel 节点额外暴露 HTTP 入站）
	nodes      []ParsedNode
	mu         sync.Mutex
	running    bool
	status     string
	reason     string
	readyPorts int
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
		httpPortMap: make(map[string]int),
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
		s.httpPortMap = make(map[string]int)
		s.setStatusLocked(SingBoxStatusNoTunnelNodes, SingBoxStatusNoTunnelNodes, 0)
		return nil
	}

	// 保持当前端口范围，assembleConfig 会复用已加载节点端口并为新节点追加端口。
	nextPortOffset := s.portOffset

	// 生成配置
	oldPortOffset := s.portOffset
	oldPortMap := s.portMap
	oldHTTPPortMap := s.httpPortMap
	oldNodes := s.nodes
	oldConfig, oldConfigErr := os.ReadFile(s.configFile)
	restoreState := func() {
		s.portOffset = oldPortOffset
		s.portMap = oldPortMap
		s.httpPortMap = oldHTTPPortMap
		s.nodes = oldNodes
		if oldConfigErr == nil {
			if err := os.WriteFile(s.configFile, oldConfig, 0644); err != nil {
				log.Printf("[custom] ⚠️ 恢复旧 sing-box 配置文件失败: %v", err)
			}
		}
	}
	s.portOffset = nextPortOffset
	if err := s.generateConfig(tunnelNodes); err != nil {
		restoreState()
		s.setStatusLocked(SingBoxStatusFailed, "config_generation_failed", 0)
		return fmt.Errorf("生成 sing-box 配置失败: %w", err)
	}
	if _, err := exec.LookPath(s.binPath); err != nil {
		restoreState()
		s.setStatusLocked(SingBoxStatusFailed, "binary_not_found", 0)
		return fmt.Errorf("sing-box 未找到: %s（请安装 sing-box 或设置 SINGBOX_PATH）", s.binPath)
	}

	// 健壮性：整份配置若校验失败，二分剔除坏节点，用剩余可用节点重建，
	// 避免单个非法节点拖垮全部订阅（一坏全灭）。
	goodNodes := s.pruneInvalidNodes(tunnelNodes)
	if len(goodNodes) == 0 {
		restoreState()
		s.setStatusLocked(SingBoxStatusFailed, "all_tunnel_nodes_invalid", 0)
		return fmt.Errorf("所有隧道节点均无法通过 sing-box 校验")
	}
	if len(goodNodes) != len(tunnelNodes) {
		log.Printf("[custom] ⚠️ 剔除 %d 个非法节点，保留 %d 个可用节点",
			len(tunnelNodes)-len(goodNodes), len(goodNodes))
		if err := s.generateConfig(goodNodes); err != nil {
			restoreState()
			s.setStatusLocked(SingBoxStatusFailed, "config_rebuild_failed", 0)
			return fmt.Errorf("重建 sing-box 配置失败: %w", err)
		}
	}

	// 重启进程
	s.stopLocked()
	if err := s.startLocked(); err != nil {
		restoreState()
		s.setStatusLocked(SingBoxStatusFailed, classifySingBoxStartError(err), 0)
		return fmt.Errorf("启动 sing-box 失败: %w", err)
	}

	s.nodes = goodNodes
	return nil
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
func (s *SingBoxProcess) generateConfig(nodes []ParsedNode) error {
	config, portMap, httpPortMap := s.assembleConfig(nodes)
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	s.portMap = portMap
	s.httpPortMap = httpPortMap
	return os.WriteFile(s.configFile, data, 0644)
}

// buildConfigBytes 生成配置 JSON 字节，但不改动运行态；供 checkNodes 探测校验使用。
func (s *SingBoxProcess) buildConfigBytes(nodes []ParsedNode) ([]byte, error) {
	config, _, _ := s.assembleConfig(nodes)
	return json.MarshalIndent(config, "", "  ")
}

// assembleConfig 组装 sing-box 配置 map 及对应的 nodeKey→port 映射，无副作用（不写状态/文件）。
// 方案 B：每个 tunnel 节点同时生成 SOCKS5 与 HTTP 两个本地入站（各占一个端口），
// 两个入站均路由到同一出站。返回 (config, socksPortMap, httpPortMap)。
// SOCKS5 端口沿用旧映射保持稳定（不破坏既有入库地址）；HTTP 端口为方案 B 新增，
// 同样从旧 httpPortMap 复用以保持稳定。两类端口共用同一 usedPorts 池避免冲突。
func (s *SingBoxProcess) assembleConfig(nodes []ParsedNode) (map[string]interface{}, map[string]int, map[string]int) {
	oldPortMap := make(map[string]int, len(s.portMap))
	for key, port := range s.portMap {
		oldPortMap[key] = port
	}
	oldHTTPPortMap := make(map[string]int, len(s.httpPortMap))
	for key, port := range s.httpPortMap {
		oldHTTPPortMap[key] = port
	}
	portMap := make(map[string]int, len(nodes))
	httpPortMap := make(map[string]int, len(nodes))
	usedPorts := make(map[int]bool, len(oldPortMap)+len(oldHTTPPortMap))
	maxPort := s.basePort + s.portOffset
	for _, port := range oldPortMap {
		usedPorts[port] = true
		if port > maxPort {
			maxPort = port
		}
	}
	for _, port := range oldHTTPPortMap {
		usedPorts[port] = true
		if port > maxPort {
			maxPort = port
		}
	}
	orderedNodes := append([]ParsedNode(nil), nodes...)
	sort.SliceStable(orderedNodes, func(i, j int) bool {
		return orderedNodes[i].NodeKey() < orderedNodes[j].NodeKey()
	})
	port := maxPort

	// allocPort 复用旧端口以保持稳定，否则从 usedPorts 池分配一个未占用端口。
	allocPort := func(key string, oldMap map[string]int) int {
		if p, ok := oldMap[key]; ok {
			return p
		}
		for {
			port++
			if !usedPorts[port] {
				break
			}
		}
		usedPorts[port] = true
		return port
	}

	var inbounds []map[string]interface{}
	var outbounds []map[string]interface{}
	var rules []map[string]interface{}

	for i, node := range orderedNodes {
		key := node.NodeKey()
		tag := fmt.Sprintf("node-%d", i)

		// 出站：根据节点类型生成（失败节点跳过，不占端口）
		outbound, err := buildOutbound(node, tag)
		if err != nil {
			log.Printf("[custom] 跳过节点 %s (%s): %v", node.Name, node.Type, err)
			continue
		}

		socksPort := allocPort(key, oldPortMap)
		portMap[key] = socksPort
		httpPort := allocPort(key, oldHTTPPortMap)
		httpPortMap[key] = httpPort

		// 入站：本地 SOCKS5 监听
		inbounds = append(inbounds, map[string]interface{}{
			"type":        "socks",
			"tag":         fmt.Sprintf("in-%s", tag),
			"listen":      "127.0.0.1",
			"listen_port": socksPort,
		})
		// 入站：本地 HTTP 监听（方案 B），与 SOCKS5 指向同一出站
		inbounds = append(inbounds, map[string]interface{}{
			"type":        "http",
			"tag":         fmt.Sprintf("in-%s-http", tag),
			"listen":      "127.0.0.1",
			"listen_port": httpPort,
		})
		outbounds = append(outbounds, outbound)

		// 路由规则：两个入站 → 同一出站
		rules = append(rules, map[string]interface{}{
			"inbound":  []string{fmt.Sprintf("in-%s", tag), fmt.Sprintf("in-%s-http", tag)},
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
	return config, portMap, httpPortMap
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
	raw["tls"] = true
	applyTLS(raw, out)
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

	if fp := getStr(raw, "client-fingerprint"); fp != "" {
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
	totalPorts := len(s.portMap) + len(s.httpPortMap)
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
	} else {
		s.setStatusLocked(SingBoxStatusRunning, SingBoxStatusRunning, readyPorts)
		log.Printf("[custom] ✅ sing-box 启动成功，管理 %d 个节点", totalPorts)
	}

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
// （s.portMap + s.httpPortMap，因端口稳定复用，正是新进程将 bind 的端口）已释放。
// 返回超时后仍被占用的端口数量（0 表示全部已释放）。
// 说明：这是"确认旧监听已消失"的探测——能连上说明旧进程仍在监听该端口，需继续等待；
// 连不上（连接被拒）说明端口已释放，可安全 bind。带上限超时，避免无限等待。
func (s *SingBoxProcess) waitPortsFreeLocked(timeout time.Duration) int {
	ports := make([]int, 0, len(s.portMap)+len(s.httpPortMap))
	for _, port := range s.portMap {
		ports = append(ports, port)
	}
	for _, port := range s.httpPortMap {
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
	for _, port := range s.httpPortMap {
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
	default:
		return "start_failed"
	}
}

// stopLocked 停止 sing-box（需持有锁）
func (s *SingBoxProcess) stopLocked() {
	if s.cmd != nil && s.cmd.Process != nil && s.running {
		log.Println("[custom] 停止 sing-box 进程...")
		cmd := s.cmd
		done := s.waitDone
		cmd.Process.Signal(os.Interrupt)
		if done == nil {
			done = make(chan struct{})
			go func() {
				cmd.Wait()
				close(done)
			}()
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			<-done
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
		TotalPorts: len(s.portMap) + len(s.httpPortMap),
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

// GetHTTPPortMap 获取所有 HTTP 端口映射（方案 B：tunnel 节点的 HTTP 入站端口）
func (s *SingBoxProcess) GetHTTPPortMap() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[string]int, len(s.httpPortMap))
	for k, v := range s.httpPortMap {
		result[k] = v
	}
	return result
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
