package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"PSNWDL/internal/activity"
	"PSNWDL/internal/config"
	"PSNWDL/internal/downloads"
	"PSNWDL/internal/jobs"
	"PSNWDL/internal/library"
	"PSNWDL/internal/pkg"
	"PSNWDL/internal/psn"
	"PSNWDL/internal/rpcs3"
)

type App struct {
	ctx      context.Context
	wailsApp *application.App
	cfg      *config.Config
	cfgPath  string
	psn      *psn.Client
	jobs     *jobs.Queue
	activity *activity.Sink
}

func NewApp(wailsApp *application.App) *App {
	return &App{wailsApp: wailsApp}
}

func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.ctx = ctx

	path, err := config.ConfigPath()
	if err != nil {
		log.Printf("config path: %v", err)
		a.cfg = config.Default()
	} else {
		a.cfgPath = path
		cfg, loadErr := config.Load(path)
		if loadErr != nil {
			log.Printf("config load: %v", loadErr)
			a.cfg = config.Default()
		} else {
			a.cfg = cfg
		}
	}

	emitter := &wailsEmitter{
		app: a.wailsApp,
		onJobDone: func() {
			go a.emitDownloadLibrary()
		},
	}
	a.activity = activity.NewSink(emitter)
	if err := ensureConfigLibraryDir(a.cfg); err != nil {
		log.Printf("library dir: %v", err)
	}
	a.psn = psn.NewClient(a.cfg.Network, a.activity)
	a.jobs = jobs.NewQueue(a.cfg.Network, a.cfg.Storage.LibraryDir, emitter, a.activity)
	return nil
}

func (a *App) GetConfig() config.Config {
	if a.cfg == nil {
		return *config.Default()
	}
	return *a.cfg
}

func (a *App) ConfigFilePath() string { return a.cfgPath }

func (a *App) PickDirectory(title, defaultDirectory string) (string, error) {
	if defaultDirectory == "" {
		defaultDirectory = a.cfg.Storage.LibraryDir
	}
	defaultDirectory = existingDirectory(defaultDirectory)
	dialog := a.wailsApp.Dialog.OpenFile().
		SetTitle(title).
		SetDirectory(defaultDirectory).
		CanChooseFiles(false).
		CanChooseDirectories(true)
	if window := a.wailsApp.Window.Current(); window != nil {
		dialog.AttachToWindow(window)
	}
	return dialog.PromptForSingleSelection()
}

func (a *App) PickGamesYML(defaultDirectory string) (string, error) {
	defaultDirectory = existingDirectory(defaultDirectory)
	dialog := a.wailsApp.Dialog.OpenFile().
		SetTitle("Select RPCS3 games.yml").
		SetDirectory(defaultDirectory).
		CanChooseFiles(true).
		CanChooseDirectories(false).
		AddFilter("YAML files (*.yml;*.yaml)", "*.yml;*.yaml").
		AddFilter("All files (*.*)", "*.*")
	if window := a.wailsApp.Window.Current(); window != nil {
		dialog.AttachToWindow(window)
	}
	return dialog.PromptForSingleSelection()
}

func (a *App) ValidateSettingsPath(kind, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("path is required")
	}

	switch kind {
	case "library":
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("path must be an existing library folder")
		}
		if !info.IsDir() {
			return fmt.Errorf("path must be a folder")
		}
		return nil
	case "games_yml":
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("path must be an existing games.yml file")
		}
		if info.IsDir() {
			return fmt.Errorf("path must be a games.yml file")
		}
		return nil
	case "hdd0_game":
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("path must be an existing dev_hdd0/game folder")
		}
		if !info.IsDir() {
			return fmt.Errorf("path must be a folder")
		}
		return nil
	default:
		return fmt.Errorf("unknown path kind %q", kind)
	}
}

func existingDirectory(path string) string {
	if path == "" {
		return ""
	}
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return path
		}
		return filepath.Dir(path)
	}
	for {
		parent := filepath.Dir(path)
		if parent == path || parent == "." {
			return ""
		}
		if info, err := os.Stat(parent); err == nil && info.IsDir() {
			return parent
		}
		path = parent
	}
}

func ensureConfigLibraryDir(cfg *config.Config) error {
	if cfg == nil {
		return nil
	}
	if strings.TrimSpace(cfg.Storage.LibraryDir) == "" {
		libraryDir, err := config.DefaultLibraryDir()
		if err != nil {
			return fmt.Errorf("resolve default library dir: %w", err)
		}
		cfg.Storage.LibraryDir = libraryDir
	}
	if err := os.MkdirAll(cfg.Storage.LibraryDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", cfg.Storage.LibraryDir, err)
	}
	return nil
}

// SaveConfig persists config and applies runtime settings to active clients.
func (a *App) SaveConfig(next *config.Config) error {
	if next == nil {
		return fmt.Errorf("config is nil")
	}
	if a.cfgPath == "" {
		return fmt.Errorf("config path not initialized")
	}
	if next.Storage.LibraryDir == "" {
		libraryDir, err := config.DefaultLibraryDir()
		if err != nil {
			return fmt.Errorf("resolve default library dir: %w", err)
		}
		next.Storage.LibraryDir = libraryDir
	}
	if strings.TrimSpace(next.Storage.LibraryDir) != strings.TrimSpace(a.cfg.Storage.LibraryDir) {
		if err := a.ValidateSettingsPath("library", next.Storage.LibraryDir); err != nil {
			return err
		}
	} else if err := ensureConfigLibraryDir(next); err != nil {
		return err
	}
	if err := config.Save(a.cfgPath, next); err != nil {
		return err
	}
	a.cfg = next
	a.wailsApp.Event.Emit("config:updated", *a.cfg)
	a.psn = psn.NewClient(a.cfg.Network, a.activity)
	a.jobs.SetLibraryDir(a.cfg.Storage.LibraryDir)
	a.jobs.SetNetwork(a.cfg.Network)
	go a.emitDownloadLibrary()
	return nil
}

func (a *App) ActivityLog() []activity.Entry {
	return a.activity.GetEntries()
}

func (a *App) ClearActivityLog() {
	a.activity.Clear()
}

func (a *App) ClearActivityLogScope(scope string) {
	a.activity.ClearScope(scope)
}

func (a *App) SearchPS3(tid string, includeDRMFree bool) (*psn.Title, error) {
	return a.psn.LookupPS3WithDRMFree(a.ctx, tid, includeDRMFree)
}

func (a *App) SearchPS4(tid string) (*psn.Title, error) {
	return a.psn.LookupPS4(a.ctx, tid)
}

func (a *App) SearchVita(tid string) (*psn.Title, error) {
	return a.psn.LookupVita(a.ctx, tid)
}

// ListFirmware fetches the locale-fanned firmware list for the given mode.
// Modes: "ps3" | "ps4" | "ps5" | "psvita".
func (a *App) ListFirmware(mode string) (*psn.FirmwareList, error) {
	switch mode {
	case "ps3":
		return a.psn.LookupPS3Firmware(a.ctx)
	case "ps4":
		return a.psn.LookupPS4Firmware(a.ctx)
	case "ps5":
		return a.psn.LookupPS5Firmware(a.ctx)
	case "psvita":
		return a.psn.LookupVitaFirmware(a.ctx)
	default:
		return nil, fmt.Errorf("unknown mode %q", mode)
	}
}

func (a *App) EnqueueDownload(req jobs.Request) (string, error) {
	return a.jobs.Enqueue(a.ctx, req)
}

func (a *App) CancelJob(id string) error { return a.jobs.Cancel(id) }
func (a *App) PauseJob(id string) error  { return a.jobs.Pause(id) }
func (a *App) ResumeJob(id string) error { return a.jobs.Resume(id) }
func (a *App) RetryJob(id string) error  { return a.jobs.Retry(id) }

func (a *App) ListJobs() []jobs.Job { return a.jobs.List() }

func (a *App) ListDownloadLibrary() ([]downloads.Title, error) {
	return downloads.Scan(a.cfg.Storage.LibraryDir)
}

func (a *App) DeleteLibraryItems(paths []string) error {
	if err := downloads.Delete(a.cfg.Storage.LibraryDir, paths); err != nil {
		return err
	}
	a.activity.Infof("library", "Deleted %d selected library item(s)", len(paths))
	return nil
}

// AutoDetectGamesYML returns the first detected RPCS3 games.yml path on this
// system, or empty string if none of the common locations exist.
func (a *App) AutoDetectGamesYML() string {
	return rpcs3.FindGamesYML()
}

// ListRPCS3Library parses the configured (or auto-detected) games.yml and
// returns one entry per registered title. Returns an empty list when no
// path is configured/found, with an error message in the second return so
// the frontend can render the appropriate empty state.
func (a *App) ListRPCS3Library() ([]rpcs3.Entry, error) {
	path := a.cfg.RPCS3.GamesYML
	if path == "" {
		path = rpcs3.FindGamesYML()
	}
	if path == "" {
		return []rpcs3.Entry{}, fmt.Errorf("games.yml not configured")
	}
	return rpcs3.ParseGamesYML(path)
}

// ReconcileTitlePS3 resolves one already-parsed RPCS3 row so the frontend can
// publish the local games.yml list immediately and fill server state per row.
func (a *App) ReconcileTitlePS3(entry rpcs3.Entry) library.Row {
	row := library.ReconcileTitlePS3(a.ctx, entry, a.psn, a.cfg.Storage.LibraryDir)
	if row.Status == library.StatusUnreachable {
		a.activity.Warnf("library", "%s: reconcile failed (%s)", row.TitleID, row.Error)
	}
	return row
}

// SyncTitlePS3 makes one RPCS3 title folder exactly match the packages
// advertised by the server: extras are removed and missing packages queued.
func (a *App) SyncTitlePS3(tid string) ([]string, error) {
	entries, err := a.ListRPCS3Library()
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.TitleID == tid {
			return a.syncRPCS3Title(entry)
		}
	}
	return nil, fmt.Errorf("title %s not found in RPCS3", tid)
}

// SyncAllPS3 removes title folders not represented in RPCS3, then exactly
// synchronizes every RPCS3 title against the server.
func (a *App) SyncAllPS3() ([]string, error) {
	entries, err := a.ListRPCS3Library()
	if err != nil {
		return nil, err
	}
	if err := a.removeNonRPCS3TitleFolders(entries); err != nil {
		return nil, err
	}

	jobIDs := []string{}
	for _, entry := range entries {
		ids, syncErr := a.syncRPCS3Title(entry)
		if syncErr != nil {
			a.activity.Warnf("library", "%s sync failed: %v", entry.TitleID, syncErr)
			continue
		}
		jobIDs = append(jobIDs, ids...)
	}
	return jobIDs, nil
}

func (a *App) syncRPCS3Title(entry rpcs3.Entry) ([]string, error) {
	if err := psn.ValidateTitleID(entry.TitleID); err != nil {
		return nil, err
	}
	title, err := a.psn.LookupPS3(a.ctx, entry.TitleID)
	if err != nil {
		return nil, err
	}
	plan, err := library.PlanTitleSync(a.cfg.Storage.LibraryDir, "ps3", entry.TitleID, title.Updates)
	if err != nil {
		return nil, err
	}
	removableExtras := a.removableSyncTargets(plan.Extras)
	if len(removableExtras) > 0 {
		if err := downloads.Delete(a.cfg.Storage.LibraryDir, removableExtras); err != nil {
			return nil, err
		}
		a.activity.Infof("library", "%s: removed %d file(s) not advertised by the server", entry.TitleID, len(removableExtras))
		go a.emitDownloadLibrary()
	}

	jobIDs := make([]string, 0, len(plan.Missing))
	for _, update := range plan.Missing {
		jobID, err := a.EnqueueDownload(jobs.Request{
			TitleID:   entry.TitleID,
			TitleName: title.Name,
			Mode:      "ps3",
			Update:    update,
		})
		if err != nil {
			return jobIDs, err
		}
		jobIDs = append(jobIDs, jobID)
	}
	a.activity.Infof("library", "%s: sync queued %d missing package(s)", entry.TitleID, len(jobIDs))
	return jobIDs, nil
}

func (a *App) removeNonRPCS3TitleFolders(entries []rpcs3.Entry) error {
	allowed := make([]string, 0, len(entries))
	for _, entry := range entries {
		allowed = append(allowed, entry.TitleID)
	}
	targets, err := library.ExtraTitleFolders(a.cfg.Storage.LibraryDir, "ps3", allowed)
	if err != nil {
		return err
	}
	targets = a.removableSyncTargets(targets)
	if len(targets) == 0 {
		return nil
	}
	if err := downloads.Delete(a.cfg.Storage.LibraryDir, targets); err != nil {
		return err
	}
	a.activity.Infof("library", "Removed %d PS3 title folder(s) not present in RPCS3", len(targets))
	go a.emitDownloadLibrary()
	return nil
}

func (a *App) removableSyncTargets(targets []string) []string {
	protected := []string{}
	for _, job := range a.jobs.List() {
		switch job.State {
		case jobs.StateQueued, jobs.StateDownloading, jobs.StatePaused, jobs.StateResuming, jobs.StateVerifying:
			protected = append(protected, job.DestPath, job.DestPath+".part")
		}
	}
	removable := make([]string, 0, len(targets))
	for _, target := range targets {
		blocked := false
		for _, activePath := range protected {
			rel, err := filepath.Rel(target, activePath)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
				blocked = true
				break
			}
		}
		if blocked {
			a.activity.Warnf("library", "Skipped removing %s because an active download uses it", target)
			continue
		}
		removable = append(removable, target)
	}
	return removable
}

// OpenFolder opens the given directory in the system's file manager.
// Cross-platform: Explorer on Windows, `open` on macOS, `xdg-open` on Linux.
// `path` may start with ~ (expanded against the user home dir). If the path
// does not exist the OS file manager will surface its own error.
func (a *App) OpenFolder(path string) error {
	absPath := path
	if !filepath.IsAbs(path) {
		if strings.HasPrefix(path, "~") {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home dir: %w", err)
			}
			absPath = filepath.Join(home, path[1:])
		} else {
			// Resolve relative paths against the home dir too, so callers can
			// pass app-relative paths like ".psnwdl/...".
			home, err := os.UserHomeDir()
			if err == nil {
				absPath = filepath.Join(home, path)
			}
		}
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", absPath)
	case "darwin":
		cmd = exec.Command("open", absPath)
	default: // linux, *bsd, etc.
		cmd = exec.Command("xdg-open", absPath)
	}
	return cmd.Start()
}

func (a *App) PendingLibraryInstallsPS3() (int, error) {
	pkgs, err := a.pendingLibraryPackagesPS3()
	return len(pkgs), err
}

// InstallLibraryPS3 installs only downloaded Library packages newer than the
// version currently present in RPCS3.
func (a *App) InstallLibraryPS3() (int, error) {
	pkgs, err := a.pendingLibraryPackagesPS3()
	if err != nil {
		return 0, err
	}
	if len(pkgs) == 0 {
		return 0, nil
	}

	hdd0Game := a.cfg.RPCS3.HDD0Game
	groups := pkg.OrderForBatchInstall(pkgs)
	installed := 0
	var installErr error
	for _, group := range groups {
		if len(group) == 0 {
			continue
		}
		titleID := group[0].TitleID
		a.activity.Infof("pkg", "Installing %d Library PKG(s) for %s", len(group), titleID)
		for _, packageInfo := range group {
			a.activity.Infof("pkg", "Installing %s v%s", packageInfo.Title, packageInfo.AppVer)
			info, extractErr := pkg.Extract(packageInfo.Path, hdd0Game)
			if extractErr != nil {
				a.activity.Errorf("pkg", "Failed to install %s v%s: %v (skipping remaining versions for this title)", packageInfo.Title, packageInfo.AppVer, extractErr)
				if installErr == nil {
					installErr = fmt.Errorf("install %s v%s: %w", packageInfo.TitleID, packageInfo.AppVer, extractErr)
				}
				break
			}
			installed++
			a.activity.Infof("pkg", "Successfully installed %s v%s to %s", packageInfo.Title, packageInfo.AppVer, filepath.Join(hdd0Game, info.TitleID))
		}
	}
	a.activity.Infof("pkg", "Library install complete: %d package(s) installed", installed)
	return installed, installErr
}

func (a *App) pendingLibraryPackagesPS3() ([]pkg.DiscoveredPKG, error) {
	hdd0Game := strings.TrimSpace(a.cfg.RPCS3.HDD0Game)
	if hdd0Game == "" {
		return nil, fmt.Errorf("dev_hdd0/game path not set")
	}
	titleRoot, err := config.TitleDirForRoot(a.cfg.Storage.LibraryDir, "ps3")
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(titleRoot); err != nil {
		if os.IsNotExist(err) {
			return []pkg.DiscoveredPKG{}, nil
		}
		return nil, err
	}
	pkgs, err := pkg.DiscoverPKGs(titleRoot)
	if err != nil && len(pkgs) == 0 {
		return nil, fmt.Errorf("discover Library PKGs: %w", err)
	}
	if err != nil {
		a.activity.Warnf("pkg", "Library PKG discovery warning: %v", err)
	}
	installedVersions := make(map[string]string)
	seenVersions := make(map[string]struct{})
	pending := make([]pkg.DiscoveredPKG, 0, len(pkgs))
	for _, packageInfo := range pkgs {
		installedVersion, found := installedVersions[packageInfo.TitleID]
		if !found {
			installedVersion, err = library.InstalledVersion(hdd0Game, packageInfo.TitleID)
			if err != nil {
				return nil, fmt.Errorf("read installed version for %s: %w", packageInfo.TitleID, err)
			}
			installedVersions[packageInfo.TitleID] = installedVersion
		}
		if library.PackageNeedsInstall(packageInfo.AppVer, installedVersion) {
			key := packageInfo.TitleID + "\x00" + packageInfo.AppVer
			if _, exists := seenVersions[key]; exists {
				continue
			}
			seenVersions[key] = struct{}{}
			pending = append(pending, packageInfo)
		}
	}
	return pending, nil
}

func (a *App) emitDownloadLibrary() {
	if a.ctx == nil || a.cfg == nil {
		return
	}

	titles, err := downloads.Scan(a.cfg.Storage.LibraryDir)
	if err != nil {
		a.wailsApp.Event.Emit("downloads:error", err.Error())
		return
	}
	a.wailsApp.Event.Emit("downloads:updated", titles)
}

// wailsEmitter adapts Wails application events to the jobs.Emitter shape.
type wailsEmitter struct {
	app       *application.App
	onJobDone func()
}

func (e *wailsEmitter) Emit(event string, data any) {
	if e.app != nil {
		e.app.Event.Emit(event, data)
	}
	if event == jobs.EventJobState && e.onJobDone != nil {
		if job, ok := data.(jobs.Job); ok && job.State == jobs.StateDone {
			e.onJobDone()
		}
	}
}
