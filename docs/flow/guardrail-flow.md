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
  │   yes → mark task failed; pause Automaton
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
verdict appended to prd.Verdicts; outcome=="block" → PRDBlocked
```

Unlike the per-task/per-story loop, this runs once, at PRD completion,
for a single named guardrail (`"rules-review"`) rather than iterating
a list of scan/skill guardrails — there is currently exactly one
guardrail defined at this level.

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
- [telemetry-flow.md](telemetry-flow.md)
- [api/autonomous.md](../api/autonomous.md)
