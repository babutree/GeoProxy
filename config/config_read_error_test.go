package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRejectsConfigReadErrorBeforeBootstrap(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	configPath := filepath.Join(dataDir, "config.json")
	if err := os.Mkdir(configPath, 0700); err != nil {
		t.Fatalf("Mkdir(config.json): %v", err)
	}

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("Load() did not reject a non-ENOENT config read error")
		}
		if message := fmt.Sprint(recovered); !strings.Contains(message, "load config: read") {
			t.Fatalf("Load() panic = %q, want explicit config read error before bootstrap", message)
		}
	}()

	Load()
}
