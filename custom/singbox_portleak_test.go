package custom

import (
	"fmt"
	"strings"
	"testing"
)

// TestAssembleConfigReusesFreedLowPort 验证缺陷4（端口泄漏）：
// 当某节点被移除后，其占用的低位端口应被释放并可被新节点复用，
// 而不是让端口分配单调向上爬升、永不回收，最终耗尽端口段。
//
// 场景：第一次仅加载 A（拿到低位端口 pA）；随后 A 下线、B 上线。
// B 作为新节点应复用被释放的 pA，而不是拿到 pA+1。
func TestAssembleConfigReusesFreedLowPort(t *testing.T) {
	s := NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort)
	nodeA := tunnelNode("a", "a.example.com", "pw-a")

	_, m1 := s.assembleConfig([]ParsedNode{nodeA})
	pA := m1[nodeA.NodeKey()]
	if pA == 0 {
		t.Fatalf("节点 A 未分配端口: m1=%v", m1)
	}

	// 预置上一轮运行态：仅 A 占用端口。
	s.portMap = m1

	// A 下线、B 上线。B 是新节点，应复用被释放的低位端口 pA。
	nodeB := tunnelNode("b", "b.example.com", "pw-b")
	_, m2 := s.assembleConfig([]ParsedNode{nodeB})
	pB := m2[nodeB.NodeKey()]
	if pB == 0 {
		t.Fatalf("节点 B 未分配端口: m2=%v", m2)
	}
	if pB != pA {
		t.Fatalf("端口泄漏：B 应复用被释放的低位端口 %d，实际拿到 %d（端口只增不减）", pA, pB)
	}
}

// TestAssembleConfigReusesMiddlePortHole 验证 allocPort 从 basePort 扫描空洞：
// 中间节点释放后留下的空洞必须被新节点复用，而不是继续从高水位 maxPort 向上爬。
//
// 场景：A、B、C 依次占用三端口；B 下线留下中间空洞；D 上线应拿到 B 的端口。
// 且所有端口仍落在本分片段 [basePort, basePort+portRangeSpan) 内。
func TestAssembleConfigReusesMiddlePortHole(t *testing.T) {
	s := NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort)
	nodeA := tunnelNode("a", "a.example.com", "pw-a")
	nodeB := tunnelNode("b", "b.example.com", "pw-b")
	nodeC := tunnelNode("c", "c.example.com", "pw-c")

	_, m1 := s.assembleConfig([]ParsedNode{nodeA, nodeB, nodeC})
	pA := m1[nodeA.NodeKey()]
	pB := m1[nodeB.NodeKey()]
	pC := m1[nodeC.NodeKey()]
	if pA == 0 || pB == 0 || pC == 0 {
		t.Fatalf("初始分配失败: m1=%v", m1)
	}
	if pA == pB || pA == pC || pB == pC {
		t.Fatalf("初始端口应互不相同: A=%d B=%d C=%d", pA, pB, pC)
	}
	// 中间空洞场景要求 B 的端口严格夹在 A 与 C 之间（按数值）。
	lo, mid, hi := pA, pB, pC
	ports := []int{pA, pB, pC}
	for i := 0; i < 3; i++ {
		for j := i + 1; j < 3; j++ {
			if ports[i] > ports[j] {
				ports[i], ports[j] = ports[j], ports[i]
			}
		}
	}
	lo, mid, hi = ports[0], ports[1], ports[2]
	if mid != pB {
		// 按 NodeKey 字典序分配时 B 应居中；若实现改序则仍以数值中间空洞为准。
		// 下面释放的是占用 mid 的节点，再断言新节点复用 mid。
	}
	// 找出占用 mid 的节点并释放它。
	var keep []ParsedNode
	var freedPort int
	for _, n := range []ParsedNode{nodeA, nodeB, nodeC} {
		if m1[n.NodeKey()] == mid {
			freedPort = mid
			continue
		}
		keep = append(keep, n)
	}
	if freedPort == 0 || len(keep) != 2 {
		t.Fatalf("未能构造中间空洞: m1=%v lo=%d mid=%d hi=%d", m1, lo, mid, hi)
	}

	s.portMap = m1
	nodeD := tunnelNode("d", "d.example.com", "pw-d")
	_, m2 := s.assembleConfig(append(keep, nodeD))

	pD := m2[nodeD.NodeKey()]
	if pD == 0 {
		t.Fatalf("节点 D 未分配端口: m2=%v", m2)
	}
	if pD != freedPort {
		t.Fatalf("中间空洞未复用：D 应得释放端口 %d，实际 %d（仍从高水位爬升）", freedPort, pD)
	}
	// 已有节点端口稳定。
	for _, n := range keep {
		if got := m2[n.NodeKey()]; got != m1[n.NodeKey()] {
			t.Fatalf("已有节点 %s 端口从 %d 变为 %d", n.Name, m1[n.NodeKey()], got)
		}
	}
	segLo := testSingBoxBasePort
	segHi := testSingBoxBasePort + portRangeSpan
	for key, p := range m2 {
		if p < segLo || p >= segHi {
			t.Fatalf("端口 %d (节点 %s) 越出分片段 [%d, %d)", p, key, segLo, segHi)
		}
	}
}

// fillSegmentPortMap 将本分片段 [basePort+1, basePort+portRangeSpan) 全部占满，
// 返回保留节点列表（供段满/溢出用例；空洞扫描语义下“仅占顶端”并不算段满）。
func fillSegmentPortMap(s *SingBoxProcess) []ParsedNode {
	lo := s.basePort + 1
	hi := s.basePort + portRangeSpan
	nodes := make([]ParsedNode, 0, hi-lo)
	s.portMap = make(map[string]int, hi-lo)
	for p := lo; p < hi; p++ {
		n := tunnelNode(fmt.Sprintf("fill-%d", p), fmt.Sprintf("fill-%d.example.com", p), "pw")
		nodes = append(nodes, n)
		s.portMap[n.NodeKey()] = p
	}
	return nodes
}

// TestAssembleConfigStaysWithinSegmentAndSkipsOnOverflow 验证分片端口段溢出保护：
// 每个分片实例独占端口段 [basePort, basePort+portRangeSpan)。
// 当端口段被占满时，新节点必须被跳过（同 buildOutbound 失败的处理方式：
// 不进入 portMap、不生成 inbound/outbound/rule），而不能越界爬进下一个分片的段。
// 在空洞扫描语义下，段满 = 段内每个端口均被仍保留的节点占用（而非仅高水位触顶）。
func TestAssembleConfigStaysWithinSegmentAndSkipsOnOverflow(t *testing.T) {
	s := NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort)
	filled := fillSegmentPortMap(s)

	// 段已满 + 一个不同的新节点，新节点应被跳过。
	newNode := tunnelNode("new", "new.example.com", "pw-new")
	_, m := s.assembleConfig(append(append([]ParsedNode(nil), filled...), newNode))

	// 段不变式：所有端口必须落在本分片段内，绝不越界到下一段。
	lo := testSingBoxBasePort
	hi := testSingBoxBasePort + portRangeSpan
	for key, p := range m {
		if p < lo || p >= hi {
			t.Fatalf("端口 %d (节点 %s) 越出分片段 [%d, %d)", p, key, lo, hi)
		}
	}

	// 已占满节点应保留原端口（assembleConfig 无副作用，s.portMap 仍是预置）。
	for _, n := range filled {
		want := s.portMap[n.NodeKey()]
		got, ok := m[n.NodeKey()]
		if !ok || got != want {
			t.Fatalf("保留节点 %s 端口: ok=%v got=%d want=%d", n.Name, ok, got, want)
		}
	}
	if len(m) != len(filled) {
		t.Fatalf("段满时 portMap 大小=%d，期望仅保留 %d 个已占满节点", len(m), len(filled))
	}

	// 段已满，新节点必须被跳过（不出现在 portMap 中）。
	if p, ok := m[newNode.NodeKey()]; ok {
		t.Fatalf("段已满时新节点未被跳过，仍被分配端口 %d", p)
	}
}

// TestReloadSegmentFullReturnsIncompleteError 覆盖段满跳过假成功：
// 端口段已满导致新节点不进 portMap 时，Reload 必须返回 error（在启动进程前即可判定），
// 不得 return nil 让上层 RefreshSubscription 删旧代理。
func TestReloadSegmentFullReturnsIncompleteError(t *testing.T) {
	s := NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort)
	filled := fillSegmentPortMap(s)
	s.nodes = append([]ParsedNode(nil), filled...)

	newNode := tunnelNode("new", "new.example.com", "pw-new")
	err := s.Reload(append(append([]ParsedNode(nil), filled...), newNode))
	if err == nil {
		t.Fatal("段满跳过节点时 Reload 应返回 incomplete error，得到 nil")
	}
	// 必须是端口不完整类错误，而非仅 binary_not_found（否则段满在缺二进制时被掩盖，
	// 有二进制时仍会假成功）。
	msg := err.Error()
	if !strings.Contains(msg, "端口段已满") || !strings.Contains(msg, "new") || !strings.Contains(msg, "trojan") {
		t.Fatalf("段满应在启动前明确报告端口段满及节点，实际: %v", err)
	}
	// 失败后不得把未分配节点伪装进 portMap（restore 后可能仍是旧映射）。
	if _, ok := s.GetPortMap()[newNode.NodeKey()]; ok {
		t.Fatal("失败路径不应保留未分配新节点的端口映射")
	}
}

// TestAssembleConfigExistingNodesKeepStablePorts 回归保护：
// 跨两次重载，已有节点端口必须保持稳定（新增节点不得改动既有节点端口），
// 否则会破坏已入库的代理地址。此用例在修复前后都应为 GREEN（纯回归护栏）。
func TestAssembleConfigExistingNodesKeepStablePorts(t *testing.T) {
	s := NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort)
	nodeA := tunnelNode("a", "a.example.com", "pw-a")

	_, m1 := s.assembleConfig([]ParsedNode{nodeA})
	pA := m1[nodeA.NodeKey()]
	if pA == 0 {
		t.Fatalf("节点 A 未分配端口: m1=%v", m1)
	}

	s.portMap = m1
	nodeB := tunnelNode("b", "b.example.com", "pw-b")
	_, m2 := s.assembleConfig([]ParsedNode{nodeA, nodeB})

	if got := m2[nodeA.NodeKey()]; got != pA {
		t.Fatalf("已有节点 A 端口从 %d 变为 %d，破坏稳定性", pA, got)
	}
	pB := m2[nodeB.NodeKey()]
	if pB == 0 {
		t.Fatal("新节点 B 未分配端口")
	}
	if pB == pA {
		t.Fatalf("A 与 B 分配了相同端口 %d", pA)
	}
}
