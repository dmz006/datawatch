# Mobile parity: designated supervisor wake target and session effort

Draft only; no issue filed.

## What changed in datawatch

A supervisor wake now types only into one designated session. The role or tag
of a session no longer makes it a target. The wake status says which session
would get a line now, and why. Separately, the session effort `quick`, `normal`
and `thorough` now sets Claude Code's `--effort` to `low`, `medium` and `high`.

## New config key

| Key | Type | Default | Meaning |
|---|---|---|---|
| `autonomous.supervisor_loop.wake.session` | string | empty | The full or short id of the only session a wake types into; empty types nothing. |

## Changed responses

- `GET /api/supervisor-loop/wake` adds `session`, `target`, `target_code`
  (`ok`, `none_designated`, `not_found`, `ambiguous`, `not_live`, `completed`,
  `output_unreadable`) and `target_reason`.
- A session record may carry `claude_effort`.

## What the mobile app needs

- A "Designated supervisor session" text field in the Supervisor wake settings
  group.
- The target sentence on the wake status line, translated by `target_code`, with
  `target_reason` for a code the app does not know.
- The new strings in the five locales (nine web keys:
  `supervisor_loop_wake_session`, its hint and seven `supervisor_wake_target_*`).
- If the app explains the session effort choice, say which Claude level each one
  sets.
