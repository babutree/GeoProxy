package custom

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"

	"goproxy/storage"
	"goproxy/validator"
)

// TestAddManualTunnelNodeRollsBackRuntimeWhenDBTransactionFails:
// Reload succeeds then DB write fails → runtime must restore pre-add nodes.
func TestAddManualTunnelNodeRollsBackRuntimeWhenDBTransactionFails(t *testing.T) {
	store := newTestStorage(t)
	sb := newSpyShard()
	// Seed an existing tunnel node already in runtime.
	old := tunnelNode("old", "old.example.com", "oldpass")
	if err := sb.Reload([]ParsedNode{old}); err != nil {
		t.Fatalf("seed Reload: %v", err)
	}
	m := &Manager{storage: store, singbox: sb}

	// Fail any manual proxy insert after runtime has changed.
	if _, err := store.GetDB().Exec(`
		CREATE TRIGGER fail_manual_insert
		BEFORE INSERT ON proxies
		WHEN NEW.source = 'manual'
		BEGIN
			SELECT RAISE(ABORT, 'injected manual insert failure');
		END
	`); err != nil {
		t.Fatalf("install insert fail trigger: %v", err)
	}

	link := "trojan://password@new.example.com:443?sni=new.example.com#new-tunnel"
	err := m.AddManualNode(link, "jp", "tunnel")
	if err == nil {
		t.Fatal("AddManualNode expected DB failure error, got nil")
	}
	if !strings.Contains(err.Error(), "injected manual insert failure") &&
		!strings.Contains(err.Error(), "存储") {
		t.Fatalf("error = %v, want mention of storage/insert failure", err)
	}

	newNode, parseErr := ParseSingleLink(link)
	if parseErr != nil {
		t.Fatalf("ParseSingleLink: %v", parseErr)
	}
	keys := map[string]bool{}
	for _, n := range sb.GetNodes() {
		keys[n.NodeKey()] = true
	}
	if keys[newNode.NodeKey()] {
		t.Fatalf("new node still in runtime after DB failure; keys=%v", keys)
	}
	if !keys[old.NodeKey()] {
		t.Fatalf("old node missing after rollback; keys=%v", keys)
	}
	count, err := store.CountBySource(storage.SourceManual)
	if err != nil {
		t.Fatalf("CountBySource: %v", err)
	}
	if count != 0 {
		t.Fatalf("manual proxies = %d, want 0 after failed add", count)
	}
	// seed + forward + rollback
	if sb.calls() < 3 {
		t.Fatalf("reloadCalls = %d, want >=3 (seed + forward + rollback)", sb.calls())
	}
}

// TestAddManualTunnelNodeSuccessCommitsRuntimeAndDatabaseTogether positive path.
func TestAddManualTunnelNodeSuccessCommitsRuntimeAndDatabaseTogether(t *testing.T) {
	store := newTestStorage(t)
	sb := newSpyShard()
	m := &Manager{storage: store, singbox: sb}

	link := "trojan://password@ok.example.com:443?sni=ok.example.com#ok"
	if err := m.AddManualNode(link, "us", "ok-note"); err != nil {
		t.Fatalf("AddManualNode: %v", err)
	}
	node, err := ParseSingleLink(link)
	if err != nil {
		t.Fatalf("ParseSingleLink: %v", err)
	}
	port, ok := sb.GetPortMap()[node.NodeKey()]
	if !ok {
		t.Fatalf("portMap missing key %s", node.NodeKey())
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	proxy, err := store.GetProxyByAddress(addr)
	if err != nil {
		t.Fatalf("GetProxyByAddress: %v", err)
	}
	if proxy.Source != storage.SourceManual || proxy.Protocol != "socks5" {
		t.Fatalf("proxy = %+v, want manual/socks5", proxy)
	}
	if !proxy.DualProtocol {
		t.Fatal("dual_protocol = false, want true for mixed tunnel")
	}
	if proxy.Region != "us" || proxy.Note != "ok-note" {
		t.Fatalf("region/note = %q/%q", proxy.Region, proxy.Note)
	}
}

// TestRefreshSubscriptionReturnsErrorWhenDeletingOldProxiesFails:
// DeleteBySubscriptionID failure must surface and not mark fetch success / not leave half state.
func TestRefreshSubscriptionReturnsErrorWhenDeletingOldProxiesFails(t *testing.T) {
	store := newTestStorage(t)
	file := writeSubscriptionFile(t, "proxies:\n  - name: n1\n    type: http\n    server: 10.0.0.1\n    port: 8080\n  - name: n2\n    type: http\n    server: 10.0.0.2\n    port: 8080\n")
	subID, err := store.AddSubscription("sub", "", file, "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}
	// Seed an old proxy for this subscription.
	if err := store.AddProxyWithSource("10.0.0.9:8080", "http", storage.SourceSubscription, subID); err != nil {
		t.Fatalf("seed old proxy: %v", err)
	}

	if _, err := store.GetDB().Exec(fmt.Sprintf(`
		CREATE TRIGGER fail_sub_delete
		BEFORE DELETE ON proxies
		WHEN OLD.subscription_id = %d
		BEGIN
			SELECT RAISE(ABORT, 'injected delete-by-subscription failure');
		END
	`, subID)); err != nil {
		t.Fatalf("install delete fail trigger: %v", err)
	}

	m := &Manager{storage: store, singbox: newSpyShard()}
	err = m.RefreshSubscription(subID)
	if err == nil {
		t.Fatal("RefreshSubscription expected delete failure, got nil")
	}
	if !strings.Contains(err.Error(), "injected delete-by-subscription failure") &&
		!strings.Contains(err.Error(), "删除") {
		t.Fatalf("error = %v, want delete failure signal", err)
	}

	// Old proxy must still exist.
	if _, err := store.GetProxyByAddress("10.0.0.9:8080"); err != nil {
		t.Fatalf("old proxy missing after failed refresh: %v", err)
	}
	// New proxies from the file must not be partially committed if delete failed first.
	if _, err := store.GetProxyByAddress("10.0.0.1:8080"); err == nil {
		t.Fatal("new proxy committed despite delete failure")
	}
}

// TestRefreshSubscriptionToDirectRemovesOldSubscriptionTunnelRuntime:
// when a subscription previously owned a tunnel node but refreshes to direct-only,
// the old tunnel must be removed from sing-box runtime instead of surviving via GetNodes().
func TestRefreshSubscriptionToDirectRemovesOldSubscriptionTunnelRuntime(t *testing.T) {
	store := newTestStorage(t)
	directFile := writeSubscriptionFile(t, "proxies:\n  - name: direct\n    type: http\n    server: 10.0.0.10\n    port: 8080\n")
	subID, err := store.AddSubscription("sub", "", directFile, "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}

	sb := newSpyShard()
	oldNode := tunnelNode("old-sub-tunnel", "old-sub.example.com", "oldpass")
	if err := sb.Reload([]ParsedNode{oldNode}); err != nil {
		t.Fatalf("seed Reload: %v", err)
	}
	oldPort := sb.GetPortMap()[oldNode.NodeKey()]
	oldAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(oldPort))
	if err := store.AddProxyWithSource(oldAddr, "socks5", storage.SourceSubscription, subID); err != nil {
		t.Fatalf("seed old subscription proxy: %v", err)
	}

	m := &Manager{storage: store, validator: validator.New(1, 1, "http://127.0.0.1/validate"), singbox: sb}
	if err := m.RefreshSubscription(subID); err != nil {
		t.Fatalf("RefreshSubscription: %v", err)
	}

	for _, n := range sb.GetNodes() {
		if n.NodeKey() == oldNode.NodeKey() {
			t.Fatal("old subscription tunnel still in runtime after direct-only refresh")
		}
	}
	if _, err := store.GetProxyByAddress(oldAddr); err == nil {
		t.Fatal("old subscription tunnel proxy still in DB after refresh")
	}
}

// TestDeleteSubscriptionRemovesRuntimeTunnel:
// deleting a subscription must remove its tunnel runtime, not just DB rows.
func TestDeleteSubscriptionRemovesRuntimeTunnel(t *testing.T) {
	store := newTestStorage(t)
	subID, err := store.AddSubscription("sub", "", writeSubscriptionFile(t, "trojan://password@delete-sub.example.com:443?sni=delete-sub.example.com#delete-sub"), "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}

	sb := newSpyShard()
	node := tunnelNode("delete-sub", "delete-sub.example.com", "password")
	if err := sb.Reload([]ParsedNode{node}); err != nil {
		t.Fatalf("seed Reload: %v", err)
	}
	port := sb.GetPortMap()[node.NodeKey()]
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if err := store.AddProxyWithSource(addr, "socks5", storage.SourceSubscription, subID); err != nil {
		t.Fatalf("seed subscription proxy: %v", err)
	}

	m := &Manager{storage: store, singbox: sb}
	if err := m.DeleteSubscription(subID); err != nil {
		t.Fatalf("DeleteSubscription: %v", err)
	}

	for _, n := range sb.GetNodes() {
		if n.NodeKey() == node.NodeKey() {
			t.Fatal("deleted subscription tunnel still in runtime")
		}
	}
	if _, err := store.GetSubscription(subID); err == nil {
		t.Fatal("subscription row still present after delete")
	}
}

// TestRefreshSubscriptionRollsBackRuntimeWhenDBDeleteFails:
// after a successful runtime reload, a DB delete failure must surface and restore old runtime.
func TestRefreshSubscriptionRollsBackRuntimeWhenDBDeleteFails(t *testing.T) {
	store := newTestStorage(t)
	file := writeSubscriptionFile(t, "trojan://password@new-sub.example.com:443?sni=new-sub.example.com#new-sub")
	subID, err := store.AddSubscription("sub", "", file, "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}

	sb := newSpyShard()
	oldNode := tunnelNode("old-sub", "old-sub.example.com", "oldpass")
	if err := sb.Reload([]ParsedNode{oldNode}); err != nil {
		t.Fatalf("seed Reload: %v", err)
	}
	oldAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(sb.GetPortMap()[oldNode.NodeKey()]))
	if err := store.AddProxyWithSource(oldAddr, "socks5", storage.SourceSubscription, subID); err != nil {
		t.Fatalf("seed old subscription proxy: %v", err)
	}
	if _, err := store.GetDB().Exec(fmt.Sprintf(`
		CREATE TRIGGER fail_sub_delete_after_reload
		BEFORE DELETE ON proxies
		WHEN OLD.subscription_id = %d
		BEGIN
			SELECT RAISE(ABORT, 'injected subscription delete failure after reload');
		END
	`, subID)); err != nil {
		t.Fatalf("install delete fail trigger: %v", err)
	}

	m := &Manager{storage: store, validator: validator.New(1, 1, "http://127.0.0.1/validate"), singbox: sb}
	err = m.RefreshSubscription(subID)
	if err == nil {
		t.Fatal("RefreshSubscription expected DB delete failure, got nil")
	}
	if !strings.Contains(err.Error(), "injected subscription delete failure after reload") {
		t.Fatalf("error = %v, want injected delete failure", err)
	}

	keys := map[string]bool{}
	for _, n := range sb.GetNodes() {
		keys[n.NodeKey()] = true
	}
	if !keys[oldNode.NodeKey()] {
		t.Fatalf("old runtime node missing after DB failure rollback; keys=%v", keys)
	}
	newNode, parseErr := ParseSingleLink("trojan://password@new-sub.example.com:443?sni=new-sub.example.com#new-sub")
	if parseErr != nil {
		t.Fatalf("ParseSingleLink: %v", parseErr)
	}
	if keys[newNode.NodeKey()] {
		t.Fatalf("new runtime node committed despite DB failure; keys=%v", keys)
	}
	if _, err := store.GetProxyByAddress(oldAddr); err != nil {
		t.Fatalf("old DB proxy missing after failed refresh: %v", err)
	}
}

// TestDeleteManualTunnelNodeReloadsWithoutDeletedNode:
// deleting a manual tunnel proxy must drop it from sing-box nodes/portMap.
func TestDeleteManualTunnelNodeReloadsWithoutDeletedNode(t *testing.T) {
	store := newTestStorage(t)
	sb := newSpyShard()
	m := &Manager{storage: store, singbox: sb}

	link := "trojan://password@del.example.com:443?sni=del.example.com#del"
	if err := m.AddManualNode(link, "jp", "to-delete"); err != nil {
		t.Fatalf("AddManualNode: %v", err)
	}
	node, _ := ParseSingleLink(link)
	port := sb.GetPortMap()[node.NodeKey()]
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	proxy, err := store.GetProxyByAddress(addr)
	if err != nil {
		t.Fatalf("GetProxyByAddress: %v", err)
	}

	if err := m.DeleteManualNode(proxy.ID); err != nil {
		t.Fatalf("DeleteManualNode: %v", err)
	}
	if _, ok := sb.GetPortMap()[node.NodeKey()]; ok {
		t.Fatal("deleted node still in portMap")
	}
	for _, n := range sb.GetNodes() {
		if n.NodeKey() == node.NodeKey() {
			t.Fatal("deleted node still in GetNodes()")
		}
	}
	if _, err := store.GetProxyByID(proxy.ID); err == nil {
		t.Fatal("DB row still present after delete")
	}
}

// TestDeleteManualDirectNodeDoesNotReloadSingBox:
// direct manual delete is DB-only.
func TestDeleteManualDirectNodeDoesNotReloadSingBox(t *testing.T) {
	store := newTestStorage(t)
	sb := newSpyShard()
	m := &Manager{storage: store, singbox: sb}
	if err := m.AddManualNode("http://203.0.113.50:8080", "us", "direct"); err != nil {
		t.Fatalf("AddManualNode: %v", err)
	}
	proxy, err := store.GetProxyByAddress("203.0.113.50:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress: %v", err)
	}
	before := sb.calls()
	if err := m.DeleteManualNode(proxy.ID); err != nil {
		t.Fatalf("DeleteManualNode: %v", err)
	}
	if sb.calls() != before {
		t.Fatalf("reloadCalls changed %d -> %d; direct delete must not Reload", before, sb.calls())
	}
}

// TestManualTunnelNodeDeleteDoesNotReappearAfterSubscriptionRefresh:
// after delete, Refresh must not re-import the node solely via GetNodes() union.
func TestManualTunnelNodeDeleteDoesNotReappearAfterSubscriptionRefresh(t *testing.T) {
	store := newTestStorage(t)
	sb := newSpyShard()
	m := &Manager{storage: store, singbox: sb}

	link := "trojan://password@ghost.example.com:443?sni=ghost.example.com#ghost"
	if err := m.AddManualNode(link, "jp", "ghost"); err != nil {
		t.Fatalf("AddManualNode: %v", err)
	}
	node, _ := ParseSingleLink(link)
	port := sb.GetPortMap()[node.NodeKey()]
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	proxy, err := store.GetProxyByAddress(addr)
	if err != nil {
		t.Fatalf("GetProxyByAddress: %v", err)
	}
	if err := m.DeleteManualNode(proxy.ID); err != nil {
		t.Fatalf("DeleteManualNode: %v", err)
	}

	// Another manual tunnel add + refresh-like merge via collect path:
	// if ghost remained in GetNodes(), mergeWithExisting would keep it.
	// After correct delete, a subsequent AddManualNode of a different tunnel
	// must not reintroduce the ghost key.
	other := "trojan://password@other.example.com:443?sni=other.example.com#other"
	if err := m.AddManualNode(other, "us", "other"); err != nil {
		t.Fatalf("AddManualNode other: %v", err)
	}
	for _, n := range sb.GetNodes() {
		if n.NodeKey() == node.NodeKey() {
			t.Fatal("deleted manual tunnel reappeared after later tunnel merge")
		}
	}
}
