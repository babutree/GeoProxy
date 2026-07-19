package storage

import (
	"database/sql"
	"errors"
	"testing"
)

// TestEnablePathsRejectOrphanSubscriptionProxy 验证所有启用入口都不能把
// 父订阅已经不存在的孤儿节点恢复为 active。
func TestEnablePathsRejectOrphanSubscriptionProxy(t *testing.T) {
	tests := []struct {
		name   string
		enable func(*Storage, *Proxy) error
	}{
		{
			name: "address",
			enable: func(store *Storage, proxy *Proxy) error {
				return store.EnableProxy(proxy.Address)
			},
		},
		{
			name: "id",
			enable: func(store *Storage, proxy *Proxy) error {
				return store.EnableProxyByID(proxy.ID)
			},
		},
		{
			name: "subscription identity",
			enable: func(store *Storage, proxy *Proxy) error {
				return store.EnableSubscriptionProxy(proxy.Address, proxy.SubscriptionID)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newTestStorage(t)
			subID, err := store.AddSubscription("orphan", "https://example.test/orphan-"+test.name, "", "auto", 60, "")
			if err != nil {
				t.Fatalf("AddSubscription(): %v", err)
			}
			address := "orphan-" + test.name + ":8080"
			if err := store.AddProxyWithSource(address, "http", SourceSubscription, subID); err != nil {
				t.Fatalf("AddProxyWithSource(): %v", err)
			}
			proxy, err := store.GetProxyByIdentity(address, SourceSubscription, subID)
			if err != nil {
				t.Fatalf("GetProxyByIdentity(): %v", err)
			}
			if err := store.DisableSubscriptionProxy(address, subID); err != nil {
				t.Fatalf("DisableSubscriptionProxy(): %v", err)
			}
			if _, err := store.GetDB().Exec("DELETE FROM subscriptions WHERE id = ?", subID); err != nil {
				t.Fatalf("delete parent subscription: %v", err)
			}

			if err := test.enable(store, proxy); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("enable orphan error = %v, want sql.ErrNoRows", err)
			}
			after, err := store.GetProxyByIdentity(address, SourceSubscription, subID)
			if err != nil {
				t.Fatalf("GetProxyByIdentity() after enable: %v", err)
			}
			if after.Status != "disabled" {
				t.Fatalf("orphan status = %q, want disabled", after.Status)
			}
		})
	}
}

// TestEnableProxyStillAllowsManualNodeWithoutSubscription 是正向对照：
// 手工节点没有父订阅，收紧 subscription source 不能误伤该合法路径。
func TestEnableProxyStillAllowsManualNodeWithoutSubscription(t *testing.T) {
	store := newTestStorage(t)
	const address = "manual-enable:8080"
	if err := store.AddProxy(address, "http"); err != nil {
		t.Fatalf("AddProxy(): %v", err)
	}
	proxy, err := store.GetProxyByIdentity(address, SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(): %v", err)
	}
	if err := store.DisableProxyByID(proxy.ID); err != nil {
		t.Fatalf("DisableProxyByID(): %v", err)
	}
	if err := store.EnableProxyByID(proxy.ID); err != nil {
		t.Fatalf("EnableProxyByID(): %v", err)
	}
	after, err := store.GetProxyByID(proxy.ID)
	if err != nil {
		t.Fatalf("GetProxyByID(): %v", err)
	}
	if after.Status != "active" {
		t.Fatalf("manual status = %q, want active", after.Status)
	}
}

// TestAvailableQueriesExcludeOrphanSubscriptionProxy 覆盖所有共享“可用节点”
// 作用域入口：孤儿订阅节点不得出现在选路、健康批次、统计或只读 API 中。
func TestAvailableQueriesExcludeOrphanSubscriptionProxy(t *testing.T) {
	store := newTestStorage(t)
	subID, err := store.AddSubscription("orphan-visible", "https://example.test/orphan-visible", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription(): %v", err)
	}
	const address = "orphan-visible:8080"
	if err := store.AddProxyWithSource(address, "http", SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource(): %v", err)
	}
	if _, err := store.GetDB().Exec(
		"UPDATE proxies SET region = 'jp', latency = 25, quality_grade = 'A' WHERE address = ? AND subscription_id = ?",
		address, subID,
	); err != nil {
		t.Fatalf("seed orphan metadata: %v", err)
	}
	if _, err := store.GetDB().Exec("DELETE FROM subscriptions WHERE id = ?", subID); err != nil {
		t.Fatalf("delete parent subscription: %v", err)
	}

	assertNoProxies := func(name string, proxies []Proxy, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		if len(proxies) != 0 {
			t.Fatalf("%s returned orphan proxies: %#v", name, proxies)
		}
	}
	assertZero := func(name string, count int, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		if count != 0 {
			t.Fatalf("%s = %d, want 0", name, count)
		}
	}

	if proxy, err := store.GetRandom(); err == nil || proxy != nil {
		t.Fatalf("GetRandom() = %#v, %v; want no available proxy", proxy, err)
	}
	proxies, err := store.GetAll()
	assertNoProxies("GetAll", proxies, err)
	proxies, err = store.GetBatchForHealthCheck(10)
	assertNoProxies("GetBatchForHealthCheck", proxies, err)
	proxies, err = store.GetByProtocol("http")
	assertNoProxies("GetByProtocol", proxies, err)
	proxies, err = store.GetByRegion("jp", nil)
	assertNoProxies("GetByRegion", proxies, err)

	regions, err := store.CountByRegion()
	if err != nil {
		t.Fatalf("CountByRegion(): %v", err)
	}
	if len(regions) != 0 {
		t.Fatalf("CountByRegion() = %#v, want empty", regions)
	}
	regionRows, err := store.GetRegionsWithCount()
	if err != nil {
		t.Fatalf("GetRegionsWithCount(): %v", err)
	}
	if len(regionRows) != 0 {
		t.Fatalf("GetRegionsWithCount() = %#v, want empty", regionRows)
	}

	total, err := store.CountAll()
	assertZero("CountAll", total, err)
	httpCount, err := store.CountAvailableByProtocol("http")
	assertZero("CountAvailableByProtocol", httpCount, err)
	subCount, err := store.CountBySource(SourceSubscription)
	assertZero("CountBySource", subCount, err)
	active, _, err := store.CountBySubscriptionID(subID)
	assertZero("CountBySubscriptionID active", active, err)
	quality, err := store.GetQualityDistribution()
	if err != nil {
		t.Fatalf("GetQualityDistribution(): %v", err)
	}
	if len(quality) != 0 {
		t.Fatalf("GetQualityDistribution() = %#v, want empty", quality)
	}
	average, err := store.GetAverageLatency("http")
	if err != nil {
		t.Fatalf("GetAverageLatency(): %v", err)
	}
	if average != 0 {
		t.Fatalf("GetAverageLatency() = %d, want 0", average)
	}

	nodes, nodeTotal, err := store.ListNodesForAPI(NodeAPIFilter{})
	if err != nil {
		t.Fatalf("ListNodesForAPI(): %v", err)
	}
	if nodeTotal != 0 || len(nodes) != 0 {
		t.Fatalf("ListNodesForAPI() total=%d nodes=%#v, want empty", nodeTotal, nodes)
	}
}
