package validator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestProbeAIReachability 覆盖 AI 可达性探测：真实 http 往返（httptest server，不 mock）。
// 语义：拿到任何 HTTP 响应（含 401）→ 0（可达）；连接失败/超时 → 1（不可达）。
// 通过覆盖包级 aiProbeTargets 变量，把 4 个探测目标指向本地 httptest server，
// 其中：一个返回 401（缺 key，仍算可达=0）、一个返回 200（可达=0）、一个已关闭（连不通=1）。
func TestProbeAIReachability(t *testing.T) {
	// 可达（401）：能连通即说明地区不封，401 只是没 key。
	unauthorized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer unauthorized.Close()
	// 可达（200）。
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
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
		"grok":   ok.URL,           // 200 → 可达 0
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

// TestUnknownRiskAIReachabilityEmpty 验证零信息风险的 AIReachability 为空串（整体未探测）。
func TestUnknownRiskAIReachabilityEmpty(t *testing.T) {
	if r := UnknownRisk(); r.AIReachability != "" {
		t.Fatalf("UnknownRisk().AIReachability = %q, want empty", r.AIReachability)
	}
}
