package storage

import "database/sql"

// proxyColumns 代理表查询的标准列列表
const proxyColumns = `id, address, protocol, exit_ip, exit_location, latency, quality_grade,
	use_count, success_count, fail_count, last_used, last_check, created_at, status, source, subscription_id,
	region, region_source, note`

type proxyScanner interface {
	Scan(dest ...interface{}) error
}

// scanProxy 扫描代理行数据
func scanProxy(rows proxyScanner) (*Proxy, error) {
	p := &Proxy{}
	var lastUsed, lastCheck sql.NullTime
	var source, region, regionSource, note sql.NullString
	var subID sql.NullInt64
	if err := rows.Scan(&p.ID, &p.Address, &p.Protocol, &p.ExitIP, &p.ExitLocation,
		&p.Latency, &p.QualityGrade, &p.UseCount, &p.SuccessCount, &p.FailCount,
		&lastUsed, &lastCheck, &p.CreatedAt, &p.Status, &source, &subID,
		&region, &regionSource, &note); err != nil {
		return nil, err
	}
	if lastUsed.Valid {
		p.LastUsed = lastUsed.Time
	}
	if lastCheck.Valid {
		p.LastCheck = lastCheck.Time
	}
	if source.Valid {
		p.Source = source.String
	} else {
		p.Source = SourceManual
	}
	if subID.Valid {
		p.SubscriptionID = subID.Int64
	}
	if region.Valid {
		p.Region = region.String
	}
	if regionSource.Valid {
		p.RegionSource = regionSource.String
	}
	if note.Valid {
		p.Note = note.String
	}
	return p, nil
}
