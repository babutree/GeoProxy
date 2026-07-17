package selector

import (
	"errors"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/affinity"
	"github.com/babutree/GeoProxy/auth"
	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/storage"
)

func setMaxSessionsPerProxy(t *testing.T, n int) {
	t.Helper()
	t.Setenv("DATA_DIR", t.TempDir())
	// Clear env that could override; Load fills defaults then we Save override.
	cfg := config.DefaultConfig()
	cfg.MaxSessionsPerProxy = n
	// Bootstrap hashes so Save works without first-boot side effects in Get path.
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
	if got := config.Get().MaxSessionsPerProxy; got != n {
		t.Fatalf("MaxSessionsPerProxy = %d, want %d", got, n)
	}
}

func TestResolveRespectsMaxSessionsPerProxy(t *testing.T) {
	setMaxSessionsPerProxy(t, 1)
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-a:8080", Region: "us", Latency: 10, Status: "active"},
		{ID: 2, Address: "us-b:8080", Region: "us", Latency: 20, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, time.Now)

	a, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "sess-a"}, nil)
	if err != nil {
		t.Fatalf("Resolve(a) error = %v", err)
	}
	b, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "sess-b"}, nil)
	if err != nil {
		t.Fatalf("Resolve(b) error = %v", err)
	}
	if a.ID == b.ID {
		t.Fatalf("both sessions bound to proxy %d; want different nodes under max=1", a.ID)
	}
}

func TestResolveMaxSessionsFullReturnsErrNoNode(t *testing.T) {
	setMaxSessionsPerProxy(t, 1)
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-only:8080", Region: "us", Latency: 10, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, time.Now)

	if _, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "sess-a"}, nil); err != nil {
		t.Fatalf("Resolve(a) error = %v", err)
	}
	_, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "sess-b"}, nil)
	if !errors.Is(err, ErrNoNode) {
		t.Fatalf("Resolve(b) err = %v, want ErrNoNode", err)
	}
}

func TestResolveMaxSessionsTwoAllowsSameProxy(t *testing.T) {
	setMaxSessionsPerProxy(t, 2)
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-only:8080", Region: "us", Latency: 10, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, time.Now)

	if _, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "sess-a"}, nil); err != nil {
		t.Fatalf("Resolve(a) error = %v", err)
	}
	if _, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "sess-b"}, nil); err != nil {
		t.Fatalf("Resolve(b) error = %v", err)
	}
	if got := sessions.CountByProxy(1); got != 2 {
		t.Fatalf("CountByProxy(1) = %d, want 2", got)
	}
}

func TestResolveStickyIgnoresOccupancyCap(t *testing.T) {
	setMaxSessionsPerProxy(t, 1)
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-a:8080", Region: "us", Latency: 10, Status: "active"},
		{ID: 2, Address: "us-b:8080", Region: "us", Latency: 20, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, time.Now)
	sessions.SetProxy("sess-a", 1, "us-a:8080", "us")

	// Fill remaining capacity on proxy 1 is already 1; sticky re-entry must still succeed.
	proxy, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "sess-a"}, nil)
	if err != nil {
		t.Fatalf("sticky Resolve error = %v", err)
	}
	if proxy.ID != 1 {
		t.Fatalf("sticky Resolve ID = %d, want 1", proxy.ID)
	}
}

func TestResolveZeroMaxDisablesLimit(t *testing.T) {
	setMaxSessionsPerProxy(t, 0)
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-only:8080", Region: "us", Latency: 10, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, time.Now)
	for _, sess := range []string{"s1", "s2", "s3"} {
		if _, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: sess}, nil); err != nil {
			t.Fatalf("Resolve(%s) error = %v under max=0", sess, err)
		}
	}
	if got := sessions.CountByProxy(1); got != 3 {
		t.Fatalf("CountByProxy(1) = %d, want 3", got)
	}
}

func TestPickIgnoresMaxSessions(t *testing.T) {
	setMaxSessionsPerProxy(t, 1)
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-only:8080", Region: "us", Latency: 10, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, time.Now)
	sessions.SetProxy("sess-a", 1, "us-only:8080", "us")

	proxy, err := Pick(store, "us", nil)
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	if proxy.ID != 1 {
		t.Fatalf("Pick() ID = %d, want 1 (no occupancy filter)", proxy.ID)
	}
}

func TestResolveRebindReleasesOldOccupancy(t *testing.T) {
	setMaxSessionsPerProxy(t, 1)
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us-old:8080", Region: "us", Latency: 10, Status: "active"},
		{ID: 2, Address: "us-new:8080", Region: "us", Latency: 20, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, time.Now)
	sessions.SetProxy("abc", 1, "us-old:8080", "us")

	proxy, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "abc"}, []int64{1})
	if err != nil {
		t.Fatalf("Resolve rebind error = %v", err)
	}
	if proxy.ID != 2 {
		t.Fatalf("rebind ID = %d, want 2", proxy.ID)
	}
	if got := sessions.CountByProxy(1); got != 0 {
		t.Fatalf("old occupancy = %d, want 0", got)
	}
	if got := sessions.CountByProxy(2); got != 1 {
		t.Fatalf("new occupancy = %d, want 1", got)
	}
}

func TestResolveOccupancyIsRegionLocal(t *testing.T) {
	setMaxSessionsPerProxy(t, 1)
	store := fakeStore{proxies: []storage.Proxy{
		{ID: 1, Address: "us:8080", Region: "us", Latency: 10, Status: "active"},
		{ID: 2, Address: "jp:8080", Region: "jp", Latency: 10, Status: "active"},
	}}
	sessions := affinity.NewWithClock(10*time.Minute, time.Now)
	if _, err := Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "us-sess"}, nil); err != nil {
		t.Fatalf("Resolve us error = %v", err)
	}
	proxy, err := Resolve(store, sessions, auth.ParsedUsername{Region: "jp", Session: "jp-sess"}, nil)
	if err != nil {
		t.Fatalf("Resolve jp error = %v", err)
	}
	if proxy.ID != 2 {
		t.Fatalf("jp session ID = %d, want 2", proxy.ID)
	}
}
