package validator

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
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
	if concurrency < 1 {
		concurrency = 1
	}
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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ipAPIInfo{}
	}

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
			"User-Agent": "goproxy-ai-probe/1.0",
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
			"User-Agent":        "goproxy-ai-probe/1.0",
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
			"User-Agent": "goproxy-ai-probe/1.0",
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
			"User-Agent": "goproxy-ai-probe/1.0",
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

// probeAIReachability 经传入的 *http.Client（即走该代理）逐个探测 4 个 AI 服务，
// 返回 JSON 对象字符串，如 {"openai":0,"claude":1,"grok":-1,"gemini":0}。
//
// 主信号 = 稳定 API（401/缺 key 等）；辅信号 = 产品层明确地区锁/放行指纹。
// 合并规则见 mergeAIProbeResults：明确封禁优先；任一明确可达则可达。
// 账号/密钥/配额不作为 IP 封禁依据。CF 拦截另由 probeCloudflareBlocked 单独记录。
//
// 每个探测复用 client 已有的 Timeout。4 个服务串行执行；任一探测异常均不 panic。
func probeAIReachability(client *http.Client) string {
	results := make(map[string]int, len(aiProbeTargets))
	for name, target := range aiProbeTargets {
		api := probeOneAIForService(client, name, target)
		product := probeAIProductLayers(client, name)
		results[name] = mergeAIProbeResults(api, product)
	}
	data, err := json.Marshal(results)
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
func classifyAIProductLayer(service string, statusCode int, header http.Header, req *http.Request, body string) int {
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
