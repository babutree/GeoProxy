package webui

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"unsafe"

	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/custom"
	"github.com/babutree/GeoProxy/storage"
	"github.com/babutree/GeoProxy/validator"
)

type failingSubscriptionFile struct {
	path        string
	writeErr    error
	chmodMode   os.FileMode
	closeCalled bool
}

func (f *failingSubscriptionFile) Name() string { return f.path }

func (f *failingSubscriptionFile) Chmod(mode os.FileMode) error {
	f.chmodMode = mode
	return nil
}

func (f *failingSubscriptionFile) WriteString(string) (int, error) {
	return 0, f.writeErr
}

func (f *failingSubscriptionFile) Close() error {
	f.closeCalled = true
	return nil
}

func TestWriteSubscriptionFilePropagatesWriteErrorAndCleansUp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subscription.yaml")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatalf("create test file: %v", err)
	}
	wantErr := errors.New("forced write failure")
	file := &failingSubscriptionFile{path: path, writeErr: wantErr}

	err := writeSubscriptionFile(file, "subscription content")

	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if file.chmodMode != 0644 {
		t.Fatalf("chmod mode = %04o, want 0644", file.chmodMode)
	}
	if !file.closeCalled {
		t.Fatal("file was not closed after write failure")
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("temporary file still exists after write failure: %v", statErr)
	}
}

// TestAPISubscriptionAddRejectsInvalidHeadersJSON 非法 headers JSON 必须 400，且不得入库。
// 使用 file_content 路径，避免依赖外网；customMgr 为 nil 时仍应在 handler 层拒绝。
func TestAPISubscriptionAddRejectsInvalidHeadersJSON(t *testing.T) {
	server := newTestServer(t)
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)

	payload := `{"name":"bad-headers","file_content":"proxies:\n  - name: n\n    type: http\n    server: 1.1.1.1\n    port: 80\n","refresh_min":60,"headers":"{not-json"}`
	req := authenticatedJSONRequest(http.MethodPost, "/api/subscription/add", payload)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	subs, err := server.storage.GetSubscriptions()
	if err != nil {
		t.Fatalf("GetSubscriptions() error = %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("invalid headers added %d subscriptions, want 0", len(subs))
	}
}

// TestAPISubscriptionAddAcceptsValidHeadersWithFileContent 合法 headers 可随 file_content 入库。
func TestAPISubscriptionAddAcceptsValidHeadersWithFileContent(t *testing.T) {
	server := newTestServer(t)
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)

	headers := `{"User-Agent":"clash.meta"}`
	payload := fmt.Sprintf(
		`{"name":"ok-headers","file_content":"proxies:\n  - name: n\n    type: http\n    server: 1.1.1.1\n    port: 80\n","refresh_min":60,"headers":%q}`,
		headers,
	)
	req := authenticatedJSONRequest(http.MethodPost, "/api/subscription/add", payload)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	subs, err := server.storage.GetSubscriptions()
	if err != nil {
		t.Fatalf("GetSubscriptions() error = %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("len(subs) = %d, want 1", len(subs))
	}
	if subs[0].Headers != headers {
		t.Fatalf("headers = %q, want %q", subs[0].Headers, headers)
	}
}

// TestAPISubscriptionAddEmptyHeadersOK 空 headers 合法，入库为空字符串（默认 UA 由拉取层补）。
func TestAPISubscriptionAddEmptyHeadersOK(t *testing.T) {
	server := newTestServer(t)
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)

	payload := `{"name":"empty-headers","file_content":"proxies:\n  - name: n\n    type: http\n    server: 1.1.1.1\n    port: 80\n","refresh_min":60,"headers":""}`
	req := authenticatedJSONRequest(http.MethodPost, "/api/subscription/add", payload)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	subs, err := server.storage.GetSubscriptions()
	if err != nil {
		t.Fatalf("GetSubscriptions() error = %v", err)
	}
	if len(subs) != 1 || subs[0].Headers != "" {
		t.Fatalf("subs=%+v, want one with empty headers", subs)
	}
}

// TestAPISubscriptionDeleteDoesNotPreDeleteProxiesWhenSubscriptionDeleteFails
// 回归：handler 不得在 DeleteSubscription 事务外先 DeleteBySubscriptionID。
// 否则 DeleteSubscription 失败后会留下“空订阅 + 代理已丢”的半删状态。
func TestAPISubscriptionDeleteDoesNotPreDeleteProxiesWhenSubscriptionDeleteFails(t *testing.T) {
	server := newTestServer(t)
	// customMgr 非 nil 才会走旧双删路径中的前置清理分支
	server.customMgr = custom.NewManager(server.storage, validator.New(1, 1, "http://127.0.0.1"), &config.Config{})

	subID, err := server.storage.AddSubscription("sub", "https://example.test/webui-delete.yaml", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	const proxyAddr = "198.51.100.50:8080"
	if err := server.storage.AddProxyWithSource(proxyAddr, "http", storage.SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}

	// 强制订阅行删除失败，模拟 DeleteSubscription 事务中途失败
	if _, err := server.storage.GetDB().Exec(`
		CREATE TRIGGER fail_webui_subscription_delete
		BEFORE DELETE ON subscriptions
		WHEN OLD.id = ` + fmt.Sprint(subID) + `
		BEGIN
			SELECT RAISE(ABORT, 'forced subscription delete failure');
		END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	req := authenticatedJSONRequest(http.MethodPost, "/api/subscription/delete", fmt.Sprintf(`{"id":%d}`, subID))
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if _, err := server.storage.GetSubscription(subID); err != nil {
		t.Fatalf("subscription should remain after failed delete: %v", err)
	}
	if _, err := server.storage.GetProxyByIdentity(proxyAddr, storage.SourceSubscription, subID); err != nil {
		t.Fatalf("proxies must not be pre-deleted outside DeleteSubscription transaction: %v", err)
	}
}

func TestAPISubscriptionDeleteUsesManagerRuntimeSafePath(t *testing.T) {
	server := newTestServer(t)
	mgr := custom.NewManager(server.storage, validator.New(1, 1, "http://127.0.0.1"), &config.Config{
		SingBoxPath:       "missing-sing-box",
		SingBoxBasePort:   10000,
		SingBoxShardCount: 1,
	})
	fake := newSubscriptionDeleteFakeSingBox()
	setManagerSingBoxForSubscriptionTest(t, mgr, fake)
	server.customMgr = mgr

	subID, err := server.storage.AddSubscription("sub", "https://example.test/delete-runtime.yaml", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	node := subscriptionTestTunnelNode("delete-runtime", "delete-runtime.example.com", "password")
	fake.nodes = []custom.ParsedNode{node}
	fake.portMap = map[string]int{node.NodeKey(): 41001}
	addr := "127.0.0.1:41001"
	if err := server.storage.AddProxyWithSource(addr, "socks5", storage.SourceSubscription, subID); err != nil {
		t.Fatalf("AddProxyWithSource() error = %v", err)
	}

	req := authenticatedJSONRequest(http.MethodPost, "/api/subscription/delete", fmt.Sprintf(`{"id":%d}`, subID))
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if fake.reloadCalls != 1 {
		t.Fatalf("runtime Reload calls = %d, want 1 via Manager.DeleteSubscription", fake.reloadCalls)
	}
	for _, n := range fake.nodes {
		if n.NodeKey() == node.NodeKey() {
			t.Fatal("deleted subscription tunnel still in manager runtime")
		}
	}
	if _, err := server.storage.GetSubscription(subID); err == nil {
		t.Fatal("subscription row still present after delete")
	}
}

type subscriptionDeleteFakeSingBox struct {
	reloadCalls int
	nodes       []custom.ParsedNode
	portMap     map[string]int
}

func newSubscriptionDeleteFakeSingBox() *subscriptionDeleteFakeSingBox {
	return &subscriptionDeleteFakeSingBox{portMap: map[string]int{}}
}

func (s *subscriptionDeleteFakeSingBox) Reload(nodes []custom.ParsedNode) error {
	s.reloadCalls++
	s.nodes = append([]custom.ParsedNode(nil), nodes...)
	s.portMap = map[string]int{}
	for i, node := range nodes {
		s.portMap[node.NodeKey()] = 41001 + i
	}
	return nil
}

func (s *subscriptionDeleteFakeSingBox) Stop() {}

func (s *subscriptionDeleteFakeSingBox) GetPortMap() map[string]int {
	out := make(map[string]int, len(s.portMap))
	for key, port := range s.portMap {
		out[key] = port
	}
	return out
}

func (s *subscriptionDeleteFakeSingBox) GetNodes() []custom.ParsedNode {
	return append([]custom.ParsedNode(nil), s.nodes...)
}

func (s *subscriptionDeleteFakeSingBox) GetRuntimeStatus() custom.SingBoxRuntimeStatus {
	return custom.SingBoxRuntimeStatus{Status: custom.SingBoxStatusRunning, Running: len(s.nodes) > 0, Nodes: len(s.nodes), ReadyPorts: len(s.nodes), TotalPorts: len(s.nodes)}
}

func (s *subscriptionDeleteFakeSingBox) GetLocalAddress(nodeKey string) string { return "" }

func setManagerSingBoxForSubscriptionTest(t *testing.T, mgr *custom.Manager, fake *subscriptionDeleteFakeSingBox) {
	t.Helper()
	field := reflect.ValueOf(mgr).Elem().FieldByName("singbox")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(fake))
}

func subscriptionTestTunnelNode(name, server, password string) custom.ParsedNode {
	return custom.ParsedNode{
		Name:   name,
		Type:   "trojan",
		Server: server,
		Port:   443,
		Raw: map[string]interface{}{
			"type":     "trojan",
			"name":     name,
			"server":   server,
			"port":     443,
			"password": password,
			"tls":      true,
			"sni":      server,
		},
	}
}
