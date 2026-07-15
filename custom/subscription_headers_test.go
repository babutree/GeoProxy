package custom

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestFetchSubscriptionURLCustomUserAgentOverridesDefault 当订阅 headers 含自定义 UA 时，
// 服务器实际收到的 User-Agent 必须是自定义值（真实 http 往返，不 mock）。
func TestFetchSubscriptionURLCustomUserAgentOverridesDefault(t *testing.T) {
	req, err := buildSubscriptionRequest("https://example.com/sub", `{"User-Agent":"clash.meta"}`)
	if err != nil {
		t.Fatalf("buildSubscriptionRequest() error = %v", err)
	}
	if got := req.Header.Get("User-Agent"); got != "clash.meta" {
		t.Fatalf("User-Agent = %q, want clash.meta", got)
	}
}

// TestBuildSubscriptionRequestRejectsInvalidHeadersJSON 非法 headers 必须显式报错，
// 不得静默回退默认 UA（否则添加/校验路径会把坏配置当成功）。
func TestBuildSubscriptionRequestRejectsInvalidHeadersJSON(t *testing.T) {
	invalid := []string{
		`{not-json`,
		`["User-Agent","clash"]`,
		`{"User-Agent":123}`,
		`null`,
	}
	for _, headersJSON := range invalid {
		t.Run(headersJSON, func(t *testing.T) {
			req, err := buildSubscriptionRequest("https://example.com/sub", headersJSON)
			if err == nil {
				t.Fatalf("buildSubscriptionRequest(%q) error = nil, want invalid headers error; req UA=%q",
					headersJSON, req.Header.Get("User-Agent"))
			}
			if !strings.Contains(err.Error(), "headers") {
				t.Fatalf("error = %v, want headers-related message", err)
			}
		})
	}
}

// TestValidateSubscriptionHeadersEmptyOK 空 headers 合法（默认 UA 路径）。
// ValidateSubscriptionHeaders 由 TODO #8 引入；在其落地前用 buildSubscriptionRequest 校验空值路径。
func TestValidateSubscriptionHeadersEmptyOK(t *testing.T) {
	req, err := buildSubscriptionRequest("https://example.com/sub", "")
	if err != nil {
		t.Fatalf("empty headers must be valid: %v", err)
	}
	if got := req.Header.Get("User-Agent"); got != "v2rayN" {
		t.Fatalf("User-Agent = %q, want default v2rayN", got)
	}
}

// TestFetchSubscriptionURLEmptyHeadersKeepsDefaultUA 向后兼容：headers 为空时，
// 服务器必须收到默认 v2rayN UA（不许破坏现有订阅拉取）。
func TestFetchSubscriptionURLEmptyHeadersKeepsDefaultUA(t *testing.T) {
	req, err := buildSubscriptionRequest("https://example.com/sub", "")
	if err != nil {
		t.Fatalf("buildSubscriptionRequest() error = %v", err)
	}
	if got := req.Header.Get("User-Agent"); got != "v2rayN" {
		t.Fatalf("User-Agent = %q, want default v2rayN", got)
	}
}

// TestFetchSubscriptionURLHeadersWithoutUAKeepsDefaultUA 向后兼容边界：headers 非空但不含
// User-Agent 时，自定义头照常发送，同时保留默认 v2rayN UA（自定义头覆盖默认，未指定则保留）。
func TestFetchSubscriptionURLHeadersWithoutUAKeepsDefaultUA(t *testing.T) {
	req, err := buildSubscriptionRequest("https://example.com/sub", `{"Authorization":"Bearer abc"}`)
	if err != nil {
		t.Fatalf("buildSubscriptionRequest() error = %v", err)
	}
	if got := req.Header.Get("User-Agent"); got != "v2rayN" {
		t.Fatalf("User-Agent = %q, want default v2rayN (UA not overridden)", got)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer abc" {
		t.Fatalf("Authorization = %q, want Bearer abc", got)
	}
}

// TestFetchSubscriptionURLSendsCustomAuthorization 自定义 Authorization 头被正确发送
// （真实 http 往返验证）。
func TestFetchSubscriptionURLSendsCustomAuthorization(t *testing.T) {
	req, err := buildSubscriptionRequest("https://example.com/sub", `{"User-Agent":"clash","Authorization":"Bearer xyz"}`)
	if err != nil {
		t.Fatalf("buildSubscriptionRequest() error = %v", err)
	}
	if got := req.Header.Get("User-Agent"); got != "clash" {
		t.Fatalf("User-Agent = %q, want clash", got)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer xyz" {
		t.Fatalf("Authorization = %q, want Bearer xyz", got)
	}
}

// TestFetchSubscriptionURLRejectsLocalTargets 覆盖订阅 URL SSRF：URL 指向本机/内网时
// 必须在发起请求前拒绝，不能让订阅刷新器成为访问本机服务的直连客户端。
func TestFetchSubscriptionURLRejectsLocalTargets(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("should-not-fetch"))
	}))
	defer srv.Close()

	m := &Manager{}
	_, err := m.fetchSubscriptionURL(srv.URL, "")
	if err == nil {
		t.Fatal("fetchSubscriptionURL(localhost) error = nil, want unsafe-target error")
	}
	if !strings.Contains(err.Error(), "unsafe subscription URL") {
		t.Fatalf("error = %v, want unsafe subscription URL", err)
	}
	if hit {
		t.Fatal("local subscription server was contacted before SSRF rejection")
	}
}

func TestFetchSubscriptionURLRejectsMetadataTarget(t *testing.T) {
	m := &Manager{}
	_, err := m.fetchSubscriptionURL("http://169.254.169.254/latest/meta-data/", "")
	if err == nil {
		t.Fatal("fetchSubscriptionURL(metadata) error = nil, want unsafe-target error")
	}
	if !strings.Contains(err.Error(), "unsafe subscription URL") {
		t.Fatalf("error = %v, want unsafe subscription URL", err)
	}
}

func TestValidateSubscriptionURLTargetRejectsUnsafeAddressClasses(t *testing.T) {
	unsafeURLs := []string{
		"file:///etc/passwd",
		"http://127.0.0.1/sub",
		"http://10.0.0.1/sub",
		"http://100.100.100.200/sub",
		"http://169.254.1.1/sub",
		"http://192.0.2.1/sub",
		"http://198.18.0.1/sub",
		"http://240.0.0.1/sub",
		"http://[::1]/sub",
		"http://[fc00::1]/sub",
		"http://[fe80::1]/sub",
		"http://[2001:db8::1]/sub",
		"http://0.0.0.0/sub",
		"http://224.0.0.1/sub",
	}
	for _, rawURL := range unsafeURLs {
		t.Run(rawURL, func(t *testing.T) {
			if err := validateSubscriptionURLTarget(rawURL); err == nil {
				t.Fatalf("validateSubscriptionURLTarget(%q) error = nil, want rejection", rawURL)
			}
		})
	}
}

func TestValidateSubscriptionURLTargetAllowsPublicLiteral(t *testing.T) {
	if err := validateSubscriptionURLTarget("https://1.1.1.1/sub"); err != nil {
		t.Fatalf("public subscription target rejected: %v", err)
	}
}
