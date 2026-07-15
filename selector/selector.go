package selector

import (
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand"
	"sort"
	"strings"
	"time"

	"goproxy/affinity"
	"goproxy/auth"
	"goproxy/config"
	"goproxy/storage"
)

const unknownLatencyRank = 1 << 30

// sessionSpreadTopK 限定 session 首次绑定时的候选节点数：
// 在该地域延迟最低的前 K 个节点中按 session 哈希分散，
// 兼顾出口质量（只用最快的几个）与分散性（不同 session 落到不同节点）。
const sessionSpreadTopK = 5

var ErrNoNode = errors.New("no available node")

// afterFirstBindPickHook is a test-only seam: when set, Resolve invokes it after
// pickForSession returns a candidate and before the bind write. Production leaves it nil.
var afterFirstBindPickHook func()

type Store interface {
	GetByRegion(region string, excludes []int64) ([]storage.Proxy, error)
	GetProxyByID(id int64) (*storage.Proxy, error)
}

func Pick(store Store, region string, excludes []int64) (*storage.Proxy, error) {
	return PickUnlock(store, region, excludes, nil)
}

// PickUnlock 在地域选路基础上叠加 unlock 过滤（openai/claude/grok/gemini/cf）。
// unlock 为空时与 Pick 完全一致。
func PickUnlock(store Store, region string, excludes []int64, unlock []string) (*storage.Proxy, error) {
	region = normalizeRegion(region)
	proxies, err := store.GetByRegion(region, excludes)
	if err != nil {
		return nil, err
	}
	available := availableProxies(proxies)
	available = filterByUnlock(available, unlock)
	if len(available) == 0 {
		return nil, noNodeError(region)
	}
	return pickLowestLatency(available), nil
}

func Resolve(store Store, sessions *affinity.Store, route auth.ParsedUsername, excludes []int64) (*storage.Proxy, error) {
	if route.Session == "" {
		return PickUnlock(store, route.Region, excludes, route.Unlock)
	}
	// Sticky fast path: no first-bind lock (read + refresh only).
	if proxy, ok := stickyBoundProxy(store, sessions, route, excludes); ok {
		return proxy, nil
	}
	if sessions == nil {
		return pickForSession(store, nil, route.Region, route.Session, excludes, maxSessionsPerProxy(), proxyCooldownMinutes(), route.Unlock)
	}

	// First-bind / rebind: serialize check + occupancy release + pick + write.
	sessions.BeginFirstBind()
	defer sessions.EndFirstBind()

	if proxy, ok := stickyBoundProxy(store, sessions, route, excludes); ok {
		return proxy, nil
	}
	rebindRegion := releaseStaleBinding(sessions, route, excludes)
	maxSessions := maxSessionsPerProxy()
	cooldownMinutes := proxyCooldownMinutes()
	proxy, err := pickForSession(store, sessions, rebindRegion, route.Session, excludes, maxSessions, cooldownMinutes, route.Unlock)
	if err != nil {
		return nil, err
	}
	if afterFirstBindPickHook != nil {
		afterFirstBindPickHook()
	}
	sessions.SetProxy(route.Session, proxy.ID, proxy.Address, proxy.Region)
	if cooldownMinutes > 0 {
		sessions.SetCooldown(proxy.ID, sessionsNow(sessions).Add(time.Duration(cooldownMinutes)*time.Minute))
	}
	return proxy, nil
}

// pickForSession 为一个 session 首次绑定选节点：在该地域延迟最低的前 K 个候选中，
// 按 session 名哈希稳定地选择一个。同一 session 恒定映射同一节点（配合黏连），
// 不同 session 分散到不同节点，同时把候选限制在最快的 K 个以保证出口质量。
// maxSessions > 0 时过滤 occupancy >= max 的节点；0 表示不限制。
// cooldownMinutes > 0 时过滤 InCooldown 节点；0 表示忽略冷却表（读侧关闭）。
func pickForSession(store Store, sessions *affinity.Store, region, session string, excludes []int64, maxSessions, cooldownMinutes int, unlock []string) (*storage.Proxy, error) {
	region = normalizeRegion(region)
	proxies, err := store.GetByRegion(region, excludes)
	if err != nil {
		return nil, err
	}
	available := availableProxies(proxies)
	available = filterByUnlock(available, unlock)
	if len(available) == 0 {
		return nil, noNodeError(region)
	}
	if maxSessions > 0 && sessions != nil {
		filtered := filterByOccupancy(available, sessions, maxSessions)
		if len(filtered) == 0 {
			return nil, noNodeCapacityError(region)
		}
		available = filtered
	}
	if cooldownMinutes > 0 && sessions != nil {
		filtered := filterByCooldown(available, sessions)
		if len(filtered) == 0 {
			return nil, noNodeCooldownError(region)
		}
		available = filtered
	}
	candidates := topKByLatency(available, sessionSpreadTopK)
	idx := hashString(session) % uint32(len(candidates))
	picked := candidates[idx]
	return &picked, nil
}

func filterByOccupancy(proxies []storage.Proxy, sessions *affinity.Store, maxSessions int) []storage.Proxy {
	if maxSessions <= 0 || sessions == nil {
		return proxies
	}
	out := make([]storage.Proxy, 0, len(proxies))
	for _, proxy := range proxies {
		if sessions.CountByProxy(proxy.ID) < maxSessions {
			out = append(out, proxy)
		}
	}
	return out
}

func filterByCooldown(proxies []storage.Proxy, sessions *affinity.Store) []storage.Proxy {
	if sessions == nil {
		return proxies
	}
	out := make([]storage.Proxy, 0, len(proxies))
	for _, proxy := range proxies {
		if !sessions.InCooldown(proxy.ID) {
			out = append(out, proxy)
		}
	}
	return out
}

func maxSessionsPerProxy() int {
	cfg := config.Get()
	if cfg == nil {
		return 0
	}
	if cfg.MaxSessionsPerProxy < 0 {
		return 0
	}
	return cfg.MaxSessionsPerProxy
}

func proxyCooldownMinutes() int {
	cfg := config.Get()
	if cfg == nil {
		return 0
	}
	if cfg.ProxyCooldownMinutes < 0 {
		return 0
	}
	return cfg.ProxyCooldownMinutes
}

// sessionsNow returns the affinity store clock so cooldown_until aligns with
// InCooldown under injectable test clocks.
func sessionsNow(sessions *affinity.Store) time.Time {
	if sessions == nil {
		return time.Now()
	}
	return sessions.Now()
}

// topKByLatency 返回按延迟升序排列的前 k 个节点（不足 k 个则全返回）。
// 地址作为次级排序键，保证结果稳定、与输入顺序无关。
func topKByLatency(proxies []storage.Proxy, k int) []storage.Proxy {
	sorted := make([]storage.Proxy, len(proxies))
	copy(sorted, proxies)
	sort.Slice(sorted, func(i, j int) bool {
		ri, rj := latencyRank(sorted[i].Latency), latencyRank(sorted[j].Latency)
		if ri != rj {
			return ri < rj
		}
		if sorted[i].Address != sorted[j].Address {
			return sorted[i].Address < sorted[j].Address
		}
		return sorted[i].ID < sorted[j].ID
	})
	if len(sorted) > k {
		sorted = sorted[:k]
	}
	return sorted
}

func hashString(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}

// stickyBoundProxy returns a live, region-matching binding without mutating occupancy.
// Callers that need rebind must use releaseStaleBinding under the first-bind lock.
func stickyBoundProxy(store Store, sessions *affinity.Store, route auth.ParsedUsername, excludes []int64) (*storage.Proxy, bool) {
	if sessions == nil {
		return nil, false
	}
	binding, ok := sessions.Get(route.Session)
	if !ok {
		return nil, false
	}
	if binding.ProxyID <= 0 || excluded(binding.ProxyID, excludes) || bindingRegionMismatch(binding, route.Region) {
		return nil, false
	}
	proxy, err := store.GetProxyByID(binding.ProxyID)
	if err != nil || proxy == nil || !proxyAvailable(*proxy) || regionMismatch(proxy.Region, route.Region) {
		return nil, false
	}
	// sticky 绑定也必须满足当前 unlock 过滤，否则释放并重新选路。
	if !proxyMatchesUnlock(*proxy, route.Unlock) {
		return nil, false
	}
	return proxy, true
}

// releaseStaleBinding drops a non-sticky binding so its occupancy does not block rebind.
// Must run under first-bind serialization.
func releaseStaleBinding(sessions *affinity.Store, route auth.ParsedUsername, excludes []int64) string {
	binding, ok := sessions.Get(route.Session)
	if !ok {
		return route.Region
	}
	rebindRegion := requestedOrBoundRegion(route.Region, binding.Region)
	if binding.ProxyID <= 0 || excluded(binding.ProxyID, excludes) || bindingRegionMismatch(binding, route.Region) {
		sessions.Remove(route.Session)
		return rebindRegion
	}
	// Binding exists but stickyBoundProxy already rejected it (dead node / region).
	sessions.Remove(route.Session)
	return rebindRegion
}

func availableProxies(proxies []storage.Proxy) []storage.Proxy {
	available := make([]storage.Proxy, 0, len(proxies))
	for _, proxy := range proxies {
		if proxyAvailable(proxy) {
			available = append(available, proxy)
		}
	}
	return available
}

func pickLowestLatency(proxies []storage.Proxy) *storage.Proxy {
	bestRank := latencyRank(proxies[0].Latency)
	candidates := []storage.Proxy{proxies[0]}
	for _, proxy := range proxies[1:] {
		rank := latencyRank(proxy.Latency)
		if rank < bestRank {
			bestRank = rank
			candidates = []storage.Proxy{proxy}
			continue
		}
		if rank == bestRank {
			candidates = append(candidates, proxy)
		}
	}
	picked := candidates[rand.Intn(len(candidates))]
	return &picked
}

func proxyAvailable(proxy storage.Proxy) bool {
	return (proxy.Status == "active" || proxy.Status == "degraded") && proxy.FailCount < 3
}

func latencyRank(latency int) int {
	if latency <= 0 {
		return unknownLatencyRank
	}
	return latency
}

func excluded(proxyID int64, excludes []int64) bool {
	for _, exclude := range excludes {
		if exclude == proxyID {
			return true
		}
	}
	return false
}

func bindingRegionMismatch(binding affinity.Binding, region string) bool {
	return regionMismatch(binding.Region, region)
}

func requestedOrBoundRegion(requestedRegion string, boundRegion string) string {
	if requestedRegion != "" {
		return requestedRegion
	}
	return boundRegion
}

func regionMismatch(nodeRegion string, requestedRegion string) bool {
	return requestedRegion != "" && normalizeRegion(nodeRegion) != normalizeRegion(requestedRegion)
}

func normalizeRegion(region string) string {
	return strings.ToLower(strings.TrimSpace(region))
}

func noNodeError(region string) error {
	if region == "" {
		return ErrNoNode
	}
	return fmt.Errorf("%w for region: %s", ErrNoNode, region)
}

func noNodeCapacityError(region string) error {
	if region == "" {
		return fmt.Errorf("%w (capacity)", ErrNoNode)
	}
	return fmt.Errorf("%w for region: %s (capacity)", ErrNoNode, region)
}

func noNodeCooldownError(region string) error {
	if region == "" {
		return fmt.Errorf("%w (cooldown)", ErrNoNode)
	}
	return fmt.Errorf("%w for region: %s (cooldown)", ErrNoNode, region)
}
