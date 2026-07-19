package selector

import (
	"fmt"
	"testing"
	"time"

	"github.com/babutree/GeoProxy/affinity"
	"github.com/babutree/GeoProxy/storage"
)

func benchmarkSessionCandidates(count int) []storage.Proxy {
	proxies := make([]storage.Proxy, 0, count)
	for i := 1; i <= count; i++ {
		proxies = append(proxies, storage.Proxy{
			ID: int64(i), Address: fmt.Sprintf("bench-%05d:1", i), Region: "jp",
			Latency: 20 + i%400, Status: "active",
			UseCount: i % 1000, SuccessCount: i % 997,
		})
	}
	return proxies
}

func BenchmarkPickSessionCandidate(b *testing.B) {
	for _, count := range []int{3, 215, 6000} {
		b.Run(fmt.Sprintf("nodes-%d", count), func(b *testing.B) {
			proxies := benchmarkSessionCandidates(count)
			active := affinity.NewWithClock(0, time.Now)
			for i := 0; i < count; i += 10 {
				active.SetProxy(fmt.Sprintf("active-%05d", i), proxies[i].ID, proxies[i].Address, "jp")
			}
			sessions := make([]string, 1024)
			for i := range sessions {
				sessions[i] = fmt.Sprintf("bench-session-%04d", i)
			}
			b.ReportAllocs()
			b.ResetTimer()
			var sink storage.Proxy
			for i := 0; i < b.N; i++ {
				sink = pickSessionCandidateWithAffinity(proxies, sessions[i%len(sessions)], active)
			}
			_ = sink
		})
	}
}

func BenchmarkPickSessionCandidateNoActiveBindings(b *testing.B) {
	for _, count := range []int{3, 215, 6000} {
		b.Run(fmt.Sprintf("nodes-%d", count), func(b *testing.B) {
			proxies := benchmarkSessionCandidates(count)
			sessions := make([]string, 1024)
			for i := range sessions {
				sessions[i] = fmt.Sprintf("bench-no-active-%04d", i)
			}
			b.ReportAllocs()
			b.ResetTimer()
			var sink storage.Proxy
			for i := 0; i < b.N; i++ {
				sink = pickSessionCandidate(proxies, sessions[i%len(sessions)])
			}
			_ = sink
		})
	}
}
