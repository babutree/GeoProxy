package selector

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/affinity"
	"github.com/babutree/GeoProxy/auth"
	"github.com/babutree/GeoProxy/storage"
)

func TestPinnedSessionHonorsCapacity(t *testing.T) {
	setMaxSessionsAndCooldown(t, 1, 0)
	store := fakeStore{proxies: []storage.Proxy{{ID: 1, Address: "node.example:443", Region: "gb", Status: "active"}}}
	sessions := affinity.New(time.Hour)
	sessions.SetProxy("existing", 1, "node.example:443", "gb")

	_, err := Resolve(store, sessions, auth.ParsedUsername{Node: "node.example:443", Session: "new"}, nil)
	if !errors.Is(err, ErrNoNode) || !strings.Contains(err.Error(), "capacity") {
		t.Fatalf("Resolve() error = %v, want capacity ErrNoNode", err)
	}
	if _, ok := sessions.Get("new"); ok {
		t.Fatal("capacity-rejected pinned session created a binding")
	}
}

func TestPinnedSessionHonorsCooldownButKeepsStickyBinding(t *testing.T) {
	setMaxSessionsAndCooldown(t, 0, 5)
	store := fakeStore{proxies: []storage.Proxy{{ID: 1, Address: "node.example:443", Region: "gb", Status: "active"}}}
	now := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	sessions := affinity.NewWithClock(time.Hour, func() time.Time { return now })
	sessions.SetCooldown(1, now.Add(time.Minute))

	_, err := Resolve(store, sessions, auth.ParsedUsername{Node: "node.example:443", Session: "new"}, nil)
	if !errors.Is(err, ErrNoNode) || !strings.Contains(err.Error(), "cooldown") {
		t.Fatalf("Resolve(new pinned session) error = %v, want cooldown ErrNoNode", err)
	}

	sessions.SetProxy("sticky", 1, "node.example:443", "gb")
	proxy, err := Resolve(store, sessions, auth.ParsedUsername{Node: "node.example:443", Session: "sticky"}, nil)
	if err != nil || proxy.ID != 1 {
		t.Fatalf("Resolve(sticky pinned session) = proxy %#v error %v, want existing binding", proxy, err)
	}
}
