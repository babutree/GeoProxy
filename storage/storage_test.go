package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestSchemaMigrationPreservesRowsAndAddsGeoFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "proxy.db")
	seedLegacyDB(t, dbPath)

	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer store.Close()

	proxy, err := store.GetProxyByAddress("1.1.1.1:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if proxy.Source != "manual" {
		t.Fatalf("Source = %q, want manual", proxy.Source)
	}
	if proxy.Region != "" || proxy.RegionSource != "" || proxy.Note != "" {
		t.Fatalf("unexpected geo defaults: %#v", proxy)
	}
	subscriptionProxy, err := store.GetProxyByAddress("2.2.2.2:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress(subscription legacy) error = %v", err)
	}
	if subscriptionProxy.Source != SourceSubscription {
		t.Fatalf("legacy custom source = %q, want %s", subscriptionProxy.Source, SourceSubscription)
	}
	assertColumnExists(t, store, "region")
	assertColumnExists(t, store, "region_source")
	assertColumnExists(t, store, "note")
	assertSourceStatusDropped(t, store)
}

func TestSchemaMigrationReturnsLocationCopyError(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "proxy.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	_, err = db.Exec(`CREATE TABLE proxies (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		address TEXT NOT NULL UNIQUE,
		protocol TEXT NOT NULL,
		location TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT 'manual'
	)`)
	if err != nil {
		t.Fatalf("create legacy proxies: %v", err)
	}
	_, err = db.Exec(`INSERT INTO proxies (address, protocol, location) VALUES ('legacy-location:8080', 'http', 'Legacy City')`)
	if err != nil {
		t.Fatalf("insert legacy proxy: %v", err)
	}
	_, err = db.Exec(`CREATE TRIGGER fail_location_copy
		BEFORE UPDATE OF exit_location ON proxies
		WHEN NEW.exit_location != OLD.exit_location
		BEGIN
			SELECT RAISE(ABORT, 'forced location copy failure');
		END`)
	if err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	store, err := New(dbPath)
	if err == nil {
		store.Close()
		t.Fatal("New() expected location migration error, got nil")
	}
	if !strings.Contains(err.Error(), "migrate location values") || !strings.Contains(err.Error(), "forced location copy failure") {
		t.Fatalf("New() error = %v, want location copy failure", err)
	}

	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer db.Close()
	var exitLocationColumns int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name = 'exit_location'`).Scan(&exitLocationColumns); err != nil {
		t.Fatalf("check exit_location column: %v", err)
	}
	if exitLocationColumns != 0 {
		t.Fatal("location migration left exit_location column after copy failure")
	}
}

func TestRegionQueriesFilterCountAndExclude(t *testing.T) {
	store := newTestStorage(t)
	insertTestSubscription(t, store, 1, "active")
	insertProxy(t, store, "us-fast:8080", "http", "us", "manual", 20, "active", 0)
	insertProxy(t, store, "us-slow:8080", "http", "us", SourceSubscription, 80, "active", 0)
	insertProxy(t, store, "jp:8080", "socks5", "jp", "manual", 10, "active", 0)
	insertProxy(t, store, "us-disabled:8080", "http", "us", "manual", 1, "disabled", 0)
	insertProxy(t, store, "us-failing:8080", "http", "us", "manual", 1, "active", 3)
	fastProxy, err := store.GetProxyByIdentity("us-fast:8080", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(us-fast) error = %v", err)
	}

	proxies, err := store.GetByRegion("US", []int64{fastProxy.ID})
	if err != nil {
		t.Fatalf("GetByRegion() error = %v", err)
	}
	if len(proxies) != 1 || proxies[0].Address != "us-slow:8080" {
		t.Fatalf("GetByRegion() = %#v, want only us-slow", proxies)
	}

	counts, err := store.CountByRegion()
	if err != nil {
		t.Fatalf("CountByRegion() error = %v", err)
	}
	if counts["us"] != 2 || counts["jp"] != 1 {
		t.Fatalf("CountByRegion() = %#v, want us=2 jp=1", counts)
	}
}

func TestManualProxyAPIs(t *testing.T) {
	store := newTestStorage(t)

	if err := store.AddManualProxy("2.2.2.2:1080", "SOCKS5", "HK", "primary"); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	proxy, err := store.GetProxyByAddress("2.2.2.2:1080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if proxy.Protocol != "socks5" || proxy.Source != "manual" || proxy.Region != "hk" || proxy.RegionSource != "manual" || proxy.Note != "primary" {
		t.Fatalf("manual proxy = %#v", proxy)
	}

	if err := store.UpdateProxyRegion("2.2.2.2:1080", "JP", false); err != nil {
		t.Fatalf("UpdateProxyRegion() error = %v", err)
	}
	if err := store.UpdateProxyNote("2.2.2.2:1080", "backup"); err != nil {
		t.Fatalf("UpdateProxyNote() error = %v", err)
	}
	proxy, err = store.GetProxyByAddress("2.2.2.2:1080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if proxy.Region != "jp" || proxy.RegionSource != "auto" || proxy.Note != "backup" {
		t.Fatalf("updated proxy = %#v", proxy)
	}

	if err := store.DeleteManualProxy("2.2.2.2:1080"); err != nil {
		t.Fatalf("DeleteManualProxy() error = %v", err)
	}
}

func TestUpdateExitInfoWritesAutoRegionAndPreservesManualRegion(t *testing.T) {
	store := newTestStorage(t)
	insertTestSubscription(t, store, 1, "active")
	insertProxyWithRegionSource(t, store, "auto:8080", "", "auto")
	insertProxyWithRegionSource(t, store, "manual:8080", "jp", "manual")

	if err := store.UpdateExitInfo("auto:8080", "8.8.8.8", "US Mountain View", 120, -1, "", -1, ""); err != nil {
		t.Fatalf("UpdateExitInfo(auto) error = %v", err)
	}
	if err := store.UpdateExitInfo("manual:8080", "1.1.1.1", "US Los Angeles", 80, -1, "", -1, ""); err != nil {
		t.Fatalf("UpdateExitInfo(manual) error = %v", err)
	}

	autoProxy, err := store.GetProxyByAddress("auto:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress(auto) error = %v", err)
	}
	if autoProxy.Region != "us" || autoProxy.ExitLocation != "US Mountain View" {
		t.Fatalf("auto proxy region/writeback = %#v, want region us and exit location preserved", autoProxy)
	}

	manualProxy, err := store.GetProxyByAddress("manual:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress(manual) error = %v", err)
	}
	if manualProxy.Region != "jp" || manualProxy.ExitLocation != "US Los Angeles" {
		t.Fatalf("manual proxy region/writeback = %#v, want region jp and exit location preserved", manualProxy)
	}
}

func TestUpdateExitInfoIgnoresInvalidRegionCode(t *testing.T) {
	store := newTestStorage(t)
	insertTestSubscription(t, store, 1, "active")
	insertProxyWithRegionSource(t, store, "unknown:8080", "hk", "auto")

	if err := store.UpdateExitInfo("unknown:8080", "9.9.9.9", "USA Miami", 150, -1, "", -1, ""); err != nil {
		t.Fatalf("UpdateExitInfo() error = %v", err)
	}

	proxy, err := store.GetProxyByAddress("unknown:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if proxy.Region != "hk" || proxy.ExitLocation != "USA Miami" {
		t.Fatalf("proxy after invalid region writeback = %#v, want region hk and exit location preserved", proxy)
	}
}

func TestManualAndSubscriptionSameAddressDoNotOverwriteIdentity(t *testing.T) {
	store := newTestStorage(t)
	subID, err := store.AddSubscription("sub", "https://example.test/sub.yaml", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if err := store.AddManualProxy("same:8080", "http", "jp", "manual"); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	if err := store.AddProxyWithSource("same:8080", "socks5", SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}

	manual, err := store.GetProxyByIdentity("same:8080", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(manual) error = %v", err)
	}
	sub, err := store.GetProxyByIdentity("same:8080", SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(subscription) error = %v", err)
	}
	if manual.Protocol != "http" || manual.SubscriptionID != 0 || manual.Source != SourceManual || manual.Note != "manual" {
		t.Fatalf("manual proxy = %#v", manual)
	}
	if sub.Protocol != "socks5" || sub.SubscriptionID != subID || sub.Source != SourceSubscription || sub.Note != "" {
		t.Fatalf("subscription proxy = %#v", sub)
	}
	if err := store.DeleteManualProxy("same:8080"); err != nil {
		t.Fatalf("DeleteManualProxy() error = %v", err)
	}
	if _, err := store.GetProxyByIdentity("same:8080", SourceSubscription, subID); err != nil {
		t.Fatalf("subscription proxy should remain after manual delete: %v", err)
	}
}

func TestAddressManualEditsRejectAmbiguousProxyIdentity(t *testing.T) {
	store := newTestStorage(t)
	subID, err := store.AddSubscription("sub", "https://example.test/manual-edit.yaml", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if err := store.AddManualProxy("ambiguous-edit:8080", "http", "jp", "manual-note"); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	if err := store.AddProxyWithSource("ambiguous-edit:8080", "http", SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}

	if err := store.UpdateProxyRegion("ambiguous-edit:8080", "US", true); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("UpdateProxyRegion() error = %v, want ambiguous identity error", err)
	}
	if err := store.UpdateProxyNote("ambiguous-edit:8080", "leaked-note"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("UpdateProxyNote() error = %v, want ambiguous identity error", err)
	}

	manual, err := store.GetProxyByIdentity("ambiguous-edit:8080", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(manual) error = %v", err)
	}
	sub, err := store.GetProxyByIdentity("ambiguous-edit:8080", SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(subscription) error = %v", err)
	}
	if manual.Region != "jp" || manual.Note != "manual-note" {
		t.Fatalf("manual proxy was modified despite ambiguity: %#v", manual)
	}
	if sub.Region != "" || sub.Note != "" {
		t.Fatalf("subscription proxy was modified despite ambiguity: %#v", sub)
	}
}

func TestGetProxyByAddressRejectsAmbiguousProxyIdentity(t *testing.T) {
	store := newTestStorage(t)
	subID, err := store.AddSubscription("sub", "https://example.test/get-by-address.yaml", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	const addr = "ambiguous-get:8080"
	if err := store.AddManualProxy(addr, "http", "jp", "manual-note"); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	if err := store.AddProxyWithSource(addr, "socks5", SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}

	proxy, err := store.GetProxyByAddress(addr)
	if err == nil {
		t.Fatalf("GetProxyByAddress() = %#v, want ambiguous identity error", proxy)
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("GetProxyByAddress() error = %v, want ambiguous identity error", err)
	}

	// Unique address still resolves.
	if err := store.AddManualProxy("unique-get:8080", "http", "us", ""); err != nil {
		t.Fatalf("AddManualProxy(unique) error = %v", err)
	}
	unique, err := store.GetProxyByAddress("unique-get:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress(unique) error = %v", err)
	}
	if unique.Address != "unique-get:8080" || unique.Source != SourceManual {
		t.Fatalf("unique proxy = %#v", unique)
	}
}

func TestLegacyAddressUniqueMigrationAllowsManualAndSubscriptionSameAddress(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "proxy.db")
	seedLegacyDB(t, dbPath)
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer store.Close()

	subID, err := store.AddSubscription("sub", "https://example.test/sub.yaml", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if err := store.AddProxyWithSource("1.1.1.1:8080", "socks5", SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource(same address after migration) error = %v", err)
	}
	if _, err := store.GetProxyByIdentity("1.1.1.1:8080", SourceManual, 0); err != nil {
		t.Fatalf("manual legacy proxy missing: %v", err)
	}
	if _, err := store.GetProxyByIdentity("1.1.1.1:8080", SourceSubscription, subID); err != nil {
		t.Fatalf("subscription same-address proxy missing: %v", err)
	}
}

func TestSubscriptionURLAndFilePathUniqueForConcurrentAddsAndUpdates(t *testing.T) {
	store := newTestStorage(t)
	const workers = 12
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := store.AddSubscription(fmt.Sprintf("sub-%d", i), "https://example.test/dup.yaml", "", "auto", 60, "")
			errCh <- err
		}(i)
	}
	wg.Wait()
	close(errCh)

	successes := 0
	duplicateErrors := 0
	for err := range errCh {
		if err == nil {
			successes++
			continue
		}
		if strings.Contains(err.Error(), "已存在") {
			duplicateErrors++
			continue
		}
		t.Fatalf("unexpected AddSubscription error: %v", err)
	}
	if successes != 1 || duplicateErrors != workers-1 {
		t.Fatalf("concurrent add results: successes=%d duplicateErrors=%d", successes, duplicateErrors)
	}

	secondID, err := store.AddSubscription("file", "", filepath.Join(t.TempDir(), "sub.yaml"), "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription(file) error = %v", err)
	}
	if err := store.UpdateSubscription(secondID, "file", "https://example.test/dup.yaml", "", "auto", 60, ""); err == nil || !strings.Contains(err.Error(), "已存在") {
		t.Fatalf("UpdateSubscription duplicate URL error = %v, want duplicate error", err)
	}
}

func TestDeleteSubscriptionDeletesProxiesAtomically(t *testing.T) {
	store := newTestStorage(t)
	subID, err := store.AddSubscription("sub", "https://example.test/delete.yaml", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if err := store.AddProxyWithSource("delete-me:8080", "http", SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}
	if err := store.DeleteSubscription(subID); err != nil {
		t.Fatalf("DeleteSubscription() error = %v", err)
	}
	if _, err := store.GetSubscription(subID); err == nil {
		t.Fatal("GetSubscription() expected error after delete, got nil")
	}
	if _, err := store.GetProxyByIdentity("delete-me:8080", SourceSubscription, subID); err == nil {
		t.Fatal("subscription proxy should be deleted with subscription")
	}
}

func TestDeleteSubscriptionRollsBackProxyDeleteWhenSubscriptionDeleteFails(t *testing.T) {
	store := newTestStorage(t)
	subID, err := store.AddSubscription("sub", "https://example.test/delete-failure.yaml", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if err := store.AddProxyWithSource("keep-on-delete-failure:8080", "http", SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}
	if _, err := store.db.Exec(`
		CREATE TRIGGER fail_subscription_delete
		BEFORE DELETE ON subscriptions
		WHEN OLD.id = ` + fmt.Sprint(subID) + `
		BEGIN
			SELECT RAISE(ABORT, 'forced subscription delete failure');
		END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	if err := store.DeleteSubscription(subID); err == nil {
		t.Fatal("DeleteSubscription() expected error, got nil")
	}
	if _, err := store.GetSubscription(subID); err != nil {
		t.Fatalf("subscription removed after failed delete: %v", err)
	}
	if _, err := store.GetProxyByIdentity("keep-on-delete-failure:8080", SourceSubscription, subID); err != nil {
		t.Fatalf("proxy removed after failed subscription delete: %v", err)
	}
}

func TestSubscriptionMetadataUpdatesRejectMissingID(t *testing.T) {
	store := newTestStorage(t)
	const missingID = int64(999999)
	t.Run("fetch", func(t *testing.T) {
		if err := store.UpdateSubscriptionFetch(missingID, 12); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("UpdateSubscriptionFetch() error = %v, want sql.ErrNoRows", err)
		}
	})
	t.Run("success", func(t *testing.T) {
		if err := store.UpdateSubscriptionSuccess(missingID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("UpdateSubscriptionSuccess() error = %v, want sql.ErrNoRows", err)
		}
	})
}

func TestDisableBlockedCountriesRollsBackOnPartialFailure(t *testing.T) {
	store := newTestStorage(t)
	insertProxy(t, store, "us-block:8080", "http", "us", SourceManual, 100, "active", 0)
	insertProxy(t, store, "cn-block:8080", "http", "cn", SourceManual, 100, "active", 0)
	if _, err := store.db.Exec(`
		CREATE TRIGGER fail_cn_disable
		BEFORE UPDATE OF status ON proxies
		WHEN OLD.address = 'cn-block:8080' AND NEW.status = 'disabled'
		BEGIN
			SELECT RAISE(ABORT, 'forced country disable failure');
		END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	if _, err := store.DisableBlockedCountries([]string{"US", "CN"}); err == nil {
		t.Fatal("DisableBlockedCountries() expected error, got nil")
	}
	for _, address := range []string{"us-block:8080", "cn-block:8080"} {
		proxy, err := store.GetProxyByAddress(address)
		if err != nil {
			t.Fatalf("GetProxyByAddress(%q) error = %v", address, err)
		}
		if proxy.Status != "active" {
			t.Fatalf("proxy %q status = %q after rollback, want active", address, proxy.Status)
		}
	}
}

func TestUserPausedAndSubscriptionPausedStateMachine(t *testing.T) {
	store := newTestStorage(t)
	subID, err := store.AddSubscription("sub", "https://example.test/state.yaml", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if err := store.AddProxyWithSource("state:8080", "http", SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}
	if err := store.PauseProxy("state:8080"); err != nil {
		t.Fatalf("PauseProxy() error = %v", err)
	}
	if count, err := store.CountAll(); err != nil || count != 0 {
		t.Fatalf("CountAll after user pause = %d, err=%v; want 0 nil", count, err)
	}
	if _, err := store.ToggleSubscription(subID); err != nil {
		t.Fatalf("ToggleSubscription(paused) error = %v", err)
	}
	proxy, err := store.GetProxyByIdentity("state:8080", SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() error = %v", err)
	}
	if proxy.Status != "active" || !proxy.UserPaused {
		t.Fatalf("proxy after subscription pause = %#v, want active plus user_paused", proxy)
	}
	if err := store.UnpauseProxy("state:8080"); err != nil {
		t.Fatalf("UnpauseProxy() error = %v", err)
	}
	if count, err := store.CountAll(); err != nil || count != 0 {
		t.Fatalf("CountAll while subscription paused = %d, err=%v; want 0 nil", count, err)
	}
	if _, err := store.ToggleSubscription(subID); err != nil {
		t.Fatalf("ToggleSubscription(active) error = %v", err)
	}
	if count, err := store.CountAll(); err != nil || count != 1 {
		t.Fatalf("CountAll after subscription resume = %d, err=%v; want 1 nil", count, err)
	}
}

func TestParentSubscriptionPausedNotBypassedByEnableProxy(t *testing.T) {
	store := newTestStorage(t)
	subID, err := store.AddSubscription("sub", "https://example.test/enable.yaml", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if err := store.AddProxyWithSource("enable:8080", "http", SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}
	if err := store.DisableProxy("enable:8080"); err != nil {
		t.Fatalf("DisableProxy() error = %v", err)
	}
	if _, err := store.ToggleSubscription(subID); err != nil {
		t.Fatalf("ToggleSubscription(paused) error = %v", err)
	}
	if err := store.EnableProxy("enable:8080"); err == nil {
		t.Fatal("EnableProxy() expected error while parent subscription paused, got nil")
	}
	proxy, err := store.GetProxyByIdentity("enable:8080", SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() error = %v", err)
	}
	if proxy.Status != "disabled" {
		t.Fatalf("proxy status = %q, want disabled", proxy.Status)
	}
	if count, err := store.CountAll(); err != nil || count != 0 {
		t.Fatalf("CountAll = %d, err=%v; want 0 nil", count, err)
	}
}

func TestCountBySubscriptionIDReturnsScanError(t *testing.T) {
	store := newTestStorage(t)
	if _, err := store.db.Exec(`DROP TABLE proxies`); err != nil {
		t.Fatalf("drop proxies: %v", err)
	}
	if _, _, err := store.CountBySubscriptionID(1); err == nil {
		t.Fatal("CountBySubscriptionID() expected error after broken schema, got nil")
	}
}

func TestProxyIdentityConsistencyForUsagePauseAndDelete(t *testing.T) {
	store := newTestStorage(t)
	subID, err := store.AddSubscription("sub", "https://example.test/identity.yaml", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if err := store.AddManualProxy("shared:8080", "http", "us", "manual"); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	if err := store.AddProxyWithSource("shared:8080", "http", SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}
	manual, err := store.GetProxyByIdentity("shared:8080", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(manual) error = %v", err)
	}
	sub, err := store.GetProxyByIdentity("shared:8080", SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(subscription) error = %v", err)
	}
	if err := store.DisableSubscriptionProxy(sub.Address, sub.SubscriptionID); err != nil {
		t.Fatalf("DisableSubscriptionProxy() error = %v", err)
	}
	if err := store.UpdateSubscriptionProxyExitInfo(sub.Address, sub.SubscriptionID, "203.0.113.9", "US Ashburn", 42, -1, "", -1, ""); err != nil {
		t.Fatalf("UpdateSubscriptionProxyExitInfo() error = %v", err)
	}
	if err := store.EnableSubscriptionProxy(sub.Address, sub.SubscriptionID); err != nil {
		t.Fatalf("EnableSubscriptionProxy() error = %v", err)
	}
	sub, err = store.GetProxyByIdentity("shared:8080", SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(subscription after validation) error = %v", err)
	}
	if sub.Status != "active" || sub.ExitIP != "203.0.113.9" || sub.Latency != 42 {
		t.Fatalf("subscription proxy after validation = %#v", sub)
	}
	manual, err = store.GetProxyByIdentity("shared:8080", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(manual after validation) error = %v", err)
	}
	if manual.ExitIP != "" || manual.Latency != 0 {
		t.Fatalf("manual proxy should not receive subscription validation writes: %#v", manual)
	}

	if err := store.RecordProxyUseByID(sub.ID, true); err != nil {
		t.Fatalf("RecordProxyUseByID(subscription success) error = %v", err)
	}
	if err := store.RecordProxyUseByID(manual.ID, false); err != nil {
		t.Fatalf("RecordProxyUseByID(manual fail) error = %v", err)
	}
	sub, _ = store.GetProxyByIdentity("shared:8080", SourceSubscription, subID)
	manual, _ = store.GetProxyByIdentity("shared:8080", SourceManual, 0)
	if sub.UseCount != 1 || sub.SuccessCount != 1 || sub.FailCount != 0 {
		t.Fatalf("subscription counts = %#v, want one success only", sub)
	}
	if manual.UseCount != 1 || manual.SuccessCount != 0 || manual.FailCount != 1 {
		t.Fatalf("manual counts = %#v, want one failure only", manual)
	}

	if err := store.PauseProxyByID(manual.ID); err != nil {
		t.Fatalf("PauseProxyByID(manual) error = %v", err)
	}
	sub, _ = store.GetProxyByIdentity("shared:8080", SourceSubscription, subID)
	manual, _ = store.GetProxyByIdentity("shared:8080", SourceManual, 0)
	if manual.UserPaused != true || sub.UserPaused != false {
		t.Fatalf("pause leaked across identities: manual=%#v sub=%#v", manual, sub)
	}
	available, err := store.GetByRegion("us", nil)
	if err != nil {
		t.Fatalf("GetByRegion() error = %v", err)
	}
	if len(available) != 1 || available[0].ID != sub.ID {
		t.Fatalf("available after manual pause = %#v, want only subscription", available)
	}

	if err := store.DeleteSubscription(subID); err != nil {
		t.Fatalf("DeleteSubscription() error = %v", err)
	}
	if _, err := store.GetProxyByIdentity("shared:8080", SourceSubscription, subID); err == nil {
		t.Fatal("subscription proxy should be removed with subscription")
	}
	if _, err := store.GetProxyByIdentity("shared:8080", SourceManual, 0); err != nil {
		t.Fatalf("manual proxy should remain after subscription delete: %v", err)
	}
}

func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func seedLegacyDB(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE proxies (id INTEGER PRIMARY KEY AUTOINCREMENT, address TEXT NOT NULL UNIQUE, protocol TEXT NOT NULL, source TEXT NOT NULL DEFAULT 'free')`)
	if err != nil {
		t.Fatalf("create legacy proxies: %v", err)
	}
	_, err = db.Exec(`INSERT INTO proxies (address, protocol, source) VALUES ('1.1.1.1:8080', 'http', 'free')`)
	if err != nil {
		t.Fatalf("insert legacy proxy: %v", err)
	}
	_, err = db.Exec(`INSERT INTO proxies (address, protocol, source) VALUES ('2.2.2.2:8080', 'http', ?)`, legacySourceCustom)
	if err != nil {
		t.Fatalf("insert legacy subscription proxy: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE source_status (id INTEGER PRIMARY KEY AUTOINCREMENT, url TEXT NOT NULL UNIQUE)`)
	if err != nil {
		t.Fatalf("create source_status: %v", err)
	}
}

func insertProxy(t *testing.T, store *Storage, address, protocol, region, source string, latency int, status string, failCount int) {
	t.Helper()
	_, err := store.db.Exec(
		`INSERT INTO proxies (address, protocol, region, source, subscription_id, latency, status, fail_count)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		address, protocol, region, source, testSubscriptionIDForSource(source), latency, status, failCount,
	)
	if err != nil {
		t.Fatalf("insert proxy %s: %v", address, err)
	}
}

func insertProxyWithRegionSource(t *testing.T, store *Storage, address, region, regionSource string) {
	t.Helper()
	_, err := store.db.Exec(
		`INSERT INTO proxies (address, protocol, region, region_source, source, subscription_id, status)
		 VALUES (?, 'http', ?, ?, ?, 1, 'active')`,
		address, region, regionSource, SourceSubscription,
	)
	if err != nil {
		t.Fatalf("insert proxy %s: %v", address, err)
	}
}

// TestSuccessResetsFailCountEnablesSelfHeal 覆盖 BUG-53：失败累加到阈值后节点被
// 选路和健康检查（fail_count < 3 过滤）排除；一次成功使用应将 fail_count 归零，
// 节点重新可选、可被健康检查纳入。
func TestSuccessResetsFailCountEnablesSelfHeal(t *testing.T) {
	store := newTestStorage(t)
	insertProxy(t, store, "10.0.0.1:8080", "http", "us", SourceManual, 100, "active", 0)
	p, err := store.GetProxyByAddress("10.0.0.1:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}

	// 累加失败到阈值（3）。
	for i := 0; i < 3; i++ {
		if err := store.RecordProxyUseByID(p.ID, false); err != nil {
			t.Fatalf("RecordProxyUseByID(fail) error = %v", err)
		}
	}
	p, _ = store.GetProxyByAddress("10.0.0.1:8080")
	if p.FailCount != 3 || p.Status != "active" {
		t.Fatalf("after 3 failures = fail_count %d status %q, want 3/active", p.FailCount, p.Status)
	}

	// 此时被选路和健康检查排除（僵尸态）。
	if nodes, err := store.GetByRegion("us", nil); err != nil || len(nodes) != 0 {
		t.Fatalf("GetByRegion after zombie = %d nodes err=%v, want 0", len(nodes), err)
	}
	if batch, err := store.GetBatchForHealthCheck(10, false); err != nil || len(batch) != 0 {
		t.Fatalf("GetBatchForHealthCheck after zombie = %d nodes err=%v, want 0", len(batch), err)
	}

	// 一次成功应清零 fail_count。
	if err := store.RecordProxyUseByID(p.ID, true); err != nil {
		t.Fatalf("RecordProxyUseByID(success) error = %v", err)
	}
	p, _ = store.GetProxyByAddress("10.0.0.1:8080")
	if p.FailCount != 0 {
		t.Fatalf("fail_count after success = %d, want 0", p.FailCount)
	}
	if p.SuccessCount != 1 {
		t.Fatalf("success_count after success = %d, want 1", p.SuccessCount)
	}

	// 节点重新可选、可被健康检查纳入。
	if nodes, err := store.GetByRegion("us", nil); err != nil || len(nodes) != 1 {
		t.Fatalf("GetByRegion after heal = %d nodes err=%v, want 1", len(nodes), err)
	}
	if batch, err := store.GetBatchForHealthCheck(10, false); err != nil || len(batch) != 1 {
		t.Fatalf("GetBatchForHealthCheck after heal = %d nodes err=%v, want 1", len(batch), err)
	}
}

func TestRecordProxyFailureByIDDisablesAtThreshold(t *testing.T) {
	store := newTestStorage(t)
	insertProxy(t, store, "failure-threshold:8080", "http", "us", SourceManual, 100, "active", 0)
	proxy, err := store.GetProxyByAddress("failure-threshold:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := store.RecordProxyFailureByID(proxy.ID, 3); err != nil {
			t.Fatalf("RecordProxyFailureByID(%d) error = %v", i+1, err)
		}
	}
	proxy, err = store.GetProxyByID(proxy.ID)
	if err != nil {
		t.Fatalf("GetProxyByID() error = %v", err)
	}
	if proxy.FailCount != 3 || proxy.Status != "disabled" {
		t.Fatalf("proxy after failures = fail_count %d status %q, want 3/disabled", proxy.FailCount, proxy.Status)
	}
}

// TestHealthCheckSuccessResetsFailCount 覆盖 BUG-53 健康检查成功路径：
// UpdateProxyExitInfo（验证/健康检查通过时调用）应将 fail_count 归零。
func TestHealthCheckSuccessResetsFailCount(t *testing.T) {
	store := newTestStorage(t)
	insertProxy(t, store, "10.0.0.2:8080", "http", "us", SourceManual, 100, "active", 2)
	p, err := store.GetProxyByAddress("10.0.0.2:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if p.FailCount != 2 {
		t.Fatalf("seed fail_count = %d, want 2", p.FailCount)
	}

	if err := store.UpdateProxyExitInfo(p.ID, "203.0.113.10", "US Ashburn", 42, -1, "", -1, ""); err != nil {
		t.Fatalf("UpdateProxyExitInfo() error = %v", err)
	}
	p, _ = store.GetProxyByAddress("10.0.0.2:8080")
	if p.FailCount != 0 {
		t.Fatalf("fail_count after health-check success = %d, want 0", p.FailCount)
	}
	if p.ExitIP != "203.0.113.10" || p.Latency != 42 {
		t.Fatalf("exit info not written: %#v", p)
	}
}

func TestAddManualProxiesRollsBackOnPartialFailure(t *testing.T) {
	store := newTestStorage(t)
	_, err := store.db.Exec(`
		CREATE TRIGGER fail_bad_manual_proxy
		BEFORE INSERT ON proxies
		WHEN NEW.address = 'manual-bad:8080'
		BEGIN
			SELECT RAISE(ABORT, 'forced manual insert failure');
		END`)
	if err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	err = store.AddManualProxies([]Proxy{
		{Address: "manual-good:1080", Protocol: "socks5"},
		{Address: "manual-bad:8080", Protocol: "http"},
	}, "us", "atomic")
	if err == nil {
		t.Fatal("AddManualProxies() expected error, got nil")
	}
	if _, err := store.GetProxyByAddress("manual-good:1080"); err == nil {
		t.Fatal("manual-good was committed despite later insert failure")
	}
}

func TestAddProxiesRollsBackOnPartialFailure(t *testing.T) {
	store := newTestStorage(t)
	_, err := store.db.Exec(`
		CREATE TRIGGER fail_bad_proxy
		BEFORE INSERT ON proxies
		WHEN NEW.address = 'batch-bad:8080'
		BEGIN
			SELECT RAISE(ABORT, 'forced batch insert failure');
		END`)
	if err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	err = store.AddProxies([]Proxy{
		{Address: "batch-good:1080", Protocol: "socks5"},
		{Address: "batch-bad:8080", Protocol: "http"},
	})
	if err == nil {
		t.Fatal("AddProxies() expected error, got nil")
	}
	if _, err := store.GetProxyByAddress("batch-good:1080"); err == nil {
		t.Fatal("batch-good was committed despite later insert failure")
	}
}

func testSubscriptionIDForSource(source string) int64 {
	if source == SourceSubscription {
		return 1
	}
	return 0
}

func insertTestSubscription(t *testing.T, store *Storage, id int64, status string) {
	t.Helper()
	_, err := store.db.Exec(
		`INSERT INTO subscriptions (id, name, status, url, file_path, format, refresh_min) VALUES (?, ?, ?, '', '', 'auto', 60)`,
		id, fmt.Sprintf("test-sub-%d", id), status,
	)
	if err != nil {
		t.Fatalf("insert subscription %d: %v", id, err)
	}
}

func assertColumnExists(t *testing.T, store *Storage, name string) {
	t.Helper()
	var count int
	err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name = ?`, name).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("column %s count = %d, err = %v", name, count, err)
	}
}

func assertSourceStatusDropped(t *testing.T, store *Storage) {
	t.Helper()
	var tableName string
	err := store.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'source_status'`).Scan(&tableName)
	if err != sql.ErrNoRows {
		t.Fatalf("source_status table still exists or query failed: table=%q err=%v", tableName, err)
	}
}

// insertProxyFull 以显式 subscription_id / user_paused 插入节点，供
// CountPausedBySubscriptionID 用例控制订阅归属与暂停位。
func insertProxyFull(t *testing.T, store *Storage, address, source string, subID int64, status string, userPaused int) {
	t.Helper()
	_, err := store.db.Exec(
		`INSERT INTO proxies (address, protocol, region, source, subscription_id, latency, status, user_paused, fail_count)
		 VALUES (?, 'http', 'us', ?, ?, 100, ?, ?, 0)`,
		address, source, subID, status, userPaused,
	)
	if err != nil {
		t.Fatalf("insert proxy %s: %v", address, err)
	}
}

// TestCountPausedBySubscriptionID 覆盖 BUG-52：CountPausedBySubscriptionID 只统计
// 指定订阅下 user_paused=1 的节点，不计 disabled/active 的未暂停节点，不跨订阅，
// 且与订阅级 subscriptions.status='paused' 正交（订阅整体暂停不改变按节点计数）。
func TestCountPausedBySubscriptionID(t *testing.T) {
	store := newTestStorage(t)
	insertTestSubscription(t, store, 1, "active") // 订阅 A
	insertTestSubscription(t, store, 2, "active") // 订阅 B

	// 订阅 A：2 个 user_paused=1（一个 active、一个 disabled 底色），
	// 加上 active 未暂停、disabled 未暂停各一个作为干扰。
	insertProxyFull(t, store, "a-paused-active:8080", SourceSubscription, 1, "active", 1)
	insertProxyFull(t, store, "a-paused-disabled:8080", SourceSubscription, 1, "disabled", 1)
	insertProxyFull(t, store, "a-active:8080", SourceSubscription, 1, "active", 0)
	insertProxyFull(t, store, "a-disabled:8080", SourceSubscription, 1, "disabled", 0)

	// 订阅 B：1 个 user_paused=1，不应计入 A。
	insertProxyFull(t, store, "b-paused:8080", SourceSubscription, 2, "active", 1)

	count, err := store.CountPausedBySubscriptionID(1)
	if err != nil {
		t.Fatalf("CountPausedBySubscriptionID(A) error = %v", err)
	}
	if count != 2 {
		t.Fatalf("CountPausedBySubscriptionID(A) = %d, want 2 (只含 A 下 user_paused=1，不含 disabled/active 未暂停、不含 B)", count)
	}

	// 订阅级暂停：把订阅 A 整体置为 paused，节点级 user_paused 不变。
	if err := store.PauseSubscription(1); err != nil {
		t.Fatalf("PauseSubscription(A) error = %v", err)
	}
	var subStatus string
	if err := store.db.QueryRow(`SELECT status FROM subscriptions WHERE id = 1`).Scan(&subStatus); err != nil {
		t.Fatalf("read subscription A status: %v", err)
	}
	if subStatus != "paused" {
		t.Fatalf("subscription A status = %q, want paused", subStatus)
	}

	countAfter, err := store.CountPausedBySubscriptionID(1)
	if err != nil {
		t.Fatalf("CountPausedBySubscriptionID(A) after subscription pause error = %v", err)
	}
	if countAfter != 2 {
		t.Fatalf("CountPausedBySubscriptionID(A) after subscription pause = %d, want 2 (证明与订阅级状态正交)", countAfter)
	}
}

// TestMigrateProxyIdentityIsAtomicOnFailure 覆盖 BUG-56：migrateProxyIdentity 在
// 单事务内执行，中途失败整体回滚，不留半迁移态。
//
// 失败注入方式（真实、非模拟）：migrateProxyIdentity 第 1 步会把 source='custom'
// 归一化为 'subscription'（migrations.go:71），随后第 4 步 assignLegacySubscriptionID
// 需要向 subscriptions 表 INSERT（migrations.go:101）。删除 subscriptions 表后再次
// 调用迁移，则第 1 步成功执行、第 4 步 INSERT 因表不存在而失败。断言：迁移返回
// error，且已执行的第 1 步被回滚（custom 节点的 source 仍为 'custom'，未变成
// 'subscription'），证明整个迁移是原子的。
func TestMigrateProxyIdentityIsAtomicOnFailure(t *testing.T) {
	store := newTestStorage(t)

	// 直接种一个 legacy 'custom' 节点（尚未归一化），subscription_id<=0 以触发
	// assignLegacySubscriptionID 的 INSERT 路径。
	if _, err := store.db.Exec(
		`INSERT INTO proxies (address, protocol, region, source, subscription_id, status)
		 VALUES ('atomic:8080', 'http', 'us', ?, 0, 'active')`,
		legacySourceCustom,
	); err != nil {
		t.Fatalf("seed legacy custom proxy: %v", err)
	}

	// 注入失败：删掉 subscriptions 表，令第 4 步 INSERT 失败。
	if _, err := store.db.Exec(`DROP TABLE subscriptions`); err != nil {
		t.Fatalf("drop subscriptions to inject failure: %v", err)
	}

	err := store.migrateProxyIdentity()
	if err == nil {
		t.Fatal("migrateProxyIdentity() expected error after failure injection, got nil")
	}

	// 关键断言：第 1 步（custom -> subscription）必须已回滚，source 仍为 'custom'。
	var source string
	if err := store.db.QueryRow(`SELECT source FROM proxies WHERE address = 'atomic:8080'`).Scan(&source); err != nil {
		t.Fatalf("read proxy source after failed migration: %v", err)
	}
	if source != legacySourceCustom {
		t.Fatalf("proxy source after failed+rolled-back migration = %q, want %q (前置步骤未回滚，迁移非原子)", source, legacySourceCustom)
	}
}

// TestMigrateProxyIdentityIdempotent 覆盖 BUG-56：正常迁移后再次调用
// migrateProxyIdentity 幂等——不报错、行数与身份状态不变。
func TestMigrateProxyIdentityIdempotent(t *testing.T) {
	store := newTestStorage(t)
	if _, err := store.db.Exec(
		`INSERT INTO proxies (address, protocol, region, source, subscription_id, status)
		 VALUES ('idem-a:8080', 'http', 'us', ?, 0, 'active'),
		        ('idem-b:8080', 'http', 'us', 'free', 0, 'paused'),
		        ('idem-c:8080', 'http', 'us', 'manual', 0, 'active')`,
		legacySourceCustom,
	); err != nil {
		t.Fatalf("seed legacy proxies: %v", err)
	}

	if err := store.migrateProxyIdentity(); err != nil {
		t.Fatalf("migrateProxyIdentity() first run error = %v", err)
	}
	snapshot := snapshotProxyIdentities(t, store)

	if err := store.migrateProxyIdentity(); err != nil {
		t.Fatalf("migrateProxyIdentity() second run error = %v", err)
	}
	snapshot2 := snapshotProxyIdentities(t, store)

	if snapshot != snapshot2 {
		t.Fatalf("migrateProxyIdentity() not idempotent:\n first  = %s\n second = %s", snapshot, snapshot2)
	}

	// 额外确认第一次迁移已生效：custom -> subscription 且获配 subscription_id>0；
	// free -> manual；paused -> active+user_paused=1。
	var subSource string
	var subID int64
	if err := store.db.QueryRow(`SELECT source, subscription_id FROM proxies WHERE address = 'idem-a:8080'`).Scan(&subSource, &subID); err != nil {
		t.Fatalf("read idem-a: %v", err)
	}
	if subSource != SourceSubscription || subID <= 0 {
		t.Fatalf("idem-a after migration source=%q subID=%d, want subscription 且 subID>0", subSource, subID)
	}
	var pausedStatus string
	var userPaused int
	if err := store.db.QueryRow(`SELECT status, user_paused FROM proxies WHERE address = 'idem-b:8080'`).Scan(&pausedStatus, &userPaused); err != nil {
		t.Fatalf("read idem-b: %v", err)
	}
	if pausedStatus != "active" || userPaused != 1 {
		t.Fatalf("idem-b after migration status=%q user_paused=%d, want active/1", pausedStatus, userPaused)
	}
}

// snapshotProxyIdentities 返回按 id 排序的 (id,address,source,subscription_id,status,
// user_paused) 拼接串，用于幂等比对。
func snapshotProxyIdentities(t *testing.T, store *Storage) string {
	t.Helper()
	rows, err := store.db.Query(
		`SELECT id, address, source, subscription_id, status, user_paused
		 FROM proxies ORDER BY id`,
	)
	if err != nil {
		t.Fatalf("snapshot query: %v", err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var id, subID int64
		var address, source, status string
		var userPaused int
		if err := rows.Scan(&id, &address, &source, &subID, &status, &userPaused); err != nil {
			t.Fatalf("snapshot scan: %v", err)
		}
		fmt.Fprintf(&b, "%d|%s|%s|%d|%s|%d;", id, address, source, subID, status, userPaused)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("snapshot rows err: %v", err)
	}
	return b.String()
}
