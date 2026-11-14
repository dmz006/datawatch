// Finished-unmerged rejudge tests: a rejudgment must also run for a
// completed or archived Automaton whose work is not yet on the base branch
// (merge state finished_not_merged) and whose completion review is pending
// or blocked — a pass clears the stale block records, a block flips the
// record back to blocked. The merge state is derived on read from the base
// repository, so these tests build a real on-disk base repo (the amg*
// helpers) rather than injecting a git runner.
package autonomous

import (
	"context"
	"strings"
	"testing"
)

// newFinishedUnmergedPRD builds, from the blocked-PRD fixture, a record of
// the given finished status (PRDCompleted or PRDArchived) whose work branch
// amgBranch carries one commit the base repo has not taken — i.e. the
// derived merge state is finished_not_merged. When withStaleBlock is set,
// the record also carries the stale block: a prior rules-review block
// verdict judged on an earlier head, a BlockedReason and a structured
// RulesReviewOutcome — the residue a pass must clear.
func newFinishedUnmergedPRD(t *testing.T, status PRDStatus, withStaleBlock bool) (*Manager, *PRD) {
	t.Helper()
	m, prd, _ := newRejudgeBlockedPRD(t, TaskCompleted)
	if withStaleBlock {
		setRejudgeHeadCommit(t, m, prd.ID, "sha-head-1")
	}
	// The work branch is ahead of the base repo: one content commit the
	// base branch has not taken, so the guard derives finished_not_merged.
	base := amgBaseRepo(t)
	amgBranchAhead(t, base)
	cfg := m.Config()
	cfg.WorktreeBaseRepo = base
	m.SetConfig(cfg)

	fresh, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD failed")
	}
	fresh.Status = status
	fresh.Git.Branch = amgBranch
	if withStaleBlock {
		fresh.Verdicts = append(fresh.Verdicts, GuardrailVerdict{
			Guardrail:        "rules-review",
			Outcome:          "block",
			Category:         "content",
			Summary:          "prior finding",
			Issues:           []string{"prior finding"},
			JudgeSessionID:   "judge-prior-3c1d",
			JudgedCommitSHA:  "sha-head-1",
			RulesFingerprint: "fingerprint-old",
		})
		fresh.RulesReviewOutcome = &RulesReviewOutcome{
			Action:       "block",
			Findings:     []string{"prior finding"},
			Summary:      "prior finding",
			JudgeOutcome: "block",
		}
	}
	if err := m.Store().SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	got, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD after SavePRD failed")
	}
	if want := MergeStateFinishedNotMerged; m.AutomatonMergeState(got) != want {
		t.Fatalf("AutomatonMergeState = %q, want %q (the fixture must actually be finished-not-merged)", m.AutomatonMergeState(got), want)
	}
	return m, got
}

// TestRejudgeFinishedUnmergedArchivedPassKeepsArchived — the core
// acceptance: an automated review runs for a finished (archived)
// finished_not_merged record whose completion review is pending (no
// verdict yet) and whose stale block residue is cleared by the pass — the
// record keeps its archived status, the block reason and outcome are
// dropped, and the verdict and rejudge decisions are recorded.
func TestRejudgeFinishedUnmergedArchivedPassKeepsArchived(t *testing.T) {
	m, prd := newFinishedUnmergedPRD(t, PRDArchived, true)
	j := newRejudgePassJudge(t)
	m.SetGuardrail(j.fn)
	before := taskStatusSummaries(prd)

	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("RejudgeCompletionGuardrail: %v", err)
	}
	if got.Status != PRDArchived {
		t.Errorf("status = %s, want archived kept by a pass", got.Status)
	}
	if got.BlockedReason != "" {
		t.Errorf("BlockedReason = %q, want the stale block cleared on a pass", got.BlockedReason)
	}
	if got.RulesReviewOutcome != nil {
		t.Errorf("RulesReviewOutcome = %+v, want cleared on a pass", got.RulesReviewOutcome)
	}
	if j.calls != 1 {
		t.Errorf("judge calls = %d, want 1 (a fresh review ran for the finished record)", j.calls)
	}
	last := got.Verdicts[len(got.Verdicts)-1]
	if last.Outcome != "pass" || last.Guardrail != "rules-review" {
		t.Errorf("latest verdict = %+v, want the fresh pass rules-review verdict", last)
	}
	kinds := decisionKinds(got)
	if !kindsContains(kinds, "guardrail_verdict") {
		t.Errorf("Decisions %v: missing the guardrail_verdict Decision", kinds)
	}
	if !kindsContains(kinds, "guardrail_rejudge") {
		t.Errorf("Decisions %v: missing the guardrail_rejudge Decision", kinds)
	}
	if strings.Join(taskStatusSummaries(got), ",") != strings.Join(before, ",") {
		t.Errorf("task statuses changed: before %v, after %v", before, taskStatusSummaries(got))
	}
	stored, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD (stored) failed")
	}
	if stored.Status != PRDArchived || stored.BlockedReason != "" || stored.RulesReviewOutcome != nil {
		t.Errorf("stored status=%s reason=%q outcome=%+v, want archived with the block records cleared", stored.Status, stored.BlockedReason, stored.RulesReviewOutcome)
	}
}

// TestRejudgeFinishedUnmergedCompletedPassKeepsCompleted — the same pass
// for a completed record: it stays completed, the block clears.
func TestRejudgeFinishedUnmergedCompletedPassKeepsCompleted(t *testing.T) {
	m, prd := newFinishedUnmergedPRD(t, PRDCompleted, true)
	j := newRejudgePassJudge(t)
	m.SetGuardrail(j.fn)

	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("RejudgeCompletionGuardrail: %v", err)
	}
	if got.Status != PRDCompleted {
		t.Errorf("status = %s, want completed kept by a pass", got.Status)
	}
	if got.BlockedReason != "" || got.RulesReviewOutcome != nil {
		t.Errorf("block records not cleared on a pass: reason=%q outcome=%+v", got.BlockedReason, got.RulesReviewOutcome)
	}
	if j.calls != 1 {
		t.Errorf("judge calls = %d, want 1", j.calls)
	}
}

// TestRejudgeFinishedUnmergedArchivedBlockFlipsBlocked — "unless the
// verdict flips it": a block verdict on a finished record flips it back to
// blocked with the new reason, so the block stays visible for operator
// action and a later pass is what lifts it again.
func TestRejudgeFinishedUnmergedArchivedBlockFlipsBlocked(t *testing.T) {
	m, prd := newFinishedUnmergedPRD(t, PRDArchived, true)
	j := newRejudgeBlockJudge(t)
	m.SetGuardrail(j.fn)

	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("RejudgeCompletionGuardrail: %v", err)
	}
	if got.Status != PRDBlocked {
		t.Errorf("status = %s, want blocked: a block verdict flips a finished record back", got.Status)
	}
	if !strings.Contains(got.BlockedReason, "would cause an incident") {
		t.Errorf("BlockedReason = %q, want the new verdict's finding", got.BlockedReason)
	}
	if j.calls != 1 {
		t.Errorf("judge calls = %d, want 1 (the stale verdict must be re-judged, not replayed)", j.calls)
	}
	last := got.Verdicts[len(got.Verdicts)-1]
	if last.Outcome != "block" || last.JudgeSessionID != "judge-rejudge-block-7f3a" {
		t.Errorf("latest verdict = %+v, want the fresh block verdict", last)
	}
}

// TestRejudgeStaleBlockClearedAfterAutoFixMerge — the stale-block flow:
// the completion review blocked on head sha-head-1, the auto-fix task has
// merged its fix (new head sha-head-2), and the rejudge re-runs the
// review once for the new head; a pass clears the old block (status,
// BlockedReason, RulesReviewOutcome) and the stale verdict never replays.
func TestRejudgeStaleBlockClearedAfterAutoFixMerge(t *testing.T) {
	m, prd := newFinishedUnmergedPRD(t, PRDCompleted, true)
	// The auto-fix task completes on the record and merges its fix onto
	// the branch: the head moves off the commit the stale verdict judged.
	setRejudgeHeadCommit(t, m, prd.ID, "sha-head-2")
	j := newRejudgePassJudge(t)
	m.SetGuardrail(j.fn)

	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("RejudgeCompletionGuardrail: %v", err)
	}
	if j.calls != 1 {
		t.Fatalf("judge calls = %d, want 1 (the stale verdict must not replay on the new head)", j.calls)
	}
	if got.Status != PRDCompleted {
		t.Errorf("status = %s, want completed after the stale block clears", got.Status)
	}
	if got.BlockedReason != "" {
		t.Errorf("BlockedReason = %q, want the stale block cleared", got.BlockedReason)
	}
	if got.RulesReviewOutcome != nil {
		t.Errorf("RulesReviewOutcome = %+v, want cleared", got.RulesReviewOutcome)
	}
	if len(got.Verdicts) == 0 {
		t.Fatal("no verdicts after the rejudge")
	}
	last := got.Verdicts[len(got.Verdicts)-1]
	if last.Outcome != "pass" || last.JudgedCommitSHA != "sha-head-2" {
		t.Errorf("latest verdict = %+v, want a fresh pass judged on the new head sha-head-2", last)
	}
	stored, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD (stored) failed")
	}
	if stored.BlockedReason != "" || stored.RulesReviewOutcome != nil {
		t.Errorf("stored block records survived the pass: reason=%q outcome=%+v", stored.BlockedReason, stored.RulesReviewOutcome)
	}
}

// TestRejudgeFinishedMergedRefused — a finished record whose work is
// already on the base branch is refused: there is nothing unmerged for a
// review to clear, the judge never runs, and no Decision is added.
func TestRejudgeFinishedMergedRefused(t *testing.T) {
	// Base repo with the work branch's commit already landed on main:
	// after ff-merging the branch, the guard must derive "merged", not
	// finished_not_merged — so land the work before wiring the base in
	// on the record.
	base := amgBaseRepo(t)
	amgBranchAhead(t, base)
	amgGit(t, base, "merge", "-q", "--ff", amgBranch)
	// Build the finished record first (no base repo configured yet).
	m, prd := newFinishedUnmergedPRDNoBase(t, PRDCompleted)
	// Now configure the base and the record's branch: the derived merge
	// state must read as merged for the record's branch.
	cfg := m.Config()
	cfg.WorktreeBaseRepo = base
	m.SetConfig(cfg)
	fresh, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD failed")
	}
	fresh.Git.Branch = amgBranch
	if err := m.Store().SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	if got := m.AutomatonMergeState(fresh); got == MergeStateFinishedNotMerged {
		t.Fatalf("AutomatonMergeState = %q, want not finished_not_merged (work is on base)", got)
	}

	j := newRejudgePassJudge(t)
	m.SetGuardrail(j.fn)
	nv, nd := len(fresh.Verdicts), len(fresh.Decisions)
	_, err := m.RejudgeCompletionGuardrail(context.Background(), fresh.ID, "operator", "")
	if err == nil {
		t.Fatal("want a refusal for a finished record already on the base branch, got nil")
	}
	if !strings.Contains(err.Error(), "finished_not_merged") {
		t.Errorf("refusal error %q does not name the finished_not_merged condition", err)
	}
	if j.calls != 0 {
		t.Errorf("judge calls = %d, want 0 (refusal happens before the judge runs)", j.calls)
	}
	stored, _ := m.Store().GetPRD(fresh.ID)
	if len(stored.Verdicts) != nv || len(stored.Decisions) != nd {
		t.Errorf("PRD changed after a refusal: verdicts=%d decisions=%d", len(stored.Verdicts), len(stored.Decisions))
	}
}

// TestRejudgeFinishedPassedReviewRefused — a finished unmerged record
// whose completion review already passed at its last verdict is refused:
// the review is not pending or blocked, so there is no block to clear.
func TestRejudgeFinishedPassedReviewRefused(t *testing.T) {
	m, prd := newFinishedUnmergedPRD(t, PRDArchived, false)
	fresh, _ := m.Store().GetPRD(prd.ID)
	fresh.Verdicts = append(fresh.Verdicts, GuardrailVerdict{
		Guardrail: "rules-review", Outcome: "pass",
		Summary: "clean review", JudgedCommitSHA: "sha-prior-9a2b",
	})
	if err := m.Store().SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	j := newRejudgePassJudge(t)
	m.SetGuardrail(j.fn)
	_, err := m.RejudgeCompletionGuardrail(context.Background(), fresh.ID, "operator", "")
	if err == nil {
		t.Fatal("want a refusal for a finished record whose review already passed, got nil")
	}
	if !strings.Contains(err.Error(), "already passed") {
		t.Errorf("refusal error %q does not say the review already passed", err)
	}
	if j.calls != 0 {
		t.Errorf("judge calls = %d, want 0 (refusal happens before the judge runs)", j.calls)
	}
	stored, _ := m.Store().GetPRD(fresh.ID)
	if kindsContains(decisionKinds(stored), "guardrail_rejudge") {
		t.Error("a guardrail_rejudge Decision exists after a refusal; none may be added")
	}
}

// newFinishedUnmergedPRDNoBase is the finished-record fixture WITHOUT the
// base repo step, so a test can control when the base lands relative to
// the record's branch.
func newFinishedUnmergedPRDNoBase(t *testing.T, status PRDStatus) (*Manager, *PRD) {
	t.Helper()
	m, prd, _ := newRejudgeBlockedPRD(t, TaskCompleted)
	fresh, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD failed")
	}
	fresh.Status = status
	if err := m.Store().SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	got, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD after SavePRD failed")
	}
	return m, got
}
