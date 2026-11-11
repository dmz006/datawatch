# datawatch — User Manual

## What is datawatch?

Datawatch is a single-binary control plane for orchestrating AI work. One operator, one daemon, one consistent surface — and underneath, a set of long-running AI sessions, ephemeral container workers, persistent memory, structured identity, multi-phase reasoning, rubric-based evaluation, and multi-persona debate, all running together with one set of lifecycle, audit, and security guarantees.

It started as a small bridge between mobile messaging apps and AI coding sessions running in `tmux` — type a message in Signal, watch the LLM work, get the answer back. That bridge is still here, but the surface around it grew. Today datawatch runs entire workflows end-to-end: it holds your operator identity (your role, your goals, your constraints) and injects it into every spawned session so the AI stays aligned. It decomposes a high-level intent into a directed graph of stories and tasks, dispatches each to the right backend, captures the output, runs it past graders, and pulls in a council of personas for structured critique when the decision is non-trivial. It remembers what was decided last week. It can spawn workers in remote Kubernetes clusters as easily as it spawns local sessions. It speaks the same REST + MCP + CLI + chat + PWA + mobile + YAML surface no matter which side you approach it from.

## Who is it for?

Operators who want **one** place to drive AI work — not a tab in five different web apps, not a notebook here and a CLI there. People who want their AI sessions to come back tomorrow remembering what was discussed today. Teams that need their AI work attributed, audited, and bounded by explicit authorization. Hobbyists who want a PAI (Personal AI Infrastructure) they can self-host without paying for SaaS layers.

## What it gives you

- **Long-lived AI sessions** that survive daemon restarts and re-attach cleanly. xterm.js streaming in the PWA, full tmux underneath, full event history captured.
- **Ephemeral container workers** in Docker or Kubernetes, spawned on demand with PQC bootstrap, distroless images, per-pod auth, and Tailscale mesh.
- **Episodic memory** — your sessions remember each other. Vector-indexed project knowledge across sessions, with the spatial schema (floor / wing / room / hall / shelf / box) that makes recall actually work. The **scope hierarchy** (persona-global → persona-in-project → project-shared → **prd-shared** → **story-shared** → session-local) lets you borrow cross-agent context without polluting higher scopes, seed curated knowledge into a narrower scope, and promote session discoveries up to shared scopes with breadcrumb provenance. **Subprocess mode** (opencode, Goose, and other MCP-based tools) routes writes to `session-local` by default and recalls the full 6-layer hierarchy. **Warm-start seeding** pre-populates a new task session from applicable scopes before it begins work. **Memory harvest** auto-promotes session learnings to `story-shared` or `prd-shared` on task completion. **Archive-on-delete** promotes valuable memories to `project-shared` before purging an entity's scope when an Automaton is permanently deleted. **Archive import** seeds a new Automaton from a prior Automaton's archived memories. **Memory handoff** (`memory_handoff` MCP tool) writes a structured handoff summary to `story-shared` so the next task in the same story starts with a clear account of what was left and why. **Automaton memory report** (`GET /api/autonomous/prds/{id}/memory-report`, `memory_prd_report` MCP tool) aggregates and deduplicates all memories associated with a completed Automaton across all scopes in one call. See [`howto/automata-memory-workflow.md`](howto/automata-memory-workflow.md) for the complete lifecycle walkthrough.
- **Multi-channel messaging** — Signal, Telegram, Discord, Slack, Matrix, Twilio, GitHub webhooks, generic webhooks, DNS channel; voice input via Whisper; image/photo attachments described by a configurable vision backend.
- **Pluggable LLM backends** — claude-code, aider, goose, gemini, opencode, opencode-acp, ollama, openwebui, custom shell.
- **Operator identity** — a structured self-description you write once and the daemon injects into every spawned session as the L0 wake-up layer.
- **Algorithm Mode** — a 7-phase structured-thinking harness (Observe → Orient → Decide → Act → Measure → Learn → Improve) you can drive a session through with output captured at each gate.
- **Evals Framework** — rubric-based grading suites (string / regex / binary / LLM-rubric) with capability vs regression thresholds.
- **Council Mode** — multi-persona debate. 12 default personas (security-skeptic, ux-advocate, perf-hawk, simplicity-advocate, ops-realist, contrarian, platform-engineer, network-engineer, data-architect, privacy, hacker, app-hacker). Quick (1 round) for fast checks, debate (3 rounds) for serious decisions. Async-first with SSE live-watch; AI persona wizard drafts `system_prompt` via LLM interview. Accepts `image_path` on `POST /api/council/run` — image is described once and injected into every persona's proposal (requires `vision.enabled: true`).
- **Skill registries** — git-backed PAI-format skill manifests synced into your workspaces on demand.
- **Secrets manager** — native AES-256-GCM store at `~/.datawatch/secrets.db` plus optional KeePass and 1Password backends; `${secret:name}` references resolve in YAML, plugin manifests, spawn-time env injection.
- **Native ACME / Let's Encrypt** — the daemon obtains and auto-renews its own browser-trusted TLS certificate (HTTP-01), no external tool and no DNS management required beyond a single A record. Staging-first workflow, restart-based apply, all 7 surfaces. See [`howto/letsencrypt-acme.md`](howto/letsencrypt-acme.md).
- **Federated observer** — multiple datawatch instances pushing process / network / GPU stats into one aggregated view.
- **Autonomous Automata (PRD-DAG)** — high-level intent decomposed into a directed graph of stories and tasks, executed under verification + guardrails.
- **Plugin framework** — manifest-driven hot-reload; subprocess + native plugins; declared comm verbs / CLI subcommands / MCP tools / mobile cards. Community plugins can be installed from a connected registry with `datawatch plugins install <registry> <name>`.

## How it's built

**Single binary.** No language runtime to install, no microservices to operate, no bus to deploy. The binary embeds the PWA, the docs, the MCP server, the messaging adapters, and the daemon — `datawatch start` is the whole install.

**One surface, mirrored seven ways.** Every feature reachable through the REST API is reachable via MCP, the CLI, every comm channel, the PWA, the mobile (Compose Multiplatform) app, and YAML on disk. **Read once, write once, audit once.** No drift between surfaces.

**Tmux-backed sessions.** AI work happens in real terminals so you can attach with `tmux attach` and see exactly what the LLM is doing — no abstraction layer between you and the work.

**Open data.** Sessions, memory, identity, audit, scheduled work, and persona definitions all live as plain files under `~/.datawatch/`. No proprietary database. Operator-editable, operator-grep-able, operator-backup-able.

## How to use this manual

Each section maps to one PWA tab or card. The structure is the same throughout: what the card is for → what each control does → links to deeper reference (architecture docs, how-to walkthroughs, diagrams). The PWA's `?` icon next to the search button (Sessions / Session detail / Automata / Observer / Settings views) deep-links you straight to the matching section here. The Alerts tab carries its own round `?` help link — at the right edge of the list's meta row (the `Cached` / `Last updated N s ago` row above the list, not a separate top-right row), with a 40 × 40 px minimum tap target on touch devices — which opens the Alerts howto ([`howto/alerts-and-notifications.md`](howto/alerts-and-notifications.md)) in a new tab.

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
  - [Launch Automaton form](#launch-automaton-form)
  - [Automaton detail — Overview / Stories / Decisions / Scan](#automaton-detail)
  - [Lifecycle subscriptions — outbound webhook and peer forwarding](#lifecycle-subscriptions--outbound-webhook-and-peer-forwarding)
  - [Infrastructure retry](#infrastructure-retry)
  - [Change record](#change-record)
  - [Generated artefacts](#generated-artefacts)
  - [Merge pipeline](#merge-pipeline)
  - [Release size](#release-size)
  - [Release readiness bar](#release-readiness-bar)
  - [Post-ship hooks](#post-ship-hooks)
  - [Merge pipeline source](#merge-pipeline-source)
  - [Merge lock](#merge-lock)
  - [Release-bump validator](#release-bump-validator)
  - [Release validator](#release-validator)
  - [Merge conflict policy](#merge-conflict-policy)
  - [Merge conflict model](#merge-conflict-model)
  - [Merge conflict mode](#merge-conflict-mode)
  - [Merge conflict confirm first N](#merge-conflict-confirm-first-n)
  - [Merge conflict classes](#merge-conflict-classes)
  - [Release approval](#release-approval)
  - [Release gate](#release-gate)
  - [Onboarding report](#onboarding-report)
  - [Onboarding report checks](#onboarding-report-checks)
  - [Onboarding report next steps](#onboarding-report-next-steps)
  - [Model routing](#model-routing)
- [Automation](#automation)
  - [Automation list](#automation-list)
  - [Automation detail and pipeline strip](#automation-detail-and-pipeline-strip)
  - [Shipped](#shipped)
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
  - [Agent trace](#agent-trace)
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

*Recursive (all descendants):* set `kill_children_recursive=true`. Kills grandchildren and deeper regardless of their own settings.
- **MCP:** `start_session(task="...", kill_children_recursive=true)`
- **REST:** `{"task": "...", "kill_children_recursive": true}`
- **CLI:** `datawatch session new "task" --kill-children-recursive`
- **Channel:** `new: kill_children_recursive=true: task description`
- **PWA:** "Kill children recursively" checkbox in the new session form.

**Leftover processes (session-end reap):** when a session ends for any reason, the daemon ends processes the worker started and left running. A process is matched only by evidence read from `/proc` (cwd, executable or script inside the session worktree or scratch folder, ancestry to the session, or the session id in its environment), never by name, and is ended by exact pid after the evidence and start time are re-checked. The result is a `session_end_reap` decision on the task and a `session_end_reap` audit entry (the decision only when the session is bound to a task; nothing is written when nothing was found). Processes that survive their signal are only counted as `survivors` in the audit entry; their pids and commands are not recorded, so check for them by hand. See [operations](operations.md#processes-a-session-leaves-behind). The full how-to — evidence kinds, the signal sequence, survivors, the audit entry and task decision, and every read surface — is [howto/automata-orphan-reaping.md](howto/automata-orphan-reaping.md).

**Viewing children:**

- **MCP:** `session_children(session_id="pp01")` — lists child sessions with state and task.
- **REST:** `GET /api/sessions/{id}/children` — JSON array of child sessions.
- **REST tree:** `GET /api/sessions?tree=1` — full session forest as nested tree.
- **REST aggregated:** `GET /api/sessions/aggregated?parent_id=<id>` — children across all federation peers.
- **CLI:** `datawatch session children <id>` — local children; `--all-servers` for federation peers.
- **Session list:** child sessions show `↳ child of [<parent-id>]` in all list views.
- **PWA Tree view:** toggle "Tree" in Sessions toolbar to group sessions by parent/child lineage with orphan indicators.

**Orphaned sessions:**

Sessions whose `parent_id` references a session that no longer exists.

- **REST:** `GET /api/sessions/orphaned` or `GET /api/sessions?orphaned=1`
- **MCP:** `list_orphaned_sessions` tool; `list_sessions(orphaned=true)` param
- **CLI:** `datawatch session orphaned` or `datawatch session list --orphaned`
- **Channel:** `session orphaned`

**Self-session discovery and scope:**

Agents that need to know their own session ID or which MCP channel they are registered under, without relying on `$CLAUDE_SESSION_ID`.

- **MCP:** `get_my_session_id(hint="<id>")` — hint is optional; finds the channel-ready active session.
- **MCP:** `get_my_scope(hint="<id>")` — the full self-scope: identity, per-session MCP server name (and the shared `datawatch` entry name it must not confuse with its own), host/container mode, project dir, channel readiness, bridge liveness, and every peer session's name + MCP server name. Read-only.
- **REST:** `GET /api/sessions/self`; `GET /api/sessions/self/scope?hint=<id>`
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

- **MCP:** `schedule_spawn(task="run audit", dir="/home/<user>/project", cron_expr="0 * * * *", name="hourly-audit", one_shot=true, ephemeral=true)`
- **REST:** `POST /api/schedules` body: `{"type": "spawn", "task": "run audit", "project_dir": "/home/<user>/project", "cron_expr": "0 * * * *", "schedule_name": "hourly-audit", "one_shot": true, "ephemeral": true}`
- **CLI:** `datawatch schedule spawn --task "run audit" --dir /home/<user>/project --cron "0 * * * *" --schedule-name hourly-audit --ephemeral`
- **CLI (shell mode):** `datawatch schedule spawn --shell "run-rules --config ~/cfg.yaml" --path /home/<user>/project --cron "0 * * * *" --schedule-name hourly`
- **Channel:** `schedule spawn task=run audit dir=/home/<user>/project cron=0 * * * * name=hourly-audit ephemeral=true`

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
  --path /home/<user>/workspace/imap-mcp \
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

**How it works:** A periodic reconciler probes each active session by reading the pane's foreground command (`tmux display-message #{pane_current_command}`). If the foreground process is not a shell, the session is alive. If the foreground is a shell (bash/sh/zsh/fish/dash/ksh/tcsh), it is alive only while that shell still has a live child process, and is flagged as a zombie only when the shell has no live child. That is why a one-shot worker running as a shell pipeline is not flagged: tmux reports `bash` as the foreground even while the worker and its `tee` are still alive, but the shell's live children keep the session marked alive until they finish.

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
      command: /home/<user>/imap-mcp/imap-mcp  # Executable (absolute path)
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

### Discussion id

The name of a discussion scope or file folder: 1 to 128 characters from letters, digits, `.`, `_` and `-`; `.` and `..` are rejected. Any other id gets HTTP 400 `invalid discussion id`. Because an accepted id can never carry a path separator, any folder path built from it stays a direct child of the discussion base folder, so an id can never resolve outside the discussion directory. Every discussion route builds that folder path through one shared helper, `discussionFolderPath`, centralized and unit-tested in `internal/server/discussion_folder.go`, which runs the validation and the containment check at the join itself; the scope, sync and file-service handlers no longer join the id into a path inline (see [operations](operations.md) for the operator items that remain open). Older ids with other characters stay on disk but are unreachable through the API; operators should rename them. See [discussion-scopes](howto/discussion-scopes.md) and [file-service](howto/file-service.md).

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

> Note: this is a generic named key-value store. The **scheduled-job result
> panel** (below) is a different thing — structured per-session run results
> with per-schedule history.

---

### Scheduled job result panel

The structured result a session posts when its work finishes, plus the
retained per-schedule history that powers the result panel.

- **MCP:** `post_result(session_id="host-a1b2", title="Build succeeded", status="ok", metrics='{"score": 92.5}')`
- **MCP:** `schedule_results_list()` — latest result per schedule, retained count, recent status sequence
- **MCP:** `schedule_results_list(schedule_name="nightly-build")` — full retained history for one schedule
- **CLI:** `datawatch schedule-results list` — latest result per schedule, retained count, recent status sequence; `datawatch schedule-results history <schedule_name>` — full retained history for one schedule; `datawatch schedule-results post --title … [--status ok] [--summary …] [--metrics JSON] [--table JSON …] [--markdown …] [--file payload.json]` (outside a spawned session add `--session <id>`; inside one it defaults to the session's own ID and the spawn-scoped result credential is used)
- **REST:** `POST /api/sessions/{id}/result`, `GET /api/schedule-results`, `GET /api/schedule-results/{schedule_name}`
- **Web:** the session status page renders each session's latest posted result in its result panel

Shared payload contract: `title` (required), `status` (`ok` / `warn` /
`error`), and optional `summary`, `metrics`, `tables`, `markdown`. A session
that belongs to a named schedule keeps its last N results (default 20,
config `results.schedule_history_limit`); every posted result is also
reflected in the session's status-board result slot, scheduled or not.

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

### Claude Code interstitial prompts

Some Claude Code sessions pause on interstitial prompts that block the session's input instead of a task — the most common is the autocompact threshold question: Claude Code asks *“Want to autocompact at 400k tokens instead?”* and waits between “**Autocompact at 400k**” and “Keep auto”, a choice where pressing **Enter** confirms the highlighted option and changes a setting. Idle prompts and similar notices sit the same way. Left alone, such a prompt makes the session look healthy while it sits idle — a worker can sit on it for an hour, holding its lane, until a supervisor watch flags it as silent.

datawatch recognises the known interstitial prompts in a session's terminal output (a small, explicit kind table, matched on the last few non-empty lines after ANSI escape codes are stripped) and closes them by sending the one safe key for that prompt kind — **Esc** (“not now”), never **Enter** on a choice that changes a setting — instead of typing anything into the prompt. The dismissal is bounded: a fixed delay before the first key, a retry only while the prompt is still visible after a re-capture, and a small attempt budget before the session is flagged. Every outcome is audited (`ui_prompt_dismissed` with the prompt kind, `ui_prompt_not_cleared` after the budget is spent, `ui_prompt_refused` when a screen could not be classified), and when the budget is exhausted datawatch raises an operator question describing the prompt and the screen tail so the operator can answer it by hand. A per-kind counter feeds `GET /api/stats` (the `ui_prompts` object) and the Prometheus `ui_prompt_dismissals_total` metric.

Known prompt kinds: the built-in table today holds one kind, **autocompact-threshold** — the “Want to autocompact at Nk tokens instead?” question with the Autocompact / Keep auto option pair and the “choose · confirm · Esc not now” hint line — dismissed with **Esc** since **Enter** would pick the highlighted row and can change a setting. Everything else classifies as *none* by design: idle prompts, quoted prompt wording, and every startup or decision dialog (workspace trust, permission and usage-limit prompts) are deliberately left for the operator. Matching is strict — all anchor phrases plus the prompt's own hint line must be present in the last 20 non-empty lines, ANSI stripped — and a partial or in-doubt capture is *none*. Kinds are governed, not configured: a new kind is only added in-tree with its safe dismissal key, the reason that key is safe, and a recorded fixture carrying the exact wording and its source — see the *Detection Pattern Governance* note in `AGENT.md`.

The four Settings → Sessions controls behind this behaviour (also `configure session.dismiss_ui_prompts=…` as a chat verb and the same keys on `PUT /api/config`, `config_set` and `datawatch config set`):

| Setting | Default | Meaning |
|---|---|---|
| `session.dismiss_ui_prompts` | on | Recognise and dismiss known interstitial prompts automatically. Switch off to answer them yourself. |
| `session.ui_prompt_dismiss_delay_seconds` | `5` | How long datawatch waits after detecting a prompt before sending the dismissal. `0` sends it at once. |
| `session.ui_prompt_clear_bound_seconds` | `30` | How long the dismissal must hold before the prompt counts as cleared. A prompt that reappears inside the bound is retried. |
| `session.ui_prompt_max_attempts` | `2` | How many dismissal attempts datawatch makes before flagging the session and asking the operator to step in. |

**Where it shows up:** Settings → Sessions in the PWA (four rows, each with a link to this section); the `configure` chat verb; `PUT /api/config` and the MCP `config_set` tool; `datawatch config set`/`get` in the CLI; the session's needs-input and question channels on escalation; the audit log entries above; the `ui_prompts` counters in `GET /api/stats` and the `get_stats` MCP tool; and the Prometheus `ui_prompt_dismissals_total` metric.

**See also:** [`howto/sessions-deep-dive.md`](howto/sessions-deep-dive.md), `docs/plans/parity-issues/claude-interstitial-prompt-dismissal.md`

---

### Inside a session — terminal area

`Tmux` tab is the live xterm.js view of the tmux pane the LLM is attached to. Read-only by default; tap the input bar to send commands.

**Toolbar:**
- `Aa ▾` font controls — A−, A+, current size, Fit-to-width.
- `Scroll` — enter tmux scroll mode (Page Up / Page Down / ESC). Ties into tmux's own scroll-back so you see real history.

**Input bar:** sends text with Enter; the daemon routes it through tmux send-keys. State transitions back to `Running` automatically on input (and bumps the channel-event timer so the gap watcher doesn't immediately re-flip).

**Loading splash:** appears while the first pane_capture frame arrives. Always dismisses — even for ended sessions, the saved final frame is shown.

**Live updates and Jump to live:** the terminal always shows the newest screen frame — a frame that arrives during the 33 ms render throttle is drawn a moment later rather than dropped. If you scroll up, new frames are held (so the view does not jump) and a **Jump to live** button appears; click it, type, send input, or scroll back to the bottom to return to the live screen. If no frame arrives for 30 s on an open session, the PWA re-subscribes to the session automatically.

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

## Classification verdicts and the publisher allowlist

> Flow: [flow/classification-verdict-flow.md](flow/classification-verdict-flow.md) · How-to: [howto/classification-verdicts.md](howto/classification-verdicts.md)

- **Classification verdict** — the result of a judge (task verification or guardrail) read in **classify** mode: the judge is asked to pick one lettered option and datawatch reads the probability of each option instead of parsing prose. A verdict record keeps the mode, the score vector, the route (node, model, read path) and the outcome. The prose explanation is written separately and never changes the verdict. The mode is set per LLM with `verification_mode` (`generate` default, or `classify`).
- **Fail closed** — missing scores, a timeout, a transport error, an abstain or a stale calibration becomes a **block** with an infra category. It is retried as infrastructure, never counted against the task, never downgraded later, and never falls back to generate mode.
- **Low confidence** — a block reached because the winner did not lead by `classifier.abstain_margin` or reach `classifier.min_probability`. It is flagged and waits for a manual decision (`datawatch llm classifier review`).
- **Capability probe** — a check that a node really returns usable option probabilities for a model, on the `native` (`/v1/systemone`) or `logprobs` path. Results are cached for `classifier.probe_ttl_seconds`; an unknown result is never cached.
- **Calibration** — a pass threshold fitted on one model, quantization and node, and scored on a held-out set. Change any of the three and the calibration is stale until redone.
- **Publisher allowlist** — `classifier.publisher_allowlist`, the model publishers a classify-mode judge may come from. Empty by default, so nothing is trusted until the operator adds a publisher. Adding needs an explicit confirmation on REST, MCP, CLI and chat, is written to the audit log, and cannot be done through `PUT /api/config`. Every other `classifier.*` tunable is settable there; each is listed in [config-reference.yaml](config-reference.yaml).

---

## Memory ceiling

- **Memory ceiling** — the most memory one worker pane or verification run may use: a share of host memory, kept between a floor and a cap. See [Memory ceiling](operations.md#memory-ceiling).
- **Scope** — the systemd user scope the worker's shell runs in; the kernel holds the ceiling there.
- **Watchdog path** — the fallback when there is no user scope: the daemon watches the pane's process group through `/proc` and ends it when it passes the ceiling.
- **No-ceiling status** — `enforcement_path` is `none` and `effective_mb` is `0` when the host has neither a user scope nor `/proc`. The run is not capped. A project or task kind opt-out (`none`) shows only in the worker session's own `ceiling_path` and `ceiling_mb`, not in the status object.

## Automata

### Automata list

Every PRD / autonomous workflow your daemon has spawned. Filtered by state pill (Running / Stopped / Failed / All).

The **Tags** filter selects Automata by tag with match ANY (default) or match ALL; an empty selection lists everything. See [howto/automata-tags.md](howto/automata-tags.md) and the *Tag selection* glossary entry.

### Launch Automaton form

The "+" FAB on the Automata view launches a wizard.

**Top strip — Start from template:** browse saved Automaton templates instead of writing one from scratch. Templates carry a complete spec (type, stories, tasks, backend, effort, model, skills) and pre-fill every field below.

**Intent + title** — short free-text describing what you want the Automaton to accomplish. Title is optional; auto-derived from intent if blank.

**Inferred** — type (software / research / operational / personal) and workspace (project profile or directory). The wizard pre-fills based on intent text; click any chip / pick a profile to override.

**Execution** — backend + effort. Backend dropdown shows only backends with a configured key/endpoint; effort dropdown shows only the values the chosen backend supports (Ollama has no effort, claude-code has quick/normal/thorough).

**Advanced (collapsed)** — guided mode (per-step approval), enable scan/rules, story-level approval. Skill registries are picked from your synced skills (Settings → Automate → Skill Registries).

### Automaton detail

4-tab layout reached by clicking any Automaton row.

- **Overview** — PRD spec + current status + persistent toolbar (Edit Spec, Settings, Request Revision, Clone to Template, Delete).
- **Stories** — per-story state + Edit / Profile / Files / Approve / Reject / Add story / Remove story. Each task under a story exposes a triangle/expand button; click once to expand inline — the expanded state persists until clicked again, and each task has its own Edit / Add task / Remove task actions. Adding or removing a story or task is a structural edit that skips the LLM entirely — use it to correct or extend a decompose result without re-running planning. Only available pre-approval (`needs_review` / `revisions_asked`); once a PRD is approved the structure locks.
- **Capacity** — shown on a running Automaton's active-session card: per-pool used/limit bars (host sessions, compute node, LLM in-flight) plus a wait queue when a task is waiting for a free slot. "Host sessions" is a machine-wide cap shared with interactive sessions — not specific to this Automaton.
- **Decisions** — every state-changing event for this Automaton; click any row to expand the raw `details` payload. Filter by source (operator / autonomous / scan / etc.).
- **Scan** — Run Scan kicks off a verifier sweep against the spec; shows pass/fail across SAST / secrets / deps / LLM grader. History persists.

The header strip carries the **initial spec** (expandable — first 280 characters shown; click "show full" to reveal the complete spec inline, "collapse" to restore); **Status badge**; **Status graphs** (visible while running or decomposing — shows ✓/✗ decompose indicator, total story + task counts, per-story progress bars with X/Y tasks and %, and async CPU%/RSS from active compute envelopes); **Settings** (`openPRDSettingsModal` — type, execution backend, planning backend / decomposition profile, effort, model, skills, guided mode); **Cancel** (always visible for non-cancelled automata, including while running); **Request Revision**; **Clone to Template**; **Delete**; and **⋯ Edit** dropdown (Edit Spec, View Sessions, and other state-appropriate actions).

**Story-failure behavior:** by default, a story failing halts the whole Automaton (`status: blocked`) instead of continuing into later, independent stories — so a broken first story can't let the run barrel ahead unattended. Fix or retry the failed task (Settings → task Retry, or the CLI/MCP `reset_task`), which also un-blocks the Automaton automatically. To restore the old "keep going regardless" behavior, set `continue_on_story_failure` — a global default in Settings → General, or per-Automaton in Settings.

**Triage card:** an Automaton that ends up failed or blocked carries a triage card on this page (and it is readable on every surface): where it stopped (the blocked or failed task), when it first entered that state, what the last session was ended for, the four retry budgets, and the automation triage alert once the Automaton has sat for `autonomous.automation.triage_alert_minutes` (default 30 minutes, `0` = off; the daemon refuses to load on boot with a negative value).

**See also:**
[howto/autonomous-planning](howto/autonomous-planning.md) ·
[howto/autonomous-review-approve](howto/autonomous-review-approve.md) ·
[howto/automata-orchestrator](howto/automata-orchestrator.md) ·
[howto/algorithm-mode](howto/algorithm-mode.md) ·
[howto/evals](howto/evals.md) ·
[howto/council-mode](howto/council-mode.md) ·
[howto/skills-sync](howto/skills-sync.md)

### Lifecycle subscriptions — outbound webhook and peer forwarding

Two subscription sinks send Automaton lifecycle events out of the daemon. **Both ship disabled** until the operator turns them on. See [`howto/automaton-lifecycle-webhooks.md`](howto/automaton-lifecycle-webhooks.md).

- **Outbound webhook sink** — a subscription sink type that POSTs each matching event, as a signed JSON envelope, over HTTPS to a receiver the operator runs. It is off until `autonomous.notify.webhook.enabled` is `true`.
- **Webhook allow-list** — the operator's list of hosts (an exact host, or an exact `host:port`) that may receive a webhook. An empty list means nothing can receive one. It is checked when a subscription is created and on every delivery.
- **Signing secret reference** — the single `${secret:name}` reference on a webhook subscription that names the vault secret used to sign each delivery. A literal secret value is refused, and the resolved value is never stored, logged, audited or returned.
- **Delivery signature** — the `X-Datawatch-Signature` header, `t=<unix seconds>,v1=<hex>`, where the hex is an HMAC-SHA256 of the timestamp, a full stop and the raw request body. A receiver checks it with the shared secret and rejects a timestamp outside the replay window.
- **Delivery id** — the `X-Datawatch-Delivery` value that identifies one delivery. It is the same on every retry, so a receiver can drop a duplicate.
- **Dropped delivery** — a webhook or peer delivery that failed its last attempt. It raises one error-level alert and writes a final audit record. The subscription itself stays armed.
- **Peer forwarding** — sending a matching lifecycle event to another datawatch daemon that is registered as a federation peer with the `autonomous:write` capability. The subscription names the peer; no address or token is stored in the subscription.
- **Peer sink** — the subscription sink type that uses peer forwarding. Its target is the registered peer name.
- **Forwarded event** — a lifecycle event received from another daemon through peer forwarding. It is re-emitted locally with its origin host, is shown as "(forwarded from …)" in human sinks, and is never forwarded on or used to drive an Automaton.
- **Hop cap and visited list** — the loop guard for forwarding. A forwarded event records the origin host, the number of hops and the hosts it has passed through. A receiving daemon refuses an event that started on its own host, that already visited it, or that has reached the hop cap.

---

### Shipped

> **Version: v8.8.0+**

"Shipped" is what **happened to the work** after a PRD or Automation finished, not a status of the PRD or Automation itself. When the daemon merges the work into a release, it stamps the record with the release name (`merged_in_release`), the merge time (`merged_at`) and the merge commit (`merge_commit`). The record keeps the status it had when the work landed (a cancelled PRD can still be shipped — its cancellation is historical, the work still reached the release), and the card's border color and action buttons still follow that stored status.

The shipped-state surfaces in the PWA everywhere a status is shown:

- **Shipped chip.** Wherever the status pill is rendered (the Automata list, the PRD detail header, the Automations list and detail), a shipped record instead shows the green chip: **"Shipped in \u003crelease>"** when a release name is recorded, plain **"Shipped"** when not. The chip takes precedence over the status pill — a cancelled-but-merged record shows **Shipped**, not Cancelled. The chip tooltip carries the merge time and merge commit.
- **Detail views.** The PRD and Automation detail views show the shipped line: the ship label plus `merged_at` and the merge commit hash.
- **Filter badge.** The Automata and Automations filter bars carry a **Shipped** status badge (in addition to "all" and the usual statuses). Selecting it shows only the records that carry the shipped-state — decided by the merged commit, never by the stored status, so a cancelled-and-merged record shows under it and the "unshipped" view never matches it. The badge counts honor the count rule below.
- **Counts.** When `autonomous.shipped.count_separately` is on (the default), the shipped records pull their count out of their stored status (so "cancelled" no longer includes shipped ones) and group under the separate **shipped** badge, keeping shipped and cancelled side by side. The dashboard also shows the total shipped record count when it is greater than zero.

Behavior is server-configured under `autonomous.shipped` — `enabled` (toggle the whole surface, default on), `show_chip` (the chip visibility, default on), `filter_default` (the starting filter selection — `all`, `shipped` or `unshipped` — default `all`), and `count_separately` (the separate-counts rule, default on) — and is reported with the shipped record counts at `/api/autonomous/shipped/counts`; the list endpoints take the selection as `shipped=true|false` and honor `filter_default` when it is absent.

## Automation

An **Automation** is a named chain of Automata: it decides which Automaton runs next when the current one completes, is blocked, or fails. An **Automaton** is one unit of work. The Automation is the chain; the Automaton is a link in it. The Automation tab sits to the left of the Automata tab, and a chain can include forks (members that run side by side), joins (where the branches meet again) and other Automations nested as a single member.

### Automation list

One card per Automation, ordered by status and then name. Each card shows the status, progress as completed members out of work members, the member (or members) running now, how long an active Automation has been idle, the reason a blocked one is blocked, and the budget ceiling with its window (marked estimated when the ceiling is a share of an allocation). The round **New Automation** button at the bottom right — or **＋ Create Automation** on the empty state — opens the create and edit form; both open the same form: members, edges, fork and join, nested Automations, default models and a budget policy. The Automation sub-tab carries a second row of six sub-tabs: **Inbox** (everything waiting on you: needs-attention, questions, held questions, security failure, supervisor watch, with a count badge), **Automations** (the list, status badges, search and filter), **Resources** (capacity, lane fill, usage, infrastructure retries), **Progress and cost** (the Progress card), **Setup and policy** (onboarding report, merge pipeline, model request watch) and **Housekeeping** (safe-to-delete, cleanup, uncommitted work). Inbox is the one open by default; the Automata and Templates sub-tabs show the Progress card not.

### Automation detail and pipeline strip

Opening a card draws the chain as a pipeline strip: a status dot on every member, an active ring on members in flight, "if blocked" and "if failed" outcomes as sub-rows, fork branches as side-by-side lanes joined by a join with its rule, nested Automations that expand one level at a time, an **inserted** marker on members added later, and dashed connectors for edges a finished member did not take. Above the strip are Pause, Resume and Cancel (cancel always asks whether the running member should be cancelled too); each member card carries Skip, Insert before / after, Remove and Edit edges, and held inserts wait under Pending inserts for approval. If another operator changed the Automation first, the page asks you to reload instead of overwriting their change.

### Shipped

An Automaton or Automation can carry **shipped state**: the record of the work having landed — the release version it landed in, when it merged, and the main-branch merge commit that proves it. Shipped state is separate from the lifecycle status: recording it never changes the status, so a PRD you cancelled *after* its work merged is both cancelled (you stopped it) and shipped (the work landed); a PRD cancelled before any merge is only cancelled. That is the shipped-vs-cancelled separation: two different questions, and both answers can be true at once.

A record with shipped state **displays as shipped** in views that report an effective status, and — while the shipped surface separates the counts — takes its own **shipped** bucket in the status counts, instead of the stored status it keeps in the store. A shipped-but-cancelled PRD shows under shipped, not cancelled; a cancelled PRD whose work never landed is still under cancelled.

Recording the state names where it came from: the **merge step** (the work landed as part of the merge), or a **backfill** (a deliberately run pass over the base repository's main branch finds the `automaton/<id>` merge commits, works out the release each one landed in, and fills in the missing records — safe to re-run, it fills only what is missing and skips what is already there). Run it with `?dry_run` first to see what would be recorded without writing anything.

The five settings under **Settings → Automate → Shipped** (config block `autonomous.shipped`) — the shipped-state master switch, the chip display, the default list view, the separate-counted shipped bucket, and the backfill-at-start switch — decide what the read surfaces report. Every read surface — the web lists and the Shipped dashboard card (server-served), the REST API, the MCP tools, the CLI and the comm channel — reports the same state from the same stored fields; the PWA client does not yet render a shipped chip, offering no shipped selection (the companion-app parity item).

**See also:**
[howto/automata-shipped-status.md](howto/automata-shipped-status.md) ·
[howto/automation-tab.md](howto/automation-tab.md) ·
[howto/automation-queue-advance.md](howto/automation-queue-advance.md)

---

## Automation requirements

The **automation requirements report** answers one question: *can this daemon actually run the automations it is configured to run?* It reads the requirement minimums, the Compute Nodes with their reported backend versions, and the configured backends, then lists every requirement that is unmet. It is a report, not a gate: nothing is blocked on it, but every read surface shows the same lines.

### Requirement minimums

A minimum is a floor on a backend's version for a named capability. The built-in one: **Ollama 0.35.0** for the `classification_verdict` capability — the version that ships `systemone`, the verdict primitive the triage classification rides on. An operator can raise a floor per capability (`autonomous.requirements.min_ollama.classification_verdict`); the effective minimum is the override when present, otherwise the built-in floor. Each registered Compute Node that is an Ollama node gets a verdict per capability — **meets**, **fails**, or **unknown** when the node has not reported a version yet.

### Backends

Automation needs a backend for each workload: an **execution** backend (at least one is required to run anything), a **planning** backend and a **verification** backend. The report lists the names configured for each slot and names each missing slot in **missing**.

### Model pull consent

**Model pull consent** (`autonomous.model_pull_consent`) is the pull gate for the model registry. Per model it records `allowed` (may be pulled) and a free-text `note`; a separate publisher list is allow-listed by name. The rule is: a model may be pulled when it is listed as allowed **or** its publisher is allow-listed; a listed-but-refused entry is refused with its note, and everything else is refused by the default-deny rule that keeps pulls operator-driven. The policy is stored in `datawatch.yaml`, and every read surface reports the same body: `models[]` (name, allowed, note), `publishers[]` and the decision rule in words.

**See also:**
[howto/automation-requirements.md](howto/automation-requirements.md) ·
[howto/model-pull-consent.md](howto/model-pull-consent.md) ·
[howto/llm-registry.md](howto/llm-registry.md)

---

## Dashboard

Mission control for your entire fleet — a live, full-screen view of every running session, active Automata, and system health indicators. Requires `autonomous.enabled: true` in `datawatch.yaml`; the nav button is hidden otherwise.

The layout is fully customisable: drag cards to reorder, resize with the width/height handle. Layout persists server-side at `GET/PUT /api/dashboard/layout` so it survives browser refreshes and re-logins.

### Operator console

Five cards that open the board — **Questions waiting**, **Held & blocked**, **Lanes**, **Usage**, **CI & release** — each pointing at the PWA anchor where that part of the daemon is handled. They are the PWA face of the one-glance summary served by `GET /api/dashboard/console` (one count, one summary line and one link per section: `questions_waiting`, `held_blocked`, `lanes`, `usage`, `ci_release`); the same body is on MCP (`dashboard_console_get`), the CLI (`datawatch dashboard console`) and the chat verb `dashboard console`. The seven switches are `dashboard.console.enabled` plus the five `dashboard.console.show_*` keys and `dashboard.hide_inactive_cards`, each a boolean that reads **true** while absent. Disabling the console drops the console cards from the board.

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

Every operator action (config change, session start/stop, secret read, rollback, Automata lifecycle, alert-rule and federation-peer changes, etc.) recorded with actor / action / details / timestamp, never the bearer token. Default view shows the last 5 entries; bump the limit dropdown for more (20 / 50 / 100). Filter by actor or action substring.

This is one of **two** separate logs: `audit.log` (operator actions, shown here) and `access.log` (every HTTP request/auth-failure/WS connect across CLI+REST+MCP). Both are retained for `audit.retention_days` (default 30), can SIEM-mirror as CEF (`audit.cef_mirror_enabled`), and — for a request forwarded through a federation peer — can carry **cross-hop attribution**: `origin_actor` + `hop_chain` detail fields naming who actually initiated the action on the forwarding daemon, not just which peer presented it, verified via an HMAC-signed chain over each hop's existing shared peer token. See [howto/audit-logging](howto/audit-logging.md) for the full query surface and the trust-model detail.

### Agent trace

A redacted record of what agents did: tool calls and results, model requests, state changes, capability and permission decisions, loop warnings and errors. Read it as a timeline for one session or one Automaton, or as a roll-up by session, task, story, Automaton, agent or backend. `tracing.mode` is `off`, `summary` (default, short first-line summaries) or `full` (longer text up to `tracing.arg_cap_bytes`); `tracing.retention_days` (default 14) sets how long events are kept and `tracing.backends` turns a backend off. Each backend has a coverage level: **full**, **partial** or **events-only**, so missing detail is never read as no activity. Secrets and private paths are removed before an event is stored. Shown as the Trace sub-tab under a session's Status tab and the Trace tab of an Automaton. OpenTelemetry export is a follow-up. See [howto/agent-tracing](howto/agent-tracing.md).

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

- **Operator identity** — wake-up L0 layer self-description loaded from `~/.datawatch/identity.yaml`. Auto-injected into every spawned session so the LLM stays anchored to your role / north-star goals / current projects / values / context. Edit via inline form, the 🤖 wizard on the Automata page, or `datawatch identity {get,set,configure,edit,reset,backups,restore}`. REST: `GET/PUT/PATCH/POST /api/identity` (POST is an alias for PATCH, added in v8.2.0 for mobile compat), `POST /api/identity/reset` (the explicit clear), `GET /api/identity/backups` and `POST /api/identity/restore`. Every save keeps a backup of what it replaced, and a write that would empty a saved identity is refused; see *Identity backups and restore* under Settings → Automata (the Automata tab of Settings) later on this page.
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

**Per-tab picker** — once servers are registered, every main view (Sessions, Alerts, Automata, Observer, Settings, Dashboard) shows a chip bar at the top:
- **All** — aggregated fetch from every server; returns items tagged with their `server` origin. Observer and Settings show no **All** chip at all.
- **Local** (labelled with this daemon's hostname, never the word "Local") — only this daemon's data (default).
- **\<name\>** — proxy mode; REST and WebSocket calls route through `/api/proxy/{name}/...` on the local daemon.
- **Selecting a chip switches the page you're on, in place** (new — confirm it live before relying on it; see the [operations.md](operations.md) "Still to review (operator)" checklist) — picking a federated host (or the local chip, or **All**) re-renders the tab you're currently looking at against that server: the **Showing: \<name\>** line above the view updates to the active host and the list becomes that host's data, with no need to leave the view and re-open it. Re-selecting the already-active chip is a no-op. A genuine switch clears that view's previous server's data first (the old session list does not survive it), and sub-pages with no chip bar (session detail, the New session form) are left untouched by a switch; while a remote is active there are also one-tap "back to local" controls — a clickable **🌐 \<name\>** indicator on the Sessions toolbar (the `.server-indicator` chip in `renderSessionsView`, pinned in `app-select-server-controls.test.js`) and a **Back to \<hostname\>** button on the server's row in Settings → Comms → Remote Servers. Found 2026-11-08 and recorded under [Unreleased] → Fixed in [CHANGELOG.md](../CHANGELOG.md); that entry records that no live verification had been run when it was recorded, but the fix is implemented in this tree's `app.js` (`selectServer` re-renders the tab you're on, Automata sub-tabs included) with PWA tests — so on this source tree the old behaviour of a tab keeping the previous server's data until you navigated away and back is gone. On the **Dashboard** the switch is per panel: each panel re-fetches for the active host, and only the **Automata** and **Cost** panels aggregate under **All** (`/api/autonomous/prds/aggregated`, `/api/cost/aggregated`); the other panels have no aggregated variant. The operator checklist is in [operations.md](operations.md#proxy-mode-multi-machine-management).

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

**Individual `surface:action` caps** — 51 across 18 surfaces: `sessions:list/read/write/kill/input`, `agents:list/read/spawn/terminate`, `observers:list/read/write`, `llms:list/read/write`, `compute:list/read/write`, `analytics:read`, `health:read`, `config:read/write`, `secrets:list/read/write`, `pipelines:list/read/start/cancel`, `autonomous:list/read/write/run`, `council:list/read/run`, `federation:list/read/write`, `docs:read`, `audit:read`, `comm:read/write`, `alerts:list/read/write`, `dashboard:read/write`. `alerts:write` (raising an alert via `create_alert`) is deliberately not granted to any built-in group by default — grant it explicitly to a peer/custom group.

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

Configurable image-description backend that extends the comms router, session input, MCP tools, skills, and Council Mode to understand image/photo attachments.

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
  describe_timeout_seconds: 0  # 0 or negative = 5s default per describe call
```

**How it integrates:**

| Surface | Behaviour |
|---|---|
| **Comms router** | Image/photo attachments from Signal/Telegram/etc. are read, described, and prepended to the message text as `[image: <desc>]` before command parsing. Works with all comms commands. |
| **`remember: [image]`** | Sending `remember:` alongside an image attachment stores the description in episodic memory: `remember: [image: <desc>] <caption>`. |
| **Session input (PWA / terminal / WebSocket `send:`)** | `[image:<path>]` in the sent text is replaced with `[image: <desc> \| path: <path>]` before delivery. Each describe call is bounded by `describe_timeout_seconds` (5 s default). On timeout, error, or empty description (or no vision service at all) the tag degrades to a bare `@<path>` — the message is still delivered and the model reads the file itself — but an unreadable file never degrades: the `[image:<path>]` tag passes through unchanged. When that bare `@<path>` form is the last thing in the input text it ends in exactly one space — that space terminates the session's `@` file reference, so the file autocomplete closes and a single Enter sends the message instead of the first Enter being spent dismissing the open completion; a tag followed by any text and a successful description are never space-terminated. Over a WebSocket, the expansion runs on the connection's input worker (FIFO), so the socket's read loop — and the terminal — stays responsive; a later plain send to the *same* session over the *same connection* waits for the still-expanding image send instead of overtaking it (the wait is per connection, not global per session). |
| **`POST /api/vision/describe`** | REST endpoint for on-demand image description. Multipart `image` field + optional `prompt`. |
| **MCP `vision_describe`** | `vision_describe(image_path, prompt)` tool — agents call it from within a session. |
| **`start_session` / `send_input` MCP** | Accept `image_paths: string[]`; descriptions are prepended to the task/text before delivery. |
| **`POST /api/council/run`** | Accepts optional `image_path`; image is described once and injected into every persona's proposal. |
| **Skill manifest `accepts_images`** | Skills declare `accepts_images: true` in SKILL.md frontmatter to signal they can receive image context. |

**Supported models:** llava, llava-phi3, llava-llama3, bakllava, moondream, minicpm-v (ollama); gpt-4o, gpt-4-vision-preview (openai); any OpenAI-compat endpoint.

**REST:** `GET /api/config` + `PUT /api/config` expose `vision.*` keys. `POST /api/vision/describe` (multipart). All routes are bearer-authenticated.

#### Web Search (multi-provider registry)

Search backend registry that injects a `web_search` MCP tool into opencode and goose agent sessions. Queries are tried against one or more named providers in priority order (lowest first), with an internal result cache to cut paid-API usage and per-provider usage tracking. Providers today: self-hosted [SearXNG](https://searxng.github.io/searxng/) and the [Brave Search API](https://brave.com/search/api/). A provider that "succeeds" with zero results is treated as a miss and the next provider is tried — this defends against the silent-degradation failure mode documented below (GH#165).

**Config (`datawatch.yaml`):**

```yaml
web_search:
  enabled: false
  cache_enabled: true
  cache_ttl_seconds: 900
  providers:
    - name: searxng-primary
      type: searxng           # searxng | brave
      enabled: true
      priority: 0
      url: ""                 # SearXNG instance URL (searxng only)
      engine: bing             # SearXNG engine — see note below (searxng only)
      num_results: 10
    - name: brave-fallback
      type: brave
      enabled: true
      priority: 1
      api_key: "${secret:brave_search_api_key}"   # literal key or ${secret:name} ref
      num_results: 10
```

Legacy single-provider fields (`provider`, `url`, `engine`, `num_results` directly under `web_search:`, from before the multi-provider registry existed) are still read: if `providers` is empty and any legacy field is set, a single `default` provider is synthesized from them at load time. New configs should use `providers[]`.

**How it integrates:**

| Surface | Behaviour |
|---|---|
| **opencode sessions** | `web_search` added to `extraMCPSpecs` → injected into `.datawatch/.mcp.json` at session start. |
| **goose sessions** | `GOOSE_MCP__WEB_SEARCH__TYPE/CMD/ARGS` env vars set on the goose process. |
| **Skill** | `web-search-guidance` skill written to `<projectDir>/.datawatch/skills/web-search-guidance/` with query guidance, engine availability notes, and result interpretation tips. |
| **Dashboard** | "Search Usage" card: total/today/this week/this month + cache hits per provider and overall, a daily usage graph, and recent search history. |
| **Monitor tab** | Web Search stat tile shows enabled state, active provider names, queries/errors counters, and last-query timestamp. |
| **MCP tools** | `websearch_providers_list/get/add/update/delete/enable/disable/test`, `websearch_stats`, `websearch_history`. Legacy `web_search_stats` kept as a deprecated alias. |

**REST:** `GET/POST /api/websearch/providers`, `GET/PATCH/DELETE /api/websearch/providers/{name}`, `POST /api/websearch/providers/{name}/{enable,disable,test}`, `GET /api/websearch/stats?days=N`, `GET /api/websearch/history?limit=&offset=`. Provider API keys are never returned by GET — write-only, same convention as every other secret-backed credential field. Legacy `GET /api/web_search/stats` kept as a deprecated alias returning the old flat shape.

**CLI:** `datawatch websearch providers | get <name> | add <name> --type ... | update <name> ... | delete <name> | enable <name> | disable <name> | test <name> | stats | history`

**Comm channel:** `websearch` (stats), `websearch providers`, `websearch stats`, `websearch history`, `websearch enable|disable|test <name>` — read-only + enable/disable only; full provider add/update/delete is chat-unfriendly and stays on REST/CLI/MCP/PWA (same scope decision as Council's `backends` list).

**MCP sub-server:** `datawatch mcp-search` runs the stdio MCP server that provides the `web_search` tool to agents, loading the full `providers[]` list from `config.yaml` (with secrets resolved) by default, or a single-provider override via `--url`/`DATAWATCH_WEB_SEARCH_URL` for standalone testing.

**Secrets:** a Brave provider's `api_key` should be a `${secret:name}` reference resolved against the configured secrets vault (builtin/KeePass/1Password/Vault) — never a plaintext key committed to `config.yaml`. Set via `datawatch secrets set <name>`, the PWA Settings secrets panel, or the provider's dedicated write-only `api_key` field in its add/update form.

**Engine note (SearXNG providers):** Only the `bing` engine is reliable in a default SearXNG install. Google, DuckDuckGo, and others trigger CAPTCHA or rate-limiting immediately. Set `engine: bing` (the default).

**Known limitation (confirmed 2026-10-02):** even with `bing` correctly enabled on the SearXNG instance, its scraped results are *inconsistently* degraded for compound/multi-word technical queries — some return precisely, many collapse to generic top-level pages for only the first or most prominent term (e.g. "postgresql vacuum bloat monitoring dashboard" → generic PostgreSQL homepage/Wikipedia/download results, nothing about vacuum or bloat). This is intermittent, not deterministic — short or less brand-ambiguous queries often work fine. It reproduces consistently across unrelated fresh queries and persists across a 45s+ cooldown, so it isn't simple burst rate-limiting from one testing session. Most likely cause: Bing's own anti-scraping measures serving lower-fidelity results to detected automated traffic rather than blocking outright — not fixable via SearXNG or datawatch configuration. Filed as GH#165. This is very likely what the LLM-research PRD's 2026-09-17 "SearXNG degraded/broken" note was observing. See `docs/plans/harness-research/candidates.md` for the original incident writeup. **The multi-provider registry's direct response to this limitation**: a Brave Search API provider (paid, no scraping/CAPTCHA risk) can be configured at a lower priority number than SearXNG so it's tried first, with SearXNG kept as a free fallback — or vice versa, SearXNG first with Brave as a paid-only-when-needed fallback. If an agent's search results look suspiciously generic for a specific/technical query on a SearXNG-only setup, that's this limitation, not a tool malfunction — try rephrasing as a shorter or more distinctive query, add a Brave provider, or fall back to a direct fetch of a known documentation URL.

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

#### Cost Report

A **token-first report of model token spend over a window and a grouping**: for each group it counts the sessions and shows **tokens in / tokens out first**, and a **USD estimate where and only where configured [cost rates](#cost-rates-usd--1k-tokens) cover every backend in the group** — where a rate is missing, the row shows tokens and no dollar figure (`rate_configured` is `false`); it never invents a number. Dollar figures are estimates at configured rates, not invoices. Where the owning provider has a [billing profile](#billing-profile-usagebilling), the row says so with its label (a bundled plan reads **“bundled plan — API-equivalent, not billed”**, and its money figure is the API-equivalent: an equivalence, not a charge, plus the usage points it represents and its share of the monthly price); a provider with no profile reads back as the assumed default and keeps an ordinary estimate. The groupings are `automaton` (default), `automation`, `backend`, `release` (with the `units` — shipped units — per row; work not yet shipped lands under `unreleased`) and `week` (weeks start at `cost_report.week_start`, Monday or Sunday); the window takes `since`/`until` (RFC3339 or `YYYY-MM-DD`), an absent `until` being now and an absent `since` the daemon's `cost_report.default_window_days` (7). The figures rest on a 31-day ledger of recorded sessions, which the report body states as `retention_days`; spend older than the ledger is visible only through lifetime totals (`lifetime_fallback`). `cost_report.include_peer_spend` (default `false`) admits spend federation peers reported; `cost_report.show_usd_estimates` (default `true`) drops the dollar figures everywhere when off. One engine serves every surface — CLI, comm channel, MCP, REST, PWA — so the same window and grouping give the same rows on all of them.

The **Cost report** dashboard card (help link target: `#cost-report`) renders that report next to the live cost card, with three controls:

- **Group by** — `automaton`, `automation`, `backend` or `week`; the `default` choice leaves the grouping to the daemon's `cost_report.default_group_by` setting.
- **Window (days)** — how far back the report reaches; blank leaves the window to the daemon's `cost_report.default_window_days` setting.
- **Refresh** — re-requests `GET /api/cost/report` with the current controls.

Rendering rules: token counts (in/out) are shown first, for every group; a USD figure appears **only** where the report carries one (a group counts toward a USD estimate only while it has a rate configured — cost rates above — under the `cost_report.show_usd_estimates` setting, and the card also shows the daemon's retention line, e.g. *Figures rest on the last 31 days of recorded sessions*, plus a note when some figures fall back to lifetime rates). The card never invents a USD number for a group the daemon did not price.

**Settings** (`cost_report` group in PWA settings): `default_group_by`, `default_window_days`, `show_usd_estimates`, `week_start` (Monday or Sunday for the `week` grouping) and `include_peer_spend`.

#### Billing Profile (`usage.billing`)

The per-provider billing mode a cost-report row is read against, keyed by a provider name from the LLM registry (`usage.billing.<provider>.*`): `mode` (`bundled_plan` — a flat subscription whose spend is not billed per request, `api_metered` — billed per request, `free_local` — no charge), `plan` (a free-text label), `monthly_price` (the plan's monthly price, 0 for free plans), `currency` (a 3-letter code), `reset_weekday` + `reset_time` (the allowance's reset, when the provider says one), and `allowance_unit` (`percent`, `requests` or `tokens`). Every field is optional and independent; a blank string or `0` clears one back to unset. A `bundled_plan` profile is what makes a cost-report row carry the **“bundled plan — API-equivalent, not billed”** label and its point figures (the API-equivalent money amount, the usage points it represents, and its share of the monthly price); the usage calculator reads its point value (per point, per week) from the same entry. Providers without a profile read back as the assumed default — an ordinary estimate, no invented figure. Operator-facing setup and reading guidance: [howto/usage-calculator.md](howto/usage-calculator.md#the-billing-profile-usagebilling) and [howto/cost-report.md](howto/cost-report.md#the-billing-profiles). Reference: [api/cost.md](api/cost.md#billing-profiles-usagebilling).

#### Detection filters

Prompt patterns + completion patterns the daemon scans tmux output for. **Prompt patterns** trigger `WaitingInput` when matched (e.g. `❯`, `$ `). **Completion patterns** trigger `Complete` (e.g. `DATAWATCH_COMPLETE:`). Per-deployment overrides; the global defaults work for most setups.

#### Compute Nodes

The Compute Node registry — hardware or remote endpoints that run LLM inference. Each node is one entry; LLMs reference nodes by name for failover routing.

##### Compute node TLS verification

How the daemon checks the certificate of an `https://` node: by default against the system roots, optionally against a private CA file (`tls_ca_file`) or a pinned SHA-256 certificate fingerprint (`tls_cert_fingerprint`), or not at all when `tls_insecure_skip_verify` is set, which is not recommended because anyone on the network path can then impersonate the node.

The settings apply to nodes whose address starts with `https://`; a plain `http://` node has no certificate to check, and the three keys do nothing for it. They are three keys on the node record (REST `tls_ca_file`, `tls_cert_fingerprint` and `tls_insecure_skip_verify` on `GET`, `POST` and `PUT /api/compute/nodes`; the same names in `compute_nodes` in the YAML config, the MCP add and update tools, the CLI flags `--tls-ca-file`, `--tls-cert-fingerprint` and `--tls-insecure-skip-verify`, the comm channel `k=v` pairs and the PWA node form), and they give four ways to check a node:

| Setting on the node | What is checked |
|---|---|
| none of the three (default) | The certificate chain and the host name, against the system roots. Go reads the system roots from the operating system and honours the `SSL_CERT_FILE` and `SSL_CERT_DIR` environment variables of the daemon process; the daemon has no CA bundle setting of its own. |
| `tls_ca_file` | The chain and the host name, against the system roots plus the PEM certificates in that file. Use it when the node's certificate is signed by a private CA. |
| `tls_cert_fingerprint` | The SHA-256 fingerprint of the certificate the node presents (the leaf certificate) must equal the pinned value. Nothing else is checked: neither the chain nor the host name. Use it for a single self-signed certificate. |
| `tls_ca_file` and `tls_cert_fingerprint` together | Both: the chain and the host name must verify against the system roots plus the CA file, and the leaf certificate must match the pinned fingerprint. |
| `tls_insecure_skip_verify` | Nothing. The node's certificate is accepted whatever it is. Off by default. |

**Rules.** `tls_insecure_skip_verify` cannot be combined with `tls_ca_file` or `tls_cert_fingerprint`; choose verify, pin or skip. A node saved with the skip setting and either of the other two is refused (REST answers 400 with `compute node: tls_insecure_skip_verify cannot be combined with tls_ca_file or tls_cert_fingerprint (choose one: verify, pin or skip)`). Skipping is never inferred, so a self-signed node with none of the three settings fails verification instead of being trusted. `tls_ca_file` is a path on the machine that runs the daemon; it is trimmed of surrounding spaces, and a file that cannot be read, or that holds no PEM certificate, is reported when the node is next probed, not when it is saved. The probe builds its client for a node once and reuses it until one of the node's three TLS settings changes (or the daemon restarts), so after replacing the contents of a CA file under the same path, save the node again or restart the daemon.

**Fingerprint format.** `tls_cert_fingerprint` is the SHA-256 of the node's DER-encoded leaf certificate: 64 hexadecimal digits. Upper or lower case, with or without colons or spaces, are all accepted and are stored as 64 lowercase digits without separators. Any other length, or a non-hex character, is refused (`compute node: tls_cert_fingerprint must be 64 hex chars (SHA-256), got 12`, or `compute node: tls_cert_fingerprint must be hex (SHA-256)`). Read the value from the certificate file on the node with `openssl x509 -in node.pem -noout -fingerprint -sha256`, or from the live server with `openssl s_client -connect gpu-box:11434 </dev/null 2>/dev/null | openssl x509 -noout -fingerprint -sha256`. Both print `sha256 Fingerprint=AB:CD:...`; paste the part after the `=`. A certificate that is renewed has a new fingerprint, so a pinned node needs the new value after every renewal.

**Supervisor watch probe.** The supervisor watch reads which models a node has resident with `GET /api/ps` on the node's address. That probe verifies the node's certificate by the settings above. When the check fails, the probe's result for that node is unknown and the watch raises no finding for it (a check that cannot tell reports nothing); the error names the fix: `node <name> certificate not trusted: ...: set tls_ca_file or tls_cert_fingerprint on the node, or tls_insecure_skip_verify to turn verification off for it`. A pinned node whose leaf has changed fails the same way, with a `fingerprint mismatch` message. The node usage probes verify by default too, with the same opt-out. Other node endpoints are outside this verification: the connectivity probe when a node is added or updated, and the node monitoring and models handlers, still do not check certificates.

**Audit and visibility.** A node with `tls_insecure_skip_verify` set is marked in four places:

- the node record carries `tls_insecure_skip_verify: true` on every read;
- the PWA node row shows a red **TLS verification OFF** badge (a pinned fingerprint or a CA file show a quiet *pinned* or *CA file* badge instead, and a default node shows none);
- the audit log gets an entry with the action `compute_node_tls_verification_off` when a node is saved with the setting (the entry's `source` is `compute_node_add` or `compute_node_update`), when the daemon starts with a configured node that has it (`source` is `config_seed`), and the first time the supervisor watch probes such a node after the daemon starts or after the node's TLS settings change (`source` is `supervisor_watch_probe`, once per change, not once per probe). Query them with `datawatch audit --action compute_node_tls_verification_off`;
- a supervisor watch finding raised for that node ends with `; certificate verification is turned off for this node`.

Every `compute_node_add` and `compute_node_update` audit entry also records whether the node has the skip setting, a CA file and a pinned fingerprint, as three booleans (`tls_insecure_skip_verify`, `tls_ca_file_set`, `tls_cert_fingerprint_set`); the CA path and the fingerprint themselves are never written.

Walkthrough with commands: [`howto/compute-nodes.md`](howto/compute-nodes.md#certificates-for-self-signed-and-private-ca-nodes).

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

#### Push Notifications (v8.2.0)

UnifiedPush + ntfy registration and fan-out. The card shows:

- **Registration status** — whether this device/browser has a registered push endpoint.
- **Register** button — calls the browser Push API, then POSTs to `POST /api/push/register` with the subscription `{endpoint, keys: {p256dh, auth}}`. Returns a registration `id`.
- **Test** button — fires `POST /api/push/notify` to send a test message to all registered endpoints.
- **Unregister** button — calls `DELETE /api/push/unregister` with the stored `id`.

CLI: `datawatch push list | test [--id <id>] [--message <m>] | unregister [--id <id>|--endpoint <url>]`.

UnifiedPush auto-discovery: `GET /.well-known/unifiedpush` returns `{"version":1,"unifiedpush":{"gateway":"/api/push/notify"}}`.

**`session_state_changed` lifecycle events (v8.10.3):** The daemon publishes a push event to topic `session-<fullID>` on every non-oscillation, non-waiting-input state transition. Payload `extras` contains `{type: "session_state_changed", old_state, new_state, task, short_summary}`. Allows mobile apps and subscribers to display contextual notifications (e.g. "Session foo — complete").

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

Per-discussion memory namespaces that are durable, federated, and conflict-aware. Each discussion scope is keyed by an operator-chosen discussion ID (1 to 128 characters, using only ASCII letters, digits, dot, underscore and hyphen (`.` and `..` alone are rejected); anything else gets HTTP 400 `invalid discussion id` on the scope, sync and file-service routes) and backed by a write-ahead log (WAL) at `~/.datawatch/discussions/<id>/wal.jsonl`.

**Scope name:** `ScopeDiscussion`

**Resolution tuple:** `(projectDir="", role="discussion/<id>", sessionID="")`

**Card:** Settings → General → Discussion Scopes. Lists all known discussion IDs; provides a New Discussion form and per-discussion Recall + Participants controls.

**Sync behavior:** When participant peers are set on a discussion, every successful write triggers an async fan-out push to each participant via `POST /api/memory/discussion/{id}/receive` (not `/api/push`, which is the ntfy topic route and does not apply the discussion ID rules). Each WAL entry carries `origin_peer` and `origin_wal_seq` fields; a peer that receives a sync push skips re-fanning any entry it originally produced (loop prevention).

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

Path traversal (`..` in a discussion ID) is rejected with 400. Because an accepted id can never carry a path separator, the folder path built from it always resolves as a direct child of the discussion base folder — a stored discussion cannot be addressed outside it.

See [`howto/discussion-scopes.md`](howto/discussion-scopes.md).

#### Notifications

Per-channel preference for daemon-emitted events: state changes, needs-input, rate-limit hits, autonomous step approvals. Off by default for chatty events; on for needs-input.

### Settings — Automate

Automaton-related cards.

- **Orchestrator** — multi-graph PRD-DAG executor. Approve / hold / cancel automated runs from this card. The **Dashboard nav button** in the bottom navigation is only shown when `autonomous.enabled: true` in `datawatch.yaml` — keeping the nav clean for operators not using Automata. **Verifier diff grounding** (v8.16.0): after each worker session completes, the verifier receives the actual `git diff` of changes alongside the task spec, so verification is grounded in code rather than spec text alone. Configurable via `autonomous.verifier_diff_max_bytes` (0 = 8 KB cap). When the project directory has no git history or the task was dispatched to a remote cluster node, the verifier falls back to spec-only verification without error. **Quality gates** (v8.17.0): the executor captures a test baseline before the first task runs, then re-runs the same test command after each task completes. When tests regress the task is marked failed and enters the auto-fix retry cycle; when `block_on_regression: false` the result is recorded but execution continues. Gate config can be set globally via `autonomous.default_quality_gates.*` or per-PRD via the `quality_gates` field on `POST /api/autonomous/prds`. `Task.quality_gate_result` carries the pass/fail counts and regression flag for each completed task. **Prompt injection hardening** (v8.18.0): all three LLM call sites (`decomposeFn`, `autonomousVerify`, `autonomousGuardrail`) prefix their prompts with a security preamble and wrap user-supplied content in `<user_data>` XML tags to prevent user-controlled text from overriding system instructions. An optional input scanner (`autonomous.injection_guard: true`) checks user-supplied PRD/task specs for 11 known injection phrases at the API boundary; findings are always logged. Set `autonomous.block_on_injection: true` to reject requests with HTTP 400 instead of warning-only. The scanner is exposed as `ScanForInjection(text string) []string` in `internal/autonomous/security.go`.
- **Identity / Telos** — same content as Settings → General → Operator identity, surfaced here too because Telos drives autonomous prioritization.
- **Algorithm Mode** — PAI's 7-phase per-session harness (Observe → Orient → Decide → Act → Measure → Learn → Improve). This card lists active sessions, current phase, captured output per gate. CLI: `datawatch algorithm {start,advance,edit,abort,reset,measure}`.
- **Evals** — rubric-based grading suites. Default suite types: `string_match`, `regex_match`, `binary_test`, `llm_rubric`. Run a suite from this card; results land in `~/.datawatch/evals/runs/`. Used by Algorithm Mode's Measure phase if configured.
- **Council Mode** — multi-persona debate. 12 default personas (security-skeptic, ux-advocate, perf-hawk, simplicity-advocate, ops-realist, contrarian, platform-engineer, network-engineer, data-architect, privacy, hacker, app-hacker). Each run is **async** by default: `POST /api/council/run` returns `{id, events_path}` immediately; subscribe to `GET /api/council/runs/{id}/events` for SSE streaming as each persona responds round-by-round. The PWA shows collapsible live-watch cards per run. Cancel with `POST /api/council/runs/{id}/cancel`. Milestone messages (run started / round complete / consensus reached) push to all configured comm channels; `council.comm_firehose: true` also sends per-persona response previews. Config: `council.llm_ref` (which LLM to use), `council.max_parallel` (concurrent personas per round, default 2). **Image critique:** `POST /api/council/run` accepts optional `image_path` — the file is described by the vision service and the description is prepended to the proposal before all personas receive it (requires `vision.enabled: true`). Use case: submit an architecture diagram or UI mockup for multi-persona review. **AI persona wizard** (v6.22.3): the + Add Persona flow can draft a `system_prompt` via LLM — answer 5 interview questions; each answer has a Refine button; result is saved to `~/.datawatch/council/personas/<name>.yaml`. Re-interview any existing persona via the 🤖 button on its row. See [`howto/council-mode.md`](howto/council-mode.md).
- **Skill Registries** — git-backed PAI-format skill manifests. Connect a registry → browse → sync. Synced skills get copied into a session's `<projectDir>/.datawatch/skills/<name>/` at spawn time when listed in the session's Skills field.
- **Type Registry** — register custom Automaton types beyond the built-in set, each with an `id`, a display `label`, an optional `description`, and a color badge. `GET /api/autonomous/types` lists every type (built-ins first, flagged with a "built-in" chip, then custom ones); `POST /api/autonomous/types` registers or updates one. Custom types appear everywhere a built-in type does — the launch wizard's type picker, the Automaton list's filter chips, and the detail view's type badge.
- **Triage** — the stuck-Automaton record and its alert. Every Automaton that is failed or blocked has a record: the blocked or failed task and its error, the time it first entered that state, the end reason of its last live session (the `ended_sessions` decision note if the task's own field is empty), and the four retry budgets. Once the Automaton has sat for `autonomous.automation.triage_alert_minutes` (default 30, `0` = off, negative refused at load and on every surface) the coordinator raises one triage alert per episode, named on every channel. Read on REST (`GET /api/autonomous/triage`, with `automation_id` / `prd_id` filters), MCP (`autonomous_triage`), CLI (`datawatch autonomous triage`), the comm channel (`autonomous triage [automation-id]`), and the Automation / Automaton detail views of the PWA.
- **Automaton uncommitted-work policy** (`autonomous.automation.uncommitted_work_policy`) — what happens when a task finishes with its work not committed: `hold` (default; the task is held with the files and a question to commit, discard or retry), `warn` (record and continue) or `off` (no check). Select in the **Uncommitted-work check** group; the group's help link opens [Uncommitted-work check](#uncommitted-work-check). Also `datawatch config set`, `PUT /api/config`, comm `configure`, and MCP `config_set`.
- **Automaton uncommitted-work scope** (`autonomous.automation.uncommitted_work_scope`) — which files the per-task check counts: `task_files` (default; the files the task changed) or `worktree` (every uncommitted file). The check before an Automaton is reported finished always covers the whole worktree. Same group and same ways to set it.
- **Automaton protected paths** (`autonomous.automation.protected_paths`) — the paths an Automaton task may not change without a decision: files, directories (trailing slash) or globs, repository-relative. Unset or empty reads the default list (the project rules file, the supervisor rules, `CODEOWNERS` and `.github/workflows/`); an explicit list replaces the default entirely. Edit in the **Protected paths** group; the group's help link opens [Protected paths](#protected-paths). Also `datawatch config set`, `PUT /api/config`, comm `configure`, and MCP `config_set`.
- **Automaton protected-paths action** (`autonomous.automation.protected_paths_action`) — what happens when a task changes a protected path: `hold` (default; the task is held and one question offers approve, reject or change the spec), `warn` (record and continue), `block` (hold the task with no question) or `off` (no check). Same group and same ways to set it; see [Protected-paths action](#protected-paths-action).
- **Automaton rules review source** (`autonomous.automation.rules_review_source`) — where the rules review reads the project rules from: `base_commit` (default; the rules as committed at the start of the Automaton's work) or `worktree` (the rules file in the working directory). Same group and same ways to set it; see [Rules-review source](#rules-review-source).

#### Merged-member policy

`autonomous.automation.merged_member_policy` decides what an Automation does when one of its member Automata ends cancelled or completed while its work is already merged into the project's own default branch. The daemon proves the merge with read-only git queries; a branch that is not merged, or whose state cannot be read, always keeps the blocked outcome. The key has three values: `completed` (the default) treats the member as completed, so the Automation follows its on-complete edge; `manual` keeps the blocked outcome and adds the merge proof to the block reason, so the operator resolves it with resume or skip; `block` keeps the blocked outcome and runs no git check at all. A change applies to the next member that ends, with no restart. The Automation detail page lists each decision (member, branch, proving commit, policy and outcome) and the same decisions are on REST (`GET /api/automations/{id}/merged-members`), MCP, the CLI and the chat verbs. See *Merged member* in the glossary below.

#### Identity backups and restore

The identity is the one file the daemon injects into every session, so the daemon keeps copies of it and will not let an ordinary save wipe it. This section covers the setting that bounds the copies, the refusal of an empty write, and how to reset and restore from the PWA.

**Identity backups to keep** (`identity.backups_to_keep`) — how many backups of the identity the daemon keeps. The default is 10. A whole number of 1 or more; anything else is refused when saved. Each save of the identity first copies the file it is about to replace, then drops the oldest copies beyond this number. A change applies on the next save, with no restart. The backups live in the `identity-backups` folder next to `identity.yaml` in the data directory, one file per backup, named by the time it was made. Set it in Settings → Automata → Identity (the *Identity backups to keep* field, with its help link to this section), or with `PUT /api/config` and `{"identity.backups_to_keep": 10}`. It is for the operator who wants a longer trail to go back through, or a shorter one on a small disk.

**Empty-write refusal** — a save that would replace a saved identity with an empty one (no role, no goals, no projects, no values, no focus and no notes) is refused. The daemon answers HTTP 409 with the code `identity_wipe_refused`, a sentence saying so and the fields it kept, writes nothing and records the refusal in the audit log. The PWA shows that sentence as an error message and leaves what you typed in the form (or the wizard) so you can fix it. To clear the identity on purpose, use Reset.

**Reset and restore from the PWA** — the Identity card in Settings → Automata has four buttons. **Save** writes the form. **Revert** reloads the form from the daemon and drops unsaved typing. **Reset** is the explicit clear: after a confirmation that says a backup is kept and Restore can undo it, it empties the saved identity (`POST /api/identity/reset`). **Restore** lists the backups, newest first, each with the time it was made in your local time, its role and how many fields it filled in; choosing one and confirming brings it back (`POST /api/identity/restore`). A restore first backs up the identity it replaces, so it can itself be undone. When no backup exists yet the list says so. The same actions are on the CLI (`datawatch identity reset`, `backups` and `restore`) and MCP (`reset_identity`, `identity_backups`, `restore_identity`). The chat channel lists the backups and restores one (`identity backups`, `identity restore <name|latest>`) but has no reset verb, so one mistyped line cannot empty the identity. See [howto/identity-and-telos](howto/identity-and-telos.md).

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

#### MCP Channel Bridge Diagnostics (v8.10.16)

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

#### App-only settings (Android/iOS)

Three Settings → About cards exist only in the mobile apps, with no server-side config behind them — purely client-side device preferences, nothing to configure here on the daemon:

- **Security (Face ID / biometric app lock)** — requires the user to re-authenticate with the device's biometric sensor (Face ID, Touch ID, Android fingerprint/face unlock) before the app's content is shown, each time it's foregrounded. A device-local gate on top of whatever bearer-token auth the daemon itself requires — it does not change what the daemon accepts over the network.
- **Theme** — the app's own light/dark/system appearance setting, independent of the PWA's `localStorage['cs_theme']` toggle (each client remembers its own preference; they are not synced).
- **Language** — the app's own UI locale picker, independent of the PWA's locale bundle selection. Both pull from the same translated string set (see [Supported Languages](messaging-backends.md#supported-languages)), but each client's choice is local to that client.

---

## Concepts & Glossary

**Rate-limit reset out of reach** — the CodeQL alert gate's state when GitHub's core rate limit resets later than the wait budget that is left (`WAIT_MAX_SECONDS` minus the time already waited). It applies to both steps: `wait-analyses` and `open-alerts`. The gate does not sleep toward a reset it cannot reach: it prints a `Rate limited` error line (naming the reset epoch when one is known) and exits 1 at once. A reset inside the budget is waited for and retried; a rate limit with no reset time is retried on the normal interval until the budget runs out. Either way the run is a rate limit, not a missing analysis, and `open-alerts` never reports a zero-alert result from it. See [`howto/codeql-local-scan.md`](howto/codeql-local-scan.md#red-check-from-a-rate-limit).


Key terms used across the docs, API, and hook payloads.

**Tag selection** — the tags chosen in an Automata list filter, and how they are checked. An object's derived system tags (for example `project:datawatch`) and its assigned tag IDs are pooled; match ANY needs at least one selected tag in the pool, match ALL needs every one, and a selection with no tags matches every object. The check is unchanged by the code-scanning fix, which only removed a computed size hint on the internal lookup set. See [howto/automata-tags.md](howto/automata-tags.md).

**Merge pending** — the state of a finished Automaton whose work branch (`automaton/<id>`) still has commits that are not on `main`, where the check is sure of it: both tests agree that the commits are missing. A branch where the two tests disagree is not merge pending; it is *needs a look*, a separate state. The work is done but has not been merged, so it can look complete while it exists only on its own branch. The merge-pending check works this out with read-only git queries and never merges, deletes or pushes. It is controlled by `autonomous.automation.merge_pending_mode` (`off` by default, `shadow` to record only, `on` to record and alert) and `autonomous.automation.merge_pending_alert_minutes` (30 by default; `0` turns the alert off). With the check `on`, one alert is raised per episode after the branch has waited that long, and merging the branch clears it. The other states are merged, empty (nothing ahead of `main`), needs a look and unknown. The PWA shows a badge on a finished Automaton and on its Automation card; the same state is on REST, MCP, the CLI and the chat verbs as `autonomous merge`. See [operations.md](operations.md) (*Merge-pending check*) and [flow/automation-merge-pending-flow.md](flow/automation-merge-pending-flow.md).

**Needs a look** — the merge-pending state for a finished Automaton whose branch the check cannot call merged or unmerged, because the two tests it uses disagree: a commit's patch and its subject line are matched against `main`, and one matches while the other does not. The usual cause is a commit that was merged but changed on the way in, for example by a conflict fix, so its subject line is on `main` and its patch is not; a reworded subject over an identical patch gives the same result. It is a prompt to compare the branch with `main` by hand, not a verdict. It raises no alert, writes no merge-pending record, and the branch is held (never listed as safe) in the safe-to-delete report. The PWA shows it as **Automaton needs a look**.

**Merged member** — a member Automaton of an Automation that ended cancelled or completed while its work branch (`automaton/<id>`) was already merged into the project's own default branch, so ending it would otherwise block the Automation although the work is on the default branch. The daemon proves the merge with read-only git queries and records one *merged-member decision* in the Automation's decision log: the member, its branch, the default branch it was proved merged into, the proving commit, the policy in force and the outcome (*advanced* when the Automation moved on, *blocked* when it stayed for the operator). What happens is set by `autonomous.automation.merged_member_policy` (`completed` by default, `manual` or `block`; see *Merged-member policy* under Settings — Automate). A member reopened by the operator resumes the chain. The Automation detail page in the PWA shows the decisions, and the same list is on REST, MCP, the CLI and the chat verbs.

**Safe-to-delete report** — a read-only list of which Automata work branches and worktrees can be cleaned up. It looks at every local `automaton/*` branch in the worktree base repository and sorts each into **safe** (all commits on `main`, or none ahead of it, and no uncommitted changes in its worktree) or **held** (merge pending, needs a look, not comparable, or a worktree with uncommitted changes). It runs git when it is read, but at most once per 60-second window: the daemon caches each repository's result in memory for 60 seconds, so every read within that lifetime comes back at once, a read past it still comes back at once, marked **stale**, with `computed_at` naming when the stored result was made, and a single background refresh, bounded at 60 seconds and keeping the last good result if it fails, keeps it current. It works whatever the merge-pending mode is, and **never deletes anything**: removing a branch or worktree is a separate step the operator checks and carries out. Read it with `GET /api/autonomous/safe-to-delete`, MCP `autonomous_safe_to_delete`, `datawatch autonomous safe-to-delete`, the chat verb `autonomous safe-to-delete`, or the card on the Automation Housekeeping sub-tab. See [operations.md](operations.md) (*Merge-pending check*).

**Automaton worktree cleanup** — the daemon's removal of what a finished Automaton leaves behind, plus a guard on the disks that hold it. An Automaton running in worktree mode works on its own branch (`automaton/<id>`) in a worktree under the worktree base repository, and its worker sessions leave scratch files in the daemon's temporary directory. Two passes clean up: the **worktrees** pass removes a completed or cancelled Automaton's work branch and worktree, and the **temp files** pass removes the daemon's own scratch files for sessions that have ended. A pass removes an entry only when it is certain nothing is lost: the Automaton is finished, no live session or lease uses the worktree, the worktree has no uncommitted changes, every commit is already on the project's default branch (taken from the Automaton's own base branch, or the branch the remote records as default, never an assumed name) and the work is older than the retention window. Removal never uses `--force` or `-D`. The mode is `autonomous.automation.cleanup_mode`: `off`, `report` (the default: record what would be removed, remove nothing) or `on`; `cleanup_retention_hours` (24), `cleanup_max_removals_per_pass` (10) and `disk_alert_percent` (85) set the rest. Each pass counts what its owner released at the end of the work (**owner**) separately from what the periodic sweep had to catch (**net**); the aim is for the net to find nothing. When the disk holding the worktree base repository or the one holding the temporary directory reaches `disk_alert_percent`, one warning alert is raised, at most once an hour, in every mode. Read the status with `GET /api/autonomous/cleanup`, MCP `autonomous_cleanup_status`, `datawatch autonomous cleanup`, the chat verb `autonomous cleanup`, or the Cleanup card on the Automation Housekeeping sub-tab. See [howto/automaton-worktree-cleanup.md](howto/automaton-worktree-cleanup.md) and [operations.md](operations.md) (*Automaton worktree cleanup*).

**Uncommitted-work check** — the daemon's guard that a task is complete only when its work is committed. An Automaton that commits its own work (it has git settings and a worktree branch or declared write directories) is checked at two moments. When a task finishes, the daemon commits the files the task declared, by name, with a `task(<short id>): <title>` message, and if the commit failed or files are still uncommitted the task is **held**: parked as blocked with the reason and the files, and one question is raised offering **commit**, **discard** or **retry**. Before an Automaton is reported finished, the whole worktree is checked once more; under the default policy a dirty worktree blocks the finished state, so the Automaton is never listed as merge ready with work that would not ship. Files outside a task's declared files are listed and never committed automatically. At that before-finished check, an artefact the daemon itself generated and that a person did not edit (a generated artefact that still carries its marker line, has no `<!-- edited -->` line, and whose body still matches the hash the daemon recorded in its sidecar when it last wrote it), together with its sidecar file, is not counted as uncommitted work, so an Automaton whose only uncommitted work is what the daemon generated reaches finished and merge ready as usual; an artefact a person edited still holds, like any other file, and the per-task check is unchanged. The policy is `autonomous.automation.uncommitted_work_policy` (`hold`, the default, `warn` or `off`) and the scope of the per-task check is `autonomous.automation.uncommitted_work_scope` (`task_files`, the default, or `worktree`). Read it with `GET /api/autonomous/uncommitted-work`, MCP `autonomous_uncommitted_work`, `datawatch autonomous uncommitted-work`, the chat verb `autonomous uncommitted-work`, or the Automata uncommitted work card on the Automation Housekeeping sub-tab. See [Uncommitted-work check](#uncommitted-work-check), [howto/automaton-uncommitted-work.md](howto/automaton-uncommitted-work.md) and [operations.md](operations.md) (*Automaton uncommitted-work check*).

**Protected paths** — the files and directories an Automaton task may not change without a decision from the operator, and the check that enforces it. After a worker attempt finishes, the daemon compares the task's directory with the commit the work started from; if a protected path changed and the Automaton is not authorised for it, the task is **held** (the default: parked as blocked, with one question offering **Approve this change**, **Reject it** or **Change the spec**), **warned** about (recorded, the task goes on) or **blocked** (parked with no question). The default list is the project rules file, the supervisor rules, `CODEOWNERS` and the workflow files. The same change also protects the judge: with the default rules review source the rules review reads the rules from the base commit, so a task cannot rewrite the rubric it is judged by. The list, action and source are `autonomous.automation.protected_paths`, `autonomous.automation.protected_paths_action` and `autonomous.automation.rules_review_source`. One Automaton can be authorised for some paths in advance. Read it with `GET /api/autonomous/protected-paths`, MCP `autonomous_protected_paths`, `datawatch autonomous protected-paths`, the chat verb `autonomous protected-paths`, or the Automata protected paths card on the Automation Overview sub-tab. See [Protected paths](#protected-paths), [howto/automaton-protected-paths.md](howto/automaton-protected-paths.md) and [operations.md](operations.md) (*Automaton protected paths*).

**Overlap warning** — a note, made when you create an Automaton, that an existing Automaton, a plans-registry row or an unmerged Automaton branch already looks like the new plan. It gives the kind, what it points to, a score from 0 to 1 and a hint for reading it. It is advice only: the create always succeeds, and nothing the warning points at is changed. The check runs in one of three modes (`off`, `shadow` or `on`, default `shadow`); only `on` adds the warnings to the create reply, and `shadow` records them in the audit log instead. See [howto/automaton-search-before-create](howto/automaton-search-before-create.md).

**Failure escalation policy** — the rule for a task that keeps failing. After each failed attempt the next attempt can run on the next worker on a ladder (for example a local model, then a stronger Claude model that reads the earlier failure reports). After a set number of failed attempts (default 3, settable to 4 or more) the task is held for operator review instead of being marked failed. Attempt cost sits alongside it: the per-kind **first-attempt worker** below keeps the cheap kinds' first attempts on a cheaper worker, so a failed task's worst case is cheap starts plus the strong ones rather than up to three full cold starts, and a first-try success is unchanged if you set no pair. It is set once for the whole daemon, and an Automaton or a single task can override it. Each setting resolves on its own, task first, then Automaton, then the daemon default. A failure no other attempt can fix is held on its own before the limit is reached — see **No-retry hold** below. Turn it off to get the old behaviour: retry up to the auto-fix retry count, then mark the task failed, and the no-retry check does not run at all. See [howto/failure-escalation](howto/failure-escalation.md).

**First-attempt worker (attempt cost)** — the cheap tier in front of worker choice: `autonomous.first_attempt` maps a task kind (`implementation`, `docs`, `note`, `final_gate`) to the backend and model that kind's first attempts run on, so the strong worker only starts once the task has actually failed. `autonomous.first_attempt_escalate_after_failures` (default 1, range 0–20) is the failure count after which the pair stops applying and the normal resolution — including the failure-escalation ladder — takes over; a task, story, PRD or Automaton pin, or a burn-guard route, always beats the pair. Daemon-wide only. Each recorded attempt carries its reason: \"first attempt for &lt;kind&gt; tasks\", \"escalated after N failures\", \"escalation ladder step\", \"operator pinned\" or \"default choice\", shown on the review entry of a held task. One shared validator refuses an unknown kind, a model-less entry for `pi`, `pi-prompt` or `ollama`, and out-of-range numbers, naming the key. See [howto/failure-escalation](howto/failure-escalation.md) → Choosing the first-attempt worker.

**Retry resume (attempt cost)** — the piece that keeps a failed attempt's retry from costing another full cold start. When a task's attempt failed verification (stalls, start and infra failures never set one up), the worker's session is still open and the attempt's backend can resume — today only `claude-code`, `autonomous.retry_resume_max_context_tokens` is above 0 (0 = fresh every time), and the session's context is at or under that cap (an unknown size passes), the next attempt resumes that session: it spawns nothing, keeps the attempt's worktree, and receives one line — a retry marker plus the same failure summary a fresh retry's prompt would carry, capped at 4,000 characters. A missing send path, or a failed send, falls back to a fresh session. The decision is `decideRetryResume` (`internal/autonomous/retry_resume_decision.go`), wired into the retry path by `tryRetryResume` (`internal/autonomous/retry_resume_flow.go`). Every attempt record after the first carries `resumed` and `resume_reason` (e.g. `resumed previous session`, `resume disabled by setting; starting a fresh attempt`, `context above the limit; starting a fresh attempt`), and a held task's review entry shows those for the last attempt as `last_resumed` / `last_resume_reason`. The failure flow itself — attempt order, `max_failures`, the ladder, the hold and the alert — is unchanged; only the cost of the attempt is lower. See [howto/failure-escalation](howto/failure-escalation.md) → Retry resume: continuing the previous session.

**No-retry hold** — the review hold for a failure that no other attempt can fix, raised while the **failure escalation policy** is on. When the verifier rejects an attempt and any of three conditions holds — the task failed on *no files changed* and writes nothing (read-only, or no planned files); its verification script is missing from the worktree; or the same failure summary, ignoring case and surrounding whitespace, has already failed twice — the failed attempt is recorded carrying the operator-readable reason (`no_retry_reason` on the attempt, `no_retry_reason` on the held-task entry of the status view, `FailureReason` source `no-retry-hold`), the task is parked in `review_needed` at once with no further attempt, and one error-level alert titled *Automaton task needs review (no retry)* is raised, naming the Automaton and the task, the reason, the last worker and the ways to act: change the task so it can be verified (give it files to write, a verification script, or a changeable spec), or cancel it. Its review entry leads with the reason. It is resolved with the same actions as a retry-limit hold — retry (useful only with a changed spec, worker or scope; an unchanged retry re-enters the hold on the same basis), edit spec and retry, change worker, narrow scope, or cancel. With `enabled: false` the check does not run: every failure takes the ordinary retry, then fail path. See [howto/failure-escalation](howto/failure-escalation.md) → No-retry failures.
**First attempt** — a task's first try, before it has failed. Its kind (`implementation`, `docs`, `note` or `final_gate`) decides which worker it runs on, through `autonomous.first_attempt`. The cheap worker for that kind is used only on the first attempt; an empty entry keeps today's choice, and an explicit worker or LLM set on the task, its story, the Automaton or by the burn guard always wins. See [howto/failed-attempt-cost](howto/failed-attempt-cost.md).

**Escalate after failures** — the failed-attempt count after which the first-attempt worker stops applying and the normal choice takes over, including the failure-escalation ladder. It is `autonomous.first_attempt_escalate_after_failures`: default `1`, so the first failure escalates; range `0` to `20`, where `0` behaves like `1`. See [howto/failed-attempt-cost](howto/failed-attempt-cost.md).

**Resume in the same session** — a retry that continues the failed attempt's worker session, with the failure summary as one line, instead of starting a fresh session with the full context. It happens only when the attempt failed verification, the session is still open, the backend can resume (today `claude-code` only), `autonomous.retry_resume_max_context_tokens` is above `0` (default `150000`) and the session's context is at or under that limit. Otherwise the retry starts fresh, and the attempt record says why. See [howto/failed-attempt-cost](howto/failed-attempt-cost.md#retrying-in-the-same-session-or-starting-a-new-one).

**No-retry reason** — the operator-readable reason recorded on a failed attempt when its failure goes to review without another attempt: no files changed for a task that writes nothing, a missing verification script, or the same failure summary failed twice. It is stored as `no_retry_reason` on the attempt and shown on the held task's review entry. Empty on an ordinary failed attempt that is retried. See [howto/failed-attempt-cost](howto/failed-attempt-cost.md#failures-that-go-to-review-at-once).

**Attempt record** — the history of one task's failed, stalled or cancelled attempts, kept on the task and never removed. Each entry names the worker and why it was chosen (`backend_reason`), whether the attempt resumed the previous session and why (`resumed`, `resume_reason`), the failure category and summary, what the verifier said, the files changed, and any `no_retry_reason`. It is read through the Automaton's task review and `datawatch autonomous status`. See [howto/failed-attempt-cost](howto/failed-attempt-cost.md#reading-the-attempt-record).

**Usage calculator** — the advisor that reads the weekly allowance readings you type and says whether you are within or over budget for the week and what each lane count costs against the rest of it. It follows one of three policies (use it all, pace to the week, run flat out), which the operator chooses. It only advises and never stops or slows a lane. See [`howto/usage-calculator.md`](howto/usage-calculator.md).

**Burn guard** — the guard that compares the projected weekly burn of the Claude sessions with the pace line of the usage calculator and, when the projection runs over the line plus a margin, steps down a ladder of admission changes: lower the Claude inflight cap, route new Claude work to `pi-prompt`, hold new Claude work. It steps back up, one rung per interval, when the projection is back under the line. Its projection starts from the newest usage reading, carries the tokens used since it forward with the cache prices scaled by a calibration ratio, and blends the burn measured between the last two readings with the token burn. The guard projection is compared with the readings-only advice projection; `max_projection_gap_points` (default 25, 0 to 100; 0 never compares) sets the largest gap allowed before the guard decides on the lower of the two. It changes admission only and never ends a running session. Its settings are under `usage.burn_guard`, including the per-kind effort ceiling and the Claude retry limit. `ask_on_change` (false by default) picks how the guard reaches out about a change: alerts only — one alert per change and no question — or, when true, at most one open question per change (a new change withdraws the open question, and questions the previous run left open that the ladder no longer supports are withdrawn at daemon start). See [`howto/burn-guard.md`](howto/burn-guard.md).

**Host load check** — the daemon's guard against an overloaded host. Every minute it reads the 1-minute load average, and once it has stayed above `multiple` (default 1.5) times the core count for `minutes` (default 10) consecutive minutes, it raises one warning alert for that episode naming the busiest process groups (5 by default) and whether each belongs to the worktree or scratch folder of a running or an ended Automaton session — proven by folder evidence, never by name. The episode ends when the load returns under the limit and the next breach alerts again. `autonomous.host_load_alert.mode` picks `alert` (the default: report only), `reap` (also ends processes of **ended** sessions that the session-end reaper's evidence rule proves, by exact pid; a running session is never touched) or `off`. Note that `reap` acts only when the check raises its alert — at the first sustained breach after it was switched on — and the load check's reap writes no record: no audit entry, no `survivors` count, no task decision, so a `session_end_reap` record always refers to the session-end pass only, and a process that ignores the end signal survives it without being counted; verify leftovers by hand. See [howto/host-load-check.md](howto/host-load-check.md), [howto/automata-orphan-reaping.md](howto/automata-orphan-reaping.md) and [operations.md](operations.md) (*Host load alert*).

**Bounded load step (rule)** — the planning rule a task that asks for CPU stress — a busy loop, a load or soak test, any command that pins the cores — must satisfy before it runs: run it niced (`nice -n 19`), cap the workers at a quarter of the cores (`nproc/4`, minimum one), give it a hard timeout, trap `EXIT INT TERM` and clean up its children, and never let it outlive its owning task. The wording is one source, carried verbatim into the built-in `bounded_load_step` standard task (daemon-mode by default, so the story's note task completes without a model session), the rules block appended to the decompose prompt, and a warn-severity `unbounded_load_step` finding in the decompose lint when a spec asks for an unbounded load step. Its role is to keep the session-end reap and the host load check from ever having to finish a task's load after it — a bounded step dies with the task that started it. See [howto/automata-orphan-reaping.md](howto/automata-orphan-reaping.md).

**review-needed (task status)** — a task held after its failed attempts reached the escalation limit. The status is `review_needed` in the API and reads "Held for review" in the web UI. It is not a failure and not finished: the task keeps its attempts and a report, the Automaton shows as blocked, and one error alert is raised. The operator retries it (optionally with a changed spec, worker or file list) or cancels it, from the web UI, the CLI, the REST API, MCP or the comm channel.

**Operator console** — the dashboard's one-glance "what needs my attention right now" summary: five sections (`questions_waiting`, `held_blocked`, `lanes`, `usage`, `ci_release`), each reporting a count, a one-line summary and the PWA anchor where the operator acts on it. It is a read roll-up of the question channel, the held/blocked triage states and uncommitted-work holds, lane fill and the capacity queue, the burn guard and usage projection, and the repo CI watch; it never writes anything. A section switched off, or whose subsystem is not running, reports `available: false` with a reason instead of failing the whole read. Switches: `dashboard.console.enabled` and the five `dashboard.console.show_*` keys, each defaulting to true while absent. Read it on REST (`GET /api/dashboard/console`), MCP (`dashboard_console_get`), CLI (`datawatch dashboard console`, or `--json` for the raw body), the chat verb `dashboard console`, or the five console cards on the PWA Dashboard view. See [howto/dashboard-operator-console.md](howto/dashboard-operator-console.md).

**Console card** — one of the five dashboard cards the operator console adds to the Dashboard view (Questions waiting, Held & blocked, Lanes, Usage, CI & release). A console card opens the PWA anchor of its console section; it carries a help link to the operator console how-to, and it disappears from the board and the Add-Card picker when `dashboard.console.enabled` is false (a stored layout is rewritten once to add the cards back when the console is switched on again). The console cards are separate from the feature cards (EKG, constellation, Gantt, …); see *Inactive card*.

**Inactive card** — a dashboard feature card that currently has no live data to draw from (no matching sessions, no usage readings, no releases to show). While `dashboard.hide_inactive_cards` is true (the default), inactive cards stay out of the grid and out of the Add-Card picker so the board only shows what is live; set the key to false to keep them visible and pin your layout. The operator console's sections are not feature cards — they are listed as above, with `available` and a reason when a section is off.

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

**Notification exclusion (`notify_exclude`)** — a top-level config list naming comm-channel backends (by each backend's own identifier — `signal`, `telegram`, `discord`, `slack`, `matrix`, `imap_mcp`, `ntfy`, `email`, `webhook`, `github_webhook`) to skip when broadcasting session-lifecycle bundled notifications (task started/waiting-for-input/completed/failed — the same "every comm channel gets the same message" broadcast every configured backend participates in by default). Checked fresh on every send, so a live config change applies without a restart. Only suppresses the *outbound* notification broadcast — a channel left enabled but listed here still handles inbound commands normally. Empty (default) excludes nothing, the behavior every channel has always had. Settings → Comms → Notifications in the PWA.

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

**Project rule** — an operator-defined check in the fourth scan
category (alongside the built-in SAST/secrets/dependency scanners),
configured under `autonomous.scan.project_rules` and evaluated by
`autonomous_prd_scan` / `POST /api/autonomous/prds/{id}/scan` like any
other scanner. Five types: `content` (a regex must/must-not match
within matching files), `consistency` (two files must agree on a
value — e.g. "these two files' version strings must match"),
`presence` (a file/pattern must exist), `parity` (built-in, checks
a story's declared parity surface against its parent plan's — ships
default-enabled as of v9.0.17, the only rule any operator
gets without configuring one), and `scope_drift` (built-in, v9.0.17 —
flags a task spec with code-creation language when
the parent PRD spec itself declares doc-only/no-code-changes work;
unlike `parity`, **not** default-enabled — a normal non-doc-only PRD
saying "Implement X" is expected work, not drift, so this needs an
explicit opt-in per the example in `docs/config-reference.yaml`). Each
rule also names a `granularity`
(`task_complete` / `story_complete` / `prd_complete` — which
completion checkpoint it's meant to run at; the checkpoints themselves
landed in v9.0.17) and an optional `action` (`file_upstream_issue`,
for gaps this repo's own PRD can't close alone). The parity and
scope_drift rules are both deliberately candidate detectors, not a
final verdict — "was there a stated exclusion reason"/"is this really
drift" is the `rules_grader_enabled` LLM judgment call's job, the same
grading pipeline every other scanner's findings already go through,
not a second mechanism. See
`docs/plans/2026-10-10-configurable-project-rules-enforcement.md`.

**Guided mode** — per-PRD setting (`guided_mode: true`,
`PRD.GuidedModeSource` internally) that pauses the executor before
every task and waits for an explicit `approve_task` call before it
runs. Declared since v6.2.0 but never actually implemented
until v9.0.17 — the set-side plumbing
existed across every surface for years with no executor code ever
reading it. Independent of the per-story approval gate
(`per_story_approval`); a PRD can use either, both, or neither. See
"Guided Mode and the Per-Task Approval Gate" in `docs/operations.md`.

**PRD git worktree** — an isolated `git worktree` + branch
(`automaton/<prd-id>-<slug>`) a PRD runs in instead of bare in the
daemon's own checkout, created automatically at `Run()` when
`autonomous.worktree_base_repo` is configured and the PRD has no
explicit `project_dir`/`project_profile`/`cluster_profile` (v9.0.19). Shares the base repo's `.git` object store —
cheap, filesystem/git-level isolation only, not process/network
isolation (that's `cluster_profile` dispatch's job). With
`PRD.Git.AutoPR: true`, a successfully-completed worktree-mode PRD
pushes the branch and opens a real PR (one per PRD, not per task),
then removes the local worktree (v9.0.20). A
`cluster_profile` PRD gets the equivalent `Git.AutoPR` behavior with no
worktree at all — the dispatched worker commits and pushes its own
branch itself (using its bootstrap-minted git token), reports it back
on completion, and the parent opens the PR from that branch directly
(v9.0.21). As of v9.0.22, that worker
creates and checks out the PRD's own dedicated `automaton/<prd-id>-<slug>`
branch fresh (instead of landing on whatever branch the dispatching
Project Profile names), closing the one gap between cluster mode and
worktree mode's own branch handling. See "PRD Git-Worktree Isolation +
Completion PR" in `docs/operations.md`.

An Automaton worker is only launched in a project directory that is a
registered worktree of the repository holding the repository root marker
files. If the directory is not registered, the launch is refused and an
alert naming the Automaton is raised, and a missing worktree directory is
no longer recreated as an empty repository. After the alert, repair the
directory, for example by re-adding it with `git worktree add`, and re-run
the Automaton.

**Automation** — a named group of Automata (PRDs) wired together as
members with per-outcome next-step edges (`on_complete` / `on_blocked` /
`on_failed`), starting at a single entry member and advancing a single
current member (the cursor) as members finish or fail. It is a distinct
tier *above* the Automaton: an Automaton is one autonomous PRD
(stories → tasks); an Automation is a group of Automata plus the edges
deciding which member runs next. For now it is stored data only — records
are CRUD-ed and persisted (`automations.jsonl`, managed via REST
`/api/automations` and MCP `automation_*` tools) but no execution loop
consumes the edges yet.

**Lifecycle event subscription** — a persisted rule that maps an Automaton
lifecycle event (PRD/task status changes, `task.session_started` /
`task.session_cleared`, `prd.updated`, and the opt-in warning kinds
`scope_warning` and `guardrail_warn`) to a delivery sink: a comm router,
a datawatch session (bridge `POST /send` preferred, `SendInput` fallback
tagged `lifecycle-sub`), a push topic, the in-app alert store, or a named
**pending events queue** (`mcp_queue` sink) that a client polls. Options:
`one_shot` (removed after first *successful* delivery — one retry, then
log + drop), `expires_at` TTL (pruned by the engine's janitor),
`coalesce` quiet-window batching for human-facing sinks. Manageable from
every surface — CLI `datawatch autonomous subscribe | unsubscribe |
subscriptions | subscription-update | subscription-test | events`, MCP
`autonomous_subscribe | autonomous_unsubscribe | autonomous_subscriptions |
autonomous_subscription_update | autonomous_subscription_test |
autonomous_events_pending` (proxying the daemon REST), comm verbs
(owner-scoped: the calling channel is both owner and sink target), REST
`/api/autonomous/subscriptions` (+ `PUT` and `/test` per id) and
`/api/autonomous/events`, and the **Subscriptions** tab on the PWA
Automaton detail view (list, add, edit, test, delete). Stored encrypted at
`<data_dir>/lifecycle_subscriptions.json`; the engine re-attaches live on
add/remove/update, so changes apply without a daemon restart. Replaces the
per-session polling-script pattern (watch for a new worker session, then
inject text).

**Pending events queue** — the bounded, in-memory, per-name list of
lifecycle events behind the `mcp_queue` sink, for clients that cannot
receive pushes (a generic MCP host, a script). The client polls with a
cursor — `autonomous_events_pending(queue, since_cursor, limit)` /
`GET /api/autonomous/events` — and passes the returned `next_cursor` back;
`dropped` is true when unread events were evicted. Each queue keeps at most
`autonomous.notify.pending_events_max` events (default 500) and is empty
after a daemon restart.

**Test-fire** — delivers one labelled `[test] ` event to a single
subscription's sink so the route can be checked without waiting for a real
event; it never types the action text and never consumes a one-shot. See
[howto/automaton-lifecycle-subscriptions.md](howto/automaton-lifecycle-subscriptions.md)
and
[flow/lifecycle-subscription-flow.md](flow/lifecycle-subscription-flow.md).

**Question** — a structured request for a decision that an Automaton (or a session working for one) raises to the operator through the daemon instead of blocking in a terminal. It has a label, the question text, optional numbered options and a recommendation, a `risk` (how costly a wrong answer is) and a `priority` (how urgent it is), and it stays open until answered or withdrawn. Raising one announces it on the PWA, in the active messaging groups and, for `priority: "high"`, as a warning alert. The whole channel is off until `autonomous.questions.enabled` is set. See [howto/operator-questions-notifications.md](howto/operator-questions-notifications.md).

**Answer** — the operator's reply to a question, stored append-only with the verbatim quote, who answered, which surface it came from and when. A later answer supersedes an earlier one without erasing it. Recording an answer fires a `question.answered` event (withdrawing fires `question.withdrawn`) that subscriptions can catch like any other Automaton lifecycle event. `GET /api/questions/{id}` shows the standing answer's summary, which is the default way for the asker to read it.

**Answer log** — the oldest-first, read-only record of every answer ever given to a question: which one stands, which were replaced, and the verbatim words of each. `GET /api/questions/{id}/answers` returns it, the PWA history view shows it under each answered question, and `datawatch question show` prints it. Nothing re-enters the open queue from the log; a changed mind is a new answer that names the one it supersedes, never an edit of an old one.

**Return-path line** — the opt-in way an answer reaches the asking session. A question raised with `return_path: true` gets, on its first answer, exactly one `DATAWATCH_ANSWER: {...}` line (a marker plus one line of JSON with the question id, answer id, chosen option, verbatim quote, actor and surface) typed into the session that asked. It is best-effort: a lost line costs one daemon warning and is never retried, because the answer is always in the store and the event. See [flow/input-required-flow.md](flow/input-required-flow.md).

**Operator decision** — a question that other work waits on, recorded on the question channel with an **id** (`q-` followed by at least 20 lowercase hex characters), a state (`open`, `answered`, `withdrawn` or `expired`) and, once answered, the operator's answer with the words quoted exactly as written. It is the same record as a question; what makes it a decision is that an Automaton task spec names its id. The read of all of them, newest first, is the decision line (`GET /api/autonomous/decisions`, MCP `autonomous_decisions`, `datawatch autonomous decisions`, the chat verb `autonomous decisions` and the Decisions card on an Automaton's detail page). See [howto/automaton-operator-decisions.md](howto/automaton-operator-decisions.md).

**Decision gate** — the check that keeps a task from starting while its spec names an operator decision that has no answer. In mode `on` the task is held with the status *Waiting for a decision* and a one-sentence reason, one warning alert is raised, and the task starts by itself once the decision is answered. In mode `shadow`, the default, nothing is held and the Automaton's decision log records once what would have been held. Two keys control it: `autonomous.questions.decision_gate` (on when absent) and `autonomous.questions.decision_gate_mode` (`shadow` or `on`; `shadow` when empty). See [operations](operations.md#decision-gate).

**Worker question marker** — the one line a worker session prints to ask the operator a design question: the text `DATAWATCH_QUESTION:` followed by a single line of JSON with the keys `question`, `options`, `recommendation`, `default`, `blocked`, and optionally `label` and `risk`. The daemon acts on it only when both `autonomous.questions.enabled` and `autonomous.questions.worker_marker` are on (both off when the key is absent). A valid line becomes an ordinary operator question asked by `worker-<short session id>`, linked to the session and its task, with `source` `marker`; a line that fails validation is never stored and is audited as `question_marker_refused` by session id, reason and line hash, never the text. See [worker questions](howto/operator-questions-workers.md).

**Blocking question** — an operator question whose asker said it cannot go on until the question is decided (`blocking: true`, from the marker's `blocked: true`). While it is open its task is parked: waiting, with the capacity slot given back and no retry counted. A non-blocking question leaves its task running.

**await_decision wait reason** — the reason shown for a task parked on an open blocking question: `await_decision: <question id>`. The coordinator tick resumes the task after the question is answered or withdrawn and sends the worker one `[automaton decision]` line carrying the decision. It is a wait, not a failure.

**Question context link** — the line *This question is about: …* in the answer dialog, which says what a question concerns and, where it can, lets you open it. For a proposal or document it is an **Open** link that opens the proposal in the documents viewer in a new tab; for an Automaton it is an **Open** button that goes to that Automaton; for a task or a registry row it names the item in words. A question about nothing in particular shows no line. It is for the operator who wants to read the thing being decided before answering. See [howto/operator-questions-answering.md](howto/operator-questions-answering.md).

**Question quality check** — the daemon's review of a question at the moment it is raised, so the operator is not handed one that cannot be answered. It looks for three things: an option with no trade-off written, question text that is too short, and an internal tracker identifier the operator cannot look up. Its mode is `off` (no check), `warn` (the question is raised and the findings go back to the asker) or `refuse` (the question is not raised until it is fixed). The tuning keys are described under the Automata question quality and queue settings below. See [howto/operator-questions-answering.md](howto/operator-questions-answering.md).

### Automata question quality and queue settings

Four settings in Settings → Automata (Autonomous Automata planning) control how the daemon judges a question an Automaton raises for the operator, and how much of each open question the queue shows. They only matter while operator questions are switched on (`autonomous.questions.enabled`). Each help link in those rows opens this section.

**Question quality check** (`autonomous.questions.quality_mode`) — what the daemon does when a raised question reads badly: too short, or naming an internal tracker identifier the operator cannot look up. `off` skips the check. `warn`, the default, raises the question anyway and hands the findings back to whoever raised it. `refuse` does not raise the question; the asker gets the findings and has to send a better one. It is for the operator who wants questions that can be answered without hunting for context: choose `warn` to nudge the Automata, `refuse` to enforce it.

**Minimum question length** (`autonomous.questions.quality_min_text_chars`) — the fewest characters a question's text may have before the check flags it as too short. The default is 60. A whole number of 0 or more; 0 turns the length check off while leaving the rest of the check running. It is for the operator who finds one-line questions too thin to answer, or who has a team that writes terse but complete ones.

**Tracker id pattern** (`autonomous.questions.quality_tracker_id_pattern`) — a regular expression that spots an internal tracker identifier in a question, such as a short run of capital letters followed by digits. A question that matches is flagged, because the operator reading it may have no way to look that identifier up. Leave it empty to use the built-in pattern, `\b[A-Z]{1,3}[0-9]{2,4}\b`. It is for the operator whose tracker uses a different identifier shape; a pattern that does not compile is refused when saved.

**Queue preview lines** (`autonomous.questions.queue_preview_lines`) — how many lines of each open question the queue row shows before its *Show more* control. The default is 3. A whole number of 1 or more. It is for the operator who wants a longer or shorter glance at each question without opening it; the full text is always one click away.

### Plan coverage filters

Two on-by-default filters on the plan requirement coverage gate, tuned so a plan is not made to carry findings for the work it plans to do. **Skip files an Automaton plan creates** (`autonomous.plan_coverage_suppress_created_files`, on when absent) — a file that a task's title or spec says it creates, adds, writes or generates, or that is new in the plan's own file list, is not reported as a missing file, because it is the plan's own output; the allowance stops where the parent folder does not exist and no task creates it, and a path in a folder nothing builds is still reported. **Count only scoped sentences** (`autonomous.plan_coverage_scoped_requirements`, on when absent) — only the sentences in the sections named by `autonomous.plan_coverage_requirement_sections` (default: the scope and constraint sections) count as requirements, a path or config key counts only through the sentence that holds it — no more loose file names, backend names or quoted phrases counted on their own — and a sentence shorter than `autonomous.plan_coverage_min_requirement_chars` characters (default 30, where 0 reads the default) does not count; a spec that carries none of the named sections falls back to the whole spec. The plan's coverage panel, `GET /api/autonomous/prds/{id}/plan_coverage`, the `autonomous_prd_plan_coverage_get` MCP tool, `datawatch autonomous prd-coverage <id>` and the chat verb `autonomous plan-coverage <id>` all show how many findings each filter withheld and the filters the report was computed under; the withheld count is not part of the acknowledgement fingerprint, so an acknowledgement of a genuine finding stays valid when only the filters change. Turning one off reverts that noise class on the next decompose. See [operations — Plan coverage filters](operations.md#plan-coverage-filters).

### Decompose lint

**Decompose lint** — the quality check the decomposer runs over each fresh decomposition, at decompose time, before the plan is stored: a rule pack over the story-and-task graph, with the decomposer given the `block` findings fed back as guidance when the mode says so. A decompose is never failed by it: findings are attached to the stored plan (recorded, never fatal), and `off` is a complete no-op (no templates, no pass, no record). **Mode** (`autonomous.decompose_lint.mode`, case-insensitive; empty/unset is `shadow`; anything else is a config error): **`off`** — nothing runs, nothing is recorded; **`shadow`** (default) — one lint pass, its record stored on the Automaton, the plan never retried; **`enforce`** — while `block` findings remain and the budget is unspent, the decomposer is re-run with the block findings fed back, and each retry is re-parsed, re-templated and re-linted. **Retry budget** (`autonomous.decompose_lint.max_retries`, default `2`; `0` is a valid explicit value — enforce then never auto-retries; `5` is the cap) counts auto-retries on top of one initial pass, so `attempts` on a stored record is `1` up to `1` + budget.

**The built-in rule pack** — the floor every run starts from: eight rules, six `block` (`edits_version_variable`, `runs_hosting_cli`, `starts_daemon_outside_sandbox`, `reads_operator_data_or_config_folders`, `read_only_but_writes_files`, `read_only_unverifiable`) then two `warn` (`no_verification_script`, `names_tracker_id`), with deliberately generic regexes that never name a particular project or home folder. The operator's `autonomous.decompose_lint.rules` is the ceiling over it: the same id replaces that built-in, a new id appends; a bare `{id, disabled: true}` entry or an id in `disabled_rules` removes a rule. A rule whose regex fails to compile is skipped with a reported error; the rest still apply.

**Automaton template** — a reusable Automaton definition held in the template store: a complete spec (type, stories, tasks, backend, effort, model, skills) that an operator starts a new Automaton from. Automata → **Templates** tab (labelled "Automaton templates" in the current UI) → **Automaton templates** sub-tab (the default) lists them; when the store is empty the sub-tab says what an Automaton template is and how to add one. Read on REST at `GET /api/autonomous/templates`, on MCP with `autonomous_template_list`, and on the CLI with `datawatch autonomous templates` and `datawatch autonomous template-get <id>`. It is not a standard task.

**Standard task** (formerly "task template"; the config keys still say `task_templates`) — the built-in task definitions the decomposer appends to a story when the plan left them out: `changelog_entry`, `parity_issue_draft`, `docs_set`, `bounded_load_step`, `final_gate`, `version_bump_left_to_merge`, idempotently (a task an earlier apply already added is detected by its `template:` marker and never re-added), run in `shadow` and `enforce` (`off` applies none), each in its own mode (`llm`, `daemon` or `off`; see **Template mode**). The built-in pack is the floor; `autonomous.task_templates.templates` overlays it by id, and `autonomous.task_templates.projects` holds per-project `enabled` overrides. The global gate is `autonomous.task_templates.enabled` (default `true`). They are read as a list on `GET /api/autonomous/standard-tasks[?project=…]`, the `autonomous_standard_tasks` MCP tool, `datawatch autonomous standard-tasks [--project …] [--json]` and the chat word `standard tasks`; the Automata page's Templates tab (labelled "Automaton templates" in the current UI) shows them under the **Standard tasks** sub-tab — a read-only table (title, kind, summary, when offered, on/off, mode), with an explanatory empty state when there is nothing to list and stacked labelled rows at phone width. A standard task is **not** an Automaton template: the decomposer appends it to a story, while an Automaton template is a whole reusable Automaton definition (see the **Automaton template** entry above, and the **Templates** tab). Detail: [howto/automaton-decompose-lint.md](howto/automaton-decompose-lint.md) §5 and [howto/automata-standard-tasks.md](howto/automata-standard-tasks.md).

**Standard task configuration** (the settings behind the standard tasks, formerly "task templates") — reusable task definitions (a changelog entry, a final gate, …) the decomposer appends to a story when the plan left them out, idempotently (a task an earlier apply already added is detected by its marker and never re-added), in `shadow` and `enforce` (`off` applies none). The built-in pack (changelog, parity-issue draft, docs set, final gate, version note, bounded load step) is the floor; `autonomous.task_templates.templates` overlays it by id, and `autonomous.task_templates.projects` holds per-project `enabled` overrides (there is no per-project mode). The global gate is `autonomous.task_templates.enabled` (default `true`). Each template runs in one of three modes — `llm` (a session), `daemon` (the daemon performs it, no session) or `off` (not appended) — set per template in the **Template mode** map; the four note and changelog templates default to `daemon`, `docs_set` and `final_gate` to `llm`. See **Daemon task mode** and **Script gate**. A template's `verification_script` names one repo-relative script file, never a command string — a value that looks like a command (whitespace, shell operators, an absolute path, a parent reference or the project root itself) is refused at config write with an error naming the template id, and a template that still carries one is skipped when the templates are applied, so it never becomes a task the verifier cannot start. The built-in final gate follows that: it carries no verification script of its own and is read-only — the daemon's [script gate](howto/automaton-decompose-lint.md#the-script-gate-the-final-gate-no-longer-spends-a-session) now performs it (a script it names under a valid path, else the project's `policy project_gate_command` or the global `autonomous.project_gate_command`, else every script the story's other tasks declare, in order until the first failure — all under `autonomous.script_gate_timeout_seconds`), and its verdict and evidence land in the report under `.datawatch/reports/`; a gate the daemon could not run at all is a **failed task with the reason named**, not an unclosed story; the version note is a note the daemon records as the story's decision in its default `daemon` mode, not an edit of the tree.

**Template mode** (`autonomous.task_templates.mode`) — the global per-template setting (a map of template id → mode) that decides who runs the task when it is appended: **`llm`** — a worker session runs it as a normal task, verifier included (default for `docs_set`; also the default for `final_gate`, but the daemon's **script gate** performs that one — resolves its declared checks, runs them under `autonomous.script_gate_timeout_seconds`, and completes or fails the task with the report under `.datawatch/reports/`, so no session, verifier or retry loop; its allowed values are `llm` or `off`, `daemon` refused on write, and `off` means the story has no closing gate at all); **`daemon`** — the daemon performs the task itself and completes it without a model session, a verifier or a retry loop: `changelog_entry` and `parity_issue_draft` are written as the same generated artefact (the write is skipped, with the reason noted, if the file was edited or the kind is disabled), and the note templates (`bounded_load_step`, `version_bump_left_to_merge`) record their spec as the story's decision note and write no file (default for those four); **`off`** — the template is not appended to the story at all. Daemon-mode tasks are skipped by the decompose lint and the per-task plan-structure checks. There are two writes, and they behave differently: the per-template mode keys (`autonomous.task_templates.mode.<id>` over `PUT /api/config`, the CLI, chat/comm, MCP `task_templates_mode`, or the per-template select in Settings) **merge one mode onto the stored map** and leave `enabled`, `templates` and `projects` alone; the decompose-lint config surface (REST `PUT /api/autonomous/decompose-lint/config`, its MCP tool, and the decompose-lint card in Settings) **replaces the block whole when it is sent** — the decompose-lint card sends back the block it loaded with only its enabled checkbox changed, so modes, custom templates and per-project overrides survive a card save (a change another client made after the card loaded is overwritten, so reopen the card before saving), while the REST, MCP and CLI writes still replace the whole block when they send it. `EffectiveTaskTemplateMode` resolves one template's effective mode (explicit setting, else the shipped default, else `llm`). See the [script-gate how-to section](howto/automaton-decompose-lint.md#the-script-gate-the-final-gate-no-longer-spends-a-session) for what the gate runs and when.

**Daemon task mode** — the `daemon` value of a template's mode: the daemon carries the task out itself and completes it without a model session, a verifier or a retry loop. Accepted only for the four daemon-capable templates (`changelog_entry`, `parity_issue_draft`, `bounded_load_step`, `version_bump_left_to_merge`); the two file-writing ones write their generated artefact, the two note ones record their spec as the decision note. Setting a template back to `llm` makes it a session task again. See [daemon task modes and the script gate](howto/automaton-daemon-task-modes.md).

**Script gate** — the daemon-run closing check for a `final_gate` task: no session, no verifier, no retry. It resolves its checks by strict preference — the task's own script under `.datawatch/verify/` (it runs alone), else the project gate command (per-project policy first, then `autonomous.project_gate_command`; it runs alone), else every script the story's other tasks declare — and runs them in story order, until the first failure, under one shared deadline (`autonomous.script_gate_timeout_seconds`, default 900, `0` = no cap). Each check's output goes to the task report and its exit status decides: pass completes the task, fail or deadline fails it. A gate with nothing runnable is a failed task with the reason named.

**Standard-task pack** — a named, versioned folder that ships standard tasks and Automaton templates together (task prompts, verification scripts, template definitions), with a `pack.yaml` manifest at its root. The built-in standard tasks are the pack named `builtin` (`packs.active` default), whose definitions are embedded in the binary from `internal/packs/builtinpacks/builtin/` and need no install. The repository's `internal/packs/testdata/` folders (`standard-tasks-pack/`, `fixture-one-kind/`) are test fixtures only, not embedded; the real standard-tasks pack is published in the community registry. Other packs are installed into an install root at `<name>/<version>/`; an installed version is never overwritten. The feature is off by default (`packs.enabled: false`). **Status:** the rules exist in the `internal/packs` package and the `packs` config keys are read, but no REST, MCP, CLI or PWA surface installs, accepts or enables a pack yet, and the daemon does not enforce the switch or validate `packs.active`. Detail: [howto/automata-standard-tasks.md](howto/automata-standard-tasks.md#installable-packs-backend-only-not-yet-wired).

**Pack task resolver** — the step that builds the ordered standard-task list for one Automaton kind from the active packs (`packs.ResolveTaskList`). Packs are walked in order; each pack's tasks are added when the pack lists that kind, in manifest order. Two tasks with the same title are refused and the error names both packs. Each resolved task carries the name and version of the pack it came from. The built-in list is resolved for the `feature` kind only, and the apply step does not filter by kind today. Detail: [howto/automata-standard-tasks.md](howto/automata-standard-tasks.md#how-the-list-is-resolved).

**Pack attribution** — the `pack_name` and `pack_version` fields on trace events, and the `pack_name` detail on audit entries, meant to record which installable pack a task came from. Trace roll-ups group by these (`dimension=pack_name`, `pack_version`), the audit query filters on `pack_name`, and the cost report has a pack breakdown. **Status:** these are read-side only. No production code writes `pack_name` on an audit entry, and the `builtin` stamp helper for trace events has no production caller, so nothing is attributed on a running daemon yet. Events and entries without a pack name count as unattributed. Detail: [howto/automata-standard-tasks.md](howto/automata-standard-tasks.md#pack-attribution-traces-audit-and-cost).

**Pack usage summary** — the per-pack count of runs (distinct sessions, tasks or Automatons), cost and last-used time, grouped by pack name and version (`packs.UsageByPack`). It is computed from the trace store; no daemon surface shows it yet. Detail: [howto/automata-standard-tasks.md](howto/automata-standard-tasks.md#pack-attribution-traces-audit-and-cost).

**Pack cost breakdown** — the cost report's rows per pack name and version (events, tokens in and out, cost), built from trace events (`costreport.PackBreakdown`). Events with no pack name fall into one `unattributed` row. It is a library function today and no cost-report surface calls it yet.

**Pack trust record** — the operator's acceptance of one pack name, version and exact content digest, with who accepted it and when, kept in `trust.json` in the install root. A pack that is not from a first-party source runs only with a record that matches its current digest; a changed file changes the digest and the old record stops applying (`pack not accepted: accept it first`).

**First-party pack source** — a skills registry flagged `first_party` (the community registry is one). Its packs run without a per-pack trust record. Accepting the source writes one record per pack that is current at that time; a later version has no record and is judged by the version-diff rules.

**Pack version diff** — the difference between a pack's accepted version and a candidate: new tasks, tools, scripts and file scopes. A new tool, script or file scope **widens access**; new tasks alone do not.

**Auto-accept (per source)** — `auto_accept_packs` on a skills registry source, off by default. When on, a new pack version from that source is accepted automatically, recorded as `auto:<source name>`, only when its version diff does not widen access. A widening version always waits for the operator.

**Decompose guidance** — the text the decomposer's prompt carries. `autonomous.decompose_guidance`: literal text wins verbatim; a path to an existing readable file uses the file's contents; a path whose file is missing, unreadable or empty falls back to the generated block; unset (default) is generated from the active rules — one line per rule, `id (severity): message`, in pack order, so the rules explain themselves to the planner until you supply your own.

**A decompose-lint finding** — one item of the result: `rule_id`, `severity` (`block`/`warn`), `message`, the task or story the rule fired on (`-` when the finding is plan-level) and a short excerpt of what matched. One finding per line in the report: `[severity] rule_id task: message`; an empty report reads `clean: 0 findings`. The record stored per Automaton is `{findings, mode, attempts, linted_at}`.

Surfaces: PWA Settings (the Decompose-lint group) and the Automaton's detail panel (recorded pass + a dry-run button); REST `GET/PUT /api/autonomous/decompose-lint/config` and `GET/POST /api/autonomous/prds/{id}/decompose-lint` (stored record / dry run); the MCP tools `autonomous_decompose_lint_config_get`, `_config_set`, `_dry_run`, `_findings`; CLI `datawatch autonomous lint config | config-set key=value … | dry-run <id> | findings <id>`; comm channel `autonomous lint …`. See [howto/automaton-decompose-lint.md](howto/automaton-decompose-lint.md).

### Uncommitted-work check

The uncommitted-work check stops a task from being marked complete while its work sits uncommitted in the Automaton worktree, where a later merge of the branch would ship without it. Two settings in Settings → General → Automata settings → **Uncommitted-work check** control it. Each help link in those rows opens this section. The check applies only to an Automaton that commits its own work.

The check before an Automaton is reported finished does not count a generated document the daemon itself wrote that a person has not edited — first line still the generated marker, no `<!-- edited -->` line, body hash still matching the hidden sidecar file next to it — together with that sidecar file, so an Automaton whose only uncommitted files are the daemon's own unedited documents is completed and merge ready as usual. A document with an edited line, a removed marker, a mismatched sidecar hash, or a missing sidecar still counts, like any other file, and so does a sidecar whose document no longer reads as generated. The per-task check is unchanged. The detail and the answer flow are in [howto/automaton-uncommitted-work.md](howto/automaton-uncommitted-work.md) (*The documents the daemon itself generated*).

**Uncommitted-work policy** (`autonomous.automation.uncommitted_work_policy`) — what happens when work is found uncommitted. `hold`, the default, commits the task's own declared files and then holds the task (or blocks the Automaton at the finished check) with the reason and the files, and raises one question. `warn` still commits the declared files but only records a warning for what is left and lets the task or Automaton complete. `off` skips the check and any commit attempt. An unset key or an unknown word reads as `hold`; any other word is refused when written. It is for the operator who wants a merge to carry everything the Automaton did: keep `hold`, and choose `warn` or `off` only when stray files are expected.

**Uncommitted-work scope** (`autonomous.automation.uncommitted_work_scope`) — which files the per-task check counts. `task_files`, the default, counts only the files the task changed itself, leaving out anything that was already modified or untracked before the task started. `worktree` counts every uncommitted file. An unset key or an unknown word reads as `task_files`. The check before an Automaton is reported finished always covers the whole worktree, whatever the scope. It is for the operator whose worktree is shared or already dirty: keep `task_files` so one old stray file does not hold every task.

**The three answers** — a held task or Automaton waits on a question with the options `commit`, `discard` and `retry`, and the default `hold`, so nothing changes until you answer. `commit` stages and commits exactly the listed files that are still uncommitted, including any outside the task's declared files, then completes the task or lets the Automaton roll up again. `discard` removes exactly the listed changes (tracked files go back to the last commit, untracked files are deleted) and runs the task again; it cannot be undone. `retry` touches no file: it runs the task again, or has the Automaton look at the worktree once more. A commit or discard that cannot be done keeps the hold and raises a fresh question. See [howto/automaton-uncommitted-work.md](howto/automaton-uncommitted-work.md).

### Checks

The checks are four automatic, report-only reviews the daemon runs around an Automaton's work: the pre-start verifier, the plan rules, the after-done checklist and the registry check. None of them writes to a project file. Each has its own mode under `autonomous.checks.*` — `off` (not run), `shadow` (run and record the findings on the Automaton's triage record, with no alert and no block) or `on` (the default: run, record and alert, and for the pre-start verifier block a start). A project can override the global values. The findings, newest first, are shown on the **Automata checks** card in the Automation view's Resources sub-tab, by `GET /api/autonomous/checks`, the `checks_status` MCP tool, `datawatch autonomous checks [--prd ID] [--project-dir DIR] [--json]` and `autonomous checks [automaton-id]` on the comm channel. See [howto/automata-checks-and-ship.md](howto/automata-checks-and-ship.md).

### Pre-start verifier

The pre-start verifier (`autonomous.checks.pre_start_verifier`) checks a task's verification script before the task starts: it must exist as a regular file inside the project directory (symlinks and `..` included), be executable and begin with a shebang line. A task with no verification script reads as not applicable. In `on` mode a problem blocks the task start and records why; `autonomous.checks.block_when_missing` (default on) is part of the same policy and is shown with it on the card and in the status read. In `shadow` mode the problem is only recorded. It is for the operator who wants a task that can never be verified to stop before it spends a session, not after.

### Plan rules

The plan rules (`autonomous.checks.plan_rules`) add advisory findings to the structural check of an Automaton's plan: a feature task with no test, a new file, function or type name that carries a tracker id, and a release task that names a fixed version number instead of the next release above main. They never fail the plan; they appear beside it. In `shadow` mode they are recorded only, in `on` mode they are also shown with the plan.

### After-done checklist

The after-done checklist (`autonomous.checks.after_done`) reviews a completed Automaton and records pass, fail or not applicable, with a reason, for each item: the version files agree, the change log has an entry for the release, a changed configuration key is in the configuration reference, a parity issue is referenced for a changed web surface, and a registry row exists for the plan. It only reports; it never edits a file. `shadow` records the result, `on` also alerts.

### Registry check

The registry check (`autonomous.checks.registry_readme`) compares the registry table in `docs/plans/README.md` with the plan documents under `docs/plans/` and reports a plan document no row links to, the same item or id listed twice, and a row whose linked file does not exist. It only reports; it never edits either file. `shadow` records the findings, `on` also alerts.

### Rules and context

The rules and context policy (`autonomous.rules_context.*`) names the files that carry a project's working rules and background. **Rules file** is one name relative to the project directory (default `AGENT.md`); **context files** are up to eight such names (default `CONTEXT.md`); **digest mode** is `off`, `no_tools` (only for backends without file tools, the default) or `all`; **digest size limit** is 0 for the default of 4096 or 512 to 65536 bytes. A project can override all four, and the older `autonomous.rules_file` and `autonomous.context_file` keys remain a fallback below the global values. Set them on the **Rules and context** card in Settings, with `GET`/`PUT /api/autonomous/rules-context`, the `rules_context_status` and `rules_context_set` MCP tools, `datawatch autonomous rules-context` or `autonomous rules` on the comm channel. See [howto/automata-rules-and-context.md](howto/automata-rules-and-context.md).

### Platform context

The platform context is a short section of text that tells every session what datawatch is and how to work with it. The setting `platform_context.enabled` (default on) turns it on or off for all sessions; `platform_context.note` adds your own text after it (up to 2000 bytes). A project can override the enabled value with its own `platform_context_enabled`.

**How it reaches a session.** For **claude-code** sessions the text is written to an **untracked `CLAUDE.local.md`** file next to the project's rules files. In a git repository that filename is added to the repository's `info/exclude` list — a per-clone ignore list that is itself not tracked — so the write never dirties `git status` and the file can never reach a commit. The tracked `CLAUDE.md` and `AGENT.md` files are **never edited** for the platform context. If the project already tracks its own `CLAUDE.local.md`, the local file is refused and that claude-code session gets the text as its **first prompt** instead. Every other backend family — pi-prompt, aider, gemini, goose, goose-prompt, ollama, opencode, opencode-acp, opencode-prompt, openwebui, pi, shell and subprocess — receives the text in the **first prompt** of the session.

Set it on the **Platform context** card in Settings, with `configure platform_context enabled on|off` or `configure platform_context note <text>` on the comm channel, or read it with the `platform_context_status` MCP tool. See [howto/sessions-deep-dive.md](howto/sessions-deep-dive.md#what-a-session-is-told-about-datawatch).


### Worker session profile

A named, user-configurable set of the things a worker, verifier or judge session **starts with**: **which MCP servers** it can use (none, the datawatch server only, or a named list), **which tools** it gets (all, or an allow-list), **whether skills are on**, the **level of starting context** (minimal, digest or full — the task's spec and constraints, the project's rules at that level, the daemon's platform context), and the **model and effort** the session runs on. In the code, tested: the four built-ins — `minimal`, `digest`, `full`, `datawatch-tools` — and the per-role and per-task-kind defaults, set to the smallest that works, with `full` one setting away on the task, the Automaton, the project or daemon-wide: the **planner on `full`** (its own start is unchanged), the **worker, the verifier and implementation, docs and final-gate tasks on `digest`**, and the **judge and `note`-kind tasks on `minimal`**; the settings (`autonomous.worker_profiles`, `autonomous.worker_profile_defaults` — the `role` and `kind` keys — and the daemon-wide name `autonomous.worker_profile`), with a name the daemon does not know refused on set, naming the valid ones; the `worker_profile` field on a task and on an Automaton, with the planner's `worker_profile: <name>` spec-line marker as a per-task name; and the resolution rule itself, **by name, in this order** — task → Automaton → project → daemon-wide → the task-kind default → the role default, set-but-empty is unset, with the rule's unknown-name fallback to the role default and its recorded decision (requested, applied, reason) implemented and tested — together with the **deterministic starting-context builder** (fixed section order, the task section last, same bytes for the same role and profile), the **per-session `worker-mcp.json` MCP config** (the pinned server list; `none`, `datawatch` only, or an exact named list, with a missing name missing from the daemon's own configuration refusing and naming every missing one before anything is written), the **profile's application in the Claude Code launcher** (that one launch gets the pinned MCP config, the tool allow-list, the skills switch and the starting context; a launch with no profile is byte-identical to before), and the **"not applied" record at session start** (one outcome per profile field, in the same stable order; each field a backend cannot honour carries the reason "the backend cannot honour this field", and the session still gets everything its task needs).

Landing with the sessions' own start work is an Automaton's own session launches actually calling the rule and passing the resolved profile to the start (worker, verifier and judge sessions start with the full harness today): the daemon-wide name and the configured role and kind defaults taking effect in those launches, the fallback's recorded decision on the task's record, and the deterministic starting context (the task section at the end) being what lets the model provider's prompt cache share a prefix across retries, ladder hops and verifiers. Worker, verifier and judge sessions are the only ones affected — a session the operator starts is unchanged, and what a task finishes is unchanged. Profiles are set and read through the same six channels a datawatch user already uses — the settings file, the **Worker session profiles** web settings group, `PUT`/`GET /api/config` (plus `POST /api/autonomous/prds/{id}/set_worker_profile` for one Automaton), the MCP tools (`config_set`, `get_config`, `worker_profile_status`, `autonomous_prd_set_worker_profile`), `datawatch config set`/`get` on the `worker_profile*` keys, and the comm channel's `configure autonomous.worker_profile*` keys — each running the same refusals on set, so a refused value changes nothing. The operator guide is [howto/worker-session-profiles.md](howto/worker-session-profiles.md).

### Starting context

The generated text a worker, verifier or judge session is handed when it starts: its role, the line naming its [worker session profile](#worker-session-profile), the constraints, the project's rules at the level the profile calls for (none at `minimal`, the rules digest at `digest`, the full rules at `full`), and the task last. It is built deterministically, so the same role and profile give the same text. Its size is recorded as the [start-up token size](#start-up-token-size). See [howto/worker-session-profiles.md](howto/worker-session-profiles.md).

### Start-up token size

The size, in tokens, of the starting context a worker attempt was launched with. Recorded on each attempt at launch and estimated from the generated starting context: one token per four bytes, rounded up (`internal/autonomous/startup_tokens.go`). Its **source** says how it was obtained: `estimated` (taken at launch — the only source written today) or `reported` (a size a worker or its runner reports at runtime; the cost report understands it, but no runner writes it yet). An attempt with no size is recorded as no data, never as zero. The sizes feed the cost report's start-up tables, the `worker_startup` block of the stats snapshot and the usage calculator's start-up advice line. See [howto/cost-report.md](howto/cost-report.md#the-start-up-tables) and [howto/worker-session-profiles.md](howto/worker-session-profiles.md).

### Start-up advice line

The read-only line in the usage advice that names the worker profile with the largest average start-up token size over its attempts, and the lighter profile that would reduce it. Advice only: it changes no setting, profile or lane. Profiles with no attempts or no average, and attempts with no profile, are left out. See [howto/usage-calculator.md](howto/usage-calculator.md#the-start-up-advice-line).

### Rules digest

The rules digest is a short, deterministic summary of the rules file for a planning backend that cannot read files: every heading, and under each only the lines with a mandatory word (must, mandatory, required, never, always, "no exceptions"), plus the headings of each context file. It is cut to the digest size limit, built only for the backends the digest mode names, and cached by the file's content hash. The planning record states whether one was injected and how large it was. See [howto/automata-rules-and-context.md](howto/automata-rules-and-context.md#the-rules-digest).

### Rules checklist

The rules checklist is the `rules_applied` list the planner gives each task: the sections of the project's rules file the task must follow. The plan check compares it with the areas the task touches (localization, parity and configuration, documentation, tests, versioning). A mismatch is an advisory **rules checklist gap** finding naming the task and the areas; it never fails a plan. See [howto/automata-rules-and-context.md](howto/automata-rules-and-context.md#the-per-task-rules-checklist).

### Planning record

The planning record is what an Automaton stores about the rules and context its planner was pointed at: each file's name, kind, short hash and size, every named file that was missing, and whether a digest was injected with its size. It never holds file content. It is shown in the Automaton's **Planning record** section, by `datawatch autonomous rules-context --prd <id>`, `autonomous rules <id>`, `GET /api/autonomous/rules-context?prd=<id>` and `rules_context_status` with `prd`. See [howto/automata-rules-and-context.md](howto/automata-rules-and-context.md#the-planning-record).

### Ship steps

The ship steps run when a release is finished, in this order: **push** the branch to the configured remote, create an annotated **tag** named from `autonomous.ship.tag_format` (default `v{version}`, with the version read from the release's version file) and push the tag, then the **CI watch**. Each step has its own mode under `autonomous.ship.*` — `off` (skip), `shadow` (record the exact git commands without running them) or `on` (the default: run). The step never force-pushes and refuses a working tree with uncommitted changes. `autonomous.ship.remote` (default `origin`) names the remote. The outcomes, newest first, are shown on the **Automata ship status** card in the Automation view's Resources sub-tab, by `GET /api/autonomous/ship`, the `ship_status` MCP tool, `datawatch autonomous ship` and `autonomous ship [automaton-id]` on the comm channel. See [howto/automata-checks-and-ship.md](howto/automata-checks-and-ship.md).

### CI watch

The ship CI watch (`autonomous.ship.ci_watch`) follows the CI runs of the pushed commit and of the tag after a successful ship step, limited to the workflows named in `autonomous.ship.ci_workflows` (an empty list watches every workflow). Each ref ends as success, failure, still running or timeout. In `shadow` mode a failure is only recorded; in `on` mode a failure also raises an alert on the Automaton's triage record. It is separate from the repo CI watch (`autonomous.automation.repo_ci_watch.*`), which watches the repository's default branch on a schedule.

### Decision records in the PWA

A decision record is the structured description of one operator decision. It
keeps the decision's title and situation, the deciding question, the options
with each one-line tradeoff, a recommendation and its reason, and the forced
default; the daemon raises its queue question from the record, so the record
is the answerable thing, not just a log line.

The PWA shows decision records in two places. The Automata area carries one
card at the top of its list: every record, newest first, each with its title,
record id, state (open, answered, or withdrawn), situation, deciding
question, options with their tradeoffs, recommendation, default, the
operator's answer (actor, surface, and the quote verbatim) when the record
is answered, the document status line outcome when one was reported or
applied, and the follow-up outcome when one ran. The card ends with the
folder watch status line: the watched directory and file count while the
folder is enabled, or a plain line that the folder watch is off, plus any
per-file read problems. A record answered by an Automaton's daemon also shows
that record id on its per-Automaton decision log line. The card is read-only:
it renders what the REST list returns and shows a short unavailable line when
the fetch fails; it never invents records.

Both places are driven by the same `autonomous.decisions.*` keys, which the
Settings → Automata panel exposes as rows and whose docs link opens the
section below or here:

| Key | Meaning |
|---|---|
| `autonomous.decisions.enabled` | Raise the record's queue question at all (the master switch). |
| `autonomous.decisions.folder` | Directory of decision files the daemon watches; empty means the folder watch is off, which the card's status line states. |
| `autonomous.decisions.folder_glob` | Which files in the folder are read (default `*.md`). |
| `autonomous.decisions.folder_withdraw_on_delete` | Withdraw a record when its file is deleted (default true). |
| `autonomous.decisions.front_matter_delimiter` | Line that opens and closes a file's front matter (default `---`). |
| `autonomous.decisions.front_matter_separator` | Splits a front matter line into key and value (default `:`). |
| `autonomous.decisions.front_matter_field_separator` | Separates the parts of an option or recommendation line (default `|`). |
| `autonomous.decisions.front_matter_keys` | Map a canonical front matter key to the name a file uses for it. |
| `autonomous.decisions.clarity_mode` | `off`, `warn` (default) or `refuse`: how a low-clarity record is treated on read. |
| `autonomous.decisions.clarity_min_text_chars` | Fewest characters the situation and the deciding text may have (default 60). |
| `autonomous.decisions.clarity_require_tradeoffs` | Ask every option for a one-line tradeoff (default true). |
| `autonomous.decisions.clarity_require_label` | Ask for a short label on the record (default true). |
| `autonomous.decisions.clarity_tracker_id_pattern` | Regex that spots an internal tracker identifier in a record; empty means the built-in pattern. |
| `autonomous.decisions.doc_root` | Directory that a record's document paths resolve against. |
| `autonomous.decisions.doc_status_mode` | `off`, `report` (default) or `on`: whether an answer is written back to the linked document's status line; `report` only shows the change and `on` applies it, and the card shows whichever outcome the record got. |
| `autonomous.decisions.doc_status_pattern` | Regex that finds the status line (default the built-in pattern). |
| `autonomous.decisions.doc_status_template` | Text the status line is replaced with; placeholders include `{choice}`, `{choice_label}`, `{quote}`, `{actor}`, `{date}`. |
| `autonomous.decisions.follow_ups` | The follow-up actions an answered record may request. |

### Decision record settings

The settings under `autonomous.decisions` (Settings → Automata) control the decision record: a structured description of one operator decision, with a title, the situation in plain words, what is being decided, the options with a line of cost and risk each, a recommendation and a forced default. The daemon raises its operator question from the record, never from the caller's own prose. Every key reads its default when absent, an explicit `false` or `0` is kept, and a bad value is refused on every surface (the config file load, `PUT /api/config`, the MCP config tools, the CLI and the comm channel `configure` verb). A change applies at once, with no restart.

**Switch and folder** — `enabled` (on by default) is the master switch. `folder` names a folder of decision files the daemon watches; empty turns the folder watch off. `folder_glob` (default `*.md`) picks which files are read. `folder_withdraw_on_delete` (on by default) withdraws a record when its file is deleted.

**File format** — `front_matter_delimiter` (default `---`) opens and closes the front matter of a file, `front_matter_separator` (default `:`) splits a line into key and value, and `front_matter_field_separator` (default `|`) separates the parts of an option or recommendation line. None of the three may be empty. `front_matter_keys` maps a canonical key name to the name your files use; empty means the canonical names.

**Clarity check** — `clarity_mode` is `off`, `warn` (the default) or `refuse`, and decides what happens to a record that reads badly. `clarity_min_text_chars` (default 60, 0 = no length check) is the fewest characters the situation and the deciding text may have. `clarity_require_tradeoffs` and `clarity_require_label` (both on by default) ask for a cost-and-risk line on every option and a short human label. `clarity_tracker_id_pattern` is a regular expression that spots an internal tracker identifier; empty uses the built-in pattern.

**Document status line** — `doc_root` is the folder document paths on a record resolve against and must stay inside; empty is the daemon's working directory. `doc_status_mode` is `off`, `report` (the default, shows what would change) or `on` (writes the change). `doc_status_pattern` (default `^Confirmed:.*$`) finds the status line and `doc_status_template` is the text it is replaced with, using `{choice}`, `{choice_label}`, `{quote}`, `{actor}`, `{date}` and `{decision_id}`.

**Follow-ups** — `follow_ups` is a list of `{name, kind, target}`. `kind` is `start_template` (target is a template id) or `open_automaton` (target is an Automaton id). The list is empty by default, so nothing runs unless you configure it. A name may not be empty or repeat.

**Lane** — one channel an Automaton can run in. There are two kinds. A **GPU lane** is one of the channels on the local model pool; which machine serves a given session is not fixed per lane, so the pool is full when every GPU lane is in use wherever it runs. A **Claude lane** runs one Automaton at a time on the Claude backend. A lane is **occupied** while work runs in it and **idle** otherwise. See [howto/automation-lane-fill.md](howto/automation-lane-fill.md).

**Lane fill** — the daemon's own check that every lane stays busy. On each poll it looks at the idle lanes and the queued Automata, and decides what each idle lane should start: a decomposed Automaton that is ready, or a draft to decompose ahead. A lane that cannot take anything shows the reason it stays idle. Lane fill never merges, never judges design or difficulty, and never holds GPU work because of Claude usage limits. See [howto/automation-lane-fill.md](howto/automation-lane-fill.md).

**Verifier-wait lane** — a lane whose occupant is a running Automaton but all of its running work is stalled waiting on a verification slot: work is in the lane, yet none of it can finish, and that is a state different from idle. The lane fill status shows it as the lane's `state: "waiting_verifier"` (only while `autonomous.lane_fill.count_verifier_wait` says the lane counts available; otherwise it reads occupied), with the `waiting_on_verifier: true` flag and the `wait_reason` text from the wait queue, for example "CI still running the checks". The CLI and comm channel lane line reads "waiting on verifier: <reason>", and the idle-lane alert names the wait and its reason before it fires. With `autonomous.lane_fill.count_verifier_wait` on (the default) it makes the Claude lane available to other work as well, because the waiting occupant is not spending Claude usage; set the key to `false` and the lane stays busy. In shadow mode, the default, the planner only records a would-start for such a lane and never starts one; in `on` mode a start on it goes through the normal start path. See [howto/automation-lane-fill.md](howto/automation-lane-fill.md).

**Triage** — the record and the alert for every Automaton that ends up failed or blocked: where it stopped, how long it has been there, what the last session was ended for, and the retry budgets; plus one comm-channel alert per stuck episode after `autonomous.automation.triage_alert_minutes`. The same record is the response of `GET /api/autonomous/triage`, the `autonomous_triage` MCP tool, `datawatch autonomous triage`, `autonomous triage [automation-id]` on the comm channel, and the triage card in the PWA's Automaton detail view.

**Shadow mode** — the lane fill's default mode, and the comparison mode of the repo CI watch (whose default is `off`: the watch does not run at all until you set a mode). In shadow the pass runs on every poll and records every decision it would make — the would-rerun, the would-cancel, the would-delete — with the evidence it used, but starts nothing, cancels nothing, deletes nothing and raises no alert. It is how the operator checks the rules against what the supervisor actually did before the daemon acts on them. The other modes are `off` (nothing runs) and `on` (the decisions are carried out).

**Held-state question** — the operator question the daemon raises on its own initiative when an Automaton sits in one of the five held states — `blocked`, `review_held` (failure-escalation review hold), `revision_requested`, `awaiting_decision` or `pending_insert` (a member insert waiting on operator approval) — waiting on a decision the daemon will not make for the operator. The question is blocking, asked by the daemon, and carries the state's real actions as options — resume, retry, retry on a stronger backend, change the task spec, cancel, approve-insert or reject-insert — plus the `hold` option, which is always offered and always the default, so nothing is ever decided silently; the recommended option comes from a fixed table keyed on the state and the failure category (`infra`, `content`, `scope` or `policy`), never a model, with one sentence of rationale. An open question is withdrawn by the daemon when the held state clears (the reason names the cleared state); an answer that names a state the item already left is refused, recorded once as a `stale` decision entry with the full answer, and closes the question. The mode is `autonomous.questions.held_mode`: `off` (nothing runs), `shadow` (the default; the pass computes each held Automaton and records one note per (item, state) on the Automaton's record, raises nothing) or `on` (one question per held (item, state), deduped by a stable key that survives a restart — never a second question for the same one while it is open). It lives under the operator-question channel switch (`autonomous.questions.enabled`, which the report endpoint names in its 503 while off), and `autonomous.questions.held_states` narrows the five states the pass covers (absent = all, explicit empty = none). It is read via REST (`GET /api/autonomous/held-questions[?prd_id=…&state=…]`, read-only), MCP (`autonomous_held_questions`), the CLI (`datawatch question held [--prd …] [--state …] [--json]`), the chat channel (`question held`, read-only) and the **Held-question** card on the Automation view; an answer goes in on any of the standard answer surfaces (PWA, REST, MCP `question_answer`, CLI `datawatch question answer`, chat) and lands once as a decision entry on the Automaton (quoted words, actor, surface, chosen option, result `applied`/`recorded`/`refused`/`stale`). See [howto/automaton-held-state-questions.md](howto/automaton-held-state-questions.md).

**Lane preference** — the setting on each Automaton that says which lane it may take: `gpu`, `claude`, or `any` (the default). The planner honours `gpu` and `claude` exactly. A person sets it; lane fill does not infer it from the work.

**Eligibility reason** — the short fixed code that says why lane fill will not start a queued Automaton. There are seven: `held` (a `HOLD` line, "do not run" or "sequenced last" in the spec), `sequenced_after_unshipped` (the spec says it follows an Automaton that is not completed), `stale` (no change for longer than `autonomous.lane_fill.stale_after_hours`, 72 by default), `proposal_not_confirmed` (an implementation Automaton whose proposal file has no `Confirmed: yes` line), `proposal_missing` (no readable proposal file named), `main_carries_work` (the default branch already has the work) and `worktree_invalid` (a recorded worktree that is gone or not registered). The first rule that refuses an Automaton gives its reason. It appears as `eligibility_reason` on the `blocked` decision, in brackets on the CLI and comm lines, and as a chip on the PWA Lane fill card. A refused Automaton is never started in any mode and does not count as eligible work, including in the supervisor watch. A check that cannot be made never refuses. See [howto/automation-lane-fill.md](howto/automation-lane-fill.md).

**Per-lane-kind mode** — the lane fill mode set for one kind of lane. `autonomous.lane_fill.gpu_mode` and `autonomous.lane_fill.claude_mode` each take `off`, `shadow` or `on`; empty inherits the base `autonomous.lane_fill.mode`, which is why a fresh install is in shadow for both kinds. It lets the operator turn lane fill on for GPU lanes first, watch, and only then for Claude lanes, and go back for one kind without touching the other. Only the decisions of a kind whose mode is `on` are carried out; a kind set to `off` plans nothing. The status `modes` object shows the mode in effect for each kind, and each change is written to the audit log as `lane_fill_mode`. See [howto/automation-lane-fill.md](howto/automation-lane-fill.md).

**Lane counts from caps** — the rule, on by default (`autonomous.lane_fill.lane_counts_from_caps`), that the number of lanes of a type is the sum of the in-flight caps of the enabled LLM entries that run its backend, the same caps the capacity pools use. A Claude backend that admits one session at a time therefore gets one Claude lane however many `claude_lanes` are configured. The configured count is kept when an enabled entry is unlimited, when no enabled entry runs that backend, when the configured count is `0`, or when the switch is off. The status `lane_counts` object gives the configured count, the effective count and the source (`llm_caps` or `config`). See [howto/automation-lane-fill.md](howto/automation-lane-fill.md).

**Repo CI watch** — the daemon's standing watch over a repository's CI. On every poll it reads the head of `main` (or the default branch) and the newest `tag_count` release-shaped tags (`v<M>.<m>.<p>`, default 5) across every workflow that ran on those head commits, keeps one record per run, and classifies each run as `none`, `infra`, `security`, `policy`, `code_or_test`, `judging` or `hung` — `security` (a named workflow failed without green/infra outcome) and `policy` (a judge verdict, at most one judge call per poll, cached per run id and never re-judged) are record-only in every mode, `code_or_test` is the other verdict (a defect for the change's owner, no automatic action), and `judging` is the transient state of a run that had no verdict in this pass. In `on` mode it takes the budgeted actions only: at most one rerun of an `infra` run per run id while the infra retry budget (`retry_policy.infra`, unset reads 0 — no automatic reruns at all) holds, one cancel of a run pending past `hung_after_minutes` (default 60), and the retention deletion of superseded runs when `delete_superseded` is `delete`, on the pass after the green record named them. A non-green run that outlives `alert_minutes` (default 30) raises exactly one alert per episode, in `on` mode only — shadow records the would-dos and writes nothing, and raises no alert. Off by default (`mode: off`). See [howto/automation-repo-ci-watch.md](howto/automation-repo-ci-watch.md).

**Security failure report** — the record the daemon keeps for a failed security run, so the finding is not buried and the Automaton line does not keep building on top of it. For a failed run on a security workflow it reads the failed log and stores one report with the dependency, the advisory identifier, the publication date (or `unknown`), the matching entry in `security/accepted-risks.yml` with its first-added, added and expires dates (or "not in the registry"), a fixed version when one is known, and a recommendation: `upgrade` (a fixed version is known), `keep` (no fix, and the registry holds an entry), `keep_no_entry` (no fix, and no entry — the operator adds one) or `unknown` (the advisory lookup or the registry could not be read). The report also names the action: `stopped` (the Automaton line carrying the failing commit was moved to blocked), `would_stop` (the same in shadow mode, where nothing is stopped) or `report_only`. The mode is `autonomous.automation.security_failure.mode`: `off`, `shadow` (the default; the report is stored, nothing is stopped and no alert is raised) or `on` (the line is stopped and one alert is raised at once). It is read on REST (`GET /api/autonomous/security-failures`), MCP (`security_failure_list`), the CLI (`datawatch autonomous security-failure`), the chat channel (`autonomous security-failure [run-id]`) and the Security failure card in the Automation view. The daemon never writes a suppression and never reruns a security run: an operator accepts a risk in `security/accepted-risks.yml` and regenerates the ignore file with `make security-registry-gen`. See [howto/automation-security-failure.md](howto/automation-security-failure.md).

**Supervisor watch** — the daemon's own version of the supervisor script (`scripts/supervisor-watch.py`). On every coordinator poll, with no model call, it runs fourteen checks — twelve that the script also runs, plus `worktree_not_registered` and `verify_script_not_executable`, which only the watch has (the comparison shows them as *only the watch*) — and keeps one finding per kind and subject. The script has checks the watch does not cover yet (an unreachable model host, node balancing, capacity lease holds, placement, commits ahead of origin). The kinds are `tmux_restart`, `daemon_inactive`, `model_not_resident`, `task_failed`, `task_review_needed`, `automaton_not_merged`, `idle_lane_eligible_work`, `worktree_no_gomod`, `worktree_not_registered`, `worktree_silent`, `verify_script_not_executable`, `main_checkout_dirty`, `ci_red` and `ci_stuck`. It is report-only in every mode: it never merges, starts, holds, cancels or retries an Automaton and never restarts a service. Its node probe, which reads the models a node has resident, verifies the node's certificate (see [Compute node TLS verification](#compute-node-tls-verification)); a node whose certificate fails stays unknown and raises no finding. The mode (`autonomous.automation.supervisor_watch.mode`) is `off`, `shadow` or `on`, and an unset mode reads `shadow`: in shadow the findings are recorded and a finding open for the alert time is marked *would alert*, but no alert is raised; in `on` one warning alert is raised per episode through the existing alert channels. The first sighting of a finding about one Automaton is also noted on that Automaton's triage record. With the script's record file configured, every pass compares the two sets of findings and keeps the result; a day counts toward the *days of full agreement* only when every comparison that day agreed, and one full day of agreement is the bar for retiring the script. A check that cannot tell reports nothing, and a missing or stale script record is never a disagreement. Read on REST (`GET /api/autonomous/supervisor-watch`), MCP (`supervisor_watch_list`), the CLI (`datawatch autonomous supervisor-watch`), the comm verb `autonomous supervisor-watch` and the Supervisor watch card in the Automation view. See [`howto/automation-supervisor-watch.md`](howto/automation-supervisor-watch.md).

<a id="supervisor-loop"></a>
## Supervisor loop {#supervisor-loop}

**Supervisor loop** — the daemon's standing loop that keeps every Automaton moving without an operator typing the next command. Each pass runs a set of mechanical steps (no model call) and a few model jobs, and keeps a run record for each. A model job runs as a one-shot call or a session, on the backend and model you set; when none is set it uses the decision assistant's configured model, and when there is none the job escalates to you instead of quietly falling back to a hosted model. A failed check is retried, and after enough failures the job escalates. The Supervisor loop card in the Automation view lists the jobs (name, style, backend, model, last outcome, escalation count) and the most recent runs. See [`howto/supervisor-loop.md`](howto/supervisor-loop.md). The settings are under Settings → Automata → Supervisor loop; each one has its entry below, with its help link. In the config file the keys sit under `autonomous.supervisor_loop`.

<a id="supervisor-loop-card"></a>
- **Supervisor loop card** — the card on the Setup sub-tab of the Automation view. It lists the jobs (name, style, backend, model, last outcome, escalation count) and the most recent runs, and links to the [how-to](howto/supervisor-loop.md). Help link: [`#supervisor-loop-card`](#supervisor-loop-card).
<a id="supervisor-loop-settings-group"></a>
- **Supervisor loop settings group** — the group under Settings → Automata that holds every `supervisor_loop` setting below. Help link: [`#supervisor-loop-settings-group`](#supervisor-loop-settings-group).

<a id="supervisor-loop-enabled"></a>
- **`supervisor_loop.enabled`** — Master switch for the whole loop. On by default.
<a id="supervisor-loop-close-finished-records-enabled"></a>
- **`supervisor_loop.close_finished_records.enabled`** — Turns the close finished records step on or off. The step closes the records of Automata that have finished: completed and rejected ones are archived on the next idle pass; a cancelled one is archived once the [`supervisor_loop.archive_cancelled_after_minutes`](#supervisor-loop-archive-cancelled-after-minutes) grace has passed since its most recent cancel. A cancelled Automaton whose branch holds commits that are not on the base branch is not archived by the loop — you archive it, its work is merged, or its branch is deleted. The loop's archives are recorded with the actor `supervisor-loop`; your own archive is unaffected and is recorded with the actor `operator`. On by default.
<a id="supervisor-loop-deploy-after-ship-enabled"></a>
- **`supervisor_loop.deploy_after_ship.enabled`** — Turns the deploy after ship step on or off. The step deploys the daemon after a release ships. On by default.
<a id="supervisor-loop-retry-failed-infra-enabled"></a>
- **`supervisor_loop.retry_failed_infra.enabled`** — Turns the retry failed infra step on or off. The step retries tasks that failed for infrastructure reasons. On by default.
<a id="supervisor-loop-cleanup-sweep-enabled"></a>
- **`supervisor_loop.cleanup_sweep.enabled`** — Turns the cleanup sweep step on or off. The step sweeps leftover worktrees and scratch files. On by default.
<a id="supervisor-loop-apply-answers-enabled"></a>
- **`supervisor_loop.apply_answers.enabled`** — Turns the apply answers step on or off. The step applies the operator's answers to held questions. On by default.
<a id="supervisor-loop-lane-count-enabled"></a>
- **`supervisor_loop.lane_count.enabled`** — Turns the lane count step on or off. The step sets the lane counts, only inside the bounds you configure. It moves nothing while the maximum is 0. On by default.
<a id="supervisor-loop-start-next-chain-step-enabled"></a>
- **`supervisor_loop.start_next_chain_step.enabled`** — Turns the start next chain step step on or off. The step starts the next Automaton in a chain when the one before it is done. On by default.
<a id="supervisor-loop-run-merge-until-conflict-enabled"></a>
- **`supervisor_loop.run_merge_until_conflict.enabled`** — Turns the run merge until conflict step on or off. The step runs merges until one conflicts. On by default.
<a id="supervisor-loop-draft-spec-style"></a>
- **`supervisor_loop.draft_spec.style`** — How the draft spec job runs: `oneshot` (a single call) or `session` (a full session). The job drafts a specification. Default `oneshot`.
<a id="supervisor-loop-draft-spec-backend"></a>
- **`supervisor_loop.draft_spec.backend`** — The backend the draft spec job runs on. Empty uses the decision assistant's configured backend.
<a id="supervisor-loop-draft-spec-model"></a>
- **`supervisor_loop.draft_spec.model`** — The model the draft spec job runs on. Empty uses the decision assistant's configured model.
<a id="supervisor-loop-classify-ci-failure-style"></a>
- **`supervisor_loop.classify_ci_failure.style`** — How the classify ci failure job runs: `oneshot` (a single call) or `session` (a full session). The job classifies a failed CI run. Default `oneshot`.
<a id="supervisor-loop-classify-ci-failure-backend"></a>
- **`supervisor_loop.classify_ci_failure.backend`** — The backend the classify ci failure job runs on. Empty uses the decision assistant's configured backend.
<a id="supervisor-loop-classify-ci-failure-model"></a>
- **`supervisor_loop.classify_ci_failure.model`** — The model the classify ci failure job runs on. Empty uses the decision assistant's configured model.
<a id="supervisor-loop-pick-backend-style"></a>
- **`supervisor_loop.pick_backend.style`** — How the pick backend job runs: `oneshot` (a single call) or `session` (a full session). The job picks the backend for a task. Default `oneshot`.
<a id="supervisor-loop-pick-backend-backend"></a>
- **`supervisor_loop.pick_backend.backend`** — The backend the pick backend job runs on. Empty uses the decision assistant's configured backend.
<a id="supervisor-loop-pick-backend-model"></a>
- **`supervisor_loop.pick_backend.model`** — The model the pick backend job runs on. Empty uses the decision assistant's configured model.
<a id="supervisor-loop-validator-retries"></a>
- **`supervisor_loop.validator_retries`** — How many times a model job is retried after its output fails the check. Default 2.
<a id="supervisor-loop-escalate-after-failures"></a>
- **`supervisor_loop.escalate_after_failures`** — How many failed attempts in one run before you are asked. Default 2.
<a id="supervisor-loop-run-retention"></a>
- **`supervisor_loop.run_retention`** — How many recent run records are kept. Default 500.
<a id="supervisor-loop-archive-cancelled-after-minutes"></a>
- **`supervisor_loop.archive_cancelled_after_minutes`** — The grace, in minutes, before a cancelled Automaton is archived: it is archived only once the time since its most recent cancel reaches this value (0 archives it at once). It exists so a cancelled branch can still be inspected, or re-cancelled to push it back, before it disappears. Separately from this grace, the loop never archives a cancelled Automaton whose branch still holds commits that are not on the base branch. Default 60.
<a id="supervisor-loop-lane-count-min"></a>
- **`supervisor_loop.lane_count.min`** — The lowest lane count the loop may set.
<a id="supervisor-loop-lane-count-max"></a>
- **`supervisor_loop.lane_count.max`** — The highest lane count the loop may set. 0 means no bounds, so the lane count step moves nothing.

**Tag subtree count** — the number of distinct Automata under a tag, counting Automata that carry the tag or any tag below it. An Automaton that carries two of the tags in the subtree is counted once. It is the main count the tag browser shows. Sessions have their own subtree count, which also includes the tags a session inherits. The direct count (Automata carrying exactly that tag) is available as a tooltip when `autonomous.tags.direct_counts_visible` is on. See [howto/automata-tags.md](howto/automata-tags.md).

**Attention group** — the set of Automata that need the operator now: every Automaton whose status is in `autonomous.tags.attention_statuses` (blocked, needs review and paused by default), plus any Automaton with a task held for review. It is the pinned "Needs attention" section above the tag browser, and the view that opens by default when something is in it. See [howto/automata-tags.md](howto/automata-tags.md).

**Inherited tag** — a tag a session gets from the Automaton that started it. It is shown as a dashed chip marked *inherited*, is read-only on the session, and changes when the Automaton's tags change. A session's own tags are separate and are the only ones set on the session itself. See [howto/automata-tags.md](howto/automata-tags.md).

**Automatic tags (auto tags)** — the three tags the engine keeps on every Automaton while it runs: one for its Automation, one for the member's stage, and one for its kind. Their shape comes from `autonomous.auto_tags` — `enabled` (on by default), `automation_template` (default `automation:{automation}`), `stage_template` (default `stage:{stage}`), `kind_template` (default `kind:{kind}`) and `kind_labels` (default `feature:feature`, `bug:bug`, `other:maintenance`). The templates may use only `{automation}`, `{stage}` and `{kind}`. A change to a template or a kind label renames the affected records in place (the tag ids stay put, so a filter or count keyed on the id does not break); a name an operator tag already owns is refused rather than taken, and nothing is written. An Automation may override `enabled` and the three templates for itself; `kind_labels` stay daemon-global. Renaming a stage migrates the stage tag with it. Turning `enabled` off strips the tags from the members and reclaims the records nobody references any more — the shared kind tags are the one exception (fleet vocabulary). Because the engine owns them, they are *system tags*: recolorable, but never renamed, moved or deleted by an operator (HTTP `409`, message `tag is a system tag`). Read them by filtering the Automata list, the progress, the trend and the session list on `tag` (repeatable) with `tag_match` (`any`/`all`). See [howto/automata-auto-tags.md](howto/automata-auto-tags.md).

**System tag** — a tag the engine keeps for its own vocabulary; today that is exactly the three auto tags, and the system marker is what the registry enforces on every write path — web, CLI, MCP, REST and chat. The name and the parent are fixed, and delete is refused (`409`, message `tag is a system tag`), while a recolor stays allowed. See [howto/automata-auto-tags.md](howto/automata-auto-tags.md).

**Placement axes** (`autonomous.tags.placement`) — the ten tag axes that turn a recorded fact into a tag: `compute`, `peer`, `backend`, `lane`, `role` (facts about a session run) and `project`, `automaton`, `automation`, `stage`, `kind` (facts the objects the Automata own carry too: an object gets the last five, never the run axes). All ten are **on by default**; `autonomous.tags.placement.enabled` is the master switch, and each axis has `<axis>.enabled` (default `true`) and `<axis>.template` (default `<axis>:{placeholder}`, for example `compute:{node}`, `automaton:{automaton}`, `automation:{automation}`, `stage:{stage}`, `kind:{kind}`). `autonomous.tags.placement.custom` declares operator axes from a recorded field (`name`, `field`, `template` with `{value}`, `enabled`). `role` and `kind` have a fixed vocabulary — roles like `worker`, `verifier`, `planner`, `judge`, `supervisor`; kinds like `task`, `decompose`, `review`, `rejudge`, `auto-fix`. Saving a change under the block runs one tag sweep over the existing sessions and records. In the PWA's Sessions header Tags menu every placement tag is listed once, under its own axis heading in the **Placement tags (system)** section; the Tags section beneath it lists the remaining system tags (padlock-marked) and the operator's tags — display-only, it never changes the tags, a stored selection or the results of an active tag filter. In the PWA's session list the session cards draw these placement tags as green chips with a small ⚙️ mark, and the chips are present at first load: the summary rows of `GET /api/sessions?view=summary` carry each row's `system_tags`, and the chip renders from the tag name itself, so it is drawn even before the tag registry has arrived. Once shown it stays through later list refreshes and session updates; only an explicit empty `system_tags` from the server removes it. See [howto/automata-tags.md](howto/automata-tags.md).

**Tag sweep** — the one pass that applies the in-force placement axes to everything that already exists: it re-derives the system tags of every session (live and archived) and of the records the Automata own — Automata, tasks, Automations, decision records, templates, held questions — from their stored facts, writing only what changed. It is idempotent (a repeated pass changes nothing), it removes the tags of an axis turned off and adds the ones turned on, and it never reads or writes the tags an operator assigned or that a session inherits. It runs when a `tags.placement.*` change is saved, once at daemon startup after the lease backfill (the daemon remembers the last block it swept), and on demand: `POST /api/autonomous/tags/sweep` (`?dry_run=true` or a `dry_run` body field), the MCP `autonomous_tag_sweep` tool, `datawatch autonomous tags sweep [--dry-run]` (alias `tag-sweep`), the comm verb `autonomous tags sweep [dry-run]`, and the sweep button in the PWA's **Placement tags** settings group. The answer is a per-kind tally — examined, changed, unchanged, failed — and a dry run reports it without writing a single tag. See [howto/automata-tags.md](howto/automata-tags.md).

**Tag inventory (tags in use)** — the read view of the derived tags actually present on sessions: `GET /api/sessions/tags`, the MCP `session_tag_list` tool, and the comm verb `autonomous session-tags list`. Each entry names the tag, its axis and value (the axis is empty for a plain tag) and the number of live or archived sessions carrying it, which is the quickest way to discover what values an axis has before filtering on one. See [howto/automata-tags.md](howto/automata-tags.md).

**Tag registry keeper** — the one daemon that holds the shared tag registry for a federation. The keeper is named by `autonomous.tags.registry_keeper`: on a peer it is the name of the keeper peer, and on the keeper it is the keeper's own name. A peer keeps a cached copy and forwards its tag edits to the keeper; when the keeper is unreachable the peer shows its last copy as stale. See [howto/automata-tag-registry.md](howto/automata-tag-registry.md).

### Project automation policy

**Project automation policy** — the middle tier of the three policy tiers the daemon resolves every Automaton setting through: **Automaton** (its own override) over **project** over **daemon** defaults. The project tier is the per-project entry stored under `projects.<name>.policy` in `datawatch.yaml`, and it is the only tier that travels with the repository. With no policy set, every key inherits the daemon's own value and nothing reads differently. See [howto/project-automation-policy.md](howto/project-automation-policy.md) for the walkthrough.

**Project policy panel** — the PWA Settings → Automata → **Project policy** view: one row per policy key with the effective value, the precedence badge, and a *set* / *inherit (clear)* control per row. Ask it for the project's name and optionally an Automaton's id to resolve with the top tier in play. The same object is edited through `project_upsert`'s `policy` argument on MCP or the `policy` field on `POST /api/projects`, and read back with `datawatch projects policy` on the CLI or the `project policy` comm verb. Help link target: [howto/project-automation-policy.md](howto/project-automation-policy.md).

**Precedence badge** — the chip next to each setting in the Project policy panel that names the tier the *effective* value came from: **Automaton**, **Project** or **Daemon**. A badge that says *Daemon* means the project set nothing for that key; its value is inherited, not copied. The same resolution is readable with `GET /api/projects/{name}/policy/effective?automaton=<id>`, the `project_policy_effective` MCP tool, `datawatch projects policy show <name> --effective [--automaton <id>]` on the CLI, or the `project policy <name> [automaton]` comm verb. Reading the effective value is always safe; only a write to the project tier or the Automaton tier changes anything.

**Policy field** — one row of the Project policy panel, one key of the project policy object. The panel groups the fields the same way this section does. All fields are optional: omit one and it falls through to the tier below.

- **Plan checks** — `plan_coverage_check` (`off`, `warn` or `block`; what the plan requirement-coverage gate does to a short plan), `plan_coverage_min_ratio` (the ratio a plan must reach; a number in `(0, 1]`), `plan_review_mode` (`off`, `warn` or `block`, the plan-quality review gate) and `scope_lint` (`off`, `warn` or `enforce`). These apply to every Automaton that plans inside this project before any Automaton's own settings are consulted.
- **Retry & escalation** — `retry_policy` (the per-category retry budgets, a whole number of attempts from 0 up for each of `infra`, `content`, `scope` and `policy`; a category left out of the map keeps the tier below it for that category only), `retry_ceiling_total` (a cap on the retries the daemon spends across all categories, an integer ≥ 0), `auto_fix_retries` (how many times an auto-fix run may be retried, an integer ≥ 0), `infra_retry_escalate_after` (after how many infrastructure retries a task may escalate instead of retrying, an integer ≥ 0 where 0 reads as *never, keep retrying*) and `failure_escalation` (the escalation level the daemon reaches when a task's retries are spent: the tier, its backend/model/effort, and how many attempts each step gets).
- **Git defaults** — `default_auto_pr` (whether a task may open a pull request without asking per task), `parallel_read_only_tasks` (whether read-only tasks may run in parallel) and `parallel_worktree_per_task` (whether each parallel task gets its own worktree).
- **Lane limits** — `max_concurrent_tasks` (the project's total in-flight task cap, an integer ≥ 1), `plan_tier_max_inflight` (per plan tier, how many Automata may be in flight at once) and `lane_fill_gpu_lanes` / `lane_fill_claude_lanes` (how many GPU and Claude lanes this project may hold from the pools, integers ≥ 0 where 0 reads as *no preference, use the daemon's own cap*).
- **Other recent keys** — `uncommitted_work_policy` and `uncommitted_work_scope` (see [Uncommitted-work check](#uncommitted-work-check); set here they bind every Automaton that commits its own work inside this project), and `burn_guard_ask_on_change` (whether a burn-guard change for a task in this project is a confirmation question to the operator or a silent write).

A value the panel cannot parse — a non-whole number, a word outside the field's vocabulary, an out-of-range ratio — is refused when saved with the reason in the panel; nothing half-written is kept. Clearing a field (the *inherit (clear)* action) deletes that key from the project tier, which is not the same as setting the daemon default: it puts the tier below back in play and the badge moves.
### Protected paths

Protected paths stop an Automaton task from changing the files that hold the project's rules without the operator's decision. A worker that weakens the rules it is judged by, or edits a workflow so a check passes, makes everything after it untrustworthy, and the history does not say who did it. Three settings in Settings → General → Automata settings → **Protected paths** control it. Each help link in those rows opens a section here. The check applies to any Automaton that has a task directory, in direct mode and in worktree mode.

**Protected paths** (`autonomous.automation.protected_paths`) — the list of paths a task may not change without a decision. Each entry is repository-relative and is a file (`CODEOWNERS`), a directory (a trailing slash or `/**`, such as `ops/`, which covers everything below it at any depth) or a glob using `*`, `?` and `[ ]` matched against the whole path (`docs/plans/*.md`; a `*` never crosses a slash). Matching is case-sensitive. The default list, used when the key is unset or empty, is the project rules file (`autonomous.rules_file`, `AGENT.md` when unset), `docs/plans/2026-10-17-supervisor-rules-and-lessons.md`, `CODEOWNERS` and `.github/workflows/`. An explicit list replaces the default entirely, so write every entry you want. An absolute entry, an entry with a `..` segment, an entry over 256 bytes and a list over 100 entries are refused when written. It is for the operator who wants the rules and the checks out of a worker's reach: keep the default, and add the directories that matter to your project.

What counts as a change is read from git alone: files the task committed, edits to tracked files that are not committed yet and new untracked files, compared with the base commit. A file that was already modified or untracked before the task started is the operator's own work and is left out, unless a commit made since the base touched it. The daemon's own write into the rules file is checked like any other change. See [howto/automaton-protected-paths.md](howto/automaton-protected-paths.md).

### Protected-paths action

**Protected-paths action** (`autonomous.automation.protected_paths_action`) — what the daemon does when a task changes a protected path the Automaton is not authorised for. `hold`, the default, parks the task as blocked with the changed paths and raises one question, labelled `protected_paths`, with the options **Approve this change** (`approve`), **Reject it** (`reject`) and **Change the spec** (`change_spec`) and the default `hold`, so nothing changes until you answer. `warn` records a decision, an audit event and a warning alert and lets the task go on; no question is raised. `block` parks the task as blocked with the changed paths and raises no question: authorise the path for the Automaton, or edit the task, then reset it. `off` does no check. The gate never reverts or rewrites anything by itself. An unset key or an unknown word reads as `hold`; any other word is refused when written. It is for the operator who wants a merge to carry no unreviewed change to the rules: keep `hold`, and choose `warn` only for a project that expects those edits.

**The three answers** — **Approve this change** keeps the change and completes the task; the approval covers only the listed paths, for this task, and never changes the Automaton's authorised paths. **Reject it** puts the listed paths back to their state at the base commit in a new commit (a path the task added is removed; history is never rewritten) and runs the task again with a note that those paths must not change; it uses none of the task's automatic-retry budget. **Change the spec** touches nothing: the task stays blocked until you edit its instructions and reset it. A restore that cannot be done keeps the hold and raises a fresh question. See [howto/automaton-protected-paths.md](howto/automaton-protected-paths.md).

### Rules-review source

**Rules-review source** (`autonomous.automation.rules_review_source`) — where the rules review reads the project rules from. The rules review is the completion check that judges an Automaton's real changes against the project's rules; it runs when `rules-review` is in the Automaton's guardrail list. `base_commit`, the default, finds the commit the Automaton's work started from and gives the judge the text of the rules file and the context file as they were at that commit, so a task cannot change the rubric it is judged by, and deleting the rules file does not skip the review. If the working-tree copy differs from that text, the judge reports the difference as a finding. `worktree` tells the judge to read the files from the working directory, as before. When the base commit cannot be found, or a git read fails, the review reads the working directory for that run and records a `rules_review_source_fallback` decision on the Automaton. An unset key or an unknown word reads as `base_commit`; any other word is refused when written. It is for the operator who wants the rubric fixed before the work starts: keep the default.

### Authorised paths

**Authorised paths** (the Automaton's `authorized_paths`) — the list of protected paths one Automaton's tasks may change without a decision. It is set per Automaton, not in the settings, because only some Automata are meant to edit the rules or a workflow. The list you send replaces the current one; an empty list clears it. Each entry follows the same three forms as the protected list (file, directory, glob) and is repository-relative; an absolute entry, a `..` segment, an entry over 256 bytes and a list over 100 entries are refused. A path under the list passes in every action, and the first time a task uses it the Automaton records a `protected_paths_authorised` decision. Authorising does not answer a task that is already held: answer its question, or reset the task. Set it with `POST /api/autonomous/prds/{id}/set_authorized_paths`, MCP `autonomous_prd_set_authorized_paths`, `datawatch autonomous authorize-paths`, the chat verb `autonomous authorize-paths`, or the **Authorised paths** card on the Automaton's detail page. Prose says authorised; identifiers, JSON fields and commands say authorized. See [howto/automaton-protected-paths.md](howto/automaton-protected-paths.md).

### Infrastructure retry

**Infrastructure failure** — a task attempt that ended because the machinery around the task broke, not because the work was wrong. The task never got a fair attempt, so the daemon does not count it as a failed attempt. Only four classes count, and nothing else is ever treated this way:

- `spawn_error` — the worker could not be started at all.
- `storage_write` — a write failed because the disk is full, the quota is used up or the file system is read-only.
- `session_killed_before_delivery` — the worker session ended before the task text reached it.
- `daemon_restart` — the daemon restarted or shut down while the task was running.

A wrong answer, a failed check, a task that went out of scope or a stall that is not a launch failure is the task's own fault and follows the normal failure path. A session that never started has its own start budget and is not classified here. A throttle or usage limit from a model provider is not an infrastructure failure either, and it is never retried by this feature.

**Infrastructure retry** (`autonomous.infra_retry.enabled`, on by default) — when an infrastructure failure ends a task attempt, the Automaton waits and tries the task again instead of counting a failure against it. This is the master switch for the whole feature; with it off, every failure takes the normal path as before.

**Back-off** (`autonomous.infra_retry.max_tries`, `autonomous.infra_retry.backoff_seconds`) — how many tries a task gets (default 3, a whole number from 1 to 1000) and how long the Automaton waits before each retry, in seconds. The default delays are 60, 300 and 540: one, five and nine minutes, 15 minutes in all. When a task has more tries than delays the last delay repeats, and an empty list means retry with no delay. While a task waits, its row says *infrastructure retry 2 of 3 at 14:05:09 (storage_write)*. A retry that follows a daemon restart waits no longer than the first delay, because the cause is already gone.

**Retried failure classes** (`autonomous.infra_retry.categories`) — which of the four classes are retried. The default is all four; an empty list retries none, so every infrastructure failure takes the normal path.

**Retry record** — what the task keeps about its retries. Each retry is written to the task as an entry with its number, class, a short reason (the error text, cut to 500 characters), the time, the delay and the time the next try starts; the task keeps the newest 50. A separate count of consecutive retries decides when the tries are spent, and it goes back to zero when the incident ends while the entries stay as history. Each retry is also logged as an `infra_retry` decision on the Automaton. The Automata page and the Automaton detail show it as **Retry n of max** on the task, and the Automation view carries an Infrastructure retries card with the policy line and every retried or held task.

**Held task** (`autonomous.infra_retry.exhausted_action`) — what a task becomes when its tries are spent and the action is `hold` (the default). The task stops for the operator's review without a failure counted against it, so the failure escalation budget is untouched. It carries the infrastructure cause, a report that lists every retry, and one error-level alert titled *Automaton task held after infrastructure retries*. With the action set to `count`, the failure is booked as one ordinary failed attempt and the normal failure handling applies.

**Held-task question** (`autonomous.infra_retry.raise_question`, on by default) — when a task is held and the operator question channel (`autonomous.questions.enabled`) is on, the daemon raises one high-priority, blocking question for the hold. It names the task, the Automaton, the failure class and the cause, and offers two answers: **Retry the task now** (the default), which runs the task again with a fresh set of tries and holds it again if the cause persists, or **Cancel the task**, which cancels it so that tasks depending on it do not run. Answering the question has the same effect as resolving the task review. If the task is resolved some other way, the question is withdrawn. With the question switched off, or the question channel off, the alert and the review report carry the cause.

**Dependents that recover** (`autonomous.infra_retry.retry_dependents`, on by default) — when a task fails for an infrastructure reason and the tasks that depend on it fail behind it, each of those dependents is marked as failed because of the dependency, with the original task named as the root. Once the root is no longer failed, cancelled, blocked or held, the dependents return to pending on their own, in dependency order, so a chain recovers in one pass, and an Automaton that ended failed or blocked only because of them returns to running. A dependent that failed for any other reason, or that still waits on another unmet dependency, is left alone. A dependency that was cancelled or that failed on its own merits marks nothing. The recovery is logged as an `infra_dependents_recovered` decision. With the setting off, dependents fail as before and stay failed until reset by hand.

The state is read with `GET /api/autonomous/infra-retries`, the `autonomous_infra_retries` MCP tool, `datawatch autonomous infra-retries` and the comm channel verb `autonomous infra-retries`. Every setting above is also a config key on all surfaces. See [howto/infrastructure-retry.md](howto/infrastructure-retry.md) for the walkthrough and [mcp.md](mcp.md) for the arguments that set the policy.

### Change record

The change record is the list of what an Automaton changed, built from the git diff of each task and merged for the Automaton: files, packages, settings, routes, locale keys, tests and the commands that were run. It is stored with the Automaton, read with `GET /api/autonomous/prds/{id}/change-record`, the `autonomous_prd_change_record` MCP tool and `datawatch autonomous change-record <id>`, and it is the only input the generated artefacts are drawn from. See [howto/generated-artefacts.md](howto/generated-artefacts.md).

### Generated artefacts

Generated artefacts are the small documents drawn from the change record when an Automaton completes: a gate report, a changelog draft, a mobile parity draft, testing-tracker rows and a registry row. Each starts with a generated marker and is a draft: a file a person edited is never overwritten, and `autonomous_prd_regenerate_artefact` (or `datawatch autonomous generated <id> <kind> --regenerate`) renders it again. Seven switches under `autonomous.generated_artefacts` (`change_record`, `gate_report`, `changelog_draft`, `parity_draft`, `tracker_rows`, `registry_row`, `combined_docs_task`) turn each piece off; unset means on. See [howto/generated-artefacts.md](howto/generated-artefacts.md).

### Merge pipeline

The merge pipeline is the ordered list of steps that takes finished branches to shipped: checks, merge, version bump, changelog, tag, push and deploy. Each project has one. A branch ships into main: the built-in pipeline brings each branch onto main and runs the project's merge-version check before anything merges — a branch that set a version of its own is refused — and the `set-release-version` step computes the release version after the merge: the next patch above main as it stands at that point. The push is fast-forward-only (it refuses to push a head that would rewrite the remote), and if the remote's main moved since the merge the run redoes from the rebase step so the version is recomputed; the run record keeps the shipped version as `release_version`. The `set-readme-release-line` step expects the README's `Current release:` line to already carry the `vX.Y.Z (YYYY-MM-DD)` shape (a line like `**Current release: v1.2.3 (2026-11-08).**`); a `Current release:` line without a date has nothing to rewrite, so the step stops the run and names it, while a line with the right version but no date still passes the `readme_line_matches` validator. A run whose branches are all already in main releases nothing new: the release steps are recorded as `skipped` and the deploy step still runs. The release steps are safe to run twice — a step that finds its work already done, the version files already carrying the version, the changelog already holding the heading or the tag already on that commit, is a no-op — and a release tag that points at another commit fails the tag step and is named, never moved. It is shown as the Merge pipeline card in Settings > Automation, which names the source in effect, the reason, the steps in order, the conflict policy and the conflict model, the release approval mode in effect per size (one select per size; choosing a mode saves that project's override), and lets you edit or remove the daemon record. The read carries the same modes in its JSON body (`release_approval_in_effect`) and its text answers. It is read with `GET /api/autonomous/merge-pipeline`, the `autonomous_merge_pipeline_get` MCP tool, `datawatch autonomous merge-pipeline show` and the comm channel verb `autonomous merge-pipeline`. A pipeline is checked before it is kept or used, and every problem is named (`autonomous_merge_pipeline_validate`, `datawatch autonomous merge-pipeline validate`). See [howto/merge-pipeline.md](howto/merge-pipeline.md) for the walkthrough, including [how the release version is chosen](howto/merge-pipeline.md#how-the-release-version-is-chosen).

### Release-bump validator

**Release-bump validator** — the read-only check the merge pipeline runs in its `validate-release-bump` step, after the release bump commit and tag and before the push. It runs five named checks: `version_variables_equal`, `changelog_heading_matches`, `one_step_above_last_tag`, `tag_not_taken` and `readme_line_matches` (a version check only — the date on the README line is the bump step's concern, not this one's). A failing check fails the step, names every check that failed and stops the push; the result is kept on the run record as `validator`. The same checks can be run by hand with `scripts/verify-release-bump.sh`. See [howto/merge-pipeline.md](howto/merge-pipeline.md#the-release-bump-validator).

### Release validator

**Release validator** — the name for the merge pipeline's read-only check of a release bump, the same check defined by the [Release-bump validator](#release-bump-validator) entry above. It is the `validate-release-bump` step, run after the bump commit and tag and before the push, and it is the function `ValidateReleaseBump`. Its result is kept on the run record as `validator` and shown as the last validator line in the merge status. Operator guide: [howto/merge-pipeline.md](howto/merge-pipeline.md#the-release-bump-validator).

**Merge pipeline file** (`autonomous.merge_pipeline.file_name`, default `.datawatch/merge-pipeline.yaml`) — the file looked for in each project, relative to the project. An absolute path or one that leaves the project is refused. It is for the operator who keeps the pipeline in the repository under another name.

**Merge pipeline version files** (`autonomous.merge_pipeline.version_files`, none by default) — the project files, relative to the project, that carry the `var Version = "X.Y.Z"` line the release-version step sets. The step runs after the merge and sets the next patch above main as of that moment — it never takes the version from the branches a run merged — and a file that already carries the run's version is left alone. With none, a pipeline that bumps the version fails that step until the files are named.

Every key of the block can be overridden for one project under `autonomous.merge_pipeline.projects.<project>.<key>`; an unset key inherits the global value, then the default.

### Release size

**Release size** — the step one merge takes on the project's version: `patch`, `minor`, `major`, or `none` (no version step). The daemon resolves it per Automaton, most specific first: an explicit `release_size` on the Automaton (an explicit major applies under any major trigger); a completed Automation marked `milestone` while the project's `major_trigger` is `milestone` (steps major); the `release_size_map` entry for the Automaton's kind; the built-in (kind `feature` steps minor, kind `bug` steps patch, other kinds step no version). The resolution never reads the diff and a major is never inferred from a kind. In this release the size is stored, and the resolver that applies these layers exists, but no merge step calls it yet: the merge runner's version-bump step that reads it is wired in the following work, so nothing set here changes a ship's bump until then. See [howto/release-size-and-milestones.md](howto/release-size-and-milestones.md) for the walkthrough, and [howto/merge-pipeline.md](howto/merge-pipeline.md) for the pipeline it applies to.

**Release readiness bar** — the evidence a release size must show before it can move from shadow to on. The bar is the same for every size: at least 10 comparison records where the daemon's plan is equal to or better than the manual merge, at least 2 of them with conflicts, at least 2 with PWA changes, and no comparison with a blocking defect. It is read with the release readiness read: `datawatch merge-pipeline readiness --size`, `GET /api/autonomous/release-readiness`, the `autonomous_release_readiness` MCP tool, the comm verb `autonomous merge-pipeline release-readiness`, and the Release readiness card on the Automation Setup sub-tab. Reaching the bar does not change a size's mode. The bar is fixed in this release; making it configurable is still to come, and the comparison records it counts are written by a stage that is still to come, so until then the counts stay at zero. See [howto/merge-pipeline.md](howto/merge-pipeline.md#move-from-shadow-to-on).

**Release size layers** — the four places a release size can come from, most specific first: an explicit `release_size` on the Automaton, a completed milestone Automation under the `milestone` major trigger, the project kind map (`release_size_map`), and the built-in default. The resolution reports the layer that decided, shown as a chip on the Automaton row and header (set on this Automaton, Automation milestone, project size map or built-in default).

**Project kind map** — the `release_size_map` setting seen from the project: its entries are layered over the daemon-wide entries and the built-in `feature: minor` and `bug: patch`, for the kinds they name only.

**Merge pipeline release size map** (`autonomous.merge_pipeline.release_size_map`, built-in `feature: minor` and `bug: patch`) — the kind-to-size entries a merge's step is resolved from. A global or project entry adds to or overrides the built-in; a kind no layer names falls back to the built-in or to no version step. A value must be `patch`, `minor`, `major` or `none`; a `major` entry is stored but never used, because a major is only ever explicit or a completed milestone. See [howto/release-size-and-milestones.md](howto/release-size-and-milestones.md).

**Major trigger** (`autonomous.merge_pipeline.major_trigger`, `manual` by default) — when a major release step is raised. `manual`: an explicit `release_size: major` is the only route besides the `breaking_change` flag; `milestone`: a completed Automation marked `milestone` also steps major; `never`: a milestone never steps a major. Any other value is refused with the key named. See [howto/release-size-and-milestones.md](howto/release-size-and-milestones.md).

**Release cadence** (`autonomous.merge_pipeline.release_cadence`, `per_merge` by default) — one release step per merged Automaton. `batch` and `manual` are named values that are refused with *not available yet*, and any other value is refused as invalid. See [howto/release-size-and-milestones.md](howto/release-size-and-milestones.md).

**Version scheme** (`autonomous.merge_pipeline.version_scheme`, `semver` by default) — versions are numbered as major.minor.patch. Any other name is refused with *not available yet*. See [howto/release-size-and-milestones.md](howto/release-size-and-milestones.md).

**Milestone, Automation** — the `milestone` flag on an Automation record marks the arc as a milestone. When every member has finished and the project's `major_trigger` is `milestone`, the arc's merges step major; with any other trigger the flag changes nothing. An unset flag is an ordinary arc. It is set with the Mark as milestone control on the Automation, or the `milestone` field of `automation_create` and `automation_update`. See [howto/release-size-and-milestones.md](howto/release-size-and-milestones.md).

**Breaking change flag, Automaton** — the `breaking_change` flag on an Automaton record marks its change as user-breaking. Regardless of the resolved release size, the resolver reports the merge as one that must go through the major approval mode, so it is never merged quietly; the approval modes themselves land in the work that follows. See [howto/release-size-and-milestones.md](howto/release-size-and-milestones.md).

### Post-ship hooks

The post-ship hooks are the hook steps a project runs after the ship — after the merge pipeline's push and deploy have succeeded, in the order the project lists them. They are off by default. A post-ship hook is either one of the four builtins (`registry_row` updates a registry row, `parity_issue` files a parity issue through the ship runner, `tag` adds a version tag that is not there yet, `notify` sends a rendered template) with its built-in target, or a `command` the daemon runs in the project. Each hook has its own `enabled` switch. A failed hook records its result in the merge run record and, only for a run that belongs to an Automaton, leaves one triage note with source `post_ship`; it never changes the ship, never undoes the deploy and never stops the hooks after it. The block and the hook list are readable and writable from every settings surface: YAML, the `GET`/`PUT /api/config` and merge-pipeline reads, `autonomous_merge_pipeline_get` and `autonomous_config_set` on MCP, `datawatch autonomous merge-pipeline post-ship set|show` on the command line, `configure` and `help` on the comm channel, and the Post-ship hooks card in the PWA settings. See [howto/post-ship-hooks.md](howto/post-ship-hooks.md) for the walkthrough and [flow/post-ship-hook-flow.md](flow/post-ship-hook-flow.md) for the flow.

**`autonomous.merge_pipeline.post_ship`** — the settings block: `enabled` (default `false`), `hooks` (the ordered list, up to 20, each a builtin with its target or a `command` with its own `enabled`), and the built-in targets `registry_row_repo_id` and `registry_row_path` (default `parity/`), `parity_issue_automaton`, `tag_ref`, `notify_backend` and `notify_template` (all unset by default; an unset target keeps that builtin disabled until it is set). Per-project: the same keys under `autonomous.merge_pipeline.projects.<project>.post_ship`.

**Triage note, source `post_ship`** — one note a failed hook leaves on the run when the run belongs to an Automaton; it names the hook and its result and is read through the usual triage notes reads. A skipped hook leaves none.

### Merge pipeline source

Where a project's pipeline comes from, and what happens when that source is bad.

**Merge pipeline sources** (`autonomous.merge_pipeline.source_order`, `file` then `record` by default) — the sources tried first to last: `file` is the project's own pipeline file, `record` is the pipeline the daemon keeps for the project. The built-in pipeline always applies after them, so a project with neither still has one. The first source that yields a usable pipeline wins, and the card and `show` output name it with the reason.

### Merge lock

**Merge lock** — the lock a merge run holds on its base repository for as long as it runs, so that two merge runs never ship into the same repository at once. A run that finds the repository locked is refused, and its record names the run that holds the lock. The lock is released when the run ends, and it is kept in `merge-locks.json` in the daemon store directory, so it still shows as held after a restart. After a restart the same Automaton takes its lock back when it starts the merge again for the same branches; a different Automaton, or other branches, is refused. The lock is shown in the merge status as `Merge lock held: …` and in the `merge_lock` field of `GET /api/autonomous/merge`. Operator guide: [howto/merge-pipeline.md](howto/merge-pipeline.md#the-merge-lock-one-merge-per-repository).

**When the project file is bad** (`autonomous.merge_pipeline.fallback_on_bad_file`, `record` by default) — what is used when the file exists but cannot be used (unreadable, too large, not valid or failing validation). `record` uses the daemon record, `default` uses the built-in pipeline, `none` uses nothing and the merge run does not start. The problem is always reported whichever you choose.

### Merge conflict policy

**Merge conflict policy** (`autonomous.merge_pipeline.conflict_policy`, `mechanical_and_model` by default) — what a merge does when a branch conflicts. `stop_and_ask` aborts the merge, leaves the project unchanged and asks you. `mechanical` resolves conflicts where both sides only added lines and asks you about anything left. `mechanical_and_model` does that first, then asks the conflict model for the rest and keeps its proposal only if the pipeline's test step passes on it. A policy that needs a model degrades to `stop_and_ask` when no model is set or none can be reached, and the card says so.

### Merge conflict model

**Merge conflict model** (`autonomous.merge_pipeline.conflict_model`, a built-in default model name) — the LLM registry entry that proposes a conflict resolution. An unset key uses the default name; an explicit empty string means no model. The setting never adds, edits or deletes a registry entry, so the name must already exist. The proposal is worked out in a scratch worktree and tested there; the project is touched only when the test step passes.

### Merge conflict mode

**Merge conflict mode** (`autonomous.merge_pipeline.conflict_mode`, `shadow` by default) — what a model proposal that passes the test step does. `shadow` records the proposal on the merge attempt and never changes the project. `on` commits a passing proposal and fast-forwards the project to it, after confirmation for the first N applied conflicts. See the [conflict mode](howto/merge-pipeline.md#conflict-mode) walkthrough.

### Merge conflict confirm first N

**Merge conflict confirm first N** (`autonomous.merge_pipeline.conflict_confirm_first_n`, 5 by default) — how many of a project's first applied model conflicts wait for your confirmation before they count as resolved. `0` turns confirmation off. It is active only when the mode is `on`, and the count is kept per project. See [confirm the first N model conflicts](howto/merge-pipeline.md#confirm-the-first-n-model-conflicts) and [read the counter](howto/merge-pipeline.md#read-the-counter).

### Merge conflict classes

**Merge conflict classes** — the kinds of file a conflict can be in: `code`, `document`, `changelog`, `version_line`, `readme_line`, `locale` and `generated`. Each class has its own mechanical rule, applied before any model is asked; a class with no rule, or a conflict no rule settles, goes to the conflict policy. `stop_and_ask` never applies a rule. See [conflict classes](howto/merge-pipeline.md#conflict-classes).

### Release approval

**Release approval mode** — the setting that says what happens to a prepared release before it leaves the machine, chosen per release size. Three modes: **auto** (the daemon runs the checks and, when they pass, ships the release and reports afterwards), **ask** (the daemon prepares everything, raises one decision with the merge plan, and ships only after a yes) and **manual** (the daemon prepares the release and reports; a person ships it). A failed check holds the release in every mode. See [howto/merge-pipeline.md](howto/merge-pipeline.md#release-approval-modes).

**Release approval** (`autonomous.merge_pipeline.release_approval`, one mode per size of release) — who has to approve a release before it ships, as a setting instead of a convention. One key per size — `patch`, `minor`, `major` — each `auto` (with the checks passing, ships without waiting), `ask` (asks the operator before it ships, and ships on their yes) or `manual` (left for the operator to ship, nothing asked). The defaults are patch `auto`, minor `auto`, major `ask`, so a major is never shipped quietly; a release whose size cannot be read resolves to `ask`, never to shipping. An unset size inherits in this order: the project's value under `autonomous.merge_pipeline.projects.<project>.release_approval`, the global value, then the default. Any other mode is refused with the key named, wherever it is set.

The global value is set with `datawatch config set autonomous.merge_pipeline.release_approval.<size> <mode>`, `PUT /api/config` with `"autonomous.merge_pipeline.release_approval.<size>": "<mode>"`, MCP config_set with the same dotted key, the comm channel `configure autonomous.merge_pipeline.release_approval.<size>=<mode>` (any other mode is refused with the key named before it is stored), or the three Release approval fields of the Automation settings page (one per size). One project's value is set under `autonomous.merge_pipeline.projects.<project>.release_approval.<size>` by the same config paths, and over MCP also through the `merge_pipeline_release_approval_patch`, `merge_pipeline_release_approval_minor` and `merge_pipeline_release_approval_major` arguments of `autonomous_config_set` with `merge_pipeline_project` naming the project. The Merge pipeline card in the PWA shows the mode in effect per size, and choosing a mode there saves that project's override; a mode that no longer fits can be cleared with `null` for the size, which reverts it to inheriting. Read it back through any of them, or in the merge pipeline read — `release_approval_in_effect` in its JSON body with the resolved mode per size for the project, and a `Release approval: patch …, minor …, major …` line in its text answers. The daemon's merge run honours these modes through the [release gate](#release-gate) — that is the daemon finishing the review and merge by itself. Moving a size toward `auto` — above all `major`, which stays the operator's unless the operator changes it — should be a recorded decision with its evidence. See [howto/merge-pipeline.md](howto/merge-pipeline.md) for the walkthrough.

### Release gate

The **release gate** is where the daemon's own merge run enforces the [release approval](#release-approval) mode. It applies to every step that leaves the machine — the tag, push, deploy and post-ship-hook steps — and decides at most once per run, before the first of them runs; the verdict is cached, so the run cannot answer the push differently from the tag. What it holds back is the leaving steps only: the merge, the version bump, the changelog and the commit land on the local project as usual, and a held release is one that is not tagged, not pushed and not deployed.

With the checks passing, the mode decides: `auto` lets the leaving steps run; `ask` raises one yes/no question ending "Ship this release?" in the operator's question channel (options "yes (ship it)" and "no (do not ship)", "no" the default) and the run ships only when the current answer is yes — a re-run for the same project, Automaton and size finds that question instead of raising a second one, and a changed mind replaces the earlier answer; `manual` holds the release for the operator and raises no question. A failed check holds the release for a person in every mode and is never asked about: a "yes" against failed checks could only be honoured by shipping them. If the question cannot be raised, the run fails short of the ship rather than guess. The steps the gate holds are still in the run's record, skipped with the mode, the size (or "not read") and the reason in plain words — `pipeline_runs` of `GET /api/autonomous/merge` and the PWA's merge status view. The operator guide is [howto/merge-pipeline.md](howto/merge-pipeline.md#the-release-gate).

### Onboarding report

The onboarding report tells you what is configured, what is missing and what to try next, so a new install can be set up without hunting through settings. It has three parts. **Configured** lists the checks that pass. **Missing** lists the checks that fail, each with the reason it failed. **Try next** lists ordered suggestions, each with a how-to link, the command, the MCP tool and the place in the PWA that does it. It is read with `GET /api/automation/onboarding-report` (add `?format=text` for plain text), the `onboarding_report` MCP tool, the `datawatch` CLI and the comm channel verb, and it is shown as the Onboarding report card in the Automation area with a refresh control. Each help link on the card and in its settings rows opens this section.

**Onboarding report** (`onboarding_report.enabled`, on by default) — turns the report on. When it is off the route, the tool, the CLI, the comm verb and the card all say the report is disabled and run no check. It is for the operator who does not want a setup checklist shown.

### Onboarding report checks

The report runs one check per setup step, in this order: `backends` (an LLM backend is available), `models` (a model is available to run), `projects` (a project directory is registered), `channels` (a comm channel is enabled), `memory` (memory is enabled) and `merge_pipeline` (projects use their own merge pipeline). A check that fails appears under Missing with its reason.

**Disabled checks** (`onboarding_report.disabled_checks`, empty by default) — a list of check ids left out of the report. An id that names no check is ignored. It is for the operator who has chosen not to use a step, such as memory, and does not want it reported as missing.

### Onboarding report next steps

**Include next steps** (`onboarding_report.include_next_steps`, on by default) — adds the Try next part: one ordered suggestion for each missing check, with a how-to link, the CLI command, the MCP tool and the PWA place that fixes it. Turn it off to see only what is configured and what is missing.

### Model request progress

When a worker or verifier session's model runs on a registered compute node, the daemon can see the model request that session is waiting on. This section explains what it sees and how to read it. The feature is the *request watch*; its settings are in Settings → General → Automata settings → **Model request watch**, and each help link in those rows opens the entry for that setting below.

**What is observed** — on every poll the daemon asks the node for its own running-models signal: which models it holds loaded and for how long. When the node has an attached observer and `autonomous.request_watch.use_gpu_signal` is on, it also reads the GPU activity of the node. A change in the running-models signal, or a GPU at or above `autonomous.request_watch.gpu_busy_percent`, counts as progress. The daemon makes no model call to find this out.

**The states** — an observation is in one of three states, plus *unknown* before the first sample:

- `generating` — the request shows signs of progress: the running-models signal changed or the GPU is busy.
- `idle` — the node answers but shows no sign of progress.
- `unreachable` — the node did not answer the poll.

**No progress for N minutes** — the time since the last sign of progress, counted from the last change. It is shown as *waiting on a model request: no progress for N minutes*, and for an unreachable node as *waiting on a model request: node unreachable, no progress for N minutes*. When it reaches `autonomous.request_watch.no_progress_minutes` the observation is over the threshold. An unreachable node counts toward this only while `autonomous.request_watch.unreachable_counts` is on. A request that is generating is never over the threshold.

**A slow request looks like a stall for a while** — a large model can take many minutes to give its first response, and during that time neither signal moves, so the request reads as idle with a growing no-progress time. It is only a stall once the threshold has passed and nothing has changed. Raise `autonomous.request_watch.no_progress_minutes` for slow models, and attach an observer to the node so that GPU activity can show the work.

**Observation only** — the request watch never ends a worker, never retries on another backend and never unloads a model. It makes no model call and changes no task. It only reports, so that a hung request can be told from a slow one and you decide what to do.

**The alert and the question** — when a request first reaches the threshold the daemon raises one alert (`autonomous.request_watch.raise_alert`) and one question (`autonomous.request_watch.raise_question`). The question is not blocking and does not park or change the task; the worker keeps running. It offers up to four choices: *keep waiting* (always offered and always the default), *end the worker*, *retry on another backend* and *unload and reload the model*. The choices are recorded only; choosing one does not do it. Both are raised once per episode: they are raised again only after the request has shown progress and then stalled again. The question is raised only while the operator question channel is on and at least two choices are offered; otherwise the alert lists the choices itself. A question still open when the request recovers or the session ends is withdrawn.

**Where to see it** — the request watch status card on the Automata page, the chips on the session and task views, `GET /api/autonomous/request-watch` (optional `session` and `automaton` query parameters), the `autonomous_request_watch` MCP tool, `datawatch autonomous request-watch [--session ID] [--automaton ID] [--json]`, and the chat verb `autonomous request-watch`. Each observation carries the session, the Automaton, the task, the role (`worker` or `verifier`), the node, the model, the state, the time of the last change, the no-progress seconds, the last poll, whether it is over the threshold and a plain-text summary.

The keys below are in the `autonomous.request_watch` block. Every key has a default, so an unset key reads as its default.

#### autonomous.request_watch.enabled

**Default** on (`true`). **Accepted values** `true` or `false`.

The master switch for the whole feature. With it off the daemon takes no sample, keeps no observation and raises no alert or question, and the read surfaces return an empty list. Turning it on again starts a fresh observation for each running worker or verifier.

**How to set it** — in YAML under `autonomous:` then `request_watch:` as `enabled: true`; with `datawatch config set autonomous.request_watch.enabled true`; with `PUT /api/config` and the body `{"autonomous.request_watch.enabled": true}`; from a chat channel with `configure autonomous.request_watch.enabled=true`; from MCP with `autonomous_config_set` with `request_watch_enabled`; or in the Settings tab with the toggle **Model request watch** in the **Model request watch** group. A refused value comes back naming the key and the valid values.

#### autonomous.request_watch.poll_seconds

**Default** 30. **Accepted values** a whole number of 1 or more.

How often, in seconds, the daemon samples each watched request. A smaller number notices a change sooner and asks the compute node more often; a larger number is quieter. The value is read again on every poll, so a change applies without a restart. A value below 1 is refused.

**How to set it** — in YAML under `autonomous:` then `request_watch:` as `poll_seconds: 15`; with `datawatch config set autonomous.request_watch.poll_seconds 15`; with `PUT /api/config` and the body `{"autonomous.request_watch.poll_seconds": 15}`; from a chat channel with `configure autonomous.request_watch.poll_seconds=15`; from MCP with `autonomous_config_set` with `request_watch_poll_seconds`; or in the Settings tab with the number field **Model request watch poll interval** in the **Model request watch** group. A refused value comes back naming the key and the valid values.

#### autonomous.request_watch.no_progress_minutes

**Default** 30. **Accepted values** a whole number of 1 or more.

How many minutes a request may show no sign of progress before the observation counts as over the threshold. Reaching it is what raises the alert and the question, once per episode. Raise it for large models whose first response is slow; lower it to hear about a hung request sooner. A value below 1 is refused.

**How to set it** — in YAML under `autonomous:` then `request_watch:` as `no_progress_minutes: 45`; with `datawatch config set autonomous.request_watch.no_progress_minutes 45`; with `PUT /api/config` and the body `{"autonomous.request_watch.no_progress_minutes": 45}`; from a chat channel with `configure autonomous.request_watch.no_progress_minutes=45`; from MCP with `autonomous_config_set` with `request_watch_no_progress_minutes`; or in the Settings tab with the number field **Model request watch no-progress threshold** in the **Model request watch** group. A refused value comes back naming the key and the valid values.

#### autonomous.request_watch.observe_workers

**Default** on (`true`). **Accepted values** `true` or `false`.

Whether the sessions that carry out the tasks of an Automaton are watched. With it off, worker sessions get no observation, chip, alert or question, while verifier sessions are still watched when `autonomous.request_watch.observe_verifiers` is on.

**How to set it** — in YAML under `autonomous:` then `request_watch:` as `observe_workers: false`; with `datawatch config set autonomous.request_watch.observe_workers false`; with `PUT /api/config` and the body `{"autonomous.request_watch.observe_workers": false}`; from a chat channel with `configure autonomous.request_watch.observe_workers=false`; from MCP with `autonomous_config_set` with `request_watch_observe_workers`; or in the Settings tab with the toggle **Watch worker requests** in the **Model request watch** group. A refused value comes back naming the key and the valid values.

#### autonomous.request_watch.observe_verifiers

**Default** on (`true`). **Accepted values** `true` or `false`.

Whether the sessions that check a finished task are watched. With it off, verifier sessions get no observation, chip, alert or question, while worker sessions are still watched when `autonomous.request_watch.observe_workers` is on.

**How to set it** — in YAML under `autonomous:` then `request_watch:` as `observe_verifiers: false`; with `datawatch config set autonomous.request_watch.observe_verifiers false`; with `PUT /api/config` and the body `{"autonomous.request_watch.observe_verifiers": false}`; from a chat channel with `configure autonomous.request_watch.observe_verifiers=false`; from MCP with `autonomous_config_set` with `request_watch_observe_verifiers`; or in the Settings tab with the toggle **Watch verifier requests** in the **Model request watch** group. A refused value comes back naming the key and the valid values.

#### autonomous.request_watch.use_gpu_signal

**Default** on (`true`). **Accepted values** `true` or `false`.

Whether GPU activity counts as progress. When the compute node has an attached observer, a GPU at or above `autonomous.request_watch.gpu_busy_percent` shows the request as generating even if the node's running-models signal has not changed. With it off, or when the node has no observer, only the running-models signal is used, and a long first response on a large model reads as idle until it changes.

**How to set it** — in YAML under `autonomous:` then `request_watch:` as `use_gpu_signal: false`; with `datawatch config set autonomous.request_watch.use_gpu_signal false`; with `PUT /api/config` and the body `{"autonomous.request_watch.use_gpu_signal": false}`; from a chat channel with `configure autonomous.request_watch.use_gpu_signal=false`; from MCP with `autonomous_config_set` with `request_watch_use_gpu_signal`; or in the Settings tab with the toggle **Use the compute node GPU signal** in the **Model request watch** group. A refused value comes back naming the key and the valid values.

#### autonomous.request_watch.gpu_busy_percent

**Default** 10. **Accepted values** a whole number from 1 to 100.

The GPU utilisation, in percent, at or above which the node counts as generating. It is used only when `autonomous.request_watch.use_gpu_signal` is on and the node has an observer. Set it above the idle load of the node so that background use does not hide a stall. A value outside 1 to 100 is refused.

**How to set it** — in YAML under `autonomous:` then `request_watch:` as `gpu_busy_percent: 20`; with `datawatch config set autonomous.request_watch.gpu_busy_percent 20`; with `PUT /api/config` and the body `{"autonomous.request_watch.gpu_busy_percent": 20}`; from a chat channel with `configure autonomous.request_watch.gpu_busy_percent=20`; from MCP with `autonomous_config_set` with `request_watch_gpu_busy_percent`; or in the Settings tab with the number field **GPU busy threshold** in the **Model request watch** group. A refused value comes back naming the key and the valid values.

#### autonomous.request_watch.unreachable_counts

**Default** on (`true`). **Accepted values** `true` or `false`.

Whether time spent with the compute node unreachable counts as no progress. On, an unreachable node reaches the threshold like an idle one, and the summary reads *waiting on a model request: node unreachable, no progress for N minutes*. Off, the unreachable time does not move the clock toward the threshold, so a short network break raises nothing.

**How to set it** — in YAML under `autonomous:` then `request_watch:` as `unreachable_counts: false`; with `datawatch config set autonomous.request_watch.unreachable_counts false`; with `PUT /api/config` and the body `{"autonomous.request_watch.unreachable_counts": false}`; from a chat channel with `configure autonomous.request_watch.unreachable_counts=false`; from MCP with `autonomous_config_set` with `request_watch_unreachable_counts`; or in the Settings tab with the toggle **Count an unreachable node as no progress** in the **Model request watch** group. A refused value comes back naming the key and the valid values.

#### autonomous.request_watch.raise_alert

**Default** on (`true`). **Accepted values** `true` or `false`.

Whether one alert is raised when a request first reaches the threshold. The alert names the Automaton, the task, the role, the node and the model, and how long the request has shown no progress. When no question is raised it also lists the choices in plain words, so it alone carries them. It is raised once per episode: not again until the request has shown progress and then stalled again. Observation continues either way.

**How to set it** — in YAML under `autonomous:` then `request_watch:` as `raise_alert: false`; with `datawatch config set autonomous.request_watch.raise_alert false`; with `PUT /api/config` and the body `{"autonomous.request_watch.raise_alert": false}`; from a chat channel with `configure autonomous.request_watch.raise_alert=false`; from MCP with `autonomous_config_set` with `request_watch_raise_alert`; or in the Settings tab with the toggle **Raise an alert at the threshold** in the **Model request watch** group. A refused value comes back naming the key and the valid values.

#### autonomous.request_watch.raise_question

**Default** on (`true`). **Accepted values** `true` or `false`.

Whether one question is raised at the threshold, offering the choices in `autonomous.request_watch.remediation_options`. The question does not block: it does not park the task or change it, and the worker keeps running. Answering it only records the choice. It is raised only while the operator question channel (`autonomous.questions.enabled`) is on and at least two choices are offered. The question is withdrawn on its own when the request shows progress again or the session ends.

**How to set it** — in YAML under `autonomous:` then `request_watch:` as `raise_question: false`; with `datawatch config set autonomous.request_watch.raise_question false`; with `PUT /api/config` and the body `{"autonomous.request_watch.raise_question": false}`; from a chat channel with `configure autonomous.request_watch.raise_question=false`; from MCP with `autonomous_config_set` with `request_watch_raise_question`; or in the Settings tab with the toggle **Raise a question at the threshold** in the **Model request watch** group. A refused value comes back naming the key and the valid values.

#### autonomous.request_watch.remediation_options

**Default** unset, which reads as all four choices. **Accepted values** a list of `end_worker`, `retry_other_backend` and `unload_reload_model`; `keep_waiting` is always offered and is always the default answer.

Which choices the threshold question offers. Unset offers all four: `keep_waiting`, `end_worker`, `retry_other_backend` and `unload_reload_model`. An explicit empty list offers `keep_waiting` only, which is different from unset; with a single choice no question is raised and the alert carries the situation. The choices are recorded only: the feature never ends a worker, retries on another backend or unloads a model, so an answer is a note for the operator to act on by hand. The list replaces the whole list each time it is set.

**How to set it** — in YAML under `autonomous:` then `request_watch:` as `remediation_options: [end_worker, unload_reload_model]`; with `datawatch config set autonomous.request_watch.remediation_options end_worker,unload_reload_model`; with `PUT /api/config` and the body `{"autonomous.request_watch.remediation_options": ["end_worker", "unload_reload_model"]}`; from a chat channel with `configure autonomous.request_watch.remediation_options=end_worker,unload_reload_model`; from MCP with `autonomous_config_set` with `request_watch_remediation_options`; or in the Settings tab with the comma separated field **Remediation options offered in the question** in the **Model request watch** group. A refused value comes back naming the key and the valid values.
### Model routing

**Model routing** (`autonomous.model_routing`) — the rules that decide what happens when an Automaton, a story or a task asks for a model that none of your compute nodes serves, and which node a task and its verification job run on. Each node's served models are read from the node and kept for a while; when you pin a worker or verifier model (or pin a task or story to a node) and no registered node serves the model, the pin is refused, or applied with a warning. A task or a story can be pinned to one compute node, and the verification job follows the worker to that node. Only models that run on a local engine reached through a compute node are checked; a hosted backend such as `claude-code` runs on no node and is never refused. The refusal message names the model and every node that was checked. See [howto/model-routing.md](howto/model-routing.md) for the walkthrough.

The settings below are the six keys of the block. Each is set on every surface (YAML, `PUT /api/config`, MCP, `datawatch config set`, `configure` in chat and the Settings page) and applies at once with no restart.

#### Model routing refusal mode

`autonomous.model_routing.refusal_mode` — what happens when a model is pinned and no registered node serves it. `refuse` (the default) turns the pin down, stores nothing and tells you which model and which nodes were checked. `warn` applies the pin anyway, records a `model_not_served_warning` decision on the Automaton and raises a warning. A node that is not a registered compute node is refused in both modes.

#### Served models cache age

`autonomous.model_routing.served_models_cache_seconds` — how long, in whole seconds, a node's list of served models is kept before the node is read again (default 300). The answer is kept whether the read worked or failed. `0` reads the node every time, so a freshly pulled model is seen at once, at the cost of a read on every pin. Refresh on the node view, `--refresh` on the CLI and `refresh=true` on the read always read the node now.

<a id="verifier-follows-the-workers-node"></a>

#### Verifier follows the worker's node

`autonomous.model_routing.verifier_follows_worker_node` — when on (the default), the verification job for a task runs on the node its worker ran on, and waits for that node instead of moving to another one, as long as that node is one of the verifier's candidates and serves the verifier's model. When off, or when the node cannot be followed, the verifier is placed like any other job; a `verifier_node_not_followed` decision says why.

#### Unreachable node policy

`autonomous.model_routing.unknown_node_policy` — how a node whose served models cannot be read counts: `assume_serves` (the default) counts it as serving the model, so a node that is down does not block a pin, and `assume_not` counts it as not serving, so a pin is refused unless a node that was read serves the model. A node of a kind that has no model list to read counts as unreadable too.

#### Node probe timeout

`autonomous.model_routing.probe_timeout_seconds` — how long, in whole seconds, one read of a node's served models may take before the node counts as unreadable (default 8, minimum 1). Raise it for a slow or distant node; lower it to find out sooner that a node is down.

#### Model routing refusal alert

`autonomous.model_routing.alert_on_refusal` — when on (the default), a pin that no node can serve raises one alert titled *Automaton model not served by any node*: at error level when the pin was refused, at warning level when `refusal_mode` is `warn` and the pin was applied. Turn it off to keep the refusal message in the reply only.

---


## Core feature reference matrix

Tracks which core features have how-to walkthroughs, plans, and architecture diagrams.

| Feature | How-to | Plan | Architecture / diagram |
|---|---|---|---|
| Sessions | [`howto/sessions-deep-dive.md`](howto/sessions-deep-dive.md) | covered in active backlog | [`architecture-overview.md`](architecture-overview.md) |
| Session Telemetry | [`howto/session-telemetry.md`](howto/session-telemetry.md) | ✓ | [`flow/telemetry-flow.md`](flow/telemetry-flow.md) |
| Channel-driven session state engine | [`howto/channel-state-engine.md`](howto/channel-state-engine.md) | active backlog | covered in `architecture.md` |
| Automata / DAG orchestrator | [`howto/autonomous-planning.md`](howto/autonomous-planning.md), [`howto/autonomous-review-approve.md`](howto/autonomous-review-approve.md), [`howto/automata-orchestrator.md`](howto/automata-orchestrator.md) | many plans | architecture covers it |
| Lane fill (shadow mode first) | [`howto/automation-lane-fill.md`](howto/automation-lane-fill.md) | [`plans/2026-10-24-lane-fill-shadow-mode.md`](plans/2026-10-24-lane-fill-shadow-mode.md) | [`flow/automata-capacity-flow.md`](flow/automata-capacity-flow.md) (lane fill section) |
| Repo CI watch (off by default, shadow comparison) | [`howto/automation-repo-ci-watch.md`](howto/automation-repo-ci-watch.md) | [`plans/parity-issues/repo-ci-watch.md`](plans/parity-issues/repo-ci-watch.md) | — |
| Security failure report (stop and report, shadow first) | [`howto/automation-security-failure.md`](howto/automation-security-failure.md) | [`plans/parity-issues/security-failure-exception.md`](plans/parity-issues/security-failure-exception.md) | — |
| Automaton worktree cleanup (report first, disk alert) | [`howto/automaton-worktree-cleanup.md`](howto/automaton-worktree-cleanup.md) | [`plans/parity-issues/2026-10-30-automation-cleanup-parity.md`](plans/parity-issues/2026-10-30-automation-cleanup-parity.md) | — |
| Automaton uncommitted-work check (hold, warn or off) | [`howto/automaton-uncommitted-work.md`](howto/automaton-uncommitted-work.md) | [`plans/parity-issues/2026-10-30-automaton-uncommitted-work-parity.md`](plans/parity-issues/2026-10-30-automaton-uncommitted-work-parity.md) | — |
| Automaton protected paths (hold, warn, block or off; rules read from the base commit) | [`howto/automaton-protected-paths.md`](howto/automaton-protected-paths.md) | — | — |
| Compute node certificate verification (verified by default, CA file, pinned fingerprint, audited opt-out) | [`howto/compute-nodes.md`](howto/compute-nodes.md#certificates-for-self-signed-and-private-ca-nodes) | [`plans/parity-issues/2026-10-30-node-tls-verification-parity.md`](plans/parity-issues/2026-10-30-node-tls-verification-parity.md) | — |
| Supervisor watch (shadow first, one finding per kind) | [`howto/automation-supervisor-watch.md`](howto/automation-supervisor-watch.md) | [`plans/parity-issues/supervisor-watch.md`](plans/parity-issues/supervisor-watch.md) | — |
| Autonomous triage (record, alert, reads on all surfaces) | covered in `CHANGELOG.md` | [`plans/2026-10-26-autonomous-triage-read-route.md`](plans/2026-10-26-autonomous-triage-read-route.md) | covered in `implementation.md` ("Automation triage") |
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
| PRD memory lifecycle | [`howto/prd-memory-workflow.md`](howto/prd-memory-workflow.md) | ✓ | ✓ |
| Daemon operations | [`howto/daemon-operations.md`](howto/daemon-operations.md) | ✓ | ✓ |
| Profiles | [`howto/profiles.md`](howto/profiles.md) | ✓ | ✓ |
| Tailscale mesh | [`howto/tailscale-mesh.md`](howto/tailscale-mesh.md) | ✓ | ✓ |
| chat / LLM quickstart | [`howto/chat-and-llm-quickstart.md`](howto/chat-and-llm-quickstart.md) | ✓ | ✓ |
| Multi-server management | [`howto/multi-servers.md`](howto/multi-servers.md) | v7.2.0 | REST proxy + aggregated endpoints; picking a server chip re-renders the current view in place |
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
| Automaton lifecycle event subscriptions | [`howto/automaton-lifecycle-subscriptions.md`](howto/automaton-lifecycle-subscriptions.md) · [`flow/lifecycle-subscription-flow.md`](flow/lifecycle-subscription-flow.md) | Unreleased (Phases 3–4 of `plans/2026-10-13-prd-lifecycle-events.md`) | persisted encrypted store, engine with one-shot/expire/coalesce, comm/session(bridge, SendInput)/topic/alert/mcp_queue sinks, pending-events queue + polling, update + test-fire, REST + MCP + CLI + owner-scoped comm verbs + PWA Subscriptions tab, live re-attach (no restart) |

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
- [`howto/push-setup.md`](howto/push-setup.md) — register/unregister/notify API, Android integration (v8.2.0)
- [`howto/channel-routing.md`](howto/channel-routing.md) — channel-address federation: route channel messages to peers, owner_peer attribution (v8.3.0)
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
- [`howto/project-automation-policy.md`](howto/project-automation-policy.md) — the project tier of policy: precedence, field-by-field reference, set & clear from the PWA panel
- [`howto/pipeline-chaining.md`](howto/pipeline-chaining.md) — DAG pipelines with before/after gates

Infrastructure:
- [`howto/profiles.md`](howto/profiles.md) — Project + Cluster Profiles
- [`howto/container-workers.md`](howto/container-workers.md) — Docker / Kubernetes ephemeral workers
- [`howto/tailscale-mesh.md`](howto/tailscale-mesh.md) — Headscale + commercial Tailscale agent mesh
- [`howto/secrets-manager.md`](howto/secrets-manager.md) — native + KeePass + 1Password + Vault backends
- [`howto/federated-observer.md`](howto/federated-observer.md) — push-based multi-host stats aggregation
- [`howto/multi-servers.md`](howto/multi-servers.md) — register remote instances, per-tab picker (selecting a host re-renders the current view in place), all-servers aggregation
- [`howto/compute-nodes.md`](howto/compute-nodes.md) — GPU/CPU node registry, kind taxonomy, observer peer binding
- [`howto/v7-compute-migration.md`](howto/v7-compute-migration.md) — migrate deprecated compute node kinds to ollama/openai-compat
- [`howto/llm-registry.md`](howto/llm-registry.md) — named LLM registry, per-node model lists, failover routing
- [`howto/ollama-marketplace.md`](howto/ollama-marketplace.md) — browse and pull models from the embedded Ollama catalog
- [`howto/guardrail-library.md`](howto/guardrail-library.md) — SAST/secrets/deps/LLM grader scan profiles
- [`howto/file-service.md`](howto/file-service.md) — federated file service: upload/delete files, peers/ + discussions/ subdirs, config caps (v8.3.0)
- [`howto/discussion-scopes.md`](howto/discussion-scopes.md) — discussion scopes: WAL-backed per-discussion memory, participant sync, conflict resolution (v8.4.0)
- [`howto/dashboard.md`](howto/dashboard.md) — mission control: constellation, EKG, sprint pipeline, customisable cards
- [`howto/claude-hooks.md`](howto/claude-hooks.md) — hook script setup, status board, auto-install for claude-code sessions

Memory + ops:
- [`howto/cross-agent-memory.md`](howto/cross-agent-memory.md) — episodic memory + knowledge graph + 6-scope hierarchy (persona-global → persona-in-project → project-shared → prd-shared → story-shared → session-local) with borrow/seed/promote
- [`howto/prd-memory-workflow.md`](howto/prd-memory-workflow.md) — complete PRD memory lifecycle: warm-start seeding, harvest, archive-on-delete, archive import, PRD memory report, scope inventory
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

### Parallel execution metrics

When an Automaton completes it records how well its tasks ran in parallel. `total_tasks` is the number of tasks, `read_only_tasks` is how many of them were read only, `concurrent_tasks` is how many ran at the same time as another task, `wall_clock_seconds` is the time from the first task start to the last task end, and `serial_seconds` is the sum of the task durations, the time a one-at-a-time run would have taken. `speedup` is `serial_seconds / wall_clock_seconds`; 1 means the tasks ran one after another, and it is 0 when no timing was recorded. The values are fields of the Automaton read (`GET /api/autonomous/prds/{id}`), of `autonomous_prd_get`, and appear as a `parallel:` line in `datawatch autonomous prd-get` and the chat `autonomous get` reply, and as the Parallel execution card on the Automaton view.

### Parallel task worktrees

**Worktree per task** (`autonomous.parallel.worktree_per_task`, on by default) — each task of an Automaton that runs alongside other tasks gets its own worktree and branch off the Automaton branch, so two tasks never write into the same files at once. When the task is verified, its branch is merged back into the Automaton branch, in dependency order, one merge at a time.

**Task merge state** — where the merge of a task branch stands, shown as a badge on the task card, in `datawatch autonomous prd-get`, in the chat `autonomous get` reply and as `merge_state` in the task read. `pending` means the branch is waiting to be merged, `merged` means it is in the Automaton branch (the task then also carries `merge_commit`, the Automaton branch commit that holds its work), `conflict` means the merge hit a conflict and `held` means the conflict policy stopped the task for the operator.

**Merge conflict policy** (`autonomous.parallel.merge_conflict_policy`) — what happens when a task branch does not merge cleanly. `additive_then_model` (the default) keeps both sides when the two changes only add lines, and asks the merge conflict model to resolve anything else. `block` holds the task at once. `triage` raises the conflict as a question for the operator.

**Merge conflict model** (`autonomous.parallel.merge_conflict_model`) — the model that resolves a merge conflict under `additive_then_model`. Empty, the default, uses the strongest configured planning model.

### Read-only tasks

A **read-only task** is a task flagged `read_only` because it changes no files, for example a task that only reads code and reports. While `autonomous.parallel.read_only_tasks` is on (the default), a read-only task skips the per-tree write lock and runs beside other tasks of the same Automaton instead of waiting for them. A task that is not flagged still takes the lock. Flag a task when you add or edit it (the `read_only` option of `autonomous_prd_add_task` and `autonomous_prd_edit_task`, or the task edit in the web app). The default is on because that is the operator's choice for this installation; other users may switch it off with `autonomous.parallel.read_only_tasks: false`, and every task then takes the lock as before.

A **report path** (`report_path`) is the one clean repo-relative file — under `.datawatch/reports/` — that a read-only task is allowed to write, so the task can still *report* (it is only read-only for everything else): the dirty check ignores exactly that file (it is excluded from the `git status` selection) while any other stray change fails the task. It is only valid on a read-only task — a report path on a task that writes files is refused — and an empty value means the task writes nothing. Set it with the task's `read_only` mark (add/edit in the CLI, chat, MCP, API or web app); see [howto/automaton-parallel-tasks.md](howto/automaton-parallel-tasks.md#allow-a-read-only-task-one-report-file).

### Parallel speedup

**Parallel speedup** is how much faster an Automaton's tasks finished than they would have one at a time: the sum of the task durations divided by the time from the first task start to the last task end. It is recorded when the Automaton completes and shown with the read-only share in the Parallel execution card; see "Parallel execution metrics" above for every field. A speedup of 1 means no overlap. The default settings are the operator's choice and other users may change them; the how-to is [howto/automaton-parallel-tasks.md](howto/automaton-parallel-tasks.md).

### Planning route

A **planning route** is the choice of planner (an LLM backend and a model, plus its escalation targets) that drafts an Automaton's plan when it is decomposed. It comes from the first rule in `autonomous.planning_routing.rules` whose match fields all pass; if routing is off or no rule matches, the global planner is used, and a planner set on the Automaton itself always wins. With `shadow` on, the route is recorded (and counted as would-route in the scorecard) but not applied. Routing is off by default with no rules, and the example rules are not active. How-to: [howto/automata-planning-routing.md](howto/automata-planning-routing.md).

### Planning ladder

A **planning ladder** is the retry budget a planning route uses when a plan fails the decompose checks: `touch_up_max` touch-up passes (defaults to 1), `replan_max` replans (defaults to 1), then escalation to the next planner in the rule after `escalate_after_attempts` failed attempts at one planner (defaults to 2). A named ladder is chosen by a rule's `ladder` field; the built-in `default` ladder is used when none is named. When every planner and budget is used up, the ladder stops with a failed plan and its full attempt record. The defaults are the operator's choice and other users may change them.

### Risk class

A **risk class** is a named set of tags and path globs that marks an Automaton as sensitive for planning routing. An Automaton belongs to a class when one of the class's tags is on it or a file its tasks touch matches one of the class's globs (`dir/**` matches everything under `dir`). The built-in classes are `security` (tag `security`; globs `security/**` and `internal/server/auth*`) and `design` (tags `design` and `ui`); a class defined under `autonomous.planning_routing.classes` with the same name replaces the built-in. A rule selects on classes with `match.risk_class`, and the scorecard counts decomposes per planner and risk class.

## Plans folder tidy

How-to: [`howto/plans-folder-tidy.md`](howto/plans-folder-tidy.md).

### Daily plans tidy

The **daily plans tidy** is the Automation that runs `scripts/plans-tidy-daily.sh` once a day. It simulates the tidy in a throwaway worktree, runs it for real, checks the plan links, commits only `docs/plans/` and pushes. It raises an alert on failure and always writes a run stamp and a daily report.

### Plans folder archive

The **plans folder archive** is where the tidy moves old files: dated plans to `historical-plans/`, old release notes to `historical-releasenotes/<minor-version>/`, and old reports and filed parity-issue drafts to `archive/YYYY-MM/` month folders. A file moves only when it is past the age cutoff and nothing references it.

### Roadmap archive

A **roadmap archive** is a file named `roadmap-archive-YYYY-MM.md` in `docs/plans/`. Closed registry rows of every stage except the current one move there, keeping their number and text. The tidy never moves these files.

### Unclassified file

An **unclassified file** is one the tidy cannot place, such as an old parity-issue draft that is not filed. It is counted, never moved, and listed by name only in the daily report. A person decides what to do with it.

### Run stamp

The **run stamp** is `.datawatch/reports/plans-tidy-last-run.json`, written on every daily run: `ran_at` in UTC, the `result`, the counts and the commit. The release smoke skips its tidy check when the result is `ok` and `ran_at` is under 24 hours old.

### Daily report

The **daily report** is `.datawatch/reports/plans-tidy-daily-report.md`, written on every daily run: the counts and the names of the unclassified files. File names appear nowhere else.
