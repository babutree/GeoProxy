package storage

import (
	"database/sql"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
)

var alpha2RegionPattern = regexp.MustCompile(`^[A-Za-z]{2}$`)

type RegionCount struct {
	Region string `json:"region"`
	Count  int    `json:"count"`
}

func (s *Storage) GetByRegion(region string, excludes []string) ([]Proxy, error) {
	query := `SELECT ` + proxyColumns + `
		 FROM proxies
		 WHERE status IN ('active', 'degraded') AND fail_count < 3`
	args := []interface{}{}
	if normalized := normalizeRegion(region); normalized != "" {
		query += ` AND region = ?`
		args = append(args, normalized)
	}
	excludeMap := makeExcludeMap(excludes)
	query += ` ORDER BY CASE WHEN latency <= 0 THEN 1 ELSE 0 END, latency ASC, RANDOM()`
	return s.queryProxies(query, args, excludeMap)
}

func (s *Storage) GetRandomByRegion(region string, excludes []string) (*Proxy, error) {
	proxies, err := s.GetByRegion(region, excludes)
	if err != nil {
		return nil, err
	}
	if len(proxies) == 0 {
		return nil, fmt.Errorf("no available proxy for region: %s", normalizeRegion(region))
	}
	proxy := proxies[rand.Intn(len(proxies))]
	return &proxy, nil
}

func (s *Storage) CountByRegion() (map[string]int, error) {
	rows, err := s.db.Query(`
		SELECT region, COUNT(*)
		FROM proxies
		WHERE status IN ('active', 'degraded') AND fail_count < 3 AND region != ''
		GROUP BY region`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var region string
		var count int
		if err := rows.Scan(&region, &count); err != nil {
			return nil, err
		}
		counts[region] = count
	}
	return counts, rows.Err()
}

func (s *Storage) GetRegionsWithCount() ([]RegionCount, error) {
	rows, err := s.db.Query(`
		SELECT region, COUNT(*)
		FROM proxies
		WHERE status IN ('active', 'degraded') AND fail_count < 3 AND region != ''
		GROUP BY region
		ORDER BY region ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	regions := []RegionCount{}
	for rows.Next() {
		var item RegionCount
		if err := rows.Scan(&item.Region, &item.Count); err != nil {
			return nil, err
		}
		regions = append(regions, item)
	}
	return regions, rows.Err()
}

func (s *Storage) queryProxies(query string, args []interface{}, excludes map[string]bool) ([]Proxy, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	proxies := []Proxy{}
	for rows.Next() {
		p, err := scanProxy(rows)
		if err != nil {
			return nil, err
		}
		if !excludes[p.Address] {
			proxies = append(proxies, *p)
		}
	}
	return proxies, rows.Err()
}

func makeExcludeMap(excludes []string) map[string]bool {
	excludeMap := make(map[string]bool, len(excludes))
	for _, address := range excludes {
		excludeMap[address] = true
	}
	return excludeMap
}

func (s *Storage) GetProxyByAddress(address string) (*Proxy, error) {
	row := s.db.QueryRow(`SELECT `+proxyColumns+` FROM proxies WHERE address = ?`, address)
	proxy, err := scanProxy(row)
	if err == nil {
		return proxy, nil
	}
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("proxy %s not found", address)
	}
	return nil, err
}

func normalizeProtocol(protocol string) string {
	return strings.ToLower(strings.TrimSpace(protocol))
}

func regionFromExitLocation(exitLocation string) string {
	fields := strings.Fields(exitLocation)
	if len(fields) == 0 || !alpha2RegionPattern.MatchString(fields[0]) {
		return ""
	}
	return strings.ToLower(fields[0])
}
