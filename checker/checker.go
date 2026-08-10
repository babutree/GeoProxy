package checker

import (
	"log"
	"time"

	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/storage"
	"github.com/babutree/GeoProxy/validator"
)

type Checker struct {
	storage *storage.Storage
}

func New(s *storage.Storage, _ *validator.Validator, _ *config.Config) *Checker {
	return &Checker{storage: s}
}

func (c *Checker) Start() {
	go func() {
		for {
			cfg := config.Get()
			time.Sleep(time.Duration(cfg.HealthIntervalMinutes) * time.Minute)
			c.run()
		}
	}()
	log.Printf("[checker] 健康检查器已启动，间隔：%d 分钟", config.Get().HealthIntervalMinutes)
}

func (c *Checker) run() {
	log.Println("[checker] 开始健康检查...")

	proxies, err := c.storage.GetAll()
	if err != nil {
		log.Printf("[checker] 获取代理失败: %v", err)
		return
	}
	if len(proxies) == 0 {
		log.Println("[checker] 没有可检查的代理")
		return
	}

	// 每次根据最新配置创建验证器。
	cfg := config.Get()
	validate := validator.NewWithConfig(cfg)

	log.Printf("[checker] 检查 %d 个代理...", len(proxies))
	results := validate.ValidateAll(proxies)

	valid, invalid := 0, 0
	for _, result := range results {
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
			valid++
			if err := c.storage.ApplyProbeObservation(identity, observation); err != nil {
				log.Printf("[checker] 更新出口信息失败: %v", err)
			}
			continue
		}
		invalid++
		if result.FailureReason == validator.FailureGeoRejected {
			// 地域拒绝属于当前策略，不是上游或传输故障；不得启动系统禁用保留期。
			if err := c.storage.DisableRouteForPolicy(identity); err != nil {
				log.Printf("[checker] 策略禁用地域拒绝节点失败: %v", err)
				continue
			}
			if err := c.storage.ApplyProbeObservation(identity, observation); err != nil {
				log.Printf("[checker] 写回地域拒绝出口信息失败: %v", err)
			}
			continue
		}
		if _, err := c.storage.RecordProbeFailure(identity, observation, 1); err != nil {
			log.Printf("[checker] 禁用代理失败: %v", err)
		}
	}

	count, _ := c.storage.CountAll()
	log.Printf("[checker] 完成: 有效=%d 失败(已禁用)=%d 剩余=%d", valid, invalid, count)
}
