// No-retry failure: what happens when a task's failed attempt cannot plausibly
// be fixed by another attempt, so the task is sent straight to operator review
// without spending more of its retry budget.
//
// A no-retry failure is detected by noRetryReasonWithPRD (see
// no_retry_reason.go). The failed attempt is still recorded, carrying the
// operator-readable reason on its TaskAttempt.NoRetryReason, and the task is
// parked in TaskReviewNeeded the same way any held task is — so it is resolved
// the same way, through ResolveTaskReview. Unlike a retry-limit hold, the
// reason (what the operator should change) is the point.
package autonomous

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// noRetryHoldSource is the FailureReason.Source of a task held for a no-retry
// failure.
const noRetryHoldSource = "no-retry-hold"

// priorFailureSummaries returns the task's recorded attempt failure summaries in
// chronological order (oldest first). It reads t under the store lock. The
// current, not-yet-recorded attempt is not included, so the list is exactly the
// failures before the one being considered for a no-retry hold.
func (m *Manager) priorFailureSummaries(t *Task) []string {
	var out []string
	m.store.WithLock(func() {
		for _, a := range t.Attempts {
			out = append(out, a.FailureSummary)
		}
	})
	return out
}

// noRetryFailHold reports whether the current verifier failure of task t, in
// Automaton prd, has no route to a fix through another attempt, and returns the
// operator-readable reason. It reads the task's recorded failures and, for the
// missing-script case, resolves the task's project directory with the PRD in
// hand (see noRetryReasonWithPRD).
func (m *Manager) noRetryFailHold(prd *PRD, t *Task, v VerificationResult) (string, bool) {
	return noRetryReasonWithPRD(prd, t, v, m.priorFailureSummaries(t))
}

// holdForNoRetry parks t in TaskReviewNeeded because its last failed attempt has
// no route to a fix, so no further attempt is scheduled. The failed attempt is
// recorded carrying the reason on NoRetryReason. The task is given a
// FailureReason (Source no-retry-hold, Retryable false) whose Detail is the
// reason — that text is the review question the operator answers — a
// ReviewSummary that leads with the reason and lists the attempt, a
// review_needed decision, and a learning. Then one alert is raised. It returns
// the error from saving the task; the other side effects are best effort.
func (m *Manager) holdForNoRetry(ctx context.Context, prd *PRD, t *Task, run attemptRun, category, summary, reason string) error {
	if reason == "" {
		reason = "no retry: another attempt cannot change the outcome"
	}
	m.recordFailedAttempt(ctx, prd, t, run, attemptFailure{
		Outcome:       attemptOutcomeFailed,
		Category:      category,
		Summary:       summary,
		Verification:  verificationSummaryOnly(summary),
		NoRetryReason: reason,
	})
	last, perr := m.parkTaskForReview(prd, t, reviewParking{
		reason: FailureReason{
			Category: orContentCategory(category), Source: noRetryHoldSource, Detail: reason,
		},
		describe: func(t *Task) (string, string) {
			return fmt.Sprintf("held for operator review: no retry — %s", reason),
				fmt.Sprintf("task %s held for operator review without another attempt: %s", t.ID, reason)
		},
		report: func(t *Task, now time.Time) *ReviewSummary {
			return buildNoRetryReviewSummary(t, now, reason)
		},
	})
	if perr != nil {
		return perr
	}
	m.raiseNoRetryAlert(prd, t, reason, last)
	return nil
}

// verificationSummaryOnly builds a VerificationResult that carries only the
// failure text, for the recorded attempt — the issues are not repeated here,
// they are already in the alert and the review prompt.
func verificationSummaryOnly(summary string) VerificationResult {
	return VerificationResult{OK: false, Summary: summary}
}

// buildNoRetryReviewSummary renders the operator's report for a task held for a
// no-retry failure: the reason, then every recorded attempt (whose last one
// carries the no-retry line). It reads t without locking, so callers that share
// t with running goroutines call it under the store lock.
func buildNoRetryReviewSummary(t *Task, now time.Time, reason string) *ReviewSummary {
	var b strings.Builder
	fmt.Fprintf(&b, "Task %s (%s) is held for operator review without another attempt: %s.\n\n",
		t.ID, t.Title, reason)
	return finishReviewSummary(&b, t, now)
}

// raiseNoRetryAlert sends the one operator alert for a task held for a no-retry
// failure, through the same sink raiseReviewAlert uses. It names the reason —
// what the operator should change. Nothing is sent when no alert sink is wired.
func (m *Manager) raiseNoRetryAlert(prd *PRD, t *Task, reason string, last *TaskAttempt) {
	m.mu.Lock()
	alert := m.alertSink
	m.mu.Unlock()
	if alert == nil {
		return
	}
	worker := "none recorded"
	if last != nil {
		worker = escalationWorkerLabel(*last)
	}
	body := fmt.Sprintf(
		"Automaton: %s (%s)\nTask: %s (%s)\n"+
			"No retry — %s\nLast worker: %s\n"+
			"Ways to act: change the task so it can be verified (give it files to write, a verification script, or a changeable spec), or cancel it.",
		prd.ID, prd.Title, t.ID, t.Title, reason, worker)
	alert("error", "Automaton task needs review (no retry)", body)
}
