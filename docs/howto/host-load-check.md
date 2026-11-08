---
docs:
  index: true
  topics: [autonomous, host-load, load-average, sustained-breach, reap, alerts]
exec_steps:
  - tool: autonomous_config_get
    description: Read the current host load check settings. The host_load_alert part reports the mode, the multiple and the minutes
    args: {}
    read_only: true
  - tool: autonomous_config_set
    description: Set the mode. off, alert (the default) or reap. One call stores only the keys you pass
    args:
      host_load_alert_mode: alert
  - tool: autonomous_config_set
    description: Worked example, a state-changing call. Set the breach multiple and the sustained minutes. A value of 0 reads back as the default
    args:
      host_load_alert_multiple: 1.5
      host_load_alert_minutes: 10
---

# Host load check — the daemon's guard for an overloaded host

A worker session that gets cancelled or crashes can leave the processes it
started running, and those keep eating CPU — which is exactly when the Automata
sharing the host slow down hardest. The host load check is the daemon's answer:
every minute it compares the host's load average with a multiple of the core
count and, once the breach has lasted the configured number of minutes, raises
**one warning alert for that episode** naming the busiest process groups and
whether each belongs to the worktree or scratch folder of a running or an
ended Automaton session.

It is independent of the burn guard and the usage calculator: those read usage
data, this one reads the running host.

## What it does every minute

1. Reads the 1-minute load average from `/proc/loadavg`.
2. Computes the limit: `multiple` × core count (default 1.5 × cores).
3. If the load is **at or under the limit**, the sustained window resets — a
   dip always clears both the timer and the "already alerted" flag.
4. Once the load has been **above the limit for `minutes` consecutive minutes**,
   it raises one alert: the load, the limit, how long the breach has lasted,
   and the mode in force.
5. The alert names up to **5 process groups** (ranked by cumulative CPU), each
   with its process count and a short command. For groups the daemon can prove
   are tied to a session, it says `worktree folder of running/ended Automaton
   session <id>` or `scratch folder of …`; a group it cannot tie to any
   session folder is listed as *not in any Automaton worktree or scratch
   folder*.
6. That's it for the episode — no second alert until the load dips under the
   limit and breaches again. The next breach alerts again (and, in `reap`
   mode, reaps again).

## The modes and defaults

`autonomous.host_load_alert.mode`:

| Value | Behaviour |
|-------|-----------|
| `off` | Does nothing — the check is disabled, whatever the two numbers say. |
| `alert` | **Default.** Reports the sustained breach with one alert per episode; touches no process. |
| `reap` | Same alert, **plus** ending the processes of **ended** sessions that the session-end reaper's evidence rule proves, by exact PID. Running sessions are never touched. |

The two numbers:

| Config key | Default | Meaning |
|------------|---------|---------|
| `autonomous.host_load_alert.multiple` | `1.5` | A load average above this many times the core count starts (or continues) a breach. |
| `autonomous.host_load_alert.minutes` | `10` | How long the breach must last in consecutive minutes before the alert (and, in `reap` mode, the reap). |

`0` (or an absent value) reads as the defaults, so `0` is a "back to default",
not an off switch — `off` only applies to the mode. A negative value or an
unknown mode is refused anywhere it is set (CLI, chat, REST, MCP, and the PWA
form). Changes are read on the next minute, **no restart needed**.

## An example alert

```
Host load high: Automata may be slowed
Load average 35.42 is above 30.00 (1.5 x cores) for 14 minute(s). Mode: reap.
- group 4120 (3 process(es), go build -o dw ./cmd/datawatch):
    worktree folder of ended Automaton session 8f3ab2
- group 4188 (1 process(es), ccsw run claude):
    scratch folder of running Automaton session 5c1e0a
- group 4307 (12 process(es), node server.js):
    not in any Automaton worktree or scratch folder
```

**Reading it.** An *ended* session row means that session is gone but the
process group it started is still running — that is the leaked CPU worth
chasing (and what `reap` handles). A *running* session row is a live worker
doing real work; do not end those by hand without checking the session is
done. The "not in any … folder" rows are processes the daemon cannot attribute
to Automaton data — they may be your own, another tool's, or a worker whose
evidence no longer matches a folder it knows.

## How to change the settings

The setting lives under `autonomous.host_load_alert` with the three keys
`mode`, `multiple`, `minutes`. Every surface works:

| Surface | Command / action |
|---|---|
| **YAML** | `autonomous.host_load_alert.mode: alert` in `config.yaml`, with `multiple` and `minutes` beside it; see the [configuration reference](../config-reference.yaml) |
| **Web/PWA** | PWA **Settings → General → Autonomous**: the *Host load check* select (off / alert / reap), plus the *multiple* and *minutes* fields. |
| **CLI** | `datawatch autonomous config-set --host-load-alert-mode reap --host-load-alert-multiple 2 --host-load-alert-minutes 15` (one flag alone sets just that key) |
| **MCP** | `autonomous_config_set` with `host_load_alert_mode`, `host_load_alert_multiple`, `host_load_alert_minutes`; read back with `autonomous_config_get` |
| **Chat/comm** | set: `configure autonomous.host_load_alert.mode=reap`; read: `autonomous host-load` (plain-text mode, limit, duration; changes nothing) |
| **REST** | `PUT /api/config` — `{"config":{"autonomous":{"host_load_alert":{"mode":"alert","multiple":1.5,"minutes":10}}}}` (read with `GET /api/config`) |

To read the effective settings without curl: the chat command `autonomous host-load` reports the mode, the limit and the duration in plain text and changes nothing.

## Choosing `reap`

`reap` reuses the session-end reaper's evidence rule: a process is ended only
when its file evidence (the handles it holds) point inside a **worktree or
scratch folder of a session that has already ended**, matched by exact PID —
never by process name or command line. Consequences to plan for:

- It reaps **only at the moment the alert fires**, i.e. once per sustained
  breach, after the breach has already lasted `minutes`. Switching to `reap`
  does not sweep what has already leaked; a leak only gets reaped by this
  path when a sustained breach happens while the setting is `reap`.
- It never touches a running session's processes, no matter how long the host
  stays overloaded.
- A process that ignores the end signal survives — and this reap keeps
  **no record** of it: there is no `session_end_reap` audit entry and no
  *survivors* count for the load check's reap (that audit trail belongs
  to the session-end pass only). To check that a reap actually cleaned
  the room, re-check the ended session's worktree and scratch folder
  yourself (for example `ps -eo pid,ppid,cmd` and `ls -l /proc/<pid>/cwd`).
- The usual session-end reap still runs independently when a session ends
  (see [Automaton worktree cleanup](automaton-worktree-cleanup.md)); this
  check is the backstop for the sessions that ended **without** their leak
  being caught in time.

## What it never does

- Ends or slows a running session — a busy worker is a live worker.
- Alert more than once per breach episode, or alert on a one-minute spike.
- Change admission (that is the [burn guard](burn-guard.md)).
- Guess ownership from a command line — attribution comes from folder
  evidence only, so a worker started outside any Automaton folder is listed
  but never acted on.

## See also

- [Processes a session leaves behind](../operations.md#processes-a-session-leaves-behind) and the [host-load section](../operations.md#host-load-alert-autonomoushost_load_alert) in the operations manual
- [Automaton worktree cleanup](automaton-worktree-cleanup.md) — the session-end reap and the branch/worktree cleanup it is paired with
- [Burn guard](burn-guard.md) — the usage-based admission ladder, the other "slowing down" control
- [Settings reference](../config-reference.yaml) — `autonomous.host_load_alert.*`
