package custom

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestProbeDisabledUsesAtomicRecoveryOperation 约束探测恢复的调用边界：
// probeDisabled 必须调用 storage 的原子恢复入口，且不得先单独启用节点。
// 元数据失败后的状态回滚由 storage/probe_recovery_test.go 行为测试覆盖。
func TestProbeDisabledUsesAtomicRecoveryOperation(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "manager.go", nil, 0)
	if err != nil {
		t.Fatalf("parse manager.go: %v", err)
	}

	var probe *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == "probeDisabled" {
			probe = fn
			break
		}
	}
	if probe == nil {
		t.Fatal("probeDisabled declaration not found")
	}

	calls := map[string]int{}
	ast.Inspect(probe.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok {
			calls[selector.Sel.Name]++
		}
		return true
	})

	if calls["RecoverSubscriptionProxyWithExitInfo"] != 1 {
		t.Fatalf("atomic recovery calls = %d, want 1", calls["RecoverSubscriptionProxyWithExitInfo"])
	}
	if calls["EnableSubscriptionProxy"] != 0 {
		t.Fatalf("probeDisabled still calls EnableSubscriptionProxy %d time(s); recovery must be atomic", calls["EnableSubscriptionProxy"])
	}
}
