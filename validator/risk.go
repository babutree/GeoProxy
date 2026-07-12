package validator

import (
	"strconv"
	"strings"
)

// IPAPIIsUnknown 表示 ipapi.is 未探测/查询失败时的 abuser_score 取值。
// 前端据此显示 "--"，存储层据此不覆盖已有有效分。
const IPAPIIsUnknown = -1.0

// RiskInfo 承载两源风险信号，分开展示、不聚合。
//   - IPAPIIsScore：ipapi.is abuser_score（0-1）；IPAPIIsUnknown(-1) 表示未探测/失败。
//   - Flags：ip-api 命中标记（proxy/hosting/mobile），已按稳定顺序去重拼接；空表示无命中或未探测。
type RiskInfo struct {
	IPAPIIsScore float64
	Flags        string
	CFBlocked    int // Cloudflare 拦截：-1未探测 0未拦截 1被拦截
	// AIReachability 是 4 个 AI 服务可达性的 JSON 对象字符串，形如
	// {"openai":0,"claude":1,"grok":-1,"gemini":0}。值语义：-1未探测 0可达 1不可达。
	// 空字符串表示整体未探测（存储层据此不覆盖已有值）。
	AIReachability string
}

// UnknownRisk 是两源都未取得有效信号时的零信息风险信息。
func UnknownRisk() RiskInfo {
	return RiskInfo{IPAPIIsScore: IPAPIIsUnknown, Flags: "", CFBlocked: -1, AIReachability: ""}
}

// parseAbuserScore 解析 ipapi.is 的 abuser_score 字段。
// 该字段返回形如 "0.0039 (Low)" 的字符串，需要提取前导数值部分。
// 无法解析时返回 0，调用方据 ipapi.is 是否成功决定是否使用。
func parseAbuserScore(raw string) float64 {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0
	}
	// 取第一个空白/括号之前的 token 作为数值部分（"0.0039 (Low)" -> "0.0039"）。
	if idx := strings.IndexAny(s, " \t("); idx >= 0 {
		s = s[:idx]
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		// abuser_score 归一化在 0-1；超范围裁剪，避免异常放大分数。
		v = 1
	}
	return v
}

// ipapiFlags 将 ip-api 的布尔信号拼成稳定的逗号分隔标记串（命中即列出）。
// 顺序固定为 proxy,hosting,mobile，便于前端稳定渲染与测试断言。
func ipapiFlags(proxy, hosting, mobile bool) string {
	var flags []string
	if proxy {
		flags = append(flags, "proxy")
	}
	if hosting {
		flags = append(flags, "hosting")
	}
	if mobile {
		flags = append(flags, "mobile")
	}
	return strings.Join(flags, ",")
}
