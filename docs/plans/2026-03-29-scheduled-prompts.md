---
date: 2026-03-29
version: 0.7.4
scope: Schedule prompts to run at a future time without burning LLM tokens
status: planned
---

# Plan: Scheduled Prompts

## Problem

Users want to queue a prompt to run later ("in 2 hours", "at midnight", "next Wednesday at 9am") without keeping an LLM session active and burning tokens. The scheduler should use datawatch's internal timer, only starting the LLM session when the scheduled time arrives.

## Current State

datawatch already has a `ScheduleStore` (`internal/session/schedule.go`) that supports scheduling commands for sessions. The `schedule` command exists in the router (`schedule <id>: <when> <cmd>`). However:
- Schedules are tied to existing sessions (session must exist)
- No support for creating a NEW session at a scheduled time
- No natural language time parsing ("in 2 hours", "next wednesday")
- Web UI has no scheduling interface for new sessions

## Scope

- `internal/session/schedule.go` — extend with deferred session creation
- `internal/session/manager.go` — scheduled session launcher
- `cmd/datawatch/main.go` — `datawatch schedule` CLI command
- `internal/router/commands.go` — `schedule new:` command variant
- `internal/server/web/app.js` — schedule option on new session form
- `internal/server/api.go` — /api/schedules endpoint

## Phases

### Phase 1 — Natural Language Time Parsing (Planned)

- Parse relative times: "in 30 minutes", "in 2 hours", "in 1 day"
- Parse absolute times: "at 14:00", "at midnight", "at 3pm"
- Parse day references: "tomorrow at 9am", "next wednesday at 10:00"
- Use Go's `time` package — no external NLP dependency
- Parsing function: `func ParseScheduleTime(input string) (time.Time, error)`
- Support timezone from system locale

### Phase 2 — Deferred Session Creation (Planned)

- New schedule type: `ScheduleTypeNewSession` (vs existing `ScheduleTypeCommand`)
- `DeferredSession` struct: task, project_dir, backend, name, scheduled_at
- Stored in ScheduleStore alongside command schedules
- On timer fire: `Manager.Start()` creates the session and sends the task
- Session only starts when timer fires — no LLM tokens consumed until then

### Phase 3 — Timer Engine (Planned)

- Background goroutine in Manager: checks pending deferred sessions every 30s
- When `time.Now() >= scheduled_at`: start the session
- After start: remove from schedule store, create alert "Scheduled session started"
- On daemon restart: recalculate timers for any pending schedules
- Cancel support: remove pending schedule by ID

### Phase 4 — CLI Command (Planned)

```bash
# Schedule a new session
datawatch schedule new "in 2 hours" --task "fix auth bug" --backend claude-code --dir /path

# Schedule with natural language
datawatch schedule new "tomorrow at 9am" --task "review PR #42" --backend opencode-acp

# List pending schedules
datawatch schedule list

# Cancel a schedule
datawatch schedule cancel <id>
```

### Phase 5 — Communication Channel Support (Planned)

```
schedule new: in 2 hours: fix the auth bug in login.go
schedule new: at 14:00: /home/project: review test coverage
schedule list
schedule cancel <id>
```

### Phase 6 — Web UI (Planned)

- New session form: "Schedule" toggle below git options
- When enabled: time input field with natural language support
- "Schedule" button replaces "Start Session"
- Pending schedules visible in session list with countdown timer
- Settings > Schedules section showing all pending with cancel buttons

## Key Files

- `internal/session/schedule.go` — DeferredSession type, persistence
- `internal/session/manager.go` — timer engine, StartDeferred method
- `internal/session/timeparse.go` — NEW: natural language time parser
- `cmd/datawatch/main.go` — schedule CLI subcommand
- `internal/router/commands.go` — schedule new: command
- `internal/server/web/app.js` — schedule UI on new session form
- `internal/server/api.go` — /api/schedules CRUD

## Dependencies

- No new Go modules — use stdlib `time` package for parsing
- ScheduleStore already exists and persists to JSON

## Estimated Effort

1-2 weeks. Phase 1-3 are the core; Phase 4-6 are interfaces.
