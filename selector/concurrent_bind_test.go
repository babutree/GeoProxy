package selector

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/affinity"
	"github.com/babutree/GeoProxy/auth"
	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/storage"
)

func setMaxSessionsAndCooldown(t *testing.T, maxSessions, cooldownMinutes int) {
	t.Helper()
	t.Setenv("DATA_DIR", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.MaxSessionsPerProxy = maxSessions
	cfg.ProxyCooldownMinutes = cooldownMinutes
	if cfg.WebUIPasswordHash == "" {
		cfg.WebUIPasswordHash = "deadbeef"
	}
	if cfg.ProxyAuthPasswordHash == "" {
		cfg.ProxyAuthPasswordHash = "deadbeef"
	}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	_ = config.Load()
	got := config.Get()
	if got.MaxSessionsPerProxy != maxSessions {
		t.Fatalf("MaxSessionsPerProxy = %d, want %d", got.MaxSessionsPerProxy, maxSessions)
	}
	if got.ProxyCooldownMinutes != cooldownMinutes {
		t.Fatalf("ProxyCooldownMinutes = %d, want %d", got.ProxyCooldownMinutes, cooldownMinutes)
	}
}

// TestResolveConcurrentFirstBindNeverExceedsMaxSessions:
// two distinct sessions racing on a single node with max=1 must yield exactly one
// success and final occupancy 1. A test hook delays after pick and before write so
// both would have passed the old check-then-act path; first-bind serialization
// must still enforce capacity.
func TestResolveConcurrentFirstBindNeverExceedsMaxSessions(t *testing.T) {
	setMaxSessionsPerProxy(t, 1)
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-only:8080", Region: "us", Latency: 10, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, time.Now)

	// Delay inside the first-bind critical section after pick to widen the race
	// window if serialization were missing (hook runs under first-bind lock).
	afterFirstBindPickHook = func() { time.Sleep(5 * time.Millisecond) }
	t.Cleanup(func() { afterFirstBindPickHook = nil })

	type outcome struct {
		proxy *storage.Proxy
		err   error
	}
	results := make(chan outcome, 2)
	var start sync.WaitGroup
	start.Add(2)
	for _, sess := range []string{"sess-a", "sess-b"} {
		go func(session string) {
			start.Done()
			start.Wait()
			p, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: session}, nil)
			results <- outcome{p, err}
		}(sess)
	}

	var okCount, failCount int
	for i := 0; i < 2; i++ {
		o := <-results
		if o.err == nil {
			okCount++
			if o.proxy == nil || o.proxy.ID != 1 {
				t.Fatalf("success proxy = %#v, want ID=1", o.proxy)
			}
			continue
		}
		failCount++
		if !errors.Is(o.err, ErrNoNode) {
			t.Fatalf("failure err = %v, want ErrNoNode", o.err)
		}
	}
	if okCount != 1 || failCount != 1 {
		t.Fatalf("ok=%d fail=%d, want exactly one success and one ErrNoNode", okCount, failCount)
	}
	if got := sessions.CountByProxy(1); got != 1 {
		t.Fatalf("CountByProxy(1) = %d, want 1 (capacity invariant)", got)
	}
}

// TestResolveConcurrentFirstBindAtomicallyStartsCooldown:
// capacity unlimited, cooldown on; two concurrent first-binds on one node must
// allow only one success and leave the node in cooldown.
func TestResolveConcurrentFirstBindAtomicallyStartsCooldown(t *testing.T) {
	setMaxSessionsAndCooldown(t, 0, 5)
	now := time.Date(2026, 7, 12, 15, 0, 0, 0, time.UTC)
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-only:8080", Region: "us", Latency: 10, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, func() time.Time { return now })

	afterFirstBindPickHook = func() { time.Sleep(5 * time.Millisecond) }
	t.Cleanup(func() { afterFirstBindPickHook = nil })

	results := make(chan error, 2)
	var start sync.WaitGroup
	start.Add(2)
	for _, sess := range []string{"cool-a", "cool-b"} {
		go func(session string) {
			start.Done()
			start.Wait()
			_, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: session}, nil)
			results <- err
		}(sess)
	}

	var okCount, failCount int
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			okCount++
			continue
		}
		failCount++
		if !errors.Is(err, ErrNoNode) {
			t.Fatalf("failure err = %v, want ErrNoNode", err)
		}
	}
	if okCount != 1 || failCount != 1 {
		t.Fatalf("ok=%d fail=%d, want one success one cooldown failure", okCount, failCount)
	}
	if !sessions.InCooldown(1) {
		t.Fatal("proxy 1 should be in cooldown after the winning first-bind")
	}
	if got := sessions.CountByProxy(1); got != 1 {
		t.Fatalf("CountByProxy(1) = %d, want 1", got)
	}
}

// TestResolveConcurrentSameSessionDoesNotSplitBinding:
// concurrent Resolve for the same session must not produce two different nodes.
func TestResolveConcurrentSameSessionDoesNotSplitBinding(t *testing.T) {
	setMaxSessionsPerProxy(t, 0)
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-a:8080", Region: "us", Latency: 10, Status: "active"},
		{ID: 2, Address: "us-b:8080", Region: "us", Latency: 10, Status: "active"},
		{ID: 3, Address: "us-c:8080", Region: "us", Latency: 10, Status: "active"},
		{ID: 4, Address: "us-d:8080", Region: "us", Latency: 10, Status: "active"},
		{ID: 5, Address: "us-e:8080", Region: "us", Latency: 10, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, time.Now)

	const workers = 16
	ids := make(chan int64, workers)
	var start, wg sync.WaitGroup
	start.Add(workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start.Done()
			start.Wait()
			p, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "same-sess"}, nil)
			if err != nil {
				t.Errorf("Resolve same session error = %v", err)
				return
			}
			ids <- p.ID
		}()
	}
	wg.Wait()
	close(ids)

	seen := map[int64]struct{}{}
	for id := range ids {
		seen[id] = struct{}{}
	}
	if len(seen) != 1 {
		t.Fatalf("same session bound to %d distinct proxies %v; want exactly 1", len(seen), seen)
	}
	if got := sessions.Count(); got != 1 {
		t.Fatalf("session count = %d, want 1", got)
	}
}

// TestResolveConcurrentCapacityInvariant:
// many concurrent first-binds must never push any node above max sessions.
func TestResolveConcurrentCapacityInvariant(t *testing.T) {
	const (
		maxPerNode = 2
		nodes      = 5
		sessionsN  = 100
	)
	setMaxSessionsPerProxy(t, maxPerNode)
	proxies := make([]storage.Proxy, 0, nodes)
	for i := 1; i <= nodes; i++ {
		proxies = append(proxies, storage.Proxy{
			ID:      int64(i),
			Address: fmt.Sprintf("us-%d:8080", i),
			Region:  "us",
			Latency: 10,
			Status:  "active",
		})
	}
	store := fakeStore{proxies: proxies}
	sessStore := affinity.NewWithClock(10*time.Minute, time.Now)

	var success atomic.Int64
	var start, wg sync.WaitGroup
	start.Add(sessionsN)
	for i := 0; i < sessionsN; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start.Done()
			start.Wait()
			session := fmt.Sprintf("cap-%03d", i)
			if _, err := Resolve(store, sessStore, auth.ParsedUsername{Region: "us", Session: session}, nil); err == nil {
				success.Add(1)
			}
		}(i)
	}
	wg.Wait()

	totalCap := maxPerNode * nodes
	gotSuccess := success.Load()
	if gotSuccess > int64(totalCap) {
		t.Fatalf("successes = %d, exceed total capacity %d", gotSuccess, totalCap)
	}
	var occupied int
	for id := int64(1); id <= nodes; id++ {
		n := sessStore.CountByProxy(id)
		if n > maxPerNode {
			t.Fatalf("CountByProxy(%d) = %d, want <= %d", id, n, maxPerNode)
		}
		occupied += n
	}
	if int64(occupied) != gotSuccess {
		t.Fatalf("sum occupancy %d != success count %d", occupied, gotSuccess)
	}
}

// TestResolveConcurrentFirstBindStormNeverExceeds hammers one node under max=1.
func TestResolveConcurrentFirstBindStormNeverExceeds(t *testing.T) {
	setMaxSessionsPerProxy(t, 1)
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-only:8080", Region: "us", Latency: 10, Status: "active"},
	}}
	for round := 0; round < 20; round++ {
		sessions := affinity.NewWithClock(10*time.Minute, time.Now)
		var start, wg sync.WaitGroup
		const n = 8
		start.Add(n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				start.Done()
				start.Wait()
				_, _ = Resolve(store, sessions, auth.ParsedUsername{
					Region:  "us",
					Session: fmt.Sprintf("storm-%d-%d", round, i),
				}, nil)
			}(i)
		}
		wg.Wait()
		if got := sessions.CountByProxy(1); got > 1 {
			t.Fatalf("round %d CountByProxy(1)=%d, want <=1", round, got)
		}
	}
}
