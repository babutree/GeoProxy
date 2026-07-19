package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/auth"
)

const (
	dashboardBehaviorMutationEnv = "GEOPROXY_DASHBOARD_BEHAVIOR_MUTATION"
	dashboardBootMarker          = "initFilterToggles();showSkeletons();markViewLazy('overview');"
	dashboardProtocolPredicate   = "function nodeSupportsInboundProtocol(p,protocol){const wanted=String(protocol||'').trim().toLowerCase();if(!wanted)return true;const actual=String((p&&p.protocol)||'').trim().toLowerCase();const dual=!!(p&&(p.dual_protocol===true||p.dual_protocol===1));if(dual)return wanted==='http'||wanted==='socks5';return actual===wanted}"
)

type dashboardHarnessResult struct {
	Scenario    string                       `json:"scenario"`
	Assertions  int                          `json:"assertions"`
	GatewayUser string                       `json:"gatewayUser"`
	NodeKey     string                       `json:"nodeKey"`
	WireVectors []dashboardNodeKeyWireVector `json:"wireVectors"`
}

type dashboardNodeKeyWireVector struct {
	NodeKey string `json:"nodeKey"`
	Token   string `json:"token"`
}

func TestDashboardProductionBundleBehavior(t *testing.T) {
	bundle := dashboardBehaviorBundle(t)
	for _, scenario := range []string{"protocol", "filters", "filter_toggle", "ai_badges", "pagination", "copy"} {
		t.Run(scenario, func(t *testing.T) {
			result := requireDashboardBehaviorScenario(t, bundle, scenario)
			if result.Scenario != scenario {
				t.Fatalf("harness scenario = %q, want %q", result.Scenario, scenario)
			}
			if result.Assertions == 0 {
				t.Fatal("harness did not execute any behavior assertions")
			}
			if scenario == "copy" {
				assertCopiedUsernameParses(t, result)
			}
		})
	}
}

func TestDashboardNodeKeyWireContractMatchesAuth(t *testing.T) {
	result := requireDashboardBehaviorScenario(t, dashboardJS, "nodekey_wire")
	if len(result.WireVectors) != 4 {
		t.Fatalf("dashboard wire vector count = %d, want 4", len(result.WireVectors))
	}
	for _, vector := range result.WireVectors {
		wantToken := auth.EncodeNodeKeyPin(vector.NodeKey)
		if vector.Token != wantToken {
			t.Fatalf("production JS token for NodeKey %q = %q, want Go token %q", vector.NodeKey, vector.Token, wantToken)
		}
		parsed, err := auth.ParseUsername("edge-node-key-" + vector.Token)
		if err != nil {
			t.Fatalf("wire token for NodeKey %q is not parseable: %v", vector.NodeKey, err)
		}
		if parsed.Node != "key-"+vector.NodeKey {
			t.Fatalf("wire token parsed node = %q, want %q", parsed.Node, "key-"+vector.NodeKey)
		}
	}
	for _, token := range []string{"", "bad/key", "A", "Zm8=", "%%%"} {
		raw := "edge-node-key-" + token
		if _, err := auth.ParseUsername(raw); err == nil {
			t.Fatalf("malformed wire token %q unexpectedly parsed", token)
		}
	}
}

// 此变异保留旧 strings.Contains 测试查找的完整 predicate 与调用片段，
// 但在实际执行时用后声明覆盖协议能力判断；行为 harness 必须将其击穿。
func TestDashboardBehaviorHarnessRejectsContainsPreservingProtocolMutation(t *testing.T) {
	mutated := containsPreservingProtocolMutation(t, dashboardJS)
	for _, evidence := range []string{
		dashboardProtocolPredicate,
		"if(dual)return wanted==='http'||wanted==='socks5'",
		"let rows=allProxies.filter(p=>nodeSupportsInboundProtocol(p,protocol)&&(!region||regionOf(p)===region))",
	} {
		if !strings.Contains(mutated, evidence) {
			t.Fatalf("controlled mutation unexpectedly removed Contains evidence %q", evidence)
		}
	}
	if strings.Contains(mutated, "(!protocol||p.protocol===protocol)") {
		t.Fatal("controlled mutation accidentally triggered the legacy negative-fragment check")
	}

	_, stderr, err := runDashboardBehaviorScenario(t, mutated, "protocol")
	if err == nil {
		t.Fatal("behavior harness accepted a contains-preserving protocol regression")
	}
	if !strings.Contains(stderr, "dual bool supports HTTP") {
		t.Fatalf("mutation failed for an unexpected reason: %v\nstderr:\n%s", err, stderr)
	}
}

func dashboardBehaviorBundle(t *testing.T) string {
	t.Helper()
	switch mutation := os.Getenv(dashboardBehaviorMutationEnv); mutation {
	case "":
		return dashboardJS
	case "legacy_protocol":
		t.Log("启用受控协议变异；旧 Contains 证据仍保留")
		return containsPreservingProtocolMutation(t, dashboardJS)
	case "ignore_purity":
		t.Log("启用受控纯净度变异；所有节点被错误归类为低风险")
		return injectDashboardBehaviorOverride(t, dashboardJS, "function purityStateOf(){return 'clean'}\n")
	case "collapse_ai_unknown":
		t.Log("启用受控 AI 三态变异；未探测被错误归类为畅通")
		return injectDashboardBehaviorOverride(t, dashboardJS, "function aiValueState(){return 'unlocked'}\n")
	default:
		t.Fatalf("unsupported %s value %q", dashboardBehaviorMutationEnv, mutation)
		return ""
	}
}

func injectDashboardBehaviorOverride(t *testing.T, bundle, override string) string {
	t.Helper()
	if !strings.Contains(bundle, dashboardBootMarker) {
		t.Fatalf("dashboard boot marker %q not found", dashboardBootMarker)
	}
	return strings.Replace(bundle, dashboardBootMarker, override+dashboardBootMarker, 1)
}

func containsPreservingProtocolMutation(t *testing.T, bundle string) string {
	t.Helper()
	const override = `
function nodeSupportsInboundProtocol(p,protocol){
  const wanted=String(protocol||'').trim().toLowerCase();
  if(!wanted)return true;
  const actual=String((p&&p.protocol)||'').trim().toLowerCase();
  return actual===wanted;
}
`
	if !strings.Contains(bundle, dashboardBootMarker) {
		t.Fatalf("dashboard boot marker %q not found", dashboardBootMarker)
	}
	return strings.Replace(bundle, dashboardBootMarker, override+dashboardBootMarker, 1)
}

func requireDashboardBehaviorScenario(t *testing.T, bundle, scenario string) dashboardHarnessResult {
	t.Helper()
	stdout, stderr, err := runDashboardBehaviorScenario(t, bundle, scenario)
	if err != nil {
		t.Fatalf("dashboard behavior scenario %q failed: %v\nstderr:\n%s\nstdout:\n%s", scenario, err, stderr, stdout)
	}
	var result dashboardHarnessResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &result); err != nil {
		t.Fatalf("decode dashboard behavior scenario %q output: %v\nstdout:\n%s", scenario, err, stdout)
	}
	return result
}

func runDashboardBehaviorScenario(t *testing.T, bundle, scenario string) (string, string, error) {
	t.Helper()
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("Node.js is required for executable dashboard behavior tests: %v", err)
	}
	harnessPath, err := filepath.Abs(filepath.Join("testdata", "dashboard_behavior_harness.js"))
	if err != nil {
		t.Fatalf("resolve dashboard behavior harness: %v", err)
	}
	if _, err := os.Stat(harnessPath); err != nil {
		t.Fatalf("stat dashboard behavior harness: %v", err)
	}
	bundlePath := filepath.Join(t.TempDir(), "dashboard-production.js")
	if err := os.WriteFile(bundlePath, []byte(bundle), 0o600); err != nil {
		t.Fatalf("write UTF-8 dashboard bundle: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, nodePath, harnessPath, bundlePath, scenario)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if ctx.Err() != nil {
		return stdout.String(), stderr.String(), fmt.Errorf("Node.js harness timeout: %w", ctx.Err())
	}
	return stdout.String(), stderr.String(), err
}

func assertCopiedUsernameParses(t *testing.T, result dashboardHarnessResult) {
	t.Helper()
	parsed, err := auth.ParseUsername(result.GatewayUser)
	if err != nil {
		t.Fatalf("copied gateway username %q is not accepted by auth parser: %v", result.GatewayUser, err)
	}
	if parsed.Base != "edge" {
		t.Fatalf("copied gateway base = %q, want edge", parsed.Base)
	}
	if parsed.Node != "key-"+result.NodeKey {
		t.Fatalf("copied gateway node pin = %q, want stable key for %q", parsed.Node, result.NodeKey)
	}
	if parsed.Region != "" || parsed.Session != "" || len(parsed.Unlock) != 0 {
		t.Fatalf("copied gateway username added unexpected routing hints: %+v", parsed)
	}
}
