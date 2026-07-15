package validator

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestProbeAIReachability 覆盖 AI 地域解锁探测：真实 http 往返（httptest server，不 mock）。
// 语义：401、已知服务解锁响应 → 0；连接失败/超时 → 1；未知响应 → -1。
// 通过覆盖包级 aiProbeTargets 变量，把 4 个探测目标指向本地 httptest server，
// 其中：一个返回 401（缺 key，仍算解锁=0）、一个返回服务语义 200（解锁=0）、一个已关闭（连不通=1）。
func TestProbeAIReachability(t *testing.T) {
	// 可达（401）：能连通即说明地区不封，401 只是没 key。
	unauthorized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Missing bearer authentication"}}`))
	}))
	defer unauthorized.Close()
	// 可达（模型列表语义，API 200）。
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"grok-1"}]}`))
	}))
	defer ok.Close()
	// 不可达：先起再立刻关闭，令后续连接被拒（真实连接失败，非模拟）。
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	downURL := down.URL
	down.Close()

	old := aiProbeTargets
	aiProbeTargets = map[string]string{
		"openai": unauthorized.URL, // 401 → 可达 0
		"claude": downURL,          // 连不通 → 不可达 1
		"grok":   ok.URL,           // 模型列表 → 可达 0
		"gemini": unauthorized.URL, // 401 → 可达 0
	}
	defer func() { aiProbeTargets = old }()

	client := &http.Client{Timeout: 2 * time.Second}
	got := probeAIReachability(client)

	var m map[string]int
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatalf("probeAIReachability() returned invalid JSON %q: %v", got, err)
	}
	want := map[string]int{"openai": 0, "claude": 1, "grok": 0, "gemini": 0}
	for k, wv := range want {
		if m[k] != wv {
			t.Fatalf("probeAIReachability()[%q] = %d, want %d (full=%q)", k, m[k], wv, got)
		}
	}
	if len(m) != 4 {
		t.Fatalf("probeAIReachability() keys = %d, want 4 (full=%q)", len(m), got)
	}
}

func TestProbeOneAIDistinguishesForbiddenAndUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/forbidden" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Missing authentication"}}`))
	}))
	defer server.Close()

	client := &http.Client{Timeout: time.Second}
	if got := probeOneAI(client, server.URL+"/unauthorized"); got != 0 {
		t.Fatalf("probeOneAI(401) = %d, want reachable (0)", got)
	}
	if got := probeOneAI(client, server.URL+"/forbidden"); got != -1 {
		t.Fatalf("probeOneAI(ambiguous 403) = %d, want unprobed (-1)", got)
	}
}

// TestUnknownRiskAIReachabilityEmpty 验证零信息风险的 AIReachability 为空串（整体未探测）。
func TestUnknownRiskAIReachabilityEmpty(t *testing.T) {
	if r := UnknownRisk(); r.AIReachability != "" {
		t.Fatalf("UnknownRisk().AIReachability = %q, want empty", r.AIReachability)
	}
}

// TestProbeOneAIGemini403PermissionDeniedReachable 验证 Gemini 风格的 403 PERMISSION_DENIED
// （缺 API key 的正常 Google 语义）应判为可达(0)：能连通 Google，只是没带 key。
func TestProbeOneAIGemini403PermissionDenied(t *testing.T) {
	// Google generativelanguage 无 key 时返回的典型响应体。
	const geminiBody = `{
  "error": {
    "code": 403,
    "message": "Method doesn't allow unregistered callers (callers without established identity). Please use API Key or other form of API consumer identity to call this API.",
    "status": "PERMISSION_DENIED"
  }
}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(geminiBody))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	if got := probeOneAI(client, server.URL); got != 0 {
		t.Fatalf("probeOneAI(Gemini 403 PERMISSION_DENIED) = %d, want reachable (0)", got)
	}
}

// TestProbeOneAIUnauthorizedReachable 验证 openai/claude/grok 风格的 401 缺 key 仍判为可达(0)。
func TestProbeOneAIUnauthorizedReachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Missing bearer or basic authentication in header","code":"missing_auth"}}`))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	if got := probeOneAI(client, server.URL); got != 0 {
		t.Fatalf("probeOneAI(401 missing key) = %d, want reachable (0)", got)
	}
}

// TestProbeOneAIServiceSemanticUnlockedResponse 验证服务响应语义足够明确时才判为解锁(0)，
// 避免只因 HTTP 200 就把任意官网/改版页面静默判成解锁。
func TestProbeOneAIServiceSemanticUnlockedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4o","object":"model"}]}`))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	if got := probeOneAI(client, server.URL); got != 0 {
		t.Fatalf("probeOneAI(service semantic unlocked response) = %d, want unlocked (0)", got)
	}
}

// TestProbeOneAIRegionalBlockResponseUnreachable 验证明确地域封禁响应判为封禁(1)，
// 即使状态码不是旧实现唯一特殊处理的 403，也不能因拿到 HTTP 响应而判成解锁。
func TestProbeOneAIRegionalBlockResponseUnreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnavailableForLegalReasons)
		_, _ = w.Write([]byte(`{"error":{"code":"unsupported_country_region_territory","message":"Service is not available in your country or region."}}`))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	if got := probeOneAI(client, server.URL); got != 1 {
		t.Fatalf("probeOneAI(regional block response) = %d, want blocked (1)", got)
	}
}

// TestProbeOneAIUnknownRedesignedResponseUnprobed 验证未知或官网改版响应不应静默全绿。
func TestProbeOneAIUnknownRedesignedResponseUnprobed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>New Product</title></head><body>Welcome</body></html>`))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	if got := probeOneAI(client, server.URL); got != -1 {
		t.Fatalf("probeOneAI(unknown redesigned response) = %d, want unprobed (-1)", got)
	}
}

// TestProbeOneAICloudflareBlock403Unreachable 验证真实边缘/CF 拦截的 403（响应体含 CF 挑战信号，
// 或含 cf-mitigated 头）判为不可达(1)：不是 Google 缺 key，而是被拦。
func TestProbeOneAICloudflareBlock403Unreachable(t *testing.T) {
	// 用例 A：CF 挑战页正文（"Just a moment" / "error code: 1020"），无 Google 权限信号。
	cfBody := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><title>Just a moment...</title></head><body>Attention Required! Cloudflare error code: 1020</body></html>`))
	}))
	defer cfBody.Close()

	// 用例 B：403 + cf-mitigated 头（正文无 Google 权限信号）。
	cfHeader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("cf-mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer cfHeader.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	if got := probeOneAI(client, cfBody.URL); got != 1 {
		t.Fatalf("probeOneAI(CF block 403 body) = %d, want unreachable (1)", got)
	}
	if got := probeOneAI(client, cfHeader.URL); got != 1 {
		t.Fatalf("probeOneAI(CF block 403 cf-mitigated) = %d, want unreachable (1)", got)
	}
}

// TestProbeOneAIForbiddenNoKnownSignalUnprobed 验证裸 403 不足以证明地域封禁。
func TestProbeOneAIForbiddenNoKnownSignalUnprobed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`forbidden`))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	if got := probeOneAI(client, server.URL); got != -1 {
		t.Fatalf("probeOneAI(bare 403) = %d, want unprobed (-1)", got)
	}
}

// TestProbeOneAIConnectionFailureUnreachable 验证连接失败（dial 已关闭端口）判为不可达(1)。
func TestProbeOneAIConnectionFailureUnreachable(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	downURL := down.URL
	down.Close() // 立刻关闭：后续连接被真实拒绝。

	client := &http.Client{Timeout: 2 * time.Second}
	if got := probeOneAI(client, downURL); got != 1 {
		t.Fatalf("probeOneAI(connection failure) = %d, want unreachable (1)", got)
	}
}

// TestProbeAIReachabilityServiceSemanticMatrix 覆盖稳定 API 端点三态：
// 401/缺 key 或模型列表 = 0；完整地域拒绝 = 1；普通文案不得误判为封禁。
func TestProbeAIReachabilityServiceSemanticMatrix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/unlocked/openai":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided","type":"invalid_request_error"}}`))
		case "/unlocked/claude":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"x-api-key header is required"}}`))
		case "/unlocked/grok":
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"grok-1"}]}`))
		case "/unlocked/gemini":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"message":"Method doesn't allow unregistered callers (callers without established identity). Please use API Key or other form of API consumer identity to call this API.","status":"PERMISSION_DENIED"}}`))
		case "/blocked/openai":
			_, _ = w.Write([]byte(`{"error":{"message":"Your country or region is not supported."}}`))
		case "/blocked/claude":
			_, _ = w.Write([]byte(`{"error":{"message":"This service is not available in your country or region."}}`))
		case "/blocked/grok":
			_, _ = w.Write([]byte(`{"error":{"message":"Grok is unavailable in your country or region."}}`))
		case "/blocked/gemini":
			_, _ = w.Write([]byte(`{"error":{"code":"unsupported_country_region_territory"}}`))
		case "/ordinary":
			_, _ = w.Write([]byte(`{"copy":"Choose your country or region to tailor privacy settings."}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	old := aiProbeTargets
	defer func() { aiProbeTargets = old }()
	client := &http.Client{Timeout: 2 * time.Second}

	aiProbeTargets = map[string]string{
		"openai": server.URL + "/unlocked/openai",
		"claude": server.URL + "/unlocked/claude",
		"grok":   server.URL + "/unlocked/grok",
		"gemini": server.URL + "/unlocked/gemini",
	}
	assertAIReachability(t, probeAIReachability(client), map[string]int{
		"openai": 0, "claude": 0, "grok": 0, "gemini": 0,
	})

	aiProbeTargets = map[string]string{
		"openai": server.URL + "/blocked/openai",
		"claude": server.URL + "/blocked/claude",
		"grok":   server.URL + "/blocked/grok",
		"gemini": server.URL + "/blocked/gemini",
	}
	assertAIReachability(t, probeAIReachability(client), map[string]int{
		"openai": 1, "claude": 1, "grok": 1, "gemini": 1,
	})

	if got := probeOneAI(client, server.URL+"/ordinary"); got != -1 {
		t.Fatalf("probeOneAI(ordinary country-or-region copy) = %d, want unprobed (-1)", got)
	}
}

func TestClassifyAIResponseRejectsAmbiguousSignals(t *testing.T) {
	tests := []struct {
		name    string
		service string
		status  int
		body    string
		want    int
	}{
		{
			name:    "country-only rejection overrides brand markers",
			service: "grok",
			status:  http.StatusOK,
			body:    `{"product":"grok","provider":"xai","message":"Grok is not available in your country."}`,
			want:    1,
		},
		{
			name:    "territory rejection",
			service: "claude",
			status:  http.StatusOK,
			body:    `{"product":"claude","page":"sign in","message":"This service is unavailable in your territory."}`,
			want:    1,
		},
		{
			name:    "region rejection",
			service: "gemini",
			status:  http.StatusOK,
			body:    `{"message":"Your region is restricted."}`,
			want:    1,
		},
		{
			name:    "rate limit with brand markers",
			service: "grok",
			status:  http.StatusTooManyRequests,
			body:    `{"product":"grok","provider":"xai","error":"rate limit exceeded"}`,
			want:    -1,
		},
		{
			name:    "ordinary attention text is not a Cloudflare block",
			service: "claude",
			status:  http.StatusOK,
			body:    `{"notice":"Attention Required to review account settings."}`,
			want:    -1, // 无解锁指纹也无拦截信号 → 未探测，不是不可达
		},
		{
			name:    "claude missing api key body is reachable",
			service: "claude",
			status:  http.StatusUnauthorized,
			body:    `{"type":"error","error":{"type":"authentication_error","message":"x-api-key header is required"}}`,
			want:    0,
		},
		{
			name:    "unauthorized status is not an IP block signal",
			service: "openai",
			status:  http.StatusUnauthorized,
			body:    `{"error":"unauthorized"}`,
			want:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyAIResponseForService(tt.service, tt.status, http.Header{}, tt.body); got != tt.want {
				t.Fatalf("classifyAIResponseForService(%q, %d) = %d, want %d", tt.service, tt.status, got, tt.want)
			}
		})
	}
}

func TestProbeOneAITruncatedUnlockedResponseIsUnprobed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"product":"grok","provider":"xai"}` + strings.Repeat(" ", 64<<10) + `Grok is not available in your country.`))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	if got := probeOneAIForService(client, "grok", server.URL); got != -1 {
		t.Fatalf("probeOneAIForService(truncated response) = %d, want unprobed (-1)", got)
	}
}

func TestProbeOneAIUnauthorizedReadFailureIsReachable(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     make(http.Header),
			Body:       failingReadCloser{},
		}, nil
	})}

	if got := probeOneAIForService(client, "openai", "https://example.invalid"); got != 0 {
		t.Fatalf("probeOneAIForService(401 read failure) = %d, want reachable (0)", got)
	}
}

func assertAIReachability(t *testing.T, raw string, want map[string]int) {
	t.Helper()
	var got map[string]int
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("probeAIReachability() returned invalid JSON %q: %v", raw, err)
	}
	for service, wantState := range want {
		if got[service] != wantState {
			t.Fatalf("probeAIReachability()[%q] = %d, want %d (full=%q)", service, got[service], wantState, raw)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type failingReadCloser struct{}

func (failingReadCloser) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func (failingReadCloser) Close() error {
	return nil
}

var _ io.ReadCloser = failingReadCloser{}
