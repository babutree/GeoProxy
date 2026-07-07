package selector

import (
	"errors"
	"testing"
	"time"

	"goproxy/affinity"
	"goproxy/auth"
	"goproxy/storage"
)

type fakeStore struct {
	proxies []storage.Proxy
}

func (s fakeStore) GetByRegion(region string, excludes []string) ([]storage.Proxy, error) {
	excluded := map[string]bool{}
	for _, address := range excludes {
		excluded[address] = true
	}

	var out []storage.Proxy
	for _, proxy := range s.proxies {
		if excluded[proxy.Address] || !proxyAvailable(proxy) {
			continue
		}
		if region != "" && proxy.Region != region {
			continue
		}
		out = append(out, proxy)
	}
	return out, nil
}

func (s fakeStore) GetProxyByAddress(address string) (*storage.Proxy, error) {
	for _, proxy := range s.proxies {
		if proxy.Address == address {
			copy := proxy
			return &copy, nil
		}
	}
	return nil, errors.New("not found")
}

func TestPickHonorsRequestedRegionWithoutFallback(t *testing.T) {
	store := fakeStore{proxies: []storage.Proxy{{Address: "jp:8080", Region: "jp", Status: "active"}}}

	_, err := Pick(store, "us", nil)

	if !errors.Is(err, ErrNoNode) || err.Error() != "no available node for region: us" {
		t.Fatalf("Pick() err = %v, want region-specific ErrNoNode", err)
	}
}

func TestPickReturnsLowestLatencyAndExcludesTried(t *testing.T) {
	store := fakeStore{proxies: []storage.Proxy{
		{Address: "us-slow:8080", Region: "us", Latency: 80, Status: "active"},
		{Address: "us-fast:8080", Region: "us", Latency: 20, Status: "active"},
	}}

	proxy, err := Pick(store, "us", []string{"us-fast:8080"})
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	if proxy.Address != "us-slow:8080" {
		t.Fatalf("Pick() = %s, want us-slow:8080", proxy.Address)
	}
}

func TestResolveRebindsFailedStickyNodeInSameRegion(t *testing.T) {
	store := fakeStore{proxies: []storage.Proxy{
		{Address: "us-old:8080", Region: "us", Latency: 10, Status: "active"},
		{Address: "us-new:8080", Region: "us", Latency: 20, Status: "active"},
		{Address: "jp:8080", Region: "jp", Latency: 1, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, time.Now)
	sessions.Set("abc", "us-old:8080", "us")

	proxy, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "abc"}, []string{"us-old:8080"})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if proxy.Address != "us-new:8080" {
		t.Fatalf("Resolve() = %s, want us-new:8080", proxy.Address)
	}
	binding, ok := sessions.Get("abc")
	if !ok || binding.NodeAddress != "us-new:8080" || binding.Region != "us" {
		t.Fatalf("binding = %#v, %v; want us-new binding", binding, ok)
	}
}

func TestResolveSessionOnlyRebindUsesBoundRegion(t *testing.T) {
	store := fakeStore{proxies: []storage.Proxy{
		{Address: "us-old:8080", Region: "us", Latency: 10, Status: "active"},
		{Address: "us-new:8080", Region: "us", Latency: 20, Status: "active"},
		{Address: "jp-fast:8080", Region: "jp", Latency: 1, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, time.Now)
	sessions.Set("abc", "us-old:8080", "us")

	proxy, err := Resolve(store, sessions, auth.ParsedUsername{Session: "abc"}, []string{"us-old:8080"})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if proxy.Address != "us-new:8080" {
		t.Fatalf("Resolve() = %s, want us-new:8080", proxy.Address)
	}
}
