# Final gate — federated peer placement design doc

Date: 2026-11-05

Gate type: read-only; no files changed, no builds, tests, gh, or daemons run.
Design doc gated: the 2026-11-03 federated peer placement proposal in the
plan set (design for v9.67.0 generation, unimplemented).

## Tally

8 PASS, 1 FAIL — 9 checks total.

## Checks

| # | Check | Result |
|---|---|---|
| 1 | Story-check script executed from the project directory | FAIL |
| 2 | Planning Rules — ISO date at top | PASS |
| 3 | Planning Rules — version at top | PASS |
| 4 | Planning Rules — Scope section | PASS |
| 5 | Planning Rules — Phases or Status | PASS |
| 6 | Parity surface — all eight surfaces each included or excluded with a stated reason | PASS |
| 7 | Federation-Parity — every remote write/run operation mapped to a capability with a guard requirement | PASS |
| 8 | Terminology — Automaton/Automata in user-facing wording, PRD only in internal names | PASS |
| 9 | Open questions — every open question carries a Recommendation | PASS |

## Note on check 1

The required story-check script was not present in the project's verify
directory; executing it produced only a shell "No such file or directory"
error, no script output. All other checks were performed by direct reading
of the design document against the project guardrails.

## Open-question coverage

8 open questions in the gated doc; 8 carry an explicit Recommendation.
