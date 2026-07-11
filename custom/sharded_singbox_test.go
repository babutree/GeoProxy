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

	reloadErr  error
	status     SingBoxRuntimeStatus
	portMap    map[string]int
	localAddrs map[string]string
	nodes      []ParsedNode
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
	if spies[failIdx].calls() != 1 {
		t.Fatalf("失败分片首次应被 Reload 1 次，得到 %d", spies[failIdx].calls())
	}
	if spies[okIdx].calls() != 1 {
		t.Fatalf("成功分片首次应被 Reload 1 次，得到 %d", spies[okIdx].calls())
	}

	err = sb.Reload([]ParsedNode{nodeFail, nodeOK})
	if err == nil {
		t.Fatalf("第二次重载失败分片仍应报错")
	}
	if spies[failIdx].calls() != 2 {
		t.Fatalf("失败分片应被重试（Reload 2 次），得到 %d", spies[failIdx].calls())
	}
	if spies[okIdx].calls() != 1 {
		t.Fatalf("成功且未变化的分片不应被再次 Reload，得到 %d", spies[okIdx].calls())
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
}
