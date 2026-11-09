// daemon_mode_checks_test.go — daemon-mode template tasks are skipped by the
// decompose lint and the plan structure check; llm-mode twins still raise
// the same findings (see daemon_mode_checks.go).
package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// daemonCheckBaseTask is a task that raises no finding at all: it has a
// verification script, a write verb with its files listed and no read-only
// flag. Each case below breaks exactly one rule on top of it.
func daemonCheckBaseTask() Task {
	return Task{
		ID:                 "task-base",
		StoryID:            "story-1",
		Title:              "Add the parser",
		Spec:               "Parse the input.",
		VerificationScript: "scripts/check.sh",
		FilesPlanned:       []string{"parser.go"},
	}
}

// daemonCheckLintCase is one per-task lint rule and a task that trips it.
type daemonCheckLintCase struct {
	rule  string
	build func(t *Task)
}

var daemonCheckLintCases = []daemonCheckLintCase{
	{rule: "no_verification_script", build: func(t *Task) {
		t.VerificationScript = ""
	}},
	{rule: "unexecutable_verification_script", build: func(t *Task) {
		t.VerificationScript = "go test ./..." // a command string, not a script file
	}},
	{rule: "read_only_unverifiable", build: func(t *Task) {
		t.ReadOnly = true
		t.FilesPlanned = nil
		t.VerificationScript = ""
		t.ReportPath = ""
	}},
	{rule: "unbounded_load_step", build: func(t *Task) {
		t.Spec = "Run a stress test of the parser."
	}},
}

// lintRuleIDs returns the set of rule ids that fire for one task alone,
// under the default effective rule set.
func lintRuleIDs(t *testing.T, task Task) map[string]bool {
	t.Helper()
	return lintRuleIDsWith(t, task, EffectiveRules(config.DecomposeLintConfig{}))
}

// lintRuleIDsWith is lintRuleIDs under an explicit rule set.
func lintRuleIDsWith(t *testing.T, task Task, rules []Rule) map[string]bool {
	t.Helper()
	stories := []Story{{ID: "story-1", Tasks: []Task{task}}}
	findings, errs := LintTasksInDir("", stories, rules)
	if len(errs) != 0 {
		t.Fatalf("rule compile errors: %v", errs)
	}
	ids := make(map[string]bool, len(findings))
	for _, f := range findings {
		ids[f.RuleID] = true
	}
	return ids
}

// TestDaemonModeTemplateSkipsLintRules: a daemon-mode task raises no lint
// finding for any per-task rule, while the same task in llm mode raises it.
func TestDaemonModeTemplateSkipsLintRules(t *testing.T) {
	if base := lintRuleIDs(t, daemonCheckBaseTask()); len(base) != 0 {
		t.Fatalf("base task fixture raises findings %v; it must be clean", base)
	}
	for _, c := range daemonCheckLintCases {
		t.Run(c.rule, func(t *testing.T) {
			llm := daemonCheckBaseTask()
			c.build(&llm)
			if got := lintRuleIDs(t, llm); !got[c.rule] {
				t.Fatalf("llm-mode twin raises %v, want %q", got, c.rule)
			}

			daemon := llm
			daemon.DaemonRun = true
			if got := lintRuleIDs(t, daemon); len(got) != 0 {
				t.Fatalf("daemon-mode task raises %v, want no findings", got)
			}
		})
	}
}

// daemonCheckPlanCase is one per-task plan-structure finding and a task
// that trips it.
type daemonCheckPlanCase struct {
	kind  string
	build func(t *Task)
}

var daemonCheckPlanCases = []daemonCheckPlanCase{
	{kind: FindingMissingVerification, build: func(t *Task) {
		t.VerificationScript = ""
		t.VerificationScriptReason = ""
	}},
	{kind: FindingUnexecutableVerification, build: func(t *Task) {
		t.VerificationScript = "go test ./..."
	}},
	{kind: FindingWriteTaskWithoutFiles, build: func(t *Task) {
		t.Title = "Implement the parser"
		t.FilesPlanned = nil
	}},
	{kind: FindingReadOnlyUnverifiable, build: func(t *Task) {
		t.ReadOnly = true
		t.FilesPlanned = nil
		t.VerificationScript = ""
		t.ReportPath = ""
	}},
	{kind: FindingReportPathOnWriterTask, build: func(t *Task) {
		t.ReportPath = ".datawatch/reports/parser.md"
	}},
}

// planKindsFor returns the set of structural finding kinds for one task.
func planKindsFor(task Task) map[string]bool {
	stories := []Story{{ID: "story-1", Tasks: []Task{task}}}
	kinds := make(map[string]bool)
	for _, f := range CheckPlanStructure(&PRD{}, stories).Findings {
		kinds[f.Kind] = true
	}
	return kinds
}

// TestDaemonModeTemplateSkipsEveryLintRule: the skip is rule-agnostic, so a
// rule that names no built-in id (here an operator-style spec match) is also
// silent on a daemon-mode task. It covers the tracker-id rule's path without
// writing a tracker id into the test.
func TestDaemonModeTemplateSkipsEveryLintRule(t *testing.T) {
	rules := []Rule{{
		ID:       "custom_spec_rule",
		Severity: config.DecomposeLintSeverityWarning,
		Message:  "spec starts with a parse verb",
		Match:    config.DecomposeLintMatch{SpecRegex: "Parse"},
	}}
	llm := daemonCheckBaseTask()
	if got := lintRuleIDsWith(t, llm, rules); !got["custom_spec_rule"] {
		t.Fatalf("llm-mode twin raises %v, want custom_spec_rule", got)
	}
	daemon := llm
	daemon.DaemonRun = true
	if got := lintRuleIDsWith(t, daemon, rules); len(got) != 0 {
		t.Fatalf("daemon-mode task raises %v, want no findings", got)
	}
}

// TestDaemonModeTemplateSkipsPlanStructure: a daemon-mode task raises no
// per-task structural finding, while the same task in llm mode raises it.
func TestDaemonModeTemplateSkipsPlanStructure(t *testing.T) {
	if base := planKindsFor(daemonCheckBaseTask()); len(base) != 0 {
		t.Fatalf("base task fixture raises findings %v; it must be clean", base)
	}
	for _, c := range daemonCheckPlanCases {
		t.Run(c.kind, func(t *testing.T) {
			llm := daemonCheckBaseTask()
			c.build(&llm)
			if got := planKindsFor(llm); !got[c.kind] {
				t.Fatalf("llm-mode twin raises %v, want %q", got, c.kind)
			}

			daemon := llm
			daemon.DaemonRun = true
			if got := planKindsFor(daemon); len(got) != 0 {
				t.Fatalf("daemon-mode task raises %v, want no findings", got)
			}
		})
	}
}

// TestDaemonModeTemplateSkipsMarkedTemplateTask: a planner-written template
// task (marker in its spec, DaemonRun not set) is daemon-mode when its
// template resolves to daemon mode, and is checked as usual when it is llm.
// The checks take no config, so the manager stamps DaemonRun on a copy.
func TestDaemonModeTemplateSkipsMarkedTemplateTask(t *testing.T) {
	marked := daemonCheckBaseTask()
	marked.VerificationScript = ""
	marked.Spec = "Write the changelog entry (" + TaskTemplateIDMarker("changelog_entry") + ")."
	marked.Title = "Write the changelog entry"
	marked.FilesPlanned = nil
	stories := []Story{{ID: "story-1", Tasks: []Task{marked}}}

	// Daemon mode: the shipped default for changelog_entry.
	daemonAuto := &config.AutonomousConfig{}
	stamped := stampDaemonModeTasks(stories, daemonAuto)
	if !stamped[0].Tasks[0].DaemonRun {
		t.Fatal("marked daemon-mode task was not stamped DaemonRun")
	}
	if stories[0].Tasks[0].DaemonRun {
		t.Fatal("stampDaemonModeTasks mutated its input stories")
	}
	if got := CheckPlanStructure(&PRD{}, stamped).Findings; len(got) != 0 {
		t.Fatalf("daemon-mode marked task raises plan findings %v, want none", got)
	}
	if got := LintTasks(stamped, EffectiveRules(config.DecomposeLintConfig{})); len(got) != 0 {
		t.Fatalf("daemon-mode marked task raises lint findings %v, want none", got)
	}

	// Llm mode: the operator set the same template to llm.
	llmAuto := &config.AutonomousConfig{TaskTemplates: config.TaskTemplatesConfig{
		Mode: map[string]string{"changelog_entry": string(config.TaskTemplateModeLLM)},
	}}
	unstamped := stampDaemonModeTasks(stories, llmAuto)
	if unstamped[0].Tasks[0].DaemonRun {
		t.Fatal("llm-mode marked task was stamped DaemonRun")
	}
	if got := planKindsFor(unstamped[0].Tasks[0]); !got[FindingMissingVerification] {
		t.Fatalf("llm-mode marked task raises %v, want %q", got, FindingMissingVerification)
	}
	if got := LintTasks(unstamped, EffectiveRules(config.DecomposeLintConfig{})); !hasLintRule(got, "no_verification_script") {
		t.Fatalf("llm-mode marked task raises lint %v, want no_verification_script", got)
	}

	// A nil config stamps nothing.
	if got := stampDaemonModeTasks(stories, nil); got[0].Tasks[0].DaemonRun {
		t.Fatal("nil config stamped DaemonRun")
	}
}

// TestDaemonModeTemplateSkipsManagerLint: the manager's dry run (LintPRD)
// wires the same skip: a stored daemon-mode template task is not linted,
// and its llm-mode twin is.
func TestDaemonModeTemplateSkipsManagerLint(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mode     string
		wantRule bool
	}{
		{name: "daemon mode is skipped", mode: string(config.TaskTemplateModeDaemon), wantRule: false},
		{name: "llm mode is linted", mode: string(config.TaskTemplateModeLLM), wantRule: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.DecomposeLint = config.DecomposeLintConfig{
				Mode:       config.DecomposeLintModeShadow,
				MaxRetries: config.DefaultDecomposeLintMaxRetries,
			}
			cfg.TaskTemplates = config.TaskTemplatesConfig{Mode: map[string]string{"changelog_entry": tc.mode}}
			m, err := NewManager(t.TempDir(), cfg, func(DecomposeRequest) (string, error) { return "", nil })
			if err != nil {
				t.Fatalf("NewManager: %v", err)
			}
			prd, _ := m.CreatePRD("write the changelog", "/p", "", "", "")
			task := Task{
				Title: "Write the changelog entry",
				Spec:  "Write the changelog entry (" + TaskTemplateIDMarker("changelog_entry") + ").",
			}
			if err := m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{task}}}); err != nil {
				t.Fatalf("SetStories: %v", err)
			}
			rep, err := m.LintPRD(prd.ID)
			if err != nil {
				t.Fatalf("LintPRD: %v", err)
			}
			if got := hasLintRule(rep.Findings, "no_verification_script"); got != tc.wantRule {
				t.Fatalf("no_verification_script fired = %v, want %v (findings %+v)", got, tc.wantRule, rep.Findings)
			}
		})
	}
}

// hasLintRule reports whether any finding carries the rule id.
func hasLintRule(findings []LintFinding, rule string) bool {
	for _, f := range findings {
		if f.RuleID == rule {
			return true
		}
	}
	return false
}
