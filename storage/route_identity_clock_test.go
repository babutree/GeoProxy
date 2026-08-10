package storage

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestApplyProbeObservationRejectsRouteRebind(t *testing.T) {
	store := newTestStorage(t)
	const nodeKey = "manual:route-cas"
	if err := store.AddManualProxyWithNodeKey("old.example:10001", "socks5", "us", "old", nodeKey); err != nil {
		t.Fatalf("AddManualProxyWithNodeKey(old) error = %v", err)
	}
	stale, err := store.GetProxyByNodeKey(nodeKey)
	if err != nil {
		t.Fatalf("GetProxyByNodeKey(stale) error = %v", err)
	}

	identity := RouteIdentityFromProxy(*stale)
	if err := store.AddManualProxyWithNodeKey("new.example:10002", "http", "gb", "new", nodeKey); err != nil {
		t.Fatalf("AddManualProxyWithNodeKey(rebind) error = %v", err)
	}
	err = store.ApplyProbeObservation(identity, ExitObservation{
		ExitIP:       "203.0.113.10",
		ExitLocation: "JP Tokyo",
		LatencyMS:    42,
	})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("ApplyProbeObservation(stale route) error = %v, want sql.ErrNoRows", err)
	}
	current, err := store.GetProxyByNodeKey(nodeKey)
	if err != nil {
		t.Fatalf("GetProxyByNodeKey(current) error = %v", err)
	}
	if current.ExitIP != "" || current.ExitLocation != "" || !current.LastCheck.IsZero() || !current.ExitCheckedAt.IsZero() {
		t.Fatalf("stale probe wrote rebound route: %#v", current)
	}
}

func TestRecordForwardFailureDoesNotAdvanceProbeClock(t *testing.T) {
	store := newTestStorage(t)
	if err := store.AddManualProxy("forward.example:10001", "socks5", "us", ""); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	proxy, err := store.GetProxyByIdentity("forward.example:10001", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() error = %v", err)
	}
	observedAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	if _, err := store.db.Exec(
		`UPDATE proxies SET status = 'active', last_check = ?, exit_checked_at = ?, disabled_at = NULL WHERE id = ?`,
		observedAt.Format("2006-01-02 15:04:05"), observedAt.Format("2006-01-02 15:04:05"), proxy.ID,
	); err != nil {
		t.Fatalf("seed probe clocks: %v", err)
	}

	disabled, err := store.RecordForwardFailure(RouteIdentityFromProxy(*proxy), 1)
	if err != nil {
		t.Fatalf("RecordForwardFailure() error = %v", err)
	}
	if !disabled {
		t.Fatal("RecordForwardFailure() did not report threshold disable")
	}
	after, err := store.GetProxyByIdentity("forward.example:10001", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(after) error = %v", err)
	}
	if !after.LastCheck.Equal(observedAt) || !after.ExitCheckedAt.Equal(observedAt) {
		t.Fatalf("forward failure changed probe clocks: last=%v exit=%v", after.LastCheck, after.ExitCheckedAt)
	}
	if after.DisabledAt.IsZero() || after.Status != "disabled" || after.FailCount != 1 {
		t.Fatalf("forward failure state = %#v, want disabled with first failure clock", after)
	}
}

func TestRecordForwardSuccessDoesNotAdvanceProbeClock(t *testing.T) {
	store := newTestStorage(t)
	if err := store.AddManualProxy("forward-success.example:10001", "socks5", "us", ""); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	proxy, err := store.GetProxyByIdentity("forward-success.example:10001", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() error = %v", err)
	}
	observedAt := time.Date(2026, time.January, 3, 3, 4, 5, 0, time.UTC)
	if _, err := store.db.Exec(
		`UPDATE proxies SET status = 'active', fail_count = 2, last_check = ?, exit_checked_at = ? WHERE id = ?`,
		observedAt.Format("2006-01-02 15:04:05"), observedAt.Format("2006-01-02 15:04:05"), proxy.ID,
	); err != nil {
		t.Fatalf("seed probe clocks: %v", err)
	}

	if err := store.RecordForwardSuccess(RouteIdentityFromProxy(*proxy)); err != nil {
		t.Fatalf("RecordForwardSuccess() error = %v", err)
	}
	after, err := store.GetProxyByIdentity("forward-success.example:10001", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(after) error = %v", err)
	}
	if !after.LastCheck.Equal(observedAt) || !after.ExitCheckedAt.Equal(observedAt) {
		t.Fatalf("forward success changed probe clocks: last=%v exit=%v", after.LastCheck, after.ExitCheckedAt)
	}
	if after.UseCount != 1 || after.SuccessCount != 1 || after.FailCount != 0 || after.LastUsed.IsZero() {
		t.Fatalf("forward success state = %#v, want usage record and cleared failure count", after)
	}
}

func TestRecordProbeFailurePersistsTrustedExitAndClocks(t *testing.T) {
	store := newTestStorage(t)
	if err := store.AddManualProxy("probe-failure.example:10001", "http", "us", ""); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	proxy, err := store.GetProxyByIdentity("probe-failure.example:10001", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() error = %v", err)
	}
	if _, err := store.db.Exec(`UPDATE proxies SET status = 'active' WHERE id = ?`, proxy.ID); err != nil {
		t.Fatalf("activate proxy: %v", err)
	}

	disabled, err := store.RecordProbeFailure(RouteIdentityFromProxy(*proxy), ExitObservation{
		ExitIP:       "203.0.113.44",
		ExitLocation: "GB London",
		LatencyMS:    64,
	}, 1)
	if err != nil {
		t.Fatalf("RecordProbeFailure() error = %v", err)
	}
	if !disabled {
		t.Fatal("RecordProbeFailure() did not report threshold disable")
	}
	after, err := store.GetProxyByIdentity("probe-failure.example:10001", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(after) error = %v", err)
	}
	if after.ExitIP != "203.0.113.44" || after.ExitLocation != "GB London" || after.Latency != 64 {
		t.Fatalf("probe failure discarded trusted exit: %#v", after)
	}
	if after.LastCheck.IsZero() || after.ExitCheckedAt.IsZero() || after.DisabledAt.IsZero() {
		t.Fatalf("probe failure clocks = last=%v exit=%v disabled=%v, want all set", after.LastCheck, after.ExitCheckedAt, after.DisabledAt)
	}
	if after.Status != "disabled" || after.FailCount != 1 {
		t.Fatalf("probe failure state = %#v, want threshold disabled", after)
	}
}

func TestRecoverProxyFromProbeAtomicallyClearsDisabledClock(t *testing.T) {
	store := newTestStorage(t)
	if err := store.AddManualProxy("recover.example:10001", "socks5", "us", ""); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	proxy, err := store.GetProxyByIdentity("recover.example:10001", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() error = %v", err)
	}
	if _, err := store.db.Exec(`UPDATE proxies SET status = 'disabled', fail_count = 3, disabled_at = CURRENT_TIMESTAMP WHERE id = ?`, proxy.ID); err != nil {
		t.Fatalf("seed disabled proxy: %v", err)
	}

	err = store.RecoverProxyFromProbe(RouteIdentityFromProxy(*proxy), ExitObservation{
		ExitIP:       "203.0.113.66",
		ExitLocation: "JP Tokyo",
		LatencyMS:    48,
	})
	if err != nil {
		t.Fatalf("RecoverProxyFromProbe() error = %v", err)
	}
	after, err := store.GetProxyByIdentity("recover.example:10001", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(after) error = %v", err)
	}
	if after.Status != "active" || after.FailCount != 0 || !after.DisabledAt.IsZero() {
		t.Fatalf("recovery state = %#v, want active with cleared disabled_at", after)
	}
	if after.ExitIP != "203.0.113.66" || after.ExitLocation != "JP Tokyo" || after.LastCheck.IsZero() || after.ExitCheckedAt.IsZero() {
		t.Fatalf("recovery did not atomically write trusted observation: %#v", after)
	}
}

func TestDisableBlockedCountriesDoesNotCreateProbeOrDisableClock(t *testing.T) {
	store := newTestStorage(t)
	if err := store.AddManualProxy("policy.example:10001", "socks5", "us", ""); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	proxy, err := store.GetProxyByIdentity("policy.example:10001", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() error = %v", err)
	}
	if _, err := store.db.Exec(`UPDATE proxies SET status = 'active' WHERE id = ?`, proxy.ID); err != nil {
		t.Fatalf("activate proxy: %v", err)
	}
	if _, err := store.DisableBlockedCountries([]string{"US"}); err != nil {
		t.Fatalf("DisableBlockedCountries() error = %v", err)
	}
	after, err := store.GetProxyByIdentity("policy.example:10001", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(after) error = %v", err)
	}
	if after.Status != "disabled" || !after.LastCheck.IsZero() || !after.DisabledAt.IsZero() {
		t.Fatalf("policy disable forged probe clocks: %#v", after)
	}
}

func TestDisableNotAllowedCountriesDoesNotCreateProbeOrDisableClock(t *testing.T) {
	store := newTestStorage(t)
	if err := store.AddManualProxy("policy-allow.example:10001", "socks5", "us", ""); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	proxy, err := store.GetProxyByIdentity("policy-allow.example:10001", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() error = %v", err)
	}
	if _, err := store.db.Exec(`UPDATE proxies SET status = 'active' WHERE id = ?`, proxy.ID); err != nil {
		t.Fatalf("activate proxy: %v", err)
	}
	if _, err := store.DisableNotAllowedCountries([]string{"GB"}); err != nil {
		t.Fatalf("DisableNotAllowedCountries() error = %v", err)
	}
	after, err := store.GetProxyByIdentity("policy-allow.example:10001", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(after) error = %v", err)
	}
	if after.Status != "disabled" || !after.LastCheck.IsZero() || !after.DisabledAt.IsZero() {
		t.Fatalf("allowlist policy disable forged probe clocks: %#v", after)
	}
}

func TestPauseSubscriptionDoesNotCreateNodeClocks(t *testing.T) {
	store := newTestStorage(t)
	subscriptionID, err := store.AddSubscription("clock", "https://example.invalid/sub", "", "plain", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if err := store.AddProxyWithSource("subscription-pause.example:10001", "socks5", SourceSubscription, subscriptionID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}
	if err := store.PauseSubscription(subscriptionID); err != nil {
		t.Fatalf("PauseSubscription() error = %v", err)
	}
	proxy, err := store.GetProxyByIdentity("subscription-pause.example:10001", SourceSubscription, subscriptionID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() error = %v", err)
	}
	if proxy.Status != "disabled" || !proxy.LastCheck.IsZero() || !proxy.ExitCheckedAt.IsZero() || !proxy.DisabledAt.IsZero() {
		t.Fatalf("subscription pause forged node clocks: %#v", proxy)
	}
}

func TestToggleSubscriptionPauseDoesNotCreateNodeClocks(t *testing.T) {
	store := newTestStorage(t)
	subscriptionID, err := store.AddSubscription("toggle-clock", "https://example.invalid/toggle", "", "plain", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if err := store.AddProxyWithSource("subscription-toggle.example:10001", "socks5", SourceSubscription, subscriptionID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}
	status, err := store.ToggleSubscription(subscriptionID)
	if err != nil {
		t.Fatalf("ToggleSubscription() error = %v", err)
	}
	if status != "paused" {
		t.Fatalf("ToggleSubscription() status = %q, want paused", status)
	}
	proxy, err := store.GetProxyByIdentity("subscription-toggle.example:10001", SourceSubscription, subscriptionID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() error = %v", err)
	}
	if proxy.Status != "disabled" || !proxy.LastCheck.IsZero() || !proxy.ExitCheckedAt.IsZero() || !proxy.DisabledAt.IsZero() {
		t.Fatalf("subscription toggle pause forged node clocks: %#v", proxy)
	}
}

func TestRecordDisabledProbeFailureAdvancesCheckWithoutRenewingDisabledClock(t *testing.T) {
	store := newTestStorage(t)
	if err := store.AddManualProxy("disabled-probe.example:10001", "socks5", "us", ""); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	proxy, err := store.GetProxyByIdentity("disabled-probe.example:10001", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() error = %v", err)
	}
	disabledAt := time.Date(2026, time.January, 1, 1, 2, 3, 0, time.UTC)
	if _, err := store.db.Exec(
		`UPDATE proxies SET status = 'disabled', fail_count = 3, last_check = NULL, disabled_at = ? WHERE id = ?`,
		disabledAt.Format("2006-01-02 15:04:05"), proxy.ID,
	); err != nil {
		t.Fatalf("seed disabled proxy: %v", err)
	}
	if err := store.RecordDisabledProbeFailure(RouteIdentityFromProxy(*proxy), ExitObservation{
		ExitIP:       "203.0.113.70",
		ExitLocation: "JP Osaka",
		LatencyMS:    72,
	}); err != nil {
		t.Fatalf("RecordDisabledProbeFailure() error = %v", err)
	}
	after, err := store.GetProxyByIdentity("disabled-probe.example:10001", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(after) error = %v", err)
	}
	if after.LastCheck.IsZero() || after.ExitCheckedAt.IsZero() || !after.DisabledAt.Equal(disabledAt) {
		t.Fatalf("disabled probe clocks = last=%v exit=%v disabled=%v, want refreshed checks and preserved disabled_at", after.LastCheck, after.ExitCheckedAt, after.DisabledAt)
	}
	if after.Status != "disabled" || after.FailCount != 3 || after.ExitIP != "203.0.113.70" {
		t.Fatalf("disabled probe changed non-observation state: %#v", after)
	}
}

func TestApplyProbeObservationRejectsPausedSubscription(t *testing.T) {
	store := newTestStorage(t)
	subscriptionID, err := store.AddSubscription("paused-cas", "https://example.invalid/paused", "", "plain", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if err := store.AddProxyWithSource("paused-cas.example:10001", "socks5", SourceSubscription, subscriptionID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}
	proxy, err := store.GetProxyByIdentity("paused-cas.example:10001", SourceSubscription, subscriptionID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() error = %v", err)
	}
	identity := RouteIdentityFromProxy(*proxy)
	if err := store.PauseSubscription(subscriptionID); err != nil {
		t.Fatalf("PauseSubscription() error = %v", err)
	}
	err = store.ApplyProbeObservation(identity, ExitObservation{ExitIP: "203.0.113.80", ExitLocation: "JP Tokyo", LatencyMS: 80})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("ApplyProbeObservation(paused subscription) error = %v, want sql.ErrNoRows", err)
	}
}

func TestRecordProbeFailureRejectsPausedSubscription(t *testing.T) {
	store := newTestStorage(t)
	subscriptionID, err := store.AddSubscription("paused-failure", "https://example.invalid/paused-failure", "", "plain", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if err := store.AddProxyWithSource("paused-failure.example:10001", "socks5", SourceSubscription, subscriptionID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}
	proxy, err := store.GetProxyByIdentity("paused-failure.example:10001", SourceSubscription, subscriptionID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() error = %v", err)
	}
	identity := RouteIdentityFromProxy(*proxy)
	if err := store.PauseSubscription(subscriptionID); err != nil {
		t.Fatalf("PauseSubscription() error = %v", err)
	}
	_, err = store.RecordProbeFailure(identity, ExitObservation{}, 1)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("RecordProbeFailure(paused subscription) error = %v, want sql.ErrNoRows", err)
	}
}

func TestDisableRouteForPolicyDoesNotCreateClocks(t *testing.T) {
	store := newTestStorage(t)
	if err := store.AddManualProxy("route-policy.example:10001", "socks5", "us", ""); err != nil {
		t.Fatalf("AddManualProxy() error = %v", err)
	}
	proxy, err := store.GetProxyByIdentity("route-policy.example:10001", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() error = %v", err)
	}
	if _, err := store.db.Exec(`UPDATE proxies SET status = 'active' WHERE id = ?`, proxy.ID); err != nil {
		t.Fatalf("activate proxy: %v", err)
	}
	if err := store.DisableRouteForPolicy(RouteIdentityFromProxy(*proxy)); err != nil {
		t.Fatalf("DisableRouteForPolicy() error = %v", err)
	}
	after, err := store.GetProxyByIdentity("route-policy.example:10001", SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(after) error = %v", err)
	}
	if after.Status != "disabled" || !after.LastCheck.IsZero() || !after.ExitCheckedAt.IsZero() || !after.DisabledAt.IsZero() {
		t.Fatalf("route policy disable created clocks: %#v", after)
	}
}
