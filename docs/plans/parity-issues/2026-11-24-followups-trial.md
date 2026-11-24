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

## 2. An Automation goes past a delivered member

- No new endpoint, setting or tool. `GET /api/automations/{id}` members can now end `completed` with `outcome`
  `already_shipped`, `started_at` absent, and the Automation `decisions` can carry `kind` `member_delivered`
  (note: member, release, commit). The app should show such a member as "Delivered, skipped" rather than "Completed",
  and list the decision. An Automation that blocks because a later member builds on the delivered member's branch
  carries the explanation in its last `blocked` decision and in the "Automation blocked" alert.
- Audit entry `automation_member_delivered` (automation_id, prd_id, release, commit).
