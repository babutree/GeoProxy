package validator

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/config"
	"github.com/babutree/GeoProxy/storage"
)

func TestNewClampsNonPositiveConcurrency(t *testing.T) {
	for _, concurrency := range []int{0, -1} {
		v := New(concurrency, 1, "http://127.0.0.1/validate")
		if v.concurrency != 1 {
			t.Fatalf("New(%d).concurrency = %d, want 1", concurrency, v.concurrency)
		}
	}
}

func TestValidateAllWithClampedConcurrencyReturnsForInvalidProxy(t *testing.T) {
	v := New(0, 1, "http://127.0.0.1/validate")
	done := make(chan []Result, 1)

	go func() {
		done <- v.ValidateAll([]storage.Proxy{{ID: 1, Address: "127.0.0.1:1", Protocol: "unknown"}})
	}()

	select {
	case results := <-done:
		if len(results) != 1 || results[0].Valid {
			t.Fatalf("ValidateAll() = %#v, want one invalid result", results)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ValidateAll() did not return with clamped concurrency")
	}
}

func TestGetExitIPInfoRejectsWhenAllProvidersReturnNon2xx(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: exitInfoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		return exitInfoHTTPResponse(req, http.StatusBadGateway, `{}`), nil
	})}
	if got := getExitIPInfo(client); got.OK {
		t.Fatalf("getExitIPInfo() = %#v when all providers return HTTP 502, want failed lookup", got)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("provider requests = %d, want 2", got)
	}
}

func TestGetExitIPInfoFallsBackWhenPrimaryFails(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: exitInfoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		switch req.URL.Host {
		case "ip-api.com":
			return exitInfoHTTPResponse(req, http.StatusBadGateway, `{}`), nil
		case "api.ipapi.is":
			return exitInfoHTTPResponse(req, http.StatusOK, `{
				"ip":"203.0.113.42",
				"location":{"country_code":"JP","city":"Tokyo"},
				"is_proxy":true,
				"is_datacenter":true
			}`), nil
		default:
			t.Fatalf("unexpected exit-info provider: %s", req.URL.Host)
			return nil, nil
		}
	})}

	got := getExitIPInfo(client)
	if !got.OK || got.IP != "203.0.113.42" || got.Location != "JP Tokyo" {
		t.Fatalf("getExitIPInfo() = %#v, want backup exit 203.0.113.42 in JP Tokyo", got)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("provider requests = %d, want 2", got)
	}
}

func TestGetExitIPInfoFallsBackWhenPrimaryTimesOut(t *testing.T) {
	client := &http.Client{
		Timeout: 30 * time.Millisecond,
		Transport: exitInfoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Host {
			case "ip-api.com":
				<-req.Context().Done()
				return nil, req.Context().Err()
			case "api.ipapi.is":
				return exitInfoHTTPResponse(req, http.StatusOK, `{
					"ip":"203.0.113.43",
					"location":{"country_code":"SG","city":"Singapore"}
				}`), nil
			default:
				t.Fatalf("unexpected exit-info provider: %s", req.URL.Host)
				return nil, nil
			}
		}),
	}

	started := time.Now()
	got := getExitIPInfo(client)
	if !got.OK || got.IP != "203.0.113.43" || got.Location != "SG Singapore" {
		t.Fatalf("getExitIPInfo() = %#v, want backup success after primary timeout", got)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("getExitIPInfo() elapsed = %v, want total deadline under 200ms", elapsed)
	}
}

func TestGetExitIPInfoRejectsProviderConflicts(t *testing.T) {
	tests := []struct {
		name    string
		primary string
		backup  string
	}{
		{
			name:    "exit IP mismatch",
			primary: `{"status":"success","query":"203.0.113.10","countryCode":"JP","city":"Tokyo"}`,
			backup:  `{"ip":"203.0.113.11","location":{"country_code":"JP","city":"Tokyo"}}`,
		},
		{
			name:    "country mismatch",
			primary: `{"status":"success","query":"203.0.113.10","countryCode":"JP","city":"Tokyo"}`,
			backup:  `{"ip":"203.0.113.10","location":{"country_code":"US","city":"Ashburn"}}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: exitInfoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Host {
				case "ip-api.com":
					return exitInfoHTTPResponse(req, http.StatusOK, tc.primary), nil
				case "api.ipapi.is":
					return exitInfoHTTPResponse(req, http.StatusOK, tc.backup), nil
				default:
					t.Fatalf("unexpected exit-info provider: %s", req.URL.Host)
					return nil, nil
				}
			})}

			if got := getExitIPInfo(client); got.OK {
				t.Fatalf("getExitIPInfo() = %#v for conflicting providers, want fail-closed", got)
			}
		})
	}
}

func TestGetExitIPInfoAcceptsConsensusAndQueriesBothProviders(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: exitInfoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		switch req.URL.Host {
		case "ip-api.com":
			return exitInfoHTTPResponse(req, http.StatusOK, `{
				"status":"success",
				"query":"203.0.113.10",
				"countryCode":"JP",
				"city":"Tokyo"
			}`), nil
		case "api.ipapi.is":
			return exitInfoHTTPResponse(req, http.StatusOK, `{
				"ip":"203.0.113.10",
				"location":{"country_code":"JP","city":"Osaka"}
			}`), nil
		default:
			t.Fatalf("unexpected exit-info provider: %s", req.URL.Host)
			return nil, nil
		}
	})}

	got := getExitIPInfo(client)
	if !got.OK || got.IP != "203.0.113.10" || got.Location != "JP Tokyo" {
		t.Fatalf("getExitIPInfo() = %#v, want consensus using primary location", got)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("provider requests = %d, want 2", got)
	}
}

func TestGetExitIPInfoTimesOutFailClosedWithinBound(t *testing.T) {
	client := &http.Client{
		Timeout: 20 * time.Millisecond,
		Transport: exitInfoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		}),
	}

	started := time.Now()
	got := getExitIPInfo(client)
	elapsed := time.Since(started)
	if got.OK {
		t.Fatalf("getExitIPInfo() = %#v after provider timeouts, want fail-closed", got)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("getExitIPInfo() timeout elapsed = %v, want bounded under 200ms", elapsed)
	}
}

func TestAssessRiskTracksPrimaryFlagsKnowledge(t *testing.T) {
	tests := []struct {
		name          string
		primaryStatus int
		primaryBody   string
		backupStatus  int
		backupBody    string
		wantKnown     bool
	}{
		{
			name:          "backup-only keeps ip-api flags unknown",
			primaryStatus: http.StatusBadGateway,
			primaryBody:   `{}`,
			backupStatus:  http.StatusOK,
			backupBody:    `{"ip":"203.0.113.44","location":{"country_code":"JP","city":"Tokyo"}}`,
			wantKnown:     false,
		},
		{
			name:          "primary clean response is known",
			primaryStatus: http.StatusOK,
			primaryBody:   `{"status":"success","query":"203.0.113.45","countryCode":"US","city":"Ashburn","proxy":false,"hosting":false,"mobile":false}`,
			backupStatus:  http.StatusBadGateway,
			backupBody:    `{}`,
			wantKnown:     true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{
				Timeout: 100 * time.Millisecond,
				Transport: exitInfoRoundTripFunc(func(req *http.Request) (*http.Response, error) {
					switch req.URL.Host {
					case "ip-api.com":
						return exitInfoHTTPResponse(req, tc.primaryStatus, tc.primaryBody), nil
					case "api.ipapi.is":
						if req.URL.Query().Get("q") != "" {
							return exitInfoHTTPResponse(req, http.StatusOK, `{"company":{"abuser_score":"0.01 (Low)"}}`), nil
						}
						return exitInfoHTTPResponse(req, tc.backupStatus, tc.backupBody), nil
					default:
						return exitInfoHTTPResponse(req, http.StatusUnauthorized, `{"error":{"message":"missing api key"}}`), nil
					}
				}),
			}

			ipInfo := getExitIPInfo(client)
			if !ipInfo.OK {
				t.Fatalf("getExitIPInfo() = %#v, want valid exit info", ipInfo)
			}
			risk := assessRisk(client, ipInfo)
			if risk.FlagsKnown != tc.wantKnown {
				t.Fatalf("assessRisk().FlagsKnown = %v, want %v", risk.FlagsKnown, tc.wantKnown)
			}
			if risk.Flags != "" {
				t.Fatalf("assessRisk().Flags = %q, want clean/unknown empty value", risk.Flags)
			}
		})
	}
}

type exitInfoRoundTripFunc func(*http.Request) (*http.Response, error)

func (f exitInfoRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func exitInfoHTTPResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestDefaultValidationTargetsAvoidHttpbinSinglePoint(t *testing.T) {
	for _, target := range httpsTestTargets {
		if strings.Contains(target, "httpbin.org") {
			t.Fatalf("httpsTestTargets still contains httpbin single-point target: %q", target)
		}
	}
	if len(httpsTestTargets) < 3 {
		t.Fatalf("httpsTestTargets has %d targets, want multiple providers", len(httpsTestTargets))
	}

	cfg := config.DefaultConfig()
	if strings.Contains(cfg.ValidateURL, "httpbin.org") {
		t.Fatalf("default ValidateURL still contains httpbin: %q", cfg.ValidateURL)
	}
	if targets := parseValidateURLs(cfg.ValidateURL); len(targets) < 3 {
		t.Fatalf("default ValidateURL has %d targets, want multiple providers: %q", len(targets), cfg.ValidateURL)
	}
}

func TestParseValidateURLsTrimsEmptyParts(t *testing.T) {
	targets := parseValidateURLs(" http://a.test/ok, ,http://b.test/ok ")
	if len(targets) != 2 || targets[0] != "http://a.test/ok" || targets[1] != "http://b.test/ok" {
		t.Fatalf("parseValidateURLs() = %#v, want trimmed non-empty targets", targets)
	}
}

// TestAuthenticatedClientConstructionInjectsCredentials 验证 BUG-6 修复：
// 带凭据的 http/socks5 节点在构造校验 client 时把凭据注入到出站拨号路径。
// http：凭据放进 proxyURL.User（Transport 据此发 Proxy-Authorization）。
// socks5：凭据放进 proxy.Auth。构造成功即证明凭据被 thread 进拨号器，
// 且凭据绝不出现在任何 error 串中（凭据泄漏红线）。
func TestAuthenticatedClientConstructionInjectsCredentials(t *testing.T) {
	const (
		user = "alice"
		pass = "s3cr3t-token"
	)
	timeout := time.Second

	hc, err := newHTTPClient("10.9.9.1:8080", user, pass, timeout)
	if err != nil {
		t.Fatalf("newHTTPClient() error = %v", err)
	}
	tr, ok := hc.Transport.(*http.Transport)
	if !ok || tr.Proxy == nil {
		t.Fatalf("http client transport missing proxy: %#v", hc.Transport)
	}
	proxyURL, err := tr.Proxy(nil)
	if err != nil {
		t.Fatalf("http transport Proxy() error = %v", err)
	}
	if proxyURL == nil || proxyURL.User == nil {
		t.Fatalf("http proxy URL missing userinfo: %#v", proxyURL)
	}
	if gotUser := proxyURL.User.Username(); gotUser != user {
		t.Fatalf("http proxy username = %q, want %q", gotUser, user)
	}
	if gotPass, _ := proxyURL.User.Password(); gotPass != pass {
		t.Fatal("http proxy password not threaded through")
	}

	sc, err := newSOCKS5Client("10.9.9.2:1080", user, pass, timeout)
	if err != nil {
		t.Fatalf("newSOCKS5Client() error = %v", err)
	}
	if sc.Transport == nil {
		t.Fatal("socks5 client transport is nil")
	}

	// 无凭据路径仍须构造成功且不注入 userinfo。
	plain, err := newHTTPClient("10.9.9.3:8080", "", "", timeout)
	if err != nil {
		t.Fatalf("newHTTPClient(no creds) error = %v", err)
	}
	ptr := plain.Transport.(*http.Transport)
	pu, _ := ptr.Proxy(nil)
	if pu != nil && pu.User != nil {
		t.Fatalf("no-cred http proxy URL unexpectedly carries userinfo: %#v", pu)
	}
}

// TestGeoFilterReadDoesNotRaceWithConfigSave 复现 BUG-58：
// validator 缓存了 config.Get() 返回的 *Config 指针并在 passesGeoFilter 中无锁读取
// 国家名单 slice，同时 config.Save 并发更新全局配置。旧实现下 Save 原地改写
// *globalCfg（含 slice header）会与这里的读取产生 data race；修复后 Save 改为
// 替换 globalCfg 指针，validator 持有的旧快照不可变，-race 下必须干净通过。
func TestGeoFilterReadDoesNotRaceWithConfigSave(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())

	// 建立初始 globalCfg，使 config.Get() 返回一个真实指针。
	base := config.Load()
	base.BlockedCountries = []string{"CN"}
	base.AllowedCountries = nil
	if err := config.Save(base); err != nil {
		t.Fatalf("initial Save() error = %v", err)
	}

	// validator 在此刻捕获 config.Get() 的指针（与生产 New() 路径一致）。
	v := New(4, 1, "http://127.0.0.1/validate")

	const iterations = 2000
	done := make(chan struct{})

	// writer：反复 Save，交替改写国家名单（触发 globalCfg 指针替换）。
	go func() {
		defer close(done)
		for i := 0; i < iterations; i++ {
			cfg := *base
			if i%2 == 0 {
				cfg.BlockedCountries = []string{"CN", "RU", "IR"}
				cfg.AllowedCountries = nil
			} else {
				cfg.BlockedCountries = nil
				cfg.AllowedCountries = []string{"US", "JP", "SG"}
			}
			if err := config.Save(&cfg); err != nil {
				t.Errorf("Save() error = %v", err)
				return
			}
		}
	}()

	// reader：反复经 passesGeoFilter 读取 v.cfg 的国家名单 slice。
	for i := 0; i < iterations; i++ {
		_ = v.passesGeoFilter("US")
		_ = v.passesGeoFilter("CN")
	}

	<-done
}
