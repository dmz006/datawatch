// BL410 Phase 0 — structured failure classification.
//
// Filed 2026-10-10, triggered into a real design 2026-10-12 by a live
// incident: PRD 7fe31a99's rules-review guardrail (BL418) spawned a
// real claude-code session that hung its full 10-minute timeout (B127:
// a flat, ask-only global model leaked into the session call and was
// rejected outright). The resulting GuardrailVerdict{Outcome:"block"}
// was indistinguishable in shape from a genuine LLM judgment finding a
// real AGENT.md violation — both collapsed to the same PRDBlocked
// terminal state with zero auto-retry, requiring a manual operator
// `reset_task(force=true)` to recover. After the real bug was fixed,
// the SAME guardrail fired again and returned a genuine `warn` with a
// real Issues list — that second case is working as designed and
// should still require a human (or, per BL410 Phase 2, a bounded,
// evidence-driven auto-fix); the first case never should have needed
// one at all.
//
// FailureReason and its four-category taxonomy exist to make that
// distinction structural instead of implicit: categorizing mechanisms
// the code already distinguishes internally (a Go error type like
// ErrWorkerStalled, a session-layer error, a JSON unmarshal failure,
// vs. a real GuardrailVerdict/VerificationResult payload a judgment
// actually produced) — never judging work CONTENT by a hardcoded rule.
// Content judgment stays exactly where it already lives: the existing
// LLM-judgment guardrail/verifier mechanism, untouched.
package autonomous

import "time"

// FailureReason is a structured, categorized account of why a
// PRD-lifecycle operation did not succeed. Additive only — it
// supplements, never replaces, the bare strings/opaque outcomes that
// exist today (Task.Error, GuardrailVerdict.Outcome=="block",
// VerificationResult.OK==false), so every existing reader of those
// fields keeps working unchanged.
type FailureReason struct {
	// Category is one of:
	//   "infra"   — the judging/executing MECHANISM itself never
	//               completed its job (session timeout/crash, a
	//               stalled worker, a failed LLM call — no output was
	//               produced at all). Retrying the identical call
	//               against a healthy session would likely produce a
	//               different outcome.
	//   "invalid-response" — the judge DID write a response, but it
	//               could not be parsed as a verdict. Terminal, NOT
	//               infra-retryable: re-running the whole judge would
	//               re-deliver the same already-relocated output file,
	//               so the response was consumed twice (or not at all) —
	//               the bbf70001 double-consumption incident class.
	//   "content" — the mechanism ran to completion and produced a
	//               real judgment: the work itself was found
	//               deficient, and the judgment's own Issues name
	//               what's wrong.
	//   "scope"   — the spec itself violates a structural constraint
	//               before any worker/judge acts on content. Retrying
	//               the identical spec unchanged reproduces the
	//               identical violation.
	//   "judge-timeout" — a judge run hit its time limit on every
	//               backend it tried, one attempt each: the judge
	//               policy list, or the verifier routing policy when
	//               autonomous.judge_policy is empty. A PRD-level
	//               rules-review judge holds the PRD in needs_review
	//               with one line per attempt. Not infra-retried and
	//               not content auto-fixed: nothing is approved,
	//               passed or blocked silently.
	//   "policy"  — an operator-configured gate deliberately
	//               intervened by design (e.g. a quality-gate
	//               regression) — working as configured, not
	//               malfunctioning.
	Category string `json:"category"`
	// Detail is a human-readable account of what happened.
	Detail string `json:"detail"`
	// Evidence holds raw supporting material: error text, a verifier/
	// guardrail's own Issues, a session output excerpt.
	Evidence []string `json:"evidence,omitempty"`
	// Source names which call site produced this, e.g.
	// "guardrail-session", "verifier-ask", "decompose-llm",
	// "decompose-parse", "verification-script-coverage",
	// "verification-script-scope".
	Source string `json:"source,omitempty"`
	// Retryable reports whether this occurrence was eligible for
	// auto-retry under the resolved policy at the time it occurred —
	// not just whether its category is generally retryable.
	Retryable  bool      `json:"retryable"`
	OccurredAt time.Time `json:"occurred_at"`
	// DependsOnTaskID is set only on a task that failed because a task it
	// depends on failed for an infrastructure reason (Source
	// "dependency-infra"). It names the task where the chain began, the one
	// whose own failure was the infrastructure failure, so a later reset of
	// that root can find every dependent that was failed behind it. Empty on
	// every other failure.
	DependsOnTaskID string `json:"depends_on_task,omitempty"`
}
