package main

import (
	"log"
	"time"

	"github.com/babutree/GeoProxy/checker"
	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/custom"
	"github.com/babutree/GeoProxy/logger"
	"github.com/babutree/GeoProxy/proxy"
	"github.com/babutree/GeoProxy/storage"
	"github.com/babutree/GeoProxy/validator"
	"github.com/babutree/GeoProxy/webui"
)

func main() {
	// 初始化日志收集器
	logger.Init()

	// 加载配置
	cfg := config.Load()

	// 首次启动会自动生成随机凭据，仅在此处一次性打印明文。
	// 之后重启不再显示；代理认证凭据可在系统设置修改，WebUI 登录密码遗失时按 README 重置。
	if boot := config.FirstBootCredentials(); boot != nil {
		logFirstBootCredentials(boot)
	}

	log.Printf("[main] 代理网关配置: HTTP=%s SOCKS5=%s WebUI=%s SessionTTL=%dmin",
		cfg.HTTPPort, cfg.SOCKS5Port, cfg.WebUIPort, cfg.SessionTTLMinutes)

	// 初始化存储
	store, err := storage.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("[main] 初始化存储失败: %v", err)
	}
	defer store.Close()

	// 初始化核心模块
	validate := validator.New(cfg.ValidateConcurrency, cfg.ValidateTimeout, cfg.ValidateURL)
	healthChecker := checker.NewHealthChecker(store, validate, cfg)

	// 地理过滤只禁用节点，不删除已有数据库行。
	if len(cfg.AllowedCountries) > 0 {
		if disabled, err := store.DisableNotAllowedCountries(cfg.AllowedCountries); err == nil && disabled > 0 {
			log.Printf("[main] 🔒 已禁用 %d 个非白名单节点", disabled)
		}
	} else if len(cfg.BlockedCountries) > 0 {
		if disabled, err := store.DisableBlockedCountries(cfg.BlockedCountries); err == nil && disabled > 0 {
			log.Printf("[main] 🔒 已禁用 %d 个屏蔽国家节点", disabled)
		}
	}

	sessionStore := proxy.SessionStore(cfg)
	// 每分钟扫描一次过期会话绑定；TTL 本身由 SessionTTLMinutes 决定（见 affinity.New）。
	sessionStore.StartGC(1 * time.Minute)
	defer sessionStore.Stop()

	httpServer := proxy.New(store, cfg, cfg.HTTPPort)
	socks5Server := proxy.NewSOCKS5(store, cfg, cfg.SOCKS5Port)

	// 初始化订阅管理器
	customMgr := custom.NewManager(store, validate, cfg)

	// 配置变更通知 channel
	configChanged := make(chan struct{}, 1)

	// 启动 WebUI（保留订阅管理器和配置变更通知）
	ui := webui.New(store, cfg, sessionStore, customMgr, configChanged)
	ui.Start()

	// 启动健康检查器
	healthChecker.StartBackground()

	// 启动订阅管理器
	go customMgr.Start()

	// 启动 HTTP 代理入口
	go func() {
		if err := httpServer.Start(); err != nil {
			log.Fatalf("[main] HTTP 代理服务失败: %v", err)
		}
	}()

	// 启动 SOCKS5 代理入口（阻塞）
	if err := socks5Server.Start(); err != nil {
		log.Fatalf("[main] SOCKS5 代理服务失败: %v", err)
	}
}

// logFirstBootCredentials 在首次启动时一次性打印自动生成的凭据。
// 这些明文仅此一次出现在日志中；重启后不再显示。代理认证凭据可在系统设置修改，
// WebUI 登录密码不在系统设置修改，遗失时按 README 重置。
func logFirstBootCredentials(boot *config.FirstBootInfo) {
	log.Println("[main] ============================================================")
	log.Println("[main] 首次启动：已自动生成登录凭据（仅显示这一次，请立即保存）")
	if boot.WebUIPassword != "" {
		log.Printf("[main]   WebUI 登录密码 : %s", boot.WebUIPassword)
	}
	if boot.ProxyAuthPassword != "" {
		log.Printf("[main]   代理认证用户名 : %s", boot.ProxyAuthUsername)
		log.Printf("[main]   代理认证密码   : %s", boot.ProxyAuthPassword)
	}
	log.Println("[main]   代理认证用户名/密码可在 WebUI“系统设置”中修改。")
	log.Println("[main]   WebUI 登录密码不在“系统设置”中修改；遗失时请按 README 的重置流程处理。")
	log.Println("[main] ============================================================")
}
