package storage

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	sqlite3 "github.com/mattn/go-sqlite3"
)

var iterationErrorDriverCounter atomic.Uint64

func newIterationErrorStorage(t *testing.T) (*Storage, string) {
	t.Helper()
	sequence := iterationErrorDriverCounter.Add(1)
	marker := fmt.Sprintf("storage-iteration-error-%d", sequence)
	driverName := fmt.Sprintf("sqlite3_storage_iteration_%d", sequence)
	sql.Register(driverName, &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			return conn.RegisterFunc(
				"storage_iteration_error",
				func() (string, error) { return "", fmt.Errorf("%s", marker) },
				false,
			)
		},
	})

	db, err := sql.Open(driverName, filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	db.SetMaxOpenConns(1)
	store := &Storage{db: db}
	if err := store.initSchema(); err != nil {
		_ = db.Close()
		t.Fatalf("initSchema() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, marker
}

func newProxyIterationErrorStorage(t *testing.T, faultColumn string, failFirst bool) (*Storage, string) {
	t.Helper()
	store, marker := newIterationErrorStorage(t)
	_, err := store.db.Exec(`
		INSERT INTO proxies
			(id, address, protocol, latency, quality_grade, last_check, status, source, subscription_id, node_key)
		VALUES
			(1, 'iteration-good:18001', 'http', 10, 'S', datetime('now', '-2 days'), 'active', 'manual', 0, 'good'),
			(2, 'iteration-fail:18002', 'http', 20, 'A', datetime('now', '-1 day'), 'active', 'manual', 0, 'fail')
	`)
	if err != nil {
		t.Fatalf("seed proxies: %v", err)
	}
	if _, err := store.db.Exec(`ALTER TABLE proxies RENAME TO iteration_proxies`); err != nil {
		t.Fatalf("rename proxies: %v", err)
	}

	condition := "id = 1"
	if failFirst {
		condition = "0"
	}
	faultExpression := fmt.Sprintf(
		"CASE WHEN %s THEN %s ELSE storage_iteration_error() END AS %s",
		condition, faultColumn, faultColumn,
	)
	projection := strings.Replace(proxyColumns, faultColumn, faultExpression, 1)
	if projection == proxyColumns {
		t.Fatalf("fault column %q not found in proxyColumns", faultColumn)
	}
	if _, err := store.db.Exec(`CREATE VIEW proxies AS SELECT ` + projection + ` FROM iteration_proxies`); err != nil {
		t.Fatalf("create proxies fault view: %v", err)
	}
	return store, marker
}

func newSubscriptionIterationErrorStorage(t *testing.T) (*Storage, string) {
	t.Helper()
	store, marker := newIterationErrorStorage(t)
	_, err := store.db.Exec(`
		INSERT INTO subscriptions
			(id, name, url, file_path, format, refresh_min, status, created_at, headers)
		VALUES
			(1, 'iteration-good', 'https://good.test/sub', '', 'auto', 60, 'active', datetime('now', '-20 days'), '{}'),
			(2, 'iteration-fail', 'https://fail.test/sub', '', 'auto', 60, 'active', datetime('now', '-30 days'), '{}')
	`)
	if err != nil {
		t.Fatalf("seed subscriptions: %v", err)
	}
	if _, err := store.db.Exec(`ALTER TABLE subscriptions RENAME TO iteration_subscriptions`); err != nil {
		t.Fatalf("rename subscriptions: %v", err)
	}
	faultExpression := `CASE WHEN id = 1 THEN headers ELSE storage_iteration_error() END AS headers`
	projection := strings.Replace(subColumns, "headers", faultExpression, 1)
	if projection == subColumns {
		t.Fatal("headers not found in subColumns")
	}
	if _, err := store.db.Exec(`CREATE VIEW subscriptions AS SELECT ` + projection + ` FROM iteration_subscriptions`); err != nil {
		t.Fatalf("create subscriptions fault view: %v", err)
	}
	return store, marker
}

func requireIterationError(t *testing.T, err error, marker string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), marker) {
		t.Fatalf("error = %v, want SQLite iteration marker %q", err, marker)
	}
}

func TestSQLiteIterationErrorFixtureFailsAfterReadableProxyRow(t *testing.T) {
	store, marker := newProxyIterationErrorStorage(t, "node_key", false)
	rows, err := store.db.Query(`SELECT ` + proxyColumns + ` FROM proxies ORDER BY id ASC`)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatalf("first Next() = false, rows.Err() = %v", rows.Err())
	}
	proxy, err := scanProxy(rows)
	if err != nil {
		t.Fatalf("scan first row: %v", err)
	}
	if proxy.ID != 1 {
		t.Fatalf("first proxy ID = %d, want 1", proxy.ID)
	}
	if rows.Next() {
		t.Fatal("second Next() = true, want SQLite iteration error")
	}
	requireIterationError(t, rows.Err(), marker)
}

func TestProxyQueriesPropagateSQLiteIterationErrors(t *testing.T) {
	tests := []struct {
		name        string
		faultColumn string
		failFirst   bool
		run         func(*Storage) error
	}{
		{
			name:        "GetRandom",
			faultColumn: "node_key",
			failFirst:   true,
			run: func(store *Storage) error {
				_, err := store.GetRandom()
				return err
			},
		},
		{
			name:        "GetAllForAdmin",
			faultColumn: "node_key",
			run: func(store *Storage) error {
				_, err := store.GetAllForAdmin()
				return err
			},
		},
		{
			name:        "GetAllFiltered",
			faultColumn: "node_key",
			run: func(store *Storage) error {
				_, err := store.GetAllFiltered("")
				return err
			},
		},
		{
			name:        "GetBatchForHealthCheck",
			faultColumn: "node_key",
			run: func(store *Storage) error {
				_, err := store.GetBatchForHealthCheck(10)
				return err
			},
		},
		{
			name:        "GetByProtocol",
			faultColumn: "node_key",
			run: func(store *Storage) error {
				_, err := store.GetByProtocol("http")
				return err
			},
		},
		{
			name:        "GetQualityDistribution",
			faultColumn: "quality_grade",
			run: func(store *Storage) error {
				_, err := store.GetQualityDistribution()
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, marker := newProxyIterationErrorStorage(t, test.faultColumn, test.failFirst)
			requireIterationError(t, test.run(store), marker)
		})
	}
}

func TestSubscriptionQueriesPropagateSQLiteIterationErrors(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Storage) error
	}{
		{
			name: "GetSubscriptions",
			run: func(store *Storage) error {
				_, err := store.GetSubscriptions()
				return err
			},
		},
		{
			name: "GetSubscription",
			run: func(store *Storage) error {
				_, err := store.GetSubscription(2)
				return err
			},
		},
		{
			name: "GetStaleSubscriptions",
			run: func(store *Storage) error {
				_, err := store.GetStaleSubscriptions(1)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, marker := newSubscriptionIterationErrorStorage(t)
			requireIterationError(t, test.run(store), marker)
		})
	}
}
