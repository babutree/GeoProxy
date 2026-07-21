package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStorageInitializationBackfillsEvidenceBearingRetentionClockWithoutChangingPending(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "retention-migration.db")
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("New(initial) error = %v", err)
	}
	subID, err := store.AddSubscription("legacy-retention", "", "legacy-retention.yaml", "auto", 60, "")
	if err != nil {
		_ = store.Close()
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if _, err := store.GetDB().Exec(
		`INSERT INTO proxies (address, protocol, source, subscription_id, status, fail_count, last_used, last_check, exit_ip, exit_location, latency)
		 VALUES ('legacy-sub-failed:8080', 'http', ?, ?, 'disabled', 1, CURRENT_TIMESTAMP, NULL, '', '', 0),
		        ('legacy-sub-metadata:8080', 'http', ?, ?, 'disabled', 0, NULL, NULL, '203.0.113.8', 'JP Tokyo', 120),
		        ('legacy-sub-used:8080', 'http', ?, ?, 'disabled', 0, CURRENT_TIMESTAMP, NULL, '', '', 0),
		        ('pending-subscription:8080', 'http', ?, ?, 'disabled', 0, NULL, NULL, '', '', 0),
		        ('legacy-sub-active:8080', 'http', ?, ?, 'active', 0, NULL, NULL, '', '', 0),
		        ('legacy-manual-disabled:8080', 'http', ?, 0, 'disabled', 0, NULL, NULL, '', '', 0)`,
		SourceSubscription, subID, SourceSubscription, subID, SourceSubscription, subID,
		SourceSubscription, subID, SourceSubscription, subID, SourceManual,
	); err != nil {
		_ = store.Close()
		t.Fatalf("seed legacy retention rows: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close(initial) error = %v", err)
	}

	store, err = New(dbPath)
	if err != nil {
		t.Fatalf("New(backfill) error = %v", err)
	}
	disabled, err := store.GetProxyByAddress("legacy-sub-failed:8080")
	if err != nil {
		_ = store.Close()
		t.Fatalf("GetProxyByAddress(evidence-bearing subscription disabled) error = %v", err)
	}
	if disabled.LastCheck.IsZero() {
		_ = store.Close()
		t.Fatal("storage initialization left evidence-bearing disabled subscription last_check NULL")
	}
	metadata, err := store.GetProxyByAddress("legacy-sub-metadata:8080")
	if err != nil {
		_ = store.Close()
		t.Fatalf("GetProxyByAddress(metadata-bearing subscription disabled) error = %v", err)
	}
	if !metadata.LastCheck.IsZero() {
		_ = store.Close()
		t.Fatalf("storage initialization treated metadata-only disabled row as proven failure: %v", metadata.LastCheck)
	}
	used, err := store.GetProxyByAddress("legacy-sub-used:8080")
	if err != nil {
		_ = store.Close()
		t.Fatalf("GetProxyByAddress(last-used-only subscription disabled) error = %v", err)
	}
	if !used.LastCheck.IsZero() {
		_ = store.Close()
		t.Fatalf("storage initialization treated last_used-only disabled row as proven failure: %v", used.LastCheck)
	}
	pending, err := store.GetProxyByAddress("pending-subscription:8080")
	if err != nil {
		_ = store.Close()
		t.Fatalf("GetProxyByAddress(pending subscription) error = %v", err)
	}
	if !pending.LastCheck.IsZero() {
		_ = store.Close()
		t.Fatalf("storage initialization changed pending subscription last_check to %v, want NULL", pending.LastCheck)
	}
	active, err := store.GetProxyByAddress("legacy-sub-active:8080")
	if err != nil {
		_ = store.Close()
		t.Fatalf("GetProxyByAddress(subscription active) error = %v", err)
	}
	if !active.LastCheck.IsZero() {
		_ = store.Close()
		t.Fatalf("storage initialization changed subscription active last_check to %v, want NULL", active.LastCheck)
	}
	manual, err := store.GetProxyByAddress("legacy-manual-disabled:8080")
	if err != nil {
		_ = store.Close()
		t.Fatalf("GetProxyByAddress(manual disabled) error = %v", err)
	}
	if !manual.LastCheck.IsZero() {
		_ = store.Close()
		t.Fatalf("storage initialization changed pending manual last_check to %v, want NULL", manual.LastCheck)
	}

	preservedAt := time.Date(2026, time.July, 17, 8, 9, 10, 0, time.UTC)
	if _, err := store.GetDB().Exec(
		"UPDATE proxies SET last_check = ? WHERE address = ?",
		preservedAt.Format("2006-01-02 15:04:05"), disabled.Address,
	); err != nil {
		_ = store.Close()
		t.Fatalf("seed existing disabled clock: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close(backfill) error = %v", err)
	}

	store, err = New(dbPath)
	if err != nil {
		t.Fatalf("New(idempotent) error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	disabled, err = store.GetProxyByAddress("legacy-sub-failed:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress(evidence-bearing subscription disabled after repeat) error = %v", err)
	}
	if !disabled.LastCheck.Equal(preservedAt) {
		t.Fatalf("repeated initialization renewed disabled clock: got %v, want %v", disabled.LastCheck, preservedAt)
	}
	metadata, err = store.GetProxyByAddress("legacy-sub-metadata:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress(metadata-bearing subscription disabled after repeat) error = %v", err)
	}
	if !metadata.LastCheck.IsZero() {
		t.Fatalf("repeated initialization changed metadata-only disabled row: %v", metadata.LastCheck)
	}
	used, err = store.GetProxyByAddress("legacy-sub-used:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress(last-used-only subscription disabled after repeat) error = %v", err)
	}
	if !used.LastCheck.IsZero() {
		t.Fatalf("repeated initialization changed last_used-only disabled row: %v", used.LastCheck)
	}
	pending, err = store.GetProxyByAddress("pending-subscription:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress(pending subscription after repeat) error = %v", err)
	}
	if !pending.LastCheck.IsZero() {
		t.Fatalf("repeated initialization changed pending subscription last_check to %v, want NULL", pending.LastCheck)
	}
	active, err = store.GetProxyByAddress("legacy-sub-active:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress(subscription active after repeat) error = %v", err)
	}
	if !active.LastCheck.IsZero() {
		t.Fatalf("repeated initialization changed subscription active last_check to %v, want NULL", active.LastCheck)
	}
	manual, err = store.GetProxyByAddress("legacy-manual-disabled:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress(manual disabled after repeat) error = %v", err)
	}
	if !manual.LastCheck.IsZero() {
		t.Fatalf("repeated initialization changed pending manual last_check to %v, want NULL", manual.LastCheck)
	}
}
