package autonomous

// Gate validity state: setting a PRD's quality-gate command re-validates it
// as a baseline in the PRD's project dir. An unusable baseline is recorded
// (status, plain reason, decision-log entry) but never fails the call, and
// Status() counts invalid gate results separately from pass/regression.

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/pipeline"
)

func TestGateValidity_SetPRDQualityGatesMarksInvalid(t *testing.T) {
	mgr, err := NewManager(t.TempDir(), Config{Enabled: true, AutoFixRetries: 0}, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Stop()

	projDir := t.TempDir()
	prd, err := mgr.Store().CreatePRD("gate validity", projDir, "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	countInvalid := func(p *PRD) int {
		n := 0
		for _, d := range p.Decisions {
			if d.Kind == "quality_gate_invalid" {
				n++
			}
		}
		return n
	}

	// A command that exits non-zero without naming a failing test is not a
	// usable baseline.
	got, err := mgr.SetPRDQualityGates(prd.ID, true, "exit 1", 30, true)
	if err != nil {
		t.Fatalf("SetPRDQualityGates must not fail on an invalid baseline: %v", err)
	}
	if got.QualityGateStatus != "invalid" {
		t.Fatalf("QualityGateStatus = %q, want invalid", got.QualityGateStatus)
	}
	if got.QualityGateInvalidReason == "" || strings.Contains(got.QualityGateInvalidReason, "exit status") {
		t.Fatalf("QualityGateInvalidReason = %q, want a plain-language reason", got.QualityGateInvalidReason)
	}
	if n := countInvalid(got); n != 1 {
		t.Fatalf("quality_gate_invalid decisions = %d, want 1", n)
	}
	for _, d := range got.Decisions {
		if d.Kind == "quality_gate_invalid" && d.Actor != "operator" {
			t.Errorf("decision actor = %q, want operator", d.Actor)
		}
	}
	stored, _ := mgr.Store().GetPRD(prd.ID)
	if stored.QualityGateStatus != "invalid" || stored.QualityGateInvalidReason != got.QualityGateInvalidReason {
		t.Fatalf("verdict not persisted: %+v", stored)
	}
	if stored.QualityGates == nil || stored.QualityGates.TestCommand != "exit 1" {
		t.Fatal("gate config must still be stored when the baseline is invalid")
	}

	// A command that reports a passing test run clears the stale verdict.
	got, err = mgr.SetPRDQualityGates(prd.ID, true, "echo 'ok  \texample.com/x'", 30, true)
	if err != nil {
		t.Fatalf("SetPRDQualityGates: %v", err)
	}
	if got.QualityGateStatus != "" || got.QualityGateInvalidReason != "" {
		t.Fatalf("stale verdict not cleared: status=%q reason=%q", got.QualityGateStatus, got.QualityGateInvalidReason)
	}
	if n := countInvalid(got); n != 1 {
		t.Fatalf("the earlier decision-log entry must remain, got %d", n)
	}

	// A PRD without a project dir is left untouched and is not an error.
	noDir, err := mgr.Store().CreatePRD("no dir", "", "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	got, err = mgr.SetPRDQualityGates(noDir.ID, true, "exit 1", 30, true)
	if err != nil {
		t.Fatalf("SetPRDQualityGates without project dir: %v", err)
	}
	if got.QualityGateStatus != "" || got.QualityGateInvalidReason != "" || countInvalid(got) != 0 {
		t.Fatalf("PRD without project dir must stay untouched: %+v", got)
	}
}

func TestGateValidity_StatusCountsInvalidGate(t *testing.T) {
	mgr, err := NewManager(t.TempDir(), Config{Enabled: true, AutoFixRetries: 0}, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Stop()

	prd, err := mgr.Store().CreatePRD("gate status", t.TempDir(), "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := mgr.Store().SetStories(prd.ID, []Story{{
		Title: "S",
		Tasks: []Task{{Title: "pass"}, {Title: "regress"}, {Title: "invalid"}},
	}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = mgr.Store().GetPRD(prd.ID)
	prd.Story[0].Tasks[0].QualityGateResult = &pipeline.QualityGateResult{}
	prd.Story[0].Tasks[1].QualityGateResult = &pipeline.QualityGateResult{Regression: true}
	// An invalid gate counts as neither pass nor regression, even if a
	// stale Regression flag were set alongside it.
	prd.Story[0].Tasks[2].QualityGateResult = &pipeline.QualityGateResult{Invalid: true, Regression: true}
	if err := mgr.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	st := mgr.Status()
	if st.QualityGateRuns != 3 {
		t.Errorf("QualityGateRuns = %d, want 3", st.QualityGateRuns)
	}
	if st.QualityGatePass != 1 {
		t.Errorf("QualityGatePass = %d, want 1", st.QualityGatePass)
	}
	if st.QualityGateFail != 1 || st.QualityGateRegressions != 1 {
		t.Errorf("fail/regressions = %d/%d, want 1/1", st.QualityGateFail, st.QualityGateRegressions)
	}
	if st.QualityGateInvalid != 1 {
		t.Errorf("QualityGateInvalid = %d, want 1", st.QualityGateInvalid)
	}
}
