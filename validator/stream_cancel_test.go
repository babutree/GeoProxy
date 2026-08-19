package validator

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/storage"
)

// 构造一批必然快速失败的节点：地址指向保留端口，拨号立刻失败，
// 不触网也不依赖外部服务。
func unreachableProxies(n int) []storage.Proxy {
	proxies := make([]storage.Proxy, n)
	for i := range proxies {
		proxies[i] = storage.Proxy{
			ID:       int64(i + 1),
			Address:  "127.0.0.1:1",
			Protocol: "http",
		}
	}
	return proxies
}

func newLeakTestValidator(concurrency int) *Validator {
	return newValidator(concurrency, 1, "http://127.0.0.1:1/never", &config.Config{})
}

// TestValidateStreamCancelReleasesAbandonedSenders 是 M-3 的核心回归。
//
// channel 缓冲是 min(len(proxies), concurrency*10)。当节点数超过该上限时
// 发送方会阻塞；若消费者中途放弃且没有取消机制，这些 goroutine 会永久卡在
// `ch <- result`，连同其占用的 sem 槽位与连接一起泄漏到进程退出——
// 不是"阻塞一会儿"，是永不退出。
//
// 关键：取消后**绝不能**继续 drain channel。继续读会把阻塞的发送方解放掉，
// 从而掩盖这个 bug（消费者放弃的真实场景就是不再读）。这里只读一个结果、
// 取消、然后彻底不管，直接断言 goroutine 数回落。
func TestValidateStreamCancelReleasesAbandonedSenders(t *testing.T) {
	const (
		total       = 60
		concurrency = 2
	)
	// 前置断言：本用例必须真的处在"发送方会阻塞"的区间，否则测不到东西。
	if buf := concurrencyBuffer(total, concurrency); buf >= total {
		t.Fatalf("buffer=%d >= total=%d; this case cannot exercise blocked senders", buf, total)
	}

	settle()
	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	ch := newLeakTestValidator(concurrency).ValidateStream(ctx, unreachableProxies(total))

	// 只消费一个结果就放弃，之后再也不读。
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("no result within 10s")
	}

	// 关键：必须等缓冲填满、发送方真的阻塞之后再取消。
	// 若在缓冲填满前就取消，所有在途发送都能顺利完成，测不到阻塞场景
	// （这正是本用例第一版漏掉变异的原因）。
	blocked := waitForGoroutineGrowth(before, 10*time.Second)
	if !blocked {
		t.Fatalf("goroutines never grew past before=%d; the buffer did not fill, so no sender ever blocked", before)
	}

	cancel()

	// 不 drain：等 goroutine 自行退出。取消感知的发送让它们立刻返回；
	// 若发送不感知取消，它们会永久卡住，goroutine 数居高不下。
	settle()
	after := runtime.NumGoroutine()

	// 留出宽松余量：runtime/测试框架自身的 goroutine 数会有小幅波动。
	// 泄漏时滞留的是 concurrency 量级的发送方 + 派发协程，远超此余量。
	if after > before+2 {
		t.Fatalf("goroutines before=%d after=%d (delta=%+d); an abandoned cancelled stream leaked senders blocked on `ch <- result`",
			before, after, after-before)
	}
	t.Logf("goroutines before=%d after=%d delta=%+d", before, after, after-before)
}

// waitForGoroutineGrowth 等待 goroutine 数明显超过基线，表明发送方已被
// 满缓冲挡住。返回 false 说明在超时内没等到，调用方应视为用例前提不成立。
func waitForGoroutineGrowth(baseline int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() > baseline+1 {
			// 再稳定观察一次，避免抓到瞬时峰值。
			time.Sleep(200 * time.Millisecond)
			return runtime.NumGoroutine() > baseline+1
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// 取消后 channel 必须被关闭：消费者若选择继续读，应当读到关闭而不是永久挂起。
func TestValidateStreamClosesChannelAfterCancel(t *testing.T) {
	const (
		total       = 60
		concurrency = 2
	)
	ctx, cancel := context.WithCancel(context.Background())
	ch := newLeakTestValidator(concurrency).ValidateStream(ctx, unreachableProxies(total))

	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("no result within 10s")
	}
	cancel()

	deadline := time.After(15 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("ValidateStream channel never closed after cancel")
		}
	}
}

// 取消后不得再派发新探测。
//
// 这条与"发送感知取消"是两件事：即使发送能被取消，若派发循环不检查 ctx，
// 整批节点仍会被逐个跑一遍网络探测，只是结果被丢弃。关闭一个 6000 节点的
// 实例时，这意味着数千次无意义的出站连接。
//
// 用大批量放大差异：批量足够大时，"停止派发"与"跑完再丢弃"在收到的结果数
// 和耗时上相差两个数量级（实测 10000 节点：2 条/4ms vs 4978 条/1.3s）。
func TestValidateStreamCancelStopsDispatchingNewProbes(t *testing.T) {
	const total = 10000
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 派发开始前就已取消

	start := time.Now()
	ch := newLeakTestValidator(4).ValidateStream(ctx, unreachableProxies(total))
	received := 0
	for range ch {
		received++
	}
	elapsed := time.Since(start)

	// 已取消的流只应放行极少量"取消前已抢到 sem"的在途探测。
	// 阈值取 total 的 1%：远高于实测值（个位数），又远低于不检查取消时的约 50%。
	if maxAllowed := total / 100; received > maxAllowed {
		t.Fatalf("received %d of %d results from a pre-cancelled stream (max %d); dispatch must stop instead of probing the whole batch",
			received, total, maxAllowed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("pre-cancelled ValidateStream took %v for %d proxies; it should stop dispatching immediately",
			elapsed, total)
	}
	t.Logf("received=%d of %d, elapsed=%v", received, total, elapsed.Round(time.Millisecond))
}

// 未取消时必须完整交付全部结果——取消能力不能以丢结果为代价。
func TestValidateStreamDeliversAllResultsWithoutCancel(t *testing.T) {
	const total = 30
	ch := newLeakTestValidator(8).ValidateStream(context.Background(), unreachableProxies(total))
	received := 0
	for range ch {
		received++
	}
	if received != total {
		t.Fatalf("received %d results, want all %d when the context is never cancelled", received, total)
	}
}

// nil ctx 必须被当作 Background 处理，不得 panic：
// 这是防御性契约，避免调用方漏传导致整个验证路径崩溃。
func TestValidateStreamTreatsNilContextAsBackground(t *testing.T) {
	//nolint:staticcheck // 显式测试 nil ctx 的防御行为
	ch := newLeakTestValidator(4).ValidateStream(nil, unreachableProxies(4))
	received := 0
	for range ch {
		received++
	}
	if received != 4 {
		t.Fatalf("received %d results with a nil context, want 4", received)
	}
}

// settle 给 runtime 一点时间回收已退出的 goroutine，降低计数抖动。
func settle() {
	for i := 0; i < 3; i++ {
		runtime.GC()
		time.Sleep(100 * time.Millisecond)
	}
}
