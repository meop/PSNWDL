package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testDelay = 50 * time.Millisecond

func startWatcher(t *testing.T, targets ...Target) <-chan string {
	t.Helper()
	changes := make(chan string, 16)
	w, err := New(testDelay, func(kind string) { changes <- kind })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	w.Set(targets)
	return changes
}

func expectChange(t *testing.T, changes <-chan string, kind string) {
	t.Helper()
	select {
	case got := <-changes:
		if got != kind {
			t.Fatalf("change kind = %q, want %q", got, kind)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no %q change reported", kind)
	}
}

func expectQuiet(t *testing.T, changes <-chan string) {
	t.Helper()
	select {
	case got := <-changes:
		t.Fatalf("unexpected %q change", got)
	case <-time.After(10 * testDelay):
	}
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReportsFilesInNewSubdirectories(t *testing.T) {
	root := t.TempDir()
	changes := startWatcher(t, Target{Kind: "library", Root: root, Depth: 3})

	if err := os.MkdirAll(filepath.Join(root, "ps3", "title", "BLUS00001"), 0o755); err != nil {
		t.Fatal(err)
	}
	expectChange(t, changes, "library")

	writeFile(t, filepath.Join(root, "ps3", "title", "BLUS00001", "BLUS00001_01.01.pkg"), "pkg")
	expectChange(t, changes, "library")

	if err := os.Remove(filepath.Join(root, "ps3", "title", "BLUS00001", "BLUS00001_01.01.pkg")); err != nil {
		t.Fatal(err)
	}
	expectChange(t, changes, "library")
}

func TestCoalescesBurstsAndIgnoresPartFiles(t *testing.T) {
	root := t.TempDir()
	changes := startWatcher(t, Target{Kind: "library", Root: root, Depth: 1})

	writeFile(t, filepath.Join(root, "a.pkg.part"), "partial")
	expectQuiet(t, changes)

	for _, name := range []string{"a.pkg", "b.pkg", "c.pkg"} {
		writeFile(t, filepath.Join(root, name), "pkg")
	}
	expectChange(t, changes, "library")
	expectQuiet(t, changes)
}

func TestNamedTargetSeesOnlyItsFile(t *testing.T) {
	dir := t.TempDir()
	changes := startWatcher(t, Target{Kind: "games", Root: dir, Name: "games.yml"})

	writeFile(t, filepath.Join(dir, "config.yml"), "other")
	expectQuiet(t, changes)

	// Editors and RPCS3 save by writing a temporary file and renaming it over
	// the original.
	writeFile(t, filepath.Join(dir, "games.yml.tmp"), "BLUS00001: /games/a\n")
	if err := os.Rename(filepath.Join(dir, "games.yml.tmp"), filepath.Join(dir, "games.yml")); err != nil {
		t.Fatal(err)
	}
	expectChange(t, changes, "games")
}

func TestDepthLimitsWatchedLevels(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "BLUS00001", "USRDIR", "DATA")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	changes := startWatcher(t, Target{Kind: "hdd0", Root: root, Depth: 1})

	// Installing a game writes thousands of files under USRDIR; none of them
	// is reported, nor the writes Windows reports on their directories.
	writeFile(t, filepath.Join(deep, "EBOOT.BIN"), "deep")
	expectQuiet(t, changes)

	writeFile(t, filepath.Join(root, "BLUS00001", "PARAM.SFO"), "sfo")
	expectChange(t, changes, "hdd0")
}

func TestSetReplacesTargets(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	changes := make(chan string, 16)
	w, err := New(testDelay, func(kind string) { changes <- kind })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	w.Set([]Target{{Kind: "library", Root: first}, {Kind: "missing", Root: filepath.Join(first, "absent")}})
	w.Set([]Target{{Kind: "library", Root: second}})

	writeFile(t, filepath.Join(first, "old.pkg"), "pkg")
	expectQuiet(t, changes)
	writeFile(t, filepath.Join(second, "new.pkg"), "pkg")
	expectChange(t, changes, "library")
}

func TestLevelBelow(t *testing.T) {
	root := filepath.Join("a", "b")
	cases := []struct {
		path  string
		level int
		ok    bool
	}{
		{root, 0, true},
		{filepath.Join(root, "c"), 1, true},
		{filepath.Join(root, "c", "d"), 2, true},
		{filepath.Join("a", "bc"), 0, false},
		{"a", 0, false},
	}
	for _, c := range cases {
		level, ok := levelBelow(root, c.path)
		if level != c.level || ok != c.ok {
			t.Errorf("levelBelow(%q, %q) = %d, %v; want %d, %v", root, c.path, level, ok, c.level, c.ok)
		}
	}
}

// Set and Close must not wait on fsnotify while events are arriving: they
// once deadlocked with the goroutine receiving those events.
func TestSetWhileEventsArriveDoesNotDeadlock(t *testing.T) {
	root := t.TempDir()
	w, err := New(testDelay, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	writing := make(chan struct{})
	go func() {
		defer close(writing)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_ = os.WriteFile(filepath.Join(root, "f.pkg"), []byte{byte(i)}, 0o644)
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 50 {
			w.Set([]Target{{Kind: "library", Root: root, Depth: 1}})
		}
		_ = w.Close()
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Set/Close deadlocked while events were arriving")
	}
	close(stop)
	<-writing
}
