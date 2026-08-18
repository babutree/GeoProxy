package webui

import (
	"net/http"
	"testing"
	"time"
)

// WebUI 服务原先用 http.ListenAndServe，除了 Go 默认值外没有任何超时：
// 慢速读取的客户端可以长期占住写缓冲，空闲 keep-alive 连接也不会被回收。
//
// 这里能安全设 WriteTimeout 的前提是：WebUI 全部端点都是短小 JSON 或内嵌
// 静态资源（最大约 120KB），没有 SSE/流式/文件下载，也不 Hijack 连接。
// proxy 包的 CONNECT 服务不满足该前提，故不共用这套超时（见常量注释）。
func TestWebUIHTTPServerHasFullTimeouts(t *testing.T) {
	srv := webUIHTTPServer(":7800", http.NewServeMux())

	if srv.Addr != ":7800" {
		t.Fatalf("Addr = %q, want :7800", srv.Addr)
	}
	if srv.Handler == nil {
		t.Fatal("Handler is nil; the server would fall back to http.DefaultServeMux")
	}

	for _, tc := range []struct {
		name string
		got  time.Duration
	}{
		{"ReadHeaderTimeout", srv.ReadHeaderTimeout},
		{"ReadTimeout", srv.ReadTimeout},
		{"WriteTimeout", srv.WriteTimeout},
		{"IdleTimeout", srv.IdleTimeout},
	} {
		if tc.got <= 0 {
			t.Fatalf("%s = %v, want a positive timeout (unbounded connections allow slow-client DoS)", tc.name, tc.got)
		}
	}

	// ReadHeaderTimeout 必须不长于 ReadTimeout，否则半请求头能拖到整体读超时才被切断。
	if srv.ReadHeaderTimeout > srv.ReadTimeout {
		t.Fatalf("ReadHeaderTimeout(%v) > ReadTimeout(%v); header stalls must be cut earlier",
			srv.ReadHeaderTimeout, srv.ReadTimeout)
	}
	// WriteTimeout 必须宽于最大响应的合理传输时间。dashboard bundle 约 120KB，
	// 即便 16KB/s 的慢速客户端也应在此窗口内读完。
	if srv.WriteTimeout < 30*time.Second {
		t.Fatalf("WriteTimeout = %v, want >= 30s so slow clients can still fetch the dashboard bundle", srv.WriteTimeout)
	}
}

// 静态资源是 WebUI 最大的响应体；确认它仍远小于 WriteTimeout 能覆盖的范围，
// 避免未来 bundle 膨胀后被超时截断而无人察觉。
func TestDashboardAssetsFitWithinWriteTimeout(t *testing.T) {
	largest := len(dashboardJS)
	if len(dashboardCSS) > largest {
		largest = len(dashboardCSS)
	}
	if len(dashboardHTML) > largest {
		largest = len(dashboardHTML)
	}

	// 以保守的 16KB/s 慢速客户端估算传输耗时。
	const slowClientBytesPerSecond = 16 << 10
	needed := time.Duration(largest) * time.Second / slowClientBytesPerSecond
	if needed >= webUIWriteTimeout {
		t.Fatalf("largest asset %d bytes needs ~%v at %d B/s, but WriteTimeout is %v; raise the timeout or split the bundle",
			largest, needed, slowClientBytesPerSecond, webUIWriteTimeout)
	}
	t.Logf("largest asset=%d bytes, ~%v at %d B/s, WriteTimeout=%v", largest, needed.Round(time.Millisecond), slowClientBytesPerSecond, webUIWriteTimeout)
}
