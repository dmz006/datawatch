---
docs:
  index: true
  topics: [session-end reaping, orphan processes, host load, host_load_alert, bounded load step, session_end_reap, audit, process cleanup]
exec_steps:
  - tool: autonomous_config_get
    description: Read the host load check settings under autonomous.host_load_alert (mode, multiple, minutes)
    args: {}
    read_only: true
  - tool: autonomous_config_set
    description: Set the host load check mode to off, alert or reap
    args:
      host_load_alert_mode: "{{params.mode}}"
    read_only: false
  - tool: autonomous_config_set
    description: Set the load-to-cores multiple (0 keeps the 1.5 default)
    args:
      host_load_alert_multiple: "{{params.multiple}}"
    read_only: false
  - tool: autonomous_config_set
    description: Set how many minutes a breach must last before the alert (and reap) fires (0 keeps the 10 default)
    args:
      host_load_alert_minutes: "{{params.minutes}}"
    read_only: false
  - tool: audit_query
    description: Read the session_end_reap audit entries and see what each session-end reap ended and what survived
    args:
      action: session_end_reap
      limit: 20
    read_only: true
---

# Session-end orphan reaping and the host load check

Sessions — especially Automaton worker sessions — leave work running longer
than they do: a build watcher, a dev server, a stress loop, a test harness
that forked children. Three mechanisms keep those from piling up on your
host, and this howto covers all three:

1. **Session-end reaping** — automatic and always on. When a session ends for
   any reason, the daemon ends the processes that session left behind,
   matched only by evidence from `/proc`, never by name.
2. **The host load check** — the `host_load_alert` setting. A once-per-minute
   watch on the load average whose `reap` mode ends, as part of a sustained
   breach, the leftover processes of *ended* sessions — a backstop for the
   session-end step when it could not catch them.
3. **The bounded load step rule** — the planning rule that a task which asks
   for CPU stress must bound it: niced, capped to a quarter of the cores,
   hard-timed, self-cleaning, and dead when its task is dead.

They act at three different moments: the rule *prevents* leftover load
during the task, session-end reaping *ends it* when the session ends, and
the host load check *contains it* while the host is under sustained
overload. A process that a *running* session is still driving is never
touched by any of them — you always pull that one yourself.

The detailed walkthrough of the load check's alert text and episode
behaviour lives in [host-load-check.md](host-load-check.md); this howto
focuses on the reaping and on what `host_load_alert` protects.

## How it works

```
task pins the CPU (bounded: niced, ≤ nproc/4, timed, self-cleaning)
        │  task ends, session still running → nothing acts
        ▼
session ends (finished, cancelled, failed, daemon stops)
        │  session-end reap: end evidence-proven leftovers of THIS session
        ▼
host still overloaded for `minutes` in a row?
        │  host load check: one alert per episode
        └── (mode=reap) also end leftovers of ENDED sessions,
            by the same evidence rule
```

Every step is visible: a `session_end_reap` audit entry per reaping session,
a `session_end_reap` decision on the bound task, one "Host load high" alert
per breach episode, and the `autonomous_lint` finding `unbounded_load_step`
when a task spec asks for unbounded CPU stress.

## Reaping at session end

**When it runs.** Every session end: finished, cancelled,
failed, or the daemon itself stopping. It runs after the session is
recorded ended, and only against that one session's worktree and scratch
folders.

**How a process is matched.** A process is attributed to a session only by
evidence read from `/proc` — never by command name. A process of another
user is never a candidate, and the daemon never signals itself. The kinds
of evidence are:

| Evidence | What proves it | Strong or weak |
|----------|---------------|----------------|
| `exe` | The executable lives inside the session's worktree or scratch folder | strong — counts whatever the parent is |
| `script` | A script run directly, or through a known interpreter (`sh`, `bash`, `python3`, `node`, …), lives inside the session folders | strong |
| `cwd` | The working directory is inside the session folders **and** the process was orphaned (its parent is `init` or a service that adopts orphans) | weak — an operator's live shell or editor sitting in the folder is never touched |
| `ancestry` | The process's parent chain reaches one of the session's known root pids | direct |
| `env` | One of its environment variable *values* equals the session id (the environment is read in memory and never logged) | direct |

**How it ends them.** One pid at a time, in order: a polite `SIGTERM`, a
short grace (2 seconds), and a forced `SIGKILL` for whatever did not exit.
Before *each* signal the evidence and the process's start time are
re-verified, so a pid that got reused for an unrelated process in the
meantime is re-checked against the same-folder/same-start-time proof
first — a reused pid is left alone and reported as a survivor. A process
that ignores even the forced signal is not re-signalled; it is counted as
a `survivor` in the audit entry, and its pid and command are *not*
recorded — you check for survivors by hand (they are visible in `ps` as
children of `init` with cwd in the session folder).

**What gets written.** Per reaping session:

- a `session_end_reap` audit entry, actor `autonomous`, subject
  `<n> ended, <m> survived`, with details: the `ended` count, the
  `survivors` count and the `commands` of the ones it ended (program name
  plus at most two arguments, secret-looking values redacted), or
- nothing at all, when the daemon found nothing matching;
- when the session is bound to a task of an Automaton, one `session_end_reap`
  decision on that task, e.g.
  `session end reap: ended 2 process(es) left behind by session pp01: watch.sh; python3 server.py`
  (plus the same line in the task's history). There is no decision and no
  alert when nothing was found.

### Reading the reap result

| Surface | Call |
|---------|------|
| MCP | `audit_query(action="session_end_reap", session_id="pp01")` |
| REST | `GET /api/audit?action=session_end_reap` (add `&session_id=pp01`, `&since=…`, `&limit=…` as you like) |
| CLI | `datawatch audit --action session_end_reap --session-id pp01 --limit 20` |
| PWA | the audit log view, filtered on `session_end_reap` (see [audit-logging.md](audit-logging.md)) |
| Chat / comm | ask the channel for the audit entries for action `session_end_reap` |

## The `host_load_alert` settings

The host load check watches the 1-minute load average once per minute.
Once the load stays above `multiple` × (core count) for `minutes`
consecutive minutes, it is a **sustained-breach episode**: one warning
alert names the busiest process groups (5 by default) and whether each
sits in the worktree or scratch folder of a running or an ended Automaton
session — proven by the same folder evidence rule as the reaper, never by
name. The episode ends when the load returns under the limit; the next
sustained breach alerts again.

The three settings, under `autonomous.host_load_alert`:

| Key | Type | Default | Meaning |
|-----|------|---------|---------|
| `autonomous.host_load_alert.mode` | string | `alert` | `off`: check off. `alert`: report only. `reap`: report, and at the alert moment end the leftover processes of **ended** sessions that the evidence rule proves. |
| `autonomous.host_load_alert.multiple` | number | `1.5` | Load limit = `multiple` × core count. `0` reads as the default 1.5; a negative value is rejected on write. |
| `autonomous.host_load_alert.minutes` | integer | `10` | Minutes the breach must last before the alert (and reap) fires. `0` reads as the default 10; a negative value is rejected on write. |

Example alert for `multiple` 1.5 on a 4-core host:

```
Host load high: Automata may be slowed
Load average 7.42 is above 6.00 (1.5 x cores) for 10 minute(s). Mode: reap.
- group 4123 (6 process(es), python3 stress.py): scratch folder of ended Automaton session pp01
- group 5510 (1 process(es), cargo): not in any Automaton worktree or scratch folder
```

An example with `reap` switched on, in which a 4-core host held at 7.5 load
for ten minutes ends two processes of an already-ended session:

```
# the episode alert arrives:
Host load high: Automata may be slowed
Load average 7.50 is above 6.00 (1.5 x cores) for 10 minute(s). Mode: reap.
# two processes of the ended session are ended by exact pid — but unlike
# the session-end path, this reap writes no record: no session_end_reap
# audit entry, no task decision, no survivors count
```

What `reap` may and may not end — the load check never touches a process of
a **running** session, and it never signals by name: it applies the
reaper's evidence rule to each ended session's folders, exactly like the
session-end step. A process that ignores its end signal survives: this path
does not re-signal it and keeps no record of it either — it is *not* counted
as a survivor in the `session_end_reap` audit (that audit trail belongs to
the session-end pass only), so after a load-check reap you confirm the room
is clean by hand. `autonomous.host_load_alert` is the only switch for the
check; there is no separate "enable reaping" toggle.

### Setting and reading it — every surface

Set `autonomous.host_load_alert.*` from any surface (it takes effect on
the next minute tick):

| Surface | Call |
|---------|------|
| PWA | the **Host load check** card in Settings: the mode choice plus `multiple` and `minutes` |
| Web UI | same card in the desktop settings view |
| REST | `PUT /api/config` with `{"config":{"autonomous":{"host_load_alert":{"mode":"reap"}}}}` |
| MCP | `autonomous_config_set(host_load_alert_multiple=2)` (also `host_load_alert_mode`, `host_load_alert_minutes`) |
| CLI | `datawatch config set autonomous.host_load_alert.mode reap` |
| Chat / comm | `set: configure autonomous.host_load_alert.minutes=15` |

Read the current values:

| Surface | Call |
|---------|------|
| PWA / Web UI | the same card shows the stored values |
| REST | `GET /api/config` (the body carries `autonomous.host_load_alert`) |
| MCP | `autonomous_config_get()` (it returns the `host_load_alert` block) or `get_config` under the `autonomous.host_load_alert` prefix |
| CLI | `datawatch config get autonomous.host_load_alert` |

### Working example: switch a host to `reap`

1. Read the current state: `autonomous_config_get`, and confirm the default
   trio `mode=alert`, `multiple=1.5`, `minutes=10`.
2. Widen the net before widening the trigger:
   `autonomous_config_set(host_load_alert_mode="reap")`.
3. Leave `multiple` and `minutes` where they are for a while; the alert text
   already tells you *which* groups are driving the load, so you only raise
   `multiple` (e.g. to 2) when your own baseline legitimately runs hot.
4. After the first sustained breach, read the alert's group list — the
   reap itself writes no `session_end_reap` record — and confirm the
   ended-session processes it named are actually gone (for example
   `ps -eo pid,ppid,cmd` plus `ls -l /proc/<pid>/cwd` in the session's
   worktree or scratch folder).
5. If the host is a shared box and you would rather know than act, set
   `mode` back to `alert` — the alerts keep coming, nothing is ended.

## The bounded load step rule

Automaton planning carries a hard rule for any task that pins the CPU —
a busy loop, a stress test, a load or soak test, any other command that
grabs the cores. Such a step must be born bounded:

- **Niced.** Run it with `nice -n 19` so it yields to the system's other
  work.
- **Capped.** Use at most a quarter of the cores — cap the worker count at
  `nproc/4`, minimum one.
- **Timed.** Give it its own hard timeout, a bounded `timeout <duration> …`
  wrapper, so it always dies on its own.
- **Self-cleaning.** Trap `EXIT INT TERM` and clean up its children, so no
  orphaned load survives the step.
- **Task-bound.** It must never outlive its owning task: when the task ends,
  the load ends with it.

An Automaton that plans a task leaves the host as it found it, so an
unbounded load step in a plan is a defect in the plan — that is why the
rule is part of the planning conversation, not just of the docs. The rule
lives in one source and is carried verbatim into three surfaces: the
built-in `bounded_load_step` standard task a fresh story starts with (run
daemon-mode by default, so the task completes as a note with no model session — the rule
still appears in the story record and is visible in the plan), the
rules block appended to the decompose prompt, and a warn-severity
`unbounded_load_step` finding in the decompose lint
([automaton-decompose-lint.md](automaton-decompose-lint.md)) when a task
spec asks for a load step the decomposer should have bounded.

A bounded stress step, end to end:

```bash
timeout 300 nice -n 19 bash -c '
  trap 'kill 0' EXIT INT TERM
  workers=$(( $(nproc) / 4 )); [ "$workers" -lt 1 ] && workers=1
  pids=()
  for _ in $(seq 1 "$workers"); do
    while :; do :; done &
    pids+=($!)
  done
  wait "${pids[@]}"
'
```

Every bound is visible in that block: `timeout 300` kills it at five
minutes no matter what; `nice -n 19` yields to real work; the worker count
is `min(nproc/4, …)`; the `trap` ends the busy loops on any exit signal;
and the whole thing lives inside the task's own shell, so when the session
ends, the step is either already dead or ended by the session-end reap.

## How the three layers protect a host

1. **Prevent.** The decompose prompt and the `bounded_load_step` standard
   task tell the planner to bound any CPU pinning, and the `unbounded_load_step`
   lint finding warns it back when it doesn't. Most sessions never leave
   load behind.
2. **Reap.** A session that does leave a process behind — a watcher, a dev
   server, a loop that forked — is reaped when the session ends for any
   reason. `ended` pids and short commands, and a `survivors` count, land
   in the `session_end_reap` audit; a bound task also gets the
   `session_end_reap` decision.
3. **Contain.** Whatever the per-session steps missed — a process reparented
   to `init`, a survivor that ignored its signals, leftovers from a
   process the evidence rule could not prove — the host load check sees it
   at the host level and, in `reap` mode, ends the ended-session
   leftovers it *can* prove while the sustained breach is being alerted.
   Running-session processes are never ended by the host-level step.

## Verification after configuring

1. Read the config back from at least two surfaces (e.g.
   `datawatch config get autonomous.host_load_alert` and the
   `autonomous.host_load_alert` block of `GET /api/config`)
   and confirm all three values.
2. The next minute tick uses the new values — no restart needed.
3. When the host first breaches under the new values, you should get the
   "Host load high" alert within the episode; under `reap`, that alert's
   group list is your only record — the load check's reap writes no
   `session_end_reap` entry — so confirm the named ended-session folders
   no longer hold their processes (`ps` and `ls -l /proc/<pid>/cwd`).
4. If you raised `multiple` or `minutes` and see nothing for a known
   overload, check the alert list — a lower breach earlier in the episode
   may have already alerted (one alert per episode; the episode resets
   only when load drops under the limit).

## Troubleshooting

**A process the session left is still running after end.** It is a
survivor: it ignored both signals (or verification refused it — e.g. its
pid was reused). The audit entry counts it under `survivors` without the
pid; find it by cwd (`ps -eo pid,ppid,cmd` plus `ls -l /proc/<pid>/cwd`
inside the session folder) and end it yourself.

**`reap` mode is set but nothing was ended.** The reap fires at the
*episode alert moment*, not continuously: the breach must be sustained for
`minutes` on a tick after the check was switched on, and `reap` only ends
processes of *ended* sessions that the evidence rule proves — a running
session's processes are visible in the alert but never signalled.

**An operator process got "reaped".** It should not have been: the
evidence rule requires same-user ownership plus a strong tie (exe/script
in the session folder, or ancestry/env to the session; a bare cwd in the
folder only counts once the process was orphaned from the session). If
you saw one ended by a *session-end* reap, read the audit entry for
action `session_end_reap` and the session it names, and the
`session_end_reap` decision line on the task, which carries the session
id and the ended commands. If the process was ended by the *host load
check* in `reap` mode, there is no such entry: that reap keeps no record
(the audit and task decision above exist only for the session-end path),
so reconstruct what happened from the "Host load high" alert's group
list at the moment of the breach.

**A task spec asks for "pin all cores".** That is the
`unbounded_load_step` warning: the spec should name the bounds
(`nice -n 19`, `nproc/4` workers, `timeout`, `trap`). The decompose-lint
finding is a warning, not a block — answer it in the spec or in the
review, not by overriding the rule.
