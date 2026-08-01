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

func TestConfigPathUsesPSNWDLEnvironmentOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "custom.toml")
	t.Setenv("PSNWDL_CONFIG", override)
	got, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	if got != override {
		t.Fatalf("ConfigPath = %q, want %q", got, override)
	}
}

func TestLoadMergesMissingFieldsWithDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[ui]\ntheme = 'dark'\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.UI.Theme != "dark" || cfg.UI.DefaultMode != "ps3" || cfg.Network.ParallelDownloads != 5 {
		t.Fatalf("merged config = %+v", cfg)
	}
}

func TestLoadRejectsInvalidTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[network\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "parse config") {
		t.Fatalf("Load error = %v, want parse error", err)
	}
}

func TestLibraryPathHelpers(t *testing.T) {
	root := t.TempDir()
	title, err := TitleDirForRoot(root, "ps3")
	if err != nil {
		t.Fatalf("TitleDirForRoot: %v", err)
	}
	firmware, err := FirmwareDirForRoot(root, "ps4")
	if err != nil {
		t.Fatalf("FirmwareDirForRoot: %v", err)
	}
	if title != filepath.Join(root, "ps3", "title") {
		t.Fatalf("title path = %q", title)
	}
	if firmware != filepath.Join(root, "ps4", "firmware") {
		t.Fatalf("firmware path = %q", firmware)
	}
}
