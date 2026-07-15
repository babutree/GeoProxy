package custom

import (
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// allowLocalSubscriptionFetch 仅测试用：放行指定 httptest URL，并改用默认拨号，
// 以便绕过生产 SSRF 校验对 loopback 的拒绝。清理钩子保证用例结束后恢复。
func allowLocalSubscriptionFetch(t *testing.T, allowedURL string) {
	t.Helper()
	oldCheck := subscriptionURLTargetCheck
	oldDial := subscriptionDialContextFn
	oldSleep := subscriptionFetchSleepFn
	t.Cleanup(func() {
		subscriptionURLTargetCheck = oldCheck
		subscriptionDialContextFn = oldDial
		subscriptionFetchSleepFn = oldSleep
	})
	subscriptionURLTargetCheck = func(urlStr string) error {
		if urlStr == allowedURL {
			return nil
		}
		return validateSubscriptionURLTarget(urlStr)
	}
	subscriptionDialContextFn = (&net.Dialer{}).DialContext
	// 重试退避在测试中立即返回，避免拖慢套件。
	subscriptionFetchSleepFn = func(time.Duration) {}
}

// TestBUGFIX022_FetchSubscriptionURL_HTTP530IncludesBodySnippet RED/GREEN：
// 非 200（如 Cloudflare 530）错误必须含状态码与响应体片段，便于判断 CF/源站问题。
func TestBUGFIX022_FetchSubscriptionURL_HTTP530IncludesBodySnippet(t *testing.T) {
	const body = "error code: 530\ncloudflare origin error"
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		// 断言请求未走任何代理相关头伪装路径之外的异常；仍应是直连 GET。
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		w.WriteHeader(530)
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	allowLocalSubscriptionFetch(t, srv.URL)
	m := &Manager{}
	_, err := m.fetchSubscriptionURL(srv.URL, "")
	if err == nil {
		t.Fatal("fetchSubscriptionURL error = nil, want HTTP 530 diagnostic error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "530") {
		t.Fatalf("error = %q, want status 530", msg)
	}
	if !strings.Contains(msg, "error code: 530") {
		t.Fatalf("error = %q, want body snippet containing %q", msg, "error code: 530")
	}
	if !strings.Contains(msg, "直接拉取") {
		t.Fatalf("error = %q, want 直接拉取 wording (no node-proxy fallback path)", msg)
	}
	if hits.Load() < 1 {
		t.Fatal("server was not contacted; expected direct fetch")
	}
}

// TestBUGFIX022_FetchSubscriptionURL_200OKStillWorks 200 路径不得因诊断改动而破坏。
func TestBUGFIX022_FetchSubscriptionURL_200OKStillWorks(t *testing.T) {
	const payload = "ss://ok-node-payload"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, payload)
	}))
	defer srv.Close()

	allowLocalSubscriptionFetch(t, srv.URL)
	m := &Manager{}
	got, err := m.fetchSubscriptionURL(srv.URL, "")
	if err != nil {
		t.Fatalf("fetchSubscriptionURL() error = %v", err)
	}
	if string(got) != payload {
		t.Fatalf("body = %q, want %q", got, payload)
	}
}

// TestBUGFIX022_FetchSubscriptionURL_EmptyBodyNon200HasStatus 空 body 的非 200 仍须带状态码。
func TestBUGFIX022_FetchSubscriptionURL_EmptyBodyNon200HasStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	allowLocalSubscriptionFetch(t, srv.URL)
	m := &Manager{}
	_, err := m.fetchSubscriptionURL(srv.URL, "")
	if err == nil {
		t.Fatal("error = nil, want HTTP 403 error")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Fatalf("error = %q, want 403", err.Error())
	}
}

// TestBUGFIX022_FetchSubscriptionURL_LongBodyTruncated 超长 body 截断到 N 字节并标明截断。
func TestBUGFIX022_FetchSubscriptionURL_LongBodyTruncated(t *testing.T) {
	long := strings.Repeat("A", subscriptionResponseSnippetMaxBytes+200)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, long)
	}))
	defer srv.Close()

	allowLocalSubscriptionFetch(t, srv.URL)
	m := &Manager{}
	_, err := m.fetchSubscriptionURL(srv.URL, "")
	if err == nil {
		t.Fatal("error = nil, want truncated diagnostic")
	}
	msg := err.Error()
	if !strings.Contains(msg, "502") {
		t.Fatalf("error = %q, want 502", msg)
	}
	if !strings.Contains(msg, "已截断") {
		t.Fatalf("error = %q, want truncation marker 已截断", msg)
	}
	// 完整超长 body 不得整段进入错误。
	if strings.Contains(msg, long) {
		t.Fatal("error contains full long body; must truncate")
	}
	// 片段应含前缀 A，但总长明显短于原 body。
	if !strings.Contains(msg, "AAAA") {
		t.Fatalf("error = %q, want truncated body prefix", msg)
	}
}

// TestBUGFIX022_SanitizeSubscriptionResponseSnippet_RedactsSecrets 响应片段脱敏常见密钥形态。
func TestBUGFIX022_SanitizeSubscriptionResponseSnippet_RedactsSecrets(t *testing.T) {
	raw := []byte(`token=super-secret-token Bearer abc.def.ghi password=hunter2 api_key=sk-live-xyz {"access_token":"json-secret"} password: yaml-secret Cookie: session=cookie-secret http://user:pass@proxy.example:8080 ss://encoded-secret`)
	got := sanitizeSubscriptionResponseSnippet(raw)
	if got == "" {
		t.Fatal("snippet empty")
	}
	for _, secret := range []string{"super-secret-token", "abc.def.ghi", "hunter2", "sk-live-xyz", "json-secret", "yaml-secret", "cookie-secret", "user:pass", "encoded-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("snippet still contains secret %q: %q", secret, got)
		}
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("snippet = %q, want [REDACTED] markers", got)
	}
}

func TestBUGFIX022_SanitizeSubscriptionResponseSnippet_RedactsBase64Subscription(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString([]byte("vmess://credential-bearing-node"))
	got := sanitizeSubscriptionResponseSnippet([]byte(raw))
	if strings.Contains(got, raw) || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("base64 subscription snippet was not redacted: %q", got)
	}
}

func TestReadSubscriptionResponseRejectsOversizeBody(t *testing.T) {
	_, err := readSubscriptionResponse(strings.NewReader("123456"), 5)
	if err == nil || !strings.Contains(err.Error(), "超过") {
		t.Fatalf("readSubscriptionResponse oversize error = %v", err)
	}
	got, err := readSubscriptionResponse(strings.NewReader("12345"), 5)
	if err != nil || string(got) != "12345" {
		t.Fatalf("readSubscriptionResponse exact limit = %q, %v", got, err)
	}
}

// TestBUGFIX022_FetchSubscriptionURL_Retries5xxOnce 5xx 允许有限即时重试；成功则返回 body。
func TestBUGFIX022_FetchSubscriptionURL_Retries5xxOnce(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "temporary")
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "recovered-payload")
	}))
	defer srv.Close()

	allowLocalSubscriptionFetch(t, srv.URL)
	m := &Manager{}
	got, err := m.fetchSubscriptionURL(srv.URL, "")
	if err != nil {
		t.Fatalf("after 5xx retry want success, got err=%v", err)
	}
	if string(got) != "recovered-payload" {
		t.Fatalf("body = %q, want recovered-payload", got)
	}
	if hits.Load() != 2 {
		t.Fatalf("hits = %d, want 2 (1 fail + 1 retry)", hits.Load())
	}
}

// TestBUGFIX022_FetchSubscriptionURL_NoRetryOnClientError 4xx（非 429）不重试。
func TestBUGFIX022_FetchSubscriptionURL_NoRetryOnClientError(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "nope")
	}))
	defer srv.Close()

	allowLocalSubscriptionFetch(t, srv.URL)
	m := &Manager{}
	_, err := m.fetchSubscriptionURL(srv.URL, "")
	if err == nil {
		t.Fatal("want 401 error")
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1 (no retry on 401)", hits.Load())
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error = %q, want 401 + body snippet", err.Error())
	}
}

// TestBUGFIX022_FetchSubscriptionURL_RetryUpperBound 持续 5xx 时尝试次数有明确上限。
func TestBUGFIX022_FetchSubscriptionURL_RetryUpperBound(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(530)
		_, _ = io.WriteString(w, "error code: 530")
	}))
	defer srv.Close()

	allowLocalSubscriptionFetch(t, srv.URL)
	m := &Manager{}
	_, err := m.fetchSubscriptionURL(srv.URL, "")
	if err == nil {
		t.Fatal("want error after exhausted retries")
	}
	if int(hits.Load()) != subscriptionFetchMaxAttempts {
		t.Fatalf("hits = %d, want max attempts %d", hits.Load(), subscriptionFetchMaxAttempts)
	}
	if !strings.Contains(err.Error(), "530") {
		t.Fatalf("error = %q, want 530", err.Error())
	}
}

// TestBUGFIX022_FetchSubscriptionURL_Retries429 429 属于可重试状态。
func TestBUGFIX022_FetchSubscriptionURL_Retries429(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, "rate limited")
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok-after-429")
	}))
	defer srv.Close()

	allowLocalSubscriptionFetch(t, srv.URL)
	m := &Manager{}
	got, err := m.fetchSubscriptionURL(srv.URL, "")
	if err != nil {
		t.Fatalf("want success after 429 retry: %v", err)
	}
	if string(got) != "ok-after-429" {
		t.Fatalf("body = %q", got)
	}
	if hits.Load() != 2 {
		t.Fatalf("hits = %d, want 2", hits.Load())
	}
}

// TestBUGFIX022_NoNodeProxyFallbackPath 代码路径必须保持“直接拉取”语义：
// 错误信息不得暗示经上游节点回源；Manager 无 sing-box/storage 时仍只做直连失败。
func TestBUGFIX022_NoNodeProxyFallbackPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(530)
		_, _ = io.WriteString(w, "error code: 530")
	}))
	defer srv.Close()

	allowLocalSubscriptionFetch(t, srv.URL)
	// 故意不注入 storage/singbox，若存在节点兜底会 panic 或走别的路径。
	m := &Manager{}
	_, err := m.fetchSubscriptionURL(srv.URL, "")
	if err == nil {
		t.Fatal("want explicit direct-fetch failure")
	}
	msg := err.Error()
	for _, banned := range []string{"节点兜底", "via proxy", "上游节点", "fallback node"} {
		if strings.Contains(strings.ToLower(msg), strings.ToLower(banned)) {
			t.Fatalf("error suggests node-proxy fallback: %q", msg)
		}
	}
	if !strings.Contains(msg, "直接拉取") {
		t.Fatalf("error = %q, want 直接拉取", msg)
	}
}
