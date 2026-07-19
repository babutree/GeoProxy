package selector

import (
	"errors"
	"fmt"
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

func (s fakeStore) GetProxyByAddress(address string) (*storage.Proxy, error) {
	for _, proxy := range s.proxies {
		if proxy.Address == address {
			copy := proxy
			return &copy, nil
		}
	}
	return nil, errors.New("not found")
}

func (s fakeStore) GetProxyByNodeKey(nodeKey string) (*storage.Proxy, error) {
	for _, proxy := range s.proxies {
		if proxy.NodeKey == nodeKey && nodeKey != "" {
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

// 新 session 必须在全部合格节点间分配，不能把较慢节点永久排除在固定 top-K 外。
func TestPickForSessionUsesAllEligibleNodes(t *testing.T) {
	proxies := make([]storage.Proxy, 0, 16)
	for id := int64(1); id <= 16; id++ {
		proxies = append(proxies, storage.Proxy{
			ID:      id,
			Address: fmt.Sprintf("jp-%02d:8080", id),
			Region:  "jp",
			Latency: int(id) * 10,
			Status:  "active",
		})
	}

	seen := make(map[int64]struct{})
	outsideOldTopFive := false
	for i := 0; i < 256; i++ {
		proxy, err := pickForSession(
			fakeStore{proxies: proxies}, nil, "jp", fmt.Sprintf("spread-%03d", i), nil, 0, 0, nil,
		)
		if err != nil {
			t.Fatalf("pickForSession() error = %v", err)
		}
		seen[proxy.ID] = struct{}{}
		outsideOldTopFive = outsideOldTopFive || proxy.ID > 5
	}

	if len(seen) <= 5 || !outsideOldTopFive {
		t.Fatalf("256 sessions covered IDs %v (count=%d), want >5 nodes including an ID outside old top-5", seen, len(seen))
	}
}

func TestPickForSessionUsesStableIDAcrossAddressChanges(t *testing.T) {
	before := []storage.Proxy{
		{ID: 10, Address: "a:8080", Region: "jp", Latency: 20, Status: "active"},
		{ID: 20, Address: "b:8080", Region: "jp", Latency: 20, Status: "active"},
	}
	after := []storage.Proxy{
		{ID: 10, Address: "z:8080", Region: "jp", Latency: 20, Status: "active"},
		{ID: 20, Address: "a:8080", Region: "jp", Latency: 20, Status: "active"},
	}

	pickedBefore, err := pickForSession(fakeStore{proxies: before}, nil, "jp", "stable-id", nil, 0, 0, nil)
	if err != nil {
		t.Fatalf("pickForSession(before) error = %v", err)
	}
	pickedAfter, err := pickForSession(fakeStore{proxies: after}, nil, "jp", "stable-id", nil, 0, 0, nil)
	if err != nil {
		t.Fatalf("pickForSession(after) error = %v", err)
	}
	if pickedBefore.ID != pickedAfter.ID {
		t.Fatalf("same session changed from ID %d to %d after address-only update", pickedBefore.ID, pickedAfter.ID)
	}
}

func TestPickForSessionFallsBackToNodeKeyWhenIDMissing(t *testing.T) {
	before := []storage.Proxy{
		{Address: "a:8080", NodeKey: "node-a", Region: "jp", Latency: 20, Status: "active"},
		{Address: "b:8080", NodeKey: "node-b", Region: "jp", Latency: 20, Status: "active"},
	}
	after := []storage.Proxy{
		{Address: "z:8080", NodeKey: "node-a", Region: "jp", Latency: 20, Status: "active"},
		{Address: "a:8080", NodeKey: "node-b", Region: "jp", Latency: 20, Status: "active"},
	}

	pickedBefore, err := pickForSession(fakeStore{proxies: before}, nil, "jp", "stable-key", nil, 0, 0, nil)
	if err != nil {
		t.Fatalf("pickForSession(before) error = %v", err)
	}
	pickedAfter, err := pickForSession(fakeStore{proxies: after}, nil, "jp", "stable-key", nil, 0, 0, nil)
	if err != nil {
		t.Fatalf("pickForSession(after) error = %v", err)
	}
	if pickedBefore.NodeKey != pickedAfter.NodeKey {
		t.Fatalf("same session changed from NodeKey %q to %q after address-only update", pickedBefore.NodeKey, pickedAfter.NodeKey)
	}
}

func TestPickForSessionLatencyWeightIsEffectiveButDoesNotStarve(t *testing.T) {
	const samples = 4096
	fastFirst := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "one:8080", Region: "jp", Latency: 20, Status: "active"},
		{ID: 2, Address: "two:8080", Region: "jp", Latency: 2000, Status: "active"},
	}}
	slowFirst := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "one:8080", Region: "jp", Latency: 2000, Status: "active"},
		{ID: 2, Address: "two:8080", Region: "jp", Latency: 20, Status: "active"},
	}}
	unknownFirst := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "one:8080", Region: "jp", Latency: 0, Status: "active"},
		{ID: 2, Address: "two:8080", Region: "jp", Latency: 20, Status: "active"},
	}}

	var fastIDOne, slowIDOne, unknownIDOne int
	for i := 0; i < samples; i++ {
		session := fmt.Sprintf("weight-%04d", i)
		picked, err := pickForSession(fastFirst, nil, "jp", session, nil, 0, 0, nil)
		if err != nil {
			t.Fatalf("pickForSession(fast) error = %v", err)
		}
		if picked.ID == 1 {
			fastIDOne++
		}
		picked, err = pickForSession(slowFirst, nil, "jp", session, nil, 0, 0, nil)
		if err != nil {
			t.Fatalf("pickForSession(slow) error = %v", err)
		}
		if picked.ID == 1 {
			slowIDOne++
		}
		picked, err = pickForSession(unknownFirst, nil, "jp", session, nil, 0, 0, nil)
		if err != nil {
			t.Fatalf("pickForSession(unknown) error = %v", err)
		}
		if picked.ID == 1 {
			unknownIDOne++
		}
	}

	if fastIDOne <= slowIDOne+512 {
		t.Fatalf("latency did not materially affect selection: ID 1 fast=%d slow=%d", fastIDOne, slowIDOne)
	}
	if slowIDOne == 0 || fastIDOne == samples {
		t.Fatalf("latency weight starved a candidate: ID 1 fast=%d slow=%d", fastIDOne, slowIDOne)
	}
	if unknownIDOne >= 1800 {
		t.Fatalf("unknown latency was favored too often: ID 1 unknown=%d of %d", unknownIDOne, samples)
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

// TestResolvePinnedNodeHitsExactAddress: -node- 锁定命中指定入口地址，绕过地域选路。
func TestResolvePinnedNodeHitsExactAddress(t *testing.T) {
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-a:8080", Region: "us", Latency: 10, Status: "active"},
		{ID: 2, Address: "us-b:8080", Region: "us", Latency: 5, Status: "active"},
	}}
	proxy, err := Resolve(store, nil, auth.ParsedUsername{Node: "us-a:8080"}, nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if proxy.ID != 1 {
		t.Fatalf("Resolve() pinned proxy ID = %d, want 1 (exact address, not lowest latency)", proxy.ID)
	}
}

// TestResolvePinnedNodeByStableKey: -node-key- 按稳定身份命中，即使 address 是临时本地端口。
func TestResolvePinnedNodeByStableKey(t *testing.T) {
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "127.0.0.1:20009", Region: "us", Latency: 10, Status: "active", NodeKey: "trojan:up.example.com:443:deadbeef"},
		{ID: 2, Address: "127.0.0.1:20010", Region: "us", Latency: 5, Status: "active", NodeKey: "trojan:other.example.com:443:cafebabe"},
	}}
	proxy, err := Resolve(store, nil, auth.ParsedUsername{Node: "key-trojan:up.example.com:443:deadbeef"}, nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if proxy.ID != 1 || proxy.Address != "127.0.0.1:20009" {
		t.Fatalf("Resolve() = id=%d addr=%q, want id=1 local mixed of stable key", proxy.ID, proxy.Address)
	}
	// 模拟端口重分配后仍按 key 命中同一 id。
	store.proxies[0].Address = "127.0.0.1:30001"
	proxy, err = Resolve(store, nil, auth.ParsedUsername{Node: "key-trojan:up.example.com:443:deadbeef"}, nil)
	if err != nil {
		t.Fatalf("Resolve() after rebind error = %v", err)
	}
	if proxy.ID != 1 || proxy.Address != "127.0.0.1:30001" {
		t.Fatalf("Resolve() after rebind = id=%d addr=%q, want same identity new port", proxy.ID, proxy.Address)
	}
}

// TestResolvePinnedNodeMissingReturnsErrNoNode: 锁定地址不存在必须显式失败，不回退选路。
func TestResolvePinnedNodeMissingReturnsErrNoNode(t *testing.T) {
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-a:8080", Region: "us", Latency: 10, Status: "active"},
	}}
	if _, err := Resolve(store, nil, auth.ParsedUsername{Node: "nope:9999"}, nil); !errors.Is(err, ErrNoNode) {
		t.Fatalf("Resolve() pinned missing err = %v, want ErrNoNode", err)
	}
}

// TestResolvePinnedNodeUnavailableReturnsErrNoNode: 锁定节点不可用（user_paused/失败/禁用）显式失败。
func TestResolvePinnedNodeUnavailableReturnsErrNoNode(t *testing.T) {
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-a:8080", Region: "us", Latency: 10, Status: "active", UserPaused: true},
	}}
	if _, err := Resolve(store, nil, auth.ParsedUsername{Node: "us-a:8080"}, nil); !errors.Is(err, ErrNoNode) {
		t.Fatalf("Resolve() pinned unavailable err = %v, want ErrNoNode", err)
	}
}

// TestResolvePinnedNodeRegionMismatchReturnsErrNoNode: 锁定节点与请求地域不符时显式失败。
func TestResolvePinnedNodeRegionMismatchReturnsErrNoNode(t *testing.T) {
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "jp-a:8080", Region: "jp", Latency: 10, Status: "active"},
	}}
	if _, err := Resolve(store, nil, auth.ParsedUsername{Region: "us", Node: "jp-a:8080"}, nil); !errors.Is(err, ErrNoNode) {
		t.Fatalf("Resolve() pinned region mismatch err = %v, want ErrNoNode", err)
	}
}

// TestResolvePinnedNodeExcludedReturnsErrNoNode: 锁定节点被 excludes 命中（如刚失败）时显式失败。
func TestResolvePinnedNodeExcludedReturnsErrNoNode(t *testing.T) {
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-a:8080", Region: "us", Latency: 10, Status: "active"},
	}}
	if _, err := Resolve(store, nil, auth.ParsedUsername{Node: "us-a:8080"}, []int64{1}); !errors.Is(err, ErrNoNode) {
		t.Fatalf("Resolve() pinned excluded err = %v, want ErrNoNode", err)
	}
}

// TestResolvePinnedNodeParentSubscriptionPausedReturnsErrNoNode: 父订阅暂停时锁定也不得命中。
func TestResolvePinnedNodeParentSubscriptionPausedReturnsErrNoNode(t *testing.T) {
	store := fakeStore{
		proxies: []storage.Proxy{
			{ID: 1, Address: "us-a:8080", Region: "us", Latency: 10, Status: "active", Source: storage.SourceSubscription, SubscriptionID: 9},
		},
		pausedSubs: map[int64]bool{9: true},
	}
	if _, err := Resolve(store, nil, auth.ParsedUsername{Node: "us-a:8080"}, nil); !errors.Is(err, ErrNoNode) {
		t.Fatalf("Resolve() pinned paused-parent err = %v, want ErrNoNode", err)
	}
}

// TestResolvePinnedNodeUnlockFilterReturnsErrNoNode: 锁定节点不满足 unlock 过滤时显式失败。
func TestResolvePinnedNodeUnlockFilterReturnsErrNoNode(t *testing.T) {
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-a:8080", Region: "us", Latency: 10, Status: "active", CFBlocked: 1},
	}}
	if _, err := Resolve(store, nil, auth.ParsedUsername{Node: "us-a:8080", Unlock: []string{"cf"}}, nil); !errors.Is(err, ErrNoNode) {
		t.Fatalf("Resolve() pinned unlock-fail err = %v, want ErrNoNode", err)
	}
}
