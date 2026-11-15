// Package watch reports file changes under a set of targets, coalescing each
// target's bursts of events into one callback.
package watch

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Target is one thing to watch. Root is a directory; Depth is how many levels
// of subdirectories below it are watched too (fsnotify is not recursive).
// When Name is set, only events for the file of that name directly in Root
// count, which is how a single file is watched: through its directory, so a
// save that replaces the file by rename is still seen.
type Target struct {
	Kind  string
	Root  string
	Depth int
	Name  string
}

// Watcher watches a replaceable set of targets.
//
// fsnotify's Add and Remove wait on its reader goroutine, which in turn waits
// for its events to be received. So no fsnotify call is made while w.mu is
// held or from the goroutine receiving events: Set builds a new fsnotify
// watcher and swaps it in, and directories created under a target are added
// from a goroutine of their own.
type Watcher struct {
	delay    time.Duration
	onChange func(kind string)

	mu      sync.Mutex
	fs      *fsnotify.Watcher
	targets []Target
	timers  map[string]*time.Timer
	closed  bool
}

// New returns a watcher that calls onChange(kind) once a target has been
// quiet for delay after a change. It watches nothing until Set.
func New(delay time.Duration, onChange func(kind string)) (*Watcher, error) {
	return &Watcher{delay: delay, onChange: onChange, timers: map[string]*time.Timer{}}, nil
}

// Set replaces the watched targets. A target whose root does not exist is
// skipped; it is picked up by the next Set.
func (w *Watcher) Set(targets []Target) {
	cleaned := make([]Target, 0, len(targets))
	for _, target := range targets {
		if strings.TrimSpace(target.Root) == "" {
			continue
		}
		target.Root = filepath.Clean(target.Root)
		cleaned = append(cleaned, target)
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	go w.loop(fsw)
	for _, target := range cleaned {
		addTree(fsw, target.Root, target.Depth)
	}

	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		_ = fsw.Close()
		return
	}
	old := w.fs
	w.fs = fsw
	w.targets = cleaned
	w.mu.Unlock()

	if old != nil {
		_ = old.Close()
	}
}

// Close stops the watcher and any pending callbacks.
func (w *Watcher) Close() error {
	w.mu.Lock()
	w.closed = true
	for _, timer := range w.timers {
		timer.Stop()
	}
	fsw := w.fs
	w.fs = nil
	w.mu.Unlock()

	if fsw == nil {
		return nil
	}
	return fsw.Close()
}

// addTree watches dir and its subdirectories down to depth levels.
func addTree(fsw *fsnotify.Watcher, dir string, depth int) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return
	}
	if err := fsw.Add(dir); err != nil {
		return
	}
	if depth <= 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			addTree(fsw, filepath.Join(dir, entry.Name()), depth-1)
		}
	}
}

func (w *Watcher) loop(fsw *fsnotify.Watcher) {
	for {
		select {
		case event, ok := <-fsw.Events:
			if !ok {
				return
			}
			w.handle(fsw, event)
		case _, ok := <-fsw.Errors:
			if !ok {
				return
			}
		}
	}
}

func (w *Watcher) handle(fsw *fsnotify.Watcher, event fsnotify.Event) {
	// A download in progress writes to a .part file; only the finished file
	// (renamed into place) is a change worth reporting.
	if strings.HasSuffix(event.Name, ".part") {
		return
	}
	path := filepath.Clean(event.Name)
	// Windows also reports a write to a directory when its contents or
	// attributes change. A watched directory's children report themselves,
	// and an unwatched one is below the depth that matters.
	if event.Op == fsnotify.Write {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return
		}
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fs != fsw {
		// From a watcher Set has replaced or Close has stopped.
		return
	}
	for _, target := range w.targets {
		level, ok := levelBelow(target.Root, path)
		if !ok || level == 0 {
			continue
		}
		if target.Name != "" {
			if level != 1 || !sameName(filepath.Base(path), target.Name) {
				continue
			}
		} else if level > target.Depth+1 {
			continue
		}
		// A directory created inside the watched tree is watched too, with
		// whatever it already holds (an extracted or copied folder arrives
		// whole).
		if event.Has(fsnotify.Create) && level <= target.Depth {
			go addTree(fsw, path, target.Depth-level)
		}
		w.schedule(target.Kind)
	}
}

// schedule (re)starts kind's quiet timer. The caller holds w.mu.
func (w *Watcher) schedule(kind string) {
	if timer, ok := w.timers[kind]; ok {
		timer.Stop()
	}
	w.timers[kind] = time.AfterFunc(w.delay, func() { w.onChange(kind) })
}

// levelBelow reports how many path elements path is below root, or false
// when it is not inside root.
func levelBelow(root, path string) (int, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return 0, false
	}
	if rel == "." {
		return 0, true
	}
	return len(strings.Split(rel, string(filepath.Separator))), true
}

func sameName(a, b string) bool {
	if os.PathSeparator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}
