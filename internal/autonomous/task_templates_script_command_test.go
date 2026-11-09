package autonomous

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// A template's verification_script that is a command string is not
// refused at any gate. It is copied onto the instantiated task so the
// plan carries it, and plan coverage and decompose lint report it on that
// task as unexecutable.
func TestTemplateVerificationScriptCommandStringReportedOnTask(t *testing.T) {
	const id = "gate-build-and-test"
	const script = "go build ./... && go test ./..."
	bad := config.TaskTemplateDef{
		ID:                 id,
		Title:              "Gate",
		Kind:               "final_gate",
		ReadOnly:           true,
		ReportPath:         ".datawatch/reports/gate-report.txt",
		VerificationScript: script,
	}

	story := testTaskTemplateStory(t)
	out, applied := applyTemplatesToStory(story, "feature", []config.TaskTemplateDef{bad})
	if len(applied) != 1 {
		t.Fatalf("applied = %v, want the template instantiated", applied)
	}
	var taskID string
	for _, tk := range out.Tasks {
		if tk.VerificationScript == script {
			taskID = tk.ID
		}
	}
	if taskID == "" {
		t.Fatal("instantiated task does not carry the command-string verification script")
	}

	// Plan coverage names the task.
	var planFound bool
	for _, f := range CheckPlanStructure(&PRD{}, []Story{out}).Findings {
		if f.Kind == FindingVerificationScriptUnexecutable && f.TaskID == taskID {
			planFound = true
		}
	}
	if !planFound {
		t.Errorf("plan coverage did not report %s on task %s", FindingVerificationScriptUnexecutable, taskID)
	}

	// Decompose lint names the task with the block rule.
	var lintFound bool
	for _, f := range LintTasks([]Story{out}, DefaultDecomposeLintRules()) {
		if f.RuleID == "verification_script_unexecutable" && f.Task == taskID {
			lintFound = true
		}
	}
	if !lintFound {
		t.Errorf("decompose lint did not report verification_script_unexecutable on task %s", taskID)
	}
}

// A repo-relative script path is never reported: it passes plan coverage
// and decompose lint, and it is copied onto the instantiated task.
func TestTemplateVerificationScriptPathNotReported(t *testing.T) {
	for _, v := range []string{"", "scripts/verify.sh", "./scripts/verify.sh", ".datawatch/verify/t1.sh", "verify.sh"} {
		if verificationScriptLooksLikeCommand(v) {
			t.Errorf("verificationScriptLooksLikeCommand(%q) = true, want false", v)
		}
	}
	story := testTaskTemplateStory(t)
	good := config.TaskTemplateDef{ID: "gate", Title: "Gate", VerificationScript: "scripts/verify.sh"}
	out, applied := applyTemplatesToStory(story, "feature", []config.TaskTemplateDef{good})
	if len(applied) != 1 {
		t.Fatalf("applied = %v, want one task for a valid script path", applied)
	}
	for _, f := range CheckPlanStructure(&PRD{}, []Story{out}).Findings {
		if f.Kind == FindingVerificationScriptUnexecutable {
			t.Errorf("valid script path reported: %s", f.Detail)
		}
	}
	for _, f := range LintTasks([]Story{out}, DefaultDecomposeLintRules()) {
		if f.RuleID == "verification_script_unexecutable" {
			t.Errorf("valid script path linted: %s", f.Message)
		}
	}
}

// The operator write path accepts a task_templates block carrying a command
// string and stores it; the problem surfaces as a finding on the task, not as
// a refused write.
func TestTemplateVerificationScriptCommandStringAcceptedOnWrite(t *testing.T) {
	mgr, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	api := &API{M: mgr}
	body := map[string]any{
		"task_templates": map[string]any{
			"enabled": true,
			"templates": []any{
				map[string]any{"id": "gate-build-and-test", "verification_script": "go build ./... && go test ./..."},
			},
		},
	}
	if err := api.SetDecomposeLintConfig(body); err != nil {
		t.Fatalf("SetDecomposeLintConfig refused the template: %v", err)
	}
	if len(mgr.cfg.TaskTemplates.Templates) != 1 {
		t.Fatalf("live templates = %d, want 1", len(mgr.cfg.TaskTemplates.Templates))
	}
	if !strings.Contains(mgr.cfg.TaskTemplates.Templates[0].VerificationScript, "&&") {
		t.Errorf("stored verification_script = %q, want the command string kept verbatim", mgr.cfg.TaskTemplates.Templates[0].VerificationScript)
	}
}
