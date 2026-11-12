package main

import (
	"errors"
	"log"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// runVerifierSessionWithRunCapRetry runs one verifier session on backend. When
// the session ends at the run cap, it runs once more on the next backend in
// order (see autonomouspkg.NextVerifierRunCapBackend). modelFor supplies the
// model for that retry backend; ok=false means no retry can be set up. The
// second cap end, or any other error, is returned unchanged so the caller
// records it as verifier infra.
func runVerifierSessionWithRunCapRetry(order []string, backend, model string,
	run func(backend, model string) (string, error),
	modelFor func(backend string) (string, bool)) (string, error) {
	return runVerifierSessionWithRunCapHook(order, backend, model, run, modelFor, nil)
}

// runVerifierSessionWithRunCapHook is runVerifierSessionWithRunCapRetry plus
// onCap, which runs on every run-cap end before the retry decision. The caller
// uses it to release the verifier's capacity lease and record the decision.
// onCap may be nil.
func runVerifierSessionWithRunCapHook(order []string, backend, model string,
	run func(backend, model string) (string, error),
	modelFor func(backend string) (string, bool),
	onCap func(capEnd *autonomouspkg.OneShotNoOutputError)) (string, error) {
	retried := false
	for {
		content, err := run(backend, model)
		if !autonomouspkg.IsVerifierRunCapEnd(err) {
			return content, err
		}
		if onCap != nil {
			var capEnd *autonomouspkg.OneShotNoOutputError
			if errors.As(err, &capEnd) {
				onCap(capEnd)
			}
		}
		next, ok := autonomouspkg.NextVerifierRunCapBackend(order, backend, retried)
		if !ok {
			return content, err
		}
		nextModel, ok := modelFor(next)
		if !ok {
			return content, err
		}
		log.Printf("[autonomous] verifier: run cap ended the session on %s; retrying once on %s", backend, next)
		retried = true
		backend, model = next, nextModel
	}
}

// verifierRunCapDecision is the onCap hook body: it releases the lease held
// under the task's verify holder, then writes the decision and audit record.
// A failure to record is logged and never changes the verification outcome.
func verifierRunCapDecision(mgr *autonomouspkg.Manager, prdID, taskID string, capEnd *autonomouspkg.OneShotNoOutputError) {
	if mgr == nil {
		return
	}
	if _, err := mgr.HandleVerifierRunCap(prdID, taskID, capEnd.RunCapElapsed, capEnd.RunCapSeconds); err != nil {
		log.Printf("[autonomous] verifier: HandleVerifierRunCap(%s/%s): %v", prdID, taskID, err)
	}
}

// verifierStatsCategory is the stats category for one verifier run. runCapEnded
// is true when the run cap ended a session on any attempt of the run. A run
// that failed with a session error and is not a cap end or a content verdict
// is counted as infra.
func verifierStatsCategory(ok bool, resultCategory, summary string, escalated bool, verr error, runCapEnded bool) string {
	cat := autonomouspkg.VerifierCategoryForRunCap(ok, resultCategory, summary, escalated, runCapEnded)
	if verr != nil && cat == autonomouspkg.VerifierCatContent {
		cat = autonomouspkg.VerifierCatInfra
	}
	return cat
}

// verifierSessionInfraResult is the verdict for a verifier session that failed
// to produce output. It is infra, never a content failure, so the caller's
// infra accounting and retry path apply.
func verifierSessionInfraResult(err error) autonomouspkg.VerificationResult {
	res := autonomouspkg.VerificationResult{
		OK:         false,
		Severity:   "medium",
		Summary:    "verifier(session): " + err.Error(),
		VerifiedAt: time.Now(),
		Category:   "infra",
	}
	if autonomouspkg.IsVerifierRunCapEnd(err) {
		res.EndReason = autonomouspkg.VerifierMaxRunEndReason
	}
	return res
}
