package autonomous

import (
	"strings"
	"testing"
)

func TestDecomposeLintText_FindingLine(t *testing.T) {
	cases := []struct {
		name string
		in   LintFinding
		want string
	}{
		{
			name: "task-scoped",
			in:   LintFinding{RuleID: "bare_verbs", Severity: "warn", Task: "t1", Message: "starts with a bare verb"},
			want: "[warn] bare_verbs t1: starts with a bare verb",
		},
		{
			name: "story-scoped falls back to the story id",
			in:   LintFinding{RuleID: "no_test_mention", Severity: "block", Story: "s1", Message: "no test"},
			want: "[block] no_test_mention s1: no test",
		},
		{
			name: "whole-plan finding uses a dash",
			in:   LintFinding{RuleID: "duplicate_titles", Severity: "warn", Message: "two stories share a title"},
			want: "[warn] duplicate_titles -: two stories share a title",
		},
	}
	for _, tc := range cases {
		if got := LintFindingLine(tc.in); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestDecomposeLintText_FindingsEmptyIsClean(t *testing.T) {
	if got := RenderLintFindings(nil); got != "clean: 0 findings" {
		t.Errorf("nil findings: got %q", got)
	}
}

func TestDecomposeLintText_ReportWording(t *testing.T) {
	rep := DecomposeLintReport{
		PRD:       "p1",
		Mode:      "shadow",
		RuleCount: 6,
		Findings: []LintFinding{
			{RuleID: "bare_verbs", Severity: "warn", Task: "t1", Message: "starts with a bare verb"},
			{RuleID: "no_test_mention", Severity: "block", Task: "t2", Message: "no test proves it"},
		},
		Summary: LintSummary{Block: 1, Warn: 1},
	}
	got := RenderDecomposeLintReport(rep)
	for _, want := range []string{
		"decompose lint of p1 (mode shadow, 6 active rules): 1 block, 1 warn",
		"[warn] bare_verbs t1: starts with a bare verb",
		"[block] no_test_mention t2: no test proves it",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report lacks %q:\n%s", want, got)
		}
	}
}

func TestDecomposeLintText_RecordAndNoRecord(t *testing.T) {
	rec := &DecomposeLintRecord{
		Mode:     "enforce",
		Attempts: 2,
		Findings: []LintFinding{
			{RuleID: "bare_verbs", Severity: "warn", Task: "t1", Message: "starts with a bare verb"},
		},
	}
	got := RenderDecomposeLintRecord("p1", rec)
	for _, want := range []string{
		"decompose lint of p1 (mode enforce, 2 attempts, 1 findings)",
		"[warn] bare_verbs t1: starts with a bare verb",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("record lacks %q:\n%s", want, got)
		}
	}

	got = RenderDecomposeLintRecord("p9", nil)
	if !strings.Contains(got, "no decompose-lint record stored for p9") {
		t.Errorf("no-record wording wrong:\n%s", got)
	}
}

func TestDecomposeLintText_OverridesValid(t *testing.T) {
	ov, err := ParseDecomposeLintOverrides([]string{
		"mode=enforce",
		"max_retries=2",
		"disabled_rules=bare_verbs, no_test_mention",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ov["mode"] != "enforce" || ov["max_retries"] != 2 {
		t.Errorf("overrides = %v", ov)
	}
	rules, ok := ov["disabled_rules"].([]string)
	if !ok || len(rules) != 2 || rules[0] != "bare_verbs" || rules[1] != "no_test_mention" {
		t.Errorf("disabled_rules = %v", ov["disabled_rules"])
	}
}

func TestDecomposeLintText_OverridesMaxRetriesZeroIsLegal(t *testing.T) {
	ov, err := ParseDecomposeLintOverrides([]string{"max_retries=0"})
	if err != nil {
		t.Fatal(err)
	}
	if ov["max_retries"] != 0 {
		t.Errorf("max_retries = %v, want 0", ov["max_retries"])
	}
}

func TestDecomposeLintText_OverridesInvalid(t *testing.T) {
	bad := [][]string{
		{"mode=bogus"},
		{"mode="},
		{"max_retries=-1"},
		{"max_retries=many"},
		{"nonsense=1"},
		{"nopair"},
	}
	for i, pairs := range bad {
		if _, err := ParseDecomposeLintOverrides(pairs); err == nil {
			t.Errorf("case %d (%v): expected an error", i, pairs)
		}
	}
}
