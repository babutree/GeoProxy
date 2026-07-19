package storage

import "testing"

// TestCountAvailableByProtocolIncludesDualMixed 对齐节点列表：mixed 节点同时可当 HTTP/SOCKS5 入口。
func TestCountAvailableByProtocolIncludesDualMixed(t *testing.T) {
	store := newTestStorage(t)
	// 纯 http / 纯 socks5 / mixed(存为 socks5+dual)
	if err := store.AddManualProxy("10.0.0.1:8080", "http", "us", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.AddManualProxy("10.0.0.2:1080", "socks5", "us", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.AddManualProxy("127.0.0.1:31001", "socks5", "jp", ""); err != nil {
		t.Fatal(err)
	}
	// 手工默认 disabled，需启用才进 available 统计
	for _, addr := range []string{"10.0.0.1:8080", "10.0.0.2:1080", "127.0.0.1:31001"} {
		p, err := store.GetProxyByIdentity(addr, SourceManual, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.EnableProxyByID(p.ID); err != nil {
			t.Fatalf("EnableProxyByID(%s): %v", addr, err)
		}
		if addr == "127.0.0.1:31001" {
			if err := store.SetProxyDualProtocol(p.ID, true); err != nil {
				t.Fatalf("SetProxyDualProtocol: %v", err)
			}
		}
	}

	httpN, err := store.CountAvailableByProtocol("http")
	if err != nil {
		t.Fatal(err)
	}
	socksN, err := store.CountAvailableByProtocol("socks5")
	if err != nil {
		t.Fatal(err)
	}
	// http: pure http + dual mixed = 2
	// socks5: pure socks5 + dual mixed = 2
	if httpN != 2 {
		t.Fatalf("CountAvailableByProtocol(http)=%d, want 2 (1 pure http + 1 dual)", httpN)
	}
	if socksN != 2 {
		t.Fatalf("CountAvailableByProtocol(socks5)=%d, want 2 (1 pure socks5 + 1 dual)", socksN)
	}

	unknownN, err := store.CountAvailableByProtocol("trojan")
	if err != nil {
		t.Fatal(err)
	}
	if unknownN != 0 {
		t.Fatalf("CountAvailableByProtocol(trojan)=%d, want 0 (dual must not widen unknown protocols)", unknownN)
	}

	normalizedHTTPN, err := store.CountAvailableByProtocol(" HTTP ")
	if err != nil {
		t.Fatal(err)
	}
	if normalizedHTTPN != 2 {
		t.Fatalf("CountAvailableByProtocol(HTTP)=%d, want 2 after normalization", normalizedHTTPN)
	}
}
