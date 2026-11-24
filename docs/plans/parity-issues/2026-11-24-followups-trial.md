# datawatch-app parity draft: follow-ups on schedules, lane fill reasons and pause (file only, no issue filed)

Date: 2026-11-24. One draft for the four daemon changes that touch the mobile client. Sections are added as each change lands.

## 1. One-time schedule wait limit

- **Setting.** `schedule.one_time_max_wait_hours` (integer, default 12, minimum 1) in the `schedule` object of
  `GET /api/config`, written with `PUT /api/config`. Add one number field to the Scheduled Spawns settings.
- **Schedule rows.** `GET /api/schedules` entries gain `waiting_since` (RFC 3339, absent when not waiting). A
  one-time schedule past the limit has `state` `failed`, `rearm_state` `gave_up`, `consecutive_failures` 0 and a
  `last_error` that starts "gave up waiting for capacity after 12 hours". Show "Gave up waiting for capacity
  (N tries)" for `gave_up` with no failures and some waits.
- **Alert.** New error alert "Scheduled spawn gave up waiting for capacity: <name>".
- **Strings** (en, de, es, fr, ja in `internal/server/web/locales/*.json`): `settings_schedule_one_time_max_wait`,
  `settings_schedule_one_time_max_wait_help`, `schedule_rearm_gave_up_waiting`.
- No new endpoint, tool or auth change. Old clients ignore the new field.
