package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"
)

func countDecisionKind(prd *PRD, kind string) int {
	n := 0
	for _, d := range prd.Decisions {
		if d.Kind == kind {
			n++
		}
	}
	return n
}

// TestRejudgeReuseAddsNoDuplicateVerdict: a second rejudge on an unchanged
// head reuses the stored verdict, so the verdict list and the
// guardrail_verdict Decisions keep one entry per review run, the stored
// verdict keeps its time, commit and fingerprint, and the guardrail_rejudge
// Decision says why it was reused.
func TestRejudgeReuseAddsNoDuplicateVerdict(t *testing.T) {
	m, prd, _ := newRejudgeBlockedPRD(t, TaskCompleted)
	setRejudgeHeadCommit(t, m, prd.ID, "sha-reuse-1")
	j := newRejudgeContentBlockJudge(t, "judge-reuse-a")
	m.SetGuardrail(j.fn)

	first, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("first rejudge: %v", err)
	}
	stored := first.Verdicts[len(first.Verdicts)-1]
	nVerdicts := len(first.Verdicts)
	nVerdictDecisions := countDecisionKind(first, "guardrail_verdict")

	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("second rejudge: %v", err)
	}
	if j.calls != 1 {
		t.Fatalf("judge calls = %d, want 1 (stored verdict reused)", j.calls)
	}
	if len(got.Verdicts) != nVerdicts {
		t.Errorf("verdicts = %d, want %d (no duplicate)", len(got.Verdicts), nVerdicts)
	}
	if n := countDecisionKind(got, "guardrail_verdict"); n != nVerdictDecisions {
		t.Errorf("guardrail_verdict decisions = %d, want %d", n, nVerdictDecisions)
	}
	last := got.Verdicts[len(got.Verdicts)-1]
	if !last.VerdictAt.Equal(stored.VerdictAt) || last.JudgedCommitSHA != stored.JudgedCommitSHA || last.RulesFingerprint != stored.RulesFingerprint {
		t.Errorf("stored verdict changed: %+v, want %+v", last, stored)
	}
	rej := lastDecision(got, "guardrail_rejudge")
	if rej == nil {
		t.Fatal("no guardrail_rejudge decision")
	}
	if !rej.VerdictReused || rej.JudgedCommitSHA != "sha-reuse-1" {
		t.Errorf("decision reused=%v sha=%q, want true and sha-reuse-1", rej.VerdictReused, rej.JudgedCommitSHA)
	}
	if want := reusedVerdictNote("sha-reuse-1", stored.VerdictAt); rej.Note != want {
		t.Errorf("note = %q, want %q", rej.Note, want)
	}
	if got.Status != PRDBlocked || got.BlockedReason == "" {
		t.Errorf("status = %s reason = %q, want blocked with the verdict's reason", got.Status, got.BlockedReason)
	}
}

// TestRejudgeReuseAppendsWhenPRDLacksVerdict: when the reused verdict came
// from the persisted cache and the PRD does not hold it, it is appended
// exactly once, with the cached verdict's own commit.
func TestRejudgeReuseAppendsWhenPRDLacksVerdict(t *testing.T) {
	m, prd, _ := newRejudgeBlockedPRD(t, TaskCompleted)
	setRejudgeHeadCommit(t, m, prd.ID, "sha-reuse-2")
	j := newRejudgeContentBlockJudge(t, "judge-reuse-b")
	m.SetGuardrail(j.fn)
	if _, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", ""); err != nil {
		t.Fatalf("first rejudge: %v", err)
	}
	fresh, _ := m.Store().GetPRD(prd.ID)
	m.Store().WithLock(func() { fresh.Verdicts = nil })
	if err := m.Store().SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("second rejudge: %v", err)
	}
	if j.calls != 1 {
		t.Fatalf("judge calls = %d, want 1", j.calls)
	}
	if len(got.Verdicts) != 1 {
		t.Fatalf("verdicts = %d, want exactly 1 appended", len(got.Verdicts))
	}
	if got.Verdicts[0].JudgedCommitSHA != "sha-reuse-2" || got.Verdicts[0].JudgeSessionID != "judge-reuse-b" {
		t.Errorf("appended verdict = %+v", got.Verdicts[0])
	}
	if rej := lastDecision(got, "guardrail_rejudge"); rej == nil || !rej.VerdictReused {
		t.Errorf("rejudge decision = %+v, want reused", rej)
	}
}

// TestRejudgeFreshVerdictNotMarkedReused: a rejudge that runs the judge
// records a new verdict and a Decision that is not marked reused.
func TestRejudgeFreshVerdictNotMarkedReused(t *testing.T) {
	m, id, j, got := runRejudgeHeadChange(t)
	_ = m
	_ = id
	if j.calls != 2 {
		t.Fatalf("judge calls = %d, want 2", j.calls)
	}
	rej := lastDecision(got, "guardrail_rejudge")
	if rej == nil || rej.VerdictReused {
		t.Fatalf("rejudge decision = %+v, want not reused", rej)
	}
	if strings.Contains(rej.Note, "stored verdict reused") {
		t.Errorf("note = %q, must not claim reuse", rej.Note)
	}
}

func TestReusedVerdictNoteWording(t *testing.T) {
	at := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	want := "stored verdict reused: the head commit abc123 and the rules text are unchanged since that verdict was made at 2026-11-11T10:00:00Z; no new review ran"
	if got := reusedVerdictNote("abc123", at); got != want {
		t.Errorf("note = %q, want %q", got, want)
	}
}

func TestVerdictAlreadyRecorded(t *testing.T) {
	at := time.Now()
	v := GuardrailVerdict{VerdictAt: at, JudgedCommitSHA: "s1", JudgeSessionID: "j1"}
	prd := &PRD{Verdicts: []GuardrailVerdict{v}}
	if !verdictAlreadyRecorded(prd, v) {
		t.Error("identical verdict not found")
	}
	for name, o := range map[string]GuardrailVerdict{
		"time":    {VerdictAt: at.Add(time.Second), JudgedCommitSHA: "s1", JudgeSessionID: "j1"},
		"commit":  {VerdictAt: at, JudgedCommitSHA: "s2", JudgeSessionID: "j1"},
		"session": {VerdictAt: at, JudgedCommitSHA: "s1", JudgeSessionID: "j2"},
	} {
		if verdictAlreadyRecorded(prd, o) {
			t.Errorf("%s differs but was reported recorded", name)
		}
	}
	if verdictAlreadyRecorded(&PRD{}, v) {
		t.Error("empty PRD reported recorded")
	}
}
