package custom

import (
	"strings"
	"testing"

	"github.com/babutree/GeoProxy/storage"
)

func TestImportManualLinksAddsDirectAndSkipsDuplicates(t *testing.T) {
	store := newTestStorage(t)
	m := &Manager{storage: store}

	text := strings.Join([]string{
		"socks5://1.1.1.1:1080 韩国 [首尔]",
		"http://2.2.2.2:8080",
		"https://3.3.3.3:443 note",
		"socks5://1.1.1.1:1080", // duplicate in batch
		"",
		"# comment",
		"trojan://x@bad.example.com:443#no",
	}, "\n")

	r, err := m.ImportManualLinks(text, "us", "batch")
	if err != nil {
		t.Fatalf("ImportManualLinks: %v", err)
	}
	if r.Added != 3 {
		t.Fatalf("added=%d, want 3; result=%+v", r.Added, r)
	}
	if r.Skipped < 1 {
		t.Fatalf("skipped=%d, want >=1 for in-batch dup", r.Skipped)
	}
	if r.Failed < 1 {
		t.Fatalf("failed=%d, want >=1 for tunnel link", r.Failed)
	}

	// Second import of same set should skip all existing.
	r2, err := m.ImportManualLinks("socks5://1.1.1.1:1080\nhttp://2.2.2.2:8080\n", "", "")
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if r2.Added != 0 || r2.Skipped < 2 {
		t.Fatalf("second import result=%+v, want added=0 skipped>=2", r2)
	}

	p, err := store.GetProxyByAddress("1.1.1.1:1080")
	if err != nil {
		t.Fatalf("GetProxyByAddress: %v", err)
	}
	if p.Source != storage.SourceManual || p.Protocol != "socks5" {
		t.Fatalf("proxy=%+v", p)
	}
	if p.Region != "us" || p.Note != "batch" {
		t.Fatalf("region/note=%q/%q", p.Region, p.Note)
	}
}

func TestImportManualLinksExtractsURLWithLeadingOrInlineAnnotation(t *testing.T) {
	store := newTestStorage(t)
	m := &Manager{storage: store}

	text := strings.Join([]string{
		"韩国 [首尔] socks5://10.0.0.1:1080",
		"备注：http://10.0.0.2:8080 可用",
		"  https://10.0.0.3:443 入库:2026-07-12",
		"prefix socks5://10.0.0.4:1080 suffix more text",
		"not-a-proxy-line only chinese 文字",
	}, "\n")

	r, err := m.ImportManualLinks(text, "kr", "anno")
	if err != nil {
		t.Fatalf("ImportManualLinks: %v", err)
	}
	if r.Added != 4 {
		t.Fatalf("added=%d, want 4; result=%+v", r.Added, r)
	}
	if r.Failed < 1 {
		t.Fatalf("failed=%d, want >=1 for non-proxy line", r.Failed)
	}
	for _, addr := range []string{"10.0.0.1:1080", "10.0.0.2:8080", "10.0.0.3:443", "10.0.0.4:1080"} {
		if _, err := store.GetProxyByAddress(addr); err != nil {
			t.Fatalf("missing imported %s: %v", addr, err)
		}
	}
	p, err := store.GetProxyByAddress("10.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	if p.Protocol != "socks5" {
		t.Fatalf("protocol=%q, want socks5", p.Protocol)
	}
}

func TestImportManualLinksSupportsUserinfoWithoutStoringCredentials(t *testing.T) {
	store := newTestStorage(t)
	m := &Manager{storage: store}

	text := strings.Join([]string{
		"socks5://user:pass@10.1.0.1:1080",
		"http://user:pass@10.1.0.2:8080",
	}, "\n")
	r, err := m.ImportManualLinks(text, "", "")
	if err != nil {
		t.Fatalf("ImportManualLinks: %v", err)
	}
	if r.Added != 2 || r.Failed != 0 {
		t.Fatalf("result=%+v, want added=2 failed=0", r)
	}

	checks := map[string]string{
		"10.1.0.1:1080": "socks5",
		"10.1.0.2:8080": "http",
	}
	for addr, protocol := range checks {
		p, err := store.GetProxyByAddress(addr)
		if err != nil {
			t.Fatalf("missing imported %s: %v", addr, err)
		}
		if p.Protocol != protocol {
			t.Fatalf("%s protocol=%q, want %q", addr, p.Protocol, protocol)
		}
	}
	for _, leaked := range []string{"user", "pass", "@"} {
		for _, addr := range r.AddedAddrs {
			if strings.Contains(addr, leaked) {
				t.Fatalf("added address %q leaked credential fragment %q", addr, leaked)
			}
		}
	}
}

func TestExtractProxyLinkFromLine(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"socks5://1.1.1.1:1080 韩国", "socks5://1.1.1.1:1080"},
		{"韩国 socks5://1.1.1.1:1080", "socks5://1.1.1.1:1080"},
		{"note: http://2.2.2.2:8080 ok", "http://2.2.2.2:8080"},
		{"  https://3.3.3.3:443 ", "https://3.3.3.3:443"},
		{"socks5://user:pass@4.4.4.4:1080 备注", "socks5://user:pass@4.4.4.4:1080"},
		{"socks5://5.5.5.5:1080　全角空格备注", "socks5://5.5.5.5:1080"},
		{"# full line comment", ""},
		{"只有中文没有代理", ""},
	}
	for _, tc := range cases {
		got := extractProxyLinkFromLine(tc.in)
		if got != tc.want {
			t.Fatalf("extractProxyLinkFromLine(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDeleteManualNodesBatch(t *testing.T) {
	store := newTestStorage(t)
	m := &Manager{storage: store, singbox: newSpyShard()}
	if err := m.AddManualNode("http://9.9.9.1:8080", "", "a"); err != nil {
		t.Fatal(err)
	}
	if err := m.AddManualNode("http://9.9.9.2:8080", "", "b"); err != nil {
		t.Fatal(err)
	}
	a, _ := store.GetProxyByAddress("9.9.9.1:8080")
	b, _ := store.GetProxyByAddress("9.9.9.2:8080")
	deleted, errs := m.DeleteManualNodes([]int64{a.ID, b.ID, 999999})
	if deleted != 2 {
		t.Fatalf("deleted=%d, want 2; errs=%v", deleted, errs)
	}
	if len(errs) != 1 {
		t.Fatalf("errs=%v, want 1 failure for missing id", errs)
	}
}
