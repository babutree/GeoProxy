package storage

import (
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// TestSubscriptionHeadersDefaultEmpty 新订阅默认 headers 为空字符串（向后兼容）。
func TestSubscriptionHeadersDefaultEmpty(t *testing.T) {
	store := newTestStorage(t)
	// headers 列在 subscriptions 表（assertColumnExists 只查 proxies 表，此处直接查订阅表）。
	var colCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name = 'headers'`).Scan(&colCount); err != nil {
		t.Fatalf("query subscriptions.headers column: %v", err)
	}
	if colCount != 1 {
		t.Fatalf("subscriptions.headers column count = %d, want 1", colCount)
	}

	id, err := store.AddSubscription("h-default", "https://example.test/h-default.yaml", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	sub, err := store.GetSubscription(id)
	if err != nil {
		t.Fatalf("GetSubscription() error = %v", err)
	}
	if sub.Headers != "" {
		t.Fatalf("default headers = %q, want empty", sub.Headers)
	}
}

// TestSubscriptionHeadersRoundTrip AddSubscription 传入 headers JSON 后能经 GetSubscription 读回。
func TestSubscriptionHeadersRoundTrip(t *testing.T) {
	store := newTestStorage(t)
	headersJSON := `{"User-Agent":"clash.meta","Authorization":"Bearer xxx"}`
	id, err := store.AddSubscription("h-roundtrip", "https://example.test/h-roundtrip.yaml", "", "auto", 60, headersJSON)
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	sub, err := store.GetSubscription(id)
	if err != nil {
		t.Fatalf("GetSubscription() error = %v", err)
	}
	if sub.Headers != headersJSON {
		t.Fatalf("headers round-trip = %q, want %q", sub.Headers, headersJSON)
	}
}

// TestUpdateSubscriptionPersistsHeaders UpdateSubscription 写入的 headers 能被读回。
func TestUpdateSubscriptionPersistsHeaders(t *testing.T) {
	store := newTestStorage(t)
	id, err := store.AddSubscription("h-update", "https://example.test/h-update.yaml", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	newHeaders := `{"User-Agent":"v2rayNG"}`
	if err := store.UpdateSubscription(id, "h-update", "https://example.test/h-update.yaml", "", "auto", 60, newHeaders); err != nil {
		t.Fatalf("UpdateSubscription() error = %v", err)
	}
	sub, err := store.GetSubscription(id)
	if err != nil {
		t.Fatalf("GetSubscription() error = %v", err)
	}
	if sub.Headers != newHeaders {
		t.Fatalf("headers after update = %q, want %q", sub.Headers, newHeaders)
	}
}

// TestSubscriptionColumnsMatchScanSubscription 弥补 subColumns 与 scanSubscription 的
// 列数/顺序对齐缺口（参考 TestProxyColumnsMatchScanProxy 的写法）：用真实 SELECT subColumns
// 后交给 scanSubscription 扫描——若列数或顺序不一致，rows.Scan 会报错，从而捕获两处失步。
func TestSubscriptionColumnsMatchScanSubscription(t *testing.T) {
	store := newTestStorage(t)
	headersJSON := `{"User-Agent":"clash"}`
	id, err := store.AddSubscription("cols-sync", "https://example.test/cols-sync.yaml", "", "auto", 77, headersJSON)
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}

	rows, err := store.db.Query(`SELECT `+subColumns+` FROM subscriptions WHERE id = ?`, id)
	if err != nil {
		t.Fatalf("query subColumns: %v", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("rows.Columns(): %v", err)
	}
	// 当前模型：id, name, url, file_path, format, refresh_min, last_fetch, last_success,
	// status, proxy_count, created_at, contributed, headers = 13 列。硬编码断言，防止任一侧漏改。
	if len(cols) != 13 {
		t.Fatalf("subColumns yields %d columns, want 13 (…, contributed, headers)", len(cols))
	}
	// 最后一列必须是 scanSubscription 末尾新增的 headers 字段。
	if cols[len(cols)-1] != "headers" {
		t.Fatalf("last SELECT column = %q, want headers", cols[len(cols)-1])
	}

	if !rows.Next() {
		t.Fatal("expected one row")
	}
	// 核心断言：scanSubscription 能无错扫描全部列——列数/顺序不一致会导致 Scan 报错。
	sub, err := scanSubscription(rows)
	if err != nil {
		t.Fatalf("scanSubscription failed (subColumns/scanSubscription out of sync?): %v", err)
	}
	if sub.Name != "cols-sync" || sub.RefreshMin != 77 || sub.Headers != headersJSON {
		t.Fatalf("scanned subscription mismatch: %#v", sub)
	}
	if rows.Err() != nil {
		t.Fatalf("rows error: %v", rows.Err())
	}
}

// TestSubColumnsIncludesHeaders 静态断言 subColumns 常量包含 headers 列。
func TestSubColumnsIncludesHeaders(t *testing.T) {
	if !strings.Contains(subColumns, "headers") {
		t.Fatal("subColumns constant missing headers")
	}
}
