package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/babutree/GeoProxy/config"
)

// TestAPIKeyCreateRejectsEmptyName 验证创建 API Key 时，
// 空名称或全空白名称必须在生成、保存任何 Key 前返回 HTTP 400。
// 有效名称的正向对照由
// TestAPIKeyCreateReturnsPlaintextOnceAndStoresHashOnly 覆盖。
func TestAPIKeyCreateRejectsEmptyName(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"empty_string", `{"name":""}`},
		{"missing_field", `{}`},
		{"whitespace_only", `{"name":"   "}`},
		{"tabs_and_newlines", "{\"name\":\" \\t\\n \"}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newTestServer(t)
			setTestGlobalConfig(t, server.cfg)

			req := authenticatedJSONRequest(http.MethodPost, "/api/apikey/create", tc.body)
			rec := httptest.NewRecorder()
			server.routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}

			// 拒绝路径不得生成或持久化任何 Key。
			if got := len(config.Get().ReadOnlyAPIKeys); got != 0 {
				t.Fatalf("keys persisted on empty-name reject = %d, want 0", got)
			}
			if got := len(server.cfg.ReadOnlyAPIKeys); got != 0 {
				t.Fatalf("live keys mutated on empty-name reject = %d, want 0", got)
			}

			// 错误响应不得回显任何生成的明文秘密，
			// 只能包含固定的校验消息。
			body := rec.Body.String()
			var payload map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode error body: %v body=%s", err, body)
			}
			if _, ok := payload["error"]; !ok {
				t.Fatalf("error response missing 'error' field: %s", body)
			}
			if _, ok := payload["key"]; ok {
				t.Fatalf("error response leaked a key field: %s", body)
			}
		})
	}
}

// TestAPIKeyCreateAcceptsValidNameAfterReject 是对应的正向测试：
// 空名称请求被拒绝后，有效名称仍可创建成功，
// 并且只保存一个 Key 指纹。
func TestAPIKeyCreateAcceptsValidNameAfterReject(t *testing.T) {
	server := newTestServer(t)
	setTestGlobalConfig(t, server.cfg)

	// 先验证拒绝路径。
	rej := authenticatedJSONRequest(http.MethodPost, "/api/apikey/create", `{"name":"  "}`)
	recRej := httptest.NewRecorder()
	server.routes().ServeHTTP(recRej, rej)
	if recRej.Code != http.StatusBadRequest {
		t.Fatalf("empty-name status = %d, want 400", recRej.Code)
	}

	// 再验证有效创建路径。
	ok := authenticatedJSONRequest(http.MethodPost, "/api/apikey/create", `{"name":"prod"}`)
	recOK := httptest.NewRecorder()
	server.routes().ServeHTTP(recOK, ok)
	if recOK.Code != http.StatusOK {
		t.Fatalf("valid create status = %d, want 200; body=%s", recOK.Code, recOK.Body.String())
	}
	if got := len(config.Get().ReadOnlyAPIKeys); got != 1 {
		t.Fatalf("keys after valid create = %d, want 1", got)
	}
}

// TestWebUIAPIKeyHashUsesConfigCanonical 验证 webui 指纹适配点与
// config 规范辅助函数产生完全相同的输出。
func TestWebUIAPIKeyHashUsesConfigCanonical(t *testing.T) {
	for _, plain := range []string{"abc", "another-secret", "  spaced  "} {
		if apiKeySHA256(plain) != config.HashAPIKey(plain) {
			t.Fatalf("apiKeySHA256(%q)=%q != config.HashAPIKey=%q",
				plain, apiKeySHA256(plain), config.HashAPIKey(plain))
		}
	}
	// 反例对照：这两个固定的不同输入不应产生相同指纹。
	if apiKeySHA256("x") == apiKeySHA256("y") {
		t.Fatal("distinct plaintext collided in apiKeySHA256")
	}
}

// TestLegacyBareSHA256KeyStillAuthenticates 固化当前向后兼容合同：
// 以裸 SHA-256 十六进制指纹持久化的旧 Key，在集中指纹实现后仍可通过
// 只读 API 中间件鉴权。legacyHash 是独立于被测代码的固定已知答案，
// 因而当前格式意外漂移时测试会失败；未来若采用版本化迁移，则应同时
// 更新迁移路径和该兼容性测试，而非永久禁止格式演进。
func TestLegacyBareSHA256KeyStillAuthenticates(t *testing.T) {
	const (
		plain      = "legacy-persisted-key"
		legacyHash = "cdc5c4494fbd5368953db8ca0be17cfb1120e32399d1c2ae710dc873adcba7de"
	)
	server := newReadOnlyAPITestServer(t, []config.APIKey{{
		ID:   "k-legacy",
		Name: "legacy",
		Hash: legacyHash,
	}}, 60)

	// 正向对照：旧 Key 可以通过鉴权。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy bare-SHA256 key rejected: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// 反向对照：同一旧指纹必须拒绝错误 Key。
	bad := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	bad.Header.Set("Authorization", "Bearer not-the-legacy-key")
	recBad := httptest.NewRecorder()
	server.routes().ServeHTTP(recBad, bad)
	if recBad.Code != http.StatusUnauthorized {
		t.Fatalf("wrong key accepted against legacy hash: status = %d, want 401", recBad.Code)
	}
}

// TestLegacyEnvImportedKeyPersistsAndAuthenticates 验证环境变量导入的 Key
// 以裸 SHA-256 指纹保存到 config.json 后，仍能通过
// config.ValidateReadOnlyAPIKey 校验；同时检查磁盘指纹与规范辅助函数
// 输出相同，防止出现分叉格式。
func TestLegacyEnvImportedKeyPersistsAndAuthenticates(t *testing.T) {
	server := newTestServer(t)
	setTestGlobalConfig(t, server.cfg)

	plain := "env-style-legacy-key"
	// 模拟此前已持久化的环境变量导入 Key：只保存指纹。
	server.cfg.ReadOnlyAPIKeys = []config.APIKey{{
		ID:   "env-import",
		Name: "env-import",
		Hash: config.HashAPIKey(plain),
	}}
	if err := config.Save(server.cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	raw, err := os.ReadFile(config.ConfigFile())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	disk := string(raw)
	if strings.Contains(disk, plain) {
		t.Fatalf("plaintext leaked to disk: %s", disk)
	}
	if !strings.Contains(disk, config.HashAPIKey(plain)) {
		t.Fatalf("config.json missing canonical hash for persisted key")
	}
	if !config.ValidateReadOnlyAPIKey(config.Get(), plain) {
		t.Fatal("persisted bare-SHA256 key failed validation after change")
	}
	if config.ValidateReadOnlyAPIKey(config.Get(), "wrong") {
		t.Fatal("wrong key validated against persisted hash")
	}
}
