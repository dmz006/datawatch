# Stats, charts and cost: what exists, what is planned, how it fits (2026-11-06)

Operator, quoted: "did you verify if there were existing prd/automation that had any of the graphs or usage stats or
details to link them?" Checked 2026-11-06 by searching the registry and every non-archived Automaton for chart, Gantt,
usage, cost, token, budget, throughput and dashboard content. The first draft of the telemetry Automaton (`4aa80001`)
did not link these and rebuilt the capture layer that `24400001` already plans; it was cancelled and replaced by
`29190001`.

| Piece | Where it stands | Role |
|---|---|---|
| Usage readings, pace line, burn guard, usage calculator | shipped | the allowance side of the budget; reused as the `allowance` mode |
| Progress history snapshots, `datawatch autonomous progress`, verifier statistics, capacity leases | shipped | the existing series for throughput, completion range and lane time |
| Cost report (`090f0001`) | shipped, archived | `datawatch cost report` by Automaton, Automation, backend or week, tokens first |
| Per-Automaton spend ledger and cost report (`24400001`) | needs_review, 36 tasks | the capture layer: tokens, time, sessions and tool calls per Automaton, and the fix for the unattributed spend (277 sessions, about 116 million output tokens) |
| Gantt charts and the all-projects dashboard (`ef660001`) | revisions asked | the Gantt component and the stacked project card; the Projects tab reuses it |
| Lane utilization view (`a4b40001`) | blocked, worktree lost | busy and idle share per lane over time; needs a rebuild |
| Dashboard view for the Automation arc (`3f3f0001`) | draft | stages, blocked reasons, shared lane use |
| Dashboard data fixes (`edc10001`) | draft | the cards fed from real data (work map, lane pulse, 30-day history, CI) |
| Settings move and small usage cards (`9f8f0001`) | draft | the usage page as three small dashboard cards |
| Projects tab (`d3680001`) | draft | project pages with history, cost, Gantt and events |
| Counts by project (`dae00001`) | draft | corrected totals overall and by project, Automation and Automaton |
| Tag everything (`b7910001`) | draft | the tags the grouping needs |
| Billing mode and the stats outputs (`29190001`) | draft | `billing_mode` api, allowance or local per LLM, and charts not covered above |

Order suggested by the dependencies: counts (`dae00001`) and tags (`b7910001`) first, then the ledger (`24400001`) with the
billing mode (`29190001`), then the Gantt (`ef660001`), the dashboards (`edc10001`, `a4b40001`, `3f3f0001`) and the
Projects tab (`d3680001`), which reads all of them.
