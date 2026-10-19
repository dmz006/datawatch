// BL410 Phase 0 — autonomousVerify and autonomousGuardrail live inside
// main() and are not unit-testable directly (same constraint
// bl369_prompt_security_test.go / session_judgment_wiring_test.go
// document); source inspection confirms each infra-vs-content
// collapse point: genuine no-output failures (session error, ask call
// failure) keep Category "infra" so the bounded infra retry can fire,
// and a successfully-parsed real judgment is never left looking like
// an infra failure just because the LLM's own JSON never mentions
// "category".
//
// 2026-10-19 (bbf70001 double-consumption / BL478 class) — a WRITTEN
// but unparseable judge response is no longer stamped "infra": that
// would make runGuardrailWithInfraRetry re-run the WHOLE judge, and
// the retry re-delivers the same already-relocated output file, so
// the response is consumed twice or not at all. The unparseable
// branches are now a terminal, non-retryable "invalid-response"
// verdict naming the real parse error, with the truncated raw output
// preserved in Issues — "invalid-response" is non-empty (so it is not
// a content-category block eligible for content auto-fix) and not
// "infra" (so it is not infra-retried).
package main

import (
	"fmt"
	"testing"
)

// contains, readMainGo are provided by bl369_prompt_security_test.go.

func TestJudgedGuardrailCategory_PessimisticInfraSelfReport(t *testing.T) {
	got := judgedGuardrailCategory("infra")
	if got != "" {
		t.Fatalf("judgedGuardrailCategory(\"infra\") = %q; want \"\" (a self-reported infra category on a parsed verdict must be treated as a content judgment, not an infra failure)", got)
	}
}

func TestJudgedGuardrailCategory_PolicyEscalationStaysNonRetryable(t *testing.T) {
	got := judgedGuardrailCategory("policy")
	if got != "policy" {
		t.Fatalf("judgedGuardrailCategory(\"policy\") = %q; want \"policy\" (a quality-gate regression must stay non-retryable, but must not masquerade as content)", got)
	}
}

func TestJudgedGuardrailCategory_EmptyStaysEmpty(t *testing.T) {
	if got := judgedGuardrailCategory(""); got != "" {
		t.Fatalf(`judgedGuardrailCategory("") = %q; want "" (empty already means "content")`, got)
	}
}

// invalidResponseWants checks the shape every written-but-unparseable
// fallback must have: a non-empty, non-infra "invalid-response"
// Category; a Summary naming the real parse error via %v (not a
// static suffix); and the truncated raw judge output preserved in
// Issues. categoryPad is the extra alignment spaces after "Category:"
// that distinguish the verifier-family verdicts (longer field
// names) from the guardrail-family ones in main.go.
func invalidResponseWants(categoryPad string) func(src string) error {
	return func(src string) error {
		if !contains(src, `Category:`+categoryPad+`"invalid-response"`) {
			return fmt.Errorf("unparseable-response fallback must be a terminal Category\"invalid-response\" verdict — never \"infra\" (that would re-run the whole judge against the same already-relocated output file) and never empty (that would trigger content auto-fix on a failure that was never a judgment)")
		}
		if !contains(src, `its response could not be parsed as a verdict: %v`) {
			return fmt.Errorf("unparseable-response Summary must name the real parse error via %%v (uerr), not a static \"unparseable response\" string")
		}
		if !contains(src, `raw judge output (%d bytes): %s`) {
			return fmt.Errorf("unparseable-response verdict must preserve the truncated raw judge output in Issues")
		}
		return nil
	}
}

// Verifier-family verdicts (VerificationResult) have longer field
// names than GuardrailVerdict's, so "Category:" is padded with one
// extra space before the value in the unparseable fallback blocks.
const (
	verifierCategoryPad  = "   "
	guardrailCategoryPad = "  "
)

func TestBL410_VerifierSessionUnparseable_TerminalInvalidResponse(t *testing.T) {
	src := readMainGo(t)
	if contains(src, `"verifier(session): unparseable response"`) {
		t.Error(`autonomousVerify: the session-unparseable fallback must no longer return a static "unparseable response" verdict stamped "infra"`)
	}
	if err := invalidResponseWants(verifierCategoryPad)(src); err != nil {
		t.Error(err)
	}
}

func TestBL410_VerifierAskUnparseable_TerminalInvalidResponse(t *testing.T) {
	src := readMainGo(t)
	if contains(src, `"verifier: unparseable response"`) {
		t.Error(`autonomousVerify: the ask-unparseable fallback must no longer return a static "unparseable response" verdict stamped "infra"`)
	}
	if err := invalidResponseWants(verifierCategoryPad)(src); err != nil {
		t.Error(err)
	}
}

func TestBL410_GuardrailSessionUnparseable_TerminalInvalidResponse(t *testing.T) {
	src := readMainGo(t)
	if contains(src, `Summary:   "guardrail(session): unparseable response — cannot verify compliance"`) {
		t.Error(`autonomousGuardrail: the session-unparseable fallback must no longer return a static "unparseable response" verdict stamped "infra"`)
	}
	if err := invalidResponseWants(guardrailCategoryPad)(src); err != nil {
		t.Error(err)
	}
}

func TestBL410_GuardrailAskUnparseable_TerminalInvalidResponse(t *testing.T) {
	src := readMainGo(t)
	if contains(src, `Summary:   "guardrail: unparseable LLM response — cannot verify compliance"`) {
		t.Error(`autonomousGuardrail: the ask-unparseable fallback must no longer return a static "unparseable LLM response" verdict stamped "infra"`)
	}
	if err := invalidResponseWants(guardrailCategoryPad)(src); err != nil {
		t.Error(err)
	}
}

// Genuine no-output failures must STILL be "infra" — the bounded
// infra retry exists for exactly these (the judge never produced
// anything to parse, so re-running the call against a healthy session
// is meaningful).
func TestBL410_SessionErrorBranchKeepsInfra(t *testing.T) {
	src := readMainGo(t)
	if !contains(src, `Summary:   "guardrail(session): " + serr.Error(),`) ||
		!contains(src, `Category:  "infra",`) ||
		!contains(src, `Summary:    "verifier(session): " + serr.Error(),`) ||
		!contains(src, `Category:   "infra",`) {
		t.Error(`autonomousGuardrail/autonomousVerify: the session-error (no output file) branch must keep Category "infra" — that is the only "infra" verdict left for a written-file-less failure, and the infra retry must still fire for it`)
	}
}

func TestBL410_AskCallFailureKeepsInfra(t *testing.T) {
	src := readMainGo(t)
	if !contains(src, `Summary:   "guardrail: ask call failed: " + reason,`) ||
		!contains(src, `Category:  "infra",`) ||
		!contains(src, `Summary:    "verifier: ask call failed: " + reason,`) ||
		!contains(src, `Category:   "infra",`) {
		t.Error(`autonomousGuardrail/autonomousVerify: the ask call-failure (no response at all) branch must keep Category "infra" — infra retry must fire when no output was produced, and only then`)
	}
}

func TestBL410_ParsedVerdictCategoryStillFiltered(t *testing.T) {
	src := readMainGo(t)
	for _, want := range []string{
		`parsed.Category = judgedGuardrailCategory(parsed.Category)`,
	} {
		if !contains(src, want) {
			t.Errorf("autonomousGuardrail: %q — a successfully-parsed verdict still must go through judgedGuardrailCategory so a self-reported \"infra\" never reads like an infra failure", want)
		}
	}
}
