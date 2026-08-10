package storage

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestFreshSchemaCreatesNullableProxyClockColumns(t *testing.T) {
	store := newTestStorage(t)
	for _, column := range []string{"exit_checked_at", "disabled_at"} {
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		err := store.db.QueryRow(
			`SELECT type, "notnull", dflt_value FROM pragma_table_info('proxies') WHERE name = ?`,
			column,
		).Scan(&columnType, &notNull, &defaultValue)
		if err != nil {
			t.Fatalf("query fresh schema column %s: %v", column, err)
		}
		if columnType != "DATETIME" || notNull != 0 || defaultValue.Valid {
			t.Fatalf("fresh schema column %s = type:%q notnull:%d default:%v, want nullable DATETIME without default",
				column, columnType, notNull, defaultValue)
		}
	}
}

func TestLegacyAddressUniqueMigrationPreservesValuesAndBackfillsOnlyProvenFailures(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy-clock-migration.db")
	lastCheck := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	createLegacyClockDatabase(t, dbPath, lastCheck)

	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("New(legacy) error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	proven := mustProxyByAddress(t, store, "proven-failure:8080")
	if proven.ExitIP != "203.0.113.9" || proven.ExitLocation != "JP Tokyo" || proven.FailCount != 2 ||
		proven.Status != "disabled" || proven.Source != SourceSubscription || proven.SubscriptionID != 1 {
		t.Fatalf("legacy values changed during address UNIQUE rebuild: %#v", proven)
	}
	if !proven.LastCheck.Equal(lastCheck) {
		t.Fatalf("legacy last_check changed during migration: got %v, want %v", proven.LastCheck, lastCheck)
	}
	if got := proxyTimeField(t, proven, "ExitCheckedAt"); !got.IsZero() {
		t.Fatalf("legacy exit metadata forged exit_checked_at: %v", got)
	}
	if got := proxyTimeField(t, proven, "DisabledAt"); got.IsZero() {
		t.Fatal("proven disabled subscription did not receive disabled_at")
	}

	for _, address := range []string{
		"pending-subscription:8080",
		"manual-disabled:8080",
		"active-subscription:8080",
		"user-paused:8080",
		"parent-paused:8080",
		"orphan-subscription:8080", "unknown-parent:8080",
	} {
		proxy := mustProxyByAddress(t, store, address)
		if got := proxyTimeField(t, proxy, "DisabledAt"); !got.IsZero() {
			t.Fatalf("%s received unsupported disabled_at %v", address, got)
		}
		if !proxy.LastCheck.IsZero() {
			t.Fatalf("%s last_check changed during migration: %v", address, proxy.LastCheck)
		}
		if got := proxyTimeField(t, proxy, "ExitCheckedAt"); !got.IsZero() {
			t.Fatalf("%s received forged exit_checked_at %v", address, got)
		}
	}

	// 旧 address UNIQUE 必须已经移除；同地址、不同 owner 可同时存在。
	if err := store.AddManualProxy("proven-failure:8080", "http", "", ""); err != nil {
		t.Fatalf("address UNIQUE still active after rebuild: %v", err)
	}

	preservedDisabledAt := time.Date(2026, time.July, 17, 8, 9, 10, 0, time.UTC)
	if _, err := store.db.Exec(
		"UPDATE proxies SET disabled_at = ? WHERE id = ?",
		preservedDisabledAt.Format("2006-01-02 15:04:05"), proven.ID,
	); err != nil {
		t.Fatalf("seed existing disabled_at: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close(first migration) error = %v", err)
	}

	store, err = New(dbPath)
	if err != nil {
		t.Fatalf("New(idempotent) error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	proven, err = store.GetProxyByIdentity("proven-failure:8080", SourceSubscription, 1)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(proven-failure:8080) error = %v", err)
	}
	if got := proxyTimeField(t, proven, "DisabledAt"); !got.Equal(preservedDisabledAt) {
		t.Fatalf("repeated initialization renewed disabled_at: got %v, want %v", got, preservedDisabledAt)
	}
	if !proven.LastCheck.Equal(lastCheck) {
		t.Fatalf("repeated initialization changed last_check: got %v, want %v", proven.LastCheck, lastCheck)
	}
}

func TestProxyClockColumnsScanRoundTrip(t *testing.T) {
	store := newTestStorage(t)
	if err := store.AddProxy("clock-scan:8080", "http"); err != nil {
		t.Fatalf("AddProxy() error = %v", err)
	}
	lastCheck := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	exitCheckedAt := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)
	disabledAt := time.Date(2026, time.March, 4, 5, 6, 7, 0, time.UTC)
	if _, err := store.db.Exec(
		`UPDATE proxies SET last_check = ?, exit_checked_at = ?, disabled_at = ? WHERE address = ?`,
		lastCheck.Format("2006-01-02 15:04:05"),
		exitCheckedAt.Format("2006-01-02 15:04:05"),
		disabledAt.Format("2006-01-02 15:04:05"),
		"clock-scan:8080",
	); err != nil {
		t.Fatalf("seed proxy clocks: %v", err)
	}

	proxy := mustProxyByAddress(t, store, "clock-scan:8080")
	if !proxy.LastCheck.Equal(lastCheck) {
		t.Fatalf("LastCheck = %v, want %v", proxy.LastCheck, lastCheck)
	}
	if got := proxyTimeField(t, proxy, "ExitCheckedAt"); !got.Equal(exitCheckedAt) {
		t.Fatalf("ExitCheckedAt = %v, want %v", got, exitCheckedAt)
	}
	if got := proxyTimeField(t, proxy, "DisabledAt"); !got.Equal(disabledAt) {
		t.Fatalf("DisabledAt = %v, want %v", got, disabledAt)
	}
}

func createLegacyClockDatabase(t *testing.T, dbPath string, lastCheck time.Time) {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	defer db.Close()

	statements := []string{
		`CREATE TABLE subscriptions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL DEFAULT '', url TEXT NOT NULL DEFAULT '', file_path TEXT NOT NULL DEFAULT '',
			format TEXT NOT NULL DEFAULT 'auto', refresh_min INTEGER NOT NULL DEFAULT 60,
			last_fetch DATETIME, status TEXT NOT NULL DEFAULT 'active', proxy_count INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`INSERT INTO subscriptions (id, name, status) VALUES
			(1, 'active-parent', 'active'), (2, 'paused-parent', 'paused'), (3, 'unknown-parent', 'unknown')`,
		`CREATE TABLE proxies (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			address TEXT NOT NULL UNIQUE,
			protocol TEXT NOT NULL,
			exit_ip TEXT NOT NULL DEFAULT '', exit_location TEXT NOT NULL DEFAULT '',
			fail_count INTEGER NOT NULL DEFAULT 0,
			last_check DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			status TEXT NOT NULL DEFAULT 'active',
			user_paused INTEGER NOT NULL DEFAULT 0,
			source TEXT NOT NULL DEFAULT 'manual',
			subscription_id INTEGER NOT NULL DEFAULT 0
		)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("create legacy database: %v", err)
		}
	}

	type legacyProxy struct {
		address        string
		exitIP         string
		exitLocation   string
		failCount      int
		lastCheck      interface{}
		status         string
		userPaused     int
		source         string
		subscriptionID int64
	}
	rows := []legacyProxy{
		{"proven-failure:8080", "203.0.113.9", "JP Tokyo", 2, lastCheck.Format("2006-01-02 15:04:05"), "disabled", 0, SourceSubscription, 1},
		{"pending-subscription:8080", "", "", 0, nil, "disabled", 0, SourceSubscription, 1},
		{"manual-disabled:8080", "", "", 3, nil, "disabled", 0, SourceManual, 0},
		{"active-subscription:8080", "", "", 3, nil, "active", 0, SourceSubscription, 1},
		{"user-paused:8080", "", "", 3, nil, "disabled", 1, SourceSubscription, 1},
		{"parent-paused:8080", "", "", 3, nil, "disabled", 0, SourceSubscription, 2},
		{"orphan-subscription:8080", "", "", 3, nil, "disabled", 0, SourceSubscription, 999},
		{"unknown-parent:8080", "", "", 3, nil, "disabled", 0, SourceSubscription, 3},
	}
	for _, row := range rows {
		if _, err := db.Exec(
			`INSERT INTO proxies
				(address, protocol, exit_ip, exit_location, fail_count, last_check, status, user_paused, source, subscription_id)
			 VALUES (?, 'http', ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.address, row.exitIP, row.exitLocation, row.failCount, row.lastCheck,
			row.status, row.userPaused, row.source, row.subscriptionID,
		); err != nil {
			t.Fatalf("seed legacy proxy %s: %v", row.address, err)
		}
	}
}

func mustProxyByAddress(t *testing.T, store *Storage, address string) *Proxy {
	t.Helper()
	proxy, err := store.GetProxyByAddress(address)
	if err != nil {
		t.Fatalf("GetProxyByAddress(%s) error = %v", address, err)
	}
	return proxy
}

func proxyTimeField(t *testing.T, proxy *Proxy, name string) time.Time {
	t.Helper()
	field := reflect.ValueOf(proxy).Elem().FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("Proxy.%s field is missing", name)
	}
	value, ok := field.Interface().(time.Time)
	if !ok {
		t.Fatalf("Proxy.%s type = %s, want time.Time", name, field.Type())
	}
	return value
}
