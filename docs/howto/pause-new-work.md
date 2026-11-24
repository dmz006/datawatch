---
docs:
  index: true
  topics: [pause, operator, autonomous, capacity, federation]
exec_steps:
  - tool: pause_status
    description: Show whether new work is paused (global and per project) with who paused it, why and when
    args: {}
    read_only: true
---

# Pause new work

A **pause** lets you stop new work without stopping anything else. While
something is paused, datawatch admits no new Automaton start, no restart, no
new worker task and no lane fill for the paused scope. Tasks that are already
running finish. Schedules, the daemon, the Automata manager, tabs, history,
dashboards and reports stay up.

It is separate from `autonomous.enabled` (which turns the manager off) and
from an Automaton's **hold** (which parks one Automaton): a pause covers
everything, or one project, and is cleared in one action.

## What is paused

| Scope | Covers |
|---|---|
| Global | Every Automaton and project |
| Project | One project directory and anything inside it (give a registered project name or a directory) |

What a pause holds, for the paused scope:

- A start by `autonomous run` (or the Run button) is refused, naming the reason. The status is left alone.
- Lane fill skips the Automaton and shows the refusal `operator_pause`.
- A worker task about to start waits with the reason `paused by operator (...)`. It does not fail and does not count as a retry. When you resume, it starts.
- After a daemon restart the pause is still in force: the state is saved in `pause.json` in the data directory. An Automaton that was running is re-attached so work already in flight is adopted, but it starts nothing new until you resume.

What a pause does not hold: tasks already holding a slot, their verification
and judges, supervisor sessions, schedules and operator sessions.

## Pause and resume

A reason is required when you pause. It is shown to everyone who is refused
and written to the audit log with who and when.

| Surface | Pause | Resume | Look |
|---|---|---|---|
| CLI | `datawatch pause --reason "release freeze"` · `datawatch pause --project web --reason "reviewing"` | `datawatch resume` · `datawatch resume --project web` · `datawatch resume --all` | `datawatch pause status` |
| REST | `POST /api/pause` `{scope, project?, reason}` | `POST /api/pause/resume` `{scope, project?}` or `{all: true}` | `GET /api/pause` |
| MCP | `pause_set` | `pause_resume` | `pause_status` |
| Chat | `pause <reason> [project=<p>]` | `resume [all] [project=<p>]` | `pause status` |
| PWA | Header pause button, **Pause** | The same dialog, **Resume** or **Resume all** | The button turns amber and shows *Paused* |

**Pause all** is the global pause. **Resume all** (`--all`, `all=true`, `resume all`)
clears the global pause and every project pause in one action.

## Only the operator can resume

The daemon refuses a resume from a session-scoped credential (the kind an
LLM session holds), even if that session was granted `pause:write`, and from
any actor named like a worker, watcher, supervisor, model, agent or
session. The refusal says why (HTTP 403, `only the operator can resume
paused work`) and is audited as `pause_resume_refused`. No rule has to be
followed by a model; the daemon enforces it.

Reading the state needs `pause:read` (in a session's default grant). Setting
or clearing needs `pause:write`, which is not in a session's default grant.
A federation peer needs `pause:write` through a capability group before it
can send a pause to this daemon.

## Federation

Each daemon keeps its own state. To send an action to peers add `--peers` (all)
or `--peer <name>` (repeatable) to the CLI, `all_peers` / `peers` to REST and
MCP, or `peers=all` / `peers=a,b` in chat. A peer that is down is reported in
its own entry and never blocks the local pause. `datawatch pause status --peers`
(`GET /api/pause?peers=1`) shows every peer's own state next to yours.

## Setting

```yaml
pause:
  start_paused: false   # default off
```

With `start_paused: true`, a daemon that has no saved pause state starts
globally paused, so a fresh or rebuilt daemon admits nothing until you
resume. A saved state always wins, so a resume survives a restart. A damaged
state file starts the daemon paused and says so on start; the first resume
rewrites it.

## Audit

`pause_set`, `pause_resumed` and `pause_resume_refused` appear in the audit log
(`datawatch audit --action 'pause_*'`) with the actor, the scope or project,
the reason or note, and the time.
