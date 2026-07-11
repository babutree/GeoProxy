package checker

import (
	"log"
	"time"

	"goproxy/config"
	"goproxy/storage"
	"goproxy/validator"
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
	log.Printf("health checker started, interval: %d min", config.Get().HealthIntervalMinutes)
}

func (c *Checker) run() {
	log.Println("[checker] start health check...")

	proxies, err := c.storage.GetAll()
	if err != nil {
		log.Printf("[checker] get proxies error: %v", err)
		return
	}
	if len(proxies) == 0 {
		log.Println("[checker] no proxies to check")
		return
	}

	// 每次用最新配置创建 validator
	cfg := config.Get()
	validate := validator.New(cfg.ValidateConcurrency, cfg.ValidateTimeout, cfg.ValidateURL)

	log.Printf("[checker] checking %d proxies...", len(proxies))
	results := validate.ValidateAll(proxies)

	valid, invalid := 0, 0
	for _, r := range results {
		if r.Valid {
			valid++
			latencyMs := int(r.Latency.Milliseconds())
			if r.ExitIP != "" && r.ExitLocation != "" {
				if err := c.storage.UpdateProxyExitInfo(r.Proxy.ID, r.ExitIP, r.ExitLocation, latencyMs, r.Risk.IPAPIIsScore, r.Risk.Flags, r.Risk.CFBlocked); err != nil {
					log.Printf("[checker] update exit info error: %v", err)
				}
			} else if r.Latency > 0 {
				if err := c.storage.UpdateLatencyByID(r.Proxy.ID, latencyMs); err != nil {
					log.Printf("[checker] update latency error: %v", err)
				}
			}
		} else {
			invalid++
			if err := c.storage.DisableProxyByID(r.Proxy.ID); err != nil {
				log.Printf("[checker] disable error: %v", err)
			}
		}
	}

	count, _ := c.storage.CountAll()
	log.Printf("[checker] done: valid=%d invalid(disabled)=%d remaining=%d", valid, invalid, count)
}
