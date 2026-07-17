package custom

import (
	"testing"

	"github.com/babutree/GeoProxy/config"
)

// TestNewManagerBuildsShardedSingBoxWithConfiguredShardCount 验证 NewManager 接入分片架构:
// singbox 字段应为 *ShardedSingBox(而非单一 *SingBoxProcess),且分片数取自 cfg.SingBoxShardCount。
//
// NewManager 构造期不触碰 storage(仅存指针),故此测试可传 nil storage、无需 cgo 即可运行。
func TestNewManagerBuildsShardedSingBoxWithConfiguredShardCount(t *testing.T) {
	cfg := &config.Config{
		SingBoxPath:       "missing-sing-box",
		SingBoxBasePort:   20000,
		SingBoxShardCount: 4,
	}

	m := NewManager(nil, nil, cfg)

	sharded, ok := m.singbox.(*ShardedSingBox)
	if !ok {
		t.Fatalf("m.singbox 类型 = %T, 期望 *ShardedSingBox(分片架构已接入)", m.singbox)
	}
	if len(sharded.shards) != 4 {
		t.Fatalf("分片数 = %d, 期望 4(取自 cfg.SingBoxShardCount)", len(sharded.shards))
	}
}

// TestNewManagerClampsZeroShardCount 验证未配置分片数(0)时收敛为至少 1 个分片,不崩溃。
func TestNewManagerClampsZeroShardCount(t *testing.T) {
	cfg := &config.Config{
		SingBoxPath:     "missing-sing-box",
		SingBoxBasePort: 20000,
		// SingBoxShardCount 未设(0)
	}

	m := NewManager(nil, nil, cfg)

	sharded, ok := m.singbox.(*ShardedSingBox)
	if !ok {
		t.Fatalf("m.singbox 类型 = %T, 期望 *ShardedSingBox", m.singbox)
	}
	if len(sharded.shards) != 1 {
		t.Fatalf("分片数 = %d, 期望 1(shardCount=0 收敛下界)", len(sharded.shards))
	}
}
