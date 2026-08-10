package storage

import (
	"testing"
	"time"
)

func TestAddManualProxyWithNodeKeyRebindClearsRouteEvidence(t *testing.T) {
	store := newTestStorage(t)
	const nodeKey = "manual:stable-clock-rebind"
	if err := store.AddManualProxyWithNodeKey("old.example:10001", "socks5", "us", "old", nodeKey); err != nil {
		t.Fatalf("AddManualProxyWithNodeKey(old) error = %v", err)
	}
	before, err := store.GetProxyByNodeKey(nodeKey)
	if err != nil {
		t.Fatalf("GetProxyByNodeKey(before) error = %v", err)
	}
	observedAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	if _, err := store.db.Exec(
		`UPDATE proxies SET exit_ip = ?, exit_location = ?, latency = ?, quality_grade = 'S', fail_count = 3,
		 last_check = ?, exit_checked_at = ?, disabled_at = ?, ipapiis_score = 0.8, ipapi_flags = 'proxy',
		 ipapi_flags_seen = 1, cf_blocked = 1, ai_reachability = '{"openai":1}' WHERE id = ?`,
		"203.0.113.9", "JP Tokyo", 42, observedAt.Format("2006-01-02 15:04:05"),
		observedAt.Format("2006-01-02 15:04:05"), observedAt.Format("2006-01-02 15:04:05"), before.ID,
	); err != nil {
		t.Fatalf("seed route evidence: %v", err)
	}

	if err := store.AddManualProxyWithNodeKey("new.example:10002", "http", "gb", "new", nodeKey); err != nil {
		t.Fatalf("AddManualProxyWithNodeKey(rebind) error = %v", err)
	}
	after, err := store.GetProxyByNodeKey(nodeKey)
	if err != nil {
		t.Fatalf("GetProxyByNodeKey(after) error = %v", err)
	}
	if after.ID != before.ID || after.Address != "new.example:10002" || after.Protocol != "http" || after.Status != "disabled" {
		t.Fatalf("rebind identity = %#v, want same ID with new pending route", after)
	}
	if after.ExitIP != "" || after.ExitLocation != "" || after.Latency != 0 || after.QualityGrade != "C" || after.FailCount != 0 {
		t.Fatalf("rebind retained exit or health evidence: %#v", after)
	}
	if !after.LastCheck.IsZero() || !after.ExitCheckedAt.IsZero() || !after.DisabledAt.IsZero() {
		t.Fatalf("rebind retained route clocks: last=%v exit=%v disabled=%v", after.LastCheck, after.ExitCheckedAt, after.DisabledAt)
	}
	if after.IPAPIIsScore != -1 || after.IPAPIFlags != "" || after.IPAPIFlagsSeen || after.CFBlocked != -1 || after.AIReachability != "" {
		t.Fatalf("rebind retained risk evidence: %#v", after)
	}
}

func TestAddManualProxyWithNodeKeySameRoutePreservesEvidence(t *testing.T) {
	store := newTestStorage(t)
	const nodeKey = "manual:stable-same-route"
	const address = "same.example:10001"
	if err := store.AddManualProxyWithNodeKey(address, "socks5", "us", "old", nodeKey); err != nil {
		t.Fatalf("AddManualProxyWithNodeKey(initial) error = %v", err)
	}
	before, err := store.GetProxyByNodeKey(nodeKey)
	if err != nil {
		t.Fatalf("GetProxyByNodeKey(before) error = %v", err)
	}
	observedAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	if _, err := store.db.Exec(
		`UPDATE proxies SET exit_ip = ?, exit_location = ?, latency = ?, quality_grade = 'S', fail_count = 2,
		 last_check = ?, exit_checked_at = ?, disabled_at = ?, ipapiis_score = 0.8, ipapi_flags = 'proxy',
		 ipapi_flags_seen = 1, cf_blocked = 1, ai_reachability = '{"openai":1}' WHERE id = ?`,
		"203.0.113.9", "US Test", 42, observedAt.Format("2006-01-02 15:04:05"),
		observedAt.Format("2006-01-02 15:04:05"), observedAt.Format("2006-01-02 15:04:05"), before.ID,
	); err != nil {
		t.Fatalf("seed route evidence: %v", err)
	}

	if err := store.AddManualProxyWithNodeKey(address, "socks5", "gb", "updated note", nodeKey); err != nil {
		t.Fatalf("AddManualProxyWithNodeKey(same route) error = %v", err)
	}
	after, err := store.GetProxyByNodeKey(nodeKey)
	if err != nil {
		t.Fatalf("GetProxyByNodeKey(after) error = %v", err)
	}
	if after.ID != before.ID || after.Address != address || after.Protocol != "socks5" || after.Region != "gb" || after.Note != "updated note" {
		t.Fatalf("same-route update = %#v, want metadata update without rebind", after)
	}
	if after.ExitIP != "203.0.113.9" || after.ExitLocation != "US Test" || after.Latency != 42 || after.QualityGrade != "S" || after.FailCount != 2 {
		t.Fatalf("same-route update cleared exit or health evidence: %#v", after)
	}
	if !after.LastCheck.Equal(observedAt) || !after.ExitCheckedAt.Equal(observedAt) || !after.DisabledAt.Equal(observedAt) {
		t.Fatalf("same-route update cleared route clocks: last=%v exit=%v disabled=%v", after.LastCheck, after.ExitCheckedAt, after.DisabledAt)
	}
	if after.IPAPIIsScore != 0.8 || after.IPAPIFlags != "proxy" || !after.IPAPIFlagsSeen || after.CFBlocked != 1 || after.AIReachability != `{"openai":1}` {
		t.Fatalf("same-route update cleared risk evidence: %#v", after)
	}
}
