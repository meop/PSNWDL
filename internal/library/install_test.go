package library

import "testing"

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
