---
docs:
  index: true
  topics: [usage, backoff, back-off, rate-limit, capacity, restart, burn-guard, alerts]
exec_steps:
  - tool: usage_backoff_status
    description: Read every active back-off flag first (backend, until, reason, source), plus the effective mode and maximum wait
    args: {}
    read_only: true
  - tool: usage_backoff_set
    description: Worked example, a state-changing call. Hold new Automaton dispatch to claude-code for 45 minutes. Give minutes or until, never both
    args: {backend: "claude-code", minutes: 45, reason: "weekly cap"}
  - tool: usage_backoff_clear
    description: Worked example, a state-changing call. Clear the flag for one backend (omit backend, or pass all, to clear every flag)
    args: {backend: "claude-code"}
  - tool: capacity_admission
    description: Ask whether a start on the backend would be admitted now or wait (changes nothing)
    args: {backend: "claude-code"}
    read_only: true
  - tool: audit_query
    description: List the restore events written at daemon start (the action is exact, no prefix needed)
    args: {action: "autonomous_usage_backoff_restored", limit: 20}
    read_only: true
---

# Usage back-off

A **usage back-off flag** tells the daemon that one backend has run out of
usage, so it should stop starting new Automaton work on that backend until a
deadline. The flag is per backend: backing off `claude-code` never affects `pi`
or `ollama`.

**A flag only holds new work. It never ends, kills or signals a session that is
already running.** Interactive session starts you make by hand are never blocked
either.

## What a flag holds

| Field | Meaning |
|---|---|
| `backend` | The backend the flag holds, for example `claude-code` |
| `until` | When the flag lifts, as a time. After it passes the flag is gone |
| `reason` | A short text shown in the wait reason and in capacity output |
| `source` | Who set it: `rate_limit` or `operator` |

There are three kinds of flag, told apart by `source` and the reason:

- **Rate-limit flag** (`source: rate_limit`). Set by the daemon when a session on
  the backend reaches the rate-limited state. It uses the reset time the backend
  printed, or, when none can be parsed, the maximum back-off
  (`autonomous.backoff_max_wait_minutes`, default 60).
- **Operator flag** (`source: operator`). Set by you, on any surface below, when
  you know the limit or want a backend paused.
- **Burn-guard flag.** Set by a rung of the [burn guard](burn-guard.md) (for
  example `hold_claude`). It carries the rung name, shown as
  `burn guard rung <rung>: <reason>` in capacity output, and the guard clears it
  itself when it steps back.

The automatic path is switched by `autonomous.usage_backoff` (`pause_backend`,
the default, or `off`). A manual set always works, even with the mode `off`.

## Set, check and clear a flag

| Surface | Status | Set | Clear |
|---|---|---|---|
| MCP | `usage_backoff_status` | `usage_backoff_set` with `backend`, and `minutes` or `until`, and optional `reason` | `usage_backoff_clear` with `backend` (omit or `all` for every flag) |
| REST | `GET /api/usage-backoff` | `POST /api/usage-backoff` with `{"backend": "claude-code", "minutes": 45, "reason": "weekly cap"}` (`until`, an RFC3339 time in the future, replaces `minutes`) | `DELETE /api/usage-backoff` with `{"backend": "claude-code"}` |
| CLI | `datawatch usage-backoff status` | `datawatch usage-backoff set <backend> <minutes\|until> [--reason text]` | `datawatch usage-backoff clear [backend\|all]` |
| Comm channel | `usage_backoff status` | `usage_backoff set <backend> <minutes> [reason]` | `usage_backoff clear [backend\|all]` |

With neither `minutes` nor `until`, a set holds the backend for 30 minutes.
`minutes` must be 1 or more and `until` must be in the future.

To confirm the effect, run `datawatch capacity` (or the comm verb `capacity`, or
the capacity card in the web app). Each active flag prints as
`claude-code: resets <time> (<reason>)`. An Automaton task held by the flag shows
a wait reason starting `backoff:` and starts by itself when the flag clears.

## Flags survive a daemon restart

Every set, clear and expiry writes the flags to `backoff.json` under the
`autonomous` folder of the daemon data folder (mode 0600). The daemon writes
once more when it shuts down or restarts.

At start the daemon reads the file back and reinstalls every flag whose `until`
has not passed, before the first Automaton dispatch. **Flags that expired while
the daemon was down are dropped**, and the file is rewritten to match. A missing
file is normal. An unreadable one is logged and the registry starts empty.

Clearing a flag by hand removes it from the file, so a cleared flag does not come
back after the next restart.

## What boot-time resume does with a backed-off backend

When the daemon resumes work at start, it does not start new sessions for a
backed-off backend:

- An Automaton task for that backend waits in the capacity queue with the same
  `backoff: ...` wait reason as for a flag set during this run, and starts when
  the flag clears.
- The boot-resume decision for that Automaton carries a sentence naming the
  backend and saying new session starts wait and live sessions are left alone.
- **Sessions already alive are never killed, restarted or signalled.** They are
  adopted as always, whatever flags are restored.

## Where the restore shows up

Each restored flag is recorded three ways:

1. **Decision log.** A boot-resume entry with outcome `backoff_restored`, read as
   described in [Reading the recorded decision](restart-with-work-running.md#reading-the-recorded-decision).
2. **Alert.** One titled **Back-off restored after restart**, naming the backend
   and the time it lifts.
3. **Audit.** An `autonomous_usage_backoff_restored` event carrying the backend,
   `until` and `source`. Read it with `audit_query` (action
   `autonomous_usage_backoff_restored`).

## Troubleshooting

- **Work is waiting after a restart.** Run `usage_backoff_status`. A restored flag
  is the reason; the wait reason on the task starts `backoff:`.
- **A flag you cleared came back.** It should not. Check the clear reached the
  daemon (`usage_backoff_status` shows no entry) and look in the audit trail for
  `autonomous_usage_backoff_cleared`.
- **A burn-guard flag you want gone.** Clearing it works, but the guard may set it
  again next interval. Change the guard instead (see [Burn guard](burn-guard.md)).

## See also

- [Automata capacity](automata-capacity.md#reactive-usage-backoff)
- [Burn guard](burn-guard.md)
- [Restart with work running](restart-with-work-running.md)
