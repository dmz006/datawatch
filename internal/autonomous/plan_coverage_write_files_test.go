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
	want := `task t9 title "update the docs" starts with write verb Update but declares no files; add the files it will change to its files list, declare a report_path under .datawatch/reports/, or make the first line of its spec READ-ONLY: <reason>`
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

func TestWriteTaskWithoutFiles_Exemptions(t *testing.T) {
	tests := []struct {
		name string
		task Task
		want bool // true = flagged
	}{
		{"read-only first line", Task{ID: "t1", Title: "Update the docs", Spec: "READ-ONLY: inspection only\nlook around"}, false},
		{"read-only after blank lines", Task{ID: "t1", Title: "Update the docs", Spec: "\n  \n  READ-ONLY: audit"}, false},
		{"marker on later line", Task{ID: "t1", Title: "Update the docs", Spec: "Edit things.\nREAD-ONLY: nope"}, true},
		{"marker lower case", Task{ID: "t1", Title: "Update the docs", Spec: "read-only: audit"}, false},
		{"marker mixed case", Task{ID: "t1", Title: "Update the docs", Spec: "Read-Only: audit"}, false},
		{"read-only without colon", Task{ID: "t1", Title: "Update the docs", Spec: "READ-ONLY audit"}, true},
		{"empty spec", Task{ID: "t1", Title: "Update the docs"}, true},
		{"kind smoke", Task{ID: "t1", Title: "Write the smoke check", Kind: TaskKindSmoke}, false},
		{"kind final_gate", Task{ID: "t1", Title: "Write the gate check", Kind: TaskKindFinalGate}, false},
		{"phrase final gate", Task{ID: "t1", Title: "Write a final gate verdict"}, false},
		{"phrase stability gate", Task{ID: "t1", Title: "Write the stability gate report"}, false},
		{"phrase suite run", Task{ID: "t1", Title: "Fix after suite run"}, false},
		{"phrase eval", Task{ID: "t1", Title: "Write an eval for the planner"}, false},
		{"phrase install", Task{ID: "t1", Title: "Update the install steps"}, false},
		{"phrase investigation", Task{ID: "t1", Title: "Write up the investigation"}, false},
		{"phrase upper case", Task{ID: "t1", Title: "Write the EVAL"}, false},
		{"suite alone is not a phrase", Task{ID: "t1", Title: "Fix the failing test suite"}, true},
		{"install in doc title word boundary", Task{ID: "t1", Title: "Update operator docs for automatic commits"}, true},
		{"eval inside longer word", Task{ID: "t1", Title: "Write the evaluation notes"}, true},
		{"installer is not install", Task{ID: "t1", Title: "Update the installer"}, true},
		{"gate phrase only in spec", Task{ID: "t1", Title: "Update the docs", Spec: "Mention the install step and the suite run."}, true},
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

func TestWriteTaskWithoutFiles_EveryGatePhraseExempts(t *testing.T) {
	for _, phrase := range planGateTitlePhrases {
		got := writeFilesFindings(Task{ID: "t1", Title: "Write the " + phrase + " notes"})
		if len(got) != 0 {
			t.Errorf("phrase %q: want no finding, got %+v", phrase, got)
		}
	}
}

func TestWriteTaskWithoutFiles_ReadOnlyProducesNoFinding(t *testing.T) {
	stories := []Story{{ID: "s1", Title: "story", Tasks: []Task{{
		ID: "t1", Title: "Update the docs", Spec: "READ-ONLY: nothing to change",
		ReportPath:               ".datawatch/reports/docs-check.md",
		VerificationScriptReason: "test",
	}}}}
	if got := CheckPlanStructure(nil, stories).Findings; len(got) != 0 {
		t.Fatalf("want no findings at all, got %+v", got)
	}
}

func TestSpecIsReadOnly(t *testing.T) {
	for spec, want := range map[string]bool{
		"":                      false,
		"   \n\t\n":             false,
		"READ-ONLY: x":          true,
		"  read-only: x":        true,
		"READ-ONLY:":            true,
		"READ-ONLY":             false,
		"READ-ONLY - x":         false,
		"a\nREAD-ONLY: x":       false,
		"\n\nREAD-ONLY: x\nfoo": true,
	} {
		if got := specIsReadOnly(spec); got != want {
			t.Errorf("specIsReadOnly(%q) = %v, want %v", spec, got, want)
		}
	}
}

// reportPathFindings runs CheckPlanStructure over a one-story plan and
// returns only the report_path_on_writer_task findings.
func reportPathFindings(t Task) []StructuralFinding {
	t.VerificationScriptReason = "test"
	stories := []Story{{ID: "s1", Title: "story", Tasks: []Task{t}}}
	var out []StructuralFinding
	for _, f := range CheckPlanStructure(nil, stories).Findings {
		if f.Kind == FindingReportPathOnWriterTask {
			out = append(out, f)
		}
	}
	return out
}

func TestReportPathOnWriterTask_Flagged(t *testing.T) {
	got := reportPathFindings(Task{ID: "t1", Title: "Write the feature",
		ReportPath: ".datawatch/reports/feature.md"})
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d: %+v", len(got), got)
	}
	f := got[0]
	if f.Kind != FindingReportPathOnWriterTask {
		t.Errorf("Kind = %q", f.Kind)
	}
	if f.StoryID != "s1" || f.TaskID != "t1" {
		t.Errorf("StoryID/TaskID = %q/%q", f.StoryID, f.TaskID)
	}
	want := "task t1 sets report_path \".datawatch/reports/feature.md\" but is not read-only; report_path is only valid on a read_only task (its report belongs under .datawatch/reports/)"
	if f.Detail != want {
		t.Errorf("Detail = %q\nwant     %q", f.Detail, want)
	}
	// Deterministic.
	again := reportPathFindings(Task{ID: "t1", Title: "Write the feature",
		ReportPath: ".datawatch/reports/feature.md"})
	if again[0].Detail != f.Detail {
		t.Error("Detail not deterministic")
	}
}

func TestReportPathOnWriterTask_NoReportPathSilent(t *testing.T) {
	if got := reportPathFindings(Task{ID: "t1", Title: "Write the feature"}); len(got) != 0 {
		t.Fatalf("a task with no report_path must not be flagged, got %+v", got)
	}
}

func TestReportPathOnWriterTask_ReadOnlySilent(t *testing.T) {
	tests := []struct {
		name string
		task Task
	}{
		{"read_only flag", Task{ID: "t1", Title: "Report on the audit",
			ReadOnly: true, ReportPath: ".datawatch/reports/audit.md"}},
		{"spec READ-ONLY marker", Task{ID: "t1", Title: "Report on the audit",
			Spec: "READ-ONLY: produces an audit report", ReportPath: ".datawatch/reports/audit.md"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := reportPathFindings(tc.task); len(got) != 0 {
				t.Fatalf("a read-only task with a report_path must not be flagged, got %+v", got)
			}
		})
	}
}

func TestWriteTaskWithoutFiles_ReadOnlyReportPathProducesNoFinding(t *testing.T) {
	// A read-only task that declares a report path and no files must be
	// free of rule 7 findings (and findings of every other kind here).
	stories := []Story{{ID: "s1", Title: "story", Tasks: []Task{{
		ID:                       "t1",
		Title:                    "Update the audit trail",
		Spec:                     "READ-ONLY: produce the audit report",
		ReportPath:               ".datawatch/reports/audit.md",
		VerificationScriptReason: "test",
	}}}}
	if got := CheckPlanStructure(nil, stories).Findings; len(got) != 0 {
		t.Fatalf("want no findings at all, got %+v", got)
	}
	// The read_only flag variant is exempt from rule 7 as well.
	stories[0].Tasks[0].Spec = ""
	stories[0].Tasks[0].ReadOnly = true
	if got := CheckPlanStructure(nil, stories).Findings; len(got) != 0 {
		t.Fatalf("want no findings at all for the flag variant, got %+v", got)
	}
}
