---
docs:
  index: true
  topics: [usage, burn, guard, ladder, effort, retry, capacity, alerts, pace_line, window]
exec_steps:
  - tool: burn_guard_status
    description: Read the guard first. It shows whether it is on, the rung in force, the projected weekly percent at the reset against the pace line and margin, and the last decision
    args: {}
    read_only: true
  - tool: burn_guard_config_get
    description: Read the guard's settings (usage.burn_guard) before changing any of them
    args: {}
    read_only: true
  - tool: burn_guard_run
    description: Evaluate the guard now instead of waiting for the next interval. It may climb or step back one rung. It never ends a running session
    args: {}
  - tool: burn_guard_config_set
    description: Worked example, a state-changing call. Widen the margin to 3 points so the guard acts later. One invalid value rejects the whole call and stores nothing
    args: {margin_points: 3}
  - tool: burn_guard_config_set
    description: Worked example, a state-changing call. Allow 2 retries of one task on Claude before the task is held for you (0 to 20)
    args: {retry_limit_claude: 2}
  - tool: burn_guard_config_set
    description: Worked example, a state-changing call. Turn on ask mode so the guard also raises a question per change alongside the alert, keeping at most one open
    args: {ask_on_change: true}
  - tool: burn_guard_config_set
    description: "Worked example, a state-changing call. Set the pace_line policy mode to shadow (off, shadow, on; unset reads shadow): it computes and records the decision without touching anything"
    args: {mode: shadow}
---

# Burn guard

The burn guard watches how fast the Claude sessions the daemon runs are using
your weekly allowance. When the burn projects past the weekly pace line before
the reset, it steps down a **ladder** of changes to what new work is admitted.
When the burn is back under the line it steps back up.

**The guard changes admission only. It never ends, kills or restarts a running
session.** A session that is already running always finishes.

Beside the ladder it carries the **pace_line policy** for the weekly and the
window usage (`mode` off, shadow, on; see *The pace_line policy: the weekly
and window guard* below).

It builds on the [usage calculator](usage-calculator.md): it uses the readings
you type there for the pace line and the ceiling, so a recent reading is the
one thing it needs from you.

## Which LLMs it guards

The guard watches the `claude-code` backend, and whether it acts depends on that
LLM entry's [billing mode](llm-registry.md#set-the-billing-mode-of-an-llm):

| `claude-code` entry's `billing_mode` | What the guard does |
|---|---|
| `allowance` (the default for `claude-code`) | Guards, exactly as described below. Nothing changes for this entry. |
| `api` or `local` | **Does not act**, unless that entry has a dollar budget (`ceiling_usd` above 0). With a dollar budget it guards as it does for an allowance entry. |

An entry with no registry row at all is treated as the `claude-code` allowance,
so a daemon that has never had its LLM registry edited behaves as before.

When the guard does not apply, each tick runs as if the guard were off: it
releases every rung it had applied (admission is never left restricted), makes
no projection and shows `off` in its status. Change the entry back to
`allowance` (or give it a dollar budget) and the next tick guards again; no
restart is needed. The effort ceiling and the Claude retry limit are published
from the `enabled` setting as before, so the billing mode does not switch them
off.

## What the guard reads per LLM entry

The guard takes two inputs, both about the LLM entry it guards:

- **Your readings** in the [usage calculator](usage-calculator.md) — the
  percent of the weekly allowance used against its reset. They give the pace
  line and the advice projection the guard compares against.
- **The session transcripts** it parses each interval — input, output,
  cache-write and cache-read tokens per hour, split worker and verifier
  sessions from everything else, priced into weekly points with the two cache
  prices (refit at each new reading when `refit_on_reading` is on).

So the guard needs no billing feed of its own: it prices the same tokens the
[usage calculator](usage-calculator.md) and the [cost
report](cost-report.md) price, and the readings are the same ones you type.

The entry's **plan type** does not change the guard's math — it always works
in allowance points and pace lines on the `claude-code` entry — but it sets
the limits the guard's rungs move:

- `billing_mode` (`allowance`, `api`, `local`) decides whether the guard acts
  at all (see [Which LLMs it guards](#which-llms-it-guards)). An `api` or
  `local` entry with a dollar budget is guarded like an allowance entry; the
  budget itself is enforced at admission by the capacity ledger's budget
  backpressure, not by the guard.
- `plan_tier` (`pro`, `max_5x`, `max_20x`, `api`) sets the entry's inflight cap
  through `autonomous.plan_tier_max_inflight` and its share of a shared dollar
  ceiling through `autonomous.plan_tier_allocation_usd` (see
  [Automata capacity](automata-capacity.md)). An explicit `max_inflight` on
  the entry wins over the table; the `lower_inflight` rung lowers whatever cap
  is in force.

## Per-model cost rates and their fallback

An LLM entry's `models` list can carry its own USD rates per 1K tokens —
`in_per_k`, `out_per_k`, `cache_read_per_k` and `cache_write_per_k`. Each of
the four fields resolves on its own against the backend's `cost_rates`
entry: a field set on the model wins, a field left unset takes the
backend's value, and the cache read and write fields have no per-backend
value so they resolve to zero when unset. The resolved rate carries a source
of `model`, `backend` or none, so a surprising figure is traceable to where
it came from.

The **by-model cost report** prices each model from that model's own rate
when one is configured, else the backend's rate. It prices cache reads
and writes from the input rate (a tenth of it for a read, 1.25 times it for
a write), and labels every dollar figure it emits as an **API-equivalent
estimate, never a bill** (`api_equivalent_usd` alongside a `usd_label` to
that effect). A rate that is absent is read as the backend's, never as zero
as a price.

Set a model's rates from any of these surfaces:

| Surface | How |
|---|---|
| CLI | `datawatch llm models add <llm> --model <name> [--in-per-k .. --out-per-k .. --cache-read-per-k .. --cache-write-per-k ..]` |
| Comm channel | `llm models add <name> model=<m> [in_per_k=<usd> out_per_k=<usd> cache_read_per_k=<usd> cache_write_per_k=<usd>]` |
| MCP | `llm_add_model` with the four `*_per_k` fields |
| REST | `PUT /api/llms/{name}` with the model under `models`; the per-backend map with `PUT /api/cost/rates` |
| Web app | the LLM edit form's per-model rate fields (blank leaves the fallback) |
| YAML config | `in_per_k` and friends on each element of the LLM's `models` list; `cost_rates` for the per-backend map |

Leaving a rate unset is what falls back; an absent rate is read as the
backend's, never as zero.

> **Operator review.** The `models`-list rates above and the `cost_rates`
> table are distinct configuration surfaces. Confirm which one the by-model
> cost report is expected to read a model's rate from — the entry's
> `models[]` fields, or a `cost_rates` entry named for the model — so the
> docs and the report agree on where a model's rate comes from. The report
> labels its dollars as an API-equivalent estimate rather than a per-row
> "rate source", so the source of a figure is a configuration concern, not a
> reported field.

## The by-model reports

The cost report carries a **by-model section** (`model_rows`) alongside its
rows: one row per model name with its input, output and cached tokens and its
API-equivalent dollars, priced at that model's own rate — the per-model cost
entry when one is configured, else the backend's rate. It is an equivalence,
not a bill, and it is absent when the report has no model rows. Read it from
any surface:

| Surface | How |
|---|---|
| CLI | `datawatch cost report` — the by-model section renders under the rows |
| Comm channel | `cost report [group_by=<g>] [since=<t>] [until=<t>]` |
| MCP | `cost_report` — `model_rows` in the body |
| REST | `GET /api/cost/report` — `model_rows` in the body |
| Web app | the **Cost report** card's **By model** section |
| Script | `scripts/claude-burn.py` reads `model_rows` and prints the by-model breakdown, falling back to the rows when the section is absent |

## Adding a second paid LLM by configuration

Registering another paid LLM is configuration only — nothing to restart, and
the guard keeps doing its job on the `claude-code` entry. Every surface takes
the same entry fields:

| Surface | How |
|---|---|
| CLI | `datawatch llm add <name> --kind <kind> --billing-mode api --plan-tier <tier>` and, for a dollar limit, `--budget-ceiling-usd <n> --budget-hours <n>` (or `--budget-weekday <day> [--budget-time HH:MM] [--budget-zone <IANA>]`) |
| MCP | `llm_add` with `billing_mode`, `plan_tier` and the `budget` object |
| REST | `POST /api/llms` or `PUT /api/llms/{name}` with the same fields |
| Web app | Settings → Compute → LLM, the **Billing mode** and budget fields on the edit form |
| Comm channel | `llm add <name> billing_mode=<mode>` (budget through the other surfaces) |
| YAML config | a `llms:` entry with `billing_mode`, `plan_tier` and `budget`; see the [configuration reference](../config-reference.yaml) |

What each field does to the new entry:

- `billing_mode api` puts its spend on metered dollars in the
  [cost report](cost-report.md) — never added to the allowance entry's points.
- `plan_tier` gives it its inflight cap (`autonomous.plan_tier_max_inflight`)
  and its share of a shared dollar ceiling
  (`autonomous.plan_tier_allocation_usd`) — see
  [Automata capacity](automata-capacity.md).
- A `budget` enforces its ceiling at admission: while the window is full,
  autonomous work for that LLM waits with a `budget window full:` reason and
  resumes by itself when the window rolls over; operator-started sessions are
  never held. See [the per-LLM dollar
  budget](automata-capacity.md#per-llm-dollar-budget).
- The [usage calculator](usage-calculator.md) reads the entry's spend into its
  projections, so the new LLM's pace shows up in the advice.

The burn guard itself still watches only the `claude-code` entry: the second
LLM is bounded by its own budget, its plan-tier caps and the usage calculator,
not by the guard's ladder.

### Guarding several paid LLM entries (`entries`)

The guard evaluates **once per paid LLM entry**. With `usage.burn_guard.entries`
unset it guards one entry, `claude-code`, with the default ladder — the behaviour
above. Set `entries` to guard more than one; each entry has its own readings,
projection, pace line, ladder and held models:

| Field | Meaning |
|---|---|
| `llm` | The LLM entry name (required, unique). |
| `usage_source` | `readings` (the readings that name this entry) or `none`. **An entry with no usage source is never held**; anything it had in force is released. |
| `plan` | `flat` (a weekly percentage) or `metered` (a spend figure in dollars). |
| `period_target` | The ceiling: a percentage for `flat` (0 means 100), dollars for `metered` (required). |
| `model` | The model whose own cost rate (the per-model `in/out/cache_read/cache_write_per_k` on the LLM entry) prices the entry's tokens; empty is the entry's first model. |
| `ladder` | This entry's rungs; empty uses the guard-wide `ladder`. The route rung sends new work to `route_to`, the cheaper rung switches the entry's model, and the hold rung holds the entry. |
| `held_models` | Limit the hold rung to these models of the entry; empty holds the whole entry. |
| `route_to` | Where the route rung sends new work; empty makes the route rung change nothing. |
| `route_cheaper_model` | The model the `route_cheaper_model` rung sends this entry's new work to, on the same backend: for a Claude entry, Haiku. Empty auto-resolves the entry's own cheapest other priced model from its per-model cost rates (Haiku on a price tie for a Claude entry); when the entry has no other priced model the rung changes nothing. |
| `points_per_usd` | For a `flat` entry, converts its dollar-priced tokens to points; 0 keeps the fitted cache coefficients. |

A metered entry projects from its newest spend reading of the period, adds its
tokens priced at its model's rate, and carries the burn (priced tokens over the
trailing window, or the spend between readings) to the reset. Its margin is
`margin_points` percent of the target.

A reading names its entry with `llm` (REST body, `usage_reading_add` `llm`,
`datawatch usage reading add --llm NAME`, comm `usage reading 72 llm=NAME`, the
web reading form's LLM entry field). A reading that names no entry is the
**first** entry's, so today's single reading series keeps working. Set the list
with `datawatch config set usage.burn_guard.entries '[{"llm":"...","usage_source":"readings","plan":"metered","period_target":200}]'`
(or the same key through `PUT /api/usage/burn-guard/config`, MCP
`burn_guard_config_set`, or the comm `configure` verb). The status
(`GET /api/usage/burn-guard`) carries one `entries[]` block per entry with its
plan, usage source, rungs in force, `held_models`, `route_to` and the
resolved `cheaper_model`; the top-level fields repeat the first entry's.

## What it does each interval

1. Reads the session transcripts and counts input, output, cache-write and
   cache-read tokens per hour, split between worker and verifier sessions and
   everything else.
2. Turns tokens into points of the weekly allowance with two prices, points per
   million cache-write tokens and points per million cache-read tokens. When
   `refit_on_reading` is on, each new usage reading refits both prices against
   the points used between readings, so the model follows what your provider
   actually charges. A poor fit shows in the status as a fit error.
3. Builds the projection (see below): the token-priced burn, checked against
   the readings-only advice, and compares the result with the target.
4. Moves **at most one rung** up or down the ladder.

With no usable reading yet, the guard holds and the status says what it needs.

## How the projection is built

The guard works from your readings first and uses the token estimate to
carry the readings forward between them.

1. **The base is the newest reading.** Its percentage is the starting point,
   and the pace line is the line at that reading.
2. **The points since that reading.** The tokens used between the newest
   reading and now are priced with the two cache prices. The sum is then
   multiplied by the **calibration ratio** (below). The base plus this sum is
   the estimate of the burn right now.
3. **Two burn rates, blended.**
   - The **reading rate** is the points that moved between the last two
     readings, per hour. It is not measured when the percentage fell (a reset
     between them) or when the readings are less than 15 minutes apart.
   - The **token rate** is the token estimate since the newest reading, scaled
     by the calibration ratio, per hour.
   - When both are measured, the burn is their even blend (half each). When
     only one is measured, that one is used.
4. **The projection to the reset.** The estimate now plus the blended burn
   per hour times the hours left to the reset. This is the **guard
   projection**, shown as `Projected` on the status and in each decision.
5. **The advice projection.** The readings-only projection from the usage
   calculator, on the recent burn between readings alone. This is the
   **advice projection**, shown as `AdviceProjected`.
6. **The gap.** The absolute difference between the two projections. It is
   compared with `max_projection_gap_points` (see below).

**The calibration ratio** says how far the token estimate sits from what your
readings actually moved. It is the points the readings moved, divided by the
token estimate for the same intervals, across the newest 6 intervals between
readings. It is held between 0.5 and 2.0, and it is 1 (no scaling) until there
are at least two intervals to compare. Because the ratio scales the token
estimate, a token estimate that runs low or high against your readings is
corrected by the readings themselves.

The target is the ceiling less the safety margin, and the guard compares the
projection with it. The status and each decision carry the target, the pace
line, the burn per hour and the allowed burn per hour, so you can see the
numbers the guard used.

## The projection gap (`max_projection_gap_points`)

The two projections should roughly agree. When they do not, the gap setting
decides which one the guard acts on.

- **Within the limit** (or the limit is 0): the guard projection decides.
- **Gap above the limit:** the tick decides on the **lower** of the two
  projections, so a token estimate that runs much higher than the readings
  cannot push the guard into a rung on its own.
- **Never skips a rung.** The lower value can hold the guard back, but it
  cannot stop a rung the guard calls for. When the guard projection is over
  the target and the newest reading is already above the pace line, the tick
  still climbs one rung. And a tick never moves more than one rung either way.

The default is **25 points**. A value of **0 never compares** the two
projections: the guard projection alone decides, whatever the gap. The range is
0 to 100. A value outside that range, or one that is not a number, is
**refused** when you set it, and nothing is stored. A saved value that is out of
range is replaced by the default on load, with a warning in the daemon log.

## What the decision records

Every evaluation records a decision, and each one carries both numbers:

- `projected` and `guard_projected`: the guard projection.
- `advice_projected`: the readings-only projection.
- `gap`: the absolute difference between them.
- `line` (the target), `over_by` (the excess the tick decided on, which is the
  lower projection's when the gap was over the limit), `points_per_hour` and
  `allowed_per_hour`.

The alert names both projections and which one was used, for example
`guard 173.7%, advice 57.0%, using the lower`. The status shows the same fields.

## The ladder

The ladder is an ordered list of rungs. Each rung is a change to admission of the ladder's **entry** (the paid LLM it
watches), not of any fixed backend. The rung names are entry-generic: a guard
with several entries applies the same ladder under each entry's own admission.

| Rung | What it does |
|---|---|
| `lower_inflight` | Lowers how many sessions the entry may have running at once. The original cap is remembered and restored when the rung is released, but only if the cap is still where the guard left it: a value you set after the lowering (a higher cap, or any other) is kept, and the step-back alert says so. The pool limit always follows the entry: once you change the entry's cap, the guard's lowered value is dropped and the new cap applies |
| `route_entry` | Sends the entry's new work to the entry's `route_to` backend instead, when one is set (the legacy first-version name for this rung is `route_pi_prompt`) |
| `route_cheaper_model` | Keeps the backend and sends the entry's new work to its own **cheaper model** (the entry's `route_cheaper_model` field, or its cheapest other priced model by its per-model cost rates when the field is empty). While the entry resolves to no cheaper model the rung changes nothing. Work already running continues; releasing the rung restores the entry's original model routing |
| `hold_entry` | Holds the entry's new work, optionally limited to its `held_models`. Work already running continues; held work starts again when the rung is released (the legacy first-version name is `hold_claude`) |

The default ladder is `lower_inflight`, `route_entry`, `hold_entry`, in that
order. `route_cheaper_model` is the fourth rung and is **not in the default**: it
is a no-op until the entry resolves a cheaper model, so you opt in by naming it
where it sits, before the hold rung (for example
`lower_inflight, route_entry, route_cheaper_model, hold_entry`). You can reorder
the rungs or leave a rung out (for example `lower_inflight`, `hold_entry`). An
unknown or repeated rung is dropped, and an empty list means the default. The
legacy first-version names `route_pi_prompt` and `hold_claude` are still
accepted in saved configs and normalize to `route_entry` and `hold_entry`.
**Climbing.** When the projection is more than `margin_points` above the target,
the guard applies the next rung. **Holding.** Inside the margin it changes
nothing. **Stepping back.** When the projection is more than `margin_points`
below the target and `step_back` is on, it releases the last rung it applied,
one per interval, in reverse order. With `step_back` off, rungs stay in force
until you release them (turn the guard off, which releases everything at once).

Turning the guard off (`enabled: false`) releases every rung at once, so
admission is never left restricted by a guard that is off.

## The pace_line policy: the weekly and window guard

Beside the ladder, the burn guard carries the **pace_line policy** for the
weekly and the window usage. Its six settings sit in the same
`usage.burn_guard` block:

- **`mode`** — `off` decides nothing; `shadow` computes the decision and
  records it with its evidence without touching admission; `on` holds new
  work at the window pause line and at the weekly pace line, and lifts the
  hold when the reading drops or the window or the week resets. An unset mode
  reads `shadow` — shadow first, then on.
- **`week_tolerance`** — how many points the week reading may run over the
  pace line and still be ok. Whole number 0 to 10, default `2`.
- **`points_to_week_factor`** — one point of ledger burn counts as this many
  points of the weekly allowance. 1 to 10, default `1.3`.
- **`window_percent_per_point`** — one point burned is this percent of the
  current window. 1 to 25, default `4`.
- **`window_stop_percent`** — a window is over at this percent used. 50 to
  95, default `85`.
- **`reading_freshness_minutes`** — a reading older than this is stale, and
  the guard asks you for a fresh one. Whole number 1 to 1440, default `60`.

The **window anchors** are fixed behavior, not a setting: windows end at
02:00, 07:00 and 12:00, then every 5 hours.

The six keys are read and written on the same six surfaces as the other
`usage.burn_guard` keys (YAML, web app, CLI, REST, MCP and the comm channel),
and they share the same rule: one invalid value rejects the whole change and
nothing is stored, and a saved value that is out of range falls back to its
default on load, with a warning in the daemon log.

## How the pace policy decides

Each interval, after the ladder, the guard runs the pace policy once for every
guarded Claude entry. It works from the readings as they stand, not from the
projection to the reset, and it has two lines:

- **The window pause line.** The window estimate is the newest session (window)
  reading plus the points burned since that reading times
  `window_percent_per_point`. When it reaches `window_stop_percent` (85 by
  default) new work is held. A session reading from before the current window
  began belongs to the finished window, so only the burn since the window began
  counts: the window reset lifts a window hold with no new reading.
- **The weekly pace line.** The week estimate is the newest weekly reading plus
  the points burned since it times `points_to_week_factor`. When it is more than
  `week_tolerance` points above the pace line now, new work is held. A newer,
  lower reading, or the weekly reset, lifts the hold.

The ladder and the policy work together. The ladder is graded: from the
projection it lowers the entry's lanes (never below one lane, so with one lane
that rung changes nothing), routes the entry's new work to its route target, and
only at its last rung, `hold_entry`, holds. The policy is the hard line: at the
pause line or the weekly pace line it holds new work whatever rung the ladder
is on. Both read the same readings and the same transcripts.

**What a hold does.**

- It makes new worker admission on that entry's pool wait. The wait reason
  reads *backend claude-code backed off by the burn guard pace hold until
  HH:MM: ...* and the task waits, it does not fail.
- **Running work finishes.** A task that is already running keeps its session,
  and its verifier and judge are not held.
- **Operator sessions and supervisor sessions are never held.** They start the
  way an operator start does, and the hold does not apply to them. They are
  accounted in the usage, not limited by the hold.
- The hold has a deadline of two guard intervals that the guard renews on every
  tick that keeps it. If the guard stops ticking, the hold lapses by itself.
- The hold is its own flag. The ladder's `hold_entry` rung and a rate-limit
  back-off keep theirs, and none of the three lifts another.

**Modes.**

| `mode` | What happens |
|---|---|
| `off` | Nothing is decided. A hold left in force is released. |
| `shadow` (the default in code) | The decision is made and recorded with its evidence as `would_hold` or `would_release`. Admission is never touched. |
| `on` | The same decision holds and lifts new work, and each change raises an alert that says how many running sessions were left alone. |

Leaving `on` (to `shadow`, to `off`, or turning the guard off) releases the hold
at once, so admission is never left restricted by a policy that is not
enforcing.

**Where the decisions show.** Every decision that changes the verdict or the
hold is kept with the numbers it was made on: the two readings and their age,
the burn since each, the estimates, the pace line, the tolerance, the window
bounds and the pause line, the rule, the mode and the action. They show in:

- the `pace` block of the guard status (`GET /api/usage/burn-guard`, the
  `burn_guard_status` tool, `datawatch usage burn-guard status`), which also
  carries the log of the last 100 decisions and the hold of each entry;
- the **Burn guard** card on the usage page of the web app (mode, what is held
  and on which rule, the five newest decisions);
- the usage line of the dashboard console;
- the chat reply to `usage burn-guard`;
- `burn_guard_pace.jsonl` in the usage data directory, one JSON line per
  decision, which survives a restart (the status shows the newest 100 after one).

A reading older than `reading_freshness_minutes` does not stop a decision: the
estimate already adds the burn since the reading. The decision is flagged
*stale* and its reason asks you for a fresh reading.

**Before you switch from shadow to on.** Let the policy run in shadow for a few
days, then read the log. Check that each `would_hold` was a moment you would
have wanted new work held, and that the lift came when you expected. Check that
the window anchors above match how your provider's windows run (the last window
of the day, 22:00 to 02:00, is four hours), and that `window_stop_percent`,
`window_percent_per_point`, `week_tolerance` and `points_to_week_factor` fit the
allowance you are pacing. Then set `mode` to `on`; no restart is needed. To
stand down at any time, set `mode` back to `shadow`: the hold is released on the
next tick, or run the guard now to apply it at once.

## Settings

All settings are under `usage.burn_guard`. Each is applied to the next
evaluation with no restart, and one invalid value rejects the whole change.

In the web app the settings live in **Settings → General → Automata**, no longer
on the usage page; the page keeps a read-only Burn guard status and its **Run now**
button, plus a **Burn guard settings** link that opens that section. Only the
place moved. The same keys are still read and written on all six
surfaces (YAML, web app, CLI, REST, MCP and the comm channel), and every key name
is unchanged, so a script or config file that used the old keys keeps working.

| Setting | Meaning | Default |
|---|---|---|
| `enabled` | Guard on or off | `true` |
| `interval_minutes` | Minutes between evaluations, 1 to 1440 | `60` |
| `margin_points` | How far the projection may run over the pace line before the guard acts, 0 to 50 | `2.0` |
| `max_projection_gap_points` | Largest allowed gap between the guard projection and the advice projection. Above it the tick decides on the lower of the two. `0` never compares, so the guard projection alone decides. 0 to 100; a value outside that range is refused | `25` |
| `ladder` | Ordered rungs applied while over the line | `lower_inflight, route_entry, hold_entry` (allowed rungs: `lower_inflight`, `route_entry`, `route_cheaper_model`, `hold_entry`, plus the legacy `route_pi_prompt` and `hold_claude`) |
| `step_back` | Release rungs in reverse once back under the line | `true` |
| `ask_on_change` | When false (default) one alert per change and no question; when true the per-change alert still goes out and at most one open question per change is kept | `false` |
| `cache_write_points_per_million` | Weekly points per million cache-write tokens, above 0 and at most 1000 | `0.373` |
| `cache_read_points_per_million` | Weekly points per million cache-read tokens, above 0 and at most 1000 | `0.0069` |
| `refit_on_reading` | Refit the two prices when a new reading arrives | `true` |
| `effort_ceiling` | Task kind to ceiling (see below) | `docs`, `parity`, `live_check`, `final_gate` all `low` |
| `retry_limit_claude` | Most retries of one task on Claude, 0 to 20 | `2` |
| `mode` | The pace_line policy mode: `off` (decides nothing), `shadow` (computes and records the decision with its evidence, holds nothing), `on` (holds new work at the window pause line and the weekly pace line, lifts the hold when the reading drops or the window or the week resets). An unset mode reads `shadow` | `shadow` |
| `week_tolerance` | How many points the week reading may run over the pace line and still be ok, whole number 0 to 10 | `2` |
| `points_to_week_factor` | One point of ledger burn counts as this many points of the weekly allowance, 1 to 10 | `1.3` |
| `window_percent_per_point` | One point burned is this percent of the current window, 1 to 25 | `4` |
| `window_stop_percent` | A window is over at this percent used, 50 to 95 | `85` |
| `reading_freshness_minutes` | A reading older than this is stale and the guard asks you for a fresh one, whole number 1 to 1440 | `60` |

An invalid value in a saved config falls back to its default with a warning in
the daemon log; the daemon still starts.

`ask_on_change` picks how the guard reaches out about a change. When false
(the default) it sends one alert per change and no question. When true it
keeps at most one open question per change until you answer it, alongside
the per-change alert.

### The question on top of the alert (`ask_on_change`)

An alert goes out every time the guard applies or releases a rung; this
setting decides whether a question then asks you to keep or release the
change.

- **`false` (the default) — one alert per change, no question.** The guard
  applies or releases the rung, warns you with the numbers behind it, and
  asks nothing of you. Each such alert also carries the one-line suggestion
  *Set `usage.burn_guard.ask_on_change` to true to get a question per
  change*, so you can turn the questions on directly from the alert.
- **`true` — the guard asks.** For every step that actually changed a setting
  it holds **at most one open question**, worded like *"The weekly burn
  projects to 104.2% at the reset against a target of 90.0%. I did this:
  lowered the claude-code inflight cap from 4 to 3. Release it, or leave
  it?"* with the answers **Leave it as is** and **Release**. Answering
  *Release* records your decision; the guard owns the rung and releases it
  itself when it steps back. If a new rung change arrives while a question
  is still open, the guard withdraws that open question first — the
  withdrawal reason says it was *superseded by a newer burn guard rung
  change* — and raises the question for the new step, so a staircase of
  changes leaves you with one question, not a pile of them.

A step raises a question only when it changed a setting. A hold (changed
nothing), a `lower_inflight` rung whose cap was already at one, a release that
would restore a cap that was never lowered, a step that only releases the
hold (a `hold_entry` release), and turning the guard off all alert but ask
nothing. Two more withdrawals keep you from ever answering
a question that no longer stands: if you turn `ask_on_change` off again, the
next change withdraws the last open question (reason: *ask_on_change is off,
so the burn guard alerts only*), and when the daemon starts, any guard
question a previous run left open that the current ladder state no longer
supports is withdrawn as stale (reason: *at start the burn guard no longer
applies the rung this question refers to*). You can set `ask_on_change` from any row of the *Where to change them* table
below, without a restart — the guard reads the current value every interval.

### Where to change them

| Surface | How |
|---|---|
| YAML (config file) | `usage.burn_guard.ask_on_change: true` under the `usage.burn_guard` key; see the [configuration reference](../config-reference.yaml) |
| Web UI | the read-only **Burn guard** card on the usage page, with **Run now** to evaluate once and a **Burn guard settings** link that opens the section below; the guard's settings (**Ask instead of alerting** is `ask_on_change`) are in **Settings → General → Automata**; the **Burn guard** stat card in **System Statistics** on the **Observer** view (showing current rung and state when the daemon is reporting guard state) |
| CLI | `datawatch config set usage.burn_guard.ask_on_change true` (switch it back with `false`); `datawatch usage burn-guard status`, `run`, `config [key=value ...]`; `datawatch automation set-effort-ceiling <id> docs=low ...` (or `--clear`) for the effort ceiling |
| MCP | `config_set` with `key: usage.burn_guard.ask_on_change`, `value: true`; `burn_guard_status`, `burn_guard_run`, `burn_guard_config_get`, `burn_guard_config_set` |
| Chat | `configure usage.burn_guard.ask_on_change=true` |
| REST | `PUT /api/config` with `{"usage.burn_guard.ask_on_change": true}`, or `PUT /api/usage/burn-guard/config` with `{"ask_on_change": true}`; `GET /api/usage/burn-guard`, `POST /api/usage/burn-guard/run`, `GET`/`PUT /api/usage/burn-guard/config`; `GET /api/config` returns the block under `usage.burn_guard`; `POST /api/automations/{id}/set_effort_ceiling` for the effort ceiling |
| Comm channel | `usage burn-guard` for the status; settings through `configure usage.burn_guard.<key>=<value>` |

## Reading an alert

Each time a rung is applied or released, the guard raises one warning alert
titled `Burn guard: climb <rung>` or `Burn guard: step_back <rung>`. With
`ask_on_change` on, it additionally raises the keeping/releasing question
described above; off, the alert is the whole notice. The alert text states
what was done and the numbers behind it, and (with `ask_on_change` off)
carries the one-line suggestion to set `usage.burn_guard.ask_on_change` to
`true` to get a question per change:

> Burn guard stepped up to rung 1 of 3 (lower_inflight): lowered the claude-code
> inflight cap from 3 to 2. (projected 31.4% at the reset, target 28.0%, burning
> 0.95 points/hour against 0.80 allowed) 2 running session(s) were left alone.

Read it from left to right:

- **Rung n of m** is how far down the ladder you are. Rung 3 of 3 means the
  last rung is in force and the guard has nothing further to do.
- **What was done** is the admission change in plain words.
- **Projected** is the weekly percent the burn reaches at the reset. **Target**
  is the ceiling less the safety margin of the usage calculator.
- **Burning ... against ... allowed** compares points per hour now with the
  points per hour that would land exactly on the line.
- **Running sessions were left alone** confirms nothing running was touched.

The question asks whether to release the change or leave it. It does not block
anything. If a step cannot raise its question, the status shows the error and
the change stays in force.

## Stepping back

To step back yourself:

- **Wait.** With `step_back` on, the guard releases one rung per interval once the
  projection is under the line by more than the margin.
- **Run now.** `burn_guard_run` (or **Run now** in the card) evaluates at once; it
  still moves at most one rung.
- **Raise the margin or reorder the ladder** if the guard acts too early or on a
  rung you do not want. Take `hold_entry` out of the ladder to keep it from
  ever pausing new work on the entry it guards.
- **Turn it off.** `enabled: false` releases every rung immediately.
- **Type a fresh reading** in the usage calculator. The pace line and the fit
  both move with it, and a reading that shows the week is lower than projected
  lets the guard step back.

### Where the rung and its reason show

While a rung holds a backend, the daemon lists it as an active back-off flag
next to the pools and budgets. You see the rung name next to its reason in
three places:

- **`datawatch capacity`** prints each active back-off flag; a flag the guard
  set reads `claude-code: resets <time> (burn guard rung <rung>: <reason>)`,
  and a flag without a rung (a rate limit or an operator set) keeps the
  plain `(<reason>)` form. The reset time is shown in the zone the daemon
  sent it in.
- **The comm channel** `capacity` verb answers with the same text.
- **The web UI** capacity card shows the rung as a small label next to the
  backend name, with the reason on its own line below.

The hold ends on its own when the guard steps back, at the next reading, or
at the weekly reset. Until then the flag stays in the list — its presence is
the clearest sign that something the guard did is still restricting
admission. While a burn-guard hold is up the backend behaves like any
usage back-off: new **worker** sessions are held, and the review roles
(guardrail judges, verifiers, planning) may still run on it through its
[review reserve](usage-backoff.md#what-a-flag-does-not-hold-the-review-reserve).

## The effort ceiling

The effort ceiling caps how much effort a kind of task may use, so routine
tasks do not spend heavy-model effort. It only ever lowers effort or moves the
task to the cheaper backend; it never raises anything, and an effort already at
or under the ceiling is kept.

Ceilings are `low`, `medium`, `high` and `cheaper_backend`. The default caps the
kinds `docs`, `parity`, `live_check` and `final_gate` at `low`. A task with no
effort set takes its ceiling. Most specific wins: the task's own ceiling, then
the Automaton's per-kind map, then the daemon-wide map under
`usage.burn_guard.effort_ceiling`. With the guard disabled every ceiling is a
no-op.

The `cheaper_backend` ceiling does not lower the effort level; it routes the
task to the cheaper backend — **`pi-prompt`** — instead, so the work still
runs, on the cheap model. The same routing is what the Claude retry limit
applies to a task it gives up on, so the cheaper rung of the guard is always
the same destination.

**`final_gate` is inert today.** The built-in final gate no longer runs as a
session — the daemon's
[script gate](automaton-decompose-lint.md#the-script-gate-the-final-gate-no-longer-spends-a-session)
performs it — so there is no session for the ceiling to cap. The setting
remains valid (`final_gate` stays in the allowed kind list); it only has an
effect on a `final_gate`-kind task that ever runs as a session again.

The ceiling has its own tools rather than the generic settings call:

- `automation_set_effort_ceiling` sets an Automaton's per-kind map (an empty map
  clears it), for example `{"docs":"low","final_gate":"cheaper_backend"}`.
- `autonomous_prd_set_task_effort_ceiling` sets one task's ceiling (empty clears
  it).
- The comm channel verb is `automation set-effort-ceiling <id> docs=low ...` (or `clear=true`), the CLI (`datawatch automation set-effort-ceiling <id> docs=low`, `--clear` to remove) and REST (`POST /api/automations/{id}/set_effort_ceiling`) carry the same call, and the PWA card shows the
  daemon-wide ceilings read-only.

## The Claude retry limit

`retry_limit_claude` is the most times one task is retried on Claude (default
2, 0 to 20). A task that has used its retries is **held for your review**, not
failed, and, with the operator question channel on, you get one question naming
the task with two answers:

- **Retry the task now** runs it again. Raise `retry_limit_claude` first, or it is
  held again at the same limit.
- **Cancel the task** cancels it; tasks that depend on it do not run. This is the
  default.

This stops a task that keeps failing from spending the week's allowance on
repeat attempts.

## Troubleshooting: the guard and the advice disagree

The usage calculator's advice can say the week is under the line while the
guard holds a rung, or the reverse. Check these in order:

1. **Read both numbers.** Run `burn_guard_status` (or `datawatch usage
   burn-guard status`) and compare `projected` with `advice_projected`. The
   alert names the one the tick used.
2. **Check the gap against the limit.** If `gap` is above
   `max_projection_gap_points`, the tick used the lower projection. If the
   guard still holds a rung, the lower projection was still over the target,
   or the newest reading was above the pace line.
3. **Check the newest reading.** A reading typed long ago leaves the token
   estimate to carry the whole week forward, and the two projections drift
   apart. Type a fresh reading in the usage calculator, then run the guard.
4. **Check the calibration.** A ratio at the 0.5 or 2.0 bound means the token
   estimate and the readings disagree strongly. Check the cache prices
   (`cache_write_points_per_million`, `cache_read_points_per_million`) and
   `refit_on_reading`. Turning `refit_on_reading` on refits both prices at each
   new reading, which is usually the fix.
5. **Decide the policy.** If you trust the readings more than the token
   estimate, lower the gap (the lower projection then decides more often).
   Setting it to `0` makes the guard use its own token projection alone, which
   is the right choice only when you trust the price model.

The guard still never ends a running session, whichever projection it uses.

## What the guard does not do

- It does not end, kill or restart a running session.
- It does not change a lane count you set; the lane count stays yours (see the
  [usage calculator](usage-calculator.md)).
- It does not run without readings; type one to give it a pace line.

## Parity

The surfaces this page added or documented in this change:

| Surface | What it now carries |
|---|---|
| **YAML / config** | `llms.<name>` entries: `billing_mode`, `plan_tier`, `models[].in_per_k` / `out_per_k` / `cache_read_per_k` / `cache_write_per_k`, `cost_rates`, `budget` |
| **REST** | `POST /api/llms`, `PUT /api/llms/{name}` (entry fields); `PUT /api/cost/rates` (per-backend map); `GET /api/cost/report` (`model_rows`); `GET /api/budget`, `GET /api/llm-roles` for reading |
| **MCP** | `llm_add`, `llm_update`, `llm_get`, `llm_list` (entry fields); `llm_add_model` (per-model rates); `cost_report` (`model_rows`); `budget_status` |
| **CLI** | `datawatch llm add\|update` (`--billing-mode`, `--plan-tier`, `--budget-*`); `datawatch llm models add` (per-model rate flags); `datawatch cost report` (by-model section); `datawatch budget status` |
| **Comm channel** | `llm add \| update <name> billing_mode=<mode>`, `llm models add` (rate keys), `cost report`, `budget` |
| **Web app** | Settings → Compute → LLM: **Billing mode**, plan tier, per-model rate fields, budget; the **Cost report** card's **By model** section |

Untouched: the guard's own settings (all still under `usage.burn_guard`, same
key names), the ladder, the status/alert/question behaviour, and the
Android/iPhone apps, which track parity separately in the `datawatch-app`
repository.

## See also

- [Usage calculator](usage-calculator.md)
- [Automata capacity](automata-capacity.md)
- [Configuration reference](../config-reference.yaml) (`usage.burn_guard`)
- [Parity status](../parity-status.md)
