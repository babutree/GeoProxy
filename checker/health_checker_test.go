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
	batchCalls atomic.Int32
	batch      []storage.Proxy
	mu         sync.Mutex
	updates    int
}

func (s *countingStore) GetQualityDistribution() (map[string]int, error) {
	return map[string]int{}, nil
}

func (s *countingStore) CountAll() (int, error) {
	return len(s.batch), nil
}

func (s *countingStore) GetBatchForHealthCheck(int, bool) ([]storage.Proxy, error) {
	s.batchCalls.Add(1)
	return s.batch, nil
}

func (s *countingStore) UpdateProxyExitInfo(int64, string, string, int, float64, string, int, string) error {
	s.mu.Lock()
	s.updates++
	s.mu.Unlock()
	return nil
}

func (s *countingStore) RecordProxyUseByID(int64, bool) error { return nil }

func (s *countingStore) RecordProxyFailureByID(int64, int) error { return nil }

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
