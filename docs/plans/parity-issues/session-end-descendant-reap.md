# fix(sessions): processes a worker started outlive a cancelled task, parity note

Parent release: the next patch above main at merge time.

## Affected area

Daemon-side session teardown and Automata task cancel. The change lives in the orphan reaper (`internal/autonomous/orphan_reaper.go`), the recorded-descendant helpers (`cmd/datawatch/descendant_processes.go`) and the session-end wiring in `cmd/datawatch/main.go`. No PWA screen, setting, REST field or locale key was added.

## Current behavior

Before the fix, cancelling a worker task could leave the commands the worker had launched (test runs, servers, helper scripts) running after the session ended. Now every session end, including cancel and failure, finds those leftovers and ends them. A process counts as the session's only on evidence read from `/proc`: its working directory, executable or script lies in the session's worktree or scratch folder, its ancestry reaches the session, or its environment carries the session id. Nothing is matched by name, and the evidence and start time are re-checked just before each signal. The reap is recorded as one decision on the task and one audit entry (`session_end_reap`), carrying the count and short commands.

## Expected behavior on the mobile app

The mobile app needs no new control. Three things should simply keep working:

- A cancelled or failed task still shows as cancelled or failed through the existing status events; no new status value is added.
- The new decision text and the `session_end_reap` audit entry show up in the task's decision list and the audit view as ordinary entries, with no special rendering.
- Nothing in the app may suggest that a cancel leaves a worker's own processes running.

## Parity gap

There is no functional gap left by this change, because the reaper is internal and has no setting or endpoint. The only open point is a check: if the app lists audit actions from a fixed set, `session_end_reap` must fall through to a generic label rather than being dropped or shown blank.

## Suggested next step

Have the mobile app owner confirm that an unknown audit action renders with its raw name and details. If it does not, file a small follow-up on the app to add a generic fallback. No translation work is needed, since no locale key was added.

## Filing record

Not filed. This is a draft only; no issue was created on any tracker.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | excluded | The reaper is internal and always on by design: no new endpoint, no new config key. Its outcomes are observable on the existing audit-log and task-decision endpoints, whose contracts are unchanged. |
| MCP | excluded | Same as REST — no MCP tool is added; the reaper adds no setting or endpoint for a tool to wrap. |
| CLI | excluded | No CLI command and no `configure` target: there is nothing configurable to set or read. |
| comm channel | excluded | No setting to configure and no readable knob to report — the reap's evidence is in the audit entry, not a comm-channel value. |
| YAML/config | excluded | Deliberately not configurable — the failure it prevents is a host you own, so the reap is always on by design. |
| PWA | excluded | No PWA screen, setting, REST field or locale key was added; the `session_end_reap` audit entry and task decision must simply render via the existing audit and decision views as ordinary entries. |
| Android | excluded | No new control is needed; the one open point tracked in this draft is that an unknown audit action (`session_end_reap`) falls through to a generic label rather than being dropped. |
| iPhone/iOS | excluded | Same as Android — pass-through rendering of the existing audit and decision payloads, tracked by this draft. |
