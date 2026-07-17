package affinity

import (
	"sync"
	"testing"
	"time"
)

func TestInCooldownBeforeUntil(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	store := NewWithClock(10*time.Minute, func() time.Time { return now })
	store.SetCooldown(1, now.Add(5*time.Minute))
	if !store.InCooldown(1) {
		t.Fatal("InCooldown(1) = false, want true before until")
	}
}

func TestInCooldownAfterUntil(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	store := NewWithClock(10*time.Minute, func() time.Time { return now })
	store.SetCooldown(1, now.Add(5*time.Minute))
	now = now.Add(6 * time.Minute)
	if store.InCooldown(1) {
		t.Fatal("InCooldown(1) = true, want false after until")
	}
}

func TestInCooldownMissingIsFalse(t *testing.T) {
	store := NewWithClock(10*time.Minute, time.Now)
	if store.InCooldown(99) {
		t.Fatal("InCooldown missing proxy should be false")
	}
}

func TestSetCooldownOverwrite(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	store := NewWithClock(10*time.Minute, func() time.Time { return now })
	store.SetCooldown(1, now.Add(2*time.Minute))
	store.SetCooldown(1, now.Add(10*time.Minute))
	now = now.Add(3 * time.Minute)
	if !store.InCooldown(1) {
		t.Fatal("InCooldown after overwrite should still be true (until=now+10m)")
	}
	now = now.Add(8 * time.Minute)
	if store.InCooldown(1) {
		t.Fatal("InCooldown after extended until elapsed should be false")
	}
}

func TestCooldownConcurrent(t *testing.T) {
	store := NewWithClock(10*time.Minute, time.Now)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				pid := int64(j%5 + 1)
				store.SetCooldown(pid, time.Now().Add(time.Minute))
				_ = store.InCooldown(pid)
			}
		}(i)
	}
	wg.Wait()
}

func TestCollectExpiredRemovesUnqueriedCooldownEntries(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	store := NewWithClock(10*time.Minute, func() time.Time { return now })
	for id := int64(1); id <= 5; id++ {
		store.SetCooldown(id, now.Add(time.Minute))
	}
	now = now.Add(2 * time.Minute) // all cooldowns expired
	// Do NOT call InCooldown/CooldownRemaining (which would lazily prune).
	store.collectExpired()
	if got := len(store.cooldown); got != 0 {
		t.Fatalf("cooldown map size after collectExpired = %d, want 0", got)
	}
}

func TestCollectExpiredKeepsActiveCooldownEntries(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	store := NewWithClock(10*time.Minute, func() time.Time { return now })
	store.SetCooldown(1, now.Add(time.Minute))    // will expire
	store.SetCooldown(2, now.Add(10*time.Minute)) // still active
	now = now.Add(2 * time.Minute)
	store.collectExpired()
	if _, ok := store.cooldown[1]; ok {
		t.Fatal("expired cooldown entry 1 not removed")
	}
	if _, ok := store.cooldown[2]; !ok {
		t.Fatal("active cooldown entry 2 wrongly removed")
	}
	if !store.InCooldown(2) {
		t.Fatal("InCooldown(2) = false, want true")
	}
}
