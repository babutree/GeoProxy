package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"
)

func TestAddManualProxyWithNodeKeyRebindsAddress(t *testing.T) {
	store := newTestStorage(t)
	const nodeKey = "manual:stable-node"

	if err := store.AddManualProxyWithNodeKey("old.example:10001", "socks5", "us", "old", nodeKey); err != nil {
		t.Fatalf("AddManualProxyWithNodeKey(old) error = %v", err)
	}
	before, err := store.GetProxyByNodeKey(nodeKey)
	if err != nil {
		t.Fatalf("GetProxyByNodeKey(before) error = %v", err)
	}

	if err := store.AddManualProxyWithNodeKey("new.example:10002", "socks5", "us", "new", nodeKey); err != nil {
		t.Fatalf("AddManualProxyWithNodeKey(new) error = %v", err)
	}
	after, err := store.GetProxyByNodeKey(nodeKey)
	if err != nil {
		t.Fatalf("GetProxyByNodeKey(after) error = %v", err)
	}
	if after.Address != "new.example:10002" {
		t.Fatalf("Address = %q, want new.example:10002", after.Address)
	}
	if after.ID != before.ID {
		t.Fatalf("ID = %d, want original ID %d", after.ID, before.ID)
	}

	var count int
	if err := store.db.QueryRow(
		`SELECT COUNT(*) FROM proxies WHERE source = ? AND subscription_id = 0 AND node_key = ?`,
		SourceManual, nodeKey,
	).Scan(&count); err != nil {
		t.Fatalf("count rows by node_key: %v", err)
	}
	if count != 1 {
		t.Fatalf("rows for node_key = %d, want 1", count)
	}
}

func TestAddManualProxyWithNodeKeyRequiresUpdatedRow(t *testing.T) {
	store := newTestStorage(t)
	const nodeKey = "manual:ignored-rebind"

	if err := store.AddManualProxyWithNodeKey("old.example:11001", "socks5", "us", "old", nodeKey); err != nil {
		t.Fatalf("AddManualProxyWithNodeKey(old) error = %v", err)
	}
	if _, err := store.db.Exec(`
		CREATE TRIGGER ignore_manual_proxy_rebind
		BEFORE UPDATE OF address ON proxies
		WHEN OLD.node_key = 'manual:ignored-rebind'
		BEGIN
			SELECT RAISE(IGNORE);
		END
	`); err != nil {
		t.Fatalf("create ignore trigger: %v", err)
	}

	err := store.AddManualProxyWithNodeKey("new.example:11002", "socks5", "us", "new", nodeKey)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("AddManualProxyWithNodeKey() error = %v, want sql.ErrNoRows", err)
	}
}

func TestAddManualProxyWithNodeKeyConcurrentInsertKeepsOneRow(t *testing.T) {
	store := newTestStorage(t)
	const (
		nodeKey = "manual:concurrent-node"
		workers = 8
	)
	start := make(chan struct{})
	errCh := make(chan error, workers)

	for i := 0; i < workers; i++ {
		go func(index int) {
			<-start
			errCh <- store.AddManualProxyWithNodeKey(
				fmt.Sprintf("concurrent.example:%d", 12000+index),
				"socks5", "us", "concurrent", nodeKey,
			)
		}(i)
	}
	close(start)

	for i := 0; i < workers; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("concurrent AddManualProxyWithNodeKey() error = %v", err)
		}
	}

	var count int
	if err := store.db.QueryRow(
		`SELECT COUNT(*) FROM proxies WHERE source = ? AND subscription_id = 0 AND node_key = ?`,
		SourceManual, nodeKey,
	).Scan(&count); err != nil {
		t.Fatalf("count rows by node_key: %v", err)
	}
	if count != 1 {
		t.Fatalf("rows for node_key = %d, want 1", count)
	}
	if _, err := store.GetProxyByNodeKey(nodeKey); err != nil {
		t.Fatalf("GetProxyByNodeKey() error = %v", err)
	}
}

func TestGetDisabledCustomProxiesRequiresActiveParentSubscription(t *testing.T) {
	store := newTestStorage(t)
	insertTestSubscription(t, store, 1, "active")
	insertTestSubscription(t, store, 2, "paused")
	insertProxyFull(t, store, "active-parent.example:13001", SourceSubscription, 1, "disabled", 0)
	insertProxyFull(t, store, "paused-parent.example:13002", SourceSubscription, 2, "disabled", 0)
	insertProxyFull(t, store, "orphan-parent.example:13003", SourceSubscription, 999, "disabled", 0)

	proxies, err := store.GetDisabledCustomProxies()
	if err != nil {
		t.Fatalf("GetDisabledCustomProxies() error = %v", err)
	}
	if len(proxies) != 1 {
		t.Fatalf("GetDisabledCustomProxies() returned %d proxies, want 1: %#v", len(proxies), proxies)
	}
	if proxies[0].Address != "active-parent.example:13001" {
		t.Fatalf("GetDisabledCustomProxies()[0].Address = %q, want active parent proxy", proxies[0].Address)
	}
}

var addressRaceDriverCounter atomic.Uint64

type addressRaceBarrier struct {
	enabled     atomic.Bool
	fired       atomic.Bool
	reached     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
}

func newAddressRaceBarrier() *addressRaceBarrier {
	return &addressRaceBarrier{
		reached: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (b *addressRaceBarrier) compare(left, right string) int {
	if b.enabled.Load() && b.fired.CompareAndSwap(false, true) {
		close(b.reached)
		<-b.release
	}
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func (b *addressRaceBarrier) start() {
	b.enabled.Store(true)
}

func (b *addressRaceBarrier) unblock() {
	b.releaseOnce.Do(func() { close(b.release) })
}

func newAddressRaceStorage(t *testing.T) (*Storage, *sql.DB, *addressRaceBarrier) {
	t.Helper()
	barrier := newAddressRaceBarrier()
	driverName := fmt.Sprintf("sqlite3_address_race_%d", addressRaceDriverCounter.Add(1))
	sql.Register(driverName, &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			return conn.RegisterCollation("address_race", barrier.compare)
		},
	})

	dbPath := filepath.Join(t.TempDir(), "proxy.db")
	db, err := sql.Open(driverName, dbPath)
	if err != nil {
		t.Fatalf("sql.Open(store) error = %v", err)
	}
	db.SetMaxOpenConns(1)
	store := &Storage{db: db}
	if err := store.initSchema(); err != nil {
		_ = db.Close()
		t.Fatalf("initSchema() error = %v", err)
	}
	var proxySchema string
	if err := db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'proxies'`,
	).Scan(&proxySchema); err != nil {
		_ = db.Close()
		t.Fatalf("read proxies schema: %v", err)
	}
	addressColumn := regexp.MustCompile(`(?m)(\baddress\s+TEXT\s+NOT\s+NULL)(\s*,)`)
	barrierSchema := addressColumn.ReplaceAllString(proxySchema, `$1 COLLATE address_race$2`)
	if barrierSchema == proxySchema {
		_ = db.Close()
		t.Fatal("proxies.address schema was not rewritten with address_race collation")
	}
	if _, err := db.Exec(`DROP TABLE proxies`); err != nil {
		_ = db.Close()
		t.Fatalf("drop binary-collation proxies table: %v", err)
	}
	if _, err := db.Exec(barrierSchema); err != nil {
		_ = db.Close()
		t.Fatalf("create address-race proxies table: %v", err)
	}
	if _, err := db.Exec(
		`CREATE UNIQUE INDEX idx_proxy_identity ON proxies(address, source, subscription_id)`,
	); err != nil {
		_ = db.Close()
		t.Fatalf("create proxy identity index: %v", err)
	}
	var journalMode string
	if err := db.QueryRow(`PRAGMA journal_mode = WAL`).Scan(&journalMode); err != nil {
		_ = db.Close()
		t.Fatalf("enable WAL: %v", err)
	}
	if strings.ToLower(journalMode) != "wal" {
		_ = db.Close()
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}

	writer, err := sql.Open(driverName, dbPath)
	if err != nil {
		_ = db.Close()
		t.Fatalf("sql.Open(writer) error = %v", err)
	}
	writer.SetMaxOpenConns(1)
	if _, err := writer.Exec(`PRAGMA busy_timeout = 0`); err != nil {
		_ = writer.Close()
		_ = db.Close()
		t.Fatalf("disable writer busy timeout: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	t.Cleanup(func() { _ = writer.Close() })
	t.Cleanup(barrier.unblock)
	return store, writer, barrier
}

func seedAddressRaceProxy(t *testing.T, store *Storage, address string) int64 {
	t.Helper()
	insertTestSubscription(t, store, 1, "active")
	res, err := store.db.Exec(
		`INSERT INTO proxies
		 (address, protocol, source, subscription_id, status, user_paused, fail_count)
		 VALUES (?, 'http', ?, 0, 'disabled', 1, 2)`,
		address, SourceManual,
	)
	if err != nil {
		t.Fatalf("insert original proxy: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("original LastInsertId(): %v", err)
	}
	return id
}

func insertRacingSubscriptionProxy(db *sql.DB, address string) (int64, error) {
	res, err := db.Exec(
		`INSERT INTO proxies
		 (address, protocol, source, subscription_id, status, user_paused, fail_count)
		 VALUES (?, 'socks5', ?, 1, 'disabled', 1, 2)`,
		address, SourceSubscription,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func createAddressMutationAudit(t *testing.T, store *Storage) {
	t.Helper()
	if _, err := store.db.Exec(`CREATE TABLE address_mutation_audit (proxy_id INTEGER NOT NULL)`); err != nil {
		t.Fatalf("create mutation audit table: %v", err)
	}
	if _, err := store.db.Exec(`
		CREATE TRIGGER audit_proxy_update
		AFTER UPDATE ON proxies
		BEGIN
			INSERT INTO address_mutation_audit (proxy_id) VALUES (OLD.id);
		END
	`); err != nil {
		t.Fatalf("create update audit trigger: %v", err)
	}
	if _, err := store.db.Exec(`
		CREATE TRIGGER audit_proxy_delete
		AFTER DELETE ON proxies
		BEGIN
			INSERT INTO address_mutation_audit (proxy_id) VALUES (OLD.id);
		END
	`); err != nil {
		t.Fatalf("create delete audit trigger: %v", err)
	}
}

func addressMutationAuditIDs(t *testing.T, store *Storage) []int64 {
	t.Helper()
	rows, err := store.db.Query(`SELECT proxy_id FROM address_mutation_audit ORDER BY proxy_id`)
	if err != nil {
		t.Fatalf("query mutation audit: %v", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan mutation audit: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate mutation audit: %v", err)
	}
	return ids
}

func waitAddressRaceBarrier(t *testing.T, barrier *addressRaceBarrier) {
	t.Helper()
	select {
	case <-barrier.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("address comparison barrier was not reached")
	}
}

func waitAddressOperation(t *testing.T, errCh <-chan error) error {
	t.Helper()
	select {
	case err := <-errCh:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("address-only operation did not finish")
		return nil
	}
}

func isSQLiteLockError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "locked") || strings.Contains(message, "busy")
}

func TestGetProxyByAddressUsesSingleSnapshotDuringIdentityReplacement(t *testing.T) {
	store, writer, barrier := newAddressRaceStorage(t)
	const address = "snapshot-get.example:14001"
	originalID := seedAddressRaceProxy(t, store, address)

	barrier.start()
	type result struct {
		proxy *Proxy
		err   error
	}
	resultCh := make(chan result, 1)
	go func() {
		proxy, err := store.GetProxyByAddress(address)
		resultCh <- result{proxy: proxy, err: err}
	}()
	waitAddressRaceBarrier(t, barrier)

	tx, err := writer.Begin()
	if err == nil {
		_, err = tx.Exec(`DELETE FROM proxies WHERE id = ?`, originalID)
	}
	if err == nil {
		_, err = tx.Exec(
			`INSERT INTO proxies (address, protocol, source, subscription_id) VALUES (?, 'http', ?, 0)`,
			address, SourceManual,
		)
	}
	if err == nil {
		_, err = tx.Exec(
			`INSERT INTO proxies (address, protocol, source, subscription_id) VALUES (?, 'socks5', ?, 1)`,
			address, SourceSubscription,
		)
	}
	if err == nil {
		err = tx.Commit()
	} else if tx != nil {
		_ = tx.Rollback()
	}
	barrier.unblock()
	if err != nil {
		t.Fatalf("replace identity during address lookup: %v", err)
	}

	var got result
	select {
	case got = <-resultCh:
	case <-time.After(5 * time.Second):
		t.Fatal("GetProxyByAddress() did not finish")
	}
	if got.err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", got.err)
	}
	if got.proxy == nil || got.proxy.ID != originalID {
		t.Fatalf("GetProxyByAddress() = %#v, want original snapshot ID %d", got.proxy, originalID)
	}
}

func TestGetProxyByAddressMissingReturnsSQLNoRows(t *testing.T) {
	store := newTestStorage(t)
	const address = "missing-address.example:14002"
	proxy, err := store.GetProxyByAddress(address)
	if proxy != nil || !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetProxyByAddress(missing) = (%#v, %v), want (nil, sql.ErrNoRows)", proxy, err)
	}
	if !strings.Contains(err.Error(), address) {
		t.Fatalf("GetProxyByAddress(missing) error = %q, want address %q", err, address)
	}
}

type addressOnlyMutationCase struct {
	name string
	run  func(*Storage, string) error
}

func addressOnlyMutationCases() []addressOnlyMutationCase {
	return []addressOnlyMutationCase{
		{name: "Delete", run: func(store *Storage, address string) error {
			return store.Delete(address)
		}},
		{name: "IncrFail", run: func(store *Storage, address string) error {
			return store.IncrFail(address)
		}},
		{name: "ResetFail", run: func(store *Storage, address string) error {
			return store.ResetFail(address)
		}},
		{name: "UpdateLatency", run: func(store *Storage, address string) error {
			return store.UpdateLatency(address, 321)
		}},
		{name: "UpdateExitInfo", run: func(store *Storage, address string) error {
			return store.UpdateExitInfo(address, "203.0.113.10", "US Test", 321, 10, "test", true, 0, "{}")
		}},
		{name: "IncrementFailCount", run: func(store *Storage, address string) error {
			return store.IncrementFailCount(address)
		}},
		{name: "DisableProxy", run: func(store *Storage, address string) error {
			return store.DisableProxy(address)
		}},
		{name: "EnableProxy", run: func(store *Storage, address string) error {
			return store.EnableProxy(address)
		}},
		{name: "PauseProxy", run: func(store *Storage, address string) error {
			return store.PauseProxy(address)
		}},
		{name: "UnpauseProxy", run: func(store *Storage, address string) error {
			return store.UnpauseProxy(address)
		}},
		{name: "UpdateProxyRegion", run: func(store *Storage, address string) error {
			return store.UpdateProxyRegion(address, "jp", true)
		}},
		{name: "UpdateProxyNote", run: func(store *Storage, address string) error {
			return store.UpdateProxyNote(address, "updated")
		}},
		{name: "RecordProxyUse", run: func(store *Storage, address string) error {
			return store.RecordProxyUse(address, true)
		}},
	}
}

func TestAddressOnlyMutationsAreAtomicAgainstConcurrentIdentityInsert(t *testing.T) {
	for _, test := range addressOnlyMutationCases() {
		t.Run(test.name, func(t *testing.T) {
			store, writer, barrier := newAddressRaceStorage(t)
			address := "atomic-" + strings.ToLower(test.name) + ".example:15001"
			originalID := seedAddressRaceProxy(t, store, address)
			createAddressMutationAudit(t, store)

			barrier.start()
			errCh := make(chan error, 1)
			go func() { errCh <- test.run(store, address) }()
			waitAddressRaceBarrier(t, barrier)
			insertedID, insertErr := insertRacingSubscriptionProxy(writer, address)
			barrier.unblock()
			operationErr := waitAddressOperation(t, errCh)

			if insertErr != nil {
				if !isSQLiteLockError(insertErr) {
					t.Fatalf("concurrent identity insert error = %v, want SQLite lock or success", insertErr)
				}
				insertedID, insertErr = insertRacingSubscriptionProxy(writer, address)
				if insertErr != nil {
					t.Fatalf("insert identity after atomic statement: %v", insertErr)
				}
			}
			if operationErr != nil {
				t.Fatalf("%s() error = %v", test.name, operationErr)
			}

			auditedIDs := addressMutationAuditIDs(t, store)
			if len(auditedIDs) != 1 || auditedIDs[0] != originalID {
				t.Fatalf(
					"%s() mutated IDs %v after concurrent identity %d, want only original ID %d",
					test.name, auditedIDs, insertedID, originalID,
				)
			}
		})
	}
}

func TestAddressOnlyMutationsRejectExistingAmbiguity(t *testing.T) {
	for _, test := range addressOnlyMutationCases() {
		t.Run(test.name, func(t *testing.T) {
			store := newTestStorage(t)
			address := "ambiguous-" + strings.ToLower(test.name) + ".example:16001"
			insertTestSubscription(t, store, 1, "active")
			insertProxyFull(t, store, address, SourceManual, 0, "disabled", 1)
			insertProxyFull(t, store, address, SourceSubscription, 1, "disabled", 1)
			createAddressMutationAudit(t, store)

			err := test.run(store, address)
			if !errors.Is(err, ErrAmbiguousProxyAddress) {
				t.Fatalf("%s() error = %v, want ErrAmbiguousProxyAddress", test.name, err)
			}
			if auditedIDs := addressMutationAuditIDs(t, store); len(auditedIDs) != 0 {
				t.Fatalf("%s() mutated ambiguous IDs %v", test.name, auditedIDs)
			}
		})
	}
}

func TestAddressOnlyMutationsMissingReturnsSQLNoRows(t *testing.T) {
	for _, test := range addressOnlyMutationCases() {
		t.Run(test.name, func(t *testing.T) {
			store := newTestStorage(t)
			address := "missing-" + strings.ToLower(test.name) + ".example:17001"
			err := test.run(store, address)
			if !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("%s() error = %v, want sql.ErrNoRows", test.name, err)
			}
		})
	}
}
