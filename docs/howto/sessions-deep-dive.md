# How-to: Sessions — deep dive

Long-lived AI work units. Backed by tmux on disk; xterm.js streamed in
the PWA; rich event channel alongside the raw terminal; remembers
itself across daemon restarts. This walkthrough covers what a session
is made of, how it survives a restart, and where to look when it
misbehaves.

## Anatomy

Each session lives in `~/.datawatch/sessions/<full_id>/`:

```
output.log         ← full append-only tmux output (ANSI preserved)
state.json         ← session metadata (state, last-channel-event, ...)
tracking/          ← per-input record kept by the Tracker
wakeup.log         ← what was injected into L0/L1 layers at start
response.md        ← (optional) last LLM response captured for /copy + alerts
```

A `cs-<full_id>` tmux session attaches to `output.log` via `tmux
pipe-pane`. The LLM (claude-code, opencode-acp, ollama, etc.) runs
inside that tmux session.

## State machine

| State | Meaning | Reachable from |
|---|---|---|
| `Running` | LLM actively processing | started, output arrives, structural busy event |
| `WaitingInput` | Paused, awaiting your reply | gap watcher, structural idle event, prompt-detect |
| `RateLimited` | Backend hit limit; will auto-resume | rate-limit pattern matched in output |
| `Complete` | LLM finished its task. Terminal. | DATAWATCH_COMPLETE marker, ACP completed event, kill |
| `Failed` | tmux died unexpectedly. Terminal. | tmux session disappears; reconcile catches it |
| `Killed` | Operator explicitly killed. Terminal. | `/api/sessions/kill` |

Terminal states are sticky; nothing resurrects a Complete / Failed /
Killed session. If you want to continue work, start a new session
seeded from the previous one.

## Lifecycle

### 1. Spawn

`POST /api/sessions/start` (or PWA "+ Session"). The daemon:

1. Allocates a `full_id` (`hostname-<4hex>`) + creates the dir.
2. Writes initial `state.json`.
3. Spawns `cs-<id>` tmux session.
4. Pipes the tmux pane to `output.log` (`tmux pipe-pane`).
5. Resolves any `${secret:...}` references in the env.
6. Renders the wake-up stack (L0 identity → L1 critical facts → L2
   room recall → L3 deep search) and writes `wakeup.log`.
7. Sends the rendered prompt as the first input via `tmux send-keys`.

Within ~1 s the LLM is running, output is streaming, the PWA shows
the session as Running.

### 2. Run

- LLM produces output → tmux pane → pipe → `output.log`.
- Daemon's `monitorOutput` goroutine `tail -f`'s the log → emits the
  `output` WS event + bumps `LastChannelEventAt` via the channel
  state engine (see [`channel-state-engine.md`](channel-state-engine.md)).
- Operator types in the input bar → `tmux send-keys` to the session
  → state goes Running.

### 3. State transitions

Driven by the priority chain documented in
[`channel-state-engine.md`](channel-state-engine.md):

1. Structural events (ACP `session.status`, `session.idle`,
   `session.completed`).
2. Channel-text NLP advisory (input-needed phrases promote;
   completion phrases do NOT).
3. Universal 15 s gap watcher (LCE > 15 s old AND state == Running →
   WaitingInput).

### 4. Daemon restart

On daemon start, `ResumeMonitors` walks every session in the store:

- If `state ∈ {Running, WaitingInput, RateLimited}`:
  - Check tmux session exists (with retry).
  - If gone → mark Failed.
  - If alive → re-establish the pipe (`tmux pipe-pane` again — the
    old child process may have died with the previous daemon),
    reset `LastChannelEventAt = now` (positive evidence the pane is
    alive), spawn a fresh `monitorOutput` goroutine.
- Terminal states (Complete / Failed / Killed) are left alone.

The 30 s warm-up grace on the gap watcher ensures the watcher won't
flip resumed sessions to WaitingInput before the operator has a chance
to interact.

### 5. End

- **Complete**: `DATAWATCH_COMPLETE:` marker arrives in output, OR
  ACP `session.completed`/`message.completed` event arrives, OR
  `monitorOutput` notices the tmux session has cleanly exited.
- **Failed**: tmux session disappears unexpectedly (process crashed,
  OOM, kernel killed it).
- **Killed**: operator hit Stop in the PWA (`POST /api/sessions/kill`).

On end: state set, `output.log` closed, `tracking/` finalized, the
`onSessionEnd` callback fires (which can write to memory subsystem,
fire alerts, etc.).

## Operator entry points

| Surface | Spawn | Read | Write | Kill |
|---|---|---|---|---|
| REST | `/api/sessions/start` | `/api/sessions/{id}` | `/api/sessions/{id}/input` | `/api/sessions/kill` |
| MCP | `session_start` | `session_get` | `session_input` | `session_kill` |
| CLI | `datawatch sessions start` | `... get` | `... input` | `... kill` |
| Comm | prefix message with `start:` | reply text | reply | `stop:<id>` |
| PWA | `+` FAB | session card | input bar | Stop button |
| Mobile | same as PWA | same | same | same |

All audit-logged.

## Where to look when something is wrong

### "State stuck on `waiting_input` but the session is producing output"

Most likely cause: `LastChannelEventAt` not bumping on output arrival.

```sh
# Confirm output IS arriving:
tail -f ~/.datawatch/sessions/<id>/output.log

# Confirm the daemon's monitorOutput is processing it (line counts):
wc -l ~/.datawatch/sessions/<id>/output.log
sleep 5
wc -l ~/.datawatch/sessions/<id>/output.log    # should be larger

# Check LCE in the API:
curl -sk https://localhost:8443/api/sessions \
  | jq '.sessions[]|select(.full_id=="<id>")|{state,last_channel_event_at,updated_at}'

# Check the daemon log for MarkChannelEvent activity:
tail -f ~/.datawatch/daemon.log | grep MarkChannel
```

If output is arriving but LCE isn't bumping → `processOutputLine`
isn't reaching the `MarkChannelEvent` call. For structured-channel
backends (claude-code MCP / opencode-acp) the bump must happen at the
top of the structured-channel branch (see manager.go around the
`hasStructuredChannel(sess)` check).

### "Splash never dismisses on session entry"

The synthesized fallback frame should always fire. If it doesn't:

```sh
# Browser dev console: confirm pane_capture WS messages are arriving.
# WS messages of type "pane_capture" with non-empty `lines` should
# come within ~200ms of subscribe.
```

Daemon side: check `subscribe` handler in `internal/server/api.go`
sends a synthesized frame even when both `CapturePaneANSI` AND
`TailOutput` are empty.

### "No new lines arriving in `output.log`"

Pipe is broken:

```sh
tmux ls | grep cs-<id>             # confirm tmux session exists
tmux list-panes -t cs-<id>          # confirm a pane exists
```

If alive but pipe is dead:

```sh
datawatch sessions repipe <id>      # manually re-establish the pipe
```

This is what `ResumeMonitors` does automatically on daemon restart.

### "Session shows Complete but I'm still working in it"

NLP false-positive (rare since v6.11.25 removed NLP→Complete promotion).
Check for a `DATAWATCH_COMPLETE:` marker that misfired:

```sh
grep -c DATAWATCH_COMPLETE ~/.datawatch/sessions/<id>/output.log
```

If non-zero, the marker fired legitimately from the LLM's shell
wrapper. The session IS done from the daemon's perspective — start a
new one to continue.

### "Two sessions both think they own the same tmux pane"

Shouldn't happen but if it does:

```sh
datawatch sessions list-orphans              # tmux panes with no session record
tmux ls | grep cs-                            # tmux sessions
ls ~/.datawatch/sessions/                     # daemon-recorded sessions
```

Reconcile manually by killing the orphan tmux session:

```sh
tmux kill-session -t cs-<orphan-id>
```

## CLI reference

```sh
datawatch sessions start --backend ... --task "..." [--profile ...]
datawatch sessions list [--state running] [--backend ...]
datawatch sessions get <id>
datawatch sessions input <id> "..."         # send a message
datawatch sessions kill <id>
datawatch sessions repipe <id>              # re-establish the tmux pipe
datawatch sessions tail <id> [-f]           # tail output.log
datawatch sessions list-orphans             # tmux sessions without a daemon record
datawatch sessions reap <id>                # delete the dir + state
```

## Linked references

- Channel state engine: [`channel-state-engine.md`](channel-state-engine.md)
- Architecture: `architecture-overview.md`
- Profiles: `profiles.md`
- See also: `daemon-operations.md` for restart + log management

## All channels reference

| Channel | How |
|---|---|
| **PWA** | Sessions list (cards) + per-session detail (tmux/channel/stats tabs). |
| **Mobile** | Same surface in Compose Multiplatform. |
| **REST** | `POST /api/sessions/start`, `GET /api/sessions[/<id>]`, `POST /api/sessions/<id>/input`, `POST /api/sessions/kill`. |
| **MCP** | `session_start`, `session_get`, `session_input`, `session_kill`, `session_list`. |
| **CLI** | `datawatch sessions {start,get,list,input,kill,tail,repipe,reap,list-orphans}`. |
| **Comm** | `start: <task>` from any chat channel spawns; reply continues. `stop:<id>` kills. |
| **YAML** | Per-session state at `~/.datawatch/sessions/<id>/state.json`. Profiles at `~/.datawatch/profiles/projects/`. |

## Screenshots needed (operator weekend pass)

- [ ] Sessions list with mixed states (running / waiting_input / complete)
- [ ] Session detail view — Tmux tab with xterm output
- [ ] Session detail view — Channel tab with bubble feed
- [ ] Session detail view — Stats tab (CPU ring + RSS + threads)
- [ ] Loading splash + immediate dismissal on entry
- [ ] State badge with stale-comms amber dot
