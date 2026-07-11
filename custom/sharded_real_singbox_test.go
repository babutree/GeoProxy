package custom

import (
	"fmt"
	"os/exec"
	"strconv"
	"testing"
)

// 本文件是路 C 的真实 sing-box 集成验证：用真实 sing-box 二进制启动分片进程，
// 验证 (1) 大规模节点(6000+)不崩溃、(2) 平滑重载——新增/移除节点只重启受影响分片
// (通过对比各分片 OS 进程 PID 直接证实，而非仅看内部状态)。
//
// 无 sing-box 二进制时显式 Skip，绝不伪造成功。

// realSingBoxBin 返回可用的 sing-box 路径；找不到则 Skip。
func realSingBoxBin(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("sing-box")
	if err != nil {
		t.Skip("跳过真实 sing-box 集成测试：PATH 无 sing-box 二进制")
	}
	return bin
}

// genTunnelNodes 生成 n 个结构合法的 trojan tunnel 节点（服务器为假，仅用于让
// sing-box 通过 check 并绑定本地 mixed 入站端口；不需要真实上游可连）。
func genTunnelNodes(n int) []ParsedNode {
	nodes := make([]ParsedNode, n)
	for i := 0; i < n; i++ {
		nodes[i] = tunnelNode(
			fmt.Sprintf("node-%d", i),
			fmt.Sprintf("n%d.example.com", i),
			"pw-"+strconv.Itoa(i),
		)
	}
	return nodes
}

// shardProcessPID 读取某分片底层 SingBoxProcess 的 OS 进程 PID（0=未运行）。
// 同包测试可访问私有字段；持锁读取避免与监控 goroutine 竞争。
func shardProcessPID(shard singBoxShard) int {
	sp, ok := shard.(*SingBoxProcess)
	if !ok {
		return -1
	}
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.cmd == nil || sp.cmd.Process == nil {
		return 0
	}
	return sp.cmd.Process.Pid
}

// TestRealSingBoxSmoke 小规模真实 sing-box 冒烟：确认「配置生成→check→run→绑定端口→PID」
// 整条机制在真实二进制下可用，再由 6000 节点测试放大规模。
func TestRealSingBoxSmoke(t *testing.T) {
	bin := realSingBoxBin(t)

	sb := NewShardedSingBox(bin, t.TempDir(), 21000, 4)
	t.Cleanup(sb.Stop)

	nodes := genTunnelNodes(20)
	if err := sb.Reload(nodes); err != nil {
		t.Fatalf("Reload(20 真实节点) 出错: %v", err)
	}

	pm := sb.GetPortMap()
	if len(pm) != 20 {
		t.Fatalf("portMap 大小=%d, 期望 20", len(pm))
	}
	rs := sb.GetRuntimeStatus()
	t.Logf("smoke 运行态: Status=%s Running=%v Nodes=%d Ready=%d/%d",
		rs.Status, rs.Running, rs.Nodes, rs.ReadyPorts, rs.TotalPorts)
	if rs.Nodes != 20 {
		t.Fatalf("运行态 Nodes=%d, 期望 20", rs.Nodes)
	}
	// 至少要有分片进程真正起来（Running），否则说明真实 sing-box 没跑起来。
	if !rs.Running {
		t.Fatalf("smoke: 无任何分片处于 Running，真实 sing-box 未成功启动: %+v", rs)
	}
}

// allShardPIDs 快照所有分片当前的 OS 进程 PID，按分片序号排列。
func allShardPIDs(sb *ShardedSingBox) []int {
	pids := make([]int, len(sb.shards))
	for i, shard := range sb.shards {
		pids[i] = shardProcessPID(shard)
	}
	return pids
}

// TestRealSingBoxSmoothReloadOnlyRestartsChangedShard 是路 C 平滑重载的核心证明：
// 用真实 sing-box 启动分片后，新增一个节点触发 Reload，通过对比重载前后各分片的
// 真实 OS 进程 PID，直接证实——只有该节点所属分片的进程被重启（PID 变化），
// 其余分片进程 PID 保持不变（进程未动 = 其已建立连接不被打断 = 平滑）。
//
// 这是"仅看内部状态"无法证明的：PID 不变才是"进程真的没重启"的硬证据。
func TestRealSingBoxSmoothReloadOnlyRestartsChangedShard(t *testing.T) {
	bin := realSingBoxBin(t)

	const n = 4
	sb := NewShardedSingBox(bin, t.TempDir(), 22000, n)
	t.Cleanup(sb.Stop)

	// 初始集：4 个映射到不同分片的节点，确保每个分片都有进程起来。
	base := nodesOnDistinctShards(t, n, n)
	if err := sb.Reload(base); err != nil {
		t.Fatalf("初始 Reload 出错: %v", err)
	}
	before := allShardPIDs(sb)
	t.Logf("重载前各分片 PID: %v", before)
	for i, pid := range before {
		if pid <= 0 {
			t.Fatalf("分片 %d 初始未运行 (PID=%d)，无法验证平滑重载", i, pid)
		}
	}

	// 新增一个节点，确定其目标分片。
	newNode := tunnelNode("added", "added.example.com", "pw-added")
	changedIdx := shardIndexForKey(newNode.NodeKey(), n)
	if err := sb.Reload(append(append([]ParsedNode(nil), base...), newNode)); err != nil {
		t.Fatalf("新增节点 Reload 出错: %v", err)
	}
	after := allShardPIDs(sb)
	t.Logf("重载后各分片 PID: %v (变化分片应为 %d)", after, changedIdx)

	for i := range before {
		if i == changedIdx {
			// 受影响分片：进程必须被重启（PID 变化且仍在运行）。
			if after[i] == before[i] {
				t.Fatalf("变化分片 %d 的 PID 未变 (%d)，说明新节点未生效", i, after[i])
			}
			if after[i] <= 0 {
				t.Fatalf("变化分片 %d 重载后未运行 (PID=%d)", i, after[i])
			}
		} else {
			// 未变化分片：进程 PID 必须保持不变（平滑核心——连接不被打断）。
			if after[i] != before[i] {
				t.Fatalf("未变化分片 %d 的进程被重启 (PID %d→%d)，破坏平滑性", i, before[i], after[i])
			}
		}
	}
}

// TestRealSingBox6000NodesNoCrash 是路 C 规模验证：用真实 sing-box 加载 6000+ tunnel 节点
// （分 N 片，每片约 6000/N 个 mixed 入站），确认不崩溃、端口全部入 portMap、分片进程起得来。
//
// 这直接证伪旧单进程"6000 节点崩溃"：分片后每进程只扛 ~1500 节点。
// 该测试较重（真实启动多个 sing-box、绑定数千端口），默认在 -short 下跳过。
func TestRealSingBox6000NodesNoCrash(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过 6000 节点重量级真实 sing-box 测试（-short）")
	}
	bin := realSingBoxBin(t)

	const total = 6000
	const n = 4
	// 端口段需足够容纳每分片 ~1500 节点：portRangeSpan=5000 足够。
	sb := NewShardedSingBox(bin, t.TempDir(), 30000, n)
	t.Cleanup(sb.Stop)

	nodes := genTunnelNodes(total)
	if err := sb.Reload(nodes); err != nil {
		t.Fatalf("Reload(%d 真实节点) 出错: %v", total, err)
	}

	pm := sb.GetPortMap()
	if len(pm) != total {
		t.Fatalf("portMap 大小=%d, 期望 %d（有节点未分配端口=段溢出或崩溃）", len(pm), total)
	}
	rs := sb.GetRuntimeStatus()
	t.Logf("6000 节点运行态: Status=%s Running=%v Nodes=%d Ready=%d/%d",
		rs.Status, rs.Running, rs.Nodes, rs.ReadyPorts, rs.TotalPorts)
	if rs.Nodes != total {
		t.Fatalf("运行态 Nodes=%d, 期望 %d", rs.Nodes, total)
	}
	if !rs.Running {
		t.Fatalf("6000 节点：无任何分片 Running，疑似崩溃: %+v", rs)
	}
	// 每个分片进程都应真正起来（PID>0），证明 6000 节点被成功分摊而非某片崩溃。
	for i, pid := range allShardPIDs(sb) {
		if pid <= 0 {
			t.Fatalf("分片 %d 未运行 (PID=%d)，6000 节点未被成功分摊", i, pid)
		}
	}
}
