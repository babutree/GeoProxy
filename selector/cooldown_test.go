package selector

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/affinity"
	"github.com/babutree/GeoProxy/auth"
	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/storage"
)

func setProxyCooldownMinutes(t *testing.T, minutes int) {
	t.Helper()
	t.Setenv("DATA_DIR", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.ProxyCooldownMinutes = minutes
	// Keep occupancy unlimited so cooldown is the only filter under test.
	cfg.MaxSessionsPerProxy = 0
	if cfg.WebUIPasswordHash == "" {
		cfg.WebUIPasswordHash = "deadbeef"
	}
	if cfg.ProxyAuthPasswordHash == "" {
		cfg.ProxyAuthPasswordHash = "deadbeef"
	}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	_ = config.Load()
	if got := config.Get().ProxyCooldownMinutes; got != minutes {
		t.Fatalf("ProxyCooldownMinutes = %d, want %d", got, minutes)
	}
}

func TestResolveCooldownBlocksOtherSession(t *testing.T) {
	setProxyCooldownMinutes(t, 5)
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-a:8080", Region: "us", Latency: 10, Status: "active"},
		{ID: 2, Address: "us-b:8080", Region: "us", Latency: 20, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, func() time.Time { return now })

	a, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "sess-a"}, nil)
	if err != nil {
		t.Fatalf("Resolve(a) error = %v", err)
	}
	b, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "sess-b"}, nil)
	if err != nil {
		t.Fatalf("Resolve(b) error = %v", err)
	}
	if a.ID == b.ID {
		t.Fatalf("sess-b got proxy %d same as sess-a during cooldown; want alternate node", a.ID)
	}
	if sessions.InCooldown(a.ID) != true {
		t.Fatalf("bound proxy %d should be in cooldown after first bind", a.ID)
	}
}

func TestResolveCooldownExpiryAllowsReuse(t *testing.T) {
	setProxyCooldownMinutes(t, 5)
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-only:8080", Region: "us", Latency: 10, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, func() time.Time { return now })

	if _, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "sess-a"}, nil); err != nil {
		t.Fatalf("Resolve(a) error = %v", err)
	}
	_, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "sess-b"}, nil)
	if !errors.Is(err, ErrNoNode) {
		t.Fatalf("Resolve(b) during cooldown err = %v, want ErrNoNode", err)
	}

	now = now.Add(6 * time.Minute)
	b, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "sess-b"}, nil)
	if err != nil {
		t.Fatalf("Resolve(b) after cooldown error = %v", err)
	}
	if b.ID != 1 {
		t.Fatalf("after cooldown ID = %d, want 1", b.ID)
	}
}

func TestResolveStickyIgnoresCooldown(t *testing.T) {
	setProxyCooldownMinutes(t, 5)
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-a:8080", Region: "us", Latency: 10, Status: "active"},
		{ID: 2, Address: "us-b:8080", Region: "us", Latency: 20, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, func() time.Time { return now })
	sessions.SetProxy("sess-a", 1, "us-a:8080", "us")
	sessions.SetCooldown(1, now.Add(5*time.Minute))

	proxy, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "sess-a"}, nil)
	if err != nil {
		t.Fatalf("sticky Resolve error = %v", err)
	}
	if proxy.ID != 1 {
		t.Fatalf("sticky Resolve ID = %d, want 1 despite cooldown", proxy.ID)
	}
}

func TestResolveAllCooldownReturnsErrNoNode(t *testing.T) {
	setProxyCooldownMinutes(t, 5)
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-a:8080", Region: "us", Latency: 10, Status: "active"},
		{ID: 2, Address: "us-b:8080", Region: "us", Latency: 20, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, func() time.Time { return now })
	sessions.SetCooldown(1, now.Add(5*time.Minute))
	sessions.SetCooldown(2, now.Add(5*time.Minute))

	_, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "new-sess"}, nil)
	if !errors.Is(err, ErrNoNode) {
		t.Fatalf("err = %v, want ErrNoNode", err)
	}
	if !strings.Contains(err.Error(), "cooldown") {
		t.Fatalf("err message = %q, want cooldown hint", err.Error())
	}
}

func TestResolveCooldownDisabledDoesNotWrite(t *testing.T) {
	setProxyCooldownMinutes(t, 0)
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-only:8080", Region: "us", Latency: 10, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, func() time.Time { return now })

	if _, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "sess-a"}, nil); err != nil {
		t.Fatalf("Resolve(a) error = %v", err)
	}
	if sessions.InCooldown(1) {
		t.Fatal("CD=0 should not write cooldown")
	}
	if _, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "sess-b"}, nil); err != nil {
		t.Fatalf("Resolve(b) error = %v under CD=0", err)
	}
}

func TestPickIgnoresCooldown(t *testing.T) {
	setProxyCooldownMinutes(t, 5)
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-only:8080", Region: "us", Latency: 10, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, func() time.Time { return now })
	sessions.SetCooldown(1, now.Add(5*time.Minute))

	proxy, err := Pick(store, "us", nil)
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	if proxy.ID != 1 {
		t.Fatalf("Pick() ID = %d, want 1 (no cooldown filter)", proxy.ID)
	}
}

func TestResolveConfigCooldownZeroIgnoresExistingCooldown(t *testing.T) {
	// Hot-update path: config CD=0 means read-side ignores cooldown table.
	setProxyCooldownMinutes(t, 0)
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-only:8080", Region: "us", Latency: 10, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, func() time.Time { return now })
	sessions.SetCooldown(1, now.Add(30*time.Minute))

	if _, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "sess-b"}, nil); err != nil {
		t.Fatalf("Resolve with CD=0 should ignore existing cooldown: %v", err)
	}
}
