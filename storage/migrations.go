package storage

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
)

func (s *Storage) migrateProxyGeoColumns() error {
	columns := []struct {
		name string
		sql  string
	}{
		{name: "region", sql: `ALTER TABLE proxies ADD COLUMN region TEXT NOT NULL DEFAULT ''`},
		{name: "note", sql: `ALTER TABLE proxies ADD COLUMN note TEXT NOT NULL DEFAULT ''`},
		{name: "region_source", sql: `ALTER TABLE proxies ADD COLUMN region_source TEXT NOT NULL DEFAULT ''`},
	}

	for _, column := range columns {
		if err := s.addProxyColumnIfMissing(column.name, column.sql); err != nil {
			return err
		}
	}
	_, err := s.db.Exec(`UPDATE proxies SET region = lower(region) WHERE region != ''`)
	if err != nil {
		return fmt.Errorf("normalize proxy region values: %w", err)
	}
	return nil
}

func (s *Storage) migrateRequiredProxyColumns() error {
	columns := []struct {
		name string
		sql  string
	}{
		{name: "exit_ip", sql: `ALTER TABLE proxies ADD COLUMN exit_ip TEXT NOT NULL DEFAULT ''`},
		{name: "exit_location", sql: `ALTER TABLE proxies ADD COLUMN exit_location TEXT NOT NULL DEFAULT ''`},
		{name: "latency", sql: `ALTER TABLE proxies ADD COLUMN latency INTEGER NOT NULL DEFAULT 0`},
		{name: "quality_grade", sql: `ALTER TABLE proxies ADD COLUMN quality_grade TEXT NOT NULL DEFAULT 'C'`},
		{name: "use_count", sql: `ALTER TABLE proxies ADD COLUMN use_count INTEGER NOT NULL DEFAULT 0`},
		{name: "success_count", sql: `ALTER TABLE proxies ADD COLUMN success_count INTEGER NOT NULL DEFAULT 0`},
		{name: "fail_count", sql: `ALTER TABLE proxies ADD COLUMN fail_count INTEGER NOT NULL DEFAULT 0`},
		{name: "last_used", sql: `ALTER TABLE proxies ADD COLUMN last_used DATETIME`},
		{name: "last_check", sql: `ALTER TABLE proxies ADD COLUMN last_check DATETIME`},
		{name: "created_at", sql: `ALTER TABLE proxies ADD COLUMN created_at DATETIME NOT NULL DEFAULT '1970-01-01 00:00:00'`},
		{name: "status", sql: `ALTER TABLE proxies ADD COLUMN status TEXT NOT NULL DEFAULT 'active'`},
		{name: "user_paused", sql: `ALTER TABLE proxies ADD COLUMN user_paused INTEGER NOT NULL DEFAULT 0`},
		{name: "source", sql: `ALTER TABLE proxies ADD COLUMN source TEXT NOT NULL DEFAULT 'manual'`},
		{name: "subscription_id", sql: `ALTER TABLE proxies ADD COLUMN subscription_id INTEGER NOT NULL DEFAULT 0`},
		{name: "ipapiis_score", sql: `ALTER TABLE proxies ADD COLUMN ipapiis_score REAL NOT NULL DEFAULT -1`},
		{name: "ipapi_flags", sql: `ALTER TABLE proxies ADD COLUMN ipapi_flags TEXT NOT NULL DEFAULT ''`},
		{name: "ipapi_flags_seen", sql: `ALTER TABLE proxies ADD COLUMN ipapi_flags_seen INTEGER NOT NULL DEFAULT 0`},
		{name: "starred", sql: `ALTER TABLE proxies ADD COLUMN starred INTEGER NOT NULL DEFAULT 0`},
		{name: "cf_blocked", sql: `ALTER TABLE proxies ADD COLUMN cf_blocked INTEGER NOT NULL DEFAULT -1`},
		{name: "dual_protocol", sql: `ALTER TABLE proxies ADD COLUMN dual_protocol INTEGER NOT NULL DEFAULT 0`},
		{name: "ai_reachability", sql: `ALTER TABLE proxies ADD COLUMN ai_reachability TEXT NOT NULL DEFAULT ''`},
		{name: "proxy_username", sql: `ALTER TABLE proxies ADD COLUMN proxy_username TEXT NOT NULL DEFAULT ''`},
		{name: "proxy_password", sql: `ALTER TABLE proxies ADD COLUMN proxy_password TEXT NOT NULL DEFAULT ''`},
		{name: "node_key", sql: `ALTER TABLE proxies ADD COLUMN node_key TEXT NOT NULL DEFAULT ''`},
	}

	for _, column := range columns {
		if err := s.addProxyColumnIfMissing(column.name, column.sql); err != nil {
			return err
		}
	}
	// 非空 node_key 上建索引，加速 -node-key- 锁定与订阅 upsert。
	// 不建全局 UNIQUE：历史空串可多行；业务层用 (subscription_id, node_key) 语义保证隧道唯一。
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_proxies_node_key ON proxies(node_key) WHERE node_key != ''`); err != nil {
		return fmt.Errorf("create idx_proxies_node_key: %w", err)
	}
	// owner 查询按 source/subscription_id 过滤；复合索引保持跨订阅多 owner，不做全局唯一约束。
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_proxies_subscription_node_key ON proxies(source, subscription_id, node_key) WHERE node_key != ''`); err != nil {
		return fmt.Errorf("create idx_proxies_subscription_node_key: %w", err)
	}
	return nil
}

// migrateProxyIdentity 将旧身份模型迁移到 (address, source, subscription_id)。
// 所有步骤在单一事务内执行，中途失败整体回滚，避免半迁移态；
// 每条语句都写成幂等形式，二次启动安全（匹配零行即无操作）。
func (s *Storage) migrateProxyIdentity() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE proxies SET source = ? WHERE source = ?`, SourceSubscription, legacySourceCustom); err != nil {
		return fmt.Errorf("normalize proxy legacy source: %w", err)
	}
	if _, err := tx.Exec(`UPDATE proxies SET source = ? WHERE source IN ('', 'free')`, SourceManual); err != nil {
		return fmt.Errorf("normalize proxy empty source: %w", err)
	}
	if _, err := tx.Exec(`UPDATE proxies SET subscription_id = 0 WHERE source != ?`, SourceSubscription); err != nil {
		return fmt.Errorf("normalize manual proxy subscription_id: %w", err)
	}
	if err := s.assignLegacySubscriptionID(tx); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE proxies SET status = 'active', user_paused = 1 WHERE status = 'paused'`); err != nil {
		return fmt.Errorf("migrate proxy paused state: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM proxies WHERE id NOT IN (SELECT MIN(id) FROM proxies GROUP BY address, source, subscription_id)`); err != nil {
		return fmt.Errorf("dedupe proxy identities: %w", err)
	}
	return tx.Commit()
}

func (s *Storage) assignLegacySubscriptionID(tx *sql.Tx) error {
	var count int
	err := tx.QueryRow(`SELECT COUNT(*) FROM proxies WHERE source = ? AND subscription_id <= 0`, SourceSubscription).Scan(&count)
	if err != nil {
		return fmt.Errorf("count legacy subscription proxies: %w", err)
	}
	if count == 0 {
		return nil
	}
	res, err := tx.Exec(`INSERT INTO subscriptions (name, format, refresh_min, status) VALUES ('Migrated subscription', 'auto', 60, 'active')`)
	if err != nil {
		return fmt.Errorf("create migrated subscription: %w", err)
	}
	legacySubID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("read migrated subscription id: %w", err)
	}
	if _, err := tx.Exec(`UPDATE proxies SET subscription_id = ? WHERE source = ? AND subscription_id <= 0`, legacySubID, SourceSubscription); err != nil {
		return fmt.Errorf("assign migrated subscription id: %w", err)
	}
	return nil
}

func (s *Storage) rebuildProxiesWithoutAddressUnique() error {
	var uniqueAddress int
	err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_index_list('proxies')
		WHERE origin = 'u' AND name = 'sqlite_autoindex_proxies_1'`).Scan(&uniqueAddress)
	if err != nil {
		return fmt.Errorf("check proxy address unique index: %w", err)
	}
	if uniqueAddress == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		CREATE TABLE proxies_new (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			address         TEXT NOT NULL,
			protocol        TEXT NOT NULL,
			region          TEXT NOT NULL DEFAULT '',
			region_source   TEXT NOT NULL DEFAULT '',
			note            TEXT NOT NULL DEFAULT '',
			exit_ip         TEXT NOT NULL DEFAULT '',
			exit_location   TEXT NOT NULL DEFAULT '',
			latency         INTEGER NOT NULL DEFAULT 0,
			quality_grade   TEXT NOT NULL DEFAULT 'C',
			use_count       INTEGER NOT NULL DEFAULT 0,
			success_count   INTEGER NOT NULL DEFAULT 0,
			fail_count      INTEGER NOT NULL DEFAULT 0,
			last_used       DATETIME,
			last_check      DATETIME,
			created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			status          TEXT NOT NULL DEFAULT 'active',
			user_paused     INTEGER NOT NULL DEFAULT 0,
			source          TEXT NOT NULL DEFAULT 'manual',
			subscription_id INTEGER NOT NULL DEFAULT 0,
			ipapiis_score   REAL NOT NULL DEFAULT -1,
			ipapi_flags     TEXT NOT NULL DEFAULT '',
			ipapi_flags_seen INTEGER NOT NULL DEFAULT 0,
			starred         INTEGER NOT NULL DEFAULT 0,
			cf_blocked      INTEGER NOT NULL DEFAULT -1,
			dual_protocol   INTEGER NOT NULL DEFAULT 0,
			ai_reachability TEXT NOT NULL DEFAULT '',
			proxy_username  TEXT NOT NULL DEFAULT '',
			proxy_password  TEXT NOT NULL DEFAULT '',
			node_key        TEXT NOT NULL DEFAULT ''
		)`); err != nil {
		return fmt.Errorf("create proxies_new: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO proxies_new (
			id, address, protocol, region, region_source, note, exit_ip, exit_location,
			latency, quality_grade, use_count, success_count, fail_count, last_used,
			last_check, created_at, status, user_paused, source, subscription_id, ipapiis_score, ipapi_flags, ipapi_flags_seen, starred, cf_blocked, dual_protocol, ai_reachability, proxy_username, proxy_password, node_key
		)
		SELECT id, address, protocol, region, region_source, note, exit_ip, exit_location,
			latency, quality_grade, use_count, success_count, fail_count, last_used,
			last_check, created_at, status, user_paused, source, subscription_id, ipapiis_score, ipapi_flags, ipapi_flags_seen, starred, cf_blocked, dual_protocol, ai_reachability, proxy_username, proxy_password, node_key
		FROM proxies`); err != nil {
		return fmt.Errorf("copy proxies_new: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE proxies`); err != nil {
		return fmt.Errorf("drop old proxies: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE proxies_new RENAME TO proxies`); err != nil {
		return fmt.Errorf("rename proxies_new: %w", err)
	}
	// rebuild 会丢掉 migrateRequiredProxyColumns 里建的部分索引，必须补回。
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_proxies_node_key ON proxies(node_key) WHERE node_key != ''`); err != nil {
		return fmt.Errorf("recreate idx_proxies_node_key after rebuild: %w", err)
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_proxies_subscription_node_key ON proxies(source, subscription_id, node_key) WHERE node_key != ''`); err != nil {
		return fmt.Errorf("recreate idx_proxies_subscription_node_key after rebuild: %w", err)
	}
	return tx.Commit()
}

// migrateSubscriptionIdentity 去重订阅（url / file_path），并把被删订阅下的代理
// 重定位到保留订阅。两个字段的去重在单一事务内执行，中途失败整体回滚，
// 避免出现「代理已重定位但重复订阅未删」或反之的半迁移态。语句幂等，二次启动安全。
func (s *Storage) migrateSubscriptionIdentity() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := dedupeSubscriptionsByField(tx, "url"); err != nil {
		return err
	}
	if err := dedupeSubscriptionsByField(tx, "file_path"); err != nil {
		return err
	}
	return tx.Commit()
}

// backfillDisabledRetentionClocks 仅为有持久化失败计数的 disabled 订阅节点补齐长期回收起点。
// NULL last_check 同时表达“待验证”，因此无法证明探测失败的 pending 节点必须保留 NULL；
// 重复启动也不得续期已有时间或改变手工、active 节点。
func (s *Storage) backfillDisabledRetentionClocks() error {
	if _, err := s.db.Exec(
		`UPDATE proxies
		 SET last_check = CURRENT_TIMESTAMP
		 WHERE source = ? AND status = 'disabled' AND last_check IS NULL
		   AND fail_count > 0`,
		SourceSubscription,
	); err != nil {
		return fmt.Errorf("backfill disabled retention clocks: %w", err)
	}
	return nil
}

func dedupeSubscriptionsByField(tx *sql.Tx, field string) error {
	if field != "url" && field != "file_path" {
		return fmt.Errorf("unsupported subscription dedupe field: %s", field)
	}
	remapSQL := fmt.Sprintf(`
		UPDATE proxies
		SET subscription_id = (
			SELECT MIN(kept.id)
			FROM subscriptions kept
			WHERE kept.%[1]s = (SELECT dup.%[1]s FROM subscriptions dup WHERE dup.id = proxies.subscription_id)
			  AND kept.%[1]s != ''
		)
		WHERE subscription_id IN (
			SELECT id FROM subscriptions WHERE %[1]s != ''
		)
		  AND subscription_id NOT IN (
			SELECT MIN(id) FROM subscriptions WHERE %[1]s != '' GROUP BY %[1]s
		)`, field)
	if _, err := tx.Exec(remapSQL); err != nil {
		return fmt.Errorf("remap duplicate subscription %s proxies: %w", field, err)
	}
	deleteSQL := fmt.Sprintf(`
		DELETE FROM subscriptions
		WHERE %[1]s != ''
		  AND id NOT IN (SELECT MIN(id) FROM subscriptions WHERE %[1]s != '' GROUP BY %[1]s)`, field)
	if _, err := tx.Exec(deleteSQL); err != nil {
		return fmt.Errorf("dedupe subscription %s values: %w", field, err)
	}
	return nil
}

func isUniqueConstraintError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}

func requireRowsAffected(rowsAffected int64, err error) error {
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Storage) addProxyColumnIfMissing(name, alterSQL string) error {
	return addProxyColumnIfMissing(s.db, name, alterSQL)
}

type sqlExecutor interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

type sqlQueryer interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

func addProxyColumnIfMissing(db interface {
	sqlExecutor
	sqlQueryer
}, name, alterSQL string) error {
	var exists int
	err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name = ?`, name).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check proxies.%s column: %w", name, err)
	}
	if exists > 0 {
		return nil
	}
	log.Printf("[storage] 迁移：添加 %s 列", name)
	if _, err := db.Exec(alterSQL); err != nil {
		return fmt.Errorf("add proxies.%s column: %w", name, err)
	}
	return nil
}

// addSubscriptionColumnIfMissing 对齐 addProxyColumnIfMissing：检查/ALTER 失败均上抛。
func (s *Storage) addSubscriptionColumnIfMissing(name, alterSQL string) error {
	var exists int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name = ?`, name).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check subscriptions.%s column: %w", name, err)
	}
	if exists > 0 {
		return nil
	}
	log.Printf("[storage] 迁移：添加 subscriptions.%s 列", name)
	if _, err := s.db.Exec(alterSQL); err != nil {
		return fmt.Errorf("add subscriptions.%s column: %w", name, err)
	}
	return nil
}

func normalizeRegion(region string) string {
	return strings.ToLower(strings.TrimSpace(region))
}
