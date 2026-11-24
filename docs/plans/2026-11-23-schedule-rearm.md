# Scheduled spawns that keep running (re-arm policy, restart check, run history)

Date: 2026-11-23. Version: v9.183.0. Plan item 6 and 7 of the queue-safety plan.

## Scope

`internal/session/schedule.go`, `schedule_rearm.go`, `manager.go`; `cmd/datawatch/main.go`,
`schedule_rearm_wire.go`; `internal/config/config.go`; `internal/server/api.go`;
`internal/router`; `internal/mcp/server.go`; `internal/server/web/app.js` and locales;
docs.

## Phases

1. Failing tests: a full session cap marks a recurring schedule failed (manager loop). Status: Done.
2. One path fires due spawns, with a refused start recorded under a re-arm policy. Status: Done.
3. Settings, alert hook, list surfaces (REST fields, MCP, CLI, comm, web). Status: Done.
4. Docs, tracker, parity draft. Status: Done.
5. Live check on a throwaway daemon, then ship with the other queue-safety builds. Status: Planned (the supervisor does the release).

## What the trace found

- Two goroutines fired due spawns: the daemon scheduler (10 s) and `Manager.StartScheduleTimer` (30 s).
  The manager one marked any refused start failed through `MarkDone(id, true)`, which ends a recurring
  schedule. That is how the detector schedules stopped at the session cap of 20.
- The two loops differed: only the scheduler had the overlap guard, the fire counters, `Git` and the
  alert; only the manager loop applied the launch-role task hook. A due entry could also start twice.
- `RecordSpawnFailure` re-armed a recurring schedule to its next time only, with no backoff, no retry
  limit and no distinction between a wait and a failure; a one-time schedule failed on the first refusal.
- The comm verb `schedule list` did not parse.

## Design

- `IsStartWait` classifies a refusal as a wait: `ErrMaxSessions`, `ErrGlobalCooldown`, `ErrDaemonDraining`,
  `capacity.ErrWaitTimeout`, or the same text from a peer. A wait never fails a schedule, never counts
  toward the retry limit, and retries after a doubling pause capped by `schedule.rearm_backoff_max_seconds`
  and never later than the next scheduled time.
- Other failures follow `schedule.rearm_mode`: `next_fire` (default, old behaviour), `backoff` (retry
  count), `stay_failed`. A give-up raises an error alert; waits raise one warn alert after 5 in a row.
- `Manager.FireDueSessions` is the one path, behind a lock; both loops call it.

## Run history (item 7), checked

- Supervisor loop runs (`supervisorloop.RunRecord`): job, style, backend, model, outcome, attempts, start,
  finish, duration, kept in a bounded file and read by REST, MCP and CLI. **Missing: cost.** The model
  client does not return usage, so nothing to record without a larger change. No dashboard card reads it.
- Daemon-run mechanical jobs (cleanup passes, dependency update, finaliser, housekeeping repairs): each
  keeps a last-run snapshot only (time, counts, last error). **Missing: history and cost.**
- Schedule fires: only the last fire was kept. Added: a bounded `fire_history` (time, result, session id,
  reason) on each schedule. Cost is reachable through the session id; not joined here.

## Parity surface

| Surface | Status |
|---|---|
| REST | Touched: `GET /api/schedules` carries the new fields; `PUT /api/config` takes `schedule.*`; openapi updated |
| MCP | Touched: `schedule_list` shows the state line; `config_set` takes `schedule.*`. No new tool: the list and settings tools already exist |
| CLI | Touched: `datawatch schedule list` has a REARM column; `datawatch config set schedule.*` |
| comm channel | Touched: `schedule list` (new parse), `configure schedule.*` |
| YAML/config | Touched: `schedule:` block in `datawatch.yaml` and `docs/config-reference.yaml` |
| PWA | Touched: Settings card and a state line per schedule row; locale keys in en, de, es, fr, ja |
| Android / iPhone | Not changed here; draft in `docs/plans/parity-issues/2026-11-23-schedule-rearm.md` |
| Federation | Excluded: schedules are per host; a peer's list is read through the existing schedule endpoints |
