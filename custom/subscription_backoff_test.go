package custom

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestSubscriptionRefreshFailureIsBackedOff(t *testing.T) {
	store := newTestStorage(t)
	if _, err := store.AddSubscription("failing", "http://127.0.0.1/subscription", "", "auto", 1, ""); err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	m := &Manager{storage: store}

	for range 5 {
		m.checkAndRefresh()
	}

	sub, err := store.GetSubscriptions()
	if err != nil {
		t.Fatalf("GetSubscriptions() error = %v", err)
	}
	if len(sub) != 1 || sub[0].LastFetch.IsZero() {
		t.Fatalf("failed refresh must record last_fetch to suppress repeated scheduler attempts, got %+v", sub)
	}
}

func TestEmptySubscriptionRefreshRecordsAttempt(t *testing.T) {
	store := newTestStorage(t)
	_, err := store.AddSubscription("empty", "", writeSubscriptionFile(t, "proxies: []"), "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	m := &Manager{storage: store, singbox: newSpyShard()}
	m.checkAndRefresh()
	subs, err := store.GetSubscriptions()
	if err != nil || len(subs) != 1 || subs[0].LastFetch.IsZero() {
		t.Fatalf("empty refresh attempt was not recorded: subs=%+v err=%v", subs, err)
	}
}

func TestCollectTunnelNodesDoesNotRefetchBackedOffSubscription(t *testing.T) {
	store := newTestStorage(t)
	excludedID, err := store.AddSubscription("current", "", writeSubscriptionFile(t, "proxies: []"), "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription(current) error = %v", err)
	}
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("proxies: []"))
	}))
	defer srv.Close()
	backedOffID, err := store.AddSubscription("backed-off", srv.URL, "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription(backed-off) error = %v", err)
	}
	if err := store.UpdateSubscriptionAttempt(backedOffID); err != nil {
		t.Fatalf("UpdateSubscriptionAttempt() error = %v", err)
	}

	oldCheck := subscriptionURLTargetCheck
	oldDial := subscriptionDialContextFn
	t.Cleanup(func() {
		subscriptionURLTargetCheck = oldCheck
		subscriptionDialContextFn = oldDial
	})
	subscriptionURLTargetCheck = func(string) error { return nil }
	subscriptionDialContextFn = (&net.Dialer{}).DialContext

	m := &Manager{storage: store, singbox: newSpyShard()}
	if _, err := m.collectAllTunnelNodesExcludingSubscription(excludedID, nil); err != nil {
		t.Fatalf("collectAllTunnelNodesExcludingSubscription() error = %v", err)
	}
	// BUG-05：collect 不再旁路 re-fetch 其它订阅；backed-off URL 也不得被触达。
	if got := hits.Load(); got != 0 {
		t.Fatalf("subscription fetched %d times via collect path, want 0", got)
	}
}

// TestRefreshSubscriptionRejectsPausedBeforeNetworkFetch 锁定暂停订阅的手工刷新边界：
// 状态已暂停时不得触达订阅 URL、解析内容或触发验证，避免用户明确暂停后仍产生外部请求。
func TestRefreshSubscriptionRejectsPausedBeforeNetworkFetch(t *testing.T) {
	store := newTestStorage(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("proxies: []"))
	}))
	defer srv.Close()

	subID, err := store.AddSubscription("paused", srv.URL, "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if err := store.PauseSubscription(subID); err != nil {
		t.Fatalf("PauseSubscription() error = %v", err)
	}

	oldCheck := subscriptionURLTargetCheck
	oldDial := subscriptionDialContextFn
	t.Cleanup(func() {
		subscriptionURLTargetCheck = oldCheck
		subscriptionDialContextFn = oldDial
	})
	subscriptionURLTargetCheck = func(string) error { return nil }
	subscriptionDialContextFn = (&net.Dialer{}).DialContext

	m := &Manager{storage: store, singbox: newSpyShard()}
	if err := m.RefreshSubscription(subID); err == nil {
		t.Fatal("RefreshSubscription(paused) error = nil, want paused rejection")
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("paused subscription was fetched %d times, want 0", got)
	}
}
