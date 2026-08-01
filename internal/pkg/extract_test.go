package pkg

import (
	"path/filepath"
	"testing"
)

// TestIsPathPKG checks detection of path-traversal PKG item names.
func TestIsPathPKG_Detected(t *testing.T) {
	cases := []struct {
		name   string
		names  []string
		expect bool
	}{
		{
			name:   "unix traversal",
			names:  []string{"PS3_GAME/USRDIR/EBOOT.BIN", "../dev_hdd0/game/BCUS98114/PARAM.SFO"},
			expect: true,
		},
		{
			name:   "windows traversal",
			names:  []string{`..\ something`},
			expect: true,
		},
		{
			name:   "backslash traversal no space",
			names:  []string{`..\something`},
			expect: true,
		},
		{
			name:   "normal PKG",
			names:  []string{"PS3_GAME/USRDIR/EBOOT.BIN", "PS3_GAME/PARAM.SFO"},
			expect: false,
		},
		{
			name:   "empty list",
			names:  []string{},
			expect: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isPathPKG(tc.names)
			if got != tc.expect {
				t.Errorf("isPathPKG(%v) = %v, want %v", tc.names, got, tc.expect)
			}
		})
	}
}

// TestResolvePathPKGDest verifies safe path resolution for path-traversal PKGs.
func TestResolvePathPKGDest(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dest")

	cases := []struct {
		name    string
		rawName string
		wantOK  bool
	}{
		{
			name:    "normal relative path",
			rawName: "PS3_GAME/USRDIR/EBOOT.BIN",
			wantOK:  true,
		},
		{
			name:    "traversal that stays inside root after normalization",
			rawName: "PS3_GAME/../PS3_GAME/PARAM.SFO",
			wantOK:  true,
		},
		{
			// PyKG's resolve_path_pkg_dest pops on an empty stack (no-op), so
			// "../../../../etc/passwd" resolves to dest_root/etc/passwd which is
			// INSIDE destRoot. This is the same behavior as PyKG.
			name:    "traversal that resolves inside root after stack clamp",
			rawName: "../../../../etc/passwd",
			wantOK:  true,
		},
		{
			name:    "double-dot only",
			rawName: "../..",
			wantOK:  false,
		},
		{
			name:    "empty path",
			rawName: "",
			wantOK:  false,
		},
		{
			name:    "slash only",
			rawName: "/",
			wantOK:  false,
		},
		{
			// Same stack-clamp behavior: backslash normalized, dots dropped, result inside root.
			name:    "backslash traversal resolves inside root",
			rawName: `..\..\Windows\System32`,
			wantOK:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolvePathPKGDest(tc.rawName, root)
			if tc.wantOK && got == "" {
				t.Errorf("resolvePathPKGDest(%q, root) = empty, expected a valid path", tc.rawName)
			}
			if !tc.wantOK && got != "" {
				t.Errorf("resolvePathPKGDest(%q, root) = %q, expected empty (unsafe)", tc.rawName, got)
			}
		})
	}
}

func TestResolveNormalPKGDest(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dest")
	tests := []struct {
		name    string
		titleID string
		wantOK  bool
	}{
		{name: "USRDIR/EBOOT.BIN", titleID: "BCUS98114", wantOK: true},
		{name: "USRDIR/../PARAM.SFO", titleID: "BCUS98114", wantOK: true},
		{name: "USRDIR/../../outside", titleID: "BCUS98114", wantOK: false},
		{name: `USRDIR\\..\\..\\outside`, titleID: "BCUS98114", wantOK: false},
		{name: "PARAM.SFO", titleID: "../outside", wantOK: false},
	}

	for _, tc := range tests {
		got := resolveNormalPKGDest(tc.name, root, tc.titleID)
		if (got != "") != tc.wantOK {
			t.Errorf("resolveNormalPKGDest(%q, %q) = %q, wantOK=%t", tc.name, tc.titleID, got, tc.wantOK)
		}
	}
}

// TestParseVersion verifies the version-string parser.
func TestParseVersion(t *testing.T) {
	cases := []struct {
		in   string
		want [2]int
	}{
		{"01.05", [2]int{1, 5}},
		{"01.00", [2]int{1, 0}},
		{"02.10", [2]int{2, 10}},
		{"0.00", [2]int{0, 0}},
		{"", [2]int{0, 0}},
		{"1", [2]int{1, 0}},
	}

	for _, tc := range cases {
		got := parseVersion(tc.in)
		if got != tc.want {
			t.Errorf("parseVersion(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestOrderForBatchInstall confirms grouping, sorting, and ordering.
func TestOrderForBatchInstall(t *testing.T) {
	pkgs := []DiscoveredPKG{
		{Path: "a.pkg", TitleID: "BCUS98114", AppVer: "01.05"},
		{Path: "b.pkg", TitleID: "BCUS98114", AppVer: "01.02"},
		{Path: "c.pkg", TitleID: "BCUS98114", AppVer: "01.00"},
		{Path: "d.pkg", TitleID: "NPEB00301", AppVer: "01.01"},
		{Path: "e.pkg", TitleID: "NPEB00301", AppVer: "01.00"},
	}

	groups := OrderForBatchInstall(pkgs)

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	// First group should be BCUS98114 (sorted before NPEB00301).
	g0 := groups[0]
	if len(g0) != 3 {
		t.Fatalf("group 0: expected 3 items, got %d", len(g0))
	}
	// Should be sorted ascending: 01.00, 01.02, 01.05.
	if g0[0].AppVer != "01.00" || g0[1].AppVer != "01.02" || g0[2].AppVer != "01.05" {
		t.Errorf("group 0 order: got %v %v %v, want 01.00 01.02 01.05",
			g0[0].AppVer, g0[1].AppVer, g0[2].AppVer)
	}

	// Second group should be NPEB00301.
	g1 := groups[1]
	if len(g1) != 2 {
		t.Fatalf("group 1: expected 2 items, got %d", len(g1))
	}
	if g1[0].AppVer != "01.00" || g1[1].AppVer != "01.01" {
		t.Errorf("group 1 order: got %v %v, want 01.00 01.01",
			g1[0].AppVer, g1[1].AppVer)
	}
}

// TestOrderForBatchInstall_TitleIDSort verifies that title IDs are sorted lexicographically.
func TestOrderForBatchInstall_TitleIDSort(t *testing.T) {
	pkgs := []DiscoveredPKG{
		{Path: "z.pkg", TitleID: "ZZZZZ99999", AppVer: "01.00"},
		{Path: "a.pkg", TitleID: "AAAAA00001", AppVer: "01.00"},
	}

	groups := OrderForBatchInstall(pkgs)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if groups[0][0].TitleID != "AAAAA00001" {
		t.Errorf("expected AAAAA00001 first, got %s", groups[0][0].TitleID)
	}
	if groups[1][0].TitleID != "ZZZZZ99999" {
		t.Errorf("expected ZZZZZ99999 second, got %s", groups[1][0].TitleID)
	}
}
