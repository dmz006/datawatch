# BL472 usage and cost calculator: operator decisions

Date started: 2026-10-25. The ten open questions of `docs/plans/2026-10-23-usage-calculator-design.md` are answered one at a time, with the operator's words quoted. The implementation PRD reads this file and does not ask again.

## Decisions

1. **Safety margin for "Pace to the week": 5 points as the default, and a setting.** (Operator, 2026-10-25: "1, 5 points as a setting".) The calculator advises against a target five points under the pace line unless the operator changes the setting.
2. **Hourly session window: no verdict of its own; it stays a secondary line.** (Operator, 2026-10-25: "1, keep it secondary".) The weekly verdict is what sets lane counts. The design leaves room for a session verdict if session readings are ever entered.
3. **Burn per lane: one figure for now, with room for per-model later.** (Operator, 2026-10-25: "1, one figure for now".) The design keeps a place to split the figure by model once a lane record per model exists.
4. **No previous reading within 2 hours: fall back to the week average and say so on the screen.** (Operator, 2026-10-25: "1, fall back and say so".) The screen shows a line that no reading exists in the last 2 hours and the week average is used, so the figure is understood to be smoother than the recent burn.
5. **Readings: typed by hand for now, with a paste path later, and the design must plan for API users and for reading usage from the account.** (Operator, 2026-10-25: "1, but plan for api users and reading from account for some users,  but me but plan for it".) For this operator the reading is typed by hand. The reading source is a pluggable input: typed, pasted usage text, and an account or API source for users who can read usage from the account or an API, each producing the same reading record. Only the typed source is built first; the others are planned for, not built. An API user's usage may be spend and tokens and not a weekly percentage, so the reading record must be able to carry either a percentage or a spend figure.
6. **Remaining work for "Run flat out": both.** (Operator, 2026-10-25: "3".) The calculator computes an estimate from the queued Automata times a cost per task, and the operator can overwrite it by typing a figure. The computed estimate is labelled as an estimate, the typed figure replaces it and is labelled as typed, and the queue's task count is shown beside both. The cost per task is a setting, and the screen must make clear which of the two figures it is using.
7. **Reset moment: a weekday, a local clock time and a time zone name.** (Operator, 2026-10-25: "1, zone name".) For example Sunday 08:00 in the America/New_York zone; the calculator follows the zone's own rules and stays correct across a daylight saving change. A fixed UTC offset is not used.
8. **Readings belong to the account, shared across federated daemons.** (Operator, 2026-10-25: "1, shared across daemons".) A weekly allowance is an account fact, so the series is shared: one daemon is the keeper of an account's series, and peer daemons read it over REST with a federation capability. The proposal must say how the keeper is chosen and what a peer does when the keeper is unreachable (show its last copy, marked as such).
9. **Bought extra credit: a separate extra-credit figure, entered by the operator, added to the ceiling.** (Operator, 2026-10-25: "1 for both".) The ceiling becomes 100 plus the extra points; the verdict and run-out figures use that ceiling, and the screen shows the extra-credit line separately.
10. **The older 22:08 reading entry is corrected in place, with a note.** (Operator, 2026-10-25: "1 for both".) The entry in the concurrency design doc now gives 1.41 days, 106.0 percent and a run-out around 02:02, with an italic line saying it was corrected on 2026-10-25 and why.

## Status

All ten questions of the usage calculator design are answered (decisions 1 to 10). The implementation PRD can be planned from this file.

## Implementation

Implemented on this line (unreleased; live in the running build):

- **Decision 1** — the 5-point safety margin: `safety_points` key (default 5.0, cap 50) applied under `pace to the week` (`TestPolicyPaceUsesTheSafetyMargin`).
- **Decision 2** — the session window is secondary: the advice carries it but the headline figure is the account reading.
- **Decision 3** — one burn-per-lane figure (`cost_per_task_points`), with the per-model split left as a place, not a field (`internal/usagecalc/model.go`).
- **Decision 4** — no reading in the last 2 hours falls back to the week average with an on-screen notice (`TestAdviseRecentFallback`, `recent_fallback` + `no_recent_reading`).
- **Decision 5** — typed readings built first (`internal/usagecalc/source.go`: typed now, pasted and API/account planned); the reading record carries either `percent` or `spend_usd`, never both.
- **Decision 6** — the Run-flat-out finish line has both figures: the estimate from the queue times the cost per task and the typed overwrite, each labelled, the task count beside both (`TestAdviseRemainingWorkEstimateAndTyped`).
- **Decision 7** — the reset is a weekday + local time + zone name, following the zone own rules across DST (`TestResetDaylightSavingChange`), stored as `reset_*` keys.
- **Decision 8** — the series is shared across federated daemons: keeper holds `data/usage/series.json`, peers read with `usage.read` (stale-marking the last copy) and forward writes to the keeper (`TestSharedSeries*`, `TestUsageSeries*`), the `X-DataWatch-Role` hop header serving the local copy to a forwarder.
- **Decision 9** — the extra credit is a separate figure the ceiling adds on, shown as its own line on every variant (`TestAdviseExtraCreditCeiling`, the parity "raised ceiling in every variant" test).
- **Decision 10** — the 22:08 entry in `docs/plans/2026-10-14-concurrency-and-budget-design.md` carries the 1.41 days / 106.0 percent / 02:02 correction with the note.

Every access surface is live and in the testing tracker (the *Usage and cost calculator — unreleased* section of `docs/testing-tracker.md`): the model and store in `internal/usagecalc/`, the REST layer in `internal/server/usage_calculator*.go` + `usage_readings*.go`, the CLI in `cmd/datawatch/cli_usage.go`, the comm channel in `internal/router/usage_calculator.go`, the six MCP tools in `internal/mcp/usage_calculator*.go`, the PWA card and settings card in `internal/server/web/app.js`, and the ten config keys in `internal/server/usage_calculator_config.go`. Operator-facing how-to: `docs/howto/usage-calculator.md`.
