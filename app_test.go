package main

import (
	"os"
	"path/filepath"
	"sort"
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

func TestPartitionSyncTargetsBlocksActiveDownloads(t *testing.T) {
	root := t.TempDir()
	safeTarget := filepath.Join(root, "ps3", "title", "BCUS98114")
	blockedTarget := filepath.Join(root, "ps3", "title", "BLES01234")
	doneTarget := filepath.Join(root, "ps3", "title", "NPEB00301")

	activeJobs := []jobs.Job{
		{ID: "1", State: jobs.StateDownloading, DestPath: filepath.Join(blockedTarget, "BLES01234_01.00.pkg")},
		{ID: "2", State: jobs.StateDone, DestPath: filepath.Join(doneTarget, "NPEB00301_01.00.pkg")},
	}

	removable, blocked := partitionSyncTargets([]string{safeTarget, blockedTarget, doneTarget}, activeJobs)

	sort.Strings(removable)
	sort.Strings(blocked)
	wantRemovable := []string{safeTarget, doneTarget}
	wantBlocked := []string{blockedTarget}
	if len(removable) != len(wantRemovable) || removable[0] != wantRemovable[0] || removable[1] != wantRemovable[1] {
		t.Errorf("removable = %v, want %v", removable, wantRemovable)
	}
	if len(blocked) != len(wantBlocked) || blocked[0] != wantBlocked[0] {
		t.Errorf("blocked = %v, want %v", blocked, wantBlocked)
	}
}

func TestPartitionSyncTargetsIgnoresTerminalJobStates(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "ps3", "title", "BCUS98114")

	for _, state := range []jobs.JobState{jobs.StateDone, jobs.StateFailed, jobs.StateCanceled} {
		activeJobs := []jobs.Job{{ID: "1", State: state, DestPath: filepath.Join(target, "BCUS98114_01.00.pkg")}}
		removable, blocked := partitionSyncTargets([]string{target}, activeJobs)
		if len(blocked) != 0 || len(removable) != 1 || removable[0] != target {
			t.Errorf("state=%s: removable=%v blocked=%v, want target removable and nothing blocked", state, removable, blocked)
		}
	}
}
