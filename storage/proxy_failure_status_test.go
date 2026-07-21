package storage

import (
	"database/sql"
	"errors"
	"testing"
)

// 这两个场景刻意保留读取时的旧 Proxy 快照，验证写入结果来自数据库当前状态。
func TestRecordProxyFailureByIDWithStatusUsesDatabaseState(t *testing.T) {
	t.Run("stale snapshot low while database reaches threshold", func(t *testing.T) {
		store := newTestStorage(t)
		insertProxy(t, store, "failure-authority-low:8080", "http", "us", SourceManual, 100, "active", 0)
		stale, err := store.GetProxyByAddress("failure-authority-low:8080")
		if err != nil {
			t.Fatalf("GetProxyByAddress() error = %v", err)
		}
		for i := 0; i < 2; i++ {
			if err := store.RecordProxyFailureByID(stale.ID, 3); err != nil {
				t.Fatalf("seed database failure %d: %v", i+1, err)
			}
		}

		disabled, err := store.RecordProxyFailureByIDWithStatus(stale.ID, 3)
		if err != nil {
			t.Fatalf("RecordProxyFailureByIDWithStatus() error = %v", err)
		}
		if !disabled {
			t.Fatal("authoritative disabled = false, want true")
		}
		if stale.FailCount != 0 {
			t.Fatalf("stale snapshot fail_count = %d, want unchanged 0", stale.FailCount)
		}
		current, err := store.GetProxyByID(stale.ID)
		if err != nil {
			t.Fatalf("GetProxyByID() error = %v", err)
		}
		if current.FailCount != 3 || current.Status != "disabled" {
			t.Fatalf("database state = %d/%q, want 3/disabled", current.FailCount, current.Status)
		}
	})

	t.Run("stale snapshot high after database success reset", func(t *testing.T) {
		store := newTestStorage(t)
		insertProxy(t, store, "failure-authority-high:8080", "http", "us", SourceManual, 100, "active", 2)
		stale, err := store.GetProxyByAddress("failure-authority-high:8080")
		if err != nil {
			t.Fatalf("GetProxyByAddress() error = %v", err)
		}
		if err := store.RecordProxyUseByID(stale.ID, true); err != nil {
			t.Fatalf("reset database failure count: %v", err)
		}

		disabled, err := store.RecordProxyFailureByIDWithStatus(stale.ID, 3)
		if err != nil {
			t.Fatalf("RecordProxyFailureByIDWithStatus() error = %v", err)
		}
		if disabled {
			t.Fatal("authoritative disabled = true, want false")
		}
		if stale.FailCount != 2 {
			t.Fatalf("stale snapshot fail_count = %d, want unchanged 2", stale.FailCount)
		}
		current, err := store.GetProxyByID(stale.ID)
		if err != nil {
			t.Fatalf("GetProxyByID() error = %v", err)
		}
		if current.FailCount != 1 || current.Status != "active" {
			t.Fatalf("database state = %d/%q, want 1/active", current.FailCount, current.Status)
		}
	})
}

func TestRecordProxyFailureByIDWithStatusRejectsMissingID(t *testing.T) {
	store := newTestStorage(t)
	disabled, err := store.RecordProxyFailureByIDWithStatus(999999, 3)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("error = %v, want sql.ErrNoRows", err)
	}
	if disabled {
		t.Fatal("missing proxy reported disabled")
	}
}
