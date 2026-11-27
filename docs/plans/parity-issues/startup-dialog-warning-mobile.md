# Parity: the start-up dialog warning (datawatch-app)

Draft only; no issue filed.

## Server behaviour

A new system alert, level `warn`, with no `session_id`, at most one for each
session start. Title, one of:

- `start-up dialog still up after 2 answers; answer it yourself`
- `start-up dialog still up when the start-up check ended; answer it yourself`

Body: `session <name> [<id>]`, or `session <id>` for a session with no name. It
holds nothing from the session's screen. No new endpoint, field or setting.

## Mobile

The alert arrives as other system alerts do (alert list and push), so no change
is needed to show it. The text comes from the server in English, as other
daemon alerts do: there is no new locale key. Check that the system alerts list
shows both titles in full. See `docs/howto/sessions-deep-dive.md`.
