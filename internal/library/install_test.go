package library

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPackageNeedsInstall(t *testing.T) {
	tests := []struct {
		pkg       string
		installed string
		want      bool
	}{
		{"01.05", "", true},
		{"01.05", "01.04", true},
		{"01.05", "01.05", false},
		{"01.04", "01.05", false},
	}
	for _, test := range tests {
		if got := PackageNeedsInstall(test.pkg, test.installed); got != test.want {
			t.Errorf("PackageNeedsInstall(%q, %q) = %t, want %t", test.pkg, test.installed, got, test.want)
		}
	}
}

func TestInstalledVersionMissingAndMalformed(t *testing.T) {
	root := t.TempDir()
	version, err := InstalledVersion(root, "BCUS98114")
	if err != nil || version != "" {
		t.Fatalf("missing InstalledVersion = %q, %v", version, err)
	}

	dir := filepath.Join(root, "BCUS98114")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "PARAM.SFO"), []byte("not an sfo"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := InstalledVersion(root, "BCUS98114"); err == nil {
		t.Fatal("malformed PARAM.SFO unexpectedly succeeded")
	}
}
