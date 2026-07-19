package custom

import (
	"strings"
	"testing"
	"time"

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

func TestReplaceSubscriptionProxiesRejectsConflictingNodeKey(t *testing.T) {
	store := newTestStorage(t)
	subID := managerIdentityTestSubscription(t, store)
	oldID := seedManagerSubscriptionProxy(t, store, subID, "old.example:8080", "stable-key", true)
	if _, err := store.GetDB().Exec(
		`UPDATE subscriptions SET proxy_count = ?, last_fetch = ? WHERE id = ?`,
		9, "2026-07-19 08:09:10", subID,
	); err != nil {
		t.Fatalf("seed subscription metadata: %v", err)
	}
	beforeSub, err := store.GetSubscription(subID)
	if err != nil {
		t.Fatalf("GetSubscription() before conflict error = %v", err)
	}
	m := &Manager{storage: store}

	_, err = m.replaceSubscriptionProxies(subID, []subscriptionProxyEntry{
		{addr: "new-a.example:8080", proto: "http", username: "alice", password: "one", nodeKey: "stable-key"},
		{addr: "new-b.example:8080", proto: "socks5", username: "bob", password: "two", nodeKey: "stable-key"},
	})
	if err == nil || !strings.Contains(err.Error(), "node_key") {
		t.Fatalf("replaceSubscriptionProxies() error = %v, want explicit node_key conflict", err)
	}

	row := proxyByIDInTest(t, store, oldID)
	if row.Address != "old.example:8080" || row.NodeKey != "stable-key" || !row.UserPaused || row.Status != "active" {
		t.Fatalf("old proxy after rejected conflict = %+v, want unchanged row", row)
	}
	var rowCount int
	if err := store.GetDB().QueryRow(
		`SELECT COUNT(*) FROM proxies WHERE source = ? AND subscription_id = ?`,
		storage.SourceSubscription, subID,
	).Scan(&rowCount); err != nil {
		t.Fatalf("count subscription proxies: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("database proxy rows after rejected conflict = %d, want 1", rowCount)
	}
	sub, err := store.GetSubscription(subID)
	if err != nil {
		t.Fatalf("GetSubscription() error = %v", err)
	}
	if sub.ProxyCount != beforeSub.ProxyCount || !sub.LastFetch.Equal(beforeSub.LastFetch) {
		t.Fatalf("subscription metadata after rejected conflict = count:%d last_fetch:%v, want unchanged", sub.ProxyCount, sub.LastFetch)
	}
}

func TestReplaceSubscriptionProxiesRejectsConflictingAddress(t *testing.T) {
	store := newTestStorage(t)
	subID := managerIdentityTestSubscription(t, store)
	oldID := seedManagerSubscriptionProxy(t, store, subID, "same.example:8080", "old-key", false)
	if _, err := store.GetDB().Exec(
		`UPDATE subscriptions SET proxy_count = ?, last_fetch = ? WHERE id = ?`,
		4, "2026-07-19 09:10:11", subID,
	); err != nil {
		t.Fatalf("seed subscription metadata: %v", err)
	}
	beforeSub, err := store.GetSubscription(subID)
	if err != nil {
		t.Fatalf("GetSubscription() before conflict error = %v", err)
	}
	m := &Manager{storage: store}

	_, err = m.replaceSubscriptionProxies(subID, []subscriptionProxyEntry{
		{addr: "same.example:8080", proto: "http", nodeKey: "new-key"},
		{addr: "same.example:8080", proto: "socks5", dual: true, username: "alice", password: "secret", nodeKey: "other-key"},
	})
	if err == nil || !strings.Contains(err.Error(), "address") {
		t.Fatalf("replaceSubscriptionProxies() error = %v, want explicit address conflict", err)
	}

	row := proxyByIDInTest(t, store, oldID)
	if row.Address != "same.example:8080" || row.NodeKey != "old-key" || row.Protocol != "http" || row.UserPaused {
		t.Fatalf("old proxy after rejected address conflict = %+v, want unchanged row", row)
	}
	sub, err := store.GetSubscription(subID)
	if err != nil {
		t.Fatalf("GetSubscription() error = %v", err)
	}
	if sub.ProxyCount != beforeSub.ProxyCount || !sub.LastFetch.Equal(beforeSub.LastFetch) {
		t.Fatalf("subscription metadata after rejected conflict = count:%d last_fetch:%v, want unchanged", sub.ProxyCount, sub.LastFetch)
	}
}

func TestReplaceSubscriptionProxiesConflictDoesNotWriteBeforeValidation(t *testing.T) {
	store := newTestStorage(t)
	subID := managerIdentityTestSubscription(t, store)
	if _, err := store.GetDB().Exec(
		`UPDATE subscriptions SET last_fetch = ? WHERE id = ?`,
		time.Date(2026, 7, 19, 10, 11, 12, 0, time.UTC), subID,
	); err != nil {
		t.Fatalf("seed subscription timestamp: %v", err)
	}
	beforeSub, err := store.GetSubscription(subID)
	if err != nil {
		t.Fatalf("GetSubscription() before conflict error = %v", err)
	}
	m := &Manager{storage: store}

	_, err = m.replaceSubscriptionProxies(subID, []subscriptionProxyEntry{
		{addr: "preflight.example:8080", proto: "http", nodeKey: "preflight-key"},
		{addr: "preflight.example:8080", proto: "http", nodeKey: "different-key"},
	})
	if err == nil {
		t.Fatal("replaceSubscriptionProxies() error = nil, want preflight conflict")
	}

	var count int
	if err := store.GetDB().QueryRow(
		`SELECT COUNT(*) FROM proxies WHERE source = ? AND subscription_id = ?`,
		storage.SourceSubscription, subID,
	).Scan(&count); err != nil {
		t.Fatalf("count rows after preflight rejection: %v", err)
	}
	if count != 0 {
		t.Fatalf("proxy rows after preflight rejection = %d, want 0", count)
	}
	sub, err := store.GetSubscription(subID)
	if err != nil {
		t.Fatalf("GetSubscription() error = %v", err)
	}
	if sub.ProxyCount != beforeSub.ProxyCount || !sub.LastFetch.Equal(beforeSub.LastFetch) {
		t.Fatalf("subscription metadata after preflight rejection = count:%d last_fetch:%v, want original", sub.ProxyCount, sub.LastFetch)
	}
}

func TestRefreshSubscriptionRejectsDirectConflictBeforeRuntimeChange(t *testing.T) {
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
		"  - name: incoming-tunnel",
		"    type: trojan",
		"    server: new.example",
		"    port: 443",
		"    password: tunnel-secret",
	}, "\n"))
	subID, err := store.AddSubscription("duplicate address", "", file, "clash", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	oldNode := tunnelNode("old", "old.example.com", "password")
	shard := newSpyShard()
	if err := shard.Reload([]ParsedNode{oldNode}); err != nil {
		t.Fatalf("seed Reload() error = %v", err)
	}
	beforeCalls := shard.calls()
	m := &Manager{
		storage:   store,
		validator: validator.New(1, 1, "http://127.0.0.1/validate"),
		singbox:   shard,
	}

	err = m.RefreshSubscription(subID)
	if err == nil || !strings.Contains(err.Error(), "address") {
		t.Fatalf("RefreshSubscription() error = %v, want address conflict", err)
	}
	if shard.calls() != beforeCalls {
		t.Fatalf("Reload calls after preflight conflict = %d, want unchanged %d", shard.calls(), beforeCalls)
	}
	if got := shard.GetNodes(); len(got) != 1 || got[0].NodeKey() != oldNode.NodeKey() {
		t.Fatalf("runtime nodes after preflight conflict = %+v, want original node", got)
	}
	var count int
	if err := store.GetDB().QueryRow(
		"SELECT COUNT(*) FROM proxies WHERE source = ? AND subscription_id = ?",
		storage.SourceSubscription, subID,
	).Scan(&count); err != nil {
		t.Fatalf("count subscription rows after conflict: %v", err)
	}
	if count != 0 {
		t.Fatalf("subscription rows after preflight conflict = %d, want 0", count)
	}
}
