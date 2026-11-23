package autonomous

import "testing"

func TestSmokeTaskRequired(t *testing.T) {
	code := Task{ID: "t1", Title: "Add handler", Spec: "x", FilesPlanned: []string{"internal/foo/a.go"}}
	smoke := Task{ID: "t2", Title: "Smoke", Spec: "Add a section to scripts/release-smoke.sh; run scripts/release-smoke.sh", FilesPlanned: []string{"scripts/release-smoke.sh"}}
	plan := func(ts ...Task) []Story { return []Story{{ID: "s1", Tasks: ts}} }
	check := func(t *testing.T, got []LintFinding, n int, sev, task, matched string) {
		t.Helper()
		if len(got) != n {
			t.Fatalf("got %d findings %+v, want %d", len(got), got, n)
		}
		if n == 0 {
			return
		}
		f := got[0]
		if f.RuleID != "smoke_task_required" || f.Severity != sev || f.Story != "s1" || f.Task != task || f.Matched != matched {
			t.Fatalf("bad finding %+v", f)
		}
	}
	t.Run("passing", func(t *testing.T) {
		check(t, RunPlanCoverageChecks(nil, plan(code, smoke), nil), 0, "", "", "")
	})
	t.Run("failing", func(t *testing.T) {
		got := RunPlanCoverageChecks(nil, plan(code), nil)
		check(t, got, 1, "block", "t1", "")
		if got[0].Message != smokeTaskRequiredMessage {
			t.Fatal("message")
		}
	})
	t.Run("docs only", func(t *testing.T) {
		d := Task{ID: "d", Title: "Docs", Spec: "x", FilesPlanned: []string{"docs/a.md"}}
		check(t, RunPlanCoverageChecks(nil, plan(d), nil), 0, "", "", "")
	})
	t.Run("read-only not counted", func(t *testing.T) {
		r := code
		r.ReadOnly = true
		check(t, RunPlanCoverageChecks(nil, plan(r), nil), 0, "", "", "")
	})
	t.Run("template tasks not counted", func(t *testing.T) {
		for _, tk := range []Task{
			{ID: "a", Kind: TaskKindFinalGate, Title: "x"},
			{ID: "b", Title: "Final gate"},
			{ID: "c", Title: "Changelog entry"},
			{ID: "d", Title: "Version note"},
			{ID: "e", Title: "Parity issue draft"},
		} {
			tk.FilesPlanned = []string{"internal/foo/a.go"}
			check(t, RunPlanCoverageChecks(nil, plan(tk), nil), 0, "", "", "")
		}
	})
	t.Run("task exemption", func(t *testing.T) {
		c := code
		c.CoverageExemptionReason = "internal refactor"
		check(t, RunPlanCoverageChecks(nil, plan(c), nil), 1, "warn", "t1", "exempt: internal refactor")
	})
	t.Run("plan exemption", func(t *testing.T) {
		check(t, RunPlanCoverageChecks(&PRD{CoverageExemptionReason: "no surface"}, plan(code), nil), 1, "warn", "t1", "exempt: no surface")
	})
	t.Run("disabled", func(t *testing.T) {
		check(t, RunPlanCoverageChecks(nil, plan(code), []string{"smoke_task_required"}), 0, "", "", "")
	})
}

// TestSmokeTaskRequiredParsesExemption checks that the decompose JSON
// parser keeps a task's coverage_exemption_reason, so the check sees it.
func TestSmokeTaskRequiredParsesExemption(t *testing.T) {
	raw := `{"title":"t","stories":[{"title":"s","tasks":[{"title":"Refactor","spec":"x","files":["internal/foo/a.go"],"coverage_exemption_reason":"internal refactor"}]}]}`
	_, stories, err := ParsePlanning(raw)
	if err != nil {
		t.Fatalf("ParsePlanning: %v", err)
	}
	if len(stories) != 1 || len(stories[0].Tasks) != 1 {
		t.Fatalf("unexpected shape: %+v", stories)
	}
	if got := stories[0].Tasks[0].CoverageExemptionReason; got != "internal refactor" {
		t.Fatalf("CoverageExemptionReason = %q, want %q", got, "internal refactor")
	}
}
