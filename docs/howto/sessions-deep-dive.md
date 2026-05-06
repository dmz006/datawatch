# How-to: Sessions — deep dive

Long-lived AI work units. Backed by tmux on disk; xterm.js streamed in
the PWA; rich event channel alongside the raw terminal; remembers
itself across daemon restarts. This walkthrough covers what a session
is made of, how it survives a restart, and where to look when it
misbehaves.

## What it is

A directory under `~/.datawatch/sessions/<full_id>/` containing:

```
output.log     ← full append-only tmux output (ANSI preserved)
state.json     ← session metadata (state, last-channel-event, ...)
tracking/      ← per-input record kept by the Tracker
wakeup.log     ← what was injected into L0/L1 layers at start
response.md    ← (optional) last LLM response captured for /copy + alerts
```

A `cs-<full_id>` tmux session attaches to `output.log` via `tmux
pipe-pane`. The LLM runs inside that tmux session.

States: `Running` (LLM processing), `WaitingInput` (paused, awaiting
your reply), `RateLimited` (auto-resumes), `Complete` / `Failed` /
`Killed` (terminal — sticky). See `channel-state-engine.md` for the
state-decision logic.

## Base requirements

- `datawatch start` — daemon up.
- `tmux` on PATH (every datawatch host needs it).
- An LLM backend with at least one model configured (claude-code,
  ollama, openai, etc.).

## Setup

No setup beyond having a backend. Sessions spawn on demand.

## Two happy paths

### 4a. Happy path — CLI

```sh
# 1. Start a session.
SID=$(datawatch sessions start \
  --backend claude-code \
  --project-dir ~/work/foo \
  --task "Audit the auth module for input-validation gaps" 2>&1 \
  | grep -oP 'session \K[a-z0-9-]+')
echo "session: $SID"

# 2. Watch its state in real time.
watch -n 1 "datawatch sessions get $SID | jq '.state, .last_channel_event_at'"

# 3. Tail the output log (most reliable view of what's happening).
datawatch sessions tail $SID -f
# (or: tail -F ~/.datawatch/sessions/$SID/output.log)

# 4. Send input.
datawatch sessions input $SID "Start with the password reset flow"

# 5. Inspect via API.
datawatch sessions get $SID | jq
#  → {"id":"abcd","full_id":"ralfthewise-abcd","state":"running",
#     "task":"...","backend":"claude-code","last_channel_event_at":"..."}

# 6. Stop when done.
datawatch sessions kill $SID

# 7. After-the-fact inspection (terminal state):
ls ~/.datawatch/sessions/$SID/
cat ~/.datawatch/sessions/$SID/state.json | jq
tail -50 ~/.datawatch/sessions/$SID/output.log
```

### 4b. Happy path — PWA

1. PWA → bottom nav **Sessions**. The list shows every session this
   daemon knows about; new sessions go to the top by default.
2. Click the **+** FAB → wizard:
   - Backend dropdown (only configured backends appear).
   - Task (free-text; one-paragraph task spec).
   - Project Profile (optional — picks workspace + git policy + skills).
   - Effort (only shown for backends that support it).
   - **Start**.
3. The wizard closes; the new session appears at the top of the list
   with state `running` + a green dot.
4. Click into the session card. Detail view opens with three tabs:
   - **Tmux** — live xterm.js stream of the LLM's terminal. Read-only
     by default; tap the input bar to send commands.
   - **Channel** — structured event bubble feed (MCP / ACP /
     chat_message). Native swipe-back through the 1000-entry buffer.
   - **Stats** — CPU ring + RSS + threads + FDs + GPU (if observer
     plugin enabled).
5. Scroll modes: `Aa ▾` font dropdown (A−, size, A+, Fit) + `📜
   Scroll` enters tmux scroll mode (Page Up / Page Down / ESC tied
   into tmux's scroll-back).
6. State transitions appear as the badge updates (Running ↔
   WaitingInput ↔ Complete). The amber pulsing dot next to the badge
   means "no channel activity for >2 s" (early visual cue).
7. Stop with the **Stop** button in the toolbar.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Same surface in the mobile companion. Tabs and scroll modes work
identically; the input bar uses the OS keyboard with auto-send-on-Enter
optional.

### 5b. REST

```sh
# Start.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"backend":"claude-code","task":"...","project_dir":"/tmp"}' \
  $BASE/api/sessions/start

# List + filter.
curl -sk -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/sessions?state=running&backend=claude-code"

# Get by ID.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/sessions/<full-id>

# Send input.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"id":"<full-id>","text":"..."}' $BASE/api/sessions/<full-id>/input

# Kill.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -d '{"id":"<full-id>"}' $BASE/api/sessions/kill
```

### 5c. MCP

Tools: `session_start`, `session_get`, `session_list`, `session_input`,
`session_kill`.

`session_start` args: `{"backend": "...", "task": "...", "project_dir":
"...", "profile": "?"}`. Returns the session metadata.

When invoked from inside an existing session (e.g. claude-code
spawning a sub-session), the parent session ID is recorded so the
two are linked in the cross-session memory subsystem.

### 5d. Comm channel

| Verb | Example |
|---|---|
| `start: <task>` | Spawns a session using the channel's default backend. The reply is the LLM's first response. |
| `<reply text>` | Continues the most recent session in this chat. |
| `state <id>` | Returns current state. |
| `stop:<id>` | Kills. |
| `restart:<id>` | Restarts a terminal-state session (spawns fresh, seeded from prior). |

Each channel adapter inherits a `session.default_backend` from config
unless overridden per-channel.

### 5e. YAML

Per-session state at `~/.datawatch/sessions/<id>/state.json` is
operator-readable but daemon-managed — don't hand-edit (race-prone).

For per-spawn-defaults, use Project Profiles
(`~/.datawatch/profiles/projects/<name>.yaml` — see `profiles.md`).
For session templates (saved bundles of backend + effort + skills +
profile), use `~/.datawatch/session-templates/<name>.yaml`.

Session templates schema:

```yaml
name: audit-flow
backend: claude-code
effort: thorough
skills: [secrets-scan, sast]
profile: prod-audit
default_task: |
  Audit the codebase for ...
```

Apply: `datawatch sessions start --template audit-flow`.

## Diagram

```
       PWA / Mobile / CLI / Comm / REST / MCP
                        │
                        ▼
  ┌──────────────────────────────────────────┐
  │ Daemon                                    │
  │  ┌──────────────┐   ┌─────────────────┐  │
  │  │ Session store│   │ Channel-state   │  │
  │  │  state.json  │◄──┤  engine          │  │
  │  └──────────────┘   └─────────────────┘  │
  │       │                                   │
  │       │ tmux pipe-pane                    │
  │       ▼                                   │
  │  ┌──────────────────────────────────┐    │
  │  │ cs-<id> tmux session              │    │
  │  │   ┌────────────────────────────┐  │    │
  │  │   │ LLM (claude-code / ollama /│  │    │
  │  │   │  opencode / etc.)          │  │    │
  │  │   └────────────────────────────┘  │    │
  │  └──────────────────────────────────┘    │
  └──────────────────────────────────────────┘
              │
              ▼
       output.log (append-only, ANSI preserved)
```

## Where to look when something is wrong

### "State stuck on `waiting_input` but the session is producing output"

Most likely cause: `LastChannelEventAt` not bumping on output arrival.
Diagnostic walk in `channel-state-engine.md` § Step-by-step debug.

### "Splash never dismisses on session entry"

Open browser dev console. WS messages of type `pane_capture` with
non-empty `lines` should arrive within ~200 ms of subscribe. The
synthesized fallback ensures at least one frame fires; if it doesn't,
check daemon log for `pane_capture` errors.

### "No new lines arriving in `output.log`"

Pipe is broken:

```sh
tmux ls | grep cs-<id>             # confirm tmux session exists
tmux list-panes -t cs-<id>          # confirm pane
datawatch sessions repipe <id>      # manually re-establish the pipe
```

### "Session shows Complete but I'm still working in it"

Grep for the marker that fired the transition:

```sh
grep -n DATAWATCH_COMPLETE ~/.datawatch/sessions/<id>/output.log
```

If the marker fired legitimately, the session IS done from the
daemon's perspective — start a new one.

### "Two sessions both think they own the same tmux pane"

```sh
datawatch sessions list-orphans       # tmux panes with no session record
tmux ls | grep cs-                     # tmux sessions
```

Reconcile by killing the orphan: `tmux kill-session -t cs-<orphan-id>`.

## Common pitfalls

- **Editing `state.json` by hand.** Race-prone. Use the API.
- **Confusing `last_channel_event_at` with `updated_at`.** UpdatedAt
  is bumped by daemon-internal housekeeping (rate-limit timer,
  reconcile, etc.). LCE is the real activity signal.
- **Spawning without a profile.** Default backend + workspace are
  fine for chat sessions, but for long-running work a Project Profile
  saves repeated config and gives the LLM cleaner context.
- **Killing a session while reading its output.** Safe — kill closes
  the pipe and marks state Killed; the log file stays for inspection.

## Linked references

- Channel state engine: [`channel-state-engine.md`](channel-state-engine.md)
- Profiles: [`profiles.md`](profiles.md)
- Architecture: `../architecture-overview.md`
- See also: `daemon-operations.md` for restart + log management

## Screenshots needed (operator weekend pass)

- [ ] Sessions list with mixed states (running / waiting_input / complete)
- [ ] New-session wizard
- [ ] Session detail — Tmux tab with xterm output
- [ ] Session detail — Channel tab with bubble feed
- [ ] Session detail — Stats tab (CPU ring + RSS + threads)
- [ ] Loading splash + immediate dismissal on entry
- [ ] State badge with stale-comms amber dot
- [ ] Scroll mode buttons (Page Up / Page Down / ESC)
