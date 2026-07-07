package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

const DefaultPassword = "goproxy"

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
	MaxRetry               int
}

var (
	globalCfg *Config
	cfgMu     sync.RWMutex
)

func passwordHash(plain string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(plain)))
}

func DefaultConfig() *Config {
	password := envOrDefault("WEBUI_PASSWORD", DefaultPassword)
	proxyPassword := os.Getenv("PROXY_AUTH_PASSWORD")
	singBoxPath := envOrDefault("SINGBOX_PATH", "sing-box")
	return &Config{
		HTTPPort:               envPort("HTTP_PORT", ":7802"),
		SOCKS5Port:             envPort("SOCKS5_PORT", ":7801"),
		WebUIPort:              envPort("WEBUI_PORT", ":7800"),
		WebUIPasswordHash:      passwordHash(password),
		ProxyAuthEnabled:       os.Getenv("PROXY_AUTH_ENABLED") == "true",
		ProxyAuthUsername:      envOrDefault("PROXY_AUTH_USERNAME", "acct"),
		ProxyAuthPassword:      proxyPassword,
		ProxyAuthPasswordHash:  hashIfSet(proxyPassword),
		SessionTTLMinutes:      envInt("SESSION_TTL_MINUTES", 10),
		DefaultRegion:          strings.ToLower(strings.TrimSpace(os.Getenv("DEFAULT_REGION"))),
		BlockedCountries:       envCountries("BLOCKED_COUNTRIES"),
		AllowedCountries:       envCountries("ALLOWED_COUNTRIES"),
		DBPath:                 dataDir() + "proxy.db",
		ValidateConcurrency:    300,
		ValidateTimeout:        10,
		ValidateURL:            "http://www.gstatic.com/generate_204",
		MaxResponseMs:          5000,
		HealthIntervalMinutes:  envInt("HEALTH_CHECK_INTERVAL", 5),
		HealthCheckBatchSize:   20,
		HealthCheckConcurrency: 50,
		CustomProbeInterval:    10,
		CustomRefreshInterval:  60,
		SingBoxPath:            singBoxPath,
		SingBoxBasePort:        20000,
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
	cfgMu.Lock()
	globalCfg = cfg
	cfgMu.Unlock()
	return cfg
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
	ProxyAuthEnabled      *bool    `json:"proxy_auth_enabled,omitempty"`
	ProxyAuthUsername     string   `json:"proxy_auth_username,omitempty"`
	ProxyAuthPassword     string   `json:"proxy_auth_password,omitempty"`
	SessionTTLMinutes     int      `json:"session_ttl_minutes,omitempty"`
	DefaultRegion         string   `json:"default_region,omitempty"`
	HealthIntervalMinutes int      `json:"health_check_interval,omitempty"`
	MaxRetry              *int     `json:"max_retry,omitempty"`
	SingBoxPath           string   `json:"singbox_path,omitempty"`
	BlockedCountries      []string `json:"blocked_countries,omitempty"`
	AllowedCountries      []string `json:"allowed_countries,omitempty"`
}

func Save(cfg *Config) error {
	cfgMu.Lock()
	if globalCfg == nil {
		globalCfg = &Config{}
	}
	*globalCfg = *cfg
	cfgMu.Unlock()

	authEnabled := cfg.ProxyAuthEnabled
	maxRetry := cfg.MaxRetry
	data, err := json.MarshalIndent(savedConfig{
		HTTPPort:              cfg.HTTPPort,
		SOCKS5Port:            cfg.SOCKS5Port,
		WebUIPort:             cfg.WebUIPort,
		ProxyAuthEnabled:      &authEnabled,
		ProxyAuthUsername:     cfg.ProxyAuthUsername,
		ProxyAuthPassword:     cfg.ProxyAuthPassword,
		SessionTTLMinutes:     cfg.SessionTTLMinutes,
		DefaultRegion:         cfg.DefaultRegion,
		HealthIntervalMinutes: cfg.HealthIntervalMinutes,
		MaxRetry:              &maxRetry,
		SingBoxPath:           cfg.SingBoxPath,
		BlockedCountries:      cfg.BlockedCountries,
		AllowedCountries:      cfg.AllowedCountries,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigFile(), data, 0644)
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
	if saved.ProxyAuthEnabled != nil {
		cfg.ProxyAuthEnabled = *saved.ProxyAuthEnabled
	}
	if saved.ProxyAuthUsername != "" {
		cfg.ProxyAuthUsername = saved.ProxyAuthUsername
	}
	if saved.ProxyAuthPassword != "" {
		cfg.ProxyAuthPassword = saved.ProxyAuthPassword
		cfg.ProxyAuthPasswordHash = passwordHash(saved.ProxyAuthPassword)
	}
	if saved.SessionTTLMinutes > 0 {
		cfg.SessionTTLMinutes = saved.SessionTTLMinutes
	}
	if saved.DefaultRegion != "" {
		cfg.DefaultRegion = strings.ToLower(strings.TrimSpace(saved.DefaultRegion))
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
	if saved.BlockedCountries != nil {
		cfg.BlockedCountries = saved.BlockedCountries
	}
	if saved.AllowedCountries != nil {
		cfg.AllowedCountries = saved.AllowedCountries
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
		country := strings.ToUpper(strings.TrimSpace(part))
		if country != "" {
			countries = append(countries, country)
		}
	}
	return countries
}
