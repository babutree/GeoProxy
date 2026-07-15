package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	testURL      = "http://ip-api.com/json/?fields=countryCode,query"
	delaySeconds = 1
)

var (
	proxyHost = envOrDefault("PROXY_HOST", "127.0.0.1")
	proxyPort = "7802"
)

type IPResponse struct {
	Query       string `json:"query"`
	CountryCode string `json:"countryCode"`
}

var (
	totalCount   = 0
	successCount = 0
)

// 国家代码转 emoji 旗帜
func countryToEmoji(countryCode string) string {
	if countryCode == "" {
		return "🌐"
	}

	countryCode = strings.ToUpper(countryCode)
	if len(countryCode) != 2 {
		return "🌐"
	}

	// 将国家代码转换为 emoji
	// A=127462, 所以 'US' -> 🇺🇸
	first := rune(countryCode[0]) - 'A' + 127462
	second := rune(countryCode[1]) - 'A' + 127462

	return string([]rune{first, second})
}

func printStats() {
	fmt.Println()
	fmt.Println("---")
	lossCount := totalCount - successCount
	lossRate := 0.0
	if totalCount > 0 {
		lossRate = float64(lossCount) / float64(totalCount) * 100
	}
	fmt.Printf("%d requests transmitted, %d received, %d failed, %.1f%% packet loss\n",
		totalCount, successCount, lossCount, lossRate)
	os.Exit(0)
}

func envOrDefault(key, defaultValue string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return defaultValue
}

func proxyAuthUsername() string {
	username := strings.TrimSpace(os.Getenv("GOPROXY_AUTH_USERNAME"))
	if region := strings.TrimSpace(os.Getenv("GOPROXY_AUTH_REGION")); region != "" {
		username += "-region-" + region
	}
	if session := strings.TrimSpace(os.Getenv("GOPROXY_AUTH_SESSION")); session != "" {
		username += "-session-" + session
	}
	return username
}

func requireProxyAuth() (string, string) {
	username := proxyAuthUsername()
	password := os.Getenv("GOPROXY_AUTH_PASSWORD")
	if strings.TrimSpace(os.Getenv("GOPROXY_AUTH_USERNAME")) == "" || password == "" {
		fmt.Fprintln(os.Stderr, "Missing proxy credentials.")
		fmt.Fprintln(os.Stderr, "Set GOPROXY_AUTH_USERNAME and GOPROXY_AUTH_PASSWORD from the first-boot log or WebUI Settings.")
		fmt.Fprintln(os.Stderr, "Optional: GOPROXY_AUTH_REGION=us GOPROXY_AUTH_SESSION=browser")
		os.Exit(2)
	}
	return username, password
}

func testHTTPProxyContinuous() {
	username, password := requireProxyAuth()

	fmt.Printf("PROXY %s:%s (%s): continuous mode\n", proxyHost, proxyPort, testURL)
	fmt.Println()

	proxyURL, _ := url.Parse(fmt.Sprintf("http://%s:%s", proxyHost, proxyPort))
	proxyURL.User = url.UserPassword(username, password)
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 15 * time.Second,
	}

	// 捕获 Ctrl+C 信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		printStats()
	}()

	for {
		totalCount++

		start := time.Now()
		resp, err := client.Get(testURL)
		elapsed := time.Since(start).Milliseconds()

		if err != nil {
			fmt.Printf("proxy #%d: request failed (%v)\n", totalCount, err)
			time.Sleep(delaySeconds * time.Second)
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var ipResp IPResponse
		if err := json.Unmarshal(body, &ipResp); err == nil {
			flag := countryToEmoji(ipResp.CountryCode)
			fmt.Printf("proxy from %s %s: seq=%d time=%dms\n", flag, ipResp.Query, totalCount, elapsed)
			successCount++
		} else {
			fmt.Printf("proxy #%d: parse error\n", totalCount)
		}

		time.Sleep(delaySeconds * time.Second)
	}
}

func main() {
	// --check-auth：仅校验代理认证环境变量后立即退出，不进入持续请求循环。
	// 缺失凭据时 requireProxyAuth 打印报错并以退出码 2 结束；便于 CI/dry-run。
	for _, arg := range os.Args[1:] {
		if arg == "--check-auth" {
			username, _ := requireProxyAuth()
			fmt.Printf("proxy auth OK: username=%s\n", username)
			return
		}
	}

	// 支持指定端口号
	if len(os.Args) > 1 {
		proxyPort = os.Args[1]
	}

	testHTTPProxyContinuous()
}
