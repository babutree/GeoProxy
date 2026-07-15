package checker

import (
	"log"
	"sync"
	"sync/atomic"
	"time"

	"goproxy/config"
	"goproxy/storage"
	"goproxy/validator"
)

// failDisableThreshold 连续失败达到该阈值即禁用节点，与代理请求路径
// (proxy 包) 使用同一阈值语义。见 BUG-53。
const failDisableThreshold = 3

// healthStore 健康检查对存储的最小依赖，便于单测注入假实现。
type healthStore interface {
	GetQualityDistribution() (map[string]int, error)
	CountAll() (int, error)
	GetBatchForHealthCheck(batchSize int, skipSGrade bool) ([]storage.Proxy, error)
	UpdateProxyExitInfo(id int64, exitIP, exitLocation string, latencyMs int, ipapiisScore float64, ipapiFlags string, cfBlocked int, aiReachability string) error
	RecordProxyUseByID(id int64, success bool) error
	RecordProxyFailureByID(id int64, threshold int) error
}

// healthValidator 健康检查对验证器的最小依赖。
type healthValidator interface {
	ValidateStream(proxies []storage.Proxy) <-chan validator.Result
}

// HealthChecker 健康检查器
type HealthChecker struct {
	storage   healthStore
	validator healthValidator
	cfg       *config.Config

	// running 防止 RunOnce 重叠：已有检查在进行时后发调用直接跳过。
	running atomic.Bool

	// background 只允许启动一次 ticker 循环，避免重复 StartBackground 泄漏 goroutine。
	bgMu         sync.Mutex
	bgStarted    bool
	bgStartCount int
	bgStop       chan struct{}
	bgDone       chan struct{}
}

func NewHealthChecker(s *storage.Storage, v *validator.Validator, cfg *config.Config) *HealthChecker {
	return &HealthChecker{
		storage:   s,
		validator: v,
		cfg:       cfg,
	}
}

func newHealthCheckerForTest(s healthStore, v healthValidator, cfg *config.Config) *HealthChecker {
	return &HealthChecker{
		storage:   s,
		validator: v,
		cfg:       cfg,
	}
}

func (hc *HealthChecker) isBackgroundStarted() bool {
	hc.bgMu.Lock()
	defer hc.bgMu.Unlock()
	return hc.bgStarted
}

func (hc *HealthChecker) backgroundStartCount() int {
	hc.bgMu.Lock()
	defer hc.bgMu.Unlock()
	return hc.bgStartCount
}

// RunOnce 执行一次健康检查；若已有检查在进行则跳过。
func (hc *HealthChecker) RunOnce() {
	if !hc.running.CompareAndSwap(false, true) {
		log.Println("[health] 上一次检查仍在进行，跳过本次")
		return
	}
	defer hc.running.Store(false)

	start := time.Now()
	log.Println("[health] 开始健康检查...")

	// 健康状态且S级占比高时，跳过S级代理检查
	skipSGrade := false
	dist, err := hc.storage.GetQualityDistribution()
	if err != nil {
		log.Printf("[health] 获取质量分布失败: %v", err)
		// 分布失败时不跳过 S 级，继续用全量候选，避免静默改变检查策略。
		dist = map[string]int{}
	}
	sGradeCount := dist["S"]
	totalCount, err := hc.storage.CountAll()
	if err != nil {
		log.Printf("[health] 获取代理数量失败: %v", err)
		return
	}
	if totalCount > 0 && float64(sGradeCount)/float64(totalCount) > 0.3 {
		skipSGrade = true
	}

	// 批量获取需要检查的代理
	proxies, err := hc.storage.GetBatchForHealthCheck(hc.cfg.HealthCheckBatchSize, skipSGrade)
	if err != nil {
		log.Printf("[health] 获取检查批次失败: %v", err)
		return
	}

	if len(proxies) == 0 {
		log.Println("[health] 无需检查的代理")
		return
	}

	log.Printf("[health] 检查 %d 个代理（跳过S级=%v）", len(proxies), skipSGrade)

	// 执行验证
	validCount := 0
	disableCount := 0
	updateCount := 0

	for result := range hc.validator.ValidateStream(proxies) {
		if result.Valid {
			validCount++
			// 更新延迟和质量等级
			latencyMs := int(result.Latency.Milliseconds())
			if err := hc.storage.UpdateProxyExitInfo(result.Proxy.ID, result.ExitIP, result.ExitLocation, latencyMs, result.Risk.IPAPIIsScore, result.Risk.Flags, result.Risk.CFBlocked, result.Risk.AIReachability); err != nil {
				log.Printf("[health] 更新出口信息失败 id=%d: %v", result.Proxy.ID, err)
			} else {
				updateCount++
			}
		} else {
			if err := hc.storage.RecordProxyFailureByID(result.Proxy.ID, failDisableThreshold); err != nil {
				log.Printf("[health] 记录失败次数失败 id=%d: %v", result.Proxy.ID, err)
			} else if result.Proxy.FailCount+1 >= failDisableThreshold {
				disableCount++
			}
		}
	}

	elapsed := time.Since(start)
	log.Printf("[health] 完成: 验证%d 有效%d 更新%d 禁用%d 耗时%v",
		len(proxies), validCount, updateCount, disableCount, elapsed)
}

// StartBackground 后台定时健康检查；重复调用是幂等的，不会再启 ticker。
func (hc *HealthChecker) StartBackground() {
	hc.bgMu.Lock()
	if hc.bgStarted {
		hc.bgMu.Unlock()
		log.Println("[health] 健康检查器已在运行，忽略重复启动")
		return
	}
	hc.bgStarted = true
	hc.bgStartCount++
	stop := make(chan struct{})
	done := make(chan struct{})
	hc.bgStop = stop
	hc.bgDone = done
	intervalMin := hc.cfg.HealthIntervalMinutes
	hc.bgMu.Unlock()

	ticker := time.NewTicker(time.Duration(intervalMin) * time.Minute)
	go func() {
		defer close(done)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				hc.RunOnce()
			case <-stop:
				return
			}
		}
	}()
	log.Printf("[health] 健康检查器已启动，间隔 %d 分钟", intervalMin)
}

// StopBackground 停止后台 ticker（测试与优雅关闭用）；未启动时为 no-op。
func (hc *HealthChecker) StopBackground() {
	hc.bgMu.Lock()
	if !hc.bgStarted {
		hc.bgMu.Unlock()
		return
	}
	stop := hc.bgStop
	done := hc.bgDone
	hc.bgStarted = false
	hc.bgStop = nil
	hc.bgDone = nil
	hc.bgMu.Unlock()

	close(stop)
	<-done
}
