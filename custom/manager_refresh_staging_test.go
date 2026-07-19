package custom

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/selector"
	"github.com/babutree/GeoProxy/storage"
	"github.com/babutree/GeoProxy/validator"
)

// TestRefreshSubscriptionStagesVerifiedNodeUntilValidation 锁定刷新窗口契约：
// 完全未变且最近一次验证成功的旧节点必须继续可选；新节点在验证完成前必须禁用。
func TestRefreshSubscriptionStagesVerifiedNodeUntilValidation(t *testing.T) {
	store := newTestStorage(t)
	oldAddr, entered, release := startBlockingRejectingHTTPProxy(t)
	newAddr, _ := startRejectingHTTPProxy(t)

	oldHost, oldPort := splitProxyAddress(t, oldAddr)
	newHost, newPort := splitProxyAddress(t, newAddr)
	content := fmt.Sprintf("proxies:\n  - name: unchanged\n    type: http\n    server: %s\n    port: %s\n  - name: new\n    type: http\n    server: %s\n    port: %s\n", oldHost, oldPort, newHost, newPort)
	file := writeSubscriptionFile(t, content)
	nodes, err := Parse([]byte(content), "auto")
	if err != nil || len(nodes) != 2 {
		t.Fatalf("Parse() nodes=%d err=%v, want two direct nodes", len(nodes), err)
	}
	subID, err := store.AddSubscription("staged-refresh", "", file, "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if _, err := store.GetDB().Exec(`
		INSERT INTO proxies (address, protocol, source, subscription_id, region_source,
			status, user_paused, fail_count, last_check, exit_ip, exit_location, latency, node_key)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, oldAddr, "http", storage.SourceSubscription, subID, "auto",
		"active", 0, 0, time.Now(), testValidationExitIP, testValidationExitLocation, 45, nodes[0].NodeKey()); err != nil {
		t.Fatalf("seed verified proxy: %v", err)
	}

	m := &Manager{
		storage:   store,
		validator: validator.New(1, 30, "http://127.0.0.1/validate"),
		singbox:   NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort),
	}
	refreshDone := make(chan error, 1)
	go func() { refreshDone <- m.RefreshSubscription(subID) }()

	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("validation did not reach blocking proxy")
	}

	oldProxy, err := store.GetProxyByIdentity(oldAddr, storage.SourceSubscription, subID)
	if err != nil {
		t.Fatalf("read old proxy during validation: %v", err)
	}
	if oldProxy.Status != "active" {
		t.Fatalf("unchanged old proxy status during validation = %q, want active", oldProxy.Status)
	}
	newProxy, err := store.GetProxyByIdentity(newAddr, storage.SourceSubscription, subID)
	if err != nil {
		t.Fatalf("read new proxy during validation: %v", err)
	}
	if newProxy.Status != "disabled" {
		t.Fatalf("new proxy status during validation = %q, want disabled", newProxy.Status)
	}
	selected, err := selector.Pick(store, "", nil)
	if err != nil {
		t.Fatalf("selector.Pick() during validation: %v", err)
	}
	if selected.NodeKey != nodes[0].NodeKey() {
		t.Fatalf("selector picked node_key=%q, want unchanged %q", selected.NodeKey, nodes[0].NodeKey())
	}

	release()
	select {
	case err := <-refreshDone:
		if err != nil {
			t.Fatalf("RefreshSubscription() error = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RefreshSubscription() did not finish after validation release")
	}
	after, err := store.GetProxyByIdentity(oldAddr, storage.SourceSubscription, subID)
	if err != nil {
		t.Fatalf("read old proxy after failed validation: %v", err)
	}
	if after.Status != "disabled" {
		t.Fatalf("failed validation left old proxy status=%q, want disabled", after.Status)
	}
}

// TestReplaceSubscriptionProxiesDoesNotKeepChangedRouteActive 验证稳定 key 不能
// 覆盖地址、协议或上游凭据变化；即使旧行 active，也必须先禁用等待新验证。
func TestReplaceSubscriptionProxiesDoesNotKeepChangedRouteActive(t *testing.T) {
	store := newTestStorage(t)
	subID, err := store.AddSubscription("changed-route", "", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	const address = "changed-route.example:8080"
	const nodeKey = "stable-key-with-changed-credentials"
	if _, err := store.GetDB().Exec(`
		INSERT INTO proxies (address, protocol, source, subscription_id, region_source,
			status, user_paused, fail_count, last_check, exit_ip, exit_location, latency,
			proxy_username, proxy_password, node_key)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, address, "http", storage.SourceSubscription, subID, "auto",
		"active", 0, 0, time.Now(), testValidationExitIP, testValidationExitLocation, 45,
		"old-user", "old-pass", nodeKey); err != nil {
		t.Fatalf("seed old route: %v", err)
	}
	m := &Manager{storage: store}
	if _, err := m.replaceSubscriptionProxies(subID, []subscriptionProxyEntry{{
		addr: address, proto: "http", username: "new-user", password: "new-pass", nodeKey: nodeKey,
	}}); err != nil {
		t.Fatalf("replaceSubscriptionProxies() error = %v", err)
	}
	row, err := store.GetProxyByIdentity(address, storage.SourceSubscription, subID)
	if err != nil {
		t.Fatalf("read changed route: %v", err)
	}
	if row.Status != "disabled" {
		t.Fatalf("changed route status = %q, want disabled", row.Status)
	}
}

func TestReplaceSubscriptionProxiesPreservesOnlyTrustedUnchangedRoute(t *testing.T) {
	tests := []struct {
		name         string
		mutate       func(*subscriptionProxyEntry)
		oldStatus    string
		oldPaused    bool
		oldFailCount int
		hasLastCheck bool
		missingProof bool
		parentPaused bool
		wantStatus   string
	}{
		{name: "trusted unchanged", hasLastCheck: true, wantStatus: "active"},
		{name: "address changed", mutate: func(e *subscriptionProxyEntry) { e.addr = "route-new.example:8080" }, hasLastCheck: true, wantStatus: "disabled"},
		{name: "protocol changed", mutate: func(e *subscriptionProxyEntry) { e.proto = "socks5" }, hasLastCheck: true, wantStatus: "disabled"},
		{name: "dual capability changed", mutate: func(e *subscriptionProxyEntry) { e.dual = false }, hasLastCheck: true, wantStatus: "disabled"},
		{name: "username changed", mutate: func(e *subscriptionProxyEntry) { e.username = "new-user" }, hasLastCheck: true, wantStatus: "disabled"},
		{name: "password changed", mutate: func(e *subscriptionProxyEntry) { e.password = "new-pass" }, hasLastCheck: true, wantStatus: "disabled"},
		{name: "node key changed", mutate: func(e *subscriptionProxyEntry) { e.nodeKey = "new-key" }, hasLastCheck: true, wantStatus: "disabled"},
		{name: "old degraded", oldStatus: "degraded", hasLastCheck: true, wantStatus: "disabled"},
		{name: "old disabled", oldStatus: "disabled", hasLastCheck: true, wantStatus: "disabled"},
		{name: "old user paused", oldPaused: true, hasLastCheck: true, wantStatus: "disabled"},
		{name: "latest check failed", oldFailCount: 1, hasLastCheck: true, wantStatus: "disabled"},
		{name: "never checked", wantStatus: "disabled"},
		{name: "missing successful validation proof", hasLastCheck: true, missingProof: true, wantStatus: "disabled"},
		{name: "parent paused", hasLastCheck: true, parentPaused: true, wantStatus: "disabled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStorage(t)
			subID, err := store.AddSubscription("trust-matrix", "", "", "auto", 60, "")
			if err != nil {
				t.Fatalf("AddSubscription() error = %v", err)
			}
			if tt.parentPaused {
				if _, err := store.GetDB().Exec(`UPDATE subscriptions SET status = ? WHERE id = ?`, "paused", subID); err != nil {
					t.Fatalf("pause subscription: %v", err)
				}
			}
			oldStatus := tt.oldStatus
			if oldStatus == "" {
				oldStatus = "active"
			}
			oldPaused := 0
			if tt.oldPaused {
				oldPaused = 1
			}
			var lastCheck interface{}
			if tt.hasLastCheck {
				lastCheck = time.Now()
			}
			exitIP, exitLocation, latency := testValidationExitIP, testValidationExitLocation, 45
			if tt.missingProof {
				exitIP, exitLocation, latency = "", "", 0
			}
			result, err := store.GetDB().Exec(`
				INSERT INTO proxies (address, protocol, source, subscription_id, region_source,
					status, user_paused, fail_count, last_check, exit_ip, exit_location, latency, dual_protocol,
					proxy_username, proxy_password, node_key)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			`,
				"route.example:8080", "http", storage.SourceSubscription, subID, "auto",
				oldStatus, oldPaused, tt.oldFailCount, lastCheck, exitIP, exitLocation, latency, true,
				"old-user", "old-pass", "stable-key",
			)
			if err != nil {
				t.Fatalf("seed route: %v", err)
			}
			oldID, err := result.LastInsertId()
			if err != nil {
				t.Fatalf("LastInsertId(): %v", err)
			}
			entry := subscriptionProxyEntry{
				addr: "route.example:8080", proto: "http", dual: true,
				username: "old-user", password: "old-pass", nodeKey: "stable-key",
			}
			if tt.mutate != nil {
				tt.mutate(&entry)
			}
			proxies, err := (&Manager{storage: store}).replaceSubscriptionProxies(subID, []subscriptionProxyEntry{entry})
			if err != nil {
				t.Fatalf("replaceSubscriptionProxies() error = %v", err)
			}
			if len(proxies) != 1 {
				t.Fatalf("returned proxies = %d, want 1", len(proxies))
			}
			row, err := store.GetProxyByID(proxies[0].ID)
			if err != nil {
				t.Fatalf("GetProxyByID(%d): %v", proxies[0].ID, err)
			}
			if row.Status != tt.wantStatus || proxies[0].Status != tt.wantStatus {
				t.Fatalf("statuses db/returned = %q/%q, want %q (old id=%d new id=%d)", row.Status, proxies[0].Status, tt.wantStatus, oldID, proxies[0].ID)
			}
		})
	}
}

func TestRefreshSubscriptionChangedRouteActivatesOnlyAfterSuccessfulValidation(t *testing.T) {
	installManagerTestConfig(t)
	store := newTestStorage(t)
	address := unusedLoopbackAddress(t)
	host, port := splitProxyAddress(t, address)
	content := fmt.Sprintf("proxies:\n  - name: changed-credentials\n    type: http\n    server: %s\n    port: %s\n    username: new-user\n    password: new-pass\n", host, port)
	nodes, err := Parse([]byte(content), "auto")
	if err != nil || len(nodes) != 1 {
		t.Fatalf("Parse() nodes=%d err=%v, want one direct node", len(nodes), err)
	}
	subID, err := store.AddSubscription("changed-success", "", writeSubscriptionFile(t, content), "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if _, err := store.GetDB().Exec(`
		INSERT INTO proxies (address, protocol, source, subscription_id, region_source,
			status, user_paused, fail_count, last_check, exit_ip, exit_location, latency,
			proxy_username, proxy_password, node_key)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, address, "http", storage.SourceSubscription, subID, "auto",
		"active", 0, 0, time.Now(), testValidationExitIP, testValidationExitLocation, 45,
		"old-user", "old-pass", nodes[0].NodeKey()); err != nil {
		t.Fatalf("seed changed proxy: %v", err)
	}
	fake := newBlockingProxyValidator(map[string]bool{address: true})
	t.Cleanup(fake.Release)
	m := &Manager{
		storage: store, validator: fake,
		singbox: NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort),
	}
	refreshDone := make(chan error, 1)
	go func() { refreshDone <- m.RefreshSubscription(subID) }()
	waitValidatorEntered(t, fake.entered)

	during, err := store.GetProxyByIdentity(address, storage.SourceSubscription, subID)
	if err != nil {
		t.Fatalf("read changed proxy during validation: %v", err)
	}
	if during.Status != "disabled" || during.Username != "new-user" || during.Password != "new-pass" {
		t.Fatalf("changed proxy during validation = %+v, want new route disabled", during)
	}
	if picked, err := selector.Pick(store, "", nil); !errors.Is(err, selector.ErrNoNode) || picked != nil {
		t.Fatalf("selector.Pick() during changed validation = proxy:%+v err:%v, want ErrNoNode", picked, err)
	}

	fake.Release()
	waitRefreshResult(t, refreshDone)
	after, err := store.GetProxyByIdentity(address, storage.SourceSubscription, subID)
	if err != nil {
		t.Fatalf("read changed proxy after validation: %v", err)
	}
	if after.Status != "active" || after.ExitIP != testValidationExitIP ||
		after.ExitLocation != testValidationExitLocation || after.Latency != 45 {
		t.Fatalf("changed proxy after successful validation = %+v", after)
	}
}

func TestRefreshSubscriptionSuccessfulValidationKeepsPreservedProxyActive(t *testing.T) {
	installManagerTestConfig(t)
	store := newTestStorage(t)
	address := unusedLoopbackAddress(t)
	host, port := splitProxyAddress(t, address)
	content := fmt.Sprintf("proxies:\n  - name: unchanged-success\n    type: http\n    server: %s\n    port: %s\n", host, port)
	nodes, err := Parse([]byte(content), "auto")
	if err != nil || len(nodes) != 1 {
		t.Fatalf("Parse() nodes=%d err=%v, want one direct node", len(nodes), err)
	}
	subID, err := store.AddSubscription("unchanged-success", "", writeSubscriptionFile(t, content), "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if _, err := store.GetDB().Exec(`
		INSERT INTO proxies (address, protocol, source, subscription_id, region_source,
			status, user_paused, fail_count, last_check, exit_ip, exit_location, latency, node_key)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, address, "http", storage.SourceSubscription, subID, "auto",
		"active", 0, 0, time.Now(), testValidationExitIP, testValidationExitLocation, 45, nodes[0].NodeKey()); err != nil {
		t.Fatalf("seed unchanged proxy: %v", err)
	}
	fake := newBlockingProxyValidator(map[string]bool{address: true})
	t.Cleanup(fake.Release)
	m := &Manager{
		storage: store, validator: fake,
		singbox: NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort),
	}
	refreshDone := make(chan error, 1)
	go func() { refreshDone <- m.RefreshSubscription(subID) }()
	waitValidatorEntered(t, fake.entered)
	during, err := store.GetProxyByIdentity(address, storage.SourceSubscription, subID)
	if err != nil || during.Status != "active" {
		t.Fatalf("preserved proxy during validation = %+v err=%v, want active", during, err)
	}

	fake.Release()
	waitRefreshResult(t, refreshDone)
	after, err := store.GetProxyByIdentity(address, storage.SourceSubscription, subID)
	if err != nil {
		t.Fatalf("read proxy after successful validation: %v", err)
	}
	if after.Status != "active" || after.ExitIP != testValidationExitIP ||
		after.ExitLocation != testValidationExitLocation || after.Latency != 45 {
		t.Fatalf("preserved proxy after successful validation = %+v", after)
	}
	subscription, err := store.GetSubscription(subID)
	if err != nil {
		t.Fatalf("GetSubscription(%d): %v", subID, err)
	}
	if subscription.LastSuccess.IsZero() {
		t.Fatal("successful preserved validation did not update subscription last_success")
	}
}

func TestRefreshSubscriptionGeoBlockedPreservedProxyIsDisabledBeforeMetadataWrite(t *testing.T) {
	installManagerTestConfig(t)
	cfg := config.DefaultConfig()
	cfg.AllowedCountries = nil
	cfg.BlockedCountries = []string{"JP"}
	config.SetGlobal(cfg)

	store := newTestStorage(t)
	address := unusedLoopbackAddress(t)
	host, port := splitProxyAddress(t, address)
	content := fmt.Sprintf("proxies:\n  - name: geo-blocked-preserved\n    type: http\n    server: %s\n    port: %s\n", host, port)
	nodes, err := Parse([]byte(content), "auto")
	if err != nil || len(nodes) != 1 {
		t.Fatalf("Parse() nodes=%d err=%v, want one direct node", len(nodes), err)
	}
	subID, err := store.AddSubscription("geo-blocked-preserved", "", writeSubscriptionFile(t, content), "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if _, err := store.GetDB().Exec(`
		INSERT INTO proxies (address, protocol, source, subscription_id, region_source,
			status, user_paused, fail_count, last_check, exit_ip, exit_location, latency, node_key)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, address, "http", storage.SourceSubscription, subID, "auto",
		"active", 0, 0, time.Now(), testValidationExitIP, testValidationExitLocation, 45, nodes[0].NodeKey()); err != nil {
		t.Fatalf("seed geo-blocked proxy: %v", err)
	}
	if _, err := store.GetDB().Exec(`
		CREATE TABLE staging_status_observations (status TEXT NOT NULL);
		CREATE TRIGGER observe_staged_exit_write
		BEFORE UPDATE OF exit_ip ON proxies
		BEGIN
			INSERT INTO staging_status_observations(status) VALUES (OLD.status);
		END
	`); err != nil {
		t.Fatalf("create status observation trigger: %v", err)
	}
	fake := newBlockingProxyValidator(map[string]bool{address: true})
	t.Cleanup(fake.Release)
	m := &Manager{
		storage: store, validator: fake,
		singbox: NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort),
	}
	refreshDone := make(chan error, 1)
	go func() { refreshDone <- m.RefreshSubscription(subID) }()
	waitValidatorEntered(t, fake.entered)
	fake.Release()
	waitRefreshResult(t, refreshDone)

	var observedStatus string
	if err := store.GetDB().QueryRow(`SELECT status FROM staging_status_observations`).Scan(&observedStatus); err != nil {
		t.Fatalf("read status observation: %v", err)
	}
	if observedStatus != "disabled" {
		t.Fatalf("status during metadata write = %q, want disabled before geo decision", observedStatus)
	}
	after, err := store.GetProxyByIdentity(address, storage.SourceSubscription, subID)
	if err != nil || after.Status != "disabled" {
		t.Fatalf("geo-blocked proxy after validation = %+v err=%v, want disabled", after, err)
	}
}

func TestRefreshSubscriptionExitWriteFailureDisablesPreservedProxy(t *testing.T) {
	installManagerTestConfig(t)
	store := newTestStorage(t)
	address := unusedLoopbackAddress(t)
	host, port := splitProxyAddress(t, address)
	content := fmt.Sprintf("proxies:\n  - name: unchanged-write-failure\n    type: http\n    server: %s\n    port: %s\n", host, port)
	nodes, err := Parse([]byte(content), "auto")
	if err != nil || len(nodes) != 1 {
		t.Fatalf("Parse() nodes=%d err=%v, want one direct node", len(nodes), err)
	}
	subID, err := store.AddSubscription("write-failure", "", writeSubscriptionFile(t, content), "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	if _, err := store.GetDB().Exec(`
		INSERT INTO proxies (address, protocol, source, subscription_id, region_source,
			status, user_paused, fail_count, last_check, exit_ip, exit_location, latency, node_key)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, address, "http", storage.SourceSubscription, subID, "auto",
		"active", 0, 0, time.Now(), testValidationExitIP, testValidationExitLocation, 45, nodes[0].NodeKey()); err != nil {
		t.Fatalf("seed unchanged proxy: %v", err)
	}
	if _, err := store.GetDB().Exec(`
		CREATE TRIGGER fail_staged_exit_write
		BEFORE UPDATE OF exit_ip ON proxies
		BEGIN
			SELECT RAISE(ABORT, 'injected staged exit write failure');
		END
	`); err != nil {
		t.Fatalf("create exit write trigger: %v", err)
	}
	fake := newBlockingProxyValidator(map[string]bool{address: true})
	t.Cleanup(fake.Release)
	m := &Manager{
		storage: store, validator: fake,
		singbox: NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort),
	}
	refreshDone := make(chan error, 1)
	go func() { refreshDone <- m.RefreshSubscription(subID) }()
	waitValidatorEntered(t, fake.entered)
	during, err := store.GetProxyByIdentity(address, storage.SourceSubscription, subID)
	if err != nil || during.Status != "active" {
		t.Fatalf("preserved proxy during validation = %+v err=%v, want active", during, err)
	}

	fake.Release()
	waitRefreshResult(t, refreshDone)
	after, err := store.GetProxyByIdentity(address, storage.SourceSubscription, subID)
	if err != nil {
		t.Fatalf("read proxy after write failure: %v", err)
	}
	if after.Status != "disabled" {
		t.Fatalf("exit write failure left preserved proxy status=%q, want disabled", after.Status)
	}
}

const (
	testValidationExitIP       = "198.51.100.44"
	testValidationExitLocation = "JP Tokyo"
)

type blockingProxyValidator struct {
	valid       map[string]bool
	entered     chan struct{}
	release     chan struct{}
	enterOnce   sync.Once
	releaseOnce sync.Once
}

func newBlockingProxyValidator(valid map[string]bool) *blockingProxyValidator {
	return &blockingProxyValidator{
		valid: valid, entered: make(chan struct{}), release: make(chan struct{}),
	}
}

func (v *blockingProxyValidator) ValidateOne(proxy storage.Proxy) (bool, time.Duration, string, string, validator.RiskInfo) {
	valid := v.valid[proxy.Address]
	if !valid {
		return false, 0, "", "", validator.UnknownRisk()
	}
	return true, 45 * time.Millisecond, testValidationExitIP, testValidationExitLocation, validator.UnknownRisk()
}

func (v *blockingProxyValidator) ValidateStream(proxies []storage.Proxy) <-chan validator.Result {
	results := make(chan validator.Result, len(proxies))
	snapshot := append([]storage.Proxy(nil), proxies...)
	go func() {
		defer close(results)
		v.enterOnce.Do(func() { close(v.entered) })
		<-v.release
		for _, proxy := range snapshot {
			valid, latency, exitIP, exitLocation, risk := v.ValidateOne(proxy)
			results <- validator.Result{
				Proxy: proxy, Valid: valid, Latency: latency,
				ExitIP: exitIP, ExitLocation: exitLocation, Risk: risk,
			}
		}
	}()
	return results
}

func (v *blockingProxyValidator) Release() {
	v.releaseOnce.Do(func() { close(v.release) })
}

func waitValidatorEntered(t *testing.T, entered <-chan struct{}) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("validator did not enter")
	}
}

func waitRefreshResult(t *testing.T, refreshDone <-chan error) {
	t.Helper()
	select {
	case err := <-refreshDone:
		if err != nil {
			t.Fatalf("RefreshSubscription() error = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RefreshSubscription() did not finish")
	}
}

func unusedLoopbackAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return address
}

func installManagerTestConfig(t *testing.T) {
	t.Helper()
	previous := config.Get()
	config.SetGlobal(config.DefaultConfig())
	t.Cleanup(func() { config.SetGlobal(previous) })
}

func splitProxyAddress(t *testing.T, address string) (string, string) {
	t.Helper()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", address, err)
	}
	if _, err := strconv.Atoi(port); err != nil {
		t.Fatalf("proxy port %q is not numeric: %v", port, err)
	}
	return host, port
}

// startBlockingRejectingHTTPProxy 在收到请求首字节后阻塞，
// 让测试能在验证结束前检查已提交的暂存状态。
func startBlockingRejectingHTTPProxy(t *testing.T) (string, <-chan struct{}, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	entered := make(chan struct{})
	releaseCh := make(chan struct{})
	var enterOnce sync.Once
	var releaseOnce sync.Once
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1)
				_, _ = c.Read(buf)
				enterOnce.Do(func() { close(entered) })
				<-releaseCh
				_, _ = c.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n"))
			}(conn)
		}
	}()
	cleanup := func() {
		releaseOnce.Do(func() { close(releaseCh) })
		_ = ln.Close()
		<-done
	}
	t.Cleanup(cleanup)
	return ln.Addr().String(), entered, func() { releaseOnce.Do(func() { close(releaseCh) }) }
}
