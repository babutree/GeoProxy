package storage

import (
	"testing"
	"time"
)

func TestDisableBlockedCountriesSetsRetentionClockWithoutRenewingDisabled(t *testing.T) {
	store := newTestStorage(t)
	insertProxy(t, store, "blocked-active:8080", "http", "cn", SourceManual, 100, "active", 0)
	insertProxy(t, store, "blocked-degraded:8080", "http", "", SourceManual, 100, "degraded", 0)
	insertProxy(t, store, "blocked-existing:8080", "http", "cn", SourceManual, 100, "disabled", 0)
	insertProxy(t, store, "blocked-unmatched:8080", "http", "us", SourceManual, 100, "active", 0)

	preservedAt := time.Date(2026, time.July, 18, 1, 2, 3, 0, time.UTC)
	if _, err := store.GetDB().Exec(
		"UPDATE proxies SET exit_location = 'CN Beijing' WHERE address = ?",
		"blocked-degraded:8080",
	); err != nil {
		t.Fatalf("seed degraded exit_location: %v", err)
	}
	if _, err := store.GetDB().Exec(
		"UPDATE proxies SET last_check = ? WHERE address = ?",
		preservedAt.Format("2006-01-02 15:04:05"), "blocked-existing:8080",
	); err != nil {
		t.Fatalf("seed existing disabled clock: %v", err)
	}

	affected, err := store.DisableBlockedCountries([]string{"CN"})
	if err != nil {
		t.Fatalf("DisableBlockedCountries() error = %v", err)
	}
	if affected != 2 {
		t.Fatalf("DisableBlockedCountries() affected = %d, want 2 active/degraded rows", affected)
	}
	assertDisabledWithClock(t, store, "blocked-active:8080")
	assertDisabledWithClock(t, store, "blocked-degraded:8080")
	assertProxyStatusAndClock(t, store, "blocked-existing:8080", "disabled", preservedAt)
	assertProxyStatusAndClock(t, store, "blocked-unmatched:8080", "active", time.Time{})

	// 已禁用行不再命中 WHERE；用固定旧时间排除同秒时间戳造成的假通过。
	if _, err := store.GetDB().Exec(
		"UPDATE proxies SET last_check = ? WHERE address = ?",
		preservedAt.Format("2006-01-02 15:04:05"), "blocked-active:8080",
	); err != nil {
		t.Fatalf("seed repeated-call clock: %v", err)
	}
	affected, err = store.DisableBlockedCountries([]string{"CN"})
	if err != nil {
		t.Fatalf("DisableBlockedCountries() repeated error = %v", err)
	}
	if affected != 0 {
		t.Fatalf("DisableBlockedCountries() repeated affected = %d, want 0", affected)
	}
	assertProxyStatusAndClock(t, store, "blocked-active:8080", "disabled", preservedAt)
}

func TestDisableNotAllowedCountriesSetsRetentionClockWithoutRenewingDisabled(t *testing.T) {
	store := newTestStorage(t)
	insertProxy(t, store, "not-allowed-active:8080", "http", "cn", SourceManual, 100, "active", 0)
	insertProxy(t, store, "not-allowed-degraded:8080", "http", "", SourceManual, 100, "degraded", 0)
	insertProxy(t, store, "not-allowed-existing:8080", "http", "de", SourceManual, 100, "disabled", 0)
	insertProxy(t, store, "allowed-active:8080", "http", "jp", SourceManual, 100, "active", 0)
	insertProxy(t, store, "unknown-active:8080", "http", "", SourceManual, 100, "active", 0)

	preservedAt := time.Date(2026, time.July, 18, 4, 5, 6, 0, time.UTC)
	if _, err := store.GetDB().Exec(
		"UPDATE proxies SET exit_location = 'DE Berlin' WHERE address = ?",
		"not-allowed-degraded:8080",
	); err != nil {
		t.Fatalf("seed degraded exit_location: %v", err)
	}
	if _, err := store.GetDB().Exec(
		"UPDATE proxies SET last_check = ? WHERE address = ?",
		preservedAt.Format("2006-01-02 15:04:05"), "not-allowed-existing:8080",
	); err != nil {
		t.Fatalf("seed existing disabled clock: %v", err)
	}

	affected, err := store.DisableNotAllowedCountries([]string{"JP"})
	if err != nil {
		t.Fatalf("DisableNotAllowedCountries() error = %v", err)
	}
	if affected != 2 {
		t.Fatalf("DisableNotAllowedCountries() affected = %d, want 2 active/degraded rows", affected)
	}
	assertDisabledWithClock(t, store, "not-allowed-active:8080")
	assertDisabledWithClock(t, store, "not-allowed-degraded:8080")
	assertProxyStatusAndClock(t, store, "not-allowed-existing:8080", "disabled", preservedAt)
	assertProxyStatusAndClock(t, store, "allowed-active:8080", "active", time.Time{})
	assertProxyStatusAndClock(t, store, "unknown-active:8080", "active", time.Time{})

	if _, err := store.GetDB().Exec(
		"UPDATE proxies SET last_check = ? WHERE address = ?",
		preservedAt.Format("2006-01-02 15:04:05"), "not-allowed-active:8080",
	); err != nil {
		t.Fatalf("seed repeated-call clock: %v", err)
	}
	affected, err = store.DisableNotAllowedCountries([]string{"JP"})
	if err != nil {
		t.Fatalf("DisableNotAllowedCountries() repeated error = %v", err)
	}
	if affected != 0 {
		t.Fatalf("DisableNotAllowedCountries() repeated affected = %d, want 0", affected)
	}
	assertProxyStatusAndClock(t, store, "not-allowed-active:8080", "disabled", preservedAt)
}

func assertDisabledWithClock(t *testing.T, store *Storage, address string) {
	t.Helper()
	proxy, err := store.GetProxyByAddress(address)
	if err != nil {
		t.Fatalf("GetProxyByAddress(%q) error = %v", address, err)
	}
	if proxy.Status != "disabled" || proxy.LastCheck.IsZero() {
		t.Fatalf("proxy %q = status:%q last_check:%v, want disabled with retention clock",
			address, proxy.Status, proxy.LastCheck)
	}
}

func assertProxyStatusAndClock(t *testing.T, store *Storage, address, wantStatus string, wantClock time.Time) {
	t.Helper()
	proxy, err := store.GetProxyByAddress(address)
	if err != nil {
		t.Fatalf("GetProxyByAddress(%q) error = %v", address, err)
	}
	if proxy.Status != wantStatus || !proxy.LastCheck.Equal(wantClock) {
		t.Fatalf("proxy %q = status:%q last_check:%v, want status:%q last_check:%v",
			address, proxy.Status, proxy.LastCheck, wantStatus, wantClock)
	}
}
