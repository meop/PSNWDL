package main

import (
	"testing"

	"PSNWDL/internal/jobs"
)

func TestWailsEmitterRefreshesDownloadsOnlyWhenJobCompletes(t *testing.T) {
	doneCalls := 0
	emitter := &wailsEmitter{onJobDone: func() { doneCalls++ }}

	emitter.Emit(jobs.EventJobProgress, jobs.Job{State: jobs.StateDownloading})
	emitter.Emit(jobs.EventJobState, jobs.Job{State: jobs.StateFailed})
	emitter.Emit(jobs.EventJobState, jobs.Job{State: jobs.StateDone})

	if doneCalls != 1 {
		t.Fatalf("completion callback count = %d, want 1", doneCalls)
	}
}
