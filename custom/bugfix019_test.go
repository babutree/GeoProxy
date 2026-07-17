package custom

import (
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/babutree/GeoProxy/storage"
	"github.com/babutree/GeoProxy/validator"
)

// TestBUGFIX019ReloadKeepsBuildableNodes verifies one rejected node does not
// prevent a buildable sibling from reaching the sing-box runtime.
func TestBUGFIX019ReloadKeepsBuildableNodes(t *testing.T) {
	s := NewSingBoxProcess("sing-box", t.TempDir(), testSingBoxBasePort)
	good := tunnelNode("good", "good.example.com", "good-password")
	bad := unsupportedXHTTPNode()

	if err := s.Reload([]ParsedNode{good, bad}); err != nil {
		t.Fatalf("Reload() error = %v; buildable node must still load", err)
	}
	t.Cleanup(s.Stop)
	ports := s.GetPortMap()
	if _, ok := ports[good.NodeKey()]; !ok {
		t.Fatalf("buildable node missing from portMap: %v", ports)
	}
	if _, ok := ports[bad.NodeKey()]; ok {
		t.Fatalf("rejected node unexpectedly entered portMap: %v", ports)
	}
}

// TestBUGFIX019ShardedCommitSkipsRejectedNode verifies assignedKeys records
// only the accepted part of a partially rejected target.
func TestBUGFIX019ShardedCommitSkipsRejectedNode(t *testing.T) {
	shard := &diagnosticSpyShard{spyShard: newSpyShard()}
	sb := newShardedSingBoxWithFactory(testSingBoxBasePort, 1, func(int, int) singBoxShard {
		return shard
	})
	good := tunnelNode("shard-good", "shard-good.example.com", "password")
	bad := unsupportedXHTTPNode()
	shard.diagnostics = assemblyDiagnostics{
		accepted: []ParsedNode{good},
		rejected: []assemblyRejectedNode{{node: bad, err: errUnsupportedTransport}},
	}

	if err := sb.Reload([]ParsedNode{good, bad}); err != nil {
		t.Fatalf("Reload() error = %v; explicitly rejected node must not block commit", err)
	}
	if !sb.assignedKeys[0][good.NodeKey()] || sb.assignedKeys[0][bad.NodeKey()] {
		t.Fatalf("assignedKeys=%v, want only accepted key", sb.assignedKeys[0])
	}
}

// TestBUGFIX019ShardedUnknownPortMissingRollsBack verifies a missing port with
// no assembly evidence remains strict and restores the prior committed state.
func TestBUGFIX019ShardedUnknownPortMissingRollsBack(t *testing.T) {
	shard := newSpyShard()
	sb := newShardedSingBoxWithFactory(testSingBoxBasePort, 1, func(int, int) singBoxShard {
		return shard
	})
	old := tunnelNode("shard-old", "shard-old.example.com", "password")
	if err := sb.Reload([]ParsedNode{old}); err != nil {
		t.Fatalf("seed Reload() error = %v", err)
	}
	shard.incompletePorts = true
	newNode := tunnelNode("shard-new", "shard-new.example.com", "password")
	err := sb.Reload([]ParsedNode{newNode})
	if err == nil || !strings.Contains(err.Error(), "未知") {
		t.Fatalf("unknown missing port must fail explicitly, got: %v", err)
	}
	if !sb.assignedKeys[0][old.NodeKey()] || sb.assignedKeys[0][newNode.NodeKey()] {
		t.Fatalf("assignedKeys not restored after strict failure: %v", sb.assignedKeys[0])
	}
}

// TestBUGFIX019ManagerCommitsAcceptedTunnelAndKeepsBadOutOfDB verifies the
// subscription boundary persists only accepted tunnel nodes.
func TestBUGFIX019ManagerCommitsAcceptedTunnelAndKeepsBadOutOfDB(t *testing.T) {
	store := newTestStorage(t)
	file := writeSubscriptionFile(t, strings.Join([]string{
		"trojan://password@good.example.com:443?sni=good.example.com#good",
		"vless://347b77a2-dbf7-4755-adb9-64ef05f51e84@bad.example.com:443?type=xhttp#bad",
	}, "\n"))
	subID, err := store.AddSubscription("bugfix019", "", file, "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	good, err := ParseSingleLink("trojan://password@good.example.com:443?sni=good.example.com#good")
	if err != nil {
		t.Fatalf("ParseSingleLink(good) error = %v", err)
	}
	bad, err := ParseSingleLink("vless://347b77a2-dbf7-4755-adb9-64ef05f51e84@bad.example.com:443?type=xhttp#bad")
	if err != nil {
		t.Fatalf("ParseSingleLink(bad) error = %v", err)
	}
	shard := &diagnosticSpyShard{spyShard: newSpyShard()}
	shard.diagnostics = assemblyDiagnostics{
		accepted: []ParsedNode{*good},
		rejected: []assemblyRejectedNode{{node: *bad, err: errUnsupportedTransport}},
	}
	m := &Manager{storage: store, validator: validator.New(1, 1, "http://127.0.0.1/validate"), singbox: shard}

	if err := m.RefreshSubscription(subID); err != nil {
		t.Fatalf("RefreshSubscription() error = %v", err)
	}
	port, ok := shard.GetPortMap()[good.NodeKey()]
	if !ok {
		t.Fatalf("accepted node missing portMap: %v", shard.GetPortMap())
	}
	if _, err := store.GetProxyByAddress(net.JoinHostPort("127.0.0.1", strconv.Itoa(port))); err != nil {
		t.Fatalf("accepted tunnel proxy missing from DB: %v", err)
	}
	if _, ok := shard.GetPortMap()[bad.NodeKey()]; ok {
		t.Fatalf("rejected node entered portMap: %v", shard.GetPortMap())
	}
	// 新入库代理默认 disabled，不能用 CountBySource（只计 active/degraded）。
	var count int
	if err := store.GetDB().QueryRow(
		`SELECT COUNT(*) FROM proxies WHERE subscription_id = ? AND source = ?`,
		subID, storage.SourceSubscription,
	).Scan(&count); err != nil {
		t.Fatalf("count subscription proxies: %v", err)
	}
	if count != 1 {
		t.Fatalf("subscription proxies=%d, want 1 accepted node", count)
	}
}

// TestBUGFIX019RefreshAllRejectedTunnelsKeepsOldRuntime 确保刷新结果全部被拒绝时，
// 返回错误不会停止该订阅此前已提交的 sing-box 运行态。
func TestBUGFIX019RefreshAllRejectedTunnelsKeepsOldRuntime(t *testing.T) {
	store := newTestStorage(t)
	file := writeSubscriptionFile(t, "vless://347b77a2-dbf7-4755-adb9-64ef05f51e84@bad.example.com:443?type=xhttp#bad")
	subID, err := store.AddSubscription("all-rejected", "", file, "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}

	sb, spies := newSpyOrchestrator(testSingBoxBasePort, 1)
	oldNode := tunnelNode("old", "old.example.com", "old-password")
	if err := sb.Reload([]ParsedNode{oldNode}); err != nil {
		t.Fatalf("seed Reload() error = %v", err)
	}
	oldPort := sb.GetPortMap()[oldNode.NodeKey()]
	oldAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(oldPort))
	if err := store.AddProxyWithSource(oldAddr, "socks5", storage.SourceSubscription, subID); err != nil {
		t.Fatalf("seed old subscription proxy: %v", err)
	}

	m := &Manager{storage: store, singbox: sb}
	if err := m.RefreshSubscription(subID); err == nil {
		t.Fatal("RefreshSubscription() expected all-rejected tunnel error, got nil")
	}

	if _, ok := sb.GetPortMap()[oldNode.NodeKey()]; !ok {
		t.Fatalf("old runtime port missing after rejected refresh: %v", sb.GetPortMap())
	}
	if stops := spies[0].stops(); stops != 0 {
		t.Fatalf("rejected refresh stopped the old runtime %d times", stops)
	}
	if _, err := store.GetProxyByAddress(oldAddr); err != nil {
		t.Fatalf("old subscription proxy missing after rejected refresh: %v", err)
	}
}

type diagnosticSpyShard struct {
	*spyShard
	diagnostics assemblyDiagnostics
}

func (s *diagnosticSpyShard) Reload(nodes []ParsedNode) error {
	if len(s.diagnostics.accepted) == 0 {
		return s.spyShard.Reload(nodes)
	}
	return s.spyShard.Reload(s.diagnostics.accepted)
}

func (s *diagnosticSpyShard) GetAssemblyDiagnostics() assemblyDiagnostics {
	return s.diagnostics
}

var errUnsupportedTransport = unsupportedTransportError{}

type unsupportedTransportError struct{}

func (unsupportedTransportError) Error() string { return "unsupported transport" }

// TestBUGFIX019UnknownMissingPortIsNotReportedAsSegmentFull requires evidence
// from the allocator before classifying a missing port as a full segment.
func TestBUGFIX019UnknownMissingPortIsNotReportedAsSegmentFull(t *testing.T) {
	node := tunnelNode("unknown", "unknown.example.com", "password")
	err := incompletePortAllocationError([]ParsedNode{node}, map[string]int{})
	if err == nil {
		t.Fatal("missing port must return an explicit error")
	}
	if strings.Contains(err.Error(), "端口段已满") {
		t.Fatalf("missing port without allocator evidence must be unknown, got: %v", err)
	}
	if !strings.Contains(err.Error(), "未知") {
		t.Fatalf("missing port without allocator evidence must be classified as unknown, got: %v", err)
	}
}

// TestBUGFIX019IncompleteDiagnosticDoesNotMutateNode verifies diagnostics do
// not change identity-bearing parsed input while forcing TLS for trojan.
func TestBUGFIX019IncompleteDiagnosticDoesNotMutateNode(t *testing.T) {
	node := ParsedNode{
		Name:   "immutable",
		Type:   "trojan",
		Server: "immutable.example.com",
		Port:   443,
		Raw: map[string]interface{}{
			"type":     "trojan",
			"password": "password",
			"sni":      "immutable.example.com",
		},
	}
	keyBefore := node.NodeKey()
	if _, ok := node.Raw["tls"]; ok {
		t.Fatal("test fixture unexpectedly already has tls")
	}

	_ = incompletePortAllocationError([]ParsedNode{node}, map[string]int{})

	if _, ok := node.Raw["tls"]; ok {
		t.Fatalf("diagnostic mutated ParsedNode.Raw: %v", node.Raw)
	}
	if got := node.NodeKey(); got != keyBefore {
		t.Fatalf("NodeKey changed after diagnostic: got %s want %s", got, keyBefore)
	}
}
