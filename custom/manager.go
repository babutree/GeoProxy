package custom

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/storage"
	"github.com/babutree/GeoProxy/validator"
)

// longTermDisabledRetention 禁用满 1 天视为长期禁用，移出 sing-box 释放端口。
// status=disabled 且 last_check 早于此时长则剔除；last_check 为零视为短期禁用，保留端口供重验证。
const longTermDisabledRetention = 24 * time.Hour

// 订阅直连拉取失败诊断与有限即时重试；调度退避由 RefreshMin 单独控制。
const (
	// subscriptionResponseSnippetMaxBytes 非 200 错误中附带的响应体片段上限。
	subscriptionResponseSnippetMaxBytes = 512
	// subscriptionResponseMaxBytes 限制成功响应，避免异常订阅耗尽进程内存。
	subscriptionResponseMaxBytes int64 = 64 << 20
	// subscriptionFetchMaxAttempts 单次 fetch 内最大尝试次数（1 次初始 + 最多 1 次重试）。
	subscriptionFetchMaxAttempts = 2
	// subscriptionFetchRetryBackoff 5xx/429 重试前的短退避。
	subscriptionFetchRetryBackoff = 200 * time.Millisecond
)

// 测试可注入：生产默认走 SSRF 校验与安全拨号；测试可放行 httptest loopback。
var (
	subscriptionURLTargetCheck = validateSubscriptionURLTarget
	subscriptionDialContextFn  = safeSubscriptionDialContext
	subscriptionFetchSleepFn   = time.Sleep
)

var unsafeSubscriptionPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:2::/48"),
	netip.MustParsePrefix("2001:db8::/32"),
}

var (
	subscriptionSecretValuePattern = regexp.MustCompile(`(?i)((?:"?(?:token|password|passwd|api[_-]?key|apikey|access[_-]?token|secret|authorization|cookie)"?\s*[:=]\s*["']?))([^"'\s,;&<>]+)`)
	subscriptionBearerPattern      = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=-]+`)
	subscriptionUserInfoPattern    = regexp.MustCompile(`(?i)\b((?:https?|socks4|socks5)://)[^/@\s]+@`)
	subscriptionProxyLinkPattern   = regexp.MustCompile(`(?i)\b(?:ss|ssr|vmess|vless|trojan|hysteria|hysteria2|tuic)://[^\s"'<>]+`)
)

type proxyValidator interface {
	ValidateOne(storage.Proxy) (bool, time.Duration, string, string, validator.RiskInfo)
	ValidateStream([]storage.Proxy) <-chan validator.Result
}

// Manager 订阅管理器
type Manager struct {
	storage   *storage.Storage
	validator proxyValidator
	singbox   singBoxShard
	stopCh    chan struct{}
	stopOnce  sync.Once
	refreshMu sync.Mutex // 防止并发刷新；Stop 也持有，避免停机与刷新/Reload 交错
	// longTermEvictedKeys 记录已剔除的长期禁用 NodeKey。
	// 防止其它订阅刷新 re-fetch 时再次占端口；本订阅下次刷新可重新入池。
	longTermEvictedKeys map[string]bool
}

type subscriptionProxyEntry struct {
	addr  string
	proto string
	dual  bool
	// username/password 是直连 http/socks 节点的上游认证凭据（拨号时注入，绝不入日志）。
	// tunnel 节点走本机 mixed 端口无需认证，留空。
	username string
	password string
	// nodeKey 稳定身份（ParsedNode.NodeKey）；隧道 address 为本地端口时仍靠此键持久化/锁定。
	nodeKey string
}

// normalizeSubscriptionProxyEntries 在任何持久化前归一化同批代理。
// 完全相同的项稳定保留首次出现；共享稳定 key 或 address 但配置不同则显式拒绝。
func normalizeSubscriptionProxyEntries(entries []subscriptionProxyEntry) ([]subscriptionProxyEntry, error) {
	normalized := make([]subscriptionProxyEntry, 0, len(entries))
	byNodeKey := make(map[string]int, len(entries))
	byAddress := make(map[string]int, len(entries))
	for inputIndex, entry := range entries {
		if entry.nodeKey != "" {
			if normalizedIndex, ok := byNodeKey[entry.nodeKey]; ok {
				if normalized[normalizedIndex] != entry {
					return nil, fmt.Errorf("订阅代理批次 node_key 冲突：第 %d 项与第 %d 项配置不一致", normalizedIndex+1, inputIndex+1)
				}
				continue
			}
		}
		if normalizedIndex, ok := byAddress[entry.addr]; ok {
			if normalized[normalizedIndex] != entry {
				return nil, fmt.Errorf("订阅代理批次 address 冲突：第 %d 项与第 %d 项配置不一致", normalizedIndex+1, inputIndex+1)
			}
			continue
		}

		normalizedIndex := len(normalized)
		normalized = append(normalized, entry)
		byAddress[entry.addr] = normalizedIndex
		if entry.nodeKey != "" {
			byNodeKey[entry.nodeKey] = normalizedIndex
		}
	}
	return normalized, nil
}

type disabledProbeTarget struct {
	proxy         storage.Proxy
	tunnelNodeKey string
}

// NewManager 创建订阅管理器
func NewManager(store *storage.Storage, v *validator.Validator, cfg *config.Config) *Manager {
	dataDir, err := config.DataDir()
	if err != nil {
		panic(fmt.Sprintf("解析运行时数据目录失败: %v", err))
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

// Stop 停止管理器。
// 先关闭 stopCh 通知后台循环退出，再持 refreshMu 等待在途 Refresh/AddManual 结束，
// 最后停止 sing-box，避免 Stop 与 Reload 并发造成端口复用竞态或停机后复活进程。
// stopOnce 保证重复 Stop 不会 close 已关闭的 channel。
func (m *Manager) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
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
			if attemptErr := m.storage.UpdateSubscriptionAttempt(sub.ID); attemptErr != nil {
				log.Printf("[custom] ⚠️ 记录订阅 [%s] 刷新尝试失败: %v", sub.Name, attemptErr)
			}
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

// probeDisabled 探测被禁用的订阅代理。
// 先剔除仍占端口的长期禁用节点；仅探测当前 portMap 仍有本地 tunnel 地址的禁用节点。
// 已剔除的长期禁用等待下次订阅刷新重新入池，不在此路径 dial。
func (m *Manager) probeDisabled() {
	// 与 Refresh/Stop 串行，避免 prune Reload 与订阅刷新交错。
	m.refreshMu.Lock()
	disabled, err := m.storage.GetDisabledCustomProxies()
	if err != nil || len(disabled) == 0 {
		m.refreshMu.Unlock()
		return
	}
	if pruned, err := m.pruneLongTermDisabledFromRuntime(disabled); err != nil {
		log.Printf("[custom] ⚠️ 剔除长期禁用节点失败: %v", err)
	} else if pruned > 0 {
		log.Printf("[custom] 🧹 已从运行态剔除 %d 个长期禁用隧道节点（>%s）", pruned, longTermDisabledRetention)
	}
	nodes := m.singbox.GetNodes()
	portMap := m.singbox.GetPortMap()

	// 仅探测仍持有本地 mixed 端口的禁用节点；无端口者视为长期剔除，等订阅刷新。
	toProbe := make([]disabledProbeTarget, 0, len(disabled))
	for _, proxy := range disabled {
		if isLongTermDisabledProxy(proxy, time.Now()) {
			continue
		}
		target := disabledProbeTarget{proxy: proxy}
		if isLocalTunnelAddress(proxy.Address) {
			key := nodeKeyForLocalAddress(nodes, portMap, proxy.Address)
			if key == "" {
				continue
			}
			target.tunnelNodeKey = key
		}
		toProbe = append(toProbe, target)
	}
	m.refreshMu.Unlock()
	if len(toProbe) == 0 {
		return
	}

	log.Printf("[custom] 🔍 探测 %d 个禁用的订阅代理（已跳过无运行态端口/长期禁用）", len(toProbe))

	cfg := config.Get()
	recovered := 0
	recoveredSubs := make(map[int64]bool)
	for _, target := range toProbe {
		proxy := target.proxy
		valid, latency, exitIP, exitLocation, risk := m.validator.ValidateOne(proxy)
		if valid {
			m.refreshMu.Lock()
			if !m.probeTargetStillCurrentLocked(target) {
				m.refreshMu.Unlock()
				continue
			}
			// 检查地理过滤：恢复前确认不在屏蔽列表中
			if exitLocation != "" && isGeoBlocked(exitLocation, cfg) {
				log.Printf("[custom] 代理 %s 验证通过但被地理过滤 (%s)，保持禁用", proxy.Address, exitLocation)
				if err := m.storage.UpdateDisabledSubscriptionProxyExitInfo(proxy.Address, proxy.SubscriptionID, exitIP, exitLocation, int(latency.Milliseconds()), risk.IPAPIIsScore, risk.Flags, risk.FlagsKnown, risk.CFBlocked, risk.AIReachability); err != nil {
					log.Printf("[custom] 写回地理过滤出口信息失败 %s: %v", proxy.Address, err)
				}
				m.refreshMu.Unlock()
				continue
			}
			if err := m.storage.RecoverSubscriptionProxyWithExitInfo(proxy.Address, proxy.SubscriptionID, exitIP, exitLocation, int(latency.Milliseconds()), risk.IPAPIIsScore, risk.Flags, risk.FlagsKnown, risk.CFBlocked, risk.AIReachability); err != nil {
				m.refreshMu.Unlock()
				log.Printf("[custom] 原子写回探测恢复结果失败 %s: %v", proxy.Address, err)
				continue
			}
			m.refreshMu.Unlock()
			recovered++
			recoveredSubs[proxy.SubscriptionID] = true
			log.Printf("[custom] ✅ 代理 %s 恢复可用 (%dms)", proxy.Address, latency.Milliseconds())
			continue
		}

		// invalid 结果必须在确认节点身份/端口仍有效后初始化失败时钟；
		// 已有 disabled 时钟由存储层保留，避免周期探测无限续期。
		m.refreshMu.Lock()
		if !m.probeTargetStillCurrentLocked(target) {
			m.refreshMu.Unlock()
			continue
		}
		if err := m.storage.DisableSubscriptionProxy(proxy.Address, proxy.SubscriptionID); err != nil {
			log.Printf("[custom] 记录禁用代理探测失败时间失败 %s: %v", proxy.Address, err)
		}
		m.refreshMu.Unlock()
	}
	// 有恢复的代理则更新对应订阅的 last_success
	for subID := range recoveredSubs {
		if subID > 0 {
			if err := m.storage.UpdateSubscriptionSuccess(subID); err != nil {
				log.Printf("[custom] 更新订阅 last_success 失败 sub=%d: %v", subID, err)
			}
		}
	}

	if recovered > 0 {
		log.Printf("[custom] 探测完成：%d/%d 恢复可用", recovered, len(toProbe))
	}
}

// probeTargetStillCurrentLocked 确认探测结果仍对应当前禁用记录、路由配置及隧道映射。
// 调用方必须持有 refreshMu，避免 RefreshSubscription 在确认和写回之间改写同一节点。
func (m *Manager) probeTargetStillCurrentLocked(target disabledProbeTarget) bool {
	current, err := m.storage.GetProxyByIdentity(target.proxy.Address, storage.SourceSubscription, target.proxy.SubscriptionID)
	if err != nil ||
		current.ID != target.proxy.ID ||
		current.Address != target.proxy.Address ||
		current.Status != "disabled" ||
		current.NodeKey != target.proxy.NodeKey ||
		current.Protocol != target.proxy.Protocol ||
		current.DualProtocol != target.proxy.DualProtocol ||
		current.Username != target.proxy.Username ||
		current.Password != target.proxy.Password {
		return false
	}
	if target.tunnelNodeKey == "" {
		return true
	}
	return localAddrMatches(m.singbox.GetPortMap(), target.tunnelNodeKey, target.proxy.Address)
}

// RefreshSubscription 刷新单个订阅。
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
		return fmt.Errorf("订阅 [%s] 无有效节点", sub.Name)
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
	var directEntries []subscriptionProxyEntry
	for _, node := range directNodes {
		addr := node.DirectAddress()
		proto := node.DirectProtocol()
		user, pass := node.DirectCredentials()
		directEntries = append(directEntries, subscriptionProxyEntry{addr: addr, proto: proto, username: user, password: pass, nodeKey: node.NodeKey()})
	}
	directEntries, err = normalizeSubscriptionProxyEntries(directEntries)
	if err != nil {
		return fmt.Errorf("归一化订阅代理失败: %w", err)
	}
	var proxyEntries []subscriptionProxyEntry

	oldNodes := append([]ParsedNode(nil), m.singbox.GetNodes()...)
	oldSubKeys, err := m.subscriptionTunnelRuntimeKeys(subID, oldNodes, m.singbox.GetPortMap())
	if err != nil {
		return fmt.Errorf("读取订阅旧运行态失败: %w", err)
	}
	runtimeChanged := false

	// 先重载 tunnel；失败时不删除该订阅旧代理，避免一次坏配置破坏旧可用配置。
	// 当前订阅旧 tunnel 必须从目标运行态移除，否则刷新为 direct/no-tunnel 会留下幽灵节点。
	if len(tunnelNodes) > 0 || len(oldSubKeys) > 0 {
		allTunnelNodes, err := m.collectAllTunnelNodesExcludingSubscription(subID, oldSubKeys)
		if err != nil {
			return fmt.Errorf("收集 tunnel 节点失败: %w", err)
		}
		// 将当前订阅本次解析出的 tunnel 节点加入，按 NodeKey 去重。
		nodeMap := make(map[string]ParsedNode)
		for _, n := range allTunnelNodes {
			nodeMap[n.NodeKey()] = n
		}
		for _, n := range tunnelNodes {
			// 本订阅刷新显式重新入池：清除长期剔除标记。
			m.clearLongTermEvictedKey(n.NodeKey())
			nodeMap[n.NodeKey()] = n
		}
		var mergedNodes []ParsedNode
		for _, n := range nodeMap {
			mergedNodes = append(mergedNodes, n)
		}

		if err := m.singbox.Reload(mergedNodes); err != nil {
			return fmt.Errorf("sing-box 重载失败: %w", err)
		}
		runtimeChanged = true
		portMap := m.singbox.GetPortMap()
		acceptedTunnelNodes, err := acceptedNodesForCommit(m.singbox, tunnelNodes, portMap)
		if err != nil {
			if rbErr := m.singbox.Reload(oldNodes); rbErr != nil {
				return fmt.Errorf("sing-box 重载失败: %w; 回滚运行态失败: %v", err, rbErr)
			}
			return fmt.Errorf("sing-box 重载失败: %w", err)
		}
		for _, node := range acceptedTunnelNodes {
			key := node.NodeKey()
			if port, ok := portMap[key]; ok {
				addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
				proxyEntries = append(proxyEntries, subscriptionProxyEntry{addr: addr, proto: "socks5", dual: true, nodeKey: key})
			}
		}
	}

	// 处理可直接使用的 HTTP/SOCKS5 节点。新节点先保持 disabled，验证通过后才进入 active 选路。
	proxyEntries = append(proxyEntries, directEntries...)
	if len(directEntries) > 0 {
		log.Printf("[custom] 📥 %d 个 HTTP/SOCKS5 节点直接入池", len(directEntries))
	}

	allProxies, err = m.replaceSubscriptionProxies(subID, proxyEntries)
	if err != nil {
		if runtimeChanged {
			if rbErr := m.singbox.Reload(oldNodes); rbErr != nil {
				return fmt.Errorf("替换订阅代理失败: %w; 回滚运行态失败: %v", err, rbErr)
			}
		}
		return fmt.Errorf("替换订阅代理失败: %w", err)
	}
	if len(tunnelNodes) > 0 {
		log.Printf("[custom] 📥 %d 个加密节点入站（单 mixed 端口，SOCKS5/HTTP 共用）通过 sing-box 转换入池", len(proxyEntries)-len(directEntries))
	}

	// 验证新入池的代理
	m.validateCustomProxies(allProxies, subID)

	log.Printf("[custom] ✅ 订阅 [%s] 刷新完成，解析 %d 节点，入池 %d 个", sub.Name, len(nodes), len(allProxies))

	return nil
}

// acceptedNodesForCommit 仅对当前订阅 target 判定可提交节点：
// 明确拒绝的坏节点可跳过；段满/未知缺失仍严格失败并触发回滚。
// 返回值是 target 的子集，避免把其它订阅的运行态节点误入库。
func acceptedNodesForCommit(shard singBoxShard, target []ParsedNode, portMap map[string]int) ([]ParsedNode, error) {
	if len(target) == 0 {
		return nil, nil
	}
	diagnostics := assemblyDiagnostics{}
	if aware, ok := shard.(assemblyAwareShard); ok {
		diagnostics = aware.GetAssemblyDiagnostics()
	}
	rejected := diagnostics.rejectedKeys()

	// 无 assembler 诊断时：先用 buildOutbound 区分明确构建失败与其它缺失。
	if len(diagnostics.rejected) == 0 && len(diagnostics.accepted) == 0 && len(diagnostics.segmentFull) == 0 {
		for _, node := range target {
			if _, ok := portMap[node.NodeKey()]; ok {
				continue
			}
			if _, err := buildOutbound(node, "diagnostic"); err != nil {
				diagnostics.rejected = append(diagnostics.rejected, assemblyRejectedNode{node: node, err: err})
				rejected[node.NodeKey()] = true
			}
		}
	}

	accepted := make([]ParsedNode, 0, len(target))
	missing := make([]ParsedNode, 0)
	for _, node := range target {
		key := node.NodeKey()
		if rejected[key] {
			continue
		}
		if _, ok := portMap[key]; ok {
			accepted = append(accepted, node)
			continue
		}
		missing = append(missing, node)
	}

	if len(missing) > 0 {
		return nil, incompletePortAllocationErrorWithDiagnostics(missing, portMap, diagnostics)
	}
	if len(accepted) == 0 {
		return nil, fmt.Errorf("所有隧道节点均被拒绝: %w", incompletePortAllocationErrorWithDiagnostics(target, portMap, diagnostics))
	}
	return accepted, nil
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
	return m.collectAllTunnelNodesExcludingSubscription(0, nil)
}

// collectAllTunnelNodesExcludingSubscription 只合并当前 sing-box 运行态快照。
// 不旁路拉取其它订阅：其它订阅的解析/入库必须走它们自己的完整 Refresh 事务（BUG-05）。
// excludedSubID 保留以兼容调用方签名；当前实现仅按 excludedRuntimeKeys 剔除。
func (m *Manager) collectAllTunnelNodesExcludingSubscription(excludedSubID int64, excludedRuntimeKeys map[string]bool) ([]ParsedNode, error) {
	_ = excludedSubID
	// 收集时排除长期禁用节点，避免继续占用 mixed 端口。
	longTermKeys := m.longTermDisabledRuntimeKeys()
	nodeMap := make(map[string]ParsedNode)
	for _, node := range m.singbox.GetNodes() {
		key := node.NodeKey()
		if excludedRuntimeKeys[key] {
			continue
		}
		if longTermKeys[key] {
			continue
		}
		nodeMap[key] = node
	}
	return mapValues(nodeMap), nil
}

func (m *Manager) subscriptionTunnelRuntimeKeys(subID int64, nodes []ParsedNode, portMap map[string]int) (map[string]bool, error) {
	rows, err := m.storage.GetDB().Query(
		`SELECT address, node_key FROM proxies WHERE subscription_id = ? AND source = ?`, subID, storage.SourceSubscription,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runtimeKeys := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		runtimeKeys[node.NodeKey()] = true
	}
	keys := make(map[string]bool)
	keyAddresses := make(map[string]string)
	for rows.Next() {
		var addr, nodeKey string
		if err := rows.Scan(&addr, &nodeKey); err != nil {
			return nil, err
		}
		key := strings.TrimSpace(nodeKey)
		if key != "" {
			if !runtimeKeys[key] {
				continue
			}
		} else {
			// 旧数据没有 node_key 时仅按当前运行态地址兼容映射。
			key = nodeKeyForLocalAddress(nodes, portMap, addr)
		}
		if key != "" {
			keys[key] = true
			keyAddresses[key] = addr
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for key := range keys {
		otherOwner, err := m.storage.HasOtherSubscriptionProxyOwner(key, keyAddresses[key], subID)
		if err != nil {
			return nil, fmt.Errorf("检查订阅 NodeKey owner 失败: %w", err)
		}
		if otherOwner {
			delete(keys, key)
		}
	}
	return keys, nil
}

func nodesExcludingKeys(nodes []ParsedNode, keys map[string]bool) []ParsedNode {
	if len(keys) == 0 {
		return append([]ParsedNode(nil), nodes...)
	}
	kept := make([]ParsedNode, 0, len(nodes))
	for _, node := range nodes {
		if keys[node.NodeKey()] {
			continue
		}
		kept = append(kept, node)
	}
	return kept
}

func (m *Manager) replaceSubscriptionProxies(subID int64, entries []subscriptionProxyEntry) ([]storage.Proxy, error) {
	entries, err := normalizeSubscriptionProxyEntries(entries)
	if err != nil {
		return nil, err
	}

	tx, err := m.storage.GetDB().Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var subscriptionStatus string
	if err := tx.QueryRow(`SELECT status FROM subscriptions WHERE id = ?`, subID).Scan(&subscriptionStatus); err != nil {
		return nil, fmt.Errorf("读取订阅状态失败: %w", err)
	}

	// 刷新前快照 user_paused：优先按 node_key 回写（端口可变时 address 会变），兼容旧数据回退 address。
	pausedByKey := map[string]bool{}
	pausedByAddr := map[string]bool{}
	pauseRows, err := tx.Query(
		`SELECT address, node_key, user_paused FROM proxies WHERE subscription_id = ? AND source = ?`,
		subID, storage.SourceSubscription,
	)
	if err != nil {
		return nil, fmt.Errorf("读取订阅节点暂停状态失败: %w", err)
	}
	for pauseRows.Next() {
		var addr, nkey string
		var paused int
		if err := pauseRows.Scan(&addr, &nkey, &paused); err != nil {
			pauseRows.Close()
			return nil, fmt.Errorf("扫描订阅节点暂停状态失败: %w", err)
		}
		if paused == 1 {
			if nkey != "" {
				pausedByKey[nkey] = true
			}
			pausedByAddr[addr] = true
		}
	}
	if err := pauseRows.Err(); err != nil {
		pauseRows.Close()
		return nil, fmt.Errorf("遍历订阅节点暂停状态失败: %w", err)
	}
	pauseRows.Close()

	// 先删本订阅中「新批次未再出现」的节点；有 node_key 的行用 UPDATE 改 address 保 id。
	incomingKeys := map[string]bool{}
	incomingAddrs := map[string]bool{}
	for _, entry := range entries {
		if entry.nodeKey != "" {
			incomingKeys[entry.nodeKey] = true
		}
		incomingAddrs[entry.addr] = true
	}
	oldRows, err := tx.Query(
		`SELECT id, address, node_key, protocol, status, user_paused, fail_count,
		        CASE WHEN last_check IS NULL THEN 0 ELSE 1 END,
		        exit_ip, exit_location, latency, dual_protocol, proxy_username, proxy_password
		   FROM proxies WHERE subscription_id = ? AND source = ?`,
		subID, storage.SourceSubscription,
	)
	if err != nil {
		return nil, fmt.Errorf("读取订阅旧代理失败: %w", err)
	}
	type oldProxyRow struct {
		id           int64
		addr         string
		key          string
		proto        string
		status       string
		userPaused   int
		failCount    int
		hasLastCheck int
		exitIP       string
		exitLocation string
		latency      int
		dual         bool
		username     string
		password     string
	}
	var staleIDs []int64
	keyToOld := map[string]oldProxyRow{}
	for oldRows.Next() {
		var r oldProxyRow
		if err := oldRows.Scan(
			&r.id, &r.addr, &r.key, &r.proto, &r.status, &r.userPaused,
			&r.failCount, &r.hasLastCheck, &r.exitIP, &r.exitLocation, &r.latency,
			&r.dual, &r.username, &r.password,
		); err != nil {
			oldRows.Close()
			return nil, fmt.Errorf("扫描订阅旧代理失败: %w", err)
		}
		if r.key != "" {
			if incomingKeys[r.key] {
				keyToOld[r.key] = r
				continue
			}
			staleIDs = append(staleIDs, r.id)
			continue
		}
		// 无 node_key 的旧行一律删除后重插：无法按稳定身份 upsert，避免端口碰巧相同时
		// 跳过 DELETE 导致刷新半迁移、以及回滚测试/运维触发器路径失效。
		staleIDs = append(staleIDs, r.id)
	}
	if err := oldRows.Err(); err != nil {
		oldRows.Close()
		return nil, fmt.Errorf("遍历订阅旧代理失败: %w", err)
	}
	oldRows.Close()
	for _, id := range staleIDs {
		if _, err := tx.Exec(`DELETE FROM proxies WHERE id = ?`, id); err != nil {
			return nil, fmt.Errorf("删除过期订阅代理 id=%d 失败: %w", id, err)
		}
	}
	if len(staleIDs) > 0 {
		log.Printf("[custom] 🧹 清理订阅过期代理 %d 个", len(staleIDs))
	}

	// 先把「有 key 且命中旧行」的节点全部改到临时 address，再写最终 port，
	// 避免同批端口对调时逐条 final 撞 UNIQUE(address,source,subscription_id)。
	type keyedUpdate struct {
		index          int
		id             int64
		entry          subscriptionProxyEntry
		userPaused     int
		preserveActive bool
		resetEvidence  bool
		startClock     bool
	}
	type pendingInsert struct {
		index      int
		entry      subscriptionProxyEntry
		userPaused int
	}
	var keyed []keyedUpdate
	var inserts []pendingInsert
	for entryIndex, entry := range entries {
		userPaused := 0
		// 有稳定身份时只按 node_key 回写暂停，禁止 address 回退。
		if entry.nodeKey != "" {
			if pausedByKey[entry.nodeKey] {
				userPaused = 1
			}
		} else if pausedByAddr[entry.addr] {
			userPaused = 1
		}
		if entry.nodeKey != "" {
			if old, ok := keyToOld[entry.nodeKey]; ok {
				resetEvidence := old.addr != entry.addr || old.proto != entry.proto ||
					old.dual != entry.dual || old.username != entry.username ||
					old.password != entry.password
				// 仅保留具有明确最近成功记录、仍可选且路由配置逐字段未变的旧节点。
				// last_check 存在且 fail_count=0 表示最新持久化检查没有未清零失败；
				// 任一身份字段变化都必须先禁用，等待本轮验证重新建立信任。
				preserveActive := subscriptionStatus == "active" &&
					old.status == "active" && old.userPaused == 0 &&
					old.failCount == 0 && old.hasLastCheck == 1 &&
					old.exitIP != "" && old.exitLocation != "" && old.latency > 0 &&
					!resetEvidence && old.key == entry.nodeKey
				startClock := !resetEvidence && !preserveActive &&
					(old.status == "active" || old.status == "degraded")
				keyed = append(keyed, keyedUpdate{
					index: entryIndex, id: old.id, entry: entry,
					userPaused: userPaused, preserveActive: preserveActive,
					resetEvidence: resetEvidence, startClock: startClock,
				})
				continue
			}
		}
		inserts = append(inserts, pendingInsert{index: entryIndex, entry: entry, userPaused: userPaused})
	}

	for _, ku := range keyed {
		tmpAddr := fmt.Sprintf("rebind-%d.invalid:0", ku.id)
		status := "disabled"
		if ku.preserveActive {
			status = "active"
		}
		resetSQL := ""
		if ku.resetEvidence {
			resetSQL = `,
				fail_count = 0, last_check = NULL, exit_ip = '', exit_location = '',
				latency = 0, quality_grade = 'C',
				region = CASE WHEN region_source != 'manual' THEN '' ELSE region END,
				ipapiis_score = -1, ipapi_flags = '', ipapi_flags_seen = 0,
				cf_blocked = -1, ai_reachability = ''`
		} else if ku.startClock {
			resetSQL = `, last_check = CURRENT_TIMESTAMP`
		}
		query := `UPDATE proxies SET address = ?, protocol = ?, status = ?, dual_protocol = ?,
				user_paused = ?, proxy_username = ?, proxy_password = ?, node_key = ?,
				region_source = CASE WHEN region_source = '' THEN 'auto' ELSE region_source END` +
			resetSQL + ` WHERE id = ?`
		if _, err := tx.Exec(
			query,
			tmpAddr, ku.entry.proto, status, ku.entry.dual, ku.userPaused,
			ku.entry.username, ku.entry.password, ku.entry.nodeKey, ku.id,
		); err != nil {
			return nil, fmt.Errorf("预更新订阅代理 node_key=%s 失败: %w", ku.entry.nodeKey, err)
		}
	}
	proxies := make([]storage.Proxy, len(entries))
	for _, ku := range keyed {
		if _, err := tx.Exec(`UPDATE proxies SET address = ? WHERE id = ?`, ku.entry.addr, ku.id); err != nil {
			return nil, fmt.Errorf("更新订阅代理 node_key=%s 失败: %w", ku.entry.nodeKey, err)
		}
		status := "disabled"
		if ku.preserveActive {
			status = "active"
		}
		proxies[ku.index] = storage.Proxy{
			ID: ku.id, Address: ku.entry.addr, Protocol: ku.entry.proto, Status: status,
			Source: storage.SourceSubscription, SubscriptionID: subID,
			DualProtocol: ku.entry.dual, UserPaused: ku.userPaused == 1,
			Username: ku.entry.username, Password: ku.entry.password, NodeKey: ku.entry.nodeKey,
		}
	}
	for _, insert := range inserts {
		entry := insert.entry
		_, err := tx.Exec(
			`INSERT INTO proxies (address, protocol, source, subscription_id, region_source, status, dual_protocol, user_paused, proxy_username, proxy_password, node_key)
			 VALUES (?, ?, ?, ?, 'auto', 'disabled', ?, ?, ?, ?, ?)
			 ON CONFLICT(address, source, subscription_id) DO UPDATE SET
				protocol = excluded.protocol,
				region_source = CASE WHEN proxies.region_source = '' THEN excluded.region_source ELSE proxies.region_source END,
				status = 'disabled',
				dual_protocol = excluded.dual_protocol,
				user_paused = excluded.user_paused,
				proxy_username = excluded.proxy_username,
				proxy_password = excluded.proxy_password,
				node_key = CASE WHEN excluded.node_key != '' THEN excluded.node_key ELSE proxies.node_key END`,
			entry.addr, entry.proto, storage.SourceSubscription, subID, entry.dual, insert.userPaused, entry.username, entry.password, entry.nodeKey,
		)
		if err != nil {
			return nil, fmt.Errorf("新增订阅代理 %s 失败: %w", entry.addr, err)
		}
		// Exec 结果不能完整表达最终实体身份；按完整唯一身份读取 ID。
		var proxyID int64
		if err := tx.QueryRow(
			`SELECT id FROM proxies WHERE address = ? AND source = ? AND subscription_id = ?`,
			entry.addr, storage.SourceSubscription, subID,
		).Scan(&proxyID); err != nil {
			return nil, fmt.Errorf("读取订阅代理 %s 身份失败: %w", entry.addr, err)
		}
		proxies[insert.index] = storage.Proxy{
			ID: proxyID, Address: entry.addr, Protocol: entry.proto, Status: "disabled",
			Source: storage.SourceSubscription, SubscriptionID: subID,
			DualProtocol: entry.dual, UserPaused: insert.userPaused == 1,
			Username: entry.username, Password: entry.password, NodeKey: entry.nodeKey,
		}
	}

	subRes, err := tx.Exec(`UPDATE subscriptions SET last_fetch = CURRENT_TIMESTAMP, proxy_count = ? WHERE id = ?`, len(entries), subID)
	if err != nil {
		return nil, fmt.Errorf("更新订阅拉取状态失败: %w", err)
	}
	updated, err := subRes.RowsAffected()
	if err != nil {
		return nil, err
	}
	if updated == 0 {
		return nil, fmt.Errorf("subscription %d not found", subID)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return proxies, nil
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
			return nil, false
		}
		proxy.DualProtocol = true
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
//
// 非 200 错误附带截断、脱敏后的响应体片段；对 5xx/429 做有上限的即时重试。
// 调度层仍由 RefreshMin 退避，且禁止经上游节点回源。
func (m *Manager) fetchSubscriptionURL(urlStr, headersJSON string) ([]byte, error) {
	if err := subscriptionURLTargetCheck(urlStr); err != nil {
		return nil, err
	}
	transport := &http.Transport{DialContext: subscriptionDialContextFn}
	client := &http.Client{
		Timeout:       30 * time.Second,
		Transport:     transport,
		CheckRedirect: subscriptionCheckRedirect,
	}

	var lastErr error
	for attempt := 1; attempt <= subscriptionFetchMaxAttempts; attempt++ {
		req, err := buildSubscriptionRequest(urlStr, headersJSON)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			// 连接/超时等：中文包装，不重试（避免拉长无意义等待；调度层再退避）。
			return nil, fmt.Errorf("直接拉取订阅 URL 失败: %w", err)
		}

		if resp.StatusCode == http.StatusOK {
			data, readErr := readSubscriptionResponse(resp.Body, subscriptionResponseMaxBytes)
			resp.Body.Close()
			if readErr != nil {
				return nil, fmt.Errorf("直接拉取订阅 URL 读取响应失败: %w", readErr)
			}
			return data, nil
		}

		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, int64(subscriptionResponseSnippetMaxBytes)+1))
		resp.Body.Close()
		lastErr = formatSubscriptionHTTPStatusError(resp.StatusCode, snippet)

		if !isSubscriptionFetchRetryableStatus(resp.StatusCode) || attempt >= subscriptionFetchMaxAttempts {
			return nil, lastErr
		}
		subscriptionFetchSleepFn(subscriptionFetchRetryBackoff)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("直接拉取订阅 URL 失败: 未知错误")
}

func readSubscriptionResponse(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("订阅响应超过 %d 字节上限", limit)
	}
	return data, nil
}

// isSubscriptionFetchRetryableStatus 仅 5xx 与 429 允许单次 fetch 内即时重试。
func isSubscriptionFetchRetryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

// formatSubscriptionHTTPStatusError 构造含状态码与脱敏响应片段的诊断错误。
func formatSubscriptionHTTPStatusError(statusCode int, body []byte) error {
	snippet := sanitizeSubscriptionResponseSnippet(body)
	if snippet == "" {
		return fmt.Errorf("直接拉取订阅 URL 返回 HTTP %d", statusCode)
	}
	truncateNote := ""
	if len(body) > subscriptionResponseSnippetMaxBytes {
		truncateNote = "（已截断）"
	}
	return fmt.Errorf("直接拉取订阅 URL 返回 HTTP %d，响应片段%s: %s", statusCode, truncateNote, snippet)
}

// sanitizeSubscriptionResponseSnippet 截断响应体并脱敏常见密钥形态，避免错误日志泄漏凭据。
// 不回显原始 Authorization 请求头；仅处理响应体中的明文片段。
func sanitizeSubscriptionResponseSnippet(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	if len(body) > subscriptionResponseSnippetMaxBytes {
		body = body[:subscriptionResponseSnippetMaxBytes]
	}
	// 统一空白，去掉控制字符，便于日志阅读。
	s := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, string(body))
	s = strings.Join(strings.Fields(s), " ")
	if isEncodedSubscriptionPayload(s) {
		return "[REDACTED]"
	}
	s = subscriptionBearerPattern.ReplaceAllString(s, "${1}[REDACTED]")
	s = subscriptionSecretValuePattern.ReplaceAllString(s, "${1}[REDACTED]")
	s = subscriptionUserInfoPattern.ReplaceAllString(s, "${1}[REDACTED]@")
	s = subscriptionProxyLinkPattern.ReplaceAllString(s, "[REDACTED]")
	return s
}

func isEncodedSubscriptionPayload(s string) bool {
	if len(s) < 16 || strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		decoded, err := encoding.DecodeString(s)
		if err != nil {
			continue
		}
		lower := strings.ToLower(string(decoded))
		for _, scheme := range []string{"ss://", "ssr://", "vmess://", "vless://", "trojan://", "hysteria://", "hysteria2://", "tuic://"} {
			if strings.Contains(lower, scheme) {
				return true
			}
		}
	}
	return false
}

// parseSubscriptionHeaders 解析订阅自定义 headers JSON。
// 空字符串 → 空 map（默认 UA）；非空必须是 JSON object 且值为 string。
// null / 数组 / 非 string 值 / 非法 JSON 一律报错，禁止静默回退。
func parseSubscriptionHeaders(headersJSON string) (map[string]string, error) {
	if headersJSON == "" {
		return map[string]string{}, nil
	}
	var custom map[string]string
	if err := json.Unmarshal([]byte(headersJSON), &custom); err != nil {
		return nil, fmt.Errorf("invalid subscription headers: %w", err)
	}
	// json.Unmarshal("null", &map) 成功但结果为 nil，必须拒绝。
	if custom == nil {
		return nil, fmt.Errorf("invalid subscription headers: must be a JSON object")
	}
	return custom, nil
}

// ValidateSubscriptionHeaders 校验订阅自定义 headers JSON：
// 空字符串合法（使用默认 UA）；非空时必须是 JSON object 且值为 string。
// 非法 JSON 显式报错，禁止静默回退默认 UA。
func ValidateSubscriptionHeaders(headersJSON string) error {
	_, err := parseSubscriptionHeaders(headersJSON)
	return err
}

func buildSubscriptionRequest(urlStr, headersJSON string) (*http.Request, error) {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	// 默认用 v2rayN UA，大部分机场都会返回完整的节点信息。
	req.Header.Set("User-Agent", "v2rayN")
	// 应用订阅自定义请求头：非空时必须是合法 JSON object；解析失败显式返回错误，
	// 不静默回退默认 UA（避免添加/校验把坏配置当成功）。
	custom, err := parseSubscriptionHeaders(headersJSON)
	if err != nil {
		return nil, err
	}
	for k, v := range custom {
		if k != "" {
			req.Header.Set(k, v)
		}
	}
	return req, nil
}

// subscriptionCheckRedirect 限制订阅拉取重定向，并避免跨 origin 转发非标准自定义密钥头。
// Go 默认只剥离 Authorization/Cookie/Proxy-Authorization；X-Token 等会被跨域复制（RISK-01）。
func subscriptionCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return fmt.Errorf("stopped after 5 redirects")
	}
	if err := subscriptionURLTargetCheck(req.URL.String()); err != nil {
		return err
	}
	if len(via) == 0 {
		return nil
	}
	prev := via[len(via)-1]
	if sameSubscriptionOrigin(prev.URL, req.URL) {
		return nil
	}
	// 跨 origin：只保留标准库也会在敏感场景处理的最小安全头集合之外的内容——
	// 这里主动剥离自定义头，仅保留 User-Agent（以及标准库已剥离的敏感标准头语义）。
	ua := req.Header.Get("User-Agent")
	req.Header = make(http.Header)
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	return nil
}

func sameSubscriptionOrigin(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

func validateSubscriptionURLTarget(urlStr string) error {
	u, err := url.Parse(urlStr)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsafe subscription URL: unsupported scheme %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("unsafe subscription URL: missing host")
	}
	return validateSubscriptionHost(u.Hostname())
}

func safeSubscriptionDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("unsafe subscription URL: invalid dial address %q", address)
	}
	if err := validateSubscriptionHost(host); err != nil {
		return nil, err
	}
	ips, err := lookupSubscriptionHost(ctx, host)
	if err != nil {
		return nil, err
	}
	var firstErr error
	dialer := &net.Dialer{}
	for _, ip := range ips {
		if isUnsafeSubscriptionIP(ip) {
			return nil, fmt.Errorf("unsafe subscription URL: resolved private target %s", ip.String())
		}
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, fmt.Errorf("unsafe subscription URL: no address for host %q", host)
}

func validateSubscriptionHost(host string) error {
	if ip := net.ParseIP(host); ip != nil {
		if isUnsafeSubscriptionIP(ip) {
			return fmt.Errorf("unsafe subscription URL: private target %s", ip.String())
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := lookupSubscriptionHost(ctx, host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if isUnsafeSubscriptionIP(ip) {
			return fmt.Errorf("unsafe subscription URL: resolved private target %s", ip.String())
		}
	}
	return nil
}

func lookupSubscriptionHost(ctx context.Context, host string) ([]net.IP, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		ips = append(ips, addr.IP)
	}
	return ips, nil
}

func isUnsafeSubscriptionIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLinkLocalUnicast() {
		return true
	}
	for _, prefix := range unsafeSubscriptionPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
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
			// 旧节点在暂存阶段可能仍为 active。先撤销旧信任，再做出口
			// 写回与地理/父订阅判断，确保任何后续失败都不会留下 active。
			if result.Proxy.Status == "active" {
				if err := m.storage.DisableSubscriptionProxy(result.Proxy.Address, subID); err != nil {
					log.Printf("[custom] 暂存节点预禁用失败 %s: %v", result.Proxy.Address, err)
					invalid++
					continue
				}
			}
			// 地理过滤节点保持 disabled；写回出口元数据时不得刷新既有禁用时钟。
			if result.ExitLocation != "" && isGeoBlocked(result.ExitLocation, cfg) {
				if err := m.storage.UpdateDisabledSubscriptionProxyExitInfo(result.Proxy.Address, subID, result.ExitIP, result.ExitLocation, latencyMs, result.Risk.IPAPIIsScore, result.Risk.Flags, result.Risk.FlagsKnown, result.Risk.CFBlocked, result.Risk.AIReachability); err != nil {
					log.Printf("[custom] 写回地理过滤出口信息失败 %s: %v", result.Proxy.Address, err)
				}
				invalid++
				continue
			}
			if err := m.storage.UpdateSubscriptionProxyExitInfo(result.Proxy.Address, subID, result.ExitIP, result.ExitLocation, latencyMs, result.Risk.IPAPIIsScore, result.Risk.Flags, result.Risk.FlagsKnown, result.Risk.CFBlocked, result.Risk.AIReachability); err != nil {
				log.Printf("[custom] 写回验证出口信息失败 %s: %v", result.Proxy.Address, err)
				// 预禁用后写回仍失败时再次禁用，覆盖并发恢复或非标准调用，
				// 不能让本轮未落地的成功结果沿用 active。
				if disableErr := m.storage.DisableSubscriptionProxy(result.Proxy.Address, subID); disableErr != nil {
					log.Printf("[custom] 写回失败后禁用节点失败 %s: %v", result.Proxy.Address, disableErr)
				}
				invalid++
				continue
			}
			parentPaused, err := m.storage.IsSubscriptionPaused(subID)
			if err != nil || parentPaused {
				log.Printf("[custom] 验证通过但父订阅不可用 %s: paused=%v err=%v", result.Proxy.Address, parentPaused, err)
				invalid++
				continue
			}
			current, err := m.storage.GetProxyByIdentity(result.Proxy.Address, storage.SourceSubscription, subID)
			if err != nil {
				log.Printf("[custom] 读取验证通过节点状态失败 %s: %v", result.Proxy.Address, err)
				invalid++
				continue
			}
			if current.Status == "disabled" {
				if err := m.storage.EnableSubscriptionProxy(result.Proxy.Address, subID); err != nil {
					log.Printf("[custom] 启用验证通过节点失败 %s: %v", result.Proxy.Address, err)
					invalid++
					continue
				}
			} else if current.Status != "active" {
				log.Printf("[custom] 验证通过节点状态不可恢复 %s: %s", result.Proxy.Address, current.Status)
				invalid++
				continue
			}
			valid++
			continue
		}
		invalid++
		if err := m.storage.DisableSubscriptionProxy(result.Proxy.Address, subID); err != nil {
			log.Printf("[custom] 禁用验证失败节点失败 %s: %v", result.Proxy.Address, err)
		}
	}

	// 有可用节点则更新 last_success
	if valid > 0 && subID > 0 {
		if err := m.storage.UpdateSubscriptionSuccess(subID); err != nil {
			log.Printf("[custom] 更新订阅 last_success 失败 sub=%d: %v", subID, err)
		}
	}

	log.Printf("[custom] 验证完成：%d 可用，%d 不可用", valid, invalid)
	return valid
}

// GetStatus 获取订阅管理器状态。
// 任一 storage 计数失败时记录错误字段，禁止把失败伪装成 0（BUG-07）。
func (m *Manager) GetStatus() map[string]interface{} {
	subscriptionCount, errSubCount := m.storage.CountBySource(storage.SourceSubscription)
	disabled, errDisabled := m.storage.GetDisabledCustomProxies()
	subs, errSubs := m.storage.GetSubscriptions()
	singboxStatus := m.singbox.GetRuntimeStatus()

	status := map[string]interface{}{
		"singbox_running":     singboxStatus.Running,
		"singbox_status":      singboxStatus.Status,
		"singbox_reason":      singboxStatus.Reason,
		"singbox_nodes":       singboxStatus.Nodes,
		"singbox_ready_ports": singboxStatus.ReadyPorts,
		"singbox_total_ports": singboxStatus.TotalPorts,
	}
	if errSubCount != nil {
		log.Printf("[custom] 状态 CountBySource 查询失败: %v", errSubCount)
		status["subscription_count_error"] = errSubCount.Error()
	} else {
		status["subscription_count"] = subscriptionCount
	}
	if errDisabled != nil {
		log.Printf("[custom] 状态 GetDisabledCustomProxies 查询失败: %v", errDisabled)
		status["disabled_count_error"] = errDisabled.Error()
	} else {
		status["disabled_count"] = len(disabled)
	}
	if errSubs != nil {
		log.Printf("[custom] 状态 GetSubscriptions 查询失败: %v", errSubs)
		status["subscription_total_error"] = errSubs.Error()
	} else {
		status["subscription_total"] = len(subs)
	}
	return status
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

// DeleteManualNode 按 id 删除手工代理。
// 直连节点只删除数据库记录；隧道节点还会退出 sing-box 运行态。
func (m *Manager) DeleteManualNode(id int64) error {
	proxy, err := m.storage.GetProxyByID(id)
	if err != nil {
		return fmt.Errorf("查找节点失败: %w", err)
	}
	if proxy.Source != storage.SourceManual {
		return fmt.Errorf("仅支持删除手工节点")
	}
	return m.deleteProxyWithRuntime(proxy)
}

// DeleteManagedProxy 删除任意来源节点：直连只改 DB，隧道节点同步从 sing-box 运行态移除。
// 通用 /api/proxy/delete 必须走此路径，禁止只删 DB 留下幽灵端口（BUG-06）。
func (m *Manager) DeleteManagedProxy(id int64) error {
	proxy, err := m.storage.GetProxyByID(id)
	if err != nil {
		return fmt.Errorf("查找节点失败: %w", err)
	}
	return m.deleteProxyWithRuntime(proxy)
}

func (m *Manager) deleteProxyWithRuntime(proxy *storage.Proxy) error {
	if !isLocalTunnelAddress(proxy.Address) {
		if err := m.storage.DeleteProxyByID(proxy.ID); err != nil {
			return fmt.Errorf("删除节点失败: %w", err)
		}
		return nil
	}

	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()

	oldNodes := append([]ParsedNode(nil), m.singbox.GetNodes()...)
	removeKey := nodeKeyForLocalAddress(oldNodes, m.singbox.GetPortMap(), proxy.Address)
	if removeKey == "" {
		removeKey = strings.TrimSpace(proxy.NodeKey)
	}
	otherOwner, err := m.storage.HasOtherProxyOwner(removeKey, proxy.Address, proxy.ID)
	if err != nil {
		return fmt.Errorf("检查节点运行态 owner 失败: %w", err)
	}
	runtimeChanged := false
	if !otherOwner {
		newNodes := make([]ParsedNode, 0, len(oldNodes))
		for _, n := range oldNodes {
			if removeKey != "" && n.NodeKey() == removeKey {
				continue
			}
			// 稳定 key 解析失败时，兼容按本地端口移除旧运行态。
			if removeKey == "" && localAddrMatches(m.singbox.GetPortMap(), n.NodeKey(), proxy.Address) {
				continue
			}
			newNodes = append(newNodes, n)
		}
		if len(newNodes) != len(oldNodes) {
			if err := m.singbox.Reload(newNodes); err != nil {
				return fmt.Errorf("sing-box 重载失败: %w", err)
			}
			runtimeChanged = true
		}
	}
	if err := m.storage.DeleteProxyByID(proxy.ID); err != nil {
		if runtimeChanged {
			if rbErr := m.singbox.Reload(oldNodes); rbErr != nil {
				return fmt.Errorf("删除节点失败: %w; 回滚运行态失败: %v", err, rbErr)
			}
		}
		return fmt.Errorf("删除节点失败: %w", err)
	}
	return nil
}

// DeleteSubscription 删除订阅，并从 sing-box 运行态移除其隧道节点。
// 先变更运行态；数据库删除失败时，将运行态补偿恢复为旧节点集合。
func (m *Manager) DeleteSubscription(id int64) error {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()

	oldNodes := append([]ParsedNode(nil), m.singbox.GetNodes()...)
	removeKeys, err := m.subscriptionTunnelRuntimeKeys(id, oldNodes, m.singbox.GetPortMap())
	if err != nil {
		return fmt.Errorf("读取订阅旧运行态失败: %w", err)
	}
	newNodes := nodesExcludingKeys(oldNodes, removeKeys)
	runtimeChanged := len(removeKeys) > 0
	if runtimeChanged {
		if err := m.singbox.Reload(newNodes); err != nil {
			return fmt.Errorf("sing-box 重载失败: %w", err)
		}
	}

	if err := m.storage.DeleteSubscription(id); err != nil {
		if runtimeChanged {
			if rbErr := m.singbox.Reload(oldNodes); rbErr != nil {
				return fmt.Errorf("删除订阅失败: %w; 回滚运行态失败: %v", err, rbErr)
			}
		}
		return fmt.Errorf("删除订阅失败: %w", err)
	}
	return nil
}

// ManualImportResult 汇总直连手工代理的批量导入结果。
type ManualImportResult struct {
	Total      int      `json:"total"`
	Added      int      `json:"added"`
	Skipped    int      `json:"skipped"`
	Failed     int      `json:"failed"`
	Errors     []string `json:"errors,omitempty"`
	AddedAddrs []string `json:"added_addrs,omitempty"`
}

// AddManualNode 从单个链接添加人工节点，存储为 source=manual。
// 直连节点（http/socks5）先以 disabled 入库，再并发验证；通过后才 active。
// 加密节点经 sing-box 转本地 mixed 后入库并保持 disabled（不在 refreshMu 内做长时间验证），
// 由 UI「测试」或后续探测写回后再 active。
func (m *Manager) AddManualNode(link, region, note string) error {
	node, err := ParseSingleLink(link)
	if err != nil {
		return fmt.Errorf("解析节点链接失败: %w", err)
	}

	if node.IsDirect() {
		addr := node.DirectAddress()
		proto := node.DirectProtocol()
		user, pass := node.DirectCredentials()
		if err := m.storage.AddManualProxyWithCredentials(addr, proto, region, note, user, pass); err != nil {
			return fmt.Errorf("存储直连节点失败: %w", err)
		}
		proxy, err := m.storage.GetProxyByIdentity(addr, storage.SourceManual, 0)
		if err != nil {
			return fmt.Errorf("读取手工节点失败: %w", err)
		}
		m.validateManualProxies([]storage.Proxy{*proxy})
		return nil
	}

	return m.addManualTunnelNode(node, region, note)
}

// ImportManualLinks 解析多行代理文本，并插入或更新直连节点。
// 每行可在开头、结尾或中间包含注记；函数提取第一个
// socks5/socks4/http/https URL 令牌；批量导入不支持隧道链接。
// address+source 相同的已有手工代理计为跳过。
func (m *Manager) ImportManualLinks(text, region, note string) (ManualImportResult, error) {
	var result ManualImportResult
	seen := map[string]bool{}
	lines := strings.Split(text, "\n")
	var toAdd []storage.Proxy
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		result.Total++
		link := extractProxyLinkFromLine(line)
		if link == "" {
			result.Failed++
			if len(result.Errors) < 20 {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: no proxy URL found", truncateForError(line, 48)))
			}
			continue
		}
		node, err := ParseSingleLink(link)
		if err != nil {
			result.Failed++
			if len(result.Errors) < 20 {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", redactedProxyLinkForError(link), err))
			}
			continue
		}
		if !node.IsDirect() {
			result.Failed++
			if len(result.Errors) < 20 {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: batch import supports only http/socks5 direct links", redactedProxyLinkForError(link)))
			}
			continue
		}
		addr := node.DirectAddress()
		key := addr + "|" + node.DirectProtocol()
		if seen[key] {
			result.Skipped++
			continue
		}
		seen[key] = true
		// 已存在相同手工代理时跳过。
		if existing, err := m.storage.GetProxyByIdentity(addr, storage.SourceManual, 0); err == nil && existing != nil {
			result.Skipped++
			continue
		}
		user, pass := node.DirectCredentials()
		toAdd = append(toAdd, storage.Proxy{Address: addr, Protocol: node.DirectProtocol(), Username: user, Password: pass})
		result.AddedAddrs = append(result.AddedAddrs, addr)
	}
	if len(toAdd) == 0 {
		return result, nil
	}
	if err := m.storage.AddManualProxies(toAdd, region, note); err != nil {
		return result, fmt.Errorf("批量写入失败: %w", err)
	}
	result.Added = len(toAdd)
	// 入库后并发验证（ValidateStream 使用配置并发度）；失败保持 disabled。
	pending := make([]storage.Proxy, 0, len(toAdd))
	for _, item := range toAdd {
		proxy, err := m.storage.GetProxyByIdentity(item.Address, storage.SourceManual, 0)
		if err != nil {
			continue
		}
		pending = append(pending, *proxy)
	}
	m.validateManualProxies(pending)
	return result, nil
}

// validateManualProxies 并发验证手工节点：写回出口/延迟/纯净度/CF/AI，通过则 Enable。
// validator 为 nil 时仅保持 disabled，不静默标为可用。
func (m *Manager) validateManualProxies(proxies []storage.Proxy) {
	if m.validator == nil || len(proxies) == 0 {
		return
	}
	log.Printf("[custom] 🔍 开始验证 %d 个手工节点", len(proxies))
	cfg := config.Get()
	valid, invalid := 0, 0
	for result := range m.validator.ValidateStream(proxies) {
		if !result.Valid {
			invalid++
			_ = m.storage.DisableProxyByID(result.Proxy.ID)
			continue
		}
		latencyMs := int(result.Latency.Milliseconds())
		if err := m.storage.UpdateProxyExitInfo(
			result.Proxy.ID,
			result.ExitIP,
			result.ExitLocation,
			latencyMs,
			result.Risk.IPAPIIsScore,
			result.Risk.Flags,
			result.Risk.FlagsKnown,
			result.Risk.CFBlocked,
			result.Risk.AIReachability,
		); err != nil {
			log.Printf("[custom] ⚠️ 写回手工节点 %s 探测结果失败: %v", result.Proxy.Address, err)
			invalid++
			continue
		}
		if result.ExitLocation != "" && isGeoBlocked(result.ExitLocation, cfg) {
			log.Printf("[custom] 手工节点 %s 验证通过但被地理过滤 (%s)，保持禁用", result.Proxy.Address, result.ExitLocation)
			invalid++
			continue
		}
		if err := m.storage.EnableProxyByID(result.Proxy.ID); err != nil {
			log.Printf("[custom] ⚠️ 启用手工节点 %s 失败: %v", result.Proxy.Address, err)
			invalid++
			continue
		}
		valid++
	}
	log.Printf("[custom] 手工节点验证完成：%d 可用，%d 不可用", valid, invalid)
}

// extractProxyLinkFromLine 从自由格式行中提取第一个直连代理 URL 令牌。
// 支持 URL 前后或行中存在注记。
func extractProxyLinkFromLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return ""
	}
	lower := strings.ToLower(line)
	schemes := []string{"socks5://", "socks4://", "https://", "http://"}
	best := -1
	bestScheme := ""
	for _, scheme := range schemes {
		if i := strings.Index(lower, scheme); i >= 0 {
			if best < 0 || i < best {
				best = i
				bestScheme = scheme
			}
		}
	}
	if best < 0 {
		return ""
	}
	// 保留剩余文本的原始大小写；lower 仅用于定位 scheme。
	rest := line[best+len(bestScheme):]
	// URL 令牌在空白符或注记转储常用分隔符处结束。
	end := len(rest)
	for i, r := range rest {
		if unicode.IsSpace(r) || r == '|' || r == ',' || r == ';' {
			end = i
			break
		}
	}
	token := strings.TrimSpace(rest[:end])
	if token == "" {
		return ""
	}
	// 移除无空格粘连在末尾的常见杂字符：] ) >
	token = strings.TrimRight(token, ")]}>\"'")
	return bestScheme + token
}

func truncateForError(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// DeleteManualNodes 使用 DeleteManualNode 按 id 批量删除手工代理。
func (m *Manager) DeleteManualNodes(ids []int64) (deleted int, errs []string) {
	for _, id := range ids {
		if err := m.DeleteManualNode(id); err != nil {
			errs = append(errs, fmt.Sprintf("id=%d: %v", id, err))
			continue
		}
		deleted++
	}
	return deleted, errs
}

// DeleteManagedProxies 批量删除任意来源节点（含订阅），走 deleteProxyWithRuntime。
// 供统一节点表多选删除；勿再走仅手工的 DeleteManualNodes。
func (m *Manager) DeleteManagedProxies(ids []int64) (deleted int, errs []string) {
	for _, id := range ids {
		if err := m.DeleteManagedProxy(id); err != nil {
			errs = append(errs, fmt.Sprintf("id=%d: %v", id, err))
			continue
		}
		deleted++
	}
	return deleted, errs
}

// addManualTunnelNode 处理加密节点：合并进现有 sing-box 节点集后重载，取本地端口入库。
// DB 写入失败时补偿回滚到旧运行态，避免幽灵节点。
func (m *Manager) addManualTunnelNode(node *ParsedNode, region, note string) error {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()

	oldNodes := append([]ParsedNode(nil), m.singbox.GetNodes()...)
	mergedNodes := m.mergeWithExistingTunnelNodes(node)
	if err := m.singbox.Reload(mergedNodes); err != nil {
		return fmt.Errorf("sing-box 重载失败: %w", err)
	}

	key := node.NodeKey()
	socksPort, ok := m.singbox.GetPortMap()[key]
	if !ok {
		if rbErr := m.singbox.Reload(oldNodes); rbErr != nil {
			return fmt.Errorf("sing-box 未为节点 %s 分配本地端口; 回滚运行态失败: %v", key, rbErr)
		}
		return fmt.Errorf("sing-box 未为节点 %s 分配本地端口", key)
	}

	// 端口合并：手动加密节点只暴露一个 mixed 本地入站（单端口同时服务 SOCKS5 与 HTTP），
	// 入库为单条 socks5 记录。前端复制时可按需以 socks5:// 或 http:// scheme 呈现同一 IP:port。
	mixedAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(socksPort))
	// 写入真实 ParsedNode.NodeKey，避免手工隧道复制落到 127.0.0.1:临时端口。
	if err := m.storage.AddManualProxyWithNodeKey(mixedAddr, "socks5", region, note, key); err != nil {
		if rbErr := m.singbox.Reload(oldNodes); rbErr != nil {
			return fmt.Errorf("存储人工节点入站失败: %w; 回滚运行态失败: %v", err, rbErr)
		}
		return fmt.Errorf("存储人工节点入站失败: %w", err)
	}
	// 手动加密节点是 mixed 端口（单端口同时服务 SOCKS5+HTTP），显式置位 dual_protocol，
	// 供前端可靠渲染双协议标签，而非靠地址长相猜测。
	p, err := m.storage.GetProxyByAddress(mixedAddr)
	if err != nil {
		_ = m.storage.DeleteManualProxy(mixedAddr)
		if rbErr := m.singbox.Reload(oldNodes); rbErr != nil {
			return fmt.Errorf("读取手动节点 %s 失败: %w; 回滚运行态失败: %v", mixedAddr, err, rbErr)
		}
		return fmt.Errorf("读取手动节点 %s 失败: %w", mixedAddr, err)
	}
	if err := m.storage.SetProxyDualProtocol(p.ID, true); err != nil {
		_ = m.storage.DeleteProxyByID(p.ID)
		if rbErr := m.singbox.Reload(oldNodes); rbErr != nil {
			return fmt.Errorf("置位手动节点 dual_protocol 失败: %w; 回滚运行态失败: %v", err, rbErr)
		}
		return fmt.Errorf("置位手动节点 dual_protocol 失败: %w", err)
	}
	// mixed 本地端口依赖上游出站；入库后保持 disabled，由「测试」或后台探测写回后再 active。
	// 不在 refreshMu 内做长时间 ValidateOne，避免阻塞订阅刷新。
	return nil
}

func isLocalTunnelAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return host == "127.0.0.1" || host == "localhost" || (ip != nil && ip.IsLoopback())
}

// isLongTermDisabledProxy 判定是否为长期禁用：status=disabled 且 last_check 非零且超过阈值。
// last_check 为零视为短期禁用，保留运行态端口供 probeDisabled 重验证。
func isLongTermDisabledProxy(p storage.Proxy, now time.Time) bool {
	if p.Status != "disabled" {
		return false
	}
	if p.LastCheck.IsZero() {
		return false
	}
	return now.Sub(p.LastCheck) > longTermDisabledRetention
}

// disabledProxyHasRuntimePort 判断禁用代理的本地地址是否仍在当前 portMap 中。
func disabledProxyHasRuntimePort(address string, portMap map[string]int) bool {
	if !isLocalTunnelAddress(address) {
		// 直连禁用代理无 mixed 端口，不在 probe 的 tunnel 端口语义内；仍允许探测。
		return true
	}
	_, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return false
	}
	for _, p := range portMap {
		if p == port {
			return true
		}
	}
	return false
}

func (m *Manager) markLongTermEvictedKey(key string) {
	if key == "" {
		return
	}
	if m.longTermEvictedKeys == nil {
		m.longTermEvictedKeys = make(map[string]bool)
	}
	m.longTermEvictedKeys[key] = true
}

func (m *Manager) clearLongTermEvictedKey(key string) {
	if m.longTermEvictedKeys == nil {
		return
	}
	delete(m.longTermEvictedKeys, key)
}

// longTermDisabledRuntimeKeys 汇总应排除的长期禁用 NodeKey：
// DB 中仍映射到当前运行态地址的长期禁用 + 已记录的剔除集合。
func (m *Manager) longTermDisabledRuntimeKeys() map[string]bool {
	keys := make(map[string]bool)
	for k, v := range m.longTermEvictedKeys {
		if v {
			keys[k] = true
		}
	}
	disabled, err := m.storage.GetDisabledCustomProxies()
	if err != nil {
		return keys
	}
	allProxies, err := m.storage.GetAllForAdmin()
	if err != nil {
		return keys
	}
	nodes := m.singbox.GetNodes()
	portMap := m.singbox.GetPortMap()
	now := time.Now()
	retainedAddresses := retainedRuntimeAddresses(allProxies, now)
	for _, p := range disabled {
		if !isLongTermDisabledProxy(p, now) {
			continue
		}
		if !isLocalTunnelAddress(p.Address) {
			continue
		}
		if retainedAddresses[p.Address] {
			continue
		}
		if key := nodeKeyForLocalAddress(nodes, portMap, p.Address); key != "" {
			keys[key] = true
			m.markLongTermEvictedKey(key)
		}
	}
	return keys
}

// pruneLongTermDisabledFromRuntime 从 sing-box 运行态移除仍占端口的长期禁用隧道节点。
// 返回剔除数量；Reload 失败时返回 error，不静默吞掉。
func (m *Manager) pruneLongTermDisabledFromRuntime(disabled []storage.Proxy) (int, error) {
	if len(disabled) == 0 {
		return 0, nil
	}
	nodes := m.singbox.GetNodes()
	if len(nodes) == 0 {
		return 0, nil
	}
	portMap := m.singbox.GetPortMap()
	now := time.Now()
	allProxies, err := m.storage.GetAllForAdmin()
	if err != nil {
		return 0, err
	}
	retainedAddresses := retainedRuntimeAddresses(allProxies, now)
	removeKeys := make(map[string]bool)
	for _, p := range disabled {
		if !isLongTermDisabledProxy(p, now) {
			continue
		}
		if !isLocalTunnelAddress(p.Address) {
			continue
		}
		if retainedAddresses[p.Address] {
			continue
		}
		key := nodeKeyForLocalAddress(nodes, portMap, p.Address)
		if key == "" {
			continue
		}
		removeKeys[key] = true
		m.markLongTermEvictedKey(key)
	}
	if len(removeKeys) == 0 {
		return 0, nil
	}
	newNodes := nodesExcludingKeys(nodes, removeKeys)
	if len(newNodes) == len(nodes) {
		return 0, nil
	}
	if err := m.singbox.Reload(newNodes); err != nil {
		return 0, err
	}
	return len(removeKeys), nil
}

func retainedRuntimeAddresses(proxies []storage.Proxy, now time.Time) map[string]bool {
	retained := make(map[string]bool)
	for _, proxy := range proxies {
		if !isLocalTunnelAddress(proxy.Address) || isLongTermDisabledProxy(proxy, now) {
			continue
		}
		retained[proxy.Address] = true
	}
	return retained
}

func localAddrMatches(portMap map[string]int, key, address string) bool {
	port, ok := portMap[key]
	if !ok {
		return false
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) == address ||
		net.JoinHostPort("localhost", strconv.Itoa(port)) == address
}

func nodeKeyForLocalAddress(nodes []ParsedNode, portMap map[string]int, address string) string {
	for _, n := range nodes {
		if localAddrMatches(portMap, n.NodeKey(), address) {
			return n.NodeKey()
		}
	}
	// 回退：仅按端口匹配。
	_, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return ""
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return ""
	}
	for key, p := range portMap {
		if p == port {
			return key
		}
	}
	return ""
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
