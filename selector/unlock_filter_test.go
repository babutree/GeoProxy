package selector

import (
	"errors"
	"testing"

	"goproxy/auth"
	"goproxy/storage"
)

func TestPickFiltersByGPTUnlock(t *testing.T) {
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "gpt-ok:1", Region: "us", Latency: 50, Status: "active", AIReachability: `{"openai":0,"claude":1}`, CFBlocked: 1},
		{ID: 2, Address: "gpt-bad:1", Region: "us", Latency: 10, Status: "active", AIReachability: `{"openai":1,"claude":0}`, CFBlocked: 0},
	}}
	proxy, err := PickUnlock(store, "us", nil, []string{"openai"})
	if err != nil {
		t.Fatalf("PickUnlock() error = %v", err)
	}
	if proxy.Address != "gpt-ok:1" {
		t.Fatalf("PickUnlock() = %s, want gpt-ok:1", proxy.Address)
	}
}

func TestPickFiltersByCFOpen(t *testing.T) {
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "cf-block:1", Region: "us", Latency: 5, Status: "active", CFBlocked: 1},
		{ID: 2, Address: "cf-open:1", Region: "us", Latency: 20, Status: "active", CFBlocked: 0},
	}}
	proxy, err := PickUnlock(store, "us", nil, []string{"cf"})
	if err != nil {
		t.Fatalf("PickUnlock() error = %v", err)
	}
	if proxy.Address != "cf-open:1" {
		t.Fatalf("PickUnlock() = %s, want cf-open:1", proxy.Address)
	}
}

func TestPickFiltersAllRequiresFive(t *testing.T) {
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "partial:1", Region: "us", Latency: 5, Status: "active", CFBlocked: 0, AIReachability: `{"openai":0,"claude":0,"grok":0,"gemini":1}`},
		{ID: 2, Address: "full:1", Region: "us", Latency: 30, Status: "active", CFBlocked: 0, AIReachability: `{"openai":0,"claude":0,"grok":0,"gemini":0}`},
	}}
	proxy, err := Resolve(store, nil, auth.ParsedUsername{Region: "us", Unlock: []string{"openai", "claude", "grok", "gemini", "cf"}}, nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if proxy.Address != "full:1" {
		t.Fatalf("Resolve() = %s, want full:1", proxy.Address)
	}
}

func TestPickUnlockNoMatchReturnsNoNode(t *testing.T) {
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "no-gpt:1", Region: "us", Latency: 5, Status: "active", AIReachability: `{"openai":1}`, CFBlocked: 0},
	}}
	_, err := PickUnlock(store, "us", nil, []string{"openai"})
	if !errors.Is(err, ErrNoNode) {
		t.Fatalf("err = %v, want ErrNoNode", err)
	}
}

func TestStickyBindingRejectedWhenUnlockNoLongerMatches(t *testing.T) {
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "old:1", Region: "us", Latency: 5, Status: "active", AIReachability: `{"openai":1}`, CFBlocked: 0},
		{ID: 2, Address: "ok:1", Region: "us", Latency: 20, Status: "active", AIReachability: `{"openai":0}`, CFBlocked: 0},
	}}
	// sticky would prefer id=1, but unlock gpt requires openai=0 → rebind to id=2
	// use Resolve without sessions first for unlock pick path
	proxy, err := Resolve(store, nil, auth.ParsedUsername{Region: "us", Unlock: []string{"openai"}}, []int64{1})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if proxy.Address != "ok:1" {
		t.Fatalf("Resolve() = %s, want ok:1", proxy.Address)
	}
}
