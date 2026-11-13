---
docs:
  index: true
  topics: [usage, billing, lanes, policy, advisor, capacity, federation]
exec_steps:
  - tool: usage_readings
    description: Look at the readings series first (newest first), so you know where the week stands before typing a new reading
    args: {limit: 10}
    read_only: true
  - tool: usage_reading_add
    description: Worked example, a state-changing call. Type a reading, 42 percent of the weekly allowance used. The daemon stamps it with the Claude lanes running in the ledger. Use your own provider figure in real use
    args: {percent: 42}
  - tool: usage_advice
    description: Ask whether you are within or over budget for the week and what each lane count costs against the rest of it. Advice only, nothing is stopped or slowed
    args: {}
    read_only: true
  - tool: usage_extra_credit_set
    description: Optional, state-changing. Enter bought extra credit in points (0 clears it); it is added to the ceiling as a separate figure
    args: {points: 0}
  - tool: usage_remaining_work_set
    description: Optional, state-changing. Type the remaining work in points to overwrite the computed estimate for the run-flat-out policy (0 clears it and the estimate is used again)
    args: {points: 0}
---

# Usage calculator

The usage calculator answers two questions about your provider's weekly
allowance:

1. Are you **within or over budget for the week**?
2. What does **each lane count cost against the rest of the week**, so you can
   choose how many lanes to run?

It takes readings you type, stamps each one with the Claude lanes the daemon
sees running, and returns advice. **It only advises. It never stops, slows or re-prioritises a lane.**
The lane count is always yours to set.

## The three policies

The policy is the target the advice aims at. **Which policy to follow is the
operator's choice**, and you can change it at any time.

| Policy | Name on screen | What the advice aims for |
|---|---|---|
| `use-all` (default) | Use it all | The largest lane count that still ends the week at or under 100 percent of the allowance (plus any extra credit). |
| `pace` | Pace to the week | The largest lane count that ends the week the safety margin below 100 percent, so the allowance lasts to the reset. |
| `finish-early` | Run flat out | All lanes run; the advice says when the remaining work finishes against when the allowance runs out. |

Set it with `usage.calculator.policy`. A single `usage_advice` call can also
pass `policy` to look at another policy without saving it.

## Typing a reading

A reading is one look at your provider's usage page.

- **Percent** of the weekly allowance used (or the spend in dollars; give
  exactly one of the two).
- **Session percent** (optional). The share of the current session window used.
  It is shown as a secondary line only and **has no verdict of its own**; the
  weekly figure is what sets lane counts.
- **At** (optional). When you read it; the default is now.

You do not type the lane count. The daemon stamps each reading with the Claude
lanes running in the capacity ledger, so the burn per lane can be worked out
from a count you cannot mistype. A `lanes` value sent by an older script or tool
is accepted and ignored.

A mistyped reading can be deleted by its id (`usage_reading_delete`); the advice
recomputes from the rest.

## Lanes running, stamped and what-if

Three lane numbers appear in the advice, and they are kept apart.

- **Claude lanes running now** is the count in the capacity ledger at the moment
  of the call (`lanes_running_now` in the JSON). It is `null`, shown as "not
  known", when no ledger is wired on the daemon.
- **Stamped lanes** is the count the daemon wrote on each reading when it was
  stored (`lanes` on the reading). The advice works the burn per lane from the
  newest reading that carries a usable stamp.
- **What-if lanes** is the count you ask the advice to plan with. It is set on
  the advice only, it is **not saved**, and it never changes how many lanes are
  running. Leave it off and the advice uses the lanes running now.

Each stamp carries a `lanes_source` that says where the count came from:

| `lanes_source` | Meaning |
|---|---|
| `live` | The reading is at the clock; the count is the ledger count now. |
| `recorded` | The reading is from an earlier time that the lane history covers; the count is the one recorded then. |
| `current` | The reading is from an earlier time the lane history does not cover, so the ledger count now is used. |
| `unavailable` | No ledger is wired, so the reading has no lane count (`lanes` is empty). |
| `typed` | A reading stored before the stamp existed that carried a typed count. The value is kept as it was and never feeds the advice. |

The keeper of the series does the stamping. A peer sends its reading to the
keeper, which stamps it from its own ledger.

## The weekly reset

The reset is **Sunday 08:00** in the configured reset zone, entered as a
weekday, a local clock time and a time zone name (the keys
`usage.calculator.reset_weekday`, `reset_time` and `reset_zone`; match them to
what your provider actually shows).

By default `reset_zone` is *empty*, which means **follow the zone the daemon
runs in** — an example being Sunday 08:00 in `America/New_York` on a daemon
whose zone is `America/New_York`. The zone is resolved when the reset runs,
in this order: the `TZ` environment variable, then the zone named in the
system local-time symlink (`/etc/localtime` into `zoneinfo/Area/City`), then
the Go runtime's local zone. The resolved zone is a proper IANA zone name,
so it follows the zone's own rules: a fixed offset such as `+05:00` is wrong
for half the year once daylight saving time starts or ends, but a zone name
keeps the countdown correct across the change.

If no zone can be resolved on the host, the reset **falls back to UTC**: the
reset runs in UTC and the advice carries a note saying the zone fell back to
UTC and pointing at `usage.calculator.reset_zone`, so you can pin it. That
note appears in every rendering (CLI, comm channel, JSON, PWA).

An **explicit zone name always wins**, a plain `UTC` included: whatever
IANA name you set is used as-is and is never rewritten to the server's zone.
`Local` is refused because it is just the server's zone under another name,
which is what the empty value already means.

**Where to see the effective zone.** When the config's zone is empty, the
surfaces show the *effective* zone marked as the server's zone, not a blank
value:

| Surface | What it shows |
|---|---|
| PWA (settings → “Usage calculator reset time zone”) | the input stays empty, the effective zone appears as placeholder text, and a note says the zone is the server's (empty in the config) |
| `GET /api/config` | `usage.calculator.reset_zone` = the effective zone, with `reset_zone_source` of `server` (or `configured` when you set it explicitly) and `reset_zone_fell_back` `true` on the UTC fallback |
| `datawatch config get usage.calculator.reset_zone` | the zone with a marker, e.g. `America/New_York (server zone)` when server-resolved, or just the zone name when set explicitly |
| Calculator advice (text + JSON) | the text forms put `(server zone)` on the reset time; the JSON carries `zone` plus `"server_zone": true` |

## The 2-hour notice

The recent burn is worked from your last two readings. If there is **no reading
in the last 2 hours**, the calculator falls back to the **week average** and
**says so on the screen**, so you know the figure is smoother than the real
recent burn. Type a fresh reading to get the recent burn back.

## The pace-to-the-week safety margin

Under the `pace` policy the target sits `usage.calculator.safety_points` below
the pace line. The default is **5 points** (0 to 50). Raise it to leave more
room, lower it to spend closer to the line.

## Extra credit

If you buy extra credit, type it with `usage_extra_credit_set`. It is added to
the ceiling (100 plus the extra points) and the verdict and run-out figures use
that ceiling. The screen shows the extra credit as a **separate line**, so you
can always see what is allowance and what is bought.

## Remaining work for run-flat-out

The `finish-early` policy needs to know how much work is left. There are two
sources and the screen always shows which one is in use:

- **Computed estimate.** The queued Automata tasks times the cost-per-task
  setting (`usage.calculator.cost_per_task_points`, default 0.1), labelled as
  an estimate.
- **Typed figure.** Type a number with `usage_remaining_work_set` and it
  **overwrites** the estimate and is labelled as typed. Typing 0 removes it and
  the estimate is used again.

The number of queued tasks is shown beside both. Treat the cost per task as a
starting figure and calibrate it against real readings.

## The point value (estimate)

The advice always carries a **point-value line**: the estimated value of one
usage point and of a full week of the allowance, with the source it came from —
**the claude-code billing profile** when you set one (a `usage.billing`
entry, typically a `bundled_plan` with its `monthly_price`) and **the assumed
default claude-code profile** when you did not. It is an
**API-equivalent estimate, not a bill**: the money is price-derived from the
profile's `monthly_price` (assuming the allowance is fully used each week),
never a metered spend, and where the profile has no `monthly_price` the
figures read zero rather than inventing a number. The cost report shows the
same profile's money as the row's `api_equivalent_usd`, `estimated_usage_points`
and `share_of_monthly_price` rows, so the two surfaces agree by construction.

See the field reference in [api/cost.md — Billing profiles](../api/cost.md#billing-profiles-usagebilling) and the
[billing profile definition](../datawatch-definitions.md#billing-profile-usagebilling), and
[how a cost-report row reads it](cost-report.md#the-billing-profiles).

## Unit labels on the usage page

The usage advice names, for each backend, the unit its figures are labelled in,
read from the LLM entry's
[billing mode](llm-registry.md#set-the-billing-mode-of-an-llm):

| Entry `billing_mode` | Unit label key | English text |
|---|---|---|
| `api` | `usage_unit_dollars` | `$ (dollars)` |
| `allowance` | `usage_unit_points` | `pts (points)` |
| `local` | `none` | no unit shown |

`GET /api/usage/advice` (and the MCP `usage_advice` tool, which returns the same
body) carries them as `unit_labels`, a map of LLM name to label key, next to the
usual advice fields. The map has one key
per LLM in the registry, and always `claude-code` (points unless its entry says
otherwise). The two label keys are locale keys in all five locales; `none` is not
a key and has no text. The calculator's advice itself is unchanged: it is still
points of the weekly allowance, and only the labels follow the billing mode.

**The web usage cards do not draw these labels yet.** They fetch the same advice
payload but do not read `unit_labels`, so today the field is for REST and MCP
clients only. Until the cards read it, the cards show what they showed before.

## The billing profile (usage.billing)

The point-value line above is driven by one setting the calculator reads:
**`usage.billing.claude-code`**, the billing profile you keep for the
`claude-code` provider. The profile describes *how you pay* for a provider,
per provider key (`usage.billing.<provider>.<field>`), where the key is the
provider name from your LLM registry — `claude-code`, `openai`, `gemini`,
`ollama`, or any other name without dots.

The profile's `mode` is the fallback for an LLM entry that has no
[`billing_mode`](llm-registry.md#set-the-billing-mode-of-an-llm) of its own; an
entry's value wins over it (see [how a cost-report row reads it](cost-report.md#the-billing-profiles)).

The calculator uses the `claude-code` entry for two things: the **point
value** (from `monthly_price`) and, when the profile names a reset, the
**allowance reset** the advice counts down to — a `reset_weekday` and
`reset_time` on the profile take the place of the
`usage.calculator.reset_*` settings for that countdown, in the calculator's
zone. The profile never touches the policy, the lanes or the verdict: it only
supplies the money and the reset the figures are measured against.

**The default assumption.** A provider with no profile at all is read as
`api_metered` with percent allowances — and every surface says so with an
"assumed" or “(assumed — no usage.billing entry)” label rather than
presenting it as a fact. Because the assumed default carries no price, the
point-value line reads zero, which is the daemon being honest: it would
rather show no estimate than invent one. Setting a profile removes the
assumption.

| `mode` | What it says | What the calculator gets from it |
|---|---|---|
| `bundled_plan` | A flat monthly subscription (Pro / Max style) whose allowance resets on a schedule. | The point value, from `monthly_price`; if the profile also names `reset_weekday`/`reset_time`, the reset it counts down to. The money stays an API-equivalent estimate, never a charge. |
| `api_metered` | Pay-per-use API spend, no allowance. | No allowance to read; figures are ordinary estimates unless you give a `monthly_price` as a stand-in rate. |
| `free_local` | Local inference (Ollama and friends) at no charge. | None — nothing to bill and no allowance. |

### Setting it up

Every field is optional and independent — set only what you know; the others
stay unset. A blank string (and `0` for numbers) clears a field back to
unset.

| Field | Accepts | A bundled-plan example |
|---|---|---|
| `usage.billing.claude-code.mode` | `bundled_plan`, `api_metered`, `free_local` | `bundled_plan` |
| `usage.billing.claude-code.plan` | Free text naming the plan; for `claude-code` the accepted tiers are `pro`, `max_5x`, `max_20x` (the same vocabulary the LLM entries' `plan_tier` uses) | `max_5x` |
| `usage.billing.claude-code.monthly_price` | A finite number at 0 or above, in `currency`; `0` for a free plan | `200` |
| `usage.billing.claude-code.currency` | A 3-letter code | `usd` |
| `usage.billing.claude-code.reset_weekday` | `sunday` through `saturday` | `thursday` |
| `usage.billing.claude-code.reset_time` | A 24-hour `HH:MM` clock time | `00:00` |
| `usage.billing.claude-code.allowance_unit` | `percent` (default), `requests` or `tokens` — the unit the allowance is counted in | `percent` |

A minimal working profile for a bundled plan is two fields:

```yaml
usage:
  billing:
    claude-code:
      mode: bundled_plan
      monthly_price: 200
```

That is enough for the calculator to carry a non-zero point value and for the
cost report to label its `claude-code` rows “bundled plan —
API-equivalent, not billed” with the point figures. Add `plan`, `currency`,
the reset and the allowance unit when you want the label to name your actual
plan.

Six ways to set it (a change is live the moment it is stored; no restart):

| Method | How |
|---|---|
| **YAML** | The `usage.billing` block in `~/.datawatch/config.yaml`, as above. |
| **CLI** | `datawatch config set usage.billing.claude-code.mode bundled_plan`, `datawatch config set usage.billing.claude-code.monthly_price 200`, and so on per field; read it back with `datawatch config get usage.billing`. |
| **Web UI** | The billing profiles group in the Usage calculator group in Settings: provider key, mode, plan, monthly price, currency, reset and allowance unit per provider, with “assumed default” marked on a provider that has no profile. |
| **REST API** | `PUT /api/config` with one field per request, e.g. `{"usage.billing.claude-code.mode": "bundled_plan"}`; the block reads back under `usage.billing` on `GET /api/config`. |
| **Comm channel** | `configure usage.billing.claude-code.mode=bundled_plan`. |
| **MCP** | The `config_set` tool with `key: usage.billing.claude-code.mode` (or any other field) and the value, one field per call; the block reads back through `config` or on the surfaces below. |

It is a config patch, not a dedicated endpoint. One invalid value rejects the
whole call (400, naming the key) and stores nothing; an unknown provider or
field is refused the same way. Deleting a provider's profile (removing the
map entry, or clearing its fields) puts it back on the assumed default.

## Sharing across federated daemons

A weekly allowance belongs to the account, not to one daemon, so the readings
are shared.

- One daemon is the **keeper** of the series. Set `usage.calculator.keeper` to
  the peer's name on the other daemons; empty means this daemon keeps it.
- Peers **read it over REST** with the `usage:read` federation capability.
- A **write on a peer goes to the keeper** (`usage:write`), so there is one
  series.
- If the keeper cannot be reached, a peer shows **its last copy, marked as
  such** (stale), rather than failing or hiding the figures.

## The usage page: one card with a verdict and three graphs

The PWA usage page no longer shows a calculator with controls. It is
**one card**: a **verdict sentence** above **three graphs of a fixed size**, all
drawn from the same `GET /api/usage/advice` call.

- **The verdict sentence** says *Within budget* or *Over budget*, how much of the
  allowance is used now and where the recent burn leads by the reset. With no
  readings it says *No verdict yet: save another reading*.
- **Weekly allowance** shows how many points are used out of the ceiling. The
  ceiling is 100 plus any extra credit you bought.
- **Pace line** compares your usage with the straight line that would use the
  whole allowance exactly at the reset: ahead of the line, or the points of
  headroom behind it, with the days since the reset and the days left.
- **Burn per day** shows the points used each day, one bar per day since the
  reset, with the recent figure and the week average. When the burn is over, it
  names the lane count to try.

Each graph has a **state** line, a **why** line and a **next** line, and a
**What is this?** help link that opens this guide. With no readings the page
shows an empty state that points to Settings.

The page has **no policy or lane controls**, only a **Refresh** button. Everything you used to set on it is in
**Settings → General → Automata**: the reading and extra credit forms, the
policy, the reset, the auto-lane fields and the billing profiles. The Burn
guard card (with its **Run now** button and a **Burn guard settings** link) still follows the usage card on the page.

The same three graphs are also **dashboard cards** (`usage-allowance`,
`usage-pace`, `usage-burn`, each 3 columns wide by default). Add them from the
dashboard card picker, the REST card API or the MCP dashboard card tools; they
draw the same way at the same size, from one advice read for the whole board.
On the Automation view they sit in a row at the top of the **Resources**
sub-tab (see [Automata resources](automata-resources.md)).

`usage.calculator.variant` (`all`, `pace-gauge`, `lane-planner`, `burn-ledger`)
is still accepted and stored, but the PWA no longer chooses a layout from it:
it always draws the three graphs. The key has no effect on the page.

## Where readings come from

Today you type them. Two other sources are **planned and not built**: pasting
the provider's usage text, and reading it from the account or an API. The
reading source is designed as a pluggable input, so these will arrive without
changing the advice.

## Five ways to use it

| Method | How |
|---|---|
| **YAML** | `usage.calculator.*` in `~/.datawatch/config.yaml` (policy, variant, `safety_points`, `cost_per_task_points`, `baseline_per_24h`, `max_lanes`, `reset_weekday`, `reset_time`, `reset_zone`, `keeper`, `auto_lanes`, `auto_lanes_max`, `auto_lanes_special_priority`) |
| **CLI** | `datawatch usage advice [--lanes N]` (the what-if), `datawatch usage reading add --percent 42`, `datawatch usage reading list`, `datawatch usage reading delete <id>`, `datawatch usage extra-credit <points>`, `datawatch usage remaining-work <points\|clear>`; settings with `datawatch config set usage.calculator.<key> <value>` |
| **Web UI** | The usage page (one card: a verdict sentence and three graphs) and the calculator's Settings fields; see [parity status](../parity-status.md) for what has shipped. The reading form has no lanes input: each reading is stamped by the daemon. The what-if lane count is no longer on the page; ask for it with `datawatch usage advice --lanes N`, `usage advice lanes=N`, `GET /api/usage/advice?lanes=N` or `usage_advice`. It is never saved and never changes the lanes running. |
| **REST API** | `GET /api/usage/advice` (`lanes` is the what-if), `GET` and `POST /api/usage/readings` (`lanes` on the body is deprecated and ignored), `DELETE /api/usage/readings/<id>`, `PUT /api/usage/extra-credit`, `PUT /api/usage/remaining-work`; settings with `PUT /api/config` |
| **Comm channel** | `usage advice [lanes=N]` (the what-if), `usage reading <percent> [session=N]`, `usage extra <points>`, `usage work <points\|clear>`; settings with `configure usage.calculator.<key>=<value>` |

The same actions are MCP tools: `usage_advice`, `usage_readings`,
`usage_reading_add`, `usage_reading_delete`, `usage_extra_credit_set` and
`usage_remaining_work_set`.

Where lanes show up on each method:

| Method | Reading `lanes` | Advice `lanes` |
|---|---|---|
| **REST API** | Deprecated, ignored | The what-if |
| **MCP** | Deprecated, ignored | The what-if |
| **CLI** | Deprecated, ignored (`--lanes` still parses) | The what-if |
| **Comm channel** | Deprecated, ignored | The what-if |

## The start-up advice line

The advice carries one **start-up** line, when the daemon has start-up data. It
names the worker profile whose attempts start with the largest average starting
context, the average in tokens, how many attempts it is over, and the direction a
lighter profile would take, for example:

> start-up: profile full has the largest average start-up, 18000 tokens over 12 attempts; switching it to a lighter profile (for example digest instead of full for this role) would reduce that. Advice only, no setting changed.

- It is **read-only advice**. It changes no profile, setting, lane or session.
- Attempts that ran with no profile are left out, because they name no profile to
  change. Profiles with no average, or no attempts, are skipped.
- No line appears when there is no start-up data yet. The figures come from the
  cost report's by-profile start-up table — see
  [cost-report.md](cost-report.md#the-start-up-tables).

## Where to see it

| Where | What you see |
|---|---|
| PWA | The usage page (one card: a verdict sentence and three graphs), the same three graphs on the Resources sub-tab and as dashboard cards. The usage reading and extra credit forms, and every calculator setting, are in **Settings → General → Automata → Usage reading and extra credit** (see [parity status](../parity-status.md) for what has shipped) |
| Android and iOS | Pending, tracked in [parity status](../parity-status.md) |
| CLI | `datawatch usage advice` |
| REST | `GET /api/usage/advice` |
| Comm channel | `usage advice` |
| MCP | `usage_advice` |

## Troubleshooting

- **"No reading in the last 2 hours" on the screen.** Expected. Type a fresh
  reading.
- **A reading is rejected.** Give exactly one of percent or spend, and a
  percent between 0 and 100.
- **The week ends at the wrong moment.** Check `reset_weekday`, `reset_time`
  and `reset_zone` against your provider; use a zone name, never an offset.
- **A zone is refused.** An unknown name or a fixed offset is rejected; the
  value is unchanged.
- **Run-flat-out shows an estimate you do not trust.** Type the remaining work,
  or calibrate `cost_per_task_points`.
- **A peer shows stale figures.** The keeper is unreachable; the peer is
  showing its last copy. Check the peer link and the `keeper` setting.
- **A peer is refused.** It needs the `usage:read` (and for writes
  `usage:write`) capability.
- **A setting snapped back to its default.** A saved value that is no longer
  allowed (for example a retired variant) falls back to the default and the
  daemon logs one line saying so.
- **Lanes did not change.** By default, by design. The calculator advises only; change the lane count yourself (the opt-in that applies it instead is [Automatic lane count](#automatic-lane-count)). The what-if lanes on the advice are never saved and
  never start or stop a lane either, and the stamped lanes on a reading only
  record what was running.

## Automatic lane count

By default the calculator advises only — you change the lane count yourself.
**Automatic lane count** (auto-lanes) is the opt-in that lets the daemon apply
that advice to the Claude lanes it runs: when it is on, a loop that runs once
at daemon start and once every hour reads the freshest advice and sets the cap
those lanes are held to at **the advice's lanes plus one lane for each open
special item** — never above the operator maximum you set, and never below one
lane. Off (the default), the loop does nothing and the lane count stays
exactly where you have it.

**The rule, in plain words.** Every hour the daemon works out three numbers:
the lanes the active policy advises, the open Automata whose capacity priority
is at or above the special-priority setting (one lane each), and your maximum.
The target is the first two added together, brought down to your maximum if it
runs over, and up to one lane if it runs under. When the target differs from
the cap in force, the daemon sets it; when it is the same, the cap stays.

**What it touches.** The cap it sets is the `max_inflight` of the first enabled
claude-code LLM entry — the value you would change in an LLM update, saved
through that same path, and the capacity ledger picks it up live. It never
starts or ends a running session, and it never touches any other backend.

Three settings control it, all under `usage.calculator.`:

| Setting | Meaning | Default |
|---|---|---|
| `auto_lanes` | on/off; when off the loop never runs | off |
| `auto_lanes_max` | the operator maximum the daemon never exceeds, a whole number 1 to 8 | 2 |
| `auto_lanes_special_priority` | the capacity priority at or above which an open Automaton counts as a special item; a whole number 0 or above, where 0 counts every open Automaton | 1 |

Every value goes through the same validation as any config key, so a refused
value stores nothing — `auto_lanes_max` outside 1 to 8 or a negative priority
is refused and the setting stays where it was.

**The five access methods.**

| Method | Set the settings | Read the state |
|---|---|---|
| **YAML** | `auto_lanes`, `auto_lanes_max` and `auto_lanes_special_priority` under `usage.calculator` in `~/.datawatch/config.yaml` | the keys in `datawatch config get` or `GET /api/config` |
| **CLI** | `datawatch usage auto-lanes set --on` (or `--off`) and `--max N` (1 to 8); or any of the three keys through `datawatch config set usage.calculator.<key> <value>` | `datawatch usage auto-lanes status` |
| **Web UI** | the three "Auto-lanes" fields in the usage calculator group in Settings | the read-only status line under them: on or off, the current cap, and the last change |
| **REST API** | `PUT /api/config` with `{"usage.calculator.auto_lanes": true}` (or any of the three keys) | `GET /api/usage/autolanes` |
| **Comm channel** | `configure usage.calculator.auto_lanes=true` (and the other two keys the same way) | `GET /api/usage/autolanes` — there is no comm verb for the status |

`datawatch usage auto-lanes status` reads `GET /api/usage/autolanes`, and
`datawatch usage auto-lanes set` writes through the same config path as
`datawatch config set`, so both sides behave exactly like the config commands:

```
datawatch usage auto-lanes status
datawatch usage auto-lanes set --on            # or --off
datawatch usage auto-lanes set --max 3
datawatch usage auto-lanes set --on --max 4    # both at once
```

The status body carries the settings (`enabled`, `policy`, `max`), the cap the
daemon applies now (`current_max_inflight`), what a fresh advice's decision
would do to that cap (`target` and `direction`), the open special items counted
now (`special_items`), the last change that moved the cap (`last_change`), and
the loop's recent decisions, newest first (`recent_changes`). From the MCP
server, `usage_autolanes_status` (read-only, `usage:read`) returns the same
state; the settings are still changed through the config path.

**Reading the change log.** Every decision is appended to `autolanes.json` in
the data directory, beside the usage series file, and the file keeps the
newest 500 decisions. Each entry says:

- `at` — when the check ran;
- `from` → `to` — the cap before and after; equal when the decision was
  `unchanged`;
- `direction` — `up`, `down` or `unchanged`, always against the cap that was
  in force at the time;
- `reason` — the plain-words line: the advice's lanes for the active policy,
  the open special items, the sum, and the clamp to the operator maximum;
- `policy`, `lanes_running` and `special_items` — the inputs of that pass.

The read surfaces show the newest 50 of those entries: `datawatch usage
auto-lanes status` and `GET /api/usage/autolanes` carry them as
`recent_changes` (newest first), and `last_change` is the newest entry that
actually moved the cap. The PWA's status line under the Auto-lanes fields in Settings shows the
same three things: on or off, the current cap, and the last change.

## See also

- [Automata capacity](automata-capacity.md)
- [Cost report](cost-report.md) — the other read surface of the same billing profiles
- [Configuration reference](../config-reference.yaml) (`usage.calculator`, `usage.billing`)
- [Burn guard](burn-guard.md)
- [Parity status](../parity-status.md)
- [Auto-lanes plan](../plans/2026-11-02-usage-auto-lanes.md)

## Burn guard

The usage page ends with a Burn guard card. It shows the guard's state and the rung in force, the projection at the reset against the pace line and margin, and the last decision, a **Burn guard settings** link, and **Run now** to evaluate once. The guard steps down a ladder of admission changes when the projected burn runs over the pace line, and never ends a running session. See [burn-guard.md](burn-guard.md) for the ladder, the settings, how to read an alert, how to step back, the effort ceiling and the Claude retry limit.
