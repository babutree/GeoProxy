package custom

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/babutree/GeoProxy/storage"
	"github.com/babutree/GeoProxy/validator"
)

// TestRefreshSubscriptionKeepsSharedNodeKeyRuntime 验证刷新一个 owner 时，
// 另一个订阅仍拥有的 NodeKey 不得从全局 sing-box 运行态移除。
func TestRefreshSubscriptionKeepsSharedNodeKeyRuntime(t *testing.T) {
	store := newTestStorage(t)
	proxyAddr, validateURL := startRejectingHTTPProxy(t)
	host, portText, err := net.SplitHostPort(proxyAddr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", proxyAddr, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("Atoi(%q): %v", portText, err)
	}
	directFile := writeSubscriptionFile(t, fmt.Sprintf(
		"proxies:\n  - name: direct\n    type: http\n    server: %s\n    port: %d\n",
		host, port,
	))
	subA := addNodeKeyTestSubscription(t, store, "owner-a", directFile)
	subB := addNodeKeyTestSubscription(t, store, "owner-b", writeSubscriptionFile(t,
		"trojan://password@shared.example.com:443?sni=shared.example.com#shared"))

	shard, node, address := seedSharedNodeKeyRuntime(t, store, subA, subB)
	m := &Manager{
		storage:   store,
		validator: validator.New(1, 1, validateURL),
		singbox:   shard,
	}

	if err := m.RefreshSubscription(subA); err != nil {
		t.Fatalf("RefreshSubscription(owner-a): %v", err)
	}
	assertRuntimeNodeKey(t, shard, node.NodeKey())
	if _, err := store.GetProxyByIdentity(address, storage.SourceSubscription, subB); err != nil {
		t.Fatalf("owner-b proxy removed by owner-a refresh: %v", err)
	}
}

// TestRefreshSubscriptionIgnoresDifferentNodeKeyAtSameAddress 验证相同旧地址但
// 稳定 key 不同的行不是目标 key 的 owner，刷新后必须回收目标运行态。
func TestRefreshSubscriptionIgnoresDifferentNodeKeyAtSameAddress(t *testing.T) {
	store := newTestStorage(t)
	subA, validateURL := addRejectingDirectNodeKeyTestSubscription(t, store, "owner-a")
	subB := addNodeKeyTestSubscription(t, store, "owner-b", writeSubscriptionFile(t,
		"trojan://other@other.example.com:443?sni=other.example.com#other"))
	shard, targetNode, _ := seedSharedNodeKeyRuntime(t, store, subA, subB)
	otherNode := tunnelNode("other", "other.example.com", "other")
	setSubscriptionNodeKey(t, store, subB, otherNode.NodeKey())
	m := &Manager{
		storage:   store,
		validator: validator.New(1, 1, validateURL),
		singbox:   shard,
	}

	if err := m.RefreshSubscription(subA); err != nil {
		t.Fatalf("RefreshSubscription(owner-a different-key peer): %v", err)
	}
	assertRuntimeMissingNodeKey(t, shard, targetNode.NodeKey())
}

// TestRefreshSubscriptionKeepsLegacyEmptyNodeKeyAtSameAddress 验证其它 owner
// 没有稳定 key 时仍按相同旧地址兼容，刷新不得回收共享运行态。
func TestRefreshSubscriptionKeepsLegacyEmptyNodeKeyAtSameAddress(t *testing.T) {
	store := newTestStorage(t)
	subA, validateURL := addRejectingDirectNodeKeyTestSubscription(t, store, "owner-a")
	subB := addNodeKeyTestSubscription(t, store, "owner-b", writeSubscriptionFile(t,
		"trojan://password@shared.example.com:443?sni=shared.example.com#shared"))
	shard, targetNode, _ := seedSharedNodeKeyRuntime(t, store, subA, subB)
	setSubscriptionNodeKey(t, store, subB, "")
	m := &Manager{
		storage:   store,
		validator: validator.New(1, 1, validateURL),
		singbox:   shard,
	}

	if err := m.RefreshSubscription(subA); err != nil {
		t.Fatalf("RefreshSubscription(owner-a legacy peer): %v", err)
	}
	assertRuntimeNodeKey(t, shard, targetNode.NodeKey())
}

// TestRefreshSubscriptionKeepsSharedOldKeyWhenOwnerSwitchesKey 验证 owner 切换到新
// NodeKey 时，旧 key 仍由其它订阅持有就必须与新 key 一起留在运行态。
func TestRefreshSubscriptionKeepsSharedOldKeyWhenOwnerSwitchesKey(t *testing.T) {
	store := newTestStorage(t)
	newFile := writeSubscriptionFile(t,
		"trojan://password@new.example.com:443?sni=new.example.com#new")
	subA := addNodeKeyTestSubscription(t, store, "owner-a", newFile)
	subB := addNodeKeyTestSubscription(t, store, "owner-b", writeSubscriptionFile(t,
		"trojan://password@shared.example.com:443?sni=shared.example.com#shared"))
	shard, oldNode, address := seedSharedNodeKeyRuntime(t, store, subA, subB)
	newNode, err := ParseSingleLink("trojan://password@new.example.com:443?sni=new.example.com#new")
	if err != nil {
		t.Fatalf("ParseSingleLink(new): %v", err)
	}
	m := &Manager{
		storage:   store,
		validator: validator.New(1, 1, "http://example.invalid/validate"),
		singbox:   shard,
	}

	if err := m.RefreshSubscription(subA); err != nil {
		t.Fatalf("RefreshSubscription(owner-a switch): %v", err)
	}
	assertRuntimeNodeKey(t, shard, oldNode.NodeKey())
	assertRuntimeNodeKey(t, shard, newNode.NodeKey())
	if _, err := store.GetProxyByIdentity(address, storage.SourceSubscription, subB); err != nil {
		t.Fatalf("owner-b proxy removed while owner-a switched key: %v", err)
	}
}

// TestDeleteSubscriptionKeepsSharedNodeKeyRuntime 验证删除一个订阅时，
// 共享 NodeKey 的其它订阅仍保持运行态与数据库所有权。
func TestDeleteSubscriptionKeepsSharedNodeKeyRuntime(t *testing.T) {
	store := newTestStorage(t)
	subA := addNodeKeyTestSubscription(t, store, "owner-a", writeSubscriptionFile(t,
		"trojan://password@shared.example.com:443?sni=shared.example.com#shared"))
	subB := addNodeKeyTestSubscription(t, store, "owner-b", writeSubscriptionFile(t,
		"trojan://password@shared.example.com:443?sni=shared.example.com#shared"))
	shard, node, address := seedSharedNodeKeyRuntime(t, store, subA, subB)
	m := &Manager{storage: store, singbox: shard}

	if err := m.DeleteSubscription(subA); err != nil {
		t.Fatalf("DeleteSubscription(owner-a): %v", err)
	}
	assertRuntimeNodeKey(t, shard, node.NodeKey())
	if _, err := store.GetProxyByIdentity(address, storage.SourceSubscription, subB); err != nil {
		t.Fatalf("owner-b proxy removed by owner-a subscription delete: %v", err)
	}
}

// TestDeleteSubscriptionIgnoresDifferentNodeKeyAtSameAddress 验证删除订阅时，
// 不同稳定 key 的陈旧同地址行不能阻止目标 key 回收。
func TestDeleteSubscriptionIgnoresDifferentNodeKeyAtSameAddress(t *testing.T) {
	store := newTestStorage(t)
	subA := addNodeKeyTestSubscription(t, store, "owner-a", writeSubscriptionFile(t,
		"trojan://password@shared.example.com:443?sni=shared.example.com#shared"))
	subB := addNodeKeyTestSubscription(t, store, "owner-b", writeSubscriptionFile(t,
		"trojan://other@other.example.com:443?sni=other.example.com#other"))
	shard, targetNode, _ := seedSharedNodeKeyRuntime(t, store, subA, subB)
	otherNode := tunnelNode("other", "other.example.com", "other")
	setSubscriptionNodeKey(t, store, subB, otherNode.NodeKey())
	m := &Manager{storage: store, singbox: shard}

	if err := m.DeleteSubscription(subA); err != nil {
		t.Fatalf("DeleteSubscription(owner-a different-key peer): %v", err)
	}
	assertRuntimeMissingNodeKey(t, shard, targetNode.NodeKey())
}

// TestDeleteSubscriptionKeepsLegacySharedAddressRuntime 验证历史空 node_key 行
// 仍按共享本地地址保守保留运行态，避免迁移前数据被误回收。
func TestDeleteSubscriptionKeepsLegacySharedAddressRuntime(t *testing.T) {
	store := newTestStorage(t)
	subA := addNodeKeyTestSubscription(t, store, "owner-a", writeSubscriptionFile(t,
		"trojan://password@shared.example.com:443?sni=shared.example.com#shared"))
	subB := addNodeKeyTestSubscription(t, store, "owner-b", writeSubscriptionFile(t,
		"trojan://password@shared.example.com:443?sni=shared.example.com#shared"))
	shard, node, address := seedSharedNodeKeyRuntime(t, store, subA, subB)
	if _, err := store.GetDB().Exec(`UPDATE proxies SET node_key = '' WHERE address = ?`, address); err != nil {
		t.Fatalf("clear legacy node_key: %v", err)
	}
	m := &Manager{storage: store, singbox: shard}

	if err := m.DeleteSubscription(subA); err != nil {
		t.Fatalf("DeleteSubscription(owner-a legacy): %v", err)
	}
	assertRuntimeNodeKey(t, shard, node.NodeKey())
}

// TestDeleteManagedProxyKeepsSharedNodeKeyRuntime 验证删除单个 owner 行时，
// 共享 key 仍有其它订阅 owner 就只能删除该行，不能回收全局运行态端口。
func TestDeleteManagedProxyKeepsSharedNodeKeyRuntime(t *testing.T) {
	store := newTestStorage(t)
	subA := addNodeKeyTestSubscription(t, store, "owner-a", writeSubscriptionFile(t,
		"trojan://password@shared.example.com:443?sni=shared.example.com#shared"))
	subB := addNodeKeyTestSubscription(t, store, "owner-b", writeSubscriptionFile(t,
		"trojan://password@shared.example.com:443?sni=shared.example.com#shared"))
	shard, node, address := seedSharedNodeKeyRuntime(t, store, subA, subB)
	ownerA, err := store.GetProxyByIdentity(address, storage.SourceSubscription, subA)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(owner-a): %v", err)
	}
	m := &Manager{storage: store, singbox: shard}

	if err := m.DeleteManagedProxy(ownerA.ID); err != nil {
		t.Fatalf("DeleteManagedProxy(owner-a): %v", err)
	}
	assertRuntimeNodeKey(t, shard, node.NodeKey())
	if _, err := store.GetProxyByIdentity(address, storage.SourceSubscription, subB); err != nil {
		t.Fatalf("owner-b proxy removed by owner-a row delete: %v", err)
	}
}

// TestDeleteManagedProxyIgnoresDifferentNodeKeyAtSameAddress 验证单行删除时，
// 不同稳定 key 的陈旧同地址行不能冒充目标 owner。
func TestDeleteManagedProxyIgnoresDifferentNodeKeyAtSameAddress(t *testing.T) {
	store := newTestStorage(t)
	subA := addNodeKeyTestSubscription(t, store, "owner-a", writeSubscriptionFile(t,
		"trojan://password@shared.example.com:443?sni=shared.example.com#shared"))
	subB := addNodeKeyTestSubscription(t, store, "owner-b", writeSubscriptionFile(t,
		"trojan://other@other.example.com:443?sni=other.example.com#other"))
	shard, targetNode, address := seedSharedNodeKeyRuntime(t, store, subA, subB)
	otherNode := tunnelNode("other", "other.example.com", "other")
	setSubscriptionNodeKey(t, store, subB, otherNode.NodeKey())
	ownerA, err := store.GetProxyByIdentity(address, storage.SourceSubscription, subA)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(owner-a): %v", err)
	}
	m := &Manager{storage: store, singbox: shard}

	if err := m.DeleteManagedProxy(ownerA.ID); err != nil {
		t.Fatalf("DeleteManagedProxy(owner-a different-key peer): %v", err)
	}
	assertRuntimeMissingNodeKey(t, shard, targetNode.NodeKey())
}

// TestDeleteManagedProxyKeepsLegacyEmptyNodeKeyAtSameAddress 验证单行删除时，
// 其它空 key 旧行仍按相同地址作为兼容 owner 保留运行态。
func TestDeleteManagedProxyKeepsLegacyEmptyNodeKeyAtSameAddress(t *testing.T) {
	store := newTestStorage(t)
	subA := addNodeKeyTestSubscription(t, store, "owner-a", writeSubscriptionFile(t,
		"trojan://password@shared.example.com:443?sni=shared.example.com#shared"))
	subB := addNodeKeyTestSubscription(t, store, "owner-b", writeSubscriptionFile(t,
		"trojan://password@shared.example.com:443?sni=shared.example.com#shared"))
	shard, targetNode, address := seedSharedNodeKeyRuntime(t, store, subA, subB)
	setSubscriptionNodeKey(t, store, subB, "")
	ownerA, err := store.GetProxyByIdentity(address, storage.SourceSubscription, subA)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(owner-a): %v", err)
	}
	m := &Manager{storage: store, singbox: shard}

	if err := m.DeleteManagedProxy(ownerA.ID); err != nil {
		t.Fatalf("DeleteManagedProxy(owner-a legacy peer): %v", err)
	}
	assertRuntimeNodeKey(t, shard, targetNode.NodeKey())
}

// TestDeleteManagedProxyOwnerCheckFailureKeepsRuntime 验证无法查询 owner 时
// 必须直接失败，不能先改变 sing-box 运行态。
func TestDeleteManagedProxyOwnerCheckFailureKeepsRuntime(t *testing.T) {
	store := newTestStorage(t)
	subID := addNodeKeyTestSubscription(t, store, "owner", writeSubscriptionFile(t,
		"trojan://password@shared.example.com:443?sni=shared.example.com#shared"))
	shard, node, address := seedSharedNodeKeyRuntime(t, store, subID)
	proxy, err := store.GetProxyByIdentity(address, storage.SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(): %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close storage for injected owner query failure: %v", err)
	}
	m := &Manager{storage: store, singbox: shard}

	if err := m.deleteProxyWithRuntime(proxy); err == nil {
		t.Fatal("deleteProxyWithRuntime() returned nil after owner query failure")
	}
	assertRuntimeNodeKey(t, shard, node.NodeKey())
}

// TestGetProxyByNodeKeyFailsClosedForCrossSubscriptionOwners 锁定全局 pin 的歧义合同：
// 同一 NodeKey 存在多个订阅 owner 时必须返回明确错误，禁止任选一行。
func TestGetProxyByNodeKeyFailsClosedForCrossSubscriptionOwners(t *testing.T) {
	store := newTestStorage(t)
	subA := addNodeKeyTestSubscription(t, store, "owner-a", writeSubscriptionFile(t,
		"trojan://password@shared.example.com:443?sni=shared.example.com#shared"))
	subB := addNodeKeyTestSubscription(t, store, "owner-b", writeSubscriptionFile(t,
		"trojan://password@shared.example.com:443?sni=shared.example.com#shared"))
	_, node, _ := seedSharedNodeKeyRuntime(t, store, subA, subB)

	proxy, err := store.GetProxyByNodeKey(node.NodeKey())
	if err == nil || proxy != nil {
		t.Fatalf("GetProxyByNodeKey() = %#v, %v；多 owner 时应 fail-closed", proxy, err)
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("GetProxyByNodeKey() error = %v，缺少歧义信号", err)
	}
}

func addNodeKeyTestSubscription(t *testing.T, store *storage.Storage, name, filePath string) int64 {
	t.Helper()
	id, err := store.AddSubscription(name, "", filePath, "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription(%s): %v", name, err)
	}
	return id
}

func addRejectingDirectNodeKeyTestSubscription(t *testing.T, store *storage.Storage, name string) (int64, string) {
	t.Helper()
	proxyAddr, validateURL := startRejectingHTTPProxy(t)
	host, portText, err := net.SplitHostPort(proxyAddr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", proxyAddr, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("Atoi(%q): %v", portText, err)
	}
	filePath := writeSubscriptionFile(t, fmt.Sprintf(
		"proxies:\n  - name: direct\n    type: http\n    server: %s\n    port: %d\n",
		host, port,
	))
	return addNodeKeyTestSubscription(t, store, name, filePath), validateURL
}

func setSubscriptionNodeKey(t *testing.T, store *storage.Storage, subscriptionID int64, nodeKey string) {
	t.Helper()
	res, err := store.GetDB().Exec(
		`UPDATE proxies SET node_key = ? WHERE source = ? AND subscription_id = ?`,
		nodeKey, storage.SourceSubscription, subscriptionID,
	)
	if err != nil {
		t.Fatalf("set subscription %d node_key: %v", subscriptionID, err)
	}
	updated, err := res.RowsAffected()
	if err != nil {
		t.Fatalf("RowsAffected(subscription %d): %v", subscriptionID, err)
	}
	if updated != 1 {
		t.Fatalf("set subscription %d node_key affected %d rows, want 1", subscriptionID, updated)
	}
}

func seedSharedNodeKeyRuntime(t *testing.T, store *storage.Storage, subIDs ...int64) (*spyShard, ParsedNode, string) {
	t.Helper()
	node := tunnelNode("shared", "shared.example.com", "password")
	shard := newSpyShard()
	if err := shard.Reload([]ParsedNode{node}); err != nil {
		t.Fatalf("seed Reload(): %v", err)
	}
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(shard.GetPortMap()[node.NodeKey()]))
	for _, subID := range subIDs {
		if _, err := store.GetDB().Exec(
			`INSERT INTO proxies (address, protocol, source, subscription_id, status, node_key)
			 VALUES (?, 'socks5', ?, ?, 'active', ?)`,
			address, storage.SourceSubscription, subID, node.NodeKey(),
		); err != nil {
			t.Fatalf("seed shared proxy for subscription %d: %v", subID, err)
		}
	}
	return shard, node, address
}

func assertRuntimeNodeKey(t *testing.T, shard singBoxShard, nodeKey string) {
	t.Helper()
	for _, node := range shard.GetNodes() {
		if node.NodeKey() == nodeKey {
			return
		}
	}
	t.Fatalf("运行态缺少仍有 owner 的 NodeKey %q；portMap=%v", nodeKey, shard.GetPortMap())
}

func assertRuntimeMissingNodeKey(t *testing.T, shard singBoxShard, nodeKey string) {
	t.Helper()
	for _, node := range shard.GetNodes() {
		if node.NodeKey() == nodeKey {
			t.Fatalf("运行态仍包含应回收的 NodeKey %q；portMap=%v", nodeKey, shard.GetPortMap())
		}
	}
}
