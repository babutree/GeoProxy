package selector

import (
	"fmt"
	"hash/fnv"
	"math"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/affinity"
	"github.com/babutree/GeoProxy/storage"
)

func countSessionSelections(t *testing.T, store fakeStore, sessions *affinity.Store, prefix string, samples int) map[int64]int {
	t.Helper()
	counts := make(map[int64]int)
	for i := 0; i < samples; i++ {
		proxy, err := pickForSession(store, sessions, "jp", fmt.Sprintf("%s-%04d", prefix, i), nil, 0, 0, nil)
		if err != nil {
			t.Fatalf("pickForSession(%s-%04d) error = %v", prefix, i, err)
		}
		counts[proxy.ID]++
	}
	return counts
}

func TestSessionSchedulerUsesReliabilityHistory(t *testing.T) {
	const samples = 4096
	highReliabilityFirst := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "high:1", Region: "jp", Latency: 100, Status: "active", UseCount: 100, SuccessCount: 98},
		{ID: 2, Address: "low:1", Region: "jp", Latency: 100, Status: "active", UseCount: 100, SuccessCount: 20},
	}}
	lowReliabilityFirst := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "high:1", Region: "jp", Latency: 100, Status: "active", UseCount: 100, SuccessCount: 20},
		{ID: 2, Address: "low:1", Region: "jp", Latency: 100, Status: "active", UseCount: 100, SuccessCount: 98},
	}}

	high := countSessionSelections(t, highReliabilityFirst, nil, "reliability", samples)
	low := countSessionSelections(t, lowReliabilityFirst, nil, "reliability", samples)
	if high[1] <= low[1]+400 {
		t.Fatalf("reliability did not affect assignments: high-quality ID1=%d, low-quality ID1=%d", high[1], low[1])
	}
	if high[2] == 0 || low[1] == 0 {
		t.Fatalf("reliability weight starved a candidate: high=%v low=%v", high, low)
	}
}

func TestSessionSchedulerUsesActiveBindingLoad(t *testing.T) {
	const samples = 4096
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "busy:1", Region: "jp", Latency: 100, Status: "active"},
		{ID: 2, Address: "idle:1", Region: "jp", Latency: 100, Status: "active"},
	}}
	busyIDOne := affinity.NewWithClock(time.Hour, time.Now)
	busyIDTwo := affinity.NewWithClock(time.Hour, time.Now)
	for i := 0; i < 8; i++ {
		session := fmt.Sprintf("existing-%02d", i)
		busyIDOne.SetProxy(session, 1, "busy:1", "jp")
		busyIDTwo.SetProxy(session, 2, "idle:1", "jp")
	}

	whenBusy := countSessionSelections(t, store, busyIDOne, "load", samples)
	whenIdle := countSessionSelections(t, store, busyIDTwo, "load", samples)
	if whenIdle[1] <= whenBusy[1]+600 {
		t.Fatalf("active binding load did not affect ID1 assignments: busy=%d, idle=%d", whenBusy[1], whenIdle[1])
	}
	if whenBusy[1] == 0 || whenIdle[2] == 0 {
		t.Fatalf("load weight starved a busy candidate: busy-ID1=%v busy-ID2=%v", whenBusy, whenIdle)
	}
}

func TestSessionSchedulerExploresSlowUnknownAndLowReliabilityCandidates(t *testing.T) {
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "fast-reliable:1", Region: "jp", Latency: 20, Status: "active", UseCount: 100, SuccessCount: 100},
		{ID: 2, Address: "slow-low:1", Region: "jp", Latency: 5000, Status: "active", UseCount: 100, SuccessCount: 5},
		{ID: 3, Address: "unknown:1", Region: "jp", Latency: 0, Status: "active"},
	}}

	counts := countSessionSelections(t, store, nil, "explore", 8192)
	for _, id := range []int64{1, 2, 3} {
		if counts[id] == 0 {
			t.Fatalf("candidate ID%d received no exploration assignments: %v", id, counts)
		}
	}
}

func TestSessionSchedulerScalesAcrossCandidateCounts(t *testing.T) {
	for _, tc := range []struct {
		count   int
		samples int
	}{
		{count: 3, samples: 256},
		{count: 215, samples: 256},
		{count: 6000, samples: 32},
	} {
		t.Run(fmt.Sprintf("nodes-%d", tc.count), func(t *testing.T) {
			proxies := make([]storage.Proxy, 0, tc.count)
			for i := 1; i <= tc.count; i++ {
				proxies = append(proxies, storage.Proxy{
					ID: int64(i), Address: fmt.Sprintf("jp-%05d:1", i), Region: "jp",
					Latency: 20 + i%400, Status: "active",
				})
			}
			seenOutsideLegacyTopFive := false
			for i := 0; i < tc.samples; i++ {
				picked := pickSessionCandidate(proxies, fmt.Sprintf("scale-%04d", i))
				if picked.ID > 5 {
					seenOutsideLegacyTopFive = true
					break
				}
			}
			if tc.count > 5 && !seenOutsideLegacyTopFive {
				t.Fatalf("%d candidates never selected outside legacy top-five", tc.count)
			}
		})
	}
}

func TestSessionSchedulerAddNodeOnlyRemapsAffectedSessions(t *testing.T) {
	base := make([]storage.Proxy, 0, 16)
	for i := 1; i <= 16; i++ {
		base = append(base, storage.Proxy{
			ID: int64(i), Address: fmt.Sprintf("node-%02d:1", i), Region: "jp",
			Latency: 100, Status: "active",
		})
	}
	added := append(append([]storage.Proxy(nil), base...), storage.Proxy{
		ID: 999, Address: "node-new:1", Region: "jp", Latency: 100, Status: "active",
	})

	const samples = 512
	remapped := 0
	for i := 0; i < samples; i++ {
		session := fmt.Sprintf("remap-%04d", i)
		before := pickSessionCandidate(base, session)
		after := pickSessionCandidate(added, session)
		if before.ID != after.ID {
			remapped++
			if after.ID != 999 {
				t.Fatalf("session %q remapped from %d to unrelated node %d", session, before.ID, after.ID)
			}
		}
	}
	if remapped == 0 || remapped >= samples {
		t.Fatalf("node addition remapped %d/%d sessions; want a bounded nonzero subset", remapped, samples)
	}
}

func TestSessionSchedulerRemoveNodeOnlyRemapsAffectedSessions(t *testing.T) {
	base := make([]storage.Proxy, 0, 16)
	withoutSeven := make([]storage.Proxy, 0, 15)
	for i := 1; i <= 16; i++ {
		proxy := storage.Proxy{
			ID: int64(i), Address: fmt.Sprintf("remove-%02d:1", i), Region: "jp",
			Latency: 100, Status: "active",
		}
		base = append(base, proxy)
		if i != 7 {
			withoutSeven = append(withoutSeven, proxy)
		}
	}

	affected := 0
	for i := 0; i < 512; i++ {
		session := fmt.Sprintf("remove-%04d", i)
		before := pickSessionCandidate(base, session)
		after := pickSessionCandidate(withoutSeven, session)
		if before.ID == 7 {
			affected++
			if after.ID == 7 {
				t.Fatalf("session %q still selected removed node", session)
			}
			continue
		}
		if after.ID != before.ID {
			t.Fatalf("session %q moved from unaffected ID%d to ID%d", session, before.ID, after.ID)
		}
	}
	if affected == 0 {
		t.Fatal("fixed corpus did not exercise removal of a selected node")
	}
}

func TestSessionSchedulerInputReorderKeepsMultifactorResult(t *testing.T) {
	proxies := []storage.Proxy{
		{ID: 1, Address: "one:1", Region: "jp", Latency: 20, Status: "active", UseCount: 100, SuccessCount: 99},
		{ID: 2, Address: "two:1", Region: "jp", Latency: 500, Status: "active", UseCount: 100, SuccessCount: 50},
		{ID: 3, Address: "three:1", Region: "jp", Latency: 0, Status: "active"},
	}
	reversed := []storage.Proxy{proxies[2], proxies[1], proxies[0]}
	sessions := affinity.NewWithClock(time.Hour, time.Now)
	sessions.SetProxy("existing", 2, "two:1", "jp")

	first, err := pickForSession(fakeStore{proxies: proxies}, sessions, "jp", "stable-multifactor", nil, 0, 0, nil)
	if err != nil {
		t.Fatalf("pickForSession(first) error = %v", err)
	}
	second, err := pickForSession(fakeStore{proxies: reversed}, sessions, "jp", "stable-multifactor", nil, 0, 0, nil)
	if err != nil {
		t.Fatalf("pickForSession(reversed) error = %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("input reorder changed selected ID from %d to %d", first.ID, second.ID)
	}
}

func TestSessionSchedulerIdentityNamespacesDoNotCollide(t *testing.T) {
	proxies := []storage.Proxy{
		{ID: 7, NodeKey: "same", Address: "id-node:1"},
		{NodeKey: "id:7", Address: "key-node:1"},
		{Address: "key:id:7"},
	}
	seen := make(map[string]int64, len(proxies))
	for _, proxy := range proxies {
		identity := sessionProxyIdentity(proxy)
		if prior, ok := seen[identity]; ok {
			t.Fatalf("identity namespace collision %q for IDs %d and %d", identity, prior, proxy.ID)
		}
		seen[identity] = proxy.ID
	}
	if len(seen) != 3 {
		t.Fatalf("identity namespace count=%d, want 3", len(seen))
	}
}

func TestSessionSchedulerIdentityHashPreservesThreeNamespaces(t *testing.T) {
	session := "identity-hash"
	proxies := []storage.Proxy{
		{ID: 7, NodeKey: "same", Address: "id-node:1"},
		{NodeKey: "id:7", Address: "key-node:1"},
		{Address: "key:id:7"},
	}
	seen := make(map[uint64]string, len(proxies))
	for _, proxy := range proxies {
		wantHasher := fnv.New64a()
		_, _ = wantHasher.Write([]byte(session))
		_, _ = wantHasher.Write([]byte{0})
		_, _ = wantHasher.Write([]byte(sessionProxyIdentity(proxy)))
		want := wantHasher.Sum64()
		got := sessionProxyHash(sessionHashSeed(session), proxy)
		if got != want {
			t.Fatalf("identity hash changed for %q: got %d want %d", sessionProxyIdentity(proxy), got, want)
		}
		if prior, ok := seen[got]; ok {
			t.Fatalf("identity hash collision for %q and %q", prior, sessionProxyIdentity(proxy))
		}
		seen[got] = sessionProxyIdentity(proxy)
	}
}

func TestSessionSchedulerTieBreakIsInputOrderIndependent(t *testing.T) {
	first := storage.Proxy{ID: 77, Address: "b:1", Region: "jp", Latency: 100, Status: "active"}
	second := storage.Proxy{ID: 77, Address: "a:1", Region: "jp", Latency: 100, Status: "active"}

	pickedForward := pickSessionCandidate([]storage.Proxy{first, second}, "equal-score")
	pickedReverse := pickSessionCandidate([]storage.Proxy{second, first}, "equal-score")
	if pickedForward.Address != pickedReverse.Address {
		t.Fatalf("tie break changed from %q to %q after reorder", pickedForward.Address, pickedReverse.Address)
	}
	if pickedForward.Address != "a:1" {
		t.Fatalf("tie break picked %q, want deterministic address a:1", pickedForward.Address)
	}
}

func TestSessionSchedulerIgnoresSchedulingMetadataAndDerivedGrade(t *testing.T) {
	base := []storage.Proxy{
		{ID: 1, Address: "meta-one:1", Region: "jp", Latency: 100, QualityGrade: "S", Status: "active", UseCount: 10, SuccessCount: 8},
		{ID: 2, Address: "meta-two:1", Region: "jp", Latency: 200, QualityGrade: "C", Status: "active", UseCount: 10, SuccessCount: 8},
	}
	changed := []storage.Proxy{
		{ID: 1, Address: "meta-one:1", Region: "jp", Latency: 100, QualityGrade: "D", Status: "active", UseCount: 10, SuccessCount: 8, LastCheck: time.Unix(1, 0), LastUsed: time.Unix(2, 0)},
		{ID: 2, Address: "meta-two:1", Region: "jp", Latency: 200, QualityGrade: "S", Status: "active", UseCount: 10, SuccessCount: 8, LastCheck: time.Unix(3, 0), LastUsed: time.Unix(4, 0)},
	}
	for i := 0; i < 256; i++ {
		session := fmt.Sprintf("metadata-%04d", i)
		before := pickSessionCandidate(base, session)
		after := pickSessionCandidate(changed, session)
		if before.ID != after.ID {
			t.Fatalf("session %q changed from ID%d to ID%d after metadata-only update", session, before.ID, after.ID)
		}
	}
}

func TestSessionSchedulerWeightsStayPositiveAndBounded(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	cases := []struct {
		name   string
		proxy  storage.Proxy
		active int
	}{
		{name: "zero"},
		{name: "best-observed", proxy: storage.Proxy{Latency: 1, UseCount: maxInt, SuccessCount: maxInt}},
		{name: "worst-observed", proxy: storage.Proxy{Latency: maxInt, UseCount: maxInt, SuccessCount: 0}, active: maxInt},
		{name: "negative-corrupt", proxy: storage.Proxy{Latency: -1, UseCount: -1, SuccessCount: -1}, active: -1},
		{name: "success-over-use", proxy: storage.Proxy{Latency: 100, UseCount: 1, SuccessCount: maxInt}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			weight := sessionCandidateWeight(tc.proxy, tc.active)
			if math.IsNaN(weight) || math.IsInf(weight, 0) {
				t.Fatalf("weight = %v, want finite", weight)
			}
			if weight < 0.375 || weight >= 2.5 {
				t.Fatalf("weight = %v, want [0.375, 2.5)", weight)
			}
		})
	}
}

func TestSessionSchedulerExtremeCountersRemainSelectable(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "max:1", Region: "jp", Latency: maxInt, Status: "active", UseCount: maxInt, SuccessCount: maxInt},
		{ID: 2, Address: "negative:1", Region: "jp", Latency: -1, Status: "active", UseCount: -1, SuccessCount: maxInt},
		{ID: 3, Address: "zero:1", Region: "jp", Latency: 0, Status: "active"},
	}}

	counts := countSessionSelections(t, store, nil, "extreme", 4096)
	for _, id := range []int64{1, 2, 3} {
		if counts[id] == 0 {
			t.Fatalf("extreme-value candidate ID%d received no assignments: %v", id, counts)
		}
	}
}
