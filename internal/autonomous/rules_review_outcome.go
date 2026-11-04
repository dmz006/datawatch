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

// RulesReviewOutcome records one blocked completion rules review.
type RulesReviewOutcome struct {
	Action       string    `json:"action"`
	Findings     []string  `json:"findings"`
	Summary      string    `json:"summary"`
	Attempt      int       `json:"attempt"`
	RetryBudget  int       `json:"retry_budget"`
	RetriesSpent int       `json:"retries_spent"`
	Reason       string    `json:"reason,omitempty"`
	At           time.Time `json:"at"`
}

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
		At:           at,
	}
}

// reviewNow is the time source for rules review records: the injected
// attempt clock when set, else the wall clock.
func (m *Manager) reviewNow() time.Time { return m.attemptNow() }
