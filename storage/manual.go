package storage

import (
	"database/sql"
	"fmt"
)

func (s *Storage) AddManualProxy(address, protocol, region, note string) error {
	return s.addManualProxyExec(s.db, address, protocol, region, note, "", "", "")
}

// AddManualProxyWithCredentials 与 AddManualProxy 相同，但持久化上游认证凭据。
// 凭据用于拨号/验证时注入出站握手；绝不写入日志或错误串。空串表示无需认证。
func (s *Storage) AddManualProxyWithCredentials(address, protocol, region, note, username, password string) error {
	return s.addManualProxyExec(s.db, address, protocol, region, note, username, password, "")
}

// AddManualProxyWithNodeKey 手工入库并可写入稳定 node_key（隧道手工节点用 ParsedNode.NodeKey）。
func (s *Storage) AddManualProxyWithNodeKey(address, protocol, region, note, nodeKey string) error {
	if nodeKey == "" {
		return s.addManualProxyExec(s.db, address, protocol, region, note, "", "", "")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var count int
	var proxyID int64
	var storedAddress string
	var storedProtocol string
	normalizedProtocol := normalizeProtocol(protocol)
	if err := tx.QueryRow(
		`SELECT COUNT(*), COALESCE(MIN(id), 0), COALESCE(MIN(address), ''), COALESCE(MIN(protocol), '')
		   FROM proxies
		  WHERE source = 'manual' AND subscription_id = 0 AND node_key = ?`,
		nodeKey,
	).Scan(&count, &proxyID, &storedAddress, &storedProtocol); err != nil {
		return err
	}
	if count > 1 {
		return fmt.Errorf("manual proxy node_key %q is ambiguous (at least 2 rows)", nodeKey)
	}
	if count == 0 {
		if err := s.addManualProxyExec(tx, address, protocol, region, note, "", "", nodeKey); err != nil {
			return err
		}
		return tx.Commit()
	}

	region = normalizeManualRegion(region)
	regionSource := "auto"
	if region != "" {
		regionSource = "manual"
	}
	if storedAddress == address && storedProtocol == normalizedProtocol {
		res, err := tx.Exec(
			`UPDATE proxies SET region = ?, region_source = ?, note = ? WHERE id = ? AND node_key = ?`,
			region, regionSource, note, proxyID, nodeKey,
		)
		if err != nil {
			return err
		}
		if err := requireRowsAffected(res.RowsAffected()); err != nil {
			return err
		}
		return tx.Commit()
	}

	// 地址或协议改变意味着旧探测证据对应的是另一条路由，必须回到待验证状态。
	res, err := tx.Exec(
		`UPDATE proxies
		    SET address = ?,
		        protocol = ?,
		        region = ?,
		        region_source = ?,
		        note = ?,
		        exit_ip = '',
		        exit_location = '',
		        latency = 0,
		        quality_grade = 'C',
		        fail_count = 0,
		        last_check = NULL,
		        exit_checked_at = NULL,
		        disabled_at = NULL,
		        ipapiis_score = -1,
		        ipapi_flags = '',
		        ipapi_flags_seen = 0,
		        cf_blocked = -1,
		        ai_reachability = '',
		        status = 'disabled',
		        proxy_username = '',
		        proxy_password = '',
		        node_key = ?
		  WHERE id = ? AND node_key = ?`,
		address, normalizedProtocol, region, regionSource, note, nodeKey, proxyID, nodeKey,
	)
	if err != nil {
		return err
	}
	if err := requireRowsAffected(res.RowsAffected()); err != nil {
		return err
	}
	return tx.Commit()
}

type proxyExec interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

func (s *Storage) addManualProxyExec(exec proxyExec, address, protocol, region, note, username, password, nodeKey string) error {
	region = normalizeManualRegion(region)
	regionSource := "auto"
	if region != "" {
		regionSource = "manual"
	}
	// 手工节点默认 disabled：须经连通/出口/纯净度/AI/CF 验证通过后才 active 入选路。
	// proxy_username/proxy_password 持久化上游认证凭据（拨号时注入，绝不入日志）。
	// node_key：调用方传入隧道稳定键；空则用 protocol|address 派生（直连场景）。
	if nodeKey == "" {
		// 仅用 DSL 安全字符（无 |），避免复制 -node-key- 时被字符集拒绝。
		nodeKey = "manual:" + normalizeProtocol(protocol) + ":" + address
	}
	_, err := exec.Exec(
		`INSERT INTO proxies (address, protocol, source, subscription_id, region, region_source, note, status, proxy_username, proxy_password, node_key)
		 VALUES (?, ?, 'manual', 0, ?, ?, ?, 'disabled', ?, ?, ?)
		 ON CONFLICT(address, source, subscription_id) DO UPDATE SET
			protocol = excluded.protocol,
			region = excluded.region,
			region_source = excluded.region_source,
			note = excluded.note,
			status = 'disabled',
			proxy_username = excluded.proxy_username,
			proxy_password = excluded.proxy_password,
			node_key = excluded.node_key`,
		address, normalizeProtocol(protocol), region, regionSource, note, username, password, nodeKey,
	)
	return err
}

func (s *Storage) AddManualProxies(proxies []Proxy, region, note string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, proxy := range proxies {
		if err := s.addManualProxyExec(tx, proxy.Address, proxy.Protocol, region, note, proxy.Username, proxy.Password, proxy.NodeKey); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Storage) UpdateProxyRegion(address, region string, manual bool) error {
	regionSource := "auto"
	if manual {
		regionSource = "manual"
	}
	res, err := s.db.Exec(
		`UPDATE proxies SET region = ?, region_source = ? WHERE `+uniqueAddressProxyIDWhere,
		normalizeManualRegion(region), regionSource, address,
	)
	return s.finishAddressOnlyMutation(address, res, err)
}

func (s *Storage) UpdateProxyRegionByID(id int64, region string, manual bool) error {
	regionSource := "auto"
	if manual {
		regionSource = "manual"
	}
	res, err := s.db.Exec(
		`UPDATE proxies SET region = ?, region_source = ? WHERE id = ?`,
		normalizeManualRegion(region), regionSource, id,
	)
	if err != nil {
		return err
	}
	return requireRowsAffected(res.RowsAffected())
}

func (s *Storage) UpdateProxyNote(address, note string) error {
	res, err := s.db.Exec(`UPDATE proxies SET note = ? WHERE `+uniqueAddressProxyIDWhere, note, address)
	return s.finishAddressOnlyMutation(address, res, err)
}

func (s *Storage) UpdateProxyNoteByID(id int64, note string) error {
	res, err := s.db.Exec(`UPDATE proxies SET note = ? WHERE id = ?`, note, id)
	if err != nil {
		return err
	}
	return requireRowsAffected(res.RowsAffected())
}

func (s *Storage) DeleteManualProxy(address string) error {
	res, err := s.db.Exec(`DELETE FROM proxies WHERE address = ? AND source = 'manual'`, address)
	if err != nil {
		return err
	}
	return requireRowsAffected(res.RowsAffected())
}
