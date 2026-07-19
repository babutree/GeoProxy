package config

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"os"
	"strings"
	"time"
)

// HashAPIKey 是只读 API Key 的唯一规范指纹函数。
//
// 当前 WebUI 创建的 bearer token 由 16 字节 crypto/rand 随机数编码而成；这里的
// 裸 SHA-256 十六进制值用于避免在 config.json 中保存该高熵令牌明文，属于
// 兼容指纹，而不是低熵密码派生方案。已持久化配置和 READONLY_API_KEYS 导入
// 均使用此格式，鉴权也按该格式比较；未配套迁移就直接改用盐或 KDF 会使
// 现有集成失效。当前版本因此必须保持该持久化合同，但这不禁止未来通过
// 版本化字段、双格式验证和显式迁移演进算法。具体边界见
// docs/READONLY_API_DESIGN.md §3.2。config 与 webui 必须统一调用本函数，
// 避免出现相互漂移的第二套实现。
func HashAPIKey(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// APIKey 表示只读 API 凭据；当前仅持久化 SHA-256 指纹。
type APIKey struct {
	ID         string    `json:"id"`
	Name       string    `json:"name,omitempty"`
	Hash       string    `json:"hash"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
	LastUsedAt time.Time `json:"last_used_at,omitempty"`
	Disabled   bool      `json:"disabled,omitempty"`
}

// ValidateReadOnlyAPIKey 判断明文是否匹配任一未禁用的 Key 指纹。
func ValidateReadOnlyAPIKey(cfg *Config, plain string) bool {
	if cfg == nil || plain == "" {
		return false
	}
	want := HashAPIKey(plain)
	for _, key := range cfg.ReadOnlyAPIKeys {
		if key.Disabled || key.Hash == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(key.Hash), []byte(want)) == 1 {
			return true
		}
	}
	return false
}

func parseReadOnlyAPIKeysEnv(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		plain := strings.TrimSpace(part)
		if plain == "" {
			continue
		}
		if _, ok := seen[plain]; ok {
			continue
		}
		seen[plain] = struct{}{}
		out = append(out, plain)
	}
	return out
}

// importReadOnlyAPIKeysFromEnv 对 READONLY_API_KEYS 明文求指纹并追加缺失项。
// Config 与磁盘中均不保存明文。
func importReadOnlyAPIKeysFromEnv(cfg *Config) (changed bool) {
	plains := parseReadOnlyAPIKeysEnv(os.Getenv("READONLY_API_KEYS"))
	if len(plains) == 0 {
		return false
	}
	existing := make(map[string]struct{}, len(cfg.ReadOnlyAPIKeys))
	for _, k := range cfg.ReadOnlyAPIKeys {
		if k.Hash != "" {
			existing[k.Hash] = struct{}{}
		}
	}
	now := time.Now().UTC()
	for i, plain := range plains {
		h := HashAPIKey(plain)
		if _, ok := existing[h]; ok {
			continue
		}
		cfg.ReadOnlyAPIKeys = append(cfg.ReadOnlyAPIKeys, APIKey{
			ID:        generateCredential(),
			Name:      "env-import",
			Hash:      h,
			CreatedAt: now.Add(time.Duration(i) * time.Nanosecond),
		})
		existing[h] = struct{}{}
		changed = true
	}
	return changed
}
