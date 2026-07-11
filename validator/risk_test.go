package validator

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestParseAbuserScore 覆盖 ipapi.is abuser_score 字符串解析：正常值、带标签、空、越界裁剪、非法。
func TestParseAbuserScore(t *testing.T) {
	cases := []struct {
		raw  string
		want float64
	}{
		{"0.0039 (Low)", 0.0039},
		{"0.85 (High)", 0.85},
		{"1", 1},
		{"0", 0},
		{"", 0},
		{"  0.5  ", 0.5},
		{"1.7", 1},       // 越界裁剪到 1
		{"-0.3", 0},      // 负值归零
		{"abc", 0},       // 非法归零
		{"(Unknown)", 0}, // 无前导数值
	}
	for _, c := range cases {
		if got := parseAbuserScore(c.raw); got != c.want {
			t.Fatalf("parseAbuserScore(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}

// TestIPAPIFlags 覆盖 ip-api 标记拼接：命中顺序固定 proxy,hosting,mobile；无命中为空串。
func TestIPAPIFlags(t *testing.T) {
	cases := []struct {
		proxy, hosting, mobile bool
		want                   string
	}{
		{false, false, false, ""},
		{true, false, false, "proxy"},
		{false, true, false, "hosting"},
		{false, false, true, "mobile"},
		{true, true, false, "proxy,hosting"},
		{true, true, true, "proxy,hosting,mobile"},
		{false, true, true, "hosting,mobile"},
	}
	for _, c := range cases {
		if got := ipapiFlags(c.proxy, c.hosting, c.mobile); got != c.want {
			t.Fatalf("ipapiFlags(%v,%v,%v) = %q, want %q", c.proxy, c.hosting, c.mobile, got, c.want)
		}
	}
}

// TestUnknownRisk 验证零信息风险信息：分数 -1、无标记。
func TestUnknownRisk(t *testing.T) {
	r := UnknownRisk()
	if r.IPAPIIsScore != IPAPIIsUnknown {
		t.Fatalf("UnknownRisk().IPAPIIsScore = %v, want %v", r.IPAPIIsScore, IPAPIIsUnknown)
	}
	if r.Flags != "" {
		t.Fatalf("UnknownRisk().Flags = %q, want empty", r.Flags)
	}
}

func TestQueryIPAPIIsRejectsNon2xxAndMissingScore(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{name: "http error json", status: http.StatusBadRequest, body: `{"error":"Invalid IP Address"}`},
		{name: "missing score", status: http.StatusOK, body: `{"is_abuser":true,"company":{}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer server.Close()

			client := server.Client()
			client.Transport = rewriteIPAPITransport{base: client.Transport, target: server.URL}
			if got := queryIPAPIIs(client, "203.0.113.1"); got.OK {
				t.Fatalf("queryIPAPIIs() OK = true for %s, want false", c.name)
			}
		})
	}
}

func TestQueryIPAPIIsAcceptsValidScore(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"is_abuser":true,"company":{"abuser_score":"0.42 (Medium)"}}`))
	}))
	defer server.Close()

	client := server.Client()
	client.Transport = rewriteIPAPITransport{base: client.Transport, target: server.URL}
	got := queryIPAPIIs(client, "203.0.113.1")
	if !got.OK || got.AbuserScore != 0.42 {
		t.Fatalf("queryIPAPIIs() = OK %v score %v, want true/0.42", got.OK, got.AbuserScore)
	}
}

// TestProbeCloudflareBlocked 覆盖 Cloudflare 拦截探测：403 / cf-mitigated 头 / 干净 200 / body 含 "Just a moment"。
// 用 rewriteIPAPITransport 把 client 请求打到测试服务器（复用同文件既有模式）。
func TestProbeCloudflareBlocked(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    int
	}{
		{
			name: "403 forbidden",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
			},
			want: 1,
		},
		{
			name: "cf-mitigated header",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("cf-mitigated", "challenge")
				w.WriteHeader(http.StatusOK)
			},
			want: 1,
		},
		{
			name: "clean 200",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("ip=203.0.113.1\nloc=US\n"))
			},
			want: 0,
		},
		{
			name: "body just a moment",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("<title>Just a moment...</title>"))
			},
			want: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server := httptest.NewTLSServer(c.handler)
			defer server.Close()

			client := server.Client()
			client.Transport = rewriteIPAPITransport{base: client.Transport, target: server.URL}
			if got := probeCloudflareBlocked(client); got != c.want {
				t.Fatalf("probeCloudflareBlocked() = %d, want %d for %s", got, c.want, c.name)
			}
		})
	}
}

type rewriteIPAPITransport struct {
	base   http.RoundTripper
	target string
}

func (t rewriteIPAPITransport) RoundTrip(req *http.Request) (*http.Response, error) {
	targetReq := req.Clone(req.Context())
	targetReq.URL.Scheme = "https"
	targetReq.URL.Host = strings.TrimPrefix(t.target, "https://")
	return t.base.RoundTrip(targetReq)
}
