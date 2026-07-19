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
	validate := validator.New(cfg.ValidateConcurrency, cfg.ValidateTimeout, cfg.ValidateURL)

	log.Printf("[checker] 检查 %d 个代理...", len(proxies))
	results := validate.ValidateAll(proxies)

	valid, invalid := 0, 0
	for _, r := range results {
		if r.Valid {
			valid++
			latencyMs := int(r.Latency.Milliseconds())
			if r.ExitIP != "" && r.ExitLocation != "" {
				if err := c.storage.UpdateProxyExitInfo(r.Proxy.ID, r.ExitIP, r.ExitLocation, latencyMs, r.Risk.IPAPIIsScore, r.Risk.Flags, r.Risk.FlagsKnown, r.Risk.CFBlocked, r.Risk.AIReachability); err != nil {
					log.Printf("[checker] 更新出口信息失败: %v", err)
				}
			} else if r.Latency > 0 {
				if err := c.storage.UpdateLatencyByID(r.Proxy.ID, latencyMs); err != nil {
					log.Printf("[checker] 更新延迟失败: %v", err)
				}
			}
		} else {
			invalid++
			if err := c.storage.DisableProxyByID(r.Proxy.ID); err != nil {
				log.Printf("[checker] 禁用代理失败: %v", err)
			}
		}
	}

	count, _ := c.storage.CountAll()
	log.Printf("[checker] 完成: 有效=%d 失败(已禁用)=%d 剩余=%d", valid, invalid, count)
}
