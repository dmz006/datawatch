package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// The note-kind built-in templates (the release version note and the
// bounded load-step note) write no file, so they carry an explicit B124
// exemption reason. Without it, plan coverage reports missing_verification
// on the note and the decomposed plan can never pass its gate.
func TestTaskTemplateNoteTasksCarryExemptionAndComplete(t *testing.T) {
	out, _ := ApplyTaskTemplates([]Story{testTaskTemplateStory(t)}, "task templates", "", testTaskTemplateCfg())
	noteIDs := map[string]bool{}
	for _, s := range out {
		for _, tk := range s.Tasks {
			if tk.Kind != "note" {
				continue
			}
			noteIDs[tk.ID] = true
			if tk.VerificationScriptReason == "" {
				t.Errorf("note task %s has no verification_script_reason", tk.ID)
			}
			if tk.VerificationScript != "" || tk.ReadOnly || tk.ReportPath != "" {
				t.Errorf("note task %s carries an edit surface: script=%q read_only=%v report=%q",
					tk.ID, tk.VerificationScript, tk.ReadOnly, tk.ReportPath)
			}
		}
	}
	if !noteIDs[templateTaskID("a1000001-s1", TemplateVersionBumpLeftToMerge)] {
		t.Fatal("version note task was not applied")
	}

	// Plan coverage accepts every note task: no missing verification on it.
	for _, f := range CheckPlanStructure(&PRD{}, out).Findings {
		if f.Kind == FindingMissingVerification && noteIDs[f.TaskID] {
			t.Errorf("plan coverage flags note task %s: %s", f.TaskID, f.Detail)
		}
	}
	// Decompose lint blocks none of them.
	for _, f := range LintTasks(out, DefaultDecomposeLintRules()) {
		if f.Severity == config.DecomposeLintSeverityBlock && noteIDs[f.Task] {
			t.Errorf("block finding on note task %s: rule %s", f.Task, f.RuleID)
		}
	}
}
