package validator

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAIProbeTargetsPreferStableServiceEndpoints 锁定探测目标：优先稳定服务端点，
// 避免依赖易改版的官网 HTML 文案指纹。
func TestAIProbeTargetsPreferStableServiceEndpoints(t *testing.T) {
	for service, target := range aiProbeTargets {
		if strings.Contains(target, "claude.ai/login") || strings.Contains(target, "grok.com/") || target == "https://gemini.google.com/" {
			t.Fatalf("service %s still probes brittle website %q; want stable service endpoint", service, target)
		}
	}
	for _, service := range []string{"openai", "claude", "grok", "gemini"} {
		if _, ok := aiProbeTargets[service]; !ok {
			t.Fatalf("missing probe target for %s", service)
		}
	}
}

// TestClassifyClaudeGrokGeminiUnauthorizedIsReachable：匿名 401 表示服务可达（缺 key），不得记为不可达。
func TestClassifyClaudeGrokGeminiUnauthorizedIsReachable(t *testing.T) {
	for _, service := range []string{"claude", "grok", "gemini"} {
		if got := classifyAIResponseForService(service, http.StatusUnauthorized, http.Header{}, `{"error":"missing api key"}`); got != 0 {
			t.Fatalf("%s 401 = %d, want reachable 0", service, got)
		}
	}
}

// TestProbeClaudeLikeAPIEndpointReachable 模拟 Anthropic/xAI/Google 风格 API：401/缺 key → 可达。
func TestProbeClaudeLikeAPIEndpointReachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"x-api-key header is required"}}`))
	}))
	defer server.Close()

	client := &http.Client{}
	for _, service := range []string{"claude", "grok", "gemini"} {
		if got := probeOneAIForService(client, service, server.URL); got != 0 {
			t.Fatalf("probeOneAIForService(%s) = %d, want 0", service, got)
		}
	}
}

// TestProbeWebsiteWithoutUnlockFingerprintIsUnprobedNotBlocked：
// 200 但无解锁指纹、也无拦截信号时，应记 -1（未探测），不要误记 1（不可达/X）。
func TestProbeWebsiteWithoutUnlockFingerprintIsUnprobedNotBlocked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><body><div id="root">marketing shell only</div></body></html>`))
	}))
	defer server.Close()

	client := &http.Client{}
	if got := probeOneAIForService(client, "claude", server.URL); got != -1 {
		t.Fatalf("ambiguous 200 HTML = %d, want unprobed (-1), not blocked (1)", got)
	}
}
