package activity

import "testing"

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
