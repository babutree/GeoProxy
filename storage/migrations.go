package storage

import (
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
		{name: "subscription_id", sql: `ALTER TABLE proxies ADD COLUMN subscription_id INTEGER NOT NULL DEFAULT 0`},
	}

	for _, column := range columns {
		if err := s.addProxyColumnIfMissing(column.name, column.sql); err != nil {
			return err
		}
	}
	return nil
}

func (s *Storage) addProxyColumnIfMissing(name, alterSQL string) error {
	var exists int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name = ?`, name).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check proxies.%s column: %w", name, err)
	}
	if exists > 0 {
		return nil
	}
	log.Printf("[storage] migrating: adding %s column", name)
	if _, err := s.db.Exec(alterSQL); err != nil {
		return fmt.Errorf("add proxies.%s column: %w", name, err)
	}
	return nil
}

func normalizeRegion(region string) string {
	return strings.ToLower(strings.TrimSpace(region))
}
