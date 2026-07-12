package custom

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"goproxy/storage"
	"goproxy/validator"
)

func TestNodeKeyIncludesCredentialsAndTransport(t *testing.T) {
	nodeA := ParsedNode{
		Name:   "a",
		Type:   "vless",
		Server: "example.com",
		Port:   443,
		Raw: map[string]interface{}{
			"type":    "vless",
			"server":  "example.com",
			"port":    443,
			"uuid":    "uuid-a",
			"network": "ws",
			"ws-opts": map[string]interface{}{"path": "/a"},
		},
	}
	nodeB := ParsedNode{
		Name:   "b",
		Type:   "vless",
		Server: "example.com",
		Port:   443,
		Raw: map[string]interface{}{
			"type":    "vless",
			"server":  "example.com",
			"port":    443,
			"uuid":    "uuid-b",
			"network": "ws",
			"ws-opts": map[string]interface{}{"path": "/a"},
		},
	}
	nodeC := ParsedNode{
		Name:   "renamed",
		Type:   "vless",
		Server: "example.com",
		Port:   443,
		Raw: map[string]interface{}{
			"type":    "vless",
			"server":  "example.com",
			"port":    443,
			"uuid":    "uuid-a",
			"network": "ws",
			"ws-opts": map[string]interface{}{"path": "/a"},
		},
	}
	nodeD := ParsedNode{
		Name:   "different-transport",
		Type:   "vless",
		Server: "example.com",
		Port:   443,
		Raw: map[string]interface{}{
			"type":    "vless",
			"server":  "example.com",
			"port":    443,
			"uuid":    "uuid-a",
			"network": "grpc",
			"grpc-opts": map[string]interface{}{
				"grpc-service-name": "svc",
			},
		},
	}

	if nodeA.NodeKey() == nodeB.NodeKey() {
		t.Fatal("NodeKey() 合并了相同 host:port 但不同凭据的节点")
	}
	if nodeA.NodeKey() != nodeC.NodeKey() {
		t.Fatal("NodeKey() 不应因展示名称变化而改变")
	}
	if nodeA.NodeKey() == nodeD.NodeKey() {
		t.Fatal("NodeKey() 合并了相同凭据但不同传输参数的节点")
	}
}

func TestSingBoxPortMapStableAcrossMultiSubscriptionReloads(t *testing.T) {
	s := NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort)
	nodeA := tunnelNode("sub-a", "a.example.com", "password-a")
	nodeB := tunnelNode("sub-b", "b.example.com", "password-b")

	_, firstPorts := s.assembleConfig([]ParsedNode{nodeA})
	s.portMap = firstPorts
	_, secondPorts := s.assembleConfig([]ParsedNode{nodeB, nodeA})

	if firstPorts[nodeA.NodeKey()] != secondPorts[nodeA.NodeKey()] {
		t.Fatalf("existing node port changed from %d to %d", firstPorts[nodeA.NodeKey()], secondPorts[nodeA.NodeKey()])
	}
	if secondPorts[nodeB.NodeKey()] == 0 {
		t.Fatal("new node did not receive a port")
	}
	if secondPorts[nodeA.NodeKey()] == secondPorts[nodeB.NodeKey()] {
		t.Fatal("different tunnel nodes received the same port")
	}

	// 端口合并：每个 tunnel 节点仅一个 mixed 端口，所有端口必须两两不同。
	seen := map[int]string{}
	for key, p := range secondPorts {
		if prev, dup := seen[p]; dup {
			t.Fatalf("port %d reused by %s and %s", p, key, prev)
		}
		seen[p] = key
	}
}

func TestCollectAllTunnelNodesKeepsLoadedNodesWhenOtherSubscriptionFetchFails(t *testing.T) {
	store := newTestStorage(t)
	goodFile := writeSubscriptionFile(t, "trojan://password-new@new.example.com:443?sni=new.example.com#new")
	badFile := filepath.Join(t.TempDir(), "missing.txt")
	if _, err := store.AddSubscription("bad", "", badFile, "auto", 60, ""); err != nil {
		t.Fatalf("AddSubscription(bad) error = %v", err)
	}
	if _, err := store.AddSubscription("good", "", goodFile, "auto", 60, ""); err != nil {
		t.Fatalf("AddSubscription(good) error = %v", err)
	}
	oldNode := tunnelNode("old", "old.example.com", "password-old")
	sb := NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort)
	sb.nodes = []ParsedNode{oldNode}
	sb.portMap = map[string]int{oldNode.NodeKey(): testSingBoxBasePort + 1}
	m := &Manager{
		storage: store,
		singbox: sb,
	}

	nodes, err := m.collectAllTunnelNodes()
	if err != nil {
		t.Fatalf("collectAllTunnelNodes() error = %v", err)
	}
	keys := make(map[string]bool)
	for _, node := range nodes {
		keys[node.NodeKey()] = true
	}
	if !keys[oldNode.NodeKey()] {
		t.Fatal("loaded old tunnel node was dropped when another subscription failed to fetch")
	}
	if len(keys) != 2 {
		t.Fatalf("collected tunnel nodes = %d, want old loaded + good file", len(keys))
	}
}

func TestRefreshSubscriptionFailureKeepsOldUsableProxy(t *testing.T) {
	store := newTestStorage(t)
	file := writeSubscriptionFile(t, "trojan://password@new.example.com:443?sni=new.example.com#new")
	subID, err := store.AddSubscription("sub", "", file, "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	oldAddr := "127.0.0.1:39001"
	if err := store.AddProxyWithSource(oldAddr, "socks5", storage.SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}
	m := &Manager{
		storage:   store,
		validator: validator.New(1, 1, "http://127.0.0.1/validate"),
		singbox:   NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort),
	}

	if err := m.RefreshSubscription(subID); err == nil {
		t.Fatal("RefreshSubscription() expected sing-box error, got nil")
	}
	proxy, err := store.GetProxyByAddress(oldAddr)
	if err != nil {
		t.Fatalf("old usable proxy was deleted after failed refresh: %v", err)
	}
	if proxy.Status != "active" {
		t.Fatalf("old proxy status = %q, want active", proxy.Status)
	}
}

func TestRefreshSubscriptionAddsNewDirectNodesDisabledUntilValidated(t *testing.T) {
	store := newTestStorage(t)
	proxyAddr, validateURL := startRejectingHTTPProxy(t)
	host, port, err := net.SplitHostPort(proxyAddr)
	if err != nil {
		t.Fatalf("SplitHostPort() error = %v", err)
	}
	file := writeSubscriptionFile(t, "proxies:\n  - name: pending-http\n    type: http\n    server: "+host+"\n    port: "+port+"\n")
	subID, err := store.AddSubscription("sub", "", file, "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	m := &Manager{
		storage:   store,
		validator: validator.New(1, 1, validateURL),
		singbox:   NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort),
	}

	if err := m.RefreshSubscription(subID); err != nil {
		t.Fatalf("RefreshSubscription() error = %v", err)
	}
	proxy, err := store.GetProxyByAddress(proxyAddr)
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if proxy.Status != "disabled" {
		t.Fatalf("new subscription proxy status = %q, want disabled until validation passes", proxy.Status)
	}
	if _, err := store.GetRandom(); err == nil {
		t.Fatal("unvalidated/failed new subscription proxy entered active selection")
	}
}

func TestSingBoxRuntimeStatusExplainsNoTunnelNodesAndFailures(t *testing.T) {
	s := NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort)
	if err := s.Reload([]ParsedNode{{Name: "direct", Type: "http", Server: "127.0.0.1", Port: 8080}}); err != nil {
		t.Fatalf("Reload(direct only) error = %v", err)
	}
	status := s.GetRuntimeStatus()
	if status.Status != SingBoxStatusNoTunnelNodes || status.Reason != "no_tunnel_nodes" || status.Running {
		t.Fatalf("direct-only status = %+v, want no_tunnel_nodes and not running", status)
	}

	if err := s.Reload([]ParsedNode{tunnelNode("tunnel", "tunnel.example.com", "password")}); err == nil {
		t.Fatal("Reload(tunnel) expected missing sing-box error, got nil")
	}
	status = s.GetRuntimeStatus()
	if status.Status != SingBoxStatusFailed || status.Reason != "binary_not_found" || status.Running {
		t.Fatalf("failed status = %+v, want failed/binary_not_found and not running", status)
	}
}

// TestManagerStopIdempotent 覆盖 Manager.Stop 生命周期：重复 Stop 不得 panic
//（旧实现 close(stopCh) 无 once，第二次 close 直接崩溃）。
func TestManagerStopIdempotent(t *testing.T) {
	store := newTestStorage(t)
	sb, spies := newSpyOrchestrator(10000, 1)
	m := &Manager{
		storage: store,
		singbox: sb,
		stopCh:  make(chan struct{}),
	}
	m.Stop()
	m.Stop() // 不得 panic
	if spies[0].stops() < 1 {
		t.Fatal("Stop 未下传到 sing-box 编排器")
	}
}

// TestManagerStopSerializesWithRefresh 验证 Stop 与 Refresh 串行：
// 持 refreshMu 的刷新在途时，Stop 必须等刷新退出后再 stop sing-box，
// 避免 stop 后刷新仍调用 Reload 造成端口复用竞态。
func TestManagerStopSerializesWithRefresh(t *testing.T) {
	store := newTestStorage(t)
	// 故意使用会失败的 tunnel 订阅：Refresh 在 Reload 前已持 refreshMu。
	file := writeSubscriptionFile(t, "trojan://password@new.example.com:443?sni=new.example.com#new")
	subID, err := store.AddSubscription("sub", "", file, "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}

	sb, spies := newSpyOrchestrator(10000, 1)
	// Reload 阻塞，模拟长时间重载窗口，便于观察 Stop 是否与 refreshMu 串行。
	block := make(chan struct{})
	release := make(chan struct{})
	spies[0].mu.Lock()
	// 通过自定义 Reload：用 reloadErr 路径不够；改为包装 wait 逻辑
	spies[0].mu.Unlock()
	// 使用 blockingShard 覆盖 Reload
	bs := &blockingShard{spyShard: spies[0], enter: block, release: release}
	sb.shards[0] = bs

	m := &Manager{
		storage:   store,
		validator: validator.New(1, 1, "http://127.0.0.1/validate"),
		singbox:   sb,
		stopCh:    make(chan struct{}),
	}

	refreshDone := make(chan error, 1)
	go func() {
		refreshDone <- m.RefreshSubscription(subID)
	}()

	select {
	case <-block:
	case <-time.After(3 * time.Second):
		t.Fatal("Refresh 未进入 Reload，无法验证与 Stop 的串行")
	}

	stopDone := make(chan struct{})
	go func() {
		m.Stop()
		close(stopDone)
	}()

	// 刷新仍持锁时 Stop 不得完成（否则说明未与 refreshMu 串行）。
	select {
	case <-stopDone:
		t.Fatal("Stop 在 Refresh 持锁期间已返回，说明未与 refreshMu 串行")
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	select {
	case <-refreshDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Refresh 在 release 后未结束")
	}
	select {
	case <-stopDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop 在 Refresh 结束后仍未返回")
	}
}

// blockingShard 在 Reload 入口发信号并阻塞，直到 release 关闭。
type blockingShard struct {
	*spyShard
	enter   chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingShard) Reload(nodes []ParsedNode) error {
	b.once.Do(func() { close(b.enter) })
	<-b.release
	return b.spyShard.Reload(nodes)
}

func TestManagerStatusIncludesExplainedSingBoxFields(t *testing.T) {
	store := newTestStorage(t)
	m := &Manager{
		storage: store,
		singbox: NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort),
	}

	if err := m.singbox.Reload([]ParsedNode{{Name: "direct", Type: "http", Server: "127.0.0.1", Port: 8080}}); err != nil {
		t.Fatalf("Reload(direct only) error = %v", err)
	}
	status := m.GetStatus()
	if status["singbox_status"] != SingBoxStatusNoTunnelNodes {
		t.Fatalf("singbox_status = %v, want %q", status["singbox_status"], SingBoxStatusNoTunnelNodes)
	}
	if status["singbox_reason"] != "no_tunnel_nodes" {
		t.Fatalf("singbox_reason = %v, want no_tunnel_nodes", status["singbox_reason"])
	}
	if status["singbox_running"] != false {
		t.Fatalf("singbox_running = %v, want false", status["singbox_running"])
	}
	if status["singbox_ready_ports"] != 0 || status["singbox_total_ports"] != 0 {
		t.Fatalf("ready/total ports = %v/%v, want 0/0", status["singbox_ready_ports"], status["singbox_total_ports"])
	}
}

func tunnelNode(name, server, password string) ParsedNode {
	return ParsedNode{
		Name:   name,
		Type:   "trojan",
		Server: server,
		Port:   443,
		Raw: map[string]interface{}{
			"type":     "trojan",
			"name":     name,
			"server":   server,
			"port":     443,
			"password": password,
			"tls":      true,
			"sni":      server,
		},
	}
}

func writeSubscriptionFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "subscription.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func startRejectingHTTPProxy(t *testing.T) (string, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
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
				_, _ = c.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n"))
			}(conn)
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		<-done
	})
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort() error = %v", err)
	}
	if _, err := strconv.Atoi(port); err != nil {
		t.Fatalf("proxy listener port is not numeric: %v", err)
	}
	return ln.Addr().String(), "http://example.invalid/validate"
}
