package validator

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// riskProbeCountingTransport 拦截全部出站请求，不触网。
// 记录请求总数与观测到的最大并发，用于断言 assessRisk 的并发行为。
type riskProbeCountingTransport struct {
	count         int64
	cur           int64
	maxConcurrent int64
	delay         time.Duration
	status        int
}

func (t *riskProbeCountingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	atomic.AddInt64(&t.count, 1)
	now := atomic.AddInt64(&t.cur, 1)
	for {
		old := atomic.LoadInt64(&t.maxConcurrent)
		if now <= old || atomic.CompareAndSwapInt64(&t.maxConcurrent, old, now) {
			break
		}
	}
	defer atomic.AddInt64(&t.cur, -1)

	if t.delay > 0 {
		select {
		case <-time.After(t.delay):
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}
	status := t.status
	if status == 0 {
		status = http.StatusTeapot
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader("{}")),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

// TestAssessRiskProbesConcurrentlyWithinFanout 锁定风险评估的并发化。
//
// assessRisk 需发起 9 次彼此独立的请求（ipapi.is 1 + Cloudflare 1 + AI API 4 +
// AI 产品层 3）。原实现完全串行，每个请求各自受 client.Timeout 约束，最坏耗时
// 是 9×Timeout，期间独占一个 ValidateStream 并发槽位。
//
// 断言两件事：
//   - 真的并发了（最大并发 > 1），且总耗时远低于串行下限；
//   - 并发被 riskProbeFanout 限制（不会瞬时打开 9 条连接耗尽 fd）。
func TestAssessRiskProbesConcurrentlyWithinFanout(t *testing.T) {
	const perRequestDelay = 120 * time.Millisecond
	tr := &riskProbeCountingTransport{delay: perRequestDelay}
	client := &http.Client{Transport: tr, Timeout: 10 * time.Second}

	start := time.Now()
	assessRisk(client, ipAPIInfo{OK: true, IP: "203.0.113.1", FlagsKnown: true})
	elapsed := time.Since(start)

	n := atomic.LoadInt64(&tr.count)
	if n == 0 {
		t.Fatal("assessRisk issued no probe requests")
	}
	maxConcurrent := atomic.LoadInt64(&tr.maxConcurrent)
	if maxConcurrent < 2 {
		t.Fatalf("max observed concurrency = %d, want >1 (probes must not run serially)", maxConcurrent)
	}
	if maxConcurrent > int64(riskProbeFanout) {
		t.Fatalf("max observed concurrency = %d, want <= riskProbeFanout(%d); unbounded fan-out exhausts sockets at ValidateConcurrency scale",
			maxConcurrent, riskProbeFanout)
	}

	serialFloor := time.Duration(n) * perRequestDelay
	// 并发后耗时上限：ceil(n/fanout) 轮 + 调度余量。取 serialFloor 的 3/4 作为
	// 宽松上限即可证明"不再是串行累加"，同时不让测试对调度抖动敏感。
	if elapsed >= serialFloor*3/4 {
		t.Fatalf("elapsed = %v with %d requests; serial floor is %v — probes appear to still run serially",
			elapsed, n, serialFloor)
	}
	t.Logf("requests=%d maxConcurrent=%d elapsed=%v (serial floor %v)", n, maxConcurrent, elapsed.Round(time.Millisecond), serialFloor)
}

// TestAssessRiskHonoursOverallBudget 锁定整轮评估的硬上限。
//
// 原实现每个请求各自超时、无整体 deadline，9 次串行的最坏耗时是 9×Timeout
// （默认 ValidateTimeout=10s 时为 90s）。现按 riskProbeBudgetFactor 设总预算，
// 超出预算的探测被跳过而非继续等待。
func TestAssessRiskHonoursOverallBudget(t *testing.T) {
	const perRequestTimeout = 150 * time.Millisecond
	// 每个请求都比自身超时更慢 → 全部靠超时收敛，最大化总耗时。
	tr := &riskProbeCountingTransport{delay: 10 * time.Second}
	client := &http.Client{Transport: tr, Timeout: perRequestTimeout}

	start := time.Now()
	risk := assessRisk(client, ipAPIInfo{OK: true, IP: "203.0.113.1", FlagsKnown: true})
	elapsed := time.Since(start)

	budget := perRequestTimeout * riskProbeBudgetFactor
	// 允许一次调度余量：预算到点后已在途的请求仍需被自身超时收敛。
	limit := budget + perRequestTimeout + 500*time.Millisecond
	if elapsed > limit {
		t.Fatalf("elapsed = %v, want <= %v (budget %v); overall deadline is not enforced", elapsed, limit, budget)
	}
	// 全部探测均失败/被截断，必须保持"未探测"语义，不得退化成"没问题"或"被封"。
	if risk.IPAPIIsScore != IPAPIIsUnknown {
		t.Fatalf("IPAPIIsScore = %v, want IPAPIIsUnknown(%v) when every probe fails", risk.IPAPIIsScore, IPAPIIsUnknown)
	}
	if risk.CFBlocked != -1 {
		t.Fatalf("CFBlocked = %d, want -1 (unprobed) when the probe never completes", risk.CFBlocked)
	}
	t.Logf("elapsed=%v budget=%v limit=%v risk=%+v", elapsed.Round(time.Millisecond), budget, limit, risk)
}

// TestAssessRiskPreservesFlagsWithoutProbes 确认并发改造未影响 ip-api 命中标记：
// Flags/FlagsKnown 来自已取得的 ipInfo，不发请求，必须原样保留。
func TestAssessRiskPreservesFlagsWithoutProbes(t *testing.T) {
	tr := &riskProbeCountingTransport{}
	client := &http.Client{Transport: tr, Timeout: time.Second}

	// ipInfo.OK=false 且 IP 为空 → 不查 ipapi.is，但 Flags 仍须生效。
	risk := assessRisk(client, ipAPIInfo{FlagsKnown: true, Proxy: true, Hosting: true})
	if !risk.FlagsKnown {
		t.Fatal("FlagsKnown = false, want true (carried from ipInfo, not from a probe)")
	}
	if risk.Flags == "" {
		t.Fatal("Flags is empty; proxy/hosting hits from ipInfo must survive the concurrent refactor")
	}
	if risk.IPAPIIsScore != IPAPIIsUnknown {
		t.Fatalf("IPAPIIsScore = %v, want IPAPIIsUnknown when exit IP is unavailable", risk.IPAPIIsScore)
	}
	t.Logf("risk=%+v requests=%d", risk, atomic.LoadInt64(&tr.count))
}

// TestProbeAIReachabilityBoundedSkipsOnExhaustedBudget 锁定截断语义：
// 预算已耗尽时每个服务保持 -1（未探测），绝不退化成 1（封禁）——
// 否则存储层会把"没测"写成"该 AI 不可达"，误归因于代理。
func TestProbeAIReachabilityBoundedSkipsOnExhaustedBudget(t *testing.T) {
	tr := &riskProbeCountingTransport{}
	client := &http.Client{Transport: tr, Timeout: time.Second}

	// deadline 已过期。
	got := probeAIReachabilityBounded(client, newProbeGate(riskProbeFanout), time.Now().Add(-time.Second))
	if atomic.LoadInt64(&tr.count) != 0 {
		t.Fatalf("issued %d requests after the budget expired, want 0", atomic.LoadInt64(&tr.count))
	}
	for service := range aiProbeTargets {
		want := `"` + service + `":-1`
		if !strings.Contains(got, want) {
			t.Fatalf("probeAIReachabilityBounded() = %q, want %s (unprobed, never 1/blocked)", got, want)
		}
	}
}

// TestClientWithProbeBudgetNarrowsTimeout 锁定派生 client 的超时收窄逻辑。
func TestClientWithProbeBudgetNarrowsTimeout(t *testing.T) {
	base := &http.Client{Timeout: 10 * time.Second}

	// 剩余预算小于原超时 → 收窄到剩余预算。
	narrowed := clientWithProbeBudget(base, time.Now().Add(300*time.Millisecond))
	if narrowed == nil {
		t.Fatal("clientWithProbeBudget() = nil with budget remaining")
	}
	if narrowed.Timeout > 400*time.Millisecond {
		t.Fatalf("derived timeout = %v, want narrowed to the remaining budget", narrowed.Timeout)
	}
	if base.Timeout != 10*time.Second {
		t.Fatalf("base client mutated: Timeout = %v, want 10s (must derive a copy)", base.Timeout)
	}

	// 剩余预算大于原超时 → 保留原超时（不放宽单请求上限）。
	kept := clientWithProbeBudget(base, time.Now().Add(time.Minute))
	if kept == nil || kept.Timeout != 10*time.Second {
		t.Fatalf("derived timeout = %v, want the original 10s when budget is larger", kept.Timeout)
	}

	// 预算耗尽 → nil，调用方跳过探测。
	if exhausted := clientWithProbeBudget(base, time.Now().Add(-time.Second)); exhausted != nil {
		t.Fatalf("clientWithProbeBudget() = %+v, want nil once the budget is exhausted", exhausted)
	}
}
