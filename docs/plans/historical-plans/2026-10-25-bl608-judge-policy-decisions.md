# BL608 judge routing policy and time limit: operator decisions

Date started: 2026-10-25. Recorded by the supervisor one question at a time, with the operator's words quoted. The BL608 PRD (`12520001`) reads this file instead of asking again.

Scope reminder: guardrail and plan-review judges follow the verifier routing policy (a distinct judge role that defaults to the verifier policy) and have a configurable time limit that raises an alert and retries on the next policy backend. Judges already take capacity leases (shipped).

## Decisions

1. **Default time limit: 30 minutes.** (Operator, 2026-10-25: "1, 30 min".) The limit counts the judge's run only, not time spent waiting for a capacity slot. It is a setting the operator can change.
2. **Retries: one attempt per backend in the policy.** (Operator, 2026-10-25: "1, one per backend".) Each backend in the judge policy gets one chance; the total number of attempts follows the length of the policy list.
3. **After the last backend has also timed out: hold for review and alert.** (Operator, 2026-10-25: "1, hold for review and alert".) The PRD waits in a visible needs-review state with a summary of every attempt (backend, time spent, what the judge returned), an alert is raised, and nothing is approved, passed or blocked silently. The operator decides the next step.
4. **Alert level: warning for each timeout, error when the PRD is held.** (Operator, 2026-10-25: "Q5 1".) Timeout, a warning: judge for the PRD timed out after the limit on a backend, retrying on the next. Hold, an error: the PRD is held for review because the judge timed out on every backend. The wording carries the PRD id, the backends tried and the time spent, and goes to the existing alert channels.

## Status

All four BL608 questions are answered (decisions 1 to 4). The PRD can be planned from this file.
