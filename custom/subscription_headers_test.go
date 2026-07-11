package custom

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestFetchSubscriptionURLCustomUserAgentOverridesDefault 当订阅 headers 含自定义 UA 时，
// 服务器实际收到的 User-Agent 必须是自定义值（真实 http 往返，不 mock）。
func TestFetchSubscriptionURLCustomUserAgentOverridesDefault(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	m := &Manager{}
	body, err := m.fetchSubscriptionURL(srv.URL, `{"User-Agent":"clash.meta"}`)
	if err != nil {
		t.Fatalf("fetchSubscriptionURL() error = %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("body = %q, want ok", string(body))
	}
	if gotUA != "clash.meta" {
		t.Fatalf("server saw User-Agent = %q, want clash.meta", gotUA)
	}
}

// TestFetchSubscriptionURLEmptyHeadersKeepsDefaultUA 向后兼容：headers 为空时，
// 服务器必须收到默认 v2rayN UA（不许破坏现有订阅拉取）。
func TestFetchSubscriptionURLEmptyHeadersKeepsDefaultUA(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	m := &Manager{}
	if _, err := m.fetchSubscriptionURL(srv.URL, ""); err != nil {
		t.Fatalf("fetchSubscriptionURL() error = %v", err)
	}
	if gotUA != "v2rayN" {
		t.Fatalf("server saw User-Agent = %q, want default v2rayN", gotUA)
	}
}

// TestFetchSubscriptionURLHeadersWithoutUAKeepsDefaultUA 向后兼容边界：headers 非空但不含
// User-Agent 时，自定义头照常发送，同时保留默认 v2rayN UA（自定义头覆盖默认，未指定则保留）。
func TestFetchSubscriptionURLHeadersWithoutUAKeepsDefaultUA(t *testing.T) {
	var gotUA, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	m := &Manager{}
	if _, err := m.fetchSubscriptionURL(srv.URL, `{"Authorization":"Bearer abc"}`); err != nil {
		t.Fatalf("fetchSubscriptionURL() error = %v", err)
	}
	if gotUA != "v2rayN" {
		t.Fatalf("server saw User-Agent = %q, want default v2rayN (UA not overridden)", gotUA)
	}
	if gotAuth != "Bearer abc" {
		t.Fatalf("server saw Authorization = %q, want Bearer abc", gotAuth)
	}
}

// TestFetchSubscriptionURLSendsCustomAuthorization 自定义 Authorization 头被正确发送
// （真实 http 往返验证）。
func TestFetchSubscriptionURLSendsCustomAuthorization(t *testing.T) {
	var gotUA, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()

	m := &Manager{}
	body, err := m.fetchSubscriptionURL(srv.URL, `{"User-Agent":"clash","Authorization":"Bearer xyz"}`)
	if err != nil {
		t.Fatalf("fetchSubscriptionURL() error = %v", err)
	}
	if string(body) != "payload" {
		t.Fatalf("body = %q, want payload", string(body))
	}
	if gotUA != "clash" {
		t.Fatalf("server saw User-Agent = %q, want clash", gotUA)
	}
	if gotAuth != "Bearer xyz" {
		t.Fatalf("server saw Authorization = %q, want Bearer xyz", gotAuth)
	}
}
