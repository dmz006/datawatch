# [sync] A quiet engine while paused, a planner gate and planner fall-through — parity draft for the datawatch-app native clients (Android, iPhone)

**Not filed yet.** This is the tracked draft; the GitHub issue is opened when
the operator files it. This draft is a file write only: it runs no hosting CLI
and creates nothing on any host.

**World this work changes** (found 2026-11-23): with the loop off or the
operator pause on, the engine still started planner sessions for a new draft,
re-planned the same Automaton again and again, ran merge passes every few
seconds, and a burn guard step back put an old inflight cap over the cap the
operator had set. The daemon now enforces, with no model involved:

- a pause holds the engine's own planner starts (lane fill decompose, the
  planner restart after a daemon restart, the re-plan after a reworded spec)
  and merge passes; an operator-run decompose is never held;
- the engine never plans an Automaton that already has a plan, and holds one
  whose plans keep failing after three engine starts with one alert;
- with `autonomous.capacity_fallthrough` on, a planner whose pools are full
  moves to an allowed alternate backend, as a task does;
- the burn guard restores only the inflight cap it lowered, and the pool limit
  follows the entry.

## Affected area

No new endpoint, setting, tool, verb or screen. The client-visible facts are
read-only:

| Fact | Where it shows | What the app should do |
|---|---|---|
| Decision kind `planner_fallthrough` (note names both backends) | Automaton decision log (`GET /api/autonomous/prds/{id}`, `decisions[]`) | Render it like the existing `capacity_fallthrough` entry. |
| A hold placed by actor `planning-gate` (reason: "the engine started planning N times without an accepted plan ...") | Automaton `status: held`, `hold.reason`, `hold.held_by` | Nothing new: the existing hold view shows it. |
| Alert title `Automaton held: planning kept failing` | Alerts list | Nothing new. |
| PRD field `auto_plan_starts` (integer, omitted when 0) | `GET /api/autonomous/prds/{id}` | Optional: show it next to the plan state; ignore if absent. |
| A planner restart waiting for a pause shows the Automaton in `planning` with no planner session until the operator resumes | PRD `status`, pause status | Nothing new: the pause banner already explains it. |
| The step back alert may say "kept the <entry> inflight cap at N" | Alerts, burn guard `last_decision.alert_text` | Show the text as given. |

## Surfaces

| Surface | Status |
|---|---|
| Config (yaml key) | Not touched: no new setting. The limit of three engine starts is a fixed value in the daemon. |
| REST / openapi | Read-only field `auto_plan_starts` only. |
| MCP, CLI, comm, PWA | Not touched. |
| Federation | Not touched: each daemon gates its own planner starts. |
| datawatch-app | This draft. |

## Open

- Whether the limit of three engine starts should become a setting (a new
  setting means every surface above and locale keys).
- Whether the loop being off, not only the operator pause, should hold the
  engine's planner starts. Lane fill is driven by its own mode, not by
  `autonomous.enabled`, and existing tests encode that.
