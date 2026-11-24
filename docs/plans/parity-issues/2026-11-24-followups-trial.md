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

## 3. Lane fill eligibility reasons (docs only)

- `operator_pause` joins `already_shipped` in the `eligibility_reason` list of `GET /api/autonomous/lane-fill`
  decisions; the daemon now documents eleven reasons. `work_closed` is never a lane fill chip. If the app maps
  reason codes to labels, add `operator_pause` ("Paused by the operator"); the PWA has no label for it yet
  (`lane_fill_eligibility_operator_pause` is missing from the locale bundles, noted in the report).

## 4. Pause: automation work and the server picker

- **Wording.** The pause control is for automation work. PWA strings changed in all five locales:
  `pause_modal_title`, `pause_badge_title_paused`, `pause_badge_title_idle`, `pause_hint`. The header button title
  and aria-label read "Pause automation work".
- **Server picker.** `POST /api/pause` and `POST /api/pause/resume` already take `peers` and `all_peers`. The PWA
  now offers a picker when the header server selector is on All: this server only (default, no extra fields), one
  server (the same request sent to `/api/proxy/{name}/api/pause[/resume]`), or all servers (`all_peers: true`).
  `GET /api/pause?peers=1` returns each peer's own state under `peers`; the window lists them.
- **Strings:** `pause_target_label`, `pause_target_local`, `pause_target_all`, `pause_peer_paused`,
  `pause_peer_clear` in `internal/server/web/locales/*.json`.
- No new endpoint, setting or tool. A paused daemon stops placing work on federated hosts and on local and cloud
  queues; it does not pause the federated host itself.

## 5. Session start leases

No client change. A test now lists every session start door; one open gap is recorded in the follow-ups report
(restarting an ended session in place takes no lease).
