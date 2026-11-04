# Completion review auto-fix and hold

- **Date:** 2026-11-04
- **Operator how-to:** [Automata completion review](../howto/automata-completion-review-autofix.md)

## Defect

A blocked completion review left the Automaton blocked with no way for
the system to act, and the findings were not shown on the attention
list. Lane fill then either stalled on it or the operator had to dig the
reason out of the logs.

## Decision: three outcomes

1. Fix task created, approved and run, then the review runs again.
2. Fix task could not be created: the Automaton is held in
   `needs_review` and the reason goes in the decisions. No retry used.
3. Retries spent (or `auto_fix_retries` is 0): held in `needs_review`
   with the findings attached as `rules_review_outcome`.

## Why lane fill must hold

A held Automaton must never count as runnable work and never occupy a
lane, or the supervisor's lane fill would loop on it. `needs_review` is
refused by lane fill; an Automaton with a fix task created is accepted.

## Why other categories stay blocked

Only content findings can be fixed by adding a task. Infrastructure and
similar faults have their own retry rules; a corrective task cannot
repair them and would only burn the budget.

## Retry ceiling interaction

`autonomous.auto_fix_retries` (default 1, 0 is off) is per Automaton.
The total ceiling `autonomous.retry_ceiling_total` still applies; when
it is reached the Automaton is held instead of fixed. The budget is
consumed only after the fix task exists.

## Parity surface

- REST: covered (`rules_review_outcome` on the attention list and
  Automaton get; add-task accepted on the parked Automaton).
- MCP: covered (`autonomous_attention_list`, `autonomous_config_set`).
- CLI: covered (`attention` output, `config set`).
- Comm channel: covered (`attention`, `configure`).
- YAML/config: covered (`autonomous.auto_fix_retries`).
- PWA: covered (attention list and Automaton view).
- Android: owed. The datawatch-app parity issue is filed by the
  supervisor at merge.
- iPhone: owed, same reason.
