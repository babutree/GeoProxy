package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mattn/go-sqlite3"
)

func insertAPINode(t *testing.T, store *Storage, p Proxy) int64 {
	t.Helper()
	if p.Source == "" {
		p.Source = SourceManual
	}
	if p.Status == "" {
		p.Status = "active"
	}
	if p.Protocol == "" {
		p.Protocol = "socks5"
	}
	res, err := execInsertAPINode(store.db, p)
	if err != nil {
		t.Fatalf("insertAPINode %s: %v", p.Address, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId %s: %v", p.Address, err)
	}
	return id
}

type apiNodeInserter interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

func execInsertAPINode(exec apiNodeInserter, p Proxy) (sql.Result, error) {
	userPaused := 0
	if p.UserPaused {
		userPaused = 1
	}
	starred := 0
	if p.Starred {
		starred = 1
	}
	dual := 0
	if p.DualProtocol {
		dual = 1
	}
	ipapiSeen := 0
	if p.IPAPIFlagsSeen {
		ipapiSeen = 1
	}
	return exec.Exec(
		`INSERT INTO proxies (
			address, protocol, region, region_source, note, exit_ip, exit_location,
			latency, quality_grade, use_count, success_count, fail_count, status, user_paused,
			source, subscription_id, ipapiis_score, ipapi_flags, ipapi_flags_seen, starred,
			cf_blocked, dual_protocol, ai_reachability
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Address, p.Protocol, p.Region, p.RegionSource, p.Note, p.ExitIP, p.ExitLocation,
		p.Latency, p.QualityGrade, p.UseCount, p.SuccessCount, p.FailCount, p.Status, userPaused,
		p.Source, p.SubscriptionID, p.IPAPIIsScore, p.IPAPIFlags, ipapiSeen, starred,
		p.CFBlocked, dual, p.AIReachability,
	)

}

func insertAPINodesBulk(t *testing.T, store *Storage, nodes []Proxy) {
	t.Helper()
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatalf("begin bulk insert: %v", err)
	}
	defer tx.Rollback()

	for _, p := range nodes {
		if p.Source == "" {
			p.Source = SourceManual
		}
		if p.Status == "" {
			p.Status = "active"
		}
		if p.Protocol == "" {
			p.Protocol = "socks5"
		}
		if _, err := execInsertAPINode(tx, p); err != nil {
			t.Fatalf("bulk insertAPINode %s: %v", p.Address, err)
		}
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit bulk insert: %v", err)
	}
}

func insertSequentialAPINodes(t *testing.T, store *Storage, prefix string, count int, aiReachability string) {
	t.Helper()
	nodes := make([]Proxy, 0, count)
	for i := 0; i < count; i++ {
		nodes = append(nodes, Proxy{
			Address: fmt.Sprintf("%s-%d:1", prefix, i), Protocol: "socks5", Region: "us",
			Latency: i + 1, Status: "active", IPAPIIsScore: -1, CFBlocked: -1,
			AIReachability: aiReachability,
		})
	}
	insertAPINodesBulk(t, store, nodes)
}

func insertAPINodeWithLatency(t *testing.T, store *Storage, address string, latency int, aiReachability string) {
	t.Helper()
	nodes := []Proxy{{
		Address: address, Protocol: "socks5", Region: "us",
		Latency: latency, Status: "active", IPAPIIsScore: -1, CFBlocked: -1,
		AIReachability: aiReachability,
	}}
	insertAPINodesBulk(t, store, nodes)
}

func aiJSON(m map[string]int) string {
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func idsOf(proxies []Proxy) []int64 {
	out := make([]int64, len(proxies))
	for i, p := range proxies {
		out[i] = p.ID
	}
	return out
}

func addrsOf(proxies []Proxy) []string {
	out := make([]string, len(proxies))
	for i, p := range proxies {
		out[i] = p.Address
	}
	return out
}

// TestListNodesForAPIAppliesFilters region/protocol/source/cf/ai/max_abuse 组合过滤。
func TestListNodesForAPIAppliesFilters(t *testing.T) {
	store := newTestStorage(t)
	insertTestSubscription(t, store, 1, "active")

	// 目标命中：us + socks5 + manual + cf open + ai openai&claude 可达 + abuse 0.05
	hit := insertAPINode(t, store, Proxy{
		Address: "hit:1080", Protocol: "socks5", Region: "us", Source: SourceManual,
		Latency: 50, Status: "active", FailCount: 0, IPAPIIsScore: 0.05, CFBlocked: 0,
		AIReachability: aiJSON(map[string]int{"openai": 0, "claude": 0, "grok": 1, "gemini": -1}),
	})
	// region 不匹配
	insertAPINode(t, store, Proxy{
		Address: "jp:1080", Protocol: "socks5", Region: "jp", Source: SourceManual,
		Latency: 10, Status: "active", IPAPIIsScore: 0.01, CFBlocked: 0,
		AIReachability: aiJSON(map[string]int{"openai": 0, "claude": 0}),
	})
	// protocol 不匹配
	insertAPINode(t, store, Proxy{
		Address: "http-us:8080", Protocol: "http", Region: "us", Source: SourceManual,
		Latency: 20, Status: "active", IPAPIIsScore: 0.01, CFBlocked: 0,
		AIReachability: aiJSON(map[string]int{"openai": 0, "claude": 0}),
	})
	// source 不匹配
	insertAPINode(t, store, Proxy{
		Address: "sub-us:1080", Protocol: "socks5", Region: "us", Source: SourceSubscription,
		SubscriptionID: 1, Latency: 30, Status: "active", IPAPIIsScore: 0.01, CFBlocked: 0,
		AIReachability: aiJSON(map[string]int{"openai": 0, "claude": 0}),
	})
	// cf blocked
	insertAPINode(t, store, Proxy{
		Address: "cf-block:1080", Protocol: "socks5", Region: "us", Source: SourceManual,
		Latency: 40, Status: "active", IPAPIIsScore: 0.01, CFBlocked: 1,
		AIReachability: aiJSON(map[string]int{"openai": 0, "claude": 0}),
	})
	// ai claude 不可达
	insertAPINode(t, store, Proxy{
		Address: "ai-fail:1080", Protocol: "socks5", Region: "us", Source: SourceManual,
		Latency: 45, Status: "active", IPAPIIsScore: 0.01, CFBlocked: 0,
		AIReachability: aiJSON(map[string]int{"openai": 0, "claude": 1}),
	})
	// max_abuse 超标
	insertAPINode(t, store, Proxy{
		Address: "abuse-high:1080", Protocol: "socks5", Region: "us", Source: SourceManual,
		Latency: 55, Status: "active", IPAPIIsScore: 0.5, CFBlocked: 0,
		AIReachability: aiJSON(map[string]int{"openai": 0, "claude": 0}),
	})
	// max_abuse 未探测 (-1) 应被排除
	insertAPINode(t, store, Proxy{
		Address: "abuse-unknown:1080", Protocol: "socks5", Region: "us", Source: SourceManual,
		Latency: 60, Status: "active", IPAPIIsScore: -1, CFBlocked: 0,
		AIReachability: aiJSON(map[string]int{"openai": 0, "claude": 0}),
	})

	maxAbuse := 0.1
	nodes, total, err := store.ListNodesForAPI(NodeAPIFilter{
		Region:   "us",
		Protocol: "socks5",
		Source:   SourceManual,
		CF:       "open",
		AI:       []string{"openai", "claude"},
		MaxAbuse: &maxAbuse,
	})
	if err != nil {
		t.Fatalf("ListNodesForAPI() error = %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1; nodes=%v", total, addrsOf(nodes))
	}
	if len(nodes) != 1 || nodes[0].ID != hit {
		t.Fatalf("nodes = %v ids=%v, want only hit id=%d", addrsOf(nodes), idsOf(nodes), hit)
	}

	// cf=blocked 应只命中 cf-block
	blocked, btotal, err := store.ListNodesForAPI(NodeAPIFilter{
		Region: "us", Protocol: "socks5", Source: SourceManual, CF: "blocked",
	})
	if err != nil {
		t.Fatalf("ListNodesForAPI(cf=blocked) error = %v", err)
	}
	if btotal != 1 || len(blocked) != 1 || blocked[0].Address != "cf-block:1080" {
		t.Fatalf("cf=blocked nodes=%v total=%d, want cf-block:1080", addrsOf(blocked), btotal)
	}
}

// TestListNodesForAPIStatusDefaultExcludesUnusable 默认 status 排除不可用；status=all 含全部。
func TestListNodesForAPIStatusDefaultExcludesUnusable(t *testing.T) {
	store := newTestStorage(t)
	insertTestSubscription(t, store, 1, "paused")

	okActive := insertAPINode(t, store, Proxy{
		Address: "ok-active:1", Protocol: "socks5", Region: "us", Latency: 10,
		Status: "active", FailCount: 0, UserPaused: false, IPAPIIsScore: -1, CFBlocked: -1,
	})
	okDegraded := insertAPINode(t, store, Proxy{
		Address: "ok-degraded:1", Protocol: "socks5", Region: "us", Latency: 20,
		Status: "degraded", FailCount: 2, UserPaused: false, IPAPIIsScore: -1, CFBlocked: -1,
	})
	insertAPINode(t, store, Proxy{
		Address: "paused:1", Protocol: "socks5", Region: "us", Latency: 5,
		Status: "active", FailCount: 0, UserPaused: true, IPAPIIsScore: -1, CFBlocked: -1,
	})
	insertAPINode(t, store, Proxy{
		Address: "failing:1", Protocol: "socks5", Region: "us", Latency: 6,
		Status: "active", FailCount: 3, UserPaused: false, IPAPIIsScore: -1, CFBlocked: -1,
	})
	insertAPINode(t, store, Proxy{
		Address: "disabled:1", Protocol: "socks5", Region: "us", Latency: 7,
		Status: "disabled", FailCount: 0, UserPaused: false, IPAPIIsScore: -1, CFBlocked: -1,
	})
	insertAPINode(t, store, Proxy{
		Address: "sub-paused:1", Protocol: "socks5", Region: "us", Latency: 8,
		Status: "active", FailCount: 0, UserPaused: false, Source: SourceSubscription, SubscriptionID: 1,
		IPAPIIsScore: -1, CFBlocked: -1,
	})

	// 默认 status：仅可用
	nodes, total, err := store.ListNodesForAPI(NodeAPIFilter{})
	if err != nil {
		t.Fatalf("ListNodesForAPI(default) error = %v", err)
	}
	if total != 2 {
		t.Fatalf("default total = %d, want 2; nodes=%v", total, addrsOf(nodes))
	}
	got := idsOf(nodes)
	if len(got) != 2 || got[0] != okActive || got[1] != okDegraded {
		t.Fatalf("default nodes ids=%v, want [%d %d] ordered by latency", got, okActive, okDegraded)
	}

	// status=all：含全部 6 条，包括父订阅暂停的节点
	all, allTotal, err := store.ListNodesForAPI(NodeAPIFilter{Status: "all"})
	if err != nil {
		t.Fatalf("ListNodesForAPI(all) error = %v", err)
	}
	if allTotal != 6 || len(all) != 6 {
		t.Fatalf("status=all total=%d len=%d nodes=%v, want 6", allTotal, len(all), addrsOf(all))
	}
}

// TestListNodesForAPIPaginationStable limit/offset 稳定序 latency ASC, id ASC。
func TestListNodesForAPIPaginationStable(t *testing.T) {
	store := newTestStorage(t)

	// 相同 latency 时按 id 升序
	idA := insertAPINode(t, store, Proxy{Address: "a:1", Protocol: "socks5", Region: "us", Latency: 100, Status: "active", IPAPIIsScore: -1, CFBlocked: -1})
	idB := insertAPINode(t, store, Proxy{Address: "b:1", Protocol: "socks5", Region: "us", Latency: 50, Status: "active", IPAPIIsScore: -1, CFBlocked: -1})
	idC := insertAPINode(t, store, Proxy{Address: "c:1", Protocol: "socks5", Region: "us", Latency: 100, Status: "active", IPAPIIsScore: -1, CFBlocked: -1})
	idD := insertAPINode(t, store, Proxy{Address: "d:1", Protocol: "socks5", Region: "us", Latency: 50, Status: "active", IPAPIIsScore: -1, CFBlocked: -1})
	// 期望全序：B, D (latency 50 by id), A, C (latency 100 by id)
	wantOrder := []int64{idB, idD, idA, idC}

	page1, total, err := store.ListNodesForAPI(NodeAPIFilter{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("page1 error = %v", err)
	}
	if total != 4 {
		t.Fatalf("total = %d, want 4", total)
	}
	if got := idsOf(page1); len(got) != 2 || got[0] != wantOrder[0] || got[1] != wantOrder[1] {
		t.Fatalf("page1 ids=%v, want %v", got, wantOrder[:2])
	}

	page2, total2, err := store.ListNodesForAPI(NodeAPIFilter{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("page2 error = %v", err)
	}
	if total2 != 4 {
		t.Fatalf("total2 = %d, want 4", total2)
	}
	if got := idsOf(page2); len(got) != 2 || got[0] != wantOrder[2] || got[1] != wantOrder[3] {
		t.Fatalf("page2 ids=%v, want %v", got, wantOrder[2:])
	}

	// 越界 offset
	empty, total3, err := store.ListNodesForAPI(NodeAPIFilter{Limit: 2, Offset: 10})
	if err != nil {
		t.Fatalf("offset overflow error = %v", err)
	}
	if total3 != 4 || len(empty) != 0 {
		t.Fatalf("offset overflow total=%d len=%d, want 4/0", total3, len(empty))
	}
}

func TestListNodesForAPISharesSnapshotBetweenCountAndPage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "proxy.db")
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer store.Close()

	if _, err := store.db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		t.Fatalf("enable WAL: %v", err)
	}
	originalID := insertAPINode(t, store, Proxy{
		Address: "original:1", Protocol: "socks5", Region: "us", Latency: 100,
		Status: "active", IPAPIIsScore: -1, CFBlocked: -1,
	})
	if _, err := store.db.Exec(`ALTER TABLE proxies RENAME TO proxy_rows`); err != nil {
		t.Fatalf("rename proxies: %v", err)
	}
	if _, err := store.db.Exec(`CREATE VIEW proxies AS
		SELECT * FROM proxy_rows WHERE node_api_snapshot_hook() = 0`); err != nil {
		t.Fatalf("create proxies view: %v", err)
	}

	readStarted := make(chan struct{})
	writerDone := make(chan struct{})
	var hookOnce sync.Once
	conn, err := store.db.Conn(context.Background())
	if err != nil {
		t.Fatalf("reserve storage connection: %v", err)
	}
	err = conn.Raw(func(driverConn any) error {
		return driverConn.(*sqlite3.SQLiteConn).RegisterFunc("node_api_snapshot_hook", func() int {
			hookOnce.Do(func() {
				close(readStarted)
				<-writerDone
			})
			return 0
		}, false)
	})
	if closeErr := conn.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("register snapshot hook: %v", err)
	}

	writerErr := make(chan error, 1)
	go func() {
		<-readStarted
		db, err := sql.Open("sqlite3", dbPath)
		if err == nil {
			_, err = db.Exec(`INSERT INTO proxy_rows (address, protocol, region, latency) VALUES ('inserted:1', 'socks5', 'us', 1)`)
			closeErr := db.Close()
			if err == nil {
				err = closeErr
			}
		}
		writerErr <- err
		close(writerDone)
	}()

	nodes, total, err := store.ListNodesForAPI(NodeAPIFilter{Limit: 1})
	if err != nil {
		t.Fatalf("ListNodesForAPI() error = %v", err)
	}
	if err := <-writerErr; err != nil {
		t.Fatalf("concurrent insert: %v", err)
	}
	if total != 1 || len(nodes) != 1 || nodes[0].ID != originalID {
		t.Fatalf("snapshot nodes=%v total=%d, want original id=%d and total=1", idsOf(nodes), total, originalID)
	}
}

// TestListNodesForAPILimitDefaultsAndCaps limit 默认 500、上限 2000；offset 默认 0。
func TestListNodesForAPILimitDefaultsAndCaps(t *testing.T) {
	store := newTestStorage(t)

	const n = 2505
	insertSequentialAPINodes(t, store, "n", n, "")

	// Limit<=0 -> 默认 500，total 仍为分页前过滤总数。
	nodes, total, err := store.ListNodesForAPI(NodeAPIFilter{Limit: 0})
	if err != nil {
		t.Fatalf("default limit error = %v", err)
	}
	if total != n || len(nodes) != nodeAPIDefaultLimit {
		t.Fatalf("default limit total=%d len=%d, want %d/%d", total, len(nodes), n, nodeAPIDefaultLimit)
	}

	// Limit > 2000 -> 截到 2000；此断言防止小 fixture 放过无 SQL cap 的实现。
	nodes2, total2, err := store.ListNodesForAPI(NodeAPIFilter{Limit: 5000})
	if err != nil {
		t.Fatalf("cap limit error = %v", err)
	}
	if total2 != n || len(nodes2) != nodeAPIMaxLimit {
		t.Fatalf("cap limit total=%d len=%d, want %d/%d", total2, len(nodes2), n, nodeAPIMaxLimit)
	}
	if nodes2[0].Latency != 1 || nodes2[len(nodes2)-1].Latency != nodeAPIMaxLimit {
		t.Fatalf("cap limit range latency=%d..%d, want 1..%d", nodes2[0].Latency, nodes2[len(nodes2)-1].Latency, nodeAPIMaxLimit)
	}

	pageAfterCap, totalAfterCap, err := store.ListNodesForAPI(NodeAPIFilter{Limit: 5000, Offset: nodeAPIMaxLimit})
	if err != nil {
		t.Fatalf("cap limit with offset error = %v", err)
	}
	if totalAfterCap != n || len(pageAfterCap) != n-nodeAPIMaxLimit {
		t.Fatalf("cap offset total=%d len=%d, want %d/%d", totalAfterCap, len(pageAfterCap), n, n-nodeAPIMaxLimit)
	}
	if pageAfterCap[0].Latency != nodeAPIMaxLimit+1 {
		t.Fatalf("cap offset first latency=%d, want %d", pageAfterCap[0].Latency, nodeAPIMaxLimit+1)
	}

	// 负 offset 按 0
	nodes3, _, err := store.ListNodesForAPI(NodeAPIFilter{Limit: 5, Offset: -3})
	if err != nil {
		t.Fatalf("neg offset error = %v", err)
	}
	if len(nodes3) != 5 {
		t.Fatalf("neg offset len=%d, want 5", len(nodes3))
	}
	// 应是 latency 最小的前 5 个
	for i := 0; i < 5; i++ {
		if nodes3[i].Latency != i+1 {
			t.Fatalf("nodes3[%d].Latency=%d, want %d", i, nodes3[i].Latency, i+1)
		}
	}
}

// TestListNodesForAPIAIPaginatesAfterFilteringAcrossBatches protects the AI path from
// filtering only the first SQL page: the sole reachable row appears after more than one
// internal batch of non-matching candidates.
func TestListNodesForAPIAIPaginatesAfterFilteringAcrossBatches(t *testing.T) {
	store := newTestStorage(t)

	insertSequentialAPINodes(t, store, "ai-miss", nodeAPIMaxLimit+5, aiJSON(map[string]int{"openai": 1}))
	insertAPINodeWithLatency(t, store, "ai-hit-late:1", nodeAPIMaxLimit+6, aiJSON(map[string]int{"openai": 0}))

	nodes, total, err := store.ListNodesForAPI(NodeAPIFilter{AI: []string{"openai"}, Limit: 1})
	if err != nil {
		t.Fatalf("ListNodesForAPI(ai) error = %v", err)
	}
	if total != 1 || len(nodes) != 1 || nodes[0].Address != "ai-hit-late:1" {
		t.Fatalf("ai late hit nodes=%v total=%d, want only ai-hit-late:1", addrsOf(nodes), total)
	}
}

// TestListNodesForAPIMaxAbuseExcludesUnprobed 显式 max_abuse 时 -1 未探测不通过。
func TestListNodesForAPIMaxAbuseExcludesUnprobed(t *testing.T) {
	store := newTestStorage(t)

	insertAPINode(t, store, Proxy{
		Address: "probed-ok:1", Protocol: "socks5", Region: "us", Latency: 10,
		Status: "active", IPAPIIsScore: 0.0, CFBlocked: -1,
	})
	insertAPINode(t, store, Proxy{
		Address: "unprobed:1", Protocol: "socks5", Region: "us", Latency: 5,
		Status: "active", IPAPIIsScore: -1, CFBlocked: -1,
	})
	insertAPINode(t, store, Proxy{
		Address: "probed-high:1", Protocol: "socks5", Region: "us", Latency: 15,
		Status: "active", IPAPIIsScore: 0.9, CFBlocked: -1,
	})

	maxAbuse := 0.2
	nodes, total, err := store.ListNodesForAPI(NodeAPIFilter{MaxAbuse: &maxAbuse})
	if err != nil {
		t.Fatalf("ListNodesForAPI error = %v", err)
	}
	if total != 1 || len(nodes) != 1 || nodes[0].Address != "probed-ok:1" {
		t.Fatalf("max_abuse nodes=%v total=%d, want only probed-ok:1", addrsOf(nodes), total)
	}

	// 无 max_abuse 时 -1 与高分都纳入（仅默认 status）
	all, allTotal, err := store.ListNodesForAPI(NodeAPIFilter{})
	if err != nil {
		t.Fatalf("no max_abuse error = %v", err)
	}
	if allTotal != 3 || len(all) != 3 {
		t.Fatalf("no max_abuse total=%d len=%d, want 3", allTotal, len(all))
	}
}

// TestListNodesForAPIAIRequiresAllReachable ai 多值 AND：全部为 0(可达) 才通过；缺字段/未探测/不可达均失败。
func TestListNodesForAPIAIRequiresAllReachable(t *testing.T) {
	store := newTestStorage(t)

	insertAPINode(t, store, Proxy{
		Address: "all-ok:1", Protocol: "socks5", Region: "us", Latency: 10, Status: "active",
		IPAPIIsScore: -1, CFBlocked: -1,
		AIReachability: aiJSON(map[string]int{"openai": 0, "claude": 0, "grok": 0, "gemini": 0}),
	})
	insertAPINode(t, store, Proxy{
		Address: "claude-bad:1", Protocol: "socks5", Region: "us", Latency: 20, Status: "active",
		IPAPIIsScore: -1, CFBlocked: -1,
		AIReachability: aiJSON(map[string]int{"openai": 0, "claude": 1, "grok": 0, "gemini": 0}),
	})
	insertAPINode(t, store, Proxy{
		Address: "claude-unknown:1", Protocol: "socks5", Region: "us", Latency: 30, Status: "active",
		IPAPIIsScore: -1, CFBlocked: -1,
		AIReachability: aiJSON(map[string]int{"openai": 0, "claude": -1}),
	})
	insertAPINode(t, store, Proxy{
		Address: "missing-claude:1", Protocol: "socks5", Region: "us", Latency: 40, Status: "active",
		IPAPIIsScore: -1, CFBlocked: -1,
		AIReachability: aiJSON(map[string]int{"openai": 0}),
	})
	insertAPINode(t, store, Proxy{
		Address: "empty-ai:1", Protocol: "socks5", Region: "us", Latency: 50, Status: "active",
		IPAPIIsScore: -1, CFBlocked: -1, AIReachability: "",
	})
	insertAPINode(t, store, Proxy{
		Address: "bad-json:1", Protocol: "socks5", Region: "us", Latency: 60, Status: "active",
		IPAPIIsScore: -1, CFBlocked: -1, AIReachability: "{not-json",
	})

	nodes, total, err := store.ListNodesForAPI(NodeAPIFilter{AI: []string{"openai", "claude"}})
	if err != nil {
		t.Fatalf("ListNodesForAPI error = %v", err)
	}
	if total != 1 || len(nodes) != 1 || nodes[0].Address != "all-ok:1" {
		t.Fatalf("ai filter nodes=%v total=%d, want only all-ok:1", addrsOf(nodes), total)
	}
}
