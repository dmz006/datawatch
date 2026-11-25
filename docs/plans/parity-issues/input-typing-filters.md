# Mobile parity: output filters that type into sessions

Draft for the datawatch-app clients (Android and iOS). File only; no issue filed.

## What changed in the daemon
- No built-in filter types into a session. An upgrade turns off the old one.
- Filters with action `send_input` or `schedule` need `allow_input: true` to be
  created (or to convert another filter), are returned with `enabled: false`,
  and are turned off again by any edit.
- `GET /api/filters` entries may carry `disabled_reason`.
- `PATCH /api/filters` can answer 400 with a refusal text; the reply of a
  successful change carries `enabled`.
- New setting `session.input_filters_enabled` (boolean, default false).
- Text a filter types starts with `[datawatch filter <id> typed this, not the
  operator]`; it appears in last input, timeline and conversation.

## What the apps need
- Filter list: a "types input" badge on `send_input` and `schedule` filters and
  the `disabled_reason` text.
- Create and edit: ask for confirmation for those actions and send
  `allow_input: true`; tell the user the filter was saved turned off.
- Show the server's refusal text instead of a generic failure.
- Settings, Sessions: the switch with the same help text as the web app.
- Locale keys (en, de, es, fr, ja): `settings_input_filters_enabled`,
  `settings_input_filters_enabled_help`, `filter_types_input_badge`,
  `filter_types_input_note`, `filter_types_input_confirm`, `filter_saved_off`,
  `filter_value_placeholder`.
- Session view: do not present a line carrying the filter mark as the user's
  own message.
