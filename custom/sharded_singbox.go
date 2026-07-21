package custom

import (
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// singBoxShard 是分片编排层依赖的最小 sing-box 能力接口。
// 抽出接口是为了让 ShardedSingBox 的编排逻辑可在测试中注入 spy 实现，
// 从而无需真实 sing-box 二进制、无需 cgo 依赖即可验证纯编排行为。
// 注意：接口不含 GetHTTPPortMap——slice2 已将双入站收敛为单 mixed 入站，
// 单端口同时服务 SOCKS5 与 HTTP，只保留单一 portMap。
type singBoxShard interface {
	Reload(nodes []ParsedNode) error
	Stop()
	GetPortMap() map[string]int
	GetNodes() []ParsedNode
	GetRuntimeStatus() SingBoxRuntimeStatus
	GetLocalAddress(nodeKey string) string
}

// assemblyAwareShard 在实现可用时提供精确的节点装配决策。
// 未实现该接口的分片保持保守回退行为。
type assemblyAwareShard interface {
	GetAssemblyDiagnostics() assemblyDiagnostics
}

// 编译期断言：真实进程实现满足分片接口。若 SingBoxProcess 方法签名漂移，此处将直接编译失败。
var _ singBoxShard = (*SingBoxProcess)(nil)
var _ assemblyAwareShard = (*SingBoxProcess)(nil)
var _ assemblyAwareShard = (*ShardedSingBox)(nil)

// ShardedSingBox 是纯编排组件：把节点集合按稳定哈希切分到 N 个独立 sing-box 分片，
// 使得订阅重载时仅重启节点集合真正变化的分片，未变化分片保持进程不动（平滑重载）。
//
// 本组件只做编排，不直接启动进程；分片行为完全委托给注入的 singBoxShard 实现。
type ShardedSingBox struct {
	mu       sync.Mutex
	shards   []singBoxShard
	stopCh   chan struct{}
	stopOnce sync.Once
	stopping bool
	// assignedKeys 记录每个分片"当前已成功加载"的节点 key 集合。
	// 仅在 shard.Reload 成功后更新，Reload 失败时保持不变，以便下次重载对该分片重试。
	assignedKeys []map[string]bool
	// assembly 汇总最近一次成功提交的节点装配诊断（跨分片）。
	assembly assemblyDiagnostics
}

// shardFactory 依据分片序号与分片起始端口构造一个分片实现。
type shardFactory func(shardIndex, shardBasePort int) singBoxShard

// NewShardedSingBox 是生产环境使用的构造器。
// 每个分片拥有独立数据目录（避免各分片 config.json 相互覆盖）与互不重叠的端口段：
//
//	分片 i 数据目录 = filepath.Join(dataDir, "shard-<i>")
//	分片 i 起始端口 = basePort + i*portRangeSpan
func NewShardedSingBox(binPath, dataDir string, basePort, shardCount int) *ShardedSingBox {
	factory := func(shardIndex, shardBasePort int) singBoxShard {
		shardDir := dataDir
		if strings.TrimSpace(dataDir) != "" {
			shardDir = filepath.Join(dataDir, fmt.Sprintf("shard-%d", shardIndex))
		}
		return NewSingBoxProcess(binPath, shardDir, shardBasePort)
	}
	sb := newShardedSingBoxWithFactory(basePort, shardCount, factory)
	sb.stopCh = make(chan struct{})
	go sb.watchShards()
	return sb
}

// newShardedSingBoxWithFactory 是测试专用构造器：注入自定义分片工厂（如 spy）。
func newShardedSingBoxWithFactory(basePort, shardCount int, factory shardFactory) *ShardedSingBox {
	// 防御性收敛：分片数至少为 1。shardCount<1 时若不收敛会构造出 0 个分片，
	// 后续 shardIndexForKey 取模会除零、Reload 分区亦无处落点，直接编排崩溃。
	if shardCount < 1 {
		shardCount = 1
	}
	sb := &ShardedSingBox{
		shards:       make([]singBoxShard, shardCount),
		assignedKeys: make([]map[string]bool, shardCount),
	}
	for i := 0; i < shardCount; i++ {
		shardBasePort := basePort + i*portRangeSpan
		sb.shards[i] = factory(i, shardBasePort)
		sb.assignedKeys[i] = make(map[string]bool)
	}
	return sb
}

// shardIndexForKey 用固定的 FNV-1a 64 位哈希把节点 key 映射到 [0, shardCount) 的分片序号。
// 使用固定哈希而非 Go 内建 map 迭代顺序或 math/rand，保证同一 key 在进程重启后仍落到同一分片，
// 这是"仅重启变化分片"平滑重载的前提。
func shardIndexForKey(key string, shardCount int) int {
	if shardCount <= 0 {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum64() % uint64(shardCount))
}

// Reload 按分片重新加载节点。整个调用持有 sb.mu：所有 portmap 读取方都在调用方已串行化的
// 刷新路径上，重载期间短暂阻塞这些读取是可接受的，也让编排逻辑保持简单且正确。
//
// 算法：
//  1. 过滤出 tunnel 节点（非 direct）。
//  2. 若无 tunnel 节点：停止所有分片，以空重载清理运行态并清空已分配 key 集。
//  3. 按 shardIndexForKey 把 tunnel 节点分区到各分片，并对每个分片的节点按 NodeKey 稳定排序。
//  4. 逐分片：仅当目标 key 集与已分配 key 集不同才调用 shard.Reload；成功则更新已分配集，
//     失败则收集错误并保持已分配集不变（下次重试），不回滚其他分片（故障隔离）。
//  5. 有错误则聚合返回，否则返回 nil。
func (sb *ShardedSingBox) Reload(nodes []ParsedNode) error {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	// 已停止：拒绝复活分片。Stop 后仍可能有在途 RefreshSubscription/addManualTunnel
	// 持 refreshMu 后调用 Reload；若不拦截会重新拉起进程并与端口释放竞态。
	// 返回错误（而非 nil）以便上层刷新路径保留旧代理，避免“假成功重载后删旧入库”。
	if sb.stopping {
		return errors.New("sing-box already stopped")
	}

	// 1. 过滤 tunnel 节点，并预先剔除明确不可构建节点（坏节点不阻断好节点，也不污染 assignedKeys）。
	var tunnelNodes []ParsedNode
	var preRejected []assemblyRejectedNode
	for _, n := range nodes {
		if n.IsDirect() {
			continue
		}
		if _, err := buildOutbound(n, "prefilter"); err != nil {
			log.Printf("[custom] 跳过节点 %s (%s): %v", n.Name, n.Type, err)
			preRejected = append(preRejected, assemblyRejectedNode{node: n, err: err})
			continue
		}
		tunnelNodes = append(tunnelNodes, n)
	}

	// 2. 全部 tunnel 节点被明确拒绝时，保留最后一次成功运行态。
	// 这与真正的 direct-only 目标不同：前者刷新失败，不能停止旧代理；后者才应清空运行态。
	if len(tunnelNodes) == 0 {
		if len(preRejected) > 0 {
			return fmt.Errorf("所有隧道节点均被拒绝: %w", incompletePortAllocationErrorWithDiagnostics(
				rejectedNodesOnly(preRejected), map[string]int{}, assemblyDiagnostics{rejected: preRejected}))
		}

		// 无 tunnel 节点：先停止进程，再复用分片的空重载语义清理节点、端口、
		// 装配诊断与失败状态。仅成功清理的分片才提交空 assignedKeys。
		var errs []error
		for i, shard := range sb.shards {
			shard.Stop()
			if err := shard.Reload(nil); err != nil {
				errs = append(errs, fmt.Errorf("shard %d clear: %w", i, err))
				continue
			}
			sb.assignedKeys[i] = make(map[string]bool)
		}
		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		sb.assembly = assemblyDiagnostics{}
		return nil
	}

	// 3. 分区并稳定排序（仅可构建节点）。
	target := make([][]ParsedNode, len(sb.shards))
	for _, n := range tunnelNodes {
		idx := shardIndexForKey(n.NodeKey(), len(sb.shards))
		target[idx] = append(target[idx], n)
	}
	for i := range target {
		sortNodesByKey(target[i])
	}

	// 4. 逐分片重载（跳过未变化分片）。任一分片失败时，对已尝试变更的分片
	// 回滚到旧 nodes 快照（含失败分片自身可能已写入的 Partial 运行态）。
	oldNodesByShard := make([][]ParsedNode, len(sb.shards))
	oldKeysByShard := make([]map[string]bool, len(sb.shards))
	for i := range sb.shards {
		oldNodesByShard[i] = append([]ParsedNode(nil), sb.shards[i].GetNodes()...)
		oldKeysByShard[i] = copyKeySet(sb.assignedKeys[i])
	}

	var errs []error
	touched := make([]bool, len(sb.shards))
	for i := range sb.shards {
		newKeys := make(map[string]bool, len(target[i]))
		for _, n := range target[i] {
			newKeys[n.NodeKey()] = true
		}
		// 核心平滑性质：key 集未变化且分片仍健康时跳过；若进程已退出/失败，
		// 即使 key 集不变也必须重载，否则崩溃分片会被永久跳过。
		if keySetsEqual(newKeys, sb.assignedKeys[i]) && !shardNeedsReloadForRuntime(target[i], sb.shards[i]) {
			continue
		}
		touched[i] = true
		if err := sb.shards[i].Reload(target[i]); err != nil {
			errs = append(errs, fmt.Errorf("shard %d: %w", i, err))
			continue
		}
		// 仅 ReadyPorts==TotalPorts 且目标 key 均在 portMap 时才提交 assignedKeys。
		// Partial / 段满跳过等假成功不得提交，否则上层会删旧代理。
		accepted, err := shardReloadCommitError(target[i], sb.shards[i])
		if err != nil {
			errs = append(errs, fmt.Errorf("shard %d: %w", i, err))
			continue
		}
		sb.assignedKeys[i] = nodeKeySet(accepted)
	}

	if len(errs) > 0 {
		var rbErrs []error
		for i := range sb.shards {
			if !touched[i] {
				continue
			}
			// 回滚前或回滚时恢复 assignedKeys，使补偿失败后仍反映
			// 最近一次已知成功提交的节点集合。
			sb.assignedKeys[i] = copyKeySet(oldKeysByShard[i])
			if len(oldNodesByShard[i]) == 0 {
				sb.shards[i].Stop()
				if err := sb.shards[i].Reload(nil); err != nil {
					rbErrs = append(rbErrs, fmt.Errorf("shard %d rollback clear: %w", i, err))
				}
				continue
			}
			if err := sb.shards[i].Reload(oldNodesByShard[i]); err != nil {
				rbErrs = append(rbErrs, fmt.Errorf("shard %d rollback: %w", i, err))
				continue
			}
			if _, err := shardReloadCommitError(oldNodesByShard[i], sb.shards[i]); err != nil {
				rbErrs = append(rbErrs, fmt.Errorf("shard %d rollback: %w", i, err))
			}
		}
		if len(rbErrs) > 0 {
			return errors.Join(append(errs, rbErrs...)...)
		}
		return errors.Join(errs...)
	}

	// 成功提交后汇总诊断，供 Manager 区分“明确拒绝”与“加载不完整”。
	sb.assembly = mergeAssemblyDiagnostics(preRejected, sb.shards)
	return nil
}

func rejectedNodesOnly(rejected []assemblyRejectedNode) []ParsedNode {
	out := make([]ParsedNode, 0, len(rejected))
	for _, r := range rejected {
		out = append(out, r.node)
	}
	return out
}

func mergeAssemblyDiagnostics(preRejected []assemblyRejectedNode, shards []singBoxShard) assemblyDiagnostics {
	out := assemblyDiagnostics{rejected: append([]assemblyRejectedNode(nil), preRejected...)}
	for _, shard := range shards {
		aware, ok := shard.(assemblyAwareShard)
		if !ok {
			// 无诊断的分片：把当前已加载节点视为 accepted。
			out.accepted = append(out.accepted, shard.GetNodes()...)
			continue
		}
		d := aware.GetAssemblyDiagnostics()
		out.accepted = append(out.accepted, d.accepted...)
		out.rejected = append(out.rejected, d.rejected...)
		out.segmentFull = append(out.segmentFull, d.segmentFull...)
	}
	return out
}

// GetAssemblyDiagnostics 汇总各分片装配诊断（调用方需在 Reload 成功后读取）。
func (sb *ShardedSingBox) GetAssemblyDiagnostics() assemblyDiagnostics {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return assemblyDiagnostics{
		accepted:    append([]ParsedNode(nil), sb.assembly.accepted...),
		rejected:    append([]assemblyRejectedNode(nil), sb.assembly.rejected...),
		segmentFull: append([]ParsedNode(nil), sb.assembly.segmentFull...),
	}
}

func copyKeySet(in map[string]bool) map[string]bool {
	if in == nil {
		return make(map[string]bool)
	}
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// shardReloadCommitError 在分片 Reload 返回 nil 后二次校验是否可提交 assignedKeys。
// 空目标（停止分片）视为完整；否则要求 portMap 覆盖全部目标 key，且运行态全就绪。
func shardReloadCommitError(target []ParsedNode, shard singBoxShard) ([]ParsedNode, error) {
	if len(target) == 0 {
		return nil, nil
	}
	portMap := shard.GetPortMap()
	diagnostics := assemblyDiagnostics{}
	if aware, ok := shard.(assemblyAwareShard); ok {
		diagnostics = aware.GetAssemblyDiagnostics()
	}

	rejected := diagnostics.rejectedKeys()
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
	rs := shard.GetRuntimeStatus()
	if rs.Status == SingBoxStatusPartial || rs.Reason == "ports_not_ready" {
		return nil, fmt.Errorf("sing-box 重载不完整: status=%s reason=%s ready=%d/%d",
			rs.Status, rs.Reason, rs.ReadyPorts, rs.TotalPorts)
	}
	if rs.TotalPorts > 0 && rs.ReadyPorts != rs.TotalPorts {
		return nil, fmt.Errorf("sing-box 端口未完全就绪（%d/%d）", rs.ReadyPorts, rs.TotalPorts)
	}
	return accepted, nil
}

// shardNeedsReloadForRuntime 判断 key 集未变化时是否仍需因运行态异常而强制重载。
// 仅对目标非空的分片生效；空分片由 key 集变化路径负责停止/清理。
// Partial / ports_not_ready / 目标 key 无端口 均视为未健康，避免 assignedKeys 相等导致永久跳过。
func shardNeedsReloadForRuntime(target []ParsedNode, shard singBoxShard) bool {
	if len(target) == 0 {
		return false
	}
	rs := shard.GetRuntimeStatus()
	switch rs.Status {
	case SingBoxStatusFailed, SingBoxStatusStopped, SingBoxStatusPartial:
		return true
	}
	if rs.Reason == "ports_not_ready" {
		return true
	}
	if rs.TotalPorts > 0 && rs.ReadyPorts != rs.TotalPorts {
		return true
	}
	// 已提交的 assigned 节点若丢失端口，必须重载；明确拒绝的节点不计入缺失。
	if _, err := shardReloadCommitError(target, shard); err != nil {
		return true
	}
	return false
}

func nodeKeySet(nodes []ParsedNode) map[string]bool {
	keys := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		keys[node.NodeKey()] = true
	}
	return keys
}

const shardHealthCheckInterval = 5 * time.Second

func (sb *ShardedSingBox) watchShards() {
	ticker := time.NewTicker(shardHealthCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := sb.recoverFailedShards(); err != nil {
				log.Printf("[custom] sing-box 分片恢复失败: %v", err)
			}
		case <-sb.stopCh:
			return
		}
	}
}

// recoverFailedShards 恢复进程异常退出的分片，不重载健康分片。
func (sb *ShardedSingBox) recoverFailedShards() error {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	if sb.stopping {
		return nil
	}

	var errs []error
	for i, shard := range sb.shards {
		if len(sb.assignedKeys[i]) == 0 || !shardNeedsReloadForRuntime(shard.GetNodes(), shard) {
			continue
		}
		nodes := shard.GetNodes()
		if err := shard.Reload(nodes); err != nil {
			errs = append(errs, fmt.Errorf("shard %d: %w", i, err))
			continue
		}
		accepted, err := shardReloadCommitError(nodes, shard)
		if err != nil {
			errs = append(errs, fmt.Errorf("shard %d recovery commit: %w", i, err))
			continue
		}
		sb.assignedKeys[i] = nodeKeySet(accepted)
	}
	return errors.Join(errs...)
}

// sortNodesByKey 按 NodeKey 对节点做稳定排序，保证分片内节点顺序确定，
// 使 sing-box 配置生成与端口分配可复现。
func sortNodesByKey(nodes []ParsedNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].NodeKey() < nodes[j].NodeKey()
	})
}

// keySetsEqual 先比长度再比成员，判断两个 key 集是否完全一致。
func keySetsEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// GetPortMap 合并所有分片的端口映射。各分片 key 按构造互不相交，直接并入新 map。
func (sb *ShardedSingBox) GetPortMap() map[string]int {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	result := make(map[string]int)
	for _, shard := range sb.shards {
		for k, v := range shard.GetPortMap() {
			result[k] = v
		}
	}
	return result
}

// GetNodes 把所有分片的已加载节点拼接进一个新切片返回。
func (sb *ShardedSingBox) GetNodes() []ParsedNode {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	var result []ParsedNode
	for _, shard := range sb.shards {
		result = append(result, shard.GetNodes()...)
	}
	return result
}

// GetNodeCount 返回所有分片端口映射长度之和（即已加载 tunnel 节点总数）。
func (sb *ShardedSingBox) GetNodeCount() int {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	total := 0
	for _, shard := range sb.shards {
		total += len(shard.GetPortMap())
	}
	return total
}

// GetLocalAddress 依据 key 的稳定分片映射，委托到对应分片查询本地地址。
func (sb *ShardedSingBox) GetLocalAddress(nodeKey string) string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	idx := shardIndexForKey(nodeKey, len(sb.shards))
	return sb.shards[idx].GetLocalAddress(nodeKey)
}

// Stop 停止所有分片。
func (sb *ShardedSingBox) Stop() {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	if sb.stopping {
		return
	}
	sb.stopping = true
	if sb.stopCh != nil {
		sb.stopOnce.Do(func() { close(sb.stopCh) })
	}
	for _, shard := range sb.shards {
		shard.Stop()
	}
}

// GetRuntimeStatus 汇总各分片运行态为单一可解释状态。
//   - Nodes/ReadyPorts/TotalPorts 为各分片对应字段之和。
//   - Nodes>0 或 Status=failed 的分片参与状态汇总；无此类分片时报告 NoTunnelNodes。
//   - 参与分片全部 running → Running；全部非 running → Failed；部分 → Partial。
func (sb *ShardedSingBox) GetRuntimeStatus() SingBoxRuntimeStatus {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	var nodes, ready, total int
	var considered []SingBoxRuntimeStatus
	for _, shard := range sb.shards {
		rs := shard.GetRuntimeStatus()
		nodes += rs.Nodes
		ready += rs.ReadyPorts
		total += rs.TotalPorts
		if rs.Nodes > 0 || rs.Status == SingBoxStatusFailed {
			considered = append(considered, rs)
		}
	}

	if len(considered) == 0 {
		return SingBoxRuntimeStatus{
			Running:    false,
			Status:     SingBoxStatusNoTunnelNodes,
			Reason:     "no_tunnel_nodes",
			Nodes:      0,
			ReadyPorts: 0,
			TotalPorts: 0,
		}
	}

	runningCount := 0
	for _, rs := range considered {
		if rs.Running && rs.Status == SingBoxStatusRunning {
			runningCount++
		}
	}

	result := SingBoxRuntimeStatus{
		Nodes:      nodes,
		ReadyPorts: ready,
		TotalPorts: total,
	}
	switch {
	case runningCount == len(considered):
		result.Status = SingBoxStatusRunning
		result.Reason = SingBoxStatusRunning
		result.Running = true
	case runningCount == 0:
		result.Status = SingBoxStatusFailed
		result.Reason = aggregateShardFailureReason(considered)
		result.Running = false
	default:
		result.Status = SingBoxStatusPartial
		result.Reason = "partial_shards"
		result.Running = true
	}
	return result
}

func aggregateShardFailureReason(statuses []SingBoxRuntimeStatus) string {
	reason := ""
	for _, status := range statuses {
		if status.Status != SingBoxStatusFailed {
			return "all_shards_failed"
		}
		candidate := strings.TrimSpace(status.Reason)
		if candidate == "" {
			return "all_shards_failed"
		}
		if reason == "" {
			reason = candidate
			continue
		}
		if reason != candidate {
			return "all_shards_failed"
		}
	}
	if reason == "" {
		return "all_shards_failed"
	}
	return reason
}
