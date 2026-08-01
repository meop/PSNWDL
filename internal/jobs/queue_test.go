package jobs

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	stdsync "sync"
	"testing"
	"time"

	"PSNWDL/internal/activity"
	"PSNWDL/internal/config"
	"PSNWDL/internal/psn"
)

type recordingEmitter struct {
	mu     stdsync.Mutex
	events []recordedEvent
}

type recordedEvent struct {
	name string
	data any
}

func (r *recordingEmitter) Emit(name string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordedEvent{name, data})
}

func (r *recordingEmitter) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	for i, e := range r.events {
		out[i] = e.name
	}
	return out
}

// setHomeDir points os.UserHomeDir() at tmpDir for the duration of the test.
func setHomeDir(t *testing.T, tmpDir string) {
	t.Helper()
	t.Setenv("HOME", tmpDir)
	t.Setenv("USERPROFILE", tmpDir)
}

// waitForTerminal polls Queue.List() until the job reaches one of the given
// terminal states, or the timeout elapses.
func waitForTerminal(t *testing.T, q *Queue, id string, timeout time.Duration) JobState {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, j := range q.List() {
			if j.ID != id {
				continue
			}
			switch j.State {
			case StateDone, StateFailed, StateCanceled:
				return j.State
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach terminal state in %s", id, timeout)
	return ""
}

func TestEnqueue_HappyPath(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)

	// Fake PKG: 256-byte body + 32-byte trailer. SHA-1 is over body only.
	body := make([]byte, 256)
	for i := range body {
		body[i] = byte(i)
	}
	trailer := make([]byte, ps3TrailerBytes)
	full := append(append([]byte{}, body...), trailer...)

	h := sha1.New()
	h.Write(body)
	expectedHash := hex.EncodeToString(h.Sum(nil))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(full)
	}))
	defer server.Close()

	emitter := &recordingEmitter{}
	act := activity.NewSink(emitter)
	q := NewQueue(config.Network{MaxConcurrentDownloads: 1, VerifyTLS: true}, "", emitter, act)

	id, err := q.Enqueue(context.Background(), Request{
		TitleID: "BCUS98114",
		Mode:    "ps3",
		Update: psn.Update{
			Version: "01.05",
			URL:     server.URL + "/pkg",
			Size:    int64(len(full)),
			SHA1Sum: expectedHash,
		},
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if state := waitForTerminal(t, q, id, 5*time.Second); state != StateDone {
		t.Fatalf("final state = %s, want done; events: %v", state, emitter.names())
	}

	expectedDest := filepath.Join(home, ".psnwdl", "download", "ps3", "updates", "BCUS98114", "BCUS98114_01.05.pkg")
	got, err := os.ReadFile(expectedDest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if len(got) != len(full) {
		t.Errorf("dest size = %d, want %d", len(got), len(full))
	}

	// .part file should be gone (renamed)
	if _, err := os.Stat(expectedDest + ".part"); !os.IsNotExist(err) {
		t.Errorf(".part file should have been renamed away: err=%v", err)
	}
}

func TestEnqueue_SHA1Mismatch(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)

	body := make([]byte, 128) // arbitrary, just needs to be >= 32 bytes total

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()

	emitter := &recordingEmitter{}
	act := activity.NewSink(emitter)
	q := NewQueue(config.Network{MaxConcurrentDownloads: 1, VerifyTLS: true}, "", emitter, act)

	id, err := q.Enqueue(context.Background(), Request{
		TitleID: "BCUS98114",
		Mode:    "ps3",
		Update: psn.Update{
			Version: "01.00",
			URL:     server.URL + "/pkg",
			Size:    int64(len(body)),
			SHA1Sum: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		},
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if state := waitForTerminal(t, q, id, 5*time.Second); state != StateFailed {
		t.Fatalf("final state = %s, want failed", state)
	}
}

func TestEnqueue_PS4UsesFullFileSHA1(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)

	body := []byte("small ps4 package fixture")
	sum := sha1.Sum(body)
	expectedHash := hex.EncodeToString(sum[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()

	act := activity.NewSink(NoopEmitter{})
	q := NewQueue(config.Network{MaxConcurrentDownloads: 1, VerifyTLS: true}, "", NoopEmitter{}, act)

	id, err := q.Enqueue(context.Background(), Request{
		TitleID: "CUSA00001",
		Mode:    "ps4",
		Update: psn.Update{
			Version: "01.00",
			URL:     server.URL + "/pkg",
			Size:    int64(len(body)),
			SHA1Sum: expectedHash,
		},
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if state := waitForTerminal(t, q, id, 5*time.Second); state != StateDone {
		t.Fatalf("final state = %s, want done", state)
	}
}

func TestEnqueue_BadStatus(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	act := activity.NewSink(NoopEmitter{})
	q := NewQueue(config.Network{MaxConcurrentDownloads: 1, VerifyTLS: true}, "", NoopEmitter{}, act)
	id, err := q.Enqueue(context.Background(), Request{
		TitleID: "BCUS98114",
		Mode:    "ps3",
		Update: psn.Update{
			Version: "01.00",
			URL:     server.URL + "/missing",
		},
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if state := waitForTerminal(t, q, id, 5*time.Second); state != StateFailed {
		t.Fatalf("final state = %s, want failed", state)
	}
}

func TestEnqueue_DownloadCanOutliveRequestTimeout(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)

	body := []byte("firmware payload")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(1100 * time.Millisecond)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	act := activity.NewSink(NoopEmitter{})
	q := NewQueue(config.Network{
		MaxConcurrentDownloads: 1,
		VerifyTLS:              true,
		RequestTimeoutSeconds:  1,
	}, "", NoopEmitter{}, act)

	id, err := q.Enqueue(context.Background(), Request{
		TitleID: "firmware",
		Mode:    "ps3",
		Kind:    KindFirmware,
		Update: psn.Update{
			Version: "4.93",
			URL:     server.URL + "/PS3UPDAT.PUP",
			Size:    int64(len(body)),
		},
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if state := waitForTerminal(t, q, id, 3*time.Second); state != StateDone {
		t.Fatalf("final state = %s, want done", state)
	}
}

func TestEnqueue_Validation(t *testing.T) {
	act := activity.NewSink(NoopEmitter{})
	q := NewQueue(config.Network{}, "", NoopEmitter{}, act)
	_, err := q.Enqueue(context.Background(), Request{
		TitleID: "BCUS98114",
		Mode:    "ps3",
		Update:  psn.Update{Version: "01.00"}, // missing URL
	})
	if err == nil {
		t.Fatal("expected error for missing URL, got nil")
	}
}

func TestEnqueue_ReturnsExistingActiveJobForSameDestination(t *testing.T) {
	q := NewQueue(config.Network{}, t.TempDir(), NoopEmitter{}, activity.NewSink(NoopEmitter{}))
	req := Request{
		TitleID: "firmware",
		Mode:    "ps3",
		Kind:    KindFirmware,
		Update:  psn.Update{Version: "4.93", URL: "https://example.com/PS3UPDAT.PUP"},
	}
	dest, err := q.destinationPath(req)
	if err != nil {
		t.Fatalf("destinationPath: %v", err)
	}
	q.jobs["job-existing"] = &Job{
		ID:       "job-existing",
		DestPath: dest,
		State:    StateDownloading,
	}

	id, err := q.Enqueue(context.Background(), req)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if id != "job-existing" {
		t.Fatalf("id = %q, want existing job id", id)
	}
	if got := len(q.List()); got != 1 {
		t.Fatalf("job count = %d, want 1", got)
	}
}

func TestDestinationPath(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)

	q := NewQueue(config.Network{}, "", NoopEmitter{}, activity.NewSink(NoopEmitter{}))
	got, err := q.destinationPath(Request{
		TitleID: "BCUS98114",
		Mode:    "ps3",
		Update:  psn.Update{Version: "01.05", URL: "https://example.com/update.pkg"},
	})
	if err != nil {
		t.Fatalf("destinationPath: %v", err)
	}
	want := filepath.Join(home, ".psnwdl", "download", "ps3", "updates", "BCUS98114", "BCUS98114_01.05.pkg")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDestinationPath_DRMFree(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)

	q := NewQueue(config.Network{}, "", NoopEmitter{}, activity.NewSink(NoopEmitter{}))
	got, err := q.destinationPath(Request{
		TitleID: "NPEA00001",
		Mode:    "ps3",
		Kind:    KindTitleUpdateDRMFree,
		Update:  psn.Update{Version: "01.00", URL: "https://example.com/update.pkg"},
	})
	if err != nil {
		t.Fatalf("destinationPath: %v", err)
	}
	want := filepath.Join(home, ".psnwdl", "download", "ps3", "updates", "NPEA00001", "NPEA00001_01.00_drm_free.pkg")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResumeReleasesPausedReader(t *testing.T) {
	q := NewQueue(config.Network{}, "", NoopEmitter{}, activity.NewSink(NoopEmitter{}))
	j := &Job{ID: "job-paused", State: StatePaused}
	pause := make(chan struct{})
	q.jobs[j.ID] = j
	q.pauses[j.ID] = pause

	if err := q.Resume(j.ID); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	select {
	case <-pause:
	case <-time.After(time.Second):
		t.Fatal("Resume did not release the paused reader")
	}
}

func TestDestinationPathRejectsUnsafeInput(t *testing.T) {
	q := NewQueue(config.Network{}, t.TempDir(), NoopEmitter{}, activity.NewSink(NoopEmitter{}))
	tests := []Request{
		{TitleID: `..\\outside`, Mode: "ps3", Update: psn.Update{Version: "01.00", URL: "https://example.com/a.pkg"}},
		{TitleID: "BCUS98114", Mode: `..\\outside`, Update: psn.Update{Version: "01.00", URL: "https://example.com/a.pkg"}},
		{TitleID: "BCUS98114", Mode: "ps3", Kind: "unknown", Update: psn.Update{Version: "01.00", URL: "https://example.com/a.pkg"}},
		{TitleID: "firmware", Mode: "ps5", Kind: KindFirmware, Update: psn.Update{Version: "01.00", URL: "file:///tmp/a.pup"}},
	}
	for _, req := range tests {
		if _, err := q.destinationPath(req); err == nil {
			t.Errorf("destinationPath(%+v) unexpectedly succeeded", req)
		}
	}
}
