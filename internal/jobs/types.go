package jobs

import "PSNWDL/internal/psn"

type JobState string

const (
	StateQueued      JobState = "queued"
	StateDownloading JobState = "downloading"
	StatePaused      JobState = "paused"
	StateResuming    JobState = "resuming"
	StateVerifying   JobState = "verifying"
	StateInstalling  JobState = "installing"
	StateDone        JobState = "done"
	StateFailed      JobState = "failed"
	StateCanceled    JobState = "canceled"
)

// Kind discriminates the job's payload + verify strategy.
//   - "title_update" (default): .pkg, PS3-style SHA-1 (body minus trailing 32 B).
//   - "title_update_drm_free": PS3 DRM-free .pkg variant from nested <url> rows.
//   - "firmware": .pup, full-file SHA-1 (when an expected hash is provided).
const (
	KindTitleUpdate        = "title_update"
	KindTitleUpdateDRMFree = "title_update_drm_free"
	KindFirmware           = "firmware"
)

// Job is a single download+verify unit. Installing is a separate explicit
// action (Queue.Install / App.InstallJob); there is no auto-install chain.
type Job struct {
	ID          string     `json:"id"`
	TitleID     string     `json:"title_id"`
	TitleName   string     `json:"title_name,omitempty"`
	Mode        string     `json:"mode"`
	Locale      string     `json:"locale,omitempty"`
	Kind        string     `json:"kind,omitempty"`
	Update      psn.Update `json:"update"`
	DestPath    string     `json:"dest_path"`
	State       JobState   `json:"state"`
	Downloaded  int64      `json:"downloaded"`
	Error       string     `json:"error,omitempty"`
	InstalledTo string     `json:"installed_to,omitempty"`
	Throughput  float64    `json:"throughput,omitempty"`
	ETA         int64      `json:"eta,omitempty"`
	Attempt     int        `json:"attempt,omitempty"`
	MaxAttempts int        `json:"max_attempts,omitempty"`
}

// Request is the frontend-facing payload to enqueue a job.
type Request struct {
	TitleID   string     `json:"title_id"`
	TitleName string     `json:"title_name,omitempty"`
	Mode      string     `json:"mode"`
	Locale    string     `json:"locale,omitempty"`
	Kind      string     `json:"kind,omitempty"`
	Update    psn.Update `json:"update"`
}

const (
	EventJobAdded    = "job:added"
	EventJobProgress = "job:progress"
	EventJobState    = "job:state"
)

// Emitter is the abstraction over Wails runtime event emission. Tests inject
// a recording emitter; production wraps wails runtime.
type Emitter interface {
	Emit(event string, data any)
}

type NoopEmitter struct{}

func (NoopEmitter) Emit(string, any) {}
