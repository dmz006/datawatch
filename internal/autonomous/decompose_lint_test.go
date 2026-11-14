package autonomous

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// Test helpers: the decompose-lint engine is pure, so tests are
// in-memory — no daemon, no operator config file, no temp dirs, no
// git. mkDecomposeLintTask and mkDecomposeLintStory fabricate the
// minimal values the engine reads (spec, title, files, read_only,
// verification script).

func mkDecomposeLintTask(id, title, spec string, files []string, readOnly bool, verif string) Task {
	t := Task{
		ID:                 id,
		Title:              title,
		Spec:               spec,
		FilesPlanned:       files,
		ReadOnly:           readOnly,
		VerificationScript: verif,
	}
	return t
}

func mkDecomposeLintStory(id string, tasks ...Task) Story {
	return Story{ID: id, Tasks: tasks}
}

func withoutFindingRule(fs []LintFinding, drop string) []LintFinding {
	out := fs[:0:0]
	for _, f := range fs {
		if f.RuleID != drop {
			out = append(out, f)
		}
	}
	return out
}

func withoutRuleID(ids []string, drop string) []string {
	out := ids[:0:0]
	for _, id := range ids {
		if id != drop {
			out = append(out, id)
		}
	}
	return out
}

func findingRuleIDs(findings []LintFinding) []string {
	ids := make([]string, 0, len(findings))
	for _, f := range findings {
		ids = append(ids, f.RuleID)
	}
	return ids
}

func findDecomposeLintFinding(findings []LintFinding, ruleID string) (LintFinding, bool) {
	for _, f := range findings {
		if f.RuleID == ruleID {
			return f, true
		}
	}
	return LintFinding{}, false
}

// TestDecomposeLintRulesPerDefaultRule is one positive case (the rule
// fires, with the expected severity and a matched excerpt) and one
// negative case (the rule must not fire) per built-in default rule, in
// the pack's order. The positive cases are built to trip exactly one
// default rule, so the assertion is on the full finding set.
func TestDecomposeLintRulesPerDefaultRule(t *testing.T) {
	rules := EffectiveRules(config.DecomposeLintConfig{})
	if len(rules) != len(DefaultDecomposeLintRules()) {
		t.Fatalf("EffectiveRules() = %d rules, want the full default pack of %d", len(rules), len(DefaultDecomposeLintRules()))
	}
	sevOf := func(id string) string {
		for _, r := range rules {
			if r.ID == id {
				return r.Severity
			}
		}
		t.Fatalf("rule %q not in effective set", id)
		return ""
	}

	cases := []struct {
		rule              string
		want              string // block|warn: the expected severity
		positive          Task
		negative          Task
		extraFindings     []string // other rules that fire unavoidably on the positive task (e.g. no_verification_script on a script-less task)
		wantMatchedSubstr string
	}{
		{
			rule: "edits_version_variable",
			want: config.DecomposeLintSeverityBlock,
			positive: mkDecomposeLintTask(
				"t1", "Bump the manifest version",
				"Update the VERSION constant to 1.2.3 in the manifest.",
				[]string{"internal/app/manifest.go"}, false, "go test ./internal/app/..."),
			negative: mkDecomposeLintTask(
				"n1", "Fix the widget render",
				"Repair the layout math in internal/w.go and run the package tests.",
				[]string{"internal/w.go"}, false, "go test ./internal/..."),
			wantMatchedSubstr: "VERSION constant",
		},
		{
			rule: "runs_hosting_cli",
			want: config.DecomposeLintSeverityBlock,
			positive: mkDecomposeLintTask(
				"t2", "Open the pull request",
				"Open the pull request with gh pr create --fill, then post the checklist.",
				nil, false, "go test ./..."),
			negative: mkDecomposeLintTask(
				"n2", "Publish the package",
				"Publish the package to the operator's registry and record the digest.",
				nil, false, "go build ./..."),
			wantMatchedSubstr: "gh pr",
		},
		{
			rule: "starts_daemon_outside_sandbox",
			want: config.DecomposeLintSeverityBlock,
			positive: mkDecomposeLintTask(
				"t3", "Warm the cache",
				"Start the cache with systemctl start cachd before running the benchmark.",
				nil, false, "go test -bench . -count=1 ./internal/bench/..."),
			negative: mkDecomposeLintTask(
				"n3", "Benchmark the parser",
				"Run the in-process benchmark loop and write the timing table.",
				nil, false, "go test ./internal/..."),
			wantMatchedSubstr: "systemctl start",
		},
		{
			rule: "reads_operator_data_or_config_folders",
			want: config.DecomposeLintSeverityBlock,
			positive: mkDecomposeLintTask(
				"t4", "Dump the local data",
				"Read the operator's private store at ~/.datawatch/session.json and print the token.",
				nil, false, "sh -c 'true'"),
			negative: mkDecomposeLintTask(
				"n4", "Read the repo docs",
				"Read docs/README.md for the expected output format.",
				[]string{"docs/README.md"}, false, "sh -c 'true'"),
			wantMatchedSubstr: ".datawatch",
		},
		{
			rule: "read_only_but_writes_files",
			want: config.DecomposeLintSeverityBlock,
			positive: mkDecomposeLintTask(
				"t5", "Survey the tree",
				"Collect the dependency inventory for the tree.",
				[]string{"internal/autonomous/models.go"}, true, "sh -c 'true'"),
			negative: mkDecomposeLintTask(
				"n5", "Survey the tree",
				"Collect the dependency inventory for the tree.",
				nil, true, "sh -c 'true'"),
			wantMatchedSubstr: "internal/autonomous/models.go",
		},
		{
			rule: "read_only_unverifiable",
			want: config.DecomposeLintSeverityBlock,
			positive: mkDecomposeLintTask(
				"t6", "Gate the slice",
				"Inspect the verdict of the slice.", nil, true, ""),
			// exempt: the same read-only task writing a report under
			// .datawatch/reports/ instead of running a verification script
			negative: func() Task {
				tk := mkDecomposeLintTask(
					"n6", "Gate the slice",
					"Inspect the verdict of the slice.", nil, true, "")
				tk.ReportPath = ".datawatch/reports/slice-gate.md"
				return tk
			}(),
			// The rule's any_of matches on the read_only flag, so the
			// excerpt names that branch.
			wantMatchedSubstr: "read_only=true",
			extraFindings:     []string{"no_verification_script"},
		},
		{
			rule: "no_verification_script",
			want: config.DecomposeLintSeverityWarning,
			positive: mkDecomposeLintTask(
				"t6", "Refactor the allocator",
				"Consolidate the arena paths in internal/mem/arena.go.",
				[]string{"internal/mem/arena.go"}, false, ""),
			negative: mkDecomposeLintTask(
				"n6", "Refactor the allocator",
				"Consolidate the arena paths in internal/mem/arena.go.",
				[]string{"internal/mem/arena.go"}, false, "go test ./internal/mem/..."),
			wantMatchedSubstr: "verification_script",
		},
		{
			rule: "names_tracker_id",
			want: config.DecomposeLintSeverityWarning,
			positive: mkDecomposeLintTask(
				"t7", "Fix the reported crash",
				"Reproduce and fix the crash described in BL123 in internal/core/sched.go.",
				[]string{"internal/core/sched.go"}, false, "go test ./internal/core/..."),
			negative: mkDecomposeLintTask(
				"n7", "Tighten the scheduler",
				"Reduce preemption overhead in internal/core/sched.go and add a regression test.",
				[]string{"internal/core/sched.go"}, false, "go test ./internal/core/..."),
			wantMatchedSubstr: "BL123",
		},
	}

	for _, c := range cases {
		// sanity: the declared severity matches the pack
		if got := sevOf(c.rule); got != c.want {
			t.Errorf("rule %q severity = %q, want %q", c.rule, got, c.want)
		}

		pos := LintTasks([]Story{mkDecomposeLintStory("s-pos", c.positive)}, rules)
		// The fixtures use command strings as scripts; the
		// unexecutable_verification_script rule has its own test.
		ids := withoutRuleID(findingRuleIDs(pos), "unexecutable_verification_script")
		want := append([]string{c.rule}, c.extraFindings...)
		if len(ids) != len(want) {
			t.Errorf("rule %q positive: findings = %v, want %v", c.rule, ids, want)
			continue
		}
		for i, w := range want {
			if ids[i] != w {
				t.Errorf("rule %q positive: findings = %v, want %v", c.rule, ids, want)
				break
			}
		}
		f, _ := findDecomposeLintFinding(pos, c.rule)
		if f.Severity != c.want {
			t.Errorf("rule %q positive: severity = %q, want %q", c.rule, f.Severity, c.want)
		}
		if f.Story != "s-pos" || f.Task != c.positive.ID {
			t.Errorf("rule %q positive: story/task = %q/%q, want s-pos/%q", c.rule, f.Story, f.Task, c.positive.ID)
		}
		if f.Message == "" {
			t.Errorf("rule %q positive: finding message is empty", c.rule)
		}
		if c.wantMatchedSubstr != "" && !strings.Contains(f.Matched, c.wantMatchedSubstr) {
			t.Errorf("rule %q positive: matched excerpt %q does not contain %q", c.rule, f.Matched, c.wantMatchedSubstr)
		}

		neg := LintTasks([]Story{mkDecomposeLintStory("s-neg", c.negative)}, rules)
		if _, ok := findDecomposeLintFinding(neg, c.rule); ok {
			t.Errorf("rule %q negative: rule fired, want no finding (all findings: %v)", c.rule, findingRuleIDs(neg))
		}
	}
}

// TestDecomposeLintEffectiveRulesDefault asserts the pack's ids and
// severities as shipped.
func TestDecomposeLintEffectiveRulesDefault(t *testing.T) {
	want := []struct{ id, sev string }{
		{"edits_version_variable", "block"},
		{"runs_hosting_cli", "block"},
		{"starts_daemon_outside_sandbox", "block"},
		{"reads_operator_data_or_config_folders", "block"},
		{"read_only_but_writes_files", "block"},
		{"read_only_unverifiable", "block"},
		{"read_only_task_writes_file", "block"},
		{"spec_path_outside_allowed_dirs", "block"},
		{"verify_script_task_files_only", "block"},
		{"verify_script_docs_without_sync", "block"},
		{"final_gate_without_script", "block"},
		{"no_verification_script", "warn"},
		{"unexecutable_verification_script", "warn"},
		{"names_tracker_id", "warn"},
		{"version_number_in_spec", "warn"},
		{"unbounded_load_step", "warn"},
	}
	got := EffectiveRules(config.DecomposeLintConfig{})
	if len(got) != len(want) {
		t.Fatalf("got %d rules, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].ID != w.id || got[i].Severity != w.sev {
			t.Errorf("rule[%d] = %q/%q, want %q/%q", i, got[i].ID, got[i].Severity, w.id, w.sev)
		}
	}
}

// TestDecomposeLintOperatorOverride asserts that an operator rule with
// a built-in id replaces the built-in (message and severity) at its
// original position, and that a new id is appended.
func TestDecomposeLintOperatorOverride(t *testing.T) {
	cfg := config.DecomposeLintConfig{
		Rules: []config.DecomposeLintRule{
			{
				ID:       "runs_hosting_cli",
				Severity: config.DecomposeLintSeverityWarning, // operator relaxes block -> warn
				Message:  "Operator-local guidance for hosting CLI use.",
				Match:    config.DecomposeLintMatch{SpecRegex: "custom\\s+hosting\\b"},
			},
			{
				ID:       "custom_lint",
				Severity: config.DecomposeLintSeverityBlock,
				Message:  "Operator-specific rule.",
				Match:    config.DecomposeLintMatch{SpecRegex: "operator\\bmarker\\b"},
			},
		},
	}
	got := EffectiveRules(cfg)
	if len(got) != 17 {
		t.Fatalf("got %d rules, want 16 built-in + 1 new", len(got))
	}
	// replaced at its original position (index 1)
	if got[1].ID != "runs_hosting_cli" || got[1].Message != "Operator-local guidance for hosting CLI use." {
		t.Fatalf("runs_hosting_cli not replaced: %+v", got[1])
	}
	if got[1].Severity != config.DecomposeLintSeverityWarning {
		t.Errorf("runs_hosting_cli severity = %q, want warn (operator value)", got[1].Severity)
	}
	// the new rule is appended last
	last := got[len(got)-1]
	if last.ID != "custom_lint" || last.Severity != config.DecomposeLintSeverityBlock {
		t.Fatalf("custom rule not appended: %+v", last)
	}

	// and the override is visible through the engine
	story := mkDecomposeLintStory("s1", mkDecomposeLintTask(
		"t1", "hosting", "call the custom hosting helper to register the route.", nil, false, "sh -c 'true'"))
	f := withoutFindingRule(LintTasks([]Story{story}, got), "unexecutable_verification_script")
	if len(f) != 1 || f[0].RuleID != "runs_hosting_cli" || f[0].Severity != "warn" {
		t.Fatalf("engine after override: %v, want one warn finding from runs_hosting_cli", f)
	}
}

// TestDecomposeLintDisabledRules covers both disable mechanisms: a
// rule flagged disabled:true (a bare {id, disabled: true} entry must
// pass config.Validate) and a disable list (DisabledRules).
func TestDecomposeLintDisabledRules(t *testing.T) {
	idsOf := func(rs []Rule) []string {
		out := make([]string, len(rs))
		for i, r := range rs {
			out[i] = r.ID
		}
		return out
	}
	absent := func(t *testing.T, rs []Rule, ids ...string) {
		t.Helper()
		for _, id := range ids {
			for _, r := range rs {
				if r.ID == id {
					t.Fatalf("disabled rule %q survived: %v", id, idsOf(rs))
				}
			}
		}
	}

	// (a) disabled flag on an operator rule naming a built-in id.
	cfgA := config.DecomposeLintConfig{
		Rules: []config.DecomposeLintRule{
			{ID: "runs_hosting_cli", Disabled: true},
			{ID: "names_tracker_id", Disabled: true},
		},
	}
	if err := cfgA.Validate(); err != nil {
		t.Fatalf("Validate with bare disabled rules: %v", err)
	}
	gotA := EffectiveRules(cfgA)
	if ids := idsOf(gotA); len(ids) != 14 {
		t.Fatalf("disabled-flag: got %v, want the 14 non-disabled built-ins", ids)
	}
	absent(t, gotA, "runs_hosting_cli", "names_tracker_id")

	// (b) the disable list removes built-ins by id.
	cfgB := config.DecomposeLintConfig{
		DisabledRules: []string{"edits_version_variable", "no_verification_script"},
	}
	gotB := EffectiveRules(cfgB)
	if ids := idsOf(gotB); len(ids) != 14 {
		t.Fatalf("disabled-list: got %v, want the 14 non-listed built-ins", ids)
	}
	absent(t, gotB, "edits_version_variable", "no_verification_script")

	// (c) disabling an operator-added rule removes it; the built-in
	// pack (16 rules) is untouched.
	cfgC := config.DecomposeLintConfig{
		Rules: []config.DecomposeLintRule{
			{ID: "operator_rule", Severity: "block", Message: "m", Match: config.DecomposeLintMatch{SpecRegex: "x"}, Disabled: true},
		},
	}
	gotC := EffectiveRules(cfgC)
	if ids := idsOf(gotC); len(ids) != 16 {
		t.Fatalf("operator rule disabled: got %v, want the 16 built-ins", ids)
	}
	absent(t, gotC, "operator_rule")

	// (d) the disable list also removes a built-in that an operator
	// tried to override: the list wins.
	cfgD := config.DecomposeLintConfig{
		DisabledRules: []string{"runs_hosting_cli"},
		Rules:         []config.DecomposeLintRule{{ID: "runs_hosting_cli", Severity: "block", Message: "m", Match: config.DecomposeLintMatch{SpecRegex: "x"}}},
	}
	gotD := EffectiveRules(cfgD)
	if len(gotD) != 15 {
		t.Fatalf("list-wins: got %v, want 15 rules", idsOf(gotD))
	}
	absent(t, gotD, "runs_hosting_cli")
}

// TestDecomposeLintInvalidRegexTolerance asserts that a rule with an
// invalid regular expression (top level, or nested in any_of) is
// skipped with a reportable error instead of crashing or taking down
// the healthy rules beside it.
func TestDecomposeLintInvalidRegexTolerance(t *testing.T) {
	good := config.DecomposeLintRule{
		ID:       "marker_rule",
		Severity: "block",
		Message:  "marker present",
		Match:    config.DecomposeLintMatch{SpecRegex: "operatormarker"},
	}
	badTop := config.DecomposeLintRule{
		ID:       "bad_regex_top",
		Severity: "block",
		Message:  "should never fire",
		Match:    config.DecomposeLintMatch{SpecRegex: "[unclosed"},
	}
	badNested := config.DecomposeLintRule{
		ID:       "bad_regex_nested",
		Severity: "block",
		Message:  "should never fire",
		Match: config.DecomposeLintMatch{
			AnyOf: []config.DecomposeLintMatch{
				{SpecRegex: "fine"},
				{SpecRegex: "("},
			},
		},
	}

	story := mkDecomposeLintStory("s1", mkDecomposeLintTask("t1", "x", "has operatormarker inside", nil, false, "sh -c 'true'"))

	findings, errs := LintTasksWithErrors([]Story{story}, []Rule{badTop, good, badNested})
	if len(errs) != 2 {
		t.Fatalf("got %d errors, want 2 (one per bad rule): %v", len(errs), errs)
	}
	joined := strings.Join(errorStrings(errs), " | ")
	for _, want := range []string{"bad_regex_top", "bad_regex_nested"} {
		if !strings.Contains(joined, want) {
			t.Errorf("error list does not name rule %q: %q", want, joined)
		}
	}
	if len(findings) != 1 || findings[0].RuleID != "marker_rule" {
		t.Fatalf("healthy rule not applied: %v", findings)
	}

	// the convenience API drops the errors, still runs the good rules
	if f := LintTasks([]Story{story}, []Rule{badTop, good, badNested}); len(f) != 1 || f[0].RuleID != "marker_rule" {
		t.Fatalf("LintTasks: %v, want the single marker finding", f)
	}
}

func errorStrings(errs []error) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Error()
	}
	return out
}

// TestDecomposeLintMatchPredicateAndComposition exercises the field
// predicates (read_only, files emptiness, verification-script emptiness)
// and the any_of / all_of composition directly, with synthetic rules.
func TestDecomposeLintMatchPredicateAndComposition(t *testing.T) {
	roTrue := true
	roFalse := false

	cases := []struct {
		name string
		rule config.DecomposeLintRule
		task Task
		want int
	}{
		{
			name: "any_of_spec_or_title_hits",
			rule: config.DecomposeLintRule{
				ID: "any_of_spec_or_title_hits", Severity: "warn", Message: "m",
				Match: config.DecomposeLintMatch{AnyOf: []config.DecomposeLintMatch{
					{SpecRegex: "applesauce"},
					{TitleRegex: "banana"},
				}},
			},
			task: mkDecomposeLintTask("t", "banana split", "nothing to report", nil, false, "sh -c 'true'"),
			want: 1,
		},
		{
			name: "any_of_neither_hits",
			rule: config.DecomposeLintRule{
				ID: "any_of_neither_hits", Severity: "warn", Message: "m",
				Match: config.DecomposeLintMatch{AnyOf: []config.DecomposeLintMatch{
					{SpecRegex: "applesauce"},
					{TitleRegex: "banana"},
				}},
			},
			task: mkDecomposeLintTask("t", "cider press", "nothing to report", nil, false, "sh -c 'true'"),
			want: 0,
		},
		{
			name: "all_of_both_predicates_hit",
			rule: config.DecomposeLintRule{
				ID: "all_of_both_predicates_hit", Severity: "warn", Message: "m",
				Match: config.DecomposeLintMatch{AllOf: []config.DecomposeLintMatch{
					{ReadOnly: &roTrue},
					{VerificationScriptEmpty: true},
				}},
			},
			task: mkDecomposeLintTask("t", "survey", "inventory the tree", nil, true, ""),
			want: 1,
		},
		{
			name: "all_of_one_predicate_absent",
			rule: config.DecomposeLintRule{
				ID: "all_of_one_predicate_absent", Severity: "warn", Message: "m",
				Match: config.DecomposeLintMatch{AllOf: []config.DecomposeLintMatch{
					{ReadOnly: &roTrue},
					{VerificationScriptEmpty: true},
				}},
			},
			task: mkDecomposeLintTask("t", "survey", "inventory the tree", nil, false, ""),
			want: 0,
		},
		{
			name: "read_only_false_asserts",
			rule: config.DecomposeLintRule{
				ID: "read_only_false_asserts", Severity: "warn", Message: "m",
				Match: config.DecomposeLintMatch{ReadOnly: &roFalse},
			},
			task: mkDecomposeLintTask("t", "edit", "change the parser", []string{"internal/p.go"}, true, "sh -c 'true'"),
			want: 0,
		},
		{
			name: "files_regex_requires_an_entry",
			rule: config.DecomposeLintRule{
				ID: "files_regex_requires_an_entry", Severity: "warn", Message: "m",
				Match: config.DecomposeLintMatch{FilesRegex: "internal/"},
			},
			task: mkDecomposeLintTask("t", "edit", "change the parser", []string{"docs/notes.md"}, false, "sh -c 'true'"),
			want: 0,
		},
	}

	for _, c := range cases {
		got := LintTasks([]Story{mkDecomposeLintStory("s", c.task)}, []Rule{c.rule})
		if len(got) != c.want {
			t.Errorf("%s: got %d findings, want %d (%v)", c.name, len(got), c.want, got)
		}
	}
}

// TestDecomposeLintSummaryCounts checks LintSummary over a mixed
// finding set.
func TestDecomposeLintSummaryCounts(t *testing.T) {
	findings := []LintFinding{
		{RuleID: "a", Severity: "block"},
		{RuleID: "b", Severity: "warn"},
		{RuleID: "c", Severity: "BLOCK"}, // tolerated case
	}
	s := SummaryOf(findings)
	if s.Block != 2 || s.Warn != 1 || s.Total() != 3 {
		t.Fatalf("SummaryOf = %+v, want block=2 warn=1 total=3", s)
	}
	if e := SummaryOf(nil); e != (LintSummary{}) {
		t.Fatalf("SummaryOf(nil) = %+v, want zero summary", e)
	}
}
