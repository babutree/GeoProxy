package selector_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/affinity"
	"github.com/babutree/GeoProxy/auth"
	"github.com/babutree/GeoProxy/selector"
	"github.com/babutree/GeoProxy/storage"
)

func TestSessionBindingRestoresProxyIdentityWithSharedAddress(t *testing.T) {
	store, err := storage.New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatalf("storage.New() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	subID, err := store.AddSubscription("sub", "https://example.test/selector.yaml", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if err := store.AddManualProxy("shared:8080", "http", "us", "manual"); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	if err := store.AddProxyWithSource("shared:8080", "http", storage.SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}
	manual, err := store.GetProxyByIdentity("shared:8080", storage.SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(manual) error = %v", err)
	}
	sub, err := store.GetProxyByIdentity("shared:8080", storage.SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(subscription) error = %v", err)
	}
	if err := store.UpdateSubscriptionProxyExitInfo(sub.Address, sub.SubscriptionID, "203.0.113.10", "US Ashburn", 25, -1, "", -1, ""); err != nil {
		t.Fatalf("UpdateSubscriptionProxyExitInfo() error = %v", err)
	}
	sub, err = store.GetProxyByIdentity("shared:8080", storage.SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(subscription after region) error = %v", err)
	}

	sessions := affinity.NewWithClock(10*time.Minute, time.Now)
	picked, err := selector.Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "stick"}, []int64{manual.ID})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if picked.ID != sub.ID {
		t.Fatalf("picked id=%d, want subscription id=%d", picked.ID, sub.ID)
	}
	binding, ok := sessions.Get("stick")
	if !ok || binding.ProxyID != sub.ID || binding.NodeAddress != sub.Address {
		t.Fatalf("binding = %#v ok=%v, want subscription identity", binding, ok)
	}
	resolved, err := selector.Resolve(store, sessions, auth.ParsedUsername{Region: "us", Session: "stick"}, nil)
	if err != nil {
		t.Fatalf("Resolve(bound) error = %v", err)
	}
	if resolved.ID != sub.ID {
		t.Fatalf("resolved id=%d, want subscription id=%d", resolved.ID, sub.ID)
	}
}
