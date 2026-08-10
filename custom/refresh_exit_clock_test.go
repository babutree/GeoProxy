package custom

import (
	"testing"
	"time"

	"github.com/babutree/GeoProxy/storage"
)

func TestReplaceSubscriptionProxiesRequiresExitCheckedAtToPreserveActive(t *testing.T) {
	store := newTestStorage(t)
	subID, err := store.AddSubscription("refresh-exit-clock", "", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}
	const address = "refresh-exit-clock.example:8080"
	const nodeKey = "refresh-exit-clock-key"
	if _, err := store.GetDB().Exec(`
		INSERT INTO proxies (
			address, protocol, source, subscription_id, region_source, status,
			fail_count, last_check, exit_checked_at, exit_ip, exit_location, latency, node_key
		) VALUES (?, ?, ?, ?, ?, ?, 0, ?, NULL, ?, ?, 45, ?)
	`, address, "http", storage.SourceSubscription, subID, "auto", "active", time.Now(), testValidationExitIP, testValidationExitLocation, nodeKey); err != nil {
		t.Fatalf("seed active route with only probe time: %v", err)
	}

	proxies, err := (&Manager{storage: store}).replaceSubscriptionProxies(subID, []subscriptionProxyEntry{{
		addr: address, proto: "http", nodeKey: nodeKey,
	}})
	if err != nil {
		t.Fatalf("replaceSubscriptionProxies: %v", err)
	}
	if len(proxies) != 1 || proxies[0].Status != "disabled" {
		t.Fatalf("replacement = %+v, want disabled until exit metadata has its own timestamp", proxies)
	}
}
