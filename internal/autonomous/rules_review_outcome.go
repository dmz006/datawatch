// Rules review outcome — a durable, structured record of what the
// completion rules review of an Automaton found and which recovery
// happened, so every surface can show it instead of parsing the
// free-text BlockedReason.
package autonomous

import "time"

// Recovery actions recorded in RulesReviewOutcome.Action.
const (
	RulesReviewActionFixTaskCreated  = "fix_task_created"
	RulesReviewActionFixTaskFailed   = "fix_task_failed"
	RulesReviewActionRetriesSpent    = "retries_spent"
	RulesReviewActionAutoFixDisabled = "auto_fix_disabled"
)

// newRulesReviewOutcome builds the record from a verdict. Findings are the
// verdict's Issues, or the Summary as a single finding when there are no
// issues; Findings is never nil.
func newRulesReviewOutcome(v GuardrailVerdict, action, reason string, attempt, budget, spent int, at time.Time) *RulesReviewOutcome {
	findings := make([]string, 0, len(v.Issues)+1)
	if len(v.Issues) > 0 {
		findings = append(findings, v.Issues...)
	} else if v.Summary != "" {
		findings = append(findings, v.Summary)
	}
	return &RulesReviewOutcome{
		Action:       action,
		Findings:     findings,
		Summary:      v.Summary,
		Attempt:      attempt,
		RetryBudget:  budget,
		RetriesSpent: spent,
		Reason:       reason,
		JudgeOutcome: judgeOutcomeKind(v),
		At:           at,
	}
}

// judgeOutcomeKind maps a verdict to how the judge ended: delivery failure to
// judge_could_not_start, the time limit to judge_timed_out, a block with a
// problem to judge_found_problem, and anything else (no judge ran) to "".
func judgeOutcomeKind(v GuardrailVerdict) string {
	switch {
	case judgeCouldNotStart(v, nil):
		return JudgeKindCouldNotStart
	case IsJudgeTimeoutHold(v):
		return JudgeKindTimedOut
	case v.Outcome == "block" && v.Category != "infra" && v.Category != "invalid-response":
		// Any other block (content, policy, test, scope, verification, ...)
		// is a judge that ran and found a problem.
		return JudgeKindFoundProblem
	}
	return ""
}

// reviewNow is the time source for rules review records: the injected
// attempt clock when set, else the wall clock.
func (m *Manager) reviewNow() time.Time { return m.attemptNow() }
