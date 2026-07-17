package custom

import (
	"fmt"
	"strings"
	"testing"
)

// TestPrunedNodesAreMarkedRejectedSoCommitDoesNotRequireTheirPorts
// 回归：真实订阅日志中「校验剔除 1 个节点」后仍报「端口分配不完整 1/1 未分配端口」
// 并拖垮整订阅。根因是 prune 丢弃节点却未写入 assembly.rejected。
func TestPrunedNodesAreMarkedRejectedSoCommitDoesNotRequireTheirPorts(t *testing.T) {
	good := tunnelNode("good", "good.example.com", "good-password")
	bad := tunnelNode("bad-check", "bad.example.com", "bad-password")
	buildFail := tunnelNode("build-fail", "build-fail.example.com", "bf-password")
	before := []ParsedNode{good, bad, buildFail}
	after := []ParsedNode{good}

	pruned := prunedAsRejected([]ParsedNode{good, bad}, after)
	if len(pruned) != 1 {
		t.Fatalf("pruned count = %d, want 1", len(pruned))
	}
	if pruned[0].node.NodeKey() != bad.NodeKey() {
		t.Fatalf("pruned key = %s, want %s", pruned[0].node.NodeKey(), bad.NodeKey())
	}
	if pruned[0].err == nil || !strings.Contains(pruned[0].err.Error(), "校验") {
		t.Fatalf("pruned err = %v, want 校验相关诊断", pruned[0].err)
	}

	// 模拟 prune 重建后必须同时保留：先前 build 失败 rejected + prune rejected。
	// 若只保留 pruned，buildFail 缺端口会被误判 incomplete。
	prevRejected := []assemblyRejectedNode{{node: buildFail, err: fmt.Errorf("构建失败")}}
	diagnostics := assemblyDiagnostics{
		accepted: []ParsedNode{good},
		rejected: append(append([]assemblyRejectedNode(nil), prevRejected...), pruned...),
	}
	portMap := map[string]int{good.NodeKey(): 20001}

	// 上层仍按完整 target=[good,bad] 提交时，必须跳过 rejected 的 bad，
	// 不得因 bad 缺端口而返回 incomplete。
	shard := &diagnosticSpyShard{
		spyShard:    newSpyShard(),
		diagnostics: diagnostics,
	}
	// 同步 spy 的 portMap，与真实 Reload 后 GetPortMap 一致。
	shard.portMap = portMap
	accepted, err := acceptedNodesForCommit(shard, before, portMap)
	if err != nil {
		t.Fatalf("acceptedNodesForCommit after prune-as-rejected must succeed, got: %v", err)
	}
	if len(accepted) != 1 || accepted[0].NodeKey() != good.NodeKey() {
		t.Fatalf("accepted = %v, want only good", keysOf(accepted))
	}

	// 分片 commit 路径同样必须跳过 rejected。
	// 构造一个 GetPortMap 仅含 good 的 spy，并注入 diagnostics。
	commitAccepted, commitErr := shardReloadCommitError(before, shard)
	if commitErr != nil {
		t.Fatalf("shardReloadCommitError after prune-as-rejected must succeed, got: %v", commitErr)
	}
	if len(commitAccepted) != 1 || commitAccepted[0].NodeKey() != good.NodeKey() {
		t.Fatalf("commit accepted = %v, want only good", keysOf(commitAccepted))
	}
}

// TestPrunedMissingWithoutRejectedStillFails 保留反例：若 prune 后未记 rejected，
// 缺端口仍必须严格失败（未知缺失），防止静默丢节点。
func TestPrunedMissingWithoutRejectedStillFails(t *testing.T) {
	good := tunnelNode("good2", "good2.example.com", "good-password")
	bad := tunnelNode("orphan", "orphan.example.com", "orphan-password")
	portMap := map[string]int{good.NodeKey(): 20002}
	// 无 rejected 诊断：bad 缺端口应被严格判 incomplete。
	shard := &diagnosticSpyShard{
		spyShard: newSpyShard(),
		diagnostics: assemblyDiagnostics{
			accepted: []ParsedNode{good},
		},
	}
	shard.portMap = portMap
	_, err := acceptedNodesForCommit(shard, []ParsedNode{good, bad}, portMap)
	if err == nil {
		t.Fatal("missing port without rejected evidence must fail")
	}
	if !strings.Contains(err.Error(), "未知") && !strings.Contains(err.Error(), "不完整") {
		t.Fatalf("want incomplete/unknown diagnostic, got: %v", err)
	}
}

func keysOf(nodes []ParsedNode) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.NodeKey())
	}
	return out
}
