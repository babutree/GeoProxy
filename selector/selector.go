package selector

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"

	"goproxy/affinity"
	"goproxy/auth"
	"goproxy/storage"
)

const unknownLatencyRank = 1 << 30

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
	proxy, err := Pick(store, rebindRegion, excludes)
	if err != nil {
		return nil, err
	}
	sessions.Set(route.Session, proxy.Address, proxy.Region)
	return proxy, nil
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
