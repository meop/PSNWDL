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

func TestDefaultPathsLiveUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PSNWDL_CONFIG", "")
	base := filepath.Join(home, ".psnwdl")

	if got, err := ConfigPath(); err != nil || got != filepath.Join(base, "config.toml") {
		t.Fatalf("ConfigPath = %q, %v", got, err)
	}
	if got, err := DefaultLibraryDir(); err != nil || got != filepath.Join(base, "library") {
		t.Fatalf("DefaultLibraryDir = %q, %v", got, err)
	}
	if got, err := TitleDirForRoot("", "ps3"); err != nil || got != filepath.Join(base, "library", "ps3", "title") {
		t.Fatalf("TitleDirForRoot default = %q, %v", got, err)
	}
	if got, err := FirmwareDirForRoot("", "ps5"); err != nil || got != filepath.Join(base, "library", "ps5", "firmware") {
		t.Fatalf("FirmwareDirForRoot default = %q, %v", got, err)
	}
	if cfg := Default(); cfg.HomeDir != home || cfg.Storage.LibraryDir != filepath.Join(base, "library") {
		t.Fatalf("Default paths = %q, %q", cfg.HomeDir, cfg.Storage.LibraryDir)
	}
}

func TestSaveRoundTripsAndKeepsHomeDirOutOfTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	cfg := Default()
	cfg.RPCS3.GamesYML = `C:\RPCS3\config\games.yml`
	cfg.UI.Theme = "light"
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "home_dir") || strings.Contains(string(data), "HomeDir") {
		t.Fatalf("HomeDir was written to config.toml:\n%s", data)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.RPCS3 != cfg.RPCS3 || got.UI != cfg.UI || got.HomeDir == "" {
		t.Fatalf("reloaded = %+v", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("Save left temp files behind: %v", entries)
	}
}

func TestSaveAndLoadReportFilesystemErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Save(filepath.Join(file, "config.toml"), Default()); err == nil {
		t.Fatal("Save under a file returned no error")
	}
	if err := Save(dir, Default()); err == nil {
		t.Fatal("Save over a directory returned no error")
	}
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "read config") {
		t.Fatalf("Load of a directory = %v, want a read error", err)
	}
	if _, err := Load(filepath.Join(file, "config.toml")); err == nil {
		t.Fatal("Load that cannot write defaults returned no error")
	}
}
