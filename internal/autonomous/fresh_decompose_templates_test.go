// fresh_decompose_templates_test.go — fresh-decomposition tests over the
// built-in task templates, end to end: a story built by
// ApplyTaskTemplates in a fresh project tree, with the two template
// tasks the operator cares about closing cleanly:
//
//   - the read-only final gate: the verifier prestart reports no
//     problem for it, it is read-only with a usable report path, and
//     the read-only report acceptance passes once its report file
//     exists (and fails before);
//   - the version note: it carries a non-empty
//     verification_script_reason, trips no missing_verification
//     finding, and the exempt-note verdict accepts it with no
//     changes.
package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// freshDecompose applies the built-in pack to one story in a fresh
// project directory (t.TempDir) and returns the updated stories, the
// applied labels and the project directory.
func freshDecompose(t *testing.T, feature, projectDir string) ([]Story, []string) {
	t.Helper()
	if projectDir == "" {
		t.Fatal("freshDecompose: projectDir must be set")
	}
	stories, applied := ApplyTaskTemplates([]Story{testTaskTemplateStory(t)}, feature, projectDir, testTaskTemplateCfg())
	if len(stories) != 1 {
		t.Fatalf("stories = %d, want 1", len(stories))
	}
	if len(applied) == 0 {
		t.Fatal("no template task applied to the fresh decomposition")
	}
	return stories, applied
}

// taskForTemplateID finds the applied task of one template (by the
// template id in the task id) in the first story.
func taskForTemplateID(t *testing.T, stories []Story, templateID string) *Task {
	t.Helper()
	for i := range stories[0].Tasks {
		if strings.Contains(stories[0].Tasks[i].ID, templateID) {
			return &stories[0].Tasks[i]
		}
	}
	t.Fatalf("template task for %q not found in the fresh decomposition", templateID)
	return nil
}

// TestFreshDecomposeFinalGateStartsAndPasses closes the fresh
// decomposition's final-gate task: the verifier prestart reports no
// problem (the gate carries no verification script — an empty script
// is n/a and never blocks), the task is read-only with a usable
// report path, and the read-only report acceptance fails before the
// report file exists and passes once the gate has written it.
func TestFreshDecomposeFinalGateStartsAndPasses(t *testing.T) {
	projectDir := t.TempDir()
	stories, _ := freshDecompose(t, "Fresh Decompose Final Gate", projectDir)
	gate := taskForTemplateID(t, stories, TemplateFinalGate)

	// Starts: the verifier prestart reports no problem for the gate.
	if res := CheckVerifierPrestart(projectDir, gate.VerificationScript); res.Outcome == PrestartProblem {
		t.Fatalf("verifier prestart reports a problem for the final gate: %s", res.Reason)
	}

	// The gate is read-only and carries a usable report path — the
	// only file a read-only task may write — and no verification
	// script (its check is its report file).
	if !gate.ReadOnly {
		t.Fatal("final gate task is not read-only")
	}
	if !taskHasUsableReportPath(*gate) {
		t.Fatalf("final gate report path %q is not usable", gate.ReportPath)
	}
	if strings.TrimSpace(gate.VerificationScript) != "" {
		t.Fatalf("final gate carries verification script %q; its check is its report file", gate.VerificationScript)
	}
	if err := validateReportPath(gate.ReportPath); err != nil {
		t.Fatalf("final gate report path invalid: %v", err)
	}

	// Passes: the read-only report acceptance fails before the report
	// file exists and passes once the gate has written it.
	reportPath := filepath.Join(projectDir, gate.ReportPath)
	if _, err := os.Stat(reportPath); err == nil {
		t.Fatalf("report file %s already exists; the project tree must be fresh", gate.ReportPath)
	}
	if readOnlyReportAccepts(projectDir, *gate) {
		t.Fatal("read-only report acceptance passes before the report file exists")
	}
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		t.Fatalf("create report dir: %v", err)
	}
	if err := os.WriteFile(reportPath, []byte("PASS: build clean; tests of touched packages pass; files hang together\n"), 0o644); err != nil {
		t.Fatalf("write report file: %v", err)
	}
	if !readOnlyReportAccepts(projectDir, *gate) {
		t.Fatalf("read-only report acceptance fails once the report file exists at %s", gate.ReportPath)
	}
}

// readOnlyReportAccepts is the read-only report check: a read-only
// task with a usable report path is accepted when its report file
// exists inside the project tree as a non-empty regular file. A
// read-only task with neither a verification script nor a usable
// report path is never accepted — there would be nothing to inspect.
func readOnlyReportAccepts(projectDir string, task Task) bool {
	if !task.ReadOnly || !taskHasUsableReportPath(task) {
		return false
	}
	full := filepath.Join(projectDir, task.ReportPath)
	fi, err := os.Stat(full)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return false
	}
	return true
}

// TestFreshDecomposeNoteTaskCompletes closes the fresh
// decomposition's version-note task: it carries a non-empty
// verification_script_reason (its B124 exemption — it is a note, not
// an edit, and there is nothing to run a check against), trips no
// missing_verification finding in the plan-structure verdict, and the
// exempt-note verdict accepts it with no changes (no planned files,
// no read-only report, no verification script to run).
func TestFreshDecomposeNoteTaskCompletes(t *testing.T) {
	projectDir := t.TempDir()
	stories, _ := freshDecompose(t, "Fresh Decompose Note", projectDir)
	note := taskForTemplateID(t, stories, TemplateVersionBumpLeftToMerge)

	// The note carries its exemption: a non-empty reason standing in
	// for the script it does not have.
	if strings.TrimSpace(note.VerificationScriptReason) == "" {
		t.Fatalf("version-note task %s has an empty verification_script_reason: %q", note.ID, note.Spec)
	}

	// ...and it therefore trips no missing_verification finding: the
	// plan-structure verdict exempts a task that states why it has no
	// verification script.
	prd := &PRD{ID: "fd-note-001", Title: "Fresh Decompose Note", ProjectDir: projectDir}
	report := CheckPlanStructure(prd, stories)
	for _, f := range report.Findings {
		if f.Kind == FindingMissingVerification && f.TaskID == note.ID {
			t.Fatalf("version-note task trips missing_verification: %s", f.Detail)
		}
	}

	// The exempt-note verdict accepts the note with no changes: it
	// writes nothing and needs nothing run against it.
	if !exemptNoteVerdict(*note) {
		t.Fatalf("exempt-note verdict refuses the version-note task: kind=%q script=%q reason=%q files=%v",
			note.Kind, note.VerificationScript, note.VerificationScriptReason, note.FilesPlanned)
	}
}

// noteKind is the task kind of the note templates (a note, not an
// edit): the built-in pack sets it as plain text in the template defs.
const noteKind = "note"

// exemptNoteVerdict is the exempt-note acceptance: a note-kind task
// that states its exemption (a non-empty verification_script_reason),
// carries no verification script, plans no files and is not read-only
// (it writes no report either) is accepted with no changes.
func exemptNoteVerdict(task Task) bool {
	return task.Kind == noteKind &&
		strings.TrimSpace(task.VerificationScript) == "" &&
		strings.TrimSpace(task.VerificationScriptReason) != "" &&
		len(task.FilesPlanned) == 0 &&
		!task.ReadOnly
}
