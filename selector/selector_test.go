package selector

import (
	"errors"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/affinity"
	"github.com/babutree/GeoProxy/auth"
	"github.com/babutree/GeoProxy/storage"
)

type fakeStore struct {
	proxies    []storage.Proxy
	pausedSubs map[int64]bool
	subErr     map[int64]error
}

type nilBoundProxyStore struct {
	fakeStore
}

func (s nilBoundProxyStore) GetProxyByID(int64) (*storage.Proxy, error) {
	return nil, nil
}

func (s fakeStore) GetByRegion(region string, excludes []int64) ([]storage.Proxy, error) {
	excluded := map[int64]bool{}
	for _, id := range excludes {
		excluded[id] = true
	}

	var out []storage.Proxy
	for _, proxy := range s.proxies {
		if excluded[proxy.ID] || !proxyAvailable(proxy) {
			continue
		}
		if region != "" && proxy.Region != region {
			continue
		}
		if proxy.SubscriptionID > 0 && s.pausedSubs[proxy.SubscriptionID] {
			continue
		}
		out = append(out, proxy)
	}
	return out, nil
}

func (s fakeStore) GetProxyByID(id int64) (*storage.Proxy, error) {
	for _, proxy := range s.proxies {
		if proxy.ID == id {
			copy := proxy
			return &copy, nil
		}
	}
	return nil, errors.New("not found")
}

func (s fakeStore) IsSubscriptionPaused(id int64) (bool, error) {
	if id <= 0 {
		return false, nil
	}
	if err, ok := s.subErr[id]; ok {
		return false, err
	}
	return s.pausedSubs[id], nil
}

func TestPickHonorsRequestedRegionWithoutFallback(t *testing.T) {
	store := fakeStore{proxies: []storage.Proxy{{ID: 1, Address: "jp:8080", Region: "jp", Status: "active"}}}

	_, err := Pick(store, "us", nil)

	if !errors.Is(err, ErrNoNode) || err.Error() != "no available node for region: us" {
		t.Fatalf("Pick() err = %v, want region-specific ErrNoNode", err)
	}
}

func TestPickReturnsLowestLatencyAndExcludesTried(t *testing.T) {
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-slow:8080", Region: "us", Latency: 80, Status: "active"},
		{ID: 2, Address: "us-fast:8080", Region: "us", Latency: 20, Status: "active"},
	}}

	proxy, err := Pick(store, "us", []int64{2})
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	if proxy.Address != "us-slow:8080" {
		t.Fatalf("Pick() = %s, want us-slow:8080", proxy.Address)
	}
}

func TestResolveRebindsFailedStickyNodeInSameRegion(t *testing.T) {
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-old:8080", Region: "us", Latency: 10, Status: "active"},
		{ID: 2, Address: "us-new:8080", Region: "us", Latency: 20, Status: "active"},
		{ID: 3, Address: "jp:8080", Region: "jp", Latency: 1, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, time.Now)
	sessions.SetProxy("abc", 1, "us-old:8080", "us")

	proxy, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "abc"}, []int64{1})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if proxy.Address != "us-new:8080" {
		t.Fatalf("Resolve() = %s, want us-new:8080", proxy.Address)
	}
	binding, ok := sessions.Get("abc")
	if !ok || binding.ProxyID != 2 || binding.NodeAddress != "us-new:8080" || binding.Region != "us" {
		t.Fatalf("binding = %#v, %v; want us-new binding", binding, ok)
	}
}

func TestResolveSessionOnlyRebindUsesBoundRegion(t *testing.T) {
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-old:8080", Region: "us", Latency: 10, Status: "active"},
		{ID: 2, Address: "us-new:8080", Region: "us", Latency: 20, Status: "active"},
		{ID: 3, Address: "jp-fast:8080", Region: "jp", Latency: 1, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, time.Now)
	sessions.SetProxy("abc", 1, "us-old:8080", "us")

	proxy, err := Resolve(store, sessions, auth.ParsedUsername{Session: "abc"}, []int64{1})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if proxy.Address != "us-new:8080" {
		t.Fatalf("Resolve() = %s, want us-new:8080", proxy.Address)
	}
}

// 同一 session 首次绑定应稳定映射到同一节点（可重复、与顺序无关）。
func TestResolveSameSessionStableFirstBind(t *testing.T) {
	proxies := []storage.Proxy{
		{ID: 1, Address: "jp1:8080", Region: "jp", Latency: 10, Status: "active"},
		{ID: 2, Address: "jp2:8080", Region: "jp", Latency: 20, Status: "active"},
		{ID: 3, Address: "jp3:8080", Region: "jp", Latency: 30, Status: "active"},
	}
	var first string
	for i := 0; i < 5; i++ {
		store := fakeStore{proxies: proxies}
		sessions := affinity.NewWithClock(10*time.Minute, time.Now)
		proxy, err := Resolve(store, sessions, auth.ParsedUsername{Region: "jp", Session: "cc01"}, nil)
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if first == "" {
			first = proxy.Address
		} else if proxy.Address != first {
			t.Fatalf("same session mapped to different nodes: %s vs %s", first, proxy.Address)
		}
	}
}

// 不同 session 应能分散到 top-K 内的不同节点，而非全部收敛到延迟最低的那个。
func TestResolveDifferentSessionsSpread(t *testing.T) {
	proxies := []storage.Proxy{
		{ID: 1, Address: "jp1:8080", Region: "jp", Latency: 10, Status: "active"},
		{ID: 2, Address: "jp2:8080", Region: "jp", Latency: 20, Status: "active"},
		{ID: 3, Address: "jp3:8080", Region: "jp", Latency: 30, Status: "active"},
	}
	seen := map[string]bool{}
	for _, sess := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		store := fakeStore{proxies: proxies}
		sessions := affinity.NewWithClock(10*time.Minute, time.Now)
		proxy, err := Resolve(store, sessions, auth.ParsedUsername{Region: "jp", Session: sess}, nil)
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		seen[proxy.Address] = true
	}
	if len(seen) < 2 {
		t.Fatalf("sessions did not spread across nodes, only saw: %v", seen)
	}
}

func TestResolveRebindsWhenStoreReturnsNilProxyWithoutError(t *testing.T) {
	store := nilBoundProxyStore{fakeStore{proxies: []storage.Proxy{
		{ID: 2, Address: "us-new:8080", Region: "us", Latency: 20, Status: "active"},
	}}}
	sessions := affinity.NewWithClock(10*time.Minute, time.Now)
	sessions.SetProxy("abc", 1, "us-old:8080", "us")

	proxy, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "abc"}, nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if proxy.ID != 2 {
		t.Fatalf("Resolve() proxy ID = %d, want rebound ID 2", proxy.ID)
	}
}

func TestResolveRejectsStickyWhenUserPaused(t *testing.T) {
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-old:8080", Region: "us", Latency: 10, Status: "active", UserPaused: true},
		{ID: 2, Address: "us-new:8080", Region: "us", Latency: 20, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, time.Now)
	sessions.SetProxy("abc", 1, "us-old:8080", "us")

	proxy, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "abc"}, nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if proxy.ID != 2 {
		t.Fatalf("Resolve() proxy ID = %d, want rebind to 2 after user_paused sticky", proxy.ID)
	}
}

func TestResolveRejectsStickyWhenParentSubscriptionPaused(t *testing.T) {
	store := fakeStore{
		proxies: []storage.Proxy{
			{ID: 1, Address: "us-old:8080", Region: "us", Latency: 10, Status: "active", Source: storage.SourceSubscription, SubscriptionID: 9},
			{ID: 2, Address: "us-new:8080", Region: "us", Latency: 20, Status: "active"},
		},
		pausedSubs: map[int64]bool{9: true},
	}
	sessions := affinity.NewWithClock(10*time.Minute, time.Now)
	sessions.SetProxy("abc", 1, "us-old:8080", "us")

	proxy, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "abc"}, nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if proxy.ID != 2 {
		t.Fatalf("Resolve() proxy ID = %d, want rebind to 2 after parent subscription paused", proxy.ID)
	}
}

func TestPickForSessionStableForSharedAddressAcrossInputOrder(t *testing.T) {
	first := storage.Proxy{ID: 10, Address: "shared:8080", Region: "us", Latency: 10, Status: "active"}
	second := storage.Proxy{ID: 20, Address: "shared:8080", Region: "us", Latency: 10, Status: "active"}

	pickedA, err := pickForSession(fakeStore{proxies: []storage.Proxy{first, second}}, nil, "us", "stable", nil, 0, 0, nil)
	if err != nil {
		t.Fatalf("first pickForSession() error = %v", err)
	}
	pickedB, err := pickForSession(fakeStore{proxies: []storage.Proxy{second, first}}, nil, "us", "stable", nil, 0, 0, nil)
	if err != nil {
		t.Fatalf("second pickForSession() error = %v", err)
	}
	if pickedA.ID != pickedB.ID {
		t.Fatalf("same session/shared address picked IDs %d and %d after input reorder", pickedA.ID, pickedB.ID)
	}
}
