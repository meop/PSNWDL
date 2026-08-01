package rpcs3

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseGamesYML_Happy(t *testing.T) {
	yml := `BCUS98114: "/home/me/Games/PS3/Gran Turismo 5/"
BLES01234: "/home/me/Games/PS3/Demon's Souls/"
NPEB00301: /home/me/Games/PS3/Echochrome/
`
	entries, err := parseGamesYMLBytes([]byte(yml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	// Sorted by title ID
	wantOrder := []string{"BCUS98114", "BLES01234", "NPEB00301"}
	for i, want := range wantOrder {
		if entries[i].TitleID != want {
			t.Errorf("[%d] TitleID = %q, want %q", i, entries[i].TitleID, want)
		}
	}
	if entries[0].InstallDir != "/home/me/Games/PS3/Gran Turismo 5/" {
		t.Errorf("InstallDir = %q", entries[0].InstallDir)
	}
}

func TestParseGamesYML_Empty(t *testing.T) {
	entries, err := parseGamesYMLBytes([]byte{})
	if err != nil {
		t.Fatalf("parse empty: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries from empty input, want 0", len(entries))
	}
}

func TestParseGamesYML_Comments(t *testing.T) {
	yml := `# RPCS3 games.yml
BCUS98114: /games/gt5/
# another comment
BLES01234: /games/demons-souls/
`
	entries, err := parseGamesYMLBytes([]byte(yml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("got %d entries, want 2", len(entries))
	}
}

func TestParseGamesYML_Malformed(t *testing.T) {
	yml := "BCUS98114: [not a string"
	_, err := parseGamesYMLBytes([]byte(yml))
	if err == nil {
		t.Error("expected parse error, got nil")
	}
}

func TestParseGamesYML_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "games.yml")
	if err := os.WriteFile(path, []byte("BCUS98114: /games/gt5/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := ParseGamesYML(path)
	if err != nil {
		t.Fatalf("ParseGamesYML: %v", err)
	}
	if len(entries) != 1 || entries[0].TitleID != "BCUS98114" {
		t.Errorf("unexpected entries: %+v", entries)
	}
}

func TestFindGamesYML_NotFound(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("APPDATA", filepath.Join(tmp, "AppData", "Roaming"))

	if got := FindGamesYML(); got != "" {
		t.Errorf("FindGamesYML in empty dir = %q, want empty", got)
	}
}

func TestDefaultGamesYMLPaths_NonEmpty(t *testing.T) {
	if len(DefaultGamesYMLPaths()) == 0 {
		t.Error("DefaultGamesYMLPaths returned no paths for this OS")
	}
}
