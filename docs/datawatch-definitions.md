# datawatch — User Manual

## What is datawatch?

Datawatch is a single-binary control plane for orchestrating AI work. One operator, one daemon, one consistent surface — and underneath, a set of long-running AI sessions, ephemeral container workers, persistent memory, structured identity, multi-phase reasoning, rubric-based evaluation, and multi-persona debate, all running together with one set of lifecycle, audit, and security guarantees.

It started as a small bridge between mobile messaging apps and AI coding sessions running in `tmux` — type a message in Signal, watch the LLM work, get the answer back. That bridge is still here, but the surface around it grew. Today datawatch runs entire workflows end-to-end: it holds your operator identity (your role, your goals, your constraints) and injects it into every spawned session so the AI stays aligned. It decomposes a high-level intent into a directed graph of stories and tasks, dispatches each to the right backend, captures the output, runs it past graders, and pulls in a council of personas for structured critique when the decision is non-trivial. It remembers what was decided last week. It can spawn workers in remote Kubernetes clusters as easily as it spawns local sessions. It speaks the same REST + MCP + CLI + chat + PWA + mobile + YAML surface no matter which side you approach it from.

## Who is it for?

Operators who want **one** place to drive AI work — not a tab in five different web apps, not a notebook here and a CLI there. People who want their AI sessions to come back tomorrow remembering what was discussed today. Teams that need their AI work attributed, audited, and bounded by explicit authorization. Hobbyists who want a PAI (Personal AI Infrastructure) they can self-host without paying for SaaS layers.

## What it gives you

- **Long-lived AI sessions** that survive daemon restarts and re-attach cleanly. xterm.js streaming in the PWA, full tmux underneath, full event history captured.
- **Ephemeral container workers** in Docker or Kubernetes, spawned on demand with PQC bootstrap, distroless images, per-pod auth, and Tailscale mesh.
- **Episodic memory** — your sessions remember each other. Vector-indexed project knowledge across sessions, with the spatial schema (floor / wing / room / hall / shelf / box) that makes recall actually work. The **scope hierarchy** (persona-global → persona-in-project → project-shared → session-local) lets you borrow cross-agent context without polluting higher scopes, seed curated knowledge into a narrower scope, and promote session discoveries up to shared scopes with breadcrumb provenance.
- **Multi-channel messaging** — Signal, Telegram, Discord, Slack, Matrix, Twilio, GitHub webhooks, generic webhooks, DNS channel; voice input via Whisper; image/photo attachments described by a configurable vision backend.
- **Pluggable LLM backends** — claude-code, aider, goose, gemini, opencode, opencode-acp, ollama, openwebui, custom shell.
- **Operator identity** — a structured self-description you write once and the daemon injects into every spawned session as the L0 wake-up layer.
- **Algorithm Mode** — a 7-phase structured-thinking harness (Observe → Orient → Decide → Act → Measure → Learn → Improve) you can drive a session through with output captured at each gate.
- **Evals Framework** — rubric-based grading suites (string / regex / binary / LLM-rubric) with capability vs regression thresholds.
- **Council Mode** — multi-persona debate. 12 default personas (security-skeptic, ux-advocate, perf-hawk, simplicity-advocate, ops-realist, contrarian, platform-engineer, network-engineer, data-architect, privacy, hacker, app-hacker). Quick (1 round) for fast checks, debate (3 rounds) for serious decisions. Async-first with SSE live-watch; AI persona wizard drafts `system_prompt` via LLM interview. Accepts `image_path` on `POST /api/council/run` — image is described once and injected into every persona's proposal (requires `vision.enabled: true`).
- **Skill registries** — git-backed PAI-format skill manifests synced into your workspaces on demand.
- **Secrets manager** — native AES-256-GCM store at `~/.datawatch/secrets.db` plus optional KeePass and 1Password backends; `${secret:name}` references resolve in YAML, plugin manifests, spawn-time env injection.
- **Federated observer** — multiple datawatch instances pushing process / network / GPU stats into one aggregated view.
- **Autonomous Automata (PRD-DAG)** — high-level intent decomposed into a directed graph of stories and tasks, executed under verification + guardrails.
- **Plugin framework** — manifest-driven hot-reload; subprocess + native plugins; declared comm verbs / CLI subcommands / MCP tools / mobile cards. Community plugins can be installed from a connected registry with `datawatch plugins install <registry> <name>`.

## How it's built

**Single binary.** No language runtime to install, no microservices to operate, no bus to deploy. The binary embeds the PWA, the docs, the MCP server, the messaging adapters, and the daemon — `datawatch start` is the whole install.

**One surface, mirrored seven ways.** Every feature reachable through the REST API is reachable via MCP, the CLI, every comm channel, the PWA, the mobile (Compose Multiplatform) app, and YAML on disk. **Read once, write once, audit once.** No drift between surfaces.

**Tmux-backed sessions.** AI work happens in real terminals so you can attach with `tmux attach` and see exactly what the LLM is doing — no abstraction layer between you and the work.

**Open data.** Sessions, memory, identity, audit, scheduled work, and persona definitions all live as plain files under `~/.datawatch/`. No proprietary database. Operator-editable, operator-grep-able, operator-backup-able.

## How to use this manual

Each section maps to one PWA tab or card. The structure is the same throughout: what the card is for → what each control does → links to deeper reference (architecture docs, how-to walkthroughs, diagrams). The PWA's `?` icon next to the search button (Sessions / Session detail / Automata / Observer / Settings views) deep-links you straight to the matching section here.

There's also a **Core feature reference matrix** further down, listing which features have dedicated walkthroughs, plans, and architecture diagrams — the gaps in that matrix are what an upcoming docs-as-MCP-interface will fall back on.

---

## Table of contents

- [Sessions](#sessions)
  - [Sessions list](#sessions-list)
  - [Session lineage — parent-child tracking](#session-lineage--parent-child-tracking)
  - [Session AI summarizer](#session-ai-summarizer)
  - [Inside a session — terminal area](#inside-a-session--terminal-area)
  - [Inside a session — channel tab](#inside-a-session--channel-tab)
  - [Inside a session — stats tab](#inside-a-session--stats-tab)
- [Automata](#automata)
  - [Automata list](#automata-list)
  - [Launch Automation form](#launch-automation-form)
  - [Automaton detail — Overview / Stories / Decisions / Scan](#automaton-detail)
- [Dashboard](#dashboard)
  - [Session constellation](#session-constellation)
  - [EKG waveform](#ekg-waveform)
  - [Sprint pipeline](#sprint-pipeline)
  - [Expand panel](#expand-panel)
- [Observer](#observer)
  - [Federated peers](#federated-peers)
  - [Process envelopes](#process-envelopes)
  - [eBPF per-process net](#ebpf-per-process-net)
  - [Audit log](#audit-log)
  - [Knowledge graph](#knowledge-graph)
  - [Daemon log](#daemon-log)
- [Settings](#settings)
  - [General](#settings-general)
  - [Plugins](#settings-plugins)
  - [Comms](#settings-comms)
  - [LLM](#settings-llm)
  - [Agents](#settings-agents)
  - [Automate](#settings-automate)
  - [MCP](#settings-mcp)
  - [About](#settings-about)
- [Documentation index](#documentation-index)

---

## Sessions

### Sessions list

The home view. Every session your daemon knows about, regardless of state. New sessions appear at the top by default; reorder is persisted per-operator.

**Card columns:**

- **State badge** (`running` / `waiting_input` / `complete` / `failed` / `killed` / `rate_limited`). The amber pulsing dot next to the badge means "no channel activity for >2 s" — an early visual cue that comms have gone quiet (15 s of silence flips Running → WaitingInput; the dot is informational only).
- **ID + backend** — the session's short ID (`xxxx`), backend label, and any agent / server tag.
- **Time** — relative since last update.
- **Action buttons** — context-dependent: Stop (active), Restart (done), Last response, Delete, multi-select checkbox.
- **Drag handle** — manual reorder.

**Greyed cards** indicate Done / Killed / Failed states; the action buttons remain at full opacity so it's obvious what's still clickable.

**Filtering:** the filter dropdown at the top scopes by state, backend, and tag. Multi-select bar appears on the first checkbox tick.

**See also:**
[howto/sessions-deep-dive](howto/sessions-deep-dive.md) ·
[howto/channel-state-engine](howto/channel-state-engine.md) ·
[howto/chat-and-llm-quickstart](howto/chat-and-llm-quickstart.md) ·
[architecture-overview](architecture-overview.md) ·
[architecture](architecture.md) ·
[backends](backends.md) ·
[api/](api/)

### Session lineage — parent-child tracking

When an AI agent spawns sub-agents to parallelize work, datawatch tracks the parent-child relationship. Each session can record the ID of the session that created it and whether its children should be stopped when it stops.

**How to record lineage:**

When starting a sub-agent session, pass the spawning session's ID:

- **MCP:** `start_session(task="...", caller_session_id="host-abc123")` — the spawning agent passes its own `$CLAUDE_SESSION_ID`.
- **REST:** `POST /api/sessions/start` body: `{"task": "...", "parent_id": "host-abc123"}`
- **CLI:** `datawatch session new "task" --parent host-abc123`
- **Channel:** `new: parent=host-abc123: task description`

**Cascade kill (opt-in):**

By default, sub-agents run independently and survive their parent. Two modes:

*Shallow (direct children only):* set `kill_children=true` when creating the parent.
- **MCP:** `start_session(task="...", kill_children=true)`
- **REST:** `{"task": "...", "kill_children": true}`
- **CLI:** `datawatch session new "task" --kill-children`
- **Channel:** `new: kill_children=true: task description`

*Recursive (all descendants — BL351):* set `kill_children_recursive=true`. Kills grandchildren and deeper regardless of their own settings.
- **MCP:** `start_session(task="...", kill_children_recursive=true)`
- **REST:** `{"task": "...", "kill_children_recursive": true}`
- **CLI:** `datawatch session new "task" --kill-children-recursive`
- **Channel:** `new: kill_children_recursive=true: task description`
- **PWA:** "Kill children recursively" checkbox in the new session form.

**Viewing children:**

- **MCP:** `session_children(session_id="pp01")` — lists child sessions with state and task.
- **REST:** `GET /api/sessions/{id}/children` — JSON array of child sessions.
- **REST tree:** `GET /api/sessions?tree=1` — full session forest as nested tree.
- **REST aggregated:** `GET /api/sessions/aggregated?parent_id=<id>` — children across all federation peers (BL352).
- **CLI:** `datawatch session children <id>` — local children; `--all-servers` for federation peers.
- **Session list:** child sessions show `↳ child of [<parent-id>]` in all list views.
- **PWA Tree view:** toggle "Tree" in Sessions toolbar to group sessions by parent/child lineage with orphan indicators.

**Orphaned sessions (BL350):**

Sessions whose `parent_id` references a session that no longer exists.

- **REST:** `GET /api/sessions/orphaned` or `GET /api/sessions?orphaned=1`
- **MCP:** `list_orphaned_sessions` tool; `list_sessions(orphaned=true)` param
- **CLI:** `datawatch session orphaned` or `datawatch session list --orphaned`
- **Channel:** `session orphaned`

**Self-session discovery (BL349):**

Agents that need to know their own session ID without relying on `$CLAUDE_SESSION_ID`.

- **MCP:** `get_my_session_id(hint="<id>")` — hint is optional; finds the channel-ready active session.
- **REST:** `GET /api/sessions/self`
- **CLI:** `datawatch session self`
- **Channel:** `session self`

**Sub-agent reporting back:**

A sub-agent can send a message to its spawning parent's input prompt without any out-of-band coordination:

- **MCP:** `reply_to_parent(session_id="cc01", text="Done. 14 tests written, all passing.")`

This sends the text as input to the parent session so it can act on the result.

**See also:** [`howto/sessions-deep-dive.md`](howto/sessions-deep-dive.md)

---

### Recurring named schedules

Schedule a command to fire on a cron schedule, targeting a session by name rather than by ID — so the schedule survives session restarts.

**Creating a recurring schedule:**

- **MCP:** `schedule_add(cron_expr="*/5 * * * *", session_name="worker", command="summarize")`
- **REST:** `POST /api/schedule` body: `{"cron_expr": "*/5 * * * *", "session_name": "worker", "command": "summarize", "schedule_name": "periodic-summary"}`
- **CLI:** `datawatch schedule add --cron "*/5 * * * *" --session-name worker --schedule-name periodic-summary "summarize"`
- **Channel:** `schedule session_name=worker cron=*/5 * * * * command=summarize`

**Cancelling by schedule name:**

- **MCP:** `schedule_cancel(name="periodic-summary")`
- **REST:** `DELETE /api/schedules?name=periodic-summary`
- **CLI:** `datawatch schedule cancel name=periodic-summary`
- **Channel:** `schedule cancel name=periodic-summary`

**Cron format:** standard 5-field (`minute hour day month weekday`). Supports `*`, `*/n`, `n`, `n-m`, `n,m,...`.

If the named session is absent when the schedule fires, the item is skipped (not failed) and will try again on the next tick.

---

### Scheduled session spawn (ephemeral one-shot)

Spawn a fresh, independent session at a scheduled time or on a recurring cron — without targeting any existing session. The spawned session starts with a clean workspace, runs its task, and (by default) terminates automatically when it outputs `DATAWATCH_COMPLETE:`. This is the recommended pattern for scheduled jobs that should be independent of any operator session (e.g. hourly audit runs, nightly reports, background data pulls).

**Creating a spawn schedule:**

- **MCP:** `schedule_spawn(task="run audit", dir="/home/user/project", cron_expr="0 * * * *", name="hourly-audit", one_shot=true, ephemeral=true)`
- **REST:** `POST /api/schedules` body: `{"type": "spawn", "task": "run audit", "project_dir": "/home/user/project", "cron_expr": "0 * * * *", "schedule_name": "hourly-audit", "one_shot": true, "ephemeral": true}`
- **CLI:** `datawatch schedule spawn --task "run audit" --dir /home/user/project --cron "0 * * * *" --schedule-name hourly-audit --ephemeral`
- **CLI (shell mode):** `datawatch schedule spawn --shell "run-rules --config ~/cfg.yaml" --path /home/user/project --cron "0 * * * *" --schedule-name hourly`
- **Channel:** `schedule spawn task=run audit dir=/home/user/project cron=0 * * * * name=hourly-audit ephemeral=true`

**Key parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `task` | string | — | Task prompt sent to the spawned AI session |
| `shell` | string | — | Shell command to run (`--subprocess` auto-set; no LLM). Use `--task` or `--shell`, not both. |
| `path` / `dir` | string | `""` | Working directory for the session (`--path` is an alias for `--dir`) |
| `backend` | string | `""` | LLM backend name (e.g. `claude-code`); omit for `--shell` jobs |
| `llm_ref` | string | `""` | Unified LLM registry reference (e.g. `"claude-sonnet"`) |
| `model` | string | `""` | Model name override |
| `effort` | string | `""` | Effort level: `quick`, `normal`, or `thorough` |
| `name` | string | `""` | Human-readable name for this schedule entry (for lookup/cancel) |
| `session-name` | string | `""` | Name given to each spawned session instance |
| `run_at` | time | computed | Explicit first fire time (ISO-8601 or `HH:MM`) |
| `cron_expr` | string | `""` | 5-field cron for recurring spawns |
| `one_shot` | bool | `true` | Auto-terminate session on `DATAWATCH_COMPLETE:` |
| `ephemeral` | bool | `false` | Reap workspace directory when session is deleted |
| `subprocess` | bool | `false` | Run via `bash -c`; exit code = completion (0=complete). Auto-set by `--shell`. |

**Overlap guard:** If the previous spawn for this schedule is still running (state not `complete`/`failed`/`killed`), the cron fire is skipped and `RunAt` advances to the next slot. This prevents stacking concurrent runs when a job takes longer than its cron interval.

**Run history:** Each spawn-type schedule tracks `fire_count`, `last_fire_at`, `last_fire_result` (`spawned`/`skipped`/`failed`), and `active_spawn_id`. `schedule list` shows FIRES / LAST-FIRE / LAST-RESULT columns.

**Shell jobs example (no LLM):**

```bash
datawatch schedule spawn \
  --cron "0 * * * *" \
  --path /home/user/workspace/imap-mcp \
  --schedule-name imap-hourly-rules \
  --shell "imap-mcp run-rules --config ~/workspace/cfg.yaml"
```

**Difference from `schedule new-session`:** `new_session` starts a session connected to the scheduler's existing session context. `spawn` starts a fully independent session with its own lifecycle, optional ephemeral workspace, `DATAWATCH_COMPLETE:` auto-termination, and overlap guard.

**Cancelling a spawn schedule:**

- **MCP:** `schedule_cancel(name="hourly-audit")`
- **REST:** `DELETE /api/schedules?name=hourly-audit`
- **CLI:** `datawatch schedule cancel name=hourly-audit`
- **Channel:** `schedule cancel name=hourly-audit`

If `cron_expr` is set, the spawn reschedules automatically after each fire. Cancel explicitly to stop recurrence.

---

### Session zombie detection

Detects when Claude has exited but the shell is still running inside the tmux pane.

**How it works:** A periodic reconciler probes each active session by running `tmux display-message #{pane_current_command}`. If the foreground process is a shell (bash/sh/zsh/fish/dash/ksh/tcsh), `claude_alive` is set to `false` (zombie). Otherwise it stays `true`.

**`claude_alive` field:**

| Value | Meaning |
|-------|---------|
| `true` | Claude process is in the foreground |
| `false` | Shell is in the foreground — Claude has exited |
| absent/null | Session is not active (terminal state) |

**Accessing alive status:**

- **REST:** included in every session JSON object as `claude_alive`
- **MCP:** `list_sessions(format=json)` includes `claude_alive`; text format shows `Alive: yes/no`
- **CLI:** `datawatch session list` shows an ALIVE column
- **Channel:** `session list` shows `[ZOMBIE]` tag for dead sessions
- **PWA:** amber `⚠ zombie` badge on session card

**On zombie detection:** a `LevelWarn` alert is created and a `session_zombie` push event fires.

---

### Session exit hooks

Automatically restart or notify when a session goes zombie or terminates abnormally.

**Hook configuration (YAML):**

```yaml
session:
  exit_hooks:
    - name: "worker"
      action: restart        # restart | notify
      cooldown_seconds: 300
    - name: "coordinator"
      action: notify
      notify_session: "ops-monitor"
      notify_message: "coordinator crashed"
      cooldown_seconds: 60
```

**Trigger conditions:** `claude_alive` flip to false (zombie), or session entering `failed` / `killed` state.

**Actions:**

| Action | Behavior |
|--------|----------|
| `restart` | Kills session and relaunches with same task/name |
| `notify` | Sends `notify_message` to the named `notify_session` via `send_input` |

**Managing hooks at runtime:**

- **MCP:** `exit_hook_list`, `exit_hook_add`, `exit_hook_delete`, `exit_hook_enable`, `exit_hook_disable`
- **REST:** `GET/POST/PUT/DELETE /api/exit-hooks`
- **CLI:** `datawatch exit-hook list/add/delete/enable/disable`
- **Channel:** `exit_hook list/add/delete/enable/disable`
- **PWA:** Exit Hooks section in Settings → Compute

---

### Extra MCP servers per session

Inject additional MCP servers into every spawned session's `.mcp.json` alongside the built-in datawatch bridge. Useful for operators who want a companion tool (e.g. `imap-mcp`, a filesystem indexer, a custom API proxy) available automatically in every session without hand-editing `.mcp.json`.

**Configuration (YAML):**

```yaml
session:
  extra_mcp_servers:
    - name: imap-mcp                         # Key in .mcp.json mcpServers block
      command: /home/user/imap-mcp/imap-mcp  # Executable (absolute path)
      args: []                               # Optional CLI args
      env:                                   # Optional env vars (${VAR} expansion supported)
        GMAIL_APP_PASSWORD: "${GMAIL_APP_PASSWORD}"
```

**How it works:** At session start, `WriteProjectMCPConfig` (non-claude-code backends) or `InjectExtrasIntoMCPConfig` (claude-code) merges each extra entry into `<project_dir>/.mcp.json`. Existing operator-added entries are preserved; only entries whose names match the extras list (or the built-in `datawatch` entry) are managed.

**Managing at runtime:**

- **REST / MCP / CLI / comm / PWA:** use the standard config API to read or update `session.extra_mcp_servers`. See `GET /api/config` → `session.extra_mcp_servers`.

---

### Session restart from any state

Restart a session regardless of its current state — running, waiting for input, failed, or killed.

- **MCP:** `restart_session(session_id="pp01")` or `restart_session(session_name="worker", task="new task")`
- **REST:** `POST /api/sessions/{id}/restart` body: `{"task": "optional new task"}`
- **CLI:** `datawatch session restart worker --task "new task"`
- **Channel:** `session restart name=worker task=new task`
- **PWA:** Restart button on session cards in all states

The session ID and name are preserved. An optional `task` parameter overrides the stored task for the new run.

---

### Work queue

A durable role-based queue for coordinating work across multiple agent sessions.

**Typical flow:**

1. **Push:** `queue_push(role="analyzer", payload={"file": "main.go"})` — any session creates a work item.
2. **Claim:** `queue_claim(role="analyzer", claimed_by="agent-01", lease_seconds=300)` — an agent claims the oldest pending item. Returns nil if none available.
3. **Complete:** `queue_complete(id="q-abc", result={"lines": 450})` — mark done with optional result.
4. **Fail:** `queue_fail(id="q-abc", error="parse error on line 42")` — mark failed.

Unclaimed items whose lease expires are automatically returned to `pending` state (checked every 30 s).

**Surfaces:**

- **MCP:** `queue_push`, `queue_claim`, `queue_complete`, `queue_fail`, `queue_list`
- **REST:** `GET /api/queue?role=&state=`, `POST /api/queue/push|claim|complete|fail`, `DELETE /api/queue/{id}`
- **CLI:** `datawatch queue push/claim/complete/fail/list`
- **Channel:** `queue push/claim/complete/fail/list`

---

### Discussion push / subscribe

Subscribe an agent session to a discussion so new entries are delivered as live input.

- **MCP:** `discussion_subscribe(discussion_id="proj-alpha", session_name="summarizer")`
- **REST:** `POST /api/discussion-subs` body: `{"discussion_id": "proj-alpha", "session_name": "summarizer"}`
- **CLI:** `datawatch discussion-sub subscribe --discussion proj-alpha --session summarizer`

When a new entry is written to the discussion WAL, datawatch sends it to the subscribing session via `send_input`.

**Long-polling:** `memory_discussion_wal(discussion_id="proj-alpha", after_seq=12, block=true, timeout=60)` — blocks until a new entry arrives or the timeout expires.

**Unsubscribe:** `discussion_unsubscribe(discussion_id="proj-alpha", session_name="summarizer")`

---

### Agent result store

A named key-value store for passing structured results between agents.

- **MCP:** `result_put(name="analysis-result", payload={"score": 0.92}, ttl_seconds=3600)`
- **MCP:** `result_get(name="analysis-result")` — returns the stored entry or "Not found".
- **MCP:** `result_list(prefix="analysis-")` — lists all entries with the given prefix.
- **MCP:** `result_delete(name="analysis-result")`
- **REST:** `GET/POST/DELETE /api/result-store`, `GET /api/result-store/{name}`
- **CLI:** `datawatch result put/get/list/delete`

Entries with a TTL are automatically expired. Data survives daemon restarts.

---

### Structured session filters

Filter `list_sessions` by name, state, backend, or alive status.

- **MCP:** `list_sessions(name="worker-*", state="running", alive="true", format="json")`
- **REST:** `GET /api/sessions?name=worker-*&state=running&alive=true`
- **CLI:** `datawatch session list --name "worker-*" --state running --alive true --json`

`name` supports glob patterns (`*`, `?`). `alive` accepts `true`, `false`, or `any` (default). `format=json` returns a structured JSON array including `claude_alive`.

---

### Session AI summarizer

Each session maintains a live AI-generated summary of its most recent output. A background pipeline triggers after each significant channel event, runs a single LLM call against recent terminal output, and produces two complementary summary forms.

**Summary fields:**

| Field | What it holds |
|---|---|
| `last_response` | Short (3 sentences, ≤15 words each): task, outcome, next step |
| `last_summary_long` | Narrative (3–5 sentences): context, decisions, blockers, next steps |

**Where to see them:**

- **PWA session card** — `last_response` appears beneath the session ID badge on every card. Click the envelope button to expand `last_summary_long`.
- **REST:** `GET /api/sessions/{id}/current-status` — returns `{last_response, last_summary_long, summary_model, summary_updated_at}`.
- **CLI:** `datawatch session status <id>` includes the summarizer output block.

**Summarizer model selection:**

Configure which LLM handles summarization via `session.summarizer.llm_ref` and `session.summarizer.model` in `datawatch.yaml` or `PUT /api/config`. The PWA shows a datalist from all registered compute nodes with quality-tier hints. When unset, the session's own backend LLM is used.

**Plain-English / TTS-safe output (v8.10.19+):**

Both summary fields are post-processed to be safe for text-to-speech surfaces like Android Auto and lock screens:
- File paths, function names, and identifiers are stripped before returning.
- Error codes, hex values, and `file.go:line` references are removed.
- Lines that are mostly non-alphabetic characters (code fragments, stack traces) are dropped.
- Markdown decorators (`**bold**`, `` `backtick` ``, bullets, numbered lists) are removed.
- Timestamps (`[11:20:37]`), version numbers (`v8.10.25`), ISO dates, exit codes, durations, and percentages are converted to spoken form rather than stripped.
- The LLM prompt explicitly prohibits all identifiers, error codes, and code terms in favor of plain spoken English.

**Work-focused summarization (v8.10.25+):**

Before sending terminal output to the LLM, known end-of-task artifacts are stripped from the tail of the captured text: git log lines (commit hash + message), timing markers (`[HH:MM:SS] done`), `DATAWATCH_COMPLETE:` markers, `Co-Authored-By:` git trailers, git commit summaries (`N files changed`), and `create/delete mode` lines. This ensures the summary describes what was actually done during the task rather than the session housekeeping that appears at the very bottom of the terminal.

For `waiting_input` sessions, the Ollama summary is preserved across repeated API polls — the stored summary is returned directly rather than being overwritten by a fresh tmux capture.

**Parser resilience:** strips `<think>…</think>` reasoning blocks, handles multi-format short/long section markers, and falls back to paragraph splitting for LLMs that don't emit a structured separator.

**See also:** [`howto/sessions-deep-dive.md`](howto/sessions-deep-dive.md)

---

### Inside a session — terminal area

`Tmux` tab is the live xterm.js view of the tmux pane the LLM is attached to. Read-only by default; tap the input bar to send commands.

**Toolbar:**
- `Aa ▾` font controls — A−, A+, current size, Fit-to-width.
- `Scroll` — enter tmux scroll mode (Page Up / Page Down / ESC). Ties into tmux's own scroll-back so you see real history.

**Input bar:** sends text with Enter; the daemon routes it through tmux send-keys. State transitions back to `Running` automatically on input (and bumps the channel-event timer so the gap watcher doesn't immediately re-flip).

**Loading splash:** appears while the first pane_capture frame arrives. Always dismisses — even for ended sessions, the saved final frame is shown.

**Generating indicator:** a 3-dot wave below the output area when the session is `Running`. Sits below the channel tab too, so it's always under the visible content.

**Anti-clobber typing detection:** when an agent (via MCP, channel relay, or session→session send) sends input to an active session while the operator is typing in the xterm input bar, the daemon holds the message until 30 s of operator keystroke idle has elapsed, so agent messages never clobber in-progress operator input. Multiple agent messages that arrive during a typing window queue in arrival order (capacity 256) and flush immediately after the first held message is finally sent. Session-to-session AI relays (agent→agent) bypass the idle wait entirely. The queue closes cleanly on session delete.

**Renderer lock:** on startup the daemon writes `"tui":"default"` into `<dataDir>/.claude/settings.json`, pinning claude-code to line-printing mode. This prevents Anthropic's server-side alt-screen flag from switching claude to alternate-screen output (`\e[?1049h`), which would break tmux scrollback and the `pipe-pane` capture that datawatch relies on.

### Inside a session — channel tab

The structured event stream — MCP `channel_reply`, ACP messages, chat-mode events — rendered as bubbles. Backed by a 1000-entry per-session buffer; native swipe / scroll wheel scrolls back through history.

Direction icons: `←` incoming, `→` outgoing, `⚡` notify.

### Inside a session — stats tab

Process metrics for the session's process tree (CPU ring, RSS, threads, FDs, network, GPU, PID). Pulled from `/api/observer/envelopes` every 5 s while the tab is open; falls back to the session's `backend:` envelope (e.g. `backend:claude-docker`) when per-session attribution isn't available — useful for docker-backed LLMs where the host observer can't reach inside the container.

**See also:**
[howto/federated-observer](howto/federated-observer.md) ·
[architecture-overview](architecture-overview.md)

### Inside a session — status tab

The **Status** sub-tab (session detail → Status) shows a live sprint/git/test dashboard assembled from hook events the session's coding agent emits. Updated via `POST /api/sessions/{id}/hook-event`; readable at `GET /api/sessions/{id}/status`. Four panels:

| Panel | What it shows |
|---|---|
| **Current focus** | Last hook event description |
| **Sprint** | Task name + completion % |
| **Tests** | Last test run outcome (pass/fail/skip counts) |
| **Git** | Current branch + recent commit |

For claude-code sessions, the daemon auto-installs a `.claude/sprint/post-event.sh` hook script into the project directory on session start. Other backends (opencode, opencode-acp) emit equivalent events through their own hook paths.

**CLI:** `datawatch session status <id>` · **REST:** `GET /api/sessions/{id}/status` · **MCP:** `session_timeline`

**See also:** [`howto/claude-hooks.md`](howto/claude-hooks.md)

---

## Automata

### Automata list

Every PRD / autonomous workflow your daemon has spawned. Filtered by state pill (Running / Stopped / Failed / All).

### Launch Automation form

The "+" FAB on the Automata view launches a wizard.

**Top strip — Start from template:** browse saved Automaton templates instead of writing one from scratch. Templates carry a complete spec (type, stories, tasks, backend, effort, model, skills) and pre-fill every field below.

**Intent + title** — short free-text describing what you want the Automaton to accomplish. Title is optional; auto-derived from intent if blank.

**Inferred** — type (software / research / operational / personal) and workspace (project profile or directory). The wizard pre-fills based on intent text; click any chip / pick a profile to override.

**Execution** — backend + effort. Backend dropdown shows only backends with a configured key/endpoint; effort dropdown shows only the values the chosen backend supports (Ollama has no effort, claude-code has quick/normal/thorough).

**Advanced (collapsed)** — guided mode (per-step approval), enable scan/rules, story-level approval. Skill registries are picked from your synced skills (Settings → Automate → Skill Registries).

### Automaton detail

4-tab layout reached by clicking any Automaton row.

- **Overview** — PRD spec + current status + persistent toolbar (Edit Spec, Settings, Request Revision, Clone to Template, Delete).
- **Stories** — per-story state + Edit / Profile / Files / Approve / Reject. Each task under a story exposes Edit / LLM / Files.
- **Decisions** — every state-changing event for this Automaton; click any row to expand the raw `details` payload. Filter by source (operator / autonomous / scan / etc.).
- **Scan** — Run Scan kicks off a verifier sweep against the spec; shows pass/fail across SAST / secrets / deps / LLM grader. History persists.

The header strip carries Status badge + Settings (`openPRDSettingsModal` — type, backend, effort, model, skills, guided mode), Request Revision, Clone to Template, Delete.

**See also:**
[howto/autonomous-planning](howto/autonomous-planning.md) ·
[howto/autonomous-review-approve](howto/autonomous-review-approve.md) ·
[howto/automata-orchestrator](howto/automata-orchestrator.md) ·
[howto/algorithm-mode](howto/algorithm-mode.md) ·
[howto/evals](howto/evals.md) ·
[howto/council-mode](howto/council-mode.md) ·
[howto/skills-sync](howto/skills-sync.md)

---

## Dashboard

Mission control for your entire fleet — a live, full-screen view of every running session, active Automata, and system health indicators. Requires `autonomous.enabled: true` in `datawatch.yaml`; the nav button is hidden otherwise.

The layout is fully customisable: drag cards to reorder, resize with the width/height handle. Layout persists server-side at `GET/PUT /api/dashboard/layout` so it survives browser refreshes and re-logins.

### Session constellation

Force-directed SVG graph where each node is a session. Node colour reflects state (green = running, amber = waiting, grey = done). A pulsing ring indicates activity; a guardrail ring shows hook health from the session's status board.

Click any node to open the **expand panel** for that session.

### EKG waveform

Scrolling canvas trace driven by every incoming `hook_update` WebSocket event. Spikes decay over time, giving a visual heartbeat of fleet activity. Flat line = quiet; busy fleet = rhythmic spikes.

### Sprint pipeline

Shown when Automata are running. Horizontal stage bar with story nodes, gate rings (pass/fail from guardrail verdicts), and stage colours matching story status. Lets you see at a glance how far along the active sprint is and where blockers have appeared.

### Expand panel

Three-column overlay opened by clicking a constellation node or a session card's `⊞` button:
- **Left sidebar** — live task tree (reuses the telemetry task tree renderer).
- **Main area** — session status board: hook health ring, current focus, test counts, git state.
- **Right rail** — guardrail verdicts from the session's telemetry.

**Additional card types** available via Edit → Add Card: event feed, sessions sparklines, 6h Gantt, 30-day heatmap, guardrail profiles view, multi-session EKG overlay, smoke run progress.

**See also:**
[howto/dashboard.md](howto/dashboard.md) ·
[howto/session-telemetry.md](howto/session-telemetry.md) ·
[howto/claude-hooks.md](howto/claude-hooks.md)

---

## Observer

The Observer view aggregates everything the daemon knows about itself + its peers — process envelopes, federated stats, audit trail, knowledge graph, plugin status, daemon log. One scrollable view, one place to look when something feels off.

### Inactive backends

The status of every comm channel that's currently disabled or disconnected (Discord, DNS, Email, etc.). Click any row to open the matching Settings → Comms configuration card.

### Federated peers

Other datawatch instances pushing observer / stats data into this one. Each peer is a row with:

- **Health dot** — green (push <15 s ago), amber (15–60 s), red (stale >60 s or never pushed).
- **Name + shape** (Agent / Standalone / Cluster) + version.
- **Last push** age.
- **📊** — last snapshot drill-down.
- **×** — remove peer (rotates token; peer auto-re-registers if it's still alive).

When ANY peer goes stale, the gear icon in the bottom nav shows a numeric badge. Click the badge to land on this card with the offending peer flashed.

**Peer health alerts (v8.9.25):** a background goroutine polls each registered peer's push timestamps every 30 s and fires a system alert on every state transition — `online → stale`, `stale → online`, or `peer → unreachable`. Alerts appear in the Alerts tab with `source: system` and the peer name so you can correlate observer gaps with network or daemon restarts on the remote host. Alert thresholds match the health-dot thresholds: stale > 60 s, unreachable on first push-timeout after a previously-healthy period.

### Process envelopes

Per-process aggregation by attribution kind: `session:`, `backend:`, `container:`, `system`. Snapshot of CPU / RSS / threads / FDs / network / GPU per envelope. Refreshes every 5 s. Click an envelope to drill into its constituent processes.

### eBPF per-process net

Kernel-traced TCP socket activity per process (when eBPF is available — kernel ≥ 5.8 + cap_bpf + cap_sys_resource). Off → see Settings → About → eBPF status row. Each row is a (process, remote endpoint, byte counts, age) tuple.

### Installed plugins

Quick list of which plugins are loaded right now + their declared verbs / commands / tools. For management see Settings → Plugins → Plugin Manager.

### Global cooldown

Datawatch's notification rate-limiter. After N notifications in a short window the daemon enters cooldown to avoid pager-storming the operator. Settings: window size, max-per-window, cooldown duration. Card shows the current cooldown state + when it'll clear.

### Session analytics

Per-session counters across the daemon's lifetime: messages in / out, tokens, cost, average response time. Useful for cost auditing and identifying chatty sessions. Default sort: cost descending.

### Audit log

Every operator action (config change, session start/stop, secret read, etc.) recorded with actor / action / details / timestamp. Default view shows the last 5 entries; bump the limit dropdown for more (20 / 50 / 100). Filter by actor or action substring.

### Knowledge graph

Browse entity-relationship triples from the episodic memory. Each row is a `(subject, predicate, object, validity_window)`. Filter by subject or predicate; click a row to expand context.

### Daemon log

Tail of `~/.datawatch/daemon.log`. For deeper investigation, tail the file directly.

**See also:**
[howto/federated-observer](howto/federated-observer.md) ·
[howto/cross-agent-memory](howto/cross-agent-memory.md) ·
[howto/daemon-operations](howto/daemon-operations.md) ·
[architecture-overview](architecture-overview.md)

---

## Settings

### Settings — General

The daily-driver knobs.

- **Operator identity** — wake-up L0 layer self-description loaded from `~/.datawatch/identity.yaml`. Auto-injected into every spawned session so the LLM stays anchored to your role / north-star goals / current projects / values / context. Edit via inline form, the 🤖 wizard on the Automata page, or `datawatch identity {get,set,configure,edit}`. REST: `GET/PUT/PATCH/POST /api/identity` (POST is an alias for PATCH, added in v8.2.0 for mobile compat).
- **Session templates** — named bundles of (backend, effort, model, profile, skills) saved as `~/.datawatch/session-templates/<name>.yaml`. Used when starting new sessions to skip the picker.
- **Device aliases** — friendly names for the device IDs in your federation. Cosmetic; helps observer rows / audit log read more cleanly.
- **Backend artifact lifecycle** — per-backend cleanup policy (e.g. claude `.mcp.json` removal post-session, opencode workspace teardown). Defaults are sensible; only touch if you see leftover artifacts.
- **Secrets store** — credentials, tokens, environment values. Native AES-256-GCM at `~/.datawatch/secrets.db` plus optional KeePass, 1Password, and HashiCorp Vault / OpenBao (KV v2, static-token auth) backends. `${secret:name}` references in YAML/plugins/spawn-time env injection. Per-secret tags + scope. Audit-logged on every read. Vault status card shows reachability + last request ID; nav badge turns red when Vault is active but unreachable.

**Badge/chip multi-select input (v8.2.0)** — all fields that previously accepted raw comma-separated text (tags, capabilities, models, skills, shared-with lists) now use a chip-based badge input: click to add a chip, × to remove, drag-to-reorder for ordered fields (e.g. LLM fallback chain). When the field has a defined set of known values (e.g. federation capability group names), a typeahead dropdown appears. Underlying value is still a comma-separated string; no schema change.
- **Docs Search (Docs-as-MCP-Interface)** — every doc, howto, and plan is searchable through a hybrid index (vector primary + keyword fallback). The same surface drives docs read, how-to listing, and plan-then-execute: a curated how-to declares its MCP-call sequence in front-matter; the operator approves once and an agent runs the steps. Per-step risk gate available for write operations. Skills + plugins must be opted-in before their docs land in the index. See [`howto/docs-as-mcp.md`](howto/docs-as-mcp.md).
- **Federated Observer (findability)** — quick-link to the Observer view (where shape A/B/C config + Federated Peers card + per-peer stats live). The card itself only links; the full observer surface is the Observer view + REST/MCP/CLI/comm parity.

### Settings — Plugins

#### Plugin Manager

Installed plugins listed with their declared surface — comm verbs (chat commands), CLI subcommands, MCP tools, mobile-app cards. Toggle enable/disable; reload re-runs the manifest. Plugins live as folders under `~/.datawatch/plugins/<name>/` with a `manifest.yaml`. Subprocess + native plugin runtimes both supported.

### Settings — Comms

#### Authentication

Bearer token controls. The **Browser token** field is the credential this PWA tab presents on every API call (stored in localStorage). The **Server bearer token** row shows whether the daemon is enforcing token auth and lets you rotate it. CA certificate download buttons retrieve the daemon's auto-generated TLS root so you can trust it on a remote device.

#### Remote Servers

Manage the list of remote datawatch instances this PWA can connect to. Adding a server lets you pivot between hosts without changing the browser URL and without exposing remote bearer tokens to the browser — the local daemon proxies all requests.

**Server list** — each row shows name, URL, enabled toggle, Test button (probes `/api/health` on the remote), Edit button, and Delete. YAML-seeded servers appear with a **Builtin** badge and cannot be deleted from the UI; remove them from `datawatch.yaml` instead.

**Add / Edit form** — fields: **Name** (short slug used in picker chips, e.g. `nas`), **URL** (base URL including port), **Bearer token** (stored server-side, masked in UI), **Enabled** toggle.

**Federated peer + CBAC** — enable the **Federated peer** toggle to let this server authenticate to the MCP SSE endpoint (`/api/mcp/sse`) using its bearer token. Once federated, the **Capabilities** field controls what that peer may do — enter a comma-separated list of builtin group names or individual `surface:action` strings:

| Builtin group | What it grants |
|---|---|
| `read-only` | List/read across all surfaces |
| `session-viewer` | sessions:list + sessions:read |
| `session-operator` | Full session + agent lifecycle |
| `config-reader` | config:read + docs:read |
| `config-admin` | config:read + config:write |
| `federation-peer` | Health + sessions + alerts + federation list |
| `full-control` | All capabilities |

Individual caps follow `surface:action` — e.g. `sessions:list`, `sessions:write`, `sessions:kill`, `config:write`, `federation:list`. Custom groups can also be referenced by name. See [Federated Access Controls](#federated-access-controls) for the full surface-action reference and how to create custom groups.

**Per-tab picker** — once servers are registered, every main view (Sessions, Alerts, Automata, Observer, Dashboard) shows a chip bar at the top:
- **All** — aggregated fetch from every server; returns items tagged with their `server` origin.
- **Local** — only this daemon's data (default).
- **\<name\>** — proxy mode; REST and WebSocket calls route through `/api/proxy/{name}/...` on the local daemon.

**Aggregated endpoints** used by the All chip:
- `GET /api/sessions/aggregated` — sessions from all servers
- `GET /api/alerts/aggregated` — alerts from all servers
- `GET /api/autonomous/prds/aggregated` — Automata from all servers

**Relationship to Federated Observer:** multi-server (active query, per-tab switching) and Federated Observer (passive push stats) are complementary. You can register a server here for UI switching AND configure it as a federated peer for process/GPU/network telemetry — they use different auth tokens and different push/pull directions.

**See also:** [`howto/multi-servers.md`](howto/multi-servers.md) · [Federated Access Controls](#federated-access-controls)

#### Federated Access Controls

Capability-based access control (CBAC) for federation peers — remote datawatch instances that authenticate to the MCP SSE endpoint (`/api/mcp/sse`) using a bearer token. Every action taken by a peer is gated against the capabilities you grant it.

**Where to configure** — three surfaces, all parity-complete (REST + MCP + CLI + comm + PWA):
- Settings → Comms → Remote Servers form (Federated peer toggle + Capabilities field)
- Observer → Federation Peers card (Add Peer form, capability group field)
- CLI: `datawatch federation peer add/update --capabilities <group-or-cap>`

**Builtin capability groups** (safe defaults for common roles):

| Group | What it grants |
|---|---|
| `federation-peer` | Health + sessions/agents list-read-input + alerts + federation:list/read — safe default for new peers |
| `session-viewer` | sessions:list, sessions:read, agents:list, agents:read |
| `session-operator` | Full session + agent lifecycle (write, kill, input, pipelines) |
| `read-only` | All :read/:list across every surface |
| `config-reader` | config:read, docs:read |
| `config-admin` | config:read + config:write |
| `inference-admin` | llms:* + compute:* |
| `analytics-viewer` | analytics:read, dashboard:read, audit:read |
| `autonomous-operator` | autonomous:list/read/write/run |
| `council-operator` | council:list/read/run |
| `comm-bridge` | sessions:list/read/input + comm:read/write + alerts |
| `full-control` | All 50 capabilities |

**Individual `surface:action` caps** — 50 across 18 surfaces: `sessions:list/read/write/kill/input`, `agents:list/read/spawn/terminate`, `observers:list/read/write`, `llms:list/read/write`, `compute:list/read/write`, `analytics:read`, `health:read`, `config:read/write`, `secrets:list/read/write`, `pipelines:list/read/start/cancel`, `autonomous:list/read/write/run`, `council:list/read/run`, `federation:list/read/write`, `docs:read`, `audit:read`, `comm:read/write`, `alerts:list/read`, `dashboard:read/write`.

**Custom groups** — create reusable named groups (Settings → Comms → Communication Configuration, or `datawatch federation group add <name> --caps "..."`) and reference them by name in the Capabilities field.

**Enforcement points** — see [`howto/federation-cbac.md`](howto/federation-cbac.md) for the full capability-gate table and verification examples.

#### Communication Configuration

Per-channel registries: Signal, Telegram, Discord, Slack, Matrix, Twilio, GitHub webhooks, Generic webhooks, DNS channel, imap-mcp. Each row exposes connect/disconnect, status, and per-channel settings (e.g. Signal device link QR, Telegram bot token, Slack workspace OAuth). Channels in red are inactive; tap to reconnect.

#### imap-mcp email command channel (v8.9.23)

The `imap_mcp` backend bridges datawatch to a running [imap-mcp](https://github.com/dmz006/imap-mcp) instance, letting operators send commands to datawatch via email. Trust enforcement (allowlist / DKIM / DMARC / HMAC replay-nonce) lives entirely in imap-mcp; datawatch acts only on `inbound.command` events that have already passed imap-mcp's trust gates.

**How it works:**

- **Receive:** subscribes to `GET /api/events` on the imap-mcp SSE endpoint. Ignores all event types except `inbound.command`.
- **Send:** session replies are delivered via `POST /api/accounts/{account}/messages/send` on the imap-mcp REST endpoint.

**Config fields** (under `imap_mcp:` in `datawatch.yaml`):

| Field | Description |
|---|---|
| `enabled` | `true` to activate the channel |
| `url` | Base URL of the imap-mcp instance (e.g. `http://localhost:8025`) |
| `account` | imap-mcp account name to use for sending replies |
| `subject_prefix` | Optional prefix prepended to outgoing email subjects (e.g. `[datawatch]`) |

**REST:** the channel appears in `GET /api/channels` with kind `imap_mcp`. Toggle via standard channel enable/disable endpoints.

**Requires:** imap-mcp v0.2.0 or later (provides both `GET /api/events` SSE and `POST /api/accounts/{account}/messages/send`). See GH#127.

#### Proxy Resilience

Connection pooling + circuit breaker policies for outbound HTTP from the daemon (LLM backends, webhooks, observer pushes). Settings: pool size, retry budget, breaker open threshold, breaker reset window. Defaults are conservative; tune up only if you're hitting rate limits at a layer datawatch can't auto-recover from.

#### Routing Rules

Comm-channel → backend routing. Each rule is a (sender / channel / pattern) → (backend / profile / model / effort) mapping. Used by the channel adapters to pick which LLM handles an inbound message. Empty list = all messages route to the default backend. Click a rule to edit; reorder by drag.

#### Channel Routing (v8.3.0)

Federation-level channel-address routing. Rules map an inbound channel address (e.g. a Telegram group ID, Signal number, Matrix room) to a specific federation peer with an optional automata type and default project directory. Config stored in `~/.datawatch/channel_routing.json`.

**Card:** Settings → Comms → Channel Routing. Lists all rules; each row shows `channel_pattern`, `peer_name`, `automata_type`. Add Rule form with the same fields. Rules are evaluated in order; the first match wins.

**Rule fields:**
- `channel_pattern` — required; the channel address to match (e.g. `telegram:group:-1001234567890`)
- `peer_name` — required; the federation peer to route to
- `automata_type` — optional; Automata type to use when spawning PRDs from this peer
- `default_project_dir` — optional; default project directory for sessions created by this peer

**REST:** `GET /api/channel/routing` (requires `comm:read`) · `PUT /api/channel/routing` (requires `comm:write`)

**CLI:** `datawatch channel routing list | add`

**Federated peer `channel_identity` field:** a `[]string` on the `multiserver.Entry` struct listing which channel addresses belong to this peer. Set in the Federation peer add/update form, via `datawatch federation peer add --channel-identity ...`, or via MCP `federation_peer_add`.

**`owner_peer` on sessions and PRDs:** when a session or PRD is created via a channel routing match, `owner_peer` is set to the matched peer's name. Surfaced in `GET /api/sessions` and `GET /api/autonomous/prds`.

See [`howto/channel-routing.md`](howto/channel-routing.md).

#### Vision System

Configurable image-description backend that extends the comms router, MCP tools, skills, and Council Mode to understand image/photo attachments.

**Config (`datawatch.yaml`):**

```yaml
vision:
  enabled: false
  backend: ollama          # "ollama" | "openai" | "openai_compat"
  endpoint: http://localhost:11434
  api_key: ""              # required for upstream OpenAI
  model: llava             # must be vision-capable
  default_prompt: ""       # defaults to "Describe this image concisely."
  max_image_bytes: 0       # 0 = 10 MB limit
```

**How it integrates:**

| Surface | Behaviour |
|---|---|
| **Comms router** | Image/photo attachments from Signal/Telegram/etc. are read, described, and prepended to the message text as `[image: <desc>]` before command parsing. Works with all comms commands. |
| **`remember: [image]`** | Sending `remember:` alongside an image attachment stores the description in episodic memory: `remember: [image: <desc>] <caption>`. |
| **`POST /api/vision/describe`** | REST endpoint for on-demand image description. Multipart `image` field + optional `prompt`. |
| **MCP `vision_describe`** | `vision_describe(image_path, prompt)` tool — agents call it from within a session. |
| **`start_session` / `send_input` MCP** | Accept `image_paths: string[]`; descriptions are prepended to the task/text before delivery. |
| **`POST /api/council/run`** | Accepts optional `image_path`; image is described once and injected into every persona's proposal. |
| **Skill manifest `accepts_images`** | Skills declare `accepts_images: true` in SKILL.md frontmatter to signal they can receive image context. |

**Supported models:** llava, llava-phi3, llava-llama3, bakllava, moondream, minicpm-v (ollama); gpt-4o, gpt-4-vision-preview (openai); any OpenAI-compat endpoint.

**REST:** `GET /api/config` + `PUT /api/config` expose `vision.*` keys. `POST /api/vision/describe` (multipart). All routes are bearer-authenticated.

### Settings — Compute

> **v7 rename:** The "LLM" tab was renamed to "Compute" in v7.0.0 and the "Agents" tab was eliminated. All content from both tabs now lives here. If you're on a saved `cs_settings_tab=llm` or `cs_settings_tab=agents` bookmark, the PWA auto-redirects to `compute`.

#### LLM Registry

The v7 named-LLM registry. Each entry gives a friendly name to an LLM backend + model + compute node combination so you can reference it by name throughout the system (session start, Automata planning, pipeline tasks).

**Card columns:** name, kind (ollama / openwebui / claude-code / etc.), compute nodes (failover order), enabled toggle, Test button.

**Add / Edit form fields:**
- **Name** — short kebab-case slug (e.g. `my-gpu-ollama`); immutable after save
- **Kind** — adapter type; determines how the daemon routes inference calls
- **Compute Nodes** — multi-select from the Compute Nodes registry; first entry is primary, rest are failover in order
- **Enabled Models** — per-node model list with optional Auto-add toggle (auto-appends newly-discovered models)
- **Enabled** toggle — disabled LLMs are rejected at session-start and excluded from pickers

**Delete guard:** if active sessions or Automata are using the LLM, delete is blocked. The modal lists offenders and offers **Reassign + Delete** (move all active bindings to another LLM then delete) or **Force Delete** (cascade-cancel all bindings first).

**In-use view:** expandable section per LLM showing active bindings (sessions/Automata/personas) with pagination and substring filter.

**CLI:** `datawatch llm list | get | add | update | delete | enable | disable | test | models list|add|remove | in-use | refresh-models | reassign | force-delete`

**See also:** [`howto/llm-registry.md`](howto/llm-registry.md)

#### LLM Configuration (legacy)

Per-backend enable/disable + setup for the original adapter system. Each backend card carries its own setup wizard (e.g. claude-code asks for `~/.claude.json`; ollama asks for the host URL). For new deployments, use the LLM Registry above.

#### Cost Rates (USD / 1K tokens)

Per-backend per-model input + output token rates the daemon multiplies session token counts by to compute `EstCostUSD`. Adjust if a backend's billing changed or you negotiated a custom rate. Values default to public list pricing on first run.

#### Detection filters

Prompt patterns + completion patterns the daemon scans tmux output for. **Prompt patterns** trigger `WaitingInput` when matched (e.g. `❯`, `$ `). **Completion patterns** trigger `Complete` (e.g. `DATAWATCH_COMPLETE:`). Per-deployment overrides; the global defaults work for most setups.

#### Compute Nodes

The Compute Node registry — hardware or remote endpoints that run LLM inference. Each node is one entry; LLMs reference nodes by name for failover routing.

**Supported kinds (LLM API protocol):**
- `ollama` — native Ollama HTTP API (local or remote)
- `openai-compat` — OpenAI-compatible `/v1` endpoint (OpenWebUI, vLLM, LMStudio, OpenAI itself, etc.)
- `gemini-api` — Google Generative Language v1beta API (`POST /v1beta/models/<model>:generateContent?key=<api_key>`)
- `opencode-api` — opencode `/v1/chat/completions` endpoint

**Routing mode (v8.0 — HOW to reach the node, orthogonal to kind):**

| `routing` | Description | Required sub-config |
|---|---|---|
| `direct` | Use `address` field directly (default) | — |
| `docker-network` | Daemon manages container lifecycle via Docker CLI | `routing_docker_network` |
| `datawatch-proxy` | Forward inference through a federated peer's `/api/proxy/llm/<name>` | `routing_datawatch_proxy` |

**`routing_docker_network` sub-config fields:**

| Field | Type | Default | Description |
|---|---|---|---|
| `image` | string | *required* | Docker image, e.g. `ollama/ollama:latest` |
| `network_name` | string | `datawatch-llm` | Docker network name |
| `port` | int | `11434` | Container port exposed to the network |
| `container_name` | string | *auto* | Optional explicit container name |
| `docker_endpoint` | string | system default | Docker socket/endpoint URL |
| `auto_start` | bool | `false` | Start container on first probe if not running |
| `auto_pull` | bool | `false` | Pull image if missing before start |
| `env` | `[]string` | — | Env vars in `KEY=VALUE` form |

**`routing_datawatch_proxy` sub-config fields:**

| Field | Type | Description |
|---|---|---|
| `peer` | string | Registered server name (from Remote Servers card) |
| `remote_llm_name` | string | LLM name on the peer to invoke |
| `timeout_seconds` | int | Per-request timeout (default 30) |

**Card columns:** name, kind, routing badge, address, GPU/RAM summary, enabled sliding switch, Edit / Test / Delete buttons.

**Edit form sections:**
- **Connection** — kind, address URL (hidden for docker-network/datawatch-proxy routing)
- **Routing** — direct / docker-network / datawatch-proxy with conditional sub-fields
- **Hardware** — OS, arch, GPU vendor/model/count, VRAM, RAM, CPU cores. The daemon auto-suggests "Computed max" concurrent requests based on VRAM × GPU count.
- **Capacity** — declared max concurrent requests (operator override)
- **Observer peer** — bind this node to a registered federated observer peer for live process/GPU stats correlation

**Save-time probe:** the daemon runs a connectivity check on every create/update. Use `?probe=skip` to bypass for emergency saves when the node is temporarily unreachable.

**Ollama marketplace:** click "Browse marketplace" on an Ollama-kind node to open the embedded catalog (llama3.1, qwen3, gemma3, deepseek-r1, etc.) with size/VRAM requirements and one-click background pull.

**Migration banner:** shown when any node still uses a deprecated kind (`local`, `remote`, `ssh`, `docker`, `k8s`). Click to re-pick a supported kind per node.

**CLI (v8.0):**
```
datawatch compute node add <name> kind=ollama routing=docker-network image=ollama/ollama:latest network=datawatch-llm port=11434
datawatch compute node add <name> kind=ollama routing=datawatch-proxy peer=dc2 remote_llm=llama3 timeout=30
```
Full verb list: `list | get | add | update | delete | detail | health | pull-model | remove-model | attach-observer | detach-observer | observer-free | observer-by-node | federation-meta-peers | migrate`

**`datawatch compute migrate` (v8.9.25):** migrates nodes still using deprecated kinds (`local`, `remote`, `ssh`, `docker`, `k8s`) to supported ones (`ollama`, `openai-compat`). Running `migrate` is required before `add` or `update` will accept those nodes from the CLI. The PWA shows a migration banner when any deprecated-kind node is detected; the CLI equivalent is `datawatch compute migrate [--dry-run]`. Internally calls `POST /api/compute/nodes/{name}/migrate` with a suggested target kind.

**MCP tools (v8.0):** `compute_node_add` and `compute_node_update` accept `routing`, `routing_docker_network_json`, and `routing_datawatch_proxy_json` string parameters.

**7-surface parity (v8.0):**

| Surface | routing | docker-network | datawatch-proxy | gemini-api | opencode-api |
|---|---|---|---|---|---|
| YAML | ✓ | ✓ | ✓ | ✓ | ✓ |
| REST | ✓ | ✓ | ✓ | ✓ | ✓ |
| MCP | ✓ | ✓ | ✓ | ✓ | ✓ |
| CLI | ✓ | ✓ | ✓ | ✓ | ✓ |
| Comm | ✓ (via `rest PUT`) | ✓ | ✓ | ✓ | ✓ |
| PWA | ✓ | ✓ | ✓ | ✓ | ✓ |
| Mobile | file issue | file issue | file issue | file issue | file issue |

**See also:** [`howto/compute-routing.md`](howto/compute-routing.md) · [`howto/compute-nodes.md`](howto/compute-nodes.md) · [`howto/v7-compute-migration.md`](howto/v7-compute-migration.md) · [`howto/ollama-marketplace.md`](howto/ollama-marketplace.md)

#### Project Profiles

Named bundles describing a project workspace: directory, git policy, pre/post hooks, default backend, default skills. Used by Automata's "Workspace" picker. Edit YAML at `~/.datawatch/profiles/projects/<name>.yaml`.

#### Cluster Profiles

Named Kubernetes contexts (kubeconfig + namespace + node selector). Used when spawning container workers in a remote cluster. Operator sets credentials once; sessions reference by name.

#### Container Workers

The agent worker fleet — Docker locally OR Kubernetes-spawned per-session pods. Settings: image base (distroless default), PQC bootstrap key, pull policy, resource limits. Workers join the Tailscale mesh on spawn for private network.

#### Tailscale Mesh Status / Configuration

Headscale-first (self-hosted), commercial Tailscale supported. Status card shows current node + advertised routes; Configuration accepts pre-auth keys or OAuth device flow. ACL Generator builds a Tailscale ACL from current node tags + agent fleet membership.

#### Push Notifications (v8.2.0, BL346 lifecycle events v8.10.3)

UnifiedPush + ntfy registration and fan-out. The card shows:

- **Registration status** — whether this device/browser has a registered push endpoint.
- **Register** button — calls the browser Push API, then POSTs to `POST /api/push/register` with the subscription `{endpoint, keys: {p256dh, auth}}`. Returns a registration `id`.
- **Test** button — fires `POST /api/push/notify` to send a test message to all registered endpoints.
- **Unregister** button — calls `DELETE /api/push/unregister` with the stored `id`.

CLI: `datawatch push list | test [--id <id>] [--message <m>] | unregister [--id <id>|--endpoint <url>]`.

UnifiedPush auto-discovery: `GET /.well-known/unifiedpush` returns `{"version":1,"unifiedpush":{"gateway":"/api/push/notify"}}`.

**BL346 — `session_state_changed` lifecycle events (v8.10.3):** The daemon publishes a push event to topic `session-<fullID>` on every non-oscillation, non-waiting-input state transition. Payload `extras` contains `{type: "session_state_changed", old_state, new_state, task, short_summary}`. Allows mobile apps and subscribers to display contextual notifications (e.g. "Session foo — complete").

See [`howto/push-setup.md`](howto/push-setup.md) · [`howto/push-notifications.md`](howto/push-notifications.md).

#### File Service (v8.3.0)

Structured file storage on the daemon, organized into `peers/` and `discussions/` subdirectories under a configurable root. Accessible from all surfaces and federation-gated via `config:read` / `config:write`.

**Card:** Settings → General → File Service. Shows storage root path, peer count, discussion count, and total disk usage (from `GET /api/files/meta`). Upload button for adding files via the browser.

**Config field:** `file_service_root` under `session:` in `datawatch.yaml`. Priority: `file_service_root` → `root_path` → user home directory.

**REST endpoints:**
- `POST /api/files` (multipart/form-data with `file` + `path` fields) — upload; requires `config:write`
- `DELETE /api/files` (JSON `{path}`) — delete; requires `config:write`
- `GET /api/files/peers/{name}` — list `<root>/peers/<name>/`; requires `config:read`
- `GET /api/files/discussions/{id}` — list `<root>/discussions/<id>/`; requires `config:read`
- `GET /api/files/meta` — storage overview; requires `config:read`

**MCP tools:** `files_upload`, `files_delete`, `files_meta`

**CLI:** `datawatch files list | upload | delete | peer`

Path traversal (`..` in any path argument) is rejected with 400 on all write endpoints.

See [`howto/file-service.md`](howto/file-service.md).

#### Discussion Scopes (v8.4.0)

Per-discussion memory namespaces that are durable, federated, and conflict-aware. Each discussion scope is keyed by an operator-chosen discussion ID and backed by a write-ahead log (WAL) at `~/.datawatch/discussions/<id>/wal.jsonl`.

**Scope name:** `ScopeDiscussion`

**Resolution tuple:** `(projectDir="", role="discussion/<id>", sessionID="")`

**Card:** Settings → General → Discussion Scopes. Lists all known discussion IDs; provides a New Discussion form and per-discussion Recall + Participants controls.

**Sync behavior:** When participant peers are set on a discussion, every successful write triggers an async fan-out push to each participant via `/api/push/<discussion-id>`. Each WAL entry carries `origin_peer` and `origin_wal_seq` fields; a peer that receives a sync push skips re-fanning any entry it originally produced (loop prevention).

**Throttle:** 60 write operations per minute per peer, enforced via a per-peer token bucket. Exceeding the limit returns HTTP 429. Local daemon writes are not throttled.

**Conflict model:** Concurrent writes from two or more peers can produce conflicting WAL entries. Conflicts are detected on receipt of a sync push and exposed at `GET /api/memory/discussion/{id}/conflicts`. Resolve by calling `POST /api/memory/discussion/{id}/conflicts/resolve` with `winner_seq` and `loser_seq`; the loser entry is tombstoned.

**REST endpoints:**
- `GET /api/memory/discussion` — list all discussion IDs; requires `comm:read`
- `GET /api/memory/discussion/{id}` — read entries; requires `comm:read`
- `POST /api/memory/discussion/{id}` — write entry; requires `comm:write`; throttled at 60/min per peer
- `DELETE /api/memory/discussion/{id}` — remove all entries and WAL; requires `comm:write`
- `GET /api/memory/discussion/{id}/wal` — read raw WAL; requires `comm:read`
- `GET /api/memory/discussion/{id}/participants` — list participant peers; requires `comm:read`
- `PUT /api/memory/discussion/{id}/participants` — set participant peer list; requires `comm:write`
- `GET /api/memory/discussion/{id}/conflicts` — list detected conflicts; requires `comm:read`
- `POST /api/memory/discussion/{id}/conflicts/resolve` — mark winning entry; requires `comm:write`

**MCP tools:** `memory_discussion_write`, `memory_discussion_recall`, `memory_discussion_wal`, `memory_discussion_participants`

**CLI:** `datawatch memory discussion list | write | recall | wal | participants`

Path traversal (`..` in a discussion ID) is rejected with 400.

See [`howto/discussion-scopes.md`](howto/discussion-scopes.md).

#### Notifications

Per-channel preference for daemon-emitted events: state changes, needs-input, rate-limit hits, autonomous step approvals. Off by default for chatty events; on for needs-input.

### Settings — Automate

Automaton-related cards.

- **Orchestrator** — multi-graph PRD-DAG executor. Approve / hold / cancel automated runs from this card. The **Dashboard nav button** in the bottom navigation is only shown when `autonomous.enabled: true` in `datawatch.yaml` — keeping the nav clean for operators not using Automata. **Verifier diff grounding** (v8.16.0): after each worker session completes, the verifier receives the actual `git diff` of changes alongside the task spec, so verification is grounded in code rather than spec text alone. Configurable via `autonomous.verifier_diff_max_bytes` (0 = 8 KB cap). When the project directory has no git history or the task was dispatched to a remote cluster node, the verifier falls back to spec-only verification without error.
- **Identity / Telos** — same content as Settings → General → Operator identity, surfaced here too because Telos drives autonomous prioritization.
- **Algorithm Mode** — PAI's 7-phase per-session harness (Observe → Orient → Decide → Act → Measure → Learn → Improve). This card lists active sessions, current phase, captured output per gate. CLI: `datawatch algorithm {start,advance,edit,abort,reset,measure}`.
- **Evals** — rubric-based grading suites. Default suite types: `string_match`, `regex_match`, `binary_test`, `llm_rubric`. Run a suite from this card; results land in `~/.datawatch/evals/runs/`. Used by Algorithm Mode's Measure phase if configured.
- **Council Mode** — multi-persona debate. 12 default personas (security-skeptic, ux-advocate, perf-hawk, simplicity-advocate, ops-realist, contrarian, platform-engineer, network-engineer, data-architect, privacy, hacker, app-hacker). Each run is **async** by default: `POST /api/council/run` returns `{id, events_path}` immediately; subscribe to `GET /api/council/runs/{id}/events` for SSE streaming as each persona responds round-by-round. The PWA shows collapsible live-watch cards per run. Cancel with `POST /api/council/runs/{id}/cancel`. Milestone messages (run started / round complete / consensus reached) push to all configured comm channels; `council.comm_firehose: true` also sends per-persona response previews. Config: `council.llm_ref` (which LLM to use), `council.max_parallel` (concurrent personas per round, default 2). **Image critique:** `POST /api/council/run` accepts optional `image_path` — the file is described by the vision service and the description is prepended to the proposal before all personas receive it (requires `vision.enabled: true`). Use case: submit an architecture diagram or UI mockup for multi-persona review. **AI persona wizard** (v6.22.3): the + Add Persona flow can draft a `system_prompt` via LLM — answer 5 interview questions; each answer has a Refine button; result is saved to `~/.datawatch/council/personas/<name>.yaml`. Re-interview any existing persona via the 🤖 button on its row. See [`howto/council-mode.md`](howto/council-mode.md).
- **Skill Registries** — git-backed PAI-format skill manifests. Connect a registry → browse → sync. Synced skills get copied into a session's `<projectDir>/.datawatch/skills/<name>/` at spawn time when listed in the session's Skills field.

**See also:**
[howto/identity-and-telos](howto/identity-and-telos.md) ·
[howto/algorithm-mode](howto/algorithm-mode.md) ·
[howto/evals](howto/evals.md) ·
[howto/council-mode](howto/council-mode.md) ·
[howto/skills-sync](howto/skills-sync.md) ·
[howto/profiles](howto/profiles.md) ·
[howto/secrets-manager](howto/secrets-manager.md) ·
[howto/comm-channels](howto/comm-channels.md) ·
[howto/tailscale-mesh](howto/tailscale-mesh.md)

### Settings — MCP

Datawatch acts as an MCP server (Model Context Protocol), exposing tools, resources, and prompts to any MCP-aware client (Claude Code, Claude Desktop, Cursor, etc.).

#### Session name resolution and permission_mode (v8.9.24)

MCP tools that accept a session identifier (`session_id`, `parent_id`, `caller_session_id`) now resolve **session names** in addition to hex IDs. Pass the human-readable session name (e.g. `"my-research"`) and the daemon looks up the matching active session. When multiple active sessions share the same name, the most recently started one wins.

**`start_session` — `permission_mode` parameter:**

The `start_session` MCP tool accepts a `permission_mode` string that sets the claude-code permission policy for the spawned session:

| Value | Effect |
|---|---|
| `auto` | claude-code default (same as omitting the flag) |
| `plan` | Read-only analysis; no file edits permitted |
| `acceptEdits` | Auto-accept file edits without prompting |
| `bypassPermissions` | Skip all permission prompts (use with caution) |

Use `permission_mode: "plan"` to spawn a session that can analyse code but not modify it — useful for autonomous review steps inside a PRD that should not write files.

**See also:** [`howto/mcp-tools.md`](howto/mcp-tools.md)

#### MCP Channel Bridge Diagnostics (BL362, v8.10.16)

When sessions fail to connect or MCP errors appear with no clear cause, this surface exposes the full diagnostic picture:

- **Bridge kind / path** — whether the daemon resolved the Go binary (`datawatch-channel`) or Node.js fallback, and its path on disk.
- **Per-session channel port** — every session's bridge registers its HTTP listen port via `POST /api/channel/ready`. The diagnostics endpoint lists those ports so you can see which bridge is on which port.
- **Live `/health` probe** — the daemon probes `GET http://127.0.0.1:PORT/health` against each session's registered bridge and reports `bridge_alive: true/false`. A dead bridge that still has a registered port means the process crashed after registering.
- **Remediation hints** — if a bridge never called ready (port=0), the hint points at `DATAWATCH_API_URL`/`DATAWATCH_TOKEN` misconfiguration. If a bridge is dead on a known port, the hint suggests restarting the session.
- **Startup stderr diagnostics** — the `datawatch-channel` binary itself now prints a config dump (`api_url`, `channel_port`, `session_id`, token set/not-set), a pre-flight `GET /api/health` probe, port-conflict identification (reads `/proc/net/tcp` + `/proc/<pid>/comm` to name the holding process), and a clearer `notifyReady` failure message on every launch.

**Surfaces:**
- `GET /api/channel/diagnostics` — JSON response with `{bridge_kind, bridge_path, global_port, sessions[], hints[]}`.
- `channel_diagnostics` MCP tool — same payload, usable from any IDE-side agent.
- `datawatch channel diagnostics [--json]` CLI — human-readable table of session→port→alive, plus hints.
- Chat command `channel diagnostics` — usable from Signal/Telegram/Slack/etc.
- PWA Settings → Monitor → Channel bridge diagnostics card — live per-session status with refresh button.

**Key env vars injected into bridge processes (documented in `docs/config-reference.yaml`):**

| Var | Default | Effect |
|-----|---------|--------|
| `DATAWATCH_CHANNEL_PORT` | 7433 (or `server.channel_port`) | HTTP listen port; 0 = auto-select. Port conflict → bridge exits with diagnostic. |
| `DATAWATCH_API_URL` | `http://localhost:8080` | Daemon callback URL; wrong value → tool discovery fails silently. |
| `DATAWATCH_TOKEN` | (server token) | Bearer auth; missing/wrong → 401 on ready + tool discovery. |
| `CLAUDE_SESSION_ID` | (session id) | Tags replies; auto-set by daemon. |

#### MCP Tools

Every datawatch capability — session management, memory, Automata, Council, evals, secrets, plugins — is available as an MCP tool. The tool catalogue is served at `GET /api/mcp/docs` (human-readable) and via the MCP `tools/list` protocol. See [`howto/mcp-tools.md`](howto/mcp-tools.md).

#### MCP Resources

Live daemon data served as readable MCP resources: sessions, Automata, alerts, memory entries, knowledge graph, observer stats. Resources update automatically; clients subscribe and receive push notifications. Resource URIs follow the pattern `datawatch:///<kind>/<id>` (e.g. `datawatch:///sessions/abc1`). Available via `GET /api/mcp/resources` and the MCP `resources/list` protocol.

#### MCP Prompts

Ten pre-built slash commands that inject live context before routing to the LLM:

| Prompt | Args | Context injected |
|--------|------|-----------------|
| `analyze-session` | `session_id` (opt) | session detail + history |
| `review-automaton` | `automaton_id` | Automaton spec + status |
| `triage-alert` | `alert_id` | alert + system stats |
| `morning-briefing` | `since` (opt) | sessions + alerts + memory + stats |
| `research-topic` | `topic` | memory + KG entities |
| `council-brief` | `council_id` | council run + personas |
| `session-summary` | `session_id` | session history |
| `diagnose-system` | — | stats + alerts + config |
| `explore-kg` | `entity` (opt) | KG entities + triples |
| `plan-sprint` | `context` (opt) | memory + version |

Access via: MCP `prompts/list` + `prompts/get` · `GET /api/mcp/prompts` · `datawatch mcp prompts list` · `!mcp prompts` in comm channels.

#### MCP Sampling

The daemon can request LLM completions from the connected Claude Code / Claude Desktop session via `sampling/createMessage`. Five built-in triggers (`alert_triage`, `anomaly_analysis`, `morning_briefing`, `council_deliberation`, `automaton_decision`) come with pre-built prompt templates that inject live daemon state. Custom prompts also supported. Results stored in a 50-entry ring buffer viewable in the **Sampling log** tab. Degrades gracefully when no MCP host is connected.

Config: `mcp.sampling.enabled`, `mcp.sampling.max_tokens`, `mcp.sampling.timeout_seconds`.

#### MCP Elicitation

The daemon can prompt the operator for structured input through the connected MCP host — without the operator leaving Claude Code. Three built-in schemas: `approval` (yes/no), `text_input` (free text), `choice` (pick one). Calls block until the operator responds or the timeout expires. Used by Automata approval gates, plugin confirmation dialogs, and autonomous decision prompts.

Config: `mcp.elicitation.enabled`, `mcp.elicitation.timeout_seconds`.

**See also:** [`howto/mcp-tools.md`](howto/mcp-tools.md) · [`howto/mcp-prompts.md`](howto/mcp-prompts.md) · [`howto/mcp-sampling.md`](howto/mcp-sampling.md) · [`howto/mcp-elicitation.md`](howto/mcp-elicitation.md)

### Settings — About

A short identity panel: this daemon's hostname + version, a link to the mobile companion app, an Orphaned Tmux Sessions maintenance row, and a single hyperlink to **System documentation & diagrams** which opens this manual in the in-app rendered viewer.

#### API

Inline links to `/api/docs` (Swagger UI), `/api/openapi.yaml` (raw OpenAPI spec), `/api/mcp/docs` (MCP tool catalogue). These are the operator-facing entry points to the daemon's REST + MCP surface — useful for scripting against datawatch from outside.

#### Mobile app pointer

GitHub link to `dmz006/datawatch-app` (the Compose Multiplatform companion). Play Store link will land here once the app is published.

#### Self-update (v8.9.21)

`datawatch update` (CLI) and `POST /api/update` (REST) install the latest prebuilt binary from the GitHub Releases goreleaser archive. The pipeline:

1. Queries the GitHub Releases API for the latest tag.
2. Downloads the goreleaser archive for the current `GOOS/GOARCH` (e.g. `datawatch_linux_amd64.tar.gz`).
3. Extracts the `datawatch` binary and atomically replaces the running binary.
4. Downloads and replaces the `datawatch-channel` sibling binary from the same archive.

A legacy bare-binary fallback is retained for pre-goreleaser releases.

**Version check only:** `GET /api/update/check` returns `{current_version, latest_version, update_available}` without installing anything. Use this to implement "check → confirm → install" workflows in scripts or the mobile app before committing to an install.

**Auto-update:** set `auto_update.enabled: true` and `auto_update.check_interval_hours` in `datawatch.yaml` to let the daemon check and install updates on a schedule.

**CLI:** `datawatch update [--check]` — `--check` is equivalent to the read-only REST endpoint.

#### Orphaned tmux sessions

Lists `cs-*` tmux sessions on this host that have no corresponding entry in the daemon's session store. Usually leftover from a crash or hard restart. Click a row to kill the orphan tmux session.

---

## Concepts & Glossary

Key terms used across the docs, API, and hook payloads.

**SessionTelemetry** — structured telemetry accumulated from hook
payloads for a session. Contains the current task, active tool and
file, sprint ancestry, task list with server-stamped timings, test
counts, a progress float, guardrail verdicts, a link to the parent
session, and a failure buffer. Retrieved via
`GET /api/sessions/{id}/telemetry` or MCP `telemetry_get`.
Ephemeral by default; durable with `persist_telemetry_on_stop`.

**sprint** — in the hook payload schema, `sprint` maps to a Story in
the Automata hierarchy: Automaton → Story (= sprint) → Task. The
`sprint` object carries `name`, `id`, `automata`, `automata_id`,
`task`, and `task_id` so telemetry can link back to the originating
Automaton story. The word "sprint" is used in hook payloads and state
files; "Story" is the UI label in the Automata view.

**task ancestry** — the chain of identifiers from a TelemetryTask
(`id`) up through the sprint (`task_id`, `id`) to the Automaton
(`automata_id`). The full ancestry appears in the `sprint` field of
the hook payload. Use `automata_id` with `autonomous_prd_get` to
navigate from a telemetry task back to the Automata view.

**Alert firing** — one historical record of an alert rule crossing its threshold. The last 100 firings are kept in memory (ring buffer) and accessible via `GET /api/alert-rules/firings` or `datawatch alert-rules firings`. Fields: `rule_id`, `fired_at`, `value`, `resolved_at`. Firings reset on daemon restart; they are not persisted to disk.

**Alert rule** — a named observer-metric threshold check persisted in `<data_dir>/alert-rules.yaml`. Evaluated every 30 s; fires a system alert or a scale_up/scale_down action when `condition.metric operator threshold` is true and the per-rule cooldown has elapsed. Supported metrics: `cpu_pct`, `mem_pct`, `gpu_pct`, `rss_bytes`, `net_rx_bps`, `net_tx_bps`. See `docs/howto/alert-rules.md`.

**Community registry** — the `dmz006/datawatch-community` GitHub repo. Pre-seeded as the first Skills + Plugins registry on every new installation. Contains categorized, community-contributed skills and plugins with mandatory `author`, `contributor_notes`, and `license` fields. Connect with `datawatch skills registry connect community`, then browse with `datawatch plugins browse-registry community`.

**Plugin install** — the ability to copy a plugin directory from a connected registry clone into the local plugins directory and reload it, via `datawatch plugins install <registry> <name>` or `POST /api/plugins/install`. The install resolves `<registry_clone_dir>/plugins/<name>/` → `<data_dir>/plugins/<name>/`, validates `manifest.yaml`, copies atomically, and calls the existing reload pipeline. No daemon restart required.

**failed_task_buf** — a per-session buffer of the last 5 hook events
received before any task transitioned to `failed`. Written into
`SessionTelemetry.FailedTaskBuf` on the failure transition. Useful
for post-mortem: shows what tools ran, what output was produced, and
what the session's state was immediately before the failure.

**LLM-optional memory recall (v8.13.3)** — when the embedding LLM (Ollama) is offline or returns an error, `Recall`, `RecallAll`, `RecallInNamespaces`, and `RetrieveContext` automatically fall back to LIKE-based keyword search against the SQLite store instead of returning an error. Fallback results carry `Similarity=0` to distinguish them from semantic results. `SaveOutputChunks` stores chunks without a vector (previously skipped them), keeping them text-searchable. `LazyReembed(batchSize int)` on `Retriever` back-fills embedding vectors for un-vectorized rows when the LLM becomes available again. The session summarizer returns `"Summary unavailable — LLM offline."` instead of propagating errors. `POST /api/ask` returns HTTP 503 + JSON `{"error":"LLM unavailable:…"}` on backend connectivity failures.

**TextSearchableBackend** — optional interface the SQLite `Store` implements (v8.13.3). Provides `SearchByText(projectDir, query, topK)`, `SearchAllByText(query, topK)`, `SearchInNamespacesByText(namespaces, query, topK)`, and `ListUnembedded(n)`. Used by `Retriever` as the keyword-search fallback when embedding is unavailable. The PG store does not yet implement it; callers check via type assertion before using.

**persist_telemetry_on_stop** — boolean config flag under `session:`
in `datawatch.yaml`. When `true`, the daemon calls
`flushTelemetryToMemory()` when a `Stop` or `SubagentStop` hook fires,
serializing the session's `SessionTelemetry` to episodic memory with a
compact summary. The entry is searchable via `memory_recall`. Default:
`false` (ephemeral).

**guardrail_verdict** — one result from a guardrail check, as reported
in the hook payload's `guardrail_verdicts[]` array. Fields: `guardrail`
(name of the check, e.g. `sast-scan`), `outcome` (`pass` | `warn` |
`block`), and optional `summary` string. Verdicts are replaced on each
event that carries `guardrail_verdicts[]` — they represent the most
recent check results, not a cumulative log. Also appears in the
orchestrator's `GET /api/orchestrator/verdicts` flat verdict log.

---

## Core feature reference matrix

Tracks which core features have how-to walkthroughs, plans, and architecture diagrams.

| Feature | How-to | Plan | Architecture / diagram |
|---|---|---|---|
| Sessions | [`howto/sessions-deep-dive.md`](howto/sessions-deep-dive.md) | covered in active backlog | [`architecture-overview.md`](architecture-overview.md) |
| Session Telemetry | [`howto/session-telemetry.md`](howto/session-telemetry.md) | ✓ | [`flow/telemetry-flow.md`](flow/telemetry-flow.md) |
| Channel-driven session state engine | [`howto/channel-state-engine.md`](howto/channel-state-engine.md) | active backlog | covered in `architecture.md` |
| Automata / DAG orchestrator | [`howto/autonomous-planning.md`](howto/autonomous-planning.md), [`howto/autonomous-review-approve.md`](howto/autonomous-review-approve.md), [`howto/automata-orchestrator.md`](howto/automata-orchestrator.md) | many plans | architecture covers it |
| Skills | [`howto/skills-sync.md`](howto/skills-sync.md) | ✓ | ✓ |
| Council Mode | [`howto/council-mode.md`](howto/council-mode.md) | ✓ | ✓ |
| Algorithm Mode | [`howto/algorithm-mode.md`](howto/algorithm-mode.md) | ✓ | ✓ |
| Evals | [`howto/evals.md`](howto/evals.md) | ✓ | ✓ |
| Identity / Telos | [`howto/identity-and-telos.md`](howto/identity-and-telos.md) | ✓ | ✓ |
| Secrets Manager | [`howto/secrets-manager.md`](howto/secrets-manager.md) | ✓ (native/KeePass/1Password/Vault) | covered in `architecture.md` |
| Container workers | [`howto/container-workers.md`](howto/container-workers.md) | ✓ | ✓ |
| Federated observer | [`howto/federated-observer.md`](howto/federated-observer.md) | ✓ | ✓ |
| Comm channels | [`howto/comm-channels.md`](howto/comm-channels.md) | ✓ | ✓ |
| Voice input | [`howto/voice-input.md`](howto/voice-input.md) | ✓ | ✓ |
| MCP tools | [`howto/mcp-tools.md`](howto/mcp-tools.md) | ✓ | ✓ |
| Pipeline chaining | [`howto/pipeline-chaining.md`](howto/pipeline-chaining.md) | ✓ | ✓ |
| Cross-agent memory | [`howto/cross-agent-memory.md`](howto/cross-agent-memory.md) | ✓ | ✓ |
| Daemon operations | [`howto/daemon-operations.md`](howto/daemon-operations.md) | ✓ | ✓ |
| Profiles | [`howto/profiles.md`](howto/profiles.md) | ✓ | ✓ |
| Tailscale mesh | [`howto/tailscale-mesh.md`](howto/tailscale-mesh.md) | ✓ | ✓ |
| chat / LLM quickstart | [`howto/chat-and-llm-quickstart.md`](howto/chat-and-llm-quickstart.md) | ✓ | ✓ |
| Multi-server management | [`howto/multi-servers.md`](howto/multi-servers.md) | v7.2.0 | REST proxy + aggregated endpoints |
| MCP Prompts | [`howto/mcp-prompts.md`](howto/mcp-prompts.md) | v7.1.0 | MCP protocol spec |
| MCP Resources | [`howto/mcp-resources.md`](howto/mcp-resources.md) | v7.1.0 | MCP protocol spec |
| MCP Sampling | [`howto/mcp-sampling.md`](howto/mcp-sampling.md) | v7.1.0 | MCP protocol spec |
| MCP Elicitation | [`howto/mcp-elicitation.md`](howto/mcp-elicitation.md) | v7.1.0 | MCP protocol spec |
| Docs-as-MCP-Interface | [`howto/docs-as-mcp.md`](howto/docs-as-mcp.md) | v6.21.0 | hybrid search index |
| Dashboard (mission control) | [`howto/dashboard.md`](howto/dashboard.md) | v7.0.0 | WebSocket-driven layout |
| LLM Registry | [`howto/llm-registry.md`](howto/llm-registry.md) | v7.0.0 | `/api/llms` CRUD + named routing |
| Compute Nodes | [`howto/compute-nodes.md`](howto/compute-nodes.md) | v7.0.0 | `/api/compute/nodes` CRUD |
| Push notifications | [`howto/push-notifications.md`](howto/push-notifications.md) | v7.0.0-alpha.35 | UnifiedPush + ntfy SSE |
| Push registration API | [`howto/push-setup.md`](howto/push-setup.md) | v8.2.0 | register/unregister/notify + Android UP |
| Async PRD decompose | [`howto/decompose-async.md`](howto/decompose-async.md) | v8.2.0 | 202 + SSE stream + Last-Event-ID |
| Channel-address federation | [`howto/channel-routing.md`](howto/channel-routing.md) | v8.3.0 | channel_identity + routing rules + owner_peer |
| Federated file service | [`howto/file-service.md`](howto/file-service.md) | v8.3.0 | peers/ + discussions/ subdirs, config:read/write caps |
| Discussion scopes | [`howto/discussion-scopes.md`](howto/discussion-scopes.md) | v8.4.0 | WAL-backed per-discussion memory, federated sync, conflict resolution, comm:read/write caps |
| Claude hooks | [`howto/claude-hooks.md`](howto/claude-hooks.md) | v7.0.0-alpha.34 | hook scripts + status board |
| Alerts & notifications | [`howto/alerts-and-notifications.md`](howto/alerts-and-notifications.md) | v7.0.0 | alert dock + per-channel delivery |
| Guardrail library | [`howto/guardrail-library.md`](howto/guardrail-library.md) | v7.0.0 | SAST/secrets/deps/LLM scan profiles |
| Ollama marketplace | [`howto/ollama-marketplace.md`](howto/ollama-marketplace.md) | v7.0.0-alpha.33 | embedded catalog + background pull |
| Session AI summarizer | [`howto/sessions-deep-dive.md`](howto/sessions-deep-dive.md) | v8.9.0–v8.9.4 | dual short/long summary, model selector, `GET /api/sessions/{id}/current-status` |
| Anti-clobber typing detection | [`howto/sessions-deep-dive.md`](howto/sessions-deep-dive.md) | v8.9.17–v8.9.19 | 30 s idle wait, per-session send queue |
| imap-mcp email channel | [`howto/comm-channels.md`](howto/comm-channels.md) | v8.9.23 | SSE receive + REST send via imap-mcp, `imap_mcp.*` config |
| MCP session name resolution | [`howto/mcp-tools.md`](howto/mcp-tools.md) | v8.9.24 | session names accepted in addition to hex IDs; `start_session` gains `permission_mode` |
| Compute node migration CLI | [`howto/compute-nodes.md`](howto/compute-nodes.md) | v8.9.25 | `datawatch compute migrate` for deprecated-kind nodes |
| Federation peer health alerts | [`howto/federated-observer.md`](howto/federated-observer.md) | v8.9.25 | background peer-health goroutine, system alerts on state transitions |
| Self-update pipeline | [`howto/daemon-operations.md`](howto/daemon-operations.md) | v8.9.21 | goreleaser archive priority, `datawatch-channel` co-update, `GET /api/update/check` |
| Session lineage | [`howto/sessions-deep-dive.md`](howto/sessions-deep-dive.md) | v8.10.0 | parent_id, kill_children, session_children MCP, reply_to_parent, REST tree |
| Recurring named schedules | [`howto/sessions-deep-dive.md`](howto/sessions-deep-dive.md) | v8.10.4 | cron_expr, session_name, schedule_name; CronNext parser; cancel-by-name |
| Name-addressed session ops | [`howto/mcp-tools.md`](howto/mcp-tools.md) | v8.10.5 | session_name on 9 MCP tools; resolveSession oldest-wins tiebreak |
| Session zombie detection | [`howto/sessions-deep-dive.md`](howto/sessions-deep-dive.md) | v8.10.6 | claude_alive *bool, tmux pane probe, reconciler integration |
| Session exit hooks | [`howto/sessions-deep-dive.md`](howto/sessions-deep-dive.md) | v8.10.7 | ExitHookStore, restart/notify actions, cooldown, session.exit_hooks[] config |
| Work queue | [`howto/sessions-deep-dive.md`](howto/sessions-deep-dive.md) | v8.10.8 | QueueStore, atomic Claim, ExpireLeases, role-based dispatch |
| Discussion push/subscribe | [`howto/discussion-scopes.md`](howto/discussion-scopes.md) | v8.10.9 | DiscussionSubStore, dispatchDiscussionEntry, WAL long-poll after_seq |
| Session restart from any state | [`howto/sessions-deep-dive.md`](howto/sessions-deep-dive.md) | v8.10.10 | RestartSession kills live tmux + relaunches, preserves ID/name/lineage |
| Agent result store | [`howto/sessions-deep-dive.md`](howto/sessions-deep-dive.md) | v8.10.11 | ResultStore KV, optional TTL, file-backed |
| Structured session filters | [`howto/sessions-deep-dive.md`](howto/sessions-deep-dive.md) | v8.10.12 | SessionFilter glob/state/backend/alive, ListSessionsFiltered, format=json |
| MCP channel bridge diagnostics | [`howto/channel-state-engine.md`](howto/channel-state-engine.md) | v8.10.16 | per-session ChannelPort registry, live /health probe, port-conflict identification, DATAWATCH_* env var documentation |
| Scheduled session spawn (ephemeral) | [`howto/sessions-deep-dive.md`](howto/sessions-deep-dive.md) | v8.11.0–v8.13.2 | DeferredSession, subprocess mode, overlap guard, run history, --shell/--path flags |
| Extra MCP servers per session | [`howto/mcp-tools.md`](howto/mcp-tools.md) | v8.13.0 | session.extra_mcp_servers, WriteProjectMCPConfig, InjectExtrasIntoMCPConfig |
| Dedicated send_input endpoint | [`howto/sessions-deep-dive.md`](howto/sessions-deep-dive.md) | v8.12.9 | POST /api/sessions/send bypasses text-command parsing; scheduleSettleMs applies to all sources |
| LLM-optional memory recall | [`docs/memory.md`](memory.md) | v8.13.3 | `Recall`/`RecallAll`/`RecallInNamespaces`/`RetrieveContext` fall back to LIKE keyword search when Ollama offline; `LazyReembed()` back-fills vectors when LLM returns; summarizer returns stub instead of error |
| FCM push payload enrichment | [`howto/push-notifications.md`](howto/push-notifications.md) | v8.13.1 | session_id/session_name/last_response in session_state_changed and waiting_input push events |
| Goose backend | [`docs/llm-backends.md`](llm-backends.md) | v8.13.36–v8.14.0 | provider/model/api_key env-var injection, MCP channel bridge, `agent-goose` container, `channel_enabled` config flag |
| Vision input system | [`docs/config-reference.yaml`](config-reference.yaml) · [`docs/mcp.md`](mcp.md) | v8.15.0 (pending) | `vision.*` config, `POST /api/vision/describe`, comms image→description injection, `vision_describe` MCP tool, `image_paths` on start_session/send_input, council `image_path`, skills `accepts_images`, `remember [image]` |

Every core feature now has a dedicated how-to. Per-channel coverage on each is being expanded so the same walkthrough works across PWA / Mobile / REST / MCP / CLI / Comm / YAML — every operator workflow is reachable from every surface.

## Documentation index

### How-to walkthroughs

Sessions + state:
- [`howto/sessions-deep-dive.md`](howto/sessions-deep-dive.md) — anatomy, lifecycle, daemon-restart resume, debugging
- [`howto/channel-state-engine.md`](howto/channel-state-engine.md) — why a session is in its current state; signals + diagnostic walkthrough
- [`howto/session-telemetry.md`](howto/session-telemetry.md) — structured task telemetry, guardrail verdicts, persist-on-stop
- [`howto/claude-hooks.md`](howto/claude-hooks.md) — hook script setup, structured payload schema, TodoWrite integration

PAI parity stack:
- [`howto/identity-and-telos.md`](howto/identity-and-telos.md) — operator self-description; injected into every session's L0
- [`howto/algorithm-mode.md`](howto/algorithm-mode.md) — 7-phase structured-thinking harness
- [`howto/evals.md`](howto/evals.md) — rubric-based grading suites
- [`howto/council-mode.md`](howto/council-mode.md) — 12-persona structured debate
- [`howto/skills-sync.md`](howto/skills-sync.md) — git-backed PAI-format skill manifests

Comms + LLM:
- [`howto/chat-and-llm-quickstart.md`](howto/chat-and-llm-quickstart.md) — most-common chat × backend pairings
- [`howto/comm-channels.md`](howto/comm-channels.md) — all 11 messaging backends
- [`howto/voice-input.md`](howto/voice-input.md) — transcription backends
- [`howto/alerts-and-notifications.md`](howto/alerts-and-notifications.md) — alert dock, per-channel delivery, push notifications
- [`howto/push-notifications.md`](howto/push-notifications.md) — UnifiedPush registration, ntfy-compat SSE streams
- [`howto/push-setup.md`](howto/push-setup.md) — BL330 register/unregister/notify API, Android integration (v8.2.0)
- [`howto/channel-routing.md`](howto/channel-routing.md) — BL331 channel-address federation: route channel messages to peers, owner_peer attribution (v8.3.0)
- [`howto/mcp-tools.md`](howto/mcp-tools.md) — wire datawatch into Claude Code / Cursor / any MCP host
- [`howto/mcp-resources.md`](howto/mcp-resources.md) — 21 URI-addressed live resources
- [`howto/mcp-prompts.md`](howto/mcp-prompts.md) — 10 prompt slash commands with live context injection
- [`howto/mcp-sampling.md`](howto/mcp-sampling.md) — LLM completions routed through the connected MCP host
- [`howto/mcp-elicitation.md`](howto/mcp-elicitation.md) — structured operator input via approval/text/choice schemas

Automata + orchestration:
- [`howto/autonomous-planning.md`](howto/autonomous-planning.md) — submit a free-form spec, watch it decompose
- [`howto/decompose-async.md`](howto/decompose-async.md) — async decompose: 202 + SSE story stream + Last-Event-ID resume (v8.2.0)
- [`howto/autonomous-review-approve.md`](howto/autonomous-review-approve.md) — PRD lifecycle gate
- [`howto/automata-orchestrator.md`](howto/automata-orchestrator.md) — multi-Automata graphs with guardrails
- [`howto/pipeline-chaining.md`](howto/pipeline-chaining.md) — DAG pipelines with before/after gates

Infrastructure:
- [`howto/profiles.md`](howto/profiles.md) — Project + Cluster Profiles
- [`howto/container-workers.md`](howto/container-workers.md) — Docker / Kubernetes ephemeral workers
- [`howto/tailscale-mesh.md`](howto/tailscale-mesh.md) — Headscale + commercial Tailscale agent mesh
- [`howto/secrets-manager.md`](howto/secrets-manager.md) — native + KeePass + 1Password + Vault backends
- [`howto/federated-observer.md`](howto/federated-observer.md) — push-based multi-host stats aggregation
- [`howto/multi-servers.md`](howto/multi-servers.md) — register remote instances, per-tab picker, all-servers aggregation
- [`howto/compute-nodes.md`](howto/compute-nodes.md) — GPU/CPU node registry, kind taxonomy, observer peer binding
- [`howto/v7-compute-migration.md`](howto/v7-compute-migration.md) — migrate deprecated compute node kinds to ollama/openai-compat
- [`howto/llm-registry.md`](howto/llm-registry.md) — named LLM registry, per-node model lists, failover routing
- [`howto/ollama-marketplace.md`](howto/ollama-marketplace.md) — browse and pull models from the embedded Ollama catalog
- [`howto/guardrail-library.md`](howto/guardrail-library.md) — SAST/secrets/deps/LLM grader scan profiles
- [`howto/file-service.md`](howto/file-service.md) — BL333 federated file service: upload/delete files, peers/ + discussions/ subdirs, config caps (v8.3.0)
- [`howto/discussion-scopes.md`](howto/discussion-scopes.md) — BL332 discussion scopes: WAL-backed per-discussion memory, participant sync, conflict resolution (v8.4.0)
- [`howto/dashboard.md`](howto/dashboard.md) — mission control: constellation, EKG, sprint pipeline, customisable cards
- [`howto/claude-hooks.md`](howto/claude-hooks.md) — hook script setup, status board, auto-install for claude-code sessions

Memory + ops:
- [`howto/cross-agent-memory.md`](howto/cross-agent-memory.md) — episodic memory + knowledge graph + 4-scope hierarchy (persona-global → project-shared → session-local) with borrow/seed/promote
- [`howto/daemon-operations.md`](howto/daemon-operations.md) — start / stop / restart / upgrade / logs
- [`howto/setup-and-install.md`](howto/setup-and-install.md) — first-time install end-to-end

### Architecture & internals

- [`architecture.md`](architecture.md) — high-level system shape
- [`architecture-overview.md`](architecture-overview.md) — daemon, backends, channels, memory
- [`backends.md`](backends.md) — LLM backend integration
- [`agents.md`](agents.md) — container worker model
- [`addons.md`](addons.md) — plugin framework

### Operations + reference

- [`setup.md`](setup.md) — install + first run
- [`api/`](api/) — REST endpoints
- [`api-mcp-mapping.md`](api-mcp-mapping.md) — MCP ↔ REST surface map

### Plans + backlog

- [`plans/README.md`](plans/README.md) — every active plan + backlog
- [`plans/historical-plans/`](plans/historical-plans/) — archived plans (>1 week)
- [`plans/historical-releasenotes/`](plans/historical-releasenotes/) — off-minor release notes

For per-feature attribution to upstream projects, see [`plan-attribution.md`](plan-attribution.md).
