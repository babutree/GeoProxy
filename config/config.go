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

const defaultDataDirName = "GeoProxy"

var legacyWorkingDirectoryMarkers = []string{
	"config.json",
	"proxy.db",
	"proxy.db-wal",
	"proxy.db-shm",
	"data.db",
	"data.db-wal",
	"data.db-shm",
	"subscriptions",
}

// configuredDataDir 返回显式 DATA_DIR；空值视为未配置，以便使用平台默认目录。
func configuredDataDir() (string, bool) {
	if raw := strings.TrimSpace(os.Getenv("DATA_DIR")); raw != "" {
		return filepath.Clean(raw), true
	}
	return "", false
}

func resolveDataDir() (string, bool, error) {
	if dir, explicit := configuredDataDir(); explicit {
		return dir, true, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", false, fmt.Errorf("resolve default data directory: %w", err)
	}
	if strings.TrimSpace(base) == "" {
		return "", false, fmt.Errorf("resolve default data directory: user config directory is empty")
	}
	base, err = filepath.Abs(base)
	if err != nil {
		return "", false, fmt.Errorf("resolve default data directory %q: %w", base, err)
	}
	return filepath.Join(base, defaultDataDirName), false, nil
}

func ensureDataDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("create data directory: path is empty")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create data directory %q: %w", dir, err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat data directory %q: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("create data directory %q: path is not a directory", dir)
	}
	return nil
}

// dataDir 只解析数据目录，不隐式创建；Load/Save 在明确的启动/写入边界创建目录。
func dataDir() string {
	dir, _, err := resolveDataDir()
	if err != nil {
		panic(err)
	}
	return dir
}

func dataDirOrEmpty() string {
	dir, _, err := resolveDataDir()
	if err != nil {
		return ""
	}
	return dir
}

func defaultDBPath() string {
	dir := dataDirOrEmpty()
	if dir == "" {
		// DefaultConfig 无 error 返回值；平台目录不可解析时保留空 DBPath，
		// 由 Load/DataDir 在有错误语义的边界显式中止，绝不回退到 CWD。
		return ""
	}
	return filepath.Join(dir, "proxy.db")
}

func ConfigFile() string { return filepath.Join(dataDir(), "config.json") }

// DataDir 返回当前配置解析出的数据目录；调用方负责在写入前创建它。
// 跨包写入入口应使用本函数，避免重新读取 DATA_DIR 后与默认目录策略漂移。
func DataDir() (string, error) {
	dir, _, err := resolveDataDir()
	if err != nil {
		return "", err
	}
	return dir, nil
}

func rejectLegacyWorkingDirectoryData(targetDataDir string) error {
	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("inspect legacy working-directory data: %w", err)
	}
	workingInfo, err := os.Stat(workingDir)
	if err != nil {
		return fmt.Errorf("inspect working directory %q: %w", workingDir, err)
	}
	targetInfo, err := os.Stat(targetDataDir)
	if err == nil && os.SameFile(workingInfo, targetInfo) {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect target data directory %q: %w", targetDataDir, err)
	}
	found := make([]string, 0, len(legacyWorkingDirectoryMarkers))
	for _, marker := range legacyWorkingDirectoryMarkers {
		_, err := os.Stat(filepath.Join(workingDir, marker))
		if err == nil {
			found = append(found, marker)
			continue
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("inspect legacy working-directory marker %q: %w", marker, err)
		}
	}
	if len(found) == 0 {
		return nil
	}
	return fmt.Errorf("DATA_DIR 未设置，当前工作目录 %q 存在旧运行时数据（%s）；为避免生成新身份，请设置 DATA_DIR 指向该旧目录，或人工迁移后再启动；不会自动迁移", workingDir, strings.Join(found, ", "))
}

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
	// MaxSessionsPerProxy 限制每个代理节点的并发粘性会话数。
	// 0 表示无限制（默认值，保持向后兼容）；Save 会拒绝小于 0 的值。
	MaxSessionsPerProxy int
	// ProxyCooldownMinutes 表示新会话首次绑定后的冷却期；
	// 在此期间，其他新会话不得绑定同一代理。
	// 0 表示禁用冷却（默认值）；Save 会拒绝小于 0 的值。
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
	// ReadOnlyAPIKeys 仅以 SHA-256 哈希保存只读 API 凭据，绝不保存明文。
	ReadOnlyAPIKeys []APIKey
	// PublicHost 是可选的公共主机名/IP，用于覆盖对外返回的 connect.host。
	PublicHost string
	// ReadOnlyAPIRatePerMin 是只读 API 的单密钥每分钟限速（默认 60）。
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
		SessionTTLMinutes:      envInt("SESSION_TTL_MINUTES", 1440),
		MaxSessionsPerProxy:    envIntNonNegative("MAX_SESSIONS_PER_PROXY", 0),
		ProxyCooldownMinutes:   envIntNonNegative("PROXY_COOLDOWN_MINUTES", 0),
		DefaultRegion:          NormalizeCountryCode(os.Getenv("DEFAULT_REGION")),
		BlockedCountries:       envCountriesDefault("BLOCKED_COUNTRIES", []string{"CN"}),
		AllowedCountries:       envCountries("ALLOWED_COUNTRIES"),
		DBPath:                 defaultDBPath(),
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
	dataDirPath, explicitDataDir, err := resolveDataDir()
	if err != nil {
		panic(err)
	}
	if !explicitDataDir {
		if err := rejectLegacyWorkingDirectoryData(dataDirPath); err != nil {
			panic(err)
		}
	}
	if err := ensureDataDir(dataDirPath); err != nil {
		panic(err)
	}

	cfg := DefaultConfig()
	// Load 已在上方成功解析并准备 dataDirPath；使用同一快照，避免环境变化导致 DBPath 漂移。
	cfg.DBPath = filepath.Join(dataDirPath, "proxy.db")
	configPath := filepath.Join(dataDirPath, "config.json")
	data, err := os.ReadFile(configPath)
	hasPersistedConfig := err == nil
	if hasPersistedConfig {
		// 已落盘配置是权威来源，不能被部署环境中的旧只读 API 变量覆盖。
		cfg.PublicHost = ""
		cfg.ReadOnlyAPIRatePerMin = 60
		var saved savedConfig
		if err := json.Unmarshal(data, &saved); err != nil {
			panic(fmt.Sprintf("load config: parse %s: %v", configPath, err))
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
	// 使用指针区分 0（无限制/禁用）与“字段缺失”。
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
	// 使用指针保证非默认限速值可往返序列化；缺失时保留 DefaultConfig 的值。
	ReadOnlyAPIRatePerMin *int `json:"readonly_api_rate_per_min,omitempty"`
}

func Save(cfg *Config) error {
	dataDirPath, _, err := resolveDataDir()
	if err != nil {
		return err
	}
	if err := ensureDataDir(dataDirPath); err != nil {
		return err
	}
	return saveConfigAt(cfg, os.Rename, filepath.Join(dataDirPath, "config.json"))
}

func saveConfig(cfg *Config, replace func(string, string) error) error {
	return saveConfigAt(cfg, replace, ConfigFile())
}

func saveConfigAt(cfg *Config, replace func(string, string) error, targetPath string) error {
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
			// 配置损坏时保留默认值（无限制），避免 panic。
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

// envIntNonNegative 将环境变量解析为允许 0 的整数；空值、未设置或无效值返回 defaultValue。
// 负数会被拒绝，并回退到 defaultValue。
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
