# [sync] Lane utilization: a read of how busy each lane was, with a dashboard card and Gantt lane rows

Draft only. No issue has been filed on any tracker.

## Affected area

The Automata lane view in the daemon and every client that shows lane or dashboard data: the PWA (already updated), the Android and iOS apps (datawatch-app), the CLI, MCP and the comm channel.

## What changed in the daemon

A new read-only view reports how the execution lanes were used over a window that ends now. Nothing is stored; each read works the figures out from the Automata records and the lane fill state. There is no new setting.

- **Route:** `GET /api/autonomous/lane-utilization`. Optional query values: `window` (a duration such as `6h`, `24h` or `7d`, from 1 hour to 30 days, default 24 hours), `bucket` (a whole number from 1 to 288, default 24) and `decisions` (a whole number from 1 to 20, default 20). A bad value answers 400. It answers an error while the autonomous feature is off.
- **Response:** `window_start`, `window_end`, `window_seconds`, `bucket_count`, and then:
  - `lanes[]`: `id`, `type` (`gpu`, `claude` or another backend name), `busy_seconds`, `idle_seconds`, `busy_share`, `idle_share`, `runs[]` (`prd_id`, `title`, `stage`, `kind`, `backend`, `start`, `end`; a run with no end is still running), `queue_wait` (`count`, `mean_seconds`, `max_seconds`), `idle_with_eligible_work`, `idle_with_eligible_seconds`, `first_eligible_item`, and `buckets[]` (`start`, `end`, `busy_share`).
  - `backend_mix[]`: `backend`, `busy_seconds`, `share`.
  - `lane_fill`: `mode`, `modes`, `decisions[]`.
  - `plan_usage`: `plan_points`, `points`, `tokens`, `percent_of_plan`.
- **No currency:** no field carries a dollar amount. The plan is flat-rate, so usage is points and tokens only. Apps must not add a money figure.
- **Other surfaces:** MCP tool `autonomous_lane_utilization` (read-only, same optional values, JSON unchanged); CLI `datawatch autonomous lane-utilization [--window] [--bucket] [--decisions] [--json]`; comm verb `autonomous lane-utilization [window]` (at most 15 lines).

## What the PWA already does

- A **Lane utilization** dashboard card: a window picker (1 hour, 6 hours, 24 hours, 7 days), a stacked graph of busy share per lane, a table per lane (busy, idle, runs, mean queue wait, backend mix, flag), the backend mix, the lane fill mode with its newest decisions, and the plan usage line. It reloads every 30 seconds.
- The dashboard Gantt chart gains a **Lane utilization** layer: one row per lane with a bar for each run, from the same data. A lane with no runs still gets a row.
- New locale keys, all with the prefix `lane_utilization_`, present in all five locales (en, de, es, fr, ja) with the same key set.

## Expected behavior in the apps

1. A lane utilization screen or card: window choice, one row per lane with busy and idle percent, runs count and mean queue wait, and the backend mix.
2. Show the idle-with-eligible-work flag on a lane, with the first item it could have taken, in a way that stands out from a plain idle lane.
3. Show the lane fill mode and the newest decisions; a decision with `executed: false` reads as "would start", not "started".
4. Show plan usage as points of the plan, percent and tokens. Never show dollars.
5. If the apps have a Gantt or timeline view, add the lane rows from `lanes[].runs`.
6. Reuse the `lane_utilization_` locale strings.

## Compatibility

No migration is needed. The route and tool are new; older apps ignore them.

## Tests

Daemon side: unit tests for the builder (busy and idle, queue wait, mixed backends, the idle-while-eligible flag, clamping, no currency keys), the REST route (defaults, custom values, bad values, wrong method, feature off, federation), the MCP tool, the CLI and the comm verb. PWA side: node tests for the card, the escaping, the locale keys, the loader and the Gantt lane rows. A live browser run and a live daemon run were not done before release.
