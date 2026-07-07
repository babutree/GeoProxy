package affinity

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStoreExpiresBindings(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	store := NewWithClock(10*time.Minute, func() time.Time { return now })

	store.Set("s1", "us-fast:8080", "us")
	now = now.Add(11 * time.Minute)

	if _, ok := store.Get("s1"); ok {
		t.Fatal("Get() returned expired binding")
	}
}

func TestStoreRefreshesBindingOnGet(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	store := NewWithClock(10*time.Minute, func() time.Time { return now })

	store.Set("s1", "us-fast:8080", "us")
	now = now.Add(9 * time.Minute)
	if _, ok := store.Get("s1"); !ok {
		t.Fatal("Get() returned no active binding")
	}
	now = now.Add(9 * time.Minute)

	if binding, ok := store.Get("s1"); !ok || binding.NodeAddress != "us-fast:8080" {
		t.Fatalf("Get() = %#v, %v; want refreshed binding", binding, ok)
	}
}

// atomicClock is a race-safe injectable clock for tests. The GC goroutine and
// the test goroutine both read the time, and the test goroutine advances it, so
// plain variable access would be a data race under -race.
type atomicClock struct {
	nanos atomic.Int64
}

func newAtomicClock(t time.Time) *atomicClock {
	c := &atomicClock{}
	c.nanos.Store(t.UnixNano())
	return c
}

func (c *atomicClock) now() time.Time {
	return time.Unix(0, c.nanos.Load()).UTC()
}

func (c *atomicClock) advance(d time.Duration) {
	c.nanos.Add(int64(d))
}

func TestStartGCRemovesExpiredBindings(t *testing.T) {
	clock := newAtomicClock(time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC))
	store := NewWithClock(10*time.Minute, clock.now)

	store.Set("s1", "us-fast:8080", "us")
	store.Set("s2", "eu-fast:8080", "eu")

	// Advance the injected clock past TTL so both bindings are expired.
	clock.advance(11 * time.Minute)

	store.StartGC(5 * time.Millisecond)
	defer store.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if len(store.List()) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("GC did not remove expired bindings; List()=%d", len(store.List()))
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestStartGCKeepsActiveBindings(t *testing.T) {
	clock := newAtomicClock(time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC))
	store := NewWithClock(10*time.Minute, clock.now)

	store.Set("active", "us-fast:8080", "us")
	store.Set("stale", "eu-fast:8080", "eu")

	// Only "stale" ages out of the window; "active" stays inside TTL.
	clock.advance(5 * time.Minute)
	store.Set("active", "us-fast:8080", "us") // reset LastActive to current clock
	clock.advance(6 * time.Minute)            // stale now 11m old, active 6m old

	store.StartGC(5 * time.Millisecond)
	defer store.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for {
		list := store.List()
		if len(list) == 1 && list[0].SessionID == "active" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("GC state wrong; List()=%#v", list)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestStartGCIgnoredWhenAlreadyRunning(t *testing.T) {
	clock := newAtomicClock(time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC))
	store := NewWithClock(10*time.Minute, clock.now)

	store.StartGC(5 * time.Millisecond)
	// Second call must not panic or leak a second goroutine.
	store.StartGC(5 * time.Millisecond)
	store.Stop()
}

func TestStartGCZeroIntervalIsNoOp(t *testing.T) {
	store := New(10 * time.Minute)
	store.StartGC(0)
	// Stop must be safe even though no goroutine was started.
	store.Stop()
}

func TestStopSafeWithoutStart(t *testing.T) {
	store := New(10 * time.Minute)
	// Never started; Stop must not panic or block.
	store.Stop()
	store.Stop()
}

func TestStopIsIdempotent(t *testing.T) {
	store := New(10 * time.Minute)
	store.StartGC(5 * time.Millisecond)
	store.Stop()
	// Second Stop must be safe.
	store.Stop()
}

func TestStartGCAfterStopRestarts(t *testing.T) {
	clock := newAtomicClock(time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC))
	store := NewWithClock(10*time.Minute, clock.now)

	store.StartGC(5 * time.Millisecond)
	store.Stop()

	// After Stop, StartGC should be able to start a fresh goroutine.
	store.Set("s1", "us-fast:8080", "us")
	clock.advance(11 * time.Minute)
	store.StartGC(5 * time.Millisecond)
	defer store.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if len(store.List()) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("restarted GC did not remove expired binding")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestListReturnsActiveBindingsWithMetadata(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	store := NewWithClock(10*time.Minute, func() time.Time { return now })

	store.Set("s1", "us-fast:8080", "us")

	list := store.List()
	if len(list) != 1 {
		t.Fatalf("List() len = %d; want 1", len(list))
	}
	got := list[0]
	if got.SessionID != "s1" || got.NodeAddress != "us-fast:8080" || got.Region != "us" {
		t.Fatalf("List()[0] = %#v; want session/node/region populated", got)
	}
	if !got.LastActive.Equal(now) {
		t.Fatalf("List()[0].LastActive = %v; want %v", got.LastActive, now)
	}
}

func TestListSkipsExpiredBindings(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	store := NewWithClock(10*time.Minute, func() time.Time { return now })

	store.Set("s1", "us-fast:8080", "us")
	now = now.Add(11 * time.Minute)

	if list := store.List(); len(list) != 0 {
		t.Fatalf("List() = %#v; want empty (expired skipped)", list)
	}
}

func TestListDoesNotRefreshLastActive(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	store := NewWithClock(10*time.Minute, func() time.Time { return now })

	store.Set("s1", "us-fast:8080", "us")
	setAt := now

	// Advance clock, then List. List must NOT refresh LastActive.
	now = now.Add(9 * time.Minute)
	list := store.List()
	if len(list) != 1 || !list[0].LastActive.Equal(setAt) {
		t.Fatalf("List() refreshed LastActive: got %v, want %v", list[0].LastActive, setAt)
	}

	// Because List did not refresh, the binding should expire on schedule
	// relative to the original set time.
	now = now.Add(2 * time.Minute) // total 11m since set
	if _, ok := store.Get("s1"); ok {
		t.Fatal("binding survived past TTL; List() must have refreshed LastActive")
	}
}

func TestTTLGetter(t *testing.T) {
	store := New(10 * time.Minute)
	if store.TTL() != 10*time.Minute {
		t.Fatalf("TTL() = %v; want 10m", store.TTL())
	}
}

func TestRemainingTTL(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	store := NewWithClock(10*time.Minute, func() time.Time { return now })

	store.Set("s1", "us-fast:8080", "us")
	list := store.List()
	if got := store.RemainingTTL(list[0]); got != 10*time.Minute {
		t.Fatalf("RemainingTTL() = %v; want 10m", got)
	}

	now = now.Add(4 * time.Minute)
	if got := store.RemainingTTL(list[0]); got != 6*time.Minute {
		t.Fatalf("RemainingTTL() = %v; want 6m", got)
	}

	now = now.Add(10 * time.Minute)
	if got := store.RemainingTTL(list[0]); got != 0 {
		t.Fatalf("RemainingTTL() past expiry = %v; want 0", got)
	}
}

func TestConcurrentAccessWithGC(t *testing.T) {
	clock := newAtomicClock(time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC))
	store := NewWithClock(10*time.Minute, clock.now)

	store.StartGC(time.Millisecond)
	defer store.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := string(rune('a' + id))
			for j := 0; j < 200; j++ {
				store.Set(key, "node:8080", "us")
				store.Get(key)
				_ = store.List()
				store.Remove(key)
			}
		}(i)
	}
	wg.Wait()
}
