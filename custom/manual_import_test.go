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

// TestImportManualLinksPersistsCredentialsWithoutLeaking 反映新契约（BUG-6）：
// 认证 http/socks5 节点的凭据必须被持久化（拨号需要），但绝不出现在
// 入库地址、错误串或结果字段中。凭据值仅存于 proxy_username/proxy_password 列。
func TestImportManualLinksPersistsCredentialsWithoutLeaking(t *testing.T) {
	store := newTestStorage(t)
	m := &Manager{storage: store}

	const secretUser = "u53rn4m3"
	const secretPass = "p4ssw0rd"
	text := strings.Join([]string{
		"socks5://" + secretUser + ":" + secretPass + "@10.1.0.1:1080",
		"http://" + secretUser + ":" + secretPass + "@10.1.0.2:8080",
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
		// 新契约：凭据被持久化以便拨号。
		if p.Username != secretUser || p.Password != secretPass {
			t.Fatalf("%s credentials not persisted (username/password mismatch)", addr)
		}
	}

	// 地址与结果字段绝不含凭据片段（避免日志/UI 泄漏）。
	for _, leaked := range []string{secretUser, secretPass, "@"} {
		for _, addr := range r.AddedAddrs {
			if strings.Contains(addr, leaked) {
				t.Fatalf("added address leaked credential fragment")
			}
		}
	}
	// 错误串（若有）也绝不含凭据。
	for _, e := range r.Errors {
		if strings.Contains(e, secretUser) || strings.Contains(e, secretPass) {
			t.Fatalf("error string leaked credential fragment")
		}
	}
}

// TestAuthenticatedNodesRoundTripCredentialsToValidatorClient 验证认证 socks5/http
// 节点的凭据从 parse→store→validator 客户端构建全程贯通（离线，不发起真实网络）。
func TestAuthenticatedNodesRoundTripCredentialsToValidatorClient(t *testing.T) {
	const secretUser = "dialer"
	const secretPass = "s3cr3t"

	for _, link := range []string{
		"socks5://" + secretUser + ":" + secretPass + "@10.2.0.1:1080",
		"http://" + secretUser + ":" + secretPass + "@10.2.0.2:8080",
	} {
		node, err := ParseSingleLink(link)
		if err != nil {
			t.Fatalf("ParseSingleLink error = %v", err)
		}
		if !node.IsDirect() {
			t.Fatalf("expected direct node for %s", node.DirectProtocol())
		}
		u, p := node.DirectCredentials()
		if u != secretUser || p != secretPass {
			t.Fatal("parsed credentials mismatch")
		}
		// DirectAddress 必须只含 host:port，无凭据。
		addr := node.DirectAddress()
		if strings.Contains(addr, secretUser) || strings.Contains(addr, secretPass) || strings.Contains(addr, "@") {
			t.Fatalf("DirectAddress leaked credentials")
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
