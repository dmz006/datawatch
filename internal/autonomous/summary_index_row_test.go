package autonomous

import "testing"

func TestSummaryEntryRowMatchesRecordRow(t *testing.T) {
	ci := &CIWatchResult{}
	p := &PRD{ID: "r1", Title: "T", Status: PRDRunning, TagIDs: []string{"a"}, SystemTags: []string{"project:x"},
		CIWatch: ci, NotifyEvents: []string{"done"}, GuardrailProfile: "g",
		Story: []Story{{ID: "s", Tasks: []Task{{ID: "t", Status: TaskPending}}}}}
	e := SummaryEntryOf(p)
	want := BuildSummaryRow(p)
	got := e.IndexRow()
	if got.ID != want.ID || got.TaskCount != want.TaskCount || got.StoryCount != 1 || got.Stories != nil || got.SummaryView.Stories != nil {
		t.Fatalf("row %+v", got)
	}
	if len(got.Tags) != 1 || got.EffectiveStatus != want.EffectiveStatus {
		t.Fatalf("row %+v", got)
	}
	if e.NotifyEvents[0] != "done" || e.GuardrailProfile != "g" {
		t.Fatal("notify inputs missing")
	}
	// The stored row is isolated from the record and from read-time edits.
	got.TaskStatusCounts["pending"] = 99
	if e.Row.TaskStatusCounts["pending"] != 1 {
		t.Fatal("IndexRow shares the stored count map")
	}
	if e.Row.CIWatch == ci {
		t.Fatal("row shares the record's CIWatch")
	}
}
