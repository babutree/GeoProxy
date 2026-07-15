package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"goproxy/config"
)

func resetPublicIPCache() {
	pubIP.mu.Lock()
	pubIP.value = ""
	pubIP.country = ""
	pubIP.done = false
	pubIP.mu.Unlock()
}

func setPublicIPCache(ip, country string) {
	pubIP.mu.Lock()
	pubIP.value = ip
	pubIP.country = country
	pubIP.done = ip != ""
	pubIP.mu.Unlock()
}

func TestResolvePublicHostPrefersConfiguredPublicHost(t *testing.T) {
	t.Setenv("PUBLIC_HOST", "stale.example.com")
	setPublicIPCache("203.0.113.10", "US")
	t.Cleanup(resetPublicIPCache)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
	req.Host = "req.example.com:7800"

	host, unresolved := resolvePublicHost(&config.Config{PublicHost: "api.example.com"}, req)
	if unresolved {
		t.Fatalf("unresolved = true, want false")
	}
	if host != "api.example.com" {
		t.Fatalf("host = %q, want %q", host, "api.example.com")
	}
}

func TestResolvePublicHostRejectsPrivateCandidates(t *testing.T) {
	cases := []struct {
		name  string
		cfg   string
		env   string
		cache string
		host  string
	}{
		{name: "rfc1918", cfg: "10.0.0.7", env: "172.16.0.7", cache: "192.168.0.7", host: "10.0.0.8:7800"},
		{name: "cgnat", cfg: "100.64.3.7", env: "100.64.3.8", cache: "100.64.3.9", host: "100.64.3.10:7800"},
		{name: "link_local", cfg: "169.254.3.7", env: "169.254.3.8", cache: "169.254.3.9", host: "169.254.3.10:7800"},
		{name: "ipv6_ula", cfg: "fd12::7", env: "fd12::8", cache: "fd12::9", host: "[fd12::10]:7800"},
		{name: "ipv6_link_local", cfg: "fe80::7", env: "fe80::8", cache: "fe80::9", host: "[fe80::10]:7800"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PUBLIC_HOST", tc.env)
			setPublicIPCache(tc.cache, "")
			t.Cleanup(resetPublicIPCache)
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = tc.host

			host, unresolved := resolvePublicHost(&config.Config{PublicHost: tc.cfg}, req)
			if !unresolved || host != "" {
				t.Fatalf("host=%q unresolved=%v, want empty/true", host, unresolved)
			}
		})
	}
}

func TestResolvePublicHostDoesNotOverridePersistedConfigWithEnvironment(t *testing.T) {
	t.Setenv("PUBLIC_HOST", "stale-env.example.com")
	resetPublicIPCache()
	t.Cleanup(resetPublicIPCache)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "persisted-request.example.com:7800"

	host, unresolved := resolvePublicHost(&config.Config{}, req)
	if unresolved {
		t.Fatal("resolvePublicHost unexpectedly unresolved")
	}
	if host != "persisted-request.example.com" {
		t.Fatalf("host = %q, want request host after persisted config is empty", host)
	}
}

func TestResolvePublicHostUsesCachedPublicIPWhenNoOverride(t *testing.T) {
	t.Setenv("PUBLIC_HOST", "")
	setPublicIPCache("203.0.113.20", "JP")
	t.Cleanup(resetPublicIPCache)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "req.example.com:7800"

	host, unresolved := resolvePublicHost(nil, req)
	if unresolved {
		t.Fatalf("unresolved = true, want false")
	}
	if host != "203.0.113.20" {
		t.Fatalf("host = %q, want %q", host, "203.0.113.20")
	}
}

func TestResolvePublicHostFallsBackToRequestHost(t *testing.T) {
	t.Setenv("PUBLIC_HOST", "")
	resetPublicIPCache()
	t.Cleanup(resetPublicIPCache)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "gateway.example.net:7800"

	host, unresolved := resolvePublicHost(&config.Config{}, req)
	if unresolved {
		t.Fatalf("unresolved = true, want false")
	}
	if host != "gateway.example.net" {
		t.Fatalf("host = %q, want %q", host, "gateway.example.net")
	}
}

func TestResolvePublicHostSkipsLoopbackAtEachLevel(t *testing.T) {
	t.Setenv("PUBLIC_HOST", "127.0.0.1")
	setPublicIPCache("localhost", "")
	t.Cleanup(resetPublicIPCache)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "[::1]:7800"

	host, unresolved := resolvePublicHost(&config.Config{}, req)
	if !unresolved {
		t.Fatalf("unresolved = false, want true")
	}
	if host != "" {
		t.Fatalf("host = %q, want empty", host)
	}
}

func TestResolvePublicHostSkipsLoopbackOverrideUsesCache(t *testing.T) {
	t.Setenv("PUBLIC_HOST", "127.0.0.1")
	setPublicIPCache("198.51.100.7", "US")
	t.Cleanup(resetPublicIPCache)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "127.0.0.1:7800"

	host, unresolved := resolvePublicHost(&config.Config{}, req)
	if unresolved {
		t.Fatalf("unresolved = true, want false")
	}
	if host != "198.51.100.7" {
		t.Fatalf("host = %q, want %q", host, "198.51.100.7")
	}
}

func TestResolvePublicHostNeverReturns127001(t *testing.T) {
	cases := []struct {
		name    string
		env     string
		cache   string
		reqHost string
	}{
		{"env_loopback_only", "127.0.0.1", "", "127.0.0.1"},
		{"cache_loopback_only", "", "127.0.0.1", "127.0.0.1:7800"},
		{"req_localhost", "", "", "localhost:7800"},
		{"req_ipv6_loopback", "", "", "[::1]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" {
				t.Setenv("PUBLIC_HOST", tc.env)
			} else {
				t.Setenv("PUBLIC_HOST", "")
			}
			if tc.cache != "" {
				setPublicIPCache(tc.cache, "")
			} else {
				resetPublicIPCache()
			}
			t.Cleanup(resetPublicIPCache)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = tc.reqHost

			host, unresolved := resolvePublicHost(&config.Config{}, req)
			if host == "127.0.0.1" || host == "localhost" || host == "::1" {
				t.Fatalf("returned forbidden host %q", host)
			}
			if !unresolved {
				t.Fatalf("unresolved = false, want true when only loopback available")
			}
			if host != "" {
				t.Fatalf("host = %q, want empty when unresolved", host)
			}
		})
	}
}

func TestResolvePublicHostDoesNotTriggerProbe(t *testing.T) {
	t.Setenv("PUBLIC_HOST", "")
	resetPublicIPCache()
	t.Cleanup(resetPublicIPCache)

	// 缓存未 done：只读，不得把 done 置 true 或填值。
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "fallback.example:9"

	host, unresolved := resolvePublicHost(nil, req)
	if unresolved || host != "fallback.example" {
		t.Fatalf("host=%q unresolved=%v, want fallback.example / false", host, unresolved)
	}

	pubIP.mu.Lock()
	done := pubIP.done
	value := pubIP.value
	pubIP.mu.Unlock()
	if done || value != "" {
		t.Fatalf("cache mutated: done=%v value=%q (must not probe)", done, value)
	}
}

func TestResolvePublicHostEmptyWhenNothingUsable(t *testing.T) {
	t.Setenv("PUBLIC_HOST", "")
	_ = os.Unsetenv("PUBLIC_HOST")
	resetPublicIPCache()
	t.Cleanup(resetPublicIPCache)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = ""

	host, unresolved := resolvePublicHost(&config.Config{}, req)
	if !unresolved {
		t.Fatalf("unresolved = false, want true")
	}
	if host != "" {
		t.Fatalf("host = %q, want empty", host)
	}
}
