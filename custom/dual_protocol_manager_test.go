package custom

import (
	"testing"

	"goproxy/storage"
)

// TestAddPendingSubscriptionProxyMarksDualProtocol 验证入库置位：
// tunnel 节点（单 mixed 端口同时服务 SOCKS5+HTTP）入库时 dual_protocol=true，
// 供前端可靠区分双协议节点；direct 单协议节点仍为 false。
// 这是方案Y（存储层显式标记）取代前端猜地址的核心置位点。
func TestAddPendingSubscriptionProxyMarksDualProtocol(t *testing.T) {
	store := newTestStorage(t)
	subID, err := store.AddSubscription("sub", "https://example.invalid/x", "", "auto", 60, "")
	if err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	m := &Manager{storage: store}

	// tunnel 节点（本地 mixed 端口）：dualProtocol=true。
	tunnel, ok := m.addPendingSubscriptionProxy("127.0.0.1:20001", "socks5", subID, true)
	if !ok {
		t.Fatal("addPendingSubscriptionProxy(tunnel) failed")
	}
	got, err := store.GetProxyByIdentity(tunnel.Address, storage.SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(tunnel) error = %v", err)
	}
	if !got.DualProtocol {
		t.Fatal("tunnel 节点 dual_protocol = false, want true")
	}

	// direct 节点（外部单协议）：dualProtocol=false。
	direct, ok := m.addPendingSubscriptionProxy("9.9.9.9:1080", "socks5", subID, false)
	if !ok {
		t.Fatal("addPendingSubscriptionProxy(direct) failed")
	}
	got, err = store.GetProxyByIdentity(direct.Address, storage.SourceSubscription, subID)
	if err != nil {
		t.Fatalf("GetProxyByIdentity(direct) error = %v", err)
	}
	if got.DualProtocol {
		t.Fatal("direct 节点 dual_protocol = true, want false")
	}
}
