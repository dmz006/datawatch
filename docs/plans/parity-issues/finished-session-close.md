# Parity draft (datawatch-app): finished session close, and per-session permission mode

File only; no issue filed.

## New setting
`session.finished_idle_close_minutes` (whole minutes; 0 = off, the default; 12
suggested). Read from `GET /api/config` → `session.finished_idle_close_minutes`,
written with `PUT /api/config`. The app's session settings screen needs one
number field with the same label and help as the web app
(`settings_session_finished_idle_close`, `..._help`).

## Behaviour the app shows
A session ended this way arrives as `complete` (not `killed`) with
`completion_summary` and an `end_note` naming the setting. No new endpoint.

## New session JSON fields
- `permission_mode` (string, absent when the start named none).
- `awaiting_input_after_relaunch` (bool, absent when false).

Both are read only; the app needs no UI for them.
