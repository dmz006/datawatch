package autonomous

// gate_disagreement — the autonomous.gate_disagreement policy
// (default "warn") governs what happens when a task's verifier PASSES
// but the quality gate disagrees:
//
//   - "warn": the task may complete; the disagreement is recorded on
//     the task (GateDisagreement field), the PRD decision log, and a
//     warning log line.
//   - "block": a real regression from a VALID baseline fails the task
//     with an error prefixed "gate_disagreement: …".
//   - An INVALID gate (no usable baseline) NEVER blocks under any
//     policy — it records + warns and lets the task complete.
//
// These tests drive the real executor through Manager.Run with stub
// spawn/verify fns. The "worker" is simulated by the spawn stub
// mutating the project (breaking a test file), so the pre-PRD baseline
// is valid and the post-task gate run shows a real regression.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/pipeline"
)

// goModuleFixture writes a minimal Go module whose tests pass at
// baseline; the spawn stub later breaks one to manufacture a regression.
func goModuleFixture(t *testing.T) string {
	t.Helper()
	projDir := t.TempDir()
	files := map[string]string{
		"go.mod":       "module testmod\ngo 1.21\n",
		"main.go":      "package main\n\nfunc main() {}\n\nfunc Add(a, b int) int { return a + b }\n",
		"main_test.go": "package main\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"1+2 != 3\")\n\t}\n}\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(projDir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return projDir
}

// breakProject simulates worker "work" that regresses the test suite:
// appends a test that panics.
func breakProject(t *testing.T, projDir string, breakFile string) {
	t.Helper()
	body := "package main\n\nimport \"testing\"\n\nfunc TestBroken(t *testing.T) { panic(\"worker broke it\") }\n"
	if err := os.WriteFile(filepath.Join(projDir, breakFile), []byte(body), 0o644); err != nil {
		t.Fatalf("breakProject: %v", err)
	}
}

// gateDisagreementRun boots store + PRD + manager the way the existing
// quality-gate suite does and runs the executor once. gate is the per-PRD quality
// gate; the return value is the fresh PRD after Run.
func gateDisagreementRun(t *testing.T, projDir string, gate pipeline.QualityGateConfig, mgrCfg Config, spawn WorkerFnStub) *PRD {
	t.Helper()
	dir := t.TempDir()
	st, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	prd, err := st.CreatePRD("gate_disagreement prd", projDir, "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := st.SetStories(prd.ID, []Story{{
		Title: "S1",
		Tasks: []Task{{Title: "T1", Spec: "build it"}},
	}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, ok := st.GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD not found after SetStories")
	}
	prd.Status = PRDApproved
	prd.QualityGates = &gate
	if err := st.SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	mgr, err := NewManager(dir, mgrCfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Stop()

	spawnFn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-gd"}, nil
	}
	if spawn != nil {
		spawnFn = func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
			return spawn(ctx, req)
		}
	}
	verifyFn := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok", VerifiedAt: time.Now()}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := mgr.Run(ctx, prd.ID, spawnFn, verifyFn); err != nil {
		t.Logf("Run returned: %v", err) // Run errors (e.g. a blocked task) are part of the assertion surface
	}

	// The manager's store and the setup store are separate in-memory
	// instances over the same data dir — read the manager's store for the
	// post-run state (same pattern as the quality-gate suite's assertions).
	fresh, ok := mgr.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("final GetPRD: not found in manager store")
	}
	return fresh
}

type WorkerFnStub = func(ctx context.Context, req SpawnRequest) (SpawnResult, error)

// findTask returns the first task of the first story.
func findTask(t *testing.T, prd *PRD) *Task {
	t.Helper()
	for _, s := range prd.Story {
		for i := range s.Tasks {
			return &s.Tasks[i]
		}
	}
	t.Fatal("no tasks found on PRD")
	return nil
}

func assertGateDisagreementDecision(t *testing.T, prd *PRD, noteWant string) {
	t.Helper()
	found := false
	for _, d := range prd.Decisions {
		if d.Kind == "gate_disagreement" && strings.Contains(d.Note, noteWant) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no gate_disagreement decision-log entry containing %q (decisions: %+v)", noteWant, prd.Decisions)
	}
}

// TestGateDisagreement_SetConfigSanitizes — Manager.SetConfig
// (hot-reload entry) normalizes an unset or unrecognized
// gate_disagreement to "warn" before storing; the policy helper agrees.
func TestGateDisagreement_SetConfigSanitizes(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Stop()

	cases := []struct{ in, want string }{
		{"", "warn"},
		{"warn", "warn"},
		{"block", "block"},
		{"bogus", "warn"},
		{"BLOCK", "warn"},
	}
	for _, c := range cases {
		cfg := mgr.Config()
		cfg.GateDisagreement = c.in
		mgr.SetConfig(cfg)
		if got := mgr.Config().GateDisagreement; got != c.want {
			t.Fatalf("SetConfig(%q) → stored %q, want %q", c.in, got, c.want)
		}
		if got := mgr.gateDisagreementPolicy(); got != c.want {
			t.Fatalf("gateDisagreementPolicy after SetConfig(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestGateDisagreement_APISetConfigRejectsInvalid — the REST/MCP write
// path (API.SetConfig) rejects invalid gate_disagreement values and
// leaves a valid value (including an omitted key) untouched.
func TestGateDisagreement_APISetConfigRejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Stop()
	apiV2 := &API{M: mgr}

	if err := apiV2.SetConfig(json.RawMessage([]byte(`{"gate_disagreement":"sometimes"}`))); err == nil {
		t.Fatal("API.SetConfig accepted gate_disagreement=sometimes, want reject")
	}
	if err := apiV2.SetConfig(json.RawMessage([]byte(`{"gate_disagreement":42}`))); err == nil {
		t.Fatal("API.SetConfig accepted non-string gate_disagreement, want reject")
	}
	if got := mgr.Config().GateDisagreement; got != "warn" {
		t.Fatalf("rejected writes must not change the stored value (now %q, was \"warn\")", got)
	}
	if err := apiV2.SetConfig(json.RawMessage([]byte(`{"enabled":true,"gate_disagreement":"block"}`))); err != nil {
		t.Fatalf("API.SetConfig rejected valid \"block\": %v", err)
	}
	if got := mgr.Config().GateDisagreement; got != "block" {
		t.Fatalf("after valid SetConfig, GateDisagreement = %q, want \"block\"", got)
	}
	// Omitted key must leave the current value untouched.
	if err := apiV2.SetConfig(json.RawMessage([]byte(`{"enabled":true}`))); err != nil {
		t.Fatalf("API.SetConfig omitting the key: %v", err)
	}
	if got := mgr.Config().GateDisagreement; got != "block" {
		t.Fatalf("after SetConfig omitting gate_disagreement, value changed to %q, want \"block\"", got)
	}
}

// TestGateDisagreement_VerdictPassRegression_Warn — verifier OK +
// valid-baseline regression + policy "warn" (BlockOnRegression OFF):
// the task COMPLETES with the disagreement recorded on the task and in
// the decision log.
func TestGateDisagreement_VerdictPassRegression_Warn(t *testing.T) {
	projDir := goModuleFixture(t)
	gate := pipeline.QualityGateConfig{
		Enabled:           true,
		TestCommand:       "go test ./...",
		Timeout:           30,
		BlockOnRegression: false,
	}
	cfg := Config{
		Enabled:          true,
		AutoFixRetries:   0,
		GateDisagreement: "warn",
	}
	prd := gateDisagreementRun(t, projDir, gate, cfg, func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		breakProject(t, projDir, "broken_test.go")
		return SpawnResult{SessionID: "sess-gd"}, nil
	})

	tk := findTask(t, prd)
	if tk.Status != TaskCompleted {
		t.Fatalf("warn policy: task = %s (err=%q), want completed", tk.Status, tk.Error)
	}
	if tk.QualityGateResult == nil || !tk.QualityGateResult.Regression {
		t.Fatalf("warn policy: expected recorded regression, got %+v", tk.QualityGateResult)
	}
	gd := tk.GateDisagreement
	if gd == nil {
		t.Fatal("warn policy: task.GateDisagreement is nil, want recorded")
	}
	if !gd.VerifierOK || !gd.GateRegression || gd.GateInvalid {
		t.Fatalf("warn policy: GateDisagreement = %+v, want verifier_ok=true gate_regression=true gate_invalid=false", gd)
	}
	if gd.Policy != "warn" {
		t.Fatalf("warn policy: GateDisagreement.Policy = %q, want \"warn\"", gd.Policy)
	}
	assertGateDisagreementDecision(t, prd, "policy=warn")
}

// TestGateDisagreement_VerdictPassRegression_Block — verifier OK +
// valid-baseline regression + policy "block" (BlockOnRegression OFF, so
// the NEW policy — not the per-PRD knob — produces the failure): the
// task FAILS with an error prefixed "gate_disagreement:".
func TestGateDisagreement_VerdictPassRegression_Block(t *testing.T) {
	projDir := goModuleFixture(t)
	gate := pipeline.QualityGateConfig{
		Enabled:           true,
		TestCommand:       "go test ./...",
		Timeout:           30,
		BlockOnRegression: false,
	}
	cfg := Config{
		Enabled:          true,
		AutoFixRetries:   0,
		GateDisagreement: "block",
	}
	prd := gateDisagreementRun(t, projDir, gate, cfg, func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		breakProject(t, projDir, "broken_test.go")
		return SpawnResult{SessionID: "sess-gd"}, nil
	})

	tk := findTask(t, prd)
	if tk.Status != TaskFailed {
		t.Fatalf("block policy: task = %s, want failed (valid-baseline regression)", tk.Status)
	}
	if !strings.Contains(tk.Error, "gate_disagreement:") {
		t.Fatalf("block policy: task.Error = %q, want prefix \"gate_disagreement:\"", tk.Error)
	}
	if tk.GateDisagreement == nil {
		t.Fatal("block policy: task.GateDisagreement is nil, want recorded")
	}
	if tk.GateDisagreement.Policy != "block" {
		t.Fatalf("block policy: GateDisagreement.Policy = %q, want \"block\"", tk.GateDisagreement.Policy)
	}
	if tk.GateDisagreement.GateInvalid {
		t.Fatal("block policy: gate must read as a valid-baseline regression, not invalid")
	}
	assertGateDisagreementDecision(t, prd, "policy=block")
}

// TestGateDisagreement_InvalidGateNeverBlocks — verifier OK + INVALID
// gate (no usable baseline) under policy "block": the gate must record
// + warn and let the task complete; it never blocks.
func TestGateDisagreement_InvalidGateNeverBlocks(t *testing.T) {
	projDir := goModuleFixture(t)
	gate := pipeline.QualityGateConfig{
		Enabled:           true,
		TestCommand:       "echo 'broken'", // nothing parseable as test output → invalid baseline
		Timeout:           30,
		BlockOnRegression: true, // even the strictest per-PRD setting must not block an unusable gate
	}
	cfg := Config{
		Enabled:          true,
		AutoFixRetries:   0,
		GateDisagreement: "block",
	}
	prd := gateDisagreementRun(t, projDir, gate, cfg, nil)

	tk := findTask(t, prd)
	if tk.Status != TaskCompleted {
		t.Fatalf("invalid gate: task = %s (err=%q), want completed (invalid gate never blocks)", tk.Status, tk.Error)
	}
	if tk.QualityGateResult == nil || !tk.QualityGateResult.Invalid {
		t.Fatalf("invalid gate: expected invalid QualityGateResult, got %+v", tk.QualityGateResult)
	}
	gd := tk.GateDisagreement
	if gd == nil {
		t.Fatal("invalid gate: task.GateDisagreement is nil, want recorded (even when it cannot block)")
	}
	if !gd.GateInvalid || gd.GateRegression || !gd.VerifierOK {
		t.Fatalf("invalid gate: GateDisagreement = %+v, want verifier_ok=true gate_invalid=true gate_regression=false", gd)
	}
	assertGateDisagreementDecision(t, prd, "invalid gate (never blocks)")
}
