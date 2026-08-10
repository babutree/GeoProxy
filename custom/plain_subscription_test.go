package custom

import "testing"

// format=plain 的订阅必须直达纯文本解析，不能错误地被当作 Base64 或 YAML。
func TestParsePlainSubscriptionAcceptsDirectProxyLines(t *testing.T) {
	data := []byte("socks5://alice:secret@socks.example:1080\n" +
		"http://http.example:8080\n" +
		"198.51.100.20:3128\n")

	nodes, err := Parse(data, "plain")
	if err != nil {
		t.Fatalf("Parse(plain) error = %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("nodes = %d, want 3", len(nodes))
	}
	if nodes[0].Type != "socks5" || nodes[0].Username != "alice" || nodes[0].Password != "secret" {
		t.Fatalf("socks node = %+v, want parsed credentials", nodes[0])
	}
	if nodes[1].Type != "http" || nodes[1].DirectAddress() != "http.example:8080" {
		t.Fatalf("http node = %+v, want http.example:8080", nodes[1])
	}
	if nodes[2].Type != "http" || nodes[2].DirectAddress() != "198.51.100.20:3128" {
		t.Fatalf("bare node = %+v, want HTTP default", nodes[2])
	}
}
