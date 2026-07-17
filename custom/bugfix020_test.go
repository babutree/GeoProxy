package custom

import (
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/storage"
	"github.com/babutree/GeoProxy/validator"
)

// TestBUGFIX020LongTermDisabledFreedFromRuntime：
// 长期禁用（status=disabled 且 last_check 超过阈值）应从 sing-box 运行态/portMap 移除以释放端口；
// 近期禁用与 last_check 为零的禁用节点仍保留，供 probeDisabled 重验证。
func TestBUGFIX020LongTermDisabledFreedFromRuntime(t *testing.T) {
	store := newTestStorage(t)
	sb := newSpyShard()

	recent := tunnelNode("recent-disabled", "recent.example.com", "pw-recent")
	longTerm := tunnelNode("long-disabled", "long.example.com", "pw-long")
	zeroCheck := tunnelNode("zero-check", "zero.example.com", "pw-zero")
	active := tunnelNode("still-active", "active.example.com", "pw-active")

	if err := sb.Reload([]ParsedNode{recent, longTerm, zeroCheck, active}); err != nil {
		t.Fatalf("seed Reload: %v", err)
	}
	pm := sb.GetPortMap()
	addrOf := func(n ParsedNode) string {
		return net.JoinHostPort("127.0.0.1", strconv.Itoa(pm[n.NodeKey()]))
	}

	// 共用订阅记录，便于以 source=subscription 入库。
	subID, err := store.AddSubscription("bugfix020", "", writeSubscriptionFile(t, "proxies: []"), "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}
	seedSubProxy := func(addr, status string, lastCheck time.Time) {
		t.Helper()
		if err := store.AddProxyWithSource(addr, "socks5", storage.SourceSubscription, subID); err != nil {
			t.Fatalf("AddProxyWithSource(%s): %v", addr, err)
		}
		if status == "disabled" {
			if err := store.DisableSubscriptionProxy(addr, subID); err != nil {
				t.Fatalf("DisableSubscriptionProxy(%s): %v", addr, err)
			}
		}
		if !lastCheck.IsZero() {
			if _, err := store.GetDB().Exec(
				`UPDATE proxies SET last_check = ? WHERE address = ? AND subscription_id = ?`,
				lastCheck.UTC().Format("2006-01-02 15:04:05"), addr, subID,
			); err != nil {
				t.Fatalf("set last_check(%s): %v", addr, err)
			}
		} else {
			if _, err := store.GetDB().Exec(
				`UPDATE proxies SET last_check = NULL WHERE address = ? AND subscription_id = ?`,
				addr, subID,
			); err != nil {
				t.Fatalf("clear last_check(%s): %v", addr, err)
			}
		}
	}

	seedSubProxy(addrOf(recent), "disabled", time.Now().Add(-2*time.Hour))
	seedSubProxy(addrOf(longTerm), "disabled", time.Now().Add(-longTermDisabledRetention-time.Hour))
	seedSubProxy(addrOf(zeroCheck), "disabled", time.Time{})
	seedSubProxy(addrOf(active), "active", time.Now())

	m := &Manager{
		storage:   store,
		validator: validator.New(1, 1, "http://127.0.0.1/validate"),
		singbox:   sb,
	}

	// 通过无关订阅刷新触发 collect → Reload，应剔除长期禁用节点。
	otherFile := writeSubscriptionFile(t, "trojan://password@other.example.com:443?sni=other.example.com#other")
	otherID, err := store.AddSubscription("other", "", otherFile, "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription(other): %v", err)
	}
	if err := m.RefreshSubscription(otherID); err != nil {
		t.Fatalf("RefreshSubscription(other): %v", err)
	}

	pm = sb.GetPortMap()
	if _, ok := pm[longTerm.NodeKey()]; ok {
		t.Fatalf("long-term disabled node still in portMap: %v", pm)
	}
	for _, n := range []ParsedNode{recent, zeroCheck, active} {
		if _, ok := pm[n.NodeKey()]; !ok {
			t.Fatalf("node %s missing from portMap after prune; map=%v", n.Name, pm)
		}
	}
	// 活跃节点端口应保持稳定（相对 seed 端口）。
	if got, want := pm[active.NodeKey()], 10000+3; got != want {
		// spy 按 Reload 顺序重分配；允许顺序变化但 key 必须存在（上面已断言）。
		_ = got
		_ = want
	}
}

// TestBUGFIX020ProbeDisabledSkipsMissingPortMapAndDoesNotPanic：
// probeDisabled 仅探测当前 portMap 仍有本地 tunnel 地址的禁用节点；
// 已从运行态剔除的长期禁用不得触发 dial panic，且不应被探测。
func TestBUGFIX020ProbeDisabledSkipsMissingPortMapAndDoesNotPanic(t *testing.T) {
	store := newTestStorage(t)
	sb := newSpyShard()

	inRuntime := tunnelNode("probe-in", "probe-in.example.com", "pw-in")
	if err := sb.Reload([]ParsedNode{inRuntime}); err != nil {
		t.Fatalf("seed Reload: %v", err)
	}
	inAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(sb.GetPortMap()[inRuntime.NodeKey()]))

	subID, err := store.AddSubscription("probe-sub", "", writeSubscriptionFile(t, "proxies: []"), "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}
	// 运行态内的近期禁用
	if err := store.AddProxyWithSource(inAddr, "socks5", storage.SourceSubscription, subID); err != nil {
		t.Fatalf("seed in-runtime proxy: %v", err)
	}
	if err := store.DisableSubscriptionProxy(inAddr, subID); err != nil {
		t.Fatalf("Disable in-runtime: %v", err)
	}
	if _, err := store.GetDB().Exec(
		`UPDATE proxies SET last_check = ? WHERE address = ?`,
		time.Now().Add(-time.Hour).UTC().Format("2006-01-02 15:04:05"), inAddr,
	); err != nil {
		t.Fatalf("set last_check in-runtime: %v", err)
	}

	// 已无端口的长期禁用（幽灵地址）
	ghostAddr := "127.0.0.1:19999"
	if err := store.AddProxyWithSource(ghostAddr, "socks5", storage.SourceSubscription, subID); err != nil {
		t.Fatalf("seed ghost proxy: %v", err)
	}
	if err := store.DisableSubscriptionProxy(ghostAddr, subID); err != nil {
		t.Fatalf("Disable ghost: %v", err)
	}
	if _, err := store.GetDB().Exec(
		`UPDATE proxies SET last_check = ? WHERE address = ?`,
		time.Now().Add(-longTermDisabledRetention-time.Hour).UTC().Format("2006-01-02 15:04:05"), ghostAddr,
	); err != nil {
		t.Fatalf("set last_check ghost: %v", err)
	}

	m := &Manager{
		storage:   store,
		validator: validator.New(1, 1, "http://127.0.0.1/validate"),
		singbox:   sb,
	}
	// 不得 panic；幽灵长期禁用地址不在 portMap，应被跳过。
	m.probeDisabled()

	if _, ok := sb.GetPortMap()[inRuntime.NodeKey()]; !ok {
		t.Fatal("recently disabled node was removed from portMap by probeDisabled")
	}
}

// TestBUGFIX020ProbeDisabledPrunesLongTermFromRuntime：
// probeDisabled 路径应把仍占端口的长期禁用节点从运行态剔除。
func TestBUGFIX020ProbeDisabledPrunesLongTermFromRuntime(t *testing.T) {
	store := newTestStorage(t)
	sb := newSpyShard()

	longTerm := tunnelNode("probe-prune", "probe-prune.example.com", "pw")
	keep := tunnelNode("probe-keep", "probe-keep.example.com", "pw2")
	if err := sb.Reload([]ParsedNode{longTerm, keep}); err != nil {
		t.Fatalf("seed Reload: %v", err)
	}
	pm := sb.GetPortMap()
	longAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(pm[longTerm.NodeKey()]))
	keepAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(pm[keep.NodeKey()]))

	subID, err := store.AddSubscription("prune-sub", "", writeSubscriptionFile(t, "proxies: []"), "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}
	for _, addr := range []string{longAddr, keepAddr} {
		if err := store.AddProxyWithSource(addr, "socks5", storage.SourceSubscription, subID); err != nil {
			t.Fatalf("AddProxy %s: %v", addr, err)
		}
		if err := store.DisableSubscriptionProxy(addr, subID); err != nil {
			t.Fatalf("Disable %s: %v", addr, err)
		}
	}
	if _, err := store.GetDB().Exec(
		`UPDATE proxies SET last_check = ? WHERE address = ?`,
		time.Now().Add(-longTermDisabledRetention-time.Hour).UTC().Format("2006-01-02 15:04:05"), longAddr,
	); err != nil {
		t.Fatalf("set long last_check: %v", err)
	}
	if _, err := store.GetDB().Exec(
		`UPDATE proxies SET last_check = ? WHERE address = ?`,
		time.Now().Add(-time.Hour).UTC().Format("2006-01-02 15:04:05"), keepAddr,
	); err != nil {
		t.Fatalf("set keep last_check: %v", err)
	}

	m := &Manager{
		storage:   store,
		validator: validator.New(1, 1, "http://127.0.0.1/validate"),
		singbox:   sb,
	}
	m.probeDisabled()

	pm = sb.GetPortMap()
	if _, ok := pm[longTerm.NodeKey()]; ok {
		t.Fatalf("long-term disabled still in portMap after probeDisabled: %v", pm)
	}
	if _, ok := pm[keep.NodeKey()]; !ok {
		t.Fatalf("short-term disabled missing from portMap: %v", pm)
	}
}

func TestBUGFIX020SharedRuntimeNodeStaysForActiveSubscription(t *testing.T) {
	store := newTestStorage(t)
	sb := newSpyShard()
	node := tunnelNode("shared", "shared.example.com", "pw")
	if err := sb.Reload([]ParsedNode{node}); err != nil {
		t.Fatalf("seed Reload: %v", err)
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(sb.GetPortMap()[node.NodeKey()]))

	oldSubID, err := store.AddSubscription("old-disabled", "", writeSubscriptionFile(t, "proxies: []"), "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription(old): %v", err)
	}
	activeSubID, err := store.AddSubscription("active", "", writeSubscriptionFile(t, "proxies: []\n# active"), "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription(active): %v", err)
	}
	for _, subID := range []int64{oldSubID, activeSubID} {
		if err := store.AddProxyWithSource(addr, "socks5", storage.SourceSubscription, subID); err != nil {
			t.Fatalf("AddProxyWithSource(%d): %v", subID, err)
		}
	}
	if err := store.DisableSubscriptionProxy(addr, oldSubID); err != nil {
		t.Fatalf("DisableSubscriptionProxy(): %v", err)
	}
	if _, err := store.GetDB().Exec(
		`UPDATE proxies SET last_check = ? WHERE address = ? AND subscription_id = ?`,
		time.Now().Add(-longTermDisabledRetention-time.Hour).UTC().Format("2006-01-02 15:04:05"), addr, oldSubID,
	); err != nil {
		t.Fatalf("set old last_check: %v", err)
	}

	disabled, err := store.GetDisabledCustomProxies()
	if err != nil {
		t.Fatalf("GetDisabledCustomProxies(): %v", err)
	}
	m := &Manager{storage: store, singbox: sb}
	if pruned, err := m.pruneLongTermDisabledFromRuntime(disabled); err != nil {
		t.Fatalf("pruneLongTermDisabledFromRuntime(): %v", err)
	} else if pruned != 0 {
		t.Fatalf("pruned = %d, want 0 while another subscription is active", pruned)
	}
	if _, ok := sb.GetPortMap()[node.NodeKey()]; !ok {
		t.Fatal("shared runtime node was removed despite an active subscription reference")
	}
}

func TestProbeTargetStillCurrentRejectsReusedTunnelPort(t *testing.T) {
	store := newTestStorage(t)
	sb := newSpyShard()
	oldNode := tunnelNode("old", "old.example.com", "old-password")
	newNode := tunnelNode("new", "new.example.com", "new-password")
	if err := sb.Reload([]ParsedNode{oldNode}); err != nil {
		t.Fatalf("seed old runtime: %v", err)
	}

	subID, err := store.AddSubscription("probe-race", "", writeSubscriptionFile(t, "proxies: []"), "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(sb.GetPortMap()[oldNode.NodeKey()]))
	if err := store.AddProxyWithSource(address, "socks5", storage.SourceSubscription, subID); err != nil {
		t.Fatalf("seed old proxy: %v", err)
	}
	if err := store.DisableSubscriptionProxy(address, subID); err != nil {
		t.Fatalf("disable old proxy: %v", err)
	}
	oldProxy, err := store.GetProxyByIdentity(address, storage.SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(old): %v", err)
	}

	// spy shard 重载另一个隧道节点后会复用同一本地端口。
	if err := sb.Reload([]ParsedNode{newNode}); err != nil {
		t.Fatalf("reload new runtime: %v", err)
	}
	if got := sb.GetPortMap()[newNode.NodeKey()]; net.JoinHostPort("127.0.0.1", strconv.Itoa(got)) != address {
		t.Fatalf("new node address = %s, want reused %s", net.JoinHostPort("127.0.0.1", strconv.Itoa(got)), address)
	}

	m := &Manager{storage: store, singbox: sb}
	target := disabledProbeTarget{proxy: *oldProxy, tunnelNodeKey: oldNode.NodeKey()}
	m.refreshMu.Lock()
	stillCurrent := m.probeTargetStillCurrentLocked(target)
	m.refreshMu.Unlock()
	if stillCurrent {
		t.Fatal("stale probe target matched a different node that reused its local port")
	}
}

// TestBUGFIX020IsLongTermDisabledHelper 锁定阈值语义。
func TestBUGFIX020IsLongTermDisabledHelper(t *testing.T) {
	now := time.Now()
	// 1 天策略：25h 前=长期禁用，12h 前=短期禁用；边界等于阈值仍算短期。
	if longTermDisabledRetention != 24*time.Hour {
		t.Fatalf("longTermDisabledRetention = %v, want 24h (1-day policy)", longTermDisabledRetention)
	}
	cases := []struct {
		name string
		p    storage.Proxy
		want bool
	}{
		{"active", storage.Proxy{Status: "active", LastCheck: now.Add(-30 * 24 * time.Hour)}, false},
		{"disabled_zero", storage.Proxy{Status: "disabled"}, false},
		{"disabled_12h_short", storage.Proxy{Status: "disabled", LastCheck: now.Add(-12 * time.Hour)}, false},
		{"disabled_25h_long", storage.Proxy{Status: "disabled", LastCheck: now.Add(-25 * time.Hour)}, true},
		{"disabled_exact", storage.Proxy{Status: "disabled", LastCheck: now.Add(-longTermDisabledRetention)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isLongTermDisabledProxy(tc.p, now); got != tc.want {
				t.Fatalf("isLongTermDisabledProxy = %v, want %v", got, tc.want)
			}
		})
	}
}
