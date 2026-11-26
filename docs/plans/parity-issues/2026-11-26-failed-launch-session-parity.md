# [sync] A Claude session that exits before it gets its task is now a failed launch

Draft only. No issue has been filed on any tracker.

## Affected area

The session list, session detail and alerts in every client of the daemon: the PWA, the Android and iOS apps (datawatch-app), the CLI, MCP and the comm channels.

## What changed in the daemon

A claude-code session now ends `failed` in two cases: its agent exits soon after datawatch declined a start-up dialog with Esc, or the agent of a new channel session exits before datawatch has typed its first task. Before, such a session could be reported `complete`, or switch between `complete`, `running` and `waiting_input`. Restarted sessions and sessions someone has used are not failed for the task. The daemon closes the leftover terminal and does not bring the session back. No setting, endpoint or key changed, and the keys datawatch sends to dialogs are unchanged.

## Data a client sees

- `state: failed`, one warning alert whose body is the reason (not the task text), and the same reason on the chat state line.
- Two new optional session fields: `launch_failure` (string) and `startup_decline` (`dialog`, `files`, `at`).

## What the mobile app needs

- Nothing is required: the state and alert already display.
- Optional: show `launch_failure` on the session detail when it is set. The reason can be long (it lists file paths), so wrap it.

## Not changed

The PWA has no new element and no new locale key.
