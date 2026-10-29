# BL414 PRD cost rollup and cost reporting: design proposal

**Date:** 2026-10-29

**Version:** none (planning document; plan-document commits are exempt from the version bump)

**Tracker:** BL414 (registry row 38)

Confirmed: no — no option is chosen. This file records the problem and the options; nothing is decided and no implementation PRD is filed until a `Confirmed: yes` line replaces this one.

## Problem

An operator cannot ask "what did this Automaton cost", "what did this Automation cost", "what did each backend cost this week". The pieces to answer exist, but the one shared per-PRD rollup is wired only into the Automation budget gate, and the operator-facing cost views are lifetime totals with no time window. Scope was narrowed on 2026-10-22: this item is reporting and the rollup only. Ceilings and hard stops live in BL428 Phase 5 (shipped) and are out of scope here.

## What exists today (verified)

| Piece | Proof |
|---|---|
| Per-session counters: `TokensIn`, `TokensOut`, `EstCostUSD`, priced by a per-backend rate table with config overrides | `internal/session/cost.go` (`AddUsage`, `EstimateCost`), `internal/server/cost.go` |
| Cost summary across all sessions with a `per_backend` breakdown (sessions, tokens, USD); no time filter | `internal/session/cost.go` (`SummaryFor`), `GET /api/cost` |
| Per-session breakdown | `GET /api/cost?session=<id>` |
| Cost across federated servers | `GET /api/cost/aggregated` in `internal/server/bl312_aggregated.go` |
| Rates read and write (write needs `config:write`) | `GET` and `PUT /api/cost/rates` |
| MCP tools `cost_summary`, `cost_usage`, `cost_rates`; CLI `cost`; comm channel `cost`; the PWA dashboard shows `total_usd` | `internal/mcp/sx_parity.go`, `cmd/datawatch/cli_sx_parity.go`, `internal/router/commands.go`, `internal/server/web/app.js` |
| A session-status cost block (same three fields) shown with the Automaton task | `internal/server/session_status_automaton.go` (`SessionAutomatonCost`) |
| A spend ledger: one entry per spend (LLM name, time, USD, tokens, session id), 31-day retention, recorded from the capacity wiring; entries without an LLM name are dropped | `internal/autonomous/budget.go` (`BudgetLedger`, `RecordSession`), `cmd/datawatch/capacity_wiring.go` |
| Ledger windows per LLM: `GET /api/budget`, `GET /api/budget/usage?llm=&from=&to=` | `internal/server/budget.go` |
| The shared per-PRD rollup `PRDCostUSD(prd, since, llm)` and per-Automation `AutomationSpendUSD`; the session set covers workers, verifiers, judges and decompose sessions; it falls back to lifetime session totals when `since` is zero or older than the ledger horizon; peer spend is excluded | `internal/autonomous/automation_cost.go` |
| The Automation budget policy and its view (spent, window, reset, hold) | `internal/autonomous/automation_budget*.go`, `AutomationStatusSummary.SpentUSD` in `internal/autonomous/models.go` |
| An optional cost field on decision-log entries (`Decision.CostUSD`) | `internal/autonomous/models.go` |
| The usage calculator (9.29.0) works on operator-entered subscription percent readings and the weekly reset, not on dollars | `internal/usagecalc/` |

MISSING:

| Rollup | Gap and evidence |
|---|---|
| Per Automaton (PRD) | `PRDCostUSD` has no caller outside the Automation budget gate; no REST, MCP, CLI, comm or PWA surface returns it |
| Per Automation | `AutomationSpendUSD` is the same: only the gate and the status row (when a budget policy exists) use it |
| Per backend, per week | `/api/cost` has no `since`; the ledger has windows but is keyed by LLM registry name, not backend family, and holds 31 days |
| Per task | `Task` and `PRD` records carry no token or cost counters; a task's cost is only reachable through its session id. Whether a retry keeps the earlier attempt's session id was not verified |
| Per call, per role, local-model cost | the per-call usage record store (BL405 Phase 4) is not started; BL405 Phase 5 (node rate times duration) is only partly done |
| Peer spend in a rollup | excluded by design in `PRDCostUSD`; opt-in peer counting exists only for the budget gate |

Known limits of the data: dollars for subscription backends are estimates at configured API rates, not billed amounts, and sessions with no LLM registry name are absent from the windowed figure (they appear only in lifetime totals).

## Options

**A. One cost report over existing data.** Add a single report with a `group_by` of automaton, automation, backend or week, and `since` and `until`, built on `PRDCostUSD`, `AutomationSpendUSD`, `SummaryFor` and the ledger. No new storage. Cost: 1 to 2 PRDs, about 8 to 10 tasks (report function, REST, MCP, CLI, comm, PWA view, docs, tests). Risk: bounded by the 31-day ledger and by the lifetime fallback, which ignores `since`; the report must label both.

**B. A plus persisted rollup snapshots.** A also writes a daily rollup per Automaton and backend, so figures survive ledger retention and deleted sessions. Cost: A plus 1 PRD, about 5 tasks. Risk: a second store that must agree with the ledger; needs a reconcile rule.

**C. Wait for per-call records.** Build the rollups on BL405 Phase 4, which gives task, role and local-model cost. Cost: Phase 4 (large, not started) plus 1 PRD. Risk: reporting waits on a long item.

**D. Per-Automaton field only.** Show `PRDCostUSD` as one field on the Automaton view and status. Cost: 1 PRD, about 4 tasks. Risk: no Automation, backend or week views; the question stays half answered.

## Recommendation

Option A, written so the data source is an interface: B or C then replace the source without changing the report or its surfaces. It answers all four missing rollups the operator named with code that already exists and is tested, and it needs no new store.

## Open questions for the operator

1. Which option? A / B / C / D.
2. "Per week" means: calendar week (Monday start), or the subscription week that the usage calculator anchors to the reset? (calendar / subscription)
3. Should dollars for subscription backends be shown as labeled estimates at API rates? (yes / no)
4. Should opt-in peer spend be included in a report when the operator turns it on? (yes / no)
5. Is a 31-day history enough, or must figures survive longer (which forces option B)? (enough / longer)

## Dependencies and order

BL405 Phases 4 and 5 supply per-call and local-model cost (option C, and a later upgrade for A). Registry row 53 (BL472) lists its inferred token and cost rollups as depending on BL414, so this item feeds the calculator's cost side. BL428 Phase 5 (ceilings and hard stops) is shipped and untouched. Order: A now, then B or C when per-task or long-history views are wanted.

## Parity surface

| Surface | What a build touches |
|---|---|
| Config | none for A; a retention key for B |
| REST | new report route with `group_by`, `since`, `until`; `fedCap` read capability |
| MCP | one new report tool (and a capability map entry, which a test requires) |
| CLI | a report subcommand beside `cost` |
| Comm channel | a report form beside `cost` |
| PWA | a cost report view and a per-Automaton cost field |
| Mobile | datawatch-app parity issue required (PWA change) |
