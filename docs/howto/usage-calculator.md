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

## The three PWA variants

`usage.calculator.variant` chooses what the PWA draws from the same advice:

| Variant | Shows |
|---|---|
| `pace-gauge` | The pace line, where the burn leaves you at the reset, and the headroom. |
| `lane-planner` | A row per lane count with its projected end of week and verdict. |
| `burn-ledger` | The burn figure with the week average beside it, the extra credit and the finish line. |
| `all` (default) | All three side by side. |

Leave it on `all` and **compare for a full week, a Sunday-to-Sunday cycle**,
before choosing one. The variant and the policy are independent: any variant can
show any policy.

## Where readings come from

Today you type them. Two other sources are **planned and not built**: pasting
the provider's usage text, and reading it from the account or an API. The
reading source is designed as a pluggable input, so these will arrive without
changing the advice.

## Five ways to use it

| Method | How |
|---|---|
| **YAML** | `usage.calculator.*` in `~/.datawatch/config.yaml` (policy, variant, `safety_points`, `cost_per_task_points`, `baseline_per_24h`, `max_lanes`, `reset_weekday`, `reset_time`, `reset_zone`, `keeper`) |
| **CLI** | `datawatch usage advice [--lanes N]` (the what-if), `datawatch usage reading add --percent 42`, `datawatch usage reading list`, `datawatch usage reading delete <id>`, `datawatch usage extra-credit <points>`, `datawatch usage remaining-work <points\|clear>`; settings with `datawatch config set usage.calculator.<key> <value>` |
| **Web UI** | The calculator card in the PWA and its Settings fields; see [parity status](../parity-status.md) for what has shipped. The reading form has no lanes input: each reading is stamped by the daemon. The card shows a **Claude lanes running now** line (or "not known" when no stamped reading says) and a **What if** stepper beside the policy. The stepper sets the lanes to run in the what-if only, is never saved, and never changes the lanes running. |
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

## Where to see it

| Where | What you see |
|---|---|
| PWA | The calculator screen (not yet shipped; see [parity status](../parity-status.md)) |
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
- **Lanes did not change.** By design. The calculator advises only; change the
  lane count yourself.

## See also

- [Automata capacity](automata-capacity.md)
- [Configuration reference](../config-reference.yaml) (`usage.calculator`)
- [Parity status](../parity-status.md)
