// Rejudgment of the PRD-completion guardrail.
//
// A PRD that ended PRDBlocked on its completion-time "rules-review"
// judgment is stuck awaiting operator action. This gives the operator a
// path to ask for the SAME judgment to run again — e.g. after the finding
// has been addressed or the prior verdict was an infra-category fluke —
// without resetting or editing any task.
//
// RejudgeCompletionGuardrail is deliberately narrow in what it accepts:
// the PRD must exist, be PRDBlocked, and have EVERY task in EVERY story
// at TaskCompleted (a re-judgment of "this work is done" is meaningless,
// and possibly premature, while real work is still pending). It then runs
// the identical rules-review GuardrailInvocation the completion rollup
// runs (prdRulesReviewInvocation) through the same
// runGuardrailWithInfraRetry infra-retry wrapper that
// runPRDCompletionRulesReview runs, and records the outcome itself: the
// verdict is durably saved FIRST (recordGuardrailVerdict), a
// "guardrail_rejudge" Decision with the acting operator names the
// outcome, and only then does the status change — a pass unblocks the
// PRD (PRDCompleted, BlockedReason cleared, one CommitPRDIfRev save
// following commitRollupStatus's concurrency guard), a block keeps the
// PRD PRDBlocked with BlockedReason set from the verdict, and no verdict
// (judge error or empty outcome) leaves the PRD blocked with its
// previous BlockedReason and returns an error so the operator sees it.
package autonomous

import (
	"context"
	"fmt"
	"time"
)

// judgeCompletionRulesReview runs the PRD-level "rules-review" judgment
// for prd and returns its raw verdict, without recording anything on the
// PRD (no Verdicts append, no Decision, no status change). It is the
// shared judge step of RejudgeCompletionGuardrail: building the
// invocation, the infra-retry wrapper and the guardrail wiring are
// exactly what runPRDCompletionRulesReview uses, differing only in that
// a missing guardrail function is an error here (an operator asked for a
// judgment and got none) instead of the silent skip the completion
// rollup applies (opt-in feature never turned on).
func (m *Manager) judgeCompletionRulesReview(ctx context.Context, prd *PRD) (GuardrailVerdict, error) {
	m.mu.Lock()
	fn := m.guardrail
	m.mu.Unlock()
	if fn == nil {
		return GuardrailVerdict{}, fmt.Errorf("no guardrail function is wired for prd %s (SetGuardrail); the rules-review judgment cannot run", prd.ID)
	}
	inv, resErr := m.prdRulesReviewInvocation(prd)
	if resErr != nil {
		// v9.10.18 — no judge model resolvable at any level: loud,
		// non-retried infra block, recorded by RejudgeCompletionGuardrail
		// exactly like any other block verdict.
		return NoGuardrailModelVerdict("rules-review", resErr), nil
	}
	return m.runGuardrailWithInfraRetry(ctx, prd, inv, fn)
}

// firstNonCompletedTask returns the (in story, then task order) first
// task of prd whose Status is not TaskCompleted — cancelled and failed
// included, exactly like any other non-completed state. All three
// returns are empty when every task is completed.
func firstNonCompletedTask(prd *PRD) (storyID, taskID string, status TaskStatus) {
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			if t.Status != TaskCompleted {
				return prd.Story[si].ID, t.ID, t.Status
			}
		}
	}
	return "", "", ""
}

// RejudgeCompletionGuardrail re-runs the PRD-completion "rules-review"
// judgment for a PRDBlocked PRD whose work is fully done, on an
// operator's behalf (actor is recorded as the "guardrail_rejudge"
// Decision's Actor, not hardcoded to "autonomous"). It refuses — with a
// clear error, and no side effects on any task (never a ResetTask, never
// a status change) — when the PRD does not exist, is not PRDBlocked, or
// has any task not at TaskCompleted; the last case names the first
// offending task's id and status.
//
// Outcome handling, in order:
//  1. The verdict is recorded durably FIRST, via
//     recordGuardrailVerdict (guardrail_verdict Decision + save +
//     judge-output cleanup), so it is in the append-only log before
//     anything else changes — exactly the order
//     runPRDCompletionRulesReview uses.
//  2. A "guardrail_rejudge" Decision (Actor = actor) names the outcome,
//     carrying the same verdict detail (outcome, category, summary,
//     judge session or backend) the guardrail_verdict Decision does.
//  3. Pass (Outcome != "block") — the verdict is appended to
//     prd.Verdicts and Status is set to PRDCompleted with BlockedReason
//     cleared in ONE store commit, through CommitPRDIfRev with the same
//     re-checks commitRollupStatus uses (still PRDBlocked, every task
//     still terminal, under the store lock), then emitPRDStatus fires
//     with the "guardrail_rejudge" Decision attached.
//  4. Block — Status stays PRDBlocked, BlockedReason is set with
//     blockedReasonFor(v), and the event carries the last block verdict.
//  5. No verdict (the judge errored, or returned an empty Outcome) — a
//     "guardrail_rejudge" Decision notes that no verdict was produced,
//     the PRD keeps its PRDBlocked status and its previous
//     BlockedReason, and an error is returned so the operator sees the
//     re-judge failed.
func (m *Manager) RejudgeCompletionGuardrail(ctx context.Context, prdID, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("rejudge completion guardrail: prd %q not found", prdID)
	}
	if prd.Status != PRDBlocked {
		return nil, fmt.Errorf("rejudge completion guardrail: prd %s is %s, not blocked; rejudgment only applies to a PRDBlocked PRD", prd.ID, prd.Status)
	}
	if storyID, taskID, status := firstNonCompletedTask(prd); taskID != "" {
		return nil, fmt.Errorf("rejudge completion guardrail: prd %s task %s (story %s) is %s, not completed; every task must be completed before the completion guardrail can be re-run", prd.ID, taskID, storyID, status)
	}

	// No-verdict path (the judge errored, or returned an empty outcome):
	// record a "guardrail_rejudge" Decision stating no verdict was
	// produced, keep the PRD blocked with its previous BlockedReason
	// untouched, and return an error so the operator sees the re-judge
	// did not yield a judgment.
	recordNoVerdict := func(detail string) error {
		detail = "no verdict was produced: " + detail
		prd.Decisions = append(prd.Decisions, Decision{
			At:    time.Now(),
			Kind:  "guardrail_rejudge",
			Actor: actor,
			Note:  fmt.Sprintf("rejudge of the rules-review completion guardrail for prd %s: %s", prd.ID, detail),
		})
		if serr := m.store.SavePRD(prd); serr != nil {
			return fmt.Errorf("rejudge completion guardrail: prd %s: record no-verdict decision: %w", prd.ID, serr)
		}
		return fmt.Errorf("rejudge completion guardrail: prd %s: %s", prd.ID, detail)
	}

	v, err := m.judgeCompletionRulesReview(ctx, prd)
	if err != nil {
		return nil, recordNoVerdict(fmt.Sprintf("the guardrail judgment failed: %v", err))
	}
	if v.Outcome == "" {
		return nil, recordNoVerdict("the guardrail judgment returned an empty outcome")
	}
	if v.VerdictAt.IsZero() {
		v.VerdictAt = time.Now()
	}
	v.Guardrail = "rules-review"

	// Record the verdict durably BEFORE anything else changes — the same
	// guardrail_verdict Decision + save + judge-output cleanup
	// runPRDCompletionRulesReview uses.
	if err := m.recordGuardrailVerdict(prd, prd.ID, v); err != nil {
		return nil, fmt.Errorf("rejudge completion guardrail: prd %s: record verdict: %w", prd.ID, err)
	}

	// The "guardrail_rejudge" Decision naming the re-judge outcome,
	// attributed to the acting operator.
	rej := Decision{
		At:              time.Now(),
		Kind:            "guardrail_rejudge",
		Actor:           actor,
		VerdictOutcome:  v.Outcome,
		FailureCategory: v.Category,
		Note:            fmt.Sprintf("rejudge of the rules-review completion guardrail: %s", v.Outcome),
		JudgeSessionID:  v.JudgeSessionID,
	}
	if v.JudgeSessionID == "" {
		rej.Backend = v.Backend
	}
	if v.Summary != "" {
		rej.Note += " - " + v.Summary
	}

	if v.Outcome != "block" {
		// Pass — verdict + "guardrail_rejudge" Decision + status +
		// cleared reason in ONE conditional commit, the same
		// CommitPRDIfRev pattern commitRollupStatus uses: only if nobody
		// rewrote the PRD since this pass observed it and every task is
		// still terminal (re-checked under the store lock).
		rev, ok := m.store.PRDRev(prd.ID)
		if !ok {
			return nil, fmt.Errorf("rejudge completion guardrail: prd %q not found", prdID)
		}
		if cerr := m.store.CommitPRDIfRev(prd.ID, rev, func(p *PRD) bool {
			if p.Status != PRDBlocked || !prdAllTasksTerminal(p) {
				return false
			}
			p.Verdicts = append(p.Verdicts, v)
			p.Decisions = append(p.Decisions, rej)
			p.Status = PRDCompleted
			p.BlockedReason = ""
			return true
		}); cerr != nil {
			return nil, fmt.Errorf("rejudge completion guardrail: prd %s: commit PRDCompleted: %w", prd.ID, cerr)
		}
		old := PRDBlocked
		prd.Verdicts = append(prd.Verdicts, v)
		prd.Decisions = append(prd.Decisions, rej)
		prd.Status = PRDCompleted
		prd.BlockedReason = ""
		m.emitPRDStatus(prd, old, lastDecision(prd, "guardrail_rejudge"), nil)
		m.recordProgressMilestone(prd.ID)
		return prd, nil
	}

	// Block — the same PRDBlocked signal the completion rollup uses: the
	// verdict goes on prd.Verdicts and the reason on BlockedReason in
	// the one save (Status is already PRDBlocked, so SavePRD's
	// clearStaleBlockedReason leaves the new reason in place).
	old := prd.Status
	prd.Verdicts = append(prd.Verdicts, v)
	prd.Decisions = append(prd.Decisions, rej)
	prd.Status = PRDBlocked
	prd.BlockedReason = blockedReasonFor(v)
	if serr := m.store.SavePRD(prd); serr != nil {
		return nil, fmt.Errorf("rejudge completion guardrail: prd %s: record block outcome: %w", prd.ID, serr)
	}
	if bl := lastBlockVerdict(prd.Verdicts); bl != nil {
		m.emitPRDStatus(prd, old, lastDecision(prd, "guardrail_rejudge"), bl)
	}
	return prd, nil
}
