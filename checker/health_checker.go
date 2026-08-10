package checker

import (
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/storage"
	"github.com/babutree/GeoProxy/validator"
)

// failDisableThreshold 连续失败达到该阈值即禁用节点，与代理请求路径
// (proxy 包) 使用同一阈值语义。见 BUG-53。
const failDisableThreshold = 3

// healthStore 健康检查对存储的最小依赖，便于单测注入假实现。
type healthStore interface {
	GetBatchForHealthCheck(batchSize int) ([]storage.Proxy, error)
	ApplyProbeObservation(identity storage.RouteIdentity, observation storage.ExitObservation) error
	RecordProbeFailure(identity storage.RouteIdentity, observation storage.ExitObservation, threshold int) (bool, error)
	DisableRouteForPolicy(identity storage.RouteIdentity) error
}

// healthValidator 健康检查对验证器的最小依赖。
type healthValidator interface {
	ValidateStream(proxies []storage.Proxy) <-chan validator.Result
}

type healthCheckSummary struct {
	valid    int
	updated  int
	disabled int
}

// HealthChecker 健康检查器
type HealthChecker struct {
	storage healthStore

	configMu         sync.RWMutex
	validator        healthValidator
	validatorFactory func(*config.Config) healthValidator
	cfg              *config.Config

	// 防止 RunOnce 重叠：已有检查在进行时，后发调用直接跳过。
	running atomic.Bool

	// 后台循环只允许启动一次，避免重复 StartBackground 泄漏协程。
	bgMu         sync.Mutex
	bgStarted    bool
	bgStartCount int
	bgStop       chan struct{}
	bgDone       chan struct{}
	bgWake       chan struct{}
}

func NewHealthChecker(s *storage.Storage, v *validator.Validator, cfg *config.Config) *HealthChecker {
	return &HealthChecker{
		storage:   s,
		validator: v,
		validatorFactory: func(live *config.Config) healthValidator {
			return validator.NewWithConfig(live)
		},
		cfg: cfg,
	}
}

func newHealthCheckerForTest(s healthStore, v healthValidator, cfg *config.Config) *HealthChecker {
	return &HealthChecker{
		storage:   s,
		validator: v,
		cfg:       cfg,
	}
}

// UpdateConfig 原子替换健康检查使用的配置与验证器快照。
// 相同指针表示配置未变化，不重复构造验证器。
func (hc *HealthChecker) UpdateConfig(cfg *config.Config) {
	if cfg == nil {
		return
	}
	hc.configMu.Lock()
	if hc.cfg == cfg {
		hc.configMu.Unlock()
		return
	}
	hc.cfg = cfg
	if hc.validatorFactory != nil {
		hc.validator = hc.validatorFactory(cfg)
	}
	hc.configMu.Unlock()

	hc.bgMu.Lock()
	wake := hc.bgWake
	hc.bgMu.Unlock()
	if wake != nil {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}

func (hc *HealthChecker) snapshots() (*config.Config, healthValidator) {
	hc.configMu.RLock()
	defer hc.configMu.RUnlock()
	return hc.cfg, hc.validator
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
	if live := config.Get(); live != nil {
		hc.UpdateConfig(live)
	}
	cfg, validate := hc.snapshots()
	if cfg == nil || validate == nil {
		log.Println("[health] 配置或验证器不可用，跳过本次")
		return
	}

	// 批量获取需要检查的代理
	proxies, err := hc.storage.GetBatchForHealthCheck(cfg.HealthCheckBatchSize)
	if err != nil {
		log.Printf("[health] 获取检查批次失败: %v", err)
		return
	}

	if len(proxies) == 0 {
		log.Println("[health] 无需检查的代理")
		return
	}

	log.Printf("[health] 检查 %d 个代理", len(proxies))

	summary := hc.checkBatchWithValidator(proxies, validate)

	elapsed := time.Since(start)
	log.Printf("[health] 完成: 验证%d 有效%d 更新%d 禁用%d 耗时%v",
		len(proxies), summary.valid, summary.updated, summary.disabled, elapsed)
}

// checkBatch 消费本批验证结果；禁用统计只采用存储写入返回的权威状态。
func (hc *HealthChecker) checkBatch(proxies []storage.Proxy) healthCheckSummary {
	_, validate := hc.snapshots()
	return hc.checkBatchWithValidator(proxies, validate)
}

func (hc *HealthChecker) checkBatchWithValidator(proxies []storage.Proxy, validate healthValidator) healthCheckSummary {
	var summary healthCheckSummary
	if validate == nil {
		return summary
	}
	for result := range validate.ValidateStream(proxies) {
		identity := storage.RouteIdentityFromProxy(result.Proxy)
		observation := storage.ExitObservation{
			ExitIP:          result.ExitIP,
			ExitLocation:    result.ExitLocation,
			LatencyMS:       int(result.Latency.Milliseconds()),
			IPAPIIsScore:    result.Risk.IPAPIIsScore,
			IPAPIFlags:      result.Risk.Flags,
			IPAPIFlagsKnown: result.Risk.FlagsKnown,
			CFBlocked:       result.Risk.CFBlocked,
			AIReachability:  result.Risk.AIReachability,
		}
		if result.Valid {
			summary.valid++
			if err := hc.storage.ApplyProbeObservation(identity, observation); err != nil {
				log.Printf("[health] 更新出口信息失败 id=%d: %v", result.Proxy.ID, err)
			} else {
				summary.updated++
			}
			continue
		}
		if result.FailureReason == validator.FailureGeoRejected {
			// 地域拒绝属于当前策略，不是上游或传输故障；不得启动系统禁用保留期。
			if err := hc.storage.DisableRouteForPolicy(identity); err != nil {
				log.Printf("[health] 策略禁用地域拒绝节点失败 id=%d: %v", result.Proxy.ID, err)
				continue
			}
			if err := hc.storage.ApplyProbeObservation(identity, observation); err != nil {
				log.Printf("[health] 写回地域拒绝出口信息失败 id=%d: %v", result.Proxy.ID, err)
			} else {
				summary.updated++
			}
			continue
		}
		disabled, err := hc.storage.RecordProbeFailure(identity, observation, failDisableThreshold)
		if err != nil {
			log.Printf("[health] 记录失败次数失败 id=%d: %v", result.Proxy.ID, err)
		} else if disabled {
			summary.disabled++
		}
	}
	return summary
}

// StartBackground 启动后台定时健康检查；重复调用幂等，不会创建第二个定时器。
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
	wake := make(chan struct{}, 1)
	hc.bgStop = stop
	hc.bgDone = done
	hc.bgWake = wake
	hc.bgMu.Unlock()

	go func() {
		defer close(done)
		for {
			interval := hc.backgroundInterval()
			timer := time.NewTimer(interval)
			select {
			case <-timer.C:
				hc.RunOnce()
			case <-wake:
				if !timer.Stop() {
					<-timer.C
				}
			case <-stop:
				if !timer.Stop() {
					<-timer.C
				}
				return
			}
		}
	}()
	log.Printf("[health] 健康检查器已启动，间隔 %v", hc.backgroundInterval())
}

func (hc *HealthChecker) backgroundInterval() time.Duration {
	cfg, _ := hc.snapshots()
	if cfg == nil || cfg.HealthIntervalMinutes <= 0 {
		return time.Minute
	}
	return time.Duration(cfg.HealthIntervalMinutes) * time.Minute
}

// StopBackground 停止后台定时器（测试与优雅关闭用）；未启动时直接返回。
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
	hc.bgWake = nil
	hc.bgMu.Unlock()

	close(stop)
	<-done
}
