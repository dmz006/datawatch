# BL608 judge routing policy and time limit: operator decisions

Date started: 2026-10-25. Recorded by the supervisor one question at a time, with the operator's words quoted. The BL608 PRD (`12520001`) reads this file instead of asking again.

Scope reminder: guardrail and plan-review judges follow the verifier routing policy (a distinct judge role that defaults to the verifier policy) and have a configurable time limit that raises an alert and retries on the next policy backend. Judges already take capacity leases (shipped).

## Decisions

1. **Default time limit: 30 minutes.** (Operator, 2026-10-25: "1, 30 min".) The limit counts the judge's run only, not time spent waiting for a capacity slot. It is a setting the operator can change.
2. **Retries: one attempt per backend in the policy.** (Operator, 2026-10-25: "1, one per backend".) Each backend in the judge policy gets one chance; the total number of attempts follows the length of the policy list.

## Still open (asked in order)

3. What happens after the last backend in the policy has also timed out.
4. The alert level and wording when a judge times out.
