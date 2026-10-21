package autonomous

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordedVerdictJudge is a recorded GuardrailFn fake. It records every
// invocation and returns one fixed verdict that carries a judge session id
// and a judge output file. The file is created by the constructor inside
// t.TempDir(), so the verdict's JudgeOutputPath points at a real file that
// recordGuardrailVerdict can remove.
type recordedVerdictJudge struct {
	invs []GuardrailInvocation
	ret  GuardrailVerdict
}

// newRecordedVerdictJudge returns a fake that answers every call with a
// verdict of the given outcome. A block carries a severity and an issue, as
// fakeGuardrailBlocking does. A pass carries only a summary, as
// fakeGuardrailAlwaysPass does.
func newRecordedVerdictJudge(t *testing.T, outcome string) *recordedVerdictJudge {
	t.Helper()
	out := filepath.Join(t.TempDir(), "judge-verdict.out")
	if err := os.WriteFile(out, []byte("verdict body"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	v := GuardrailVerdict{
		Outcome:         outcome,
		Summary:         "judge " + outcome,
		JudgeSessionID:  "judge-" + outcome + "-7f3a",
		JudgeOutputPath: out,
	}
	if outcome == "block" {
		v.Severity = "high"
		v.Issues = []string{"would cause an incident"}
	}
	return &recordedVerdictJudge{ret: v}
}

func (r *recordedVerdictJudge) fn(_ context.Context, inv GuardrailInvocation) (GuardrailVerdict, error) {
	r.invs = append(r.invs, inv)
	return r.ret, nil
}

// newRulesReviewVerdictPRD returns a manager and a PRD whose project dir
// holds an AGENT.md, with rules-review opted in at the PRD level. Content
// auto-fix is disabled so a content block ends the review instead of
// retrying it.
func newRulesReviewVerdictPRD(t *testing.T) (*Manager, *PRD) {
	t.Helper()
	cfg := DefaultConfig()
	// v9.10.18 — a configured judge model (tier-4 global verification
	// model) so the recorded fake rules-review judge is reachable; the
	// tests assert its verdict, not a resolution-failure block.
	cfg.VerificationModel = "ollama/qwen3.8:27b"
	m, err := NewManager(t.TempDir(), cfg, fakeDecompose)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	prd, err := m.CreatePRD("spec", dir, "claude", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	prd.PerPRDGuardrails = []string{"rules-review"}
	off := false
	prd.ContentAutoFixEnabled = &off
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return m, prd
}

func TestGuardrailVerdictBlock_RulesReviewBlock(t *testing.T) {
	m, prd := newRulesReviewVerdictPRD(t)
	rj := newRecordedVerdictJudge(t, "block")
	m.SetGuardrail(rj.fn)

	blocked, contentFix, err := m.runPRDCompletionRulesReview(context.Background(), prd)
	if err != nil {
		t.Fatalf("runPRDCompletionRulesReview: %v", err)
	}
	if !blocked {
		t.Fatal("blocked = false, want true for a block verdict")
	}
	if contentFix != nil {
		t.Errorf("contentFixVerdict = %+v, want nil with auto-fix disabled", contentFix)
	}
	if len(prd.Decisions) != 1 {
		t.Fatalf("Decisions: got %d entries, want 1", len(prd.Decisions))
	}
	d := prd.Decisions[0]
	if d.Kind != "guardrail_verdict" {
		t.Errorf("Kind = %q, want guardrail_verdict", d.Kind)
	}
	if d.VerdictOutcome != "block" {
		t.Errorf("VerdictOutcome = %q, want block", d.VerdictOutcome)
	}
	if d.JudgeSessionID != rj.ret.JudgeSessionID {
		t.Errorf("JudgeSessionID = %q, want %q", d.JudgeSessionID, rj.ret.JudgeSessionID)
	}
	if !strings.Contains(d.Note, "rules-review") {
		t.Errorf("Note %q does not name the guardrail rules-review", d.Note)
	}
	if _, err := os.Stat(rj.ret.JudgeOutputPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("judge output still present after a saved verdict: err=%v", err)
	}
}

func TestGuardrailVerdictBlock_RulesReviewPass(t *testing.T) {
	m, prd := newRulesReviewVerdictPRD(t)
	rj := newRecordedVerdictJudge(t, "pass")
	m.SetGuardrail(rj.fn)

	blocked, _, err := m.runPRDCompletionRulesReview(context.Background(), prd)
	if err != nil {
		t.Fatalf("runPRDCompletionRulesReview: %v", err)
	}
	if blocked {
		t.Fatal("blocked = true for a pass verdict")
	}
	if len(prd.Decisions) != 1 {
		t.Fatalf("Decisions: got %d entries, want 1", len(prd.Decisions))
	}
	if got := prd.Decisions[0].VerdictOutcome; got != "pass" {
		t.Errorf("VerdictOutcome = %q, want pass", got)
	}
	if prd.BlockedReason != "" {
		t.Errorf("BlockedReason = %q, want empty for a pass", prd.BlockedReason)
	}
	if _, err := os.Stat(rj.ret.JudgeOutputPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("judge output still present after a saved verdict: err=%v", err)
	}
}

func TestGuardrailVerdictBlock_TaskLevelBlock(t *testing.T) {
	m, prd, _ := newScriptGuardrailPRD(t, []string{"rules"}, nil)
	rj := newRecordedVerdictJudge(t, "block")
	m.SetGuardrail(rj.fn)
	off := false
	prd.ContentAutoFixEnabled = &off
	tk := &Task{ID: "t1", Title: "T", Spec: "do"}

	blocked, _, err := m.runPerTaskGuardrails(context.Background(), prd, tk)
	if err != nil {
		t.Fatalf("runPerTaskGuardrails: %v", err)
	}
	if !blocked {
		t.Fatal("blocked = false, want true for a block verdict")
	}
	var found bool
	for _, d := range prd.Decisions {
		if d.Kind == "guardrail_verdict" && d.VerdictOutcome == "block" {
			found = true
		}
	}
	if !found {
		t.Errorf("no guardrail_verdict decision with outcome block in %+v", prd.Decisions)
	}
	if tk.FailureReason == nil {
		t.Fatal("task FailureReason is nil, want it set on a block")
	}
	if tk.FailureReason.Source != "guardrail-task-block" {
		t.Errorf("FailureReason.Source = %q, want guardrail-task-block", tk.FailureReason.Source)
	}
}

func TestGuardrailVerdictBlock_OutputFileKeptUntilSaveSucceeds(t *testing.T) {
	m, prd := newRulesReviewVerdictPRD(t)
	rj := newRecordedVerdictJudge(t, "block")
	v := rj.ret
	v.Guardrail = "rules-review"
	path := v.JudgeOutputPath

	m.savePRDHook = func(*PRD) error { return errors.New("disk full") }
	if err := m.recordGuardrailVerdict(prd, prd.ID, v); err == nil {
		t.Fatal("recordGuardrailVerdict: want save error, got nil")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("judge output removed despite failed save: %v", err)
	}

	m.savePRDHook = nil
	if err := m.recordGuardrailVerdict(prd, prd.ID, v); err != nil {
		t.Fatalf("recordGuardrailVerdict after save recovers: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("judge output still present after successful save: err=%v", err)
	}
}
