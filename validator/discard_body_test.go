package validator

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// endlessBody 是一个永不结束的响应体：模拟恶意/故障上游持续吐数据。
// 每次 Read 都立刻返回满缓冲，绝不返回 io.EOF。
type endlessBody struct {
	reads int
}

func (b *endlessBody) Read(p []byte) (int, error) {
	b.reads++
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

// TestDiscardResponseBodyIsBounded 锁定「丢弃响应体」路径的读取上限。
//
// checkHTTPSConnect 与 validateConnectivity 只需要读掉响应体让连接可复用，
// 不需要读完。原实现是 io.Copy(io.Discard, resp.Body)，对无限响应体会一直读下去：
// client.Timeout 约束的是整个请求，但在持续有数据到达时不会触发，
// 于是单次探测可以无限期占住一个 ValidateStream 并发槽位。
func TestDiscardResponseBodyIsBounded(t *testing.T) {
	body := &endlessBody{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		discardResponseBody(body)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("discardResponseBody did not return on an endless body; the read must be bounded")
	}

	// 上限必须真的生效：读取量不应远超 discardBodyLimit。
	// 单次 Read 至多填满调用方缓冲，故总读取量 <= limit + 一个缓冲。
	maxReads := discardBodyLimit/1024 + 2
	if body.reads > maxReads {
		t.Fatalf("discardResponseBody issued %d reads on an endless body, want <= %d (limit %d bytes)",
			body.reads, maxReads, discardBodyLimit)
	}
}

// TestDiscardResponseBodyDrainsShortBody 确认正常短响应仍被完整读干净，
// 使底层连接可以被安全复用/关闭。
func TestDiscardResponseBodyDrainsShortBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("short body"))
	}))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	discardResponseBody(resp.Body)
	// 排干后再读应立刻 EOF。
	n, err := resp.Body.Read(make([]byte, 8))
	if n != 0 || err != io.EOF {
		t.Fatalf("after discard: read n=%d err=%v, want 0/EOF (body must be fully drained)", n, err)
	}
	resp.Body.Close()
}
