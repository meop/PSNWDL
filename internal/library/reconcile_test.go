package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"PSNWDL/internal/psn"
	"PSNWDL/internal/rpcs3"
)

type fakePSN struct {
	titles map[string]*psn.Title
	errors map[string]error
}

func (f *fakePSN) LookupPS3(_ context.Context, tid string) (*psn.Title, error) {
	if err, ok := f.errors[tid]; ok {
		return nil, err
	}
	if t, ok := f.titles[tid]; ok {
		return t, nil
	}
	return &psn.Title{ID: tid}, nil
}

func setHomeDir(t *testing.T, tmpDir string) {
	t.Helper()
	t.Setenv("HOME", tmpDir)
	t.Setenv("USERPROFILE", tmpDir)
}

func writeCachedPKG(t *testing.T, home, tid, version string) {
	t.Helper()
	dir := filepath.Join(home, ".psnwdl", "download", "ps3", "updates", tid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, tid+"_"+version+".pkg")
	if err := os.WriteFile(name, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReconcilePS3(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)

	// BCUS98114: have v01.05 locally, server has v01.05 → up_to_date
	writeCachedPKG(t, home, "BCUS98114", "01.05")
	// BLES01234: have v01.00, server has v01.08 → update_available
	writeCachedPKG(t, home, "BLES01234", "01.00")
	// NPEB00301: nothing cached, server has v01.00 → missing_all
	// NPUB30528: server unreachable
	// NPHA80100: server has no updates → no_updates

	fake := &fakePSN{
		titles: map[string]*psn.Title{
			"BCUS98114": {ID: "BCUS98114", Name: "Gran Turismo 5", Updates: []psn.Update{
				{Version: "01.04"}, {Version: "01.05"},
			}},
			"BLES01234": {ID: "BLES01234", Name: "Demon's Souls", Updates: []psn.Update{
				{Version: "01.00"}, {Version: "01.08"},
			}},
			"NPEB00301": {ID: "NPEB00301", Updates: []psn.Update{
				{Version: "01.00"},
			}},
			"NPHA80100": {ID: "NPHA80100", Updates: nil},
		},
		errors: map[string]error{
			"NPUB30528": errors.New("connection refused"),
		},
	}

	entries := []rpcs3.Entry{
		{TitleID: "BCUS98114", InstallDir: "/g/gt5"},
		{TitleID: "BLES01234", InstallDir: "/g/ds"},
		{TitleID: "NPEB00301", InstallDir: "/g/echo"},
		{TitleID: "NPUB30528", InstallDir: "/g/wipeout"},
		{TitleID: "NPHA80100", InstallDir: "/g/blank"},
	}

	rows := ReconcilePS3WithHDD0(context.Background(), entries, fake, "")
	if len(rows) != 5 {
		t.Fatalf("got %d rows, want 5", len(rows))
	}

	want := map[string]Status{
		"BCUS98114": StatusUpToDate,
		"BLES01234": StatusUpdateAvailable,
		"NPEB00301": StatusMissingAll,
		"NPUB30528": StatusUnreachable,
		"NPHA80100": StatusNoUpdates,
	}
	for _, r := range rows {
		if r.Status != want[r.TitleID] {
			t.Errorf("%s status = %s, want %s", r.TitleID, r.Status, want[r.TitleID])
		}
	}
}

func TestReconcilePS3_Empty(t *testing.T) {
	rows := ReconcilePS3WithHDD0(context.Background(), nil, &fakePSN{}, "")
	if len(rows) != 0 {
		t.Errorf("expected no rows, got %d", len(rows))
	}
}

func TestHighestCachedVersion(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)

	writeCachedPKG(t, home, "BCUS98114", "01.00")
	writeCachedPKG(t, home, "BCUS98114", "01.13")
	writeCachedPKG(t, home, "BCUS98114", "01.05")

	got, err := highestCachedVersion("ps3", "BCUS98114")
	if err != nil {
		t.Fatal(err)
	}
	if got != "01.13" {
		t.Errorf("got %q, want 01.13", got)
	}
}

func TestHighestCachedVersion_NoDir(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)

	got, err := highestCachedVersion("ps3", "BCUS00000")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("got %q, want empty for missing dir", got)
	}
}

func TestCompareVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"01.05", "01.05", 0},
		{"01.04", "01.05", -1},
		{"01.13", "01.05", 1},
		{"", "01.00", -1},
		{"01.00", "", 1},
		{"", "", 0},
		{"02.00", "01.99", 1},
	}
	for _, tc := range cases {
		got := compareVersion(tc.a, tc.b)
		if (got < 0 && tc.want >= 0) || (got > 0 && tc.want <= 0) || (got == 0 && tc.want != 0) {
			t.Errorf("compareVersion(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
