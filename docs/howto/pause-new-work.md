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

A **pause** lets you stop new automation work without stopping anything else.
It is for the work datawatch runs itself: while something is paused, datawatch
admits no new Automaton start, no restart, no new worker task and no lane fill
for the paused scope. Tasks that are already
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
| PWA | Header pause button (**Pause automation work**), **Pause** | The same dialog, **Resume** or **Resume all** | The button turns amber and shows *Paused* |

**Pause all** is the global pause. **Resume all** (`--all`, `all=true`, `resume all`)
clears the global pause and every project pause in one action.

## Which server a pause goes to

A pause belongs to the daemon it is sent to. It stops that daemon from placing new
automation work wherever the work would run: on its own host, in a local or cloud
queue, or on a federated host it would send work to. It does **not** pause the
federated host itself. That host may be doing its own work, and its own pause is set
by connecting to it.

By default a pause is for this daemon only, and there is no setting that spreads a
pause by default. In the web app, when the header server selector is on **All**, the
pause window shows a **Server** picker with *This server only* (the default), each
server (the same enabled list the selector shows) and *All servers*:

- *This server only* sends the request here, as before.
- A single server is reached through the proxy, as if you had connected to it, so
  only that server is paused or resumed.
- *All servers* pauses or resumes here and sends the same request to every federation
  peer (`all_peers`); the window then lists what each server answered, with an error
  for one that could not be reached.

With the selector on this server or on one remote server, the pause goes to the
server you are looking at, as before, and the picker is not shown. The other surfaces
choose peers with `--peers`, `all_peers` and `peers`, see Federation below.

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

## An alert when a script resumes

Until real identities exist, the local API needs no token, so a script, a CLI
call or a model that reaches the API looks the same as you. To make that
visible, every resume that takes effect and does not come from the web app or
from a session credential raises one alert (level warn) and writes a
`safeguard_action` audit entry. The alert names:

- the action and what it cleared (`global`, a project, or all pauses),
- the reason you gave (`--note`; the alert says "no reason given" when there is none),
- the caller: the actor label it claimed, its remote address, its user agent
  and what kind of credential it used (never the credential itself).

The web app sends a plain `X-Datawatch-Client: pwa` header on every call, so
resuming from the header button raises no alert; it is still audited, with the
marker, the address and the user agent. The header is not a secret and anyone
can send it, so it can only hide the alert, never allow anything, and a call
that sends it leaves its own audit record. A resume from a session credential
is refused as before and needs no alert. A resume that clears nothing raises
nothing. The CLI (`datawatch resume`) and chat (`resume`) are not the web app, so
they raise the alert too; so does a peer's resume, on the receiving daemon, as
`peer:<name>`.

Turn it off with `audit.safeguard_alert: false` (Settings -> General -> Audit &
Access Log). The audit entry is written either way. The same alert covers the
attempt-count force reset, see [attempt-cap.md](attempt-cap.md).

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
the reason or note, and the time. A resume from a caller that is not a session
also writes `safeguard_action` (see above), whether or not an alert was raised.
