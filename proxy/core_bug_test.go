package proxy

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/babutree/GeoProxy/auth"
	"github.com/babutree/GeoProxy/storage"
)

// 拨号失败后会话不能继续粘滞到已失败节点。
//
// selector.Resolve 在拨号之前就写入 session -> node 绑定（首绑）。当随后的拨号
// 失败时，旧实现只累加 fail_count，绑定仍然存在。下一次同 session 请求会经由
// stickyBoundProxy 直接命中同一个刚失败的节点（只要它 fail_count<3 仍“可用”），
// 并且绕过 cooldown 过滤——会话被“租约”粘死在坏节点上。
//
// 修复目标：拨号失败后，若该 session 仍绑定到失败节点，则释放绑定，使下一次
// 请求重新选路并尊重 cooldown/健康过滤，而不是粘死。

func TestTunnelDialFailureReleasesSessionBinding(t *testing.T) {
	deadAddr := reserveClosedAddr(t)
	store := newProxyTestStore()
	addProxy(t, store, deadAddr, "http", 1)
	server := newProxyTestServer(store, proxyTestConfig(0)) // MaxRetry=0：单次尝试
	route := auth.ParsedUsername{Session: "failed-tunnel"}

	req := httptest.NewRequest(http.MethodConnect, "http://example.test:443", nil)
	req.Host = "example.test:443"
	// 拨号在 hijack 之前失败，普通 recorder 足够（不会触达 hijack 路径）。
	server.handleTunnel(httptest.NewRecorder(), req, route)

	binding, ok := server.sessions.Get(route.Session)
	if ok && binding.ProxyID == 1 {
		t.Fatalf("session still leased to failed node after dial failure: binding=%#v (failure stickiness)", binding)
	}
}

func TestHTTPDialFailureReleasesSessionBinding(t *testing.T) {
	deadAddr := reserveClosedAddr(t)
	store := newProxyTestStore()
	addProxy(t, store, deadAddr, "http", 1)
	server := newProxyTestServer(store, proxyTestConfig(0)) // MaxRetry=0
	route := auth.ParsedUsername{Session: "failed-http"}

	req := httptest.NewRequest(http.MethodGet, "http://example.test/x", nil)
	server.handleHTTP(httptest.NewRecorder(), req, route)

	binding, ok := server.sessions.Get(route.Session)
	if ok && binding.ProxyID == 1 {
		t.Fatalf("session still leased to failed node after HTTP dial failure: binding=%#v (failure stickiness)", binding)
	}
}

func TestSOCKS5DialFailureReleasesSessionBinding(t *testing.T) {
	deadAddr := reserveClosedAddr(t)
	store := newProxyTestStore()
	addProxy(t, store, deadAddr, "socks5", 1)
	server := newSocks5TestServer(store, proxyTestConfig(0))
	client, upstream := net.Pipe()
	t.Cleanup(func() { client.Close() })
	route := auth.ParsedUsername{Session: "failed-socks5"}

	done := make(chan struct{})
	go func() {
		defer close(done)
		server.handleConnection(upstream)
	}()
	writeSocks5NoAuthHandshake(t, client)
	writeSocks5DomainRequest(t, client, "example.test", 443)
	reply := make([]byte, 10)
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatalf("read failure reply: %v", err)
	}
	if reply[1] != 0x01 {
		t.Fatalf("SOCKS5 reply = %#x, want general failure", reply)
	}
	<-done

	binding, ok := server.sessions.Get(route.Session)
	if ok && binding.ProxyID == 1 {
		t.Fatalf("session still leased to failed SOCKS5 node: binding=%#v", binding)
	}
}

// 并发失败计数达到禁用阈值时仍必须禁用节点。
//
// recordProxyFailure 在选路时读取的 p.FailCount 快照上判断 p.FailCount+1>=3。
// 三个并发请求在 fail_count=0 时同时选中该节点，各自持 FailCount=0 的快照拨号
// 失败；每个 caller 看到的都是 0+1=1 < 3，谁都不禁用。原子自增后真实 fail_count
// 到达 3，但节点仍 status=active——被选路/健康检查以 fail_count<3 静默排除、又
// 永远得不到成功归零的僵尸态。
//
// 修复目标：记录失败后按权威(自增后)的 fail_count 判定禁用，保证不变式
// “fail_count>=阈值 ⟹ status=disabled”在并发下成立。

func TestConcurrentFailuresDisableNodeAtThreshold(t *testing.T) {
	store := newProxyTestStore()
	addProxy(t, store, "10.0.0.1:9", "http", 1) // 节点初始 fail_count=0
	id := store.addressID["10.0.0.1:9"]

	const concurrent = failDisableThreshold // 恰好达到阈值
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 每个 goroutine 持选路时的旧快照（FailCount=0），模拟并发请求。
			snapshot := &storage.Proxy{ID: id, Address: "10.0.0.1:9", Protocol: "http", FailCount: 0}
			<-start
			recordProxyFailure(store, snapshot)
		}()
	}
	close(start)
	wg.Wait()

	got, err := store.GetProxyByID(id)
	if err != nil {
		t.Fatalf("GetProxyByID error = %v", err)
	}
	if got.FailCount != concurrent {
		t.Fatalf("fail_count = %d, want %d", got.FailCount, concurrent)
	}
	if got.Status != "disabled" {
		t.Fatalf("node status = %q with fail_count=%d, want disabled (zombie: fail_count>=%d but still active)", got.Status, got.FailCount, failDisableThreshold)
	}
}

// 出站 SOCKS5 域名超过 255 字节时必须显式拒绝。
//
// server.go dialViaProxy 的 socks5 分支用 byte(len(targetHost)) 写入域名长度。
// 域名 >255 字节时 byte() 截断，向上游发出长度字段错误的损坏帧（静默数据损坏）。
// 修复目标：显式拒绝超长域名并返回明确错误，而不是静默截断。

func TestDialViaSocks5RejectsOverlongDomain(t *testing.T) {
	server := New(nil, proxyTestConfig(0), ":0")
	overlong := strings.Repeat("a", 256) + ".com:443" // targetHost 260 字节 > 255
	// 用已关闭地址：修复后应在拨号前就以“长度错误”返回，根本不触达拨号。
	_, err := server.dialViaProxy(&storage.Proxy{Address: reserveClosedAddr(t), Protocol: "socks5"}, overlong)
	if err == nil {
		t.Fatal("dialViaProxy accepted overlong socks5 domain, want explicit length error")
	}
	if !strings.Contains(err.Error(), "too long") {
		t.Fatalf("error = %v, want an explicit domain-length error (not a silent truncation / dial error)", err)
	}
}
