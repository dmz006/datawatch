# Mobile parity draft: requests

Draft, not filed. For `datawatch-app` (Android and iOS).

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

## Surface list (REST and web, as built)

1. **Request list:** `GET /api/requests` with `state, kind, role, scope_kind, scope_id, holder` → `{"requests":[view]}`; one: `GET /api/requests?id=<id>` → view. A view has `id, kind, role, scope{kind,id}, state, label, reason_code, holder{session_id,family}, overdue, may_run_twice, proof_as, calls, created`. No content.
2. **Readings:** the 13 labels above. "done" only when `state` is `done` and `proof_as` is `done`; for other closed ones use `proof_as` (`delivered`, `accepted_unproven`; none: "closed, proof not stated"). Web keys: `request_label_*`.
3. **Roles:** `GET /api/requests/roles` → `{"roles":[binding]}` (show self-given and "taking not yet proven"); `POST …/roles/give` `{role, scope, session_id, force?, reason_code?}`; `POST …/roles/release` `{role, scope, session_id}`.
4. **Actions:** `POST /api/requests/{id}/reissue|drop|force|accept-unproven` with `reason_code`; `words` (required for accept-unproven); `repeat_ok` (reissue); `to_session` (force). Confirm drop and force; force says it may run twice. Clear typed words after each action.
5. **Errors:** plain text with 400, 403, 404, 409, 422 (other computer), 503; show as given. Through a server proxy every request route answers 422.
6. **Create** (`POST /api/requests` `{kind, role, scope, content{ask,…}, key?}` → `{request, created}`) and **from-backlog**: routes exist; the web has no form.
7. **Settings:** six under `delivery.`; `GET /api/config` gives the values in force under `delivery_requests`, and only set ones under `delivery`. `PUT` with `delivery.<name>`.
8. **Older queue:** `claimed_by` is no longer used; the list can answer 503.
9. Five locales. The app never calls take, seen, accept, report or decline.
