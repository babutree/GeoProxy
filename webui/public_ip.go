package webui

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// publicIPCache 缓存首次查询到的公网 IP 与所在国家码，避免每次请求都外呼。
// 查询失败时不缓存，允许后续重试（例如网络恢复后）。
type publicIPCache struct {
	mu        sync.Mutex
	value     string
	country   string
	done      bool
	fetchedAt time.Time
	fetching  chan struct{}
}

var pubIP publicIPCache

const publicIPCacheTTL = 5 * time.Minute

// publicIPFetch 是可替换的获取切入点，生产环境指向真实探测，测试可注入确定性结果。
var publicIPFetch = fetchPublicIPAndCountry

// publicIPProviders 是多个纯文本返回公网 IP 的服务，按顺序尝试，任一成功即返回。
// 国内网络可能屏蔽部分服务，故提供多个备选而非单点依赖。
var publicIPProviders = []string{
	"https://api.ipify.org",
	"https://ip.sb",
	"https://ifconfig.me/ip",
	"https://icanhazip.com",
}

// fetchPublicIP 依次尝试各 provider 获取公网 IP；全部失败返回空字符串。
func fetchPublicIP() string {
	client := &http.Client{Timeout: 5 * time.Second}
	for _, url := range publicIPProviders {
		ip := tryFetchIP(client, url)
		if ip != "" {
			return ip
		}
	}
	return ""
}

// fetchPublicIPAndCountry 经 ip-api.com 一次取得公网 IP 与国家码（用于地图定位网关）。
// 成功返回 (ip, 大写两位国家码)；失败则降级：IP 走纯文本 provider、国家码留空（前端降级为 CN）。
func fetchPublicIPAndCountry() (string, string) {
	client := &http.Client{Timeout: 5 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://ip-api.com/json/?fields=status,countryCode,query", nil)
	if err == nil {
		if resp, err := client.Do(req); err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
				var result struct {
					Status      string `json:"status"`
					CountryCode string `json:"countryCode"`
					Query       string `json:"query"`
				}
				if json.Unmarshal(body, &result) == nil && result.Status == "success" && net.ParseIP(result.Query) != nil {
					return result.Query, strings.ToUpper(result.CountryCode)
				}
			}
		}
	}
	// 降级：ip-api 不可用时仍尽力返回 IP，国家码留空由前端降级为 CN。
	return fetchPublicIP(), ""
}

func tryFetchIP(client *http.Client, url string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return ""
	}
	candidate := strings.TrimSpace(string(body))
	if net.ParseIP(candidate) == nil {
		return ""
	}
	return candidate
}

// cachedPublicIP 获取带期限的公网 IP 快照；网络请求在锁外执行，避免阻塞其它连接/API。
func cachedPublicIP() (string, string) {
	pubIP.mu.Lock()
	if pubIP.done && pubIP.value != "" && !pubIP.fetchedAt.IsZero() && time.Since(pubIP.fetchedAt) < publicIPCacheTTL {
		ip, country := pubIP.value, pubIP.country
		pubIP.mu.Unlock()
		return ip, country
	}
	if pubIP.fetching != nil {
		fetching := pubIP.fetching
		pubIP.mu.Unlock()
		<-fetching
		pubIP.mu.Lock()
		ip, country := pubIP.value, pubIP.country
		pubIP.mu.Unlock()
		return ip, country
	}
	fetching := make(chan struct{})
	pubIP.fetching = fetching
	pubIP.mu.Unlock()

	ip, country := publicIPFetch()
	pubIP.mu.Lock()
	if ip == "" {
		// 过期快照不再冒充当前公网地址；失败后下一次请求可重试。
		pubIP.value = ""
		pubIP.country = ""
		pubIP.done = false
		pubIP.fetchedAt = time.Time{}
	} else {
		pubIP.value = ip
		pubIP.country = country
		pubIP.done = true
		pubIP.fetchedAt = time.Now()
	}
	pubIP.fetching = nil
	close(fetching)
	ip, country = pubIP.value, pubIP.country
	pubIP.mu.Unlock()
	return ip, country
}

// apiPublicIP 返回服务器公网 IP（缓存有期限）。
// 供 WebUI 连接指南展示真实可连地址，而非写死的 127.0.0.1。
func (s *Server) apiPublicIP(w http.ResponseWriter, r *http.Request) {
	ip, country := cachedPublicIP()

	jsonOK(w, map[string]string{"public_ip": ip, "country": country})
}
