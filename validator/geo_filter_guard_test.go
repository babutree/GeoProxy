package validator

import (
	"testing"

	"github.com/babutree/GeoProxy/config"
)

// TestExitCountryCodeMatchesConfigNormalization 锁定出口国家码与配置侧共用同一套
// 归一化。地理过滤两端必须同源：出口侧经 newExitIPInfo → NormalizeCountryCode，
// 配置侧经 NormalizeCountryCodes；任一端改用别的规则都会让过滤静默失效。
func TestExitCountryCodeMatchesConfigNormalization(t *testing.T) {
	for _, tc := range []struct {
		location string
		want     string
	}{
		{"US", "US"},
		{"US Seattle", "US"},
		{"us seattle", "US"}, // 归一化为大写，与配置侧一致
		{"  JP Tokyo", "JP"}, // 前导空白不影响取码（切片写法会取到 "  "）
		{"USA Miami", ""},    // 非 alpha-2 不得被截断成 "US"
		{"U", ""},            // 过短
		{"1S Somewhere", ""}, // 非字母
		{"", ""},             // 空
	} {
		t.Run(tc.location, func(t *testing.T) {
			if got := exitCountryCode(tc.location); got != tc.want {
				t.Fatalf("exitCountryCode(%q) = %q, want %q", tc.location, got, tc.want)
			}
			// 与配置侧同源校验：合法码经配置归一化后必须不变。
			if tc.want != "" && config.NormalizeCountryCode(tc.want) != tc.want {
				t.Fatalf("config.NormalizeCountryCode(%q) != %q; geo filter ends would diverge", tc.want, tc.want)
			}
		})
	}
}

// TestGeoDecisionIsFailClosed 锁定地理过滤的 fail-closed 语义与国家码提取规则。
//
// 原实现是 `if len(exitLocation) >= 2 && !passesGeoFilter(exitLocation[:2])`：
//   - 取不出国家码时整个跳过过滤并放行节点（fail-open）；
//   - `[:2]` 直接截断，"CNX Somewhere" 被误判成被屏蔽的 "CN"，
//     " US Seattle" 取到 "  " 而漏过过滤。
//
// 地理过滤是策略控制：无法判定出口国家时必须拒绝，绝不能落到"通过"分支。
func TestGeoDecisionIsFailClosed(t *testing.T) {
	v := newValidator(1, 1, "http://127.0.0.1:1/never", &config.Config{
		BlockedCountries: []string{"CN"},
	})

	for _, tc := range []struct {
		name       string
		location   string
		wantPass   bool
		wantReason FailureReason
	}{
		// 正常放行/拒绝。
		{"allowed country", "US Seattle", true, FailureNone},
		{"allowed bare code", "US", true, FailureNone},
		{"blocked country", "CN Beijing", false, FailureGeoRejected},
		{"blocked bare code", "CN", false, FailureGeoRejected},

		// fail-closed：取不出合法 alpha-2 一律不放行。
		{"empty location", "", false, FailureExitMetadata},
		{"single char", "U", false, FailureExitMetadata},
		{"non-alpha code", "1S Somewhere", false, FailureExitMetadata},
		{"three letter code", "USA Miami", false, FailureExitMetadata},

		// [:2] 截断陷阱：CNX 不是 CN，不得被当成被屏蔽国家；
		// 但也不能因此放行——必须归为元数据缺失。
		{"truncation trap", "CNX Somewhere", false, FailureExitMetadata},

		// 前导空白：[:2] 会取到 "  " 从而漏过过滤；Fields 取到真实码。
		{"leading whitespace blocked", "  CN Beijing", false, FailureGeoRejected},
		{"leading whitespace allowed", "  US Seattle", true, FailureNone},

		// 大小写：出口侧与配置侧同源归一化。
		{"lowercase blocked", "cn beijing", false, FailureGeoRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pass, reason := v.geoDecision(tc.location)
			if pass != tc.wantPass {
				t.Fatalf("geoDecision(%q) pass = %v, want %v (geo filter must fail closed)", tc.location, pass, tc.wantPass)
			}
			if reason != tc.wantReason {
				t.Fatalf("geoDecision(%q) reason = %q, want %q", tc.location, reason, tc.wantReason)
			}
		})
	}
}

// TestGeoDecisionWhitelistTakesPriority 确认白名单优先于黑名单的既有语义未被改动。
func TestGeoDecisionWhitelistTakesPriority(t *testing.T) {
	v := newValidator(1, 1, "http://127.0.0.1:1/never", &config.Config{
		AllowedCountries: []string{"JP"},
		BlockedCountries: []string{"CN"},
	})
	if pass, _ := v.geoDecision("JP Tokyo"); !pass {
		t.Fatal("geoDecision(JP) = false with JP whitelisted")
	}
	// 白名单非空时，不在白名单中的国家一律拒绝（即使不在黑名单）。
	if pass, reason := v.geoDecision("US Seattle"); pass || reason != FailureGeoRejected {
		t.Fatalf("geoDecision(US) = (%v, %q), want (false, %q) under a JP whitelist", pass, reason, FailureGeoRejected)
	}
}
