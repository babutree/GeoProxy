package validator

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
	"goproxy/config"
	"goproxy/storage"
)

type Validator struct {
	concurrency   int
	timeout       time.Duration
	validateURL   string
	validateURLs  []string
	maxResponseMs int
	cfg           *config.Config
}

func concurrencyBuffer(total, concurrency int) int {
	if total < concurrency*10 {
		return total
	}
	return concurrency * 10
}

func New(concurrency, timeoutSec int, validateURL string) *Validator {
	cfg := config.Get()
	maxMs := 0
	if cfg != nil {
		maxMs = cfg.MaxResponseMs
	}
	return &Validator{
		concurrency:   concurrency,
		timeout:       time.Duration(timeoutSec) * time.Second,
		validateURL:   validateURL,
		validateURLs:  parseValidateURLs(validateURL),
		maxResponseMs: maxMs,
		cfg:           cfg,
	}
}

func parseValidateURLs(value string) []string {
	parts := strings.Split(value, ",")
	targets := make([]string, 0, len(parts))
	for _, part := range parts {
		target := strings.TrimSpace(part)
		if target != "" {
			targets = append(targets, target)
		}
	}
	return targets
}

type Result struct {
	Proxy        storage.Proxy
	Valid        bool
	Latency      time.Duration
	ExitIP       string
	ExitLocation string
	Risk         RiskInfo // 两源风险信号：ipapi.is 分数 + ip-api 命中标记，分开展示不聚合
}

// ipAPIInfo 是 ip-api.com 返回的出口信息（含风险信号）。
type ipAPIInfo struct {
	IP       string
	Location string
	Proxy    bool // proxy=true：VPN/代理/Tor 出口
	Hosting  bool // hosting=true：数据中心/托管
	Mobile   bool // mobile=true：移动网络
	OK       bool // 查询是否成功
}

// getExitIPInfo 通过代理获取出口 IP、地理位置及风险信号（proxy/hosting/mobile）。
// 使用 ip-api.com，fields 扩展 proxy,hosting,mobile 以支持风险分派生。
func getExitIPInfo(client *http.Client) ipAPIInfo {
	// 扩展 fields：新增 proxy,hosting,mobile 用于风险评估。
	resp, err := client.Get("http://ip-api.com/json/?fields=status,country,countryCode,city,query,proxy,hosting,mobile")
	if err != nil {
		return ipAPIInfo{}
	}
	defer resp.Body.Close()

	var result struct {
		Status      string `json:"status"`
		Query       string `json:"query"` // IP 地址
		Country     string `json:"country"`
		CountryCode string `json:"countryCode"`
		City        string `json:"city"`
		Proxy       bool   `json:"proxy"`
		Hosting     bool   `json:"hosting"`
		Mobile      bool   `json:"mobile"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || result.Status != "success" {
		return ipAPIInfo{}
	}

	// 返回格式：IP, "国家代码 城市"
	location := result.CountryCode
	if result.City != "" {
		location = fmt.Sprintf("%s %s", result.CountryCode, result.City)
	}

	return ipAPIInfo{
		IP:       result.Query,
		Location: location,
		Proxy:    result.Proxy,
		Hosting:  result.Hosting,
		Mobile:   result.Mobile,
		OK:       true,
	}
}

// ipapiIsInfo 是 ipapi.is 返回的风险信号。
type ipapiIsInfo struct {
	Datacenter  bool
	VPN         bool
	Proxy       bool
	Tor         bool
	Abuser      bool
	AbuserScore float64 // 已解析的归一化滥用分（0-1）
	OK          bool
}

// queryIPAPIIs 经同一 proxy client 请求 ipapi.is，显式指定出口 IP (?q=<exitIP>)，
// 确保查到的是节点出口 IP 而非网关自身 IP。exitIP 由 ip-api 已先行取得。
// 查询失败/超时/解析失败时返回 OK=false，供上层降级。
func queryIPAPIIs(client *http.Client, exitIP string) ipapiIsInfo {
	if exitIP == "" {
		return ipapiIsInfo{}
	}
	resp, err := client.Get("https://api.ipapi.is/?q=" + url.QueryEscape(exitIP))
	if err != nil {
		return ipapiIsInfo{}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ipapiIsInfo{}
	}

	// abuser_score 返回形如 "0.0039 (Low)" 的字符串，用 string 接收后解析。
	var raw struct {
		IsDatacenter bool `json:"is_datacenter"`
		IsVPN        bool `json:"is_vpn"`
		IsProxy      bool `json:"is_proxy"`
		IsTor        bool `json:"is_tor"`
		IsAbuser     bool `json:"is_abuser"`
		Company      struct {
			AbuserScore string `json:"abuser_score"`
		} `json:"company"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return ipapiIsInfo{}
	}
	if strings.TrimSpace(raw.Company.AbuserScore) == "" {
		return ipapiIsInfo{}
	}

	return ipapiIsInfo{
		Datacenter:  raw.IsDatacenter,
		VPN:         raw.IsVPN,
		Proxy:       raw.IsProxy,
		Tor:         raw.IsTor,
		Abuser:      raw.IsAbuser,
		AbuserScore: parseAbuserScore(raw.Company.AbuserScore),
		OK:          true,
	}
}

// cloudflareProbeURL 作为 Cloudflare 可达性/拦截信号的基准探测目标。
const cloudflareProbeURL = "https://www.cloudflare.com/cdn-cgi/trace"

// probeCloudflareBlocked 经传入的 *http.Client（即走该代理）探测 Cloudflare 是否拦截，
// 返回 -1/0/1：
//   - 请求失败/超时 → -1（未知，不武断判为拦截）。
//   - 命中拦截信号 → 1。信号判定（命中任一）：HTTP 状态 403；或响应头存在 "cf-mitigated"；
//     或响应体含 "Just a moment"/"cf-chl"/"Attention Required"/"error code: 1020"。
//   - 否则（如 200）→ 0。
func probeCloudflareBlocked(client *http.Client) int {
	resp, err := client.Get(cloudflareProbeURL)
	if err != nil {
		return -1
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return 1
	}
	if resp.Header.Get("cf-mitigated") != "" {
		return 1
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return -1
	}
	text := string(body)
	for _, sig := range []string{"Just a moment", "cf-chl", "Attention Required", "error code: 1020"} {
		if strings.Contains(text, sig) {
			return 1
		}
	}
	return 0
}

// aiProbeTargets 是 4 个 AI 服务可达性探测目标。选用各家公开的、无需鉴权即可返回
// 响应（通常 401 缺 key）的端点：能连通即说明地区不封，连不通才是不可达。
// 抽成包级变量以便测试用 httptest URL 覆盖（真实 http 往返，不 mock）。
var aiProbeTargets = map[string]string{
	"openai": "https://api.openai.com/v1/models",
	"claude": "https://api.anthropic.com/v1/messages",
	"grok":   "https://api.x.ai/v1/models",
	"gemini": "https://generativelanguage.googleapis.com/v1beta/models",
}

// probeAIReachability 经传入的 *http.Client（即走该代理）逐个探测 4 个 AI 服务是否可达，
// 返回 JSON 对象字符串，如 {"openai":0,"claude":1,"grok":-1,"gemini":0}。
//
// 语义注意：这里探测的是「能否连通该 AI 服务」，与 probeCloudflareBlocked 的
// 「是否被拦截」语义不同。判定规则（每个 AI）：
//   - 请求成功拿到任何 HTTP 响应（含 401/403/404，只是没 key/无权限）→ 0（可达）：
//     能连通说明该地区不封锁该 AI。
//   - 请求失败/超时/连接错误（连不通）→ 1（不可达）。
//
// 每个探测复用 client 已有的 Timeout（不无限等）。4 个探测串行执行，简单可靠；
// 任一探测异常均不 panic，如实记为 1（不可达）。
func probeAIReachability(client *http.Client) string {
	results := make(map[string]int, len(aiProbeTargets))
	for name, target := range aiProbeTargets {
		results[name] = probeOneAI(client, target)
	}
	data, err := json.Marshal(results)
	if err != nil {
		// map[string]int 序列化不会失败；兜底返回空串（整体未探测），不 panic。
		return ""
	}
	return string(data)
}

// probeOneAI 探测单个 AI 端点是否可达：拿到任何 HTTP 响应→0（可达），连不通/超时→1（不可达）。
func probeOneAI(client *http.Client, target string) int {
	resp, err := client.Get(target)
	if err != nil {
		return 1
	}
	// 只关心能否连通，主动读干净并关闭 body 以复用连接；不解析内容。
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	resp.Body.Close()
	return 0
}

// assessRisk 收集两源风险信号，分开返回（不聚合）：
//   - ip-api 的 proxy/hosting/mobile 命中标记（来自已取得的 ipInfo）
//   - ipapi.is 的 abuser_score（经同一 client 走节点代理请求；失败则记 IPAPIIsUnknown）
//   - Cloudflare 拦截探测（经同一 client 走节点代理请求）
//   - AI 服务可达性探测（经同一 client 走节点代理请求）
func assessRisk(client *http.Client, ipInfo ipAPIInfo) RiskInfo {
	risk := RiskInfo{IPAPIIsScore: IPAPIIsUnknown}
	if ipInfo.OK {
		risk.Flags = ipapiFlags(ipInfo.Proxy, ipInfo.Hosting, ipInfo.Mobile)
	}
	if ipInfo.OK && ipInfo.IP != "" {
		if is := queryIPAPIIs(client, ipInfo.IP); is.OK {
			risk.IPAPIIsScore = is.AbuserScore
		}
	}
	risk.CFBlocked = probeCloudflareBlocked(client)
	risk.AIReachability = probeAIReachability(client)
	return risk
}

// HTTPS 测试目标列表，随机选一个验证代理的 CONNECT 隧道能力
var httpsTestTargets = []string{
	"https://www.google.com",
	"https://www.openai.com",
	"https://www.github.com",
	"https://www.cloudflare.com",
	"https://www.gstatic.com/generate_204",
}

// checkHTTPSConnect 通过 HTTP 代理实际访问一个随机 HTTPS 网站，验证 CONNECT 隧道是否可用
// 首次失败会换一个目标重试一次，避免目标网站偶尔抽风导致误杀
func checkHTTPSConnect(proxyAddr string, timeout time.Duration) bool {
	proxyURL, err := url.Parse(fmt.Sprintf("http://%s", proxyAddr))
	if err != nil {
		return false
	}

	client := &http.Client{
		Transport: &http.Transport{
			Proxy:               http.ProxyURL(proxyURL),
			TLSHandshakeTimeout: timeout,
		},
		Timeout: timeout,
	}

	// 随机起始索引
	start := int(time.Now().UnixNano() % int64(len(httpsTestTargets)))

	for attempt := 0; attempt < 2; attempt++ {
		idx := (start + attempt) % len(httpsTestTargets)
		resp, err := client.Get(httpsTestTargets[idx])
		if err != nil {
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		// 2xx 或 3xx 都算成功（部分网站会重定向）
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			return true
		}
	}

	return false
}

// ValidateAll 并发验证所有代理，返回验证结果
func (v *Validator) ValidateAll(proxies []storage.Proxy) []Result {
	var results []Result
	for r := range v.ValidateStream(proxies) {
		results = append(results, r)
	}
	return results
}

// ValidateStream 并发验证，边验证边通过 channel 返回结果
func (v *Validator) ValidateStream(proxies []storage.Proxy) <-chan Result {
	ch := make(chan Result, concurrencyBuffer(len(proxies), v.concurrency))
	sem := make(chan struct{}, v.concurrency)
	var wg sync.WaitGroup

	go func() {
		for _, p := range proxies {
			wg.Add(1)
			sem <- struct{}{}
			go func(px storage.Proxy) {
				defer wg.Done()
				defer func() { <-sem }()
				valid, latency, exitIP, exitLocation, risk := v.ValidateOne(px)
				ch <- Result{Proxy: px, Valid: valid, Latency: latency, ExitIP: exitIP, ExitLocation: exitLocation, Risk: risk}
			}(p)
		}
		wg.Wait()
		close(ch)
	}()

	return ch
}

// ValidateOne 验证单个代理是否可用，返回是否有效、延迟、出口IP、地理位置和 IP 风险信号。
// 风险信号：验证通过路径经同一 proxy client 分别探测 ip-api.com（命中标记）与 ipapi.is（滥用分），
// 两源分开不聚合；未走到风险探测的失败路径统一返回 UnknownRisk()。
func (v *Validator) ValidateOne(p storage.Proxy) (bool, time.Duration, string, string, RiskInfo) {
	var client *http.Client
	var err error

	switch p.Protocol {
	case "http":
		client, err = newHTTPClient(p.Address, v.timeout)
	case "socks5":
		client, err = newSOCKS5Client(p.Address, v.timeout)
	default:
		log.Printf("unknown protocol %s for %s", p.Protocol, p.Address)
		return false, 0, "", "", UnknownRisk()
	}

	if err != nil {
		return false, 0, "", "", UnknownRisk()
	}

	latency, ok := v.validateConnectivity(client)
	if !ok {
		return false, latency, "", "", UnknownRisk()
	}

	// 响应时间过滤
	if v.maxResponseMs > 0 && latency > time.Duration(v.maxResponseMs)*time.Millisecond {
		return false, latency, "", "", UnknownRisk()
	}

	// 获取出口 IP 和地理位置（仅在验证通过时）
	ipInfo := getExitIPInfo(client)
	exitIP, exitLocation := ipInfo.IP, ipInfo.Location

	// 必须能获取到出口信息
	if exitIP == "" || exitLocation == "" {
		return false, latency, exitIP, exitLocation, UnknownRisk()
	}

	// 地理过滤：白名单优先，否则走黑名单
	if len(exitLocation) >= 2 && !v.passesGeoFilter(exitLocation[:2]) {
		return false, latency, exitIP, exitLocation, UnknownRisk()
	}

	// HTTP 代理额外检测：必须支持 HTTPS CONNECT 隧道
	if p.Protocol == "http" {
		if !checkHTTPSConnect(p.Address, v.timeout) {
			return false, latency, exitIP, exitLocation, UnknownRisk()
		}
	}

	// 风险信号探测：经同一 proxy client 分别取两源；出口 IP 已从 ip-api 取得。
	risk := assessRisk(client, ipInfo)

	return true, latency, exitIP, exitLocation, risk
}

// passesGeoFilter 依据白/黑名单判断某国家代码是否通过地理过滤。
// 读取 v.cfg 的国家名单 slice；v.cfg 是 config.Get() 返回的不可变快照指针，
// config.Save 通过替换 globalCfg 指针（而非原地改写）保证这里的读取不会撕裂。
func (v *Validator) passesGeoFilter(countryCode string) bool {
	if v.cfg == nil {
		return true
	}
	if len(v.cfg.AllowedCountries) > 0 {
		// 白名单模式：不在白名单中则拒绝
		for _, a := range v.cfg.AllowedCountries {
			if countryCode == a {
				return true
			}
		}
		return false
	}
	// 黑名单模式
	for _, blocked := range v.cfg.BlockedCountries {
		if countryCode == blocked {
			return false
		}
	}
	return true
}

func (v *Validator) validateConnectivity(client *http.Client) (time.Duration, bool) {
	for _, target := range v.validateURLs {
		start := time.Now()
		resp, err := client.Get(target)
		latency := time.Since(start)
		if err != nil {
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		// 验证状态码（200 或 204 都接受）
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
			return latency, true
		}
	}
	return 0, false
}

func newHTTPClient(address string, timeout time.Duration) (*http.Client, error) {
	proxyURL, err := url.Parse(fmt.Sprintf("http://%s", address))
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: timeout,
	}, nil
}

func newSOCKS5Client(address string, timeout time.Duration) (*http.Client, error) {
	dialer, err := proxy.SOCKS5("tcp", address, nil, proxy.Direct)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: &http.Transport{
			Dial: dialer.Dial,
		},
		Timeout: timeout,
	}, nil
}
