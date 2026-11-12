package autonomous

// Gate validity state: setting a PRD's quality-gate command re-validates it
// as a baseline in the PRD's project dir. An unusable baseline is recorded
// (status, plain reason, decision-log entry) but never fails the call, and
// Status() counts invalid gate results separately from pass/regression.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/pipeline"
)

func TestGateValidity_SetPRDQualityGatesMarksInvalid(t *testing.T) {
	mgr, err := NewManager(t.TempDir(), Config{Enabled: true, AutoFixRetries: 0}, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	allowTempProjectRoots(mgr)
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
	got, err := mgr.SetPRDQualityGates(prd.ID, true, "exit 1", 30, true, "", nil)
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
	got, err = mgr.SetPRDQualityGates(prd.ID, true, "echo 'ok  \texample.com/x'", 30, true, "", nil)
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
	got, err = mgr.SetPRDQualityGates(noDir.ID, true, "exit 1", 30, true, "", nil)
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
	allowTempProjectRoots(mgr)
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

// gvSetup creates a one-task approved PRD in dir with the given gate config.
func gvSetup(t *testing.T, dir string, qg pipeline.QualityGateConfig) (*Manager, *PRD) {
	t.Helper()
	m, err := NewManager(t.TempDir(), Config{Enabled: true, AutoFixRetries: 0}, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	allowTempProjectRoots(m)
	prd, err := m.CreatePRD("spec", dir, "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do it"}}}})
	prd, _ = m.Store().GetPRD(prd.ID) // re-fetch so task IDs are populated
	prd.Status = PRDApproved
	prd.QualityGates = &qg
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd
}

// gvRun executes the PRD's sole task with a passing verifier.
func gvRun(t *testing.T, m *Manager, prd *PRD) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-" + req.Title}, nil
	}
	if err := m.Run(ctx, prd.ID, spawn, okVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// gvAssertInvalidPRD verifies the invalid-baseline recording contract.
func gvAssertInvalidPRD(t *testing.T, m *Manager, prd *PRD) *PRD {
	t.Helper()
	got, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD not found after run")
	}
	if got.QualityGateStatus != "invalid" {
		t.Errorf("QualityGateStatus = %q, want %q", got.QualityGateStatus, "invalid")
	}
	if strings.TrimSpace(got.QualityGateInvalidReason) == "" {
		t.Error("QualityGateInvalidReason is empty; want the plain-language reason")
	}
	found := false
	for _, d := range got.Decisions {
		if d.Kind == "quality_gate_invalid" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no decision entry with kind quality_gate_invalid; got kinds: %v", gvDecisionKinds(got.Decisions))
	}
	task := got.Story[0].Tasks[0]
	if task.Status != TaskCompleted {
		t.Fatalf("task status = %s, want completed (invalid gate must never fail a task)", task.Status)
	}
	if task.QualityGateResult == nil {
		t.Fatal("task.QualityGateResult is nil; want the recorded invalid result")
	}
	if !task.QualityGateResult.Invalid {
		t.Error("task.QualityGateResult.Invalid = false, want true")
	}
	if !strings.HasPrefix(task.QualityGateResult.Summary, "gate invalid (warning): ") {
		t.Errorf("summary = %q, want prefix %q", task.QualityGateResult.Summary, "gate invalid (warning): ")
	}
	t.Logf("reason: %s", got.QualityGateInvalidReason)
	return got
}

func gvDecisionKinds(ds []Decision) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.Kind)
	}
	return out
}

// TestGateValidity_SeamInvalidBaseline_CompletesWithWarning pins the
// contract with a controlled fake test run: exit non-zero with no failing
// test names (the phantom-failure signature). Even with BlockOnRegression
// on, the task completes because the baseline is unusable.
func TestGateValidity_SeamInvalidBaseline_CompletesWithWarning(t *testing.T) {
	dir := t.TempDir()
	orig := runQualityGateTests
	runs := 0
	runQualityGateTests = func(cmd, d string, _ int, _ []string) pipeline.TestResult {
		runs++
		if d != dir {
			t.Errorf("run %d in dir %q, want %q", runs, d, dir)
		}
		if cmd != "fake-cmd" {
			t.Errorf("cmd = %q, want %q", cmd, "fake-cmd")
		}
		// Exit non-zero, ZERO failing test names — the invalid signature.
		return pipeline.TestResult{ExitCode: 2, Output: "setup failed", TestsFound: 0, FailedTests: nil}
	}
	t.Cleanup(func() { runQualityGateTests = orig })

	m, prd := gvSetup(t, dir, pipeline.QualityGateConfig{
		Enabled: true, TestCommand: "fake-cmd", Timeout: 10, BlockOnRegression: true,
	})
	gvRun(t, m, prd)
	gvAssertInvalidPRD(t, m, prd)
}

// TestGateValidity_RealRun_InvalidBaseline_Completes exercises the REAL
// pipeline.RunTests path (sh -c over a nonexistent generated file — the
// worktree phantom-failure class) and the same warning-only contract.
func TestGateValidity_RealRun_InvalidBaseline_Completes(t *testing.T) {
	dir := t.TempDir()
	m, prd := gvSetup(t, dir, pipeline.QualityGateConfig{
		Enabled: true, TestCommand: "go test ./nonexistent/... ", Timeout: 20, BlockOnRegression: true,
	})
	gvRun(t, m, prd)
	gvAssertInvalidPRD(t, m, prd)
}

// TestGateValidity_RealRun_RegressionStillFailsTask is the regression
// guard: with a VALID baseline, a new failing test detected after the task
// must fail the task (BlockOnRegression) — the warning path must not have
// silently disabled real regression blocking.
func TestGateValidity_RealRun_RegressionStillFailsTask(t *testing.T) {
	dir := t.TempDir()
	const taps = "taps.txt"
	// Baseline (captured before spawn): one already-failing test (beta)
	// and one passing — a valid baseline with failed names.
	if err := os.WriteFile(filepath.Join(dir, taps), []byte("ok 1 - alpha\nnot ok 1 - beta\n"), 0o644); err != nil {
		t.Fatalf("write baseline taps: %v", err)
	}
	m, prd := gvSetup(t, dir, pipeline.QualityGateConfig{
		Enabled: true, TestCommand: "cat " + taps, Timeout: 10, BlockOnRegression: true,
	})

	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		// The "task's work" introduces a NEW failing test (gamma).
		_ = os.WriteFile(filepath.Join(dir, taps), []byte(
			"ok 1 - alpha\nok 2 - delta\nnot ok 1 - beta\nnot ok 2 - gamma\n"), 0o644)
		return SpawnResult{SessionID: "sess-" + req.Title}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := m.Run(ctx, prd.ID, spawn, okVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD not found after run")
	}
	task := got.Story[0].Tasks[0]
	if task.Status != TaskReviewNeeded {
		t.Fatalf("task status = %s, want review_needed (valid baseline + new failing test must block)", task.Status)
	}
	if task.QualityGateResult == nil {
		t.Fatal("task.QualityGateResult is nil")
	}
	if !task.QualityGateResult.Regression || task.QualityGateResult.Invalid {
		t.Fatalf("gate result = %+v, want Regression=true Invalid=false", task.QualityGateResult)
	}
	// The held task's Error is the one-line hold note; the cause is in the failure detail.
	if task.FailureReason == nil || !strings.Contains(task.FailureReason.Detail, "regression") {
		t.Errorf("failure detail = %+v, want it to mention the regression", task.FailureReason)
	}
}
