package autonomous

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// maxVerifierIdleRetries is the fixed cap on how many times an idle verifier
// is retried on a fresh verifier session for one verification. When the cap
// is used up the task takes the existing hold path (review needed).
const maxVerifierIdleRetries = 3

// verifierIdleHoldSource is the FailureReason source for a task held because
// its verifier kept going idle.
const verifierIdleHoldSource = "verifier-idle-hold"

// verifyRetryingIdle runs verify and, when it fails with a VerifierIdleError,
// treats the failure as the verifier's own incident: it records the verifier
// incident and runs verify again (a fresh verifier session each call), up to
// maxVerifierIdleRetries times. An idle verifier never touches the worker's
// attempt budget, Task.Attempts or Task.EscalationFailures. When the cap is
// used up the task is parked for operator review and held is true. Any
// other result (including a non-idle error) is returned unchanged. When held is true,
// err is the error from parking the task, not a verify error.
func (m *Manager) verifyRetryingIdle(ctx context.Context, prd *PRD, t *Task, verify VerifyFn) (vr VerificationResult, held bool, err error) {
	for retries := 0; ; retries++ {
		vr, err = verify(ctx, prd, t)
		if err == nil || ctx.Err() != nil || !IsVerifierIdleError(err) {
			return vr, false, err
		}
		backend, model := t.VerificationBackend, t.VerificationModel
		if rerr := m.RecordVerifierIncident(prd.ID, t.ID, backend, model, 0); rerr != nil {
			log.Printf("[autonomous] prd=%s task=%s: record verifier incident: %v", prd.ID, t.ID, rerr)
		}
		if retries >= maxVerifierIdleRetries {
			return vr, true, m.holdForVerifierIdle(prd, t, err, retries+1)
		}
		log.Printf("[autonomous] prd=%s task=%s: verifier idle (%v), retry %d of %d on a fresh verifier",
			prd.ID, t.ID, err, retries+1, maxVerifierIdleRetries)
	}
}

// holdForVerifierIdle parks t for operator review after its verifier went
// idle on every try. It records no failed attempt and adds no escalation
// failure: the worker's result was never judged.
func (m *Manager) holdForVerifierIdle(prd *PRD, t *Task, cause error, idles int) error {
	_, perr := m.parkTaskForReview(prd, t, reviewParking{
		reason: FailureReason{
			Category: "infra", Source: verifierIdleHoldSource,
			Detail: boundInfraReason(cause.Error()),
		},
		describe: func(t *Task) (string, string) {
			return fmt.Sprintf("held for operator review: the verifier went idle %d times", idles),
				fmt.Sprintf("task %s held for operator review: the verifier went idle %d times; no worker failure was counted", t.ID, idles)
		},
		report: func(t *Task, now time.Time) *ReviewSummary {
			var b strings.Builder
			fmt.Fprintf(&b, "The Automaton task %s (%s) was held because its verifier went idle %d times.\n", t.ID, t.Title, idles)
			fmt.Fprintf(&b, "Cause: %s\nThe worker's result was never judged, and no worker failure was counted.\n\n", cause)
			return finishReviewSummary(&b, t, now)
		},
	})
	return perr
}
