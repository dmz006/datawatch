---
docs:
  index: true
topics:
  - usage
  - billing
  - lanes
  - policy
  - advisor
exec_steps:
  - tool: usage_readings
    description: First look at the readings series so the operator knows the current cycle position (latest percent / spend, when, session percent, lanes), before typing a new one. Optional arguments:
    args:
      limit: "(optional) maximum number of readings to return, newest first"
    verify: response contains the readings series; each entry has percent and/or spend_usd, when, session percent, lanes and its id
  - tool: usage_reading_add
    description: Type a fresh reading of the provider's weekly allowance (percentage used and/or spend-to-date), optionally with the session percent and the lanes that were running — the calculator's core input.
    args:
      percent: "(optional) percent of the weekly allowance already used (0–100)"
      spend_usd: "(optional) provider-spend dollars consumed this cycle"
      at: "(optional) RFC3339 time of the reading; defaults to now"
      session_percent: "(optional) percent of a single Claude session that this reading covers"
      lanes: "(optional) how many lanes were running at that reading"
    verify: the call returns the stored reading and the current series position
  - tool: usage_reading_delete
    description: (Optional) Remove a mistyped reading by id from the series; the advice recomputes from the rest.
    args:
      id: the reading id returned by usage_readings or usage_reading_add
    verify: the reading no longer appears in a follow-up usage_readings call
  - tool: usage_extra_credit_set
    description: (Optional) Set the provider's extra-credit allowance, or clear it with 0. Advice counts it on top of the weekly allowance, with a ceiling.
    args:
      points: extra credit in points (0 to clear, 0–20000)
    verify: usage_advice reports the extra credit and its ceiling
  - tool: usage_remaining_work_set
    description: (Optional) Set the typed remaining work in points for the finish-early estimate, or clear it with 0.
    args:
      points: remaining work in task points (0 clears)
    verify: with the finish-early policy active, usage_advice reports the finish estimate
  - tool: usage_advice
    description: Get the one advice result — latest reading, lane choice, pace line and headroom after the safety margin, the burn figure (recent, with the weekly average and the fallback notice when it fell back), the projected end of cycle, run-out warnings, the finish-early estimate under the finish-early policy, and the plain-English advice. Advice only — the calculator never stops or slows lanes.
    args:
      variant: "(optional, one call only) pace-gauge | lane-planner | burn-ledger; omit to keep the stored usage.calculator.variant"
      policy: "(optional, one call only) use-all | pace | finish-early; omit to keep the stored usage.calculator.policy"
    verify: the response contains the advice text, the figures, and the policy / variant used
---

# Usage calculator

The **usage calculator** takes readings of a provider's weekly allowance (the kind
you read off the Claude or Gemini usage page) and tells you, in plain English, how
many lanes can safely run for the rest of the cycle under one of three spend
policies. It calculates, in one shared core, a single burn figure (the recent burn,
with the weekly average shown alongside), the projected end of cycle for each burn
line, run-out warnings, and — under the finish-early policy — a finish estimate
from your queue and per-task cost against the typed remaining work. **Advice only:
the calculator never stops, slows or re-prioritises lanes.** The operator sets the
lane count.

The design and the operator decisions behind these behaviours are on file in
[`plans/2026-10-23-usage-calculator-design.md`](../plans/2026-10-23-usage-calculator-design.md)
and [`plans/2026-10-25-usage-calculator-decisions.md`](../plans/2026-10-25-usage-calculator-decisions.md).

## The three spend policies

The policy is the spend target the advice aims at (`usage.calculator.policy`,
default `pace` unless you decide otherwise). Any renderer variant can show the
advice for any policy — they are independent.

| Policy | Spends | Advice for N lanes |
|---|---|---|
| `use-all` | Run until used up | Fit the whole remaining allowance: the largest lane count whose projected end at the recent burn is at most 100 percent. |
| `pace` (default) | Pace to the week | Last until the reset with a safety margin: the largest lane count whose projected end is at most 100 minus the safety margin (95 by default if `safety_points` is 5). |
| `finish-early` | Run flat out | Finish the remaining work before the allowance does; all lanes run, and the advice names the finish time and the deficit to make. |

The policies share one core, so they cannot disagree about the numbers — they
differ only in the target and the advice text.

## Typing readings

A **reading** is one observation of the provider's weekly allowance: percent used
and/or spend-so-far in dollars, with `at`, the optional session percent (what share
of a single session this reading covers), and the lanes that were running.

- `usage_reading_add` (MCP) · `POST /api/usage/readings` (REST) ·
  `datawatch usage reading add` (CLI) · `usage reading <percent> [session] [lanes]`
  (comm) — percent is required, spend is optional.
- `usage_readings` (MCP) · `GET /api/usage/readings` (REST) ·
  `datawatch usage reading list` (CLI) · `/usagereadings` (comm) —
  newest-first series.
- `usage_reading_delete` (MCP) · `DELETE /api/usage/readings/<id>` (REST) ·
  `datawatch usage reading delete <id>` (CLI) · `usage rm <id>` (comm) —
  drop a mistyped reading by id.

## Extra credit and remaining work

- **Extra credit** — the provider's bonus allowance on top of the weekly cycle,
  with a ceiling on the advice. `usage_extra_credit_set <points>` (MCP) ·
  `PUT /api/usage/extra-credit` (REST) · `datawatch usage extra-credit <points>`
  (CLI) · `usage extra <points>` (comm). 0 clears it.
- **Remaining work** — the typed points of work you expect to finish this cycle;
  under the `finish-early` policy the advice estimates from your queue
  (`points` × `cost_per_task_point`) against it. `usage_remaining_work_set
  <points>` (MCP) · `PUT /api/usage/remaining-work` (REST) ·
  `datawatch usage remaining-work <points|clear>` (CLI) · `usage work <points|clear>`
  (comm). 0 or `clear` clears it.

## Advice

`usage_advice` returns the one shared result: the latest reading, the lane choice
(recent lanes and the max), the pace line with the headroom after the safety
margin, the burn figure, the projected end of cycle per line, run-out warnings,
the finish-early estimate under the finish-early policy, and the plain-English
advice. It carries:

- **The recent burn, and the weekly average beside it.** When there is no
  reading within about the recent window (roughly the last two hours), the recent
  burn is unknown and the advice falls back to the weekly average burn, with a
  notice saying so. The figure you see is always the one the advice is built
  from, and you can see where it came from.
- **A run-out warning** when the policy's target will be exceeded at the recent
  burn, naming the lane count that fits.
- **The finish-early estimate** (queue × per-task cost vs typed remaining work)
  only when the `finish-early` policy is active.

Variant and policy are independent; the optional `variant` and `policy` arguments
override the stored settings **for that one call only** and are not saved.

## Where it shows up

Every figure is the same across surfaces:

| Surface | What you get |
|---|---|
| **MCP** | `usage_advice`, `usage_readings`, `usage_reading_add`, `usage_reading_delete`, `usage_extra_credit_set`, `usage_remaining_work_set` |
| **REST** | `GET /api/usage/advice`, `GET /api/usage/readings`, `POST /api/usage/readings`, `DELETE /api/usage/readings/<id>`, `PUT /api/usage/extra-credit`, `PUT /api/usage/remaining-work` |
| **CLI** | `datawatch usage advice [--variant … --policy …]` · `datawatch usage reading add/list/delete` · `datawatch usage extra-credit <points>` · `datawatch usage remaining-work <points\|clear>` |
| **Comm** | `usage` · `usage advice [variant [policy]]` · `usage reading <percent> [session pct] [lanes]` [at …] · `usage rm <id>` · `usage extra <points>` · `usage work <points\|clear>` · `/usagereadings` |
| **PWA / mobile** | the calculator lands as the PWA screen that the settings entry points to (arrives with the mobile app release on Android and iOS — see [parity-status.md](../parity-status.md)); the PWA/Android/iOS UI surfaces behind it (the three renderer variants, the reading form, the policy and variant pickers, and the safety-margin, per-task-cost, max-lanes and reset settings) are **on file and not yet shipped** — until then use MCP, REST, CLI or comm. |

## Settings

All keys live under `usage.calculator.` in `~/.datawatch/config.yaml`:

| Key | Type | Default | Meaning |
|---|---|---|---|
| `variant` | enum | `all` | Which renderer(s) to show: `pace-gauge` · `lane-planner` · `burn-ledger` · `all` (all three side by side, for the comparison period). |
| `policy` | enum | `use-all` | The spend target: `use-all` · `pace` · `finish-early`. |
| `safety_points` | int (0–50) | `5` | The pace safety margin (percentage points below 100 that the `pace` target must stay under). |
| `cost_per_task_point` | float (>0) | `0.1` | How many allowance points one task point of work costs, for the finish-early estimate. |
| `baseline_per_24h` | int (0–20) | `3` | Baseline points of burn per 24 h that the calculator assumes even with no lanes running. |
| `max_lanes` | int (1–8) | `3` | The largest lane count the advice is allowed to suggest. |
| `reset_weekday` | 0–6 | provider's weekday (`0`=Sunday) | Which weekday the allowance resets. |
| `reset_clock` | `"HH:MM"` | provider's clock (`"08:00"`) | What time it resets. |
| `reset_zone` | string | `"local"` | Timezone the reset is in. |
| `keeper.enabled` | bool | `true` | Federation keeper: this daemon keeps fresh copies of peers' calculator state so a dead owner's advice still answers. |

Set the policy and the reset the first time: the policy decides what the advice
aims for, and the reset must match what your provider actually shows.

## Variants

The **variant** is which renderer(s) draw the same advice result — independent of
the policy. The three PWA renderers are on file and not yet shipped (they ship
with the mobile app release on Android and iOS); until then the REST result body
is the single source of truth every surface reads:

- **Pace gauge** — the pace line: where the current burn leaves you at the
  reset, the headroom after the safety margin, and the lane count that keeps
  you on target.
- **Lane planner** — the per-lane finish table: for each lane count 0..max, the
  projected end of cycle at the recent burn and the target verdict.
- **Burn ledger** — the burn figure with the weekly average alongside, the
  projected reset of allowance plus extra credit, and the finish-early finish
  line when it is active.

`usage.calculator.variant` controls what the PWA shows: pick `pace-gauge`,
`lane-planner` or `burn-ledger`, or `all` (default) to see all three side by
side while you compare them against your provider's real allowance.

## Federation

Usage-calculator state is federated. A peer that grants `usage:read` gets
`usage_advice` and `usage_readings`; `usage:write` gets
`usage_reading_add`, `usage_reading_delete`, `usage_extra_credit_set` and
`usage_remaining_work_set`. Read tools that this daemon has not been granted
delegate to a granted peer; writes that this daemon is not allowed fall through
to its own store. The **keeper** holds fresh copies of peer calculator state so
a peer's `usage_advice` keeps answering after that peer goes down; results
served from the keeper's copy are marked as such.

## How it's wired

- Config schema: `docs/config-reference.yaml` § `usage.calculator` (with the
  five-method access table per key).
- MCP handlers: `internal/mcp/usage_calculator.go`.
- REST endpoints: `internal/server/http.go` (usage calculator routes).
- CLI: `cmd/datawatch/cli_usage.go`.
- Federation capabilities: `usage:read`, `usage:write` —
  `internal/federation/mcp_tool_caps.go`.
- Operator decisions: `docs/plans/2026-10-25-usage-calculator-decisions.md`.
- Design: `docs/plans/2026-10-23-usage-calculator-design.md`.
