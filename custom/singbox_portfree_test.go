package custom

import (
	"fmt"
	"net"
	"testing"
	"time"
)

// TestWaitPortsFreeReturnsZeroWhenNoPorts 无端口时应立即返回 0（不阻塞）。
func TestWaitPortsFreeReturnsZeroWhenNoPorts(t *testing.T) {
	s := NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort)
	start := time.Now()
	if busy := s.waitPortsFreeLocked(2 * time.Second); busy != 0 {
		t.Fatalf("waitPortsFreeLocked with no ports = %d, want 0", busy)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("waitPortsFreeLocked with no ports took %s, want near-immediate", elapsed)
	}
}

// TestWaitPortsFreeReturnsZeroWhenPortFree 端口未被占用时应快速返回 0。
func TestWaitPortsFreeReturnsZeroWhenPortFree(t *testing.T) {
	// 先占一个端口拿到其编号，随即释放，确保该端口此刻空闲。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	freePort := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	s := NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort)
	s.portMap = map[string]int{"node-key": freePort}

	start := time.Now()
	if busy := s.waitPortsFreeLocked(2 * time.Second); busy != 0 {
		t.Fatalf("waitPortsFreeLocked on free port = %d, want 0", busy)
	}
	if elapsed := time.Since(start); elapsed > 1*time.Second {
		t.Fatalf("waitPortsFreeLocked on free port took %s, want fast", elapsed)
	}
}

// TestWaitPortsFreeTimesOutWhenPortBusy 端口持续被占用时应等满超时并返回占用数（负向路径）。
func TestWaitPortsFreeTimesOutWhenPortBusy(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	busyPort := ln.Addr().(*net.TCPAddr).Port

	s := NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort)
	s.portMap = map[string]int{"node-key": busyPort}

	timeout := 600 * time.Millisecond
	start := time.Now()
	busy := s.waitPortsFreeLocked(timeout)
	elapsed := time.Since(start)

	if busy != 1 {
		t.Fatalf("waitPortsFreeLocked on busy port = %d, want 1", busy)
	}
	// 必须真的等满了超时（证明它在轮询等待，而非立即放弃或误判空闲）。
	if elapsed < timeout {
		t.Fatalf("waitPortsFreeLocked returned after %s, want >= timeout %s", elapsed, timeout)
	}
}

// TestWaitPortsFreeReleasesMidWait 端口在等待期间被释放，应在超时前返回 0（证明轮询生效）。
func TestWaitPortsFreeReleasesMidWait(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	releasePort := ln.Addr().(*net.TCPAddr).Port

	s := NewSingBoxProcess("missing-sing-box", t.TempDir(), testSingBoxBasePort)
	s.httpPortMap = map[string]int{"node-key": releasePort}

	// 300ms 后释放端口；waitPortsFreeLocked 应在 2s 超时内检测到释放并返回 0。
	go func() {
		time.Sleep(300 * time.Millisecond)
		ln.Close()
	}()

	start := time.Now()
	busy := s.waitPortsFreeLocked(2 * time.Second)
	elapsed := time.Since(start)

	if busy != 0 {
		t.Fatalf("waitPortsFreeLocked after mid-wait release = %d, want 0", busy)
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("waitPortsFreeLocked took full timeout %s, want early return after release", elapsed)
	}
	_ = fmt.Sprint(elapsed)
}
