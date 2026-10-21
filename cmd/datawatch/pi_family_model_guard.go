// pi-family model pinning guards for the daemon's session-routed
// judges (v9.10.19).
//
// pi and pi-prompt launch the pi.dev harness with an explicit --model
// flag naming the model the session must run on. When a session
// reaches a pi-family backend with an empty model, the harness
// silently uses that host's own default model — on a federated peer
// that is the operator-local node model the session exists to
// replace. Both of the daemon's LLM-judgment session paths (the
// autonomousGuardrail closure and the autonomous verify closure in
// main.go) therefore resolve an explicit, non-empty model before
// spawning the session, or fail loudly naming the configuration keys
// an operator should set — never fall back to the backend's built-in
// default. Non-pi session backends keep their existing behavior.
package main

import (
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// guardPiFamilySessionModel resolves the session model for a
// guardrail judgment that will run in a one-shot session (B124 path)
// and, for pi-family session kinds, enforces the v9.10.19 pinning
// invariant.
//
// Non-pi kinds (the gmodel fallback still applies to them):
// a genuine per-unit override (unitModel) carries through; otherwise
// the session backend's own registry-default model (nonPiFallback).
// An ask-compatible model name that belongs to a different backend is
// rejected by the session harness outright (found live: a real
// claude-code session hung on an Ollama model name), so it must not
// leak in here either — only the override or the backend's own
// default.
//
// pi / pi-prompt kinds: the model is resolved with
// autonomous.ResolveGuardrailModel — the invocation's
// pre-resolved model (the manager's resolveUnitGuardrailModel
// contract) first, then the documented global tiers
// (autonomous.guardrail_model → autonomous.verification_model →
// autonomous.planning_model) — and the result is always an explicit,
// non-empty model. When no tier resolves, a loud, non-retried
// infra-category block verdict (NoGuardrailModelVerdict) is
// returned instead: the judge never ran, so this is a configuration
// failure, not a finding about the artifact under review. The caller
// returns the verdict as-is.
func guardPiFamilySessionModel(
	gkind, gbackend, guardrailName, unitModel,
	cfgGuardrail, cfgVerification, cfgPlanning, nonPiFallback string,
) (model string, verdict *autonomouspkg.GuardrailVerdict) {
	if !isPiFamilyKind(gkind) {
		if unitModel != "" {
			return unitModel, nil
		}
		return nonPiFallback, nil
	}
	pinned, perr := autonomouspkg.ResolveGuardrailModel(
		gbackend,
		unitModel,
		"", // task tier: the manager-side caller already folded the task's pin into the invocation
		cfgGuardrail, cfgVerification, cfgPlanning,
	)
	if perr != nil {
		v := autonomouspkg.NoGuardrailModelVerdict(guardrailName, perr)
		return "", &v
	}
	return pinned, nil
}

// guardPiFamilyVerifyModel resolves the session model for a pi-family
// verify judgment (B124 path) that still has no model after
// selectVerifierLLM applied the documented chain (task → story → PRD
// → autonomous.verification_model): it takes the last documented tier
// (autonomous.planning_model), validated as a pair that the session
// backend can serve, and — when even that is unset or unservable —
// returns a loud "setup" verdict naming the configuration keys.
// "setup" is the same verdict class as a missing verifier script:
// the judge never ran, so the block stops the task without spending
// the auto-fix retry budget (retrying cannot materialize a model
// that is not configured). The caller returns the result as-is.
func guardPiFamilyVerifyModel(vbackend, cfgVerification, cfgPlanning string) (model string, setup *autonomouspkg.VerificationResult) {
	pinned, perr := autonomouspkg.ResolvePinnedModel(vbackend,
		autonomouspkg.ModelTier{Key: "autonomous.verification_model", Model: cfgVerification},
		autonomouspkg.ModelTier{Key: "autonomous.planning_model", Model: cfgPlanning},
	)
	if perr != nil {
		return "", &autonomouspkg.VerificationResult{
			OK:       false,
			Severity: "high",
			Summary:  "verifier: judge never ran — no pinned model for the pi-family verify session (" + perr.Error() + ")",
			Issues: []string{
				"configuration failure, not a finding about the task",
				"set one of: task verification_model, story verification_model, PRD model, autonomous.verification_model, autonomous.planning_model",
			},
			VerifiedAt: time.Now(),
			Category:   "setup",
		}
	}
	return pinned, nil
}
