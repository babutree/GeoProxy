package config

import (
	"crypto/pbkdf2"
	crand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// ===== WebUI 登录密码哈希 =====
//
// WebUI 登录密码只存哈希（明文从不落盘），是 config.json 泄露后唯一还能提供
// 保护的凭据。原实现是无盐 SHA-256：单次哈希、无盐，对彩虹表和 GPU 爆破
// 几乎没有抵抗力。现改为加盐 PBKDF2-HMAC-SHA256。
//
// 为什么只升级 WebUI 密码、不升级代理认证密码（ProxyAuthPasswordHash）：
//
//  1. 代理认证在**每个 HTTP 请求 / 每条 SOCKS5 连接**上校验
//     （proxy/server.go 的 checkAuthWithConfig、socks5_server.go 的认证分支）。
//     PBKDF2 在本机实测 210k 次迭代约 98ms，100 req/s 就需要约 10 个 CPU 核，
//     网关会直接瘫痪。密码哈希的慢是为了拖慢离线爆破，不能放在热路径上。
//  2. 更根本的是：代理密码的**明文**按设计与哈希存在同一个 config.json
//     （ProxyAuthPassword，供 WebUI 复制含密码的完整代理 URL）。能读到哈希的
//     攻击者同时就读到了明文，强化哈希保护不了任何东西。
//
// 所以这不是"漏改"，是两者安全模型本就不同：登录密码是纯哈希凭据，
// 代理密码是明文凭据。
const (
	pbkdf2Scheme = "pbkdf2-sha256"
	// WebUIPasswordHashScheme 导出方案前缀，供调用方判断存量哈希是否已升级。
	WebUIPasswordHashScheme = pbkdf2Scheme
	// pbkdf2Iterations 取 OWASP 对 PBKDF2-HMAC-SHA256 的推荐区间下沿。
	// 登录是低频操作且有失败锁定（5 次失败锁 1 分钟），单次约 100ms 对用户
	// 无感；再往上调会放大登录接口的 CPU 放大攻击面，收益却有限——
	// 自动生成的密码是 16 字符 × 55 字符表（约 92.5 bit 熵），本就不可爆破，
	// 迭代次数真正保护的是用户手工设置的弱密码。
	pbkdf2Iterations = 210_000
	pbkdf2SaltBytes  = 16
	pbkdf2KeyBytes   = 32
	// legacySHA256HexLen 是旧无盐 SHA-256 哈希的十六进制长度。
	legacySHA256HexLen = sha256.Size * 2
)

// HashWebUIPassword 生成加盐 PBKDF2 哈希，编码为
// pbkdf2-sha256$<迭代次数>$<base64 盐>$<base64 派生密钥>。
// 迭代次数写进串里，将来调整参数时旧哈希仍可校验。
func HashWebUIPassword(plain string) (string, error) {
	salt := make([]byte, pbkdf2SaltBytes)
	if _, err := crand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	return encodePBKDF2(plain, salt, pbkdf2Iterations)
}

func encodePBKDF2(plain string, salt []byte, iterations int) (string, error) {
	key, err := pbkdf2.Key(sha256.New, plain, salt, iterations, pbkdf2KeyBytes)
	if err != nil {
		return "", fmt.Errorf("derive password key: %w", err)
	}
	return fmt.Sprintf("%s$%d$%s$%s",
		pbkdf2Scheme,
		iterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyWebUIPassword 校验登录密码。
//
// 返回 (匹配, 需要升级)：needsUpgrade 为 true 表示存量哈希还是旧的无盐
// SHA-256，调用方应在校验通过后用 HashWebUIPassword 重新哈希并落盘。
// 这是唯一可行的迁移时机——只有此刻才拿得到明文。
//
// 校验失败时 needsUpgrade 恒为 false：绝不能因为一次失败的登录尝试就改写
// 已存储的凭据。
func VerifyWebUIPassword(plain, encoded string) (ok bool, needsUpgrade bool) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return false, false
	}
	if strings.HasPrefix(encoded, pbkdf2Scheme+"$") {
		return verifyPBKDF2(plain, encoded), false
	}
	// 存量无盐 SHA-256：校验通过则提示升级。
	if verifyLegacySHA256(plain, encoded) {
		return true, true
	}
	return false, false
}

func verifyPBKDF2(plain, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != pbkdf2Scheme {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations <= 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(salt) == 0 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(expected) == 0 {
		return false
	}
	actual, err := pbkdf2.Key(sha256.New, plain, salt, iterations, len(expected))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func verifyLegacySHA256(plain, encoded string) bool {
	if len(encoded) != legacySHA256HexLen {
		return false
	}
	expected := make([]byte, sha256.Size)
	n, err := hex.Decode(expected, []byte(encoded))
	if err != nil || n != sha256.Size {
		return false
	}
	actual := sha256.Sum256([]byte(plain))
	return subtle.ConstantTimeCompare(actual[:], expected) == 1
}
