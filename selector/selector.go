package selector

import (
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand"
	"sort"
	"strings"

	"goproxy/affinity"
	"goproxy/auth"
	"goproxy/storage"
)

const unknownLatencyRank = 1 << 30

// sessionSpreadTopK 限定 session 首次绑定时的候选节点数：
// 在该地域延迟最低的前 K 个节点中按 session 哈希分散，
// 兼顾出口质量（只用最快的几个）与分散性（不同 session 落到不同节点）。
const sessionSpreadTopK = 5

var ErrNoNode = errors.New("no available node")

type Store interface {
	GetByRegion(region string, excludes []string) ([]storage.Proxy, error)
	GetProxyByAddress(address string) (*storage.Proxy, error)
}

func Pick(store Store, region string, excludes []string) (*storage.Proxy, error) {
	region = normalizeRegion(region)
	proxies, err := store.GetByRegion(region, excludes)
	if err != nil {
		return nil, err
	}
	available := availableProxies(proxies)
	if len(available) == 0 {
		return nil, noNodeError(region)
	}
	return pickLowestLatency(available), nil
}

func Resolve(store Store, sessions *affinity.Store, route auth.ParsedUsername, excludes []string) (*storage.Proxy, error) {
	if route.Session == "" {
		return Pick(store, route.Region, excludes)
	}
	proxy, rebindRegion := resolveBoundProxy(store, sessions, route, excludes)
	if proxy != nil {
		return proxy, nil
	}
	proxy, err := pickForSession(store, rebindRegion, route.Session, excludes)
	if err != nil {
		return nil, err
	}
	sessions.Set(route.Session, proxy.Address, proxy.Region)
	return proxy, nil
}

// pickForSession 为一个 session 首次绑定选节点：在该地域延迟最低的前 K 个候选中，
// 按 session 名哈希稳定地选择一个。同一 session 恒定映射同一节点（配合黏连），
// 不同 session 分散到不同节点，同时把候选限制在最快的 K 个以保证出口质量。
func pickForSession(store Store, region, session string, excludes []string) (*storage.Proxy, error) {
	region = normalizeRegion(region)
	proxies, err := store.GetByRegion(region, excludes)
	if err != nil {
		return nil, err
	}
	available := availableProxies(proxies)
	if len(available) == 0 {
		return nil, noNodeError(region)
	}
	candidates := topKByLatency(available, sessionSpreadTopK)
	idx := hashString(session) % uint32(len(candidates))
	picked := candidates[idx]
	return &picked, nil
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
		return sorted[i].Address < sorted[j].Address
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

func resolveBoundProxy(store Store, sessions *affinity.Store, route auth.ParsedUsername, excludes []string) (*storage.Proxy, string) {
	binding, ok := sessions.Get(route.Session)
	if !ok {
		return nil, route.Region
	}
	rebindRegion := requestedOrBoundRegion(route.Region, binding.Region)
	if excluded(binding.NodeAddress, excludes) || bindingRegionMismatch(binding, route.Region) {
		return nil, rebindRegion
	}
	proxy, err := store.GetProxyByAddress(binding.NodeAddress)
	if err != nil || !proxyAvailable(*proxy) || regionMismatch(proxy.Region, route.Region) {
		sessions.Remove(route.Session)
		return nil, rebindRegion
	}
	return proxy, rebindRegion
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

func excluded(address string, excludes []string) bool {
	for _, exclude := range excludes {
		if exclude == address {
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
