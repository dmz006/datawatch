# Promotion findings ledger (archive)

Superseded 2026-11-16 21:10: these eleven findings are now daemon finding records (`datawatch plan-record list findings`), each linked to the Automaton filed for it, and new findings are created there. This file stays as the archive of the first evening.

Started 2026-11-16 20:55. Operator, quoted: "You're finding bugs as you go. Keep track of those so you can file the PRDs to get those bugs fixed for like things missing like the definitions or if you're not finding how to set things up or there's anything else missing or there's a bug, remember to file those as you go."

This is the running list of bugs, gaps and missing documentation found while turning shipped features on and supervising the Automata. Rules for it:

1. Add a finding the tick it is found, with the evidence that proves it (a command and its output, a record, a log line), before filing.
2. File the Automaton the same tick, after the overlap check. Active overlaps are extended, not duplicated.
3. Each Automaton spec names the outcome, the surfaces that need parity, and every open design question (the PWA placement of anything new is the operator's decision, no task decides it).
4. When an Automaton ships, turn its feature on, verify it live, and mark the row here.

## Filed

| # | Finding | Evidence | Filed as | State |
|---|---|---|---|---|
| 1 | Settings that ship off have no definitions entry, and some no documentation at all | 9 of 14 checked settings absent from the definitions; planning routing, default quality gates and tag placement axes absent everywhere | `8a3e0001` Document every shipped setting that is off by default | draft |
| 2 | Nothing lists the shipped-but-off settings; the configuration read shows only keys that were set | the event wake was invisible until found by reading the roadmap | `a4900001` A shipped-but-off list | draft, PWA placement is an open question |
| 3 | A planner reply that cannot be parsed is recorded not retryable and the Automaton returns to draft | four failure shapes on two Automata in one evening (empty task, bad escape, one object instead of the plan, truncated) | `c8f50001` Retry a planner reply that cannot be parsed | draft |
| 4 | Judge and verifier infrastructure faults block Automata and re-run workers | three finished Automata held by judge timeouts; one task re-ran its worker three times (about 45 minutes each) on verifier timeouts and a truncated reply | `09c30001` A judge or verifier infrastructure fault must not block an Automaton or re-run the worker | draft |
| 5 | A task added later has no model and defaults to one the nodes are not pinned to; a withdrawn placement question fails the task | a corrective task failed with "the operator declined, or the ask timed out" after the question was withdrawn | `13cf0001` A task added later inherits its story and Automaton model | draft |
| 6 | No way to add a dependency to an existing task, to accept a hand-verified task, and repair-dependencies returns the whole record | edit-task cannot change dependencies; the repair call changed nothing and returned about 69,000 characters | `53f80001` Add a dependency to an existing task, accept a hand-verified task | draft, PWA placement is an open question |
| 7 | Final-gate scripts keep missing the docs sync, hand-write the allowed-file list and exceed the script time limit | five Automata needed a hand repair and a reset in one day | `e81e0001` Final-gate scripts: shared helpers | draft |
| 8 | Merge gates passed a CodeQL fix that did not close twelve alerts; no gate runs the local scan | the scanner reported twelve alerts still open on the next push | `08170001` Merge gates run the local CodeQL scan; follow-up fix itself is `4c000001` | draft |
| 9 | The CodeQL gate cannot tell open alerts from a failed analysis, and a manual dispatch does not re-analyze | all four analysis jobs aborted on an installation rate limit; the gate stayed red with no code cause | `32100001` The CodeQL gate must say whether alerts are open or the analysis failed | draft |
| 10 | GPU lane count ignores the node engine slots, so the watch reports a false idle lane at nearly every poll | pool cap 3, two nodes with one engine each, "IDLE pi lane (2/3 held)" while every engine is in use | `526f0001` GPU lane count must follow the node engine slots | draft |
| 11 | Some MCP and CLI reads are unbounded, and the CLI has no question get | question list returned about 142,000 characters; `datawatch question get` printed the help | `64650001` Bound the output of MCP and CLI reads | draft |

## Found, not a product defect, or not filed

| Finding | Why not filed | Next step |
|---|---|---|
| The daemon burn guard (projection, graded steps) and the supervisor cron guard (pace line, on or off) disagree about the week | a policy conflict, not a bug | the operator decides which rule governs; raised 2026-11-16 20:45 |
| An Automaton whose only change is a stray completion marker at the repository root (718a0001) | debris, not a defect | the operator records the landing as dropped |
| Settings that default off and were never set cannot be seen in the configuration read | covered by finding 2 | none |

## Open questions for the operator (no task may decide these)

- Where the shipped-but-off list appears in the PWA (finding 2).
- Where the re-verify, accept and set-dependency actions appear in the PWA (findings 4 and 6).
