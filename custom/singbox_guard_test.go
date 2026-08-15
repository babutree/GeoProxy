package custom

import (
	"strings"
	"testing"
)

// TestConvertPluginOptsIsDeterministic 锁定 plugin_opts 的确定性输出。
// Go map 迭代顺序随机；不排序会让同一节点每次生成不同的 plugin_opts 串，
// 生成的 sing-box 配置不可复现，排障时 diff 全是噪音。
//
// 注意：NodeKey 由 json.Marshal(Raw) 计算（encoding/json 对 map key 排序），
// 不经过 convertPluginOpts，所以本函数不影响节点身份与端口稳定性——
// 这也是本测试与 TestNodeKeyStableAcrossRebuildsWithMapPluginOpts 分工的原因。
func TestConvertPluginOptsIsDeterministic(t *testing.T) {
	opts := map[string]interface{}{
		"mode": "tls", "host": "x.example", "path": "/p", "zzz": "1", "aaa": "2",
	}
	first := convertPluginOpts("obfs", opts)
	for i := 0; i < 200; i++ {
		if got := convertPluginOpts("obfs", opts); got != first {
			t.Fatalf("convertPluginOpts unstable at iteration %d: %q vs %q", i, got, first)
		}
	}
	// 排序后的期望串：key 升序。
	const want = "aaa=2;host=x.example;mode=tls;path=/p;zzz=1"
	if first != want {
		t.Fatalf("convertPluginOpts = %q, want key-sorted %q", first, want)
	}
	if !strings.Contains(first, ";") {
		t.Fatalf("convertPluginOpts lost the ; separator: %q", first)
	}
}

// TestNodeKeyStableAcrossRebuildsWithMapPluginOpts 证明节点身份不受 map 顺序影响。
// 这是端口稳定性（keySetsEqual 按 NodeKey 集合判定是否需要重载分片）的前提。
func TestNodeKeyStableAcrossRebuildsWithMapPluginOpts(t *testing.T) {
	build := func() ParsedNode {
		return ParsedNode{Type: "ss", Server: "a.example", Port: 443, Raw: map[string]interface{}{
			"plugin": "obfs",
			"plugin-opts": map[string]interface{}{
				"mode": "tls", "host": "x.example", "path": "/p", "zzz": "1", "aaa": "2",
			},
		}}
	}
	base := build()
	first := base.NodeKey()
	for i := 0; i < 200; i++ {
		node := build()
		if got := node.NodeKey(); got != first {
			t.Fatalf("NodeKey unstable at iteration %d: %q vs %q (port allocation would churn)", i, got, first)
		}
	}
}
