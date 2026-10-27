// plan_coverage_write_files_report_test.go — production-shaped coverage
// of the write_task_without_files finding: a plan with a final gate, a
// no-files gate task, a read-only task, tasks that declare files and one
// write task that declares none must yield exactly that one finding in
// the report and in the stored record, gate Approve per mode, and be
// acknowledged only for the exact failing surface.
package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

const (
	writeFilesNoFilesTitle = "Update operator docs for automatic commits"
	writeFilesReportSpec   = "Report write tasks that declare no files before approval."
)

// writeFilesReportStories is a two-story, six-task plan shaped like a real
// decomposed PRD. Only the last task is a write task with no files.
func writeFilesReportStories() []Story {
	script := "go test ./..."
	return []Story{
		{
			Title: "Report the finding",
			Tasks: []Task{
				{
					Title: "Add the finding kind", Spec: "Add the finding to the structural checker.",
					FilesPlanned: []string{"internal/autonomous/checker.go"}, VerificationScript: script,
				},
				{
					Title: "Cover the finding with tests", Spec: "Add table tests for the checker.",
					FilesPlanned: []string{"internal/autonomous/checker_test.go"}, VerificationScript: script,
				},
				{
					Title: "Update the report notes", Spec: "READ-ONLY: nothing is edited, the notes are only read.",
					VerificationScriptReason: "read-only task, nothing to run",
				},
			},
		},
		{
			Title: "Close out the work",
			Tasks: []Task{
				{
					Title: "Write the final verdict", Spec: "Collect the results of the earlier stories.",
					Kind: TaskKindFinalGate, VerificationScriptReason: "gate task, runs the suite itself",
				},
				{
					Title: "Run the stability gate", Spec: "Run the full suite three times and compare.",
					VerificationScriptReason: "gate task, runs the suite itself",
				},
				{
					Title: writeFilesNoFilesTitle, Spec: "Describe the new commit behaviour for operators.",
					VerificationScriptReason: "prose only, reviewed by the verifier",
				},
			},
		},
	}
}

// newWriteFilesReportPRD stores a needs_review PRD whose ProjectDir is a
// temp tree holding every file the plan names, records the plan-coverage
// report in the given mode the way decompose does, and returns the
// manager, the stored PRD and the ID of the no-files write task.
func newWriteFilesReportPRD(t *testing.T, mode string) (*Manager, *PRD, string) {
	t.Helper()
	projectDir := t.TempDir()
	for _, rel := range []string{"internal/autonomous/checker.go", "internal/autonomous/checker_test.go"} {
		p := filepath.Join(projectDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte("package autonomous\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	m := newGateManager(t, mode, 1.0, gateCleanPlan)
	prd, err := m.CreatePRD(writeFilesReportSpec, projectDir, "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, writeFilesReportStories()); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDNeedsReview
	m.recordPlanCoverage(prd, prd.Story, config.PlanCoverageSettings{Mode: mode, MinRatio: 1.0})
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	var noFilesID string
	for _, s := range prd.Story {
		for _, task := range s.Tasks {
			if task.Title == writeFilesNoFilesTitle {
				noFilesID = task.ID
			}
		}
	}
	if noFilesID == "" {
		t.Fatalf("no-files write task not found in stored plan")
	}
	return m, prd, noFilesID
}

func requireOnlyWriteFilesFinding(t *testing.T, findings []StructuralFinding, taskID string) StructuralFinding {
	t.Helper()
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want exactly one", findings)
	}
	f := findings[0]
	if f.Kind != FindingWriteTaskWithoutFiles {
		t.Fatalf("finding kind = %q, want %q", f.Kind, FindingWriteTaskWithoutFiles)
	}
	if f.TaskID != taskID {
		t.Fatalf("finding task = %q, want %q", f.TaskID, taskID)
	}
	for _, want := range []string{taskID, writeFilesNoFilesTitle, "files", planReadOnlyMarker} {
		if !strings.Contains(f.Detail, want) {
			t.Fatalf("detail %q must mention %q", f.Detail, want)
		}
	}
	return f
}

func TestWriteFilesReport_ReportAndStoredRecordCarryOneFinding(t *testing.T) {
	_, prd, noFilesID := newWriteFilesReportPRD(t, "block")

	rep := CheckPlanStructure(prd, prd.Story)
	if rep.Count() != 1 {
		t.Fatalf("report count = %d, findings = %+v, want 1", rep.Count(), rep.Findings)
	}
	f := requireOnlyWriteFilesFinding(t, rep.Findings, noFilesID)

	pc := prd.PlanCoverage
	if pc == nil {
		t.Fatal("no stored plan-coverage record")
	}
	if len(pc.Report.Unmatched) != 0 {
		t.Fatalf("spec has no extractable requirements, unmatched = %+v", pc.Report.Unmatched)
	}
	stored := requireOnlyWriteFilesFinding(t, pc.Findings, noFilesID)
	if stored != f {
		t.Fatalf("stored finding %+v differs from report finding %+v", stored, f)
	}
	if !pc.IsFailing() {
		t.Fatal("a record carrying a structural finding must be failing")
	}
}

func TestWriteFilesReport_BlockModeRefusesApproval(t *testing.T) {
	m, prd, _ := newWriteFilesReportPRD(t, "block")

	_, err := m.Approve(prd.ID, "operator", "approve")
	if err == nil {
		t.Fatal("Approve must be refused in block mode while the finding is unacknowledged")
	}
	if !strings.Contains(err.Error(), "plan-coverage gate") {
		t.Fatalf("error must be the plan-coverage gate error: %v", err)
	}
	if !strings.Contains(err.Error(), "1 structural finding") {
		t.Fatalf("error must name the finding count: %v", err)
	}
	again, _ := m.Store().GetPRD(prd.ID)
	if again.Status != PRDNeedsReview {
		t.Fatalf("status after refused Approve = %q, want needs_review", again.Status)
	}
}

func TestWriteFilesReport_WarnAndOffStoreButDoNotGate(t *testing.T) {
	for _, mode := range []string{"warn", "off"} {
		t.Run(mode, func(t *testing.T) {
			m, prd, noFilesID := newWriteFilesReportPRD(t, mode)
			if prd.PlanCoverage == nil || prd.PlanCoverage.Mode != mode {
				t.Fatalf("stored record: %+v", prd.PlanCoverage)
			}
			requireOnlyWriteFilesFinding(t, prd.PlanCoverage.Findings, noFilesID)

			appr, err := m.Approve(prd.ID, "operator", mode+" mode")
			if err != nil {
				t.Fatalf("Approve in %s mode must not be gated: %v", mode, err)
			}
			if appr.Status != PRDApproved {
				t.Fatalf("status = %q, want approved", appr.Status)
			}
		})
	}
}

func TestWriteFilesReport_AckCoversExactlyThisFinding(t *testing.T) {
	m, prd, _ := newWriteFilesReportPRD(t, "block")
	firstFP := prd.PlanCoverage.Fingerprint()

	if _, err := m.AcknowledgePlanCoverage(prd.ID, "operator", " "); err == nil {
		t.Fatal("a blank note must be rejected")
	}
	if _, err := m.Approve(prd.ID, "operator", ""); err == nil {
		t.Fatal("Approve must stay refused after a rejected acknowledgement")
	}

	acked, err := m.AcknowledgePlanCoverage(prd.ID, "operator", "docs task edits files chosen at run time")
	if err != nil {
		t.Fatalf("AcknowledgePlanCoverage: %v", err)
	}
	if got := acked.PlanCoverage.Ack; got == nil || got.Fingerprint != firstFP || got.Note == "" {
		t.Fatalf("ack = %+v, want fingerprint %q and a note", got, firstFP)
	}
	if err := m.planCoverageApprovalGate(prd.ID, acked); err != nil {
		t.Fatalf("matching ack must clear the gate: %v", err)
	}

	// The plan changes: a second write task without files adds a finding,
	// so the failing surface differs and the carried ack no longer covers it.
	changed, _ := m.Store().GetPRD(prd.ID)
	stories := append([]Story(nil), changed.Story...)
	last := len(stories) - 1
	stories[last].Tasks = append(append([]Task(nil), stories[last].Tasks...), Task{
		Title: "Document the new setting", Spec: "Describe the setting for operators.",
		VerificationScriptReason: "prose only, reviewed by the verifier",
	})
	if err := m.Store().SetStories(changed.ID, stories); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	changed, _ = m.Store().GetPRD(prd.ID)
	changed.Status = PRDNeedsReview
	m.recordPlanCoverage(changed, changed.Story, config.PlanCoverageSettings{Mode: "block", MinRatio: 1.0})
	if err := m.Store().SavePRD(changed); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	pc := changed.PlanCoverage
	if len(pc.Findings) != 2 {
		t.Fatalf("findings = %+v, want two after the plan change", pc.Findings)
	}
	if pc.Fingerprint() == firstFP {
		t.Fatal("a changed failing surface must change the fingerprint")
	}
	if pc.Ack == nil || pc.Ack.Fingerprint != firstFP {
		t.Fatalf("prior ack should be carried forward unchanged, got %+v", pc.Ack)
	}
	if _, err := m.Approve(prd.ID, "operator", "again"); err == nil {
		t.Fatal("the old acknowledgement must not cover the changed surface")
	} else if !strings.Contains(err.Error(), "2 structural finding") {
		t.Fatalf("gate error must name the new finding count: %v", err)
	}

	fresh, err := m.AcknowledgePlanCoverage(prd.ID, "operator", "both docs tasks edit files chosen at run time")
	if err != nil {
		t.Fatalf("fresh acknowledgement: %v", err)
	}
	if fresh.PlanCoverage.Ack.Fingerprint != pc.Fingerprint() {
		t.Fatalf("fresh ack fingerprint = %q, want %q", fresh.PlanCoverage.Ack.Fingerprint, pc.Fingerprint())
	}
	appr, err := m.Approve(prd.ID, "operator", "approved")
	if err != nil {
		t.Fatalf("Approve after the fresh acknowledgement: %v", err)
	}
	if appr.Status != PRDApproved {
		t.Fatalf("status = %q, want approved", appr.Status)
	}
}
