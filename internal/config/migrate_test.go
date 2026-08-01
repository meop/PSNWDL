package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateLibraryLayoutMovesLegacyDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	legacyRoot := filepath.Join(home, ".psnwdl", "download")
	legacyTitle := filepath.Join(legacyRoot, "ps3", "updates", "BCUS98114")
	legacyFirmware := filepath.Join(legacyRoot, "ps3", "updates", "firmware")
	if err := os.MkdirAll(legacyTitle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(legacyFirmware, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyTitle, "BCUS98114_01.05.pkg"), []byte("pkg"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyFirmware, "firmware_4.93.pup"), []byte("pup"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{SchemaVersion: 1, Storage: Storage{LibraryDir: legacyRoot}}
	migrated, err := MigrateLibraryLayout(cfg)
	if err != nil {
		t.Fatalf("MigrateLibraryLayout: %v", err)
	}
	if !migrated {
		t.Fatal("MigrateLibraryLayout reported no migration")
	}
	newRoot := filepath.Join(home, ".psnwdl", "library")
	if cfg.Storage.LibraryDir != newRoot {
		t.Fatalf("library dir = %q, want %q", cfg.Storage.LibraryDir, newRoot)
	}
	if cfg.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version = %d, want %d", cfg.SchemaVersion, SchemaVersion)
	}
	for _, path := range []string{
		filepath.Join(newRoot, "ps3", "title", "BCUS98114", "BCUS98114_01.05.pkg"),
		filepath.Join(newRoot, "ps3", "firmware", "unknown", "firmware_4.93.pup"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("migrated file %s: %v", path, err)
		}
	}
}

func TestMigrateLibraryLayoutAddsUnknownFirmwareLocale(t *testing.T) {
	root := t.TempDir()
	firmwareDir := filepath.Join(root, "ps3", "firmware")
	if err := os.MkdirAll(firmwareDir, 0o755); err != nil {
		t.Fatal(err)
	}
	firmware := filepath.Join(firmwareDir, "firmware_4.93.pup")
	if err := os.WriteFile(firmware, []byte("pup"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{SchemaVersion: 2, Storage: Storage{LibraryDir: root}}
	migrated, err := MigrateLibraryLayout(cfg)
	if err != nil {
		t.Fatalf("MigrateLibraryLayout: %v", err)
	}
	if !migrated {
		t.Fatal("MigrateLibraryLayout reported no migration")
	}
	if _, err := os.Stat(filepath.Join(firmwareDir, "unknown", "firmware_4.93.pup")); err != nil {
		t.Fatalf("migrated firmware: %v", err)
	}
}

func TestMigrateLibraryLayoutKeepsCustomRoot(t *testing.T) {
	root := t.TempDir()
	legacyTitle := filepath.Join(root, "ps4", "updates", "CUSA00001")
	if err := os.MkdirAll(legacyTitle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyTitle, "CUSA00001_01.00.pkg"), []byte("pkg"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{SchemaVersion: 1, Storage: Storage{LibraryDir: root}}
	if _, err := MigrateLibraryLayout(cfg); err != nil {
		t.Fatalf("MigrateLibraryLayout: %v", err)
	}
	if cfg.Storage.LibraryDir != root {
		t.Fatalf("custom root changed to %q", cfg.Storage.LibraryDir)
	}
	if _, err := os.Stat(filepath.Join(root, "ps4", "title", "CUSA00001", "CUSA00001_01.00.pkg")); err != nil {
		t.Fatalf("migrated custom-root file: %v", err)
	}
}
