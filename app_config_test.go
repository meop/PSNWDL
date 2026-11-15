package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"PSNWDL/internal/activity"
	"PSNWDL/internal/config"
)

// startTestApp runs the real service startup against a config file in a
// temporary home, with a real (not running) Wails application for events.
func startTestApp(t *testing.T, initial string) (*App, string) {
	t.Helper()
	return startTestAppWith(t, initial, nil)
}

// startTestAppWith is startTestApp with fake standing in for Sony's
// endpoints; a nil fake keeps the real PSN client.
func startTestAppWith(t *testing.T, initial string, fake psnClient) (*App, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfgPath := filepath.Join(home, ".psnwdl", "config.toml")
	t.Setenv("PSNWDL_CONFIG", cfgPath)
	if initial != "" {
		if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cfgPath, []byte(initial), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	app := NewApp(application.New(application.Options{Name: "PSNWDL test"}))
	if fake != nil {
		app.newPSN = func(config.Network, *activity.Sink) psnClient { return fake }
	}
	if err := app.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil {
		t.Fatalf("ServiceStartup: %v", err)
	}
	t.Cleanup(func() { _ = app.ServiceShutdown() })
	if app.ConfigFilePath() != cfgPath {
		t.Fatalf("ConfigFilePath = %q, want %q", app.ConfigFilePath(), cfgPath)
	}
	return app, cfgPath
}

func makeRPCS3Paths(t *testing.T) (gamesYML, hdd0Game string) {
	t.Helper()
	root := t.TempDir()
	gamesYML = filepath.Join(root, "config", "games.yml")
	hdd0Game = filepath.Join(root, "dev_hdd0", "game")
	if err := os.MkdirAll(filepath.Dir(gamesYML), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gamesYML, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(hdd0Game, 0o755); err != nil {
		t.Fatal(err)
	}
	return gamesYML, hdd0Game
}

func reload(t *testing.T, path string) *config.Config {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestSaveConfigPersistsEverySetting(t *testing.T) {
	app, cfgPath := startTestApp(t, "")
	gamesYML, hdd0Game := makeRPCS3Paths(t)
	libraryDir := t.TempDir()

	next := app.GetConfig()
	next.RPCS3.GamesYML = gamesYML
	next.RPCS3.HDD0Game = hdd0Game
	next.Storage.LibraryDir = libraryDir
	next.Network.ParallelDownloads = 9
	next.Network.Retries = 7
	next.Network.TimeoutSeconds = 42
	next.Network.VerifyTLS = true
	next.UI.Theme = "light"
	next.UI.DefaultMode = "ps4"
	next.UI.DefaultDownload = "title"
	if err := app.SaveConfig(&next); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	got := reload(t, cfgPath)
	got.HomeDir = next.HomeDir
	if *got != next {
		t.Fatalf("reloaded config = %+v\nwant %+v", *got, next)
	}
	if app.GetConfig() != next {
		t.Fatalf("GetConfig after save = %+v\nwant %+v", app.GetConfig(), next)
	}
}

// A stored RPCS3 path that no longer exists (RPCS3 moved or uninstalled) must
// not block fixing the other path or saving unrelated settings.
func TestSaveConfigSucceedsWithStaleStoredRPCS3Paths(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")
	app, cfgPath := startTestApp(t, "[rpcs3]\n"+
		"games_yml = '"+filepath.Join(gone, "config", "games.yml")+"'\n"+
		"hdd0_game = '"+filepath.Join(gone, "dev_hdd0", "game")+"'\n")
	gamesYML, hdd0Game := makeRPCS3Paths(t)

	next := app.GetConfig()
	next.UI.Theme = "dark"
	if err := app.SaveConfig(&next); err != nil {
		t.Fatalf("unrelated setting blocked by stale paths: %v", err)
	}

	next = app.GetConfig()
	next.RPCS3.HDD0Game = hdd0Game
	if err := app.SaveConfig(&next); err != nil {
		t.Fatalf("fixing hdd0_game blocked by stale games_yml: %v", err)
	}

	next = app.GetConfig()
	next.RPCS3.GamesYML = gamesYML
	if err := app.SaveConfig(&next); err != nil {
		t.Fatalf("fixing games_yml: %v", err)
	}

	got := reload(t, cfgPath)
	if got.UI.Theme != "dark" || got.RPCS3.GamesYML != gamesYML || got.RPCS3.HDD0Game != hdd0Game {
		t.Fatalf("reloaded config = %+v", *got)
	}
}

func TestSaveConfigRejectsInvalidChangedPaths(t *testing.T) {
	app, cfgPath := startTestApp(t, "")
	missing := filepath.Join(t.TempDir(), "missing")

	for name, mutate := range map[string]func(*config.Config){
		"games_yml":   func(c *config.Config) { c.RPCS3.GamesYML = filepath.Join(missing, "games.yml") },
		"hdd0_game":   func(c *config.Config) { c.RPCS3.HDD0Game = filepath.Join(missing, "dev_hdd0", "game") },
		"library_dir": func(c *config.Config) { c.Storage.LibraryDir = missing },
	} {
		t.Run(name, func(t *testing.T) {
			before := app.GetConfig()
			next := before
			mutate(&next)
			if err := app.SaveConfig(&next); err == nil {
				t.Fatal("invalid path was saved")
			}
			if app.GetConfig() != before {
				t.Fatal("in-memory config changed after a rejected save")
			}
			if got := reload(t, cfgPath); got.RPCS3 != before.RPCS3 || got.Storage != before.Storage {
				t.Fatalf("config file changed after a rejected save: %+v", *got)
			}
		})
	}
}

// An edit to config.toml made outside the app is applied like a save from
// Settings; the app's own saves change nothing further.
func TestEditedConfigFileIsApplied(t *testing.T) {
	app, cfgPath := startTestApp(t, "")
	libraryDir := filepath.Join(t.TempDir(), "library")

	edited := app.GetConfig()
	edited.Storage.LibraryDir = libraryDir
	edited.Network.ParallelDownloads = 2
	if err := config.Save(cfgPath, &edited); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for app.GetConfig().Network.ParallelDownloads != 2 {
		if time.Now().After(deadline) {
			t.Fatal("edited config.toml was not applied")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := app.GetConfig().Storage.LibraryDir; got != libraryDir {
		t.Fatalf("library_dir = %q, want %q", got, libraryDir)
	}
	if _, err := os.Stat(libraryDir); err != nil {
		t.Fatalf("edited library_dir was not created: %v", err)
	}
	if !hasActivity(app, "library", "Applied settings changed in") {
		t.Fatal("applying the edit was not narrated")
	}
}

func TestReloadConfigFileIgnoresBadAndUnchangedFiles(t *testing.T) {
	app, cfgPath := startTestApp(t, "")
	before := app.GetConfig()

	app.reloadConfigFile()
	if hasActivity(app, "library", "Applied settings") {
		t.Fatal("an unchanged config.toml was re-applied")
	}

	if err := os.WriteFile(cfgPath, []byte("not = [valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.reloadConfigFile()
	if !hasActivity(app, "library", "Ignored an edit to") {
		t.Fatal("a config.toml that does not parse was not reported")
	}
	if app.GetConfig().Storage.LibraryDir != before.Storage.LibraryDir {
		t.Fatal("a config.toml that does not parse changed the running config")
	}

	if err := os.Remove(cfgPath); err != nil {
		t.Fatal(err)
	}
	app.reloadConfigFile()
	if _, err := os.Stat(cfgPath); err == nil {
		t.Fatal("a deleted config.toml was rewritten with defaults")
	}
}
