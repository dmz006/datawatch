# Guardrail execution flow

End-to-end sequence for how a guardrail is resolved and invoked during
Automaton execution.

## Resolution chain

Resolved independently per level — task, story, and prd (PRD
completion, BL418):

```
per-Automaton explicit fields
  PerTaskGuardrails / PerStoryGuardrails / PerPRDGuardrails
        │ non-empty (for this level)?
        ▼ yes → use explicit list
        │ no
        ▼
  GuardrailProfile assigned?
        │ yes → load profile.Guardrails
        │ no
        ▼
  Global Config
  (autonomous.per_task_guardrails / per_story_guardrails / per_prd_guardrails)
```

All three lists are empty by default — a level with no entries never
invokes a guardrail, even if a rules/context file or scan config
exists.

## Per-task execution

```
Task completes
      │
      ▼
resolveGuardrails(prd, "task")
      │ returns []string of guardrail names
      ▼
for each name:
  ┌─ in registry as type=="scan"?
  │   yes → invokeScanGuardrail(entry, invocation)
  │            builds targeted ScanConfig (only that scanner)
  │            calls scan.Run(dir, cfg, scanners, grader)
  │            converts Result → GuardrailVerdict
  │   no  → GuardrailFn registered? → call fn(ctx, inv)
  │   no  → no-op (unknown guardrail; logged)
  └─►
      │
      ▼
  verdict.Outcome == "block"?
  │   yes → see "Retry, escalation, and content auto-fix" below
  │   no  → continue
```

## PRD-completion guardrail (`rules-review`, BL418)

```
PRD rolls up to completed
      │
      ▼
runPRDCompletionProjectRulesCheck (deterministic scan, unchanged)
      │
      ▼
runPRDCompletionRulesReview
      │ "rules-review" in resolveGuardrails(prd, "prd")?
      │   no  → no-op
      │   yes → rules/context file present in ProjectDir?
      │           no  → no-op
      │           yes → fn(ctx, inv)  (same GuardrailFn as task/story,
      │                  judgment review of the real diff, not a scan)
      ▼
verdict appended to prd.Verdicts; outcome=="block" → see "Retry,
escalation, and content auto-fix" below (PRDBlocked only on genuine
exhaustion, not on the first block)
```

Unlike the per-task/per-story loop, this runs once, at PRD completion,
for a single named guardrail (`"rules-review"`) rather than iterating
a list of scan/skill guardrails — there is currently exactly one
guardrail defined at this level.

## Retry, escalation, and content auto-fix

Every `block` verdict (at task, story, or PRD-completion level) carries
a `Category` — see [howto/retry-policy.md](../howto/retry-policy.md)
for the full taxonomy and default budgets. A block does not pause the
Automaton immediately; it goes through this sequence first:

```
block verdict (Category: infra | content | scope | policy)
      │
      ▼
  Category == "infra"?
      │ yes → runGuardrailWithInfraRetry: retry the identical call,
      │       keeping the original backend/model for
      │       resolveInfraEscalateAfter(prd) attempts (default 2),
      │       then fall back to the global default backend/model —
      │       bounded by retryBudgetRemaining(prd, "infra")
      │ no
      ▼
  Category == "content" (default/empty) and
  resolveContentAutoFixEnabled(prd) and budget remains?
      │ yes, task/story level → respawn the same unit with the
      │       verdict's Issues fed back as a RetryHint (same
      │       mechanism as a normal verification-failure retry)
      │ yes, PRD-completion level (no single unit to respawn) →
      │       synthesizeCorrectiveWork appends a new story+task to
      │       the SAME Automaton (never a new one), spec built from
      │       the verdict's findings; skips per-story approval when
      │       auto_fix_skip_approval is true (default); once it
      │       completes, the PRD-completion check re-fires
      │ no (scope/policy, or budget exhausted)
      ▼
  recordFailureLearning(prd, unitID, category, detail)
      → writes one Learning row (fires once, only on genuine
        exhaustion — never on a retry that still has budget left)
      ▼
  TaskBlocked / story block / PRDBlocked — operator action required
```

The retry ceiling (`resolveRetryCeiling`, default 5 — see
retry-policy.md) bounds total retries across every category for one
Automaton's whole lifecycle, independent of any single category's own
budget.

## Verdict outcomes

| Outcome | Condition |
|---------|-----------|
| `pass`  | Zero findings at or above threshold |
| `warn`  | Findings exist but below threshold |
| `block` | Findings at or above `fail_on_severity` |

Severity order: `info` < `warning` < `error` < `critical`.

## Scan-type guardrail dispatch

```
invokeScanGuardrail(entry, inv)
  │
  ├─ entry.ScanType == "sast"    → enable only SASTEnabled
  ├─ entry.ScanType == "secrets" → enable only SecretsEnabled
  └─ entry.ScanType == "deps"    → enable only DepsEnabled

  scan.Run(dir, cfg, []scanner, nil)
  → Result{Findings []Finding}

  for each finding:
    if severityGE(finding.Severity, entry.FailOnSeverity):
      outcome = "block"
    else:
      outcome = "warn" (if any finding) or "pass"
```

## Telemetry

Each guardrail verdict is appended to the session telemetry record:

```json
{
  "guardrail_verdicts": [
    {"guardrail": "sast-scan",    "outcome": "pass"},
    {"guardrail": "secrets-scan", "outcome": "block", "detail": "AWS key in config.go:14"}
  ]
}
```

Reachable via `GET /api/sessions/{id}/telemetry` or the `telemetry`
comm verb.

## See also

- [howto/guardrail-library.md](../howto/guardrail-library.md)
- [howto/retry-policy.md](../howto/retry-policy.md) — categories, budgets, content auto-fix, Learning
- [telemetry-flow.md](telemetry-flow.md)
- [api/autonomous.md](../api/autonomous.md)
