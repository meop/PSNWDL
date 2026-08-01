package library

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	stdsync "sync"

	"PSNWDL/internal/config"
	"PSNWDL/internal/psn"
	"PSNWDL/internal/rpcs3"
)

const reconcileConcurrency = 6

type Status string

const (
	StatusChecking       Status = "checking"
	StatusNoneDownloaded Status = "none_downloaded"
	StatusSomeDownloaded Status = "some_downloaded"
	StatusAllDownloaded  Status = "all_downloaded"
	StatusNone           Status = "none"
	StatusUnreachable    Status = "unreachable"
)

// Row describes how completely one RPCS3 title's server update set exists in
// the download library. Installation state is deliberately not part of this
// view; the Emulator pane owns synchronization, while Library owns stored files.
type Row struct {
	TitleID         string       `json:"title_id"`
	Name            string       `json:"name,omitempty"`
	InstallDir      string       `json:"install_dir"`
	Status          Status       `json:"status"`
	DownloadedCount int          `json:"downloaded_count"`
	UpdateCount     int          `json:"update_count"`
	Updates         []psn.Update `json:"updates,omitempty"`
	Error           string       `json:"error,omitempty"`
}

// SyncPlan is the exact difference between the server-advertised package set
// and one local title folder.
type SyncPlan struct {
	Missing         []psn.Update
	Extras          []string
	DownloadedCount int
	UpdateCount     int
}

// PSNLookup is the subset of psn.Client used by reconciliation.
type PSNLookup interface {
	LookupPS3(ctx context.Context, tid string) (*psn.Title, error)
}

func ReconcilePS3(ctx context.Context, entries []rpcs3.Entry, client PSNLookup, libraryRoot string) []Row {
	if len(entries) == 0 {
		return []Row{}
	}

	rows := make([]Row, len(entries))
	sem := make(chan struct{}, reconcileConcurrency)
	var wg stdsync.WaitGroup

	for i, entry := range entries {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, entry rpcs3.Entry) {
			defer wg.Done()
			defer func() { <-sem }()
			rows[i] = reconcileOne(ctx, entry, client, libraryRoot)
		}(i, entry)
	}
	wg.Wait()
	return rows
}

func ReconcileTitlePS3(ctx context.Context, entry rpcs3.Entry, client PSNLookup, libraryRoot string) Row {
	return reconcileOne(ctx, entry, client, libraryRoot)
}

func reconcileOne(ctx context.Context, entry rpcs3.Entry, client PSNLookup, libraryRoot string) Row {
	row := Row{TitleID: entry.TitleID, InstallDir: entry.InstallDir}
	title, err := client.LookupPS3(ctx, entry.TitleID)
	if err != nil {
		row.Status = StatusUnreachable
		row.Error = err.Error()
		return row
	}

	row.Name = title.Name
	row.Updates = title.Updates
	plan, err := PlanTitleSync(libraryRoot, "ps3", entry.TitleID, title.Updates)
	if err != nil {
		row.Status = StatusUnreachable
		row.Error = err.Error()
		return row
	}
	row.DownloadedCount = plan.DownloadedCount
	row.UpdateCount = plan.UpdateCount
	row.Status = statusForCounts(plan.DownloadedCount, plan.UpdateCount)
	return row
}

func statusForCounts(downloaded, total int) Status {
	switch {
	case total == 0:
		return StatusNone
	case downloaded == 0:
		return StatusNoneDownloaded
	case downloaded < total:
		return StatusSomeDownloaded
	default:
		return StatusAllDownloaded
	}
}

// PlanTitleSync compares exact expected filenames. A high-version file does
// not hide a missing lower update, and unexpected files/directories are
// returned for deletion by the caller.
func PlanTitleSync(libraryRoot, mode, titleID string, updates []psn.Update) (SyncPlan, error) {
	titleRoot, err := config.TitleDirForRoot(libraryRoot, mode)
	if err != nil {
		return SyncPlan{}, err
	}
	titleDir := filepath.Join(titleRoot, titleID)

	expected := make(map[string]psn.Update, len(updates))
	order := make([]string, 0, len(updates))
	for _, update := range updates {
		name := expectedPackageName(titleID, update.Version)
		if _, exists := expected[name]; exists {
			continue
		}
		expected[name] = update
		order = append(order, name)
	}

	plan := SyncPlan{UpdateCount: len(expected)}
	present := make(map[string]bool, len(expected))
	entries, err := os.ReadDir(titleDir)
	if err != nil && !os.IsNotExist(err) {
		return SyncPlan{}, fmt.Errorf("read title library %s: %w", titleDir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if update, exists := expected[name]; exists && !entry.IsDir() {
			info, infoErr := entry.Info()
			if infoErr != nil {
				return SyncPlan{}, fmt.Errorf("inspect title package %s: %w", filepath.Join(titleDir, name), infoErr)
			}
			if update.Size <= 0 || info.Size() == update.Size {
				present[name] = true
				continue
			}
		}
		plan.Extras = append(plan.Extras, filepath.Join(titleDir, name))
	}

	for _, name := range order {
		if present[name] {
			plan.DownloadedCount++
			continue
		}
		plan.Missing = append(plan.Missing, expected[name])
	}
	return plan, nil
}

func ExtraTitleFolders(libraryRoot, mode string, allowedTitleIDs []string) ([]string, error) {
	titleRoot, err := config.TitleDirForRoot(libraryRoot, mode)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(titleRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("read title library %s: %w", titleRoot, err)
	}
	allowed := make(map[string]struct{}, len(allowedTitleIDs))
	for _, titleID := range allowedTitleIDs {
		allowed[titleID] = struct{}{}
	}
	extras := []string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, keep := allowed[entry.Name()]; !keep {
			extras = append(extras, filepath.Join(titleRoot, entry.Name()))
		}
	}
	return extras, nil
}

func expectedPackageName(titleID, version string) string {
	version = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`\\/:*?"<>|`, r) {
			return '_'
		}
		return r
	}, version)
	return fmt.Sprintf("%s_%s.pkg", titleID, version)
}
