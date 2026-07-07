package storage

import "log"

// AddProxy 新增手动节点，已存在则忽略
func (s *Storage) AddProxy(address, protocol string) error {
	result, err := s.db.Exec(
		`INSERT OR IGNORE INTO proxies (address, protocol, source, region_source) VALUES (?, ?, 'manual', 'auto')`,
		address, protocol,
	)
	if err != nil {
		log.Printf("[storage] AddProxy %s error: %v", address, err)
		return err
	}

	// 检查是否真的插入了
	affected, _ := result.RowsAffected()
	if affected == 0 {
		log.Printf("[storage] AddProxy %s ignored (already exists or constraint)", address)
	}
	return nil
}

// AddProxies 批量新增
func (s *Storage) AddProxies(proxies []Proxy) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO proxies (address, protocol, source, region_source) VALUES (?, ?, 'manual', 'auto')`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, p := range proxies {
		if _, err := stmt.Exec(p.Address, p.Protocol); err != nil {
			log.Printf("insert proxy %s error: %v", p.Address, err)
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
	result, err := s.db.Exec(
		`INSERT OR IGNORE INTO proxies (address, protocol, source, subscription_id, region_source) VALUES (?, ?, ?, ?, 'auto')`,
		address, protocol, source, subID,
	)
	if err != nil {
		log.Printf("[storage] AddProxyWithSource %s error: %v", address, err)
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		// 已存在，更新 source 和 subscription_id
		_, err = s.db.Exec(`UPDATE proxies SET source = ?, subscription_id = ? WHERE address = ?`, source, subID, address)
	}
	return err
}
