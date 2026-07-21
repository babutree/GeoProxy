package main

import (
	"bytes"
	"encoding/json"
	stdhtml "html"
	"log"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/babutree/GeoProxy/auth"
	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/storage"
)

const canonicalUsernameDSL = `<base>[-region-<cc>][-unlock-<token>][-node-<host:port|key-<base64url(nodeKey)>>][-session-<id>]`

func readRepositoryDocument(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return string(data)
}

func requireUsernameDSLSemantics(t *testing.T, path, content string) {
	t.Helper()
	if !strings.Contains(content, canonicalUsernameDSL) {
		t.Fatalf("%s 缺少包含 node 的固定顺序 DSL：%s", path, canonicalUsernameDSL)
	}
	checks := []struct {
		name    string
		options []string
	}{
		{name: "稳定配置身份", options: []string{"stable configuration identity", "stable node identity", "稳定配置身份", "稳定节点身份"}},
		{name: "兼容入口地址", options: []string{"compatibility entrance address", "backward-compatible entrance address", "兼容入口地址", "兼容旧入口地址"}},
		{name: "不是最终出口 IP", options: []string{"not the final exit IP", "not a final exit IP", "不是最终出口 IP", "非最终出口 IP"}},
		{name: "无匹配时不回退", options: []string{"no fallback", "never falls back", "不回退", "无回退"}},
		{name: "node 优先于 session 黏连", options: []string{"node pin takes precedence over session affinity", "node pin determines routing", "node 锁定优先于 session 黏连", "node 锁定决定路由"}},
	}
	for _, check := range checks {
		found := false
		for _, option := range check.options {
			if strings.Contains(content, option) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s 缺少 DSL 合同语义：%s", path, check.name)
		}
	}
}

func TestReadmeProxyExamplesAvoidHttpbinSinglePoint(t *testing.T) {
	readme := readRepositoryDocument(t, "README.md")
	if strings.Contains(readme, "httpbin.org") {
		t.Fatal("README.md still uses httpbin.org in proxy examples")
	}
	if !strings.Contains(readme, "https://www.gstatic.com/generate_204") {
		t.Fatal("README.md missing stable HTTPS proxy example target")
	}
}

func TestReadmeCredentialSettingsScopeIsExplicit(t *testing.T) {
	readme := strings.Join(strings.Fields(readRepositoryDocument(t, "README.md")), " ")
	for _, stale := range []string{
		"You can change all credentials later under **Settings**.",
		"then log in and change them in Settings.",
		"Settings for authentication, default region",
		"Change them in the WebUI **Settings** page.",
	} {
		if strings.Contains(readme, stale) {
			t.Fatalf("README.md 仍泛化 Settings 可修改全部凭据：%s", stale)
		}
	}
	for _, required := range []string{
		"Proxy authentication username/password can be changed in the WebUI **Settings** panel.",
		"The WebUI login password is not editable in Settings; use the reset procedure below.",
		"The proxy authentication username/password can be changed in the WebUI **Settings** page.",
		"The WebUI login password is not editable there; use the reset procedure below.",
		"Settings for proxy authentication, default region",
		"Reset only the WebUI password (keeps proxy authentication username, filters, and all subscription",
	} {
		if !strings.Contains(readme, required) {
			t.Fatalf("README.md 缺少凭据范围合同：%s", required)
		}
	}
}

func TestFirstBootCredentialLogMatchesSettingsScope(t *testing.T) {
	previousOutput := log.Writer()
	previousFlags := log.Flags()
	previousPrefix := log.Prefix()
	var output bytes.Buffer
	log.SetOutput(&output)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	})

	logFirstBootCredentials(&config.FirstBootInfo{
		WebUIPassword:     "webui-test-secret",
		ProxyAuthUsername: "proxy-test-user",
		ProxyAuthPassword: "proxy-test-secret",
	})
	got := output.String()
	for _, required := range []string{
		"[main]   代理认证用户名/密码可在 WebUI“系统设置”中修改。",
		"[main]   WebUI 登录密码不在“系统设置”中修改；遗失时请按 README 的重置流程处理。",
	} {
		if !strings.Contains(got, required) {
			t.Fatalf("首次启动日志缺少凭据范围合同 %q；日志=%s", required, got)
		}
	}
	for _, stale := range []string{
		"登录 WebUI 后可在“系统设置”中修改这些凭据",
		"用户须用这些凭据登录 WebUI 后自行修改",
		"用户应尽快登录 WebUI 修改",
	} {
		if strings.Contains(got, stale) {
			t.Fatalf("首次启动日志仍泛化凭据修改范围 %q；日志=%s", stale, got)
		}
	}

	mainSource := readRepositoryDocument(t, "main.go")
	for _, staleComment := range []string{
		"用户须用这些凭据登录 WebUI 后自行修改",
		"用户应尽快登录 WebUI 修改",
	} {
		if strings.Contains(mainSource, staleComment) {
			t.Fatalf("main.go 注释仍泛化凭据修改范围：%s", staleComment)
		}
	}
}

func TestUsernameDSLPublicContractMatchesParser(t *testing.T) {
	// DESIGN_LANGUAGE.md 是本地设计原型，未纳入 Git；CI 合同只读取公开跟踪文档与 UI 资产。
	for _, path := range []string{
		"README.md",
		"GEO_FILTER.md",
		"docs/PRD.md",
	} {
		t.Run(path, func(t *testing.T) {
			content := readRepositoryDocument(t, path)
			requireUsernameDSLSemantics(t, path, content)
			if strings.Contains(content, `[-node-<host:port>][-session-<id>]`) {
				t.Fatalf("%s 仍把 host:port 写成唯一 node 语法", path)
			}
		})
	}

	for _, path := range []string{"webui/dashboard.go", "webui/dashboard_assets.go"} {
		t.Run(path, func(t *testing.T) {
			content := stdhtml.UnescapeString(readRepositoryDocument(t, path))
			requireUsernameDSLSemantics(t, path, content)
			if strings.Contains(content, "-node-IP:端口") {
				t.Fatalf("%s 仍把入口地址写成唯一推荐 node 形式", path)
			}
		})
	}

	const nodeKey = "trojan:jp.example.com:443:node01"
	example := "username-region-jp-unlock-gpt-node-key-" + auth.EncodeNodeKeyPin(nodeKey) + "-session-app01"
	readme := readRepositoryDocument(t, "README.md")
	if !strings.Contains(readme, "`"+example+"`") {
		t.Fatalf("README.md 缺少可执行的完整 node-key 示例：%s", example)
	}
	parsed, err := auth.ParseUsername(example)
	if err != nil {
		t.Fatalf("文档示例无法由 auth.ParseUsername 解析：%v", err)
	}
	if parsed.Base != "username" || parsed.Region != "jp" || parsed.Node != "key-"+nodeKey || parsed.Session != "app01" || strings.Join(parsed.Unlock, ",") != "openai" {
		t.Fatalf("文档示例解析结果错误：%#v", parsed)
	}
}

func TestDataDirectoryDocumentationMatchesRuntimeContracts(t *testing.T) {
	document := readRepositoryDocument(t, "DATA_DIRECTORY.md")
	compose := readRepositoryDocument(t, "docker-compose.yml")
	envExample := readRepositoryDocument(t, ".env.example")
	prd := readRepositoryDocument(t, "docs/PRD.md")

	for _, fragment := range []string{
		"type: bind",
		"source: ${HOST_DATA_DIR:-./data}",
		"target: /app/data",
		"DATA_DIR=/app/data",
	} {
		if !strings.Contains(compose, fragment) {
			t.Fatalf("docker-compose.yml 缺少数据目录合同：%s", fragment)
		}
	}
	if !strings.Contains(envExample, "HOST_DATA_DIR=./data") {
		t.Fatal(".env.example 缺少 HOST_DATA_DIR=./data")
	}

	for _, fragment := range []string{
		"默认 Compose 使用 bind mount",
		"`${HOST_DATA_DIR:-./data}`",
		"`/app/data`",
		"`DATA_DIR=/app/data`",
		"`os.UserConfigDir()/GeoProxy`",
		"不会自动迁移",
		"首次成功启动会生成并写入 `config.json`",
	} {
		if !strings.Contains(document, fragment) {
			t.Fatalf("DATA_DIRECTORY.md 缺少运行时合同：%s", fragment)
		}
	}
	for _, stale := range []string{
		"默认 `docker-compose.yml` 使用 Docker named volume",
		"Dokploy / 生产部署（Named Volume）",
		"使用 Named Volume（推荐）",
		"如果还没有通过 WebUI 修改配置，这个文件可能不存在",
	} {
		if strings.Contains(document, stale) {
			t.Fatalf("DATA_DIRECTORY.md 仍含过期说法：%s", stale)
		}
	}
	for _, fragment := range []string{"`DATA_DIR`", "`os.UserConfigDir()/GeoProxy`", "`HOST_DATA_DIR`", "bind mount"} {
		if !strings.Contains(prd, fragment) {
			t.Fatalf("docs/PRD.md 缺少数据目录合同：%s", fragment)
		}
	}

	assertDocumentedConfigJSONKeys(t, document)
	assertDocumentedSQLiteColumns(t, document, "proxies", "### `proxies`", "### `subscriptions`")
	assertDocumentedSQLiteColumns(t, document, "subscriptions", "### `subscriptions`", "## 备份与恢复")
}

func TestDataDirectoryDocumentationDoesNotDescribeResolvedManagerAsCWDDrift(t *testing.T) {
	document := strings.Join(strings.Fields(readRepositoryDocument(t, "DATA_DIRECTORY.md")), " ")
	if !strings.Contains(document, "`custom.NewManager` 均使用 `config.DataDir()` 的解析结果") {
		t.Error("DATA_DIRECTORY.md 未明确 custom.NewManager 使用 config.DataDir() 的解析结果")
	}
	for _, stale := range []string{
		"`custom.NewManager` 的 sing-box 生命周期 仍直接读取 `DATA_DIR`",
		"未设置时不会自动继承该原生默认目录",
		"避免 sing-box 临时配置落到 CWD",
		"`BUGFIX-075`",
	} {
		if strings.Contains(document, stale) {
			t.Errorf("DATA_DIRECTORY.md 仍把已统一的数据目录链写成未完成：%s", stale)
		}
	}
}

func TestPRDHasNoLegacyTenMinuteSessionTTL(t *testing.T) {
	prd := readRepositoryDocument(t, "docs/PRD.md")
	legacySessionTTL := regexp.MustCompile(`(?im)(?:TTL|会话)[^\r\n]{0,80}(?:^|[^0-9])10\s*分钟|(?:^|[^0-9])10\s*分钟[^\r\n]{0,80}(?:TTL|会话)`)
	if matches := legacySessionTTL.FindAllString(prd, -1); len(matches) > 0 {
		t.Fatalf("docs/PRD.md 仍含 10 分钟会话 TTL 旧合同：%q", matches)
	}
	for _, stale := range []string{"会话 TTL 10 分钟", "TTL 10 分钟", "10 分钟会话 TTL"} {
		if !legacySessionTTL.MatchString(stale) {
			t.Errorf("10 分钟会话 TTL 旧合同未被正则捕获：%q", stale)
		}
	}
	for _, legal := range []string{"会话 TTL 110 分钟", "会话 TTL 210 分钟", "会话 TTL 1010 分钟"} {
		if matches := legacySessionTTL.FindAllString(legal, -1); len(matches) > 0 {
			t.Fatalf("合法的多位数会话 TTL 被误报：%q -> %q", legal, matches)
		}
	}
}

func TestReadmeDocumentsNativeDefaultDataDirectory(t *testing.T) {
	section := documentSection(t, readRepositoryDocument(t, "README.md"), "## Configuration", "### Lost the WebUI password?")
	var dataDirRow string
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "| `DATA_DIR` |") {
			dataDirRow = strings.Join(strings.Fields(line), " ")
			break
		}
	}
	if dataDirRow == "" {
		t.Fatal("README.md Configuration 表缺少 DATA_DIR 行")
	}
	for _, required := range []string{
		"`os.UserConfigDir()/GeoProxy` when unset",
		"`/app/data`",
		"`HOST_DATA_DIR`",
	} {
		if !strings.Contains(dataDirRow, required) {
			t.Errorf("README.md DATA_DIR 行缺少合同 %q：%s", required, dataDirRow)
		}
	}
	if strings.Contains(dataDirRow, "| empty | Optional directory") {
		t.Error("README.md DATA_DIR 行仍把默认写成 empty 的可选目录")
	}
}

func assertDocumentedConfigJSONKeys(t *testing.T, document string) {
	t.Helper()
	t.Setenv("DATA_DIR", t.TempDir())
	cfg := config.Load()
	cfg.DefaultRegion = "JP"
	cfg.AllowedCountries = []string{"JP"}
	cfg.ReadOnlyAPIKeys = []config.APIKey{{
		ID:   "docs-contract",
		Name: "docs-contract",
		Hash: config.HashAPIKey("docs-contract-secret"),
	}}
	cfg.PublicHost = "gateway.example"
	if err := config.Save(cfg); err != nil {
		t.Fatalf("保存文档合同配置失败：%v", err)
	}
	raw, err := os.ReadFile(config.ConfigFile())
	if err != nil {
		t.Fatalf("读取文档合同配置失败：%v", err)
	}
	var persisted map[string]json.RawMessage
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatalf("解析文档合同配置失败：%v", err)
	}
	section := documentSection(t, document, "### `config.json`", "## SQLite schema")
	for key := range persisted {
		if !strings.Contains(section, "`"+key+"`") {
			t.Fatalf("DATA_DIRECTORY.md 的 config.json 小节缺少实际持久化键：%s", key)
		}
	}
}

func assertDocumentedSQLiteColumns(t *testing.T, document, table, startHeading, endHeading string) {
	t.Helper()
	store, err := storage.New(":memory:")
	if err != nil {
		t.Fatalf("创建文档合同数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rows, err := store.GetDB().Query("SELECT name FROM pragma_table_info(?) ORDER BY cid", table)
	if err != nil {
		t.Fatalf("读取 %s schema 失败：%v", table, err)
	}
	defer rows.Close()
	section := documentSection(t, document, startHeading, endHeading)
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatalf("读取 %s 列失败：%v", table, err)
		}
		if !strings.Contains(section, "`"+column+"`") {
			t.Fatalf("DATA_DIRECTORY.md 的 %s 小节缺少实际列：%s", table, column)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历 %s schema 失败：%v", table, err)
	}
}

func documentSection(t *testing.T, content, startHeading, endHeading string) string {
	t.Helper()
	start := strings.Index(content, startHeading)
	if start < 0 {
		t.Fatalf("文档缺少小节：%s", startHeading)
	}
	endOffset := strings.Index(content[start+len(startHeading):], endHeading)
	if endOffset < 0 {
		t.Fatalf("文档缺少小节终点：%s", endHeading)
	}
	return content[start : start+len(startHeading)+endOffset]
}

func TestReadOnlyAPIDesignMatchesAPIKeyLifecycle(t *testing.T) {
	design := readRepositoryDocument(t, "docs/READONLY_API_DESIGN.md")
	handlers := readRepositoryDocument(t, "webui/apikey_handlers.go")
	server := readRepositoryDocument(t, "webui/server.go")
	middleware := readRepositoryDocument(t, "webui/api_key_middleware.go")

	for _, fragment := range []string{"raw := make([]byte, 16)", "rand.Read(raw)"} {
		if !strings.Contains(handlers, fragment) {
			t.Fatalf("API Key 生成源码合同已变化：缺少 %s", fragment)
		}
	}
	if !strings.Contains(server, "apiKeyLimiter: newAPIKeyRateLimiter(rate, time.Now)") {
		t.Fatal("Server 构造时初始化 API Key 限流器的源码合同已变化")
	}
	if !strings.Contains(middleware, "func (s *Server) ensureAPIKeyLimiter()") {
		t.Fatal("ensureAPIKeyLimiter 防御性入口已移除，需同步设计文档测试")
	}

	if strings.Contains(design, "复用现有随机凭据逻辑") {
		t.Fatal("READONLY_API_DESIGN 仍误称 API Key 复用其它凭据 helper")
	}
	for _, fragment := range []string{
		"`webui/apikey_handlers.go`",
		"`crypto/rand`",
		"16 字节",
		"独立生成",
		"Server 构造时",
		"`ensureAPIKeyLimiter`",
		"防御性惰性初始化",
		"首次构造",
	} {
		if !strings.Contains(design, fragment) {
			t.Fatalf("READONLY_API_DESIGN 缺少 API Key 生命周期合同：%s", fragment)
		}
	}
}

func TestSessionTTLAndCredentialSettingsDocumentationMatchesRuntime(t *testing.T) {
	readme := readRepositoryDocument(t, "README.md")
	compose := readRepositoryDocument(t, "docker-compose.yml")
	envExample := readRepositoryDocument(t, ".env.example")
	prd := readRepositoryDocument(t, "docs/PRD.md")
	settings := readRepositoryDocument(t, "webui/dashboard.go")

	for path, content := range map[string]string{
		"docker-compose.yml": compose,
		".env.example":       envExample,
		"README.md":          readme,
		"docs/PRD.md":        prd,
	} {
		if !strings.Contains(content, "1440") {
			t.Fatalf("%s 缺少 Session TTL 1440 分钟默认合同", path)
		}
	}
	if strings.Contains(compose, "SESSION_TTL_MINUTES:-10") ||
		strings.Contains(prd, "TTL = 10 分钟") ||
		strings.Contains(prd, "| SESSION_TTL_MINUTES | 10 |") {
		t.Fatal("公开部署/PRD 仍保留 Session TTL 10 分钟旧默认")
	}

	if !strings.Contains(settings, "代理认证密码（留空不改）") {
		t.Fatal("Settings 页面未明确只提供代理认证密码修改")
	}
	if strings.Contains(settings, "WebUI 登录密码") {
		t.Fatal("Settings 页面不应声称可修改 WebUI 登录密码")
	}
	if !strings.Contains(readme, "Proxy authentication username/password can be changed in the WebUI **Settings** panel.") ||
		!strings.Contains(readme, "The WebUI login password is not editable in Settings") {
		t.Fatal("README 未区分代理凭据可编辑与 WebUI 登录密码重置合同")
	}
}
