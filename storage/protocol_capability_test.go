package storage

import (
	"reflect"
	"testing"
)

func seedProtocolCapabilityNodes(t *testing.T, store *Storage) map[string]int64 {
	t.Helper()
	insertTestSubscription(t, store, 77, "paused")

	nodes := []Proxy{
		{Address: "pure-http:1", Protocol: "http", Region: "jp", Latency: 30, Status: "active"},
		{Address: "pure-socks:1", Protocol: "socks5", Region: "jp", Latency: 20, Status: "active"},
		{Address: "dual:1", Protocol: "socks5", Region: "jp", Latency: 10, Status: "active", DualProtocol: true},
		{Address: "pure-trojan:1", Protocol: "trojan", Region: "jp", Latency: 5, Status: "active"},
		{Address: "dual-disabled:1", Protocol: "socks5", Region: "jp", Latency: 1, Status: "disabled", DualProtocol: true},
		{Address: "dual-user-paused:1", Protocol: "socks5", Region: "jp", Latency: 2, Status: "active", UserPaused: true, DualProtocol: true},
		{Address: "dual-failing:1", Protocol: "socks5", Region: "jp", Latency: 3, Status: "active", FailCount: 3, DualProtocol: true},
		{
			Address: "dual-parent-paused:1", Protocol: "socks5", Region: "jp", Latency: 4,
			Status: "active", Source: SourceSubscription, SubscriptionID: 77, DualProtocol: true,
		},
	}
	ids := make(map[string]int64, len(nodes))
	for _, node := range nodes {
		ids[node.Address] = insertAPINode(t, store, node)
	}
	return ids
}

func TestGetByProtocolUsesInboundCapabilities(t *testing.T) {
	store := newTestStorage(t)
	seedProtocolCapabilityNodes(t, store)

	httpNodes, err := store.GetByProtocol("http")
	if err != nil {
		t.Fatalf("GetByProtocol(http) error = %v", err)
	}
	if got := addrsOf(httpNodes); !reflect.DeepEqual(got, []string{"dual:1", "pure-http:1"}) {
		t.Fatalf("GetByProtocol(http) = %v, want dual then pure HTTP by latency", got)
	}

	socksNodes, err := store.GetByProtocol(" SOCKS5 ")
	if err != nil {
		t.Fatalf("GetByProtocol(socks5) error = %v", err)
	}
	if got := addrsOf(socksNodes); !reflect.DeepEqual(got, []string{"dual:1", "pure-socks:1"}) {
		t.Fatalf("GetByProtocol(socks5) = %v, want dual and pure SOCKS5", got)
	}

	unknownNodes, err := store.GetByProtocol(" TROJAN ")
	if err != nil {
		t.Fatalf("GetByProtocol(trojan) error = %v", err)
	}
	if got := addrsOf(unknownNodes); !reflect.DeepEqual(got, []string{"pure-trojan:1"}) {
		t.Fatalf("GetByProtocol(trojan) = %v, want exact unknown protocol only", got)
	}
}

func TestListNodesForAPIUsesInboundCapabilitiesAndKeepsPaging(t *testing.T) {
	store := newTestStorage(t)
	seedProtocolCapabilityNodes(t, store)

	first, total, err := store.ListNodesForAPI(NodeAPIFilter{Protocol: " HTTP ", Limit: 1})
	if err != nil {
		t.Fatalf("ListNodesForAPI(http page 1) error = %v", err)
	}
	if total != 2 || len(first) != 1 || first[0].Address != "dual:1" {
		t.Fatalf("http page 1 nodes=%v total=%d, want dual:1 and total 2", addrsOf(first), total)
	}

	second, total, err := store.ListNodesForAPI(NodeAPIFilter{Protocol: "http", Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("ListNodesForAPI(http page 2) error = %v", err)
	}
	if total != 2 || len(second) != 1 || second[0].Address != "pure-http:1" {
		t.Fatalf("http page 2 nodes=%v total=%d, want pure-http:1 and total 2", addrsOf(second), total)
	}

	unknown, total, err := store.ListNodesForAPI(NodeAPIFilter{Protocol: " TROJAN "})
	if err != nil {
		t.Fatalf("ListNodesForAPI(trojan) error = %v", err)
	}
	if total != 1 || len(unknown) != 1 || unknown[0].Address != "pure-trojan:1" {
		t.Fatalf("trojan nodes=%v total=%d, want exact pure-trojan:1 only", addrsOf(unknown), total)
	}
}

func TestRandomByProtocolExcludeFilteredUsesInboundCapabilities(t *testing.T) {
	store := newTestStorage(t)
	seedProtocolCapabilityNodes(t, store)

	proxy, err := store.GetRandomByProtocolExcludeFiltered(
		" HTTP ",
		[]string{"pure-http:1"},
		SourceManual,
	)
	if err != nil {
		t.Fatalf("GetRandomByProtocolExcludeFiltered(http) error = %v", err)
	}
	if proxy.Address != "dual:1" {
		t.Fatalf("GetRandomByProtocolExcludeFiltered(http) = %q, want dual:1", proxy.Address)
	}

	if _, err := store.GetRandomByProtocolExcludeFiltered(
		"trojan",
		[]string{"pure-trojan:1"},
		SourceManual,
	); err == nil {
		t.Fatal("GetRandomByProtocolExcludeFiltered(trojan) matched dual after exact node was excluded")
	}
}

func TestLowestLatencyByProtocolExcludeFilteredUsesInboundCapabilities(t *testing.T) {
	store := newTestStorage(t)
	seedProtocolCapabilityNodes(t, store)

	proxy, err := store.GetLowestLatencyByProtocolExcludeFiltered("http", nil, SourceManual)
	if err != nil {
		t.Fatalf("GetLowestLatencyByProtocolExcludeFiltered(http) error = %v", err)
	}
	if proxy.Address != "dual:1" {
		t.Fatalf("GetLowestLatencyByProtocolExcludeFiltered(http) = %q, want lower-latency dual:1", proxy.Address)
	}

	proxy, err = store.GetLowestLatencyByProtocolExcludeFiltered(
		"http",
		[]string{"dual:1"},
		SourceManual,
	)
	if err != nil {
		t.Fatalf("GetLowestLatencyByProtocolExcludeFiltered(http, exclude dual) error = %v", err)
	}
	if proxy.Address != "pure-http:1" {
		t.Fatalf("GetLowestLatencyByProtocolExcludeFiltered(http, exclude dual) = %q, want pure-http:1", proxy.Address)
	}

	if _, err := store.GetLowestLatencyByProtocolExcludeFiltered(
		"trojan",
		[]string{"pure-trojan:1"},
		SourceManual,
	); err == nil {
		t.Fatal("GetLowestLatencyByProtocolExcludeFiltered(trojan) matched dual after exact node was excluded")
	}
}

// GetAverageLatency 按存储的物理协议聚合，不把 mixed 的 SOCKS5 记录重复计入 HTTP。
func TestGetAverageLatencyKeepsStoredProtocolContract(t *testing.T) {
	store := newTestStorage(t)
	insertAPINode(t, store, Proxy{
		Address: "physical-http:1", Protocol: "http", Region: "jp",
		Latency: 30, Status: "active",
	})
	insertAPINode(t, store, Proxy{
		Address: "mixed-stored-socks:1", Protocol: "socks5", Region: "jp",
		Latency: 10, Status: "active", DualProtocol: true,
	})

	httpAverage, err := store.GetAverageLatency("http")
	if err != nil {
		t.Fatalf("GetAverageLatency(http) error = %v", err)
	}
	if httpAverage != 30 {
		t.Fatalf("GetAverageLatency(http) = %d, want physical HTTP average 30", httpAverage)
	}

	socksAverage, err := store.GetAverageLatency("socks5")
	if err != nil {
		t.Fatalf("GetAverageLatency(socks5) error = %v", err)
	}
	if socksAverage != 10 {
		t.Fatalf("GetAverageLatency(socks5) = %d, want stored SOCKS5 average 10", socksAverage)
	}
}
