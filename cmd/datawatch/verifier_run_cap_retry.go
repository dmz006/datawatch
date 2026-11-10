package main

import (
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
	retried := false
	for {
		content, err := run(backend, model)
		if !autonomouspkg.IsVerifierRunCapEnd(err) {
			return content, err
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

// verifierSessionInfraResult is the verdict for a verifier session that failed
// to produce output. It is infra, never a content failure, so the caller's
// infra accounting and retry path apply.
func verifierSessionInfraResult(err error) autonomouspkg.VerificationResult {
	return autonomouspkg.VerificationResult{
		OK:         false,
		Severity:   "medium",
		Summary:    "verifier(session): " + err.Error(),
		VerifiedAt: time.Now(),
		Category:   "infra",
	}
}
