package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestDecomposeLintFixedDate(t *testing.T) {
	rule := Rule{ID: "fixed_calendar_date_in_test", Severity: config.DecomposeLintSeverityWarning, Message: "m",
		Match: config.DecomposeLintMatch{FixedCalendarDateInTest: true}}
	run := func(task Task) []LintFinding {
		return LintTasks([]Story{{ID: "s", Tasks: []Task{task}}}, []Rule{rule})
	}
	t.Run("test task with date", func(t *testing.T) {
		f := run(Task{ID: "a", Title: "Cover expiry", Spec: "Expire at 2026-03-14 in the test.", FilesPlanned: []string{"internal/x/expiry_test.go"}})
		if len(f) != 1 || f[0].Matched != "2026-03-14" || f[0].RuleID != "fixed_calendar_date_in_test" {
			t.Fatalf("want one finding with date, got %+v", f)
		}
		f = run(Task{ID: "b", Title: "Write the expiry test", Spec: "Use 2025-12-01."})
		if len(f) != 1 {
			t.Fatalf("title-based test task: want one finding, got %+v", f)
		}
	})
	t.Run("test task without date", func(t *testing.T) {
		if f := run(Task{ID: "a", Title: "Add a test", Spec: "Use time.Now plus one hour.", FilesPlanned: []string{"web/a.spec.ts"}}); len(f) != 0 {
			t.Fatalf("want none, got %+v", f)
		}
	})
	t.Run("non-test task with date", func(t *testing.T) {
		if f := run(Task{ID: "a", Title: "Update the changelog", Spec: "Entry dated 2026-11-16.", FilesPlanned: []string{"CHANGELOG.md"}}); len(f) != 0 {
			t.Fatalf("want none, got %+v", f)
		}
	})
	t.Run("plain clean task", func(t *testing.T) {
		if f := run(Task{ID: "a", Title: "Rename a helper", Spec: "Rename foo to bar.", FilesPlanned: []string{"internal/x/x.go"}}); len(f) != 0 {
			t.Fatalf("want none, got %+v", f)
		}
	})
	t.Run("built-in rule is warn", func(t *testing.T) {
		for _, r := range DefaultDecomposeLintRules() {
			if r.ID == "fixed_calendar_date_in_test" {
				if r.Severity != config.DecomposeLintSeverityWarning {
					t.Fatalf("severity %q", r.Severity)
				}
				return
			}
		}
		t.Fatal("built-in rule missing")
	})
}
