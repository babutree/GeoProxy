package logger

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

func resetLinesForTest(t *testing.T) {
	t.Helper()
	mu.Lock()
	lines = nil
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		lines = nil
		mu.Unlock()
	})
}

func discardStdout(t *testing.T) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, r)
		close(done)
	}()
	t.Cleanup(func() {
		_ = w.Close()
		os.Stdout = old
		<-done
		_ = r.Close()
	})
}

// TestWriterTruncateDropsUnderlyingCapacity 复现截断后底层数组仍保留已丢弃行引用：
// len 虽被压到 maxLines，但 cap 与旧 string 头仍滞留，长期运行内存只增不减。
func TestWriterTruncateDropsUnderlyingCapacity(t *testing.T) {
	resetLinesForTest(t)
	discardStdout(t)
	w := &writer{}
	const extra = 200
	for i := 0; i < maxLines+extra; i++ {
		msg := fmt.Sprintf("line-%05d-%s", i, strings.Repeat("x", 64))
		if _, err := w.Write([]byte(msg)); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	mu.RLock()
	gotLen := len(lines)
	gotCap := cap(lines)
	first := lines[0]
	last := lines[len(lines)-1]
	mu.RUnlock()

	if gotLen != maxLines {
		t.Fatalf("len(lines)=%d, want %d", gotLen, maxLines)
	}
	// 截断后 cap 不得显著大于 maxLines，否则说明仍挂着已丢弃前缀。
	if gotCap > maxLines {
		t.Fatalf("cap(lines)=%d after truncate, want <= %d (dropped lines still retained)", gotCap, maxLines)
	}
	if !strings.Contains(first, "line-00200") {
		t.Fatalf("oldest retained line = %q, want around line-00200", first)
	}
	if !strings.Contains(last, fmt.Sprintf("line-%05d", maxLines+extra-1)) {
		t.Fatalf("newest retained line = %q, want last written", last)
	}
}

// TestGetLinesConcurrentWithWrite 并发写读不应 data race，且截断后结果长度受 maxLines 约束。
func TestGetLinesConcurrentWithWrite(t *testing.T) {
	resetLinesForTest(t)
	discardStdout(t)
	w := &writer{}
	var wg sync.WaitGroup
	const writers = 8
	const perWriter = 100
	wg.Add(writers + 2)
	for i := 0; i < writers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < perWriter; j++ {
				_, _ = w.Write([]byte(fmt.Sprintf("w%d-%d", id, j)))
			}
		}(i)
	}
	for r := 0; r < 2; r++ {
		go func() {
			defer wg.Done()
			for k := 0; k < 200; k++ {
				got := GetLines(50)
				if len(got) > maxLines {
					t.Errorf("GetLines returned %d lines > maxLines", len(got))
					return
				}
			}
		}()
	}
	wg.Wait()

	all := GetLines(0)
	if len(all) > maxLines {
		t.Fatalf("GetLines(0)=%d, want <= %d", len(all), maxLines)
	}
	if len(all) == 0 {
		t.Fatal("GetLines(0) empty after concurrent writes")
	}
}

// TestGetLinesEmptyAndNegativeN 空缓冲与 n<=0 应返回空/全量，不 panic。
func TestGetLinesEmptyAndNegativeN(t *testing.T) {
	resetLinesForTest(t)
	discardStdout(t)
	if got := GetLines(10); len(got) != 0 {
		t.Fatalf("empty GetLines(10)=%v, want empty", got)
	}
	w := &writer{}
	_, _ = w.Write([]byte("only"))
	if got := GetLines(0); len(got) != 1 {
		t.Fatalf("GetLines(0)=%d, want 1", len(got))
	}
	if got := GetLines(-1); len(got) != 1 {
		t.Fatalf("GetLines(-1)=%d, want 1", len(got))
	}
}
