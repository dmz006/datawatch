# Parity issue draft — burn guard (datawatch-app)

Filed on **datawatch-app** as dmz006/datawatch-app#332 on 2026-11-02, titled `[sync] feat(usage): burn guard status, settings and alerts`.
Parent release: the next patch above main at merge time.

## What changed in datawatch

A new burn guard compares the projected weekly burn of the Claude sessions with the pace line of the usage calculator. When the projection runs over the line plus a margin, it steps down a ladder of admission changes (lower the Claude inflight cap, route new Claude work to `pi-prompt`, hold new Claude work) and steps back when the burn is under the line. It never ends a running session. Each change raises one warning alert and one queue question. Task kinds also get an effort ceiling, and a task has a limit on retries on Claude. Howto: `docs/howto/burn-guard.md`.

This is a capability-parity issue: the app needs the guard's **status**, its **settings** and its **alerts**.

## Status (read)

`GET /api/usage/burn-guard` returns the state (`off`, `idle`, `guarding`, `waiting`), the rung in force and the rungs applied, the last projection (projected weekly percent, points per hour, allowed per hour, the pace line `line` and the `margin`, over or under), the last decision, the fitted cache coefficients, the last error and the counters. Show it on the usage calculator screen as a card: state, rung n of m, projection against the line, last decision. Treat unknown fields as ignorable.

`POST /api/usage/burn-guard/run` evaluates now and returns the decision and refreshed status; it answers an error when the guard is not running on the daemon. Add a **Run now** button.

## Settings (read and write)

`GET`/`PUT /api/usage/burn-guard/config` (the same keys through `PUT /api/config` under `usage.burn_guard`). One invalid value rejects the whole PUT and stores nothing; show the daemon's message.

| Key | Label | Type | Default | Range |
|---|---|---|---|---|
| `enabled` | Burn guard on | boolean | on | |
| `interval_minutes` | Minutes between checks | whole number | 60 | 1 to 1440 |
| `margin_points` | Margin over the pace line (points) | number | 2.0 | 0 to 50 |
| `ladder` | Ladder order | ordered subset of `lower_inflight`, `route_pi_prompt`, `hold_claude` | all three in that order | |
| `step_back` | Step back when under the line | boolean | on | |
| `cache_write_points_per_million` | Cache-write price (points per million tokens) | number | 0.373 | above 0, at most 1000 |
| `cache_read_points_per_million` | Cache-read price (points per million tokens) | number | 0.0069 | above 0, at most 1000 |
| `refit_on_reading` | Refit prices on each reading | boolean | on | |
| `retry_limit_claude` | Most retries of one task on Claude | whole number | 2 | 0 to 20 |

`effort_ceiling` (task kind to `low`, `medium`, `high` or `cheaper_backend`) is part of the config response and is shown **read-only**; it is set per Automaton and per task through the Automaton routes, not through this settings call.

## Alerts (read)

Each step raises a system alert titled `Burn guard: climb <rung>` or `Burn guard: step_back <rung>` and one queue question, both carrying the numbers and what was done. They arrive through the existing alert and question lists; no new endpoint. The work is to make sure the app renders them like any other alert and question, and links the alert to the burn guard card.

## Locale keys

Add the card, settings and state strings to the five bundles (`en`, `de`, `es`, `fr`, `ja`). The key set is the `burn_guard_*` keys in `internal/server/web/locales/en.json` of the datawatch repository (title, help, run now, state names, direction names, rung names, each setting label, the effort ceiling label and hint, the error and message strings); copy them with the same names.

## Acceptance

- The card shows state, rung, projection against the line and margin, and the last decision, and refreshes about once a minute.
- Run now works and shows the decision or the daemon's error.
- Each setting loads, saves and shows the daemon's rejection message on an invalid value.
- Alerts and questions from the guard render and open.
- Nothing in the app ends or restarts a session as a result of the guard.

## Parity surface

| Surface | Included | Reason or where |
|---|---|---|
| REST | Yes | `GET /api/usage/burn-guard`, `POST /api/usage/burn-guard/run`, `GET`/`PUT /api/usage/burn-guard/config`, `GET /api/config` under `usage.burn_guard`, `POST /api/automations/{id}/set_effort_ceiling` |
| MCP | Yes | `burn_guard_status`, `burn_guard_run`, `burn_guard_config_get`, `burn_guard_config_set`, `automation_set_effort_ceiling`, `autonomous_prd_set_task_effort_ceiling` |
| CLI | Yes | `datawatch usage burn-guard status|run|config`, `datawatch automation set-effort-ceiling` |
| comm channel | Yes | `usage burn-guard`, `configure usage.burn_guard.<key>=<value>`, `automation set-effort-ceiling` |
| YAML/config | Yes | the `usage.burn_guard` block in `docs/config-reference.yaml` |
| PWA | Yes | the Burn guard card on the usage calculator view, Settings > Usage burn guard, the Monitor stat card |
| Android | Yes | this issue: status card, settings and alerts |
| iPhone | Yes | the same issue text applies once the iOS app exists; there is no iOS app in this repository's release set, so it is tracked on the same issue |
