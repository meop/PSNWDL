package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

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
	ctx         context.Context
	wailsApp    *application.App
	cfg         *config.Config
	cfgPath     string
	psn         *psn.Client
	jobs        *jobs.Queue
	activeMode  string
	activity    *activity.Sink
	watchMu     sync.Mutex
	lastDLSig   string
	lastDLJSON  string
	lastEmuSig  string
	lastEmuJSON string
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

	emitter := &wailsEmitter{app: a.wailsApp}
	a.activity = activity.NewSink(emitter)
	if err := ensureConfigLibraryDir(a.cfg); err != nil {
		log.Printf("library dir: %v", err)
	}
	a.psn = psn.NewClient(a.cfg.Network, a.activity)
	a.jobs = jobs.NewQueue(a.cfg.Network, a.cfg.Storage.LibraryDir, emitter, a.activity)
	a.activeMode = a.cfg.UI.DefaultMode
	if a.activeMode == "" {
		a.activeMode = "ps3"
	}

	a.startStateWatcher()
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
	go a.evaluateDownloadLibrary(true)
	go a.evaluateEmulatorLibrary(true)
	return nil
}

// SetActiveMode notifies backend actions that depend on the current mode.
func (a *App) SetActiveMode(mode string) {
	a.activeMode = mode
	if mode == "ps3" {
		go a.evaluateEmulatorLibrary(true)
	}
}

func (a *App) ActivityLog() []activity.Entry {
	return a.activity.GetEntries()
}

func (a *App) ClearActivityLog() {
	a.activity.Clear()
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
	go a.evaluateDownloadLibrary(true)
	go a.evaluateEmulatorLibrary(true)
	return nil
}

func (a *App) UpdateDownloadLibrary() ([]downloads.Title, error) {
	titles, err := downloads.Scan(a.cfg.Storage.LibraryDir)
	if err != nil {
		return nil, err
	}

	queued := 0
	for _, t := range titles {
		n, err := a.enqueueUpdatesForCachedTitle(t)
		if err != nil {
			a.activity.Warnf("library", "%s/%s update check failed: %v", t.Mode, t.TitleID, err)
			continue
		}
		queued += n
	}
	a.activity.Infof("library", "Library update check queued %d item(s)", queued)
	next, err := downloads.Scan(a.cfg.Storage.LibraryDir)
	if err == nil {
		go a.evaluateDownloadLibrary(true)
	}
	return next, err
}

func (a *App) enqueueUpdatesForCachedTitle(t downloads.Title) (int, error) {
	if t.TitleID == "firmware" {
		return a.enqueueFirmwareUpdatesForCachedTitle(t)
	}

	var title *psn.Title
	var err error
	switch t.Mode {
	case "ps3":
		title, err = a.psn.LookupPS3(a.ctx, t.TitleID)
	case "ps4":
		title, err = a.psn.LookupPS4(a.ctx, t.TitleID)
	case "psvita":
		title, err = a.psn.LookupVita(a.ctx, t.TitleID)
	default:
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	queued := 0
	for _, u := range title.Updates {
		if library.CompareVersion(u.Version, t.LatestVersion) <= 0 {
			continue
		}
		if _, err := a.EnqueueDownload(jobs.Request{
			TitleID:   t.TitleID,
			TitleName: title.Name,
			Mode:      t.Mode,
			Update:    u,
		}); err != nil {
			return queued, err
		}
		queued++
	}
	return queued, nil
}

func (a *App) enqueueFirmwareUpdatesForCachedTitle(t downloads.Title) (int, error) {
	list, err := a.ListFirmware(t.Mode)
	if err != nil {
		return 0, err
	}

	queued := 0
	for _, e := range list.Entries {
		if library.CompareVersion(e.Version, t.LatestVersion) <= 0 {
			continue
		}
		name := strings.TrimSpace(strings.Join([]string{strings.ToUpper(t.Mode), e.Type, e.Locale}, " "))
		if _, err := a.EnqueueDownload(jobs.Request{
			TitleID:   "firmware",
			TitleName: name,
			Mode:      t.Mode,
			Kind:      jobs.KindFirmware,
			Update: psn.Update{
				Version: e.Version,
				Size:    e.Size,
				SHA1Sum: e.SHA1Sum,
				URL:     e.URL,
			},
		}); err != nil {
			return queued, err
		}
		queued++
	}
	return queued, nil
}

// InstallJob extracts a finished download into the configured RPCS3
// dev_hdd0/game path. PS3-only.
func (a *App) InstallJob(id string) error {
	return a.jobs.Install(id, a.cfg.RPCS3.HDD0Game)
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

// ReconcileLibraryPS3 enriches each RPCS3 library entry with server +
// local-cache state, producing per-title status badges.
func (a *App) ReconcileLibraryPS3() ([]library.Row, error) {
	entries, err := a.ListRPCS3Library()
	if err != nil {
		return []library.Row{}, err
	}
	a.activity.Infof("library", "Reconciling %d title(s) against PSN…", len(entries))
	rows := library.ReconcilePS3WithPaths(a.ctx, entries, a.psn, a.cfg.RPCS3.HDD0Game, a.cfg.Storage.LibraryDir)

	// Summarize the result so the Activity console narrates what the numbers mean.
	upToDate, updates, missing, unreachable := 0, 0, 0, 0
	for _, r := range rows {
		switch r.Status {
		case library.StatusUpToDate:
			upToDate++
		case library.StatusUpdateAvailable:
			updates++
		case library.StatusMissingAll:
			missing++
		case library.StatusUnreachable:
			unreachable++
			a.activity.Warnf("library", "%s: server unreachable (%s)", r.TitleID, r.Error)
		}
	}
	a.activity.Infof("library", "Reconcile complete: %d up to date, %d update(s) available, %d missing, %d unreachable",
		upToDate, updates, missing, unreachable)
	return rows, nil
}

// SyncTitlePS3 enqueues only the missing updates for a single title.
func (a *App) SyncTitlePS3(tid string) error {
	entries, err := a.ListRPCS3Library()
	if err != nil {
		return err
	}

	for _, e := range entries {
		if e.TitleID != tid {
			continue
		}

		cached, _ := library.HighestCachedVersionIn(a.cfg.Storage.LibraryDir, "ps3", tid)
		title, err := a.psn.LookupPS3(a.ctx, tid)
		if err != nil {
			return err
		}

		for _, u := range title.Updates {
			if library.CompareVersion(u.Version, cached) <= 0 {
				continue
			}
			_, _ = a.EnqueueDownload(jobs.Request{
				TitleID:   tid,
				TitleName: title.Name,
				Mode:      "ps3",
				Update:    u,
			})
		}
		return nil
	}

	return fmt.Errorf("title %s not found in library", tid)
}

// ClearTitleCache removes the cache folder for a single title.
func (a *App) ClearTitleCache(tid string) error {
	if err := psn.ValidateTitleID(tid); err != nil {
		return err
	}
	dir, err := config.UpdatesDirForRoot(a.cfg.Storage.LibraryDir, "ps3")
	if err != nil {
		return err
	}
	target := filepath.Join(dir, tid)
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("remove cache for %s: %w", tid, err)
	}
	library.InvalidateInstalledVersionCache(tid)
	a.activity.Infof("library", "Cleared cache for %s", tid)
	go a.evaluateDownloadLibrary(true)
	go a.evaluateEmulatorLibrary(true)
	return nil
}

// OpenFolder reveals the given path in the system's file manager.
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
		// /select, opens the parent and selects the item. We're given a folder,
		// so this reveals it inside its parent — what the user expects.
		cmd = exec.Command("explorer", "/select,", absPath)
	case "darwin":
		cmd = exec.Command("open", absPath)
	default: // linux, *bsd, etc.
		cmd = exec.Command("xdg-open", absPath)
	}
	return cmd.Start()
}

// InstallFolderPS3 discovers PKGs in `root`, groups them by TitleID,
// orders by version ascending, and installs each group in order, skipping
// a title's remaining versions if any earlier one fails.
func (a *App) InstallFolderPS3(root, hdd0Game string) error {
	if hdd0Game == "" {
		return fmt.Errorf("dev_hdd0/game path not set")
	}

	pkgs, err := pkg.DiscoverPKGs(root)
	if err != nil && len(pkgs) == 0 {
		a.activity.Errorf("pkg", "Discover failed: %v", err)
		return fmt.Errorf("discover pkgs: %w", err)
	}

	a.activity.Infof("pkg", "Discovered %d PKG(s) in %s", len(pkgs), root)
	if len(pkgs) == 0 {
		return fmt.Errorf("no pkgs found in %s", root)
	}

	groups := pkg.OrderForBatchInstall(pkgs)
	a.activity.Infof("pkg", "Grouped into %d title(s)", len(groups))

	for _, group := range groups {
		if len(group) == 0 {
			continue
		}

		titleID := group[0].TitleID
		a.activity.Infof("pkg", "Installing %d PKG(s) for %s", len(group), titleID)

		for _, p := range group {
			a.activity.Infof("pkg", "Installing %s v%s", p.Title, p.AppVer)

			info, err := pkg.Extract(p.Path, hdd0Game)
			if err != nil {
				a.activity.Errorf("pkg", "Failed to install %s v%s: %v (skipping remaining versions for this title)", p.Title, p.AppVer, err)
				break
			}

			a.activity.Infof("pkg", "Successfully installed %s v%s to %s", p.Title, p.AppVer, filepath.Join(hdd0Game, info.TitleID))
		}
	}

	a.activity.Info("pkg", "Batch install complete")
	return nil
}

func (a *App) startStateWatcher() {
	go func() {
		a.evaluateDownloadLibrary(true)
		a.evaluateEmulatorLibrary(true)

		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				a.evaluateDownloadLibrary(false)
				a.evaluateEmulatorLibrary(false)
			}
		}
	}()
}

func (a *App) evaluateDownloadLibrary(force bool) {
	if a.ctx == nil || a.cfg == nil {
		return
	}

	sig := treeSignature(a.cfg.Storage.LibraryDir)
	a.watchMu.Lock()
	sameSig := sig == a.lastDLSig
	a.watchMu.Unlock()
	if sameSig && !force {
		return
	}

	titles, err := downloads.Scan(a.cfg.Storage.LibraryDir)
	if err != nil {
		a.wailsApp.Event.Emit("downloads:error", err.Error())
		return
	}
	payload, _ := json.Marshal(titles)

	a.watchMu.Lock()
	changed := force || string(payload) != a.lastDLJSON
	a.lastDLSig = sig
	if changed {
		a.lastDLJSON = string(payload)
	}
	a.watchMu.Unlock()

	if changed {
		a.wailsApp.Event.Emit("downloads:updated", titles)
	}
}

func (a *App) evaluateEmulatorLibrary(force bool) {
	if a.ctx == nil || a.cfg == nil || a.activeMode != "ps3" {
		return
	}
	if a.cfg.RPCS3.GamesYML == "" || a.cfg.RPCS3.HDD0Game == "" {
		return
	}

	sig := fileSignature(a.cfg.RPCS3.GamesYML) + "|" + treeSignature(a.cfg.RPCS3.HDD0Game) + "|" + downloadInventorySignature(a.cfg.Storage.LibraryDir)
	a.watchMu.Lock()
	sameSig := sig == a.lastEmuSig
	a.watchMu.Unlock()
	if sameSig && !force {
		return
	}

	rows, err := a.ReconcileLibraryPS3()
	if err != nil {
		a.wailsApp.Event.Emit("library:error", err.Error())
		return
	}
	payload, _ := json.Marshal(rows)

	a.watchMu.Lock()
	changed := force || string(payload) != a.lastEmuJSON
	a.lastEmuSig = sig
	if changed {
		a.lastEmuJSON = string(payload)
	}
	a.watchMu.Unlock()

	if changed {
		a.wailsApp.Event.Emit("library:updated", rows)
	}
}

func treeSignature(root string) string {
	if root == "" {
		return ""
	}
	info, err := os.Stat(root)
	if err != nil {
		return "missing:" + root
	}
	if !info.IsDir() {
		return fileSignature(root)
	}

	var b strings.Builder
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			b.WriteString("err:")
			b.WriteString(path)
			b.WriteString(";")
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		b.WriteString(rel)
		b.WriteString("|")
		b.WriteString(fmt.Sprintf("%d|%d|%t;", info.Size(), info.ModTime().UnixNano(), d.IsDir()))
		return nil
	})
	return b.String()
}

func fileSignature(path string) string {
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil {
		return "missing:" + path
	}
	return fmt.Sprintf("%s|%d|%d", path, info.Size(), info.ModTime().UnixNano())
}

func downloadInventorySignature(root string) string {
	titles, err := downloads.Scan(root)
	if err != nil {
		return "err:" + err.Error()
	}
	var b strings.Builder
	for _, title := range titles {
		b.WriteString(title.Mode)
		b.WriteString("/")
		b.WriteString(title.TitleID)
		b.WriteString("|")
		b.WriteString(title.LatestVersion)
		b.WriteString("|")
		b.WriteString(fmt.Sprint(title.FileCount))
		b.WriteString(";")
	}
	return b.String()
}

// wailsEmitter adapts Wails application events to the jobs.Emitter shape.
type wailsEmitter struct{ app *application.App }

func (e *wailsEmitter) Emit(event string, data any) {
	if e.app == nil {
		return
	}
	e.app.Event.Emit(event, data)
}
