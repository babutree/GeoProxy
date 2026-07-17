package custom

import "testing"

// TestBug6CredentialParsePath 用合成账密验证 http/socks 账密从链接解析到 DirectCredentials 的全链路。
// 绝不含真实订阅/密钥。
func TestBug6CredentialParsePath(t *testing.T) {
	cases := []struct{ link, wantUser, wantPass, wantProto string }{
		{"http://u1:p1@1.2.3.4:3129", "u1", "p1", "http"},
		{"socks5://u2:p2@5.6.7.8:1080", "u2", "p2", "socks5"},
		{"http://1.2.3.4:8080", "", "", "http"},
	}
	for _, tc := range cases {
		node, err := ParseSingleLink(tc.link)
		if err != nil {
			t.Fatalf("ParseSingleLink(%q) error=%v", tc.link, err)
		}
		if !node.IsDirect() {
			t.Fatalf("%q: IsDirect=false, want true (type=%s)", tc.link, node.Type)
		}
		if node.DirectProtocol() != tc.wantProto {
			t.Fatalf("%q: proto=%s want %s", tc.link, node.DirectProtocol(), tc.wantProto)
		}
		u, p := node.DirectCredentials()
		if u != tc.wantUser || p != tc.wantPass {
			t.Fatalf("%q: creds=(%q,%q) want (%q,%q)", tc.link, u, p, tc.wantUser, tc.wantPass)
		}
	}
}
