# Schedule Spawn — Ephemeral Session Scheduling (GH#128)

**Date:** 2026-06-15
**Version at planning:** v8.10.25
**Ships in:** v8.11.0
**Issue:** dmz006/datawatch#128

## Scope

Adds a `spawn` schedule type: schedule a fresh ephemeral one-shot session
(task + path + backend/LLM) to fire at a fixed time or on a recurring cron,
completely independent of any running session.

Primary use case: imap-mcp hourly email-check replaces a session-bound command
schedule with a session-independent spawn that creates a clean isolated session,
runs the task to completion, then terminates without leaving persistent state.

## Files Affected

- `internal/session/schedule.go` — new `SchedTypeSpawn`, extended `DeferredSession`, `AddSpawn`
- `internal/session/manager.go` — pass new `DeferredSession` fields to `StartOptions`
- `internal/server/api.go` — `handleSchedules` POST: spawn type support
- `cmd/datawatch/main.go` — `schedule spawn` CLI subcommand (both `session schedule` and top-level)
- `internal/mcp/server.go` — `schedule_spawn` MCP tool
- `internal/router/commands.go` — comms channel `schedule spawn` command
- `docs/datawatch-definitions.md` — Schedule section update
- `CHANGELOG.md`, version files → v8.11.0

## Design

### New schedule type: `spawn`

```
SchedTypeSpawn = "spawn"
```

Semantically identical to `new_session` but carries additional fields in
`DeferredSession` and defaults to `OneShot=true, Ephemeral=true`. The
`new_session` type is unchanged (backward compat). Both types are processed
by the same scheduler tick.

### Expanded `DeferredSession`

```go
LLMRef    string `json:"llm_ref,omitempty"`    // unified registry LLM name
Model     string `json:"model,omitempty"`       // model override
Effort    string `json:"effort,omitempty"`      // quick/normal/thorough
OneShot   bool   `json:"one_shot,omitempty"`    // auto-terminate on DATAWATCH_COMPLETE:
Ephemeral bool   `json:"ephemeral,omitempty"`   // clean workspace on delete
```

### Cron recurrence

`ScheduledCommand.CronExpr` already exists. `MarkDone` already reschedules
cron-based entries. The scheduler passes `CronExpr` through to the existing
`MarkDone` path — no changes needed for recurrence.

### API (`POST /api/schedules`)

```json
{
  "type": "spawn",
  "command": "task text for the new session",
  "project_dir": "/path/to/workspace",
  "backend": "claude",
  "llm_ref": "",
  "model": "",
  "effort": "normal",
  "one_shot": true,
  "ephemeral": true,
  "name": "spawned-session-name",
  "cron_expr": "0 * * * *",
  "schedule_name": "imap-hourly",
  "run_at": ""
}
```

### CLI

```bash
datawatch schedule spawn \
  --task "check and process new emails" \
  --dir /home/dmz/workspace \
  --backend claude \
  --cron "0 * * * *" \
  --schedule-name imap-hourly \
  --name imap-spawn
```

Also under `datawatch session schedule spawn`.

### MCP

```json
{ "tool": "schedule_spawn",
  "task": "check and process new emails",
  "project_dir": "/home/dmz/workspace",
  "backend": "claude",
  "cron_expr": "0 * * * *",
  "schedule_name": "imap-hourly",
  "name": "imap-spawn",
  "one_shot": true,
  "ephemeral": true }
```

### Comms channel

```
schedule spawn task=<text> [dir=<path>] [backend=<b>] [cron=<c>] [name=<sched-name>] [session-name=<n>]
```

## Phases

| # | Phase | Status |
|---|-------|--------|
| 1 | schedule.go — type + struct + AddSpawn | Done |
| 2 | manager.go — scheduler tick update | Done |
| 3 | api.go — spawn handler | Done |
| 4 | main.go — CLI spawn command | Done |
| 5 | mcp/server.go — schedule_spawn tool | Done |
| 6 | router/commands.go — comms channel | Done |
| 7 | Tests | Done |
| 8 | Docs + version v8.11.0 | Done |
