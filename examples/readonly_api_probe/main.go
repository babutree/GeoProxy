// 简易 GeoProxy 只读 API 对接与探测工具。
//
// 能力覆盖：
//   GET /api/v1/ping
//   GET /api/v1/nodes   （含 region/protocol/connect/cf/ai/分页 过滤）
//   GET /api/v1/occupancy
//
// 用法示例：
//
//	go run . -base http://127.0.0.1:7800 -key YOUR_API_KEY
//	go run . -base http://HOST:7800 -key YOUR_API_KEY -region us -protocol socks5 -connect gateway -limit 20
//	go run . -base http://HOST:7800 -key YOUR_API_KEY -header x-api-key -ai openai,claude -cf open
//
// 环境变量（可替代 flag，flag 优先）：
//
//	GEOPROXY_API_BASE   例如 http://127.0.0.1:7800
//	GEOPROXY_API_KEY    只读 API Key 明文（仅创建时显示一次）
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type probeConfig struct {
	BaseURL    string
	APIKey     string
	HeaderMode string // bearer | x-api-key
	Timeout    time.Duration

	Region   string
	Protocol string
	Source   string
	Connect  string
	Status   string
	MaxAbuse string
	CF       string
	AI       string
	Limit    int
	Offset   int

	SkipOccupancy bool
	Pretty        bool
	FailFast      bool
}

type nodesResponse struct {
	Total int              `json:"total"`
	Count int              `json:"count"`
	Nodes []map[string]any `json:"nodes"`
}

type occupancyRow struct {
	ProxyID                  int64  `json:"proxy_id"`
	Address                  string `json:"address"`
	ActiveSessions           int    `json:"active_sessions"`
	MaxSessions              int    `json:"max_sessions"`
	CooldownRemainingSeconds int64  `json:"cooldown_remaining_seconds"`
	Note                     string `json:"note,omitempty"`
}

func main() {
	cfg, err := parseConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "参数错误: %v\n", err)
		os.Exit(2)
	}
	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "探测失败: %v\n", err)
		os.Exit(1)
	}
}

func parseConfig(args []string) (probeConfig, error) {
	fs := flag.NewFlagSet("readonly_api_probe", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	cfg := probeConfig{
		BaseURL:    strings.TrimSpace(os.Getenv("GEOPROXY_API_BASE")),
		APIKey:     strings.TrimSpace(os.Getenv("GEOPROXY_API_KEY")),
		HeaderMode: "bearer",
		Timeout:    15 * time.Second,
		Limit:      20,
		Pretty:     true,
	}

	fs.StringVar(&cfg.BaseURL, "base", cfg.BaseURL, "API 根地址，如 http://127.0.0.1:7800")
	fs.StringVar(&cfg.APIKey, "key", cfg.APIKey, "只读 API Key")
	fs.StringVar(&cfg.HeaderMode, "header", cfg.HeaderMode, "鉴权头：bearer 或 x-api-key")
	fs.DurationVar(&cfg.Timeout, "timeout", cfg.Timeout, "HTTP 超时")
	fs.StringVar(&cfg.Region, "region", "", "nodes 过滤 region=us")
	fs.StringVar(&cfg.Protocol, "protocol", "", "nodes 过滤 protocol=http|socks5")
	fs.StringVar(&cfg.Source, "source", "", "nodes 过滤 source=manual|subscription")
	fs.StringVar(&cfg.Connect, "connect", "", "nodes 过滤 connect=direct|gateway")
	fs.StringVar(&cfg.Status, "status", "", "nodes 过滤 status=all（默认仅可用）")
	fs.StringVar(&cfg.MaxAbuse, "max-abuse", "", "nodes 过滤 max_abuse=0..1")
	fs.StringVar(&cfg.CF, "cf", "", "nodes 过滤 cf=open|blocked")
	fs.StringVar(&cfg.AI, "ai", "", "nodes 过滤 ai=openai,claude,...")
	fs.IntVar(&cfg.Limit, "limit", cfg.Limit, "nodes limit（1..2000，默认 20 便于预览）")
	fs.IntVar(&cfg.Offset, "offset", 0, "nodes offset")
	fs.BoolVar(&cfg.SkipOccupancy, "skip-occupancy", false, "跳过 occupancy")
	fs.BoolVar(&cfg.Pretty, "pretty", true, "JSON 缩进输出")
	fs.BoolVar(&cfg.FailFast, "fail-fast", true, "任一步失败立即退出")

	if err := fs.Parse(args); err != nil {
		return probeConfig{}, err
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.HeaderMode = strings.ToLower(strings.TrimSpace(cfg.HeaderMode))
	if cfg.BaseURL == "" {
		return probeConfig{}, errors.New("缺少 -base 或 GEOPROXY_API_BASE")
	}
	if cfg.APIKey == "" {
		return probeConfig{}, errors.New("缺少 -key 或 GEOPROXY_API_KEY")
	}
	if cfg.HeaderMode != "bearer" && cfg.HeaderMode != "x-api-key" {
		return probeConfig{}, fmt.Errorf("无效 -header %q，应为 bearer 或 x-api-key", cfg.HeaderMode)
	}
	if _, err := url.ParseRequestURI(cfg.BaseURL); err != nil {
		return probeConfig{}, fmt.Errorf("无效 -base: %w", err)
	}
	return cfg, nil
}

func run(cfg probeConfig) error {
	client := &http.Client{Timeout: cfg.Timeout}
	fmt.Printf("== GeoProxy 只读 API 探测 ==\nbase=%s header=%s timeout=%s\n\n", cfg.BaseURL, cfg.HeaderMode, cfg.Timeout)

	// 1) ping
	if err := stepPing(client, cfg); err != nil {
		if cfg.FailFast {
			return err
		}
		fmt.Fprintf(os.Stderr, "[warn] ping: %v\n", err)
	}

	// 2) nodes
	if err := stepNodes(client, cfg); err != nil {
		if cfg.FailFast {
			return err
		}
		fmt.Fprintf(os.Stderr, "[warn] nodes: %v\n", err)
	}

	// 3) occupancy
	if !cfg.SkipOccupancy {
		if err := stepOccupancy(client, cfg); err != nil {
			if cfg.FailFast {
				return err
			}
			fmt.Fprintf(os.Stderr, "[warn] occupancy: %v\n", err)
		}
	}

	fmt.Println("== 探测完成 ==")
	return nil
}

func stepPing(client *http.Client, cfg probeConfig) error {
	fmt.Println("-- GET /api/v1/ping")
	status, body, err := doJSON(client, cfg, http.MethodGet, "/api/v1/ping", nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("HTTP %d body=%s", status, truncate(string(body), 300))
	}
	printJSON(body, cfg.Pretty)
	fmt.Println()
	return nil
}

func stepNodes(client *http.Client, cfg probeConfig) error {
	q := url.Values{}
	setIf := func(k, v string) {
		if strings.TrimSpace(v) != "" {
			q.Set(k, strings.TrimSpace(v))
		}
	}
	setIf("region", cfg.Region)
	setIf("protocol", cfg.Protocol)
	setIf("source", cfg.Source)
	setIf("connect", cfg.Connect)
	setIf("status", cfg.Status)
	setIf("max_abuse", cfg.MaxAbuse)
	setIf("cf", cfg.CF)
	setIf("ai", cfg.AI)
	if cfg.Limit > 0 {
		q.Set("limit", strconv.Itoa(cfg.Limit))
	}
	if cfg.Offset > 0 {
		q.Set("offset", strconv.Itoa(cfg.Offset))
	}

	path := "/api/v1/nodes"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	fmt.Printf("-- GET %s\n", path)

	status, body, err := doJSON(client, cfg, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if status == http.StatusTooManyRequests {
		return fmt.Errorf("HTTP 429 限流（默认 60/min/key）；请降低频率或稍后重试；body=%s", truncate(string(body), 200))
	}
	if status == http.StatusUnauthorized {
		return fmt.Errorf("HTTP 401 鉴权失败；检查 Key 是否正确、是否已吊销；body=%s", truncate(string(body), 200))
	}
	if status == http.StatusBadRequest {
		return fmt.Errorf("HTTP 400 参数非法；body=%s", truncate(string(body), 300))
	}
	if status != http.StatusOK {
		return fmt.Errorf("HTTP %d body=%s", status, truncate(string(body), 300))
	}

	var resp nodesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("解析 nodes 响应: %w", err)
	}
	fmt.Printf("total=%d count=%d (本页)\n", resp.Total, resp.Count)
	printJSON(body, cfg.Pretty)
	fmt.Println()

	// 对接提示摘要（不重复整包 JSON）
	summarizeNodes(resp.Nodes)
	fmt.Println()
	return nil
}

func stepOccupancy(client *http.Client, cfg probeConfig) error {
	fmt.Println("-- GET /api/v1/occupancy")
	status, body, err := doJSON(client, cfg, http.MethodGet, "/api/v1/occupancy", nil)
	if err != nil {
		return err
	}
	if status == http.StatusTooManyRequests {
		return fmt.Errorf("HTTP 429 限流；body=%s", truncate(string(body), 200))
	}
	if status != http.StatusOK {
		return fmt.Errorf("HTTP %d body=%s", status, truncate(string(body), 300))
	}

	var rows []occupancyRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return fmt.Errorf("解析 occupancy 响应: %w", err)
	}
	fmt.Printf("occupancy_rows=%d\n", len(rows))
	printJSON(body, cfg.Pretty)
	fmt.Println()

	// 安全边界抽查：私有地址不得明文出现（应脱敏为 gateway-local）
	for _, row := range rows {
		if strings.HasPrefix(row.Address, "127.") || strings.Contains(row.Address, "192.168.") || strings.Contains(row.Address, "10.") {
			// 宽松提示：若服务端正确脱敏则不会命中；命中则告警
			fmt.Fprintf(os.Stderr, "[warn] occupancy 地址疑似未脱敏: proxy_id=%d address=%q note=%q\n", row.ProxyID, row.Address, row.Note)
		}
	}
	return nil
}

func summarizeNodes(nodes []map[string]any) {
	if len(nodes) == 0 {
		fmt.Println("本页无节点。")
		return
	}
	fmt.Println("本页摘要（连接方式）:")
	directN, gatewayN := 0, 0
	for i, n := range nodes {
		id := fmt.Sprint(n["id"])
		region := fmt.Sprint(n["region"])
		protocol := fmt.Sprint(n["protocol"])
		status := fmt.Sprint(n["status"])
		conn, _ := n["connect"].(map[string]any)
		mode := ""
		if conn != nil {
			mode = fmt.Sprint(conn["mode"])
		}
		switch mode {
		case "direct":
			directN++
			host := fmt.Sprint(conn["host"])
			port := fmt.Sprint(conn["port"])
			fmt.Printf("  [%d] id=%s region=%s protocol=%s status=%s connect=direct %s:%s\n",
				i+1, id, region, protocol, status, host, port)
		case "gateway":
			gatewayN++
			host := fmt.Sprint(conn["host"])
			socks := fmt.Sprint(conn["gateway_socks5_port"])
			httpPort := fmt.Sprint(conn["gateway_http_port"])
			hint, hasHint := conn["username_hint"].(string)
			hintErr, hasErr := conn["username_hint_error"].(string)
			line := fmt.Sprintf("  [%d] id=%s region=%s protocol=%s status=%s connect=gateway host=%s socks=%s http=%s",
				i+1, id, region, protocol, status, host, socks, httpPort)
			if hasHint && strings.TrimSpace(hint) != "" {
				line += " username_hint=" + hint
			} else if hasErr && strings.TrimSpace(hintErr) != "" {
				line += " username_hint_error=" + hintErr
			} else {
				line += " username_hint=<missing>"
			}
			fmt.Println(line)
		default:
			fmt.Printf("  [%d] id=%s region=%s protocol=%s status=%s connect=%v\n",
				i+1, id, region, protocol, status, mode)
		}
	}
	fmt.Printf("本页统计: direct=%d gateway=%d\n", directN, gatewayN)
	fmt.Println("提示: gateway 节点须用网关端口 + 代理认证密码（API 不下发密码）；direct 节点可直连 host:port。")
}

func doJSON(client *http.Client, cfg probeConfig, method, path string, body io.Reader) (int, []byte, error) {
	u := cfg.BaseURL + path
	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	switch cfg.HeaderMode {
	case "x-api-key":
		req.Header.Set("X-API-Key", cfg.APIKey)
	default:
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, raw, nil
}

func printJSON(raw []byte, pretty bool) {
	if !pretty {
		fmt.Println(string(raw))
		return
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		fmt.Println(string(raw))
		return
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
