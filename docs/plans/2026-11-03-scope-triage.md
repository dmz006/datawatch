# 10.0.0 scope list for the operator's decision

**Date:** 2026-11-03
**Version:** written at 9.73.2
**Status:** information only; nothing in this document changes the scope
**Scope:** the 36 Automata created since 2026-11-02, with their open task counts, so the operator can see where the remaining work comes from. Only the operator decides what is in or out of 10.0.0. The "supervisor note" column is an opinion, not an action; no item has been moved, deferred or cancelled.

## Why the estimate moved from about 4 to about 4.9 days

Open tasks: 361 across 55 live Automata. 175 are in Automata created since 2026-11-02; 186 are older. The remaining work grew because new work was filed (operator requests and defects found while building), not because tasks slowed. Only 38 percent of open tasks are the change itself; the rest is surfaces (19), documentation (16), gate report (13) and locales, parity and changelog (11), which the generated-wrapping and bug-batching Automata aim to cut.

## The list

Open = tasks not yet completed. "Undecomposed" means the plan is still being made.

### Defects and fixes found while building

| Automaton | Open | Note |
|---|---|---|
| Secret scan before daemon push | 3 | supervisor note: security |
| Spec read returns full text, edits refuse truncated specs | 0 (final gate) | |
| Priority by kind | 3 | |
| Read-only tasks may write a declared report path | 5 | |
| Review-timed-out state and size-aware judge routing | 6 | |
| Snapshot skip list on every finish path | 7 | |
| Merge pipeline card empty state | 6 | |
| CI listing fix (image refresh and cleanup) | 7 | |
| Safe-to-delete report speed | 9 | |
| Keep running work through a daemon restart | 4 | |
| Drain compute nodes and honour consumer denials | 1 | |
| Move queued planning runs and unstarted tasks to another pool | 8 | |
| Peer selection in the web UI (operator-reported) | undecomposed | |
| Flaky test in the autonomous package | undecomposed | |

### Features the operator asked for or approved

| Automaton | Open | Note |
|---|---|---|
| Classification-based verdicts (V Method) | 10 | |
| Requirements and model-pull consent | 7 | |
| Verifier slots per node | 9 | |
| Verifier cap above 1 needs a measured headroom check | 10 | |
| Planning routing and retry ladder | 9 | |
| Decompose lint, templates and guidance | 1 | nearly done |
| Rules and context reach the planner | undecomposed | |
| Automatic grouping tags | 9 | |
| Placement auto-tags | 11 | |
| Federated peer placement design (docs) | 5 | |
| Gantt charts and the all-projects dashboard | 6 | |
| Lane utilization view | 9 | |
| Sessions tile mode and grouping | 10 | |
| Observer stats pull for federated peers | 10 | |
| Configurable splash dwell | 8 | |
| Automation sub-tab re-layout (six sub-tabs) | undecomposed | planning pilot |
| Bug batches and tiny-fix lane | undecomposed | filed 2026-11-03 |
| Generated wrapping (gate report, changelog, parity draft) | undecomposed | filed 2026-11-03 |

### Held or overlapping

| Automaton | Note |
|---|---|
| Dashboard view for the Automation arc (draft) | overlaps the Gantt and lane utilization Automata; the supervisor suggests asking whether it is still wanted |
| Integration-stage placeholders (two) | held until their prerequisites merge |
| Release audit and full validation placeholder | held; final stage |
| Deflake round five (blocked) | needs a decision: merge, redo or drop |

## What the supervisor will not do

Nothing here is moved, deferred, cancelled or reprioritised without the operator's word. The supervisor keeps filing what is asked, plans it, and reports the effect on the estimate each tick.

## Decisions the operator may want to make (asked one at a time when the operator wants them)

1. Whether the dashboard arc view draft is still wanted given the Gantt and lane utilization Automata.
2. What to do with the blocked deflake round (merge, redo or drop).

## Parity surface

This document changes no code or surface; no parity entries apply.
