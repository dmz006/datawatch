package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func verifyStableWrite(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func verifyStableRule(t *testing.T, id string) Rule {
	t.Helper()
	for _, r := range DefaultDecomposeLintRules() {
		if r.ID == id {
			if r.Severity != config.DecomposeLintSeverityBlock {
				t.Fatalf("%s severity = %q, want block", id, r.Severity)
			}
			return r
		}
	}
	t.Fatalf("rule %s missing from the default pack", id)
	return Rule{}
}

func verifyStableLint(dir, id string, rule Rule, task Task) []LintFinding {
	_ = id
	return LintTasksIn(dir, []Story{{ID: "s", Tasks: []Task{task}}}, []Rule{rule})
}

func TestVerifyStable_TaskFilesOnly(t *testing.T) {
	rule := verifyStableRule(t, "verify_script_task_files_only")
	dir := t.TempDir()
	verifyStableWrite(t, dir, "v/head.sh", "#!/bin/sh\ngit diff --name-only HEAD | grep -qx a.go\n")
	verifyStableWrite(t, dir, "v/literal.sh", "#!/bin/sh\nallowed='a.go b.go'\ngit status --porcelain | diff - /dev/null\n")
	verifyStableWrite(t, dir, "v/cumulative.sh", "#!/bin/sh\nbase=$(git merge-base HEAD main)\ngit diff --name-only \"$base\"\n")
	verifyStableWrite(t, dir, "v/helper_lib.sh", "base=$(git merge-base HEAD main)\ngit diff --name-only \"$base\"\n")
	verifyStableWrite(t, dir, "v/with_helper.sh", "#!/bin/sh\n. ./v/helper_lib.sh\ngit diff --name-only HEAD\n")
	verifyStableWrite(t, dir, "v/unrelated.sh", "#!/bin/sh\ngo test ./...\n")

	files := []string{"a.go", "b.go"}
	for _, c := range []struct {
		name, script string
		want         int
	}{
		{"diff against HEAD fails", "v/head.sh", 1},
		{"literal file list fails", "v/literal.sh", 1},
		{"merge base passes", "v/cumulative.sh", 0},
		{"followed helper using the merge base passes", "v/with_helper.sh", 0},
		{"unrelated script passes", "v/unrelated.sh", 0},
		{"missing script is skipped", "v/missing.sh", 0},
	} {
		got := verifyStableLint(dir, "", rule, Task{ID: "t", VerificationScript: c.script, FilesPlanned: files})
		if len(got) != c.want {
			t.Errorf("%s: %d findings, want %d: %+v", c.name, len(got), c.want, got)
		}
	}
}

func TestVerifyStable_DocsWithoutSync(t *testing.T) {
	rule := verifyStableRule(t, "verify_script_docs_without_sync")
	dir := t.TempDir()
	verifyStableWrite(t, dir, "v/docs.sh", "#!/bin/sh\nscripts/check-docs-sync.sh\n")
	verifyStableWrite(t, dir, "v/embedded.sh", "#!/bin/sh\ndiff -r docs internal/server/web/docs\n")
	verifyStableWrite(t, dir, "v/howto.sh", "#!/bin/sh\ntest -f docs/howto/x.md\n")
	verifyStableWrite(t, dir, "v/synced.sh", "#!/bin/sh\nmake sync-docs\nscripts/check-docs-sync.sh\n")
	verifyStableWrite(t, dir, "v/sync_lib.sh", "make sync-docs\n")
	verifyStableWrite(t, dir, "v/helper_synced.sh", "#!/bin/sh\nsource v/sync_lib.sh\nscripts/check-howto-coverage.sh\n")
	verifyStableWrite(t, dir, "v/nodocs.sh", "#!/bin/sh\ngo vet ./...\n")

	for _, c := range []struct {
		name, script string
		want         int
	}{
		{"docs check without sync fails", "v/docs.sh", 1},
		{"embedded docs without sync fails", "v/embedded.sh", 1},
		{"howto check without sync fails", "v/howto.sh", 1},
		{"sync-docs run passes", "v/synced.sh", 0},
		{"sync-docs in a followed helper passes", "v/helper_synced.sh", 0},
		{"no docs check passes", "v/nodocs.sh", 0},
		{"missing script is skipped", "v/missing.sh", 0},
		{"command string is skipped", "make check-docs-sync", 0},
	} {
		got := verifyStableLint(dir, "", rule, Task{ID: "t", VerificationScript: c.script})
		if len(got) != c.want {
			t.Errorf("%s: %d findings, want %d: %+v", c.name, len(got), c.want, got)
		}
	}
}

func TestVerifyStable_FinalGateWithoutScript(t *testing.T) {
	rule := verifyStableRule(t, "final_gate_without_script")
	for _, c := range []struct {
		name string
		task Task
		want int
	}{
		{"bare final gate fails", Task{ID: "t", Kind: TaskKindFinalGate}, 1},
		{"kind is case-insensitive", Task{ID: "t", Kind: "Final_Gate"}, 1},
		{"with a script passes", Task{ID: "t", Kind: TaskKindFinalGate, VerificationScript: "v/x.sh"}, 0},
		{"with a reason passes", Task{ID: "t", Kind: TaskKindFinalGate, VerificationScriptReason: "report only"}, 0},
		{"with a report path passes", Task{ID: "t", Kind: TaskKindFinalGate, ReportPath: ".datawatch/reports/g.txt"}, 0},
		{"other kind passes", Task{ID: "t", Kind: "docs"}, 0},
	} {
		if got := verifyStableLint("", "", rule, c.task); len(got) != c.want {
			t.Errorf("%s: %d findings, want %d: %+v", c.name, len(got), c.want, got)
		}
	}
}

func TestVerifyStable_FindingNamesScriptAndRule(t *testing.T) {
	dir := t.TempDir()
	verifyStableWrite(t, dir, "v/head.sh", "#!/bin/sh\ngit diff --name-only HEAD\n")
	verifyStableWrite(t, dir, "v/docs.sh", "#!/bin/sh\nscripts/check-docs-sync.sh\n")
	rules := DefaultDecomposeLintRules()
	for _, c := range []struct{ rule, script string }{
		{"verify_script_task_files_only", "v/head.sh"},
		{"verify_script_docs_without_sync", "v/docs.sh"},
	} {
		got := LintTasksIn(dir, []Story{{ID: "s", Tasks: []Task{{ID: "t", VerificationScript: c.script}}}}, rules)
		f, ok := findDecomposeLintFinding(got, c.rule)
		if !ok {
			t.Fatalf("%s did not fire: %+v", c.rule, got)
		}
		if f.Script != c.script || !strings.Contains(f.Message, c.script) || !strings.Contains(f.Message, c.rule) {
			t.Errorf("%s finding message %q / script %q must name the rule and %q", c.rule, f.Message, f.Script, c.script)
		}
		line := LintFindingLine(f)
		if !strings.Contains(line, c.script) || !strings.Contains(line, c.rule) {
			t.Errorf("rendered line %q must name rule and script", line)
		}
	}
	// An operator-replaced message without the path still renders it.
	line := LintFindingLine(LintFinding{RuleID: "r", Severity: "block", Task: "t", Message: "custom", Script: "v/x.sh"})
	if !strings.Contains(line, "v/x.sh") {
		t.Errorf("line %q lacks the script path", line)
	}
}

func TestVerifyStable_TemplatesAndCumulativePlanClean(t *testing.T) {
	dir := t.TempDir()
	verifyStableWrite(t, dir, "v/cumulative.sh", "#!/bin/sh\nmake sync-docs\nbase=$(git merge-base HEAD main)\ngit diff --name-only \"$base\"\nscripts/check-docs-sync.sh\n")
	var tasks []Task
	for i, def := range BuiltInTaskTemplates() {
		r := RenderTaskTemplateDef(def, config.TaskTemplateSlots{Feature: "widget", Files: "a.go", FilesList: "a.go"})
		tasks = append(tasks, Task{
			ID: "t" + string(rune('a'+i)), Title: r.Title, Spec: r.Spec, Kind: r.Kind, ReadOnly: r.ReadOnly,
			ReportPath: r.ReportPath, VerificationScript: r.VerificationScript, VerificationScriptReason: r.VerificationScriptReason,
			FilesPlanned: []string{"a.go"},
		})
	}
	tasks = append(tasks, Task{ID: "cum", Title: "Cumulative", Spec: "Do it.", VerificationScript: "v/cumulative.sh", FilesPlanned: []string{"a.go"}})
	mine := map[string]bool{"verify_script_task_files_only": true, "verify_script_docs_without_sync": true, "final_gate_without_script": true}
	for _, f := range LintTasksIn(dir, []Story{{ID: "s", Tasks: tasks}}, DefaultDecomposeLintRules()) {
		if mine[f.RuleID] {
			t.Errorf("unexpected %s on %s: %s", f.RuleID, f.Task, f.Message)
		}
	}
}

func TestVerifyStable_HelpersEdgeCases(t *testing.T) {
	dir := t.TempDir()
	// a helper path that escapes the directory or is absolute is not followed
	verifyStableWrite(t, dir, "v/esc.sh", "#!/bin/sh\n. ../outside.sh\n. /etc/profile\n. \"$X/y.sh\"\ngit diff --name-only HEAD\n")
	if _, ok := verifyScriptTaskFilesOnly(dir, Task{VerificationScript: "v/esc.sh"}); !ok {
		t.Error("script with unfollowable helpers should still be judged on its own text")
	}
	if _, _, ok := verifyScriptContent(dir, Task{VerificationScript: "../x.sh"}); ok {
		t.Error("a script outside the directory must be skipped")
	}
	if _, _, ok := verifyScriptContent("", Task{VerificationScript: "v/esc.sh"}); ok {
		t.Error("no directory must skip")
	}
	// a literal list equal to the task's own files, no git command
	verifyStableWrite(t, dir, "v/list.sh", "#!/bin/sh\nfor f in a.go b.go; do diff \"$f\" ref/\"$f\"; done\n")
	if _, ok := verifyScriptTaskFilesOnly(dir, Task{VerificationScript: "v/list.sh", FilesPlanned: []string{"a.go", "b.go"}}); !ok {
		t.Error("literal own-files list should fire")
	}
	if scriptListsOwnFiles("a.go", Task{}) {
		t.Error("no planned files cannot equal a list")
	}
	// the finding path falls back through any_of / all_of and the declared script
	m, err := compileMatch(config.DecomposeLintMatch{AnyOf: []config.DecomposeLintMatch{{VerificationScriptDocsWithoutSync: true}}, AllOf: []config.DecomposeLintMatch{{Kind: "x"}}}, "r")
	if err != nil {
		t.Fatal(err)
	}
	if !m.usesScriptContent() {
		t.Error("nested script predicate should be detected")
	}
	if got := verifyScriptFindingPath(m, dir, Task{VerificationScript: "v/none.sh"}); got != "v/none.sh" {
		t.Errorf("fallback path = %q", got)
	}
}
