package storage

import (
	"fmt"
	"log"
)

// AddProxy 新增手动节点，已存在则忽略
func (s *Storage) AddProxy(address, protocol string) error {
	result, err := s.db.Exec(
		`INSERT INTO proxies (address, protocol, source, subscription_id, region_source)
		 VALUES (?, ?, 'manual', 0, 'auto')
		 ON CONFLICT(address, source, subscription_id) DO NOTHING`,
		address, protocol,
	)
	if err != nil {
		log.Printf("[storage] AddProxy %s 失败: %v", address, err)
		return err
	}

	// 检查是否真的插入了
	affected, _ := result.RowsAffected()
	if affected == 0 {
		log.Printf("[storage] AddProxy %s 已忽略（节点已存在或违反约束）", address)
	}
	return nil
}

// AddProxies 批量新增
func (s *Storage) AddProxies(proxies []Proxy) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO proxies (address, protocol, source, subscription_id, region_source)
		VALUES (?, ?, 'manual', 0, 'auto')
		ON CONFLICT(address, source, subscription_id) DO NOTHING`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, p := range proxies {
		if _, err := stmt.Exec(p.Address, p.Protocol); err != nil {
			log.Printf("[storage] 插入代理 %s 失败: %v", p.Address, err)
			return err
		}
	}
	return tx.Commit()
}

// AddProxyWithSource 新增代理并指定来源和订阅ID
func (s *Storage) AddProxyWithSource(address, protocol, source string, subscriptionID ...int64) error {
	subID := int64(0)
	if len(subscriptionID) > 0 {
		subID = subscriptionID[0]
	}
	if source == SourceManual {
		subID = 0
	}
	if source == SourceSubscription && subID <= 0 {
		return fmt.Errorf("subscription proxy requires subscription_id")
	}
	_, err := s.db.Exec(
		`INSERT INTO proxies (address, protocol, source, subscription_id, region_source)
		 VALUES (?, ?, ?, ?, 'auto')
		 ON CONFLICT(address, source, subscription_id) DO UPDATE SET
			protocol = excluded.protocol,
			region_source = CASE WHEN proxies.region_source = '' THEN excluded.region_source ELSE proxies.region_source END`,
		address, protocol, source, subID,
	)
	if err != nil {
		log.Printf("[storage] AddProxyWithSource %s 失败: %v", address, err)
		return err
	}
	return nil
}
