// readonly_unverifiable_test.go — the read_only_unverifiable lint rule
// and the readonly_unverifiable structural finding: a read-only task
// that declares neither a verification script nor a report path under
// .datawatch/reports/ cannot prove a failure, so both gates refuse it.

package autonomous

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// lintUnverifiableTask builds a read-only task with the given
// verification script and report path.
func lintUnverifiableTask(script, report string) Task {
	return Task{
		ID:                 "t1",
		Title:              "Gate the slice",
		Spec:               "Inspect the verdict of the slice.",
		ReadOnly:           true,
		VerificationScript: script,
		ReportPath:         report,
	}
}

// TestDecomposeLintRuleReadOnlyUnverifiable asserts each branch of the
// rule's match conditions: a read-only task with neither a verification
// script nor a usable report path is a block finding; either presence
// exempts it; a report path that is not legal (outside
// .datawatch/reports/, absolute, or with a .. segment) is *not* an
// exemption — the task is refused the same as one with no report path
// at all.
func TestDecomposeLintRuleReadOnlyUnverifiable(t *testing.T) {
	rule := builtinReadOnlyUnverifiableRule(t)
	cases := []struct {
		name string
		task Task
		want int
	}{
		{"neither script nor report", lintUnverifiableTask("", ""), 1},
		{"script exempts", lintUnverifiableTask("go test ./internal/autonomous/", ""), 0},
		{"valid report path exempts", lintUnverifiableTask("", ".datawatch/reports/verdict.md"), 0},
		{"report outside the reports dir still refused", lintUnverifiableTask("", "reports/verdict.md"), 1},
		{"absolute report path still refused", lintUnverifiableTask("", "/etc/verdict.md"), 1},
		{"dotdot report path still refused", lintUnverifiableTask("", ".datawatch/reports/../verdict.md"), 1},
	}
	for _, c := range cases {
		story := mkDecomposeLintStory("s", c.task)
		got := LintTasks([]Story{story}, []Rule{rule})
		if len(got) != c.want {
			t.Errorf("%s: %d findings, want %d: %+v", c.name, len(got), c.want, got)
			continue
		}
		if c.want == 1 {
			f := got[0]
			if f.RuleID != "read_only_unverifiable" || f.Severity != config.DecomposeLintSeverityBlock {
				t.Errorf("%s: finding %s/%s, want read_only_unverifiable/block", c.name, f.RuleID, f.Severity)
			}
			if f.Story != "s" || f.Task != "t1" {
				t.Errorf("%s: finding locates %s/%s, want s/t1", c.name, f.Story, f.Task)
			}
		}
	}

	// A non-read-only task never trips the rule, whatever else it lacks.
	other := lintUnverifiableTask("", "")
	other.ReadOnly = false
	if got := LintTasks([]Story{mkDecomposeLintStory("s", other)}, []Rule{rule}); len(got) != 0 {
		t.Errorf("non-read-only task: %d findings, want 0: %+v", len(got), got)
	}
}

// builtinReadOnlyUnverifiableRule returns the shipped read_only_unverifiable
// rule from the built-in pack, so the tests exercise the rule the
// decomposer actually runs rather than a copy that could drift from it.
func builtinReadOnlyUnverifiableRule(t *testing.T) Rule {
	t.Helper()
	for _, r := range DefaultDecomposeLintRules() {
		if r.ID == "read_only_unverifiable" {
			return r
		}
	}
	t.Fatal("built-in pack is missing the read_only_unverifiable rule")
	return Rule{}
}

// TestDecomposeLintRuleReadOnlyUnverifiableSpecMarker asserts a task
// made read-only only by the READ-ONLY: marker opening its spec is held
// to the same rule as a task with the read_only flag: refused with
// neither proof, exempt with either. A marker on a later line does not
// make the task read-only, so it is not flagged.
func TestDecomposeLintRuleReadOnlyUnverifiableSpecMarker(t *testing.T) {
	rules := []Rule{builtinReadOnlyUnverifiableRule(t)}
	mk := func(spec, script, report string) Task {
		return Task{
			ID:                 "t1",
			Title:              "Gate the slice",
			Spec:               spec,
			VerificationScript: script,
			ReportPath:         report,
		}
	}
	cases := []struct {
		name string
		task Task
		want int
	}{
		{"marker opens spec, neither proof", mk("READ-ONLY: audit the retry paths", "", ""), 1},
		{"marker after blank lines, neither proof", mk("\n  \nread-only: audit", "", ""), 1},
		{"marker opens spec, script exempts", mk("READ-ONLY: audit", "go test ./internal/autonomous/", ""), 0},
		{"marker opens spec, valid report exempts", mk("READ-ONLY: audit", "", ".datawatch/reports/audit.md"), 0},
		{"marker on a later line is not read-only", mk("Edit things.\nREAD-ONLY: nope", "", ""), 0},
	}
	for _, c := range cases {
		got := LintTasks([]Story{mkDecomposeLintStory("s", c.task)}, rules)
		if len(got) != c.want {
			t.Errorf("%s: %d findings, want %d: %+v", c.name, len(got), c.want, got)
			continue
		}
		if c.want == 1 && got[0].RuleID != "read_only_unverifiable" {
			t.Errorf("%s: finding %s, want read_only_unverifiable", c.name, got[0].RuleID)
		}
	}
}

// TestCheckPlanStructureReadOnlyUnverifiableSpecMarker asserts the
// structural finding applies to a task made read-only only by its spec
// marker, the same as to one with the read_only flag.
func TestCheckPlanStructureReadOnlyUnverifiableSpecMarker(t *testing.T) {
	mk := func(task Task) *PlanStructureReport {
		story := Story{
			ID:     "s1",
			PRDID:  "p1",
			Title:  "Slice",
			Status: StoryInProgress,
			Tasks:  []Task{task},
		}
		return CheckPlanStructure(nil, []Story{story})
	}
	marked := Task{ID: "t1", Title: "Audit the slice", Spec: "READ-ONLY: audit the retry paths", Status: TaskQueued}
	r := mk(marked)
	if r.Pass {
		t.Error("marker-only read-only task with neither proof: Pass = true, want false")
	}
	if f := findUnverifiableFinding(r); f == nil || f.TaskID != "t1" {
		t.Fatalf("marker-only read-only task: want a readonly_unverifiable finding on t1, got %+v", r.Findings)
	}

	proved := marked
	proved.ID = "t2"
	proved.ReportPath = ".datawatch/reports/audit.md"
	if f := findUnverifiableFinding(mk(proved)); f != nil {
		t.Errorf("marker-only read-only task with a report path: %s, want none", f.Detail)
	}
}

// TestDefaultPackReadOnlyUnverifiable asserts the built-in pack ships
// the rule with block severity and that its finding names both ways a
// read-only task can prove a failure: the verification script and the
// report path under .datawatch/reports/.
func TestDefaultPackReadOnlyUnverifiable(t *testing.T) {
	rules := DefaultDecomposeLintRules()
	var bi *Rule
	for i := range rules {
		if rules[i].ID == "read_only_unverifiable" {
			bi = &rules[i]
			break
		}
	}
	if bi == nil {
		t.Fatal("built-in pack is missing the read_only_unverifiable rule")
	}
	if bi.Severity != config.DecomposeLintSeverityBlock {
		t.Fatalf("read_only_unverifiable severity = %q, want block", bi.Severity)
	}

	f := LintTasks([]Story{mkDecomposeLintStory("s", lintUnverifiableTask("", ""))}, rules)
	var hit *LintFinding
	for i := range f {
		if f[i].RuleID == "read_only_unverifiable" {
			hit = &f[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("built-in rule did not fire on an unverifiable read-only task: %+v", f)
	}
	if hit.Severity != config.DecomposeLintSeverityBlock {
		t.Errorf("finding severity = %q, want block", hit.Severity)
	}
	m := strings.ToLower(hit.Message)
	if !strings.Contains(m, "verification script") {
		t.Errorf("finding message must name the verification script: %q", hit.Message)
	}
	if !strings.Contains(hit.Message, ".datawatch/reports/") {
		t.Errorf("finding message must name the report path under .datawatch/reports/: %q", hit.Message)
	}
}

// structuralTask builds a minimal in-plan task for a structural
// finding case.
func structuralTask(id string) Task {
	return Task{
		ID:       id,
		Title:    "Gate the slice",
		Spec:     "Inspect the verdict of the slice.",
		ReadOnly: true,
	}
}

// findUnverifiableFinding returns the readonly_unverifiable finding in
// a structural report, if any.
func findUnverifiableFinding(r *PlanStructureReport) *StructuralFinding {
	for i := range r.Findings {
		if r.Findings[i].Kind == FindingReadOnlyUnverifiable {
			return &r.Findings[i]
		}
	}
	return nil
}

// TestCheckPlanStructureReadOnlyUnverifiable asserts the structural
// finding for an unverifiable read-only task: blocked (Pass false) with
// the typed finding naming both the verification script and the report
// path under .datawatch/reports/; exempt when either is declared and
// usable; still refused when the report path is not legal; and no
// finding for a non-read-only task.
func TestCheckPlanStructureReadOnlyUnverifiable(t *testing.T) {
	mk := func(task Task) *PlanStructureReport {
		story := Story{
			ID:     "s1",
			PRDID:  "p1",
			Title:  "Slice",
			Status: StoryInProgress,
			Tasks:  []Task{task},
		}
		return CheckPlanStructure(nil, []Story{story})
	}

	// (a) neither script nor report: blocked, finding names both checks.
	base := structuralTask("t1")
	r := mk(base)
	if r.Pass {
		t.Error("unverifiable read-only task: Pass = true, want false")
	}
	f := findUnverifiableFinding(r)
	if f == nil {
		t.Fatalf("no readonly_unverifiable finding; got %+v", r.Findings)
	}
	if f.StoryID != "s1" || f.TaskID != "t1" {
		t.Errorf("finding locates %s/%s, want s1/t1", f.StoryID, f.TaskID)
	}
	for _, want := range []string{"verification script", ".datawatch/reports/"} {
		if !strings.Contains(f.Detail, want) {
			t.Errorf("finding detail must mention %q: %q", want, f.Detail)
		}
	}

	// (b) a verification script exempts the task.
	scripted := structuralTask("t2")
	scripted.VerificationScript = "go test ./internal/autonomous/"
	if f := findUnverifiableFinding(mk(scripted)); f != nil {
		t.Errorf("scripted read-only task: %s, want none", f.Detail)
	}

	// (c) a valid report path under .datawatch/reports/ exempts it.
	reported := structuralTask("t3")
	reported.ReportPath = ".datawatch/reports/slice-gate.md"
	if f := findUnverifiableFinding(mk(reported)); f != nil {
		t.Errorf("reported read-only task: %s, want none", f.Detail)
	}

	// (d) an invalid report path (outside .datawatch/reports/) is not
	// an exemption: the task is refused.
	invalid := structuralTask("t4")
	invalid.ReportPath = "reports/slice-gate.md"
	if f := findUnverifiableFinding(mk(invalid)); f == nil {
		t.Error("read-only task with a report path outside .datawatch/reports/ and no script: want a finding, got none")
	}

	// (e) a non-read-only task is not flagged by this rule.
	writer := Task{
		ID:           "t5",
		Title:        "Write the change",
		Spec:         "Make the edit.",
		Kind:         "feature",
		Status:       TaskQueued,
		FilesPlanned: []string{"internal/autonomous/readonly_unverifiable_test.go"},
	}
	if f := findUnverifiableFinding(mk(writer)); f != nil {
		t.Errorf("non-read-only task: %s, want none", f.Detail)
	}
}

// TestCheckPlanStructureReadOnlyUnverifiableSmokeAndFinalGateNoExemption
// asserts that no task kind is exempt from the unverifiable check: a
// smoke task and a final_gate task that carry neither a verification
// script nor a report path are findings.
func TestCheckPlanStructureReadOnlyUnverifiableSmokeAndFinalGateNoExemption(t *testing.T) {
	mk := func(kind string, id, script, report string) *PlanStructureReport {
		task := Task{
			ID:                 id,
			Title:              "Gate",
			Spec:               "Inspect the verdict of the slice.",
			Kind:               kind,
			Status:             TaskQueued,
			ReadOnly:           true,
			VerificationScript: script,
			ReportPath:         report,
		}
		story := Story{
			ID:     "s1",
			PRDID:  "p1",
			Title:  "Slice",
			Status: StoryInProgress,
			Tasks:  []Task{task},
		}
		return CheckPlanStructure(nil, []Story{story})
	}
	for _, kind := range []string{TaskKindSmoke, TaskKindFinalGate} {
		if f := findUnverifiableFinding(mk(kind, "t1", "", "")); f == nil {
			t.Errorf("kind %s with neither script nor report: want a finding, got none", kind)
		}
		// ...and the same kind *with* one of the two proofs is exempt.
		if f := findUnverifiableFinding(mk(kind, "t2", "go test ./internal/autonomous/", "")); f != nil {
			t.Errorf("kind %s with a verification script: %s, want none", kind, f.Detail)
		}
		if f := findUnverifiableFinding(mk(kind, "t3", "", ".datawatch/reports/gate.md")); f != nil {
			t.Errorf("kind %s with a report path: %s, want none", kind, f.Detail)
		}
	}
}

// TestTaskTemplateFinalGateCarriesReadOnlyUnverifiableProof asserts the
// shipped final_gate template task is not an unverifiable read-only
// task: the template def carries a usable report path under
// .datawatch/reports/ (its verdict is the proof), copied onto the
// produced task. It carries no verification script.
func TestTaskTemplateFinalGateCarriesReadOnlyUnverifiableProof(t *testing.T) {
	for _, def := range BuiltInTaskTemplates() {
		if def.ID != TemplateFinalGate {
			continue
		}
		if err := validateReportPath(def.ReportPath); err != nil {
			t.Fatalf("final_gate template def report path %q unusable: %v", def.ReportPath, err)
		}
	}
	story := testTaskTemplateStory(t)
	got, applied := ApplyTaskTemplates([]Story{story}, "readonly unverifiable proof", "proj", testTaskTemplateCfg())
	if len(got) != 1 {
		t.Fatalf("stories = %d, want 1", len(got))
	}
	appliedIDs := map[string]bool{}
	for _, id := range applied {
		appliedIDs[id] = true
	}
	for _, tsk := range got[0].Tasks {
		if !strings.Contains(tsk.Spec, TaskTemplateIDMarker(TemplateFinalGate)) {
			continue
		}
		if strings.TrimSpace(tsk.VerificationScript) == "" && !taskHasUsableReportPath(tsk) {
			t.Fatalf("template task %s: no verification script or usable report path (an unverifiable read-only task)", tsk.ID)
		}
		// and the whole template task must not trip the rule
		found := LintTasks(got, []Rule{{
			ID:       "read_only_unverifiable",
			Severity: "block",
			Message:  "m",
			Match: config.DecomposeLintMatch{
				ReadOnly:                &decomposeLintRO,
				VerificationScriptEmpty: true,
				ReportPathEmpty:         true,
			},
		}})
		for _, f := range found {
			if f.Task == tsk.ID {
				t.Fatalf("template task %s: %s", tsk.ID, f.Message)
			}
		}
	}
	_ = appliedIDs
}
