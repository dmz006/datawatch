# Usage and cost calculator — mobile contract

Date: 2026-10-25
Superseded by: —
Sources: `docs/plans/2026-10-23-usage-calculator-design.md` (design), `docs/plans/2026-10-25-usage-calculator-decisions.md` (operator decisions)

This contract is for the native clients of `dmz006/datawatch-app` (Android and iPhone / iOS). It fixes the REST contract the clients parse, the federation capabilities that gate it, the settings keys, the screen behaviour, the acceptance criteria, and the exact locale key set with its English values. The daemon side is shipped in this repository on the feature branch for PRD 07630001; the numbers are computed once on the daemon (`internal/usagecalc`), so every client renders the same values from the same response body. Where this contract and the two source documents disagree, the design doc wins on design matters and the decisions doc wins on operator decisions.

## What the calculator is

One weekly verdict and one set of advice, computed on the daemon from the operator's typed readings of the account's weekly usage percentage:

- **The verdict** — "Within budget" or "Over budget" — decided on the recent burn (newest reading against the one within 2 hours behind it, falling back to the week average with a notice when there is no such earlier reading), with the week-average verdict beside it.
- **The advice** — how many Claude lanes fit the rest of the week under one of the three policies: **Run until used up** (`use-all`), **Pace to the week** (`pace`, the pace line minus the safety margin), **Run flat out** (`finish-early`, until the remaining work is done).
- **Three renderers** (variants) of the same result body: pace gauge, lane planner, burn ledger. Any renderer shows any policy; the variant picks the layout, the policy picks the target, and neither one is stored by advice calls.

The calculator advises. No client, renderer, endpoint or background check may stop, slow, cap or reorder a lane on the strength of it (the advice-only bar of the concurrency design).

## REST contract

All endpoints are served by the daemon's API server. Read endpoints require the federation capability `usage:read`, write endpoints `usage:write` (see the capabilities table below). Bodies are JSON. Timestamps are RFC 3339.

### GET /api/usage/advice

One advice for this account, with the resolved selections and the series provenance. Capability: `usage:read`.

Query parameters — all optional, per-call overrides applied to the read-only copy of the settings (never stored), invalid ones answer 400:

| Parameter | Allowed values | Notes |
|---|---|---|
| `variant` | `pace-gauge`, `lane-planner`, `burn-ledger`, `all` | unknown value: 400 |
| `policy` | `use-all`, `pace`, `finish-early` | unknown value: 400 |
| `lanes` | whole number of 0 or more | the lane count to plan with for this call |
| `cost` | number of 0 or more points | this call's cost per queued task |

200 response body:

| Field | Type | Meaning |
|---|---|---|
| `variant` | string | the variant resolved for this response |
| `policy` | string | the policy resolved for this response |
| `stale` | bool | the shared series was served from the last copy because the keeper could not be reached — see the source block below |
| `stale_reason` | string | why the copy is stale, when `stale` is true |
| `copied_at` | string (RFC 3339) | when that last copy was taken |
| `text` | object | `{ "cli": string, "comm": string }` — the two shared text forms of this advice, built on the daemon so every surface shows the same words; the client may render them verbatim |
| `advice` | object | the result body, below |

Status codes: 200 OK; 400 for a bad `variant`, `policy`, `lanes` or `cost`; 503 when the calculator is not wired on the daemon or the series is unavailable; 403 when the request carries a wrong or missing federation capability (`usage:read`).

The `advice` object — the result body of this call, with the resolved policy of this call in its `advice` field:

| Field | Type | Meaning |
|---|---|---|
| `needs` | string (absent when none) | set when the operator must supply something first (for example the remaining work for Run flat out); the reading numbers are left at zero and the explanation is in `advice.headline` |
| `read_at` | string (absent with no readings) | the newest reading's moment |
| `percent` | number | the newest weekly percent |
| `latest_age_hours` | number | age of the newest reading |
| `last_reset_at`, `next_reset_at` | strings | the reset moments around the newest reading |
| `extra_credit_points` | number | bought extra credit, 0 or more |
| `ceiling` | number | 100 plus the extra credit |
| `lanes` | number | the operator's own lane count for this call |
| `recent_lanes` | number | lanes that ran since the previous reading |
| `max_lanes` | number | the planner's lane ceiling |
| `safety_points` | number | the pace policy's margin under the pace line |
| `days_since_reset`, `days_left` | numbers | position in the week |
| `allowance_per_24h` | number | points per 24 hours the allowance allows |
| `pace_line` | number | the pace line at the newest reading |
| `headroom` | number | points of headroom |
| `recent_burn_per_24h` | number | the burn of the recent readings |
| `week_avg_burn_per_24h` | number | the burn of the whole week so far |
| `recent_fallback` | bool | true when the week average stood in for the recent burn (no earlier reading within 2 hours) |
| `fallback_reason`, `fallback_notice` | strings (absent when none) | why, and the operator-facing notice |
| `projected_end_recent`, `projected_end_avg` | numbers | projected end at the reset per burn |
| `run_out_at_recent`, `run_out_at_avg` | strings (absent when the week ends first) | when each burn runs the allowance out |
| `hours_early_recent`, `hours_early_avg` | numbers | how early each projection lands before the reset |
| `verdict` | string | "Within budget" or "Over budget", on the recent burn (absent when `needs` is set) |
| `verdict_avg` | string | the verdict on the week average, beside the first |
| `ahead_of_pace` | bool | the newest reading is under the pace line |
| `per_lane_per_24h` | number | the one burn figure per lane, inferred from the readings |
| `by_model` | map (absent when none) | per-model burn, when recorded |
| `baseline_per_24h` | number | the idle baseline |
| `zone` | string (absent when none) | the IANA zone the weekly reset runs in (effective value): the operator's set zone, or the daemon's local zone when they left it unset |
| `server_zone` | bool | true when the operator left the zone unset, so the reset follows the daemon's local time zone |
| `by_lanes` | array | one `LaneRow` per lane count 0 through `max_lanes` |
| `chosen` | object (absent when none) | the `LaneRow` of the operator's own lane count |
| `remaining_work` | object | `{ in_use, points, estimate_points, typed_points, queue_tasks, cost_per_task }` — `in_use` is `estimate`, `typed` or `none`; see Screen behaviour |
| `session` | object (absent when none) | `{ percent, read_at, label }` — the session percent, secondary: it has no verdict and feeds no calculation |
| `series` | array | `{ at, percent, hours_since_reset }` per percentage reading since the last reset |
| `daily` | array | the points used per day since the reset (straight-line estimates; burn ledger) |

Each `LaneRow` in `by_lanes` and `chosen`:

| Field | Type | Meaning |
|---|---|---|
| `lanes` | number | the lane count of the row |
| `burn_per_24h` | number | measured burn for this lane count |
| `planning_burn_per_24h` | number | the planning figure from the queue spread over the lanes — not a forecast; it can disagree with the measured one |
| `projected_end` | number | the projected end percent at the reset |
| `run_out_at` | string (absent when the week ends first) | when this lane count runs the allowance out |
| `hours_early` | number | how early the projection lands before the reset |

The policy object at `advice.advice` (the inner `advice` object of the result body):

| Field | Type | Meaning |
|---|---|---|
| `policy` | string | the resolved policy |
| `lanes_suggested` | number | the advised count: at least 1 for `use-all` and `pace`, `max_lanes` for `finish-early` — it never names zero |
| `headline` | string | the policy's own words for this call (Run until used up / Pace to the week / Run flat out) |
| `notes` | array of strings | the operator-facing notes for this policy and this reading |
| `finish` | object (absent outside Run flat out) | `{ hours, at, end_percent, spare_points, before_allowance, no_burn }` — the finish line of the remaining work against the allowance |
| `policy_target` | number (absent under finish-early) | the target end at the reset the lane table fits against: the ceiling under `use-all`, ceiling minus the safety margin under `pace` |

### GET /api/usage/readings

The shared readings record plus the source block. Capability: `usage:read`. Hop-marked peer fetches (`X-Usage-Hop: 1`, sent by a peer's own fetch) receive the bare account record so a peer and the keeper see the same wire shape.

| Field | Type | Meaning |
|---|---|---|
| `readings` | array of `Reading` | the shared series, see below |
| `extra_credit_points` | number | the bought extra credit |
| `updated_at` | string | when the record last changed |
| `keeper` | string | the keeper's peer name (empty when the answering daemon is the keeper) |
| `is_keeper` | bool | this daemon keeps the series |
| `stale`, `stale_reason`, `copied_at` | see the source block | set when the serving daemon answered from its last copy |

`Reading` object:

| Field | Type | Meaning |
|---|---|---|
| `id` | string | the daemon-assigned identity, used by DELETE |
| `at` | string | the reading's moment (the client may suggest one; the daemon finalizes it) |
| `percent` | number (one of percent/spend) | the weekly percent, 0 to 1000 |
| `spend_usd` | number (one of percent/spend) | a spend figure; stored and listed, not yet turned into a verdict |
| `session_percent` | number (absent when not given) | the session percent, 0 to 100, secondary |
| `lanes` | number (absent when not given) | the Claude lanes running at the reading |
| `source` | string | `typed` today; see POST |

### POST /api/usage/readings

Append one reading to the shared series. Capability: `usage:write`. If the answering daemon is not the keeper it forwards the write to the keeper and relays the stored reading back.

Body: one `Reading` as above, with exactly one of `percent` or `spend_usd`. 200 response: `{ "ok": true, "reading": { ... the stored reading, with its final id and at ... } }` (the legacy `{"ok":true}` shape from an older keeper confirms the write but omits the reading). The `source` value gates the write: `typed` is built, `pasted` and `account` answer 501 (planned reading sources, deliberately out of this release), any other value answers 400. 400 also for an invalid reading; 503 when the keeper could not be reached (nothing was stored).

### DELETE /api/usage/readings

Remove one reading by id. Capability: `usage:write`. Two wire forms, same operation: the operator form `DELETE /api/usage/readings?id=<id>` and the peer wire form `DELETE /api/usage/readings/<id>`. Either is served at the keeper and at any peer (a peer forwards to its keeper). 200: `{ "ok": true }`; 400 without an id; 404 when the keeper does not know it; 503 when the keeper could not be reached.

### PUT /api/usage/extra-credit

Set the bought extra credit on the shared record. Capability: `usage:write`. Body: `{ "points": <number of 0 or more> }`. 200: `{ "ok": true }`; 400 for a missing or negative figure; 503 when the keeper could not be reached.

### PUT /api/usage/remaining-work and DELETE /api/usage/remaining-work

Set and remove the operator's typed remaining-work figure (Run flat out). Capability: `usage:write`. PUT body: `{ "points": <number of 0 or more> }`; DELETE removes it (`{"ok":true}` either way). The figure is **daemon-local**: it stays in the daemon that received the write and does not travel with the shared series, so each daemon's `remaining_work` in its advice reflects its own typed figure or its own queue estimate.

## The source block (stale copies)

The readings series lives on the keeper daemon — the one whose `usage.calculator.keeper` is empty (default: this daemon keeps its own) — and peers read through it, holding a last copy on disk. When a peer's keeper cannot be reached, the peer serves the last copy and marks it. Clients must surface the mark, not silently trust the numbers.

| Where | Field / header | Meaning |
|---|---|---|
| Body of `GET /api/usage/advice` | `stale` (bool), `stale_reason` (string), `copied_at` (RFC 3339) | the provenance of the series this advice read |
| Body of `GET /api/usage/readings` | `is_keeper`, `stale`, `stale_reason`, `copied_at` | the provenance of the record served |
| Headers of both endpoints | `X-Usage-Stale: <reason>`, `X-Usage-Copied-At: <RFC 3339 UTC>` | set exactly when the body's `stale` is true |

On a stale copy the client shows the last-copy marker (for example: "Last copy from <keeper> at <time>"), using the same wording as `usage_calc_stale` and `usage_calc_keeper_generic` below.

## Federation capabilities

| Capability | Granted to | Gates |
|---|---|---|
| `usage:read` | peers that may look at the account's series | `GET /api/usage/advice`, `GET /api/usage/readings` (including the hop-guarded local answer) |
| `usage:write` | peers that may record into the account's series | `POST /api/usage/readings`, `DELETE /api/usage/readings` (both wire forms), `PUT /api/usage/extra-credit`, `PUT` and `DELETE /api/usage/remaining-work` |

A request without the capability for its verb answers 403. A peer without the write capability can read and hold a stale copy but cannot record; record and extra credit live on the keeper, remaining work is local to the writing daemon.

## Settings keys

All keys live under `usage.calculator.` in the daemon configuration (`~/.datawatch/config.yaml`, `usage:` block). Each is individually settable on every surface (YAML, CLI `datawatch config set usage.calculator.<key> <value>`, PWA settings card, REST `PUT /api/config` with the dotted key, comm channel `configure usage.calculator.<key>=<value>`, MCP `config_set`). A patch with one invalid key rejects the whole patch; nothing else in that patch applies.

| Key | Type / allowed values | Default | Notes |
|---|---|---|---|
| `usage.calculator.variant` | `pace-gauge` \| `lane-planner` \| `burn-ledger` \| `all` | `all` | which layout to show; `all` shows the switcher with the three layouts over the same reading while the operator compares |
| `usage.calculator.policy` | `use-all` \| `pace` \| `finish-early` | `use-all` | the advice target; independent of the variant |
| `usage.calculator.safety_points` | number, 0 to 50 | `5` | the pace policy's margin under the pace line; 0 is allowed |
| `usage.calculator.cost_per_task_points` | number above 0 | `0.1` | the weekly points one queued task costs; the starting figure of the Run flat out estimate |
| `usage.calculator.baseline_per_24h` | number, 0 to 20 | `3` | the idle burn per 24 hours |
| `usage.calculator.max_lanes` | whole number, 1 to 8 | `3` | the largest lane count the planner offers |
| `usage.calculator.reset_weekday` | `sunday` … `saturday` | `sunday` | the reset weekday |
| `usage.calculator.reset_time` | 24-hour `HH:MM` | `08:00` | the reset clock time |
| `usage.calculator.reset_zone` | IANA zone name (fixed offsets rejected) | empty | the reset time zone; empty follows the zone the daemon runs in (falling back to UTC when that cannot be resolved). GET /api/config shows the effective zone plus `reset_zone_source` (`configured` or `server`) and `reset_zone_fell_back` (bool) beside it; saving empty clears the stored zone back to the server default |
| `usage.calculator.keeper` | peer name (letters, digits, dot, dash, underscore) | empty | the federation peer that keeps the shared readings; empty means this daemon |

An invalid value at a settings surface is a 400 and changes nothing; an invalid *saved* value degrades to its default with a warning at daemon start.

## Screen behaviour (native clients)

The native screens mirror the PWA surface: one usage-calculator screen with the shared frame below, and the three variant layouts switched by the `variant` setting. The numbers and the words come from the daemon's `advice` body — the client renders, it does not recompute.

### The shared frame (every variant, every platform)

- **The verdict chip first**: the policy headline + the verdict ("Within budget" / "Over budget") + **the one burn figure** for the account — the recent burn per 24 hours, and the week-average burn beside it when the two are shown. One figure per line, per the decision that the calculator presents one burn figure for now; per-model figures are not part of this release.
- **The freshness notice**: `usage_calc_fallback_default` / `usage_calc_field_burn` + `usage_calc_burn_fallback` — shown exactly when `advice.recent_fallback` is true (no earlier reading within 2 hours; the week average stands in).
- **The session percent**, when present: `advice.session` rendered with `usage_calc_session_secondary` — secondary, **no verdict**, and it feeds no calculation.
- **The stale marker**: when `stale` is true in the response, the last-copy line (`usage_calc_stale` with the keeper name — `usage_calc_keeper_generic` when unknown — and `copied_at`). Never suppressed, never blended into fresh numbers.
- **Zero lanes is the operator's own choice and is never advised**: `lanes_suggested` is at least 1 under `use-all` and `pace` and `max_lanes` under `finish-early`. When the operator's own count is 0 the screens show it under `usage_calc_zero_lanes` (lane table badge `usage_calc_lane_badge_operator`). No screen, platform or background check may stop, slow, cap or reorder a lane on the strength of the calculator.
- **Readings form** (`usage_calc_reading_title` and below): the operator types the weekly percent (0–1000), an optional session percent (0–100) and the lane count; `POST /api/usage/readings` with `source: "typed"`. Pasted and account sources are not built: they answer 501 and the native clients of this release do not offer them.
- **Extra credit line**: `usage_calc_extra_line` — "Ceiling %1$s% (100 plus %2$s extra-credit points)" — from `advice.ceiling` and `advice.extra_credit_points`, with the editor and `PUT /api/usage/extra-credit`.
- **Remaining work line** (Run flat out): `usage_calc_remaining_line` — the figure in use (`estimate` or `typed`, per `advice.remaining_work.in_use`), the queue count (`queue_tasks`), and the cost per task. The estimate is queue-size × cost per task; a typed figure replaces it (`PUT /api/usage/remaining-work`), and it is cleared with `DELETE /api/usage/remaining-work`. When the cost per task is unset the estimate is absent (`usage_calc_ledger_queue_no_estimate`), not zero.
- **Policy selection in-screen** changes the headline, the notes, the lane suggestion and the finish block for this viewing only (per-call `policy`); it does not save a setting, per `usage_calc_policy_hint`.

### The three variants and the switcher

| Variant | Layout | Data it needs, all in the response |
|---|---|---|
| `pace-gauge` (Pace gauge) | one gauge of the weekly allowance with the pace line and both projections at the reset; the verdict card, the chosen line, and the advice | `ceiling`, `pace_line`, `percent`, `projected_end_*`, `run_out_at_*`, `chosen`, the policy advice |
| `lane-planner` (Lane planner) | the verdict, then the lane table (one row per count 0 through `max_lanes`, with burn, projected end, run out, hours early and fit against the policy target), the week timeline and the finish block | `by_lanes`, `chosen`, `policy_target`, `finish`, the target lines |
| `burn-ledger` (Burn ledger) | the verdict and reading fields, then the per-day bars since the reset, the queue section and the per-Automaton cost note | `daily`, `series`, `remaining_work`, `cost_per_task`, the queue lines |

With `variant = all` (the default) the client shows the **switcher on the same reading and the same policy**: the operator flips renderers and the numbers move nowhere. Per-variant rendering is selected with the per-call `variant` parameter or the variant setting; per the retirement rule a value the daemon no longer allows falls back to `all`, so a future rename cannot break the client.

### The reference formulas

The client implements **no formulas**. The authoritative reference is the `UsageModel` embedded unchanged in `docs/plans/assets/usage-calculator-mockups/variant-pace-gauge.html` inside `<script id="usage-model">`, ported into the daemon as `internal/usagecalc` (`advice` and the `Advise` service). If a native rendering ever disagrees with the response body, the response body is right.

## Acceptance criteria

Common to both platforms: the screen renders every field of the shared frame above from `GET /api/usage/advice` and `GET /api/usage/readings`; a typed reading round-trips through `POST /api/usage/readings`; delete, extra credit and remaining work work through the endpoints above; the stale marker is shown whenever `stale` is true; all labels come from the locale keys below, in all five locales after the issue's translations land; an unauthenticated or capability-less request shows the daemon's 403, not a crash or a blank screen.

**Android (phone and tablet)**

1. A Usage calculator screen exists behind the existing account / settings tab, using the daemon's REST contract above against the paired daemon's address and token.
2. The shared frame shows the verdict chip, the one burn figure with the week average beside it, the freshness notice when `recent_fallback` is true, the session percent as a secondary line without a verdict, the stale marker when stale, the extra-credit line and the remaining-work line with the queue count.
3. The variant switcher honours `usage.calculator.variant` (default `all` shows all three layouts over the same reading); switching policy changes only the headline, the notes, the lane suggestion and the finish block.
4. The readings form saves a typed reading with `source: "typed"` and lists the series with deletion; pasted/account sources are not offered.
5. Zero lanes renders as the operator's own choice (and is never rendered as advice); no UI element on the calculator can stop, slow or reorder a lane.
6. The settings card exposes the ten `usage.calculator.*` keys with the locale labels below, saving through the same config surface as settings.
7. All new labels are taken from the locale key set below; no English fallback text is hard-coded in layouts.

**Android Wear** — scope is deliberately limited: **a glanceable verdict only** (the verdict chip with the weekly percent, and the stale marker when the copy is stale). No forms, no variants, no settings, no series list. Beyond that the launcher does not add the calculator.

**Android Auto** — no calculator screen; at most the glanceable verdict tile on the media / glance surface where the app already projects. It does not require any new capability beyond what Wear uses, and no write endpoint is reachable from the car UI.

**iPhone / iOS**

1. Same screen set and shared frame as Android items 1–7, with the settings card on the iOS settings surface; everything through the same REST contract, so a reading typed on the phone and a reading typed on the phone (or a tablet) land in the same shared series on the keeper.
2. The variant switcher and the per-call variant/policy/lanes parameters behave identically to Android.
3. The stale marker and the freshness notice are shown in the same places, with the same locale wording.
4. Zero-lanes rendering, the advice-only bar and the locale requirements are the same as Android items 5 and 7.

## Mobile issues

| Issue | URL |
|---|---|
| (a) `[sync] Usage and cost calculator: weekly within-or-over verdict, three screens, shared readings` (Android, Android Wear, Android Auto, iPhone / iOS) | <https://github.com/dmz006/datawatch-app/issues/286> |
| (b) `feat(i18n): add usage_calc and settings_usage_calculator keys for usage calculator parity` (DE / ES / FR / JA) | <https://github.com/dmz006/datawatch-app/issues/287> |

## Locale keys (complete list, English values)

The English bundle (`internal/server/web/locales/en.json`) carries the key set below. The i18n issue requests the same keys in `de.json`, `es.json`, `fr.json` and `ja.json`; the daemon-side guard test compares the key sets across all five bundles, so a missing key on one side breaks the build.

### Settings card (16 keys)

| Key | English value |
|---|---|
| `settings_usage_calculator_variant` | Usage calculator layout (pace-gauge \| lane-planner \| burn-ledger \| all) |
| `settings_usage_calculator_policy` | Usage calculator policy |
| `settings_usage_calculator_policy_use_all` | Run until used up |
| `settings_usage_calculator_policy_pace` | Pace to the week |
| `settings_usage_calculator_policy_finish_early` | Run flat out |
| `settings_usage_calculator_safety_points` | Usage calculator safety margin (points under the pace line, 0 to 50, default 5) |
| `settings_usage_calculator_cost_per_task_points` | Usage calculator cost per task (weekly points, above 0, default 0.1) |
| `settings_usage_calculator_baseline_per_24h` | Usage calculator idle burn (points per 24 hours, 0 to 20, default 3) |
| `settings_usage_calculator_max_lanes` | Usage calculator largest lane count (1 to 8, default 3) |
| `settings_usage_calculator_reset_weekday` | Usage calculator reset weekday (default sunday) |
| `settings_usage_calculator_reset_time` | Usage calculator reset time (24-hour HH:MM, default 08:00) |
| `settings_usage_calculator_reset_time_hint` | Local clock time of the weekly reset, for example 08:00. |
| `settings_usage_calculator_reset_zone` | Usage calculator reset time zone (IANA name; empty follows the server's zone) |
| `settings_usage_calculator_reset_zone_hint` | An IANA zone name such as America/New_York. Empty uses the zone the daemon runs in, falling back to UTC if that cannot be found. A fixed offset such as +02:00 is not accepted. |
| `settings_usage_calculator_reset_zone_server_note` | server zone — empty in the config; the reset follows this daemon's local time zone (shown under the reset zone input exactly when `reset_zone_source` is `server`) |
| `settings_usage_calculator_keeper` | Usage calculator keeper (federation peer that keeps the readings, empty = this daemon) |
| `settings_usage_calculator_keeper_hint` | Name of a federation peer. Leave empty to keep the readings on this daemon. |


### Usage calculator screen (144 keys)

| Key | English value |
|---|---|
| `usage_calc_title` | Usage calculator |
| `usage_calc_intro` | Is this week's burn within budget, and how many Claude lanes fit the rest of the week? The calculator advises; you choose the lane count and the policy. |
| `usage_calc_loading` | Loading advice… |
| `usage_calc_error` | Could not load usage advice: %1$s |
| `usage_calc_refresh` | Refresh |
| `usage_calc_empty_title` | No readings yet |
| `usage_calc_empty_body` | Save this week's weekly percentage below. Advice appears once one reading is saved. |
| `usage_calc_stale` | Last copy from %1$s at %2$s |
| `usage_calc_keeper_generic` | the keeper |
| `usage_calc_fallback_default` | No earlier reading at least 2 hours back; the week average is used. |
| `usage_calc_within` | Within budget |
| `usage_calc_over` | Over budget |
| `usage_calc_week_average` | Week average |
| `usage_calc_recent_burn` | Recent burn |
| `usage_calc_session_secondary` | Session %1$s% (secondary, no verdict) |
| `usage_calc_zero_lanes` | Zero lanes is the operator's own choice and is never advised. |
| `usage_calc_variant_label` | View |
| `usage_calc_variant_pace_gauge` | Pace gauge |
| `usage_calc_variant_lane_planner` | Lane planner |
| `usage_calc_variant_burn_ledger` | Burn ledger |
| `usage_calc_variant_unavailable` | This view is not available yet. |
| `usage_calc_reading_title` | Save a reading |
| `usage_calc_percent_label` | Weekly percent |
| `usage_calc_percent_range` | Enter a number from 0 to 1000. |
| `usage_calc_session_label` | Session percent (optional) |
| `usage_calc_session_range` | Enter a number from 0 to 100, or leave it empty. |
| `usage_calc_lanes_label` | Claude lanes running |
| `usage_calc_lanes_minus` | Fewer lanes |
| `usage_calc_lanes_plus` | More lanes |
| `usage_calc_read_at_label` | Read at |
| `usage_calc_save_reading` | Save reading |
| `usage_calc_policy_title` | Usage policy |
| `usage_calc_policy_hint` | The policy changes the advice only. It does not save a setting. |
| `usage_calc_policy_use_all` | Run until used up |
| `usage_calc_policy_pace` | Pace to the week |
| `usage_calc_policy_finish_early` | Run flat out |
| `usage_calc_use_suggested` | Use suggested lanes (%1$s) |
| `usage_calc_extra_title` | Extra credit |
| `usage_calc_extra_label` | Bought extra credit (points) |
| `usage_calc_extra_save` | Save extra credit |
| `usage_calc_extra_line` | Ceiling %1$s% (100 plus %2$s extra-credit points) |
| `usage_calc_remaining_title` | Remaining work (Run flat out) |
| `usage_calc_remaining_estimate` | estimate |
| `usage_calc_remaining_typed` | typed |
| `usage_calc_remaining_none` | no figure yet |
| `usage_calc_remaining_line` | %1$s points (%2$s) · queued tasks: %3$s · cost per task %4$s points |
| `usage_calc_remaining_override_label` | Type a figure to replace the estimate (points) |
| `usage_calc_remaining_save_typed` | Use typed figure |
| `usage_calc_remaining_clear` | Clear typed figure |
| `usage_calc_counter_title` | Count views on this device |
| `usage_calc_counter_hint` | Off by default. Kept only in this browser and never sent to the daemon. |
| `usage_calc_counter_on` | On |
| `usage_calc_counter_off` | Off |
| `usage_calc_counter_show` | Show counts |
| `usage_calc_counter_row` | %1$s: opened %2$s times, advice used %3$s times |
| `usage_calc_counter_clear` | Clear counts |
| `usage_calc_field_read` | Read |
| `usage_calc_field_days` | Days since reset |
| `usage_calc_field_pace` | Pace line |
| `usage_calc_headroom` | %1$s points of headroom |
| `usage_calc_ahead_of_pace` | ahead of the pace line |
| `usage_calc_field_burn` | Burn per 24 hours |
| `usage_calc_burn_fallback` | week average used, no reading in the last 2 hours |
| `usage_calc_field_projected` | Projected end at reset |
| `usage_calc_field_runout` | Runs out |
| `usage_calc_runout_none` | none before the reset |
| `usage_calc_hours_early` | %1$s hours early |
| `usage_calc_field_verdict` | Verdict |
| `usage_calc_field_lanes` | Lanes and policy |
| `usage_calc_lanes_policy` | %1$s lanes, %2$s |
| `usage_calc_field_advice` | Advice |
| `usage_calc_chosen_line` | Chosen: %1$s lanes, %2$s per 24 hours, ends at %3$s |
| `usage_calc_gauge_title` | Weekly allowance |
| `usage_calc_gauge_now` | Now %1$s% |
| `usage_calc_gauge_pace` | Pace %1$s% |
| `usage_calc_gauge_recent` | Recent burn %1$s% |
| `usage_calc_gauge_average` | Week average %1$s% |
| `usage_calc_gauge_over_edge_recent` | Recent burn: over, past the edge (%1$s%) |
| `usage_calc_gauge_over_edge_average` | Week average: over, past the edge (%1$s%) |
| `usage_calc_series_title` | Readings since the reset |
| `usage_calc_series_pace` | Pace line |
| `usage_calc_series_projection` | Projection to reset |
| `usage_calc_days` | %1$s days |
| `usage_calc_points_range` | Enter a number of points, 0 or more. |
| `usage_calc_lane_table_title` | What if: each lane count to the reset |
| `usage_calc_lane_col_lanes` | Lanes |
| `usage_calc_lane_col_burn` | Burn per 24 hours |
| `usage_calc_lane_col_end` | Projected end at reset |
| `usage_calc_lane_col_runout` | Runs out |
| `usage_calc_lane_col_early` | Hours early |
| `usage_calc_lane_col_fit` | Fit |
| `usage_calc_lane_fits` | fits |
| `usage_calc_lane_over` | does not fit |
| `usage_calc_lane_running` | running |
| `usage_calc_lane_none` | none |
| `usage_calc_lane_badge_advised` | Advised |
| `usage_calc_lane_badge_chosen` | Your choice |
| `usage_calc_lane_badge_never` | Never advised |
| `usage_calc_lane_badge_operator` | Operator's decision |
| `usage_calc_lane_target_use_all` | Target for this policy: %1$s% at the reset, the full allowance. |
| `usage_calc_lane_target_pace` | Target for this policy: %1$s% at the reset, with the safety margin. |
| `usage_calc_lane_target_none` | Run flat out has no end target: every lane runs, so every row reads running. |
| `usage_calc_lane_finish_needs_work` | Enter the remaining work to see the finish time. |
| `usage_calc_lane_finish_no_burn` | No finish time: no lane is burning above the idle baseline. |
| `usage_calc_lane_finish_within` | Work finishes %1$s, %2$s points to spare. |
| `usage_calc_lane_finish_late` | Work finishes %1$s, after the allowance runs out. |
| `usage_calc_lane_timeline_title` | Week timeline: each lane count from the newest reading |
| `usage_calc_lane_timeline_aria` | Week timeline from the last reset to the next reset, with the readings and one line per lane count |
| `usage_calc_lane_timeline_now` | Now %1$s |
| `usage_calc_lane_timeline_hint` | A tick marks every 24 hours. Each line runs from the newest reading to the reset, or to the point where that lane count runs out. |
| `usage_calc_lane_line_label` | Lanes %1$s |
| `usage_calc_lane_line_runout` | Lanes %1$s run out %2$s |
| `usage_calc_lane_per_lane_note` | Burn per lane: %1$s per 24 hours, inferred, recalibrate at each reading. |
| `usage_calc_lane_fields_summary` | Reading fields |
| `usage_calc_ledger_chart_title` | Points used each day since the reset |
| `usage_calc_ledger_chart_note` | Each bar is the points used that day. The totals are estimates from straight-line interpolation between readings, and together they sum to the newest percent. |
| `usage_calc_ledger_chart_aria` | Bar chart of the points used each day since the reset |
| `usage_calc_ledger_bar_title` | %1$s: %2$s points |
| `usage_calc_ledger_bar_title_estimated` | %1$s: %2$s points, estimated |
| `usage_calc_ledger_day_partial` | so far |
| `usage_calc_ledger_daily_empty` | No day totals yet. They appear once a reading follows the reset. |
| `usage_calc_ledger_kind_note` | A split by kind of work needs session attribution, which does not exist yet, so each day shows one total. |
| `usage_calc_ledger_cost_title` | Cost per Automaton |
| `usage_calc_ledger_cost_col_lanes` | Lanes |
| `usage_calc_ledger_cost_col_measured` | Measured, per 24 h |
| `usage_calc_ledger_cost_col_planning` | Planning, per 24 h |
| `usage_calc_ledger_cost_note` | The measured and planning columns can disagree. The planning column is not a forecast: it is the queue size spread over the lanes until the reset. |
| `usage_calc_ledger_cost_empty` | No lane counts in this response yet. |
| `usage_calc_ledger_queue_title` | Queue |
| `usage_calc_ledger_queue_line` | Queued Automata: %1$s, at %2$s points each. |
| `usage_calc_ledger_queue_estimate` | Estimate: %1$s points (queued Automata times the cost per Automaton). |
| `usage_calc_ledger_queue_no_estimate` | Estimate: none, because no cost per Automaton is set. |
| `usage_calc_ledger_queue_typed` | Typed override: %1$s points. |
| `usage_calc_ledger_queue_no_typed` | Typed override: none. |
| `usage_calc_ledger_queue_in_use` | In use: %1$s. |
| `usage_calc_ledger_in_use_estimate` | the estimate |
| `usage_calc_ledger_in_use_typed` | the typed override |
| `usage_calc_ledger_in_use_none` | no figure |
| `usage_calc_ledger_consumers_title` | Other consumers |
| `usage_calc_ledger_consumers_empty` | No consumer registry exists yet, so other consumers of the weekly allowance are not listed, and no figure is shown for them. |
| `usage_calc_ledger_text_title` | Text form |
| `usage_calc_ledger_text_cli` | CLI |
| `usage_calc_ledger_text_comm` | Comm channel |
| `usage_calc_ledger_text_empty` | No text form in this response. |

