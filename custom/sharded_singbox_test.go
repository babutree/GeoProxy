package custom

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// spyShard 是满足 singBoxShard 的测试替身，记录 Reload/Stop 调用并允许注入返回错误、
// 运行态与端口映射，从而无需真实 sing-box 二进制、无需 cgo 即可验证纯编排逻辑。
type spyShard struct {
	mu          sync.Mutex
	reloadCalls int
	lastKeys    map[string]bool
	stopCalls   int

	reloadErr error
	// incompletePorts：模拟段满跳过——Reload 返回 nil 但不为目标 key 分配端口。
	incompletePorts bool
	// forcePartial：模拟 ports_not_ready——portMap 完整但 Status=Partial。
	forcePartial bool
	status       SingBoxRuntimeStatus
	portMap      map[string]int
	localAddrs   map[string]string
	nodes        []ParsedNode
}

func newSpyShard() *spyShard {
	return &spyShard{
		lastKeys:   map[string]bool{},
		portMap:    map[string]int{},
		localAddrs: map[string]string{},
	}
}

func (s *spyShard) Reload(nodes []ParsedNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadCalls++
	keys := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		keys[n.NodeKey()] = true
	}
	s.lastKeys = keys
	if s.reloadErr != nil {
		return s.reloadErr
	}
	s.nodes = append([]ParsedNode(nil), nodes...)
	if s.incompletePorts {
		s.portMap = map[string]int{}
		s.status = SingBoxRuntimeStatus{
			Running:    true,
			Status:     SingBoxStatusRunning,
			ReadyPorts: 0,
			TotalPorts: 0,
		}
		return nil
	}
	s.portMap = make(map[string]int, len(nodes))
	for i, n := range nodes {
		s.portMap[n.NodeKey()] = 10000 + i
	}
	if s.forcePartial {
		s.status = SingBoxRuntimeStatus{
			Running:    true,
			Status:     SingBoxStatusPartial,
			Reason:     "ports_not_ready",
			Nodes:      len(nodes),
			ReadyPorts: 0,
			TotalPorts: len(nodes),
		}
		return nil
	}
	s.status = SingBoxRuntimeStatus{
		Running:    true,
		Status:     SingBoxStatusRunning,
		Nodes:      len(nodes),
		ReadyPorts: len(nodes),
		TotalPorts: len(nodes),
	}
	return nil
}

func (s *spyShard) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopCalls++
}

func (s *spyShard) GetPortMap() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int, len(s.portMap))
	for k, v := range s.portMap {
		out[k] = v
	}
	return out
}

func (s *spyShard) GetNodes() []ParsedNode {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ParsedNode(nil), s.nodes...)
}

func (s *spyShard) GetRuntimeStatus() SingBoxRuntimeStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *spyShard) GetLocalAddress(nodeKey string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.localAddrs[nodeKey]
}

func (s *spyShard) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reloadCalls
}

func (s *spyShard) stops() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopCalls
}

// newSpyOrchestrator 构造一个使用 spy 分片的编排器，并返回按分片序号对应的 spy 列表。
func newSpyOrchestrator(basePort, shardCount int) (*ShardedSingBox, []*spyShard) {
	var spies []*spyShard
	factory := func(shardIndex, shardBasePort int) singBoxShard {
		s := newSpyShard()
		spies = append(spies, s)
		return s
	}
	sb := newShardedSingBoxWithFactory(basePort, shardCount, factory)
	return sb, spies
}

// directNode 构造一个 direct（http）节点，用于验证"仅 direct 节点"路径。
func directNodeForShard(name string) ParsedNode {
	return ParsedNode{
		Name:   name,
		Type:   "http",
		Server: "127.0.0.1",
		Port:   8080,
		Raw:    map[string]interface{}{"type": "http", "server": "127.0.0.1", "port": 8080},
	}
}

// nodesOnDistinctShards 返回 count 个映射到互不相同分片的 tunnel 节点。
func nodesOnDistinctShards(t *testing.T, shardCount, count int) []ParsedNode {
	t.Helper()
	byShard := map[int]ParsedNode{}
	for i := 0; len(byShard) < count && i < 100000; i++ {
		n := tunnelNode(fmt.Sprintf("n%d", i), fmt.Sprintf("node-%d.example.com", i), fmt.Sprintf("pw%d", i))
		idx := shardIndexForKey(n.NodeKey(), shardCount)
		if _, ok := byShard[idx]; !ok {
			byShard[idx] = n
		}
	}
	if len(byShard) < count {
		t.Fatalf("无法找到 %d 个映射到不同分片的节点（仅找到 %d）", count, len(byShard))
	}
	out := make([]ParsedNode, 0, count)
	for _, n := range byShard {
		out = append(out, n)
		if len(out) == count {
			break
		}
	}
	return out
}

// TestShardIndexStableAndBounded 验证哈希路由稳定、有界且具备基本分散性。
func TestShardIndexStableAndBounded(t *testing.T) {
	const n = 4
	key := "trojan:example.com:443:deadbeef"

	first := shardIndexForKey(key, n)
	for i := 0; i < 100; i++ {
		if got := shardIndexForKey(key, n); got != first {
			t.Fatalf("shardIndexForKey 不稳定: 第 %d 次得到 %d，首次 %d", i, got, first)
		}
	}

	used := map[int]bool{}
	for i := 0; i < 20; i++ {
		k := fmt.Sprintf("key-%d", i)
		idx := shardIndexForKey(k, n)
		if idx < 0 || idx >= n {
			t.Fatalf("索引越界: key=%s idx=%d 不在 [0,%d)", k, idx, n)
		}
		used[idx] = true
	}
	if len(used) < 2 {
		t.Fatalf("20 个不同 key 全部塌缩到 %d 个分片，分散性不足", len(used))
	}
}

// TestConstructorCreatesShardCountShardsAndClamps 验证分片数创建与下界收敛。
func TestConstructorCreatesShardCountShardsAndClamps(t *testing.T) {
	sb, spies := newSpyOrchestrator(10000, 4)
	if len(sb.shards) != 4 || len(sb.assignedKeys) != 4 || len(spies) != 4 {
		t.Fatalf("N=4 应创建 4 个分片，得到 shards=%d assigned=%d spies=%d",
			len(sb.shards), len(sb.assignedKeys), len(spies))
	}

	for _, bad := range []int{0, -3} {
		sb, spies := newSpyOrchestrator(10000, bad)
		if len(sb.shards) != 1 || len(sb.assignedKeys) != 1 || len(spies) != 1 {
			t.Fatalf("N=%d 应收敛为 1 个分片，得到 shards=%d assigned=%d spies=%d",
				bad, len(sb.shards), len(sb.assignedKeys), len(spies))
		}
	}
}

// TestPortSegmentsDisjoint 经真实构造器验证各分片端口段互不重叠。
func TestPortSegmentsDisjoint(t *testing.T) {
	const basePort = 20000
	const shardCount = 4
	sb := NewShardedSingBox("missing-sing-box", t.TempDir(), basePort, shardCount)
	if len(sb.shards) != shardCount {
		t.Fatalf("应有 %d 个分片，得到 %d", shardCount, len(sb.shards))
	}

	type seg struct{ lo, hi int }
	segs := make([]seg, shardCount)
	for i := 0; i < shardCount; i++ {
		sp, ok := sb.shards[i].(*SingBoxProcess)
		if !ok {
			t.Fatalf("分片 %d 不是 *SingBoxProcess", i)
		}
		wantBase := basePort + i*portRangeSpan
		if sp.basePort != wantBase {
			t.Fatalf("分片 %d basePort=%d，期望 %d", i, sp.basePort, wantBase)
		}
		segs[i] = seg{lo: wantBase, hi: wantBase + portRangeSpan}
	}
	for i := 0; i < shardCount; i++ {
		for j := i + 1; j < shardCount; j++ {
			if segs[i].lo < segs[j].hi && segs[j].lo < segs[i].hi {
				t.Fatalf("分片 %d 段 [%d,%d) 与分片 %d 段 [%d,%d) 重叠",
					i, segs[i].lo, segs[i].hi, j, segs[j].lo, segs[j].hi)
			}
		}
	}
}

// TestReloadSkipsUnchangedShards 验证核心平滑性质：相同节点集重载不触发任何分片重载。
func TestReloadSkipsUnchangedShards(t *testing.T) {
	const n = 4
	sb, spies := newSpyOrchestrator(10000, n)
	setA := nodesOnDistinctShards(t, n, 3)

	if err := sb.Reload(setA); err != nil {
		t.Fatalf("首次 Reload 出错: %v", err)
	}
	before := make([]int, len(spies))
	totalBefore := 0
	for i, s := range spies {
		before[i] = s.calls()
		totalBefore += before[i]
	}
	if totalBefore == 0 {
		t.Fatalf("首次 Reload 未触发任何分片重载")
	}

	if err := sb.Reload(setA); err != nil {
		t.Fatalf("第二次 Reload 出错: %v", err)
	}
	for i, s := range spies {
		if s.calls() != before[i] {
			t.Fatalf("分片 %d 在相同节点集重载后被再次 Reload: %d → %d", i, before[i], s.calls())
		}
	}
}

// TestReloadRetriesUnchangedFailedShard 覆盖崩溃分片恢复：即使节点 key 集未变化，
// 只要分片运行态已经失败，也必须再次 Reload，不能因 assignedKeys 相等而永久跳过。
func TestReloadRetriesUnchangedFailedShard(t *testing.T) {
	const n = 4
	sb, spies := newSpyOrchestrator(10000, n)
	node := tunnelNode("will-crash", "will-crash.example.com", "pw")
	idx := shardIndexForKey(node.NodeKey(), n)

	if err := sb.Reload([]ParsedNode{node}); err != nil {
		t.Fatalf("首次 Reload 出错: %v", err)
	}
	before := spies[idx].calls()
	spies[idx].status = SingBoxRuntimeStatus{
		Running:    false,
		Status:     SingBoxStatusFailed,
		Reason:     "process_exited",
		Nodes:      1,
		ReadyPorts: 0,
		TotalPorts: 1,
	}

	if err := sb.Reload([]ParsedNode{node}); err != nil {
		t.Fatalf("崩溃后 Reload 出错: %v", err)
	}
	if got := spies[idx].calls(); got != before+1 {
		t.Fatalf("崩溃分片 key 集未变也必须重载，Reload 次数=%d，期望 %d", got, before+1)
	}

	for i, s := range spies {
		if i == idx {
			continue
		}
		if got := s.calls(); got != 0 {
			t.Fatalf("未受影响分片 %d 不应被 Reload，得到 %d", i, got)
		}
	}
}

// TestRecoverFailedShardsRestartsOnlyFailedShard 验证健康检查无需等待订阅变化，
// 可直接恢复已崩溃分片，同时不触碰仍健康的分片。
func TestRecoverFailedShardsRestartsOnlyFailedShard(t *testing.T) {
	const n = 4
	sb, spies := newSpyOrchestrator(10000, n)
	nodes := nodesOnDistinctShards(t, n, 2)
	if err := sb.Reload(nodes); err != nil {
		t.Fatalf("首次 Reload 出错: %v", err)
	}

	failedIdx := shardIndexForKey(nodes[0].NodeKey(), n)
	before := make([]int, len(spies))
	for i, shard := range spies {
		before[i] = shard.calls()
	}
	spies[failedIdx].status = SingBoxRuntimeStatus{
		Running: false,
		Status:  SingBoxStatusStopped,
		Reason:  "process_exited",
		Nodes:   1,
	}

	if err := sb.recoverFailedShards(); err != nil {
		t.Fatalf("recoverFailedShards() error = %v", err)
	}
	for i, shard := range spies {
		want := before[i]
		if i == failedIdx {
			want++
		}
		if got := shard.calls(); got != want {
			t.Fatalf("分片 %d Reload 次数=%d，期望 %d", i, got, want)
		}
	}
}

// TestReloadRetriesUnchangedPartialShard：key 集未变但 Status=Partial 时不得永久跳过。
func TestReloadRetriesUnchangedPartialShard(t *testing.T) {
	const n = 4
	sb, spies := newSpyOrchestrator(10000, n)
	node := tunnelNode("partial-retry", "partial-retry.example.com", "pw")
	idx := shardIndexForKey(node.NodeKey(), n)

	if err := sb.Reload([]ParsedNode{node}); err != nil {
		t.Fatalf("首次 Reload 出错: %v", err)
	}
	before := spies[idx].calls()
	spies[idx].status = SingBoxRuntimeStatus{
		Running:    true,
		Status:     SingBoxStatusPartial,
		Reason:     "ports_not_ready",
		Nodes:      1,
		ReadyPorts: 0,
		TotalPorts: 1,
	}

	if err := sb.Reload([]ParsedNode{node}); err != nil {
		t.Fatalf("Partial 后 Reload 出错: %v", err)
	}
	if got := spies[idx].calls(); got != before+1 {
		t.Fatalf("Partial 分片 key 集未变也必须重载，Reload 次数=%d，期望 %d", got, before+1)
	}
}

// TestRecoverFailedShardsRejectsPartialReloadCommit：健康检查重载后仍为 Partial 时，
// 必须返回提交门错误并保留最后一次成功的 assignedKeys，供后续周期继续重试。
func TestRecoverFailedShardsRejectsPartialReloadCommit(t *testing.T) {
	const n = 4
	sb, spies := newSpyOrchestrator(10000, n)
	nodes := nodesOnDistinctShards(t, n, 2)
	if err := sb.Reload(nodes); err != nil {
		t.Fatalf("首次 Reload 出错: %v", err)
	}

	partialIdx := shardIndexForKey(nodes[0].NodeKey(), n)
	before := make([]int, len(spies))
	for i, shard := range spies {
		before[i] = shard.calls()
	}
	spies[partialIdx].mu.Lock()
	spies[partialIdx].forcePartial = true
	spies[partialIdx].status = SingBoxRuntimeStatus{
		Running:    true,
		Status:     SingBoxStatusPartial,
		Reason:     "ports_not_ready",
		Nodes:      1,
		ReadyPorts: 0,
		TotalPorts: 1,
	}
	spies[partialIdx].mu.Unlock()

	beforeKeys := copyKeySet(sb.assignedKeys[partialIdx])
	if err := sb.recoverFailedShards(); err == nil {
		t.Fatal("recoverFailedShards() partial reload error = nil, want commit-gate error")
	}
	if !keySetsEqual(sb.assignedKeys[partialIdx], beforeKeys) {
		t.Fatalf("partial recovery changed assignedKeys: got %v, want %v", sb.assignedKeys[partialIdx], beforeKeys)
	}
	for i, shard := range spies {
		want := before[i]
		if i == partialIdx {
			want++
		}
		if got := shard.calls(); got != want {
			t.Fatalf("分片 %d Reload 次数=%d，期望 %d", i, got, want)
		}
	}
}

// TestReloadRetriesUnchangedIncompletePortMap：已提交 assignedKeys 后若 portMap 丢 key，
// 不得因 key 集相等永久跳过（段满/无端口不得视为已成功分配）。
func TestReloadRetriesUnchangedIncompletePortMap(t *testing.T) {
	const n = 4
	sb, spies := newSpyOrchestrator(10000, n)
	node := tunnelNode("no-port-retry", "no-port-retry.example.com", "pw")
	idx := shardIndexForKey(node.NodeKey(), n)

	if err := sb.Reload([]ParsedNode{node}); err != nil {
		t.Fatalf("首次 Reload 出错: %v", err)
	}
	if !sb.assignedKeys[idx][node.NodeKey()] {
		t.Fatalf("首次成功后应提交 assignedKeys")
	}
	before := spies[idx].calls()

	// 运行态退化为无端口，且后续 Reload 仍 incomplete，确保不会跳过也不会“假成功提交”。
	spies[idx].mu.Lock()
	spies[idx].incompletePorts = true
	spies[idx].portMap = map[string]int{}
	spies[idx].status = SingBoxRuntimeStatus{
		Running:    true,
		Status:     SingBoxStatusRunning,
		Nodes:      1,
		ReadyPorts: 0,
		TotalPorts: 0,
	}
	spies[idx].mu.Unlock()

	err := sb.Reload([]ParsedNode{node})
	if err == nil {
		t.Fatal("无端口 incomplete 时 Reload 应返回 error")
	}
	// forward incomplete attempt + compensation reload of old snapshot
	if got := spies[idx].calls(); got < before+2 {
		t.Fatalf("无端口 incomplete 应触发重载与回滚，Reload 次数=%d，期望 >= %d", got, before+2)
	}
	// 提交失败时保留旧 assignedKeys 以便下次仍走 needsReload；不得清空为“未分配”后静默成功。
	if !sb.assignedKeys[idx][node.NodeKey()] {
		t.Fatalf("incomplete 重试失败后应保留旧 assignedKeys 以便继续重试，得到 %v", sb.assignedKeys[idx])
	}
}

// TestRecoverFailedShardsRestartsIncompletePortShard：portMap 缺目标 key 时健康检查应重载。
func TestRecoverFailedShardsRestartsIncompletePortShard(t *testing.T) {
	const n = 4
	sb, spies := newSpyOrchestrator(10000, n)
	nodes := nodesOnDistinctShards(t, n, 2)
	if err := sb.Reload(nodes); err != nil {
		t.Fatalf("首次 Reload 出错: %v", err)
	}

	badIdx := shardIndexForKey(nodes[0].NodeKey(), n)
	before := make([]int, len(spies))
	for i, shard := range spies {
		before[i] = shard.calls()
	}
	spies[badIdx].mu.Lock()
	spies[badIdx].incompletePorts = true
	spies[badIdx].portMap = map[string]int{}
	spies[badIdx].status = SingBoxRuntimeStatus{
		Running:    true,
		Status:     SingBoxStatusRunning,
		Nodes:      1,
		ReadyPorts: 0,
		TotalPorts: 0,
	}
	spies[badIdx].mu.Unlock()

	beforeKeys := copyKeySet(sb.assignedKeys[badIdx])
	if err := sb.recoverFailedShards(); err == nil {
		t.Fatal("recoverFailedShards() incomplete reload error = nil, want commit-gate error")
	}
	if !keySetsEqual(sb.assignedKeys[badIdx], beforeKeys) {
		t.Fatalf("incomplete recovery changed assignedKeys: got %v, want %v", sb.assignedKeys[badIdx], beforeKeys)
	}
	for i, shard := range spies {
		want := before[i]
		if i == badIdx {
			want++
		}
		if got := shard.calls(); got != want {
			t.Fatalf("分片 %d Reload 次数=%d，期望 %d", i, got, want)
		}
	}
}

func TestRecoverFailedShardsDoesNothingAfterStop(t *testing.T) {
	sb, spies := newSpyOrchestrator(10000, 1)
	node := tunnelNode("stopped", "stopped.example.com", "pw")
	if err := sb.Reload([]ParsedNode{node}); err != nil {
		t.Fatalf("首次 Reload 出错: %v", err)
	}
	before := spies[0].calls()
	spies[0].status = SingBoxRuntimeStatus{Status: SingBoxStatusStopped, Nodes: 1}

	sb.Stop()
	if err := sb.recoverFailedShards(); err != nil {
		t.Fatalf("停止后 recoverFailedShards() error = %v", err)
	}
	if got := spies[0].calls(); got != before {
		t.Fatalf("停止后分片被复活，Reload 次数=%d，期望 %d", got, before)
	}
}

// TestReloadDoesNothingAfterStop 生命周期竞态：Manager.Stop / 编排器 Stop 之后，
// 仍在途的订阅刷新不得再调用 shard.Reload 复活已停止进程（端口复用竞态与资源泄漏）。
// 故意换节点集，确保若未检查 stopping 会命中真实 Reload 路径（而非 key 集相等跳过）。
func TestReloadDoesNothingAfterStop(t *testing.T) {
	sb, spies := newSpyOrchestrator(10000, 2)
	nodeA := tunnelNode("post-stop-a", "post-stop-a.example.com", "pw-a")
	if err := sb.Reload([]ParsedNode{nodeA}); err != nil {
		t.Fatalf("首次 Reload 出错: %v", err)
	}
	before := make([]int, len(spies))
	for i, s := range spies {
		before[i] = s.calls()
	}
	sb.Stop()

	// 节点集变化 → 正常路径必会 shard.Reload；停止后必须被拦截。
	nodeB := tunnelNode("post-stop-b", "post-stop-b.example.com", "pw-b")
	if err := sb.Reload([]ParsedNode{nodeB}); err == nil {
		t.Fatal("停止后 Reload error = nil, want already-stopped error")
	}
	for i, s := range spies {
		if got := s.calls(); got != before[i] {
			t.Fatalf("停止后分片 %d 被 Reload 复活：次数=%d，期望 %d", i, got, before[i])
		}
	}
}

// TestConcurrentReloadAndStopDoesNotReviveShards 加压：Stop 与 Reload 并发时，
// 最终状态必须是已停止且不再接受后续 Reload 复活（-race 下可暴露锁/标志遗漏）。
func TestConcurrentReloadAndStopDoesNotReviveShards(t *testing.T) {
	sb, spies := newSpyOrchestrator(10000, 2)
	nodeA := tunnelNode("race-a", "race-a.example.com", "pw-a")
	if err := sb.Reload([]ParsedNode{nodeA}); err != nil {
		t.Fatalf("首次 Reload 出错: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			n := tunnelNode(fmt.Sprintf("race-%d", i), fmt.Sprintf("race-%d.example.com", i), "pw")
			_ = sb.Reload([]ParsedNode{n})
		}
	}()
	go func() {
		defer wg.Done()
		sb.Stop()
	}()
	wg.Wait()

	afterStop := make([]int, len(spies))
	for i, s := range spies {
		afterStop[i] = s.calls()
	}
	// Stop 完成后，换节点集的 Reload 仍不得复活任何分片。
	nodeFinal := tunnelNode("race-final", "race-final.example.com", "pw-final")
	if err := sb.Reload([]ParsedNode{nodeFinal}); err == nil {
		t.Fatal("停止后 Reload error = nil, want already-stopped error")
	}
	for i, s := range spies {
		if got := s.calls(); got != afterStop[i] {
			t.Fatalf("Stop 竞态后分片 %d 仍被复活：次数=%d，期望 %d", i, got, afterStop[i])
		}
	}
}

// TestReloadTargetsOnlyChangedShard 验证新增一个节点只重载其所属分片。
func TestReloadTargetsOnlyChangedShard(t *testing.T) {
	const n = 4
	sb, spies := newSpyOrchestrator(10000, n)
	setA := nodesOnDistinctShards(t, n, 2)

	if err := sb.Reload(setA); err != nil {
		t.Fatalf("首次 Reload 出错: %v", err)
	}
	before := make([]int, len(spies))
	for i, s := range spies {
		before[i] = s.calls()
	}

	newNode := tunnelNode("brand-new", "brand-new.example.com", "brand-new-pw")
	newIdx := shardIndexForKey(newNode.NodeKey(), n)

	if err := sb.Reload(append(append([]ParsedNode(nil), setA...), newNode)); err != nil {
		t.Fatalf("第二次 Reload 出错: %v", err)
	}

	for i, s := range spies {
		want := before[i]
		if i == newIdx {
			want = before[i] + 1
		}
		if s.calls() != want {
			t.Fatalf("分片 %d Reload 次数=%d，期望 %d（newIdx=%d）", i, s.calls(), want, newIdx)
		}
	}
}

// TestReloadNodeRemovalReloadsOnlyAffectedShard 验证移除节点只重载被影响分片。
func TestReloadNodeRemovalReloadsOnlyAffectedShard(t *testing.T) {
	const n = 4
	sb, spies := newSpyOrchestrator(10000, n)
	pair := nodesOnDistinctShards(t, n, 2)
	nodeX, nodeY := pair[0], pair[1]
	idxX := shardIndexForKey(nodeX.NodeKey(), n)

	if err := sb.Reload([]ParsedNode{nodeX, nodeY}); err != nil {
		t.Fatalf("首次 Reload 出错: %v", err)
	}
	before := make([]int, len(spies))
	for i, s := range spies {
		before[i] = s.calls()
	}

	if err := sb.Reload([]ParsedNode{nodeY}); err != nil {
		t.Fatalf("移除后 Reload 出错: %v", err)
	}
	for i, s := range spies {
		want := before[i]
		if i == idxX {
			want = before[i] + 1
		}
		if s.calls() != want {
			t.Fatalf("分片 %d Reload 次数=%d，期望 %d（受影响分片 idxX=%d）", i, s.calls(), want, idxX)
		}
	}
}

// TestReloadDoesNotCommitAssignedKeysOnIncompletePortMap 覆盖段满/跳过节点假成功：
// shard.Reload 返回 nil 但 portMap 缺少目标 key 时，不得提交 assignedKeys，且整体 Reload 必须报错。
func TestReloadDoesNotCommitAssignedKeysOnIncompletePortMap(t *testing.T) {
	const n = 4
	sb, spies := newSpyOrchestrator(10000, n)
	node := tunnelNode("seg-full", "seg-full.example.com", "pw")
	idx := shardIndexForKey(node.NodeKey(), n)
	spies[idx].incompletePorts = true

	err := sb.Reload([]ParsedNode{node})
	if err == nil {
		t.Fatal("portMap 缺少目标 key 时 Reload 应返回 error，得到 nil")
	}
	if len(sb.assignedKeys[idx]) != 0 {
		t.Fatalf("incomplete 时不得提交 assignedKeys，得到 %v", sb.assignedKeys[idx])
	}
}

// TestReloadDoesNotCommitAssignedKeysOnPartialStatus 覆盖 Partial 假成功：
// ReadyPorts < TotalPorts / Status=partial 时不得提交 assignedKeys。
func TestReloadDoesNotCommitAssignedKeysOnPartialStatus(t *testing.T) {
	const n = 4
	sb, spies := newSpyOrchestrator(10000, n)
	node := tunnelNode("partial-node", "partial.example.com", "pw")
	idx := shardIndexForKey(node.NodeKey(), n)
	spies[idx].forcePartial = true

	err := sb.Reload([]ParsedNode{node})
	if err == nil {
		t.Fatal("Partial 状态时 Reload 应返回 error，得到 nil")
	}
	if len(sb.assignedKeys[idx]) != 0 {
		t.Fatalf("Partial 时不得提交 assignedKeys，得到 %v", sb.assignedKeys[idx])
	}
}

// TestReloadCommitsAssignedKeysOnlyWhenFullyReady 对照：全就绪且 portMap 完整时仍应提交。
func TestReloadCommitsAssignedKeysOnlyWhenFullyReady(t *testing.T) {
	const n = 4
	sb, _ := newSpyOrchestrator(10000, n)
	node := tunnelNode("ready-node", "ready.example.com", "pw")
	idx := shardIndexForKey(node.NodeKey(), n)
	key := node.NodeKey()

	if err := sb.Reload([]ParsedNode{node}); err != nil {
		t.Fatalf("全就绪 Reload 应成功: %v", err)
	}
	if !sb.assignedKeys[idx][key] {
		t.Fatalf("全就绪时应提交 assignedKeys，得到 %v", sb.assignedKeys[idx])
	}
}

// TestReloadFaultIsolation 验证单分片失败不影响其他分片，且失败分片下次被重试。
func TestReloadFaultIsolation(t *testing.T) {
	const n = 4
	sb, spies := newSpyOrchestrator(10000, n)
	pair := nodesOnDistinctShards(t, n, 2)
	nodeFail, nodeOK := pair[0], pair[1]
	failIdx := shardIndexForKey(nodeFail.NodeKey(), n)
	okIdx := shardIndexForKey(nodeOK.NodeKey(), n)

	spies[failIdx].reloadErr = errors.New("boom")

	err := sb.Reload([]ParsedNode{nodeFail, nodeOK})
	if err == nil {
		t.Fatalf("期望聚合错误，得到 nil")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("shard %d", failIdx)) {
		t.Fatalf("聚合错误应提及 shard %d，实际: %v", failIdx, err)
	}
	if spies[failIdx].calls() < 1 {
		t.Fatalf("失败分片首次应被 Reload，得到 %d", spies[failIdx].calls())
	}
	// Successful shard is rolled back after sibling failure (empty old snapshot → Stop, not extra Reload).
	if spies[okIdx].calls() < 1 {
		t.Fatalf("成功分片首次应被 Reload，得到 %d", spies[okIdx].calls())
	}
	// After overall failure, assignedKeys stay at pre-reload snapshot (empty here).
	if len(sb.assignedKeys[okIdx]) != 0 {
		t.Fatalf("失败回滚后 ok 分片 assignedKeys 应为空，得到 %v", sb.assignedKeys[okIdx])
	}

	beforeFail := spies[failIdx].calls()
	beforeOK := spies[okIdx].calls()
	err = sb.Reload([]ParsedNode{nodeFail, nodeOK})
	if err == nil {
		t.Fatalf("第二次重载失败分片仍应报错")
	}
	if spies[failIdx].calls() <= beforeFail {
		t.Fatalf("失败分片应被重试，calls %d -> %d", beforeFail, spies[failIdx].calls())
	}
	// ok shard must be attempted again because previous attempt was rolled back.
	if spies[okIdx].calls() <= beforeOK {
		t.Fatalf("回滚后成功分片应再次尝试 Reload，calls %d -> %d", beforeOK, spies[okIdx].calls())
	}
}

// TestAggregatePortMaps 验证端口映射、节点计数与节点列表的聚合正确性。
func TestAggregatePortMaps(t *testing.T) {
	const n = 3
	sb, spies := newSpyOrchestrator(10000, n)

	spies[0].portMap = map[string]int{"a": 1}
	spies[0].nodes = []ParsedNode{tunnelNode("a", "a.example.com", "pa")}

	spies[1].portMap = map[string]int{"b": 2, "c": 3}
	spies[1].nodes = []ParsedNode{tunnelNode("b", "b.example.com", "pb"), tunnelNode("c", "c.example.com", "pc")}

	spies[2].portMap = map[string]int{"d": 4}
	spies[2].nodes = []ParsedNode{tunnelNode("d", "d.example.com", "pd")}

	gotPort := sb.GetPortMap()
	wantPort := map[string]int{"a": 1, "b": 2, "c": 3, "d": 4}
	if !equalIntMap(gotPort, wantPort) {
		t.Fatalf("GetPortMap=%v，期望 %v", gotPort, wantPort)
	}

	if got := sb.GetNodeCount(); got != 4 {
		t.Fatalf("GetNodeCount=%d，期望 4", got)
	}
	if got := len(sb.GetNodes()); got != 4 {
		t.Fatalf("GetNodes 长度=%d，期望 4", got)
	}
}

func equalIntMap(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestGetLocalAddressRoutesToCorrectShard 验证按 key 路由到正确分片取地址。
func TestGetLocalAddressRoutesToCorrectShard(t *testing.T) {
	const n = 4
	sb, spies := newSpyOrchestrator(10000, n)

	node := tunnelNode("addr", "addr.example.com", "addr-pw")
	key := node.NodeKey()
	idx := shardIndexForKey(key, n)
	want := "127.0.0.1:34567"
	spies[idx].localAddrs[key] = want

	if got := sb.GetLocalAddress(key); got != want {
		t.Fatalf("GetLocalAddress=%q，期望 %q（应路由到分片 %d）", got, want, idx)
	}
}

// TestReloadNoTunnelNodesStopsAllShards 验证仅含 direct 节点时停止所有分片并报告 NoTunnelNodes。
func TestReloadNoTunnelNodesStopsAllShards(t *testing.T) {
	const n = 4
	sb, spies := newSpyOrchestrator(10000, n)

	directs := []ParsedNode{directNodeForShard("d1"), directNodeForShard("d2")}
	if err := sb.Reload(directs); err != nil {
		t.Fatalf("Reload 出错: %v", err)
	}
	for i, s := range spies {
		if s.stops() < 1 {
			t.Fatalf("分片 %d 未收到 Stop", i)
		}
	}
	rs := sb.GetRuntimeStatus()
	if rs.Status != SingBoxStatusNoTunnelNodes {
		t.Fatalf("Status=%q，期望 %q", rs.Status, SingBoxStatusNoTunnelNodes)
	}
	if rs.Running {
		t.Fatalf("Running=true，期望 false")
	}
}

// TestRuntimeStatusRollup 验证运行态汇总的三种情形与数值求和。
func TestRuntimeStatusRollup(t *testing.T) {
	// 情形 1：所有活跃分片均 running → 聚合 Running。
	sb, spies := newSpyOrchestrator(10000, 3)
	spies[0].status = SingBoxRuntimeStatus{Running: true, Status: SingBoxStatusRunning, Nodes: 2, ReadyPorts: 2, TotalPorts: 2}
	spies[1].status = SingBoxRuntimeStatus{Running: true, Status: SingBoxStatusRunning, Nodes: 1, ReadyPorts: 1, TotalPorts: 1}
	rs := sb.GetRuntimeStatus()
	if !rs.Running || rs.Status != SingBoxStatusRunning {
		t.Fatalf("情形1: 期望 Running/running，得到 Running=%v Status=%q", rs.Running, rs.Status)
	}
	if rs.Nodes != 3 || rs.ReadyPorts != 3 || rs.TotalPorts != 3 {
		t.Fatalf("情形1: 数值求和错误 Nodes=%d ReadyPorts=%d TotalPorts=%d，期望 3/3/3",
			rs.Nodes, rs.ReadyPorts, rs.TotalPorts)
	}

	// 情形 2：活跃分片中一个失败 → Partial。
	sb, spies = newSpyOrchestrator(10000, 3)
	spies[0].status = SingBoxRuntimeStatus{Running: true, Status: SingBoxStatusRunning, Nodes: 2, ReadyPorts: 2, TotalPorts: 2}
	spies[1].status = SingBoxRuntimeStatus{Running: false, Status: SingBoxStatusFailed, Nodes: 1, ReadyPorts: 0, TotalPorts: 1}
	rs = sb.GetRuntimeStatus()
	if rs.Status != SingBoxStatusPartial || !rs.Running {
		t.Fatalf("情形2: 期望 Partial/Running，得到 Status=%q Running=%v", rs.Status, rs.Running)
	}
	if rs.Nodes != 3 || rs.ReadyPorts != 2 || rs.TotalPorts != 3 {
		t.Fatalf("情形2: 数值求和错误 Nodes=%d ReadyPorts=%d TotalPorts=%d，期望 3/2/3",
			rs.Nodes, rs.ReadyPorts, rs.TotalPorts)
	}

	// 情形 3：所有活跃分片均失败 → Failed。
	sb, spies = newSpyOrchestrator(10000, 3)
	spies[0].status = SingBoxRuntimeStatus{Running: false, Status: SingBoxStatusFailed, Nodes: 2, ReadyPorts: 0, TotalPorts: 2}
	spies[1].status = SingBoxRuntimeStatus{Running: false, Status: SingBoxStatusFailed, Nodes: 1, ReadyPorts: 0, TotalPorts: 1}
	rs = sb.GetRuntimeStatus()
	if rs.Status != SingBoxStatusFailed || rs.Running {
		t.Fatalf("情形3: 期望 Failed/非Running，得到 Status=%q Running=%v", rs.Status, rs.Running)
	}
	if rs.Nodes != 3 || rs.ReadyPorts != 0 || rs.TotalPorts != 3 {
		t.Fatalf("情形3: 数值求和错误 Nodes=%d ReadyPorts=%d TotalPorts=%d，期望 3/0/3",
			rs.Nodes, rs.ReadyPorts, rs.TotalPorts)
	}

	// 情形 4：一个 running 分片与一个零节点 failed 分片 → Partial。
	sb, spies = newSpyOrchestrator(10000, 3)
	spies[0].status = SingBoxRuntimeStatus{Running: true, Status: SingBoxStatusRunning, Nodes: 1, ReadyPorts: 1, TotalPorts: 1}
	spies[1].status = SingBoxRuntimeStatus{Running: false, Status: SingBoxStatusFailed, Reason: "data_dir_invalid"}
	rs = sb.GetRuntimeStatus()
	if rs.Status != SingBoxStatusPartial || !rs.Running {
		t.Fatalf("情形4: 期望 Partial/Running，得到 Status=%q Reason=%q Running=%v", rs.Status, rs.Reason, rs.Running)
	}

	// 情形 5：所有分片均为零节点 failed，且原因一致 → 透传具体原因。
	sb, spies = newSpyOrchestrator(10000, 3)
	spies[0].status = SingBoxRuntimeStatus{Running: false, Status: SingBoxStatusFailed, Reason: "data_dir_invalid"}
	spies[1].status = SingBoxRuntimeStatus{Running: false, Status: SingBoxStatusFailed, Reason: "data_dir_invalid"}
	rs = sb.GetRuntimeStatus()
	if rs.Status != SingBoxStatusFailed || rs.Reason != "data_dir_invalid" || rs.Running {
		t.Fatalf("情形5: 期望 Failed/data_dir_invalid/非Running，得到 Status=%q Reason=%q Running=%v", rs.Status, rs.Reason, rs.Running)
	}

	// 情形 6：存在非 failed 的被考虑分片时，不得把另一分片的具体失败原因伪装为全局原因。
	sb, spies = newSpyOrchestrator(10000, 2)
	spies[0].status = SingBoxRuntimeStatus{Running: false, Status: SingBoxStatusStopped, Reason: SingBoxStatusStopped, Nodes: 1, TotalPorts: 1}
	spies[1].status = SingBoxRuntimeStatus{Running: false, Status: SingBoxStatusFailed, Reason: "data_dir_invalid"}
	rs = sb.GetRuntimeStatus()
	if rs.Status != SingBoxStatusFailed || rs.Reason != "all_shards_failed" || rs.Running {
		t.Fatalf("情形6: 期望 Failed/all_shards_failed/非Running，得到 Status=%q Reason=%q Running=%v", rs.Status, rs.Reason, rs.Running)
	}
}
