package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultNetworkSettings(t *testing.T) {
	cfg := Default()
	if cfg.Network.MaxConcurrentDownloads != 5 {
		t.Errorf("max concurrent downloads = %d, want 5", cfg.Network.MaxConcurrentDownloads)
	}
	if cfg.Network.RetryCount != 3 {
		t.Errorf("retry count = %d, want 3", cfg.Network.RetryCount)
	}
}

func TestLoadWritesFullDefaultConfigWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if _, err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(data)
	for _, expected := range []string{
		"[rpcs3]",
		"games_yml = ''",
		"hdd0_game = ''",
		"[storage]",
		"[network]",
		"max_concurrent_downloads = 5",
		"request_timeout_seconds = 15",
		"retry_count = 3",
		"[ui]",
		"default_mode = 'ps3'",
		"default_download = 'firmware'",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("generated config missing %q:\n%s", expected, text)
		}
	}
}
