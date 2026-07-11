package custom

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"goproxy/config"
	"goproxy/storage"
	"goproxy/validator"
)

// Manager 订阅管理器
type Manager struct {
	storage   *storage.Storage
	validator *validator.Validator
	singbox   singBoxShard
	stopCh    chan struct{}
	refreshMu sync.Mutex // 防止并发刷新
}

// NewManager 创建订阅管理器
func NewManager(store *storage.Storage, v *validator.Validator, cfg *config.Config) *Manager {
	dataDir := ""
	if d := os.Getenv("DATA_DIR"); d != "" {
		dataDir = d
	}

	return &Manager{
		storage:   store,
		validator: v,
		singbox:   NewShardedSingBox(cfg.SingBoxPath, dataDir, cfg.SingBoxBasePort, cfg.SingBoxShardCount),
		stopCh:    make(chan struct{}),
	}
}

// Start 启动后台循环
func (m *Manager) Start() {
	log.Println("[custom] 订阅管理器启动")

	// 启动时立即刷新所有订阅
	go m.initialRefresh()

	// 订阅刷新循环
	go m.refreshLoop()

	// 探测唤醒循环
	go m.probeLoop()
}

// Stop 停止管理器
func (m *Manager) Stop() {
	close(m.stopCh)
	m.singbox.Stop()
	log.Println("[custom] 订阅管理器已停止")
}

// initialRefresh 启动时刷新所有活跃订阅
func (m *Manager) initialRefresh() {
	time.Sleep(3 * time.Second) // 等待其他模块初始化
	subs, err := m.storage.GetSubscriptions()
	if err != nil || len(subs) == 0 {
		return
	}

	activeSubs := 0
	for _, sub := range subs {
		if sub.Status == "active" {
			activeSubs++
		}
	}
	if activeSubs == 0 {
		return
	}

	log.Printf("[custom] 启动刷新，共 %d 个活跃订阅", activeSubs)
	m.RefreshAll()
}

// refreshLoop 订阅刷新循环
func (m *Manager) refreshLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.checkAndRefresh()
		}
	}
}

// checkAndRefresh 检查并刷新到期的订阅 + 暂停长期无可用节点的订阅
func (m *Manager) checkAndRefresh() {
	// 暂停连续 7 天无可用节点的订阅，保留订阅和节点记录供人工排查。
	m.cleanupStaleSubscriptions()

	subs, err := m.storage.GetSubscriptions()
	if err != nil {
		log.Printf("[custom] 获取订阅列表失败: %v", err)
		return
	}

	for _, sub := range subs {
		if sub.Status != "active" {
			continue
		}
		// 检查是否到刷新时间
		if !sub.LastFetch.IsZero() && time.Since(sub.LastFetch) < time.Duration(sub.RefreshMin)*time.Minute {
			continue
		}
		log.Printf("[custom] 🔄 订阅 [%s] 到期，开始刷新", sub.Name)
		if err := m.RefreshSubscription(sub.ID); err != nil {
			log.Printf("[custom] ❌ 订阅 [%s] 刷新失败: %v", sub.Name, err)
		}
	}
}

// cleanupStaleSubscriptions 暂停连续 7 天无可用节点的订阅
func (m *Manager) cleanupStaleSubscriptions() {
	staleSubs, err := m.storage.GetStaleSubscriptions(7)
	if err != nil || len(staleSubs) == 0 {
		return
	}

	for _, sub := range staleSubs {
		if err := m.storage.PauseSubscription(sub.ID); err != nil {
			log.Printf("[custom] ⚠️ 暂停订阅 [%s] 失败: %v", sub.Name, err)
			continue
		}
		log.Printf("[custom] ⚠️ 暂停订阅 [%s]：连续 7 天无可用节点，已保留订阅和节点记录", sub.Name)
	}
}

// probeLoop 探测唤醒循环
func (m *Manager) probeLoop() {
	// 等待初始化完成
	time.Sleep(5 * time.Second)

	for {
		cfg := config.Get()
		interval := time.Duration(cfg.CustomProbeInterval) * time.Minute
		if interval < time.Minute {
			interval = 10 * time.Minute
		}

		select {
		case <-m.stopCh:
			return
		case <-time.After(interval):
			m.probeDisabled()
		}
	}
}

// probeDisabled 探测被禁用的订阅代理
func (m *Manager) probeDisabled() {
	disabled, err := m.storage.GetDisabledCustomProxies()
	if err != nil || len(disabled) == 0 {
		return
	}

	log.Printf("[custom] 🔍 探测 %d 个禁用的订阅代理", len(disabled))

	cfg := config.Get()
	recovered := 0
	recoveredSubs := make(map[int64]bool)
	for _, proxy := range disabled {
		valid, latency, exitIP, exitLocation, risk := m.validator.ValidateOne(proxy)
		if valid {
			// 检查地理过滤：恢复前确认不在屏蔽列表中
			if exitLocation != "" && isGeoBlocked(exitLocation, cfg) {
				log.Printf("[custom] 代理 %s 验证通过但被地理过滤 (%s)，保持禁用", proxy.Address, exitLocation)
				m.storage.UpdateSubscriptionProxyExitInfo(proxy.Address, proxy.SubscriptionID, exitIP, exitLocation, int(latency.Milliseconds()), risk.IPAPIIsScore, risk.Flags, risk.CFBlocked)
				continue
			}
			m.storage.EnableSubscriptionProxy(proxy.Address, proxy.SubscriptionID)
			m.storage.UpdateSubscriptionProxyExitInfo(proxy.Address, proxy.SubscriptionID, exitIP, exitLocation, int(latency.Milliseconds()), risk.IPAPIIsScore, risk.Flags, risk.CFBlocked)
			recovered++
			recoveredSubs[proxy.SubscriptionID] = true
			log.Printf("[custom] ✅ 代理 %s 恢复可用 (%dms)", proxy.Address, latency.Milliseconds())
		}
	}
	// 有恢复的代理则更新对应订阅的 last_success
	for subID := range recoveredSubs {
		if subID > 0 {
			m.storage.UpdateSubscriptionSuccess(subID)
		}
	}

	if recovered > 0 {
		log.Printf("[custom] 探测完成：%d/%d 恢复可用", recovered, len(disabled))
	}
}

// RefreshSubscription 刷新��个订阅
func (m *Manager) RefreshSubscription(subID int64) error {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()

	sub, err := m.storage.GetSubscription(subID)
	if err != nil {
		return fmt.Errorf("获取订阅失败: %w", err)
	}

	// 获取订阅内容
	data, err := m.fetchSubscriptionData(sub)
	if err != nil {
		return fmt.Errorf("拉取订阅内容失败: %w", err)
	}

	// 解析节点
	nodes, err := Parse(data, sub.Format)
	if err != nil {
		return fmt.Errorf("解析订阅内容失败: %w", err)
	}

	if len(nodes) == 0 {
		log.Printf("[custom] ⚠️ 订阅 [%s] 无有效节点", sub.Name)
		return nil
	}

	log.Printf("[custom] 订阅 [%s] 解析到 %d 个节点", sub.Name, len(nodes))

	// 分类节点
	var directNodes []ParsedNode
	var tunnelNodes []ParsedNode
	for _, node := range nodes {
		if node.IsDirect() {
			directNodes = append(directNodes, node)
		} else {
			tunnelNodes = append(tunnelNodes, node)
		}
	}

	// 收集所有入池的代理（带正确的协议信息）
	var allProxies []storage.Proxy
	// 端口合并：每个 tunnel 节点只暴露一个 mixed 本地入站（单端口同时服务 SOCKS5 与 HTTP），
	// 作为单条代理入库。协议登记为 socks5（mixed 端口接受 SOCKS5 连接，代理拨号路径按 socks5 处理）。
	type tunnelProxy struct {
		addr  string
		proto string
	}
	var tunnelProxies []tunnelProxy

	// 先重载 tunnel；失败时不删除该订阅旧代理，避免一次坏配置破坏旧可用配置。
	if len(tunnelNodes) > 0 {
		// 收集所有订阅的 tunnel 节点（需合并）
		allTunnelNodes, err := m.collectAllTunnelNodes()
		if err != nil {
			log.Printf("[custom] ⚠️ 收集 tunnel 节点失败: %v", err)
		}
		// 将当前订阅的 tunnel 节点也加入，去重
		nodeMap := make(map[string]ParsedNode)
		for _, n := range allTunnelNodes {
			nodeMap[n.NodeKey()] = n
		}
		for _, n := range tunnelNodes {
			nodeMap[n.NodeKey()] = n
		}
		var mergedNodes []ParsedNode
		for _, n := range nodeMap {
			mergedNodes = append(mergedNodes, n)
		}

		if err := m.singbox.Reload(mergedNodes); err != nil {
			return fmt.Errorf("sing-box 重载失败: %w", err)
		} else {
			portMap := m.singbox.GetPortMap()
			for _, node := range tunnelNodes {
				key := node.NodeKey()
				if port, ok := portMap[key]; ok {
					addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
					tunnelProxies = append(tunnelProxies, tunnelProxy{addr: addr, proto: "socks5"})
				}
			}
		}
	}

	// 到这里才替换该订阅代理：拉取/解析/隧道重载失败都不会破坏旧可用配置。
	oldDeleted, _ := m.storage.DeleteBySubscriptionID(subID)
	if oldDeleted > 0 {
		log.Printf("[custom] 🧹 清理订阅 [%s] 旧代理 %d 个", sub.Name, oldDeleted)
	}

	// 处理可直接使用的 HTTP/SOCKS5 节点。新节点先保持 disabled，验证通过后才进入 active 选路。
	for _, node := range directNodes {
		addr := node.DirectAddress()
		proto := node.DirectProtocol()
		proxy, ok := m.addPendingSubscriptionProxy(addr, proto, subID, false)
		if ok {
			allProxies = append(allProxies, *proxy)
		}
	}
	if len(directNodes) > 0 {
		log.Printf("[custom] 📥 %d 个 HTTP/SOCKS5 节点直接入池", len(directNodes))
	}

	for _, tp := range tunnelProxies {
		proxy, ok := m.addPendingSubscriptionProxy(tp.addr, tp.proto, subID, true)
		if ok {
			allProxies = append(allProxies, *proxy)
		}
	}
	if len(tunnelProxies) > 0 {
		log.Printf("[custom] 📥 %d 个加密节点入站（单 mixed 端口，SOCKS5/HTTP 共用）通过 sing-box 转换入池", len(tunnelProxies))
	}

	// 验证新入池的代理
	m.validateCustomProxies(allProxies, subID)

	// 更新订阅信息（记录实际入池的代理数）
	m.storage.UpdateSubscriptionFetch(subID, len(allProxies))
	log.Printf("[custom] ✅ 订阅 [%s] 刷新完成，解析 %d 节点，入池 %d 个", sub.Name, len(nodes), len(allProxies))

	return nil
}

// RefreshAll 刷新所有活跃订阅
func (m *Manager) RefreshAll() {
	subs, err := m.storage.GetSubscriptions()
	if err != nil {
		log.Printf("[custom] 获取订阅列表失败: %v", err)
		return
	}

	for _, sub := range subs {
		if sub.Status != "active" {
			continue
		}
		if err := m.RefreshSubscription(sub.ID); err != nil {
			log.Printf("[custom] ❌ 订阅 [%s] 刷新失败: %v", sub.Name, err)
		}
	}
}

// collectAllTunnelNodes 收集所有订阅中需要 tunnel 的节点
func (m *Manager) collectAllTunnelNodes() ([]ParsedNode, error) {
	nodeMap := make(map[string]ParsedNode)
	for _, node := range m.singbox.GetNodes() {
		nodeMap[node.NodeKey()] = node
	}

	subs, err := m.storage.GetSubscriptions()
	if err != nil {
		return mapValues(nodeMap), err
	}

	for _, sub := range subs {
		if sub.Status != "active" {
			continue
		}
		data, err := m.fetchSubscriptionData(&sub)
		if err != nil {
			continue
		}
		nodes, err := Parse(data, sub.Format)
		if err != nil {
			continue
		}
		for _, node := range nodes {
			if !node.IsDirect() {
				nodeMap[node.NodeKey()] = node
			}
		}
	}
	return mapValues(nodeMap), nil
}

func mapValues(nodeMap map[string]ParsedNode) []ParsedNode {
	nodes := make([]ParsedNode, 0, len(nodeMap))
	for _, node := range nodeMap {
		nodes = append(nodes, node)
	}
	return nodes
}

// addPendingSubscriptionProxy 新增一条订阅代理并标记为待验证。
// dualProtocol=true 表示该地址是单 mixed 端口（同时服务 SOCKS5+HTTP）的隧道节点，
// 显式落库供前端可靠渲染双协议标签，而非靠地址长相猜测。
func (m *Manager) addPendingSubscriptionProxy(addr, proto string, subID int64, dualProtocol bool) (*storage.Proxy, bool) {
	if err := m.storage.AddProxyWithSource(addr, proto, storage.SourceSubscription, subID); err != nil {
		log.Printf("[custom] ⚠️ 新增订阅代理 %s 失败: %v", addr, err)
		return nil, false
	}
	if err := m.storage.DisableSubscriptionProxy(addr, subID); err != nil {
		log.Printf("[custom] ⚠️ 将订阅代理 %s 标记为待验证失败: %v", addr, err)
		return nil, false
	}
	proxy, err := m.storage.GetProxyByIdentity(addr, storage.SourceSubscription, subID)
	if err != nil {
		log.Printf("[custom] ⚠️ 读取订阅代理 %s 身份失败: %v", addr, err)
		return nil, false
	}
	if dualProtocol {
		if err := m.storage.SetProxyDualProtocol(proxy.ID, true); err != nil {
			log.Printf("[custom] ⚠️ 标记订阅代理 %s 双协议失败: %v", addr, err)
		} else {
			proxy.DualProtocol = true
		}
	}
	return proxy, true
}

// fetchSubscriptionData 获取订阅数据
func (m *Manager) fetchSubscriptionData(sub *storage.Subscription) ([]byte, error) {
	// 优先使用本地文件
	if sub.FilePath != "" {
		data, err := os.ReadFile(sub.FilePath)
		if err != nil {
			return nil, fmt.Errorf("读取文件 %s 失败: %w", sub.FilePath, err)
		}
		return data, nil
	}

	// 从 URL 拉取
	if sub.URL == "" {
		return nil, fmt.Errorf("订阅未配置 URL 或文件路径")
	}

	data, err := m.fetchSubscriptionURL(sub.URL, sub.Headers)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// fetchSubscriptionURL 直连拉取订阅 URL；失败时显式返回错误，不通过上游节点兜底。
// headersJSON 为订阅自定义请求头的 JSON 对象字符串（如 {"User-Agent":"clash.meta"}）。
// 向后兼容：先设默认 User-Agent: v2rayN，再逐个应用自定义头——自定义头覆盖默认，
// 未指定 User-Agent 时保留默认 v2rayN，不破坏现有订阅拉取。
func (m *Manager) fetchSubscriptionURL(urlStr, headersJSON string) ([]byte, error) {
	transport := &http.Transport{}

	client := &http.Client{Timeout: 30 * time.Second, Transport: transport}
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	// 默认用 v2rayN UA，大部分机场都会返回完整的节点信息。
	req.Header.Set("User-Agent", "v2rayN")
	// 应用订阅自定义请求头：解析成功则逐个 Set（覆盖同名默认头）；
	// 空或非法 JSON 时静默保留默认头，保证向后兼容。
	if headersJSON != "" {
		var custom map[string]string
		if err := json.Unmarshal([]byte(headersJSON), &custom); err == nil {
			for k, v := range custom {
				if k != "" {
					req.Header.Set(k, v)
				}
			}
		} else {
			log.Printf("[custom] ⚠️ 订阅自定义请求头解析失败，回退默认 UA: %v", err)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("直接拉取订阅 URL 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("直接拉取订阅 URL 返回 HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// validateCustomProxies 验证订阅代理，返回可用数
func (m *Manager) validateCustomProxies(proxies []storage.Proxy, subID int64) int {
	if len(proxies) == 0 {
		return 0
	}

	log.Printf("[custom] 🔍 开始验证 %d 个订阅代理", len(proxies))

	cfg := config.Get()
	resultCh := m.validator.ValidateStream(proxies)
	valid, invalid := 0, 0
	for result := range resultCh {
		if result.Valid {
			latencyMs := int(result.Latency.Milliseconds())
			m.storage.UpdateSubscriptionProxyExitInfo(result.Proxy.Address, subID, result.ExitIP, result.ExitLocation, latencyMs, result.Risk.IPAPIIsScore, result.Risk.Flags, result.Risk.CFBlocked)
			// 检查地理过滤
			if result.ExitLocation != "" && isGeoBlocked(result.ExitLocation, cfg) {
				m.storage.DisableSubscriptionProxy(result.Proxy.Address, subID)
				invalid++
			} else {
				m.storage.EnableSubscriptionProxy(result.Proxy.Address, subID)
				valid++
			}
		} else {
			invalid++
			m.storage.DisableSubscriptionProxy(result.Proxy.Address, subID)
		}
	}

	// 有可用节点则更新 last_success
	if valid > 0 && subID > 0 {
		m.storage.UpdateSubscriptionSuccess(subID)
	}

	log.Printf("[custom] 验证完成：%d 可用，%d 不可用", valid, invalid)
	return valid
}

// GetStatus 获取订阅管理器状态
func (m *Manager) GetStatus() map[string]interface{} {
	subscriptionCount, _ := m.storage.CountBySource(storage.SourceSubscription)
	disabled, _ := m.storage.GetDisabledCustomProxies()
	subs, _ := m.storage.GetSubscriptions()
	singboxStatus := m.singbox.GetRuntimeStatus()

	return map[string]interface{}{
		"singbox_running":     singboxStatus.Running,
		"singbox_status":      singboxStatus.Status,
		"singbox_reason":      singboxStatus.Reason,
		"singbox_nodes":       singboxStatus.Nodes,
		"singbox_ready_ports": singboxStatus.ReadyPorts,
		"singbox_total_ports": singboxStatus.TotalPorts,
		"subscription_count":  subscriptionCount,
		"disabled_count":      len(disabled),
		"subscription_total":  len(subs),
	}
}

// ValidateSubscription 验证订阅能否解析出节点（不入库，仅检查）。
// headersJSON 为订阅自定义请求头 JSON，用于 URL 拉取——保证对默认 UA 返回 401 的机场
// 在添加校验阶段也能带上自定义头，与后续刷新拉取行为一致。
func (m *Manager) ValidateSubscription(url, filePath, headersJSON string) (int, error) {
	var data []byte
	var err error

	if filePath != "" {
		data, err = os.ReadFile(filePath)
		if err != nil {
			return 0, fmt.Errorf("读取文件失败: %w", err)
		}
	} else if url != "" {
		data, err = m.fetchSubscriptionURL(url, headersJSON)
		if err != nil {
			return 0, err
		}
	} else {
		return 0, fmt.Errorf("未提供 URL 或文件")
	}

	nodes, err := Parse(data, "auto")
	if err != nil {
		return 0, err
	}
	if len(nodes) == 0 {
		return 0, fmt.Errorf("解析结果为空，未找到有效代理节点")
	}

	return len(nodes), nil
}

// isGeoBlocked 检查代理出口位置是否被地理过滤
func isGeoBlocked(exitLocation string, cfg *config.Config) bool {
	if exitLocation == "" || len(exitLocation) < 2 {
		return false
	}
	countryCode := exitLocation[:2]

	// 白名单模式优先
	if len(cfg.AllowedCountries) > 0 {
		for _, allowed := range cfg.AllowedCountries {
			if countryCode == allowed {
				return false
			}
		}
		return true // 不在白名单中
	}

	// 黑名单模式
	for _, blocked := range cfg.BlockedCountries {
		if countryCode == blocked {
			return true
		}
	}
	return false
}

// GetSingBox 获取 sing-box 编排器（singBoxShard 接口）。
func (m *Manager) GetSingBox() singBoxShard {
	return m.singbox
}

// AddManualNode 从单个链接添加人工节点，存储为 source=manual。
// 直连节点（http/socks5）直接入库；加密节点通过 sing-box 转换为本地 socks5 后入库。
func (m *Manager) AddManualNode(link, region, note string) error {
	node, err := ParseSingleLink(link)
	if err != nil {
		return fmt.Errorf("解析节点链接失败: %w", err)
	}

	if node.IsDirect() {
		addr := node.DirectAddress()
		proto := node.DirectProtocol()
		if err := m.storage.AddManualProxy(addr, proto, region, note); err != nil {
			return fmt.Errorf("存储直连节点失败: %w", err)
		}
		return nil
	}

	return m.addManualTunnelNode(node, region, note)
}

// addManualTunnelNode 处理加密节点：合并进现有 sing-box 节点集后重载，取本地端口入库。
func (m *Manager) addManualTunnelNode(node *ParsedNode, region, note string) error {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()

	mergedNodes := m.mergeWithExistingTunnelNodes(node)
	if err := m.singbox.Reload(mergedNodes); err != nil {
		return fmt.Errorf("sing-box 重载失败: %w", err)
	}

	key := node.NodeKey()
	socksPort, ok := m.singbox.GetPortMap()[key]
	if !ok {
		return fmt.Errorf("sing-box 未为节点 %s 分配本地端口", key)
	}

	// 端口合并：手动加密节点只暴露一个 mixed 本地入站（单端口同时服务 SOCKS5 与 HTTP），
	// 入库为单条 socks5 记录。前端复制时可按需以 socks5:// 或 http:// scheme 呈现同一 IP:port。
	mixedAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(socksPort))
	if err := m.storage.AddManualProxy(mixedAddr, "socks5", region, note); err != nil {
		return fmt.Errorf("存储人工节点入站失败: %w", err)
	}
	// 手动加密节点是 mixed 端口（单端口同时服务 SOCKS5+HTTP），显式置位 dual_protocol，
	// 供前端可靠渲染双协议标签，而非靠地址长相猜测。
	if p, err := m.storage.GetProxyByAddress(mixedAddr); err == nil {
		if err := m.storage.SetProxyDualProtocol(p.ID, true); err != nil {
			log.Printf("[custom] ⚠️ 置位手动节点 dual_protocol 失败: %v", err)
		}
	}
	return nil
}

// mergeWithExistingTunnelNodes 将人工节点合并进现有订阅 tunnel 节点集，按 NodeKey 去重，
// 避免 Reload 时驱逐已有订阅 tunnel 节点。
func (m *Manager) mergeWithExistingTunnelNodes(node *ParsedNode) []ParsedNode {
	allTunnelNodes, err := m.collectAllTunnelNodes()
	if err != nil {
		log.Printf("[custom] ⚠️ 收集 tunnel 节点失败: %v", err)
	}

	nodeMap := make(map[string]ParsedNode)
	for _, n := range allTunnelNodes {
		nodeMap[n.NodeKey()] = n
	}
	nodeMap[node.NodeKey()] = *node

	mergedNodes := make([]ParsedNode, 0, len(nodeMap))
	for _, n := range nodeMap {
		mergedNodes = append(mergedNodes, n)
	}
	return mergedNodes
}
