package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// reviewIdentityFixture is a manager reading rules from the working tree,
// with the judge verdict cache open, a fake git runner whose reported head the
// test can move, and a PRD whose project directory holds a rules file.
type reviewIdentityFixture struct {
	m   *Manager
	prd *PRD
	dir string
	// head is what the fake runner prints for "rev-parse HEAD".
	head string
}

func newReviewIdentityFixture(t *testing.T) *reviewIdentityFixture {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Automation.RulesReviewSource = config.RulesReviewSourceWorktree
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules text one"), 0o644); err != nil {
		t.Fatal(err)
	}
	prd, err := m.store.CreatePRD("spec", dir, "ollama", "llama-3", EffortLow)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	f := &reviewIdentityFixture{m: m, prd: prd, dir: dir, head: "head-commit-one"}
	m.driftGit = func(ctx context.Context, d string, args ...string) (string, error) {
		return f.head + "\n", nil
	}
	return f
}

func (f *reviewIdentityFixture) invocation(t *testing.T) GuardrailInvocation {
	t.Helper()
	inv, _ := f.m.prdRulesReviewInvocation(context.Background(), f.prd)
	return inv
}

func TestReviewIdentityPRDInvocationCarriesBothFields(t *testing.T) {
	f := newReviewIdentityFixture(t)
	inv := f.invocation(t)
	if inv.ReviewHead != "head-commit-one" {
		t.Errorf("ReviewHead = %q, want the head the git runner printed", inv.ReviewHead)
	}
	if want := f.m.reviewRulesFingerprint(context.Background(), f.prd); want == "" || inv.RulesFingerprint != want {
		t.Errorf("RulesFingerprint = %q, want %q", inv.RulesFingerprint, want)
	}
	if got := judgeVerdictCommitSHA(f.prd, inv); got != "head-commit-one" {
		t.Errorf("judgeVerdictCommitSHA = %q, want ReviewHead to win", got)
	}
}

func TestReviewIdentityReviewHeadWinsOverAttemptCommit(t *testing.T) {
	prd := reviewHeadPRD("dir", "attemptsha")
	if got := judgeVerdictCommitSHA(prd, GuardrailInvocation{ReviewHead: "realhead"}); got != "realhead" {
		t.Errorf("commit = %q, want realhead", got)
	}
	if got := judgeVerdictCommitSHA(prd, GuardrailInvocation{}); got != "attemptsha" {
		t.Errorf("commit = %q, want the attempt commit when ReviewHead is empty", got)
	}
	if got := judgeVerdictCommitSHA(nil, GuardrailInvocation{ReviewHead: "realhead"}); got != "realhead" {
		t.Errorf("commit = %q, want realhead even without a PRD", got)
	}
}

func TestReviewIdentityMovedHeadMissesCache(t *testing.T) {
	f := newReviewIdentityFixture(t)
	inv := f.invocation(t)
	verdict := GuardrailVerdict{Guardrail: "rules-review", Outcome: "pass"}
	f.m.judgeVerdictCacheRecord(f.prd, inv, verdict, nil)
	if _, ok := f.m.judgeVerdictCacheHit(f.prd, inv); !ok {
		t.Fatal("same head and rules must hit the cache")
	}

	f.head = "head-commit-two"
	moved := f.invocation(t)
	if moved.ReviewHead == inv.ReviewHead {
		t.Fatalf("ReviewHead did not move: %q", moved.ReviewHead)
	}
	if _, ok := f.m.judgeVerdictCacheHit(f.prd, moved); ok {
		t.Error("a moved head must miss the cache")
	}
}

func TestReviewIdentityRulesEditChangesKey(t *testing.T) {
	f := newReviewIdentityFixture(t)
	inv := f.invocation(t)
	f.m.judgeVerdictCacheRecord(f.prd, inv, GuardrailVerdict{Guardrail: "rules-review", Outcome: "pass"}, nil)

	if err := os.WriteFile(filepath.Join(f.dir, "AGENT.md"), []byte("rules text two"), 0o644); err != nil {
		t.Fatal(err)
	}
	edited := f.invocation(t)
	if edited.RulesFingerprint == inv.RulesFingerprint {
		t.Fatal("editing the rules file did not change the fingerprint")
	}
	if judgeVerdictRulesHash(edited) == judgeVerdictRulesHash(inv) {
		t.Error("a rules edit must change the rules hash of the verdict key")
	}
	if _, ok := f.m.judgeVerdictCacheHit(f.prd, edited); ok {
		t.Error("a rules edit must miss the cache")
	}
}

func TestReviewIdentityPerTaskInvocationUnaffected(t *testing.T) {
	f := newReviewIdentityFixture(t)
	inv := GuardrailInvocation{
		PRDID:     f.prd.ID,
		Level:     "task",
		UnitID:    "t1",
		UnitTitle: "the task",
		UnitSpec:  "task spec",
		Guardrail: "rules",
	}
	if inv.ReviewHead != "" || inv.RulesFingerprint != "" {
		t.Fatal("a per-task invocation must leave both identity fields empty")
	}
	want := JudgeVerdictRulesHash("rules", "", "the task", "task spec", "")
	if got := judgeVerdictRulesHash(inv); got != want {
		t.Errorf("per-task rules hash = %q, want the unchanged key %q", got, want)
	}
	prd := reviewHeadPRD("", "attemptsha")
	if got := judgeVerdictCommitSHA(prd, inv); got != "attemptsha" {
		t.Errorf("per-task commit = %q, want the attempt commit", got)
	}
}

func TestReviewIdentityStampTwiceGivesSameResult(t *testing.T) {
	f := newReviewIdentityFixture(t)
	var inv GuardrailInvocation
	f.m.stampReviewIdentity(context.Background(), f.prd, &inv)
	first := inv
	f.m.stampReviewIdentity(context.Background(), f.prd, &inv)
	if inv != first {
		t.Errorf("second stamp changed the invocation: %+v then %+v", first, inv)
	}
	if first.ReviewHead == "" || first.RulesFingerprint == "" {
		t.Errorf("stamp left a field empty: %+v", first)
	}
}

func TestReviewIdentityStampIgnoresNilInvocation(t *testing.T) {
	f := newReviewIdentityFixture(t)
	f.m.stampReviewIdentity(context.Background(), f.prd, nil) // must not panic
}

func TestReviewIdentityStampWithoutProjectDirFallsBackToAttemptCommit(t *testing.T) {
	m := &Manager{driftGit: func(ctx context.Context, d string, args ...string) (string, error) {
		t.Fatal("runner must not be called without a project directory")
		return "", nil
	}}
	var inv GuardrailInvocation
	m.stampReviewIdentity(context.Background(), reviewHeadPRD("", "attemptsha"), &inv)
	if inv.ReviewHead != "attemptsha" || inv.RulesFingerprint != "" {
		t.Errorf("got head %q fingerprint %q, want the attempt commit and no fingerprint", inv.ReviewHead, inv.RulesFingerprint)
	}
}

// rejudgeOnceForIdentity runs one rejudge of a blocked PRD on a fixed head
// and returns the manager, the PRD id, the counted judge and the PRD.
func rejudgeOnceForIdentity(t *testing.T) (*Manager, string, *rejudgeJudge, *PRD) {
	t.Helper()
	m, prd, _ := newRejudgeBlockedPRD(t, TaskCompleted)
	setRejudgeHeadCommit(t, m, prd.ID, "sha-identity-1")
	j := newRejudgeContentBlockJudge(t, "judge-identity-a")
	m.SetGuardrail(j.fn)
	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("first RejudgeCompletionGuardrail: %v", err)
	}
	if j.calls != 1 {
		t.Fatalf("first run judge calls = %d, want 1", j.calls)
	}
	return m, prd.ID, j, got
}

func TestReviewIdentityRejudgeRecordsFingerprintOnVerdict(t *testing.T) {
	m, _, _, got := rejudgeOnceForIdentity(t)
	last := got.Verdicts[len(got.Verdicts)-1]
	want := m.reviewRulesFingerprint(context.Background(), got)
	if want == "" || last.RulesFingerprint != want {
		t.Errorf("verdict fingerprint = %q, want %q", last.RulesFingerprint, want)
	}
}

func TestReviewIdentityRejudgeReplaysWhenHeadAndRulesUnchanged(t *testing.T) {
	m, id, j, _ := rejudgeOnceForIdentity(t)
	if _, err := m.RejudgeCompletionGuardrail(context.Background(), id, "operator", ""); err != nil {
		t.Fatalf("second RejudgeCompletionGuardrail: %v", err)
	}
	if j.calls != 1 {
		t.Errorf("judge calls = %d, want 1 (same head and rules replay the stored verdict)", j.calls)
	}
}

func TestReviewIdentityRejudgeJudgesAgainWhenStoredVerdictHasNoFingerprint(t *testing.T) {
	m, id, j, _ := rejudgeOnceForIdentity(t)
	// A verdict recorded before the fingerprint existed has none.
	fresh, ok := m.Store().GetPRD(id)
	if !ok {
		t.Fatal("GetPRD failed")
	}
	for i := range fresh.Verdicts {
		fresh.Verdicts[i].RulesFingerprint = ""
	}
	if err := m.Store().SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	got, err := m.RejudgeCompletionGuardrail(context.Background(), id, "operator", "")
	if err != nil {
		t.Fatalf("second RejudgeCompletionGuardrail: %v", err)
	}
	if j.calls != 2 {
		t.Fatalf("judge calls = %d, want 2 (a verdict with no fingerprint is stale)", j.calls)
	}
	if last := got.Verdicts[len(got.Verdicts)-1]; last.RulesFingerprint == "" {
		t.Error("the fresh verdict must record the fingerprint")
	}
	if _, err := m.RejudgeCompletionGuardrail(context.Background(), id, "operator", ""); err != nil {
		t.Fatalf("third RejudgeCompletionGuardrail: %v", err)
	}
	if j.calls != 2 {
		t.Errorf("judge calls = %d, want 2 (judged again only once)", j.calls)
	}
}

func TestReviewIdentityRejudgeJudgesAgainWhenRulesEdited(t *testing.T) {
	m, id, j, got := rejudgeOnceForIdentity(t)
	before := got.Verdicts[len(got.Verdicts)-1].RulesFingerprint
	if err := os.WriteFile(filepath.Join(got.ProjectDir, "AGENT.md"), []byte("edited rules"), 0o644); err != nil {
		t.Fatal(err)
	}

	after, err := m.RejudgeCompletionGuardrail(context.Background(), id, "operator", "")
	if err != nil {
		t.Fatalf("second RejudgeCompletionGuardrail: %v", err)
	}
	if j.calls != 2 {
		t.Fatalf("judge calls = %d, want 2 (edited rules must be judged again)", j.calls)
	}
	if now := after.Verdicts[len(after.Verdicts)-1].RulesFingerprint; now == "" || now == before {
		t.Errorf("fingerprint after the edit = %q, before = %q; want a different non-empty value", now, before)
	}
}
