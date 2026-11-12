# Parity issue draft — cost report (datawatch-app)

**Not filed yet.** This is the single mobile parity record for the cost report. File it on
**datawatch-app** as a sync issue (`[sync] feat(usage): cost report`), the way
`burn-guard.md` was filed as dmz006/datawatch-app#332, and record the issue URL and number
here before the merge. Parent release: the next patch above main at merge time.

## What changed in datawatch

A token-first cost report of the daemon's recorded sessions. For a window and a grouping it
counts the sessions per group and shows **tokens in / tokens out first**, and a **USD estimate
where and only where configured cost rates cover every backend in the group** — where a rate is
missing, the row shows tokens and `rate_configured` false, and no dollar figure is shown, not a
zero and not a fallback price. Groupings: `automaton` (default), `automation`, `backend`,
`week`. Window: `since`/`until`; absent `until` is now, absent `since` is the daemon's
7-day default. The body carries the 31-day retention the figures rest on
(`retention_days`), a `lifetime_fallback` flag, and, for each row, `sessions`, `tokens_in`,
`tokens_out`, `usd_estimate` (absent when there is none) and `rate_configured`. Settings under
`cost_report.*`: `default_group_by` (`automaton`), `default_window_days` (7),
`show_usd_estimates` (true), `week_start` (`monday`), `include_peer_spend` (false).
Howto: `docs/howto/cost-report.md`.

This is a capability-parity issue: the app needs the **read** and the **settings**.

## Read

`GET /api/cost/report[?group_by=<g>&since=<t>&until=<t>]` (RFC3339 or `YYYY-MM-DD` times;
`group_by` one of `automaton`, `automation`, `backend`, `week`; an absent parameter falls back
to the daemon's setting). Show it as a card next to the live cost card: the grouping, the
window, one row per group with the sessions and the tokens in and out **first**, the USD
figure only where the body carried one for that row, the retention line
("Figures rest on the last 31 days of recorded sessions" style, from the body), and the
lifetime-fallback note when the body says so. Do not compute, format or guess a USD where the
body does not carry one. The card refreshes on a short poll and tolerates a daemon that
answers 400/404/503 (or has no such route) by hiding the card or showing an error line.

## Settings (read and write)

The five `cost_report.*` keys through `GET`/`PUT /api/config`; one invalid value rejects the
whole PUT and stores nothing — show the daemon's message.

| Key | Label | Type | Default |
|---|---|---|---|
| `default_group_by` | Default grouping | one of `automaton`, `automation`, `backend`, `week` | `automaton` |
| `default_window_days` | Default window (days) | whole number | 7 |
| `show_usd_estimates` | Show USD estimates | boolean | on |
| `week_start` | Weeks start | `monday` or `sunday` | `monday` |
| `include_peer_spend` | Include peer spend | boolean | off |

## Locale keys

Add the card, row, state and settings strings to the five bundles (`en`, `de`, `es`, `fr`,
`ja`). The key set is the `cost_report_*` and `settings_cost_report_*` keys in
`internal/server/web/locales/en.json` of the datawatch repository (card title, help link,
refresh, group-by and window labels, each setting label and hint, the retention and fallback
wording); copy them with the same names.

## Acceptance

- The card shows the groups with session counts and tokens first, the USD only where the body
  carried it, the retention line and, when the body says so, the lifetime note.
- Changing the grouping, the window or `show_usd_estimates` changes exactly what the card
  shows, and a missing USD figure is never filled in.
- Each setting loads, saves and shows the daemon's rejection message on an invalid value.
- Nothing in the body is a spend figure the daemon did not price.

## Parity surface

| Surface | Included | Reason or where |
|---|---|---|
| REST | Yes | `GET /api/cost/report` with `group_by`, `since`, `until`; `GET`/`PUT /api/config` under `cost_report`; `GET /openapi.json` documents the route |
| MCP | Yes | the read-only `cost_report` tool (`group_by`, `since`, `until` optional) and the `cost_report.*` keys of `config_set` |
| CLI | Yes | `datawatch cost report [--group-by …] [--since …] [--until …] [--format json]`; `datawatch config set cost_report.<key>` |
| comm channel | Yes | `cost report [group_by=…] [since=…] [until=…]`; `configure cost_report.<key>=<value>` |
| YAML/config | Yes | the `cost_report` block in `docs/config-reference.yaml` |
| PWA | Yes | the Cost report card on the dashboard, the five settings fields with help links, and the locale keys in all five bundles |
| Android | Sync issue | this file |
| iPhone/iOS | Sync issue | this file |
