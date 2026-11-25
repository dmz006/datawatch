# feat(schedules): a command schedule created with a session name waits for the next session with that name (datawatch next release)

Parent release: datawatch next release

## What changed in datawatch

A command schedule created with a session name is no longer cancelled or failed when its session is killed, deleted or ends. It stays `pending` and is typed into the next live session with that name. A recurring one skips the fires it misses. One warn alert is raised per wait. Schedules created with a session id only behave as before. Schedules that had already failed or been cancelled are not revived.

## Changed REST contract

No new endpoint or setting. Schedule entries from `GET /api/schedules`:

- `rearm_state` can now be `waiting_session`. The entry stays `pending`.
- `name_wait_alerted` (boolean, omitted when false) is true once the alert was raised.
- While waiting, `last_error` reads `no live session named "<name>"`. A recurring entry that skipped a fire has `last_fire_result` `skipped` and a `fire_history` row.
- A delivery clears `rearm_state`, `last_error`, `last_fire_result` and `name_wait_alerted`.

A client must tolerate a `rearm_state` it does not know.

## Client work

- Schedule rows: show "Waiting for a session named <name>" for `waiting_session` (new web string `schedule_rearm_waiting_session`, in en, de, es, fr, ja).
- The new alert arrives as an ordinary system alert; nothing to add.
- Do not treat a schedule whose `session_id` is a closed session as dead when it has a `session_name` and is `pending`.
