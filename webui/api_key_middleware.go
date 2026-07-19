package webui

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/babutree/GeoProxy/config"
)

// apiKeyRateLimiter 是按 Key 隔离的内存令牌桶。
//
// ratePerMin 在限流器首次构造时从 config.ReadOnlyAPIRatePerMin 读取并固定。
// 当前没有热重载路径；限流器一旦创建，修改速率配置后需重启服务才会生效。
// 该约束已记录在 docs/READONLY_API_DESIGN.md，避免调用方误以为在线编辑
// 配置会立即调整已经运行的限流器。
//
// buckets 按 Key 保存桶状态；若不清理，伪造、轮换或一次性 Key 会持续累积，
// 最终造成长时间运行进程的内存泄漏。空闲超过 bucketIdleTTL 的桶会被清理。
// 桶空闲至少 60 秒后已完全补满，因此下次请求时以满容量延迟重建，
// 与保留原桶的限流语义一致。清理过程持有 l.mu，并复用既有互斥锁；
// sweepInterval 将扫描限制为每个周期最多一次。桶数量达到 maxBuckets 时，
// 即使尚未到周期也会强制清理，从而在不同 Key 突发请求下硬性限制增长。
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
	// bucketIdleTTL 表示桶在被清理前允许保持未访问状态的时长。
	// 桶从空状态完全补满固定需要 60 秒，达到该空闲时长时已恢复容量，
	// 因此清理不会改变限流语义。
	bucketIdleTTL = 10 * time.Minute
	// sweepInterval 限制空闲扫描频率，避免繁忙限流器在每次请求时遍历全表。
	// 该周期只影响清理成本，不改变单个桶的补充速率。
	sweepInterval = time.Minute
	// maxBuckets 是硬上限；达到上限时忽略扫描节流并立即清理，
	// 即使不同的伪造 Key 大量涌入，也能限制桶映射占用的内存。
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
			// 每 60 秒补充 ratePerMin 个令牌。
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

// evictStaleLocked 清理空闲时间不短于 bucketIdleTTL 的桶。
// 调用方必须持有 l.mu。扫描通常按 sweepInterval 节流；
// 映射达到 maxBuckets 时立即执行，不受节流限制，
// 以保证内存占用有界。
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
		// 仅对认证方案名大小写不敏感，同时接受 "bearer "。
		if len(auth) > len(prefix) && strings.EqualFold(auth[:len(prefix)], prefix) {
			return strings.TrimSpace(auth[len(prefix):])
		}
	}
	return strings.TrimSpace(r.Header.Get("X-API-Key"))
}

// apiKeySHA256 是 webui 的 API Key 指纹适配点；当前仍生成裸 SHA-256
// 十六进制兼容指纹，并委托给 config.HashAPIKey，确保两个包共用唯一实现。
// 保留该函数签名以兼容现有调用方和测试；未来格式迁移必须由配置层版本化
// 协调，不能在此形成独立算法。
func apiKeySHA256(plain string) string {
	return config.HashAPIKey(plain)
}

type readOnlyAPIKeyMatch struct {
	ID   string
	Hash string
}

// matchReadOnlyAPIKey 返回匹配的 Key 索引；无匹配时返回 -1。
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

// apiKeyMiddleware 鉴权只读 API Key，并按 Key 执行独立限流。
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

// apiV1Ping 处理只读 API Key 鉴权后的最小探活请求。
func (s *Server) apiV1Ping(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}
