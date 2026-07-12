package logger

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

const maxLines = 500

var (
	lines []string
	mu    sync.RWMutex
)

// Init 替换标准 log 输出，同时保留控制台输出
func Init() {
	log.SetFlags(0)
	log.SetOutput(&writer{})
}

type writer struct{}

func (w *writer) Write(p []byte) (n int, err error) {
	line := strings.TrimRight(string(p), "\n")
	ts := time.Now().Format("15:04:05")
	formatted := fmt.Sprintf("[%s] %s", ts, line)

	mu.Lock()
	lines = append(lines, formatted)
	if len(lines) > maxLines {
		// 重新分配并拷贝，断开对已丢弃前缀的底层数组引用，避免旧日志字符串长期滞留。
		kept := make([]string, maxLines)
		copy(kept, lines[len(lines)-maxLines:])
		lines = kept
	}
	mu.Unlock()

	// 同时输出到控制台
	fmt.Println(formatted)
	return len(p), nil
}

// GetLines 返回最近 N 条日志
func GetLines(n int) []string {
	mu.RLock()
	defer mu.RUnlock()
	if n <= 0 || n > len(lines) {
		n = len(lines)
	}
	result := make([]string, n)
	copy(result, lines[len(lines)-n:])
	return result
}
