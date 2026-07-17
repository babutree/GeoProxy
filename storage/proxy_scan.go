package storage

import "database/sql"

// proxyColumns 代理表查询的标准列列表
const proxyColumns = `id, address, protocol, exit_ip, exit_location, latency, quality_grade,
	use_count, success_count, fail_count, last_used, last_check, created_at, status, user_paused, source, subscription_id,
	region, region_source, note, ipapiis_score, ipapi_flags, ipapi_flags_seen, starred, cf_blocked, dual_protocol, ai_reachability,
	proxy_username, proxy_password`

type proxyScanner interface {
	Scan(dest ...interface{}) error
}

// scanProxy 扫描代理行数据
func scanProxy(rows proxyScanner) (*Proxy, error) {
	p := &Proxy{}
	var lastUsed, lastCheck sql.NullTime
	var source, region, regionSource, note sql.NullString
	var subID sql.NullInt64
	var userPaused, ipapiFlagsSeen, starred, dualProtocol int
	if err := rows.Scan(&p.ID, &p.Address, &p.Protocol, &p.ExitIP, &p.ExitLocation,
		&p.Latency, &p.QualityGrade, &p.UseCount, &p.SuccessCount, &p.FailCount,
		&lastUsed, &lastCheck, &p.CreatedAt, &p.Status, &userPaused, &source, &subID,
		&region, &regionSource, &note, &p.IPAPIIsScore, &p.IPAPIFlags, &ipapiFlagsSeen,
		&starred, &p.CFBlocked, &dualProtocol, &p.AIReachability,
		&p.Username, &p.Password); err != nil {
		return nil, err
	}
	p.UserPaused = userPaused == 1
	p.IPAPIFlagsSeen = ipapiFlagsSeen == 1
	p.Starred = starred == 1
	p.DualProtocol = dualProtocol == 1
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
