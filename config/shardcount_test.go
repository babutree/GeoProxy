package config

import "testing"

// TestDefaultSingBoxShardCountIsFour 验证分片数默认值为 4（路 C 决策：默认 4、可配置）。
func TestDefaultSingBoxShardCountIsFour(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("DATA_DIR", t.TempDir())

	cfg := DefaultConfig()

	if cfg.SingBoxShardCount != 4 {
		t.Fatalf("SingBoxShardCount = %d, want 4 (默认分片数)", cfg.SingBoxShardCount)
	}
}

// TestSingBoxShardCountRoundTrips 验证分片数经 Save/Load 持久化往返（可配置）。
func TestSingBoxShardCountRoundTrips(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("DATA_DIR", t.TempDir())

	cfg := Load()
	cfg.SingBoxShardCount = 8
	if err := Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	reloaded := Load()
	if reloaded.SingBoxShardCount != 8 {
		t.Fatalf("SingBoxShardCount after reload = %d, want 8", reloaded.SingBoxShardCount)
	}
}
