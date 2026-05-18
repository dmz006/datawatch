---
docs:
  index: true
  topics: [sessions, tmux, lifecycle]
exec_params:
  - {name: llm, required: true, description: "LLM registry name (e.g. claude-code, my-ollama). Use 'datawatch llm list' to see options."}
  - {name: task, required: true, description: "Initial task / prompt for the session"}
  - {name: project_dir, required: false, default: ".", description: "Working directory"}
  - {name: compute_node, required: false, description: "Compute node name override (must be in the LLM's node list if set)"}
exec_steps:
  - tool: list_sessions
    description: Inventory current sessions before adding a new one
    args: {}
    read_only: true
  - tool: start_session
    description: Spawn the new session with the chosen LLM
    args:
      llm: "{{params.llm}}"
      task: "{{params.task}}"
      project_dir: "{{params.project_dir}}"
    read_only: false
  - tool: session_timeline
    description: Verify the new session is producing events
    args: {}
    read_only: true
---
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
- At least one LLM configured in the registry (`datawatch llm list`).
  See [`llm-registry.md`](llm-registry.md) to set one up.

## v7 Session fields

v7.0 added two new orthogonal identifiers carried on every session object:

| Field | JSON key | Meaning |
|-------|----------|---------|
| `LLMRef` | `llm_ref` | LLM registry entry name used to start this session (e.g. `claude-code`). Present only when session was started via the v7 `llm` path. |
| `ComputeNodeRef` | `compute_node_ref` | Compute Node the session is bound to. Set when `compute_node` was specified or auto-resolved from the LLM's node list. |
| `BackendFamily` | `backend_family` | Adapter family string (`claude-code`, `ollama`, `opencode`, etc.). Renamed from `llm_backend` in v7.0-alpha.27. External tooling must read `backend_family`; the legacy `llm_backend` key is no longer emitted. |

**Migration note:** If you have scripts or apps reading `sess.llm_backend`, switch them to `sess.backend_family`. Stored `state.json` files from before alpha.27 load cleanly (the daemon accepts both on read), but all new writes use the new key.

## Setup

No setup beyond having a backend. Sessions spawn on demand.

## Two happy paths

### 4a. Happy path — CLI

```sh
# 1. Start a session using the v7 LLM registry.
#    --llm   → LLM registry name (replaces legacy --backend)
#    --compute → optional; must be in the LLM's compute_nodes list if set
SID=$(datawatch sessions start \
  --llm claude-code \
  --project-dir ~/work/foo \
  --task "Audit the auth module for input-validation gaps" 2>&1 \
  | grep -oP 'session \K[a-z0-9-]+')
echo "session: $SID"

# With an explicit compute node (failover override):
SID=$(datawatch sessions start \
  --llm my-ollama --compute gpu-box-1 \
  --task "Refactor the auth module" 2>&1 | grep -oP 'session \K[a-z0-9-]+')

# 2. Watch its state in real time.
watch -n 1 "datawatch sessions get $SID | jq '.state, .last_channel_event_at'"

# 3. Tail the output log (most reliable view of what's happening).
datawatch sessions tail $SID -f
# (or: tail -F ~/.datawatch/sessions/$SID/output.log)

# 4. Send input.
datawatch sessions input $SID "Start with the password reset flow"

# 5. Inspect via API — note v7 field names.
datawatch sessions get $SID | jq
#  → {"id":"abcd","full_id":"ralfthewise-abcd","state":"running",
#     "task":"...","llm_ref":"claude-code","compute_node_ref":"gpu-box-1",
#     "backend_family":"claude-code","last_channel_event_at":"..."}

# 6. Rebind LLM in-flight (surgical patch, safe while session is running):
datawatch sessions set-llm-ref $SID my-other-llm

# 7. Stop when done.
datawatch sessions kill $SID

# 8. After-the-fact inspection (terminal state):
ls ~/.datawatch/sessions/$SID/
cat ~/.datawatch/sessions/$SID/state.json | jq
tail -50 ~/.datawatch/sessions/$SID/output.log
```

### 4b. Happy path — PWA

1. PWA → bottom nav **Sessions**. The list shows every session this
   daemon knows about; new sessions go to the top by default.
2. **Filter bar** — above the session list, three controls left to right:
   - **LLM (N) ▸** — collapses/expands a badge row showing each
     backend-family (`claude-code`, `opencode`, `opencode-acp`,
     `opencode-prompt`, `openwebui`, `ollama`, `shell`,
     `council-virtual`). Click a badge to filter; button highlights
     when a filter is active.
   - **State (N) ▸** — collapses/expands a colored dot rail for every
     real state (Running 🟢 / Waiting 🟠 / Rate-limited 🔴 / Complete ⚪
     / Failed 🔴 / Killed ⚪). Click any dot to filter; multiple states
     can be selected simultaneously.
   - **Text input** — narrows by session name, task, ID,
     `llm_ref`, or `compute_node_ref` substrings. Combines with the
     badge filters.
3. Click the **+** FAB → wizard:
   - **LLM** dropdown (populated from `/api/llms`; disabled entries
     filtered out; adapter kind shown in label).
   - **Compute Node** — cascading dropdown seeded from the chosen LLM's
     `compute_nodes` list. First entry labeled "(primary)"; subsequent
     "(failover N)". Shows "(any node OK)" when the LLM has no pinned
     list.
   - **Model** (populated from that LLM's enabled model list).
   - Task (free-text; one-paragraph task spec).
   - Project Profile (optional — picks workspace + git policy + skills).
   - Effort (only shown for backends that support it).
   - **Start**.
4. The wizard closes; the new session appears at the top of the list
   with state `running` + a green dot.
5. Click into the session card. The **meta-bar** shows:
   - ⚡ `<llm-name>` green badge (v7 LLM registry name, when set).
   - ⚙ `<compute-node>` purple badge (v7 Compute Node, when set).
   - Hover each for a tooltip explaining the v7 field.
   Detail view opens with three tabs:
   - **Tmux** — live xterm.js stream of the LLM's terminal. Read-only
     by default; tap the input bar to send commands.
   - **Channel** — structured event bubble feed (MCP / ACP /
     chat_message). Native swipe-back through the 1000-entry buffer.
   - **Status** — two sub-tabs:
     - **Status** — hook-fed board: Current focus / Sprint / Tests /
       Git cards (populated when claude-code hook scripts are wired;
       see [`claude-hooks.md`](claude-hooks.md)).
     - **Stats** — CPU ring + RSS + threads + FDs + GPU (if observer
       plugin enabled). Sections layout: process metrics + memory +
       thread/FD counts + optional GPU row.
6. Scroll modes: `Aa ▾` font dropdown (A−, size, A+, Fit) + `📜
   Scroll` enters tmux scroll mode (Page Up / Page Down / ESC tied
   into tmux's scroll-back).
7. State transitions appear as the badge updates (Running ↔
   WaitingInput ↔ Complete). The amber pulsing dot next to the badge
   means "no channel activity for >2 s" (early visual cue).
8. Stop with the **Stop** button in the toolbar.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Same surface in the mobile companion. Tabs and scroll modes work
identically; the input bar uses the OS keyboard with auto-send-on-Enter
optional.

### 5b. REST

```sh
# Start — v7 LLM registry path.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"llm":"claude-code","task":"...","project_dir":"/tmp"}' \
  $BASE/api/sessions/start

# Start — with explicit compute node.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"llm":"my-ollama","compute_node":"gpu-box-1","task":"..."}' \
  $BASE/api/sessions/start

# List + filter.
curl -sk -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/sessions?state=running&llm=claude-code"

# Get by ID — note v7 field names in response.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/sessions/<full-id>
# → {"id":"...","backend_family":"claude-code","llm_ref":"claude-code",
#    "compute_node_ref":"gpu-box-1","state":"running",...}

# Rebind LLM in-place (safe while session is running).
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"id":"<full-id>","llm_ref":"my-other-llm"}' \
  $BASE/api/sessions/set_llm_ref

# Send input.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"id":"<full-id>","text":"..."}' $BASE/api/sessions/<full-id>/input

# Kill.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -d '{"id":"<full-id>"}' $BASE/api/sessions/kill
```

### 5c. MCP

Tools: `start_session`, `list_sessions`, `session_output`, `send_input`,
`kill_session`, `restart_session`.

`start_session` args: `{"llm": "<registry-name>", "task": "...",
"project_dir": "...", "compute_node": "?", "profile": "?"}`. Returns
the full session metadata including `llm_ref` and `compute_node_ref`.

When invoked from inside an existing session (e.g. claude-code
spawning a sub-session), the parent session ID is recorded so the
two are linked in the cross-session memory subsystem.

### 5d. Comm channel

| Verb | Example |
|---|---|
| `start: <task>` | Spawns a session using the channel's default LLM. |
| `new: llm=<name> [compute=<node>]: <task>` | v7 — picks a specific LLM (and optionally a Compute Node) for this session. |
| `<reply text>` | Continues the most recent session in this chat. |
| `state <id>` | Returns current state. |
| `stop:<id>` | Kills. |
| `restart:<id>` | Restarts a terminal-state session (spawns fresh, seeded from prior). |

Each channel adapter inherits a `session.default_llm` from config
unless overridden per-channel. The `new:` verb routes through the REST
loopback so the same validation (LLM enabled, compute node in list) runs
in one place.

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
llm: claude-code
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
- **Spawning without a profile.** Default LLM + workspace are
  fine for chat sessions, but for long-running work a Project Profile
  saves repeated config and gives the LLM cleaner context.
- **Killing a session while reading its output.** Safe — kill closes
  the pipe and marks state Killed; the log file stays for inspection.

## Live Task Tree + Status Tab Dashboard

The **Status** sub-tab now shows a live task tree populated from structured
telemetry. It updates in real-time via WebSocket `hook_update` events — no
manual refresh needed.

**Session types detected automatically:**

| Type | Source | Notes |
|------|--------|-------|
| `automata` | Sprint `prd_id` in telemetry | Links to Automata detail |
| `cc` | TodoWrite hook events | Task IDs start with `todo-` |
| `test-runner` | `tests` in telemetry | Pass/fail/skip counts |
| `generic` | Fallback | Shows raw task list |

**On-demand guardrail invocation** from the Status tab: use the
Commands dropdown → Guardrails group to run `sast-scan`, `secrets-scan`,
or `deps-scan` against the session's project directory. The verdict is
appended to the session's telemetry immediately.

```sh
# Via CLI
datawatch session guardrail <session-id> sast-scan

# Via REST
curl -X POST https://localhost:8443/api/sessions/<id>/guardrail \
     -H 'Authorization: Bearer TOKEN' \
     -d '{"name":"sast-scan"}'
```

**Sprint ancestry breadcrumb** — when `telemetry.sprint.prd_id` is
present, the task tree card shows a clickable breadcrumb linking to the
Automata detail view.

**Parent session link** — if `telemetry.parent_session_id` is set, a
link appears at the top of the Status pane to navigate to the spawning
session.

**Deep-link from Automata detail**: each story's worker session row now
has a "● Status" button that navigates directly to that session's Status
sub-tab.

## Linked references

- Channel state engine: [`channel-state-engine.md`](channel-state-engine.md)
- Profiles: [`profiles.md`](profiles.md)
- Architecture: `../architecture-overview.md`
- Guardrails: [`guardrail-library.md`](guardrail-library.md)
- See also: `daemon-operations.md` for restart + log management

## Screenshots

![Sessions list with filter bar](https://raw.githubusercontent.com/dmz006/datawatch/main/docs/howto/screenshots/sessions-filter.png)

![New-session wizard](https://raw.githubusercontent.com/dmz006/datawatch/main/docs/howto/screenshots/sessions-new-wizard.png)

![Session detail — Tmux tab](https://raw.githubusercontent.com/dmz006/datawatch/main/docs/howto/screenshots/session-detail-tmux.png)

![Session detail — Status tab (hook board)](https://raw.githubusercontent.com/dmz006/datawatch/main/docs/howto/screenshots/session-detail-status.png)

![Session detail — Stats tab](https://raw.githubusercontent.com/dmz006/datawatch/main/docs/howto/screenshots/session-detail-stats.png)

<!-- Screenshots still needed: Channel tab bubble feed, loading splash, stale-comms amber dot -->

---

## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [howto/channel-state-engine](channel-state-engine.md)
- [howto/chat-and-llm-quickstart](chat-and-llm-quickstart.md)
- [howto/llm-registry](llm-registry.md)
- [howto/compute-nodes](compute-nodes.md)
- [howto/comm-channels](comm-channels.md)
- [howto/mcp-tools](mcp-tools.md)
- [architecture-overview](../architecture-overview.md)
- [api/sessions](../api/sessions.md)
