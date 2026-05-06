# datawatch v5.10.0 — release notes

**Date:** 2026-04-26
**Spans:** v5.9.0 → v5.10.0
**Closed:** BL191 Q6 (guardrails-at-all-levels) — and with it, BL191 in its entirety

## What's new

### BL191 Q6 — Guardrails at all levels

Operator's answer to Q6: *"guardrails belong at all levels, it should
be for any prd with more than one story"*. v5.10.0 ships the
"all levels" half. The per-PRD-with-many-stories auto-routing to the
BL117 orchestrator stays as a future cut.

**Where guardrails live now:**

| Level | Mechanism | Surface |
|-------|-----------|---------|
| PRD | BL117 orchestrator graph | `internal/orchestrator/runner.go` (existing, unchanged) |
| Story | NEW — `Manager.runPerStoryGuardrails` after every task in the story completes | `Story.Verdicts []GuardrailVerdict` |
| Task | NEW — `Manager.runPerTaskGuardrails` after `verify` returns green | `Task.Verdicts []GuardrailVerdict` |

### Models

```go
// New shared shape — same field set as orchestrator.Verdict so PWA /
// chat clients render uniformly.
type GuardrailVerdict struct {
    Guardrail string    // "rules" | "security" | "release-readiness" | "docs-diagrams-architecture"
    Outcome   string    // "pass" | "warn" | "block"
    Severity  string    // info | low | medium | high | critical
    Summary   string
    Issues    []string
    VerdictAt time.Time
}

// Story adds:
Verdicts []GuardrailVerdict

// Task adds:
Verdicts []GuardrailVerdict

// PRDStatus adds:
PRDBlocked PRDStatus = "blocked"  // a guardrail returned `block`; awaits operator
```

### Config

```yaml
autonomous:
  per_task_guardrails: []     # default empty — opt-in
  per_story_guardrails: []    # default empty — opt-in
  # Names match the BL117 orchestrator surface:
  #   rules, security, release-readiness, docs-diagrams-architecture
  # `verification_backend` and `verification_effort` apply to the
  # guardrail LLM call (same /api/ask loopback as BL25 verifier).
```

Empty list = guardrails disabled at that level. Operators turning it
on for the first time should pair `verification_backend` with a
*different* backend than `decomposition_backend` — the BL25 design
prefers cross-backend independence.

### Behavior

- After each task verifies green, every guardrail in
  `per_task_guardrails` runs. Verdicts append to `Task.Verdicts`.
  A `block` outcome marks the task `TaskBlocked`, halts the PRD walk,
  and flips the PRD to `PRDBlocked` so the loop status reflects it.
- After every task in a story completes, every guardrail in
  `per_story_guardrails` runs. Verdicts append to `Story.Verdicts`.
  A `block` flips the PRD to `PRDBlocked`.
- An unparseable LLM response becomes a `warn` so best-effort runs
  still progress. Real `block` requires a deliberate JSON answer
  from the guardrail LLM.

### Wiring (main.go)

The new `GuardrailFn` indirection is wired in `cmd/datawatch/main.go`
to a `/api/ask` loopback exactly like the BL25 verifier — same shape,
same `verification_backend` / `verification_effort` knobs, same JSON
parse path. Operators get production-ready guardrails the moment they
add a name to either of the two new config keys.

### Tests

6 new unit tests:

- `TestPerTaskGuardrails_NoConfigured_NoOp` — empty list = no
  invocation even when a blocking GuardrailFn is wired.
- `TestPerTaskGuardrails_AllPass_AppendsVerdicts` — verdicts append in
  config order with correct guardrail names.
- `TestPerTaskGuardrails_Block_HaltsPRD` — block on first task →
  PRDBlocked + first task TaskBlocked + second task untouched.
- `TestPerStoryGuardrails_FireAfterAllTasksDone` — story-level
  verdict appears only after every task in the story completed.
- `TestPerStoryGuardrails_Block_HaltsPRD` — block on a story →
  PRDBlocked.
- `TestPerTaskGuardrails_NoFnWired_SilentNoOp` — no `SetGuardrail()`
  call → silent no-op, PRD still completes (bare-daemon safety).

Total daemon test count: **1345 passed**.

## What this closes

BL191 in its entirety:

- Q1 review/approve gate — v5.2.0 ✅
- Q2 templates — v5.2.0 ✅
- Q3 decisions log — v5.2.0 ✅
- Q4 recursive child-PRDs — v5.9.0 ✅
- Q5 PWA full CRUD — v5.5.0 ✅ (BL202)
- Q6 guardrails-at-all-levels — v5.10.0 ✅

## Known follow-ups (still open)

- **BL180 Phase 2** — eBPF kprobes (resume) + cross-host federation
  correlation. Operator said don't close BL180 until cross-host works.
- **BL190** — PWA screenshot rebuild for the now-13-doc howto suite
  (chrome plugin removed; will use puppeteer-core + seeded JSONL fixtures).
- **BL191 Q6 follow-up** — auto-route any PRD with >1 story through
  the BL117 orchestrator (operator's "for any prd with more than one
  story" half of Q6). Stays a follow-up because the autonomous +
  orchestrator surfaces both work cleanly today; auto-routing is a
  convenience, not a correctness requirement.

## Upgrade path

```bash
datawatch update                                  # check + install
datawatch restart                                 # apply the new binary

# Try guardrails (opt-in):
datawatch config set autonomous.per_task_guardrails '["rules","security"]'
datawatch reload
datawatch autonomous prd-create "add a small feature"
datawatch autonomous prd-decompose <id>
datawatch autonomous prd-approve <id>
datawatch autonomous prd-run <id>
datawatch autonomous prd-get <id> | jq '.stories[].tasks[].verdicts'
```
