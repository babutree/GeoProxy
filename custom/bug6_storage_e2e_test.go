package custom

import (
	"testing"

	"github.com/babutree/GeoProxy/storage"
)

// TestBug6StorageRoundTrip 用合成账密验证 http/socks 账密经 AddManualProxyWithCredentials
// 存库后, 能通过 GetAllForAdmin(WebUI /api/proxies 所用查询) 原样读回。绝不含真实密钥。
func TestBug6StorageRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.New(dir + "/test.db")
	if err != nil {
		t.Fatalf("storage.New error=%v", err)
	}
	defer store.Close()

	if err := store.AddManualProxyWithCredentials("1.2.3.4:3129", "http", "us", "note-a", "user-a", "pass-a"); err != nil {
		t.Fatalf("AddManualProxyWithCredentials error=%v", err)
	}

	proxies, err := store.GetAllForAdmin()
	if err != nil {
		t.Fatalf("GetAllForAdmin error=%v", err)
	}
	if len(proxies) != 1 {
		t.Fatalf("got %d proxies, want 1", len(proxies))
	}
	p := proxies[0]
	if p.Username != "user-a" || p.Password != "pass-a" {
		t.Fatalf("creds=(%q,%q) want (user-a,pass-a) — 凭据未存/读回", p.Username, p.Password)
	}
}
