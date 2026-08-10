package checker

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/storage"
	"github.com/babutree/GeoProxy/validator"
)

// slowValidator 模拟慢速探测：每个结果延迟 delay，用于制造 RunOnce 重叠。
type slowValidator struct {
	delay   time.Duration
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (v *slowValidator) ValidateStream(proxies []storage.Proxy) <-chan validator.Result {
	v.calls.Add(1)
	ch := make(chan validator.Result, len(proxies))
	go func() {
		defer close(ch)
		if v.started != nil {
			select {
			case v.started <- struct{}{}:
			default:
			}
		}
		if v.release != nil {
			<-v.release
		} else if v.delay > 0 {
			time.Sleep(v.delay)
		}
		for _, p := range proxies {
			ch <- validator.Result{Proxy: p, Valid: true, Latency: time.Millisecond}
		}
	}()
	return ch
}

type countingStore struct {
	batchCalls           atomic.Int32
	failureCalls         atomic.Int32
	probeFailureCalls    atomic.Int32
	policyCalls          atomic.Int32
	batch                []storage.Proxy
	failureDisabled      bool
	failureErr           error
	mu                   sync.Mutex
	updates              int
	lastProbeObservation storage.ExitObservation
}

func (s *countingStore) GetBatchForHealthCheck(int) ([]storage.Proxy, error) {
	s.batchCalls.Add(1)
	return s.batch, nil
}

func (s *countingStore) UpdateProxyExitInfo(int64, string, string, int, float64, string, bool, int, string) error {
	s.mu.Lock()
	s.updates++
	s.mu.Unlock()
	return nil
}

func (s *countingStore) RecordProxyUseByID(int64, bool) error { return nil }

func (s *countingStore) RecordProxyFailureByID(int64, int) error { return nil }

func (s *countingStore) RecordProxyFailureByIDWithStatus(int64, int) (bool, error) {
	s.failureCalls.Add(1)
	return s.failureDisabled, s.failureErr
}

func (s *countingStore) ApplyProbeObservation(storage.RouteIdentity, storage.ExitObservation) error {
	s.mu.Lock()
	s.updates++
	s.mu.Unlock()
	return nil
}

func (s *countingStore) RecordProbeFailure(_ storage.RouteIdentity, observation storage.ExitObservation, _ int) (bool, error) {
	s.probeFailureCalls.Add(1)
	s.mu.Lock()
	s.lastProbeObservation = observation
	s.mu.Unlock()
	return s.failureDisabled, s.failureErr
}
func (s *countingStore) DisableRouteForPolicy(storage.RouteIdentity) error {
	s.policyCalls.Add(1)
	return nil
}

type resultValidator struct {
	results []validator.Result
}

func (v resultValidator) ValidateStream([]storage.Proxy) <-chan validator.Result {
	ch := make(chan validator.Result, len(v.results))
	for _, result := range v.results {
		ch <- result
	}
	close(ch)
	return ch
}

func TestCheckBatchUsesAuthoritativeDisabledStatus(t *testing.T) {
	tests := []struct {
		name                  string
		snapshotFailCount     int
		authoritativeDisabled bool
		wantDisabled          int
	}{
		{name: "stale snapshot low", snapshotFailCount: 0, authoritativeDisabled: true, wantDisabled: 1},
		{name: "stale snapshot high", snapshotFailCount: 2, authoritativeDisabled: false, wantDisabled: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			proxy := storage.Proxy{ID: 1, FailCount: tc.snapshotFailCount}
			store := &countingStore{
				batch:           []storage.Proxy{proxy},
				failureDisabled: tc.authoritativeDisabled,
			}
			hc := newHealthCheckerForTest(
				store,
				resultValidator{results: []validator.Result{{Proxy: proxy, Valid: false}}},
				&config.Config{},
			)

			summary := hc.checkBatch(store.batch)
			if summary.disabled != tc.wantDisabled {
				t.Fatalf("disabled=%d, want %d", summary.disabled, tc.wantDisabled)
			}
			if got := store.probeFailureCalls.Load(); got != 1 {
				t.Fatalf("failure writes=%d, want 1", got)
			}
		})
	}
}

// TestRunOnceChecksSGradeNode 覆盖 BUG-024：健康检查必须处理批次中的 S 级节点，
// 不能依据质量分布把它们永久排除。
func TestRunOnceChecksSGradeNode(t *testing.T) {
	store := &countingStore{
		batch: []storage.Proxy{{ID: 1, Address: "s-grade:8080", Protocol: "http", QualityGrade: "S", Status: "active"}},
	}
	v := &slowValidator{}
	cfg := &config.Config{HealthCheckBatchSize: 1, HealthIntervalMinutes: 1}
	hc := newHealthCheckerForTest(store, v, cfg)

	hc.RunOnce()

	if got := v.calls.Load(); got != 1 {
		t.Fatalf("ValidateStream calls=%d, want 1", got)
	}
}

// TestRunOnceSkipsWhenAlreadyRunning 复现：两次重叠 RunOnce 会并发跑完探测；
// 期望后发 RunOnce 在已有检查进行中时直接跳过，避免 fail 计数/禁用双写。
func TestRunOnceSkipsWhenAlreadyRunning(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	v := &slowValidator{started: started, release: release}
	store := &countingStore{batch: []storage.Proxy{{ID: 1, Address: "127.0.0.1:1", Protocol: "socks5", Status: "active"}}}
	cfg := &config.Config{HealthCheckBatchSize: 10, HealthIntervalMinutes: 1}
	hc := newHealthCheckerForTest(store, v, cfg)

	done1 := make(chan struct{})
	go func() {
		defer close(done1)
		hc.RunOnce()
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first RunOnce did not start validation")
	}

	// 在第一次仍阻塞时发起第二次；不得再进入 ValidateStream。
	hc.RunOnce()
	if got := v.calls.Load(); got != 1 {
		close(release)
		<-done1
		t.Fatalf("ValidateStream calls=%d, want 1 (second RunOnce must skip)", got)
	}
	if got := store.batchCalls.Load(); got != 1 {
		close(release)
		<-done1
		t.Fatalf("GetBatchForHealthCheck calls=%d, want 1", got)
	}

	close(release)
	select {
	case <-done1:
	case <-time.After(2 * time.Second):
		t.Fatal("first RunOnce did not finish")
	}
}

// TestStartBackgroundIsIdempotent 重复 StartBackground 不得再启第二个 ticker 循环。
func TestStartBackgroundIsIdempotent(t *testing.T) {
	v := &slowValidator{delay: time.Millisecond}
	store := &countingStore{batch: []storage.Proxy{{ID: 1, Address: "127.0.0.1:1", Protocol: "socks5", Status: "active"}}}
	cfg := &config.Config{HealthCheckBatchSize: 10, HealthIntervalMinutes: 60}
	hc := newHealthCheckerForTest(store, v, cfg)

	hc.StartBackground()
	hc.StartBackground()
	// 给 goroutine 一点调度时间；间隔 60min，不应触发 RunOnce。
	time.Sleep(50 * time.Millisecond)
	if got := v.calls.Load(); got != 0 {
		t.Fatalf("ValidateStream calls=%d before first tick, want 0", got)
	}
	// 通过内部状态断言只启动一次（见实现后的 started/stop 契约）。
	if !hc.isBackgroundStarted() {
		t.Fatal("background not marked started")
	}
	// 第二次调用后仍只算启动一次。
	if hc.backgroundStartCount() != 1 {
		t.Fatalf("backgroundStartCount=%d, want 1", hc.backgroundStartCount())
	}
	hc.StopBackground()
}

func TestHealthCheckerPersistsTrustedExitFromInvalidResult(t *testing.T) {
	proxy := storage.Proxy{ID: 1, Address: "exit-after-reject.example:10001", Protocol: "http", Source: storage.SourceManual}
	store := &countingStore{batch: []storage.Proxy{proxy}}
	hc := newHealthCheckerForTest(
		store,
		resultValidator{results: []validator.Result{{
			Proxy: proxy, Valid: false, Latency: 64 * time.Millisecond,
			ExitIP: "203.0.113.91", ExitLocation: "GB London",
		}}},
		&config.Config{},
	)

	hc.checkBatch(store.batch)
	if got := store.probeFailureCalls.Load(); got != 1 {
		t.Fatalf("RecordProbeFailure calls=%d, want 1", got)
	}
	store.mu.Lock()
	observation := store.lastProbeObservation
	store.mu.Unlock()
	if observation.ExitIP != "203.0.113.91" || observation.ExitLocation != "GB London" || observation.LatencyMS != 64 {
		t.Fatalf("probe failure observation = %#v, want trusted exit", observation)
	}
}

// TestHealthCheckerGeoRejectionUsesPolicyDisable 锁定三时钟语义：地域策略拒绝
// 不是上游故障，不能增加失败计数或开始系统禁用保留期；可信出口仍须写回。
func TestHealthCheckerGeoRejectionUsesPolicyDisable(t *testing.T) {
	proxy := storage.Proxy{ID: 1, Address: "geo-rejected.example:10001", Protocol: "http", Source: storage.SourceManual}
	store := &countingStore{batch: []storage.Proxy{proxy}}
	hc := newHealthCheckerForTest(
		store,
		resultValidator{results: []validator.Result{{
			Proxy: proxy, Valid: false, FailureReason: validator.FailureGeoRejected,
			Latency: 64 * time.Millisecond, ExitIP: "203.0.113.92", ExitLocation: "US Ashburn",
		}}},
		&config.Config{},
	)

	summary := hc.checkBatch(store.batch)
	if got := store.probeFailureCalls.Load(); got != 0 {
		t.Fatalf("RecordProbeFailure calls=%d, want 0 for geo policy rejection", got)
	}
	if got := store.policyCalls.Load(); got != 1 {
		t.Fatalf("DisableRouteForPolicy calls=%d, want 1", got)
	}
	if summary.disabled != 0 {
		t.Fatalf("system disabled=%d, want 0 for geo policy rejection", summary.disabled)
	}
	store.mu.Lock()
	updates := store.updates
	store.mu.Unlock()
	if updates != 1 {
		t.Fatalf("ApplyProbeObservation updates=%d, want 1 for trusted exit", updates)
	}
}
