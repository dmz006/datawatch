package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecomposeLintText_FindingLineShapes(t *testing.T) {
	cases := []struct {
		name string
		f    LintFinding
		want string
	}{
		{"task line", LintFinding{RuleID: "bare_verbs", Severity: "warn", Task: "t1", Message: "m"}, "[warn] bare_verbs t1: m"},
		{"story fallback", LintFinding{RuleID: "thin", Severity: "block", Story: "s2", Message: "m"}, "[block] thin s2: m"},
		{"whole plan dash", LintFinding{RuleID: "thin", Severity: "warn", Message: "m"}, "[warn] thin -: m"},
	}
	for _, c := range cases {
		if got := LintFindingLine(c.f); got != c.want {
			t.Errorf("%s: LintFindingLine = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDecomposeLintText_RenderSetCleanAndLines(t *testing.T) {
	if got := RenderLintFindings(nil); got != "clean: 0 findings" {
		t.Errorf("empty set render = %q, want the clean wording", got)
	}
	findings := []LintFinding{
		{RuleID: "a", Severity: "warn", Task: "t1", Message: "m1"},
		{RuleID: "b", Severity: "block", Task: "t2", Message: "m2"},
	}
	got := RenderLintFindings(findings)
	if !strings.Contains(got, "[warn] a t1: m1\n[block] b t2: m2") {
		t.Errorf("render = %q, want one line per finding in order", got)
	}
}

func TestDecomposeLintText_RenderReportAndRecord(t *testing.T) {
	rep := DecomposeLintReport{
		PRD: "p1", Mode: "shadow", RuleCount: 6,
		Findings: []LintFinding{{RuleID: "a", Severity: "warn", Task: "t1", Message: "m"}},
		Summary:  LintSummary{Block: 0, Warn: 1},
	}
	got := RenderDecomposeLintReport(rep)
	for _, want := range []string{"decompose lint of p1", "mode shadow", "6 active rules", "0 block, 1 warn", "[warn] a t1: m"} {
		if !strings.Contains(got, want) {
			t.Errorf("report render lacks %q:\n%s", want, got)
		}
	}

	rec := &DecomposeLintRecord{
		Findings: []LintFinding{{RuleID: "a", Severity: "block", Story: "s1", Message: "m"}},
		Mode:     "enforce", Attempts: 2,
	}
	got = RenderDecomposeLintRecord("p1", rec)
	for _, want := range []string{"mode enforce", "2 attempts", "[block] a s1: m"} {
		if !strings.Contains(got, want) {
			t.Errorf("record render lacks %q:\n%s", want, got)
		}
	}
	got = RenderDecomposeLintRecord("p9", nil)
	if !strings.Contains(got, "no decompose-lint record stored for p9") {
		t.Errorf("nil record render lacks the no-record wording:\n%s", got)
	}
}

func TestDecomposeLintText_ParseOverrides(t *testing.T) {
	overrides, err := ParseDecomposeLintOverrides([]string{"mode=enforce", "max_retries=3", "disabled_rules=a, b"})
	if err != nil {
		t.Fatal(err)
	}
	if overrides["mode"] != "enforce" || overrides["max_retries"] != 3 {
		b, _ := json.Marshal(overrides)
		if !strings.Contains(string(b), `"enforce"`) || !strings.Contains(string(b), `"max_retries":3`) {
			t.Errorf("overrides = %s, wrong values", b)
		}
	}
	rules, ok := overrides["disabled_rules"].([]string)
	if !ok || len(rules) != 2 || rules[0] != "a" || rules[1] != "b" {
		t.Errorf("disabled_rules = %#v, want [a b]", overrides["disabled_rules"])
	}

	bads := [][]string{
		{"mode=bogus"},
		{"max_retries=-1"},
		{"max_retries=many"},
		{"nonsense=1"},
		{"=v"},
		{"noequals"},
		{},
	}
	for i, kv := range bads {
		if _, err := ParseDecomposeLintOverrides(kv); err == nil {
			t.Errorf("case %d (%v): expected an error", i, kv)
		}
	}
}

func TestDecomposeLintText_MergePreservesUntouched(t *testing.T) {
	current := []byte(`{"mode":"shadow","max_retries":4,"rules":[{"id":"bare_verbs","severity":"warn"}],"disabled_rules":[]}`)
	merged, err := MergeDecomposeLintBlock(current, map[string]any{"mode": "enforce"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(merged)
	for _, want := range []string{`"mode":"enforce"`, `"max_retries":4`, `"bare_verbs"`, `"disabled_rules":[]`} {
		if !strings.Contains(s, want) {
			t.Errorf("merged lacks %q:\n%s", want, s)
		}
	}

	// A null/absent current block still yields a well-formed block.
	merged, err = MergeDecomposeLintBlock(nil, map[string]any{"mode": "shadow"})
	if err != nil {
		t.Fatal(err)
	}
	var block map[string]json.RawMessage
	if err := json.Unmarshal(merged, &block); err != nil {
		t.Fatalf("merged from null is not an object: %v", err)
	}
	if string(block["mode"]) != `"shadow"` {
		t.Errorf("mode = %s, want the override", block["mode"])
	}

	// A malformed current block is an error, not a silent wipe.
	if _, err := MergeDecomposeLintBlock([]byte(`[1,2]`), map[string]any{"mode": "on"}); err == nil {
		t.Error("a non-object current block must be refused")
	}
}
