package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"PSNWDL/internal/activity"
	"PSNWDL/internal/config"
	"PSNWDL/internal/jobs"
	"PSNWDL/internal/library"
	"PSNWDL/internal/pkg/pkgtest"
	"PSNWDL/internal/psn"
	"PSNWDL/internal/rpcs3"
)

// fakePSN stands in for Sony's endpoints. Titles not in the map are
// unreachable.
type fakePSN struct {
	mu       sync.Mutex
	titles   map[string]*psn.Title
	firmware map[string]*psn.FirmwareList
	calls    []string
}

func (f *fakePSN) record(call string) {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
}

func (f *fakePSN) title(call, tid string) (*psn.Title, error) {
	f.record(call + " " + tid)
	if title, ok := f.titles[tid]; ok {
		return title, nil
	}
	return nil, errors.New("title unreachable")
}

func (f *fakePSN) firmwareList(console string) (*psn.FirmwareList, error) {
	f.record("firmware " + console)
	if list, ok := f.firmware[console]; ok {
		return list, nil
	}
	return nil, errors.New("firmware unreachable")
}

func (f *fakePSN) LookupPS3(_ context.Context, tid string) (*psn.Title, error) {
	return f.title("ps3", tid)
}

func (f *fakePSN) LookupPS3WithDRMFree(_ context.Context, tid string, includeDRMFree bool) (*psn.Title, error) {
	if includeDRMFree {
		return f.title("ps3+drmfree", tid)
	}
	return f.title("ps3", tid)
}

func (f *fakePSN) LookupPS4(_ context.Context, tid string) (*psn.Title, error) {
	return f.title("ps4", tid)
}

func (f *fakePSN) LookupVita(_ context.Context, tid string) (*psn.Title, error) {
	return f.title("vita", tid)
}

func (f *fakePSN) LookupPS3Firmware(context.Context) (*psn.FirmwareList, error) {
	return f.firmwareList("ps3")
}

func (f *fakePSN) LookupPS4Firmware(context.Context) (*psn.FirmwareList, error) {
	return f.firmwareList("ps4")
}

func (f *fakePSN) LookupPS5Firmware(context.Context) (*psn.FirmwareList, error) {
	return f.firmwareList("ps5")
}

func (f *fakePSN) LookupVitaFirmware(context.Context) (*psn.FirmwareList, error) {
	return f.firmwareList("psvita")
}

// pkgServer serves fixed payloads by path, standing in for Sony's CDN.
type pkgServer struct {
	*httptest.Server
	mu       sync.Mutex
	payloads map[string][]byte
	// failing paths answer 500 until cleared.
	failing map[string]bool
	// held paths send half the body, then hang until the request ends or
	// release(name) is called.
	held    map[string]chan struct{}
	started chan string
}

func newPKGServer(t *testing.T) *pkgServer {
	s := &pkgServer{
		payloads: map[string][]byte{},
		failing:  map[string]bool{},
		held:     map[string]chan struct{}{},
		started:  make(chan string, 16),
	}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		payload, ok := s.payloads[r.URL.Path]
		failing := s.failing[r.URL.Path]
		hold := s.held[r.URL.Path]
		s.mu.Unlock()
		switch {
		case !ok:
			http.NotFound(w, r)
		case failing:
			http.Error(w, "unavailable", http.StatusInternalServerError)
		case hold != nil:
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			_, _ = w.Write(payload[:len(payload)/2])
			w.(http.Flusher).Flush()
			select {
			case s.started <- r.URL.Path:
			default:
			}
			select {
			case <-r.Context().Done():
				return
			case <-hold:
				_, _ = w.Write(payload[len(payload)/2:])
			}
		default:
			http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(payload))
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// update registers payload at /<name> and returns a matching psn.Update.
func (s *pkgServer) update(version, name string, payload []byte) psn.Update {
	s.mu.Lock()
	s.payloads["/"+name] = payload
	s.mu.Unlock()
	return psn.Update{
		Version: version,
		Size:    int64(len(payload)),
		URL:     s.URL + "/" + name,
	}
}

func (s *pkgServer) set(field map[string]bool, name string, on bool) {
	s.mu.Lock()
	field["/"+name] = on
	s.mu.Unlock()
}

// hold makes requests for name stall halfway until release(name).
func (s *pkgServer) hold(name string) {
	s.mu.Lock()
	s.held["/"+name] = make(chan struct{})
	s.mu.Unlock()
}

func (s *pkgServer) release(name string) {
	s.mu.Lock()
	if ch := s.held["/"+name]; ch != nil {
		close(ch)
		delete(s.held, "/"+name)
	}
	s.mu.Unlock()
}

func payloadFor(label string) []byte {
	return bytes.Repeat([]byte(label+"|"), 4096)
}

func waitForJob(t *testing.T, app *App, id string, want ...jobs.JobState) jobs.Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last jobs.Job
	for time.Now().Before(deadline) {
		for _, job := range app.ListJobs() {
			if job.ID != id {
				continue
			}
			last = job
			for _, state := range want {
				if job.State == state {
					return job
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s state = %q (error %q), want one of %v", id, last.State, last.Error, want)
	return last
}

func hasActivity(app *App, scope, fragment string) bool {
	for _, entry := range app.ActivityLog() {
		if entry.Scope == scope && strings.Contains(entry.Message, fragment) {
			return true
		}
	}
	return false
}

func titleFile(app *App, tid, version string) string {
	return filepath.Join(app.GetConfig().Storage.LibraryDir, "ps3", "title", tid, tid+"_"+version+".pkg")
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestActivityLogMethods(t *testing.T) {
	app, _ := startTestAppWith(t, "", &fakePSN{})
	app.ClearActivityLog()
	app.activity.Infof("psn", "psn entry")
	app.activity.Warnf("jobs", "jobs entry")

	if got := len(app.ActivityLog()); got != 2 {
		t.Fatalf("ActivityLog has %d entries, want 2", got)
	}
	app.ClearActivityLogScope("psn")
	entries := app.ActivityLog()
	if len(entries) != 1 || entries[0].Scope != "jobs" || entries[0].Level != activity.LevelWarn {
		t.Fatalf("after ClearActivityLogScope(psn): %+v", entries)
	}
	if _, err := time.Parse(time.RFC3339, entries[0].Ts); err != nil {
		t.Fatalf("activity timestamp %q is not RFC3339: %v", entries[0].Ts, err)
	}
	app.ClearActivityLog()
	if got := len(app.ActivityLog()); got != 0 {
		t.Fatalf("ActivityLog has %d entries after ClearActivityLog", got)
	}
}

func TestSearchAndFirmwareDelegateToPSN(t *testing.T) {
	fake := &fakePSN{
		titles: map[string]*psn.Title{
			"BCUS98114": {ID: "BCUS98114", Name: "PS3 game"},
			"CUSA00001": {ID: "CUSA00001", Name: "PS4 game"},
			"PCSE00001": {ID: "PCSE00001", Name: "Vita game"},
		},
		firmware: map[string]*psn.FirmwareList{
			"ps3": {Console: "ps3"}, "ps4": {Console: "ps4"},
			"ps5": {Console: "ps5"}, "psvita": {Console: "psvita"},
		},
	}
	app, _ := startTestAppWith(t, "", fake)

	if title, err := app.SearchPS3("BCUS98114", true); err != nil || title.Name != "PS3 game" {
		t.Fatalf("SearchPS3 = %+v, %v", title, err)
	}
	if title, err := app.SearchPS4("CUSA00001"); err != nil || title.Name != "PS4 game" {
		t.Fatalf("SearchPS4 = %+v, %v", title, err)
	}
	if title, err := app.SearchVita("PCSE00001"); err != nil || title.Name != "Vita game" {
		t.Fatalf("SearchVita = %+v, %v", title, err)
	}
	if _, err := app.SearchPS4("CUSA99999"); err == nil {
		t.Fatal("SearchPS4 of an unreachable title returned no error")
	}
	for _, mode := range []string{"ps3", "ps4", "ps5", "psvita"} {
		list, err := app.ListFirmware(mode)
		if err != nil || list.Console != mode {
			t.Fatalf("ListFirmware(%s) = %+v, %v", mode, list, err)
		}
	}
	if _, err := app.ListFirmware("psp"); err == nil {
		t.Fatal("ListFirmware accepted an unknown mode")
	}
	if fake.calls[0] != "ps3+drmfree BCUS98114" {
		t.Fatalf("SearchPS3 did not pass includeDRMFree: calls %v", fake.calls)
	}
}

func TestSaveConfigRebuildsPSNClientFromNetworkSettings(t *testing.T) {
	app, _ := startTestApp(t, "")
	var built []config.Network
	app.newPSN = func(net config.Network, _ *activity.Sink) psnClient {
		built = append(built, net)
		return &fakePSN{}
	}
	next := app.GetConfig()
	next.Network.TimeoutSeconds = 99
	next.Network.VerifyTLS = true
	if err := app.SaveConfig(&next); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if len(built) != 1 || built[0].TimeoutSeconds != 99 || !built[0].VerifyTLS {
		t.Fatalf("PSN client rebuilt with %+v, want the saved network settings", built)
	}
}

// A download lands in the Library, is listed, can be deleted, and never
// installs anything into RPCS3 on its own.
func TestDownloadListAndDeleteLibraryItem(t *testing.T) {
	_, hdd0Game := makeRPCS3Paths(t)
	app, _ := startTestAppWith(t, "", &fakePSN{})
	srv := newPKGServer(t)
	payload := payloadFor("BCUS98114 01.01")

	id, err := app.EnqueueDownload(jobs.Request{
		TitleID: "BCUS98114",
		Mode:    "ps3",
		Update:  srv.update("01.01", "BCUS98114-01.01.pkg", payload),
	})
	if err != nil {
		t.Fatalf("EnqueueDownload: %v", err)
	}
	job := waitForJob(t, app, id, jobs.StateDone)
	if job.DestPath != titleFile(app, "BCUS98114", "01.01") {
		t.Fatalf("DestPath = %q, want the Library title branch", job.DestPath)
	}
	got, err := os.ReadFile(job.DestPath)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("downloaded file differs from served payload (err %v)", err)
	}
	if entries, _ := os.ReadDir(hdd0Game); len(entries) != 0 {
		t.Fatalf("download wrote into dev_hdd0/game: %v", entries)
	}

	titles, err := app.ListDownloadLibrary()
	if err != nil || len(titles) != 1 || titles[0].TitleID != "BCUS98114" || titles[0].FileCount != 1 {
		t.Fatalf("ListDownloadLibrary = %+v, %v", titles, err)
	}

	if err := app.DeleteLibraryItems([]string{job.DestPath}); err != nil {
		t.Fatalf("DeleteLibraryItems: %v", err)
	}
	if exists(job.DestPath) {
		t.Fatal("DeleteLibraryItems left the file in place")
	}
	if !hasActivity(app, "library", "Deleted 1 selected library item(s)") {
		t.Fatal("DeleteLibraryItems did not narrate to the activity log")
	}
	outside := filepath.Join(t.TempDir(), "outside.pkg")
	writeFile(t, outside, []byte("x"))
	if err := app.DeleteLibraryItems([]string{outside}); err == nil || !exists(outside) {
		t.Fatalf("DeleteLibraryItems outside the Library: err %v, file kept %v", err, exists(outside))
	}
}

func TestEnqueueDownloadRejectsBadRequests(t *testing.T) {
	app, _ := startTestAppWith(t, "", &fakePSN{})
	for name, req := range map[string]jobs.Request{
		"no url":       {TitleID: "BCUS98114", Mode: "ps3", Update: psn.Update{Version: "01.00"}},
		"bad mode":     {TitleID: "BCUS98114", Mode: "psp", Update: psn.Update{Version: "01.00", URL: "http://x/y"}},
		"bad title id": {TitleID: "../etc", Mode: "ps3", Update: psn.Update{Version: "01.00", URL: "http://x/y"}},
	} {
		if _, err := app.EnqueueDownload(req); err == nil {
			t.Errorf("%s: EnqueueDownload accepted it", name)
		}
	}
	if len(app.ListJobs()) != 0 {
		t.Fatalf("rejected requests created jobs: %+v", app.ListJobs())
	}
}

func TestJobControlsPauseResumeCancelRetry(t *testing.T) {
	app, _ := startTestAppWith(t, "[network]\nretries = 0\n", &fakePSN{})
	srv := newPKGServer(t)

	for name, err := range map[string]error{
		"cancel": app.CancelJob("missing"),
		"pause":  app.PauseJob("missing"),
		"resume": app.ResumeJob("missing"),
		"retry":  app.RetryJob("missing"),
	} {
		if err == nil {
			t.Errorf("%s of an unknown job returned no error", name)
		}
	}

	// Pause and resume a download in flight.
	pausePayload := payloadFor("pause")
	srv.hold("pause.pkg")
	pauseID, err := app.EnqueueDownload(jobs.Request{TitleID: "BLES00001", Mode: "ps3",
		Update: srv.update("01.00", "pause.pkg", pausePayload)})
	if err != nil {
		t.Fatal(err)
	}
	<-srv.started
	waitForJob(t, app, pauseID, jobs.StateDownloading)
	if err := app.PauseJob(pauseID); err != nil {
		t.Fatalf("PauseJob: %v", err)
	}
	waitForJob(t, app, pauseID, jobs.StatePaused)
	if err := app.RetryJob(pauseID); err == nil {
		t.Fatal("RetryJob accepted a paused job")
	}
	if err := app.ResumeJob(pauseID); err != nil {
		t.Fatalf("ResumeJob: %v", err)
	}
	srv.release("pause.pkg")
	paused := waitForJob(t, app, pauseID, jobs.StateDone)
	if got, _ := os.ReadFile(paused.DestPath); !bytes.Equal(got, pausePayload) {
		t.Fatal("resumed download is not the full payload")
	}

	// Cancel a download in flight.
	srv.hold("cancel.pkg")
	cancelID, err := app.EnqueueDownload(jobs.Request{TitleID: "BLES00002", Mode: "ps3",
		Update: srv.update("01.00", "cancel.pkg", payloadFor("cancel"))})
	if err != nil {
		t.Fatal(err)
	}
	<-srv.started
	if err := app.CancelJob(cancelID); err != nil {
		t.Fatalf("CancelJob: %v", err)
	}
	waitForJob(t, app, cancelID, jobs.StateCanceled)

	// Retry a failed download once the server recovers.
	retryPayload := payloadFor("retry")
	srv.set(srv.failing, "retry.pkg", true)
	retryID, err := app.EnqueueDownload(jobs.Request{TitleID: "BLES00003", Mode: "ps3",
		Update: srv.update("01.00", "retry.pkg", retryPayload)})
	if err != nil {
		t.Fatal(err)
	}
	failed := waitForJob(t, app, retryID, jobs.StateFailed)
	srv.set(srv.failing, "retry.pkg", false)
	if err := app.RetryJob(retryID); err != nil {
		t.Fatalf("RetryJob: %v", err)
	}
	// Retry queues a fresh job for the same file.
	var retried string
	for deadline := time.Now().Add(5 * time.Second); retried == "" && time.Now().Before(deadline); {
		for _, job := range app.ListJobs() {
			if job.ID != retryID && job.DestPath == failed.DestPath {
				retried = job.ID
			}
		}
	}
	if retried == "" {
		t.Fatal("RetryJob queued no new job")
	}
	done := waitForJob(t, app, retried, jobs.StateDone)
	if got, _ := os.ReadFile(done.DestPath); !bytes.Equal(got, retryPayload) {
		t.Fatal("retried download is not the full payload")
	}
}

func TestAutoDetectAndListRPCS3Library(t *testing.T) {
	appData := t.TempDir()
	t.Setenv("APPDATA", appData)
	t.Setenv("XDG_CONFIG_HOME", appData)
	app, _ := startTestAppWith(t, "", &fakePSN{})

	if got := app.AutoDetectGamesYML(); got != "" {
		t.Fatalf("AutoDetectGamesYML with no RPCS3 = %q", got)
	}
	if entries, err := app.ListRPCS3Library(); err == nil || len(entries) != 0 {
		t.Fatalf("ListRPCS3Library with no games.yml = %v, %v", entries, err)
	}

	candidates := rpcs3.DefaultGamesYMLPaths()
	if len(candidates) == 0 {
		t.Skip("no games.yml candidates on this OS")
	}
	detected := candidates[0]
	writeFile(t, detected, []byte("BLUS30001: /games/b/\nBCUS98114: /games/a/\n"))
	if got := app.AutoDetectGamesYML(); got != detected {
		t.Fatalf("AutoDetectGamesYML = %q, want %q", got, detected)
	}
	entries, err := app.ListRPCS3Library()
	if err != nil || len(entries) != 2 || entries[0].TitleID != "BCUS98114" {
		t.Fatalf("ListRPCS3Library from detected games.yml = %+v, %v", entries, err)
	}

	configured, _ := makeRPCS3Paths(t)
	writeFile(t, configured, []byte("NPEB00001: /games/c/\n"))
	next := app.GetConfig()
	next.RPCS3.GamesYML = configured
	if err := app.SaveConfig(&next); err != nil {
		t.Fatal(err)
	}
	entries, err = app.ListRPCS3Library()
	if err != nil || len(entries) != 1 || entries[0].TitleID != "NPEB00001" {
		t.Fatalf("configured games.yml should win over detection: %+v, %v", entries, err)
	}
}

func TestReconcileTitlePS3(t *testing.T) {
	fake := &fakePSN{titles: map[string]*psn.Title{
		"BCUS98114": {ID: "BCUS98114", Name: "Game", Updates: []psn.Update{{Version: "01.01"}, {Version: "01.02"}}},
	}}
	app, _ := startTestAppWith(t, "", fake)
	installDir := t.TempDir()
	writeFile(t, titleFile(app, "BCUS98114", "01.01"), []byte("pkg"))

	row := app.ReconcileTitlePS3(rpcs3.Entry{TitleID: "BCUS98114", InstallDir: installDir})
	if row.Status != library.StatusSomeDownloaded || row.DownloadedCount != 1 || row.UpdateCount != 2 {
		t.Fatalf("row = %+v", row)
	}

	app.ClearActivityLog()
	row = app.ReconcileTitlePS3(rpcs3.Entry{TitleID: "BLUS30001", InstallDir: installDir})
	if row.Status != library.StatusUnreachable {
		t.Fatalf("unreachable title row = %+v", row)
	}
	if !hasActivity(app, "library", "BLUS30001: reconcile failed") {
		t.Fatal("unreachable reconcile was not narrated")
	}
}

// Sync is exact: files the server no longer advertises are removed and every
// missing advertised version is queued, gaps included.
func TestSyncTitlePS3IsExact(t *testing.T) {
	srv := newPKGServer(t)
	fake := &fakePSN{titles: map[string]*psn.Title{}}
	app, _ := startTestAppWith(t, "", fake)
	fake.titles["BCUS98114"] = &psn.Title{ID: "BCUS98114", Name: "Game", Updates: []psn.Update{
		srv.update("01.01", "a.pkg", payloadFor("01.01")),
		srv.update("01.02", "b.pkg", payloadFor("01.02")),
		srv.update("01.03", "c.pkg", payloadFor("01.03")),
	}}
	writeFile(t, titleFile(app, "BCUS98114", "01.02"), payloadFor("01.02"))
	stale := titleFile(app, "BCUS98114", "00.99")
	writeFile(t, stale, []byte("old"))

	if _, err := app.SyncTitlePS3("not-a-title"); err == nil {
		t.Fatal("SyncTitlePS3 accepted an invalid title id")
	}
	if _, err := app.SyncTitlePS3("BLUS30001"); err == nil {
		t.Fatal("SyncTitlePS3 of an unreachable title returned no error")
	}

	ids, err := app.SyncTitlePS3("BCUS98114")
	if err != nil {
		t.Fatalf("SyncTitlePS3: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("SyncTitlePS3 queued %d jobs, want 2 (01.01 and 01.03)", len(ids))
	}
	for _, id := range ids {
		waitForJob(t, app, id, jobs.StateDone)
	}
	if exists(stale) {
		t.Fatal("sync kept a file the server no longer advertises")
	}
	for _, version := range []string{"01.01", "01.02", "01.03"} {
		if !exists(titleFile(app, "BCUS98114", version)) {
			t.Fatalf("sync left %s missing", version)
		}
	}
	if !hasActivity(app, "library", "removed 1 file(s) not advertised") ||
		!hasActivity(app, "library", "sync queued 2 missing PKG(s)") {
		t.Fatal("sync was not narrated")
	}

	// A second sync has nothing to do.
	if ids, err := app.SyncTitlePS3("BCUS98114"); err != nil || len(ids) != 0 {
		t.Fatalf("second SyncTitlePS3 = %v, %v", ids, err)
	}
}

func TestSyncAllPS3RemovesTitlesNotInRPCS3(t *testing.T) {
	srv := newPKGServer(t)
	fake := &fakePSN{titles: map[string]*psn.Title{}}
	app, _ := startTestAppWith(t, "", fake)
	fake.titles["BCUS98114"] = &psn.Title{ID: "BCUS98114", Updates: []psn.Update{
		srv.update("01.01", "a.pkg", payloadFor("a")),
	}}
	orphan := titleFile(app, "BLES09999", "01.00")
	writeFile(t, orphan, []byte("orphan"))

	ids, err := app.SyncAllPS3([]rpcs3.Entry{{TitleID: "BCUS98114"}, {TitleID: "BLUS30001"}})
	if err != nil {
		t.Fatalf("SyncAllPS3: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("SyncAllPS3 queued %v, want one job", ids)
	}
	waitForJob(t, app, ids[0], jobs.StateDone)
	if exists(filepath.Dir(orphan)) {
		t.Fatal("SyncAllPS3 kept a title folder RPCS3 does not list")
	}
	if !hasActivity(app, "library", "BLUS30001 sync failed") {
		t.Fatal("a failing title should be narrated and skipped, not abort the sync")
	}
}

// Sync never deletes a file an active download is writing.
func TestSyncSkipsFilesOfActiveDownloads(t *testing.T) {
	srv := newPKGServer(t)
	fake := &fakePSN{titles: map[string]*psn.Title{}}
	app, _ := startTestAppWith(t, "", fake)
	srv.hold("active.pkg")
	active := srv.update("01.00", "active.pkg", payloadFor("active"))

	id, err := app.EnqueueDownload(jobs.Request{TitleID: "BLES09999", Mode: "ps3", Update: active})
	if err != nil {
		t.Fatal(err)
	}
	<-srv.started

	if _, err := app.SyncAllPS3([]rpcs3.Entry{}); err != nil {
		t.Fatalf("SyncAllPS3: %v", err)
	}
	if !hasActivity(app, "library", "because an active download uses it") {
		t.Fatal("the active download's folder was not protected")
	}
	if err := app.CancelJob(id); err != nil {
		t.Fatal(err)
	}
	waitForJob(t, app, id, jobs.StateCanceled)
}

func TestFolderCommandIsCrossPlatform(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	abs := filepath.Join(home, "library")

	for goos, want := range map[string]string{"windows": "explorer", "darwin": "open", "linux": "xdg-open", "freebsd": "xdg-open"} {
		cmd, err := folderCommand(goos, abs)
		if err != nil {
			t.Fatalf("%s: %v", goos, err)
		}
		if name := strings.TrimSuffix(filepath.Base(cmd.Path), ".exe"); name != want && filepath.Base(cmd.Args[0]) != want {
			t.Errorf("%s: command %v, want %s", goos, cmd.Args, want)
		}
		if cmd.Args[len(cmd.Args)-1] != abs {
			t.Errorf("%s: path arg %q, want %q", goos, cmd.Args[len(cmd.Args)-1], abs)
		}
	}
	for in, want := range map[string]string{
		"~/.psnwdl/library": filepath.Join(home, ".psnwdl", "library"),
		".psnwdl/library":   filepath.Join(home, ".psnwdl", "library"),
		filepath.Clean(abs): abs,
	} {
		cmd, err := folderCommand(runtime.GOOS, in)
		if err != nil {
			t.Fatal(err)
		}
		if got := cmd.Args[len(cmd.Args)-1]; got != want {
			t.Errorf("folderCommand(%q) path = %q, want %q", in, got, want)
		}
	}
}

func TestLibraryInstallPS3InstallsOnlyNewerPKGs(t *testing.T) {
	_, hdd0Game := makeRPCS3Paths(t)
	app, _ := startTestAppWith(t, "", &fakePSN{})

	if _, err := pendingTotal(app); err == nil {
		t.Fatal("PendingLibraryPKGsPS3 without dev_hdd0/game returned no error")
	}
	if _, err := app.InstallLibraryPKGsPS3(); err == nil {
		t.Fatal("InstallLibraryPKGsPS3 without dev_hdd0/game returned no error")
	}
	next := app.GetConfig()
	next.RPCS3.HDD0Game = hdd0Game
	if err := app.SaveConfig(&next); err != nil {
		t.Fatal(err)
	}
	if n, err := pendingTotal(app); err != nil || n != 0 {
		t.Fatalf("PendingLibraryPKGsPS3 with an empty Library = %d, %v", n, err)
	}
	if n, err := app.InstallLibraryPKGsPS3(); err != nil || n != 0 {
		t.Fatalf("InstallLibraryPKGsPS3 with an empty Library = %d, %v", n, err)
	}

	// Installed: BCUS98114 01.01. Library: 01.01 (already installed), 01.02,
	// 01.03, and a duplicate 01.03 copy; plus a title not installed at all.
	writeFile(t, filepath.Join(hdd0Game, "BCUS98114", "PARAM.SFO"), pkgtest.SFO("BCUS98114", "01.01", "Game"))
	for _, version := range []string{"01.01", "01.02", "01.03"} {
		pkgtest.WriteTitlePKG(t, titleFile(app, "BCUS98114", version), "BCUS98114", version, "Game")
	}
	pkgtest.WriteTitlePKG(t, filepath.Join(filepath.Dir(titleFile(app, "BCUS98114", "x")), "copy", "dup.pkg"), "BCUS98114", "01.03", "Game")
	pkgtest.WriteTitlePKG(t, titleFile(app, "BLES00001", "01.00"), "BLES00001", "01.00", "Other")

	if n, err := pendingTotal(app); err != nil || n != 3 {
		t.Fatalf("PendingLibraryPKGsPS3 = %d, %v; want 3 (01.02, 01.03, BLES00001)", n, err)
	}
	n, err := app.InstallLibraryPKGsPS3()
	if err != nil || n != 3 {
		t.Fatalf("InstallLibraryPKGsPS3 = %d, %v", n, err)
	}
	if v, _ := library.InstalledVersion(hdd0Game, "BCUS98114"); v != "01.03" {
		t.Fatalf("installed BCUS98114 version = %q, want 01.03 (installed in order)", v)
	}
	if eboot, _ := os.ReadFile(filepath.Join(hdd0Game, "BLES00001", "USRDIR", "EBOOT.BIN")); string(eboot) != "EBOOT BLES00001 01.00" {
		t.Fatalf("BLES00001 EBOOT.BIN = %q", eboot)
	}
	if !hasActivity(app, "pkg", "Library install complete: 3 PKG(s) installed") {
		t.Fatal("install was not narrated")
	}
	if n, err := pendingTotal(app); err != nil || n != 0 {
		t.Fatalf("PendingLibraryPKGsPS3 after install = %d, %v", n, err)
	}
}

func TestLibraryInstallPS3SurfacesBadPKGs(t *testing.T) {
	_, hdd0Game := makeRPCS3Paths(t)
	app, _ := startTestAppWith(t, "", &fakePSN{})
	next := app.GetConfig()
	next.RPCS3.HDD0Game = hdd0Game
	if err := app.SaveConfig(&next); err != nil {
		t.Fatal(err)
	}

	// Only an unreadable PKG: discovery fails outright.
	writeFile(t, titleFile(app, "BCUS98114", "01.00"), []byte("not a pkg"))
	if _, err := pendingTotal(app); err == nil {
		t.Fatal("discovery of only unreadable PKGs returned no error")
	}

	// A readable PKG beside it: discovery warns and carries on.
	pkgtest.WriteTitlePKG(t, titleFile(app, "BLES00001", "01.00"), "BLES00001", "01.00", "Other")
	if n, err := pendingTotal(app); err != nil || n != 1 {
		t.Fatalf("PendingLibraryPKGsPS3 = %d, %v", n, err)
	}
	if !hasActivity(app, "pkg", "Library PKG discovery warning") {
		t.Fatal("skipped PKG was not narrated")
	}

	// A PKG whose data is cut short is discovered (its PARAM.SFO is intact)
	// but fails to install, naming the title; other titles still install.
	truncated := pkgtest.PKG([]pkgtest.Item{
		{Name: "PARAM.SFO", Data: pkgtest.SFO("BLES00002", "01.00", "Cut")},
		{Name: "USRDIR/EBOOT.BIN", Data: bytes.Repeat([]byte("E"), 4096)},
	})
	writeFile(t, titleFile(app, "BLES00002", "01.00"), truncated[:len(truncated)-1024])
	n, err := app.InstallLibraryPKGsPS3()
	if err == nil || n != 1 || !strings.Contains(err.Error(), "BLES00002") {
		t.Fatalf("InstallLibraryPKGsPS3 with a truncated PKG = %d, %v", n, err)
	}
	if v, _ := library.InstalledVersion(hdd0Game, "BLES00001"); v != "01.00" {
		t.Fatalf("intact title was not installed beside the broken one (version %q)", v)
	}
	if !hasActivity(app, "pkg", "Failed to install") {
		t.Fatal("install failure was not narrated")
	}
}

func TestStartupHelpers(t *testing.T) {
	t.Run("existingDirectory", func(t *testing.T) {
		root := t.TempDir()
		file := filepath.Join(root, "games.yml")
		writeFile(t, file, []byte("x"))
		for in, want := range map[string]string{
			"":                                   "",
			root:                                 root,
			file:                                 root,
			filepath.Join(root, "a", "b", "c"):   root,
			filepath.Join(file, "not", "a", "d"): root,
		} {
			if got := existingDirectory(in); got != want {
				t.Errorf("existingDirectory(%q) = %q, want %q", in, got, want)
			}
		}
	})

	t.Run("ensureConfigLibraryDir", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		if err := ensureConfigLibraryDir(nil); err != nil {
			t.Fatal(err)
		}
		cfg := &config.Config{}
		if err := ensureConfigLibraryDir(cfg); err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(home, ".psnwdl", "library"); cfg.Storage.LibraryDir != want || !exists(want) {
			t.Fatalf("library dir = %q (exists %v), want %q created", cfg.Storage.LibraryDir, exists(want), want)
		}
		blocked := &config.Config{Storage: config.Storage{LibraryDir: filepath.Join(home, "file", "lib")}}
		writeFile(t, filepath.Join(home, "file"), []byte("x"))
		if err := ensureConfigLibraryDir(blocked); err == nil {
			t.Fatal("ensureConfigLibraryDir under a file returned no error")
		}
	})

	t.Run("GetConfig before startup", func(t *testing.T) {
		if got := (&App{}).GetConfig(); got.Network.ParallelDownloads != config.Default().Network.ParallelDownloads {
			t.Fatalf("GetConfig without config = %+v, want defaults", got)
		}
	})

	t.Run("startup with an unreadable config", func(t *testing.T) {
		app, _ := startTestAppWith(t, "[network\n", &fakePSN{})
		if got := app.GetConfig(); got.UI != config.Default().UI {
			t.Fatalf("broken config.toml should fall back to defaults, got %+v", got)
		}
	})

	t.Run("SaveConfig guards", func(t *testing.T) {
		app := NewApp(nil)
		if err := app.SaveConfig(nil); err == nil {
			t.Fatal("SaveConfig(nil) returned no error")
		}
		if err := app.SaveConfig(config.Default()); err == nil {
			t.Fatal("SaveConfig before startup returned no error")
		}
	})

	t.Run("emitDownloadLibrary before startup", func(t *testing.T) {
		(&App{}).emitDownloadLibrary()
	})
}

func TestSaveConfigDefaultsEmptyLibraryDir(t *testing.T) {
	app, cfgPath := startTestAppWith(t, "", &fakePSN{})
	next := app.GetConfig()
	next.Storage.LibraryDir = ""
	if err := app.SaveConfig(&next); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	want, _ := config.DefaultLibraryDir()
	if got := reload(t, cfgPath).Storage.LibraryDir; got != want {
		t.Fatalf("empty library dir saved as %q, want default %q", got, want)
	}
}

func TestJobCompletionRefreshesDownloads(t *testing.T) {
	var done atomic.Int32
	emitter := &wailsEmitter{onJobDone: func() { done.Add(1) }}
	emitter.Emit(jobs.EventJobState, "not a job")
	emitter.Emit(jobs.EventJobState, jobs.Job{State: jobs.StateDone})
	if done.Load() != 1 {
		t.Fatalf("onJobDone called %d times, want 1", done.Load())
	}
}

// pendingTotal sums PendingLibraryPKGsPS3 over every title.
func pendingTotal(app *App) (int, error) {
	counts, err := app.PendingLibraryPKGsPS3()
	total := 0
	for _, n := range counts {
		total += n
	}
	return total, err
}

func TestInstallTitlePKGsPS3InstallsOnlyThatTitle(t *testing.T) {
	_, hdd0Game := makeRPCS3Paths(t)
	app, _ := startTestAppWith(t, "", &fakePSN{})
	next := app.GetConfig()
	next.RPCS3.HDD0Game = hdd0Game
	if err := app.SaveConfig(&next); err != nil {
		t.Fatal(err)
	}
	if _, err := app.InstallTitlePKGsPS3("not a title"); err == nil {
		t.Fatal("InstallTitlePKGsPS3 accepted an invalid title ID")
	}

	pkgtest.WriteTitlePKG(t, titleFile(app, "BCUS98114", "01.01"), "BCUS98114", "01.01", "Game")
	pkgtest.WriteTitlePKG(t, titleFile(app, "BCUS98114", "01.02"), "BCUS98114", "01.02", "Game")
	pkgtest.WriteTitlePKG(t, titleFile(app, "BLES00001", "01.00"), "BLES00001", "01.00", "Other")

	counts, err := app.PendingLibraryPKGsPS3()
	if err != nil || counts["BCUS98114"] != 2 || counts["BLES00001"] != 1 || len(counts) != 2 {
		t.Fatalf("PendingLibraryPKGsPS3 = %v, %v", counts, err)
	}
	if n, err := app.InstallTitlePKGsPS3("BCUS98114"); err != nil || n != 2 {
		t.Fatalf("InstallTitlePKGsPS3 = %d, %v", n, err)
	}
	if v, _ := library.InstalledVersion(hdd0Game, "BCUS98114"); v != "01.02" {
		t.Fatalf("installed BCUS98114 version = %q, want 01.02", v)
	}
	counts, err = app.PendingLibraryPKGsPS3()
	if err != nil || len(counts) != 1 || counts["BLES00001"] != 1 {
		t.Fatalf("PendingLibraryPKGsPS3 after one title's install = %v, %v", counts, err)
	}
	if n, err := app.InstallTitlePKGsPS3("BCUS98114"); err != nil || n != 0 {
		t.Fatalf("second InstallTitlePKGsPS3 = %d, %v", n, err)
	}
}

func TestLocalTitlesPS3ReadsOnlyLocalFiles(t *testing.T) {
	_, hdd0Game := makeRPCS3Paths(t)
	app, _ := startTestAppWith(t, "", &fakePSN{})
	next := app.GetConfig()
	next.RPCS3.HDD0Game = hdd0Game
	if err := app.SaveConfig(&next); err != nil {
		t.Fatal(err)
	}
	installDir := t.TempDir()
	writeFile(t, filepath.Join(hdd0Game, "BCUS98114", "PARAM.SFO"), pkgtest.SFO("BCUS98114", "01.01", "Game"))

	titles := app.LocalTitlesPS3([]rpcs3.Entry{
		{TitleID: "BCUS98114", InstallDir: installDir},
		{TitleID: "BLES00001", InstallDir: filepath.Join(installDir, "gone")},
	})
	if len(titles) != 2 {
		t.Fatalf("LocalTitlesPS3 = %+v", titles)
	}
	if titles[0].Name != "Game" || titles[0].Missing {
		t.Fatalf("installed title = %+v", titles[0])
	}
	if titles[1].Name != "" || !titles[1].Missing {
		t.Fatalf("missing title = %+v", titles[1])
	}
}
