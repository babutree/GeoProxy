package config

import (
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

// credentialLength 是自动生成凭据的长度（远超 8 位下限）。
const credentialLength = 16

// credentialAlphabet 仅含字母与数字，不含符号，避免在 SOCKS5 / URL / Basic 认证中出现转义问题；
// 同时去掉了易混淆字符（0/O、1/l/I）以便人工抄写。
const credentialAlphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// FirstBootInfo 保存首次启动时生成的明文凭据，仅用于在日志中一次性展示。
type FirstBootInfo struct {
	WebUIPassword     string
	ProxyAuthUsername string
	ProxyAuthPassword string
}

var firstBoot *FirstBootInfo

// FirstBootCredentials 返回本次进程首次启动生成的凭据；非首次启动返回 nil。
func FirstBootCredentials() *FirstBootInfo { return firstBoot }

func dataDir() string {
	if d := os.Getenv("DATA_DIR"); d != "" {
		os.MkdirAll(d, 0755)
		return d + "/"
	}
	return ""
}

func ConfigFile() string { return dataDir() + "config.json" }

type Config struct {
	HTTPPort               string
	SOCKS5Port             string
	WebUIPort              string
	WebUIPasswordHash      string
	ProxyAuthEnabled       bool
	ProxyAuthUsername      string
	ProxyAuthPassword      string
	ProxyAuthPasswordHash  string
	SessionTTLMinutes      int
	DefaultRegion          string
	BlockedCountries       []string
	AllowedCountries       []string
	DBPath                 string
	ValidateConcurrency    int
	ValidateTimeout        int
	ValidateURL            string
	MaxResponseMs          int
	HealthIntervalMinutes  int
	HealthCheckBatchSize   int
	HealthCheckConcurrency int
	CustomProbeInterval    int
	CustomRefreshInterval  int
	SingBoxPath            string
	SingBoxBasePort        int
	SingBoxShardCount      int
	MaxRetry               int
}

var (
	globalCfg *Config
	cfgMu     sync.RWMutex
)

func passwordHash(plain string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(plain)))
}

// generateCredential 生成一个仅含字母数字的随机凭据，长度为 credentialLength。
func generateCredential() string {
	buf := make([]byte, credentialLength)
	if _, err := crand.Read(buf); err != nil {
		// crypto/rand 失败极罕见；显式 panic 而非静默降级到弱随机。
		panic(fmt.Sprintf("generate credential: %v", err))
	}
	out := make([]byte, credentialLength)
	for i, b := range buf {
		out[i] = credentialAlphabet[int(b)%len(credentialAlphabet)]
	}
	return string(out)
}

// DefaultConfig 返回非凭据类的默认配置。凭据（WebUI 密码、代理账号/密码）不在此设置，
// 由首次启动引导逻辑在 Load 中生成并落盘到 config.json。
func DefaultConfig() *Config {
	singBoxPath := envOrDefault("SINGBOX_PATH", "sing-box")
	return &Config{
		HTTPPort:               envPort("HTTP_PORT", ":7802"),
		SOCKS5Port:             envPort("SOCKS5_PORT", ":7801"),
		WebUIPort:              envPort("WEBUI_PORT", ":7800"),
		ProxyAuthEnabled:       true,
		ProxyAuthUsername:      "acct",
		SessionTTLMinutes:      envInt("SESSION_TTL_MINUTES", 10),
		DefaultRegion:          NormalizeCountryCode(os.Getenv("DEFAULT_REGION")),
		BlockedCountries:       envCountriesDefault("BLOCKED_COUNTRIES", []string{"CN"}),
		AllowedCountries:       envCountries("ALLOWED_COUNTRIES"),
		DBPath:                 dataDir() + "proxy.db",
		ValidateConcurrency:    300,
		ValidateTimeout:        10,
		ValidateURL:            "http://www.gstatic.com/generate_204,http://cp.cloudflare.com/generate_204,http://captive.apple.com/hotspot-detect.html",
		MaxResponseMs:          5000,
		HealthIntervalMinutes:  envInt("HEALTH_CHECK_INTERVAL", 5),
		HealthCheckBatchSize:   20,
		HealthCheckConcurrency: 50,
		CustomProbeInterval:    10,
		CustomRefreshInterval:  60,
		SingBoxPath:            singBoxPath,
		SingBoxBasePort:        20000,
		SingBoxShardCount:      envInt("SINGBOX_SHARD_COUNT", 4),
		MaxRetry:               envInt("MAX_RETRY", 3),
	}
}

func Load() *Config {
	cfg := DefaultConfig()
	data, err := os.ReadFile(ConfigFile())
	if err == nil {
		var saved savedConfig
		if json.Unmarshal(data, &saved) == nil {
			applySavedConfig(cfg, saved)
		}
	}

	// 首次启动引导：config.json 尚无凭据时，生成随机凭据并落盘。
	// 明文仅保存在 firstBoot 供日志一次性展示，不写入磁盘明文（磁盘只存 hash）。
	if cfg.WebUIPasswordHash == "" || cfg.ProxyAuthPasswordHash == "" {
		bootstrapCredentials(cfg)
	}

	cfgMu.Lock()
	globalCfg = cfg
	cfgMu.Unlock()
	return cfg
}

// bootstrapCredentials 为缺失的凭据生成随机值并持久化到 config.json。
func bootstrapCredentials(cfg *Config) {
	info := &FirstBootInfo{}
	if cfg.WebUIPasswordHash == "" {
		info.WebUIPassword = generateCredential()
		cfg.WebUIPasswordHash = passwordHash(info.WebUIPassword)
	}
	if cfg.ProxyAuthPasswordHash == "" {
		if cfg.ProxyAuthUsername == "" {
			cfg.ProxyAuthUsername = "acct"
		}
		info.ProxyAuthUsername = cfg.ProxyAuthUsername
		info.ProxyAuthPassword = generateCredential()
		cfg.ProxyAuthPasswordHash = passwordHash(info.ProxyAuthPassword)
	}
	firstBoot = info
	if err := Save(cfg); err != nil {
		// 落盘失败必须显式暴露：否则重启后凭据丢失且用户被永久锁在外面。
		panic(fmt.Sprintf("persist bootstrap credentials: %v", err))
	}
}

func Get() *Config {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return globalCfg
}

type savedConfig struct {
	HTTPPort              string   `json:"http_port,omitempty"`
	SOCKS5Port            string   `json:"socks5_port,omitempty"`
	WebUIPort             string   `json:"webui_port,omitempty"`
	WebUIPasswordHash     string   `json:"webui_password_hash,omitempty"`
	ProxyAuthEnabled      *bool    `json:"proxy_auth_enabled,omitempty"`
	ProxyAuthUsername     string   `json:"proxy_auth_username,omitempty"`
	ProxyAuthPassword     string   `json:"proxy_auth_password,omitempty"`
	ProxyAuthPasswordHash string   `json:"proxy_auth_password_hash,omitempty"`
	SessionTTLMinutes     int      `json:"session_ttl_minutes,omitempty"`
	DefaultRegion         string   `json:"default_region,omitempty"`
	HealthIntervalMinutes int      `json:"health_check_interval,omitempty"`
	MaxRetry              *int     `json:"max_retry,omitempty"`
	SingBoxPath           string   `json:"singbox_path,omitempty"`
	SingBoxShardCount     int      `json:"singbox_shard_count,omitempty"`
	BlockedCountries      []string `json:"blocked_countries,omitempty"`
	AllowedCountries      []string `json:"allowed_countries,omitempty"`
}

func Save(cfg *Config) error {
	authEnabled := cfg.ProxyAuthEnabled
	maxRetry := cfg.MaxRetry
	data, err := json.MarshalIndent(savedConfig{
		HTTPPort:              cfg.HTTPPort,
		SOCKS5Port:            cfg.SOCKS5Port,
		WebUIPort:             cfg.WebUIPort,
		WebUIPasswordHash:     cfg.WebUIPasswordHash,
		ProxyAuthEnabled:      &authEnabled,
		ProxyAuthUsername:     cfg.ProxyAuthUsername,
		ProxyAuthPasswordHash: cfg.ProxyAuthPasswordHash,
		SessionTTLMinutes:     cfg.SessionTTLMinutes,
		DefaultRegion:         NormalizeCountryCode(cfg.DefaultRegion),
		HealthIntervalMinutes: cfg.HealthIntervalMinutes,
		MaxRetry:              &maxRetry,
		SingBoxPath:           cfg.SingBoxPath,
		SingBoxShardCount:     cfg.SingBoxShardCount,
		BlockedCountries:      NormalizeCountryCodes(cfg.BlockedCountries),
		AllowedCountries:      NormalizeCountryCodes(cfg.AllowedCountries),
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(ConfigFile(), data, 0644); err != nil {
		return err
	}

	saved := *cfg
	saved.ProxyAuthPassword = ""
	saved.DefaultRegion = NormalizeCountryCode(saved.DefaultRegion)
	saved.BlockedCountries = NormalizeCountryCodes(saved.BlockedCountries)
	saved.AllowedCountries = NormalizeCountryCodes(saved.AllowedCountries)
	// 用指针替换而非原地改写 *globalCfg：已发布的 *Config 视为不可变快照。
	// 这样任何通过 Get() 持有旧指针的读者（如 validator 缓存的 v.cfg）要么看到
	// 完整的旧结构体，要么在下次 Get() 时看到完整的新结构体，绝不会读到被 Save
	// 原地改写到一半的 slice header（数据竞争 / 读撕裂）。
	cfgMu.Lock()
	globalCfg = &saved
	cfgMu.Unlock()
	return nil
}

func applySavedConfig(cfg *Config, saved savedConfig) {
	if saved.HTTPPort != "" {
		cfg.HTTPPort = normalizePort(saved.HTTPPort)
	}
	if saved.SOCKS5Port != "" {
		cfg.SOCKS5Port = normalizePort(saved.SOCKS5Port)
	}
	if saved.WebUIPort != "" {
		cfg.WebUIPort = normalizePort(saved.WebUIPort)
	}
	if saved.WebUIPasswordHash != "" {
		cfg.WebUIPasswordHash = saved.WebUIPasswordHash
	}
	if saved.ProxyAuthEnabled != nil {
		cfg.ProxyAuthEnabled = *saved.ProxyAuthEnabled
	}
	if saved.ProxyAuthUsername != "" {
		cfg.ProxyAuthUsername = saved.ProxyAuthUsername
	}
	if saved.ProxyAuthPasswordHash != "" {
		cfg.ProxyAuthPasswordHash = saved.ProxyAuthPasswordHash
	} else if saved.ProxyAuthPassword != "" {
		cfg.ProxyAuthPasswordHash = passwordHash(saved.ProxyAuthPassword)
	}
	if saved.SessionTTLMinutes > 0 {
		cfg.SessionTTLMinutes = saved.SessionTTLMinutes
	}
	if saved.DefaultRegion != "" {
		cfg.DefaultRegion = NormalizeCountryCode(saved.DefaultRegion)
	}
	if saved.HealthIntervalMinutes > 0 {
		cfg.HealthIntervalMinutes = saved.HealthIntervalMinutes
	}
	if saved.MaxRetry != nil {
		cfg.MaxRetry = *saved.MaxRetry
	}
	if saved.SingBoxPath != "" {
		cfg.SingBoxPath = saved.SingBoxPath
	}
	if saved.SingBoxShardCount > 0 {
		cfg.SingBoxShardCount = saved.SingBoxShardCount
	}
	if saved.BlockedCountries != nil {
		cfg.BlockedCountries = NormalizeCountryCodes(saved.BlockedCountries)
	}
	if saved.AllowedCountries != nil {
		cfg.AllowedCountries = NormalizeCountryCodes(saved.AllowedCountries)
	}
}

func envOrDefault(key string, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func hashIfSet(value string) string {
	if value == "" {
		return ""
	}
	return passwordHash(value)
}

func envPort(key string, defaultValue string) string {
	return normalizePort(envOrDefault(key, defaultValue))
}

func normalizePort(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, ":") {
		return value
	}
	return ":" + value
}

func envInt(key string, defaultValue int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return defaultValue
	}
	return value
}

func envCountries(key string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	countries := make([]string, 0, len(parts))
	for _, part := range parts {
		country := NormalizeCountryCode(part)
		if country != "" {
			countries = append(countries, country)
		}
	}
	return countries
}

func NormalizeCountryCode(value string) string {
	code := strings.ToUpper(strings.TrimSpace(value))
	if len(code) != 2 {
		return ""
	}
	for _, ch := range code {
		if ch < 'A' || ch > 'Z' {
			return ""
		}
	}
	return code
}

func NormalizeCountryCodes(values []string) []string {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		code := NormalizeCountryCode(value)
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		normalized = append(normalized, code)
	}
	return normalized
}

// envCountriesDefault 与 envCountries 类似，但在环境变量“未设置”时返回给定默认值。
// 用 LookupEnv 区分“未设置”和“显式设为空”：显式设为空表示用户主动清空该名单，
// 此时返回空而非默认，保证用户可以关闭默认屏蔽。
func envCountriesDefault(key string, defaultValue []string) []string {
	raw, present := os.LookupEnv(key)
	if !present {
		return defaultValue
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return envCountries(key)
}
