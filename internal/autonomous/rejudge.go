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
// runPRDCompletionRulesReview uses. Recording the outcome is a separate
// concern; judgeCompletionRulesReview returns the raw verdict so the
// next step can record it.
package autonomous

import (
	"context"
	"fmt"
	"log"
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
	return m.runGuardrailWithInfraRetry(ctx, prd, m.prdRulesReviewInvocation(prd), fn)
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
// operator's behalf (actor is logged with the judgment). It refuses —
// with a clear error, and no side effects on any task (never a
// ResetTask, never a status change) — when the PRD does not exist, is
// not PRDBlocked, or has any task not at TaskCompleted; the last case
// names the first offending task's id and status. After judging it
// returns the unchanged PRD; recording the verdict's outcome is the
// next step's job (see judgeCompletionRulesReview's doc comment).
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
	v, err := m.judgeCompletionRulesReview(ctx, prd)
	if err != nil {
		return nil, err
	}
	// Outcome handling (verdict recording, unblock on a clean pass) is a
	// follow-up step; for now the judgment has run and the PRD is
	// returned unchanged with the verdict result in the log.
	log.Printf("autonomous: rejudge completion guardrail prd=%s actor=%s: rules-review verdict %s - %s", prd.ID, actor, v.Outcome, v.Summary)
	return prd, nil
}
