# [sync] A Claude session that quits at a declined start-up dialog now ends failed

Draft only. No issue has been filed on any tracker.

## Affected area

The session list, session detail and alerts in every client of the daemon: the PWA, the Android and iOS apps (datawatch-app), the CLI, MCP and the comm channels.

## What changed in the daemon

1. A claude-code channel session ends `failed` when datawatch declined a start-up dialog with Esc and the agent then left, its task never delivered and nothing else sent to it. Every other exit is handled as before.
2. Before typing a task, a scheduled command or a line the daemon writes on its own into a claude-code session, the daemon looks for two signs that the agent has left the terminal, and the send reports an error if either shows. It is not proof: the daemon types whenever neither sign shows, and MCP `send_input` and `send_saved_command` are not checked.

No setting, endpoint or key changed, and the keys datawatch sends to dialogs are unchanged.

## Data a client sees

- `state: failed`, one warning alert whose body is the reason (not the task text), and the same reason on the chat state line.
- Three new optional session fields: `launch_failure` (string), `startup_decline` (`dialog`, `files`, `at`) and `launch_input_sent` (bool).
- A scheduled command refused by the typing check is marked failed and is not retried; a recurring one stops.

## What the mobile app needs

- Nothing is required: the state, the alert and the error already display.
- Optional: show `launch_failure` on the session detail when it is set. The reason can be long (it lists file paths), so wrap it.

## Not changed

The PWA has no new element and no new locale key.
