package storage

import (
	"database/sql"
	"path/filepath"
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

func TestRegionQueriesFilterCountAndExclude(t *testing.T) {
	store := newTestStorage(t)
	insertProxy(t, store, "us-fast:8080", "http", "us", "manual", 20, "active", 0)
	insertProxy(t, store, "us-slow:8080", "http", "us", SourceSubscription, 80, "active", 0)
	insertProxy(t, store, "jp:8080", "socks5", "jp", "manual", 10, "active", 0)
	insertProxy(t, store, "us-disabled:8080", "http", "us", "manual", 1, "disabled", 0)
	insertProxy(t, store, "us-failing:8080", "http", "us", "manual", 1, "active", 3)

	proxies, err := store.GetByRegion("US", []string{"us-fast:8080"})
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
	insertProxyWithRegionSource(t, store, "auto:8080", "", "auto")
	insertProxyWithRegionSource(t, store, "manual:8080", "jp", "manual")

	if err := store.UpdateExitInfo("auto:8080", "8.8.8.8", "US Mountain View", 120); err != nil {
		t.Fatalf("UpdateExitInfo(auto) error = %v", err)
	}
	if err := store.UpdateExitInfo("manual:8080", "1.1.1.1", "US Los Angeles", 80); err != nil {
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
	insertProxyWithRegionSource(t, store, "unknown:8080", "hk", "auto")

	if err := store.UpdateExitInfo("unknown:8080", "9.9.9.9", "USA Miami", 150); err != nil {
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
		`INSERT INTO proxies (address, protocol, region, source, latency, status, fail_count)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		address, protocol, region, source, latency, status, failCount,
	)
	if err != nil {
		t.Fatalf("insert proxy %s: %v", address, err)
	}
}

func insertProxyWithRegionSource(t *testing.T, store *Storage, address, region, regionSource string) {
	t.Helper()
	_, err := store.db.Exec(
		`INSERT INTO proxies (address, protocol, region, region_source, source, status)
		 VALUES (?, 'http', ?, ?, ?, 'active')`,
		address, region, regionSource, SourceSubscription,
	)
	if err != nil {
		t.Fatalf("insert proxy %s: %v", address, err)
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
