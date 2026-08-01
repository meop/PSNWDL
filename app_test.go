package main

import (
	"os"
	"path/filepath"
	"testing"

	"PSNWDL/internal/jobs"
)

func TestWailsEmitterRefreshesDownloadsOnlyWhenJobCompletes(t *testing.T) {
	doneCalls := 0
	emitter := &wailsEmitter{onJobDone: func() { doneCalls++ }}

	emitter.Emit(jobs.EventJobProgress, jobs.Job{State: jobs.StateDownloading})
	emitter.Emit(jobs.EventJobState, jobs.Job{State: jobs.StateFailed})
	emitter.Emit(jobs.EventJobState, jobs.Job{State: jobs.StateDone})

	if doneCalls != 1 {
		t.Fatalf("completion callback count = %d, want 1", doneCalls)
	}
}

func TestValidateSettingsPathRequiresRPCS3PathNames(t *testing.T) {
	root := t.TempDir()
	gamesYML := filepath.Join(root, "games.yml")
	wrongYML := filepath.Join(root, "titles.yml")
	gameDir := filepath.Join(root, "dev_hdd0", "game")
	wrongDir := filepath.Join(root, "dev_hdd0", "games")

	if err := os.WriteFile(gamesYML, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wrongYML, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wrongDir, 0o755); err != nil {
		t.Fatal(err)
	}

	app := &App{}
	if err := app.ValidateSettingsPath("games_yml", gamesYML); err != nil {
		t.Errorf("valid games.yml rejected: %v", err)
	}
	if err := app.ValidateSettingsPath("hdd0_game", gameDir); err != nil {
		t.Errorf("valid dev_hdd0/game rejected: %v", err)
	}
	if err := app.ValidateSettingsPath("games_yml", wrongYML); err == nil {
		t.Error("existing file not named games.yml was accepted")
	}
	if err := app.ValidateSettingsPath("hdd0_game", wrongDir); err == nil {
		t.Error("existing folder not ending in dev_hdd0/game was accepted")
	}
}
