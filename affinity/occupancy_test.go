package affinity

import (
	"sync"
	"testing"
	"time"
)

func TestCountByProxyTwoSessionsSameProxy(t *testing.T) {
	store := NewWithClock(10*time.Minute, time.Now)
	store.SetProxy("s1", 1, "us-a:8080", "us")
	store.SetProxy("s2", 1, "us-a:8080", "us")
	if got := store.CountByProxy(1); got != 2 {
		t.Fatalf("CountByProxy(1) = %d, want 2", got)
	}
	if got := store.CountByProxy(2); got != 0 {
		t.Fatalf("CountByProxy(2) = %d, want 0", got)
	}
}

func TestCountByProxyRemoveDecrements(t *testing.T) {
	store := NewWithClock(10*time.Minute, time.Now)
	store.SetProxy("s1", 1, "us-a:8080", "us")
	store.SetProxy("s2", 1, "us-a:8080", "us")
	store.Remove("s1")
	if got := store.CountByProxy(1); got != 1 {
		t.Fatalf("CountByProxy(1) after Remove = %d, want 1", got)
	}
}

func TestRemoveIfProxyIDPreservesReboundSession(t *testing.T) {
	store := NewWithClock(10*time.Minute, time.Now)
	store.SetProxy("s1", 1, "us-a:8080", "us")

	if !store.RemoveIfProxyID("s1", 1) {
		t.Fatal("RemoveIfProxyID() = false, want old binding removed")
	}
	store.SetProxy("s1", 2, "us-b:8080", "us")
	if store.RemoveIfProxyID("s1", 1) {
		t.Fatal("RemoveIfProxyID() removed rebound session with a different proxy ID")
	}
	binding, ok := store.Get("s1")
	if !ok || binding.ProxyID != 2 {
		t.Fatalf("binding = %#v, %v; want proxy 2", binding, ok)
	}
}

func TestCountByProxyTTLExpiryReleasesOccupancy(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	store := NewWithClock(10*time.Minute, func() time.Time { return now })
	store.SetProxy("s1", 1, "us-a:8080", "us")
	if got := store.CountByProxy(1); got != 1 {
		t.Fatalf("CountByProxy before expiry = %d, want 1", got)
	}
	now = now.Add(11 * time.Minute)
	if got := store.CountByProxy(1); got != 0 {
		t.Fatalf("CountByProxy after TTL = %d, want 0", got)
	}
}

func TestCountByProxyGCReleasesOccupancy(t *testing.T) {
	clock := newAtomicClock(time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC))
	store := NewWithClock(10*time.Minute, clock.now)
	store.SetProxy("s1", 7, "us-a:8080", "us")
	clock.advance(11 * time.Minute)
	store.StartGC(5 * time.Millisecond)
	defer store.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if store.CountByProxy(7) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("GC did not release occupancy; CountByProxy(7)=%d", store.CountByProxy(7))
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestCountByProxyRebindMovesOccupancy(t *testing.T) {
	store := NewWithClock(10*time.Minute, time.Now)
	store.SetProxy("s1", 1, "us-a:8080", "us")
	store.SetProxy("s1", 2, "us-b:8080", "us")
	if got := store.CountByProxy(1); got != 0 {
		t.Fatalf("CountByProxy(1) after rebind = %d, want 0", got)
	}
	if got := store.CountByProxy(2); got != 1 {
		t.Fatalf("CountByProxy(2) after rebind = %d, want 1", got)
	}
}

func TestCountByProxyGetExpiryCleansOccupancy(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	store := NewWithClock(10*time.Minute, func() time.Time { return now })
	store.SetProxy("s1", 1, "us-a:8080", "us")
	now = now.Add(11 * time.Minute)
	if _, ok := store.Get("s1"); ok {
		t.Fatal("Get should miss expired binding")
	}
	if got := store.CountByProxy(1); got != 0 {
		t.Fatalf("CountByProxy after Get expiry = %d, want 0", got)
	}
}

func TestCountByProxyConcurrentSetRemove(t *testing.T) {
	store := NewWithClock(10*time.Minute, time.Now)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sid := string(rune('a' + id))
			for j := 0; j < 200; j++ {
				store.SetProxy(sid, int64(j%3+1), "n:8080", "us")
				_ = store.CountByProxy(int64(j%3 + 1))
				if j%2 == 0 {
					store.Remove(sid)
				}
			}
		}(i)
	}
	wg.Wait()
}
