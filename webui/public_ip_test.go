package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func resetPublicIPCacheForTest() {
	pubIP.mu.Lock()
	pubIP.value = ""
	pubIP.country = ""
	pubIP.done = false
	pubIP.fetchedAt = time.Time{}
	pubIP.fetching = nil
	pubIP.mu.Unlock()
}

func TestPublicIPFetchDoesNotHoldCacheLock(t *testing.T) {
	resetPublicIPCacheForTest()
	t.Cleanup(resetPublicIPCacheForTest)
	originalFetch := publicIPFetch
	t.Cleanup(func() { publicIPFetch = originalFetch })

	started := make(chan struct{})
	release := make(chan struct{})
	publicIPFetch = func() (string, string) {
		close(started)
		<-release
		return "203.0.113.8", "US"
	}

	done := make(chan struct{})
	go func() {
		rec := httptest.NewRecorder()
		(&Server{}).apiPublicIP(rec, httptest.NewRequest(http.MethodGet, "/api/public-ip", nil))
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("public IP fetch did not start")
	}

	lockAcquired := make(chan struct{})
	go func() {
		pubIP.mu.Lock()
		pubIP.mu.Unlock()
		close(lockAcquired)
	}()
	select {
	case <-lockAcquired:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("public IP cache mutex remained locked during network fetch")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("public IP request did not finish")
	}
}

func TestPublicIPCacheRefreshesAfterExpiry(t *testing.T) {
	resetPublicIPCacheForTest()
	t.Cleanup(resetPublicIPCacheForTest)
	originalFetch := publicIPFetch
	t.Cleanup(func() { publicIPFetch = originalFetch })
	pubIP.mu.Lock()
	pubIP.value = "203.0.113.7"
	pubIP.country = "US"
	pubIP.done = true
	pubIP.fetchedAt = time.Now().Add(-publicIPCacheTTL - time.Second)
	pubIP.mu.Unlock()

	var calls atomic.Int32
	publicIPFetch = func() (string, string) {
		calls.Add(1)
		return "203.0.113.9", "JP"
	}
	rec := httptest.NewRecorder()
	(&Server{}).apiPublicIP(rec, httptest.NewRequest(http.MethodGet, "/api/public-ip", nil))
	if calls.Load() != 1 {
		t.Fatalf("refresh calls = %d, want 1", calls.Load())
	}
	if !strings.Contains(rec.Body.String(), "203.0.113.9") || !strings.Contains(rec.Body.String(), "JP") {
		t.Fatalf("response did not contain refreshed cache: %s", rec.Body.String())
	}
}
