package webui

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"
	"time"

	"goproxy/config"
)

// apiKeyRateLimiter is an in-memory per-key token bucket.
//
// ratePerMin is fixed at limiter
// construction time (server startup) from config.ReadOnlyAPIRatePerMin. There is
// no hot-reload path; a config change to the rate takes effect only after a
// restart. This is intentional and documented in docs/READONLY_API_DESIGN.md so
// callers are not misled into thinking a live edit re-tunes the limiter.
//
// buckets is a per-key map. Without
// eviction, forged/rotated/one-shot keys would accumulate buckets forever and
// leak memory in a long-running process. We evict buckets that have been idle
// longer than bucketIdleTTL. Eviction is safe for limiting semantics because a
// bucket idle for >= 60s has already fully refilled to capacity; recreating it
// lazily at full capacity on the next request is identical to keeping it. The
// sweep runs under l.mu (reusing the existing lock), is throttled to at most
// once per sweepInterval, and is forced whenever the map reaches maxBuckets so
// growth is hard-bounded even under a burst of distinct keys.
type apiKeyRateLimiter struct {
	mu         sync.Mutex
	ratePerMin int
	now        func() time.Time
	buckets    map[string]*tokenBucket
	lastSweep  time.Time
}

type tokenBucket struct {
	tokens   float64
	lastSeen time.Time
}

const (
	// bucketIdleTTL is how long a bucket may be untouched before it is evicted.
	// Any bucket idle this long has fully refilled to capacity (full refill from
	// empty always takes 60s), so eviction never changes limiting behavior.
	bucketIdleTTL = 10 * time.Minute
	// sweepInterval throttles how often the idle sweep runs, so a busy limiter
	// does not walk the whole map on every request.
	sweepInterval = time.Minute
	// maxBuckets is a hard cap that forces an immediate sweep regardless of the
	// throttle, bounding memory even under a flood of distinct (e.g. forged) keys.
	maxBuckets = 10000
)

func newAPIKeyRateLimiter(ratePerMin int, now func() time.Time) *apiKeyRateLimiter {
	if ratePerMin <= 0 {
		ratePerMin = 60
	}
	if now == nil {
		now = time.Now
	}
	return &apiKeyRateLimiter{
		ratePerMin: ratePerMin,
		now:        now,
		buckets:    make(map[string]*tokenBucket),
	}
}

func (l *apiKeyRateLimiter) allow(keyID string) bool {
	if l == nil || keyID == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.evictStaleLocked(now)

	b, ok := l.buckets[keyID]
	if !ok {
		if len(l.buckets) >= maxBuckets {
			return false
		}
		b = &tokenBucket{tokens: float64(l.ratePerMin), lastSeen: now}
		l.buckets[keyID] = b
	} else {
		elapsed := now.Sub(b.lastSeen).Seconds()
		if elapsed > 0 {
			// Refill ratePerMin tokens per 60 seconds.
			b.tokens += elapsed * float64(l.ratePerMin) / 60.0
			if b.tokens > float64(l.ratePerMin) {
				b.tokens = float64(l.ratePerMin)
			}
			b.lastSeen = now
		}
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// evictStaleLocked removes buckets idle longer than bucketIdleTTL. It must be
// called with l.mu held. The sweep is throttled to once per sweepInterval unless
// the map has reached maxBuckets, in which case it runs immediately to keep
// memory bounded.
func (l *apiKeyRateLimiter) evictStaleLocked(now time.Time) {
	forced := len(l.buckets) >= maxBuckets
	if !forced && !l.lastSweep.IsZero() && now.Sub(l.lastSweep) < sweepInterval {
		return
	}
	l.lastSweep = now
	for id, b := range l.buckets {
		if now.Sub(b.lastSeen) >= bucketIdleTTL {
			delete(l.buckets, id)
		}
	}
}

func extractAPIKey(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		const prefix = "Bearer "
		if strings.HasPrefix(auth, prefix) {
			return strings.TrimSpace(auth[len(prefix):])
		}
		// Also accept "bearer " case-insensitively for the scheme only.
		if len(auth) > len(prefix) && strings.EqualFold(auth[:len(prefix)], prefix) {
			return strings.TrimSpace(auth[len(prefix):])
		}
	}
	return strings.TrimSpace(r.Header.Get("X-API-Key"))
}

// apiKeySHA256 is the webui-side seam for API-key hashing. Behavior is unchanged
// (bare SHA-256 hex); it now delegates to config.HashAPIKey so there is a single
// canonical implementation shared by both packages. The signature is
// preserved so existing callers and tests are unaffected.
func apiKeySHA256(plain string) string {
	return config.HashAPIKey(plain)
}

type readOnlyAPIKeyMatch struct {
	ID   string
	Hash string
}

// matchReadOnlyAPIKey returns the matched key index, or -1.
func matchReadOnlyAPIKey(cfg *config.Config, plain string) int {
	if cfg == nil || plain == "" {
		return -1
	}
	want := apiKeySHA256(plain)
	for i, key := range cfg.ReadOnlyAPIKeys {
		if key.Disabled || key.Hash == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(key.Hash), []byte(want)) == 1 {
			return i
		}
	}
	return -1
}

func (s *Server) readOnlyAPIKeyMatch(plain string) (readOnlyAPIKeyMatch, bool) {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	idx := matchReadOnlyAPIKey(s.cfg, plain)
	if idx < 0 {
		return readOnlyAPIKeyMatch{}, false
	}
	key := s.cfg.ReadOnlyAPIKeys[idx]
	return readOnlyAPIKeyMatch{ID: key.ID, Hash: key.Hash}, true
}

func (s *Server) markReadOnlyAPIKeyUsed(plain string, usedAt time.Time) bool {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	idx := matchReadOnlyAPIKey(s.cfg, plain)
	if idx < 0 {
		return false
	}
	oldCfg := *s.cfg
	keys := append([]config.APIKey(nil), oldCfg.ReadOnlyAPIKeys...)
	keys[idx].LastUsedAt = usedAt
	newCfg := oldCfg
	newCfg.ReadOnlyAPIKeys = keys
	s.cfg = &newCfg
	return true
}

func (s *Server) ensureAPIKeyLimiter() *apiKeyRateLimiter {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	if s.apiKeyLimiter != nil {
		return s.apiKeyLimiter
	}
	rate := 60
	if s.cfg != nil && s.cfg.ReadOnlyAPIRatePerMin > 0 {
		rate = s.cfg.ReadOnlyAPIRatePerMin
	}
	s.apiKeyLimiter = newAPIKeyRateLimiter(rate, time.Now)
	return s.apiKeyLimiter
}

// apiKeyMiddleware authenticates read-only API keys and enforces per-key rate limits.
func (s *Server) apiKeyMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		plain := extractAPIKey(r)
		key, ok := s.readOnlyAPIKeyMatch(plain)
		if !ok {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		bucketID := key.ID
		if bucketID == "" {
			bucketID = key.Hash
		}
		if !s.ensureAPIKeyLimiter().allow(bucketID) {
			jsonError(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		if !s.markReadOnlyAPIKeyUsed(plain, time.Now().UTC()) {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// apiV1Ping is a minimal stub for read-only API key middleware tests.
func (s *Server) apiV1Ping(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}
