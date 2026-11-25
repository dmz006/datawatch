# Mobile parity: designated supervisor wake target and session effort

Draft only; no issue filed.

## What changed in datawatch

A supervisor wake now types only into one designated session. The role or tag
of a session no longer makes it a target. The wake status says which session
would get a line now, and why. Separately, a named session effort `quick`,
`normal` or `thorough` now sets Claude Code's `--effort` to `low`, `medium` or
`high`. A launch where neither the request nor the worker profile names an effort
sends no `--effort` flag, as before. A configured default of `quick` or
`thorough` is mapped too; the built-in `normal` is not.

## New config key

| Key | Type | Default | Meaning |
|---|---|---|---|
| `autonomous.supervisor_loop.wake.session` | string | empty | The full or short id of the only session a wake types into; empty types nothing. |

## Changed responses

- `GET /api/supervisor-loop/wake` adds `session`, `target`, `target_code`
  (`ok`, `none_designated`, `not_found`, `ambiguous`, `not_live`, `completed`,
  `output_unreadable`) and `target_reason`.
- A session record may carry `claude_effort` and `effort_named`. A record's
  `effort` of `normal` without `effort_named` set no Claude level.

## What the mobile app needs

- A "Designated supervisor session" text field in the Supervisor wake settings
  group.
- The target sentence on the wake status line, translated by `target_code`, with
  `target_reason` for a code the app does not know.
- The new strings in the five locales (nine web keys:
  `supervisor_loop_wake_session`, its hint and seven `supervisor_wake_target_*`).
- If the app explains the session effort choice, say which Claude level each one
  sets, and that leaving it unchosen sends none (unless the configured default is
  `quick` or `thorough`). The app should not send `effort` unless the user picked
  one.
