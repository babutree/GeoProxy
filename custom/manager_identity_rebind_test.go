package custom

import (
	"fmt"
	"strings"
	"testing"

	"github.com/babutree/GeoProxy/storage"
)

func managerIdentityTestSubscription(t *testing.T, store *storage.Storage) int64 {
	t.Helper()
	url := "https://example.test/manager-identity-" + strings.ReplaceAll(t.Name(), "/", "-")
	id, err := store.AddSubscription("manager identity", url, "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	return id
}

func seedManagerSubscriptionProxy(t *testing.T, store *storage.Storage, subID int64, address, nodeKey string, paused bool) int64 {
	t.Helper()
	userPaused := 0
	if paused {
		userPaused = 1
	}
	result, err := store.GetDB().Exec(`
		INSERT INTO proxies (address, protocol, source, subscription_id, region_source, status, user_paused, node_key)
		VALUES (?, 'http', ?, ?, 'manual', 'active', ?, ?)
	`, address, storage.SourceSubscription, subID, userPaused, nodeKey)
	if err != nil {
		t.Fatalf("seed proxy %s: %v", address, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("seed proxy %s LastInsertId: %v", address, err)
	}
	return id
}

func proxyByIDInTest(t *testing.T, store *storage.Storage, id int64) *storage.Proxy {
	t.Helper()
	proxy, err := store.GetProxyByID(id)
	if err != nil {
		t.Fatalf("GetProxyByID(%d): %v", id, err)
	}
	return proxy
}

// TestReplaceSubscriptionProxiesReturnsDatabaseIDsForNewRows 锁定多条新增代理的实体身份。
func TestReplaceSubscriptionProxiesReturnsDatabaseIDsForNewRows(t *testing.T) {
	store := newTestStorage(t)
	subID := managerIdentityTestSubscription(t, store)
	m := &Manager{storage: store}
	entry := subscriptionProxyEntry{
		addr: "insert-id.example:8080", proto: "http", username: "u", password: "p", nodeKey: "insert-id-key",
	}
	if err := store.AddProxyWithSource(entry.addr, "http", storage.SourceManual); err != nil {
		t.Fatalf("seed same-address manual proxy: %v", err)
	}
	other := subscriptionProxyEntry{
		addr: "insert-id-other.example:8080", proto: "socks5", nodeKey: "insert-id-other-key",
	}

	proxies, err := m.replaceSubscriptionProxies(subID, []subscriptionProxyEntry{entry, other})
	if err != nil {
		t.Fatalf("replaceSubscriptionProxies() error = %v", err)
	}
	if len(proxies) != 2 {
		t.Fatalf("returned proxies = %d, want 2", len(proxies))
	}
	if proxies[0].ID == 0 || proxies[1].ID == 0 {
		t.Fatalf("returned IDs = %d/%d, want non-zero database identities", proxies[0].ID, proxies[1].ID)
	}
	if proxies[0].ID == proxies[1].ID {
		t.Fatalf("returned IDs = %d/%d, want distinct database rows", proxies[0].ID, proxies[1].ID)
	}
	for _, proxy := range proxies {
		row := proxyByIDInTest(t, store, proxy.ID)
		if row.ID != proxy.ID || row.Address != proxy.Address || row.NodeKey != proxy.NodeKey || row.Source != storage.SourceSubscription || row.SubscriptionID != subID {
			t.Fatalf("database row = %+v, want returned proxy identity %+v", row, proxy)
		}
	}
}

// TestReplaceSubscriptionProxiesKeyedUpdatePreservesIDAndPaused 验证按 key 刷新不会替换原行。
func TestReplaceSubscriptionProxiesKeyedUpdatePreservesIDAndPaused(t *testing.T) {
	store := newTestStorage(t)
	subID := managerIdentityTestSubscription(t, store)
	oldID := seedManagerSubscriptionProxy(t, store, subID, "old-keyed.example:8080", "keyed-id", true)
	m := &Manager{storage: store}

	proxies, err := m.replaceSubscriptionProxies(subID, []subscriptionProxyEntry{{
		addr: "new-keyed.example:9090", proto: "socks5", nodeKey: "keyed-id",
	}})
	if err != nil {
		t.Fatalf("replaceSubscriptionProxies() error = %v", err)
	}
	if len(proxies) != 1 || proxies[0].ID != oldID {
		t.Fatalf("returned proxy = %+v, want existing ID %d", proxies, oldID)
	}
	row := proxyByIDInTest(t, store, oldID)
	if row.Address != "new-keyed.example:9090" || row.NodeKey != "keyed-id" || !row.UserPaused {
		t.Fatalf("keyed row after refresh = %+v, want new address and paused preservation", row)
	}
}

// TestReplaceSubscriptionProxiesLegacyAddressReturnsReplacementID 验证无 NodeKey 旧行删除重插后的身份与暂停状态。
func TestReplaceSubscriptionProxiesLegacyAddressReturnsReplacementID(t *testing.T) {
	store := newTestStorage(t)
	subID := managerIdentityTestSubscription(t, store)
	oldID := seedManagerSubscriptionProxy(t, store, subID, "legacy-address.example:8080", "", true)
	m := &Manager{storage: store}

	proxies, err := m.replaceSubscriptionProxies(subID, []subscriptionProxyEntry{{
		addr: "legacy-address.example:8080", proto: "http",
	}})
	if err != nil {
		t.Fatalf("replaceSubscriptionProxies() error = %v", err)
	}
	if len(proxies) != 1 || proxies[0].ID == 0 || proxies[0].ID == oldID {
		t.Fatalf("returned proxy = %+v, want non-zero replacement ID different from %d", proxies, oldID)
	}
	if _, err := store.GetProxyByID(oldID); err == nil {
		t.Fatalf("legacy proxy old ID %d still exists after delete/reinsert", oldID)
	}
	row := proxyByIDInTest(t, store, proxies[0].ID)
	if row.Address != "legacy-address.example:8080" || row.NodeKey != "" || !row.UserPaused || row.Status != "disabled" {
		t.Fatalf("legacy replacement row = %+v, want address identity with paused state", row)
	}
}

// TestReplaceSubscriptionProxiesRebindsSwappedPortsByNodeKey 验证按 NodeKey 两阶段交换地址。
func TestReplaceSubscriptionProxiesRebindsSwappedPortsByNodeKey(t *testing.T) {
	store := newTestStorage(t)
	subID := managerIdentityTestSubscription(t, store)
	leftID := seedManagerSubscriptionProxy(t, store, subID, "swap-left.example:8080", "swap-left", true)
	rightID := seedManagerSubscriptionProxy(t, store, subID, "swap-right.example:8080", "swap-right", false)
	m := &Manager{storage: store}

	proxies, err := m.replaceSubscriptionProxies(subID, []subscriptionProxyEntry{
		{addr: "swap-right.example:8080", proto: "http", nodeKey: "swap-left"},
		{addr: "swap-left.example:8080", proto: "http", nodeKey: "swap-right"},
	})
	if err != nil {
		t.Fatalf("replaceSubscriptionProxies() port swap error = %v", err)
	}
	if len(proxies) != 2 {
		t.Fatalf("returned proxies = %d, want 2", len(proxies))
	}
	byKey := map[string]storage.Proxy{}
	for _, proxy := range proxies {
		if proxy.ID == 0 {
			t.Fatalf("returned proxy %q has zero ID", proxy.NodeKey)
		}
		byKey[proxy.NodeKey] = proxy
	}
	if byKey["swap-left"].ID != leftID || byKey["swap-right"].ID != rightID {
		t.Fatalf("IDs by key = left:%d right:%d, want left:%d right:%d", byKey["swap-left"].ID, byKey["swap-right"].ID, leftID, rightID)
	}
	if byKey["swap-left"].Address != "swap-right.example:8080" || byKey["swap-right"].Address != "swap-left.example:8080" {
		t.Fatalf("returned addresses by key = left:%q right:%q", byKey["swap-left"].Address, byKey["swap-right"].Address)
	}
	if !byKey["swap-left"].UserPaused || byKey["swap-right"].UserPaused {
		t.Fatalf("paused flags by key = left:%v right:%v, want true/false", byKey["swap-left"].UserPaused, byKey["swap-right"].UserPaused)
	}
	left := proxyByIDInTest(t, store, leftID)
	right := proxyByIDInTest(t, store, rightID)
	if left.Address != "swap-right.example:8080" || left.NodeKey != "swap-left" || !left.UserPaused {
		t.Fatalf("database left row = %+v, want swapped address with original identity/paused", left)
	}
	if right.Address != "swap-left.example:8080" || right.NodeKey != "swap-right" || right.UserPaused {
		t.Fatalf("database right row = %+v, want swapped address with original identity/unpaused", right)
	}
	for _, row := range []*storage.Proxy{left, right} {
		if strings.HasPrefix(row.Address, "rebind-") {
			t.Fatalf("temporary rebind address leaked for id %d: %q", row.ID, row.Address)
		}
	}
	var leaked int
	if err := store.GetDB().QueryRow(`SELECT COUNT(*) FROM proxies WHERE address LIKE 'rebind-%'`).Scan(&leaked); err != nil {
		t.Fatalf("count temporary rebind rows: %v", err)
	}
	if leaked != 0 {
		t.Fatalf("temporary rebind rows = %d, want 0", leaked)
	}
}

// TestReplaceSubscriptionProxiesRollsBackRebindOnMidBatchFailure 验证批次中途失败时事务补偿。
func TestReplaceSubscriptionProxiesRollsBackRebindOnMidBatchFailure(t *testing.T) {
	store := newTestStorage(t)
	subID := managerIdentityTestSubscription(t, store)
	leftID := seedManagerSubscriptionProxy(t, store, subID, "rollback-left.example:8080", "rollback-left", true)
	rightID := seedManagerSubscriptionProxy(t, store, subID, "rollback-right.example:8080", "rollback-right", false)
	if _, err := store.GetDB().Exec(fmt.Sprintf(`
		CREATE TRIGGER fail_rebind_final
		BEFORE UPDATE OF address ON proxies
		WHEN NEW.address = 'rollback-right.example:8080' AND NEW.subscription_id = %d
		BEGIN
			SELECT RAISE(ABORT, 'injected rebind final update failure');
		END
	`, subID)); err != nil {
		t.Fatalf("create rebind failure trigger: %v", err)
	}

	m := &Manager{storage: store}
	_, err := m.replaceSubscriptionProxies(subID, []subscriptionProxyEntry{
		{addr: "rollback-right.example:8080", proto: "socks5", dual: true, username: "new-left", password: "secret-left", nodeKey: "rollback-left"},
		{addr: "rollback-left.example:8080", proto: "socks5", dual: true, username: "new-right", password: "secret-right", nodeKey: "rollback-right"},
	})
	if err == nil || !strings.Contains(err.Error(), "injected rebind final update failure") {
		t.Fatalf("replaceSubscriptionProxies() error = %v, want injected failure", err)
	}
	left := proxyByIDInTest(t, store, leftID)
	right := proxyByIDInTest(t, store, rightID)
	if left.Address != "rollback-left.example:8080" || right.Address != "rollback-right.example:8080" {
		t.Fatalf("addresses after rollback = %q/%q, want original addresses", left.Address, right.Address)
	}
	if !left.UserPaused || right.UserPaused {
		t.Fatalf("paused flags after rollback = %v/%v, want true/false", left.UserPaused, right.UserPaused)
	}
	for _, row := range []*storage.Proxy{left, right} {
		if row.Status != "active" || row.Protocol != "http" || row.DualProtocol || row.Username != "" || row.Password != "" || row.RegionSource != "manual" {
			t.Fatalf("row %d retained partial rebind state after rollback: %+v", row.ID, row)
		}
	}
	subscription, err := store.GetSubscription(subID)
	if err != nil {
		t.Fatalf("GetSubscription(%d): %v", subID, err)
	}
	if subscription.ProxyCount != 0 || !subscription.LastFetch.IsZero() {
		t.Fatalf("subscription metadata changed after rollback: count=%d last_fetch=%v", subscription.ProxyCount, subscription.LastFetch)
	}
	var leaked int
	if err := store.GetDB().QueryRow(`SELECT COUNT(*) FROM proxies WHERE address LIKE 'rebind-%'`).Scan(&leaked); err != nil {
		t.Fatalf("count temporary rebind rows after rollback: %v", err)
	}
	if leaked != 0 {
		t.Fatalf("temporary rebind rows after rollback = %d, want 0", leaked)
	}
}
