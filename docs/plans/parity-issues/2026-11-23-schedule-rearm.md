# datawatch-app parity draft: schedule re-arm state and settings (file only, no issue filed)

Date: 2026-11-23. Daemon change: scheduled spawns keep running after a refused or failed start.

## What the app needs

1. **Schedule list rows.** `GET /api/schedules` entries now carry `last_fire_at`, `last_fire_result`
   (`spawned`, `skipped`, `failed`, `deferred`), `last_error`, `rearm_state` (`""`, `retrying`,
   `waiting_capacity`, `gave_up`), `consecutive_failures`, `consecutive_waits`, `fire_count` and
   `fire_history` (newest 20, oldest first: `at`, `result`, `session_id`, `error`). Show under each
   schedule: the next fire (`run_at`), the last result with its reason, and the re-arm state with its
   count. A `failed` schedule shows no next fire; `gave_up` reads as an error.
2. **Settings.** Four keys under `schedule`: `rearm_mode` (`next_fire`, `backoff`, `stay_failed`),
   `rearm_max_retries`, `rearm_backoff_seconds`, `rearm_backoff_max_seconds`. Read from `GET /api/config`
   (`schedule` object), written with `PUT /api/config` using dotted keys. Invalid values are ignored.
3. **Alerts.** Two new system alerts: warn "Scheduled spawn is waiting for capacity: <name>" and error
   "Scheduled spawn gave up: <name>". They arrive through the normal alert feed; no new channel.

## Strings (English; the PWA has de, es, fr, ja)

`settings_schedule_rearm_mode`, `_mode_help`, `_max_retries`, `_backoff`, `_backoff_max`,
`schedule_row_next`, `schedule_row_no_next`, `schedule_row_last`, `schedule_result_spawned`,
`schedule_result_deferred`, `schedule_result_failed`, `schedule_result_skipped`, `schedule_rearm_waiting`,
`schedule_rearm_retrying`, `schedule_rearm_gave_up` in `internal/server/web/locales/*.json`.

## Not changed

No new endpoint, no new MCP tool, no auth change. Old clients ignore the new fields.
