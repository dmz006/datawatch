# Mobile parity draft: requests

Draft, not filed. For `datawatch-app` (Android and iOS). To be completed with the surface list from the web, REST and tool work before filing.

## What the daemon gains

- Requests: saved items addressed to a role in a scope. States: waiting, delivered, accepted, reported, done, failed, expired, cancelled.
- Labels that must be shown as given. Only proven done reads "done": waiting; typed, unconfirmed; fetched, unconfirmed; delivered to the holder; in a completed model call; accepted by the holder; reported: finished, not accepted; done; delivered (not the same as done); done, accepted without proof; failed; expired; cancelled.
- Routes: `GET`/`POST /api/requests`, `GET /api/requests/mine`, `GET /api/requests/roles`, `POST /api/requests/roles/give`, `POST /api/requests/roles/release`, `POST /api/requests/from-backlog`, and `POST /api/requests/{id}/` `take`, `seen`, `accept`, `report`, `decline`, `reissue`, `drop`, `force`, `accept-unproven`. `take`, `seen`, `accept`, `report` and `decline` are for the holder's own session credential; the app does not call them.
- Six settings under `delivery`: `request_take_by_seconds`, `request_due_seconds`, `request_due_seconds_by_kind`, `reminder_interval_seconds`, `reminders_per_session_hour`, `reminders_per_host_hour`.

## Asked of the app

1. A request list with filters (state, role, scope), showing the labels above and reason codes. No request content is in any listing.
2. Role list; give (with force and a reason code) and release.
3. Operator actions with a reason code: reissue (with the "may run twice" confirmation), drop, force, accept unproven.
4. The six settings.
5. Five locales, as the web app.

## Not in the app

Typing, pi delivery and the "done when" check happen in the daemon.
