package custom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/babutree/GeoProxy/config"
)

func TestNewManagerUsesResolvedDataDir(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", "  "+dataDir+"  ")
	cfg := &config.Config{
		SingBoxPath:       "missing-sing-box",
		SingBoxBasePort:   testSingBoxBasePort,
		SingBoxShardCount: 1,
	}

	m := NewManager(nil, nil, cfg)
	t.Cleanup(m.Stop)

	sharded, ok := m.singbox.(*ShardedSingBox)
	if !ok {
		t.Fatalf("m.singbox 类型 = %T，期望 *ShardedSingBox", m.singbox)
	}
	process, ok := sharded.shards[0].(*SingBoxProcess)
	if !ok {
		t.Fatalf("shard 类型 = %T，期望 *SingBoxProcess", sharded.shards[0])
	}
	want := filepath.Join(dataDir, "shard-0", "singbox")
	if process.configDir != want {
		t.Fatalf("sing-box 数据目录 = %q，期望 config.DataDir 解析后的 %q", process.configDir, want)
	}
}

func TestNewSingBoxProcessRejectsEmptyDataDir(t *testing.T) {
	s := NewSingBoxProcess("missing-sing-box", "", testSingBoxBasePort)
	if s.configDir != "" {
		t.Fatalf("空数据目录被解析为 %q，禁止回退到当前工作目录", s.configDir)
	}

	err := s.Reload([]ParsedNode{{
		Name:   "invalid-data-dir",
		Type:   "vmess",
		Server: "127.0.0.1",
		Port:   443,
	}})
	if err == nil || !strings.Contains(err.Error(), "数据目录") {
		t.Fatalf("Reload 错误 = %v，期望显式的数据目录错误", err)
	}
}

func TestNewShardedSingBoxRejectsBlankRootDataDir(t *testing.T) {
	t.Chdir(t.TempDir())
	sb := NewShardedSingBox("missing-sing-box", "", testSingBoxBasePort, 1)
	t.Cleanup(sb.Stop)

	process, ok := sb.shards[0].(*SingBoxProcess)
	if !ok {
		t.Fatalf("shard 类型 = %T，期望 *SingBoxProcess", sb.shards[0])
	}
	if process.configDir != "" {
		t.Errorf("空白根数据目录生成了 configDir=%q，禁止分片链回退到相对/CWD 路径", process.configDir)
	}
	initial := sb.GetRuntimeStatus()
	if initial.Status != SingBoxStatusFailed || initial.Reason != "data_dir_invalid" {
		t.Fatalf("空根分片初始运行态 = %+v，期望 failed/data_dir_invalid", initial)
	}

	err := sb.Reload([]ParsedNode{tunnelNode("blank-root", "blank-root.example.com", "password")})
	if err == nil || !strings.Contains(err.Error(), "数据目录") {
		t.Fatalf("分片 Reload 错误 = %v，期望显式的数据目录错误", err)
	}
	rs := sb.GetRuntimeStatus()
	if rs.Status != SingBoxStatusNoTunnelNodes || rs.Reason != SingBoxStatusNoTunnelNodes || rs.Running {
		t.Fatalf("空旧目标回滚后的运行态 = %+v，期望 no_tunnel_nodes/非运行", rs)
	}
}

func TestNewSingBoxProcessRejectsFileDataDir(t *testing.T) {
	parent := t.TempDir()
	dataFile := filepath.Join(parent, "data-file")
	if err := os.WriteFile(dataFile, []byte("not a directory"), 0600); err != nil {
		t.Fatalf("创建数据文件失败: %v", err)
	}

	s := NewSingBoxProcess("missing-sing-box", dataFile, testSingBoxBasePort)
	if s.configDir != "" {
		t.Errorf("普通文件数据目录生成了 configDir=%q，期望拒绝", s.configDir)
	}
	err := s.Reload([]ParsedNode{tunnelNode("file-root", "file-root.example.com", "password")})
	if err == nil || !strings.Contains(err.Error(), "数据目录") {
		t.Fatalf("普通文件数据目录 Reload 错误 = %v，期望显式错误", err)
	}
	if _, statErr := os.Stat(filepath.Join(dataFile, "singbox")); !os.IsNotExist(statErr) {
		t.Fatalf("普通文件路径下出现旁路目录，stat error = %v", statErr)
	}
}
