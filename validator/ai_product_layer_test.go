package validator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestOpenAIProductLayerRegionalBlock 吸收缝合怪 ChatGPT 检测：
// compliance/cookie_requirements 出现 unsupported_country → 不可达。
func TestOpenAIProductLayerRegionalBlock(t *testing.T) {
	apiOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"missing api key"}`))
	}))
	defer apiOK.Close()
	productBlocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"error":{"code":"unsupported_country","message":"Country not supported"}}`))
	}))
	defer productBlocked.Close()

	oldTargets := aiProbeTargets
	oldExtra := aiProductProbeTargets
	defer func() {
		aiProbeTargets = oldTargets
		aiProductProbeTargets = oldExtra
	}()
	aiProbeTargets = map[string]string{"openai": apiOK.URL, "claude": apiOK.URL, "grok": apiOK.URL, "gemini": apiOK.URL}
	aiProductProbeTargets = map[string][]string{"openai": {productBlocked.URL}}

	client := &http.Client{Timeout: 2 * time.Second}
	got := probeAIReachability(client)
	var m map[string]int
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatal(err)
	}
	if m["openai"] != 1 {
		t.Fatalf("openai product regional block = %d, want 1 (full=%s)", m["openai"], got)
	}
}

// TestClaudeProductLayerAppUnavailableInRegion 吸收缝合怪 Claude 检测：
// 最终落到 app-unavailable-in-region → 不可达。
func TestClaudeProductLayerAppUnavailableInRegion(t *testing.T) {
	apiOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error"}}`))
	}))
	defer apiOK.Close()
	// 同域重定向到含 unavailable-in-region 的路径，模拟官方地区不可用页最终 URL。
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "app-unavailable-in-region") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<html>unavailable in your region</html>`))
			return
		}
		http.Redirect(w, r, "/app-unavailable-in-region", http.StatusFound)
	}))
	defer blocked.Close()

	oldTargets := aiProbeTargets
	oldExtra := aiProductProbeTargets
	defer func() {
		aiProbeTargets = oldTargets
		aiProductProbeTargets = oldExtra
	}()
	aiProbeTargets = map[string]string{"openai": apiOK.URL, "claude": apiOK.URL, "grok": apiOK.URL, "gemini": apiOK.URL}
	aiProductProbeTargets = map[string][]string{"claude": {blocked.URL}}

	// 自定义 client 跟随重定向，便于读最终 URL 语义；探测函数内部会处理。
	client := &http.Client{Timeout: 2 * time.Second}
	// 直接测产品层分类
	if got := probeOneAIProductLayer(client, "claude", blocked.URL); got != 1 {
		t.Fatalf("claude product unavailable region = %d, want 1", got)
	}
	got := probeAIReachability(client)
	var m map[string]int
	_ = json.Unmarshal([]byte(got), &m)
	if m["claude"] != 1 {
		t.Fatalf("claude merge = %d, want product block to win over API 401 (full=%s)", m["claude"], got)
	}
}

// TestGeminiProductLayerUnlockFingerprint 吸收缝合怪 Gemini：body 含 45631641,null,true 记可达辅证。
func TestGeminiProductLayerUnlockFingerprint(t *testing.T) {
	apiUnknown := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limit"}`))
	}))
	defer apiUnknown.Close()
	productOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`window.WIZ_global_data = [[45631641,null,true],"US"];`))
	}))
	defer productOK.Close()

	oldTargets := aiProbeTargets
	oldExtra := aiProductProbeTargets
	defer func() {
		aiProbeTargets = oldTargets
		aiProductProbeTargets = oldExtra
	}()
	aiProbeTargets = map[string]string{"openai": apiUnknown.URL, "claude": apiUnknown.URL, "grok": apiUnknown.URL, "gemini": apiUnknown.URL}
	aiProductProbeTargets = map[string][]string{"gemini": {productOK.URL}}

	client := &http.Client{Timeout: 2 * time.Second}
	got := probeAIReachability(client)
	var m map[string]int
	_ = json.Unmarshal([]byte(got), &m)
	if m["gemini"] != 0 {
		t.Fatalf("gemini product unlock fingerprint = %d, want 0 (full=%s)", m["gemini"], got)
	}
}

// TestMergePrefersExplicitBlockOverAPIAuthOK：产品层明确封禁压过 API 401 假“可达”。
func TestMergePrefersExplicitBlockOverAPIAuthOK(t *testing.T) {
	if got := mergeAIProbeResults(0, 1); got != 1 {
		t.Fatalf("merge(api=0,product=1)=%d want 1", got)
	}
	if got := mergeAIProbeResults(1, 0); got != 1 {
		t.Fatalf("merge(api=1,product=0)=%d want 1", got)
	}
	if got := mergeAIProbeResults(0, -1); got != 0 {
		t.Fatalf("merge(api=0,product=-1)=%d want 0", got)
	}
	if got := mergeAIProbeResults(-1, 0); got != 0 {
		t.Fatalf("merge(api=-1,product=0)=%d want 0", got)
	}
	if got := mergeAIProbeResults(-1, -1); got != -1 {
		t.Fatalf("merge(-1,-1)=%d want -1", got)
	}
}

// TestAIProductTargetsConfiguredForCoreServices 只配置 AI 产品层，不引入流媒体。
func TestAIProductTargetsConfiguredForCoreServices(t *testing.T) {
	for _, svc := range []string{"openai", "claude", "gemini"} {
		if len(aiProductProbeTargets[svc]) == 0 {
			t.Fatalf("missing product-layer probes for %s", svc)
		}
	}
	// 不要求 grok 产品层（缝合怪也无可靠 grok 产品探测时允许仅 API）。
	for svc, urls := range aiProductProbeTargets {
		for _, u := range urls {
			if strings.Contains(u, "netflix") || strings.Contains(u, "disney") || strings.Contains(u, "bbc") {
				t.Fatalf("must not include non-AI unlock probes: %s %s", svc, u)
			}
		}
	}
}

// TestClaudeGenericCloudflareChallengeDoesNotOverrideReachableAPI 回归保护：
// 产品层 claude.ai 对几乎所有数据中心/代理 IP 下发通用 Cloudflare 反爬挑战
// （cf-mitigated: challenge / "Just a moment" / error code: 1020），这是与地区无关
// 的机器人拦截，不得判为地域封禁。API 主信号 401（认证错误=可达）应最终胜出，
// 否则会把可用节点误判为 Claude 全红（真实故障复盘）。
func TestClaudeGenericCloudflareChallengeDoesNotOverrideReachableAPI(t *testing.T) {
	// API 层：匿名 401 认证错误 = 可达。
	apiOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error"}}`))
	}))
	defer apiOK.Close()
	// 产品层：通用 CF 挑战（cf-mitigated 头 + "Just a moment" 正文），无任何地域指纹。
	cfChallenge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("cf-mitigated", "challenge")
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><title>Just a moment...</title></head><body>Attention Required! Cloudflare error code: 1020</body></html>`))
	}))
	defer cfChallenge.Close()

	oldTargets := aiProbeTargets
	oldExtra := aiProductProbeTargets
	defer func() {
		aiProbeTargets = oldTargets
		aiProductProbeTargets = oldExtra
	}()
	aiProbeTargets = map[string]string{"openai": apiOK.URL, "claude": apiOK.URL, "grok": apiOK.URL, "gemini": apiOK.URL}
	aiProductProbeTargets = map[string][]string{"claude": {cfChallenge.URL}}

	client := &http.Client{Timeout: 2 * time.Second}
	// 产品层单独分类：通用 CF 挑战 → -1（未知），不得为 1。
	if got := probeOneAIProductLayer(client, "claude", cfChallenge.URL); got != -1 {
		t.Fatalf("claude generic CF challenge product classify = %d, want -1 (unknown, not regional block)", got)
	}
	// 合并结果：API 401 可达应胜出。
	got := probeAIReachability(client)
	var m map[string]int
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatal(err)
	}
	if m["claude"] != 0 {
		t.Fatalf("claude merge = %d, want 0 (reachable API wins over generic CF challenge) (full=%s)", m["claude"], got)
	}
}
