package selector

import (
	"encoding/json"
	"strings"

	"github.com/babutree/GeoProxy/storage"
)

// proxyMatchesUnlock 判断节点是否满足全部必需的解锁条件。
// 必需令牌使用标准名称：openai/claude/grok/gemini/cf。
// AI JSON 缺失或无效，或结果未探测（-1）时，不满足对应的 AI 解锁条件。
// CF 要求 cf_blocked == 0（开放）；未探测（-1）和已阻断（1）均不满足。
func proxyMatchesUnlock(proxy storage.Proxy, unlock []string) bool {
	if len(unlock) == 0 {
		return true
	}
	var ai map[string]int
	raw := strings.TrimSpace(proxy.AIReachability)
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &ai)
	}
	for _, req := range unlock {
		switch req {
		case "cf":
			if proxy.CFBlocked != 0 {
				return false
			}
		case "openai", "claude", "grok", "gemini":
			if ai == nil {
				return false
			}
			if v, ok := ai[req]; !ok || v != 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func filterByUnlock(proxies []storage.Proxy, unlock []string) []storage.Proxy {
	if len(unlock) == 0 {
		return proxies
	}
	out := make([]storage.Proxy, 0, len(proxies))
	for _, p := range proxies {
		if proxyMatchesUnlock(p, unlock) {
			out = append(out, p)
		}
	}
	return out
}
