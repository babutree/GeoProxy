package validator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/storage"
	"golang.org/x/net/proxy"
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
	return newValidator(concurrency, timeoutSec, validateURL, config.Get())
}

// NewWithConfig 从同一不可变配置快照构造验证器，避免并发保存配置时混用新旧字段。
func NewWithConfig(cfg *config.Config) *Validator {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	return newValidator(cfg.ValidateConcurrency, cfg.ValidateTimeout, cfg.ValidateURL, cfg)
}

func newValidator(concurrency, timeoutSec int, validateURL string, cfg *config.Config) *Validator {
	if concurrency < 1 {
		concurrency = 1
	}
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

type FailureReason string

const (
	FailureNone                FailureReason = ""
	FailureConnectivity        FailureReason = "connectivity"
	FailureLatency             FailureReason = "latency"
	FailureExitMetadata        FailureReason = "exit_metadata"
	FailureGeoRejected         FailureReason = "geo_rejected"
	FailureHTTPConnectRejected FailureReason = "http_connect_rejected"
)

type Result struct {
	Proxy         storage.Proxy
	Valid         bool
	Latency       time.Duration
	ExitIP        string
	ExitLocation  string
	Risk          RiskInfo // 两源风险信号：ipapi.is 分数 + ip-api 命中标记，分开展示不聚合
	FailureReason FailureReason
}

// ipAPIInfo 是出口信息与可用的风险布尔信号。
type ipAPIInfo struct {
	IP         string
	Location   string
	Proxy      bool // proxy=true：VPN/代理/Tor 出口
	Hosting    bool // hosting=true：数据中心/托管
	Mobile     bool // mobile=true：移动网络
	FlagsKnown bool // 主出口源 ip-api 是否成功返回风险标记字段
	OK         bool // 查询是否成功
}

const (
	primaryExitInfoURL     = "http://ip-api.com/json/?fields=status,country,countryCode,city,query,proxy,hosting,mobile"
	backupExitInfoURL      = "https://api.ipapi.is/"
	defaultExitInfoLimit   = 64 << 10
	defaultExitInfoTimeout = 5 * time.Second
)

// getExitIPInfo 通过代理查询两个独立出口信息源。
// 两源都成功时必须对出口 IP 和国家码达成一致；HTTPS 备源可在明文源失败时单独定案，
// 明文源只能交叉验证并补充风险标记。HTTPS 源失败或两源冲突时一律失败。
func getExitIPInfo(client *http.Client) ipAPIInfo {
	if client == nil {
		return ipAPIInfo{}
	}
	timeout := client.Timeout
	if timeout <= 0 {
		timeout = defaultExitInfoTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	sources := []func(context.Context, *http.Client) ipAPIInfo{
		queryPrimaryExitInfo,
		queryBackupExitInfo,
	}
	type sourceResult struct {
		index int
		info  ipAPIInfo
	}
	resultCh := make(chan sourceResult, len(sources))
	for index, source := range sources {
		go func() {
			resultCh <- sourceResult{index: index, info: source(ctx, client)}
		}()
	}

	results := make([]ipAPIInfo, len(sources))
	remaining := len(sources)
	for remaining > 0 {
		select {
		case result := <-resultCh:
			results[result.index] = result.info
			remaining--
		case <-ctx.Done():
			// 截止时刻只采用已经完成的结果；缓冲通道保证迟到协程不会阻塞。
			for {
				select {
				case result := <-resultCh:
					results[result.index] = result.info
				default:
					return mergeCompletedExitIPInfos(results)
				}
			}
		}
	}
	return mergeCompletedExitIPInfos(results)
}

func mergeCompletedExitIPInfos(results []ipAPIInfo) ipAPIInfo {
	// results[0] 来自明文 HTTP，只能补充风险标记并与 HTTPS 结果交叉验证；
	// 不能在 HTTPS 源失败时单独决定出口 IP/地域，否则链路劫持可污染选路标签。
	if len(results) < 2 || !results[1].OK {
		return ipAPIInfo{}
	}
	if !results[0].OK {
		return results[1]
	}
	return mergeExitIPInfos([]ipAPIInfo{results[0], results[1]})
}

func queryPrimaryExitInfo(ctx context.Context, client *http.Client) ipAPIInfo {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, primaryExitInfoURL, nil)
	if err != nil {
		return ipAPIInfo{}
	}
	resp, err := client.Do(req)
	if err != nil {
		return ipAPIInfo{}
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return ipAPIInfo{}
	}

	var result struct {
		Status      string `json:"status"`
		Query       string `json:"query"`
		CountryCode string `json:"countryCode"`
		City        string `json:"city"`
		Proxy       bool   `json:"proxy"`
		Hosting     bool   `json:"hosting"`
		Mobile      bool   `json:"mobile"`
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, defaultExitInfoLimit+1))
	if err != nil || len(body) > defaultExitInfoLimit || json.Unmarshal(body, &result) != nil || result.Status != "success" {
		return ipAPIInfo{}
	}
	return newExitIPInfo(result.Query, result.CountryCode, result.City, result.Proxy, result.Hosting, result.Mobile, true)
}

func queryBackupExitInfo(ctx context.Context, client *http.Client) ipAPIInfo {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, backupExitInfoURL, nil)
	if err != nil {
		return ipAPIInfo{}
	}
	resp, err := client.Do(req)
	if err != nil {
		return ipAPIInfo{}
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return ipAPIInfo{}
	}

	var result struct {
		IP       string `json:"ip"`
		Location struct {
			CountryCode string `json:"country_code"`
			City        string `json:"city"`
		} `json:"location"`
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, defaultExitInfoLimit+1))
	if err != nil || len(body) > defaultExitInfoLimit || json.Unmarshal(body, &result) != nil {
		return ipAPIInfo{}
	}
	return newExitIPInfo(result.IP, result.Location.CountryCode, result.Location.City, false, false, false, false)
}

func newExitIPInfo(ip, countryCode, city string, proxyFlag, hosting, mobile, flagsKnown bool) ipAPIInfo {
	parsedIP := net.ParseIP(strings.TrimSpace(ip))
	countryCode = config.NormalizeCountryCode(countryCode)
	if parsedIP == nil || countryCode == "" {
		return ipAPIInfo{}
	}
	location := countryCode
	if city = strings.TrimSpace(city); city != "" {
		location = fmt.Sprintf("%s %s", countryCode, city)
	}
	return ipAPIInfo{
		IP:         parsedIP.String(),
		Location:   location,
		Proxy:      proxyFlag,
		Hosting:    hosting,
		Mobile:     mobile,
		FlagsKnown: flagsKnown,
		OK:         true,
	}
}

func mergeExitIPInfos(infos []ipAPIInfo) ipAPIInfo {
	if len(infos) == 0 {
		return ipAPIInfo{}
	}
	merged := infos[0]
	for _, candidate := range infos[1:] {
		if candidate.IP != merged.IP || exitCountryCode(candidate.Location) != exitCountryCode(merged.Location) {
			return ipAPIInfo{}
		}
		if len(strings.Fields(merged.Location)) == 1 && len(strings.Fields(candidate.Location)) > 1 {
			merged.Location = candidate.Location
		}
	}
	return merged
}

func exitCountryCode(location string) string {
	fields := strings.Fields(location)
	if len(fields) == 0 {
		return ""
	}
	return config.NormalizeCountryCode(fields[0])
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
// 确保查到的是节点出口 IP 而非网关自身 IP。exitIP 由出口信息查询已先行取得。
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

// aiProbeTargets 是 4 个 AI 服务可达性探测目标（主信号：稳定 API）。
// 匿名请求通常返回 401/缺 key，表示“出口能连上服务 API”；
// 明确地域拒绝/CF 挑战/连接失败才记不可达。
// 抽成包级变量以便测试用 httptest URL 覆盖。
var aiProbeTargets = map[string]string{
	"openai": "https://api.openai.com/v1/models",
	"claude": "https://api.anthropic.com/v1/models",
	"grok":   "https://api.x.ai/v1/models",
	"gemini": "https://generativelanguage.googleapis.com/v1beta/models",
}

// aiProductProbeTargets 是产品层辅信号（只覆盖 AI，不含流媒体）。
// 吸收社区解锁脚本中已验证的明确地区拒绝/放行指纹，用于纠正“API 401 可达
// 但产品层实际地区锁”或“API 未知但产品层明确可用”的情况。
// 空切片表示该服务仅依赖 API 主信号（如 grok）。
var aiProductProbeTargets = map[string][]string{
	// OpenAI 合规端点：unsupported_country 为明确地区锁（缝合怪 ChatGPT 检测同源）。
	"openai": {"https://api.openai.com/compliance/cookie_requirements"},
	// Claude：最终落到 app-unavailable-in-region 为明确地区锁。
	"claude": {"https://claude.ai/"},
	// Gemini：页面含 45631641,null,true 为社区常用解锁指纹。
	"gemini": {"https://gemini.google.com/"},
}

type aiProbeRule struct {
	headers            map[string]string
	unlockedBodyGroups [][]string
}

var aiProbeRules = map[string]aiProbeRule{
	"openai": {
		headers: map[string]string{
			"Accept":     "application/json",
			"User-Agent": "geoproxy-ai-probe/1.0",
		},
		// 401 已在 classify 中直接记可达；body 组用于偶发 200 列表响应。
		unlockedBodyGroups: [][]string{
			{"object", "list", "data"},
			{"authentication", "api key"},
		},
	},
	"claude": {
		headers: map[string]string{
			"Accept":            "application/json",
			"anthropic-version": "2023-06-01",
			"User-Agent":        "geoproxy-ai-probe/1.0",
		},
		unlockedBodyGroups: [][]string{
			{"authentication_error"},
			{"x-api-key", "required"},
			{"api key", "required"},
		},
	},
	"grok": {
		headers: map[string]string{
			"Accept":     "application/json",
			"User-Agent": "geoproxy-ai-probe/1.0",
		},
		unlockedBodyGroups: [][]string{
			{"object", "list", "data"},
			{"incorrect api key"},
			{"api key", "required"},
		},
	},
	"gemini": {
		headers: map[string]string{
			"Accept":     "application/json",
			"User-Agent": "geoproxy-ai-probe/1.0",
		},
		unlockedBodyGroups: [][]string{
			{"unregistered caller", "api key"},
			{"api key not valid"},
			{"api keys", "expected"},
		},
	},
}

var defaultAIProbeRule = aiProbeRule{
	unlockedBodyGroups: [][]string{
		{"object", "list", "data", "model"},
	},
}

const aiProbeBodyLimit = 64 << 10

// discardBodyLimit 是"只需丢弃响应体以复用连接"路径的读取上限。
// 恶意/故障上游可以返回无限长的响应体；不限长的 io.Copy 会让单次探测
// 无限读下去（client.Timeout 只约束整个请求，但在持续有数据时不会触发），
// 从而拖住一个 ValidateStream 并发槽位。与其它探测路径的 64KiB 上限一致。
const discardBodyLimit = 64 << 10

// discardResponseBody 读掉并丢弃至多 discardBodyLimit 字节的响应体。
// 目的只是让底层连接可被复用/干净关闭，不需要读完整个响应。
func discardResponseBody(body io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, discardBodyLimit))
}

var aiCFBlockSignals = []string{"cf-chl", "error code: 1020"}

var aiRegionalBlockCodes = map[string]struct{}{
	"unsupported_country":                  {},
	"unsupported_country_region_territory": {},
	"country_not_supported":                {},
	"region_not_supported":                 {},
}

var aiRegionalRejectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:your\s+)?(?:country|region|territory)(?:\s+or\s+(?:your\s+)?(?:country|region|territory))?\s+(?:is\s+)?(?:not\s+(?:supported|available)|unavailable|restricted)\b`),
	regexp.MustCompile(`(?i)\b(?:not\s+(?:supported|available)|unavailable|restricted)\s+(?:in|for)\s+(?:your\s+)?(?:country|region|territory)(?:\s+or\s+(?:your\s+)?(?:country|region|territory))?\b`),
}

// probeAIReachability 经传入的 *http.Client（即走该代理）探测 4 个 AI 服务，
// 返回 JSON 对象字符串，如 {"openai":0,"claude":1,"grok":-1,"gemini":0}。
//
// 主信号 = 稳定 API（401/缺 key 等）；辅信号 = 产品层明确地区锁/放行指纹。
// 合并规则见 mergeAIProbeResults：明确封禁优先；任一明确可达则可达。
// 账号/密钥/配额不作为 IP 封禁依据。CF 拦截另由 probeCloudflareBlocked 单独记录。
//
// 每个探测复用 client 已有的 Timeout；任一探测异常均不 panic。
// 本函数保持独立入口（供单测直接调用）：自建闸门与预算，语义与 assessRisk 内一致。
func probeAIReachability(client *http.Client) string {
	return probeAIReachabilityBounded(client, newProbeGate(riskProbeFanout), riskProbeDeadline(client))
}

// probeAIReachabilityBounded 是 probeAIReachability 的受控版本：并发闸门与总预算
// 由调用方传入，从而与同一节点的 ipapi.is / Cloudflare 探测共享同一份资源约束。
//
// 4 个服务并发；每个服务内部 API 层与产品层仍顺序执行——产品层只是 API 层的辅助
// 信号，串行两步的延迟已被服务间并发摊平，无需再细分。
// 被闸门或预算截断的服务保持 -1（未探测），不退化成 1（封禁）。
func probeAIReachabilityBounded(client *http.Client, gate probeGate, deadline time.Time) string {
	names := make([]string, 0, len(aiProbeTargets))
	for name := range aiProbeTargets {
		names = append(names, name)
	}
	results := make([]int, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func(idx int, service, target string) {
			defer wg.Done()
			// 默认未探测；仅在真正跑完探测后才被覆盖。
			results[idx] = -1
			gate.run(client, deadline, func(c *http.Client) {
				api := probeOneAIForService(c, service, target)
				product := probeAIProductLayers(c, service)
				results[idx] = mergeAIProbeResults(api, product)
			})
		}(i, name, aiProbeTargets[name])
	}
	wg.Wait()

	merged := make(map[string]int, len(names))
	for i, name := range names {
		merged[name] = results[i]
	}
	data, err := json.Marshal(merged)
	if err != nil {
		// map[string]int 序列化不会失败；兜底返回空串（整体未探测），不 panic。
		return ""
	}
	return string(data)
}

// mergeAIProbeResults 合并 API 主信号与产品层辅信号。
// 明确不可达(1)优先；否则任一明确可达(0)则可达；都未知才 -1。
func mergeAIProbeResults(api, product int) int {
	if api == 1 || product == 1 {
		return 1
	}
	if api == 0 || product == 0 {
		return 0
	}
	return -1
}

// probeAIProductLayers 对某服务的全部产品层 URL 探测，返回最“严重”结果：
// 任一条明确封禁 → 1；否则任一条明确可达 → 0；否则 -1。
func probeAIProductLayers(client *http.Client, service string) int {
	urls := aiProductProbeTargets[service]
	if len(urls) == 0 {
		return -1
	}
	best := -1
	for _, u := range urls {
		got := probeOneAIProductLayer(client, service, u)
		if got == 1 {
			return 1
		}
		if got == 0 {
			best = 0
		}
	}
	return best
}

// probeOneAIProductLayer 探测单条产品层 URL 的明确地区锁/放行指纹。
// 连接失败返回 -1（不把网络抖动升级成产品封禁；API 层已对连接失败记 1）。
func probeOneAIProductLayer(client *http.Client, service, target string) int {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return -1
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "text/html,application/json,*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	// OpenAI 合规端点需要类浏览器/API 头，与缝合怪一致。
	if service == "openai" {
		req.Header.Set("Authorization", "Bearer null")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://platform.openai.com")
		req.Header.Set("Referer", "https://platform.openai.com/")
	}

	resp, err := client.Do(req)
	if err != nil {
		return -1
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, aiProbeBodyLimit+1))
	if err != nil {
		return -1
	}
	if len(body) > aiProbeBodyLimit {
		body = body[:aiProbeBodyLimit]
	}
	return classifyAIProductLayer(service, resp.StatusCode, resp.Header, resp.Request, string(body))
}

// classifyAIProductLayer 只识别“明确”的产品层地区锁或解锁指纹，不做模糊猜测。
//
// 注意：产品层网站（如 claude.ai）对几乎所有数据中心/代理出口 IP 都会下发
// 通用 Cloudflare 反爬挑战（cf-mitigated: challenge / "Just a moment" / error code: 1020），
// 这是与地区无关的机器人拦截，不能作为“地域封禁”依据——否则会把 API 层
// 明确可达（401 认证错误）的节点误判为不可达。真正的地域封禁有独立指纹
// （app-unavailable-in-region、unsupported_country、明确地区拒绝文案），单独识别。
// 故此处不再把通用 CF 挑战判为 1；遇到通用挑战时返回 -1（未知），让 API 主信号决定。
func classifyAIProductLayer(service string, statusCode int, header http.Header, req *http.Request, body string) int {
	lower := strings.ToLower(body)
	if hasRegionalBlockCode(body) || hasExplicitRegionalRejection(body) {
		return 1
	}
	// Claude：最终 URL 落到官方“本地区不可用”页。
	if service == "claude" && req != nil && req.URL != nil {
		final := strings.ToLower(req.URL.String())
		if strings.Contains(final, "app-unavailable-in-region") || strings.Contains(final, "unavailable-in-region") {
			return 1
		}
	}
	// OpenAI 合规：unsupported_country 文案/码。
	if service == "openai" && strings.Contains(lower, "unsupported_country") {
		return 1
	}
	// Gemini 社区指纹：页面数据含 45631641,null,true。
	if service == "gemini" && strings.Contains(lower, "45631641,null,true") {
		return 0
	}
	// 其它产品层响应保持未知，避免营销页误报。
	return -1
}

// probeOneAI 探测单个 AI 端点是否地域解锁。判定：
//   - 连接失败/超时（连不通）→ 1（不可达）。
//   - 401 或明确服务语义 → 0（可达）：能连通服务，401 仅表示匿名请求需要凭据。
//   - 403 需按响应体细分（见 classifyAIResponse）：
//     Google 未注册调用者语义 → 0；明确地域/CF 拦截 → 1；其它 403 → -1。
//   - 其它未知响应 → -1（未探测）。
func probeOneAI(client *http.Client, target string) int {
	return probeOneAIForService(client, "", target)
}

func probeOneAIForService(client *http.Client, service, target string) int {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return -1
	}
	if rule, ok := aiProbeRules[service]; ok {
		for name, value := range rule.headers {
			req.Header.Set(name, value)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return 1
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, aiProbeBodyLimit+1))
	if err != nil {
		if resp.StatusCode == http.StatusUnauthorized {
			return 0
		}
		return -1
	}
	truncated := len(body) > aiProbeBodyLimit
	if truncated {
		body = body[:aiProbeBodyLimit]
	}
	result := classifyAIResponseForService(service, resp.StatusCode, resp.Header, string(body))
	if truncated && result != 1 {
		return -1
	}
	return result
}

// classifyAIResponse 依据集中规则把 AI 服务响应判为三态：0 解锁、1 封禁/不可达、-1 未探测。
func classifyAIResponse(statusCode int, header http.Header, body string) int {
	return classifyAIResponseForService("", statusCode, header, body)
}

func classifyAIResponseForService(service string, statusCode int, header http.Header, body string) int {
	// cf-mitigated 头是明确的 CF 拦截信号，优先判不可达。
	if header.Get("cf-mitigated") != "" {
		return 1
	}

	lower := strings.ToLower(body)
	if hasAICloudflareBlock(statusCode, lower) {
		return 1
	}
	if hasRegionalBlockCode(body) || hasExplicitRegionalRejection(body) {
		return 1
	}

	if statusCode == http.StatusUnauthorized {
		return 0
	}
	if statusCode == http.StatusForbidden {
		return classifyForbidden(body)
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return -1
	}
	rule, ok := aiProbeRules[service]
	if !ok {
		rule = defaultAIProbeRule
	}
	for _, group := range rule.unlockedBodyGroups {
		if containsAll(lower, group) {
			return 0
		}
	}
	return -1
}

func hasAICloudflareBlock(statusCode int, lower string) bool {
	for _, sig := range aiCFBlockSignals {
		if strings.Contains(lower, sig) {
			return true
		}
	}
	if statusCode < http.StatusBadRequest {
		return false
	}
	return strings.Contains(lower, "cloudflare") && (strings.Contains(lower, "just a moment") || strings.Contains(lower, "attention required"))
}

func hasRegionalBlockCode(body string) bool {
	var value any
	if json.Unmarshal([]byte(body), &value) != nil {
		return false
	}
	return containsRegionalBlockCode(value)
}

func containsRegionalBlockCode(value any) bool {
	switch current := value.(type) {
	case map[string]any:
		for key, nested := range current {
			if isRegionalBlockCodeField(key, nested) {
				return true
			}
			if containsRegionalBlockCode(nested) {
				return true
			}
		}
	case []any:
		for _, nested := range current {
			if containsRegionalBlockCode(nested) {
				return true
			}
		}
	}
	return false
}

func isRegionalBlockCodeField(key string, value any) bool {
	switch strings.ToLower(key) {
	case "code", "error_code", "reason", "status":
		text, ok := value.(string)
		if !ok {
			return false
		}
		_, blocked := aiRegionalBlockCodes[strings.ToLower(text)]
		return blocked
	default:
		return false
	}
}

func hasExplicitRegionalRejection(body string) bool {
	for _, pattern := range aiRegionalRejectionPatterns {
		if pattern.MatchString(body) {
			return true
		}
	}
	return false
}

// classifyForbidden 只识别匿名请求预期收到的无凭据响应；其它账号/权限错误不推断 IP 状态。
func classifyForbidden(body string) int {
	lower := strings.ToLower(body)
	if hasAnonymousCredentialChallenge(lower) {
		return 0
	}
	return -1
}

func hasAnonymousCredentialChallenge(lower string) bool {
	for _, code := range []string{"missing_auth", "missing_api_key", "api_key_missing", "authentication_required"} {
		if strings.Contains(lower, code) {
			return true
		}
	}
	for _, term := range []string{"api key", "api_key", "authentication", "bearer token", "credential"} {
		if strings.Contains(lower, term) && (strings.Contains(lower, "missing") || strings.Contains(lower, "not provided") || strings.Contains(lower, "without ")) {
			return true
		}
	}
	return strings.Contains(lower, "unregistered caller") && strings.Contains(lower, "api key")
}

func containsAll(text string, signals []string) bool {
	for _, sig := range signals {
		if !strings.Contains(text, sig) {
			return false
		}
	}
	return true
}

// ===== 单节点风险评估的并发与总预算 =====
//
// assessRisk 需发起最多 9 次彼此完全独立的请求（ipapi.is 1 + Cloudflare 1 +
// 4 个 AI 服务的 API 层 4 + 产品层 3）。它们域名不同、无数据依赖，结果只填
// RiskInfo 的不同字段，没有任何理由串行。
//
// 串行的代价是超时会累加：每个请求各自受 client.Timeout（= ValidateTimeout，
// 默认 10s）约束，9 次串行的最坏耗时是 90s，期间该节点独占一个 ValidateStream
// 的并发槽位。
//
// 这里用两个约束同时解决延迟与资源：
//   - riskProbeFanout 限制单节点的并发探测数。ValidateConcurrency 默认 300，
//     不设上限地把 9 个探测全部并发会瞬时占用 300×9=2700 个 socket
//     （newHTTPClient/newSOCKS5Client 都 DisableKeepAlives，每请求一条连接），
//     在 nofile=1024 的环境直接耗尽 fd。取 3 时上限为 300×3=900。
//   - riskProbeBudget 为整轮评估设总预算，由 clientWithProbeBudget 在每次探测
//     启动时把 client.Timeout 收窄到"剩余预算"，预算耗尽则直接跳过该探测。
//     这样无需 context 改造即可得到硬上限：任何探测都不会跨过 deadline 继续。
//
// 预算取 3×单请求超时：fanout=3 时 9 个探测最多 3 轮，恰好与 3×10s 对齐。
// 被预算截断的探测保持"未探测"语义（-1 / 空串），绝不退化成"封禁"——
// 存储层的 CASE WHEN 保护据此不覆盖已有有效值。
const (
	riskProbeFanout         = 3
	riskProbeBudgetFactor   = 3
	riskProbeFallbackBudget = 30 * time.Second
)

// riskProbeDeadline 计算本轮风险评估的截止时刻。
func riskProbeDeadline(client *http.Client) time.Time {
	budget := riskProbeFallbackBudget
	if client != nil && client.Timeout > 0 {
		budget = client.Timeout * riskProbeBudgetFactor
	}
	return time.Now().Add(budget)
}

// clientWithProbeBudget 返回一个共享同一 Transport、但 Timeout 不超过剩余预算的
// 派生 client。预算已耗尽时返回 nil，调用方须跳过该次探测。
//
// 浅拷贝 http.Client 是安全的：Transport 本身按设计支持并发复用，
// 这里只改 Timeout 这一个值语义字段。
func clientWithProbeBudget(base *http.Client, deadline time.Time) *http.Client {
	if base == nil {
		return nil
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return nil
	}
	derived := *base
	if derived.Timeout <= 0 || derived.Timeout > remaining {
		derived.Timeout = remaining
	}
	return &derived
}

// probeGate 是单节点风险评估的并发闸门。
type probeGate chan struct{}

func newProbeGate(size int) probeGate {
	if size < 1 {
		size = 1
	}
	return make(probeGate, size)
}

// run 取得一个探测槽位后执行 fn，并把 fn 可用的 client 收窄到剩余预算。
// 预算在排队期间耗尽时直接返回，不执行 fn（保持"未探测"）。
func (g probeGate) run(base *http.Client, deadline time.Time, fn func(*http.Client)) {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case g <- struct{}{}:
		defer func() { <-g }()
	case <-timer.C:
		return
	}
	client := clientWithProbeBudget(base, deadline)
	if client == nil {
		return
	}
	fn(client)
}

// assessRisk 收集两源风险信号，分开返回（不聚合）：
//   - 主出口源 ip-api 的 proxy/hosting/mobile 命中标记（来自已取得的 ipInfo）
//   - ipapi.is 的 abuser_score（经同一 client 走节点代理请求；失败则记 IPAPIIsUnknown）
//   - Cloudflare 拦截探测（经同一 client 走节点代理请求）
//   - AI 服务可达性探测（经同一 client 走节点代理请求）
//
// 三组探测彼此独立，并发执行并共享同一 probeGate 与总预算（见上方常量注释）。
// 各分支写入独立局部变量，wg.Wait() 之后才汇总进 RiskInfo，不共享可变状态。
func assessRisk(client *http.Client, ipInfo ipAPIInfo) RiskInfo {
	risk := RiskInfo{IPAPIIsScore: IPAPIIsUnknown, FlagsKnown: ipInfo.FlagsKnown}
	if ipInfo.FlagsKnown {
		risk.Flags = ipapiFlags(ipInfo.Proxy, ipInfo.Hosting, ipInfo.Mobile)
	}

	deadline := riskProbeDeadline(client)
	gate := newProbeGate(riskProbeFanout)

	// 未探测的保守初值：与 UnknownRisk 一致，绝不把"没测"说成"没问题"或"被封"。
	abuserScore := IPAPIIsUnknown
	cfBlocked := -1
	aiReachability := ""

	var wg sync.WaitGroup
	if ipInfo.OK && ipInfo.IP != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			gate.run(client, deadline, func(c *http.Client) {
				if is := queryIPAPIIs(c, ipInfo.IP); is.OK {
					abuserScore = is.AbuserScore
				}
			})
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		gate.run(client, deadline, func(c *http.Client) {
			cfBlocked = probeCloudflareBlocked(c)
		})
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		aiReachability = probeAIReachabilityBounded(client, gate, deadline)
	}()
	wg.Wait()

	risk.IPAPIIsScore = abuserScore
	risk.CFBlocked = cfBlocked
	risk.AIReachability = aiReachability
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
func checkHTTPSConnect(proxyAddr, username, password string, timeout time.Duration) bool {
	proxyURL, err := url.Parse(fmt.Sprintf("http://%s", proxyAddr))
	if err != nil {
		return false
	}
	if username != "" || password != "" {
		proxyURL.User = url.UserPassword(username, password)
	}

	client := &http.Client{
		Transport: &http.Transport{
			Proxy:               http.ProxyURL(proxyURL),
			TLSHandshakeTimeout: timeout,
			DisableKeepAlives:   true,
		},
		Timeout: timeout,
	}
	defer client.CloseIdleConnections()

	// 随机起始索引
	start := int(time.Now().UnixNano() % int64(len(httpsTestTargets)))

	for attempt := 0; attempt < 2; attempt++ {
		idx := (start + attempt) % len(httpsTestTargets)
		resp, err := client.Get(httpsTestTargets[idx])
		if err != nil {
			continue
		}
		discardResponseBody(resp.Body)
		resp.Body.Close()

		// 2xx 或 3xx 都算成功（部分网站会重定向）
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			return true
		}
	}

	return false
}

// ValidateAll 并发验证所有代理，返回验证结果。
// 不可取消；需要中断能力的调用方用 ValidateStream 并传入可取消的 ctx。
func (v *Validator) ValidateAll(proxies []storage.Proxy) []Result {
	var results []Result
	for r := range v.ValidateStream(context.Background(), proxies) {
		results = append(results, r)
	}
	return results
}

// ValidateStream 并发验证，边验证边通过 channel 返回结果。
//
// ctx 取消后：
//   - 不再派发新的探测（已排队等待 sem 的 goroutine 直接退出）；
//   - 阻塞在发送上的 goroutine 立即释放，channel 随即关闭。
//
// 这是消费者提前放弃时不泄漏 goroutine 的关键：channel 缓冲是
// min(len(proxies), concurrency*10)，当节点数超过该上限（默认 300×10=3000）
// 时发送方会阻塞；若消费者中途 break/return 且无取消机制，这些 goroutine
// 会永久卡在 `ch <- ...`，连同它们占用的 sem 槽位与连接一起泄漏到进程退出。
//
// 注意：ctx 不会中断"已经在执行"的单节点探测——ValidateOneResult 内部由
// client.Timeout 与风险评估预算约束（见 riskProbeBudgetFactor），
// 最坏约 4×ValidateTimeout 后自行返回。取消保证的是不再新增工作、
// 且不会有 goroutine 永久滞留，而不是立刻停止所有网络 IO。
func (v *Validator) ValidateStream(ctx context.Context, proxies []storage.Proxy) <-chan Result {
	if ctx == nil {
		ctx = context.Background()
	}
	ch := make(chan Result, concurrencyBuffer(len(proxies), v.concurrency))
	sem := make(chan struct{}, v.concurrency)
	var wg sync.WaitGroup

	go func() {
		for _, p := range proxies {
			// 取消后停止派发剩余节点；已在途的探测仍会跑完并尝试发送。
			select {
			case <-ctx.Done():
				wg.Wait()
				close(ch)
				return
			case sem <- struct{}{}:
			}
			wg.Add(1)
			go func(px storage.Proxy) {
				defer wg.Done()
				defer func() { <-sem }()
				result := v.ValidateOneResult(px)
				// 消费者已放弃时不得阻塞在发送上，否则 goroutine 与其
				// 占用的 sem 槽位、连接会滞留到进程退出。
				select {
				case ch <- result:
				case <-ctx.Done():
				}
			}(p)
		}
		wg.Wait()
		close(ch)
	}()

	return ch
}

// ValidateOne 保留旧调用约定；需要失败原因的调用方应使用 ValidateOneResult。
func (v *Validator) ValidateOne(p storage.Proxy) (bool, time.Duration, string, string, RiskInfo) {
	result := v.ValidateOneResult(p)
	return result.Valid, result.Latency, result.ExitIP, result.ExitLocation, result.Risk
}

// ValidateOneResult 验证单个代理并保留失败阶段，调用方据此区分策略拒绝与系统故障。
func (v *Validator) ValidateOneResult(p storage.Proxy) Result {
	var client *http.Client
	var err error

	switch p.Protocol {
	case "http":
		client, err = newHTTPClient(p.Address, p.Username, p.Password, v.timeout)
	case "socks5":
		client, err = newSOCKS5Client(p.Address, p.Username, p.Password, v.timeout)
	default:
		log.Printf("[validator] 未知协议 %s，节点 %s", p.Protocol, p.Address)
		return Result{Proxy: p, Risk: UnknownRisk(), FailureReason: FailureConnectivity}
	}
	if err != nil {
		return Result{Proxy: p, Risk: UnknownRisk(), FailureReason: FailureConnectivity}
	}
	defer client.CloseIdleConnections()

	latency, ok := v.validateConnectivity(client)
	if !ok {
		return Result{Proxy: p, Latency: latency, Risk: UnknownRisk(), FailureReason: FailureConnectivity}
	}
	if v.maxResponseMs > 0 && latency > time.Duration(v.maxResponseMs)*time.Millisecond {
		return Result{Proxy: p, Latency: latency, Risk: UnknownRisk(), FailureReason: FailureLatency}
	}

	ipInfo := getExitIPInfo(client)
	exitIP, exitLocation := ipInfo.IP, ipInfo.Location
	if exitIP == "" {
		return Result{Proxy: p, Latency: latency, ExitIP: exitIP, ExitLocation: exitLocation, Risk: UnknownRisk(), FailureReason: FailureExitMetadata}
	}
	if ok, reason := v.geoDecision(exitLocation); !ok {
		return Result{Proxy: p, Latency: latency, ExitIP: exitIP, ExitLocation: exitLocation, Risk: UnknownRisk(), FailureReason: reason}
	}
	if p.Protocol == "http" && !checkHTTPSConnect(p.Address, p.Username, p.Password, v.timeout) {
		return Result{Proxy: p, Latency: latency, ExitIP: exitIP, ExitLocation: exitLocation, Risk: UnknownRisk(), FailureReason: FailureHTTPConnectRejected}
	}

	return Result{
		Proxy: p, Valid: true, Latency: latency, ExitIP: exitIP, ExitLocation: exitLocation,
		Risk: assessRisk(client, ipInfo), FailureReason: FailureNone,
	}
}

// geoDecision 判定出口地点能否通过地理过滤，返回 (是否放行, 失败原因)。
//
// 地理过滤是策略控制，必须 fail-closed。原实现是
// `if len(exitLocation) >= 2 && !passesGeoFilter(exitLocation[:2])`，有两个问题：
//   - 取不出国家码时整个跳过过滤并放行节点（fail-open），与「不静默回退」冲突；
//   - `[:2]` 直接截断，"CNX Somewhere" 会被误判成被屏蔽的 "CN"，
//     " US Seattle" 又会取到 "  " 而漏过过滤。
//
// 改用 exitCountryCode：它按空白切分后走 config.NormalizeCountryCode，
// 与 Allowed/BlockedCountries 的归一化同源，两端不会因大小写或格式漂移而失配。
// 取不出合法 alpha-2 时归类为出口元数据缺失（FailureExitMetadata），
// 由调用方按探测失败处理，而不是当作"通过地理过滤"。
func (v *Validator) geoDecision(exitLocation string) (bool, FailureReason) {
	countryCode := exitCountryCode(exitLocation)
	if countryCode == "" {
		return false, FailureExitMetadata
	}
	if !v.passesGeoFilter(countryCode) {
		return false, FailureGeoRejected
	}
	return true, FailureNone
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
		discardResponseBody(resp.Body)
		resp.Body.Close()

		// 验证状态码（200 或 204 都接受）
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
			return latency, true
		}
	}
	return 0, false
}

func newHTTPClient(address, username, password string, timeout time.Duration) (*http.Client, error) {
	proxyURL, err := url.Parse(fmt.Sprintf("http://%s", address))
	if err != nil {
		return nil, err
	}
	// 认证 http 代理：把凭据放进 proxyURL.User，http.Transport 会据此发出
	// Proxy-Authorization 头。凭据仅存于内存中的 URL，绝不写入日志。
	if username != "" || password != "" {
		proxyURL.User = url.UserPassword(username, password)
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy:             http.ProxyURL(proxyURL),
			DisableKeepAlives: true,
		},
		Timeout: timeout,
	}, nil
}

func newSOCKS5Client(address, username, password string, timeout time.Duration) (*http.Client, error) {
	var auth *proxy.Auth
	if username != "" || password != "" {
		auth = &proxy.Auth{User: username, Password: password}
	}
	forward := &net.Dialer{Timeout: timeout}
	dialer, err := proxy.SOCKS5("tcp", address, auth, forward)
	if err != nil {
		return nil, err
	}
	contextDialer, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return nil, fmt.Errorf("socks5 dialer does not support context cancellation")
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext:       contextDialer.DialContext,
			DisableKeepAlives: true,
		},
		Timeout: timeout,
	}, nil
}
