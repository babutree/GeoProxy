package webui

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"goproxy/config"
)

func testAPIKeyHash(plain string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(plain)))
}

func newReadOnlyAPITestServer(t *testing.T, keys []config.APIKey, ratePerMin int) *Server {
	t.Helper()
	server := newTestServer(t)
	if ratePerMin <= 0 {
		ratePerMin = 60
	}
	server.cfg.ReadOnlyAPIKeys = keys
	server.cfg.ReadOnlyAPIRatePerMin = ratePerMin
	return server
}

func TestReadOnlyAPIRejectsMissingOrBadKey(t *testing.T) {
	plain := "good-readonly-key"
	server := newReadOnlyAPITestServer(t, []config.APIKey{{
		ID:   "k1",
		Name: "primary",
		Hash: testAPIKeyHash(plain),
	}}, 60)

	cases := []struct {
		name   string
		header func(*http.Request)
	}{
		{name: "missing", header: func(*http.Request) {}},
		{name: "bad_bearer", header: func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer wrong-key")
		}},
		{name: "bad_x_api_key", header: func(r *http.Request) {
			r.Header.Set("X-API-Key", "wrong-key")
		}},
		{name: "empty_bearer", header: func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer ")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
			tc.header(req)
			rec := httptest.NewRecorder()
			server.routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
			}
		})
	}
}

func TestReadOnlyAPIRejectsDisabledKey(t *testing.T) {
	plain := "disabled-readonly-key"
	server := newReadOnlyAPITestServer(t, []config.APIKey{{
		ID:       "k-disabled",
		Hash:     testAPIKeyHash(plain),
		Disabled: true,
	}}, 60)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestReadOnlyAPIAcceptsValidKey(t *testing.T) {
	plain := "valid-readonly-key"
	server := newReadOnlyAPITestServer(t, []config.APIKey{{
		ID:   "k-ok",
		Name: "ok",
		Hash: testAPIKeyHash(plain),
	}}, 60)

	t.Run("authorization_bearer", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
		req.Header.Set("Authorization", "Bearer "+plain)
		rec := httptest.NewRecorder()
		server.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v body=%s", err, rec.Body.String())
		}
		if body["ok"] != true {
			t.Fatalf("body = %#v, want ok=true", body)
		}
	})

	t.Run("x_api_key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
		req.Header.Set("X-API-Key", plain)
		rec := httptest.NewRecorder()
		server.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
	})
}

func TestReadOnlyAPIUpdatesLastUsedAt(t *testing.T) {
	plain := "last-used-key"
	before := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	server := newReadOnlyAPITestServer(t, []config.APIKey{{
		ID:         "k-last",
		Hash:       testAPIKeyHash(plain),
		LastUsedAt: before,
	}}, 60)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	req.Header.Set("X-API-Key", plain)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	got := server.cfg.ReadOnlyAPIKeys[0].LastUsedAt
	if !got.After(before) {
		t.Fatalf("LastUsedAt = %v, want after %v", got, before)
	}
}

func TestReadOnlyAPIKeyCannotAccessWriteEndpoints(t *testing.T) {
	plain := "readonly-no-write"
	server := newReadOnlyAPITestServer(t, []config.APIKey{{
		ID:   "k-nowrite",
		Hash: testAPIKeyHash(plain),
	}}, 60)

	paths := []string{
		"/api/config/save",
		"/api/proxy/delete",
		"/api/subscription/add",
		"/api/manual-node/add",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+plain)
			rec := httptest.NewRecorder()
			server.routes().ServeHTTP(rec, req)
			if rec.Code == http.StatusOK {
				t.Fatalf("API key must not access write endpoint %s; got 200 body=%s", path, rec.Body.String())
			}
			if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 401 or 403; body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestReadOnlyAPIRateLimited(t *testing.T) {
	plain := "rate-limited-key"
	server := newReadOnlyAPITestServer(t, []config.APIKey{{
		ID:   "k-rate",
		Hash: testAPIKeyHash(plain),
	}}, 2)

	// Inject a frozen clock so refill does not race; capacity = rate = 2.
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	server.apiKeyLimiter = newAPIKeyRateLimiter(2, func() time.Time { return now })

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
		req.Header.Set("Authorization", "Bearer "+plain)
		rec := httptest.NewRecorder()
		server.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want %d; body=%s", i+1, rec.Code, http.StatusOK, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over-limit status = %d, want %d; body=%s", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}

	// After one minute of virtual time, tokens refill.
	now = now.Add(time.Minute)
	server.apiKeyLimiter.now = func() time.Time { return now }
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	req2.Header.Set("Authorization", "Bearer "+plain)
	rec2 := httptest.NewRecorder()
	server.routes().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("after refill status = %d, want %d; body=%s", rec2.Code, http.StatusOK, rec2.Body.String())
	}
}

func TestReadOnlyAPIRateLimitIsPerKey(t *testing.T) {
	plainA := "rate-key-a"
	plainB := "rate-key-b"
	server := newReadOnlyAPITestServer(t, []config.APIKey{
		{ID: "ka", Hash: testAPIKeyHash(plainA)},
		{ID: "kb", Hash: testAPIKeyHash(plainB)},
	}, 1)

	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	server.apiKeyLimiter = newAPIKeyRateLimiter(1, func() time.Time { return now })

	// Exhaust key A.
	reqA := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	reqA.Header.Set("X-API-Key", plainA)
	recA := httptest.NewRecorder()
	server.routes().ServeHTTP(recA, reqA)
	if recA.Code != http.StatusOK {
		t.Fatalf("key A first status = %d, want 200", recA.Code)
	}
	reqA2 := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	reqA2.Header.Set("X-API-Key", plainA)
	recA2 := httptest.NewRecorder()
	server.routes().ServeHTTP(recA2, reqA2)
	if recA2.Code != http.StatusTooManyRequests {
		t.Fatalf("key A second status = %d, want 429", recA2.Code)
	}

	// Key B still has its own budget.
	reqB := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	reqB.Header.Set("X-API-Key", plainB)
	recB := httptest.NewRecorder()
	server.routes().ServeHTTP(recB, reqB)
	if recB.Code != http.StatusOK {
		t.Fatalf("key B status = %d, want 200; body=%s", recB.Code, recB.Body.String())
	}
}

// TestAPIKeyRateLimiterEvictsStaleBuckets constructs many distinct keys, then
// advances virtual time well beyond any reasonable idle TTL and touches a single
// active key. The stale buckets must be evicted so the map does not grow without
// bound.
func TestAPIKeyRateLimiterEvictsStaleBuckets(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	l := newAPIKeyRateLimiter(60, func() time.Time { return now })

	const n = 1000
	for i := 0; i < n; i++ {
		l.allow(fmt.Sprintf("throwaway-key-%d", i))
	}
	if got := bucketCountForTest(l); got != n {
		t.Fatalf("after inserts bucket count = %d, want %d", got, n)
	}

	// Advance far past any sane idle TTL; a single active request must trigger
	// eviction of the now-stale buckets.
	now = now.Add(time.Hour)
	l.allow("active-after-idle")

	if got := bucketCountForTest(l); got != 1 {
		t.Fatalf("after idle sweep bucket count = %d, want 1 (only the active key)", got)
	}
}

func TestAPIKeyRateLimiterRejectsNewBucketAtHardLimit(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	l := newAPIKeyRateLimiter(60, func() time.Time { return now })

	for i := 0; i < maxBuckets; i++ {
		if !l.allow(fmt.Sprintf("key-%d", i)) {
			t.Fatalf("bucket %d rejected before hard limit", i)
		}
	}
	if l.allow("over-limit") {
		t.Fatal("new bucket was allowed after reaching the hard limit")
	}
	if got := bucketCountForTest(l); got != maxBuckets {
		t.Fatalf("bucket count = %d, want hard limit %d", got, maxBuckets)
	}
}

// TestAPIKeyRateLimiterKeepsActiveBucket proves eviction does not remove buckets
// that keep being used: an active key touched throughout the window survives while
// an untouched stale key is evicted.
func TestAPIKeyRateLimiterKeepsActiveBucket(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	l := newAPIKeyRateLimiter(60, func() time.Time { return now })

	l.allow("active-key")
	l.allow("stale-key")

	// Touch the active key every 30s for an hour; never touch the stale key.
	for i := 0; i < 120; i++ {
		now = now.Add(30 * time.Second)
		l.allow("active-key")
	}

	if !bucketExistsForTest(l, "active-key") {
		t.Fatalf("active-key was evicted despite continuous use")
	}
	if bucketExistsForTest(l, "stale-key") {
		t.Fatalf("stale-key survived past idle TTL and was not evicted")
	}
}

// TestAPIKeyRateLimiterEvictionDoesNotChangeLimiting proves normal 60/min-per-key
// limiting is unchanged: after eviction of unrelated stale keys, an active key that
// exhausted its budget is still limited until refill.
func TestAPIKeyRateLimiterEvictionDoesNotChangeLimiting(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	l := newAPIKeyRateLimiter(2, func() time.Time { return now })

	// Fill the map with stale keys that will be evicted.
	for i := 0; i < 500; i++ {
		l.allow(fmt.Sprintf("noise-%d", i))
	}

	// Exhaust the active key's 2-token budget.
	if !l.allow("k") {
		t.Fatalf("request 1 should be allowed")
	}
	if !l.allow("k") {
		t.Fatalf("request 2 should be allowed")
	}
	if l.allow("k") {
		t.Fatalf("request 3 should be rejected (budget exhausted)")
	}

	// Advance past idle TTL so noise keys get evicted, but the active key is
	// touched now and must follow the same refill rule (full refill in 60s).
	now = now.Add(time.Hour)
	// After a full minute of idle the active key refills to capacity, so it is
	// allowed again — this is refill, identical to pre-eviction semantics.
	if !l.allow("k") {
		t.Fatalf("after >=60s refill the active key should be allowed again")
	}
	// Noise keys must have been evicted.
	if got := bucketCountForTest(l); got != 1 {
		t.Fatalf("after idle sweep bucket count = %d, want 1 (only active key)", got)
	}
}

func bucketCountForTest(l *apiKeyRateLimiter) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

func bucketExistsForTest(l *apiKeyRateLimiter, key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.buckets[key]
	return ok
}

func TestAPIKeyMiddlewareConcurrentSafe(t *testing.T) {
	plain := "concurrent-key"
	server := newReadOnlyAPITestServer(t, []config.APIKey{{
		ID:   "k-conc",
		Hash: testAPIKeyHash(plain),
	}}, 1000)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	server.apiKeyLimiter = newAPIKeyRateLimiter(1000, func() time.Time { return now })

	var wg sync.WaitGroup
	errCh := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
			req.Header.Set("Authorization", "Bearer "+plain)
			rec := httptest.NewRecorder()
			server.routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				errCh <- fmt.Errorf("status = %d, want 200", rec.Code)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

func TestEnsureAPIKeyLimiterConcurrentSafe(t *testing.T) {
	server := newReadOnlyAPITestServer(t, nil, 60)
	server.apiKeyLimiter = nil

	const workers = 32
	start := make(chan struct{})
	results := make(chan *apiKeyRateLimiter, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- server.ensureAPIKeyLimiter()
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	var first *apiKeyRateLimiter
	for limiter := range results {
		if first == nil {
			first = limiter
			continue
		}
		if limiter != first {
			t.Fatal("concurrent initialization returned different limiter instances")
		}
	}
}

func TestAPIKeyAuthVsRevokeConcurrentSafe(t *testing.T) {
	plain := "concurrent-revoke-key"
	server := newReadOnlyAPITestServer(t, []config.APIKey{{
		ID:   "k-revoke-conc",
		Hash: testAPIKeyHash(plain),
	}}, 1000)
	setTestGlobalConfig(t, server.cfg)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	server.apiKeyLimiter = newAPIKeyRateLimiter(1000, func() time.Time { return now })

	mux := server.routes()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	errCh := make(chan error, 64)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
				req.Header.Set("Authorization", "Bearer "+plain)
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK && rec.Code != http.StatusUnauthorized {
					errCh <- fmt.Errorf("status = %d, want 200 or 401", rec.Code)
					return
				}
			}
		}()
	}

	serveAuthenticated(t, server, "/api/apikey/revoke", `{"id":"k-revoke-conc"}`, http.StatusOK)
	close(stop)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status after revoke = %d, want %d; body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}
