package autonomous

import "strings"

// attemptCategoryFor picks the failure category recorded on a TaskAttempt.
// A non-empty existing category is kept. Otherwise the category comes from the
// end reason, then the failure text, using the existing vocabulary (none,
// infra, security, policy, code_or_test, judging, hung, invalid-response).
// It returns "" only when there is no end reason, no summary and no verifier
// issue. outcome is accepted for symmetry with the attempt record; the end
// reason carries the same information.
func attemptCategoryFor(endReason, outcome, existingCategory, summary, verifierSummary string, verifierIssues []string) string {
	if existingCategory != "" {
		return existingCategory
	}
	text := strings.ToLower(summary + " " + verifierSummary)
	hasText := strings.TrimSpace(summary) != "" || strings.TrimSpace(verifierSummary) != "" || len(verifierIssues) > 0
	if endReason == "" && !hasText {
		return ""
	}
	if endReason == attemptEndCancelledByOperator || (endReason == "" && outcome == attemptOutcomeCancelled) {
		return "none"
	}
	if strings.Contains(text, "merge refused") || strings.Contains(text, "commit failed") {
		return "policy"
	}
	switch endReason {
	case attemptEndVerifierFailed:
		return "code_or_test"
	case attemptEndStalled:
		return "hung"
	case attemptEndSessionDied, attemptEndLaunchRefused, attemptEndAdoptedThenFailed, attemptEndDaemonRestart:
		return "infra"
	case attemptEndExecutorError:
		if strings.Contains(text, "timeout") || strings.Contains(text, "timed out") || strings.Contains(text, "no output") {
			return "infra"
		}
	}
	if hasText {
		return "code_or_test"
	}
	return "none"
}
