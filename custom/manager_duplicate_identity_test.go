package custom

import (
	"strings"
	"testing"

	"github.com/babutree/GeoProxy/storage"
	"github.com/babutree/GeoProxy/validator"
)

func TestReplaceSubscriptionProxiesDeduplicatesEquivalentEntries(t *testing.T) {
	store := newTestStorage(t)
	subID := managerIdentityTestSubscription(t, store)
	secondID := seedManagerSubscriptionProxy(t, store, subID, "second.example:8080", "second-key", false)
	m := &Manager{storage: store}

	entries := []subscriptionProxyEntry{
		{addr: "first.example:8080", proto: "http", username: "alice", password: "secret", nodeKey: "first-key"},
		{addr: "first.example:8080", proto: "http", username: "alice", password: "secret", nodeKey: "first-key"},
		{addr: "second.example:8080", proto: "socks5", dual: true, nodeKey: "second-key"},
		{addr: "second.example:8080", proto: "socks5", dual: true, nodeKey: "second-key"},
	}

	proxies, err := m.replaceSubscriptionProxies(subID, entries)
	if err != nil {
		t.Fatalf("replaceSubscriptionProxies() error = %v", err)
	}
	if len(proxies) != 2 {
		t.Fatalf("returned proxies = %d, want 2", len(proxies))
	}
	if proxies[0].Address != "first.example:8080" || proxies[1].Address != "second.example:8080" {
		t.Fatalf("returned order = %q, %q; want first occurrence order", proxies[0].Address, proxies[1].Address)
	}
	if proxies[0].ID == 0 || proxies[1].ID == 0 || proxies[0].ID == proxies[1].ID {
		t.Fatalf("returned IDs = %d, %d; want two distinct database identities", proxies[0].ID, proxies[1].ID)
	}
	if proxies[1].ID != secondID {
		t.Fatalf("second returned ID = %d, want preserved database identity %d", proxies[1].ID, secondID)
	}

	var rowCount int
	if err := store.GetDB().QueryRow(
		`SELECT COUNT(*) FROM proxies WHERE source = ? AND subscription_id = ?`,
		storage.SourceSubscription, subID,
	).Scan(&rowCount); err != nil {
		t.Fatalf("count subscription proxies: %v", err)
	}
	if rowCount != 2 {
		t.Fatalf("database proxy rows = %d, want 2", rowCount)
	}
	sub, err := store.GetSubscription(subID)
	if err != nil {
		t.Fatalf("GetSubscription() error = %v", err)
	}
	if sub.ProxyCount != 2 {
		t.Fatalf("subscription proxy_count = %d, want 2", sub.ProxyCount)
	}
}

func TestReplaceSubscriptionProxiesKeepsFirstOnConflictingNodeKey(t *testing.T) {
	store := newTestStorage(t)
	subID := managerIdentityTestSubscription(t, store)
	_ = seedManagerSubscriptionProxy(t, store, subID, "old.example:8080", "stable-key", true)
	m := &Manager{storage: store}

	proxies, err := m.replaceSubscriptionProxies(subID, []subscriptionProxyEntry{
		{addr: "new-a.example:8080", proto: "http", username: "alice", password: "one", nodeKey: "stable-key"},
		{addr: "new-b.example:8080", proto: "socks5", username: "bob", password: "two", nodeKey: "stable-key"},
	})
	if err != nil {
		t.Fatalf("replaceSubscriptionProxies() error = %v, want keep-first skip conflict", err)
	}
	if len(proxies) != 1 {
		t.Fatalf("returned proxies = %d, want 1 (first occurrence kept)", len(proxies))
	}
	if proxies[0].Address != "new-a.example:8080" || proxies[0].NodeKey != "stable-key" {
		t.Fatalf("kept proxy = %+v, want first occurrence new-a/stable-key", proxies[0])
	}
	var rowCount int
	if err := store.GetDB().QueryRow(
		`SELECT COUNT(*) FROM proxies WHERE source = ? AND subscription_id = ?`,
		storage.SourceSubscription, subID,
	).Scan(&rowCount); err != nil {
		t.Fatalf("count subscription proxies: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("database proxy rows after keep-first = %d, want 1", rowCount)
	}
}

func TestReplaceSubscriptionProxiesKeepsFirstOnConflictingAddress(t *testing.T) {
	store := newTestStorage(t)
	subID := managerIdentityTestSubscription(t, store)
	_ = seedManagerSubscriptionProxy(t, store, subID, "same.example:8080", "old-key", false)
	m := &Manager{storage: store}

	proxies, err := m.replaceSubscriptionProxies(subID, []subscriptionProxyEntry{
		{addr: "same.example:8080", proto: "http", nodeKey: "new-key"},
		{addr: "same.example:8080", proto: "socks5", dual: true, username: "alice", password: "secret", nodeKey: "other-key"},
	})
	if err != nil {
		t.Fatalf("replaceSubscriptionProxies() error = %v, want keep-first skip conflict", err)
	}
	if len(proxies) != 1 {
		t.Fatalf("returned proxies = %d, want 1", len(proxies))
	}
	if proxies[0].Protocol != "http" || proxies[0].NodeKey != "new-key" {
		t.Fatalf("kept proxy = %+v, want first occurrence http/new-key", proxies[0])
	}
}

func TestReplaceSubscriptionProxiesConflictKeepsFirstWrite(t *testing.T) {
	store := newTestStorage(t)
	subID := managerIdentityTestSubscription(t, store)
	m := &Manager{storage: store}

	proxies, err := m.replaceSubscriptionProxies(subID, []subscriptionProxyEntry{
		{addr: "preflight.example:8080", proto: "http", nodeKey: "preflight-key"},
		{addr: "preflight.example:8080", proto: "http", nodeKey: "different-key"},
	})
	if err != nil {
		t.Fatalf("replaceSubscriptionProxies() error = %v, want keep-first", err)
	}
	if len(proxies) != 1 {
		t.Fatalf("returned proxies = %d, want 1", len(proxies))
	}

	var count int
	if err := store.GetDB().QueryRow(
		`SELECT COUNT(*) FROM proxies WHERE source = ? AND subscription_id = ?`,
		storage.SourceSubscription, subID,
	).Scan(&count); err != nil {
		t.Fatalf("count rows after keep-first: %v", err)
	}
	if count != 1 {
		t.Fatalf("proxy rows after keep-first = %d, want 1", count)
	}
	sub, err := store.GetSubscription(subID)
	if err != nil {
		t.Fatalf("GetSubscription() error = %v", err)
	}
	if sub.ProxyCount != 1 {
		t.Fatalf("subscription proxy_count = %d, want 1 after keep-first write", sub.ProxyCount)
	}
}

func TestRefreshSubscriptionKeepsFirstOnDirectAddressConflict(t *testing.T) {
	store := newTestStorage(t)
	file := writeSubscriptionFile(t, strings.Join([]string{
		"proxies:",
		"  - name: first",
		"    type: http",
		"    server: same.example",
		"    port: 8080",
		"    username: alice",
		"    password: one",
		"  - name: second",
		"    type: socks5",
		"    server: same.example",
		"    port: 8080",
		"    username: bob",
		"    password: two",
	}, "\n"))
	subID, err := store.AddSubscription("duplicate address", "", file, "clash", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	m := &Manager{
		storage:   store,
		validator: validator.New(1, 1, "http://127.0.0.1/validate"),
		singbox:   newSpyShard(),
	}

	// keep-first：同 address 冲突不再整单失败，保留 http 首项并完成刷新。
	if err := m.RefreshSubscription(subID); err != nil {
		t.Fatalf("RefreshSubscription() error = %v, want nil with keep-first", err)
	}
	var count int
	if err := store.GetDB().QueryRow(
		"SELECT COUNT(*) FROM proxies WHERE source = ? AND subscription_id = ?",
		storage.SourceSubscription, subID,
	).Scan(&count); err != nil {
		t.Fatalf("count subscription rows after keep-first: %v", err)
	}
	if count != 1 {
		t.Fatalf("subscription rows after keep-first = %d, want 1", count)
	}
	proxy, err := store.GetProxyByIdentity("same.example:8080", storage.SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() error = %v", err)
	}
	if proxy.Protocol != "http" {
		t.Fatalf("kept protocol = %q, want http (first occurrence)", proxy.Protocol)
	}
}
