package activity

import (
	"strings"
	"testing"
)

func TestClearScopeOnlyRemovesMatchingEntries(t *testing.T) {
	sink := NewSink(nil)
	sink.Info("psn", "lookup")
	sink.Info("jobs", "download")
	sink.Info("psn", "complete")

	sink.ClearScope("psn")
	entries := sink.GetEntries()
	if len(entries) != 1 || entries[0].Scope != "jobs" {
		t.Fatalf("entries after scoped clear = %+v", entries)
	}
}

func TestLoggingHelpersAndClear(t *testing.T) {
	sink := NewSink(nil)
	sink.Warn("psn", "warning")
	sink.Error("jobs", "error")
	sink.Infof("library", "stored %d", 2)
	sink.WarnfWithJob("pkg", "retry %d", "job-1", 3)

	entries := sink.GetEntries()
	if len(entries) != 4 {
		t.Fatalf("entry count = %d, want 4", len(entries))
	}
	if entries[0].Level != "warn" || entries[1].Level != "error" {
		t.Fatalf("levels = %q, %q", entries[0].Level, entries[1].Level)
	}
	if !strings.Contains(entries[2].Message, "stored 2") || entries[3].JobID != "job-1" {
		t.Fatalf("formatted entries = %+v", entries)
	}

	sink.Clear()
	if got := sink.GetEntries(); len(got) != 0 {
		t.Fatalf("entries after Clear = %+v", got)
	}
}
