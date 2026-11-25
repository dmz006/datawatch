// Verifier faults: a verifier that could not answer, or whose reply could not
// be read, has said nothing about the worker's result.
//
// Only the verifier runs again, on the infrastructure retry budget and back-off
// (autonomous.infra_retry.max_tries and backoff_seconds). The worker is not
// launched again and no failure is counted. When the tries are used up the task
// is held for operator review with the worker's commit kept, one alert and,
// when the operator question channel is on, one question. This holds for either
// exhausted_action: booking the failure against the task would run the worker
// again. With infra_retry.enabled off the first fault holds the task.
//
// The hold has its own FailureReason source, so the supervisor loop's automatic
// retry of infrastructure holds, which runs the worker, never picks it up.
package autonomous

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

const (
	// InfraClassVerifierFault is the InfraRetryRecord class of a verifier-only
	// retry. It is not one of infra_retry.categories: it cannot be switched
	// into a worker re-run.
	InfraClassVerifierFault = "verifier_fault"
	// verifierFaultHoldSource is the FailureReason.Source of a task held after
	// its verifier-only retries ran out.
	verifierFaultHoldSource = "verifier-fault-hold"
	// verdictCategoryInvalidResponse is the category of a verifier reply that
	// could not be read as a verdict.
	verdictCategoryInvalidResponse = "invalid-response"
	// questionDefaultHold is a question default that picks no option.
	questionDefaultHold = "hold"
)

// isVerifierFault reports whether vr is a fault of the verifier rather than a
// finding about the work. A result marked as the worker's fault never is. A
// result the verify path marked as its own always is, whatever its text says.
// Otherwise an unreadable reply is one, and so is an infra result unless its
// text names a machinery failure the infrastructure retry already handles.
func isVerifierFault(vr VerificationResult) bool {
	switch {
	case vr.OK || vr.WorkerFault:
		return false
	case vr.VerifierFault:
		return true
	case vr.Category == verdictCategoryInvalidResponse:
		return true
	}
	return vr.Category == infraRetryFailureCategory && !isInfraText(vr.Summary)
}

// verifyRetryingFaults verifies t, running only the verification again while
// its result is a verifier fault and tries remain. held is true when the caller
// must stop working on the task: it was parked for review (err is the error
// from parking it) or the context ended during a back-off (err is nil).
func (m *Manager) verifyRetryingFaults(ctx context.Context, prd *PRD, t *Task, run attemptRun, verify VerifyFn) (vr VerificationResult, held bool, err error) {
	for {
		vr, held, err = m.verifyRetryingIdle(ctx, prd, t, verify)
		if held || err != nil || ctx.Err() != nil || !isVerifierFault(vr) {
			return vr, held, err
		}
		cfg := m.infraRetryConfig()
		maxTries := cfg.EffectiveMaxTries()
		if !cfg.EffectiveEnabled() {
			maxTries = 0
		}
		var used int
		m.store.WithLock(func() { used = t.VerifierFaultRetries })
		cause := boundInfraReason(vr.Summary)
		if used >= maxTries {
			return vr, true, m.holdAfterVerifierFaults(ctx, prd, t, run, vr, cause, used)
		}
		plan := infraRetryPlan{
			Class: InfraClassVerifierFault, Reason: cause,
			Number: used + 1, MaxTries: maxTries, Delay: infraBackoffDelay(cfg, used+1),
		}
		log.Printf("[autonomous] prd=%s task=%s: verifier fault, verifier-only retry %d of %d in %s: %s",
			prd.ID, t.ID, plan.Number, plan.MaxTries, plan.Delay, cause)
		// No verdict is recorded: a restart during the back-off then verifies
		// again instead of running the worker (see ResolveVerifyingResume).
		m.store.WithLock(func() { t.Verification = nil; t.Error = cause })
		m.recordInfraRetry(prd, t, plan)
		if m.waitInfraRetry(ctx, prd, t, plan) != nil {
			return vr, true, nil
		}
		m.store.WithLock(func() { t.Error = "" })
		m.noteVerifyStart(t.ID)
	}
}

// holdAfterVerifierFaults parks t for operator review after its verifier
// faulted on every try. The attempt is recorded without counting as a failure,
// and the worker's session and commits are left as they are.
func (m *Manager) holdAfterVerifierFaults(ctx context.Context, prd *PRD, t *Task, run attemptRun, vr VerificationResult, cause string, retries int) error {
	m.recordFailedAttempt(ctx, prd, t, run, attemptFailure{
		Outcome: attemptOutcomeFailed, Category: infraRetryFailureCategory, Summary: cause,
		Verification: vr, Requeue: true,
	})
	last := vr
	_, perr := m.parkTaskForReview(prd, t, reviewParking{
		reason: FailureReason{
			Category: infraRetryFailureCategory, Source: verifierFaultHoldSource,
			Detail: boundInfraReason(InfraClassVerifierFault + ": " + cause),
		},
		describe: func(t *Task) (string, string) {
			t.Verification = &last
			return fmt.Sprintf("held for operator review: the verifier could not give a verdict (%d verifier-only retries)", retries),
				fmt.Sprintf("task %s held for operator review: the verifier could not give a verdict after %d verifier-only retries; no worker failure was counted", t.ID, retries)
		},
		report: func(t *Task, now time.Time) *ReviewSummary {
			var b strings.Builder
			fmt.Fprintf(&b, "The Automaton task %s (%s) was held because its verifier could not give a verdict, after %d verifier-only retries.\n", t.ID, t.Title, retries)
			fmt.Fprintf(&b, "Infrastructure cause: %s\n", cause)
			b.WriteString("The worker's result was never judged. Its commit is kept, the worker was not run again, and no failure was counted.\n\n")
			for _, r := range t.InfraRetries {
				if r.Class == InfraClassVerifierFault {
					fmt.Fprintf(&b, "- verifier retry %d at %s, waited %d s: %s\n", r.Number, r.At.Format("15:04:05"), r.DelaySeconds, r.Reason)
				}
			}
			b.WriteString("\n")
			return finishReviewSummary(&b, t, now)
		},
	})
	if perr != nil {
		return perr
	}
	m.raiseVerifierFaultAlert(prd, t, cause, retries)
	m.raiseVerifierFaultQuestion(prd, t, cause, retries)
	return nil
}

func (m *Manager) raiseVerifierFaultAlert(prd *PRD, t *Task, cause string, retries int) {
	m.mu.Lock()
	alert := m.alertSink
	m.mu.Unlock()
	if alert == nil {
		return
	}
	alert("error", "Automaton task held: no verdict from the verifier", fmt.Sprintf(
		"Automaton: %s (%s)\nTask: %s (%s)\nCause: %s\nVerifier-only retries used: %d\n"+
			"The worker's commit is kept and was never judged. Ways to act: accept the result after checking it, "+
			"retry the task (this runs the worker again), or cancel it.",
		prd.ID, prd.Title, t.ID, t.Title, cause, retries))
}

// raiseVerifierFaultQuestion asks the operator about a held task. It shares the
// infrastructure hold question's asker, so an answer resolves the task the same
// way. Its default is "hold": nothing runs without an answer.
func (m *Manager) raiseVerifierFaultQuestion(prd *PRD, t *Task, cause string, retries int) {
	if !m.infraRetryConfig().EffectiveRaiseQuestion() {
		return
	}
	store, enabled := m.operatorQuestionChannel()
	if !enabled {
		return
	}
	cause = strings.TrimRight(strings.Join(strings.Fields(cause), " "), ".")
	q := Question{
		Label:     infraHoldQuestionLabel,
		AskedBy:   infraHoldQuestionAsker,
		AskerKind: QuestionAskerSystem,
		Context:   QuestionContextTask,
		PRDID:     prd.ID,
		TaskID:    t.ID,
		Text: fmt.Sprintf("Task %s in Automaton %s is held: the verifier could not give a verdict after %d verifier-only retries: %s. The worker's commit is kept and was never judged. Retry the task or cancel it?",
			t.Title, automatonLabel(prd), retries, cause),
		Options: []QuestionOption{
			{ID: reviewActionRetry, Label: "Retry the task",
				Tradeoffs: "Runs the worker again, then verifies. Fix the verifier first, or the task is held again. To keep the commit without a new run, check it and accept the held task instead."},
			{ID: reviewActionCancel, Label: "Cancel the task",
				Tradeoffs: "Cancels the task; tasks that depend on it do not run."},
		},
		Default:          questionDefaultHold,
		Priority:         QuestionPriorityHigh,
		Blocking:         true,
		SourceLineSHA256: infraHoldQuestionKey(prd.ID, t.ID, InfraClassVerifierFault, retries),
	}
	if _, _, err := store.RaiseUnlessOpenSourceLine(q); err != nil {
		log.Printf("[autonomous] prd=%s task=%s: raise verifier hold question: %v", prd.ID, t.ID, err)
	}
}
