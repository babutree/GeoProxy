package custom

import (
	"testing"
	"time"
)

func TestInitialRefreshStopsBeforeStartupDelayAndStorageAccess(t *testing.T) {
	oldDelay := initialRefreshDelay
	initialRefreshDelay = 10 * time.Millisecond
	t.Cleanup(func() { initialRefreshDelay = oldDelay })

	m := &Manager{stopCh: make(chan struct{})}
	close(m.stopCh)

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("stopped initial refresh touched nil storage: %v", recovered)
		}
	}()
	started := time.Now()
	m.initialRefresh()
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("stopped initial refresh waited %v, want immediate cancellation", elapsed)
	}
}

func TestRefreshSubscriptionRejectsAfterManagerStop(t *testing.T) {
	m := &Manager{stopCh: make(chan struct{})}
	close(m.stopCh)
	if err := m.RefreshSubscription(1); err == nil {
		t.Fatal("stopped manager refresh returned nil error")
	}
}
