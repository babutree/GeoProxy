package selector

import (
	"encoding/json"
	"strings"

	"github.com/babutree/GeoProxy/storage"
)

// proxyMatchesUnlock returns true when the proxy satisfies every required unlock filter.
// Required tokens use normalized names: openai/claude/grok/gemini/cf.
// Missing/invalid AI JSON or unprobed (-1) does not satisfy a required AI filter.
// CF requires cf_blocked == 0 (open); -1 unprobed and 1 blocked fail.
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
