package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultNetworkSettings(t *testing.T) {
	cfg := Default()
	if cfg.Network.ParallelDownloads != 5 {
		t.Errorf("parallel downloads = %d, want 5", cfg.Network.ParallelDownloads)
	}
	if cfg.Network.Retries != 3 {
		t.Errorf("retries = %d, want 3", cfg.Network.Retries)
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
		"parallel_downloads = 5",
		"retries = 3",
		"timeout_seconds = 15",
		"verify_tls = false",
		"[ui]",
		"default_mode = 'ps3'",
		"default_download = 'firmware'",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("generated config missing %q:\n%s", expected, text)
		}
	}

	networkFields := []string{
		"parallel_downloads = 5",
		"retries = 3",
		"timeout_seconds = 15",
		"verify_tls = false",
	}
	previous := -1
	for _, field := range networkFields {
		index := strings.Index(text, field)
		if index <= previous {
			t.Errorf("network field %q is not in alphabetical order:\n%s", field, text)
		}
		previous = index
	}
}
