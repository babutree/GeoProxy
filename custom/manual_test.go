package custom

import (
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/babutree/GeoProxy/storage"
)

// newTestStorage 创建临时 SQLite 存储（CGO 已启用）。
func newTestStorage(t *testing.T) *storage.Storage {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "proxy.db")
	store, err := storage.New(dbPath)
	if err != nil {
		t.Fatalf("storage.New() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// TestAddManualNodeDirectHTTP 验证 http:// 直连链接被存储为 source=manual，且 region/note 正确。
func TestAddManualNodeDirectHTTP(t *testing.T) {
	store := newTestStorage(t)
	m := &Manager{storage: store}

	if err := m.AddManualNode("http://1.2.3.4:8080", "hk", "primary"); err != nil {
		t.Fatalf("AddManualNode() error = %v", err)
	}

	proxy, err := store.GetProxyByAddress("1.2.3.4:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if proxy.Source != storage.SourceManual {
		t.Fatalf("Source = %q, want %s", proxy.Source, storage.SourceManual)
	}
	if proxy.Protocol != "http" {
		t.Fatalf("Protocol = %q, want http", proxy.Protocol)
	}
	if proxy.Region != "hk" || proxy.RegionSource != "manual" {
		t.Fatalf("region = %q/%q, want hk/manual", proxy.Region, proxy.RegionSource)
	}
	if proxy.Note != "primary" {
		t.Fatalf("Note = %q, want primary", proxy.Note)
	}
}

// TestAddManualNodeDirectSocks5EmptyRegion 验证 socks5:// 直连链接、空 region 时 region_source 保持 auto。
func TestAddManualNodeDirectSocks5EmptyRegion(t *testing.T) {
	store := newTestStorage(t)
	m := &Manager{storage: store}

	if err := m.AddManualNode("socks5://5.6.7.8:1080", "", "backup"); err != nil {
		t.Fatalf("AddManualNode() error = %v", err)
	}

	proxy, err := store.GetProxyByAddress("5.6.7.8:1080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if proxy.Protocol != "socks5" || proxy.Source != storage.SourceManual {
		t.Fatalf("proxy = %q/%q, want socks5/manual", proxy.Protocol, proxy.Source)
	}
	if proxy.Region != "" || proxy.RegionSource != "auto" {
		t.Fatalf("region = %q/%q, want empty/auto", proxy.Region, proxy.RegionSource)
	}
	if proxy.Note != "backup" {
		t.Fatalf("Note = %q, want backup", proxy.Note)
	}
}

// TestAddManualNodeParseFailure 验证无法解析的链接显式返回错误（无静默成功）。
func TestAddManualNodeParseFailure(t *testing.T) {
	store := newTestStorage(t)
	m := &Manager{storage: store}

	if err := m.AddManualNode("not-a-valid-link", "", ""); err == nil {
		t.Fatal("AddManualNode() expected error for invalid link, got nil")
	}
}

// TestAddManualNodeTunnel 验证加密节点路径。缺少 sing-box 二进制时显式跳过，不伪造成功。
func TestAddManualNodeTunnel(t *testing.T) {
	const singBoxBin = "sing-box"
	if _, err := exec.LookPath(singBoxBin); err != nil {
		t.Skipf("跳过 tunnel 路径：未找到 sing-box 二进制 (%q)，加密节点入库依赖 sing-box 转换", singBoxBin)
	}

	store := newTestStorage(t)
	m := &Manager{
		storage: store,
		singbox: NewSingBoxProcess(singBoxBin, t.TempDir(), testSingBoxBasePort),
	}
	t.Cleanup(func() { m.singbox.Stop() })

	node, err := ParseSingleLink("trojan://password@9.9.9.9:443?sni=example.com#manual-tunnel")
	if err != nil {
		t.Fatalf("ParseSingleLink() error = %v", err)
	}
	if err := m.AddManualNode("trojan://password@9.9.9.9:443?sni=example.com#manual-tunnel", "jp", "tunnel"); err != nil {
		t.Fatalf("AddManualNode() error = %v", err)
	}

	// 端口合并（缺陷5）：单 mixed 端口 → 每个加密节点只入库一行（socks5 协议），不再有独立 HTTP 行。
	// 手工节点默认 disabled 待验证，不能用只计 active 的 CountBySource。
	var count int
	if err := store.GetDB().QueryRow(
		`SELECT COUNT(*) FROM proxies WHERE source = ?`, storage.SourceManual,
	).Scan(&count); err != nil {
		t.Fatalf("count manual proxies: %v", err)
	}
	if count != 1 {
		t.Fatalf("manual proxies = %d, want 1 (单 mixed 端口)", count)
	}

	port := m.singbox.GetPortMap()[node.NodeKey()]
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	proxy, err := store.GetProxyByAddress(addr)
	if err != nil {
		t.Fatalf("GetProxyByAddress(%s) error = %v", addr, err)
	}
	if proxy.Protocol != "socks5" || proxy.Region != "jp" || proxy.Source != storage.SourceManual {
		t.Fatalf("proxy = %q/%q/%q, want socks5/jp/manual", proxy.Protocol, proxy.Region, proxy.Source)
	}
	if proxy.Status != "disabled" {
		t.Fatalf("tunnel manual status = %q, want disabled until validation", proxy.Status)
	}
}

// testSingBoxBasePort 是 tunnel 测试使用的 sing-box 本地监听起始端口。
const testSingBoxBasePort = 39000
