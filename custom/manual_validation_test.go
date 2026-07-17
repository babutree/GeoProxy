package custom

import (
	"testing"

	"github.com/babutree/GeoProxy/storage"
	"github.com/babutree/GeoProxy/validator"
)

// TestAddManualDirectNodeStartsDisabledUntilValidated：
// 手工直连 HTTP/SOCKS 不得默认 active 进选路；导入后先 disabled 待验证。
func TestAddManualDirectNodeStartsDisabledUntilValidated(t *testing.T) {
	store := newTestStorage(t)
	m := &Manager{
		storage:   store,
		validator: validator.New(1, 1, "http://example.invalid/validate"),
		singbox:   newSpyShard(),
	}

	if err := m.AddManualNode("socks5://203.0.113.10:1080", "", "no-pass"); err != nil {
		t.Fatalf("AddManualNode() error = %v", err)
	}
	proxy, err := store.GetProxyByIdentity("203.0.113.10:1080", storage.SourceManual, 0)
	if err != nil {
		t.Fatalf("GetProxyByIdentity() error = %v", err)
	}
	if proxy.Status != "disabled" {
		t.Fatalf("status = %q, want disabled until validation", proxy.Status)
	}
	if _, err := store.GetRandom(); err == nil {
		t.Fatal("unvalidated manual node entered active selection")
	}
}

// TestImportManualLinksStartsDisabledAndDoesNotEnterSelection：
// 批量导入同样先 disabled；验证失败的节点保持不可用。
func TestImportManualLinksStartsDisabledAndDoesNotEnterSelection(t *testing.T) {
	store := newTestStorage(t)
	m := &Manager{
		storage:   store,
		validator: validator.New(1, 1, "http://example.invalid/validate"),
		singbox:   newSpyShard(),
	}

	r, err := m.ImportManualLinks("socks5://203.0.113.20:1080\nhttp://203.0.113.21:8080\n", "", "batch")
	if err != nil {
		t.Fatalf("ImportManualLinks() error = %v", err)
	}
	if r.Added != 2 {
		t.Fatalf("added=%d, want 2; result=%+v", r.Added, r)
	}
	for _, addr := range []string{"203.0.113.20:1080", "203.0.113.21:8080"} {
		proxy, err := store.GetProxyByIdentity(addr, storage.SourceManual, 0)
		if err != nil {
			t.Fatalf("GetProxyByIdentity(%s) error = %v", addr, err)
		}
		if proxy.Status != "disabled" {
			t.Fatalf("%s status = %q, want disabled until validation", addr, proxy.Status)
		}
	}
	if _, err := store.GetRandom(); err == nil {
		t.Fatal("unvalidated batch-import nodes entered active selection")
	}
}
