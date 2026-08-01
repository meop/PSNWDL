package downloads

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanUsesFirmwareAndTitleBranches(t *testing.T) {
	root := t.TempDir()
	firmwareDir := filepath.Join(root, "ps3", "firmware", "us")
	titleDir := filepath.Join(root, "ps3", "title", "BCUS98114")
	if err := os.MkdirAll(firmwareDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(titleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(firmwareDir, "firmware_4.93.pup"), []byte("pup"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(titleDir, "BCUS98114_01.05.pkg"), []byte("pkg"), 0o644); err != nil {
		t.Fatal(err)
	}

	titles, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(titles) != 2 {
		t.Fatalf("titles = %d, want 2", len(titles))
	}
	byID := map[string]Title{}
	for _, title := range titles {
		byID[title.TitleID] = title
	}
	if byID["firmware"].Path != firmwareDir {
		t.Errorf("firmware title = %+v", byID["firmware"])
	}
	if byID["firmware"].Locale != "us" {
		t.Errorf("firmware locale = %q, want us", byID["firmware"].Locale)
	}
	if byID["BCUS98114"].Path != titleDir {
		t.Errorf("download title = %+v", byID["BCUS98114"])
	}
}

func TestDeletePrunesEmptyTitleDirectories(t *testing.T) {
	root := t.TempDir()
	titleDir := filepath.Join(root, "ps3", "title", "BCUS98114")
	if err := os.MkdirAll(titleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(titleDir, "BCUS98114_01.05.pkg")
	if err := os.WriteFile(file, []byte("pkg"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Delete(root, []string{file}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(titleDir); !os.IsNotExist(err) {
		t.Fatalf("empty title directory remains: %v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("library root removed: %v", err)
	}
}
