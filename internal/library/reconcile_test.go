package library

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"PSNWDL/internal/psn"
	"PSNWDL/internal/rpcs3"
)

// buildTestSFO constructs a minimal PARAM.SFO binary containing TITLE_ID and
// TITLE, matching the layout internal/pkg.ParseSFO expects.
func buildTestSFO(t *testing.T, titleID, title string) []byte {
	t.Helper()
	entries := []struct{ k, v string }{{"TITLE", title}, {"TITLE_ID", titleID}}

	headerSize := 0x14
	entryAreaSize := len(entries) * 0x10
	keyTableStart := headerSize + entryAreaSize
	keyTableSize := 0
	for _, e := range entries {
		keyTableSize += len(e.k) + 1
	}
	dataTableStart := keyTableStart + keyTableSize

	type valInfo struct{ off, size int }
	valInfos := make([]valInfo, len(entries))
	off := 0
	for i, e := range entries {
		valInfos[i] = valInfo{off: off, size: len(e.v) + 1}
		off += len(e.v) + 1
	}

	buf := make([]byte, dataTableStart+off)
	copy(buf[0:4], []byte{0x00, 'P', 'S', 'F'})
	binary.LittleEndian.PutUint32(buf[0x08:0x0C], uint32(keyTableStart))
	binary.LittleEndian.PutUint32(buf[0x0C:0x10], uint32(dataTableStart))
	binary.LittleEndian.PutUint32(buf[0x10:0x14], uint32(len(entries)))

	keyPos := 0
	for i, e := range entries {
		entryOff := 0x14 + i*0x10
		binary.LittleEndian.PutUint16(buf[entryOff:], uint16(keyPos))
		binary.LittleEndian.PutUint16(buf[entryOff+2:], 0x0204)
		binary.LittleEndian.PutUint32(buf[entryOff+4:], uint32(valInfos[i].size))
		binary.LittleEndian.PutUint32(buf[entryOff+8:], uint32(valInfos[i].size))
		binary.LittleEndian.PutUint32(buf[entryOff+12:], uint32(valInfos[i].off))

		copy(buf[keyTableStart+keyPos:], e.k)
		buf[keyTableStart+keyPos+len(e.k)] = 0
		keyPos += len(e.k) + 1

		copy(buf[dataTableStart+valInfos[i].off:], e.v)
	}
	return buf
}

type fakePSN struct {
	titles map[string]*psn.Title
	errors map[string]error
}

func (f *fakePSN) LookupPS3(_ context.Context, tid string) (*psn.Title, error) {
	if err, ok := f.errors[tid]; ok {
		return nil, err
	}
	if title, ok := f.titles[tid]; ok {
		return title, nil
	}
	return &psn.Title{ID: tid}, nil
}

func writeCachedPKG(t *testing.T, root, tid, version string) string {
	t.Helper()
	dir := filepath.Join(root, "ps3", "title", tid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, tid+"_"+version+".pkg")
	if err := os.WriteFile(path, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReconcilePS3UsesExactDownloadedCounts(t *testing.T) {
	root := t.TempDir()
	writeCachedPKG(t, root, "BCUS98114", "01.05")
	writeCachedPKG(t, root, "BLES01234", "01.00")

	fake := &fakePSN{
		titles: map[string]*psn.Title{
			"BCUS98114": {ID: "BCUS98114", Name: "Gran Turismo 5", Updates: []psn.Update{
				{Version: "01.05"},
			}},
			"BLES01234": {ID: "BLES01234", Name: "Demon's Souls", Updates: []psn.Update{
				{Version: "01.00"}, {Version: "01.08"},
			}},
			"NPEB00301": {ID: "NPEB00301", Updates: []psn.Update{{Version: "01.00"}}},
			"NPHA80100": {ID: "NPHA80100"},
		},
		errors: map[string]error{"NPUB30528": errors.New("connection refused")},
	}

	entries := []rpcs3.Entry{
		{TitleID: "BCUS98114"},
		{TitleID: "BLES01234"},
		{TitleID: "NPEB00301"},
		{TitleID: "NPUB30528"},
		{TitleID: "NPHA80100"},
	}
	rows := ReconcilePS3(context.Background(), entries, fake, root, "")
	want := map[string]Status{
		"BCUS98114": StatusAllDownloaded,
		"BLES01234": StatusSomeDownloaded,
		"NPEB00301": StatusNoneDownloaded,
		"NPUB30528": StatusUnreachable,
		"NPHA80100": StatusNone,
	}
	for _, row := range rows {
		if row.Status != want[row.TitleID] {
			t.Errorf("%s status = %s, want %s", row.TitleID, row.Status, want[row.TitleID])
		}
	}
}

func TestReconcileOneUsesLocalSFONameOverNetwork(t *testing.T) {
	root := t.TempDir()
	installDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(installDir, "PARAM.SFO"), buildTestSFO(t, "NPHA80100", "Local Disc Title"), 0o644); err != nil {
		t.Fatal(err)
	}

	fake := &fakePSN{titles: map[string]*psn.Title{
		"NPHA80100": {ID: "NPHA80100", Name: "Network Title"},
	}}

	row := reconcileOne(context.Background(), rpcs3.Entry{TitleID: "NPHA80100", InstallDir: installDir}, fake, root, "")
	if row.Name != "Local Disc Title" {
		t.Fatalf("Name = %q, want local SFO title to win", row.Name)
	}
}

// TestReconcileOneLeavesNameEmptyWithoutLocalSFO asserts we deliberately do
// NOT fall back to the PSN-supplied name: RPCS3 has no web lookup, so a title
// it can't label locally should render the same way here (falling back to
// the title ID in the UI), not diverge by picking up a name RPCS3 itself
// never shows.
func TestReconcileOneLeavesNameEmptyWithoutLocalSFO(t *testing.T) {
	root := t.TempDir()
	fake := &fakePSN{titles: map[string]*psn.Title{
		"NPHA80100": {ID: "NPHA80100", Name: "Network Title"},
	}}

	row := reconcileOne(context.Background(), rpcs3.Entry{TitleID: "NPHA80100"}, fake, root, "")
	if row.Name != "" {
		t.Fatalf("Name = %q, want empty (no local SFO to source a name from)", row.Name)
	}
}

func TestReconcileOneUsesLocalSFONameWhenNoUpdatesPublished(t *testing.T) {
	root := t.TempDir()
	hdd0Game := t.TempDir()
	titleDir := filepath.Join(hdd0Game, "NPHA80100")
	if err := os.MkdirAll(titleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(titleDir, "PARAM.SFO"), buildTestSFO(t, "NPHA80100", "Installed Title"), 0o644); err != nil {
		t.Fatal(err)
	}

	// No entry for NPHA80100 in fake.titles: LookupPS3 returns an empty
	// Title (mirroring the real client's response for titles with no
	// published updates), same as fakePSN's default fallback.
	fake := &fakePSN{}

	row := reconcileOne(context.Background(), rpcs3.Entry{TitleID: "NPHA80100"}, fake, root, hdd0Game)
	if row.Name != "Installed Title" {
		t.Fatalf("Name = %q, want name from dev_hdd0/game PARAM.SFO", row.Name)
	}
}

func TestReconcileOneMissingReflectsInstallDirExistence(t *testing.T) {
	root := t.TempDir()
	installDir := t.TempDir()
	fake := &fakePSN{}

	present := reconcileOne(context.Background(), rpcs3.Entry{TitleID: "NPHA80100", InstallDir: installDir}, fake, root, "")
	if present.Missing {
		t.Errorf("Missing = true for an install_dir that exists, want false")
	}

	gone := reconcileOne(context.Background(), rpcs3.Entry{TitleID: "NPHA80100", InstallDir: filepath.Join(installDir, "deleted")}, fake, root, "")
	if !gone.Missing {
		t.Errorf("Missing = false for an install_dir that doesn't exist, want true")
	}

	empty := reconcileOne(context.Background(), rpcs3.Entry{TitleID: "NPHA80100"}, fake, root, "")
	if !empty.Missing {
		t.Errorf("Missing = false for an empty install_dir, want true")
	}
}

func TestPlanTitleSyncFindsGapsAndExtras(t *testing.T) {
	root := t.TempDir()
	tid := "BCUS98114"
	writeCachedPKG(t, root, tid, "01.02")
	extra := writeCachedPKG(t, root, tid, "99.99")
	updates := []psn.Update{
		{Version: "01.01", URL: "https://example.com/1.pkg"},
		{Version: "01.02", URL: "https://example.com/2.pkg"},
		{Version: "01.03", URL: "https://example.com/3.pkg"},
	}

	plan, err := PlanTitleSync(root, "ps3", tid, updates)
	if err != nil {
		t.Fatalf("PlanTitleSync: %v", err)
	}
	if plan.DownloadedCount != 1 || plan.UpdateCount != 3 {
		t.Fatalf("counts = %d/%d, want 1/3", plan.DownloadedCount, plan.UpdateCount)
	}
	if len(plan.Missing) != 2 || plan.Missing[0].Version != "01.01" || plan.Missing[1].Version != "01.03" {
		t.Fatalf("missing = %+v", plan.Missing)
	}
	if len(plan.Extras) != 1 || plan.Extras[0] != extra {
		t.Fatalf("extras = %+v, want %s", plan.Extras, extra)
	}
}

func TestPlanTitleSyncReplacesWrongSizePackage(t *testing.T) {
	root := t.TempDir()
	tid := "BCUS98114"
	path := writeCachedPKG(t, root, tid, "01.00")
	plan, err := PlanTitleSync(root, "ps3", tid, []psn.Update{
		{Version: "01.00", Size: 99, URL: "https://example.com/update.pkg"},
	})
	if err != nil {
		t.Fatalf("PlanTitleSync: %v", err)
	}
	if len(plan.Missing) != 1 || len(plan.Extras) != 1 || plan.Extras[0] != path {
		t.Fatalf("plan = %+v, want package marked missing and extra", plan)
	}
}

func TestStatusForCounts(t *testing.T) {
	tests := []struct {
		downloaded int
		total      int
		want       Status
	}{
		{0, 0, StatusNone},
		{0, 3, StatusNoneDownloaded},
		{1, 3, StatusSomeDownloaded},
		{3, 3, StatusAllDownloaded},
	}
	for _, test := range tests {
		if got := statusForCounts(test.downloaded, test.total); got != test.want {
			t.Errorf("statusForCounts(%d, %d) = %s, want %s", test.downloaded, test.total, got, test.want)
		}
	}
}

func TestExtraTitleFolders(t *testing.T) {
	root := t.TempDir()
	for _, titleID := range []string{"BCUS98114", "BLES01234"} {
		if err := os.MkdirAll(filepath.Join(root, "ps3", "title", titleID), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	extras, err := ExtraTitleFolders(root, "ps3", []string{"BCUS98114"})
	if err != nil {
		t.Fatalf("ExtraTitleFolders: %v", err)
	}
	want := filepath.Join(root, "ps3", "title", "BLES01234")
	if len(extras) != 1 || extras[0] != want {
		t.Fatalf("extras = %+v, want %s", extras, want)
	}
}
