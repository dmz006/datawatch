# Mobile parity draft: schedules that did not run, and session notices

Draft only; no issue has been filed.

## What the daemon now does
- A command schedule datawatch ends without typing it becomes `failed` or `cancelled`,
  with the reason in `last_error`, and raises a system warn alert.
- A schedule in `GET /api/schedules` may carry `kind` (`rate_limit_resume` or absent).
- A recurring command schedule (by session id or name) can be `pending` with
  `consecutive_failures` above 0 and a `last_error`. It alerts on the first
  failure in a row and every 24th, not on every one.
- A one-shot shell script session that exits non-zero ends `failed`, not `complete`.
- Creating a schedule for a missing or ended session is refused; the REST reply is an
  error whose body starts with `schedule refused:`.
- Two new system alerts: sessions found dead at daemon start, and running sessions
  silent past `session.stale_timeout_seconds`.

## What the app should check
- The alert list shows these system alerts (they have no session id).
- The create-schedule screen shows the refusal text rather than a generic failure.
- The schedule list tolerates the unknown `kind` field and shows `last_error` on
  `failed` and `cancelled` command schedules.

No new endpoint, setting or screen.
