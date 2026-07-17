package config

import (
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	HTTPPort              string
	SOCKS5Port            string
	WebUIPort             string
	WebUIPasswordHash     string
	ProxyAuthEnabled      bool
	ProxyAuthUsername     string
	ProxyAuthPassword     string
	ProxyAuthPasswordHash string
	SessionTTLMinutes     int
	// MaxSessionsPerProxy limits concurrent sticky sessions per proxy node.
	// 0 means unlimited (default, backward compatible). Values < 0 are rejected on Save.
	MaxSessionsPerProxy int
	// ProxyCooldownMinutes is the quiet period after a new session first-bind
	// during which other new sessions must not bind the same proxy.
	// 0 disables cooldown (default). Values < 0 are rejected on Save.
	ProxyCooldownMinutes   int
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
	// ReadOnlyAPIKeys holds read-only API credentials as SHA-256 hashes only (never plaintext).
	ReadOnlyAPIKeys []APIKey
	// PublicHost is an optional public hostname/IP override for external connect.host.
	PublicHost string
	// ReadOnlyAPIRatePerMin is the per-key rate limit for the read-only API (default 60).
	ReadOnlyAPIRatePerMin int
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
		ProxyAuthUsername:      "username",
		SessionTTLMinutes:      envInt("SESSION_TTL_MINUTES", 10),
		MaxSessionsPerProxy:    envIntNonNegative("MAX_SESSIONS_PER_PROXY", 0),
		ProxyCooldownMinutes:   envIntNonNegative("PROXY_COOLDOWN_MINUTES", 0),
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
		PublicHost:             strings.TrimSpace(os.Getenv("PUBLIC_HOST")),
		ReadOnlyAPIRatePerMin:  envInt("READONLY_API_RATE_PER_MIN", 60),
	}
}

func Load() *Config {
	cfg := DefaultConfig()
	data, err := os.ReadFile(ConfigFile())
	hasPersistedConfig := err == nil
	if hasPersistedConfig {
		// 已落盘配置是权威来源，不能被部署环境中的旧只读 API 变量覆盖。
		cfg.PublicHost = ""
		cfg.ReadOnlyAPIRatePerMin = 60
		var saved savedConfig
		if err := json.Unmarshal(data, &saved); err != nil {
			panic(fmt.Sprintf("load config: parse %s: %v", ConfigFile(), err))
		}
		applySavedConfig(cfg, saved)
	}

	// 首次启动引导：config.json 尚无凭据时，生成随机凭据并落盘。
	// WebUI 登录密码只存 hash；代理认证密码为支持复制完整 URL 会明文写入 config.json（见 Save）。
	// firstBoot 仅缓存明文供日志一次性展示。
	needBootstrap := cfg.WebUIPasswordHash == "" || cfg.ProxyAuthPasswordHash == ""
	if needBootstrap {
		bootstrapCredentials(cfg)
	}

	if !hasPersistedConfig {
		imported := importReadOnlyAPIKeysFromEnv(cfg)
		if imported {
			if err := Save(cfg); err != nil {
				panic(fmt.Sprintf("persist readonly api keys: %v", err))
			}
		}
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
			cfg.ProxyAuthUsername = "username"
		}
		info.ProxyAuthUsername = cfg.ProxyAuthUsername
		info.ProxyAuthPassword = generateCredential()
		cfg.ProxyAuthPasswordHash = passwordHash(info.ProxyAuthPassword)
		// 代理密码保留明文到运行态，供 Save 落盘、WebUI 复制含密码的完整代理 URL。
		// 这是有意的设计取舍：代理密码明文存储，登录密码仍只存哈希，两者安全模型分开。
		cfg.ProxyAuthPassword = info.ProxyAuthPassword
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

// SetGlobal 发布配置快照到进程全局。
// 生产路径由 Load/Save 调用；测试可用来安装/恢复快照，避免请求路径继续读旧指针。
func SetGlobal(cfg *Config) {
	cfgMu.Lock()
	globalCfg = cfg
	cfgMu.Unlock()
}

type savedConfig struct {
	HTTPPort              string `json:"http_port,omitempty"`
	SOCKS5Port            string `json:"socks5_port,omitempty"`
	WebUIPort             string `json:"webui_port,omitempty"`
	WebUIPasswordHash     string `json:"webui_password_hash,omitempty"`
	ProxyAuthEnabled      *bool  `json:"proxy_auth_enabled,omitempty"`
	ProxyAuthUsername     string `json:"proxy_auth_username,omitempty"`
	ProxyAuthPassword     string `json:"proxy_auth_password,omitempty"`
	ProxyAuthPasswordHash string `json:"proxy_auth_password_hash,omitempty"`
	SessionTTLMinutes     int    `json:"session_ttl_minutes,omitempty"`
	// pointer so 0 (unlimited / disabled) can be distinguished from "field absent"
	MaxSessionsPerProxy   *int     `json:"max_sessions_per_proxy,omitempty"`
	ProxyCooldownMinutes  *int     `json:"proxy_cooldown_minutes,omitempty"`
	DefaultRegion         string   `json:"default_region,omitempty"`
	HealthIntervalMinutes int      `json:"health_check_interval,omitempty"`
	MaxRetry              *int     `json:"max_retry,omitempty"`
	SingBoxPath           string   `json:"singbox_path,omitempty"`
	SingBoxShardCount     int      `json:"singbox_shard_count,omitempty"`
	BlockedCountries      []string `json:"blocked_countries,omitempty"`
	AllowedCountries      []string `json:"allowed_countries,omitempty"`
	ReadOnlyAPIKeys       []APIKey `json:"readonly_api_keys,omitempty"`
	PublicHost            string   `json:"public_host,omitempty"`
	// pointer so a non-default rate can round-trip; absent means keep DefaultConfig value
	ReadOnlyAPIRatePerMin *int `json:"readonly_api_rate_per_min,omitempty"`
}

func Save(cfg *Config) error {
	return saveConfig(cfg, os.Rename)
}

func saveConfig(cfg *Config, replace func(string, string) error) error {
	if cfg.MaxSessionsPerProxy < 0 {
		return fmt.Errorf("max_sessions_per_proxy must be >= 0, got %d", cfg.MaxSessionsPerProxy)
	}
	if cfg.ProxyCooldownMinutes < 0 {
		return fmt.Errorf("proxy_cooldown_minutes must be >= 0, got %d", cfg.ProxyCooldownMinutes)
	}
	authEnabled := cfg.ProxyAuthEnabled
	maxRetry := cfg.MaxRetry
	maxSessions := cfg.MaxSessionsPerProxy
	cooldown := cfg.ProxyCooldownMinutes
	ratePerMin := cfg.ReadOnlyAPIRatePerMin
	data, err := json.MarshalIndent(savedConfig{
		HTTPPort:              cfg.HTTPPort,
		SOCKS5Port:            cfg.SOCKS5Port,
		WebUIPort:             cfg.WebUIPort,
		WebUIPasswordHash:     cfg.WebUIPasswordHash,
		ProxyAuthEnabled:      &authEnabled,
		ProxyAuthUsername:     cfg.ProxyAuthUsername,
		ProxyAuthPassword:     cfg.ProxyAuthPassword,
		ProxyAuthPasswordHash: cfg.ProxyAuthPasswordHash,
		SessionTTLMinutes:     cfg.SessionTTLMinutes,
		MaxSessionsPerProxy:   &maxSessions,
		ProxyCooldownMinutes:  &cooldown,
		DefaultRegion:         NormalizeCountryCode(cfg.DefaultRegion),
		HealthIntervalMinutes: cfg.HealthIntervalMinutes,
		MaxRetry:              &maxRetry,
		SingBoxPath:           cfg.SingBoxPath,
		SingBoxShardCount:     cfg.SingBoxShardCount,
		BlockedCountries:      NormalizeCountryCodes(cfg.BlockedCountries),
		AllowedCountries:      NormalizeCountryCodes(cfg.AllowedCountries),
		ReadOnlyAPIKeys:       cfg.ReadOnlyAPIKeys,
		PublicHost:            strings.TrimSpace(cfg.PublicHost),
		ReadOnlyAPIRatePerMin: &ratePerMin,
	}, "", "  ")
	if err != nil {
		return err
	}
	targetPath := ConfigFile()
	tempFile, err := os.CreateTemp(filepath.Dir(targetPath), ".config-*.tmp")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	if err := tempFile.Chmod(0600); err != nil {
		tempFile.Close()
		return err
	}
	written, err := tempFile.Write(data)
	if err == nil && written != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		tempFile.Close()
		return err
	}
	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := replace(tempPath, targetPath); err != nil {
		return err
	}

	saved := *cfg
	// 代理密码保留明文在运行态，供已认证 WebUI 一键复制含密码的完整代理 URL；不再清空。
	// 注意：WebUI 登录密码仍只存哈希（WebUIPasswordHash），此处不涉及登录密码。
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
	if saved.ProxyAuthPassword != "" {
		// 代理密码明文往返恢复，供复制含密码的完整代理 URL。
		cfg.ProxyAuthPassword = saved.ProxyAuthPassword
	}
	if saved.ProxyAuthPasswordHash != "" {
		cfg.ProxyAuthPasswordHash = saved.ProxyAuthPasswordHash
	} else if saved.ProxyAuthPassword != "" {
		cfg.ProxyAuthPasswordHash = passwordHash(saved.ProxyAuthPassword)
	}
	if saved.SessionTTLMinutes > 0 {
		cfg.SessionTTLMinutes = saved.SessionTTLMinutes
	}
	if saved.MaxSessionsPerProxy != nil {
		if *saved.MaxSessionsPerProxy < 0 {
			// corrupt config: keep default (unlimited) rather than panic
			cfg.MaxSessionsPerProxy = 0
		} else {
			cfg.MaxSessionsPerProxy = *saved.MaxSessionsPerProxy
		}
	}
	if saved.ProxyCooldownMinutes != nil {
		if *saved.ProxyCooldownMinutes < 0 {
			cfg.ProxyCooldownMinutes = 0
		} else {
			cfg.ProxyCooldownMinutes = *saved.ProxyCooldownMinutes
		}
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
	if saved.ReadOnlyAPIKeys != nil {
		cfg.ReadOnlyAPIKeys = saved.ReadOnlyAPIKeys
	}
	cfg.PublicHost = strings.TrimSpace(saved.PublicHost)
	if saved.ReadOnlyAPIRatePerMin != nil && *saved.ReadOnlyAPIRatePerMin > 0 {
		cfg.ReadOnlyAPIRatePerMin = *saved.ReadOnlyAPIRatePerMin
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

// envIntNonNegative parses env as int allowing 0. Empty/unset/invalid → defaultValue.
// Negative values are rejected and fall back to defaultValue.
func envIntNonNegative(key string, defaultValue int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
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
