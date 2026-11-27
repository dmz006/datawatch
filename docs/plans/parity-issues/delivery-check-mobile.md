# Parity: the delivery check (datawatch-app)

Draft only; no issue filed.

## Server behaviour

datawatch keeps one list of work that was not delivered or is not finished and
checks it every few minutes. The check only reports; it blocks nothing. Entries
hold identifiers, kinds, states, reason codes and times only. See
`docs/howto/delivery-check.md`.

- `GET /api/delivery` returns the report without running a check. A disabled
  check answers `503` with "the delivery check is not available".
- `POST /api/delivery/check` runs a check and returns the report.
- Report fields: `report_only`, `result` (`green`/`red`), `red` (codes), `at`,
  `pass`, `items`, `backlog`, `accepted`, `guarded`, `unrecorded`, `by_rule`,
  `by_kind`, `sources` (`name`, `ok`), `channels`, `heartbeat`, `start_mark`,
  `non_operator_actions`, `uncovered`. Lists may be empty or absent.
- Entry fields: `key`, `kind`, `id`, `state`, `invariant`, `reason`,
  `waits_on`, `since`, `review_by`, `count`, `backlog`, `told_at`,
  `last_sent_at`, `send_fails`, `seen_at`, `seen_by`, `accepted`
  (`reason`, `until`, `by`).
- `POST /api/delivery/ack`, `/accept`, `/drop` take `keys`, `kind`, `state`,
  `match_reason`, `backlog` (at least one) and `actor`; accept also takes
  `reason` and `until`, drop takes `reason`. They answer `{"changed": n}`.
  An unknown field is refused. Refusals are plain text: 400, 404, 409, 503.
- `POST /api/delivery/test-message` answers
  `{"results": [{"channel", "ok", "code"}]}`.
- Settings in `GET /api/config` under `delivery`: `enabled`, `notify`,
  `interval_seconds`; `PUT /api/config` takes `delivery.<key>`.

## Mobile client

- A delivery view: result, "report only", why it is red (a known code shows its
  text, an unknown one is shown as it is), new, backlog and accepted items,
  check now.
- Actions: acknowledge (the only one an alarm or clock-jump notice takes), accept with a reason code and a date (a plain date means the end of that day, UTC; at most 30 days
  ahead), drop with a reason code and a confirmation, send test message and its
  result per channel. Show the daemon's refusal text.
- Never show an empty list as green when the request failed or the check is off.
- Do not draw a very long list at once (the web app draws 100 rows per group).
- The three settings.
- New strings in en, de, es, fr and ja.
