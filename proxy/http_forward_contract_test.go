package proxy

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/auth"
)

func TestHTTPUpstreamBodyReadErrorRecordsFailureAndReleasesSession(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "16")
		w.Header().Set("Connection", "close")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "short")
	}))
	t.Cleanup(upstream.Close)

	store := newProxyTestStore()
	address := upstreamAddr(t, upstream.URL)
	addProxy(t, store, address, "http", 1)
	server := newProxyTestServer(store, proxyTestConfig(0))
	route := auth.ParsedUsername{Session: "http-truncated-body"}
	server.sessions.SetProxy(route.Session, 1, address, "")

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://origin.example/truncated", nil)
	server.handleHTTP(recorder, req, route)

	got, err := store.GetProxyByID(1)
	if err != nil {
		t.Fatalf("GetProxyByID() error = %v", err)
	}
	if got.SuccessCount != 0 || got.FailCount != 1 || got.UseCount != 1 {
		t.Fatalf("truncated response accounting = use:%d success:%d fail:%d, want 1/0/1", got.UseCount, got.SuccessCount, got.FailCount)
	}
	if binding, ok := server.sessions.Get(route.Session); ok && binding.ProxyID == got.ID {
		t.Fatalf("truncated upstream response kept failed sticky binding: %#v", binding)
	}
}

func TestHTTPClientWriteErrorKeepsNodeHealthNeutral(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "complete response")
	}))
	t.Cleanup(upstream.Close)

	store := newProxyTestStore()
	address := upstreamAddr(t, upstream.URL)
	addProxy(t, store, address, "http", 1)
	setProxyFailCountForHTTPContract(t, store, 1, 2)
	server := newProxyTestServer(store, proxyTestConfig(0))
	w := &writeFailingResponseWriter{header: make(http.Header), err: errors.New("client disconnected")}
	req := httptest.NewRequest(http.MethodGet, "http://origin.example/client-write-error", nil)

	server.handleHTTP(w, req, emptyRoute())

	got, err := store.GetProxyByID(1)
	if err != nil {
		t.Fatalf("GetProxyByID() error = %v", err)
	}
	if got.UseCount != 0 || got.SuccessCount != 0 || got.FailCount != 2 {
		t.Fatalf("client write error changed node health = use:%d success:%d fail:%d, want 0/0/2", got.UseCount, got.SuccessCount, got.FailCount)
	}
	if w.writeCalls == 0 {
		t.Fatal("test writer was never exercised")
	}
}

func TestHTTPInboundCancellationKeepsNodeHealthNeutral(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		upstream.Close()
	})

	store := newProxyTestStore()
	address := upstreamAddr(t, upstream.URL)
	addProxy(t, store, address, "http", 1)
	setProxyFailCountForHTTPContract(t, store, 1, 2)
	cfg := proxyTestConfig(0)
	cfg.ValidateTimeout = 2
	server := newProxyTestServer(store, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "http://origin.example/canceled", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		server.handleHTTP(httptest.NewRecorder(), req, emptyRoute())
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("upstream request did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("inbound cancellation was not propagated to the outbound request")
	}

	got, err := store.GetProxyByID(1)
	if err != nil {
		t.Fatalf("GetProxyByID() error = %v", err)
	}
	if got.UseCount != 0 || got.SuccessCount != 0 || got.FailCount != 2 {
		t.Fatalf("inbound cancellation changed node health = use:%d success:%d fail:%d, want 0/0/2", got.UseCount, got.SuccessCount, got.FailCount)
	}
}

func TestHTTPCompleteResponseRecordsTransportSuccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "complete response")
	}))
	t.Cleanup(upstream.Close)

	store := newProxyTestStore()
	address := upstreamAddr(t, upstream.URL)
	addProxy(t, store, address, "http", 1)
	setProxyFailCountForHTTPContract(t, store, 1, 2)
	server := newProxyTestServer(store, proxyTestConfig(0))
	recorder := httptest.NewRecorder()
	server.handleHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://origin.example/complete", nil), emptyRoute())

	got, err := store.GetProxyByID(1)
	if err != nil {
		t.Fatalf("GetProxyByID() error = %v", err)
	}
	if got.UseCount != 1 || got.SuccessCount != 1 || got.FailCount != 0 {
		t.Fatalf("complete accounting = %d/%d/%d, want 1/1/0", got.UseCount, got.SuccessCount, got.FailCount)
	}
	if recorder.Code != http.StatusOK || recorder.Body.String() != "complete response" {
		t.Fatalf("response = %d/%q, want 200/complete response", recorder.Code, recorder.Body.String())
	}
}

func TestHTTPProxy407RetriesRecordsFailureAndRebindsSession(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Proxy-Authenticate", `Basic realm="upstream"`)
		w.Header().Set("X-First-Proxy", "must-not-leak")
		http.Error(w, "proxy auth", http.StatusProxyAuthRequired)
	}))
	t.Cleanup(bad.Close)
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Good-Proxy", "selected")
		_, _ = io.WriteString(w, "retried")
	}))
	t.Cleanup(good.Close)

	store := newProxyTestStore()
	badAddr, goodAddr := upstreamAddr(t, bad.URL), upstreamAddr(t, good.URL)
	addProxy(t, store, badAddr, "http", 1)
	addProxy(t, store, goodAddr, "http", 2)
	server := newProxyTestServer(store, proxyTestConfig(1))
	route := auth.ParsedUsername{Session: "http-proxy-407"}
	server.sessions.SetProxy(route.Session, 1, badAddr, "")
	recorder := httptest.NewRecorder()
	server.handleHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://origin.example/proxy-auth", nil), route)

	failed, err := store.GetProxyByID(1)
	if err != nil {
		t.Fatalf("GetProxyByID(failed) error = %v", err)
	}
	working, err := store.GetProxyByID(2)
	if err != nil {
		t.Fatalf("GetProxyByID(working) error = %v", err)
	}
	if failed.UseCount != 1 || failed.SuccessCount != 0 || failed.FailCount != 1 {
		t.Fatalf("407 accounting = %d/%d/%d, want 1/0/1", failed.UseCount, failed.SuccessCount, failed.FailCount)
	}
	if working.UseCount != 1 || working.SuccessCount != 1 || working.FailCount != 0 {
		t.Fatalf("retry accounting = %d/%d/%d, want 1/1/0", working.UseCount, working.SuccessCount, working.FailCount)
	}
	if recorder.Code != http.StatusOK || recorder.Body.String() != "retried" {
		t.Fatalf("retry response = %d/%q, want 200/retried", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Proxy-Authenticate"); got != "" {
		t.Fatalf("first proxy authentication header leaked to retry response: %q", got)
	}
	if got := recorder.Header().Get("X-First-Proxy"); got != "" {
		t.Fatalf("first proxy header leaked to retry response: %q", got)
	}
	if got := recorder.Header().Get("X-Good-Proxy"); got != "selected" {
		t.Fatalf("retry response lost selected proxy header: %q", got)
	}
	if binding, ok := server.sessions.Get(route.Session); !ok || binding.ProxyID != working.ID {
		t.Fatalf("binding = %#v ok=%v, want proxy %d", binding, ok, working.ID)
	}
}

func TestHTTPProxy407WithNonReplayableBodyDoesNotTrySecondNode(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "proxy auth", http.StatusProxyAuthRequired)
	}))
	t.Cleanup(bad.Close)
	var secondAttempts atomic.Int32
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondAttempts.Add(1)
		_, _ = io.WriteString(w, "must-not-run")
	}))
	t.Cleanup(good.Close)

	store := newProxyTestStore()
	badAddr, goodAddr := upstreamAddr(t, bad.URL), upstreamAddr(t, good.URL)
	addProxy(t, store, badAddr, "http", 1)
	addProxy(t, store, goodAddr, "http", 2)
	server := newProxyTestServer(store, proxyTestConfig(1))
	body := strings.Repeat("x", maxReplayBodyBytes+1)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "http://origin.example/non-replayable", strings.NewReader(body))

	server.handleHTTP(recorder, request, emptyRoute())

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("response status=%d body=%q, want 502", recorder.Code, recorder.Body.String())
	}
	if got := secondAttempts.Load(); got != 0 {
		t.Fatalf("second node attempts=%d, want 0 for non-replayable body", got)
	}
	failed, err := store.GetProxyByID(1)
	if err != nil {
		t.Fatalf("GetProxyByID(failed) error = %v", err)
	}
	if failed.UseCount != 1 || failed.SuccessCount != 0 || failed.FailCount != 1 {
		t.Fatalf("407 accounting = %d/%d/%d, want 1/0/1", failed.UseCount, failed.SuccessCount, failed.FailCount)
	}
}

func TestSOCKS5UpstreamOrigin407RemainsTransportSuccess(t *testing.T) {
	upstream := startTrackedProxy(t, func(conn net.Conn, reader *bufio.Reader, requestSeen chan<- struct{}, clientClosed chan<- struct{}) error {
		if err := completeTrackedSOCKS5Handshake(conn, reader, "", ""); err != nil {
			return err
		}
		request, err := http.ReadRequest(reader)
		if err != nil {
			return err
		}
		close(requestSeen)
		defer request.Body.Close()
		if _, err := io.Copy(io.Discard, request.Body); err != nil {
			return err
		}
		if _, err := io.WriteString(conn, "HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: 6\r\nConnection: close\r\n\r\norigin"); err != nil {
			return err
		}
		close(clientClosed)
		return nil
	})

	store := newProxyTestStore()
	addProxy(t, store, upstream.address, "socks5", 1)
	setProxyFailCountForHTTPContract(t, store, 1, 2)
	server := newProxyTestServer(store, proxyTestConfig(0))
	recorder := httptest.NewRecorder()

	server.handleHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://origin.example/source-407", nil), emptyRoute())

	if recorder.Code != http.StatusProxyAuthRequired || recorder.Body.String() != "origin" {
		t.Fatalf("response=%d/%q, want 407/origin", recorder.Code, recorder.Body.String())
	}
	got, err := store.GetProxyByID(1)
	if err != nil {
		t.Fatalf("GetProxyByID() error = %v", err)
	}
	if got.UseCount != 1 || got.SuccessCount != 1 || got.FailCount != 0 {
		t.Fatalf("SOCKS5 origin 407 accounting=%d/%d/%d, want 1/1/0", got.UseCount, got.SuccessCount, got.FailCount)
	}
}

func TestHTTPTruncatedResponseDoesNotRetryAfterHeadersCommit(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "16")
		w.Header().Set("Connection", "close")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "short")
	}))
	t.Cleanup(bad.Close)
	var secondAttempts atomic.Int32
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondAttempts.Add(1)
		_, _ = io.WriteString(w, "must-not-run")
	}))
	t.Cleanup(good.Close)

	store := newProxyTestStore()
	badAddr, goodAddr := upstreamAddr(t, bad.URL), upstreamAddr(t, good.URL)
	addProxy(t, store, badAddr, "http", 1)
	addProxy(t, store, goodAddr, "http", 2)
	server := newProxyTestServer(store, proxyTestConfig(1))
	recorder := httptest.NewRecorder()

	server.handleHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://origin.example/truncated-retry", nil), emptyRoute())

	if recorder.Code != http.StatusOK || recorder.Body.String() != "short" {
		t.Fatalf("partial response=%d/%q, want 200/short", recorder.Code, recorder.Body.String())
	}
	if got := secondAttempts.Load(); got != 0 {
		t.Fatalf("second node attempts=%d, want 0 after response headers commit", got)
	}
	failed, err := store.GetProxyByID(1)
	if err != nil {
		t.Fatalf("GetProxyByID(failed) error = %v", err)
	}
	if failed.UseCount != 1 || failed.SuccessCount != 0 || failed.FailCount != 1 {
		t.Fatalf("truncated accounting = %d/%d/%d, want 1/0/1", failed.UseCount, failed.SuccessCount, failed.FailCount)
	}
}

func TestHTTPOrigin429And503RemainTransportSuccess(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, http.StatusText(status), status)
			}))
			t.Cleanup(upstream.Close)

			store := newProxyTestStore()
			address := upstreamAddr(t, upstream.URL)
			addProxy(t, store, address, "http", 1)
			setProxyFailCountForHTTPContract(t, store, 1, 2)
			server := newProxyTestServer(store, proxyTestConfig(0))
			recorder := httptest.NewRecorder()
			server.handleHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://origin.example/status", nil), emptyRoute())

			got, err := store.GetProxyByID(1)
			if err != nil {
				t.Fatalf("GetProxyByID() error = %v", err)
			}
			if recorder.Code != status {
				t.Fatalf("status = %d, want %d", recorder.Code, status)
			}
			if got.UseCount != 1 || got.SuccessCount != 1 || got.FailCount != 0 {
				t.Fatalf("accounting = %d/%d/%d, want 1/1/0", got.UseCount, got.SuccessCount, got.FailCount)
			}
		})
	}
}

func TestHTTPUppercaseSchemeIsCanonicalizedBeforeForwarding(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "canonical")
	}))
	t.Cleanup(upstream.Close)

	store := newProxyTestStore()
	address := upstreamAddr(t, upstream.URL)
	addProxy(t, store, address, "http", 1)
	server := newProxyTestServer(store, proxyTestConfig(0))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://origin.example/canonical", nil)
	request.URL.Scheme = "HTTP"

	server.handleHTTP(recorder, request, emptyRoute())

	if recorder.Code != http.StatusOK || recorder.Body.String() != "canonical" {
		t.Fatalf("response=%d/%q, want 200/canonical", recorder.Code, recorder.Body.String())
	}
	got, err := store.GetProxyByID(1)
	if err != nil {
		t.Fatalf("GetProxyByID() error = %v", err)
	}
	if got.UseCount != 1 || got.SuccessCount != 1 || got.FailCount != 0 {
		t.Fatalf("canonical scheme accounting=%d/%d/%d, want 1/1/0", got.UseCount, got.SuccessCount, got.FailCount)
	}
}

func TestHTTPDirectTruncatedResponseDoesNotLogCompletion(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "16")
		w.Header().Set("Connection", "close")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "short")
	}))
	t.Cleanup(upstream.Close)

	server := newProxyTestServer(newProxyTestStore(), proxyTestConfig(0))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, upstream.URL, nil)
	logs := captureHTTPContractLogs(t, func() {
		server.httpDirect(recorder, request, nil, nil, true)
	})

	if recorder.Code != http.StatusOK || recorder.Body.String() != "short" {
		t.Fatalf("direct partial response = %d/%q, want 200/short", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(logs, "直连请求完成") {
		t.Fatalf("truncated direct response was logged as complete: %s", logs)
	}
	if !strings.Contains(logs, "直连上游响应体读取失败") {
		t.Fatalf("truncated direct response did not log upstream read failure: %s", logs)
	}
}

func TestCopyHTTPResponseSeparatesReaderAndWriterErrors(t *testing.T) {
	t.Run("data plus EOF is complete", func(t *testing.T) {
		reader := &singleResponseRead{data: []byte("body"), err: io.EOF}
		var writer strings.Builder
		bytesCopied, readErr, writeErr := copyHTTPResponse(&writer, reader)
		if bytesCopied != 4 || readErr != nil || writeErr != nil || writer.String() != "body" {
			t.Fatalf("copy = bytes:%d read:%v write:%v body:%q", bytesCopied, readErr, writeErr, writer.String())
		}
	})
	t.Run("data plus unexpected EOF is upstream failure", func(t *testing.T) {
		reader := &singleResponseRead{data: []byte("body"), err: io.ErrUnexpectedEOF}
		var writer strings.Builder
		bytesCopied, readErr, writeErr := copyHTTPResponse(&writer, reader)
		if bytesCopied != 4 || !errors.Is(readErr, io.ErrUnexpectedEOF) || writeErr != nil || writer.String() != "body" {
			t.Fatalf("copy = bytes:%d read:%v write:%v body:%q", bytesCopied, readErr, writeErr, writer.String())
		}
	})
	t.Run("short write without error is client failure", func(t *testing.T) {
		writer := &shortResponseWriter{}
		bytesCopied, readErr, writeErr := copyHTTPResponse(writer, strings.NewReader("body"))
		if bytesCopied != 3 || readErr != nil || !errors.Is(writeErr, io.ErrShortWrite) || writer.String() != "bod" {
			t.Fatalf("copy = bytes:%d read:%v write:%v body:%q", bytesCopied, readErr, writeErr, writer.String())
		}
	})
	t.Run("write error is client failure", func(t *testing.T) {
		want := errors.New("client disconnected")
		writer := &writeFailingResponseWriter{err: want}
		bytesCopied, readErr, writeErr := copyHTTPResponse(writer, strings.NewReader("body"))
		if bytesCopied != 0 || readErr != nil || !errors.Is(writeErr, want) {
			t.Fatalf("copy = bytes:%d read:%v write:%v", bytesCopied, readErr, writeErr)
		}
	})
}

func TestHTTPInvalidForwardRequestFailsBeforeSelection(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*http.Request)
	}{
		{name: "invalid method", mutate: func(r *http.Request) { r.Method = "bad method" }},
		{name: "invalid URL", mutate: func(r *http.Request) {
			r.URL = &url.URL{Scheme: "http", Host: "[::1", Path: "/"}
		}},
		{name: "relative URL", mutate: func(r *http.Request) {
			r.URL = &url.URL{Path: "/relative"}
		}},
		{name: "unsupported scheme", mutate: func(r *http.Request) {
			r.URL = &url.URL{Scheme: "ftp", Host: "origin.example", Path: "/unsupported"}
		}},
		{name: "empty hostname", mutate: func(r *http.Request) {
			r.URL = &url.URL{Scheme: "http", Host: ":80", Path: "/missing-host"}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newProxyTestStore()
			addProxy(t, store, "127.0.0.1:9", "http", 1)
			server := newProxyTestServer(store, proxyTestConfig(2))
			route := auth.ParsedUsername{Session: "invalid-forward-" + strings.ReplaceAll(tt.name, " ", "-")}
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "http://origin.example/invalid", nil)
			tt.mutate(req)

			server.handleHTTP(recorder, req, route)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%q, want 400", recorder.Code, recorder.Body.String())
			}
			got, err := store.GetProxyByID(1)
			if err != nil {
				t.Fatalf("GetProxyByID() error = %v", err)
			}
			if got.UseCount != 0 || got.SuccessCount != 0 || got.FailCount != 0 {
				t.Fatalf("invalid request changed health: %#v", got)
			}
			if binding, ok := server.sessions.Get(route.Session); ok {
				t.Fatalf("invalid request created binding: %#v", binding)
			}
		})
	}
}

type writeFailingResponseWriter struct {
	header     http.Header
	status     int
	err        error
	writeCalls int
}

func (w *writeFailingResponseWriter) Header() http.Header { return w.header }

func (w *writeFailingResponseWriter) WriteHeader(status int) { w.status = status }

func (w *writeFailingResponseWriter) Write([]byte) (int, error) {
	w.writeCalls++
	return 0, w.err
}

type singleResponseRead struct {
	data []byte
	err  error
	done bool
}

func (r *singleResponseRead) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	r.done = true
	return copy(p, r.data), r.err
}

type shortResponseWriter struct {
	strings.Builder
}

func (w *shortResponseWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	n := len(p) - 1
	_, _ = w.Builder.Write(p[:n])
	return n, nil
}

var httpContractLogMu sync.Mutex

func captureHTTPContractLogs(t *testing.T, action func()) string {
	t.Helper()
	httpContractLogMu.Lock()
	defer httpContractLogMu.Unlock()
	var logs strings.Builder
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	oldPrefix := log.Prefix()
	log.SetOutput(&logs)
	log.SetFlags(0)
	log.SetPrefix("")
	defer func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
		log.SetPrefix(oldPrefix)
	}()
	action()
	return logs.String()
}

func setProxyFailCountForHTTPContract(t *testing.T, store *fakeProxyStore, id int64, failCount int) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	proxy, ok := store.proxies[id]
	if !ok {
		t.Fatalf("proxy id %d not found", id)
	}
	proxy.FailCount = failCount
	store.proxies[id] = proxy
}
