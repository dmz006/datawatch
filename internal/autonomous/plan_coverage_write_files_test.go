package autonomous

import (
	"strings"
	"testing"
)

// writeFilesFindings runs CheckPlanStructure over a one-story plan and
// returns only the write_task_without_files findings.
func writeFilesFindings(t Task) []StructuralFinding {
	t.VerificationScriptReason = "test"
	stories := []Story{{ID: "s1", Title: "story", Tasks: []Task{t}}}
	var out []StructuralFinding
	for _, f := range CheckPlanStructure(nil, stories).Findings {
		if f.Kind == FindingWriteTaskWithoutFiles {
			out = append(out, f)
		}
	}
	return out
}

func TestWriteTaskWithoutFiles_EveryVerbDetected(t *testing.T) {
	for _, verb := range planWriteTaskVerbs {
		got := writeFilesFindings(Task{ID: "t1", Title: verb + " the thing"})
		if len(got) != 1 {
			t.Errorf("verb %q: want 1 finding, got %d", verb, len(got))
		}
	}
}

func TestWriteTaskWithoutFiles_Table(t *testing.T) {
	tests := []struct {
		name string
		task Task
		want bool
	}{
		{"lower case", Task{ID: "t1", Title: "write the parser"}, true},
		{"upper case", Task{ID: "t1", Title: "WRITE the parser"}, true},
		{"mixed case", Task{ID: "t1", Title: "wRiTe the parser"}, true},
		{"leading whitespace", Task{ID: "t1", Title: "  Add a rule"}, true},
		{"non-verb run", Task{ID: "t1", Title: "Run the suite"}, false},
		{"non-verb review", Task{ID: "t1", Title: "Review the diff"}, false},
		{"verb after first word", Task{ID: "t1", Title: "Schedule a write"}, false},
		{"empty title", Task{ID: "t1", Title: ""}, false},
		{"verb prefix of longer word", Task{ID: "t1", Title: "Writer notes"}, false},
		{"files planned set", Task{ID: "t1", Title: "Add a rule", FilesPlanned: []string{"a.go"}}, false},
		{"only files generated", Task{ID: "t1", Title: "Add a rule", FilesGenerated: []string{"a.go"}}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := writeFilesFindings(tc.task)
			if (len(got) == 1) != tc.want || len(got) > 1 {
				t.Fatalf("want flagged=%v, got %d findings: %+v", tc.want, len(got), got)
			}
		})
	}
}

func TestWriteTaskWithoutFiles_FindingFields(t *testing.T) {
	got := writeFilesFindings(Task{ID: "t9", Title: "update the docs"})
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d", len(got))
	}
	f := got[0]
	if f.Kind != "write_task_without_files" {
		t.Errorf("Kind = %q", f.Kind)
	}
	if f.StoryID != "s1" || f.TaskID != "t9" {
		t.Errorf("StoryID/TaskID = %q/%q", f.StoryID, f.TaskID)
	}
	want := `task t9 title "update the docs" starts with write verb Update but declares no files; add the files it will change to its files list, or make the first line of its spec READ-ONLY: <reason>`
	if f.Detail != want {
		t.Errorf("Detail = %q\nwant     %q", f.Detail, want)
	}
	again := writeFilesFindings(Task{ID: "t9", Title: "update the docs"})
	if again[0].Detail != f.Detail {
		t.Errorf("Detail not deterministic")
	}
}

func TestTitleStartsWithWriteVerb(t *testing.T) {
	if !titleStartsWithWriteVerb("  fix bug") {
		t.Error("expected fix to match")
	}
	if titleStartsWithWriteVerb("Schedule a write") {
		t.Error("write after first word must not match")
	}
	if strings.TrimSpace("") != "" || titleStartsWithWriteVerb("   ") {
		t.Error("blank title must not match")
	}
}
