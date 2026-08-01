package activity

import (
	"fmt"
	"sync"
	"time"
)

const maxEntries = 9000

type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Entry is one line in the activity console. Ts is an RFC3339 timestamp
// string rather than time.Time so the Wails TS model generator produces a
// clean `string` (the generator has no mapping for time.Time and logs a
// noisy "Not found: time.Time" on every run). The frontend parses it with
// `new Date(ts)`, which handles RFC3339 fine.
type Entry struct {
	Ts      string `json:"ts"`
	Level   Level  `json:"level"`
	Scope   string `json:"scope"`
	Message string `json:"message"`
	JobID   string `json:"job_id,omitempty"`
}

type Sink struct {
	mu      sync.Mutex
	entries []Entry
	emitter Emitter
}

type Emitter interface {
	Emit(event string, data any)
}

func NewSink(emitter Emitter) *Sink {
	return &Sink{
		entries: make([]Entry, 0, 100),
		emitter: emitter,
	}
}

func (s *Sink) Log(level Level, scope, message string) {
	s.LogWithJob(level, scope, message, "")
}

func (s *Sink) LogWithJob(level Level, scope, message, jobID string) {
	s.mu.Lock()
	entry := Entry{
		Ts:      time.Now().UTC().Format(time.RFC3339Nano),
		Level:   level,
		Scope:   scope,
		Message: message,
		JobID:   jobID,
	}

	s.entries = append(s.entries, entry)
	if len(s.entries) > maxEntries {
		s.entries = s.entries[len(s.entries)-maxEntries:]
	}
	s.mu.Unlock()

	if s.emitter != nil {
		s.emitter.Emit("activity:log", entry)
	}
}

func (s *Sink) Info(scope, message string) {
	s.Log(LevelInfo, scope, message)
}

func (s *Sink) Warn(scope, message string) {
	s.Log(LevelWarn, scope, message)
}

func (s *Sink) Error(scope, message string) {
	s.Log(LevelError, scope, message)
}

func (s *Sink) InfoWithJob(scope, message, jobID string) {
	s.LogWithJob(LevelInfo, scope, message, jobID)
}

func (s *Sink) WarnWithJob(scope, message, jobID string) {
	s.LogWithJob(LevelWarn, scope, message, jobID)
}

func (s *Sink) ErrorWithJob(scope, message, jobID string) {
	s.LogWithJob(LevelError, scope, message, jobID)
}

func (s *Sink) Printf(level Level, scope, format string, args ...any) {
	s.Log(level, scope, fmt.Sprintf(format, args...))
}

func (s *Sink) Infof(scope, format string, args ...any) {
	s.Printf(LevelInfo, scope, format, args...)
}

func (s *Sink) Warnf(scope, format string, args ...any) {
	s.Printf(LevelWarn, scope, format, args...)
}

func (s *Sink) Errorf(scope, format string, args ...any) {
	s.Printf(LevelError, scope, format, args...)
}

func (s *Sink) InfofWithJob(scope, format, jobID string, args ...any) {
	s.LogWithJob(LevelInfo, scope, fmt.Sprintf(format, args...), jobID)
}

func (s *Sink) WarnfWithJob(scope, format, jobID string, args ...any) {
	s.LogWithJob(LevelWarn, scope, fmt.Sprintf(format, args...), jobID)
}

func (s *Sink) ErrorfWithJob(scope, format, jobID string, args ...any) {
	s.LogWithJob(LevelError, scope, fmt.Sprintf(format, args...), jobID)
}

func (s *Sink) GetEntries() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Entry, len(s.entries))
	copy(out, s.entries)
	return out
}

func (s *Sink) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = s.entries[:0]
}

func (s *Sink) ClearScope(scope string) {
	if scope == "" || scope == "all" {
		s.Clear()
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.entries[:0]
	for _, entry := range s.entries {
		if entry.Scope != scope {
			kept = append(kept, entry)
		}
	}
	s.entries = kept
}
