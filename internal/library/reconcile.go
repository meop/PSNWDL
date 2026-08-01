package library

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	stdsync "sync"

	"PSNWDL/internal/config"
	"PSNWDL/internal/pkg"
	"PSNWDL/internal/psn"
	"PSNWDL/internal/rpcs3"
)

const reconcileConcurrency = 6

type Status string

const (
	StatusUpToDate           Status = "up_to_date"
	StatusUpdateAvailable    Status = "update_available"
	StatusMissingAll         Status = "missing_all"
	StatusNoUpdates          Status = "no_updates"
	StatusUnreachable        Status = "unreachable"
	StatusCachedNotInstalled Status = "cached_not_installed"
)

// Row is a per-title reconciled view of "library has it" + "server says X" +
// "cache has Y", with a derived Status the frontend can colour-code.
type Row struct {
	TitleID          string       `json:"title_id"`
	Name             string       `json:"name,omitempty"`
	InstallDir       string       `json:"install_dir"`
	Status           Status       `json:"status"`
	InstalledVersion string       `json:"installed_version,omitempty"`
	LatestLocal      string       `json:"latest_local,omitempty"`
	LatestServer     string       `json:"latest_server,omitempty"`
	Updates          []psn.Update `json:"updates,omitempty"`
	Error            string       `json:"error,omitempty"`
}

// PSNLookup is the subset of psn.Client used by reconciliation. Defined as
// an interface so tests can inject a fake.
type PSNLookup interface {
	LookupPS3(ctx context.Context, tid string) (*psn.Title, error)
}

type InstalledVersionCache struct {
	mu       stdsync.Mutex
	versions map[string]versionCacheEntry
}

type versionCacheEntry struct {
	version string
	mtime   int64
	size    int64
}

var installedVersionCache = &InstalledVersionCache{
	versions: make(map[string]versionCacheEntry),
}

func (c *InstalledVersionCache) Get(hdd0Game, titleID string) string {
	if hdd0Game == "" || titleID == "" {
		return ""
	}

	dir := filepath.Join(hdd0Game, titleID)
	sfoPath := filepath.Join(dir, "PARAM.SFO")
	info, err := os.Stat(sfoPath)
	if err != nil {
		return ""
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	key := filepath.Clean(sfoPath)
	entry, exists := c.versions[key]
	if exists && entry.mtime == info.ModTime().UnixNano() && entry.size == info.Size() {
		return entry.version
	}

	version := readInstalledVersion(dir)
	c.versions[key] = versionCacheEntry{
		version: version,
		mtime:   info.ModTime().UnixNano(),
		size:    info.Size(),
	}
	return version
}

func readInstalledVersion(titleDir string) string {
	sfoPath := filepath.Join(titleDir, "PARAM.SFO")
	data, err := os.ReadFile(sfoPath)
	if err != nil {
		return ""
	}

	info, err := pkg.ParseSFO(data)
	if err != nil {
		return ""
	}

	if info.AppVer == "" {
		return ""
	}

	return strings.TrimPrefix(info.AppVer, "01.")
}

func (c *InstalledVersionCache) Invalidate(titleID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for path := range c.versions {
		if filepath.Base(filepath.Dir(path)) == titleID {
			delete(c.versions, path)
		}
	}
}

func InvalidateInstalledVersionCache(titleID string) {
	installedVersionCache.Invalidate(titleID)
}

func ClearInstalledVersionCache() {
	installedVersionCache.mu.Lock()
	defer installedVersionCache.mu.Unlock()
	clear(installedVersionCache.versions)
}

// ReconcilePS3 computes a Row per RPCS3 library entry by combining the local
// cached PKG versions with the server's *-ver.xml answer.
func ReconcilePS3(ctx context.Context, entries []rpcs3.Entry, client PSNLookup) []Row {
	return ReconcilePS3WithHDD0(ctx, entries, client, "")
}

func ReconcilePS3WithHDD0(ctx context.Context, entries []rpcs3.Entry, client PSNLookup, hdd0Game string) []Row {
	return ReconcilePS3WithPaths(ctx, entries, client, hdd0Game, "")
}

func ReconcilePS3WithPaths(ctx context.Context, entries []rpcs3.Entry, client PSNLookup, hdd0Game, libraryRoot string) []Row {
	if len(entries) == 0 {
		return []Row{}
	}

	rows := make([]Row, len(entries))
	sem := make(chan struct{}, reconcileConcurrency)
	var wg stdsync.WaitGroup

	for i, e := range entries {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, e rpcs3.Entry) {
			defer wg.Done()
			defer func() { <-sem }()
			rows[i] = reconcileOne(ctx, e, client, hdd0Game, libraryRoot)
		}(i, e)
	}
	wg.Wait()
	return rows
}

func reconcileOne(ctx context.Context, e rpcs3.Entry, client PSNLookup, hdd0Game, libraryRoot string) Row {
	row := Row{
		TitleID:    e.TitleID,
		InstallDir: e.InstallDir,
	}

	installedVersion := installedVersionCache.Get(hdd0Game, e.TitleID)
	if installedVersion != "" {
		row.InstalledVersion = installedVersion
	}

	localVer, _ := HighestCachedVersionIn(libraryRoot, "ps3", e.TitleID)
	row.LatestLocal = localVer

	title, err := client.LookupPS3(ctx, e.TitleID)
	if err != nil {
		row.Status = StatusUnreachable
		row.Error = err.Error()
		return row
	}
	row.Name = title.Name
	row.Updates = title.Updates

	if len(title.Updates) == 0 {
		row.Status = StatusNoUpdates
		return row
	}

	serverVer := highestServerVersion(title.Updates)
	row.LatestServer = serverVer

	row.Status = statusForVersions(installedVersion, localVer, serverVer, hdd0Game != "")
	return row
}

func statusForVersions(installedVersion, localVersion, serverVersion string, hasInstallPath bool) Status {
	if !hasInstallPath {
		switch {
		case localVersion == "":
			return StatusMissingAll
		case compareVersion(localVersion, serverVersion) >= 0:
			return StatusUpToDate
		default:
			return StatusUpdateAvailable
		}
	}

	switch {
	case installedVersion != "" && compareVersion(installedVersion, serverVersion) >= 0:
		return StatusUpToDate
	case localVersion != "" && compareVersion(localVersion, serverVersion) >= 0:
		return StatusCachedNotInstalled
	case installedVersion == "" && localVersion == "":
		return StatusMissingAll
	default:
		return StatusUpdateAvailable
	}
}

// HighestCachedVersion scans ~/.psnwdl/library/<mode>/title/<TID>/ for files named
// <TID>_<version>.pkg and returns the lexically-highest version, or "".
func HighestCachedVersion(mode, tid string) (string, error) {
	return HighestCachedVersionIn("", mode, tid)
}

func HighestCachedVersionIn(libraryRoot, mode, tid string) (string, error) {
	dir, err := config.TitleDirForRoot(libraryRoot, mode)
	if err != nil {
		return "", err
	}
	titleDir := filepath.Join(dir, tid)
	entries, err := os.ReadDir(titleDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	prefix := tid + "_"
	var best string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".pkg") {
			continue
		}
		v := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".pkg")
		if compareVersion(v, best) > 0 {
			best = v
		}
	}
	return best, nil
}

func highestCachedVersion(mode, tid string) (string, error) {
	return HighestCachedVersion(mode, tid)
}

func highestServerVersion(updates []psn.Update) string {
	var best string
	for _, u := range updates {
		if compareVersion(u.Version, best) > 0 {
			best = u.Version
		}
	}
	return best
}

// CompareVersion does a string compare which is correct for Sony's zero-padded
// NN.NN format (e.g. "01.05" < "01.13" < "02.00").
func CompareVersion(a, b string) int {
	return compareVersion(a, b)
}

func compareVersion(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == "":
		return -1
	case b == "":
		return 1
	default:
		return strings.Compare(a, b)
	}
}
