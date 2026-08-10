package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/auth"
	"github.com/babutree/GeoProxy/storage"
)

func TestHTTPConnectRejectsMalformedTargetBeforeSelection(t *testing.T) {
	store := newProxyTestStore()
	addProxy(t, store, reserveClosedAddr(t), "http", 1)
	server := newProxyTestServer(store, proxyTestConfig(0))
	route := auth.ParsedUsername{Session: "invalid-connect-target"}
	request := httptest.NewRequest(http.MethodConnect, "http://invalid.example", nil)
	request.Host = "invalid target"
	recorder := httptest.NewRecorder()

	server.handleTunnel(recorder, request, route)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%q, want 400", recorder.Code, recorder.Body.String())
	}
	proxy, err := store.GetProxyByID(1)
	if err != nil {
		t.Fatalf("GetProxyByID(): %v", err)
	}
	if proxy.UseCount != 0 || proxy.FailCount != 0 {
		t.Fatalf("malformed CONNECT changed node health: %#v", proxy)
	}
	if binding, ok := server.sessions.Get(route.Session); ok {
		t.Fatalf("malformed CONNECT created session binding: %#v", binding)
	}
}

func TestReadSOCKS5AddressRejectsDomainControlCharacters(t *testing.T) {
	domain := []byte("example.test\r\nX-Injected: yes")
	input := append([]byte{byte(len(domain))}, domain...)
	if host, err := readSOCKS5Address(bytes.NewReader(input), 0x03); err == nil {
		t.Fatalf("readSOCKS5Address() accepted injectable domain %q", host)
	}
}

func TestReadSOCKS5RequestRejectsZeroPortBeforeSelection(t *testing.T) {
	domain := []byte("example.test")
	request := []byte{0x05, 0x01, 0x00, 0x03, byte(len(domain))}
	request = append(request, domain...)
	request = append(request, 0x00, 0x00)

	server := NewSOCKS5(nil, proxyTestConfig(0), ":0")
	if target, err := server.readSOCKS5Request(&bufferConn{Reader: bytes.NewReader(request)}); err == nil {
		t.Fatalf("readSOCKS5Request() accepted zero-port target %q", target)
	}
}

func TestSOCKSHTTPTransportUsesDialContext(t *testing.T) {
	server := New(nil, proxyTestConfig(0), ":0")
	client, err := server.buildClient(&storage.Proxy{Address: reserveClosedAddr(t), Protocol: "socks5"})
	if err != nil {
		t.Fatalf("buildClient(): %v", err)
	}
	transport := client.Transport.(*http.Transport)
	if transport.DialContext == nil {
		t.Fatal("SOCKS transport has no DialContext; request cancellation cannot reach dialing")
	}
	if transport.Dial != nil {
		t.Fatal("SOCKS transport still uses context-blind Dial")
	}
}

func TestHTTPProxyClientReturnsRedirectWithoutFollowing(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/next", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)

	server := New(nil, proxyTestConfig(0), ":0")
	client, err := server.buildClient(&storage.Proxy{Address: upstreamAddr(t, upstream.URL), Protocol: "http"})
	if err != nil {
		t.Fatalf("buildClient(): %v", err)
	}
	defer client.CloseIdleConnections()
	response, err := client.Get("http://origin.example/start")
	if err != nil {
		t.Fatalf("client.Get(): %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want original 302", response.StatusCode)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("proxy requests = %d, want 1 without gateway-side redirect follow", got)
	}
}

func TestHTTPForwardingStripsResponseHopByHopHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Proxy-Authenticate", "Basic upstream")
		w.Header().Set("Keep-Alive", "timeout=5")
		w.Header().Set("X-End-To-End", "keep-me")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)

	store := newProxyTestStore()
	addProxy(t, store, upstreamAddr(t, upstream.URL), "http", 1)
	server := newProxyTestServer(store, proxyTestConfig(0))
	recorder := httptest.NewRecorder()
	server.handleHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://origin.example/headers", nil), emptyRoute())

	for _, name := range []string{"Connection", "Proxy-Authenticate", "Keep-Alive"} {
		if got := recorder.Header().Get(name); got != "" {
			t.Fatalf("response header %s = %q, want stripped", name, got)
		}
	}
	if got := recorder.Header().Get("X-End-To-End"); got != "keep-me" {
		t.Fatalf("X-End-To-End = %q, want keep-me", got)
	}
}

func TestCleanResponseHeadersRemovesConnectionNominatedHeaders(t *testing.T) {
	header := http.Header{
		"Connection":       {"X-Hop, close"},
		"X-Hop":            {"remove-me"},
		"Proxy-Connection": {"remove-me"},
		"X-End-To-End":     {"keep-me"},
	}
	cleanResponseHeaders(header)

	for _, name := range []string{"Connection", "X-Hop", "Proxy-Connection"} {
		if got := header.Get(name); got != "" {
			t.Fatalf("header %s = %q, want stripped", name, got)
		}
	}
	if got := header.Get("X-End-To-End"); got != "keep-me" {
		t.Fatalf("X-End-To-End = %q, want keep-me", got)
	}
}

func TestHTTPOverCapBodyPreservesKnownContentLength(t *testing.T) {
	body := bytes.Repeat([]byte{'x'}, maxReplayBodyBytes+1)
	contentLength := make(chan int64, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentLength <- r.ContentLength
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)

	store := newProxyTestStore()
	addProxy(t, store, upstreamAddr(t, upstream.URL), "http", 1)
	server := newProxyTestServer(store, proxyTestConfig(0))
	request := httptest.NewRequest(http.MethodPost, "http://origin.example/upload", bytes.NewReader(body))
	server.handleHTTP(httptest.NewRecorder(), request, emptyRoute())

	if got := <-contentLength; got != int64(len(body)) {
		t.Fatalf("upstream ContentLength = %d, want %d", got, len(body))
	}
}

func TestHTTPBodyPreReadSetsAndClearsReadDeadline(t *testing.T) {
	cfg := proxyTestConfig(0)
	cfg.ValidateTimeout = 3
	server := newProxyTestServer(newProxyTestStore(), cfg)
	writer := &readDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	request := httptest.NewRequest(http.MethodPost, "http://origin.example/body", strings.NewReader("body"))
	server.handleHTTP(writer, request, emptyRoute())

	if len(writer.deadlines) < 2 {
		t.Fatalf("read deadlines = %#v, want bounded pre-read followed by clear", writer.deadlines)
	}
	if writer.deadlines[0].IsZero() {
		t.Fatal("first read deadline is zero")
	}
	if last := writer.deadlines[len(writer.deadlines)-1]; !last.IsZero() {
		t.Fatalf("last read deadline = %v, want cleared zero deadline", last)
	}
}

type readDeadlineRecorder struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
}

func (w *readDeadlineRecorder) SetReadDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return nil
}

func TestHTTPDirectDoesNotUseEnvironmentProxy(t *testing.T) {
	var proxyRequests atomic.Int32
	environmentProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxyRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(environmentProxy.Close)
	t.Setenv("HTTP_PROXY", environmentProxy.URL)
	t.Setenv("http_proxy", environmentProxy.URL)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	cfg := proxyTestConfig(0)
	cfg.ValidateTimeout = 1
	server := newProxyTestServer(newProxyTestStore(), cfg)
	request := httptest.NewRequest(http.MethodGet, "http://10.255.255.1:81/private", nil)
	server.httpDirect(httptest.NewRecorder(), request, nil, nil, true)

	if got := proxyRequests.Load(); got != 0 {
		t.Fatalf("bypass request used environment proxy %d time(s), want direct dialing", got)
	}
}
