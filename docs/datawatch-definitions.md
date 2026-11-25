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
- **Coverage matrix** — `docs/skills/coverage-matrix.yaml`: one row per feature inventory entry naming the skills that cover it, a status of none, partial or full, and a note. Rows marked none or partial are the gaps. See [`howto/skills-find-install-write.md`](howto/skills-find-install-write.md).
- **Feature inventory** — `docs/skills/inventory.yaml`: every area of the product with its MCP tools, how-tos, REST routes, commands and settings. A check script fails when it drifts from the code.
- **Staged skill** — a skill draft written under `internal/skills/testdata/staging/`, waiting to be published to the community registry by an operator. It is not embedded in the binary. See [`skills/publish-instructions.md`](skills/publish-instructions.md).
- **Secrets manager** — native AES-256-GCM store at `~/.datawatch/secrets.db` plus optional KeePass and 1Password backends; `${secret:name}` references resolve in YAML, plugin manifests, spawn-time env injection.
- **Native ACME / Let's Encrypt** — the daemon obtains and auto-renews its own browser-trusted TLS certificate (HTTP-01), no external tool and no DNS management required beyond a single A record. Staging-first workflow, restart-based apply, all 7 surfaces. See [`howto/letsencrypt-acme.md`](howto/letsencrypt-acme.md).
- **Federated observer** — multiple datawatch instances pushing process / network / GPU stats into one aggregated view. Observer pushes over `https://` verify the counterpart's certificate by default (system trust store; or, a worker's push, the parent's pinned leaf fingerprint) — skipping verification is an explicit, startup-warned opt-out. See [howto/federated-observer → Push-channel TLS (verified by default)](howto/federated-observer.md#push-channel-tls-verified-by-default).
- **Autonomous Automata (PRD-DAG)** — high-level intent decomposed into a directed graph of stories and tasks, executed under verification + guardrails.
- **Plugin framework** — manifest-driven hot-reload; subprocess + native plugins; declared comm verbs / CLI subcommands / MCP tools / mobile cards. Community plugins can be installed from a connected registry with `datawatch plugins install <registry> <name>`.
- **Model-use source** — the daemon-side provider of which models a service-kind plugin calls (`GET /api/plugins/<name>/models`). When none is wired the daemon still starts; the route answers 503 and model-node events do not run.
- **Git hosts** — the daemon's named list of git push targets: host `kind`, base URL, token source (`host_login`, `vault`, or the credential broker), and tag-signing; readiness (login, account, scopes) and a connection test per host over REST, MCP, CLI and the comm channel, with the token never returned on any read; the list itself is writable per host field or whole on every operator surface (REST `PUT /api/config`, `datawatch config set git.hosts.<name>.<field>`, `configure git.hosts.<name>.<field>=...`, and the **Git hosts settings card** in PWA Settings → General — the per-host editor with readiness, Test connection and whole-list save); per-host operations — PR/MR open, issue and release creation on GitHub and GitLab, listing a ref's CI runs (the read the repo CI watch takes for a repository on one of your hosts), clone/push on every kind, and the explicit "no forge API" answer on the plain-git kinds — each completed operation audit-logged as one `host_git_operation` entry (names only, never the token). See [Git hosts](#git-hosts).

## How it's built

**Single binary.** No language runtime to install, no microservices to operate, no bus to deploy. The binary embeds the PWA, the docs, the MCP server, the messaging adapters, and the daemon — `datawatch start` is the whole install.

**One surface, mirrored seven ways.** Every feature reachable through the REST API is reachable via MCP, the CLI, every comm channel, the PWA, the mobile (Compose Multiplatform) app, and YAML on disk. **Read once, write once, audit once.** No drift between surfaces.

**Tmux-backed sessions.** AI work happens in real terminals so you can attach with `tmux attach` and see exactly what the LLM is doing — no abstraction layer between you and the work.

**Open data.** Sessions, memory, identity, audit, scheduled work, and persona definitions all live as plain files under `~/.datawatch/`. No proprietary database. Operator-editable, operator-grep-able, operator-backup-able.

## How to use this manual

Each section maps to one PWA tab or card. The structure is the same throughout: what the card is for → what each control does → links to deeper reference (architecture docs, how-to walkthroughs, diagrams). The PWA's `?` icon next to the search button (Sessions / Session detail / Automata / Alerts / Observer / Settings) deep-links you to the view's documentation. On the Alerts tab it points at the Alerts howto ([`howto/alerts-and-notifications.md`](howto/alerts-and-notifications.md)) and opens it in a new tab; on the other views it lands on the matching section above. The Alerts tab carries no separate help link of its own.

**Alerts tab (tidied for a narrow phone viewport).** The list's meta row now appears only when it has something to say: `Cached`, a small `Loading…` while a refresh runs, or the `Data may be out of date` marker — there is no "Last updated N s ago" counter. On the Alerts list the stale marker shows when a refresh failed or the data is past a fixed 2-minute window (Alerts polls every 30 s; other list cards use 5 minutes); a failed refresh keeps the cached rows and adds a small failure banner with a `Retry` button. The sub-tabs are **Active sessions** (N), **Historical sessions** (N) and **System alerts** (N) — the first two numbers count sessions, the third counts alerts — while the `🔔` heading always counts the *alerts* in the selected tab (e.g. "5 alerts in 3 active sessions"), matching the `all` chip, and the four category chips (prompts / errors / warn / info) sum to it. Alert cards wrap on narrow screens via CSS (`flex-wrap` + `overflow-wrap: anywhere`), so long session names and messages break inside the card instead of clipping past the phone edge, with the markup identical at 360 px and 390 px. See [`howto/alerts-and-notifications.md`](howto/alerts-and-notifications.md) and [operations.md](operations.md) (*Alerts tab (help link, state row, refresh)*).

There's also a **Core feature reference matrix** further down, listing which features have dedicated walkthroughs, plans, and architecture diagrams — the gaps in that matrix are what an upcoming docs-as-MCP-interface will fall back on.

---

## Table of contents

- [Sessions](#sessions)
  - [Sessions list](#sessions-list)
  - [Session lineage — parent-child tracking](#session-lineage--parent-child-tracking)
  - [Session AI summarizer](#session-ai-summarizer)
  - [Claude model defaults](#claude-model-defaults)
  - [explicit model](#explicit-model)
  - [Mechanical work](#mechanical-work)
  - [Message submission (paste wait and Enter retry)](#message-submission-paste-wait-and-enter-retry)
  - [Inside a session — terminal area](#inside-a-session--terminal-area)
  - [Inside a session — channel tab](#inside-a-session--channel-tab)
  - [Inside a session — stats tab](#inside-a-session--stats-tab)
- [Automata](#automata)
  - [Automata list](#automata-list)
  - [Launch Automaton form](#launch-automaton-form)
  - [Automaton detail — Overview / Stories / Decisions / Scan](#automaton-detail)
  - [Lifecycle subscriptions — outbound webhook and peer forwarding](#lifecycle-subscriptions--outbound-webhook-and-peer-forwarding)
  - [Events](#events)
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
  - [Push and tag](#push-and-tag)
  - [Release path](#release-path)
  - [Release CI window](#release-ci-window)
  - [Release record](#release-record)
  - [Merge shadow](#merge-shadow)
  - [Shadow merge plan](#shadow-merge-plan)
  - [Shadow merge checks](#shadow-merge-checks)
  - [Deploy after ship](#deploy-after-ship)
  - [autonomous.deploy.build_command](#autonomous-deploy-build-command)
  - [autonomous.deploy.install_command](#autonomous-deploy-install-command)
  - [autonomous.deploy.binary_path](#autonomous-deploy-binary-path)
  - [autonomous.deploy.restart_command](#autonomous-deploy-restart-command)
  - [autonomous.deploy.health_check](#autonomous-deploy-health-check)
  - [autonomous.deploy.drain_timeout](#autonomous-deploy-drain-timeout)
  - [autonomous.deploy.health_timeout](#autonomous-deploy-health-timeout)
  - [Onboarding report](#onboarding-report)
  - [Onboarding report checks](#onboarding-report-checks)
  - [Onboarding report next steps](#onboarding-report-next-steps)
  - [Model routing](#model-routing)
- [Automation](#automation)
  - [Automation list](#automation-list)
  - [Automation detail and pipeline strip](#automation-detail-and-pipeline-strip)
  - [Shipped](#shipped)
  - [Completed](#completed)
  - [Completed migration](#completed-migration)
- [Dashboard](#dashboard)
  - [Network card (work map)](#network-card-work-map)
  - [Lane pulse](#lane-pulse)
  - [Burn panel](#burn-panel)
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
- [Secure mode and encryption at rest](#secure-mode-and-encryption-at-rest)
- [Git hosts](#git-hosts)
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

**Controls:** all main controls sit in one compact toolbar line that never splits into multiple rows: a search box, a **Filters** button with a badge counting active filters, a **Fold all** button (while a grouping is active) and the **List / Tiles / Compact** view mode buttons (three icon buttons, the active one marked) (see [multi-servers: Switch between servers](howto/multi-servers.md#switch-between-servers-header-server-button)); one line below the toolbar the **Tree** and **History** buttons sit with the clickable **🌐** "back to local" indicator, shown while a federated server is the active one (that line is behind the header's 🔍 search-&-filters toggle — hidden by default, shown after you switch it on for this browser). Search matches name, id, backend, tag text and state, shows an **N of M** count while active, and the search text is remembered on the device. The **Filters** sheet holds **Group by**, saved **Views**, the **Status** chip group, the watched-only toggle, the **Backend** chip group and the Tags filters (the **Status** and **Backend** groups are multi-select sets — each chip carries the count of that state or backend family in the list, a zero-count chip stays in the group only while picked, several picks match *any* of the picked values, a second tap on an active chip removes exactly that one, and the group’s **All** chip, active while nothing is picked, clears the whole group; the two groups apply together and the **Backend** group never touches the search box; a pick of a historical state auto-enables the History view; the Tags filter is a search box, collapsible Kind / Role / Lane / Compute node / Project / Your tags / Other groups with a **Show all** control, and an **Automaton** picker; every chip and picker item leads with the readable name — tag, project/folder, node or Automaton title — the id riding only as small secondary text, and **unnamed** with the id when no name resolves, so a raw id is never the primary; the **Compute node** group is per node — one entry per local host under its real hostname or federated peer under its registered peer name, built from the sessions in the list, aliases merged, never “local” or “all”; the per-Automaton, per-stage and per-task tags get no chip; the picked **Status** chips, picked **Backend** chips and selected tags all show together as a compact row of removable chips above the list, with a **Clear all** that clears every pick across the three groups at once); it closes on a tap outside, its close button or Escape. When grouped, each group header shows a count, folds and unfolds, remembers its fold state per device, and the **Fold all** button in the toolbar line folds every group at once; on a phone-width screen a card's extra tags collapse to a single **+N** chip. With no grouping, **List**, **Tiles** and **Compact** always draw three different layouts: the **Tree** button (parent/child lineage) applies to **List** only, and choosing any mode in the view mode select switches the Tree choice off. The multi-select bar appears on the first checkbox tick. Nothing changed in settings or server behavior: these are browser-side choices with no setting, API, CLI or MCP surface. See [howto/sessions-tiles-and-grouping](howto/sessions-tiles-and-grouping.md).

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

Killing, deleting or ending the session does not cancel or fail a schedule created with a session name. It stays pending, goes to the next live session with that name, and raises one alert while there is none. A recurring schedule skips the fires it misses. A schedule created with a session id only ends with its session. Nothing is typed into a session that shows an approval or confirmation prompt. Details: [`howto/schedule-rearm.md`](howto/schedule-rearm.md#commands-sent-to-a-session-by-name).

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
| `effort` | string | `""` | Effort level: `quick`, `normal`, or `thorough` (claude-code: `--effort` `low`, `medium`, `high`) |
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

### Hermetic process-scan test

A test that decides which process sent a stop signal by scanning a process table limited to the test's own child processes, so unrelated host processes cannot change the result. `TestSignalChildProcScan` in `internal/shutdowncause` is one. It needs no running daemon. See [Running the process-scan tests](howto/testing-against-a-running-daemon.md#running-the-process-scan-tests) and the stop-sender note in [operations](operations.md).

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

### Gone session records

**Live session** — a session that something still drives: a terminal session or process exists for it, or the daemon holds a runner for it. A record in `running`, `waiting_input`, `stalled` or `rate_limited` that is not live is not really running, and the finaliser settles it. Sessions recorded by another host are never judged by this daemon. See [howto/sessions-deep-dive.md](howto/sessions-deep-dive.md#sessions-that-are-really-running).

**Finalised session** — a session record the finaliser set to its final state because its terminal session and process were gone. It becomes `complete` when its output ends with the completion marker, otherwise `killed`, and carries an end time (`ended_at`) and the note `process gone, finalised` (`end_note`). The record and its output are kept. Every other end (a kill, a session that ends on its own) writes `ended_at` and an `end_note` of its own, such as `killed by <caller>: <reason>`; only a session that still runs, or a record from before this was added, has neither. See [howto/sessions-deep-dive.md](howto/sessions-deep-dive.md#sessions-that-are-really-running) and [api/sessions.md](api/sessions.md).

**Live (judged liveness)** — the one shared answer to "is this session record still live?". It replaces re-deriving the answer from a single signal. A record is judged `ended` (already `complete`, `failed` or `killed`), `foreign` (belongs to another host, so this daemon never finalises or re-labels it), `live` (something still drives it: its terminal pane exists, its backend launch is still running, the daemon holds a runner for it, its recorded subprocess still runs the session's task, or the daemon's own report path ends it, as for a virtual backend or a session bound to an agent) or `gone` (not in a final state and none of those signs is present). Any one sign is enough to be `live`. A terminal-pane listing that fails for a reason other than "no server running" is never read as every pane being gone. This is a different thing from the zombie check above: the zombie check asks whether the model process still runs inside a pane that exists, while this judgement asks whether anything at all still drives a record that is not in a final state.

**Runner registry** — the daemon's in-memory list of the sessions it drives itself with a goroutine or process because they have no terminal pane. A subprocess session is added when it starts and removed when its process returns; a subprocess re-attached after a daemon restart is added again until its watcher finishes. It is not persisted, so after a restart it holds only what this daemon is driving now. Today only subprocess sessions are registered; chat-mode and other non-pane sessions are not yet.

**Gone final state** — what a `gone` record is finalised as. The tail of its output log is read and terminal escape sequences are stripped. If the last non-empty line begins with `DATAWATCH_COMPLETE:` the record becomes `complete`; otherwise (no marker, a marker followed by more output, or an empty or missing log) it becomes `killed`. The record also gets `ended_at` and `end_note`.

**`ended_at` / `end_note`** — the only two session fields about how a record ended: when it ended and a short note saying how. Every path that moves a session to complete, failed or killed writes both, and the first end wins. Restarting a session or bringing it back clears both. Both are left out of the session JSON while a session runs and on records that ended before the fields existed. Detail: [api/sessions.md](api/sessions.md).

**Verified task delivery** — for a session whose task the daemon types itself, proof that the task reached the session: its text was seen on screen after typing, or, after a daemon restart, the worker visibly has it. The claim saved before typing (`task_delivered_at`) is not proof; `task_delivery_unverified` stays true until it is. See [api/sessions.md](api/sessions.md#task-delivery-on-launch).

**Finaliser** — the pass that applies the live judgement and the gone final state. One run finalises the records the daemon holds that are still `running`, `waiting_input`, `stalled` or `rate_limited` and are `gone`, then the saved session folders (`<data_dir>/sessions/<id>/session.json`) in the same four states. A folder whose record the daemon holds follows that record; a folder with no record is judged by its own saved fields and finalised from the tail of its own output log. Records are saved as copies with the terminal re-checked just before writing; folders get only `session.json` rewritten (temporary file, then rename), and a `session.json` that cannot be parsed is skipped and counted. Nothing is ever deleted: the record, the output log and the project folder are kept. A failed terminal listing makes the pass finalise nothing.

**Finalise summary** — what the last finaliser run left in memory: the run time, the records finalised, the folders finalised, the number left as they were over both parts, and whether the terminal listing worked for both parts. Each run replaces it, and a run that finalises something also writes one counts-only line to the daemon journal. It is not saved to disk.

**Finaliser loop** — the background loop that runs the finaliser once at start-up and then after every interval. The daemon starts it right after it resumes its sessions. The interval is read again each cycle, so a change applies without a restart; zero means no scheduled runs (the start-up run still happens). It ends with the daemon.

**Finalise interval** — the setting `session.finalise_interval_minutes`: whole minutes between finaliser runs, default 10, 0 turns scheduled runs off. A negative value in the config file falls back to 10 and a negative value sent to the config API is ignored. Change it on the Settings page (General, Sessions), with `datawatch config set`, the `config_set` MCP tool, the `configure` chat command or `PUT /api/config`; no restart is needed.

**Status today:** the daemon starts the finaliser loop, so gone records and folders are finalised on the interval above and carry `ended_at` and `end_note`. The restart check still runs first and still marks a missing pane `failed`; the finaliser never changes a record already in a final state. Nothing shows the finalise summary yet (no REST, MCP, CLI, chat or PWA surface, and the PWA does not show `end_note`), and only subprocess sessions are registered as runners. Detail: [operations.md — Gone session records and the shared live judgement](operations.md#restart-safety) and [howto/sessions-deep-dive.md](howto/sessions-deep-dive.md#gone-session-records).

**Finished session close** — the setting `session.finished_idle_close_minutes`: end an interactive claude-code session that is proven finished and has stayed idle this many minutes, which frees its capacity slot. Default 0 (off); 12 is the suggested value; a negative value sent to the config API is ignored. The session is left `complete`, with its completion summary, its output log and the reason in `end_note`; unpushed commits follow the same push-before-teardown setting as a kill. Detail: [howto/sessions-deep-dive.md](howto/sessions-deep-dive.md#ending-finished-idle-sessions).

**Proven finished** — all of these, on every check (once a minute) for the whole period: the session is an interactive claude-code session in `waiting_input`; its screen ends on its own `DATAWATCH_COMPLETE:` line (not the one quoted in its task) with nothing after it but turn notes and the input box; no shell, monitor or background agent is shown as running; no permission dialog is open; its hooks show no turn still running; no tmux client is attached; no live child session exists; no scheduled input is pending for it; and it is not a one-shot, an Automaton's session, a supervisor or agent session, or a restarted or resumed session that has had no input since. Anything that cannot be read counts as not proven. The period starts again whenever the output or the text in the input box changes, and after a daemon restart.

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

**How it works:** At session start the daemon merges each extra entry into `<project_dir>/.mcp.json`, for every backend. Existing operator-added entries are preserved; only entries whose names match the extras list are managed. The `datawatch` bridge entry is never written there: see [Token in daemon-written MCP files](#token-in-daemon-written-mcpjson-files). An extra's own `env` values are written into the project file as configured, so do not put secrets there.

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

**How the store is protected.** The store is one file, and more than one process can open it (the daemon, a standalone `datawatch mcp` server, the `datawatch result` command). Every put, delete and expiry takes a lock on a sidecar `<file>.lock` file, re-reads the file and applies the change to what it finds, so a copy that is out of date can no longer erase entries another process wrote. Every save writes a temporary file in the same folder, syncs it, renames it over the old file and syncs the folder, so a crash or a full disk leaves the previous file whole. `datawatch result put` and `delete` go through the running daemon; `get` and `list` only read the file and work without the daemon.

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

### Scheduled spawn re-arm

What a scheduled spawn does when its session does not start. A start refused for
capacity (a full session cap, the global cooldown, a draining daemon, a capacity
wait timeout) is a **wait**: the schedule stays pending and retries after a pause
that doubles up to a ceiling, never later than its next scheduled time, and is
never marked failed. Any other failure follows the **re-arm policy**:
`next_fire` (default: try again at the next scheduled time), `backoff` (retry
sooner, up to a retry count, then give up) or `stay_failed` (fail at once). When
the policy gives up, the schedule stays failed and an alert is raised. Each
schedule list shows the next fire, the last result with its reason and the
re-arm state (`retrying`, `waiting_capacity`, `gave_up`).

- **Config:** `schedule.rearm_mode`, `schedule.rearm_max_retries`, `schedule.rearm_backoff_seconds`, `schedule.rearm_backoff_max_seconds`, `schedule.one_time_max_wait_hours` (a one-time schedule refused for capacity waits this long, default 12 hours, then fails with the reason and an error alert; recurring schedules wait without a limit); read at every failed start
- **CLI:** `datawatch schedule list` (REARM column); `datawatch config set schedule.rearm_mode backoff`
- **MCP:** `schedule_list`; `config_set key=schedule.rearm_mode value=backoff`
- **REST:** `GET /api/schedules` (`last_fire_result`, `last_error`, `rearm_state`, `consecutive_failures`, `consecutive_waits`, `fire_history`); `PUT /api/config`
- **Chat:** `schedule list`; `configure schedule.rearm_mode=backoff`
- **Web:** Settings → General → Scheduled Spawns; a line under each schedule in the Monitor tab's Scheduled Events list

A due **command** is typed at most once: the daemon saves a claim on the fire
(`firing_since` in `GET /api/schedules`) before it sends any key, and types one
scheduled command at a time into a session. A send held by a prompt gives the
claim back and the schedule stays due. A claim still there when the daemon starts
means a restart cut the send off; it is not typed again (a recurring schedule
moves to its next fire, any other is marked failed) and a warn alert is raised.

See [howto/schedule-rearm](howto/schedule-rearm.md).

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

### Claude model defaults

The daemon's own model choices for Claude Code launches when no operator layer names a model: **`sonnet`** for any worker, verifier, judge, or planner launch whose model is otherwise empty, and **`haiku`** for autonomous mechanical work (docs, changelogs, parity-issue drafts, bounded load steps, version notes, locale strings, formatting cleanup). These are datawatch defaults, never the operator's Claude account default, and the daemon never silently upgrades to a more expensive model. See the [Claude model defaults how-to](howto/claude-model-defaults.md) and [explicit model](#explicit-model).

**Where it shows up:** the model field in session records, worker attempt records, the judge's decision log, and the planner's record — all naming exactly the value passed to the CLI as `--model`; see the [Claude model defaults how-to](howto/claude-model-defaults.md) and the [operations entry](operations.md#claude-model-defaults).

### explicit model

A model explicitly named by the operator — at the session, task, story, PRD, automation, or config-line level — as opposed to a daemon default. An explicit model always wins over the daemon default and is never substituted: the session record holds exactly the value the CLI received as `--model`. A Claude Code launch whose model is empty after every layer is refused up front rather than started (see [Claude model defaults](#claude-model-defaults)); naming an Opus-class model is only possible this way. Every direct start surface can name it: CLI `datawatch session new --model <name> [--effort quick|normal|thorough]`, MCP `start_session` (`model`, `effort`, `name`), chat `new: model=<name>: <task>`, REST `model` on `POST /api/sessions/start`. When none is named the daemon falls back to the LLM row's model, then the global planning model, and refuses a `claude-code` start only if all are empty; an empty `model=` in chat is rejected with a reply asking for a value. **Operator review:** confirm the `quick|normal|thorough` effort values match the daemon, and that the PWA start form is covered separately. See the [explicit-model policy](howto/explicit-model-policy.md) how-to.

### Mechanical work

The volume tasks the daemon runs inside an Automaton as part of its own standard work — **documentation sets, changelog entries, parity-issue drafts, bounded load steps, version notes, locale strings, formatting cleanup, and test runs** — which run on the cheaper `haiku` default instead of the usual `sonnet`, keeping cheap work fast and the premium model reserved for substantive work. See [Claude model defaults](#claude-model-defaults).

### Output filters that type into sessions

An output filter watches session output for a pattern. Two actions **type into the session**: `send_input` (type now) and `schedule` (type when the session next waits for input). These are the rules the daemon enforces for them; no model has to cooperate.

- **None is built in.** A fresh install has no filter that types. An upgrade turns off the built-in typing filter that older versions shipped (it answered Claude Code's approval prompt by itself), records why in the filter list (`disabled_reason`), cancels what it had already queued, and refuses to turn it back on.
- **Off by default, twice.** The daemon-wide switch `session.input_filters_enabled` is off, and each typing filter is also created turned off. Any edit to a typing filter turns it off again.
- **Made on purpose.** Creating one needs an explicit confirmation: the web app asks, the CLI needs `--types-input`, the REST API needs `allow_input: true`. Over MCP they cannot be created or turned on, and the switch cannot be turned on. Chat can list filters and turn one off.
- **Never answers a prompt.** A pattern that matches a permission, safety or confirmation prompt (including catch-all patterns) is refused. The value must be one line of visible text: no bare Enter, no control keys. At run time nothing is scheduled or typed from a prompt line, or while such a prompt is on screen.
- **Always marked.** Every line a filter types starts with `[datawatch filter <id> typed this, not the operator]`. The session timeline records `source:filter:<id>`, the conversation file says it was typed by that filter, and the audit log has a `filter_input` entry with actor `filter:<id>` and the text. Because of the mark, a typed line is not a shell command or slash command on its own.

**Everything else the daemon types on its own** (scheduled commands, nudges, loop rescue, supervisor wake, exit hooks, lifecycle and discussion pushes, task delivery, replies from child sessions, question answers, rate-limit and MCP retries, startup context) follows the same prompt rule. Claude Code's approval menu has the accepting choice preselected, so any text followed by Enter would approve it. The daemon reads the live screen first and types nothing while a permission or confirmation prompt is there, or when the screen cannot be read. A refused send is audited as `automated_input_refused` (at most once per session and source every 10 minutes). A queued command stays pending and is typed after a person answers the prompt; the other senders report the refusal and do not keep the text. Input a person sends (web, chat, CLI, REST API) is not affected: answering a prompt remotely works as before. MCP tools are not in that list: see [MCP tools and waiting prompts](#mcp-tools-and-waiting-prompts). One case is narrower: a chat message that names no session goes to the single waiting session, and while that session shows a prompt only a plain answer (yes, no, y, n, always or a choice number) is typed; anything else is refused with a note to use `send <id>:`.

The one deliberate exception is the launcher's startup auto-accept (folder trust, development channels, MCP server and bypass-mode dialogs). It never acts while a tool or safety approval prompt is on screen.

### MCP tools and waiting prompts

Whoever calls an MCP tool may be an AI session, so a tool that types into another session (`send_input`, `send_saved_command`, `reply_to_parent`, and the push to discussion subscribers) follows the rule above. While the target shows an approval, permission, safety or other confirmation prompt (any numbered choice menu or yes/no question counts), or while its screen cannot be read, the tool is refused: the refusal says that a prompt is waiting for the operator and that nothing was typed. The screen is read again before Enter; if a prompt has appeared by then, Enter is not pressed and the refusal says the text may be left in the session. Each refusal is audited as `automated_input_refused` with actor `daemon:mcp` (or the push's own source), at most once per session and source every 10 minutes. There is no setting and no exception. The rule is the same for calls that arrive through `POST /api/mcp/call`, a federation peer or `datawatch mcp`.

No MCP tool can call the daemon routes that type into a session (`/api/sessions/send`, `/api/sessions/{id}/input`, `/api/test/message`, `/api/proxy/…`), whatever its arguments contain; such an attempt is refused and audited with source `mcp-route`. Tools that only queue text (`schedule_add`, `autonomous_subscribe`, `exit_hook_add`, `question_answer`) are accepted, and the daemon applies the rule when it comes to type.

This stops answers sent through these tools. It does not prove who answered a prompt, and an agent with a shell can still reach a session another way.

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

### Message submission (paste wait and Enter retry)

How datawatch makes sure a message sent to a session is sent, not just typed. A long, multi-line or image-bearing message reaches Claude Code as a **paste placeholder** (`[Pasted text #1 +12 lines]`), and an Enter that arrives before the terminal has taken the paste in can be dropped, leaving the message sitting unsent in the input box.

Every send from the web app, the terminal input bar, REST, MCP, the CLI and comm channels now follows one sequence: type the text and pause for the settle delay (`session.schedule_settle_ms`, default 200 ms); **wait up to 1.5 seconds** for the placeholder, or the end of your text, to appear next to the input prompt; press **Enter once**; and, for a pasted, multi-line or longer message, re-read the terminal after about 0.4 seconds and press **Enter again** (at most two more times) only while the placeholder or text is still in the input box and the assistant has not started. Once the placeholder is gone or the assistant is working, no further Enter is sent, so an Enter that worked is exactly one submit. A short single-line message gets one Enter and no re-check; an empty message is a bare Enter.

The timings are fixed and there is no setting, endpoint or field for them; only the settle delay is tunable. A send that runs out of retries still counts as delivered and leaves the text in the input box for you to submit by hand. Chat-mode ollama sessions do not go through this path. Not to be confused with **task-prompt delivery** (`session.task_delivery_window_seconds` and `session.task_delivery_attempts`), which re-delivers an Automaton task's prompt until assistant activity is seen. Delivery waits for the Claude main prompt; the check recognises the Claude 2.1.330 idle screen (a ready prompt box), while trust-folder and other framed choice dialogs are not treated as ready. See [How a sent message is submitted](howto/sessions-deep-dive.md#5h-how-a-sent-message-is-submitted) and [Message submission to a session](operations.md#message-submission-to-a-session-paste-wait-and-enter-retry).

### Inside a session — terminal area

`Tmux` tab is the live xterm.js view of the tmux pane the LLM is attached to. Read-only by default; tap the input bar to send commands.

**Layout:** the session page (Tmux / Channel / Status) is a full-height, non-scrolling page: the tab content fills the space between the header and the keyboard-aware bottom of the screen, the input bar is the last element and nothing is rendered below it — no blank band under the input row, for local and peer sessions alike. The bottom nav is hidden while the page is open. The sizing is pure flex / viewport units with no fixed pixel heights, so window resize, browser zoom, the software keyboard and the iOS safe-area insets keep the page filled, and the terminal re-fits on resize without a reload.

**Header icons (phone width):** on a phone-width screen the session header carries two icon buttons in place of the open tag chips and Scheduled list: a **tag** icon (badge = number of tags) and a **clock** icon (badge = number of scheduled jobs; its tooltip gives the next run time). A zero count hides the button. Tapping one opens its list in place (tag chips, or one row per job with a cancel control); only one list is open at a time, and it closes on a second tap of the icon, a tap outside, or Escape. Each is a button with an accessible name and `aria-expanded`. Open state is held in page memory only (no storage key, no setting); wide screens keep the full chip rows and per-job rows and show no header icons. Browser-side only: no API, CLI, MCP or comm surface. See [howto/sessions-deep-dive](howto/sessions-deep-dive.md).

**Toolbar:**
- `Aa ▾` font controls — A−, A+, current size, Fit-to-width.
- `Scroll` — enter tmux scroll mode (Page Up / Page Down / ESC). Ties into tmux's own scroll-back so you see real history.

**Input bar:** sends text with Enter; the daemon routes it through tmux send-keys and confirms the message was submitted (see [Message submission](#message-submission-paste-wait-and-enter-retry)). State transitions back to `Running` automatically on input (and bumps the channel-event timer so the gap watcher doesn't immediately re-flip).

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

For claude-code sessions, the daemon auto-installs a `.claude/sprint/post-event.sh` hook script into the project directory on session start. The environment file beside it holds the daemon URL and session id, never a token. Other backends (opencode, opencode-acp) emit equivalent events through their own hook paths.

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

## Secure mode and encryption at rest

> Page: [encryption.md](encryption.md) · Evidence: [encryption audit](plans/2026-11-13-encryption-audit.md)

- **Secure mode** — the daemon runs with `--secure` (or finds an encrypted `config.yaml` and turns it on by itself). It asks for a password, from `DATAWATCH_SECURE_PASSWORD` or a prompt, and uses it to encrypt `config.yaml` and a defined set of data stores. It does not cover every file in the data directory.
- **Data key** — the 32-byte key every encrypted store uses, derived from the password and the salt in the `config.yaml` header. It exists only in daemon memory. There is no command to change the password or rotate this key.
- **Encrypted store** — a file the daemon writes through the encrypting layer when the key is present: whole-file (`DWDAT2`), streaming blocks for logs (`DWLOG1`) or one line at a time (`ENC:`). Without secure mode the same files are plain.
- **Plain today** — a store datawatch could encrypt but does not yet, such as the token and credential files, the Automaton store, spend files, most tracker files and the audit and trace logs. The operator decides which of these are covered for 10.0.
- **Cannot be encrypted** — data another program must read in plain form (git repositories and worktrees, files the launched tools read, boot messages written before the key exists) or that is not at rest (network traffic, pane captures). Use disk-level encryption for these.
- **Secrets vault** — `secrets.db`, encrypted with its own random key file `secrets.key` (or `DATAWATCH_SECRETS_KEY`), separate from the secure-mode password. The key file sits in the same folder as the vault.
- **Fixed file list** — the files that `datawatch security encryption status`, `migrate` and `wipe-plaintext` act on. A clean status covers only these files.

## Memory ceiling

- **Memory ceiling** — the most memory one worker pane or verification run may use: a share of host memory, kept between a floor and a cap. See [Memory ceiling](operations.md#memory-ceiling).
- **Scope** — the systemd user scope the worker's shell runs in; the kernel holds the ceiling there.
- **Watchdog path** — the fallback when there is no user scope: the daemon watches the pane's process group through `/proc` and ends it when it passes the ceiling.
- **No-ceiling status** — `enforcement_path` is `none` and `effective_mb` is `0` when the host has neither a user scope nor `/proc`. The run is not capped. A project or task kind opt-out (`none`) shows only in the worker session's own `ceiling_path` and `ceiling_mb`, not in the status object.

## Automata

**Automaton id** — eight hex characters. The daemon issues an id only when no record, record file, `automaton/` branch or worktree folder already holds it, and at start it raises a warning alert for any record whose branch or worktree folder is older than the record. See [howto/autonomous-recovery.md](howto/autonomous-recovery.md) (*An Automaton id that was issued twice*).

### Automata list

Every PRD / autonomous workflow your daemon has spawned. Filtered by state pill (Running / Stopped / Failed / All).

The **Tags** filter selects Automata by tag with match ANY (default) or match ALL; an empty selection lists everything. The flat list view also has a **List / Tiles / Compact** toggle and a **Group by** select (see *List modes and group-by*). See [howto/automata-tags.md](howto/automata-tags.md) and the *Tag selection* glossary entry.

**Summary list and Load more** — the Automata list loads one page of 50 summary rows (counts and identity, no story or task text) from the daemon's in-memory summary index, so the tab opens quickly however much text the Automata hold. **Load more** fetches the next 50; opening a row fetches that one Automaton in full. See [howto/automata-tab-loading.md](howto/automata-tab-loading.md) and [howto/list-endpoints-and-paging.md](howto/list-endpoints-and-paging.md).

**Archived Automaton** — a completed, shipped or cancelled Automaton older than the *retention window*, `autonomous.archive.retention_days` (default 30), whose bulky parts (decision log, task session history, long errors, memory report, raw judge text) were moved to one archive file. The live record keeps its summary, counts, ids, statuses and every story and task title, so lists, counts and cost read as before; nothing is deleted, and `GET /api/autonomous/prds/{id}/archive` reads the moved parts. `autonomous.archive.enabled` (default true) lets the daemon archive on its own; `datawatch autonomous archive` reports a dry run and `--apply` does it after a verified backup. See [howto/automata-archive.md](howto/automata-archive.md) and [operations.md](operations.md) (*What the archive does*).

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
- **Stories** — per-story state + Edit / Profile / Files / Approve / Reject / Add story / Remove story. Each task under a story exposes a triangle/expand button; click once to expand inline — the expanded state persists until clicked again, and each task has its own Edit / Add task / Remove task actions. Adding or removing a story or task is a structural edit that skips the LLM entirely — use it to correct or extend a decompose result without re-running planning. Available pre-approval (`needs_review` / `revisions_asked`) and on a `failed` Automaton, where resetting a task reopens it as `running`; a running or completed Automaton keeps its structure locked. A cancelled Automaton is kept for `autonomous.archive_grace_minutes` (default 60, `0` = at once) before the supervisor loop archives it, and any finished Automaton whose branch holds commits that are not on the base branch is kept until that work lands or the branch is deleted — the loop's guard is separate from this grace. See [Repairing a Failed Automaton in Place](operations.md#repairing-a-failed-automaton-in-place).
- **Capacity** — shown on a running Automaton's active-session card: per-pool used/limit bars (host sessions, compute node, LLM in-flight) plus a wait queue when a task is waiting for a free slot. "Host sessions" is a machine-wide cap shared with interactive sessions — not specific to this Automaton.
- **Verifying lease release** — when a task enters `verifying`, its worker's capacity slot is released at once and the verifier takes its own slot (`verify:<task>`). The slot is not handed over, so a slow verification does not hold a lane for other work. A task in verification is listed under `tasks.verifying` in the capacity response, under *Verifying* on the Capacity card and under `Verifying (n):` in `datawatch capacity`. Its state is `verifying` while the verifier runs, or `waiting_for_verifier` while it queues for a verifier slot. See [howto/automata-capacity.md](howto/automata-capacity.md#the-worker-slot-at-verification).
- **Pool fall-through** — the setting `autonomous.capacity_fallthrough` (default off). On, a worker task or a planner whose requested pool cannot be granted is admitted on an alternate backend that has room instead of waiting (a planner is judged on its own pools and records a `planner_fallthrough` decision). The requested pool is the one the task's effective backend maps to (task, story, PRD, Automaton, lane preference, daemon default). Alternates are tried in order: the other lane fill backend on its default model, then LLM entries marked as targets in name order; a disabled, full or non-serving backend is skipped, and verifiers and judges never fall through. A waiting task re-reads the setting on every wait pass, so turning it on admits waiting tasks with no restart; an explicit backend is held to the same rule and falls through only while the setting is on. The admission is audited as `autonomous_capacity_admitted` with `fall_through`, `from_pools`, `to_pools` and `note`, and the note ends `; explicit backend; fall-through setting on` for an explicit backend. See [howto/automata-capacity.md](howto/automata-capacity.md#pool-fall-through).
- **Allowed fall-through target** — an LLM entry setting (`fallthrough_target`, default off). With `autonomous.capacity_fallthrough` on, a worker whose requested pool is full may be admitted on a marked entry instead of waiting. The other lane fill backend is tried first without any mark; an alternate that is disabled, full or cannot serve the model is skipped, and verifiers never fall through. Set it with `fallthrough_target: true` in YAML, `datawatch llm update <name> --fallthrough-target`, the web app LLM edit switch, `PUT /api/llms/<name>`, MCP `llm_update` or `llm_add`, or the comm channel `llm update <name> fallthrough_target=true`.
- **Worker tmux kill guard** — the setting `autonomous.tmux_kill_guard` (default on). On, every worker, verifier and judge pane the daemon starts finds a guard `tmux` first on its `PATH`, for every backend, on first launch and on restart. The guard refuses a server-wide kill (the server kill, or the session kill with `-a`) unless the call names a throwaway socket with `-S <path>` or `-L <name>`; a path under the daemon's own tmux socket directory, `-L default` and a bare `TMUX_TMPDIR=` prefix are still refused. Every other tmux call runs the real tmux unchanged. It stops the accidental call, not one that runs the real tmux binary by its absolute path; operator sessions are never guarded. Set it with `autonomous.tmux_kill_guard` in YAML, `PUT /api/config`, MCP `config_set`, `datawatch config set`, the chat `configure` verb or the Settings switch. See [operations.md](operations.md).
- **Live setting and restart-needed setting** — a config key is either live (it takes effect as soon as it is saved) or restart-needed (the daemon reads it once at start, for example `server.port`, `mcp.sse_port` or `database.*`). `PUT /api/config` lists the saved keys under `applied` and `requires_restart`; `datawatch config set` and the MCP `config_set` tool print which one applied; the web Settings page shows a Restart needed badge; the daemon logs the restart-needed list once at start. See [operations.md](operations.md).
- **Admission class** — how the daemon admitted a session start on the capacity pools, decided by how the daemon launched it and never by a field in the request (the old `one_shot` flag is accepted and ignored). **Worker**: an Automaton's own launch (task, decompose, verifier, judge), admitted by the Automata manager before it starts; the restart of a worker session (rate-limit fallback, exit hook) is a worker start that queues at the head, ahead of every other waiting worker, and still respects the cap. **Supervisor**: a supervisor loop job or a schedule whose launch role is supervisor. **Operator**: every other start (the app, the CLI, chat, MCP, the API, restarts); a start from any outside client is an operator start whatever it sends. Every start path is admitted at the session manager, and a static test lists them. See [howto/automata-capacity.md](howto/automata-capacity.md#who-is-admitted-worker-supervisor-and-operator-starts).
- **Supervisor credential** — a named token an operator mints (`datawatch capacity supervisor-credential mint --name detector`, or `POST /api/capacity/supervisor-credential`) so an external supervisor such as the detector timer starts sessions over REST in the supervisor class. It carries only `sessions:write` and `capacity:supervisor-start`, is shown once at mint, and the operator can list it (by name) and revoke it at once. Only the operator can mint it: a session credential or a federation peer cannot, and a session token that merely lists the capability gets nothing. A start with it may use the supervisor buffer on a pool and never goes past it; a full buffer waits, then is refused. Every start with it is audited as `supervisor_credential_start` with the credential name. A start without it is an operator start. It is a stopgap, removed when the daemon runs the detector itself; `autonomous.capacity_supervisor_credential_enabled` (default true) switches every credential off without revoking it.
- **Operator overage** — an operator start is never refused by a pool limit. When the pool is full the session is admitted over the limit and its lease records the overage (the pool, who, how far over). The overage counts in the pool's used figure, shows on `GET /api/capacity` (`over` on the pool, `class` and `overage` on the lease), in `datawatch capacity`, in the chat `capacity` verb and in the PWA capacity view, and raises an alert when `autonomous.capacity_operator_overage_alert` is on. Autonomous work waits until the pool is back under its limit. A compute node's engine slots (`node:` and `node-verify:` pools) are soft by default like every other pool: how many models an engine runs at once is up to the user's hardware, so an operator start over a node's slots is admitted and the overage is recorded. A node whose `hard_limit` is on keeps the old behaviour for every class: an operator start waits for a free engine there. Workers wait on a node's slots either way. The host session cap (`session.max_sessions`) holds for operators after an **operator allowance** above it: `autonomous.capacity_operator_reserve_host` (default 5) slots only operator starts may use. An operator start at the cap goes into the allowance, raises an alert, and its session records how far over the cap it is (`over_session_cap`; `session_cap.over` on `GET /api/capacity`). Past the allowance the start is refused with a reason naming the setting. Workers never use the allowance, and supervisors have their own host slots (`autonomous.capacity_supervisor_reserve_host`).
- **Worker over the limit** — an adopted worker lease (work that survived a restart) that sits in a pool or node holding more than its limit. The session is real and stays recorded; new work waits until the pool or node is back under its limit. The capacity read flags the lease with `over_limit: true`, `over_limit_code: adopted_over_limit` and an `over_limit_reason` naming the pool or node, what it holds and its limit; the web Capacity card marks the holder **over the limit**. A worker admitted through the queue is never flagged, and an operator session over a cap shows its own overage instead. See [howto/automata-capacity.md](howto/automata-capacity.md).
- **Hard limit (compute node)** — a per-node setting, `hard_limit` (default off), for a user who knows the node's engine truly cannot take more models at once. Off, the node's engine slots are soft: an operator start may go over them and the overage is recorded; on, that node's `node:` and `node-verify:` pools wait for a free engine for every class, as they did before. Workers wait on a node's slots whichever way it is set, so where workers are placed does not change. Set it in the node form of the web app, `datawatch compute node update <name> --hard-limit`, REST `PUT /api/compute/nodes/{name}`, the MCP `compute_node_update` tool, the chat `compute node update <name> hard_limit=true`, or `hard_limit: true` on a `compute_nodes` entry in `config.yaml`. A peer's node setting is its own: each daemon reads only its own node records. See [howto/compute-nodes](howto/compute-nodes.md) and [howto/automata-capacity](howto/automata-capacity.md#soft-node-slots-and-hard_limit).
- **Supervisor buffer** — slots above a pool's limit that only supervisor-class starts may use, so supervisors can always start and never queue behind workers: Claude 1 (`autonomous.capacity_supervisor_reserve_claude`), pi 1 (`autonomous.capacity_supervisor_reserve_pi`) and 4 host session slots above `session.max_sessions` (`autonomous.capacity_supervisor_reserve_host`). Supervisor sessions are still counted in each pool's used figure, so usage shows them by class. A usage back-off or burn hold does not block them.
- **Account pool** (`account:claude`) — one pool that every claude-code LLM entry also draws on, so the per-entry limits cannot add up past the account. Its limit is `autonomous.capacity_account_claude_limit` (default 2: claude-code 1 plus claude-lean 1; 0 = unlimited). A subscription backend a start names that matches no LLM entry and no compute node is reported (`unmapped` on `GET /api/capacity`) and held to one session, never treated as unlimited. Federation: each host has its own local caps (a peer's `llm:` and `node:` pools are its own and add to the federated capacity, shown per host in the cluster view). The account pool alone spans hosts: a peer's leases count against it here only for a confirmed `billing_group` fingerprint match (the peer's leases on the matched entry) or a `peer_window` opt-in (its whole account pool); a pending match counts nothing and is reported as a possible shared account awaiting confirmation; a peer with a separate Claude account adds its own.
- **Decisions** — every state-changing event for this Automaton; click any row to expand the raw `details` payload. Filter by source (operator / autonomous / scan / etc.).
- **Scan** — Run Scan kicks off a verifier sweep against the spec; shows pass/fail across SAST / secrets / deps / LLM grader. History persists.

The header strip carries the **initial spec** (expandable — first 280 characters shown; click "show full" to reveal the complete spec inline, "collapse" to restore); **Status badge**; **Status graphs** (visible while running or decomposing — shows ✓/✗ decompose indicator, total story + task counts, per-story progress bars with X/Y tasks and %, and async CPU%/RSS from active compute envelopes); **Settings** (`openPRDSettingsModal` — type, execution backend, planning backend / decomposition profile, effort, model, skills, guided mode); **Cancel** (always visible for non-cancelled automata, including while running); **Request Revision**; **Clone to Template**; **Delete**; and **⋯ Edit** dropdown (Edit Spec, View Sessions, and other state-appropriate actions).

**Story-failure behavior:** by default, a story failing halts the whole Automaton (`status: blocked`) instead of continuing into later, independent stories — so a broken first story can't let the run barrel ahead unattended. Fix or retry the failed task (Settings → task Retry, or the CLI/MCP `reset_task`), which also un-blocks the Automaton automatically. To restore the old "keep going regardless" behavior, set `continue_on_story_failure` — a global default in Settings → General, or per-Automaton in Settings.

**Triage card:** an Automaton that ends up failed or blocked carries a triage card on this page (and on the Automations Inbox, where the needs-attention card holds one card per such Automaton): where it stopped (the blocked or failed task), the reason — a blocked card shows the whole reason, a line naming where it came from (the Automaton's own block reason, a guardrail verdict, a task error or an ended session, or a task held for review), and one concrete next step — when it first entered that state, what the last session was ended for, the four retry budgets, and the automation triage alert once the Automaton has sat for `autonomous.automation.triage_alert_minutes` (default 30 minutes, `0` = off; the daemon refuses to load on boot with a negative value).

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
- **Forwarded event** — a lifecycle event received from another daemon through peer forwarding. It is re-emitted locally with its origin host, is shown as "(forwarded from …)" in human sinks, and is never forwarded on or used to drive an Automaton. It carries the sending peer's name as `peer`.
- **Peer name on an event** (`peer`) — the registered name of the federation peer that delivered a forwarded event. The receiving daemon sets it from the authenticated identity of the request, never from the payload, so a `peer` value in the body is ignored. A locally originated event, and a forward delivered by an admin caller, has none.
- **Peer filter** — the optional `peer` field of a subscription filter (and the `peer` parameter of the MCP lifecycle events resource). It matches only events that peer forwarded to this daemon; a local event never matches it. It must be 1–64 letters, digits, `.`, `_` or `-`. It is set through REST and the MCP resource URI; the CLI, comm verb, `autonomous_subscribe` tool and PWA form have no field for it yet. See [Events from a federation peer](howto/automaton-lifecycle-subscriptions.md#events-from-a-federation-peer).
- **Peer event subscription** — the pull direction of federation: this daemon reads a registered peer's pending-events queue with the peer's stored bearer token and feeds each event into local dispatch with `peer` set. It needs the `autonomous:read` grant for the peer, reads lifecycle events only, and drops an event the peer had itself received as forwarded. It runs only while `autonomous.notify.forwarding.peer_events` and `autonomous.notify.forwarding.enabled` are both true, and you start, stop and read it with `datawatch autonomous peer-events` (also the `autonomous peer-events` comm verb, `/api/autonomous/peer-events` on REST and the `autonomous_peer_events` and `autonomous_peer_events_control` MCP tools).
- **Peer down** — a federation peer that hangs or refuses connections. It never delays or drops local delivery: every local sink still receives each local event once. Forwarding to the downed peer retries on the sink's schedule and, after the last attempt, drops that delivery with one alert; when the peer returns, forwarding resumes, with no outbox for what was dropped. Covered by `TestPeerDownDoesNotBlockLocalDelivery` and `TestPeerDownRecovers` in `internal/autonomous/lifecycle_peer_down_test.go`. The 2026-11-16 change around these tests was test-side only (a locked `gotPin` in the shared `fakePlacementAdvisor` and a wait-then-read fix in the recovery test); the behavior described here is unchanged — see [testing.md](testing.md#peer-down-recovery-test-flake-2026-11-16).
- **Hop cap and visited list** — the loop guard for forwarding. A forwarded event records the origin host, the number of hops and the hosts it has passed through. A receiving daemon refuses an event that started on its own host, that already visited it, or that has reached the hop cap.

---

### Events

**Events** is the third sub-tab of the Automata view (between Automata and Templates): one newest-first stream of what the daemon watches and does for you. A row is an **event**; the kinds are `watch`, `decision`, `retry`, `check`, `cleanup`, `alert` and `error`. Each row shows the time, the kind, the project, and the owner — an **Automation** (the parent) and/or an **Automaton** (the child); expand it for the detail, the reason and the next action.

- **watch** — supervisor watch findings (open findings; a resolved one is a quiet row) and the repo CI watch state: off (a quiet row that says how to turn it on), not polled yet, or one warn row per failing or pending CI run with its run link.
- **decision** — the recent lane fill planner decisions: start Automaton, decompose, report merge, blocked (warn), idle (quiet), or a decision that ran with an error.
- **retry** — one row per task retried or held after an infrastructure failure; a held row says how many retries were spent and what to do.
- **check** — the findings the Automata checks recorded; the reason names the check that wrote it.
- **cleanup** — one row per branch in the safe-to-delete report (held-for-uncommitted-changes rows are warn) plus one cleanup status row for both passes and the disk readings (error when a pass failed, warn while a disk alert is on).
- **alert** — part of the kind vocabulary, reserved for daemon alerts; today an alert surfaces as the **warn** severity of the feed's own row rather than a separate row. The one exception is an expired operator hold, which shows as one warn row, "Hold expired: <title> is ready to review".

**Held (Automaton)** is a status an operator sets on purpose: the Automaton waits by decision, so it is not blocked, failed or waiting for review, and no start path, lane fill pass or housekeeping step touches it or its tasks. A hold records a required reason, an optional free-text condition (never evaluated), an optional look-again time and the status to restore on release. When the look-again time passes the status does not change: the hold is marked expired once, with one decision entry, one warn alert and one Events row, and the Automaton stays held until an operator extends or releases it. Held Automata are counted apart from blocked and failed ones in triage, supervisor watch, progress and the dashboard console. A hold migration lists blocked or failed look-alikes and holds the ones the operator chooses, with a reason the operator supplies. See [howto/automaton-hold](howto/automaton-hold.md).
- **error** — the raw errors the daemon recorded, classified into what happened, why it matters and what to do next. A harmless permission error (the daemon touching a path it does not need) is a quiet info row; a real permission denial or any other failure is an error row with a **Next action** button that opens the tab that owns the event.

A **quiet** row is background state that needs no action — resolved findings, an idle lane, the watch off, safe branches, harmless permission errors. The cards that used to sit on their own panels (supervisor watch, recent decisions, infrastructure retries, Automata checks, safe-to-delete, cleanup status) are rows here now; their reads over REST, MCP, CLI and chat are unchanged.

Reads: PWA (kind chips, project, search, Load more); REST `GET /api/autonomous/event-log` (`kind=` comma list, `project=`, `q=`, `limit=` 1–200 default 50, `before=` the previous `next_cursor`; the body carries `total` and `next_cursor`); CLI `datawatch autonomous event-log`; comm `autonomous event-log`. See [howto/automata-events.md](howto/automata-events.md).

### Shipped

> **Version: v8.8.0+**

**Shipped** is not a status and not a label an operator sees on a finished record: finished work reads **Completed**. Shipped is only the name of the release detail (release, merge time, merge commit, source) and of the record shown when you hover the Completed chip, for work that was merged code.

"Shipped" is what **happened to the work** after a PRD or Automation finished, not a status of the PRD or Automation itself. When the daemon merges the work into a release, it stamps the record with the release name (`merged_in_release`), the merge time (`merged_at`) and the merge commit (`merge_commit`). The record keeps the status it had when the work landed (a cancelled PRD can still be shipped — its cancellation is historical, the work still reached the release), and the card's border color and action buttons still follow that stored status.

The shipped-state surfaces in the PWA everywhere a status is shown. **A record that carries shipped state displays as Completed — there is no "Shipped" label, chip text, filter badge or count bucket.**

- **Completed chip.** Wherever the status pill is rendered (the Automata list, the PRD detail header, the Automations list and detail), a shipped record instead shows the green chip, whose text is the ordinary **Completed** label. The chip takes precedence over the status pill — a cancelled-but-merged record shows **Completed**, not Cancelled. The chip tooltip adds the release, merge time and merge commit when the record has a merge commit.
- **Detail.** The release (`merged_in_release`), merge time (`merged_at`), merge commit (`merge_commit`) and source (`shipped_source`) stay on the record and are returned only for a record that carries a merge commit; the separate shipped line in the detail views is gone.
- **Filter badge.** The Automata and Automations filter bars have no Shipped badge. Shipped records are selected by the **Completed** badge, because the status filter matches the displayed status. `status=shipped` is still accepted by the REST list as a legacy alias, and `shipped=true|false` still selects on shipped state.
- **Counts.** Records that carry shipped state count in the **completed** bucket together with stored-completed records, so finished work is never counted as cancelled. `autonomous.shipped.count_separately` is kept for compatibility and no longer creates a second bucket. The dashboard shows the shipped total as a Completed count when it is greater than zero.

Behavior is server-configured under `autonomous.shipped` — `enabled` (toggle the whole surface, default on), `show_chip` (the chip visibility, default on), `filter_default` (the starting filter selection — `all`, `shipped` or `unshipped` — default `all`), and `count_separately` (kept for compatibility; it no longer creates a separate bucket, default on) — and is reported with the shipped record counts at `/api/autonomous/shipped/counts`; the list endpoints take the selection as `shipped=true|false` and honor `filter_default` when it is absent.

## Automation

An **Automation** is a named chain of Automata: it decides which Automaton runs next when the current one completes, is blocked, or fails. An **Automaton** is one unit of work. The Automation is the chain; the Automaton is a link in it. The Automation tab sits to the left of the Automata tab, and a chain can include forks (members that run side by side), joins (where the branches meet again) and other Automations nested as a single member. Chains, a `max_parallel` limit, decision waits and stage gating shape how much of the plan runs side by side; see *Max parallel*, *Decision wait* and *Stage gate* in the glossary below and [howto/automation-chains.md](howto/automation-chains.md).

### Automation list

One card per Automation, ordered by status and then name. The flat list has the same **List / Tiles / Compact** toggle and **Group by** select as the Automata list (see *List modes and group-by*). Each card shows the status, progress as completed members out of work members, the member (or members) running now, how long an active Automation has been idle, the reason a blocked one is blocked, and the budget ceiling with its window (marked estimated when the ceiling is a share of an allocation). The round **New Automation** button at the bottom right — or **＋ Create Automation** on the empty state — opens the create and edit form; both open the same form: members, edges, fork and join, nested Automations, default models and a budget policy. The Automation sub-tab carries a second row of five sub-tabs: **Inbox** (everything waiting on you: needs-attention, questions, held questions, security failure, supervisor watch, with a count badge), **Automations** (the list, status badges, search and filter), **Resources** (the Usage calculator card — a verdict sentence and three graphs: weekly allowance, pace line, burn per day — then capacity, lane fill with each lane's node, ship status), **Progress and cost** (the Progress card — the kind table, work left, completion range — the Progress, Cost and Forecast metric cards, and the four titled cost chart cards: cost over time, tool usage, retries and failure categories, combined report), and **Housekeeping** (an action list: the Unlanded work card with Keep, Archive branch and Delete worktree buttons, and the uncommitted work card with a Commit button). The former **Setup and policy** sub-tab was removed: its status cards are on the Events tab and its configuration is in Settings under Automata. Inbox is the one open by default; the Automata and Templates sub-tabs show the Progress card not.

### Automation detail and pipeline strip

Opening a card draws the chain as a pipeline strip: a status dot on every member, an active ring on members in flight, "if blocked" and "if failed" outcomes as sub-rows, fork branches as side-by-side lanes joined by a join with its rule, nested Automations that expand one level at a time, an **inserted** marker on members added later, and dashed connectors for edges a finished member did not take. Above the strip are Pause, Resume and Cancel (cancel always asks whether the running member should be cancelled too); each member card carries Skip, Insert before / after, Remove and Edit edges, and held inserts wait under Pending inserts for approval. If another operator changed the Automation first, the page asks you to reload instead of overwriting their change.

### Shipped

An Automaton or Automation can carry **shipped state**: the record of the work having landed — the release version it landed in, when it merged, and the main-branch merge commit that proves it. Shipped state is separate from the lifecycle status: recording it never changes the status, so a PRD you cancelled *after* its work merged is both cancelled (you stopped it) and shipped (the work landed); a PRD cancelled before any merge is only cancelled. That is the shipped-vs-cancelled separation: two different questions, and both answers can be true at once.

A record with shipped state **displays as Completed** in views that report an effective status, and counts in the **completed** bucket of the status counts, together with the records whose stored status is completed; it keeps the stored status it has in the store. A shipped-but-cancelled PRD shows as Completed, never as cancelled and never as a separate "shipped" bucket; a cancelled PRD whose work never landed still shows as Cancelled. The [Completed migration](#completed-migration) is what changes the stored status.

A shipped record is also what finishes its Automaton across a restart: an Automaton whose record carries a merge time (`merged_at`) and a merge commit (`merge_commit`) and whose branch is contained in main is finished as **Completed** on daemon restart and at run start — it is not re-run and its rules-review judge is not started again, so a deploy restart no longer repeats judging on an empty diff. An Automaton whose `merge_commit` is not in main still gets its judge. The completed record gets one `merged_branch_finish` decision-log line. See the note in [operations.md](operations.md#merged-work-stays-finished-across-a-daemon-restart).

Recording the state names where it came from: the **merge step** (the work landed as part of the merge), or a **backfill** (a deliberately run pass over the base repository's main branch finds the `automaton/<id>` merge commits, works out the release each one landed in, and fills in the missing records — safe to re-run, it fills only what is missing and skips what is already there). Run it with `?dry_run` first to see what would be recorded without writing anything.

The five settings under **Settings → Automate → Shipped** (config block `autonomous.shipped`) — the shipped-state master switch, the chip display, the default list view, the (now inert) separate-count switch, and the backfill-at-start switch — decide what the read surfaces report. Every read surface — the web lists and chip, the Shipped dashboard card payload (server-served), the REST API, the MCP tools, the CLI and the comm channel — reports the same state from the same stored fields, with shipped work shown as Completed. The companion apps still show the old Shipped label until they follow (the companion-app parity item).

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

### Completed

**Completed** is the status an Automaton or Automation shows when its work finished. It covers work that completed normally and work that was cancelled *after* its branch merged: once a record carries a merge commit it reads Completed in every count, list and chip, never Cancelled. **Cancelled** means abandoned work only. The daemon's own `mark_completed` post-ship step and the merge helper's `completed_mark` step mark a merged Automaton Completed right after the ship, and the [Completed migration](#completed-migration) fixes older records. See [howto/automata-shipped-status.md](howto/automata-shipped-status.md).

### Completed migration

The **completed migration** moves Automata (and Automations) that ended `cancelled` but whose work is already merged on the default branch to **Completed**, so finished work stops showing as cancelled. Evidence is a shipped record or the read-only merge proof of the `automaton/<id>` branch; records with neither stay cancelled. The completion time is the merge time (shipped record) or the merge commit's commit time, never the run time, and each change is logged as a `completed_migration` decision. It never touches a session, refuses to run while a final gate is running, and is safe to repeat. Run it with a dry run first: CLI `datawatch autonomous completed-migrate`, REST `POST /api/autonomous/completed/migrate`, MCP `autonomous_completed_migrate`, comm `autonomous completed-migrate`. Unlike [Shipped](#shipped), which never changes a stored status (it only changes how a merged record is displayed: Completed), this does. See [howto/automata-completed-migration.md](howto/automata-completed-migration.md).

## Dashboard

Mission control for your entire fleet — a live, full-screen view of every running session, active Automata, and system health indicators. Requires `autonomous.enabled: true` in `datawatch.yaml`; the nav button is hidden otherwise.

The layout is fully customisable: drag cards to reorder, resize with the width/height handle. Layout persists server-side at `GET/PUT /api/dashboard/layout` so it survives browser refreshes and re-logins.

### Operator console

Five cards that open the board — **Questions waiting**, **Held & blocked**, **Lanes**, **Usage**, **CI & release** — each pointing at the PWA anchor where that part of the daemon is handled. They are the PWA face of the one-glance summary served by `GET /api/dashboard/console` (one count, one summary line and one link per section: `questions_waiting`, `held_blocked`, `lanes`, `usage`, `ci_release`); the same body is on MCP (`dashboard_console_get`), the CLI (`datawatch dashboard console`) and the chat verb `dashboard console`. The seven switches are `dashboard.console.enabled` plus the five `dashboard.console.show_*` keys and `dashboard.hide_inactive_cards`, each a boolean that reads **true** while absent. Disabling the console drops the console cards from the board.

<a id="session-constellation"></a>

### Network card (work map)

The **work map** is the graph of every project, Automation, Automaton, task and live session with one edge per parent-to-child link. The **Network** card draws it from `GET /api/dashboard/work-map`: one node per project, Automation, Automaton, task and live session, with an edge for every parent-to-child link. Click a project to expand it and a session to open it. A button in the footer steps to the next running session. The card replaces the older session constellation; it is hidden from the grid while the map has no nodes.

<a id="ekg-waveform"></a>

### Lane pulse

The **Lane pulse** card replaces the older EKG waveform. It draws one trace per capacity lane and node over a time window (default 6 hours, up to 7 days), in three states: **busy** (a capacity lease held), **waiting** (a request queued for that lane and node) and **idle** (neither). Waiting often overlaps busy, because a request queues while the lane is full. Data: `GET /api/dashboard/lane-pulse?window=<duration>`. The card is hidden until a lane held or waited in the window, or the burn panel can read a level.

### Burn panel

The panel under the lane pulse traces. It shows the weekly usage level in **points** of the allowance, against the **pace line**, and says *ahead of pace* or *on pace*. It never shows dollars. When the usage calculator cannot answer, the panel shows the reason instead of a level. See [usage-calculator.md](howto/usage-calculator.md).

**Lane utilization**, **Throughput forecast** and **Verifier health** are opt-in series cards (Edit → Add Card). Lane utilization shows, for each lane and clock hour, the share of that hour in which a lane held a lease. Throughput forecast shows each saved progress day with the completion range in days and the completions per active day over the 24 hours before that snapshot. Verifier health shows verifier waits, runs and the pass rate per compute node; the pass rate is the passing verifier results over all results logged for that node in the window, and a node with no logged result shows a dash rather than 0%. Each card is hidden until it has data.

### Expand panel

Three-column overlay opened by the `⊞` button on a session card or an Automata card:
- **Left sidebar** — live task tree (reuses the telemetry task tree renderer).
- **Main area** — session status board: hook health ring, current focus, test counts, git state.
- **Right rail** — guardrail verdicts from the session's telemetry.

### Feature cards and their data

Every card has a one-line purpose under its title and a **?** link to its own section in [howto/dashboard.md](howto/dashboard.md#card-data-sources) (what it is for, where its data comes from, what *off* means).


Each feature card reads one live source. When it has nothing to draw it shows a one-line reason and a link here, and while `dashboard.hide_inactive_cards` is true (the default) it stays off the grid.

- **Live Events** — the daemon's own event stream (`GET /api/autonomous/event-log`), the same feed as the Events tab. Not hook events.
- **Timeline** — the 6-hour Gantt, drawn from each Automaton task's start and end timestamps (bounded to 960 px wide).
- **30-Day Activity** — the 30-day heatmap, drawn from the daily progress trend, the shipped counts and the release tags. Each day's tooltip gives the Automata completed to date, the shipped count and any release.
- **CI & release** — the operator console's `ci_release` section (see *Operator console*): the running and latest release, the ship policy, the repo CI watch and up to five failing or hung runs.
- **Smoke Run** — release smoke and end-to-end run progress (`GET /api/smoke/progress`). A release smoke run against an external daemon (`DW_BASE`) posts its progress to that daemon; a default run's throwaway test daemon is not posted.
- **Cost report** — see *Cost Report* below.
- **Search Usage** — see *Web Search (multi-provider registry)* below.
- **Work map** — a read-only graph from `GET /api/dashboard/work-map`: one node per project, Automation, Automaton, task and live session, with an edge for every parent-to-child link, plus a **Running now** list of the live sessions in an active state. With no trace store the card's header reads "placement only (no trace store)". Tasks are capped per Automaton (the overflow is `tasks_omitted`).
- **Network** — the same work map payload, drawn as the Network card's graph. See *Network card (work map)* above.
- **Lane pulse** — the lane and node traces and the burn panel from `GET /api/dashboard/lane-pulse`. See *Lane pulse* and *Burn panel* above.
- **Release train** — tags, CI runs and deploys in the window, in order (`GET /api/dashboard/series/release-train`). Hidden until the window holds a tag, a CI run or deploy state.
- **Review backlog** — plans and held questions waiting for a decision, oldest first (`GET /api/dashboard/series/review-backlog`). Hidden while nothing waits.
- **Defects per day** — defects found and fixed per UTC day (`GET /api/dashboard/series/defects-per-day`). Hidden until a defect was found or fixed in the window.

**Dashboard data on every surface** — the work map, the lane pulse and the six series (lane utilization, throughput forecast, verifier health, release train, review backlog, defects per day) are readable beyond the web page, each returning the REST body unchanged: MCP `dashboard_work_map_get`, `dashboard_lane_pulse_get` and `dashboard_series_get` (argument `series`, plus `window`, `days` or `limit`), CLI `datawatch dashboard work-map`, `lane-pulse` and `series <name>`, and the chat verbs `dashboard work-map`, `dashboard lane-pulse [window]` and `dashboard series <name> [value]`. No setting controls them. See [howto/dashboard.md](howto/dashboard.md).

**Dashboard on a phone** — at 600px or less the stat bar wraps onto more lines so **+ Card** and **Edit** stay visible, and the Dashboard scrolls as one page (the card grid grows to fit instead of clipping in a nested scroll box). Layout only; no setting, API or MCP change. See [howto/dashboard.md](howto/dashboard.md#on-a-phone).

**Additional card types** available via Edit → Add Card: event feed, sessions sparklines, 6h Gantt (the last six hours of session activity from hook events — not the Automata interval chart, see *Timeline chart*), 30-day heatmap, guardrail profiles view, lane pulse (the rebuilt multi-session EKG), smoke run progress, Network (work map), work map. The *Automaton Timeline* card (the Automata interval chart, see *Timeline chart*) is not an add-on: it is a full-width system card in the default board layout, shown even when the chart is empty (a one-line empty message), and not gated by `dashboard.hide_inactive_cards`.

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

A peer that last pushed more than a minute ago shows an inline "Stale (age)" note on its row, and a peer that has never pushed shows "Never pushed". The daemon's own row is skipped. The Settings nav button no longer shows a stale-peer badge.

**Silent-peer alert:** the daemon raises one system alert ("Observer peer silent: <name>") when a registered peer has not pushed for longer than `observer.peers.silent_alert_minutes` (default 5; 0 = off), and one recovery alert when it pushes again. The threshold is settable from PWA Settings, the CLI, MCP and the comm channel.

**Peer health alerts (v8.9.25):** a background goroutine polls each registered peer's push timestamps every 30 s and fires a system alert on every state transition — `online → stale`, `stale → online`, or `peer → unreachable`. Alerts appear in the Alerts tab with `source: system` and the peer name so you can correlate observer gaps with network or daemon restarts on the remote host. Alert thresholds match the health-dot thresholds: stale > 60 s, unreachable on first push-timeout after a previously-healthy period.

### Process envelopes

Per-process aggregation by attribution kind: `session:`, `backend:`, `container:`, `system`. Snapshot of CPU / RSS / threads / FDs / network / GPU per envelope. Refreshes every 5 s. Click an envelope to drill into its constituent processes.

### eBPF per-process net

Kernel-traced TCP socket activity per process (when eBPF is available — kernel ≥ 5.8 + cap_bpf + cap_sys_resource). Off → see Settings → About → eBPF status row. Each row is a (process, remote endpoint, byte counts, age) tuple. The capabilities live on the binary file, so every release removes them until `datawatch setup ebpf` is run again; a passwordless sudoers rule (`datawatch setup ebpf --sudoers`) lets updates restore them. See [eBPF per-process telemetry](howto/ebpf-process-telemetry.md).

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

A redacted record of what agents did: tool calls and results, model requests, state changes, capability and permission decisions, loop warnings and errors. Read it as a timeline for one session or one Automaton, or as a roll-up by session, task, story, Automaton, agent or backend. `tracing.mode` is `off`, `summary` (default, short first-line summaries) or `full` (longer text up to `tracing.arg_cap_bytes`); `tracing.retention_days` (default 0 = keep while the session exists; above 0 the daemon prunes traces older than that many days at the next daily prune; a negative value or one above 3650 is refused) sets age-based pruning, and `tracing.backends` turns a backend off. A trace lives with its session: deleting a session deletes its trace, while ended, killed or archived sessions keep theirs. Each backend has a coverage level: **full**, **partial** or **events-only**, so missing detail is never read as no activity. Secrets and private paths are removed before an event is stored. Shown as the Trace sub-tab under a session's Status tab and the Trace tab of an Automaton. OpenTelemetry export is a follow-up. See [howto/agent-tracing](howto/agent-tracing.md).

### Push-channel TLS (verified by default)

Observer **pushes** — a worker's Shape A push into its parent, and the primary → root federation push (`observer.federation`) — verify the counterpart's TLS certificate. Until this change the worker push ran with certificate checking off unconditionally, so a peer that could push to a self-signed / private-CA `https://` parent before may need the parent's pin or the explicit opt-out below.

- **Worker push** (`https://` parent): default is verification against the **system trust store** (one startup notice). `DATAWATCH_PARENT_CERT_FINGERPRINT` pins the parent's leaf — the same variable the parent injects for the bootstrap channel; a malformed pin makes the push refuse to start (fail closed). The **pin wins over the opt-out**. `DATAWATCH_OBSERVER_PUSH_INSECURE=1|true|yes|on` is the explicit opt-out, honored only when no pin is set, with a startup warning that names the parent host and no tokens. An `http://` parent has no TLS to verify.
- **Federation push** (primary → root): verified by default; `observer.federation.insecure: true` still skips verification but the daemon prints a startup `WARNING: TLS certificate verification is SKIPPED for the federation root <url>`. No pin knob on this push — trust the root's certificate instead.
- **Unchanged:** the bootstrap channel's existing unverified fallback for a TLS-less parent, and `datawatch-stats`, the separate reporter binary with its own `--insecure-tls` flag.

Walkthrough (including deriving the parent's leaf fingerprint): [howto/federated-observer → Push-channel TLS (verified by default)](howto/federated-observer.md#push-channel-tls-verified-by-default). Operations entry: [operations.md → Observer push TLS (verify by default)](operations.md#observer-push-tls-verify-by-default). The cross-link reference for all three peer links — pins, opt-outs and the upgrade note — is [howto/peer-tls-verification](howto/peer-tls-verification.md).

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
- **Secrets store** — credentials, tokens, environment values. Native AES-256-GCM at `~/.datawatch/secrets.db` plus optional KeePass, 1Password, and HashiCorp Vault / OpenBao (KV v2, static-token auth) backends. `${secret:name}` references in YAML/plugins/spawn-time env injection. Per-secret tags + scope. Audit-logged on every read. Vault status row (Settings → Secrets Store) shows reachability, last request ID, last error and last success. The former red Settings nav badge for an unreachable Vault is removed; a Vault unreachable past `secrets.vault.unreachable_alert_after_seconds` (default 120, `0` = off) raises a system alert instead. See [`howto/secrets-manager.md`](howto/secrets-manager.md#vault-status).

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

**Add / Edit form** — fields: **Name** (short slug used in URLs and in the header server picker, e.g. `nas`), **URL** (base URL including port), **Bearer token** (stored server-side, masked in UI), **Enabled** toggle, **Skip TLS certificate verification** checkbox (per-peer opt-out; shows "TLS certificate verification is disabled for this server — use only on a trusted network."), and the **Certificate fingerprint (pin)** field (`tls_cert_fingerprint`, 64-hex leaf pin; mutually exclusive with the checkbox). For `https://` remotes the daemon verifies the peer's leaf certificate against the system trust store by default; a pin (recommended for self-signed remotes) wins over the skip, and a skipped, unpinned peer is announced with a warn-level **Peer certificate not verified** system alert (at most once per peer per daemon run). See [`howto/multi-servers.md`](howto/multi-servers.md#peer-tls-verified-by-default) and the cross-link reference [`howto/peer-tls-verification.md`](howto/peer-tls-verification.md).

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

<a id="server-selector"></a>

**Server selector** (the header server button) — the fixed app header carries one shared server icon button on **every page** (`id="serverPickerBtn"`: a server icon, a status dot and, on screens wider than 600 px, a short name; on a phone only the icon and dot show). It replaced the old server dropdown and the per-page server chip rows, and it is the only server selector in the web app: the Sessions toolbar has none. Pressing it, or a three-finger swipe up (at least 64 px) anywhere on a touch screen, opens the **Servers** window with one button row per choice. The window is a dialog: focus moves onto the active row, **Tab** stays inside it, **Escape**, **Close** or a click outside closes it and focus returns to the button. A peer whose reachability check failed keeps its row, marked **(unreachable)**, and stays selectable. See [howto/multi-servers.md](howto/multi-servers.md#switch-between-servers-header-server-button).
- **All servers** — aggregated fetch from every server; returns items tagged with their `server` origin. (The **Observer** and **Settings** views are the two that have no aggregated form: while **All servers** is the active entry they are deliberately not re-rendered, so they keep showing the single server whose data they last rendered.)
- **This server** (labelled with this daemon's real hostname once it resolves, never the word "Local") — only this daemon's data (the boot default only while this browser has no saved choice).
- **\<name\>** — proxy mode; REST and WebSocket calls route through `/api/proxy/{name}/...` on the local daemon.
- **The active entry is remembered per browser** (this browser's local storage, not on the daemon): when you reload the page or come back later the same entry is already selected, and the boot round of data fetches runs against it, so restoring the choice costs no second refresh. A saved peer name is validated against the current server list at boot: if that peer has since been removed or disabled, or cannot be validated within a short bound (this daemon's `/api/servers` down or slow), the PWA starts on **All servers** and keeps that, so the stale choice does not survive the next reload. With local storage blocked the choice simply lives in memory for the page's lifetime, exactly as it did before the choice was saved. No new endpoint, setting, or daemon surface — it is browser-local state.
- **The header button follows the entry across page navigation** — one shared control in the fixed header, so the active entry (and its saved value) is the same on every page.
- **Selecting an entry switches the page you're on, in place** (new — confirm it live before relying on it; see the [operations.md](operations.md) "Still to review (operator)" checklist) — picking a federated host (or the **This server** entry, or **All servers**) re-renders the tab you're currently looking at against that server: the **Showing: \<name\>** line above the view updates to the active host and the list becomes that host's data, with no need to leave the view and re-open it. Re-selecting the already-active entry is a no-op. A genuine switch clears that view's previous server's data first (the old session list does not survive it), and sub-pages (session detail, the New session form) are left untouched by a switch; while a remote is active there are also one-tap "back to local" controls — a clickable **🌐 \<name\>** indicator on the Sessions toolbar (the `.server-indicator` chip in `renderSessionsView`, pinned in `app-select-server-controls.test.js`) and a **Back to \<hostname\>** button on the server's row in Settings → Comms → Remote Servers. Found 2026-11-08 and recorded under [Unreleased] → Fixed in [CHANGELOG.md](../CHANGELOG.md); that entry records that no live verification had been run when it was recorded, but the fix is implemented in this tree's `app.js` (`selectServer` re-renders the tab you're on, Automata sub-tabs included) with PWA tests — so on this source tree the old behaviour of a tab keeping the previous server's data until you navigated away and back is gone. On the **Dashboard** the switch is per panel: each panel re-fetches for the active host, and only the **Automata** and **Cost** panels aggregate under **All servers** (`/api/autonomous/prds/aggregated`, `/api/cost/aggregated`); the other panels have no aggregated variant. The operator checklist is in [operations.md](operations.md#proxy-mode-multi-machine-management).

**Aggregated endpoints** used by **All servers**:
- `GET /api/sessions/aggregated` — sessions from all servers
- `GET /api/alerts/aggregated` — alerts from all servers
- `GET /api/autonomous/prds/aggregated` — Automata from all servers

**Relationship to Federated Observer:** multi-server (active query, per-page switching) and Federated Observer (passive push stats) are complementary. You can register a server here for UI switching AND configure it as a federated peer for process/GPU/network telemetry — they use different auth tokens and different push/pull directions.

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

#### Email notification backend (SMTP)

The `email` backend is outbound-only: it sends session notifications over SMTP using the `email.*` settings. When a send names no recipient it uses the configured `email.to` address. A recipient or sender containing a newline is rejected before any network connection is made. Its unit tests, including the empty-recipient fallback, run against a local in-process fake SMTP server, so they need no network and never wait on a connection timeout. See [`messaging-backends.md`](messaging-backends.md) § Email. For inbound email commands see the imap-mcp channel below.

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
| **Dashboard** | "Search Usage" card: total/today/this week/this month + cache hits per provider and overall, a daily usage graph, recent search history, and searches **by caller** (session, Automaton task, MCP client, REST, CLI). |
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

Per-backend per-model input + output token rates the daemon multiplies session token counts by to compute `EstCostUSD`. Adjust if a backend's billing changed or you negotiated a custom rate. Values default to public list pricing on first run. An LLM entry's models list carries its own per-model rates per model (`in_per_k`, `out_per_k`, plus `cache_read_per_k` and `cache_write_per_k`): they win for that model, field by field, over the per-backend rate, which is the fallback (cache prices have no per-backend value and resolve to 0 when unset). Per-model rates are what the cost report's by-model rows and the burn guard's per-entry projections price with, and what the guard's `route_cheaper_model` rung compares when it resolves an entry's cheapest other model (Haiku for a Claude entry). Set them per model on the LLM entry: REST `POST`/`PUT /api/llms`, MCP `llm_add` / `llm_update`, `datawatch llm add`/`update`, comm `llm <name> models=...`, or the LLM edit form in the web app.

#### Cost Report

A **token-first report of model token spend over a window and a grouping**: for each group it counts the sessions and shows **tokens in / tokens out first**, and a **USD estimate where and only where configured [cost rates](#cost-rates-usd--1k-tokens) cover every backend in the group** — where a rate is missing, the row shows tokens and no dollar figure (`rate_configured` is `false`); it never invents a number. Dollar figures are estimates at configured rates, not invoices. A row says how to read its money with a label taken from each backend's [LLM billing mode](#llm-billing-mode-billing_mode) (then the provider's [billing profile](#billing-profile-usagebilling) where the entry has no value): a bundled plan (`allowance`) reads **“bundled plan — API-equivalent, not billed”**, and its money figure is the API-equivalent: an equivalence, not a charge, plus the usage points it represents and its share of the monthly price; an `api` entry reads **“api_metered — billed per use”**; a `local` entry reads **“free local inference — not billed, token usage only”** with no money figure; a backend with neither an entry value nor a profile reads back as the assumed default and keeps an ordinary estimate. **Dollars and points are never added together:** a row that spans more than one billing unit reads `mixed` (**“mixed billing modes — see the per-unit totals”**), carries no single `usd_estimate`, and shows one total per unit in `unit_totals` (`dollars` for `api`, `points` and `percent` for `allowance`, tokens and time only for `local`); the report body repeats them across all rows as `totals_by_unit`, and the CLI prints them as a `totals by unit` block. The groupings are `automaton` (default), `automation`, `backend`, `release` (with the `units` — shipped units — per row; work not yet shipped lands under `unreleased`) and `week` (weeks start at `cost_report.week_start`, Monday or Sunday); the window takes `since`/`until` (RFC3339 or `YYYY-MM-DD`), an absent `until` being now and an absent `since` the daemon's `cost_report.default_window_days` (7). The figures rest on a 31-day ledger of recorded sessions, which the report body states as `retention_days`; spend older than the ledger is visible only through lifetime totals (`lifetime_fallback`). `cost_report.include_peer_spend` (default `false`) admits spend federation peers reported; `cost_report.show_usd_estimates` (default `true`) drops the dollar figures everywhere when off. An **Automaton's row** counts every session it launches (worker, verification, rules-review, decompose and guardrail sessions, retries included) from a session-keyed spend ledger that outlives deleted sessions, and carries tokens, cache tokens, time (`span_seconds`, `active_seconds`), `sessions` and `tool_calls`; the same row is the `cost` field of an Automaton read over REST and MCP. One engine serves every surface — CLI, comm channel, MCP, REST, PWA — so the same window and grouping give the same rows on all of them. Independent of the grouping it also carries a **by-model** section (`model_rows`): one row per model that ran (an unnamed model reads as the backend), each with its input / output and cache-read / cache-creation tokens and an **API-equivalent figure at the model's own rate** — the per-model cost entry when one is configured, otherwise the backend's rate. The label says it is “an equivalence, not a bill”: per-model rows are the plan's API-equivalent, even for an `api` entry, and a row without a configured rate shows tokens only.

An Automaton's row counts every session it launched (worker, verification, rules-review, decompose and guardrail sessions, retries included), taken from the spend ledger, which also records each session's active time and tool calls and keeps the attribution after the session is deleted. Sessions from before this change have no attribution and read as `unattributed`. The checks status reports an **attribution check** counting Automaton-launched sessions with no Automaton id, so a launcher that misses the stamp is visible rather than silently under-counted. It also counts **sessions with no Automaton and no role** (no Automaton id, no task, and a blank or `other` role), the spend whose owner is truly unknown and that lands under `unattributed`; spend with no Automaton but a `supervisor` or other known role lands under **Supervisor and other** instead, and is not counted as unknown. Each ledger entry also records the session's role (`worker`, `decompose`, `rules_review`, `verification`, `guardrail`, `supervisor`, or `other` when none was given). A missing Automaton row in a window means no activity in it (the default window is 7 days), not zero spend; a window reaching before the 31-day ledger reports spend before that date as unknown (`unknown_spend_note`, `spend_unknown`), never zero and never backfilled. Cache read and creation tokens are shown beside tokens in and out and are not priced separately. The Automaton detail Overview in the PWA and the merge report show the same cost.

The **Cost report** dashboard card (help link target: `#cost-report`) renders that report next to the live cost card, with three controls:

- **Group by** — `automaton`, `automation`, `backend` or `week`; the `default` choice leaves the grouping to the daemon's `cost_report.default_group_by` setting.
- **Window (days)** — how far back the report reaches; blank leaves the window to the daemon's `cost_report.default_window_days` setting.
- **Refresh** — re-requests `GET /api/cost/report` with the current controls.

Rendering rules: token counts (in/out) are shown first, for every group; a USD figure appears **only** where the report carries one (a group counts toward a USD estimate only while it has a rate configured — cost rates above — under the `cost_report.show_usd_estimates` setting, and the card also shows the daemon's retention line, e.g. *Figures rest on the last 31 days of recorded sessions*, plus a note when some figures fall back to lifetime rates). The card never invents a USD number for a group the daemon did not price.

What the report counts: input and output tokens, **prompt-cache read and creation tokens** (kept apart from input, because they price differently: a cache read at a tenth of the input rate, a cache creation at 1.25 times it), and the tokens of **local-model sessions** (pi and ollama; counted, never priced). A pi session's tokens come from its own session log. The body carries a `totals` block with all of them summed. Every dollar figure on the card is labelled **API-equivalent**: it is what the usage would cost at API list prices, not a charge. Where usage readings exist (`GET /api/usage/readings`), the body also carries a `reconciliation` block and the card shows it under the table: the points the readings show against the pace line, the API-equivalent dollars priced for the same window, the implied dollars per point, and the burn per day. With no readings there is no block. The card is not shown while the report is unread or has no rows; its one-line reason links back here.

**Settings** (`cost_report` group in PWA settings): `default_group_by`, `default_window_days`, `show_usd_estimates`, `week_start` (Monday or Sunday for the `week` grouping) and `include_peer_spend`.

#### Usage Charts and the Combined Usage Report

Three read-only views of recorded work over time, and the one report that joins them. **Cost over time** (`GET /api/cost/series`) buckets the spend ledger by hour or day and draws a line per Automaton, project or model: tokens on every point, with money beside them in its own [billing unit](#llm-billing-mode-billing_mode) (dollars, points or none) and never added across units. **Tool usage** (`GET /api/cost/tool-usage`) is the number of tool calls per group over time, with the total time spent inside tools (`tool_seconds`; parallel calls overlap); only counts and time are recorded, never tool names. **Retries and failure categories** (`GET /api/cost/failures`) counts failed, stalled and cancelled task attempts per week and per category (`infra`, `code_or_test`, `policy`, `none`, `content`, …) for each project, Automation or Automaton; a task's **retries** are its automatic retry count, counted once per task, and the count restarts when the task is reset, so it is a floor. A category is derived when an attempt is recorded — a verifier failure is `code_or_test`, a refused merge or a failed commit is `policy`, and a refused launch or placement, a timeout, or an executor error with no output is `infra`; a category the verifier or executor named is kept as it is — and stored attempts with a blank category are filled in by one backfill pass at each daemon start, idempotent, so a rerun changes nothing. An attempt with no end reason, no summary and no verifier issue stays blank, reads as `uncategorized` (that is now all an `uncategorized` attempt means), and is counted in the payload's `uncategorized_attempts` ([howto/failed-attempt-cost](howto/failed-attempt-cost.md#failure-categories)). The **combined usage report** is the three joined: `datawatch cost charts`, `cost charts` on the comm channel and the MCP `cost_charts` tool. The routes take `group_by` and `window`, and the series and tool-usage routes also `bucket` (`hour` or `day`); there is no setting of its own, the default window being `cost_report.default_window_days`. The same report is `datawatch cost charts [--group-by <g>] [--window <d>] [--json]`, the comm verb `cost charts [group_by=<g>] [window=<d>]` and the read-only MCP tool `cost_charts`. The Automata page draws them as four titled chart cards in the **Progress and cost** tab: a shared row of controls sets the grouping (Project, Automation or Automaton), the window (7 days to 8 weeks) and tokens or money, and each card repeats them plus a **Series** choice (top 5, 10, 20 or All, 10 by default) that draws the chosen groups by total and folds the rest into one **Other** line or row so nothing is dropped; the cost and tool-usage legends show or hide a series in that card without a new request, a hidden one keeping its name struck through, and every series is named by its label — an Automaton's title, never its id. A card with no data is not drawn and its reason appears at the top of the **Events** tab. Operator guide: [howto/cost-report.md](howto/cost-report.md#the-chart-cards-on-the-automata-page). Reference: [api/cost.md](api/cost.md#cost-series-get-apicostseries).

#### LLM Billing Mode (`billing_mode`)

A field on each [LLM registry](#llm-registry) entry saying how that entry is paid for: `api` (metered, billed per token), `allowance` (a bundled plan allowance that resets on a schedule) or `local` (local inference, no per-call charge). Empty resolves from the entry's own value, then the kind's [billing profile](#billing-profile-usagebilling) (`bundled_plan` → `allowance`, `api_metered` → `api`, `free_local` → `local`), then the kind default. On upgrade every entry without a value is written once with its resolved value, so behavior is unchanged. It is set on every surface (YAML `billing_mode:` on the entry, the **Billing mode** field of the Settings → Compute → LLM edit form, REST `POST`/`PUT /api/llms` and `PUT /api/config` with `llms.<name>.billing_mode`, MCP `llm_add` / `llm_update`, CLI `--billing-mode`, comm `billing_mode=` or `configure llm.<name>.billing_mode=<mode>`); an unknown value is refused everywhere with one message naming the allowed values, and the mobile apps are tracked in the parity issue. On a cost report row that spans more than one mode the money is shown per unit and never added: dollars for `api`, points for `allowance`, tokens and time only for `local`. The resolved value is read by the cost report (which keeps dollars, points and local usage apart), the per-LLM dollar budget (a `local` entry has no budget), the burn guard (acts on an `allowance` entry, and on an `api` or `local` entry only with a dollar budget) and the usage advice (a unit label per backend: dollars, points or none). It takes precedence over the per-provider `usage.billing.<provider>.mode` (`bundled_plan` / `api_metered` / `free_local`), which is only the fallback for an entry with no value; the profile still supplies the plan, monthly price and reset the figures use. Operator guide: [howto/llm-registry.md](howto/llm-registry.md#what-the-billing-mode-changes).

#### Billing Profile (`usage.billing`)

The per-provider billing profile a cost-report row is read against, keyed by a provider name from the LLM registry (`usage.billing.<provider>.*`). The profile's `mode` is the fallback for an LLM entry with no [`billing_mode`](#llm-billing-mode-billing_mode) of its own — an entry's value wins — while the rest of the profile (monthly price, plan, reset) is what the report's points and the usage calculator's point value are computed from. Fields: `mode` (`bundled_plan` — a flat subscription whose spend is not billed per request, `api_metered` — billed per request, `free_local` — no charge), `plan` (a free-text label), `monthly_price` (the plan's monthly price, 0 for free plans), `currency` (a 3-letter code), `reset_weekday` + `reset_time` (the allowance's reset, when the provider says one), and `allowance_unit` (`percent`, `requests` or `tokens`). Every field is optional and independent; a blank string or `0` clears one back to unset. A `bundled_plan` profile is what makes a cost-report row carry the **“bundled plan — API-equivalent, not billed”** label and its point figures (the API-equivalent money amount, the usage points it represents, and its share of the monthly price); the usage calculator reads its point value (per point, per week) from the same entry. Providers without a profile read back as the assumed default — an ordinary estimate, no invented figure. Operator-facing setup and reading guidance: [howto/usage-calculator.md](howto/usage-calculator.md#the-billing-profile-usagebilling) and [howto/cost-report.md](howto/cost-report.md#the-billing-profiles). Reference: [api/cost.md](api/cost.md#billing-profiles-usagebilling).

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

**Supervisor watch probe.** The supervisor watch reads which models a node has resident with `GET /api/ps` on the node's address. That probe verifies the node's certificate by the settings above. When the check fails, the probe's result for that node is unknown and the watch raises no finding for it (a check that cannot tell reports nothing); the error names the fix: `node <name> certificate not trusted: ...: set tls_ca_file or tls_cert_fingerprint on the node, or tls_insecure_skip_verify to turn verification off for it`. A pinned node whose leaf has changed fails the same way, with a `fingerprint mismatch` message. The node usage probes verify by default too, with the same opt-out. Other node endpoints are outside this verification: the connectivity probe when a node is added or updated, and the node monitoring and models handlers, still do not check certificates. Model pulls are separate from all of these: they always verify the node's certificate, trusting `tls_ca_file` or `tls_cert_fingerprint` when set, and there is no setting that turns pull verification off ([`verify_tls`](#verify_tls) is kept for configuration compatibility only).

**Audit and visibility.** A node with `tls_insecure_skip_verify` set is marked in four places:

- the node record carries `tls_insecure_skip_verify: true` on every read;
- the PWA node row shows a red **TLS verification OFF** badge (a pinned fingerprint or a CA file show a quiet *pinned* or *CA file* badge instead, and a default node shows none);
- the audit log gets an entry with the action `compute_node_tls_verification_off` when a node is saved with the setting (the entry's `source` is `compute_node_add` or `compute_node_update`), when the daemon starts with a configured node that has it (`source` is `config_seed`), and the first time the supervisor watch probes such a node after the daemon starts or after the node's TLS settings change (`source` is `supervisor_watch_probe`, once per change, not once per probe). Query them with `datawatch audit --action compute_node_tls_verification_off`;
- a supervisor watch finding raised for that node ends with `; certificate verification is turned off for this node`.

Every `compute_node_add` and `compute_node_update` audit entry also records whether the node has the skip setting, a CA file and a pinned fingerprint, as three booleans (`tls_insecure_skip_verify`, `tls_ca_file_set`, `tls_cert_fingerprint_set`); the CA path and the fingerprint themselves are never written.

Walkthrough with commands: [`howto/compute-nodes.md`](howto/compute-nodes.md#certificates-for-self-signed-and-private-ca-nodes).

###### verify_tls

`verify_tls` is a boolean on the node record, default `false`, kept for configuration compatibility. Model pulls to that node always verify the node's `https://` certificate against the system roots, plus the node's `tls_ca_file` when set, and the leaf must match `tls_cert_fingerprint` when pinned. A pull whose certificate does not verify fails with an error that names the node and the certificate problem: `node <name>: certificate verification failed: <certificate error>; trust the certificate with tls_ca_file or tls_cert_fingerprint`. A plain `http://` node is unaffected. There is no setting that turns pull verification off; `tls_insecure_skip_verify` never applies to a pull.

The field no longer changes behavior: a pull verifies whether it is `true` or `false`, so an existing `verify_tls: false` does not keep the check off. For a node whose certificate the system does not trust, name the trust instead — set `tls_ca_file` or `tls_cert_fingerprint` on the node (a pull from such a node fails until it does, and the error names the fix).

It is separate from `tls_ca_file`, `tls_cert_fingerprint` and `tls_insecure_skip_verify` in the probes: those three govern the usage and supervisor watch probes only. For pulls the direction reverses — `tls_ca_file` and `tls_cert_fingerprint` do help a pull (a private CA or a pinned leaf the system does not otherwise trust), while `tls_insecure_skip_verify` does not.

Set it with `verify_tls` in the body of `POST` and `PUT /api/compute/nodes` (REST), `--verify-tls` on `datawatch compute node add` and `update` (CLI), `verify_tls` on the MCP `compute_node_add` and `compute_node_update` tools, the chat pair `verify_tls=true|false`, and the **Verify TLS for model pulls (always on; kept for compatibility)** checkbox in the PWA node form — still sent when the node is saved, but without effect on pulls. Update replaces the whole node, so leaving it out leaves it `false` — the pull verifies either way. Walkthrough: [`howto/compute-nodes.md`](howto/compute-nodes.md#verify-the-certificate-on-model-pulls).

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
| `docker_endpoint` | string | system default | Docker socket/endpoint URL; one that starts with `-` or holds a control character is refused when docker is run |
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
- **Observer peer** — bind this node to a registered federated observer peer for live process/GPU stats correlation. A peer that pushes stats never creates a node and never supplies an address; if a node with the peer's name exists the peer is bound to it and the node's address is never changed by a push

**Observed peers with no node:** a peer that pushes stats and has no matching node is listed here on the Compute page, in the node list REST response (`observed_peers`), the `compute node list` CLI output and comm reply, and the `compute_node_list` MCP tool. **Add as a compute node** opens the normal node form with the name filled and the address empty; nothing is created until you submit it. Federated peers and compute nodes are separate: keep a peer for monitoring and add an LLM node only when wanted.

**Auto-created nodes that never answered** (`unanswered_auto_created`): a review report of nodes created automatically by earlier versions whose address has never answered a probe. Nodes are never deleted or disabled automatically; review and delete them by hand with the delete action. A node leaves the report when its address answers a probe or health check, or when you delete or recreate it.

**Save-time probe:** the daemon runs a connectivity check on every create/update. Use `?probe=skip` to bypass for emergency saves when the node is temporarily unreachable.

**Ollama marketplace:** click "Browse marketplace" on an Ollama-kind node to open the embedded catalog (llama3.1, qwen3, gemma3, deepseek-r1, etc.) with size/VRAM requirements and one-click background pull. The button's click handler carries the node name escaped (`escJsAttr`), so a name with quotes still opens the catalog and injects nothing.

**Edit form values render escaped:** in the Compute node and LLM edit forms, every `value="…"` attribute is HTML-escaped (`escHtml`), so a name, tag, capacity or key value that carries quotes, angle brackets or `on…="` text is displayed as text and round-trips unchanged on re-save instead of becoming live markup or a live attribute (fixes the `xss-through-dom` findings, alert 850; closure confirmed on the next "Open high/critical alerts" gate run on main — see [operations.md](operations.md#open-code-scanning-alerts-fixed-on-main-closure-pending-the-gate-run)). The project section card title tag strip is a fixed-point strip (repeats until no live tag-shaped span remains) rather than a single pass (`incomplete-multi-character-sanitization`, alert 849). Regression tests: `internal/server/web/app-codeql-dom.test.js`.

**Migration banner:** shown when any node still uses a deprecated kind (`local`, `remote`, `ssh`, `docker`, `k8s`). Click to re-pick a supported kind per node. The kind migration dialog's reselect handler carries the node name escaped (`escJsAttr`), so a name with a single quote in it re-renders the dialog as data and injects nothing — the last `xss-through-dom` spot of the alert 850 batch (closure pending the gate run; see [operations.md](operations.md#open-code-scanning-alerts-fixed-on-main-closure-pending-the-gate-run)).

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
- **Automaton branch refresh** (`autonomous.branch_refresh_enabled`, `autonomous.branch_refresh_behind_limit`) — merge the base branch into an Automaton branch between tasks once it is at least the limit behind (default off; limit default 5, 0 reads as 5). A per-Automaton override beats the daemon setting; set it with `datawatch autonomous branch-refresh`, chat `autonomous branch-refresh`, MCP `autonomous_prd_set_branch_refresh`, the Branch refresh card on the Automaton page, or `POST /api/autonomous/prds/{id}/branch-refresh` (`enabled` true, false or null), and read the state on the Automaton `GET` reply. See [Branch refresh](#branch-refresh) and [howto/automaton-branch-refresh.md](howto/automaton-branch-refresh.md). Also `datawatch config set`, `PUT /api/config`, comm `configure`, and MCP `config_set`.
- **Automaton uncommitted-work policy** (`autonomous.automation.uncommitted_work_policy`) — what happens when a task finishes with its work not committed: `hold` (default; the task is held with the files and a question to commit, discard or retry), `warn` (record and continue) or `off` (no check). Select in the **Uncommitted-work check** group; the group's help link opens [Uncommitted-work check](#uncommitted-work-check). Also `datawatch config set`, `PUT /api/config`, comm `configure`, and MCP `config_set`.
- **Automaton uncommitted-work scope** (`autonomous.automation.uncommitted_work_scope`) — which files the per-task check counts: `task_files` (default; the files the task changed) or `worktree` (every uncommitted file). The check before an Automaton is reported finished always covers the whole worktree. Same group and same ways to set it.
- **Automaton protected paths** (`autonomous.automation.protected_paths`) — the paths an Automaton task may not change without a decision: files, directories (trailing slash) or globs, repository-relative. Unset or empty reads the default list (the project rules file, the supervisor rules, `CODEOWNERS` and `.github/workflows/`); an explicit list replaces the default entirely. Edit in the **Protected paths** group; the group's help link opens [Protected paths](#protected-paths). Also `datawatch config set`, `PUT /api/config`, comm `configure`, and MCP `config_set`.
- **Automaton protected-paths action** (`autonomous.automation.protected_paths_action`) — what happens when a task changes a protected path: `hold` (default; the task is held and one question offers approve, reject or change the spec), `warn` (record and continue), `block` (hold the task with no question) or `off` (no check). Same group and same ways to set it; see [Protected-paths action](#protected-paths-action).
- **Automaton rules review source** (`autonomous.automation.rules_review_source`) — where the rules review reads the project rules from: `base_commit` (default; the rules as committed at the start of the Automaton's work) or `worktree` (the rules file in the working directory). Same group and same ways to set it; see [Rules-review source](#rules-review-source).
- **Pending items** — a card on the Automation view's Housekeeping sub-tab for work that has no Automaton yet. It shows the pending item policy and auto-resume in effect, the file registry line (or that none is configured), every Automation that is held on a waiting item, and every pending item with its state, its ref and the Automations that carry it. A waiting item has an *Automaton id to attach* field and an **Attach Automaton** button; the card also has an *Add a pending item* form and, when a file registry is configured, a **Read the file registry now** button. A member that is a pending item shows the badge *Waiting for an Automaton* on the pipeline strip. The card's help link and the group of nine settings in Settings → General → Automata settings → **Pending items** open [Pending items](#pending-items). Read it with `GET /api/autonomous/pending-items`, MCP `autonomous_pending_items`, `datawatch autonomous pending-items` or the chat verb `autonomous pending-items`. See [howto/automaton-pending-items.md](howto/automaton-pending-items.md).

#### Child inheritance

`autonomous.automation.child_inherit` (`true` by default; an unset key reads `true`; `false` restores the earlier behavior) decides whether a child Automaton starts from its parent's setup. When on, a child inherits, where its creation set nothing itself: the project link and the project's repository directory (never a worktree path), the parent's Automation (it joins right after the parent through the normal member insert), backend and model as a pair, verification backend and model, tags and priority. A Sonnet or Haiku parent never gives a child an Opus or Fable model. One `child_inherit` decision on the child records what was inherited, clamped or skipped. The key is read when each child is created, so a change applies to the next child with no restart. See *Child inheritance* in the glossary below and [howto/automaton-child-inheritance.md](howto/automaton-child-inheritance.md).

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

The mode and the model belong to that one session. A start that names no mode runs in the claude-code mode that was configured when the daemon started (a start through REST first takes the claude-code LLM's current mode), never in the mode another session was started with. A restart runs in the session's own recorded mode (`permission_mode` in the session JSON) and on its own recorded model. A restarted record with no model passes no `--model`, except that pi and goose sessions get their family's configured model.

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
| `DATAWATCH_TOKEN` | (the injected bearer — in a session's private launch file or per-session registration: the session's scoped, short-lived credential, default `session-default` grant; in the instance-level `<data_dir>/.mcp.json`: the instance-scoped, expiring credential with the same default grant, valid about an hour, refreshed at each session launch and revoked when no live session remains) | Bearer auth; missing/wrong → 401 on ready + tool discovery. |
| `CLAUDE_SESSION_ID` | (session id) | Tags replies; auto-set by daemon. |

#### MCP Tools

Every datawatch capability — session management, memory, Automata, Council, evals, secrets, plugins — is available as an MCP tool. The tool catalogue is served at `GET /api/mcp/docs` (human-readable) and via the MCP `tools/list` protocol. See [`howto/mcp-tools.md`](howto/mcp-tools.md).

#### Token in daemon-written `.mcp.json` files

**No daemon token is written into a project folder.** A session's `datawatch` entry (its token and its identity) goes into that session's **private launch file**, `<data_dir>/launch/<tmux session>/session-mcp.json` (folder `0700`, file `0600`), which is removed when the session ends. It is written for sessions that are not claude-code and for claude-code sessions whose [worker session profile](#worker-session-profile) selects `datawatch`; other claude-code sessions get their bridge from the per-session registration (`claude mcp add`). Two sessions in one project folder therefore never share a file or an identity. A profiled launch picks its servers from the operator's entries in `<project_dir>/.mcp.json` plus its own private file; a daemon entry found in the project file is never used. A session that is not claude-code finds the path of its file in the `DATAWATCH_MCP_CONFIG` variable of its tmux session (new windows inherit it; in the first window read it with `tmux show-environment DATAWATCH_MCP_CONFIG`). A client that reads MCP servers from the project folder no longer finds `datawatch` there; give it that path, for example `claude --mcp-config <path>`.

**Old entries.** A `datawatch` entry that an earlier daemon wrote into a project's `.mcp.json` (it has the daemon's API URL and a token or a session id) is removed at the next launch in that folder that is not an unprofiled claude-code launch (that launch removes the `datawatch` entry by name, as before). A `datawatch` entry without a token or session id is yours and is kept. Folders where no session starts again keep the old entry; delete it by hand.

**Commits.** No automatic commit or attempt snapshot takes an `.mcp.json` that holds a daemon entry or a written-out `DATAWATCH_TOKEN`, tracked or not, with or without a `.gitignore` rule; one that a worker staged is unstaged and the working file is left alone. A `${DATAWATCH_TOKEN}` reference is not a token. A commit you or a worker make with `git` directly is not covered.

The token in the private launch file is the session's **scoped, short-lived credential**, minted when the session starts (kept `0600` under the data directory so a running session's bridge survives a daemon restart), granting the `session-default` capability group by default (override: `session.capabilities`; see [`howto/federation-cbac`](howto/federation-cbac.md)), and revoked when the session ends or is killed. It is **never** the daemon's long-lived `server.token`. The **instance-level** file `<data_dir>/.mcp.json` carries the **instance-scoped, expiring credential** — the same `session-default` grant by default, valid about an hour — refreshed at each session launch (the existing token is reused while more than half its lifetime remains; a failed mint removes the entry rather than keeping a stale credential, with no fallback to the long-lived token), and when no live session remains — the last session ends, or the daemon shuts down — that credential is revoked and the `datawatch` entry (with the file, if it was the only server) is removed; a stale entry still holding the admin token from an earlier daemon is dropped at daemon startup and the token value is never logged. Operator-managed MCP host configs — `~/.cursor/mcp.json`, entries added with `claude mcp add`, an IDE-side `mcp.json` — are yours, still authenticate with the daemon's bearer or a federation peer token, and are unaffected. **Operator review:** a helper that reads `DATAWATCH_TOKEN` out of a project's `.mcp.json` no longer finds one; move it to an operator-managed credential with the capabilities it needs.

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

### Self-config

**Self-config** is the ability of a session or a connected agent to change the daemon's own non-secret configuration at runtime — things like `session.max_sessions`, a detection pattern, an LLM entry, or the burn-guard budget. It is expressed as a **capability group** named `self-config`, and the single operator-facing switch for the whole group is `mcp.allow_self_config`.

**The group** is builtin and fixed in code. It grants: `config:write`, `autonomous:write`, `observers:write`, `llms:write`, `usage:write`. It deliberately grants **no** access to anything under `secrets:`, anything under `federation:`, or daemon-identity control (sessions kill, compute control, plugin install, tailscale admin). Being a group rather than a loose list of caps is what makes it grantable and auditable as one unit.

**The alias.** `mcp.allow_self_config` exists for the operator's convenience in both YAML and the Settings UI; under the hood it *is* the group. One field, three effects: (1) while it is disabled, every MCP configuration-write tool refuses with the reason `self-modify is disabled`; (2) the default capability set handed to a spawned session is `session-default` **plus** `self-config` while enabled, and `session-default` alone while disabled (an explicit `session.capabilities` list wins over both); and (3) while disabled a chat-channel `configure` for a configuration key is refused and the refusal is audit-logged. Setting the flag on does not grant capabilities to a credential that never had the group; it only controls whether the default grant includes it and whether the write paths accept it.

**Default: enabled.** Absent `mcp.allow_self_config` means enabled — the operator's answer for this installation, baked into the default. An explicit `false` disables. Re-enabling is removing the `false` (or setting `true`) and restarting — never a session flipping the key itself: on the MCP surface `config_set` always refuses it, whatever the alias's state (bootstrap-protected), and on the other session surfaces the key is only reachable by a credential that holds `config:write` itself, which the alias's own disabled state removes from the default grant. The operator's own surfaces (YAML, CLI, Web UI, REST with the operator credential) can set it either way.

**Configurable settings.**

| Setting | Default | Meaning |
|---|---|---|
| `mcp.allow_self_config` | enabled (absent) | Alias for the `self-config` group: on = sessions/agents may write their daemon's non-secret configuration; explicit `false` = they may not |
| `mcp.self_config_audit_path` | `<data dir>/audit/config-self-modify.jsonl` | Where the self-config audit file (JSONL) is written |
| `mcp.self_config_audit_denied` | on | When on (default), refused attempts are recorded too (`allowed: false` + `reason`); `false` records only the successful writes |

**Audit.** One record per configuration-write attempt by a session or peer credential, regardless of surface — exactly the surfaces they write from: **mcp** (a config-write tool), **rest** (`PUT /api/config`), **comm** (the `configure` verb). Each record carries the time, the surface, the tool or route, the keys and values (secret-looking values redacted), the acting credential (a session `session:<id>` or a federation peer `peer:<name>`; the operator's own admin writes are not recorded), whether it was allowed, and the refusal reason when not. Reading the audit is never gated: `self_config_audit` (MCP, `limit` 1–1000, default 100), `datawatch audit self-config` (CLI), `GET /api/audit/self-config` (REST), the **Self-config** card on the PWA, or the file itself on disk.

**How to set it** — in YAML under `mcp:` as `allow_self_config: false`; with `datawatch config set mcp.allow_self_config false` (operator CLI); in the PWA under Settings → General → **Self-config** (toggle **Allow self-config**, plus the audit path and the record-denied toggle); with `PUT /api/config` and the body `{"mcp.allow_self_config": false}`; or from inside a session — there the key is bootstrap-protected: `config_set` with it is always refused, whatever the alias's state. All writes need a restart to affect the default capability grant of newly spawned sessions. Scoping and hardening — narrowing the group with a custom one, removing it from the session default, or granting it to one selected daemon — is walked through step by step in [`howto/self-config-capability.md`](howto/self-config-capability.md).

**See also:** [`howto/self-config-capability.md`](howto/self-config-capability.md) · [`howto/federation-cbac.md`](howto/federation-cbac.md) · capability groups · [Sensitive writes](#sensitive-writes)

### Sensitive writes

**Sensitive writes** are a class of MCP tools the operator singled out as high-impact: setting, deleting and reading a secret value (`secret_set`, `secret_delete`, `secret_get`), the identity writers (`set_identity`, `update_identity`, `configure_identity`, `restore_identity`, `reset_identity`), daemon control (`restart_daemon`, `reload`), and `plugin_install`, `plugin_run_subcommand`, `send_input`, `start_session`, `tailscale_acl_push`, `tailscale_auth_key` and `acme_renew`. `secret_get` is in the class although it only reads, because it returns the secret value. Listing secret names, checking that one exists and the vault status are not sensitive.

It is one of three classes every MCP tool belongs to: **configuration writes** (the daemon settings and component registries covered by [Self-config](#self-config)), **sensitive writes**, and everything else.

**The switch.** `mcp.allow_sensitive_writes` decides whether a **session or a federation peer** may call the class. Default: **enabled** — an absent key and `true` both mean on, and nothing adds the key for you. An explicit `false` makes the daemon refuse each such call from a session or peer with a message that names the setting; the operator's own Web UI, REST, CLI and admin calls are never refused. It is independent of `mcp.allow_self_config`.

**Audit.** Calls to the class are recorded in the same audit trail as configuration writes, allowed and refused, with `class: "sensitive"` (configuration writes carry `class: "config"`). A sensitive record keeps the argument names and never the values.

**How to set it** — on the Settings page, in the **Self-config** card, as the **Allow sensitive writes** toggle; with `datawatch config set mcp.allow_sensitive_writes false`; with `PUT /api/config`; or with the comm `configure` command (it appears in `configure list`; the reply's "Restart may be required." is generic wording, the change is already live). Each is applied to the running daemon and saved to the config file, with no restart. `GET /api/config`, `datawatch config get` and the MCP `get_config` tool with `section=mcp` report the effective value, so a config that never set the key reads `true`. In YAML it is `allow_sensitive_writes: false` under `mcp:`, followed by a restart. Write exactly `true` or `false`: any other text, such as `on`, is read as off.

**A session can change it.** The `config_set` tool reaches the same setting and, unlike `mcp.allow_self_config`, the key is not bootstrap-protected, so while self-config is on a session may set it back to `true`. Close `mcp.allow_self_config` as well when the switch must be operator-only.

**See also:** [Self-config](#self-config) · [`howto/self-config-capability.md`](howto/self-config-capability.md#sensitive-writes-a-separate-switch) · [`operations.md`](operations.md#sensitive-writes-what-sessions-and-peers-may-not-do)

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

### Rate-limit reset out of reach

The CodeQL alert gate's state when GitHub's core rate limit resets later than the wait budget that is left (`WAIT_MAX_SECONDS` minus the time already waited). It applies to both steps: `wait-analyses` and `open-alerts`. The gate does not sleep toward a reset it cannot reach: it prints a `Rate limited` error line (naming the reset epoch when one is known) and exits 1 at once. A reset inside the budget is waited for and retried; a rate limit with no reset time is retried on the normal interval until the budget runs out. Either way the run is a rate limit, not a missing analysis, and `open-alerts` never reports a zero-alert result from it. See [`howto/codeql-local-scan.md`](howto/codeql-local-scan.md#red-check-from-a-rate-limit).

**Scanner-accepted barrier** — a code change that makes a CodeQL code-scanning alert close because the scanner recognizes the safe pattern, rather than the alert being dismissed. Example: the PWA session card quick-command select is built with `createElement` and `textContent`, so command names display as text and are never parsed as markup; behavior is unchanged. The alert counts as closed only after the next "Open high/critical alerts" gate run on main shows it gone. See [operations.md](operations.md#open-code-scanning-alerts-fixed-on-main-closure-pending-the-gate-run).


Key terms used across the docs, API, and hook payloads.

**per-turn chunked output capture** — planned (design pass approved,
not implemented): recording a session's output in one chunk per turn so
session history does not depend on tmux scrollback. Today history comes
from `output.log` (written by `tmux pipe-pane`) plus the tmux scrollback
buffer. Do not rely on chunked capture existing yet. Design:
[`plans/2026-10-14-turn-chunked-session-history.md`](plans/2026-10-14-turn-chunked-session-history.md);
its open questions await operator review.

**Code scanning upload (Release workflow)** — the Release workflow steps that send SARIF reports to GitHub Code Scanning: the gosec report (category `gosec`, `gosec baseline-diff` job) and the Trivy reports in the base and agent image jobs. Each is a copy of a result a scan step has already judged, so none blocks a release: a step tries up to four times with waits of 60, 180 and 300 seconds between attempts (this covers GitHub's rate limit, but a retry happens on any failure), a failed attempt never fails the job or holds up goreleaser or the image pushes, and if every attempt fails the step logs a warning and the run continues. The blocking gosec baseline check and Trivy scan steps are unchanged. Each SARIF file is also kept as a run artifact for 90 days (`gosec-results` and `trivy-<image>-sarif`). A run whose upload gave up has no new entry in Code Scanning, which is not the same as a clean scan. `make check-sarif-upload` (part of `make lint`) fails if any workflow upload becomes blocking or loses its retries. Not a daemon setting. See [operations.md](operations.md#code-scanning-upload-in-the-release-workflow), [howto/release-push-and-tag.md](howto/release-push-and-tag.md#when-the-code-scanning-upload-fails) and [security-review.md](security-review.md).

**gofmt format check** — the repository's gofmt audit, `scripts/check-gofmt.sh` (Makefile target `check-gofmt`, part of the `lint` target and the local pre-tag run). It lists the Go files under `cmd/` and `internal/` that `gofmt -l` reports as unformatted, excluding files with a `Code generated` header (first five lines) and anything under a `testdata/` or `vendor/` path; any remaining offender is listed with its fix command (`gofmt -w <file>`) and the script exits 1, a clean tree prints OK and exits 0. The script is read-only — it never writes or formats — and `make fmt` (`gofmt -w .`) is the fix. It is not a daemon setting, has no REST/MCP/CLI/PWA surface, and runs as a step of the `test + lint` job in the CI workflow (`.github/workflows/ci.yaml`, every push to `main` and pull request); enforcement additionally comes from the `make lint` battery and the local pre-tag run on the build host. See [howto/gofmt-check.md](howto/gofmt-check.md) and [operations.md](operations.md#gofmt-format-check).

**no-PWA-when-spec-forbids guard** — the guard against a branch changing
the PWA even when its task spec forbids a PWA change, added after an
Automaton branch (e19b0001, daemon-side priority stall wake) modified
`internal/server/web/app.js` although its own spec forbade it.
`scripts/check-no-pwa-when-spec-forbids.sh <spec-file> [base-ref]`
lowercases the spec and matches it against a closed list of five phrases
(`no pwa change`, `no pwa part`, `forbade any pwa change`, `forbids any pwa
change`, `must not change the pwa`); only then does it refuse (exit 1,
stderr line `forbidden PWA change: internal/server/web/app.js`) when the
exact path `internal/server/web/app.js` appears in `git diff --name-only`
against the base ref (default the merge base of `HEAD` and `main`, or an
explicit second argument). Exit 0 when the spec does not forbid the PWA or
`app.js` is untouched; exit 2 on a wrong argument count, a missing spec
file, an undeterminable default base ref, or a failed `git diff`. The guard
is read-only, and its self-test, `tests/test-no-pwa-when-spec-forbids.sh`
(a throwaway `mktemp -d` git repo, four cases asserting the exit code and
the exact stderr line), is one of the entries in the `test:` target of the
`Makefile`; the guard itself is run by hand — it is in neither `make lint`
nor the CI workflows. It is not a daemon setting and has no REST/MCP/CLI/PWA
surface of its own. See
[howto/no-pwa-when-spec-forbids.md](howto/no-pwa-when-spec-forbids.md)
and [operations.md](operations.md#no-pwa-when-spec-forbids-guard).

**Tag selection** — the tags chosen in an Automata list filter, and how they are checked. An object's derived system tags (for example `project:datawatch`) and its assigned tag IDs are pooled; match ANY needs at least one selected tag in the pool, match ALL needs every one, and a selection with no tags matches every object. The check is unchanged by the code-scanning fix, which only removed a computed size hint on the internal lookup set. See [howto/automata-tags.md](howto/automata-tags.md).

**Merge pending** — the state of a finished Automaton whose work branch (`automaton/<id>`) still has commits that are not on `main`, where the check is sure of it: both tests agree that the commits are missing. A branch where the two tests disagree is not merge pending; it is *needs a look*, a separate state. The work is done but has not been merged, so it can look complete while it exists only on its own branch. The merge-pending check works this out with read-only git queries and never merges, deletes or pushes. It is controlled by `autonomous.automation.merge_pending_mode` (`off` by default, `shadow` to record only, `on` to record and alert) and `autonomous.automation.merge_pending_alert_minutes` (30 by default; `0` turns the alert off). With the check `on`, one alert is raised per episode after the branch has waited that long, and merging the branch clears it. The other states are merged, empty (nothing ahead of `main`), needs a look and unknown. The PWA shows a badge on a finished Automaton and on its Automation card; the same state is on REST, MCP, the CLI and the chat verbs as `autonomous merge`. See [operations.md](operations.md) (*Merge-pending check*) and [flow/automation-merge-pending-flow.md](flow/automation-merge-pending-flow.md).

**Terminal-state merge coverage** — the merge-pending check's coverage of archived, needs_review, blocked and failed Automata, not only completed ones, controlled by `autonomous.automation.merge_pending_include_terminal` (on by default; an unset key reads true; readable and writable on the config file, REST, MCP, CLI, the messaging channel and the web app). Covered Automata get the same states, shadow decisions and once-per-episode alert, which names the Automaton, its state and its branch; a failed or blocked Automaton's triage record is left as it is. See [operations.md](operations.md) (*Merge-pending check*).

**Clean shadow window** — a period of merge-pending shadow mode with enough decisions to judge, during which `shadow_decisions_false` (would-alert decisions whose episode later ended empty or needs a look rather than merged) stayed at 0. It is the operator's signal to switch `merge_pending_mode` from `shadow` to `on`; the daemon never switches by itself. See [operations.md](operations.md) (*Merge-pending check*).

**Needs a look** — the merge-pending state for a finished Automaton whose branch the check cannot call merged or unmerged, because the two tests it uses disagree: a commit's patch and its subject line are matched against `main`, and one matches while the other does not. The usual cause is a commit that was merged but changed on the way in, for example by a conflict fix, so its subject line is on `main` and its patch is not; a reworded subject over an identical patch gives the same result. It is a prompt to compare the branch with `main` by hand, not a verdict. It raises no alert, writes no merge-pending record, and the branch is held (never listed as safe) in the safe-to-delete report. The PWA shows it as **Automaton needs a look**.

**Child inheritance** — the way a child Automaton (one a running Automaton spawns for a task, a fix Automaton filed from a scan, or a defect filed with a parent) starts from its parent's setup at creation: project link and repository directory, the parent's Automation, backend and model, verification backend and model, tags and priority. An explicit value given at creation always wins, and a model is never copied without its backend. A child of a Sonnet or Haiku parent is never put on an Opus or Fable model by inheritance. It runs once per child, never fails the creation, and leaves one `child_inherit` decision on the child. It is controlled by `autonomous.automation.child_inherit` (on by default). See [howto/automaton-child-inheritance.md](howto/automaton-child-inheritance.md) and [operations.md](operations.md) (*Child inheritance*).

**Branch chain** — a run of members between a fork and its join that executes one after another on a single branch, while the other branches run side by side. A chain member carries `on_complete` only; when it finishes, only its own branch moves on. If a chain member blocks, fails, is cancelled or cannot start, its branch ends failed, the members behind it are skipped and the join's `on_branch_failure` rule decides the outcome. Chain members must be Automaton, nested Automation or decision wait members. Status lists each chain under `branch_chains`. See [howto/automation-chains.md](howto/automation-chains.md).

**Max parallel** — an optional limit on how many members of an Automation run at once: `max_parallel` on the Automation counts all running members, on a fork it counts only that fork's branch members, and the smaller applies when both are set. Zero or unset means no limit. A member held back shows `waiting_for_slot` (not blocked); waiting members start in member order as running ones finish, and a restart keeps them waiting. The limit can be changed on a running Automation (or one fork, with a member id) without resending the record: REST `POST /api/automations/{id}/set_max_parallel`, MCP `automation_set_max_parallel`, CLI `datawatch automation set-max-parallel`, chat `automation set-max-parallel`; raising or clearing it starts waiting members at once, lowering it never stops a running one. See [howto/automation-chains.md](howto/automation-chains.md) and [operations.md](operations.md) (*Automation parallelism, decision waits and stage gating*).

**Decision wait** — a member of kind `decision_wait` that raises an operator question (asker `decision-wait`) and an "Automation waiting for you" alert, then holds only its own branch until the question is answered. Any answer completes the member; withdrawing the question fails that branch. Status shows `waiting_for_operator`. The web app's Automation strip shows these waits (waiting for the operator, for a slot, for a stage) with a count line above the pipeline, and the Inbox lists the question. It needs `autonomous.questions.enabled`. See [howto/automation-chains.md](howto/automation-chains.md).

**Stage gate** — an optional Automation setting (`stage_gate`, off by default) that turns a member's `stage` label into an order: a staged member does not start while an earlier stage has unfinished members. Stages are ordered by first appearance in the `members` array; members without a stage are never held and never hold others. A held member shows `waiting_for_stage`. Finished means completed, failed or skipped, so a blocked member keeps holding the next stage. It composes with Max parallel. It can be flipped on a running Automation with `set_stage_gate` (REST `POST /api/automations/{id}/set_stage_gate`, MCP `automation_set_stage_gate`, CLI `datawatch automation set-stage-gate`, chat `automation set-stage-gate`): off starts held members at once, on holds members that have not started. See [howto/automation-chains.md](howto/automation-chains.md).

**Waiting status** — one of `waiting_for_slot` (Max parallel full), `waiting_for_stage` (Stage gate holding) or `waiting_for_operator` (Decision wait open). A waiting member has not started or is paused on an answer; it is not blocked and not failed. See [howto/automation-chains.md](howto/automation-chains.md).

**Merged member** — a member Automaton of an Automation that ended cancelled or completed while its work branch (`automaton/<id>`) was already merged into the project's own default branch, so ending it would otherwise block the Automation although the work is on the default branch. The daemon proves the merge with read-only git queries and records one *merged-member decision* in the Automation's decision log: the member, its branch, the default branch it was proved merged into, the proving commit, the policy in force and the outcome (*advanced* when the Automation moved on, *blocked* when it stayed for the operator). What happens is set by `autonomous.automation.merged_member_policy` (`completed` by default, `manual` or `block`; see *Merged-member policy* under Settings — Automate). A member reopened by the operator resumes the chain. The Automation detail page in the PWA shows the decisions, and the same list is on REST, MCP, the CLI and the chat verbs.

**Project membership** — which configured project an Automaton counts toward in every project roll-up (the Projects page, its plans, Gantt rows, History, progress snapshots, cost and progress grouping and the Events project filter). The first match wins: the Automaton's own *project link* (the name of a configured project), the link of the Automation holding it, its parent Automaton's project (a spawned child follows its parent), then its project directory (a git worktree maps to its repository). A link beats a directory match, and a link naming an unknown project is ignored. An Automaton no step places is in the **Automata without a project** group (`without_project` in the roll-up response): listed once, never counted in any project's total. See [howto/projects-tab.md](howto/projects-tab.md#which-automata-belong-to-a-project).

**Project link** — the explicit `project` field on an Automaton (and on an Automation): the name of a configured project. It is the first thing *Project membership* checks. Set or clear an Automaton's link from the Project field in Automaton settings, `POST /api/autonomous/prds/{id}/set_project`, `datawatch autonomous prd-set-project <id> <project|clear>`, MCP `autonomous_prd_set_project`, or the chat verb `automaton project <id> <name|clear>`. An unknown name is refused with the configured names; each change is written to the Automaton's decision log and no session is touched. The Automaton read reply reports the effective project as `project` and how it was found as `project_source` (`link`, `automation`, `parent`, `directory` or `none`). No surface sets an Automation's link yet. See [howto/projects-tab.md](howto/projects-tab.md#link-an-automaton-to-a-project).

**Roll-up cache** — the daemon's short-term memory of a finished project roll-up. The roll-up is built from the summary index rather than the full Automaton records, and the result is kept per project and window, keyed by the *store revision* together with the project configuration and the Automation list. Any change to a member Automaton, to the project's directory or arcs, or to the Automation list gives a new key, so the next read rebuilds it and a project nobody touched is answered from the cache. The measured targets for 700 Automata are under 300 ms warm and under 1.5 s cold. It is automatic and has no setting; the answer is the same as an uncached read, and `generated_at` in the response is the time it was built. The Projects page also keeps the last good roll-up per project in the browser, so it opens at once from that copy and refreshes behind it. See [howto/projects-tab.md](howto/projects-tab.md#4-read-the-roll-up) and [operations.md](operations.md) (*Project roll-up and read speed*).

<a id="summary-index"></a>
**Summary index** — the daemon's in-memory list of one small entry per Automaton: identity (id, title, status, revision), story and task counts, status counts, cost totals, tags, type, backend, priority and parent, and none of the story or task text. It is built from the store when the daemon starts and kept current on every write. The Automata list, the Projects roll-up, the timeline, History and the progress counts are all read from it, so they never read a record from disk and do not slow down as Automata gain spec and task text. An archived Automaton reads the same entry as before. It is automatic and has no setting. See [howto/automata-tab-loading.md](howto/automata-tab-loading.md).

<a id="store-revision"></a>
**Store revision** — a counter the Automaton store raises on every change to any Automaton (a create, an update, a task change, a delete or a load). A read never changes it. The roll-up cache and the once-per-revision points calculation key on it: while the revision is unchanged a repeat read is answered from memory, and a write moves the revision on so the next read rebuilds. The revision is internal and is not a setting; an Automaton's own `revision` on a list row is a separate per-record value. See *Roll-up cache* and [operations.md](operations.md) (*Project roll-up and read speed*).

<a id="retention-window"></a>
**Retention window** — for the archive, how long a finished Automaton is kept whole before its bulky parts are archived: `autonomous.archive.retention_days` (default 30; a value of `0` means 30; a negative number is refused). An Automaton that is completed, shipped or cancelled and was last updated longer ago than the window becomes eligible, unless it has a running task, an open session or an unmerged branch it still needs. The setting changes live. Not to be confused with the worktree cleanup's `cleanup_retention_hours` or the progress snapshot retention. See [howto/automata-archive.md](howto/automata-archive.md).

**Automaton without a project** — an Automaton that no *Project membership* step places: it has no *project link* that names a configured project, neither its Automation nor its parent resolves to one, and its directory matches no project. It is shown once in the **Automata without a project** group (`without_project` in the roll-up response) with its own tasks, points and throughput, is the same whichever project you open, and is not counted in any project's total, so no Automaton is invisible and no project's totals include work that belongs to nobody. Set its link (see *Project link*) to move it into a project. See [howto/projects-tab.md](howto/projects-tab.md#which-automata-belong-to-a-project).

**Project membership source** — how an Automaton's project was found, reported as `project_source` next to `project` in the Automaton read reply: `link` (its own project link), `automation` (the link of the Automation holding it), `parent` (its parent Automaton's project), `directory` (the directory match, the fallback, where a worktree maps to its repository) or `none` (the Automaton is without a project). The Project field in Automaton settings shows the same source in words. See [howto/projects-tab.md](howto/projects-tab.md#which-automata-belong-to-a-project).

**Project link backfill** — one pass that links Automata with no project link to the `datawatch` project, matching through the Automation's project, the parent's project, then the repository directory (a worktree maps to its repository). It never changes an Automaton that already has a link, skips templates, and lists an Automaton that matches nothing as skipped. The default is a dry run that only returns the report (entries with project, source and result, and the scanned, matched, applied, skipped and failed counts); apply writes the links and a second apply finds nothing to do. Run it with `datawatch autonomous project-link-backfill [--apply]`, `POST /api/autonomous/project-link-backfill` (`GET` returns the latest report), MCP `autonomous_project_link_backfill`, or the chat verb `automaton project-backfill [apply]`. At daemon start it runs once: a dry run unless `autonomous.project_link_backfill_apply` is `true` (default `false`), in which case it is applied and, if every write succeeded, a done marker (`project_link_backfill.done`) stops later starts from applying it again. See [howto/projects-tab.md](howto/projects-tab.md#link-existing-automata-in-one-pass).

**Safe-to-delete report** — a read-only list of which Automata work branches and worktrees can be cleaned up. It looks at every local `automaton/*` branch in the worktree base repository and sorts each into **safe** (all commits on `main`, or none ahead of it, and no uncommitted changes in its worktree) or **held** (merge pending, needs a look, not comparable, or a worktree with uncommitted changes). It runs git when it is read, but at most once per 60-second window: the daemon caches each repository's result in memory for 60 seconds, so every read within that lifetime comes back at once, a read past it still comes back at once, marked **stale**, with `computed_at` naming when the stored result was made, and a single background refresh, bounded at 60 seconds and keeping the last good result if it fails, keeps it current. It works whatever the merge-pending mode is, and **never deletes anything**: removing a branch or worktree is a separate step the operator checks and carries out. Read it with `GET /api/autonomous/safe-to-delete`, MCP `autonomous_safe_to_delete`, `datawatch autonomous safe-to-delete`, the chat verb `autonomous safe-to-delete`, or the card under the stream on the Automata Events sub-tab. See [operations.md](operations.md) (*Merge-pending check*).

**Automaton worktree cleanup** — the daemon's removal of what a finished Automaton leaves behind, plus a guard on the disks that hold it. An Automaton running in worktree mode works on its own branch (`automaton/<id>`) in a worktree under the worktree base repository, and its worker sessions leave scratch files in the daemon's temporary directory. Two passes clean up: the **worktrees** pass removes a completed or cancelled Automaton's work branch and worktree, and the **temp files** pass removes the daemon's own scratch files for sessions that have ended. A third pass, the **cache folders sweep**, has no status row: it runs once at daemon start and on every coordinator poll, in every `cleanup_mode`, and removes stale Go build cache (`go-build`) and Trivy scan-cache folders that sit directly in the temporary directory (at least 60 minutes old, a fixed retention) only when no live process holds them — the **liveness probe** walks `/proc` on Linux; on a platform without a readable `/proc` liveness is unknown, unknown is treated as held, and the sweep removes nothing. Nothing else removes these folders, which is why the temp directory used to fill up until builds and tests failed. A pass removes an entry only when it is certain nothing is lost: the Automaton is finished, no live session or lease uses the worktree, the worktree has no uncommitted changes the cleanup cannot name (an **untracked** harness file of the daemon's own tooling or backends — `.mcp.json`, `.aider.conf.yml`, `.aider.chat.history.md`, `.aider.tags.cache.*`, `.goose/`, `.gemini/`, `.opencode/`, `opencode.json`, the injected skills folder and launcher files, and the per-session MCP discovery file — does not hold the pass; a changed tracked file or an unrecognised untracked file does), every commit is already on the project's default branch (taken from the Automaton's own base branch, or the branch the remote records as default, never an assumed name) and the work is older than the retention window. Removal runs `git worktree remove` strict, with `--force` only in that harness-file case (those files listed on the removal and in the audit entry as `resolved_files`), and the branch is always `git branch -d`, never `-D`. An entry the pass holds for uncommitted work is reported with the held `files` and a `release_action` — the one action that frees it (commit the work or discard the files; the next pass resolves the entry), and the status carries the pass's latest held entries as `last_worktree_skips`. The temp pass counts a path the file system refuses — unreadable while walking, or a refused removal — as skipped for reason `unreadable`, not as a failure. The mode is `autonomous.automation.cleanup_mode`: `off`, `report` (the default: record what would be removed, remove nothing) or `on`; `cleanup_retention_hours` (24) and `cleanup_max_removals_per_pass` (10) set the rest (the removal cap does not bound the cache folders sweep). Each pass counts what its owner released at the end of the work (**owner**) separately from what the periodic sweep had to catch (**net**); the aim is for the net to find nothing. The disk guard checks two disks against their own thresholds — the worktree disk against `disk_alert_percent` (85) and the temp disk against the lower of that value and a fixed 80 — and while either is at or above its threshold raises one warning alert, at most once an hour, in every mode; the alert names the disk (and, while the temp disk is over, the biggest folders directly in the temp folder) and, in the status, `biggest_temp_folders` carries the top five with each one's liveness. The alert never removes anything. Read the status with `GET /api/autonomous/cleanup`, MCP `autonomous_cleanup_status`, `datawatch autonomous cleanup`, the chat verb `autonomous cleanup`, or the Cleanup card on the Automation Events sub-tab. See [howto/automaton-worktree-cleanup.md](howto/automaton-worktree-cleanup.md) and [operations.md](operations.md) (*Automaton worktree cleanup*).

**Task worktree cleanup** — what closing an Automaton does with the worktree and branch of each of its tasks, under the same `cleanup_mode`. A task worktree with uncommitted or untracked files (other than the daemon's own generated and tooling files) is never removed, nor is one holding real files in the daemon's ignored folders under `.datawatch/`, and a task branch or Automaton branch is deleted only after its tip is kept under `refs/datawatch/reap-backup/` (a task branch also only when its commits are on the default branch). The cleanup on reject or archive follows the same mode. The Automaton's `task_worktree_cleanup` says how many were kept and why. See [Automaton worktree cleanup](howto/automaton-worktree-cleanup.md#task-worktrees-and-task-branches).

**Generated document exemption** — the worktree cleanup and the safe-to-delete report do not count the daemon's own generated documents (gate report, parity issue draft, drafts, and the hash file beside each) as uncommitted work. Only an untracked file that still carries the generated marker, carries no edited line and matches its recorded hash qualifies; a hand-written or edited file holds the work tree. The cleanup deletes the qualifying files with the tree and lists them in `resolved_files`. See [howto/automaton-worktree-cleanup.md](howto/automaton-worktree-cleanup.md#what-is-never-touched).

**Unlanded work** — an archived Automaton whose work branch (`automaton/<id>`) has commits that are not on the project's default branch, so the cleanup will never remove it. The review is read-only and compares by commit content, so a rebased or renumbered merge still counts as landed. Each branch is recommended as **landed** (every commit is already on the default branch: a leftover), **superseded** (every file its commits change was changed on the default branch since it forked) or **needs decision** (nothing shows it was replaced). The actions are **Keep** (leave it alone, with a note), **Archive branch** (move it to `refs/archive/<branch>`, commits stay reachable) and **Delete worktree** (the only destructive one: needs an explicit confirm and writes a verified git bundle backup of the branch's commits first; uncommitted work in the worktree is not in that backup and is discarded with the worktree). Each read also carries **counts**: per landing state (**landed**, **pending** = superseded, **live** = no settled recommendation yet, **unlanded** = needs decision), per age band of the last commit (under one day, under one week, older) and the oldest pending branch with its age. When the pending count stays above `autonomous.unlanded_alert_threshold` (default 3), a once-a-day **pending-branch alert** names the oldest one; `autonomous.unlanded_alert_enabled` turns it off. The supervisor watch reports each such branch as the `archived_unmerged` finding, and the supervisor loop's close step keeps a finished Automaton while its branch holds commits that are not on the base branch (a cancelled one past its grace included; `autonomous.archive_keep_unmerged` releases the guard for completed and rejected only) — see [operations.md](operations.md#keep-unmerged-branch-work-on-archive-autonomousarchive_keep_unmerged). It is shown as the **Unlanded work** card on the Housekeeping sub-tab, and also reached by REST, the CLI and the chat verb (MCP is read-only). See [howto/automaton-worktree-cleanup.md](howto/automaton-worktree-cleanup.md).

**Landing record** — the per-Automaton record (`landing` on the Automaton) of whether its work branch's work reached the project's default branch. An Automaton that carries a work branch starts with the state **pending** (set by the daemon when the branch is created); **landed** is set when the branch ships — the `mark_completed` post-ship hook names the merge commit — and **kept** or **dropped** are operator decisions (an actor and a non-empty reason, refused when the branch is already landed, each writing one `landing_keep` or `landing_drop` decision on the record's decision log). A **pending** landing whose branch tip is an ancestor of the default branch (`merge-base --is-ancestor`) is treated as landed, whether the merge came from the pipeline, the shipped record call or a hand merge: the archive check passes and the record is updated to **landed** with the merge commit; a branch with commits not in the default branch stays not landed. While the state is still **pending** and not landed that way, archiving the Automaton is refused on every surface, the branch named in the error: merge it, or record keep or drop for its landing; records without a landing (no branch, or never migrated) archive as before. The record rides on every Automaton read, and keep/drop are available on every operator surface: `POST /api/autonomous/prds/{id}/landing/keep|drop` (REST), the CLI `autonomous prd-landing-keep|drop <id> --reason <text>`, MCP `autonomous_landing_keep|drop`, the chat verb `autonomous landing keep|drop <id> <reason…>`, and the Keep/Drop buttons in the landing block of the Automaton's Overview sub-tab (PWA). A one-time landing migration (`MigrateLanding`) backfills a `pending` record (actor `landing_migration`) for every archived Automaton whose branch git cannot prove merged on the default branch — idempotent, changes no status and writes no decision, and skips rather than guesses when the default branch is unknown. The daemon runs it once at start, in the background (`StartLandingMigration`): it logs the count it marked, raises one operator alert (*Archived Automata with unlanded branches*) when that count is above zero, and writes a done marker (`landing_migration.done` in the autonomous data directory) after a pass with no errors so it never runs again; a pass that hit an error, such as an unknown default branch, is retried at the next start. It has no operator command; the daemon start is its only caller. It is a different thing from the *Unlanded work* review above, which lists archived branches by their git state. See [howto/landing-record.md](howto/landing-record.md) and [howto/automaton-worktree-cleanup.md](howto/automaton-worktree-cleanup.md) (*The landing record*).

**Cleanup permission error** — a worktree removal the file system refused because a build or test left read-only files in it. The cleanup status shows one record for them with a plain reason, the affected paths, and whether the safe fix can run. The fix restores owner write permission only, inside the worktree base; it removes nothing, and it checks that every path is inside the worktree base before touching the file system. A **Fix permissions** button inside the Cleanup card on the Events sub-tab runs it. A record whose folders are already gone is hidden. See [howto/automaton-worktree-cleanup.md](howto/automaton-worktree-cleanup.md).

**Task-subbranch reap** — a separate reaper that removes the finished *task* sub-branches (`worktree_branch`, one per task in parallel mode) of Automata whose work has settled, in the repository. It first works out each Automaton's **landing state** from git alone — **landed** (the `automaton/<id>` work branch exists and every commit of it is already on the base branch by commit *content*, `git cherry`), **dropped** (the work branch is gone, so only the base can contain the work), or **pending** (the work branch still carries commits not on base — a comparison git cannot answer reads as pending, so an unknown state never loses a branch) — and skips pending Automata whole. A task sub-branch is removed only when **every commit of it is contained in the Automaton's work branch or in the base branch**; before any removal a **backup ref** `refs/datawatch/reap-backup/<branch>/<Unix seconds>` (branch-name characters that would not be ref-safe become dashes) is written, and the branch is deleted (`git branch -D`, the one forced delete in this area — permitted because the backup ref exists; a failed backup means the branch is not deleted, a failed delete leaves the backup restorable) only after the backup exists. A **restore** (RestoreReaped) recreates the branch from the newest backup ref for that name, and errors without creating anything when no backup ref exists. Backup refs older than `autonomous.reaped_branch_backup_days` are pruned — unset or 0 reads the default **30** whole days; a negative value is refused as a config error — and pruning closes the restore window for that branch. A merge in progress (`MERGE_HEAD` in any of the Automata's working directories) ends the pass early: no backup is written, nothing is deleted. Set the retention with `datawatch config set autonomous.reaped_branch_backup_days 45`, `PUT /api/config` or `PUT /api/autonomous/config`, the chat verb `configure autonomous.reaped_branch_backup_days=45`, MCP `config_set`, or the PWA Settings field "Reaped Automaton branch backup kept (days; 0 or unset = 30)"; `GET /api/config` reports the effective days under `autonomous`. Distinct from the worktree cleanup (work branches and worktrees), from the unlanded work review (its **Keep** records do *not* suppress the reaper), and from the session-end / host-load reapers (they reap *processes*, not branches). The report, the pass and the restore are on every surface: `GET /api/autonomous/branch-reap` (report: `reaped`, `waiting`, `backups`, `backup_days`, `computed_at`), `POST /api/autonomous/branch-reap/run` (one pass; no body; a concurrent pass answers 409), `POST /api/autonomous/branch-reap/restore` (one branch from its newest backup ref; a branch that was never reaped answers 404 — the two `POST`s need the autonomous write capability from a federated peer), the CLI `datawatch autonomous branch-reap [--json]`, `branch-reap-run [--json]` and `branch-reap-restore <branch> [--json]`, the MCP tools `autonomous_branch_reap` (read-only), `autonomous_branch_reap_run` and `autonomous_branch_reap_restore`, the comm verbs `autonomous branch-reap` and `autonomous branch-reap-restore <branch>` (no pass verb), and the PWA **Branch reaper** card on the Automata Events sub-tab (report and **Restore** per branch; no run button). Nothing runs a pass on a timer and the backup expiry has no caller yet — the refs accumulate until something prunes them; review both. **Live-daemon wiring (found 2026-11-18, fixed):** until the fix, a running daemon answered 503 with `the autonomous manager does not expose the branch reaper` on all three routes even with the base repo set (the daemon's API wrapper did not carry the reaper's methods); the CLI, MCP, comm and PWA surfaces all failed with it and finished Automata kept piling up. The daemon now wires the reaper in with the routes, and the 503 means only that `autonomous.worktree_base_repo` is unset. See [howto/automaton-worktree-cleanup.md](howto/automaton-worktree-cleanup.md) (*The task sub-branch reaper*).

**Contained branch** — a task sub-branch every commit of which is already in its Automaton's work branch or in the base branch, so deleting it loses nothing. Only contained branches of settled (landed or dropped) Automata are reaped, never during a merge, and always after a backup ref under `refs/datawatch/reap-backup/` is written; restore recreates the branch from its newest backup ref within `autonomous.reaped_branch_backup_days` (default 30). See [howto/reap-contained-branches.md](howto/reap-contained-branches.md).

**Branch refresh** — the daemon merging the base branch (usually `main`) into an Automaton's own branch in the gap between two tasks, so a long-running Automaton does not fall far behind. It runs only when refresh is on for the Automaton, no task is running in its worktree, refreshing has not been stopped, and the branch is at or over the behind limit. Each merge attempt, whether it ends clean, in a conflict or in an error, is recorded as a `branch_refresh` Decision. A conflict in any file other than `CHANGELOG.md` is never resolved by the daemon (a conflict confined to `CHANGELOG.md` takes the base branch's copy and counts as clean): the branch is left as it was, refreshing stops for that Automaton and one question (asked by `branch-refresh`) offers *resolve by hand*, *retry later* or *leave the branch alone*. Settings are `autonomous.branch_refresh_enabled` (default `false`) and `autonomous.branch_refresh_behind_limit` (default 5). The per-Automaton override is set with `POST /api/autonomous/prds/{id}/branch-refresh` (also the CLI `autonomous branch-refresh`, chat `autonomous branch-refresh`, MCP `autonomous_prd_set_branch_refresh` and the Branch refresh card on the Automaton page). Not yet connected: answering the conflict question does not yet change whether refreshing is stopped, and nothing else clears the stop (changing the override or updating the branch by hand does not), so a stopped Automaton is not refreshed again and its branch must be merged with the base by hand. See [howto/automaton-branch-refresh.md](howto/automaton-branch-refresh.md).

**Uncommitted-work check** — the daemon's guard that a task is complete only when its work is committed. An Automaton that commits its own work (it has git settings and a worktree branch or declared write directories) is checked at two moments. The daemon's own marker files, such as the `DATAWATCH_COMPLETE` completion marker, are never counted. When a task finishes, the daemon commits the files the task declared, by name, with a `task(<short id>): <title>` message (each declared file is checked with `git check-ignore`; a declared file that git ignores and does not track, such as one under `.datawatch/`, is skipped instead of failing the commit, is never force-staged, and, when the task is held or warned, the information-only note `skipped because it is git-ignored: <list>` is carried in its hold reason; when every declared file is ignored no commit is made and nothing is left staged; and uncommitted-work itself never grows from files the repository's ignore rules ignore and that are not tracked, or from a file whose whole content is the daemon's injected marked sections — a tracked rules file whose only change is those sections, or a newly created one that holds only them — so neither holds a task nor opens a question), and if the commit failed or files are still uncommitted the task is **held**: parked as blocked with the reason and the files, and one question is raised offering **commit**, **discard** or **retry**. Before an Automaton is reported finished, the whole worktree is checked once more; under the default policy a dirty worktree blocks the finished state, so the Automaton is never listed as merge ready with work that would not ship. Files outside a task's declared files are listed and never committed automatically. At that before-finished check, an artefact the daemon itself generated and that a person did not edit (a generated artefact that still carries its marker line, has no `<!-- edited -->` line, and whose body still matches the hash the daemon recorded in its sidecar when it last wrote it), together with its sidecar file, is not counted as uncommitted work, so an Automaton whose only uncommitted work is what the daemon generated reaches finished and merge ready as usual; an artefact a person edited still holds, like any other file, and the per-task check is unchanged. The policy is `autonomous.automation.uncommitted_work_policy` (`hold`, the default, `warn` or `off`) and the scope of the per-task check is `autonomous.automation.uncommitted_work_scope` (`task_files`, the default, or `worktree`). Read it with `GET /api/autonomous/uncommitted-work`, MCP `autonomous_uncommitted_work`, `datawatch autonomous uncommitted-work`, the chat verb `autonomous uncommitted-work`, or the Automata uncommitted work card on the Automation Housekeeping sub-tab. See [Uncommitted-work check](#uncommitted-work-check), [howto/automaton-uncommitted-work.md](howto/automaton-uncommitted-work.md) and [operations.md](operations.md) (*Automaton uncommitted-work check*).

**Protected paths** — the files and directories an Automaton task may not change without a decision from the operator, and the check that enforces it. After a worker attempt finishes, the daemon compares the task's directory with the commit the work started from; if a protected path changed and the Automaton is not authorised for it, the task is **held** (the default: parked as blocked, with one question offering **Approve this change**, **Reject it** or **Change the spec**), **warned** about (recorded, the task goes on) or **blocked** (parked with no question). The default list is the project rules file, the supervisor rules, `CODEOWNERS` and the workflow files. The same change also protects the judge: with the default rules review source the rules review reads the rules from the base commit, so a task cannot rewrite the rubric it is judged by. The list, action and source are `autonomous.automation.protected_paths`, `autonomous.automation.protected_paths_action` and `autonomous.automation.rules_review_source`. One Automaton can be authorised for some paths in advance. Read it with `GET /api/autonomous/protected-paths`, MCP `autonomous_protected_paths`, `datawatch autonomous protected-paths`, the chat verb `autonomous protected-paths`, or the Automata protected paths card on the Automation Overview sub-tab. See [Protected paths](#protected-paths), [howto/automaton-protected-paths.md](howto/automaton-protected-paths.md) and [operations.md](operations.md) (*Automaton protected paths*). The check reads the working copy, the staged copy and the last commit, so a change that is committed or staged and then undone only in the working copy still counts. In a rules file, only text that is exactly what datawatch writes is set aside: other text between its markers, under its headings or after an unclosed marker counts as a change, and when the check cannot tell, the change is held.

**Drift checker git hardening** — how the file drift check and the undeclared-edit check run git: every call passes `-c core.fsmonitor=false`, `-c core.hooksPath=` and `--no-optional-locks`, and `git diff` also gets `--no-ext-diff`, so a program a project's own git configuration names (a file-system monitor, a hooks directory or an external diff program) is never started by the check. The calls are read-only (`diff`, `log`) and each runs against one project folder; a folder that is empty, contains a NUL byte, is relative, starts with a dash or is not an existing directory is refused with an `invalid git directory` error and git is not run, so the check for that task finds nothing. It is not a setting and has no switch; the findings, `drift_mode` and `undeclared_edit_mode` are unchanged. See [howto/file-drift-guardrail.md](howto/file-drift-guardrail.md#how-the-checker-runs-git) and [operations.md](operations.md) (*Undeclared Edit Mode*).

**Files left behind** — the list an Automaton keeps of files its run left in the project folder when it reached a final outcome (done or failed). The end-of-run cleanup removes the harness scratch files it can; a scratch file that is tracked by git, and any unexpected untracked file, stay and are listed. Each entry is one sentence (`leftover file: <path>`), repeated finishes do not add the same entry twice, and it never blocks or changes the Automaton. Harness scratch files the daemon itself writes for a session (`worker-mcp.json`, `worker-starting-context.md`) are never staged into a task's commit. The list is the `leftover_warnings` field of `GET /api/autonomous/prds/{id}` (and the `autonomous_prd_get` tool) and shows in the web Automaton detail under *Files left behind*; the datawatch-app mobile client has no list yet (parity draft `docs/plans/parity-issues/leftover-warnings-parity.md`).

**Pending item** — a named piece of work that has no Automaton yet, kept in the daemon's own registry so an Automation can carry it as a member and keep its place in the order. It has an id the daemon generates (eight hex characters), an optional `ref` (an outside label for the same work), a title, a state (`waiting`, `attached` or `done`) and a source (`internal`, added by hand, or `file`, copied from a file registry). When the Automation's cursor reaches a waiting item, the pending item policy decides: `hold` (the default) blocks the Automation there, with a reason that names the item, until an Automaton is attached, the item turns done, or the member is skipped or removed; `skip` passes the member over. Attaching an Automaton converts the member in place into an ordinary Automaton member (same position, edges and status), moves the item to `attached`, and, with auto-resume on, resumes the held Automation; when that Automaton completes the item becomes `done`. The registry is never read out of a document by default: an optional file registry copies rows from a pipe table in a file you name, using the id pattern and column settings you give it, and never assumes a file name, a numbering sequence or a repository layout. Add an item with `POST /api/autonomous/pending-items`, MCP `autonomous_pending_item_add`, `datawatch autonomous pending-item-add` or the chat verb `autonomous pending-item-add`. See [Pending items](#pending-items), [howto/automaton-pending-items.md](howto/automaton-pending-items.md) and [plans/2026-10-28-automation-pending-item-member-proposal.md](plans/2026-10-28-automation-pending-item-member-proposal.md).

**Overlap warning** — a note, made when you create an Automaton, that an existing Automaton, a plans-registry row or an unmerged Automaton branch already looks like the new plan. It gives the kind, what it points to, a score from 0 to 1 and a hint for reading it. It is advice only: the create always succeeds, and nothing the warning points at is changed. The check runs in one of three modes (`off`, `shadow` or `on`, default `shadow`); only `on` adds the warnings to the create reply, and `shadow` records them in the audit log instead. See [howto/automaton-search-before-create](howto/automaton-search-before-create.md).

**Failure escalation policy** — the rule for a task that keeps failing. After each failed attempt the next attempt can run on the next worker on a ladder (for example a local model, then a stronger Claude model that reads the earlier failure reports). After a set number of failed attempts (default 3, settable to 4 or more) the task is held for operator review instead of being marked failed. Attempt cost sits alongside it: the per-kind **first-attempt worker** below keeps the cheap kinds' first attempts on a cheaper worker, so a failed task's worst case is cheap starts plus the strong ones rather than up to three full cold starts, and a first-try success is unchanged if you set no pair. It is set once for the whole daemon, and an Automaton or a single task can override it. Each setting resolves on its own, task first, then Automaton, then the daemon default. A failure no other attempt can fix is held on its own before the limit is reached — see **No-retry hold** below. Turn it off to get the old behaviour: retry up to the auto-fix retry count, then mark the task failed, and the no-retry check does not run at all. A model-less `claude-code` step runs on the daemon default model (`sonnet`; `haiku` for a mechanical task kind) rather than the backend's built-in or the account's default (the **explicit model policy** below). While the usage back-off guard holds a step's backend, that step is skipped for the attempt — the escalation never starts a new session on the held backend, and the attempt runs on the task's normally resolved worker instead — with the step, the model and every skip recorded on the attempt record and in the decision log (see [howto/usage-backoff](howto/usage-backoff.md)). See [howto/failure-escalation](howto/failure-escalation.md).

**First-attempt worker (attempt cost)** — the cheap tier in front of worker choice: `autonomous.first_attempt` maps a task kind (`implementation`, `docs`, `note`, `final_gate`) to the backend and model that kind's first attempts run on, so the strong worker only starts once the task has actually failed. `autonomous.first_attempt_escalate_after_failures` (default 1, range 0–20) is the failure count after which the pair stops applying and the normal resolution — including the failure-escalation ladder — takes over; a task, story, PRD or Automaton pin, or a burn-guard route, always beats the pair. Daemon-wide only. Each recorded attempt carries its reason: \"first attempt for &lt;kind&gt; tasks\", \"escalated after N failures\", \"escalation ladder step\", \"operator pinned\" or \"default choice\", shown on the review entry of a held task. One shared validator refuses an unknown kind, a model-less entry for `pi`, `pi-prompt` or `ollama`, and out-of-range numbers, naming the key; a model-less `claude-code` entry is accepted, and an attempt that resolves to no model starts on the daemon default model, `sonnet` (or `haiku` for a mechanical task type), not the backend's built-in default (the **explicit model policy** below). See [howto/failure-escalation](howto/failure-escalation.md) → Choosing the first-attempt worker.

**Retry resume (attempt cost)** — the piece that keeps a failed attempt's retry from costing another full cold start. When a task's attempt failed verification (stalls, start and infra failures never set one up), the worker's session is still open and the attempt's backend can resume — today only `claude-code`, `autonomous.retry_resume_max_context_tokens` is above 0 (0 = fresh every time), and the session's context is at or under that cap (an unknown size passes), the next attempt resumes that session: it spawns nothing, keeps the attempt's worktree, and receives one line — a retry marker plus the same failure summary a fresh retry's prompt would carry, capped at 4,000 characters. A missing send path, or a failed send, falls back to a fresh session. The decision is `decideRetryResume` (`internal/autonomous/retry_resume_decision.go`), wired into the retry path by `tryRetryResume` (`internal/autonomous/retry_resume_flow.go`). Every attempt record after the first carries `resumed` and `resume_reason` (e.g. `resumed previous session`, `resume disabled by setting; starting a fresh attempt`, `context above the limit; starting a fresh attempt`), and a held task's review entry shows those for the last attempt as `last_resumed` / `last_resume_reason`. The failure flow itself — attempt order, `max_failures`, the ladder, the hold and the alert — is unchanged; only the cost of the attempt is lower. See [howto/failure-escalation](howto/failure-escalation.md) → Retry resume: continuing the previous session.

**No-retry hold** — the review hold for a failure that no other attempt can fix, raised while the **failure escalation policy** is on. When the verifier rejects an attempt and any of three conditions holds — the task failed on *no files changed* and writes nothing (read-only, or no planned files); its verification script is missing from the worktree; or the same failure summary, ignoring case and surrounding whitespace, has already failed twice — the failed attempt is recorded carrying the operator-readable reason (`no_retry_reason` on the attempt, `no_retry_reason` on the held-task entry of the status view, `FailureReason` source `no-retry-hold`), the task is parked in `review_needed` at once with no further attempt, and one error-level alert titled *Automaton task needs review (no retry)* is raised, naming the Automaton and the task, the reason, the last worker and the ways to act: change the task so it can be verified (give it files to write, a verification script, or a changeable spec), or cancel it. Its review entry leads with the reason. It is resolved with the same actions as a retry-limit hold — retry (useful only with a changed spec, worker or scope; an unchanged retry re-enters the hold on the same basis), edit spec and retry, change worker, narrow scope, or cancel. With `enabled: false` the check does not run: every failure takes the ordinary retry, then fail path. See [howto/failure-escalation](howto/failure-escalation.md) → No-retry failures.
**First attempt** — a task's first try, before it has failed. Its kind (`implementation`, `docs`, `note` or `final_gate`) decides which worker it runs on, through `autonomous.first_attempt`. The cheap worker for that kind is used only on the first attempt; an empty entry keeps today's choice, and an explicit worker or LLM set on the task, its story, the Automaton or by the burn guard always wins. See [howto/failed-attempt-cost](howto/failed-attempt-cost.md).

**Escalate after failures** — the failed-attempt count after which the first-attempt worker stops applying and the normal choice takes over, including the failure-escalation ladder. It is `autonomous.first_attempt_escalate_after_failures`: default `1`, so the first failure escalates; range `0` to `20`, where `0` behaves like `1`. See [howto/failed-attempt-cost](howto/failed-attempt-cost.md).

**Resume in the same session** — a retry that continues the failed attempt's worker session, with the failure summary as one line, instead of starting a fresh session with the full context. It happens only when the attempt failed verification, the session is still open, the backend can resume (today `claude-code` only), `autonomous.retry_resume_max_context_tokens` is above `0` (default `150000`) and the session's context is at or under that limit. Otherwise the retry starts fresh, and the attempt record says why. See [howto/failed-attempt-cost](howto/failed-attempt-cost.md#retrying-in-the-same-session-or-starting-a-new-one).

**No-retry reason** — the operator-readable reason recorded on a failed attempt when its failure goes to review without another attempt: no files changed for a task that writes nothing, a missing verification script, or the same failure summary failed twice. It is stored as `no_retry_reason` on the attempt and shown on the held task's review entry. Empty on an ordinary failed attempt that is retried. See [howto/failed-attempt-cost](howto/failed-attempt-cost.md#failures-that-go-to-review-at-once).

**Runner evidence** — what the daemon records about a worker's runner when it ends a stalled attempt: the `pi` exit status, the last output lines, the runner's processes, and the compute node and model it ran on. It comes from the live pane, or from the session `output.log` when the pane is already gone. The stall error ends with a summary of it, `(pi exited with status N; node X; model Y; last: ...)`, capped at 300 characters, and the error still counts as a stalled worker. See [howto/autonomous-recovery.md](howto/autonomous-recovery.md#reading-why-a-worker-stalled).

**Attempt record** — the history of one task's failed, stalled or cancelled attempts, kept on the task and never removed. Each entry names the worker and why it was chosen (`backend_reason`), whether the attempt resumed the previous session and why (`resumed`, `resume_reason`), the failure category and summary, what the verifier said, the files changed, any `no_retry_reason`, how long the worker lease requests waited (`queue_wait_seconds`) and the time from verification start to the verdict (`verify_seconds`, counted in the daemon from the end of the wait for the worker, so it does not include the worker's run). The category is derived when the attempt is recorded, first matching rule wins — a category the verifier or executor named is kept; a cancel by the operator is `none`; a refused merge or a failed commit (in the summary or verifier text) is `policy`; a verifier failure is `code_or_test`; a stalled attempt is `hung`; a session that died, a daemon restart, a refused launch or a timeout is `infra`; anything else with a summary or verifier issue is `code_or_test` — and a daemon-start backfill fills it in for older blank entries; it is one pass, idempotent, so a rerun changes nothing, and an attempt with no end reason, no summary and no verifier issue stays blank (the failures chart draws it as `uncategorized`, and `GET /api/cost/failures` counts it in `uncategorized_attempts`). It is read through the Automaton's task review and `datawatch autonomous status`. See [howto/failed-attempt-cost](howto/failed-attempt-cost.md#reading-the-attempt-record).

**Paused by operator** (`pause.start_paused`) — a datawatch-level state, global or per project, that stops new work from being admitted: no new Automaton start, no restart, no new worker task, no lane fill, no planning the engine starts by itself (lane fill's decompose, the planner restart after a daemon restart, the re-plan after a reworded spec, a further planner session of a plan the engine already started, and the planning and running of a child Automaton or scan-fix task, which wait and do not fail) and no merge pass (the stalled-merge retry included; a task that finishes still folds its own branch) for the paused scope. A merge run or deploy run asked for while paused is refused, a deferred deploy is not retried at daemon start, and an `on`-mode gate repair is held. Tasks already running finish, and schedules, the daemon, the Automata manager, tabs, history, dashboards and reports stay up. It is not `autonomous.enabled` (which turns the manager off) and not an Automaton **hold** (which parks one Automaton): one action pauses everything, one action (`resume all`) clears every pause. A pause needs a reason, is saved across a daemon restart, and is written to the audit log with who, why and when. Only the operator can resume: the daemon refuses a resume from a session's credential and from any worker, watcher, supervisor or model. A pause can be sent to federation peers, and each peer reports its own state. A pause is for automation work only: it stops this daemon from placing work, locally, in a cloud queue or on a federated host, and it does not pause the federated host itself; by default it is for this daemon only, and the web app's pause window (button title "Pause automation work") offers each server or all servers when the header server selector is on All. Supervisor sessions and operator sessions are not held. See [`howto/pause-new-work.md`](howto/pause-new-work.md).

**Safeguard alert** (`audit.safeguard_alert`, default on) — an interim alert that tells you when a safeguard is lowered by a caller that is neither the web app nor a session credential: a **pause** resume, an attempt-count force reset, or a force run of Automaton work that already shipped. Today the local API needs no token, so a script, a CLI call or a model that reaches it is indistinguishable from you; the alert names the caller (remote address, user agent, the actor label it claimed), the action, the target and the reason it gave, fires once per action, and is also written to the audit log as `safeguard_action`. The web app is told apart by an `X-Datawatch-Client: pwa` header; it is not a secret, so it can only hide the alert, and every marked call is audited with the marker. A session credential is refused or demoted as before and raises nothing. It is replaced by real caller identities when the authentication design lands. See [`howto/pause-new-work.md`](howto/pause-new-work.md) and [`howto/attempt-cap.md`](howto/attempt-cap.md).

**Usage back-off flag** (`autonomous.usage_backoff`, `autonomous.backoff_max_wait_minutes`) — a hold on one backend that makes new **worker** sessions for it wait, with a `backoff:` reason, until the usage limit lifts. A flag can carry a list of models (**held models**): then only work on those models of the backend waits, its wait reason names them (`backed off for models opus, sonnet until ...`), and work on the other models keeps running; with no list the whole backend is held. It never blocks an interactive session and never touches another backend. The one carve-out in the hold: the review roles (guardrail judges, verifiers, decompose/planning sessions) may still be admitted on the held backend through its **review reserve** below. It is saved to disk, so it survives a daemon restart: at start the daemon reinstalls every flag whose time has not passed and records each one as a **Back-off restored after restart** alert, a boot-resume entry and an audit event, and sessions already running are left alone. See [`howto/automata-capacity.md`](howto/automata-capacity.md#across-a-daemon-restart) and [`howto/usage-backoff.md`](howto/usage-backoff.md#holding-only-certain-models).

**Review reserve** (`autonomous.backoff_reserve_for_review`, `autonomous.backoff_reserve_tokens_per_hour`) — the bounded allowance that lets review work through a usage back-off. While a backend is held, new worker sessions are refused for the whole flag, but the review roles — guardrail judges, verifiers and decompose/planning sessions — may be admitted on it through the reserve: a fixed number of live session slots (`backoff_reserve_for_review`, default `1`; `0` disables the reserve, restoring the old refuse-everyone behavior) plus optionally a rolling one-hour token budget (`backoff_reserve_tokens_per_hour`, default `0` = no budget, enforced only when token usage is reported). A live verifier or planner session on the backend already holds one of the slots, and a request that cannot get a slot waits with the back-off-classified reason `pool llm:<backend> backpressure: back-off, review reserve in use`, resetting with the flag. Every admission and refusal is audited as `autonomous_usage_backoff_reserve_admitted` / `autonomous_usage_backoff_reserve_refused` (backend, role, `slots`, `used_slots`, `tokens_used`, `token_cap`). Both keys are read live at decision time, so a change needs no daemon restart, and they are settable from every config surface — YAML (`autonomous.backoff_reserve_for_review`, `autonomous.backoff_reserve_tokens_per_hour`), REST `PUT /api/config` with those dotted keys, MCP `autonomous_config_set` with `backoff_reserve_for_review` / `backoff_reserve_tokens_per_hour`, CLI `datawatch config set autonomous.backoff_reserve_for_review=…`, comm `configure autonomous.backoff_reserve_for_review=…`, and the two *Usage back-off review reserve* fields in PWA Settings — with the effective values and the reserve's per-backend window usage shown in every `usage-backoff` status. The reserve's rolling token state is in memory and does not survive a restart. See [`howto/usage-backoff.md`](howto/usage-backoff.md#what-a-flag-does-not-hold-the-review-reserve) and [`howto/automata-capacity.md`](howto/automata-capacity.md#the-review-reserve).

**Usage calculator** — the advisor that reads the weekly allowance readings you type and says whether you are within or over budget for the week and what each lane count costs against the rest of it. It follows one of three policies (use it all, pace to the week, run flat out), which the operator chooses. It only advises and never stops or slows a lane. After a weekly reset with no new reading it estimates the new week from the tokens used since the reset, marks the figure as estimated (`advice.estimated`) and the series as stale, and asks for a reading to correct it; the burn guard projects from the same estimate. In the web app it is one card — a verdict sentence over three graphs (**Weekly allowance**, **Pace line**, **Burn per day**) — which is also available as dashboard cards and sits (as those three graphs) on the Automation view's Resources sub-tab; its settings are in **Settings → General → Automata**. See [`howto/usage-calculator.md`](howto/usage-calculator.md).

**Burn guard** — the guard that compares the projected weekly burn of the Claude sessions with the pace line of the usage calculator and, when the projection runs over the line plus a margin, steps down a ladder of admission changes: `lower_inflight` (lowers how many sessions the entry may run at once), `route_entry` (sends the entry's new work to its `route_to` backend when one is set), `route_cheaper_model` (keeps the backend and sends the entry's new work to its cheaper model — Haiku for a Claude entry, otherwise the entry's own cheapest other priced model by its per-model cost rates), `hold_entry` (holds the entry's new work, optionally limited to its `held_models`; work already running continues). It steps back up, one rung per interval, when the projection is back under the line. The default ladder is `lower_inflight`, `route_entry`, `hold_entry`; `route_cheaper_model` is the fourth, opt-in rung and is a no-op for an entry that resolves no cheaper model. The legacy first-version names `route_pi_prompt` and `hold_claude` are still accepted in saved configs and normalize to `route_entry` and `hold_entry`. By default it guards the `claude-code` entry; `usage.burn_guard.entries` guards any number of paid LLM entries, each with its own readings, plan (`flat` weekly percentage or `metered` dollar spend), `period_target`, priced model, ladder, held models, route target, cheaper model and (for `flat` entries) `points_per_usd`. Its projection starts from the newest usage reading, carries the tokens used since it forward with the cache prices scaled by a calibration ratio, and blends the burn measured between the last two readings with the token burn. The guard projection is compared with the readings-only advice projection; `max_projection_gap_points` (default 25, 0 to 100; 0 never compares) sets the largest gap allowed before the guard decides on the lower of the two. It changes admission only and never ends a running session. Its settings are under `usage.burn_guard`, including the per-kind effort ceiling and the Claude retry limit. In the web app the Burn guard card on the usage page is read-only — status, **Run now** and a **Burn guard settings** link that opens **Settings → General → Automata**. `ask_on_change` (false by default) picks how the guard reaches out about a change: alerts only — one alert per change and no question — or, when true, at most one open question per change (a new change withdraws the open question, and questions the previous run left open that the ladder no longer supports are withdrawn at daemon start). Beside the ladder it carries the **pace_line policy** for the weekly and the window usage (see the *Pace line policy* entry below). See [`howto/burn-guard.md`](howto/burn-guard.md).

**Pace line policy** — the burn guard's policy for the weekly and the window usage, beside the ladder of admission changes, set by six keys in the same `usage.burn_guard` block. `mode` picks `off` (decides nothing; a hold in force is released), `shadow` (computes the decision and records it with its evidence without touching admission; the default — an unset mode reads shadow, shadow first, then on) or `on` (holds new worker admission when the current window is at `window_stop_percent` or the week is more than `week_tolerance` points over the pace line, and lifts the hold when the reading drops or the window or the week resets; running work finishes and operator and supervisor sessions are never held). On the week side, `week_tolerance` (default 2, whole number 0 to 10) is how many points the week reading may run over the pace line and still be ok, and `points_to_week_factor` (default 1.3, 1 to 10) counts one point of ledger burn as that many points of the weekly allowance. On the window side, `window_percent_per_point` (default 4, 1 to 25) is how much of the current window one point burned is, and `window_stop_percent` (default 85, 50 to 95) is the percent used at which a window is over; the window anchors — windows ending at 02:00, 07:00 and 12:00, then every 5 hours — are fixed behavior, not a setting. `reading_freshness_minutes` (default 60, whole number 1 to 1440) is how long a reading may be before it is stale and the guard asks for a fresh one. All six keys are read and written on the same surfaces as the other `usage.burn_guard` keys: one invalid value rejects the whole change and nothing is stored, and a saved out-of-range value falls back to its default on load with a warning in the daemon log. Every decision that changes the verdict or the hold is recorded with its evidence in the `pace` block of the burn guard status, in the web app card, on the dashboard usage line, in the chat reply and in `burn_guard_pace.jsonl`. See [`howto/burn-guard.md`](howto/burn-guard.md#the-pace_line-policy-the-weekly-and-window-guard).


**Host load check** — the daemon's guard against an overloaded host. Every minute it reads the 1-minute load average, and once it has stayed above `multiple` (default 1.5) times the core count for `minutes` (default 10) consecutive minutes, it raises one warning alert for that episode naming the busiest process groups (5 by default) and whether each belongs to the worktree or scratch folder of a running or an ended Automaton session — proven by folder evidence, never by name. The episode ends when the load returns under the limit and the next breach alerts again. `autonomous.host_load_alert.mode` picks `alert` (the default: report only), `reap` (also ends processes of **ended** sessions that the session-end reaper's evidence rule proves, by exact pid; a running session is never touched) or `off`. Note that `reap` acts only when the check raises its alert — at the first sustained breach after it was switched on — and the load check's reap writes no record: no audit entry, no `survivors` count, no task decision, so a `session_end_reap` record always refers to the session-end pass only, and a process that ignores the end signal survives it without being counted; verify leftovers by hand. See [howto/host-load-check.md](howto/host-load-check.md), [howto/automata-orphan-reaping.md](howto/automata-orphan-reaping.md) and [operations.md](operations.md) (*Host load alert*).

**Bounded load step (rule)** — the planning rule a task that asks for CPU stress — a busy loop, a load or soak test, any command that pins the cores — must satisfy before it runs: run it niced (`nice -n 19`), cap the workers at a quarter of the cores (`nproc/4`, minimum one), give it a hard timeout, trap `EXIT INT TERM` and clean up its children, and never let it outlive its owning task. The wording is one source, carried verbatim into the built-in `bounded_load_step` standard task (daemon-mode by default, so the story's note task completes without a model session), the rules block appended to the decompose prompt, and a warn-severity `unbounded_load_step` finding in the decompose lint when a spec asks for an unbounded load step. Its role is to keep the session-end reap and the host load check from ever having to finish a task's load after it — a bounded step dies with the task that started it. See [howto/automata-orphan-reaping.md](howto/automata-orphan-reaping.md).

**Model placement** — which compute node a model runs on, when the requested model is not resident on any eligible node. The daemon plans from the node's known state: free memory and free disk (each reduced by the operator-set headroom), the model's size in GiB, and node-level conditions — disabled nodes, maintenance windows, and nodes pinned to another model (see [`howto/compute-nodes.md`](howto/compute-nodes.md)) are never silently overridden. What happens when no node has the model is the *placement policy*. Settings are the `compute_placement.*` block (`missing_model`, `mem_headroom_pct`, `disk_headroom_gb`, `ask_timeout_minutes`, `assume_model_size_gb`); see the [operations runbook](operations.md) (*Model placement*) and [`howto/compute-placement-ask.md`](howto/compute-placement-ask.md).

**Placement policy** — `compute_placement.missing_model`: `ask` (default), `auto_load` or `refuse`. What the daemon does when the routed node does not have the requested model. `ask` records a *pending placement ask* and waits for the operator to approve (the model is then pulled, if the node does not have it, and loaded) or deny (the placement is refused); an unanswered ask expires after `compute_placement.ask_timeout_minutes` (default 30; `0` means no timeout), and expiry is a refusal. `auto_load` loads the model without asking. `refuse` never loads and the refusal is audited. Every `compute_placement.*` key is read at decision time, so a change from any surface (YAML, `datawatch compute placements config-set`, PWA Settings → General, `PUT /api/config`, the comm channel `configure`, MCP `config_set`) applies at the next placement decision without a restart.

**Pending placement ask** — the entry the daemon records when the placement policy is `ask` and no eligible node has the model: the ask id, the node, the model, the reason, the assumed model size in GB (`0` = unknown), the requested time and the expiry. The answer is one-shot: an approved ask pulls and loads the model on the node, a denied ask refuses the placement, and an unanswered ask expires when `ask_timeout_minutes` passes — `decided_by` is recorded as `operator` or `timeout` respectively. A closed ask (approved, denied or expired) cannot be re-decided: a second answer attempt is refused (HTTP 409). The queue is pending asks only: read it with `GET /api/compute/placements` (fields `asks` and a `pending` count), MCP `compute_placement_list`, `datawatch compute placements list`, the comm channel `compute placement list`, or the **Compute placements** card in PWA Settings → General. Answer with `POST /api/compute/placements/<id>/approve|deny`, MCP `compute_placement_answer` (args `id`, `decision`), `datawatch compute placements approve|deny <id>`, or the comm channel `compute placement approve|deny <id>`. The capability gate is `compute:list` for the list and `compute:write` for the answer. Every step of its life is audited: `placement_asked`, `placement_approved`, `placement_denied`, `placement_loaded`, `placement_refused` (CEF 88–92; see [`howto/audit-logging.md`](howto/audit-logging.md)).

**Placement refusal** — the terminal outcome where the wanted model does not get loaded: a deny, an expiry, the `refuse` policy, or a load offered to no usable node. It is recorded, not silent — the deny and expiry live on the ask record (one each, one-shot), and the refusal event is the `placement_refused` audit entry naming the node, the model, the policy in force and the reason text (CEF 69).

**Timeline chart** — the read-only, Gantt-style interval chart of the Automata, across every project the daemon runs them in. One project at a time, each with its Automata, each with its stages, and each task (a *member*) as a bar with its start and end (`end` set only when it finished — an open bar ends at the server's `today`), its blocked and waiting intervals each with the reason, and the Automaton's end: its finished date, or the **projected finish** paced from its finished members (shown as `no projection — <reason>` when the run has not enough finished members — or no member start recorded at all — to pace from — never as a date). `counts` totals what the chart shows, including the blocked members. It is a pure read — no configuration, no state, nothing stored when it is read — and every surface decodes the exact same object: REST `GET /api/autonomous/timeline` (query parameters `project`, `automaton` (id or title), `stage` (id or title), `kind` (`feature`, `bug`, `other`) or `tag`, all optional — an unknown value comes back as an empty chart, not an error), MCP `autonomous_timeline`, CLI `datawatch autonomous timeline` (text bars: `#` work, `X` blocked, `~` waiting, `T` today, `P` projected finish, every bar scaled to the same chart span, day precision only; `--json` for the raw body), and the `timeline` comm channel command (the short per-Automaton summary). It is distinct from the dashboard's 6h Gantt card, which draws the last six hours of session activity from hook events. In the web UI the *Automaton Timeline* dashboard card (all projects, with the five filters) and the *Timeline* tab of one Automaton's detail view render the same object. See [howto/automata-timeline.md](howto/automata-timeline.md).

**review-needed (task status)** — a task held after its failed attempts reached the escalation limit. The status is `review_needed` in the API and reads "Held for review" in the web UI. It is not a failure and not finished: the task keeps its attempts and a report, the Automaton shows as blocked, and one error alert is raised. The operator retries it (optionally with a changed spec, worker or file list), cancels it, or accepts its result, from the CLI, the REST API, MCP or the comm channel (the web UI has retry and cancel, no accept button yet). Accept is allowed only when the task's last worker session left a completion marker in its recorded output and the daemon finds a task commit (a pre-task snapshot commit does not count; a read-only task needs none, but needs a report path or a verification script), every file the task names, and a passing run of its own verification script; otherwise it is refused, naming what is missing, and the task stays held. There is no override. See [howto/failure-escalation.md](howto/failure-escalation.md#what-accept-checks). **Operator review:** add an Accept button to the web UI if you want parity.

**Operator console** — the dashboard's one-glance "what needs my attention right now" summary: five sections (`questions_waiting`, `held_blocked`, `lanes`, `usage`, `ci_release`), each reporting a count, a one-line summary and the PWA anchor where the operator acts on it. It is a read roll-up of the question channel, the held/blocked triage states and uncommitted-work holds, lane fill and the capacity queue, the burn guard and usage projection, and the repo CI watch; it never writes anything. The **CI & release** section carries a `release` object while it is available: the running and latest release, the ship policy, the watch mode and its Settings link when the watch is off, and up to five failing or hung runs (its `count` is the number of CI runs under watch). A section switched off, or whose subsystem is not running, reports `available: false` with a reason instead of failing the whole read. Switches: `dashboard.console.enabled` and the five `dashboard.console.show_*` keys, each defaulting to true while absent. Read it on REST (`GET /api/dashboard/console`), MCP (`dashboard_console_get`), CLI (`datawatch dashboard console`, or `--json` for the raw body), the chat verb `dashboard console`, or the five console cards on the PWA Dashboard view. See [howto/dashboard-operator-console.md](howto/dashboard-operator-console.md).

**Plan summary** — the dashboard's read of one plan (an Automation) as its stages in order: `bugs`, `features`, `final`, each with total, done, running, blocked and a percent. Maintenance work is a separate count, never a stage. A blocked stage is named with a plain-language reason. It also carries the shared lane usage and the plan usage in points and tokens (never dollars). Read it on REST (`GET /api/dashboard/arc`, optional `?arc=`), MCP (`dashboard_arc_get`), CLI (`datawatch dashboard arc`, or `--json`), the chat verb `dashboard arc`, or the **Plan** card on the PWA Dashboard view (opt-in: Edit → Add Card → Plan; it refreshes about every 30 seconds and keeps the last data if a refresh fails). A part the daemon cannot read reports `available: false` with a reason. `dashboard.console.enabled` governs it; it has no setting of its own. See [howto/dashboard-plan-view.md](howto/dashboard-arc-view.md) and [howto/dashboard-operator-console.md](howto/dashboard-operator-console.md#plan-summary).

**Console card** — one of the five dashboard cards the operator console adds to the Dashboard view (Questions waiting, Held & blocked, Lanes, Usage, CI & release). A console card opens the PWA anchor of its console section; it carries a help link to the operator console how-to, and it disappears from the board and the Add-Card picker when `dashboard.console.enabled` is false (a stored layout is rewritten once to add the cards back when the console is switched on again). The console cards are separate from the feature cards (Network, Lane pulse, Gantt, …); see *Inactive card*.

**Console card** — one of the five dashboard cards the operator console adds to the Dashboard view (Questions waiting, Held & blocked, Lanes, Usage, CI & release). A console card opens the PWA anchor of its console section; it carries a help link to the operator console how-to, and it disappears from the board and the Add-Card picker when `dashboard.console.enabled` is false (a stored layout is rewritten once to add the cards back when the console is switched on again). The console cards are separate from the other board cards (EKG, constellation, Gantt, …); see *Inactive card*.

**Inactive card** — one of the dashboard's five feature-gated cards (Integration status, Guardrails, Smoke Run, Memory Scopes, Search Usage) that currently has no live data to draw from (no schedules, no guardrail verdicts, no smoke runs, no memory entries, no search providers). While `dashboard.hide_inactive_cards` is true (the default), inactive cards stay out of the grid and out of the Add-Card picker so the board only shows what is live; set the key to false to keep them visible and pin your layout. The other board cards — the console cards and the default-layout cards such as Multi-EKG, 6h Gantt, 30-Day Activity and the *Automaton Timeline* — are not gated by the key: they stay in place and show an empty state instead.

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

**Skills coverage check** — `scripts/skills-coverage-check.sh`, a shell check that fails when `docs/skills/inventory.yaml` (every MCP tool, how-to, route, command and setting), `docs/skills/coverage-matrix.yaml` (which skills cover each entry) or the skill sources drift from the code. A skill named by a matrix row must exist in `docs/skills/staging/` or in `docs/skills/registry-skills.txt`, the list of published community skill names kept by hand. Its failure modes are tested by `tests/ci/test-skills-coverage-check.sh`. See [howto/skills-sync](howto/skills-sync.md).

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

**Scan fix loop** (`autonomous.scan.fix_loop_enabled`,
`autonomous.scan.fix_loop_max_retries`) — the option that turns a failing
scan into an automated fix attempt. Both keys are off/zero by default
(`fix_loop_enabled: false`, `fix_loop_max_retries: 0`): with the loop off,
a failing scan is just a failing scan. When it is on, a `scan/fixloop` run
scans the Automaton first, and findings at or above
`autonomous.scan.fail_on_severity` drive the fix cycle: open one corrective
task for the findings (a child Automaton whose
spec is built from them, linked to the scanned Automaton, starting from its
setup), execute it, re-scan — repeating for up to
`fix_loop_max_retries` fix-and-rescan cycles (the initial scan is not one of
them, and the run stops early as soon as a re-scan passes). When the budget
is spent and the scan still fails, the scanned Automaton is put on hold
with a plain-English reason that names the remaining findings;
`fix_loop_max_retries: 0` means "no fix attempts" — the failing scan is
held immediately. The loop never overrides an operator gate: if a plan gate
(coverage, plan review, hold) refuses the corrective task, the refusal is
recorded on the attempt. Its trail (every opened fix Automaton and the
final hold) is in the scanned Automaton's decision log under actor
`scan_fix_loop`, and each path leaves an audit event (`scan_fixloop`,
`scan_fixloop_disabled` when the disabled loop degenerates to a plain
scan, `scan_fix` for the single fix task). Not the same as
`autonomous.infra_retry` (that retries tasks whose *machinery* failed,
free) — the fix loop reacts to the *work* failing its quality scan and pays
a budget. Set the two keys from YAML, the PWA (Settings → Automata, *Guardrail Library*
card: the *Auto-fix loop* toggle plus *Fix loop max retries*; each row saves
itself the moment it changes), `PUT /api/autonomous/scan/config` (partial body
OK), `datawatch autonomous scan-config-set fix_loop_enabled=true
fix_loop_max_retries=3`, the `autonomous_scan_config_set` MCP tool, or the
comm channel's autonomous verbs, and they apply to scan runs from that moment
on; a config-file change needs a daemon restart to take effect. Run the loop
with `POST /api/autonomous/prds/{id}/scan/fixloop`,
`datawatch autonomous prd-scan-fixloop <id>`, `autonomous_prd_scan_fixloop`,
the comm verb `autonomous scan-fixloop <prd-id>` (aliases `scan_fixloop`,
`scan_fix_loop`), or the *Run Fix Loop* button on the Automaton detail's Scan tab; just
one corrective task without the loop comes from `.../scan/fix`,
`datawatch autonomous prd-scan-fix <id>`, `autonomous_prd_scan_fix`, or
`autonomous scan-fix <prd-id>`. See
[howto/scan-fix-loop](howto/scan-fix-loop.md).

**Gate repair** (`autonomous.gate_repair.mode`) — the engine behind a failed final gate script: after a gate fails it classifies the failure (`later_story_script`, `docs_mirror_stale`, `allowed_list_rejects`, `tracker_id_css_hex`, `pipe_sigpipe`, `restart_killed` or `unknown`), and in `on` mode applies the safe repair for the class at most once per (task, class) — the budget persists across restarts — then resets the failed task and its dependents so the gate runs again. In `report` (the default) it classifies and records what it would do, changing nothing and raising one "Gate repair found" warning per failed gate; in `on`, a spent budget, an `unknown` class or a refused repair is one held operator question (option ids `retry` — the default — or `cancel`) plus a "Gate repair held" warning, never a retry loop and never a guess, and a failed repair spends nothing and raises no question; in `off` it does nothing. A gate repair never merges, ships, deletes, ends or signals a session, changes a model or backend, or touches a security workflow, and its script edits are limited to the task's own `.datawatch/verify` folder (the docs-mirror repair is the one step outside it: `make sync-docs`, which only refreshes the gitignored mirror). The mode is set through the standard config set paths (`autonomous.gate_repair.mode`), and the engine is read as `GET /api/autonomous/gate-repair`, the `autonomous_gate_repair_status` MCP tool, `datawatch autonomous gate-repair status`, or the chat verb `autonomous gate-repair status` — each record says what was found, what it would do or did, and how to undo it. The engine can also be run on demand in the mode in effect: one pass over every failed gate, or one failed gate given both ids, as `POST /api/autonomous/gate-repair/run`, the `autonomous_gate_repair_run` MCP tool, `datawatch autonomous gate-repair run` or the chat verb `autonomous gate-repair run` — a run spends the same budget, writes the same records and changes nothing else. See [howto/automaton-gate-repair.md](howto/automaton-gate-repair.md).

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
then releases the local worktree: it is removed only when `cleanup_mode`
is `on` and it holds no work of its own (v9.0.20). A
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

<a id="event-kind-registry-entry"></a>
**Event kind registry** — the one in-daemon list of every lifecycle event
kind a subscription or notify set may name, each with a one-line
description and a group (`automaton`, `task`, `review`, `capacity`,
`budget`). The subscription validator and the notify-event validator derive
from it, and the web subscription form loads its kind checkboxes from it. The
Automaton notify-event pickers still carry a built-in copy of the list. A
plugin may register further kinds and sink types at start-up; they validate
like the built-in ones. Full table:
[api/autonomous.md](api/autonomous.md#lifecycle-event-kinds).
<a id="event-kind-listing"></a>
**Event kind listing** — the read-only view of the event kind registry, so
you can see what a subscription can name without reading a table. Each kind
shows its group, its one-line description and whether it is in the default set
that wakes the supervisor. REST `GET /api/autonomous/event-kinds`, the
`autonomous_event_kinds` MCP tool, `datawatch autonomous event-kinds
[--group G] [--json]`, `autonomous event-kinds [group=<g>]` on the comm channel
and the kind checkboxes of the web subscription form all show the same list,
including kinds a plugin registered. It changes nothing.
<a id="subscription-preview"></a>
**Subscription preview** — `POST /api/autonomous/subscriptions/preview`, a
read-only count of what a filter (`kinds`, `project`, `owner`) matches in the
last 24 hours, with the newest 20 matches. It stores nothing and does not apply
the notify set. Its `kinds` are the Events log's own kinds (`watch`, `decision`,
`retry`, `check`, `cleanup`, `alert`, `error`), counted from the Events log, or
any lifecycle kind the picker lists (such as `task.failed`), counted from the
lifecycle events this daemon recorded since it started; `lifecycle_since` in the
answer says when that recording began.
The web subscription form's "Would have matched in the last day" line is its
only caller. See [howto/automaton-lifecycle-subscriptions.md](howto/automaton-lifecycle-subscriptions.md#find-the-kinds-and-preview-a-filter).
<a id="mcp-resource-subscription"></a>
**MCP resource subscription** — a push path for MCP clients that stores
nothing. With `mcp.resource_subscriptions` on (off by default; restart to
apply), a client calls `resources/subscribe` on
`datawatch://events/lifecycle?kinds=…&level=…` and receives
`notifications/resources/updated` for each matching lifecycle event. It uses
the same filter rules, kind list and notify-set gate as a stored
[lifecycle event subscription](#lifecycle-event-subscription), lives only as
long as the MCP session (at most 32 per session) and never changes how stored
subscriptions deliver. See [mcp.md](mcp.md#resource-subscriptions).

**Supervisor event kinds** — the opt-in lifecycle event kinds that let an
event-driven supervisor wake on a state instead of polling for it. The
*condition* kinds `automaton.stuck_running`, `automaton.merge_ready` and
`automaton.tasks_done_or_cancelled` are edge-triggered: sent once when the
condition starts to hold and again only after it clears.
`task.capacity_wait_too_long` is sent once per waiting episode when a task
has waited for a lane longer than half of
`autonomous.capacity_wait_timeout_seconds`. `burn_guard.rung_changed` is
sent when the burn guard's rung changes and carries the old and new rung;
it belongs to no Automaton, so only the global notify tier can allow it.
`judge.verdict_block` / `judge.verdict_warn` / `judge.verdict_pass` are sent
once when a judge verdict with that outcome is recorded, and
`task.final_gate_failed` once when a final gate task fails. None of them
changes a status, verdict or gate outcome.

<a id="lifecycle-event-subscription"></a>
**Lifecycle event subscription** — a persisted rule that maps an Automaton
lifecycle event (PRD/task status changes, `task.session_started` /
`task.session_cleared`, `prd.updated`, the question kinds, the opt-in
warning kinds `scope_warning` and `guardrail_warn`, and the opt-in
*supervisor event kinds* above) to a delivery sink: a comm router,
a datawatch session (bridge `POST /send` preferred, `SendInput` fallback
tagged `lifecycle-sub`), a push topic, the in-app alert store, or a named
**pending events queue** (`mcp_queue` sink) that a client polls. Options:
`one_shot` (removed after first *successful* delivery — one retry, then
log + drop), `expires_at` TTL (pruned by the engine's janitor),
`coalesce` quiet-window batching for human-facing sinks. Delivery: each
subscription consumes from its own bounded in-memory queue on its own
goroutine; emitting an event never waits on any subscriber, and a
subscriber whose queue is full misses events of its own (per-subscriber
dropped counter, rate-limited log warning) while every other subscriber
keeps receiving — one slow sink can never stall the Automaton or another
subscriber. Manageable from
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

Two on-by-default filters on the plan requirement coverage gate, tuned so a plan is not made to carry findings for the work it plans to do. **Skip files an Automaton plan creates** (`autonomous.plan_coverage_suppress_created_files`, on when absent) — a file that a task's title or spec says it creates, adds, writes or generates, or that is new in the plan's own file list, is not reported as a missing file, because it is the plan's own output; the allowance stops where the parent folder does not exist and no task creates it, and a path in a folder nothing builds is still reported. The matching is over names exactly as written (a name is one piece, up to the first space), so a path written severed or truncated in the spec is a different name from the full one and is not rescued by the plan's file list — the **full-file-path rule** in the planning prompts prevents that upstream. **Count only scoped sentences** (`autonomous.plan_coverage_scoped_requirements`, on when absent) — only the sentences in the sections named by `autonomous.plan_coverage_requirement_sections` (default: the scope and constraint sections) count as requirements, a path or config key counts only through the sentence that holds it — no more loose file names, backend names or quoted phrases counted on their own — and a sentence shorter than `autonomous.plan_coverage_min_requirement_chars` characters (default 30, where 0 reads the default) does not count; a spec that carries none of the named sections falls back to the whole spec. The plan's coverage panel, `GET /api/autonomous/prds/{id}/plan_coverage`, the `autonomous_prd_plan_coverage_get` MCP tool, `datawatch autonomous prd-coverage <id>` and the chat verb `autonomous plan-coverage <id>` all show how many findings each filter withheld and the filters the report was computed under; the withheld count is not part of the acknowledgement fingerprint, so an acknowledgement of a genuine finding stays valid when only the filters change. Turning one off reverts that noise class on the next decompose. See [operations — Plan coverage filters](operations.md#plan-coverage-filters).

### Decompose lint

**Decompose lint** — the quality check the decomposer runs over each fresh decomposition, at decompose time, before the plan is stored: a rule pack over the story-and-task graph, with the decomposer given the `block` findings fed back as guidance when the mode says so. A decompose is never failed by it: findings are attached to the stored plan (recorded, never fatal), and `off` is a complete no-op (no templates, no pass, no record). **Mode** (`autonomous.decompose_lint.mode`, case-insensitive; empty/unset is `enforce`; anything else is a config error): **`off`** — nothing runs, nothing is recorded; **`shadow`** — one lint pass, its record stored on the Automaton, the plan never retried; **`enforce`** (default) — while `block` findings remain and the budget is unspent, the decomposer is re-run with the block findings fed back, and each retry is re-parsed, re-templated and re-linted. **Retry budget** (`autonomous.decompose_lint.max_retries`, default `2`; `0` is a valid explicit value — enforce then never auto-retries; `5` is the cap) counts auto-retries on top of one initial pass, so `attempts` on a stored record is `1` up to `1` + budget.

**Planning failure** — what an Automaton records when its planning attempt gives up: the reason, the category, the source rule, the exact phrases that tripped it (at most 20), whether it is retryable, the retry counts per category with the budget of that category, and when it happened. A non-retryable planning failure raises one warning alert, "Automaton could not be planned: <title>", once per recorded failure (a restart does not raise it again), and a retryable one raises nothing until its budget is spent. A decompose whose output is not valid JSON, is empty, or was too large to parse is recorded with source `decompose-parse` and category `invalid-response` — terminal, never infra-retried — and the raw output is kept as `decompose-output.json.failed` in the PRD's scratch directory. A plan that is valid JSON but has a task `verification` value that is a number, boolean or array, or a malformed `verification` object, is instead a `content`-category record, rule `decompose-parse`: it is retried once within the content budget with the parse error — it names the offending field and task — carried back to the planner as the next attempt's guidance, never an infra retry, and a spent budget leaves a non-retryable `content` record on the same alert and question path; a `verification` value that is a plain string is accepted as an unverified result (`ok` false, the string as its summary), and a missing value or `null` means "no verification result". See [howto/automaton-decompose-lint.md](howto/automaton-decompose-lint.md#6a-planning-failures-what-you-see-what-the-daemon-fixes-what-it-asks), [howto/automata-planning-failures.md](howto/automata-planning-failures.md) and [howto/retry-policy.md](howto/retry-policy.md).

**Decompose output abort (the 2026-11-18 SIGABRT fix)** — the guard that keeps a bad decompose output from aborting the daemon: the raw output is refused above 8 MiB before parsing, and the parse-plus-lint step (shared by the sync and streaming decompose paths and the re-attached planner ingest) runs under a panic guard, so a panic or an unparseable output is returned as the named failure `decompose output unparseable: <reason>` (length plus a 200-byte head) and recorded as a terminal planning failure — source `decompose-parse`, category `invalid-response` — rather than an infra retry. The fix also bounds the decompose-lint help probe that caused the abort itself: it runs `prefix --help` only (never `-h`, never the bare prefix), under a 5-second timeout, in its own process group, with its output capped at 64 KiB. If the daemon still dies, the runtime's fatal crash dump is written to `<data>/daemon-crash.log` (each start appends a `=== datawatch vX started pid=… at … ===` header) in addition to stderr — the journal that reads stderr keeps its copy — and, when the host keeps cores, `coredumpctl list` / `coredumpctl info <pid>` hold the SIGABRT core. See [howto/decompose-async.md](howto/decompose-async.md), [howto/automaton-decompose-lint.md](howto/automaton-decompose-lint.md) and [operations.md](operations.md) ("Decompose output and daemon aborts").

**Spec check** — the spec lint run for you when you create or revise an Automaton: the reply carries a `spec_check` list of findings (rule, exact phrase, severity, whether the daemon can correct it, suggested rewording), shown on REST, MCP, the CLI, chat and the web form. It is a warning only and never blocks the create or the revision. See [howto/automata-planning-failures.md](howto/automata-planning-failures.md).

**Spec lint** — the deterministic, model-free check of a spec's own text for the phrases the decompose checks reject: a fixed release number (`release-version-fixed`), a tracker id (`names_tracker_id`), a path to a system binary (`system-binary-path`), a private address (`private-address`) and a worktree path (`worktree-path`). Each finding carries the rule, the exact phrase, a severity (`warning` or `error`), whether the daemon can correct it and a suggested rewording. The first three are warnings the daemon can reword; the last two are errors only you can fix.

**Planning failure line** — the plain-text `planning failed: <id>: <reason>` line that `datawatch autonomous prd-list` prints for each Automaton with a recorded planning failure, and the **Planning failed** section that `prd-get` prints. Both go to standard error, never standard output, so the command's standard output is one JSON document that a script can parse as it is. See [howto/automata-planning-failures.md](howto/automata-planning-failures.md).

**Planning failure question** — the one question raised for a non-retryable planning failure when the daemon can reword every spec lint finding: "Fix the wording and plan again" rewrites the spec with the suggestions, applies it as a revision and plans again; "Leave it" (the default) changes nothing. It is asked by `planning` on the ordinary question store, raised once per failure, and not raised again while an open question carries the same Automaton, rule and phrases. A spec with a finding the daemon cannot reword gets the alert and no question. See [howto/automata-planning-failures.md](howto/automata-planning-failures.md).

**Release number self-correction** — when `release-version-fixed` is the only content failure of a decomposed plan and every other plan check passes on the corrected plan, the daemon rewrites each fixed three-part number that is the Automaton's own release version in release stories and tasks (never a Python, Go or library version, a pin, a path or an image tag) to "the release" (when "version" or "release" introduced it) or "the next patch above main", instead of failing the plan. Each rewording is one `auto_correct` line on the Automaton's decision log, `old phrase -> new phrase` with its story and task. The version itself is taken at merge time as the next patch above main.

**Forbidden-pattern list** — the fixed list the planner's prompt always carries, after the decompose guidance block: a fixed release number, a tracker id, a real system path, a private address and a worktree path, each with one correct alternative and the rule name the spec lint and the checks report. It is not configurable and `autonomous.decompose_guidance` does not replace it.

**The built-in rule pack** — the floor every run starts from: thirty rules, twenty-four `block` (`edits_version_variable`, `runs_hosting_cli`, `starts_daemon_outside_sandbox`, `reads_operator_data_or_config_folders`, `read_only_but_writes_files`, `read_only_unverifiable`, `read_only_task_writes_file`, `spec_path_outside_allowed_dirs`, `verify_script_task_files_only`, `verify_script_docs_without_sync`, `artefact_files_allowed`, `final_gate_artefact_allowed`, `script_invokes_known_flag`, `check_matches_spec_text`, `verify_script_rejects_own_files`, `verify_script_needs_later_task_output`, `verify_script_sh_for_bash`, `verify_script_pipe_grep_q`, `verify_script_hex_pattern`, `final_gate_without_script`, `verify_script_artefact_files`, `verify_script_final_gate_rerun`, `verify_script_unknown_flag`, `verify_script_text_not_in_spec`) then six `warn` (`no_verification_script`, `unexecutable_verification_script`, `names_tracker_id`, `version_number_in_spec`, `fixed_calendar_date_in_test`, `unbounded_load_step`); the verification-script rules name the rule id in the finding, and the ones that read a script name its path too (the five script-content rules `verify_script_rejects_own_files`, `verify_script_needs_later_task_output`, `verify_script_sh_for_bash`, `verify_script_pipe_grep_q` and `verify_script_hex_pattern` also quote the offending line) — see [howto/automaton-verification-scripts.md](howto/automaton-verification-scripts.md), with deliberately generic regexes that never name a particular project or home folder. The operator's `autonomous.decompose_lint.rules` is the ceiling over it: the same id replaces that built-in, a new id appends; a bare `{id, disabled: true}` entry or an id in `disabled_rules` removes a rule. A rule whose regex fails to compile is skipped with a reported error; the rest still apply.
**Full-file-path rule** — the fixed `Rules` bullet both planning prompts carry beside the forbidden-pattern list: the planner writes **full file paths verbatim** — every file named anywhere in a task title, spec or files list is written exactly as its real repo-relative path, the full path in one piece, no space inside the name, no ellipsis, no abbreviation, no truncation, and no display form of a path (a severed name such as `docs/plans/2026-11-16-re`, or a report path written as prose, is a defect; a long hyphenated name is spelled out completely). It is not configurable and `autonomous.decompose_guidance` does not replace it. The reason is the name-based structural check of the plan coverage gate: file-name tokens are extracted up to the first space, a fragment is a different name from the full path, and a name that exists neither in the tree nor in an earlier task's `files_generated` is reported as a missing file. It is a prompt rule, not a decompose-lint rule: no rule id fires on a severed name — the reporting gate is the backstop. See [the decompose-lint how-to](howto/automaton-decompose-lint.md#the-full-file-path-rule-is-always-enforced-too).

**The built-in rule pack** — the floor every run starts from: thirteen rules, nine `block` (`edits_version_variable`, `runs_hosting_cli`, `starts_daemon_outside_sandbox`, `reads_operator_data_or_config_folders`, `read_only_but_writes_files`, `read_only_unverifiable`, `verify_script_task_files_only`, `verify_script_docs_without_sync`, `final_gate_without_script`) then four `warn` (`no_verification_script`, `unexecutable_verification_script`, `names_tracker_id`, `unbounded_load_step`); the three verification-script rules name the script and the rule id in the finding — see [howto/automaton-verification-scripts.md](howto/automaton-verification-scripts.md), with deliberately generic regexes that never name a particular project or home folder. The operator's `autonomous.decompose_lint.rules` is the ceiling over it: the same id replaces that built-in, a new id appends; a bare `{id, disabled: true}` entry or an id in `disabled_rules` removes a rule. A rule whose regex fails to compile is skipped with a reported error; the rest still apply.

**Automaton template** — a reusable Automaton definition held in the template store: a complete spec (type, stories, tasks, backend, effort, model, skills) that an operator starts a new Automaton from. Automata → **Templates** tab (labelled "Automaton templates" in the current UI) → **Automaton templates** sub-tab (the default) lists them; when the store is empty the sub-tab says what an Automaton template is and how to add one. Read on REST at `GET /api/autonomous/templates`, on MCP with `autonomous_template_list`, and on the CLI with `datawatch autonomous templates` and `datawatch autonomous template-get <id>`. It is not a standard task.

**Standard task** (formerly "task template"; the config keys still say `task_templates`) — the built-in task definitions the decomposer appends to a story when the plan left them out: `changelog_entry`, `parity_issue_draft`, `docs_set`, `bounded_load_step`, `final_gate`, `version_bump_left_to_merge`, idempotently (a task an earlier apply already added is detected by its `template:` marker and never re-added), run in `shadow` and `enforce` (`off` applies none), each in its own mode (`llm`, `daemon` or `off`; see **Template mode**). The built-in pack is the floor; `autonomous.task_templates.templates` overlays it by id, and `autonomous.task_templates.projects` holds per-project `enabled` overrides. The global gate is `autonomous.task_templates.enabled` (default `true`). They are read as a list on `GET /api/autonomous/standard-tasks[?project=…]`, the `autonomous_standard_tasks` MCP tool, `datawatch autonomous standard-tasks [--project …] [--json]` and the chat word `standard tasks`; the Automata page's Templates tab (labelled "Automaton templates" in the current UI) shows them under the **Standard tasks** sub-tab — a read-only table (title, kind, summary, when offered, on/off, mode), with an explanatory empty state when there is nothing to list and stacked labelled rows at phone width. The list's `runner` field names what actually carries each task out — `session`, `script gate`, `daemon` or `none` (see **Standard task runner**). A merge-only Automaton (one whose spec only lands, merges or rebases existing branches) gets no `docs_set` task, and the plan shows the skip; see **Nothing to document**. A standard task is **not** an Automaton template: the decomposer appends it to a story, while an Automaton template is a whole reusable Automaton definition (see the **Automaton template** entry above, and the **Templates** tab). Detail: [howto/automaton-decompose-lint.md](howto/automaton-decompose-lint.md) §5 and [howto/automata-standard-tasks.md](howto/automata-standard-tasks.md).

**Nothing to document** — the outcome of a docs set task whose attempts end with an empty diff while every other task of its story completed: it is finished as nothing to document, not held after three tries, with the verifier summary recorded on the task as a note. A merge-only Automaton never gets a docs set task, so it never reaches this outcome. Any other docs set failure is still held for operator review. See [howto/automata-standard-tasks.md](howto/automata-standard-tasks.md#merge-only-automata-and-the-nothing-to-document-outcome).

**Merge-only Automaton** — an Automaton whose spec only lands work that already exists in a finished branch (merging, rebasing, resolving conflicts, landing a finished branch) and adds no new work. Detected from the spec text: at least one merge signal, and no work verb (add, implement, create, write, fix, build). A merge-only Automaton does not receive the **docs set** standard task (`docs_set`); every other standard task, including `final_gate`, is still applied (standard tasks are applied only when decompose lint is `shadow` or `enforce`). An ambiguous spec is treated as not merge-only, so the docs task stays. The match is on words, so a spec that says `landing` with no work verb (for example "update the landing page") is skipped too; operators should review such a spec. The decompose log line `docs set task skipped: merge-only Automaton` shows each skip. Detail: [howto/automata-standard-tasks.md](howto/automata-standard-tasks.md#when-docs_set-is-skipped-merge-only-automata).

**Standard task configuration** (the settings behind the standard tasks, formerly "task templates") — reusable task definitions (a changelog entry, a final gate, …) the decomposer appends to a story when the plan left them out, idempotently (a task an earlier apply already added is detected by its marker and never re-added), in `shadow` and `enforce` (`off` applies none). The built-in pack (changelog, parity-issue draft, docs set, final gate, version note, bounded load step) is the floor; `autonomous.task_templates.templates` overlays it by id, and `autonomous.task_templates.projects` holds per-project `enabled` overrides (there is no per-project mode). The global gate is `autonomous.task_templates.enabled` (default `true`). Each template runs in one of three modes — `llm` (a session), `daemon` (the daemon performs it, no session) or `off` (not appended) — set per template in the **Template mode** map; the four note and changelog templates default to `daemon`, `docs_set` and `final_gate` to `llm`. See **Daemon task mode** and **Script gate**. A template's `verification_script` names one repo-relative script file, never a command string — a value that looks like a command (whitespace, shell operators, an absolute path, a parent reference or the project root itself) is refused at config write with an error naming the template id, and a template that still carries one is skipped when the templates are applied, so it never becomes a task the verifier cannot start. There is one final gate per Automaton, at the end (the last task of the last story), and it always carries a verification script or an exemption reason. The built-in final gate follows that: it carries no verification script of its own and is read-only — the daemon's [script gate](howto/automaton-decompose-lint.md#the-script-gate-the-final-gate-no-longer-spends-a-session) now performs it (a script it names under a valid path, else the project's `policy project_gate_command` or the global `autonomous.project_gate_command`, else every script the other tasks of every story of the Automaton declare (standard-task tasks and other final gates skipped), in order until the first failure — all under `autonomous.script_gate_timeout_seconds`), and its verdict and evidence land in the report under `.datawatch/reports/`; a gate the daemon could not run at all is a **failed task with the reason named**, not an unclosed story; the version note is a note the daemon records as the story's decision in its default `daemon` mode, not an edit of the tree.

**Template mode** (also called the standard task mode; `autonomous.task_templates.mode`) — the global per-standard-task setting (a map of template id → mode) that decides who runs the task when it is appended: **`llm`** — a worker session runs it as a normal task, verifier included (default for `docs_set`; also the default for `final_gate`, but the daemon's **script gate** performs that one — resolves its declared checks, runs them under `autonomous.script_gate_timeout_seconds`, and completes or fails the task with the report under `.datawatch/reports/`, so no session, verifier or retry loop; its allowed values are `llm` or `off`, `daemon` refused on write, and `off` means the Automaton has no closing gate at all); **`daemon`** — the daemon performs the task itself and completes it without a model session, a verifier or a retry loop: `changelog_entry` and `parity_issue_draft` are written as the same generated artefact (the write is skipped, with the reason noted, if the file was edited or the kind is disabled), and the note templates (`bounded_load_step`, `version_bump_left_to_merge`) record their spec as the story's decision note and write no file (default for those four); **`off`** — the template is not appended to the story at all. Daemon-mode tasks are skipped by the decompose lint and the per-task plan-structure checks. There are two writes, and they behave differently: the per-template mode keys (`autonomous.task_templates.mode.<id>` over `PUT /api/config`, the CLI, chat/comm, MCP `task_templates_mode`, or the per-template select in Settings) **merge one mode onto the stored map** and leave `enabled`, `templates` and `projects` alone; the decompose-lint config surface (REST `PUT /api/autonomous/decompose-lint/config`, its MCP tool, and the decompose-lint card in Settings) **replaces the block whole when it is sent** — the decompose-lint card sends back the block it loaded with only its enabled checkbox changed, so modes, custom templates and per-project overrides survive a card save (a change another client made after the card loaded is overwritten, so reopen the card before saving), while the REST, MCP and CLI writes still replace the whole block when they send it. `EffectiveTaskTemplateMode` resolves one template's effective mode (explicit setting, else the shipped default, else `llm`). See the [script-gate how-to section](howto/automaton-decompose-lint.md#the-script-gate-the-final-gate-no-longer-spends-a-session) for what the gate runs and when.

**Standard task runner** (`runner`) — the word the standard-task list shows beside a task's mode, naming what actually carries it out: `session` (a model session, for mode `llm`), `script gate` (the daemon's script gate, for `final_gate` on `llm`), `daemon` (the daemon itself, for mode `daemon`) or `none` (mode `off`; the task is not appended). Read on CLI, REST, MCP and the comm channel; the Automata sub-tab does not show it yet. See [howto/automata-standard-tasks.md](howto/automata-standard-tasks.md#the-runner-column).

**Daemon task mode** — the `daemon` value of a template's mode: the daemon carries the task out itself and completes it without a model session, a verifier or a retry loop. Accepted only for the four daemon-capable templates (`changelog_entry`, `parity_issue_draft`, `bounded_load_step`, `version_bump_left_to_merge`); the two file-writing ones write their generated artefact, the two note ones record their spec as the decision note. Setting a template back to `llm` makes it a session task again. See [daemon task modes and the script gate](howto/automaton-daemon-task-modes.md).

**Script gate** — the daemon-run closing check for a `final_gate` task: no session, no verifier, no retry. It resolves its checks by strict preference — the task's own script under `.datawatch/verify/` (it runs alone), else the project gate command (per-project policy first, then `autonomous.project_gate_command`; it runs alone), else every script the other tasks of every story of the Automaton declare (standard-task tasks and other final gates skipped) — and runs them in story order, until the first failure, under one shared deadline (`autonomous.script_gate_timeout_seconds`, default 900, `0` = no cap). Each check's output goes to the task report and its exit status decides: pass completes the task, fail or deadline fails it. A failure whose captured output says the storage it wrote to ran out (`no space left on device`, `ENOSPC`, `disk quota exceeded`, in any casing) reads as **temporary folder full**: that is the task's failure reason, and the infra-retry classifier matches that text into the `storage_write` class, so a gate that ran out of space takes the infra-retry path instead of being counted against the task; the gate's other bad endings (signal kill, never ran, deadline) keep their own reasons. See [howto/automaton-worktree-cleanup.md](howto/automaton-worktree-cleanup.md#the-no-space-gate-reason-temporary-folder-full) and [operations.md](operations.md#automaton-worktree-cleanup-finished-work-branches-worktrees-and-daemon-temp-files). A gate with nothing runnable is a failed task with the reason named. There is one final gate per Automaton, on the last story, depending on every task; the planner is told to plan exactly one and to give it a script or a `verification_script_reason`.

**Standard-task pack** — a named, versioned folder that ships standard tasks and Automaton templates together (task prompts, verification scripts, template definitions), with a `pack.yaml` manifest at its root. The built-in standard tasks are the pack named `builtin` (`packs.active` default), whose definitions are embedded in the binary from `internal/packs/builtinpacks/builtin/` and need no install. The repository's `internal/packs/testdata/` folders (`standard-tasks-pack/`, `fixture-one-kind/`) are test fixtures only, not embedded; the real standard-tasks pack is published in the community registry. Other packs are installed into an install root at `<name>/<version>/`; an installed version is never overwritten. The feature is off by default (`packs.enabled: false`). **Status:** list, install, accept, remove, the enable check, the `packs.active` validation and input saving work on REST, MCP, CLI and the chat command, and the web Settings card has the two `packs.*` fields and the inputs form (no web list, install, accept or remove yet: the Packs sub-tab under Automata > Templates is a shell with the disabled-state explanation, an Enable link and a skills registry link; its install, accept, change-summary, auto-accept and usage scripts exist but are not rendered). The resolver is not yet given `packs.active`, so an installed pack does not yet add tasks. Detail: [howto/packs.md](howto/packs.md).

**Active pack list** — the ordered packs that apply to one Automaton (`packs.ActivePacks`). Packs off gives `["builtin"]`. Packs on takes the first non-empty list of the Automaton's own `packs`, the project's list, then global `packs.active`; lists are never merged, and all empty gives `["builtin"]`. The Automaton field is set with `SetPacks` (trimmed, unique, priority order kept). **Status:** no surface sets an Automaton's list, the resolver is not given the result, and the project config entry has no pack list. Detail: [howto/packs.md](howto/packs.md#which-packs-apply-to-an-automaton).

**Pack enable check** — the rule that packs may be turned on only when at least one skills registry source is connected and enabled (`packs.CheckEnable`). It refuses with "packs need at least one connected skills registry source; connect one in the skills registry settings". **Status:** called when `packs.enabled` is turned on through REST, MCP, CLI, chat or the web switch (refusal is HTTP 409); a hand edit of the config file skips it. Detail: [howto/packs.md](howto/packs.md).

**Pack task resolver** — the step that builds the ordered standard-task list for one Automaton kind from the active packs (`packs.ResolveTaskList`). Packs are walked in order; each pack's tasks are added when the pack lists that kind, in manifest order. Two tasks with the same title are refused and the error names both packs. Each resolved task carries the name and version of the pack it came from. The built-in list is resolved for the `feature` kind only, and the apply step does not filter by kind today. Detail: [howto/automata-standard-tasks.md](howto/automata-standard-tasks.md#how-the-list-is-resolved).

**Pack input** — a value a pack declares in `pack.yaml` (`name`, `type`, `default`, `required`, `secret`). It resolves in the order Automaton, project, global, then the declared default (`packs.ResolveInputs`); a required input with no value anywhere is an error. A **secret** input accepts only a vault reference `${secret:NAME}` (`packs.CheckSecretInput`) and is shown masked as `***` if it holds anything else (`packs.RedactInputs`). A daemon-run standard task records its redacted inputs as `pack_inputs` at creation and keeps them. **Status:** inputs can be saved on every surface (the web at global and project level) and the save refuses a literal value for a secret input, but task creation does not read the saved values and the built-in pack declares none, so `pack_inputs` is empty today. Detail: [howto/packs.md](howto/packs.md#pack-inputs-the-three-levels).

**Pack attribution** — the `pack_name` and `pack_version` fields on trace events, and the `pack_name` detail on audit entries, meant to record which installable pack a task came from. Trace roll-ups group by these (`dimension=pack_name`, `pack_version`), the audit query filters on `pack_name`, and the cost report has a pack breakdown. **Status:** these are read-side only. No production code writes `pack_name` on an audit entry, and the `builtin` stamp helpers for trace events (`StampBuiltinPack`, `BuiltinStandardTaskTraceEvent`) have no production caller, so nothing is attributed on a running daemon yet. Events and entries without a pack name count as unattributed. Detail: [howto/packs.md](howto/packs.md#reporting-the-files-are-the-source-of-truth).

**Pack usage summary** — the per-pack count of runs (distinct sessions, tasks or Automatons), cost and last-used time, grouped by pack name and version (`packs.UsageByPack`). It is computed from the trace store; no daemon surface shows it yet. The Packs sub-tab has a table script for it, but the script is not loaded and the `GET /api/packs/usage` route it reads is not registered. Detail: [howto/packs.md](howto/packs.md#reporting-the-files-are-the-source-of-truth).

**Pack cost breakdown** — the cost report's rows per pack name and version (events, tokens in and out, cost), built from trace events (`costreport.PackBreakdown`). Events with no pack name fall into one `unattributed` row. It is a library function today and no cost-report surface calls it yet.

**Pack trust record** — the operator's acceptance of one pack name, version and exact content digest, with who accepted it and when, kept in `trust.json` in the install root. A pack that is not from a first-party source runs only with a record that matches its current digest; a changed file changes the digest and the old record stops applying (`pack not accepted: accept it first`).

**First-party pack source** — a skills registry flagged `first_party` (the community registry is one). Its packs run without a per-pack trust record. Accepting the source writes one record per pack that is current at that time; a later version has no record and is judged by the version-diff rules.

**Pack version diff** — the difference between a pack's accepted version and a candidate: new tasks, tools, scripts and file scopes. A new tool, script or file scope **widens access**; new tasks alone do not.

**Auto-accept (per source)** — `auto_accept_packs` on a skills registry source, off by default. When on, a new pack version from that source is accepted automatically, recorded as `auto:<source name>`, only when its version diff does not widen access. A widening version always waits for the operator. The Packs sub-tab has a per-source switch script for it (not yet rendered); nothing in the daemon reads the flag yet.

**Decompose guidance** — the text the decomposer's prompt carries. `autonomous.decompose_guidance`: literal text wins verbatim; a path to an existing readable file uses the file's contents; a path whose file is missing, unreadable or empty falls back to the generated block; unset (default) is generated from the active rules — one line per rule, `id (severity): message`, in pack order, so the rules explain themselves to the planner until you supply your own.

**A decompose-lint finding** — one item of the result: `rule_id`, `severity` (`block`/`warn`), `message`, the task or story the rule fired on (`-` when the finding is plan-level) and a short excerpt of what matched. One finding per line in the report: `[severity] rule_id task: message`; an empty report reads `clean: 0 findings`. The record stored per Automaton is `{findings, mode, attempts, linted_at}`.

Surfaces: PWA Settings (the Decompose-lint group) and the Automaton's detail panel (recorded pass + a dry-run button); REST `GET/PUT /api/autonomous/decompose-lint/config` and `GET/POST /api/autonomous/prds/{id}/decompose-lint` (stored record / dry run); the MCP tools `autonomous_decompose_lint_config_get`, `_config_set`, `_dry_run`, `_findings`; CLI `datawatch autonomous lint config | config-set key=value … | dry-run <id> | findings <id>`; comm channel `autonomous lint …`. See [howto/automaton-decompose-lint.md](howto/automaton-decompose-lint.md).

### Branch refresh

Branch refresh merges the base branch into an Automaton branch between tasks when the branch is at least `autonomous.branch_refresh_behind_limit` commits behind (default 5), so the final merge does not meet hundreds of commits of drift. It is off until `autonomous.branch_refresh_enabled` is set, or an Automaton's override says otherwise. A conflict leaves the branch untouched, stops refreshing for that Automaton and raises one question. The override is set from the Automaton page's Branch refresh card, the CLI, chat, MCP or REST (`POST /api/autonomous/prds/{id}/branch-refresh`); the question's answer is not yet applied, so a stopped Automaton stays stopped. A merge that fails for another reason is recorded with result `error` and the run goes on. See [howto/automaton-branch-refresh.md](howto/automaton-branch-refresh.md).

### Uncommitted-work check

The uncommitted-work check stops a task from being marked complete while its work sits uncommitted in the Automaton worktree, where a later merge of the branch would ship without it. Two settings in Settings → General → Automata settings → **Uncommitted-work check** control it. Each help link in those rows opens this section. The check applies only to an Automaton that commits its own work.

The check before an Automaton is reported finished does not count a generated document the daemon itself wrote that a person has not edited — first line still the generated marker, no `<!-- edited -->` line, body hash still matching the hidden sidecar file next to it — together with that sidecar file, so an Automaton whose only uncommitted files are the daemon's own unedited documents is completed and merge ready as usual. A document with an edited line, a removed marker, a mismatched sidecar hash, or a missing sidecar still counts, like any other file, and so does a sidecar whose document no longer reads as generated. The per-task check is unchanged. The detail and the answer flow are in [howto/automaton-uncommitted-work.md](howto/automaton-uncommitted-work.md) (*The documents the daemon itself generated*).

**Completion marker file** (`DATAWATCH_COMPLETE`) — the file a worker writes at the root of its worktree to say it has finished. It is the daemon's own marker, not work: new worktrees and the repository ignore it, the daemon deletes that one exact file from a finished Automaton's worktree just before the finished check, and both checks skip it (with `CHECKPOINT.md` and the tree lock) and say so in the question text. A worktree with only a marker is not held; a marker plus a real file is held for the real file only. See [howto/automaton-uncommitted-work.md](howto/automaton-uncommitted-work.md) (*The completion marker file*).

**Launcher tooling files** (`worker-mcp.json`, `worker-starting-context.md`) — the two files the session launcher writes when it starts a worker session (the session's own MCP config and its [starting context](#starting-context); see [Worker session profile](#worker-session-profile)). Each launch writes them into its own private folder (owner-only, outside the project folder, removed when the session ends), never into the project folder, because the MCP config can hold the session's API token and two sessions started in one project folder must not share a file; copies that an older version left at the working directory root are still treated as the launcher's artifacts. They are the launcher's artifacts, not the task's work: neither check counts an untracked root copy (a launcher-named file you track, or a same-named file in a subfolder, still counts — the match is the exact name at the repository root). The attempt snapshot and the automatic-commit staging are stricter — they skip both root names by name, whether or not the repository tracks them (skip reason `launcher`) — and just before a task branch merges into the Automaton's work branch the daemon removes a launcher file that the branch committed when the merge target has none (a committed copy the merge target itself carries, like a same-named file in a subfolder, is kept). No new setting; the detail is in [howto/automaton-uncommitted-work.md](howto/automaton-uncommitted-work.md), [howto/automaton-parallel-tasks.md](howto/automaton-parallel-tasks.md), [howto/autonomous.md](howto/autonomous.md) and [howto/worker-session-profiles.md](howto/worker-session-profiles.md).

**Uncommitted-work policy** (`autonomous.automation.uncommitted_work_policy`) — what happens when work is found uncommitted. `hold`, the default, commits the task's own declared files and then holds the task (or blocks the Automaton at the finished check) with the reason and the files, and raises one question. `warn` still commits the declared files but only records a warning for what is left and lets the task or Automaton complete. `off` skips the check and any commit attempt. An unset key or an unknown word reads as `hold`; any other word is refused when written. It is for the operator who wants a merge to carry everything the Automaton did: keep `hold`, and choose `warn` or `off` only when stray files are expected.

**Uncommitted-work scope** (`autonomous.automation.uncommitted_work_scope`) — which files the per-task check counts. `task_files`, the default, counts only the files the task changed itself, leaving out anything that was already modified or untracked before the task started. `worktree` counts every uncommitted file. An unset key or an unknown word reads as `task_files`. The check before an Automaton is reported finished always covers the whole worktree, whatever the scope. It is for the operator whose worktree is shared or already dirty: keep `task_files` so one old stray file does not hold every task.

**The three answers** — a held task or Automaton waits on a question with the options `commit`, `discard` and `retry`, and the default `hold`, so nothing changes until you answer. `commit` stages and commits exactly the listed files that are still uncommitted, including any outside the task's declared files, then completes the task or lets the Automaton roll up again. `discard` removes exactly the listed changes (tracked files go back to the last commit, untracked files are deleted) and runs the task again; it cannot be undone. `retry` touches no file: it runs the task again, or has the Automaton look at the worktree once more. A fourth answer, `ignore`, touches no file and clears the hold; the daemon answers with it by itself, at start, for any older open question whose files are all marker files. A commit or discard that cannot be done keeps the hold and raises a fresh question. See [howto/automaton-uncommitted-work.md](howto/automaton-uncommitted-work.md).

### Checks

The checks are four automatic, report-only reviews the daemon runs around an Automaton's work: the pre-start verifier, the plan rules, the after-done checklist and the registry check. None of them writes to a project file. Each has its own mode under `autonomous.checks.*` — `off` (not run), `shadow` (run and record the findings on the Automaton's triage record, with no alert and no block) or `on` (the default: run, record and alert, and for the pre-start verifier block a start). A project can override the global values. The findings, newest first, are shown on the **Automata checks** card in the Automation view's Resources sub-tab, by `GET /api/autonomous/checks`, the `checks_status` MCP tool, `datawatch autonomous checks [--prd ID] [--project-dir DIR] [--json]` and `autonomous checks [automaton-id]` on the comm channel. See [howto/automata-checks-and-ship.md](howto/automata-checks-and-ship.md).

### Pre-start verifier

The pre-start verifier (`autonomous.checks.pre_start_verifier`) checks a task's verification script before the task starts: it must exist as a regular file inside the project directory (symlinks and `..` included), be executable and begin with a shebang line. A task with no verification script reads as not applicable. A script that is only missing is not always a problem: the daemon first writes the script from a `Write it VERBATIM to <path>` block in the task spec (recorded as a `verifier_materialize` decision, never over an existing file), and a spec with no such block that tells the worker to create the script lets the task start with a `verifier_prestart_task_created` decision, because the check after the task fails it if the file was never written. See [howto/automaton-verification-scripts.md](howto/automaton-verification-scripts.md#a-script-that-does-not-exist-yet). In `on` mode a problem blocks the task start and records why; `autonomous.checks.block_when_missing` (default on) is part of the same policy and is shown with it on the card and in the status read. In `shadow` mode the problem is only recorded. It is for the operator who wants a task that can never be verified to stop before it spends a session, not after.

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

### RTK instructions

The RTK instructions are the short block of text that tells a session to put `rtk` (Rust Token Killer) in front of its shell commands so the output is compressed before it reaches the model. The block is delimited by the marker lines `<!-- rtk-instructions -->` and `<!-- /rtk-instructions -->`. By default datawatch delivers it through the [untracked session context file](#untracked-session-context-file), and it never writes it into `AGENT.md` or `CLAUDE.md`. The setting `rtk.write_rules_files` (default `false`) restores the old behaviour for the RTK block and for the memory instructions and cross-session communication rule too: they are merged into the tracked [rules files](#rules-files) instead, together with a refresh of the `# Session Guardrails` header, and the untracked file no longer carries them. The session record's `rtk_instructions_delivered` field is `true` once the text is in place by either route.

At daemon start a cleanup pass removes old RTK, memory and cross-session blocks from a tracked rules file only when those blocks are the file's sole difference from the committed version — the file is tracked, has one plain unstaged modification, and is byte-identical to `git show HEAD:<file>` without them. Otherwise the file is left untouched and one warning alert per worktree names it. The pass is skipped while `rtk.write_rules_files` is on. Set the setting in Settings → General → **RTK (Token Savings)**, in `config.yaml`, with `PUT /api/config`, `datawatch config set`, the `config_set` MCP tool or `configure rtk.write_rules_files=true` on the comm channel. See the RTK how-to, [rtk-integration.md](rtk-integration.md#where-the-rtk-instructions-go).

### Rules files

The rules files are the files that carry a project's working rules: `AGENT.md` and `CLAUDE.md` in the project directory, and the file the rules and context policy names (see [Rules and context](#rules-and-context)). People write them; the daemon does not. By default the daemon never writes into them: it does not create them, does not refresh their `# Session Guardrails` header, and does not append the RTK instructions, the memory instructions, the cross-session communication rule or the platform context — all of those go to the [untracked session context file](#untracked-session-context-file) — and the single setting `rtk.write_rules_files` is what turns the old merge back on — and an Automaton worker must not add checkpoint or progress entries to them (see [Worker checkpoint rule](#worker-checkpoint-rule)). A change to the project rules file is also a [protected path](#protected-paths) by default.

### Untracked session context file

The untracked session context file is `CLAUDE.local.md` in a project directory: the per-session file through which datawatch hands a claude-code session its [platform context](#platform-context), its [RTK instructions](#rtk-instructions), its memory instructions and its cross-session communication rule, each in its own marked block. In a git repository the filename is added to the repository's `info/exclude` list, a per-clone ignore list that is not itself tracked, so writing it never changes `git status` and the file cannot reach a commit. Text you write outside the marked blocks is kept, and the file is deleted when nothing is left in it. A `CLAUDE.local.md` that the project already tracks is never written to.


### Worker session profile

A named, user-configurable set of the things a session **starts with**: **which MCP servers** it can use (none, the datawatch server only, or a named list), **which tools** it gets (all, or an allow-list), **whether skills are on**, the **level of starting context** (minimal, digest or full — the task's spec and constraints, the project's rules at that level, the daemon's platform context), and the **model and effort** the session runs on. Four built-ins ship — `minimal`, `digest`, `full` and `datawatch-tools` — and a default profile per role (planner, worker, verifier, judge) and per task kind (implementation, docs, note, final gate) is the table a launch falls back to. A launch resolves its profile by name in one order — task → Automaton → project → daemon-wide → kind default → role default, the first set name winning — so the kind defaults reach both task launches and session starts, and the six session kinds sit in that same table: `supervisor` on the lean `datawatch-tools` (it drives datawatch through its tools and nothing else), `operator_interactive` on `digest`, `scheduled_command` on `minimal`, `planner` on `full`, `one_shot` on `minimal` and `review` on `minimal`; with nothing configured no profile is applied and the session starts with the full harness. Worker, verifier and judge launches each resolve and apply the profile they land on, and a start you run resolves its own layers: a named `worker_profile_name` first, then the daemon-wide name, then the kind row its launch facts name (`launch_role=supervisor` or `launch_role=planner`, `launch_kind=review`, or a one-shot start); a plain start with no name and no such launch fact gets no profile at all and starts the full harness, exactly as before. A profile never shrinks the work a task needs — spec, constraints and verification stay whole — it only chooses the starting context and the tool and MCP sets. Profiles are set and read through the settings file, the web settings group, the REST, MCP, CLI and comm-channel keys, the New Session form, and the [profile in force](#profile-in-force) read. See [howto/worker-session-profiles.md](howto/worker-session-profiles.md). A lean supervisor still sees the datawatch tool server: its tools stay discoverable through tool search (see [how the tools stay reachable](howto/worker-session-profiles.md)).

A profile's scope is every way a session starts: a worker, verifier or judge task launch, a start of any of the six session kinds, or a plain start that names one — a plain start that names nothing is unchanged (full harness), and what a task finishes is unchanged.

### Profile in force

The profile a role, a task kind or one of the six session kinds — `supervisor`, `operator_interactive`, `scheduled_command`, `planner`, `one_shot`, `review` — launches with, and the tier that set it: the daemon-wide name, the configured kind default, the configured role default, or the built-in default when nothing is set (or `none`, no profile applied). Read it on REST (`GET /api/config`, the `autonomous.in_force` block), MCP (`worker_profile_status`, its `in_force` block), the CLI (`datawatch config get` on the worker-profile keys), the comm channel (a bare `configure autonomous.worker_profile…` key) and the Profile in force section of the Worker session profiles group in the web settings. See [howto/worker-session-profiles.md](howto/worker-session-profiles.md#setting-and-reading-it).

### Fallback to the full harness

What a session starts with when its resolved profile cannot be applied: the session starts anyway, with the full harness, as it did before profiles existed, and the fallback and its reason land on the record (the session record for a start, the launch decision for a task launch). The fallback sticks: a resume or a restart of a session that fell back keeps it profile-less. Nothing is dropped silently. A launch with nothing configured is not a fallback — no profile is applied and the session starts with the full harness, with no reason. See [howto/worker-session-profiles.md](howto/worker-session-profiles.md#what-a-fallback-reason-means).

### Starting context

The generated text a session that starts under a profile — a worker, verifier or judge launch, or a start of a session kind that resolves a row — is handed when it starts: its role, the line naming its [worker session profile](#worker-session-profile), the constraints, the project's rules at the level the profile calls for (none at `minimal`, the rules digest at `digest`, the full rules at `full`), and the task last. It is built deterministically, so the same role and profile give the same text. Its size is recorded as the [start-up token size](#start-up-token-size). The two session artifacts it pairs with — `worker-mcp.json` and this `worker-starting-context.md` — are the [launcher tooling files](#uncommitted-work-check): no check that counts a task's work treats the untracked root copies as work. See [howto/worker-session-profiles.md](howto/worker-session-profiles.md).

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

### Worker checkpoint rule

The short, fixed rule appended to every worker prompt that carries the [rules and context](#rules-and-context) block: a worker of an automated Automaton task must not add, edit or remove checkpoint, progress-note or status entries in `AGENT.md`, `CLAUDE.md` or the configured rules file, and it must not copy their Checkpoints section into its work or notes; entries there are written only by the human operator and by the supervisor. A worker that needs to leave a progress note writes it to `CHECKPOINT.md` at the root of its working directory instead — the daemon relocates that file, and the uncommitted-work checks treat it as a marker, not work. The rule is static instruction text that reads no repository files, and a prompt that already holds it (a retried task) is not given it a second time. Only worker prompts carry it: the planning prompt decompose uses does not, and a prompt with no rules block (a task with no project directory) has none. It is an instruction, not a wall: if a worker appends an entry anyway and that entry is the file's entire change since the base commit, the [protected paths](#protected-paths) gate strips it out and records a `protected_paths_checkpoint_reverted` decision without holding the task, but a change that goes beyond the entry is still caught by the gate. See [howto/automata-rules-and-context.md](howto/automata-rules-and-context.md#the-no-checkpoints-rule) and [howto/automaton-protected-paths.md](howto/automaton-protected-paths.md#the-checkpoint-entry-revert).

### Restart guard

A restart that would interrupt in-flight work — a planning Automaton, a verifying task, a running judge, a running task, a running final gate or a live session — is refused by default on every surface: `POST /api/restart` answers 409 with the interruption report, `datawatch restart` prints the report and exits non-zero, the `restart_daemon` MCP tool returns an error carrying the report, and `restart` on the chat channel answers with the report. Nothing is stopped. The Web UI's Restart button (and the auto-restart after a config change) sends no force, so it reports the refusal. To restart anyway: `datawatch restart --force`, `POST /api/restart?force=true` or a `{"force": true}` body, `restart_daemon` with `force=true`, or `restart force` on the messaging channel. The shared read-only report is `GET /api/autonomous/restart-interruptions` (also the `restart_interruptions` MCP tool, `datawatch autonomous restart-interruptions`, and the chat verb `autonomous restart-interruptions`); it names each interrupted item with its id and counts, carries `needs_force` (set while any of them is in flight), and every refusal above reuses it as is. A forced restart still applies `autonomous.restart_policy`. The live confirmation of the refusal and the forced paths on a build in use is still owed (see the *Still to review (operator)* notes in [operations.md](operations.md)). Where to force it on each surface is [howto/restart-with-work-running.md](howto/restart-with-work-running.md#forcing-a-restart-past-running-work).

### Planning restart policy

`autonomous.planning_restart_policy` decides what a daemon restart does to an Automaton that was still planning (decompose in flight). `reattach` — the default, and what an unset or unrecognised value reads as — keeps planning running, per [Planner re-attach](#planner-re-attach). `restart` skips straight to the one automatic planning restart, with the decision recording it. `draft` resets the Automaton to draft, as the daemon did before the setting existed. The write paths refuse any other value and name the valid ones. Set it under `autonomous:` in `~/.datawatch/config.yaml`, by `PUT /api/config`, the `autonomous_config_set` MCP tool, `datawatch autonomous config-set`, `configure` on the chat channel, or the PWA Settings **Automata** tab; `GET /api/config` returns the stored value. The decision of an automatic restart is one entry in the loop status' `boot_resume` array (the `prd_id`, the `outcome`, the `reason` and the `at`), read on `GET /api/autonomous/status`, the `autonomous_loop_status` MCP tool, or `datawatch autonomous status`. The operator decision (2026-11-08) is recorded in [plans/restart-matrix-findings.md](plans/restart-matrix-findings.md); the walkthrough is in [howto/autonomous-recovery.md](howto/autonomous-recovery.md) (what happens to Automata when the daemon restarts) and the how-to in [howto/restart-with-work-running.md](howto/restart-with-work-running.md), and the restart matrix and the release-smoke skip status are in [operations.md](operations.md).

### Planner re-attach

An Automaton in `planning` state (decompose in flight) when the daemon stops is not reset to `draft`, and its planner session is not ended. At boot the daemon: (1) re-attaches a reader to the live planner session (tmux) and its output file, so the decompose completes as if the restart had not happened; (2) ingests the complete plan if the planner session ended while the daemon was down and left one — no new planner runs; (3) restarts planning once if neither yields a usable plan, with the decision recording it. Either way the Automaton stays in `planning`: a restart (re-attach or fresh decompose) never counts a failure, records a failure or resets the Automaton to draft, however many restarts happen in a row; only the `draft` policy value resets it. A live planner session that no reader owns is ended on every path, and the decision says how many. The plan then proceeds to review (approve/revise) as usual. This is the default `reattach` value of `autonomous.planning_restart_policy`; the live check on a build in use is still owed (see the how-to's *Current status* and the *Still to review (operator)* notes in [operations.md](operations.md)). See [howto/restart-with-work-running.md](howto/restart-with-work-running.md).

### verifying

The state of a task whose worker session has ended and whose result is being checked. A verifying task keeps its place in the Automaton but releases its worker lane at once. Its verification takes a verifier slot (a node's `verifier_slots` or the verifier pool); with none free it waits for one without holding a worker slot. It shows under `tasks.verifying` in `GET /api/capacity`, in `datawatch capacity` and `capacity_status`, and on the Settings capacity card, each marked as running its verifier or waiting for a slot. A retry after a failed verification goes back through admission and may wait in `waiting_capacity`. See [howto/automata-capacity.md](howto/automata-capacity.md#verifying-tasks-in-the-capacity-view).

### Verifying restart behavior

A task in `verifying` state (its worker finished, its verdict not yet delivered) when the daemon stops keeps its worker session and its worker result; no retry is counted and the worker is not started again. At boot the daemon, in order: (1) re-attaches a waiter to the live verifier session where one is still alive — the verdict that lands completes the task (pass) or goes to the normal retry path (fail) and no second verifier is started; (2) consumes the verdict the verifier session left in its verdict file where it ended while the daemon was down; (3) only where neither is usable falls back to re-verifying the finished worker once, or resetting a task with no worker result to `pending`. A guardrail judgment in flight (for example the completion guardrail judging a finished Automaton) resumes the same way — attach to the live judgment, read the verdict it left, or re-judge once — and is never run twice for the same decision. The boot path does this before any task state is resumed, so a verdict that lands after boot goes through the ordinary completion or retry path. The live check on a build in use is still owed, and the restart matrix that would run it cannot run against this tree's build yet — its sandbox config lacks `server.allowed_project_roots`, so its states fail at Automaton creation (see the *Current status* note in [howto/restart-with-work-running.md](howto/restart-with-work-running.md#current-status); the *Still to review (operator)* notes are in [operations.md](operations.md)); the operator walkthrough is in [howto/autonomous-recovery.md](howto/autonomous-recovery.md) (*A task still verifying at restart*) and the walkthrough of all three cases is in [howto/restart-with-work-running.md](howto/restart-with-work-running.md#verifying-judging-and-verification-scripts).

### Final gate

The check, or set of checks, that runs the whole verification at the end of an Automaton: the last task's verification script, the project gate command, or every task's script in story order. The daemon runs it itself, with no worker session. It runs in its own session and keeps a durable record (the process and its start time, its output and result files), so it survives a daemon stop or restart and is re-attached at the next start instead of being failed. See [howto/restart-with-work-running.md](howto/restart-with-work-running.md#verifying-judging-and-verification-scripts) and [Verification script run record](#verification-script-run-record).

### Gate drain

The wait a graceful daemon restart or stop makes for running final gates before it exits. It lasts up to `autonomous.gate_drain_minutes` (default 10; 0 = do not wait; a negative value is rejected), polls until the last gate ends, and holds new gates back meanwhile. When it times out the daemon exits anyway: the unfinished gates keep running and re-attach at the next start, and nothing is signalled or killed. The running gates are listed as `running_gates` / `running_gate_count` in the restart interruption report (REST `GET /api/autonomous/restart-interruptions`, MCP `restart_interruptions`, CLI and chat), and `POST /api/restart` answers with `gates_waited` and `gates_interrupted`. See [howto/restart-with-work-running.md](howto/restart-with-work-running.md#waiting-for-running-final-gates) and [operations.md](operations.md).

### Restart retry

The one extra run a final gate gets when a signal kills it within five minutes of the daemon starting, taken as a casualty of the restart rather than a real failure. The Automaton reruns the gate once; the retry is outside the task's retry budget, counts no attempt, and is marked on the task (`gate_restart_retry_used`). The decision log names the daemon restart as the cause and the gate report notes the retry. A second signal kill, a kill later than five minutes after the daemon started, a timeout and a nonzero exit code are real failures. See [howto/restart-with-work-running.md](howto/restart-with-work-running.md#verifying-judging-and-verification-scripts).

### Verification script and exemption reason

A task's **verification script** is the one repo-relative script file the verifier runs to check the task; a command string is not a script. A task that needs none carries a **verification script exemption reason** instead, the stated reason the plan check accepts in place of a script. The operator can set either on a task that is still editable, without a re-decompose: the `verification_script` and `verification_script_reason` fields of `edit_task` (MCP `autonomous_prd_edit_task`, CLI `prd-edit-task --verification-script` / `--verification-script-reason`, comm `edit-task`, REST `edit_task`, or the **Verification script** and **Script exemption reason** fields of the task editor in the web app). The script path must be clean, repo-relative, and not absolute, a `..` path or the project root; it is also added to the task's generated files, and the file itself must already exist in the project. See [the operations guide](operations.md) for the held-task recovery flow.

Verification also runs the repository lint: when a unit's changed files (the branch's diff against its merge base with `main`, untracked files included) touch Go files, the daemon runs `golangci-lint` over the touched packages after the verification script passes — for a task and a story alike — and the unit passes verification only if the lint is clean. A lint finding, or a linter that cannot start, fails the unit with a **non-retryable** `content` failure, source `lint-gate`: the failure reason carries the finding text and the same text (capped at 8 KiB) is appended to the script output, so the operator sees exactly what to fix — and because the failure is non-retryable, no automatic attempt follows; fix the findings (or install `golangci-lint` on the worker's node) and reset the unit. A unit whose changed files touch no Go files, or that has no project directory, or a project directory that is not a git work tree, skips this step. Details and the operator review items are in [howto/automaton-verification-scripts.md](howto/automaton-verification-scripts.md#7-the-verification-lint-gate); the entry beside the merge checks is **Verification lint gate** below.

### Cumulative allowed-file helper

The shared piece of a verification script that decides which changed files are acceptable. Its list is cumulative from the start of the Automaton: every file any story may touch, the files the standard tasks generate (changelog draft, parity issue draft and its hash file, docs, the registry plan files), the Go files `gofmt` may rewrite, and the docs files. It is compared against the merge base (`git merge-base HEAD main`), never against one task's own file list, so an early task and the last task pass against the same list. The planner is told to use it, and the `verify_script_task_files_only` lint rule flags a script that does not. See [howto/automaton-verification-scripts.md](howto/automaton-verification-scripts.md).

### Final gate exemption reason

The stated reason a `final_gate` task gives, in its `verification_script_reason`, for having no verification script of its own (for example that the closing check is the project's configured gate command). A final gate always carries a script, a usable report path, or this reason; with none of the three the `final_gate_without_script` lint rule flags it and the plan check refuses the plan. Set it with the `verification_script_reason` field of the task edit on REST, MCP, CLI, the comm channel or the web app. See [howto/automaton-verification-scripts.md](howto/automaton-verification-scripts.md#4-one-final-gate-per-automaton).

### Verification script run record

A task verification script, and a final-gate script, do not run as a direct child of the daemon with no record: they run detached and keep a durable run record under the data directory (`verify-runs`) — the process and its start time, its output, its exit code, and whether its result has been delivered. At boot the restarted daemon: waits on a record whose process is still running and takes its result when it ends; consumes once the exit code of a record whose process ended while the daemon was down and whose result is not yet delivered; or retires an undelivered, unfinished run left by an earlier attempt of the same script before running the script from the start, exactly once, where no usable record survives. The timeout is enforced from the recorded start time, so a script that outlives the daemon still ends on time across the restart. A record's process is trusted only while the operating system's own process table agrees with it: the process must exist, must not be a zombie, and must have started at the recorded time (within a few seconds). A start time that cannot be read, or that is implausibly large (beyond about 2^50 clock ticks), makes the process untrusted rather than guessed at, exactly as on a host without a process table, and the record is then handled as one whose process is gone. A **final gate** keeps its own record per check and project directory, so a final gate task found running at boot returns to `pending` with no failure and no retry counted and re-attaches to its record; a stopping daemon marks the gate interrupted, not failed. A gate killed by a signal when the daemon started less than five minutes earlier is rerun once (the **gate restart retry**, recorded on the task as `gate_restart_retry_used`), outside the retry budget; any later or second kill is a real failure. The shipped systemd unit sets `KillMode=process` so a service stop leaves the gate running. The effect: a slow or side-effect-heavy verification is not re-run on a restart, which is the opposite of the behaviour recorded in [plans/restart-matrix-findings.md](plans/restart-matrix-findings.md) ("daemon children, rerun from scratch, OPERATOR PICK") — that report measured the daemon before this fix, and the matrix states and release-smoke skips describing the old behaviour stay until they are re-run on a build from this tree — which is not possible yet, because the matrix sandbox config lacks `server.allowed_project_roots` and its states fail at Automaton creation (see the *Current status* note in [howto/restart-with-work-running.md](howto/restart-with-work-running.md#current-status); the *Still to review (operator)* notes are in [operations.md](operations.md)). See the plan in [plans/2026-11-09-verification-script-boot-resume.md](plans/2026-11-09-verification-script-boot-resume.md) and the walkthrough in [howto/restart-with-work-running.md](howto/restart-with-work-running.md#verifying-judging-and-verification-scripts).

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
### Pending items

A pending item is a named piece of work that has no Automaton yet. An Automation can carry it as a member, so the order of the chain is kept even though the work is not written up. When the Automation reaches a waiting item it holds there, or skips the member, until an Automaton is attached. Nine settings control how that works. They sit in Settings → General → Automata settings → **Pending items**, the group's help link opens this section, and each field's help link opens the entry below with the matching heading. Two keys are about the Automation; the other seven describe an optional file registry that the daemon reads to copy rows in as pending items. With no registry path set there is no file registry, and every pending item is one you added yourself.

Every key below is the same key on every surface: in the daemon's `config.yaml` under `autonomous:` → `automation:` (the file registry keys sit one level deeper, under `pending_item_source:`); `datawatch config set <key> <value>`; the Settings field named in the entry; `PUT /api/config` with the dotted key (or `PUT /api/autonomous/config` with the `automation` object), and `GET /api/config` for the value in effect; the chat verb `configure <key>=<value>`; and the MCP tools `config_set` (the dotted key) and `autonomous_config_set` (the `automation_` argument named in the entry). A value that is not allowed is refused on every surface with an error that names the allowed words, and nothing is saved. All nine are read live, with no restart. See [howto/automaton-pending-items.md](howto/automaton-pending-items.md).

#### Pending item policy

**YAML name:** `autonomous.automation.pending_item_policy`. **Allowed values:** `hold` or `skip`. **Default:** `hold`.

What an Automation does when its cursor reaches a member that is a waiting pending item. `hold` blocks the Automation at that member, with a reason that names the item, and every later member waits behind it; the hold ends when an Automaton is attached, when the item turns done, or when the member is skipped or removed. `skip` passes the member over on its normal complete edge, and the item stays waiting. An unset key or an unknown word reads as `hold`; any other word is refused when written. Keep `hold` when the order matters and the work must be written up before the chain goes on; choose `skip` when a waiting item should never stop the chain. **On every surface:** YAML `pending_item_policy`; CLI `datawatch config set autonomous.automation.pending_item_policy skip`; Web UI field *Pending item policy*; REST `PUT /api/config` key `autonomous.automation.pending_item_policy`; comm `configure autonomous.automation.pending_item_policy=skip`; MCP `config_set` or `autonomous_config_set` argument `automation_pending_item_policy`.

#### Pending item auto resume

**YAML name:** `autonomous.automation.pending_item_auto_resume`. **Allowed values:** `true` or `false`. **Default:** `true`.

Whether attaching an Automaton to a waiting item resumes an Automation that was blocked on it. With `true`, the member is converted and the Automation resumes and starts the Automaton at once. With `false`, the member is still converted but the Automation stays blocked, and the attach reply says so; resume it yourself when you want the Automaton to start. It is for the operator who wants to look at an Automation before it goes on. **On every surface:** YAML `pending_item_auto_resume`; CLI `datawatch config set autonomous.automation.pending_item_auto_resume false`; Web UI field *Pending item auto-resume*; REST `PUT /api/config` key `autonomous.automation.pending_item_auto_resume`; comm `configure autonomous.automation.pending_item_auto_resume=false`; MCP `config_set` or `autonomous_config_set` argument `automation_pending_item_auto_resume`.

#### Pending item file registry path

**YAML name:** `autonomous.automation.pending_item_source.path`. **Allowed values:** a file path; empty means no file registry. **Default:** empty.

The file the daemon reads to copy rows in as pending items. It is a Markdown file that holds a pipe table. Empty means there is no file registry: pending items are only the ones added by hand, and the refresh action reports that nothing is configured and changes nothing. An absolute path is used as given. A relative path is resolved against the daemon's default project directory and may not leave it, by `..` or by a symbolic link. The file must be a regular file of at most 2 MiB. The daemon only reads it, never writes to it, and reads it once when it starts and whenever you use the refresh action; it does not poll. **On every surface:** YAML `pending_item_source.path`; CLI `datawatch config set autonomous.automation.pending_item_source.path registry/work.md`; Web UI field *Pending item file registry path*; REST `PUT /api/config` key `autonomous.automation.pending_item_source.path`; comm `configure autonomous.automation.pending_item_source.path=registry/work.md`; MCP `config_set` or `autonomous_config_set` argument `automation_pending_item_source_path`. The refresh action is `POST /api/autonomous/pending-items/refresh`, MCP `autonomous_pending_items_refresh`, `datawatch autonomous pending-items-refresh` and the chat verb `autonomous pending-items-refresh`.

#### Pending item file registry id pattern

**YAML name:** `autonomous.automation.pending_item_source.id_pattern`. **Allowed values:** a valid regular expression (Go syntax) of at most 512 characters. **Default:** `^\S+$`, which accepts any non-blank id.

Which id cells count as pending items. Only a row whose id cell matches the pattern is read; every other row is counted as skipped. Use it when your ids have a shape of their own, for example `^\d+$` for plain numbers, or `^[a-z]+(-[a-z]+)*$` for short names made of words joined by hyphens. Nothing assumes a numbering sequence, so the default accepts anything that has no spaces. An expression that does not compile is refused when written. **On every surface:** YAML `pending_item_source.id_pattern`; CLI `datawatch config set autonomous.automation.pending_item_source.id_pattern '^\d+$'`; Web UI field *Pending item file registry id pattern*; REST `PUT /api/config` key `autonomous.automation.pending_item_source.id_pattern`; comm `configure autonomous.automation.pending_item_source.id_pattern=^\d+$`; MCP `config_set` or `autonomous_config_set` argument `automation_pending_item_source_id_pattern`.

#### Pending item file registry id column

**YAML name:** `autonomous.automation.pending_item_source.id_column`. **Allowed values:** a header name (matched without regard to case) or a column number counting from 1. **Default:** `1`.

Which column of the table holds the id. The id cell becomes the item's `ref`, the outside label the daemon uses to recognise the same row on the next read. A header name wins when a header carries exactly that text, so a column titled `2` is found by its name before the number is tried. A table is used only when its header resolves both the id column and the title column. **On every surface:** YAML `pending_item_source.id_column`; CLI `datawatch config set autonomous.automation.pending_item_source.id_column Slug`; Web UI field *Pending item file registry id column*; REST `PUT /api/config` key `autonomous.automation.pending_item_source.id_column`; comm `configure autonomous.automation.pending_item_source.id_column=Slug`; MCP `config_set` or `autonomous_config_set` argument `automation_pending_item_source_id_column`.

#### Pending item file registry title column

**YAML name:** `autonomous.automation.pending_item_source.title_column`. **Allowed values:** a header name (matched without regard to case) or a column number counting from 1. **Default:** `2`.

Which column of the table holds the title of the work. A row with an empty title is skipped, and a changed title in the file updates the item's title on the next read. It is set the same way as the id column. **On every surface:** YAML `pending_item_source.title_column`; CLI `datawatch config set autonomous.automation.pending_item_source.title_column Work`; Web UI field *Pending item file registry title column*; REST `PUT /api/config` key `autonomous.automation.pending_item_source.title_column`; comm `configure autonomous.automation.pending_item_source.title_column=Work`; MCP `config_set` or `autonomous_config_set` argument `automation_pending_item_source_title_column`.

#### Pending item file registry state column

**YAML name:** `autonomous.automation.pending_item_source.state_column`. **Allowed values:** a header name (matched without regard to case) or a column number counting from 1; empty means the table has no state column. **Default:** empty.

Which column of the table holds the state of the work. With no state column set, no row is ever read as done, so an item only becomes done when its attached Automaton completes. With one set, a row whose state cell is one of the done values turns its item done, which releases any Automation held on it. **On every surface:** YAML `pending_item_source.state_column`; CLI `datawatch config set autonomous.automation.pending_item_source.state_column Status`; Web UI field *Pending item file registry state column*; REST `PUT /api/config` key `autonomous.automation.pending_item_source.state_column`; comm `configure autonomous.automation.pending_item_source.state_column=Status`; MCP `config_set` or `autonomous_config_set` argument `automation_pending_item_source_state_column`.

#### Pending item file registry done values

**YAML name:** `autonomous.automation.pending_item_source.done_values`. **Allowed values:** a list of non-blank words, compared without regard to case. **Default:** `done`, `closed`, `shipped`.

The state words that mean the work is finished. It has no effect without a state column. A row whose state cell equals one of them moves its item to `done`, and a done item never goes back. In YAML it is a list; with `datawatch config set`, the chat verb `configure` and `config_set` it is written as comma separated words. **On every surface:** YAML `pending_item_source.done_values`; CLI `datawatch config set autonomous.automation.pending_item_source.done_values "done,closed,shipped"`; Web UI field *Pending item file registry done values* (comma separated); REST `PUT /api/config` key `autonomous.automation.pending_item_source.done_values`; comm `configure autonomous.automation.pending_item_source.done_values=done,closed`; MCP `config_set` or `autonomous_config_set` argument `automation_pending_item_source_done_values`.

#### Pending item file registry missing rows

**YAML name:** `autonomous.automation.pending_item_source.on_missing_row`. **Allowed values:** `keep` or `drop`. **Default:** `keep`.

What happens to an item that came from the file when its row has disappeared from the file. `keep` leaves it exactly as it is. `drop` removes it, but only when it is still waiting and is not a member of any Automation; an attached or done item, or one an Automation carries, is always kept. An unset key or an unknown word reads as `keep`; any other word is refused when written. Items you added by hand are never touched by a read. **On every surface:** YAML `pending_item_source.on_missing_row`; CLI `datawatch config set autonomous.automation.pending_item_source.on_missing_row drop`; Web UI field *Pending item file registry when a row goes missing*; REST `PUT /api/config` key `autonomous.automation.pending_item_source.on_missing_row`; comm `configure autonomous.automation.pending_item_source.on_missing_row=drop`; MCP `config_set` or `autonomous_config_set` argument `automation_pending_item_source_on_missing_row`.

**Lane** — one channel work can run in. There are two kinds. A **GPU lane** is one of the channels on the local model pool; which machine serves a given session is not fixed per lane, so the pool is full when every GPU lane is in use wherever it runs. A **Claude lane** runs one Automaton at a time on the Claude backend. A lane is **occupied** only while an Automaton with a live worker, verifier or judge session, or a task that is starting, runs in it, and **idle** otherwise; a running Automaton that has finished its tasks, is waiting for its completion review and merge, or is waiting on a task-branch merge that has not progressed does not occupy its lane (see *Waiting Automaton* and *Lane fill*). Lane fill chooses which Automaton an idle lane starts; the lane a single task uses is chosen when that task is admitted, from its effective backend. See [howto/automation-lane-fill.md](howto/automation-lane-fill.md) and [howto/automata-task-routing.md](howto/automata-task-routing.md).

**Admission** — the moment a task asks for a slot and is let in, queued or moved. Admission reads the task's effective backend at that moment, so the lane is chosen when the task is admitted and not when it is planned, and a relaunch or re-queue is judged again. When the task's own pool is full and another allowed pool is free, the other pool is used under the retry-other-backend rule (pool fall-through, `autonomous.capacity_fallthrough`, off by default), and the choice and reason are written to the Automaton's decision log as a `capacity_fallthrough` entry. See [howto/automata-capacity.md](howto/automata-capacity.md) and [howto/automation-lane-fill.md](howto/automation-lane-fill.md).

**Per-task backend override** — a backend (and model) set on one task instead of inherited. A task inherits its story's setting, then the Automaton's, then the global default; the planner never writes a backend onto a task, so an override exists only when the spec names one (a line that is exactly `backend: <name>`) or a person sets it. While the task waits for capacity, changing the override moves its queued request to the new pool at once, and a reset re-queues it on the new pool. A task that holds a slot, is running or has finished is left alone. See [howto/automata-task-routing.md](howto/automata-task-routing.md).

**No-node wait** — the state of a pi-family task that was granted capacity but has no compute node of its LLM entry leased. No worker is started: the task gives its slot back and waits for a node, or moves to another pool, with a wait reason that names the backend and model. It is not a failure and is not counted against the content retry budget, so its retry counters do not move; only the capacity wait timeout ends the wait. See [howto/automata-capacity.md](howto/automata-capacity.md) and [howto/automation-lane-fill.md](howto/automation-lane-fill.md).

**Lane fill** — the daemon's own check that every lane stays busy. On each poll it looks at the idle lanes and the queued Automata, and decides what each idle lane should start: a decomposed Automaton that is ready, or a draft to decompose ahead. A lane that cannot take anything shows the reason it stays idle. A `needs_review` Automaton whose stored plan-coverage record fails the approve gate in `block` mode without a covering acknowledgment is refused up front as `plan_coverage_blocked` and the lane takes the next eligible Automaton instead; lane fill never approves it and never acknowledges on the operator's behalf, so the only remedies are a re-decompose with a plan that covers the spec or an explicit acknowledgment of exactly that failing surface (`AcknowledgePlanCoverage`). Lane fill never merges, never judges design or difficulty, and never holds GPU work because of Claude usage limits. An unreachable pool node that holds no work is reported as a degraded host instead of blocking every GPU lane. A running Automaton counts as occupying a lane only while it has live work (a running worker, test, verifier or judge session, a held capacity lease, a task starting, or a merge still inside its window); otherwise it is a *waiting Automaton*, listed under `waiting` with a reason (`waiting_review_merge`, `merge_stalled`, `no_task_startable`), and its lane stays free for the next eligible work. A task-branch merge pending longer than `autonomous.lane_fill.stalled_merge_minutes` (default 30; `0` off) is a *stalled merge*: it frees the lane, and with the mode `on` it is alerted once and retried. See [howto/automation-lane-fill.md](howto/automation-lane-fill.md#a-running-automaton-with-no-live-work).

**Waiting Automaton** — a running Automaton that holds no live work, so it does not occupy a lane. It has no live worker, verifier or judge session, no capacity lease still held for one of its tasks, no task starting, and no verified task whose merge is still inside the stalled-merge window. It is shown as waiting, with a reason and a stable reason code, in the `waiting` list of the lane fill status (`prd_id`, `title`, `backend`, `wait_reason`, `reason_code`; an empty list when none): `waiting_review_merge` (every task is completed and nothing is left to merge, so it waits for the completion review and the closing of the Automaton), `merge_stalled` (verified work has been pending its merge past `autonomous.lane_fill.stalled_merge_minutes` with no progress) or `no_task_startable` (no task is live, starting or ready, and nothing is pending a merge). Its lane is left idle for the next eligible work, and lane fill never starts a second run of it. The status is read with `GET /api/autonomous/lane-fill`, MCP `autonomous_lane_fill_status`, `datawatch autonomous lane-fill`, `autonomous lane-fill` on the comm channel and the **Lane fill** card on the Automation sub-tab. It is different from a *verifier-wait lane*, where the occupant still holds live work. See [howto/automation-lane-fill.md](howto/automation-lane-fill.md#a-running-automaton-with-no-live-work).

**Stalled merge** — a verified task, working on its own branch, whose merge into the Automaton branch has stayed pending with no progress for longer than `autonomous.lane_fill.stalled_merge_minutes` (a whole number of minutes, default `30`; `0` turns the rule off, so a pending merge keeps the lane however long it waits). The wait is timed from the moment the task records for its merge, else from when lane fill first saw it pending; that observation clock is kept in memory, so after a daemon restart a stalled merge keeps the lane for at most one more window. The Automaton stops holding its lane and is listed as a waiting Automaton with the reason code `merge_stalled`. What else happens follows the lane fill mode: `off` does nothing; `shadow` records the stalled merge as a decision and raises no alert and retries nothing; `on` raises one warning alert, "Task merge stalled: <task>", and retries the Automaton's merges (at most one retry per Automaton at a time, spaced a few minutes apart). A merge held for the operator (it carries a merge block, is held or needs review, or its Automaton is blocked) is alerted about but never retried. The alert fires again only after the merge succeeds, or the Automaton stops running, and the same task stalls again. It is settable from the Web UI (Settings → General → Autonomous), REST (`PUT /api/config`), MCP (`autonomous_config_set` with `lane_fill_stalled_merge_minutes`), the CLI (`datawatch config set`), the comm channel (`configure`) and the `config.yaml` key `autonomous.lane_fill.stalled_merge_minutes`; each refuses a negative, fractional or non-numeric value. See [howto/automation-lane-fill.md](howto/automation-lane-fill.md#a-running-automaton-with-no-live-work).

**Verifier-wait lane** — a lane whose occupant is a running Automaton but all of its running work is stalled waiting on a verification slot: work is in the lane, yet none of it can finish, and that is a state different from idle. The lane fill status shows it as the lane's `state: "waiting_verifier"` (only while `autonomous.lane_fill.count_verifier_wait` says the lane counts available; otherwise it reads occupied), with the `waiting_on_verifier: true` flag and the `wait_reason` text from the wait queue, for example "CI still running the checks". The CLI and comm channel lane line reads "waiting on verifier: <reason>", and the idle-lane alert names the wait and its reason before it fires. With `autonomous.lane_fill.count_verifier_wait` on (the default) it makes the Claude lane available to other work as well, because the waiting occupant is not spending Claude usage; set the key to `false` and the lane stays busy. In shadow mode, the default, the planner only records a would-start for such a lane and never starts one; in `on` mode a start on it goes through the normal start path. See [howto/automation-lane-fill.md](howto/automation-lane-fill.md).

**Verifier run cap** — `autonomous.verifier_max_run_seconds` (default 2700, forty-five minutes): the configured total wall-clock bound on one verifier session, in contrast to the no-progress watchdog (`autonomous.verifier_no_progress_timeout_seconds`), which only ends a session that shows no output file, no pane change, no live runner process and no held capacity slot. An explicit `0` keeps the cap off and is kept as `0`, never replaced by the default; a negative value is refused when the daemon loads the config. **The cap is enforced:** the verifier poll loop ends a session once its accumulated run time reaches the cap, ignoring progress signals and not counting time spent waiting for a capacity slot. The end frees the verifier's capacity lease, records a `verifier_run_cap` decision and audit entry with end reason `verifier_max_run`, and is retried once on the next backend in the verifier policy order. This keeps one long-lived verifier from holding its slot for hours. Run time is counted from the verifier's capacity grant, not the queue wait, and a cut run is retried once and is not a failed task. A run the cap ends is counted under the `run_cap` verifier category (not `infra`) and shows as `run_cap_ends` in the capacity API's `verifier` object. It is settable from the config file and every autonomous config surface (the Web settings page, the CLI, the REST API, the comm channel and the MCP config tool), each of which refuses a negative or non-whole value. See [operations.md — Verifier throughput](operations.md#verifier-throughput) and [howto/automaton-verifier-run-cap.md](howto/automaton-verifier-run-cap.md).

**Triage** — the record and the alert for every Automaton that ends up failed or blocked: where it stopped, how long it has been there, what the last session was ended for, and the retry budgets; a blocked record additionally carries the block reason and `reason_source`, the source that reason text came from (the Automaton's own block reason, a guardrail verdict's note, a task error or an ended session, a task held for operator review, or none), and one comm-channel alert per stuck episode after `autonomous.automation.triage_alert_minutes`. The same record is the response of `GET /api/autonomous/triage`, the `autonomous_triage` MCP tool, `datawatch autonomous triage`, `autonomous triage [automation-id]` on the comm channel, and the triage card in the PWA's Automaton detail view and the needs-attention card on the Automations Inbox.

**Shadow mode** — the lane fill's default mode, and the comparison mode of the repo CI watch (whose default is `off`: the watch does not run at all until you set a mode). In shadow the pass runs on every poll and records every decision it would make — the would-rerun, the would-cancel, the would-delete — with the evidence it used, but starts nothing, cancels nothing, deletes nothing and raises no alert. It is how the operator checks the rules against what the supervisor actually did before the daemon acts on them. The other modes are `off` (nothing runs) and `on` (the decisions are carried out).

**Held-state question** — the operator question the daemon raises on its own initiative when an Automaton sits in one of the five held states — `blocked`, `review_held` (failure-escalation review hold), `revision_requested`, `awaiting_decision` or `pending_insert` (a member insert waiting on operator approval) — waiting on a decision the daemon will not make for the operator. The question is blocking, asked by the daemon, and carries the state's real actions as options — resume, retry, retry on a stronger backend, change the task spec, cancel, approve-insert or reject-insert — plus the `hold` option, which is always offered and always the default, so nothing is ever decided silently; the recommended option comes from a fixed table keyed on the state and the failure category (`infra`, `content`, `scope` or `policy`), never a model, with one sentence of rationale. An open question is withdrawn by the daemon when the held state clears (the reason names the cleared state); an answer that names a state the item already left is refused, recorded once as a `stale` decision entry with the full answer, and closes the question. The mode is `autonomous.questions.held_mode`: `off` (nothing runs), `shadow` (the default; the pass computes each held Automaton and records one note per (item, state) on the Automaton's record, raises nothing) or `on` (one question per held (item, state), deduped by a stable key that survives a restart — never a second question for the same one while it is open). It lives under the operator-question channel switch (`autonomous.questions.enabled`, which the report endpoint names in its 503 while off), and `autonomous.questions.held_states` narrows the five states the pass covers (absent = all, explicit empty = none). It is read via REST (`GET /api/autonomous/held-questions[?prd_id=…&state=…]`, read-only), MCP (`autonomous_held_questions`), the CLI (`datawatch question held [--prd …] [--state …] [--json]`), the chat channel (`question held`, read-only) and the **Held-question** card on the Automation view, whose open entries each show the question's own text (cut to three lines; the row opens the full question), the recommended option and the default (*Held on the default: hold* when the record carries none), and a link that opens the Automaton detail (labelled *Open task in Automaton* when the entry names a task, *Open Automaton* otherwise); an answer goes in on any of the standard answer surfaces (PWA, REST, MCP `question_answer`, CLI `datawatch question answer`, chat) and lands once as a decision entry on the Automaton (quoted words, actor, surface, chosen option, result `applied`/`recorded`/`refused`/`stale`). See [howto/automaton-held-state-questions.md](howto/automaton-held-state-questions.md).

**Lane preference** — the setting on each Automaton that says which lane it may take: `gpu`, `claude`, or `any` (the default). The planner honours `gpu` and `claude` exactly. A person sets it; lane fill does not infer it from the work. The preference also decides the worker when nothing else names one: the order is task, story, PRD, Automaton, lane preference (the Claude or GPU lane backend), then the daemon default, so the capacity pool a task asks for is the pool of that backend.

**Degraded host** — a pool host whose loaded models lane fill could not read, but whose failure does not block the lanes. It is degraded only when the host holds no work (no lease, no Automaton placement, no running session naming it, and no lane placed on it) and has been unreachable for at least `autonomous.lane_fill.host_down_after_minutes` (default 10; `0` never treats an unreachable host as down, so every failure blocks). The lanes are then judged from the readable hosts and the host is listed under `evidence.degraded` with its name, the error text and the time it first failed, separate from the lane blockers and from `evidence.error`. A successful read clears it on the next poll. A failed session list always blocks. See [howto/automation-lane-fill.md](howto/automation-lane-fill.md).

**Eligibility reason** — the short fixed code that says why lane fill will not start a queued Automaton. There are eleven: `operator_hold` (an operator put the Automaton on hold; it is checked first and applies to every status lane fill would otherwise consider), `held` (a `HOLD` line, "do not run" or "sequenced last" in the spec), `sequenced_after_unshipped` (the spec says it follows an Automaton that is not completed), `stale` (no change for longer than `autonomous.lane_fill.stale_after_hours`, 72 by default), `proposal_not_confirmed` (an implementation Automaton whose proposal file has no `Confirmed: yes` line), `proposal_missing` (no readable proposal file named: the proposal is the `docs/plans/<name>.md` path named on an explicit proposal field or line in the title or spec — a path that is merely mentioned names none, and the plan folder's README is never one), `main_carries_work` (the default branch already has the work), `worktree_invalid` (a recorded worktree that is gone or not registered), `already_shipped` (the Automaton's own record says its work already shipped: a release and a merge commit are both set; it is checked right after the operator hold, it never starts, and the daemon marks it delivered instead of leaving it queued; a shipped record on a different Automaton, such as one it is sequenced after, never causes it), `operator_pause` (the operator has paused new work, globally or for the Automaton's project; it is checked right after `already_shipped`, so a shipped Automaton is reported as shipped and not as paused, and lane fill neither starts nor decomposes it until the operator resumes) and `plan_coverage_blocked` (a `needs_review` Automaton whose stored plan-coverage record fails the approve gate in `block` mode with no covering acknowledgment; lane fill never acknowledges on the operator's behalf). The two proposal rules never apply to a research, operational, personal or defect Automaton, nor to an Automaton whose title or spec states that its own job is to write the proposal — the authoring Automaton is not gated on the `Confirmed: yes` line of the proposal it is producing. The first rule that refuses an Automaton gives its reason. It appears as `eligibility_reason` on the `blocked` decision (so on the API and MCP surfaces), in brackets on the CLI and comm lines, and as the reason on the decision's row in the Events sub-tab of the web app's Automation Resources tab. A refused Automaton is never started in any mode and does not count as eligible work anywhere: not in lane fill's own idle alert (a lane whose whole waiting queue is refused shows the refusal line as its newest decision and raises no idle alert) and not in the supervisor watch. A check that cannot be made never refuses. See [howto/automation-lane-fill.md](howto/automation-lane-fill.md).

**Finished-work guard** — the daemon rule that finished work never sits unmerged. Setting `autonomous.finished_work` (`on` by default, `shadow` records only, `off` disables). Archive and cancel refuse an Automaton whose branch holds commits that are not on the default branch unless a **landing decision** (`keep` or `drop`, with a reason) is recorded; a branch that cannot be compared is refused too. A bounded **sweep** gives finished work older than a day an **owner** (who last acted on the record, else the operator) and one open question: land it by hand, leave it alone, or drop it. Work whose code tasks are all complete while docs or gate tasks remain is offered for landing and the remaining tasks stay separately retryable; nothing changes their status. A **daily digest** lists everything waiting, and a **weekly audit** reads every Automaton (cancelled and branchless ones too) and writes a decision record. See [howto/finished-work.md](howto/finished-work.md).

**Re-ask date** — the date the daemon asks again about a branch that was left alone. A `keep` decision or the answer "leave it alone" sets it 14 days out, "I will land it" 3 days out. If the work is still not on the default branch on that date the sweep raises a new question; the record keeps the dated `leave_alone` decision in its log.

**Shipped-work rule** — the daemon rule that closed or released work never runs. Setting `autonomous.shipped_refusal` (`on` by default, `shadow` logs only, `off` disables). The daemon refuses to start, approve or decompose an Automaton whose own record has a release and a merge commit (`already_shipped`), and any Automaton that is archived, cancelled, completed or rejected (`work_closed`). Queued work with a shipped record is dropped from every queue on each lane fill poll: it ends delivered (status completed, never cancelled), with the reason on the Automaton (`shipped_refusal` and a decision). Only the Automaton's own record counts, so a shipped record on another Automaton never blocks it, and an Automaton on hold or already running is never touched. An operator runs shipped work anyway with an audited force and a reason (`datawatch autonomous prd-run <id> --force --reason "..."`, chat `autonomous run <id> --force <reason>`, MCP `autonomous_prd_run` with `force` and `reason`, REST body `{force, reason}`); the force covers only that merge commit. An Automation that reaches a member whose own record already shipped does not start it and does not block: it marks the member delivered (completed, outcome `already_shipped`, a `member_delivered` decision) and goes on to the next member; only when a later member builds on that member's branch does it block with a clear message. See [howto/automation-lane-fill.md](howto/automation-lane-fill.md#shipped-work-never-runs).

**Per-lane-kind mode** — the lane fill mode set for one kind of lane. `autonomous.lane_fill.gpu_mode` and `autonomous.lane_fill.claude_mode` each take `off`, `shadow` or `on`; empty inherits the base `autonomous.lane_fill.mode`, which is why a fresh install is in shadow for both kinds. It lets the operator turn lane fill on for GPU lanes first, watch, and only then for Claude lanes, and go back for one kind without touching the other. Only the decisions of a kind whose mode is `on` are carried out; a kind set to `off` plans nothing. The status `modes` object shows the mode in effect for each kind, and each change is written to the audit log as `lane_fill_mode`. See [howto/automation-lane-fill.md](howto/automation-lane-fill.md).

**Lane counts from caps** — the rule, on by default (`autonomous.lane_fill.lane_counts_from_caps`), that the number of lanes of a type is the sum of the in-flight caps of the enabled LLM entries that run its backend, the same caps the capacity pools use. A Claude backend that admits one session at a time therefore gets one Claude lane however many `claude_lanes` are configured. The configured count is kept when an enabled entry is unlimited, when no enabled entry runs that backend, when the configured count is `0`, or when the switch is off. The status `lane_counts` object gives the configured count, the effective count and the source (`llm_caps` or `config`). See [howto/automation-lane-fill.md](howto/automation-lane-fill.md).

**Repo CI watch** — the daemon's standing watch over a repository's CI. On every poll it reads the head of `main` (or the default branch) and the newest `tag_count` release-shaped tags (`v<M>.<m>.<p>`, default 5) across every workflow that ran on those head commits, keeps one record per run, and classifies each run as `none`, `infra`, `security`, `policy`, `code_or_test`, `judging` or `hung` — `security` (a named workflow failed without green/infra outcome) and `policy` (a judge verdict, at most one judge call per poll, cached per run id and never re-judged) are record-only in every mode, `code_or_test` is the other verdict (a defect for the change's owner, no automatic action), and `judging` is the transient state of a run that had no verdict in this pass. In `on` mode it takes the budgeted actions only: at most one rerun of an `infra` run per run id while the infra retry budget (`retry_policy.infra`, unset reads 0 — no automatic reruns at all) holds, one cancel of a run pending past `hung_after_minutes` (default 60), and the retention deletion of superseded runs when `delete_superseded` is `delete`, on the pass after the green record named them. A non-green run that outlives `alert_minutes` (default 30) raises exactly one alert per episode, in `on` mode only — shadow records the would-dos and writes nothing, and raises no alert. For a watched repository whose origin belongs to a `git.hosts` entry of kind `github` or `gitlab`, the runs are listed through that host (`gh run list --repo` / `glab api .../pipelines`, 15-second bound) instead of the per-SHA `gh` path, and the poll is strictly read-only there — no rerun, cancel or deletion, and a `pending` run never becomes `hung` because the listing has no start time; a repository on any other kind of host keeps the unlisted state the pass has no data for. Off by default (`mode: off`). See [howto/automation-repo-ci-watch.md](howto/automation-repo-ci-watch.md).

**Security failure report** — the record the daemon keeps for a failed security run, so the finding is not buried and the Automaton line does not keep building on top of it. For a failed run on a security workflow it reads the failed log and stores one report with the dependency, the advisory identifier, the publication date (or `unknown`), the matching entry in `security/accepted-risks.yml` with its first-added, added and expires dates (or "not in the registry"), a fixed version when one is known, and a recommendation: `upgrade` (a fixed version is known), `keep` (no fix, and the registry holds an entry), `keep_no_entry` (no fix, and no entry — the operator adds one) or `unknown` (the advisory lookup or the registry could not be read). The report also names the action: `stopped` (the Automaton line carrying the failing commit was moved to blocked), `would_stop` (the same in shadow mode, where nothing is stopped) or `report_only`. The mode is `autonomous.automation.security_failure.mode`: `off`, `shadow` (the default; the report is stored, nothing is stopped and no alert is raised) or `on` (the line is stopped and one alert is raised at once). It is read on REST (`GET /api/autonomous/security-failures`), MCP (`security_failure_list`), the CLI (`datawatch autonomous security-failure`), the chat channel (`autonomous security-failure [run-id]`) and the Security failure card in the Automation view. The daemon never writes a suppression and never reruns a security run: an operator accepts a risk in `security/accepted-risks.yml` and regenerates the ignore file with `make security-registry-gen`. See [howto/automation-security-failure.md](howto/automation-security-failure.md).

**Supervisor watch** — the daemon's own version of the supervisor script (`scripts/supervisor-watch.py`). On every coordinator poll, with no model call, it runs eighteen checks — twelve that the script also runs, `priority_stall` and `backlog`, which both the script and the watch now report, plus `worktree_not_registered`, `verify_script_not_executable`, `main_checkout_automaton` and `archived_unmerged`, which only the watch has (the comparison shows them as *only the watch*) — and keeps one finding per kind and subject. The read surfaces also carry **per-band wait stats**: the waiting Automata grouped into three fixed bands — `high` (80 or more), `medium` (50 through 79) and `low` (below 50) — each band row with its waiting count, its oldest wait in whole minutes and how many of its waiters also satisfy the watch's priority-stall rule; the bands are fixed, and a band that waits nothing is omitted. The priority stall finding (`priority_stall`) names a queued Automaton at or above `autonomous.automation.supervisor_watch.priority_floor` (unset reads 80) once its waiting age is past `autonomous.automation.supervisor_watch.stall_threshold_minutes` (unset reads 30); the backlog finding (`backlog`) is one finding for the queue as a whole — every waiting Automaton plus every failed or blocked Automaton that still has work left — naming it highest priority first, then oldest. Both findings are report-only. Defect intake creates its Automatons with no project dir, so each runs isolated in its own worktree; the launch guard refuses approve and run for an Automaton whose project dir is the main checkout of the daemon's own repository and names the remedy — recreate the Automaton without a project dir — and `main_checkout_automaton` surfaces an open Automaton still pointed at it. The script has checks the watch does not cover yet (an unreachable model host, node balancing, capacity lease holds, placement, commits ahead of origin). The kinds are `tmux_restart`, `daemon_inactive`, `model_not_resident`, `task_failed`, `task_review_needed`, `automaton_not_merged`, `archived_unmerged`, `idle_lane_eligible_work`, `worktree_no_gomod`, `worktree_not_registered`, `worktree_silent`, `verify_script_not_executable`, `main_checkout_dirty`, `main_checkout_automaton`, `ci_red`, `ci_stuck`, `priority_stall` and `backlog`. It is report-only in every mode: it never merges, starts, holds, cancels or retries an Automaton and never restarts a service. Its node probe, which reads the models a node has resident, verifies the node's certificate (see [Compute node TLS verification](#compute-node-tls-verification)); a node whose certificate fails stays unknown and raises no finding. The mode (`autonomous.automation.supervisor_watch.mode`) is `off`, `shadow` or `on`, and an unset mode reads `shadow`: in shadow the findings are recorded and a finding open for the alert time is marked *would alert*, but no alert is raised; in `on` one warning alert is raised per episode through the existing alert channels. The first sighting of a finding about one Automaton is also noted on that Automaton's triage record. With the script's record file configured, every pass compares the two sets of findings and keeps the result; a day counts toward the *days of full agreement* only when every comparison that day agreed, and one full day of agreement is the bar for retiring the script. A check that cannot tell reports nothing, and a missing or stale script record is never a disagreement. Read on REST (`GET /api/autonomous/supervisor-watch`), MCP (`supervisor_watch_list`), the CLI (`datawatch autonomous supervisor-watch`), the comm verb `autonomous supervisor-watch` and the Supervisor watch card in the Automation view. See [`howto/automation-supervisor-watch.md`](howto/automation-supervisor-watch.md). The repairs the daemon makes for these findings, and the ones that always stay with an operator, are in [howto/automaton-housekeeping-repairs.md](howto/automaton-housekeeping-repairs.md); the daemon runs the repair pass on the supervisor loop's schedule in `report` mode by default, so the watch and the pass report until an operator turns a group to `on`.

**Held Automaton (operator hold)** — an Automaton an operator put on hold on purpose, for example until after a release; its status is `held`. It waits by decision: not blocked, not failed, not in review, and nothing starts it (approve, run, decompose, reset and requeue refuse it and name the reason; lane fill skips it with the eligibility reason `operator_hold`; chains, repairs, the loop, watchdogs, boot passes, retries and merge passes leave it and its tasks alone). A hold records a required reason (500 characters at most), an optional free-text condition that is never evaluated, an optional future look-again time, the status it came from, and who and when. A hold may start from `draft`, `needs_review`, `approved`, `revisions_asked`, `paused`, `blocked` or `failed`; extend replaces the fields you give; release restores the earlier status (and a blocked reason). Passing of the look-again time never changes the status. The hold, extend and release operations and the held group have REST routes, the MCP tools `autonomous_prd_hold`, `autonomous_prd_release_hold`, `autonomous_held` and `autonomous_held_migrate`, the CLI verbs `datawatch autonomous prd-hold`, `prd-release-hold`, `held` and `held-migrate`, the comm verbs `autonomous hold`, `release-hold`, `held` and `held-migrate`, and the web app surface (the Hold button, the Inbox **Held Automata** group and the `held` status). There is no configuration key for hold: it stands or stands down on operator action only. See [howto/automaton-hold.md](howto/automaton-hold.md).

**Supervisor fresh session** — a supervisor session that datawatch starts new on a schedule instead of keeping one alive, so it never runs on a full or stale context. Each one starts from a handover with three layers: the build context (profile, tool servers, project directory, model), the rules (the default build supervisor pack that ships in `packs/build-supervisor/`, then the project rules pack, then the project rules file, with the later ones winning) and the direction (each plan in order, its next piece of work and every Automaton with its state and block reason). It also reads the pinned **state record** — decisions, open work, what was last checked and the corrections you gave, each edit stamped with who and why — and every correction carries a class: `state` (expires with its situation), `standing_rule` (promoted to the project rules pack) or `product_defect` (filed as an Automaton, workaround in the handover); recording the same correction more than twice raises a `correction.repeated` finding. A session is replaced when its context size or age passes its limit: the new one starts and answers before the old one ends. The model is named at set-up and is always Sonnet or Haiku, never Opus. Set up with `datawatch supervisor-setup run`, `POST /api/supervisor-setup`, the MCP tool `supervisor_setup` or the chat verb `autonomous supervisor-setup`; the settings and their defaults are under `supervisor_fresh_sessions` in the config reference. See [howto/supervisor-fresh-sessions](howto/supervisor-fresh-sessions.md).

**Housekeeping repair** — a fix the daemon makes by itself to a supervisor watch finding, only after the finding has stayed open past a grace period. The daemon runs the repair pass on the supervisor loop's schedule (each idle tick), and every group defaults to `report`, so it records what it would do and acts on nothing until an operator turns a group to `on`; a repair never merges, ships, deletes a session, kills or signals a session, changes a model or backend, or touches a security workflow. Every finding kind (`STUCK`, `MERGE WAIT`, `MERGE READY`, `JUDGE BLOCKED`, `NEW failed task`, `BACKLOG`, `IDLE lanes`, `SILENT`, `LEASE`, `WORKTREE`, `TMUX`, `PLACEMENT`, `NODE`, `MODEL`, `MAIN`, `PRD`, `DAEMON`, `CI STUCK`, `CI RED`, `priority_stall`, `backlog` and `archived_unmerged`) has a row in the table in [howto/automaton-housekeeping-repairs.md](howto/automaton-housekeeping-repairs.md) giving its cause, whether the daemon can fix it safely alone, what it does and when, and what stays for an operator.

**Repair group** — one of the five settings that switch the housekeeping repairs: `autonomous.housekeeping_repairs.task_chain` (`NEW failed task`, `BACKLOG`, and the three task chain repairs, see the Task chain repair entry), `.advance_judge` (`STUCK`, `MERGE WAIT`, `JUDGE BLOCKED`, and the three advance and judge repairs, see the Advance and judge repair entry), `.lane_fill` (`IDLE lanes`, `LEASE`, `PLACEMENT`, and the lane fill repair, see the Lane fill repair entry) `.leftovers` (`WORKTREE`, and the leftover lease, worktree and tmux name of a finished Automaton, see the Leftover release repair entry) and `.planning` (a draft with no stories whose planning failed for a non-retryable reason, see the Planning repair entry). Each takes a repair mode. A fifth setting, `.grace_seconds` (default 300), is the wait every group applies before it acts. Kinds outside the five groups are reported in every mode. The settings are accepted from the config file and on every config set path (`config set` on the CLI, `PUT /api/config`, MCP `config_set`, the comm `configure` verb and Settings in the web UI), and the daemon runs the repair pass on the supervisor loop's schedule.

**Task chain repair** — one of three repairs in the `task_chain` repair group for stuck Automaton task chains: the *dependent reset* (a task that failed only because a task it depends on failed gets a plain reset, never a force-cancel, once every dependency is done), the *stale docs mirror* repair (a final gate that failed because the gitignored embedded docs mirror `internal/server/web/docs` is out of date runs `make sync-docs` in the Automaton worktree, then the gate is retried once) and the *leaked file* repair (a leaked marker or stray file that blocks a finish is removed with `git clean -f`, then the task is retried once, only when the file is untracked and inside the Automaton worktree). Each retries at most once; when the failure reason is ambiguous, a second retry would be needed or the file is tracked, the repair refuses, records a skipped entry and raises one alert. The daemon runs them on the supervisor loop's schedule; in `report` (the default) each records what it would do and changes nothing. See [howto/automaton-housekeeping-repairs.md](howto/automaton-housekeeping-repairs.md).

**Advance and judge repair** — one of three repairs in the `advance_judge` repair group for finished and review-blocked Automata: the *advance* (an Automaton that is running with every task finished is moved to its next state by the normal completion step after the grace, never while a live run holds it), the *judge repair* (an Automaton blocked by the rules review with no repair task gets one repair task made once from the review's real findings, then reset; the fact is recorded so it is never made again) and the *merge report* (an Automaton waiting to merge or ready to merge is only reported, never merged). Each refuses as an unsafe finding, with one alert, when a task is not finished, the judge repair was already made once, or the review recorded no findings. The daemon runs them, and each makes at most one finding per Automaton from a single check of its state, so a finding never mixes a finished task with a not-finished one. See [howto/automaton-housekeeping-repairs.md](howto/automaton-housekeeping-repairs.md).

**Lane fill repair** — the repair in the `lane_fill` repair group for an idle lane with eligible work waiting: after the grace it starts the next eligible Automaton through the normal run path, never changing its model or backend. It refuses as an unsafe finding, with one alert, when no eligible work exists, capacity is full, the lane is at its inflight cap, the usage guard is paused, or the only eligible Automata need a backend the lane may not use. It is written and tested; the daemon does not build it yet, so turning the group to `on` does not activate it (the group's lane-count correction does run). See [howto/automaton-housekeeping-repairs.md](howto/automaton-housekeeping-repairs.md) and [howto/automation-lane-fill.md](howto/automation-lane-fill.md).

**Repair mode** — the value of a repair group: `off` (the group does nothing), `report` (the default, and what an unset value reads: it records what it would repair and acts on nothing) or `on` (it repairs once the grace has passed). Any other value is refused.

**Leftover release repair** — the three repairs in the `leftovers` repair group that release what a finished Automaton leaves behind: its capacity lease, its worktree and its tmux name. Each releases one leftover only when the safe-to-delete check passes (the Automaton is finished, holds no uncommitted work, has no unlanded commits and has no live session), re-read just before the release. When a fact fails or the check cannot be answered, the repair refuses as an unsafe finding, with one alert, and nothing is released. It never kills, ends or signals a session, merges, ships, changes a model or backend, or touches a security workflow. The daemon does not build them yet, so turning the group to `on` runs the group's disk cleanup sweep but not the release repairs. See [howto/automaton-housekeeping-repairs.md](howto/automaton-housekeeping-repairs.md).

**Planning repair** — the repair in the `planning` repair group for a draft Automaton with no stories whose recorded planning failure is not retryable. When every spec lint finding on the draft is one the daemon can reword, then after the grace it rewrites the spec with the suggestions, applies it as a revision and plans again, the same path as the *Fix the wording and plan again* answer of the planning failure question (see Planning failure question). When a finding cannot be reworded (a private address, a worktree path) it refuses as an unsafe finding, with one alert, and leaves the spec alone. A retryable failure is never touched. In `report` mode it only records what it would do. It never touches a session, merges, ships, or changes a model or backend. See [howto/automaton-housekeeping-repairs.md](howto/automaton-housekeeping-repairs.md) and [howto/automaton-decompose-lint.md](howto/automaton-decompose-lint.md#6a-planning-failures-what-you-see-what-the-daemon-fixes-what-it-asks).

**Planning repair group** — the repair group `autonomous.housekeeping_repairs.planning`, with the modes `off`, `report` (the default) and `on`. In `on` it takes the *Fix the wording and plan again* answer for a draft Automaton with no stories and a non-retryable planning failure, after the grace, only when every spec check finding can be reworded. See the Planning repair entry and [howto/automata-planning-failures.md](howto/automata-planning-failures.md#the-planning-housekeeping-group).

**Unarchive repair** — the one-time repair in the `unarchive` repair group for an Automaton that the daemon archived while its branch still carried unmerged commits (an archive that slipped past the merge check). After the grace it moves the Automaton back to the status it had before the archive — completed, or cancelled if it had been cancelled before archiving, read from the decision log — and records one decision (kind `unarchive_unmerged`, actor `housekeeping-repair`) and one alert. It never touches an Automaton the operator archived or cancelled, one that is not archived anymore, or one whose branch state cannot be checked (no worktree, no repo, no upstream base): those are recorded as skipped with one alert instead, and it never kills or signals a session, never merges or ships, and never changes a model or backend. It runs once: after the pass it records that it has run and never runs again, so in `report` it only shows what it would do until that one pass, and `off` does nothing. See [howto/automaton-housekeeping-repairs.md](howto/automaton-housekeeping-repairs.md).

**Housekeeping repairs view** — the read-only report of the housekeeping repairs: per repair group (`task_chain`, `advance_judge`, `lane_fill`, `leftovers`, `planning`, `unarchive`) the mode in effect, what a report-mode pass would do (`would`), what an on-mode pass did (`did`), and the cases that failed or were skipped as unsafe, each with what was found, what was done and how to undo it. Available as `GET /api/autonomous/housekeeping-repairs`, the `housekeeping_repairs` MCP tool, `datawatch autonomous housekeeping-repairs`, the chat verb `autonomous housekeeping-repairs`, and the Housekeeping repairs card on the Automation view's Housekeeping sub-tab. Reading it repairs nothing, raises no alert and changes no mode. See [howto/automaton-housekeeping-repairs.md](howto/automaton-housekeeping-repairs.md).

**Dormant feature** — a shipped Automata setting whose current value is off or in report or shadow mode: the scan fix loop, the scan rules grader, the scope breadth check, the default quality gates, the remote branch reap, notification forwarding, the merge-until-conflict loop job and the merge pending mode. The read lists, for each one, its key, its state (`off` or `shadow`), its default, a one-sentence purpose and whether anything on record shows it was ever exercised (`ever_exercised`, `last_exercised`, `evidence`), so the operator can see in one place what is finished but not live. It only lists: it never changes a setting, and settings that decide which LLM, route, judge, verifier, repository or credential is used are not listed. Available as `GET /api/autonomous/dormant-features`, the `autonomous_dormant_features` MCP tool, `datawatch autonomous dormant-features [--json]` and the chat verb `autonomous dormant-features`. See [operations](operations.md#dormant-features-and-the-refusal-ledger).

**Closed outcome** — the class a finished Automaton ended in, kept on its own record as `closed_outcome`: `delivered` (its work is on the base branch, by itself or by the twin named in `delivered_via`), `superseded` (replaced by the Automaton named in `delivered_via`), `dropped` (thrown away on purpose) or `held` (waiting by decision; never archived). Cancel, reject and archive record it in the same step: a shipped record means delivered, a rejection or a cancel with work left undone means dropped. The archive step refuses a finished Automaton whose outcome the daemon cannot prove, one whose outcome is held or whose hold is not met, and one whose spec declares a hold and has no recorded outcome. See [howto/closed-work-outcome.md](howto/closed-work-outcome.md).

**Closed record** — an Automaton that is cancelled, rejected or archived, or that carries any recorded closed outcome, whatever its status reads. No restart or periodic recovery touches it: no protected-paths release, task reset, boot resume, repair, answer routing, merge retry or lane fill. A record with an outcome that reads running gets its closed status back, with a `closed_record_restored` decision naming why. Only the operator reopens one (their own task reset, reset to draft, or the force past the shipped-work rule), which clears the outcome and records `closed_outcome_cleared`. It has no setting. See the closed work how-to.

**Closed outcome rule** — the setting `autonomous.closed_outcome_archive`: `on` (the default) makes the archive step refuse as described under Closed outcome, `shadow` only logs what it would refuse, `off` disables the rule. It applies live.

**Delivered via** — the `delivered_via` field of an Automaton: the id of the twin Automaton that delivered its work (outcome delivered; the twin must carry a shipped record) or of the one that replaced it (outcome superseded).

**Deploy record** — the record of a release deploy on an Automaton (`deploy`): the version, the time, the host and the health check result (`passed`, `failed` or `unchecked`). The deploy-after-ship step writes it onto every Automaton shipped in the release it deployed; `POST /api/autonomous/deploy/record`, the `autonomous_deploy_record` MCP tool, `datawatch autonomous deploy-record` and the chat verb `autonomous deploy-record` write it for a deploy done by hand.

**Closed work check** — the standing check of closed work, mode `autonomous.closed_outcome_check` (`shadow` default, `on` also alerts once per finding, `off`). It flags Automata closed with no outcome, completed work with no shipped record, shipped work with no deploy record on a host that deploys after ship, a deployed version older than the shipped one, and work closed as delivered whose intent proof (the files and symbols its spec names on the base branch, plus task completion) no longer holds; the proof re-runs once a day. Read it with `GET /api/autonomous/outcome-check`, the `autonomous_outcome_check` MCP tool, `datawatch autonomous outcome-check`, the chat verb `autonomous outcome-check` or the Closed work check card on the Housekeeping sub-tab. It never changes an Automaton.

**Refusal ledger** — the bounded record (500 entries, kept in `refusal_ledger.json` beside the other Automata stores) of Automaton steps that were refused for a stated reason: one entry per Automaton and step, with the latest reason, first and last seen and a repeat count, newest first. A repeat folds into its entry instead of adding a new one. It is shown as `refusals` on the housekeeping repairs read (REST, MCP `housekeeping_repairs`, CLI and chat). Nothing in the daemon records into it yet, so the list is empty until the archive, repair and merge steps are wired to it.

**Finished-work findings** — the bounded store (200 entries, `finished_work_findings.json`) the finished-work sweep writes into: per Automaton the status, the named cause and the action taken, newest first. It is shown as `finished_work` on the housekeeping repairs read. The sweep step that fills it is not in the daemon yet, so the list is empty.

**Repair grace period** — how long a finding must stay open before a repair acts on it: `autonomous.housekeeping_repairs.grace_seconds`, a whole number of seconds, default 300, `0` for no grace, one value for all six groups. A repair never acts on the poll that first sees a finding, and a finding that closes by itself during the grace is never repaired. See [howto/automaton-housekeeping-repairs.md](howto/automaton-housekeeping-repairs.md).

**Git host** — one named entry in the `git.hosts` list: a host the daemon may push to, with its `kind` (`github`, `gitlab`, `gitea`, `bitbucket` or `git`), a `base_url` (required for the self-hosted kinds), a credential source and a tag-signing setting (`sign_tags`, `signing_key`). The `name` is a unique lowercase slug and is the key used in `git.hosts.<name>.<field>` and in the connection-test path — a field write changes a host's fields but never its name; renaming is a whole-list write. Until the vault lookup reaches the push/clone callers, a `vault` host's push or clone fails with "no secret resolver" — push targets should use `host_login` (see the howto's current-build note). See [Connect datawatch to your git host](howto/connect-git-host.md) and [Git hosts](#git-hosts).

**Host readiness** — the check datawatch runs on a git host to say whether it can use it: `gh auth status` or `glab auth status` for GitHub and GitLab (login, account, and the `repo` + `workflow` or `api` scope), and `git ls-remote` against the base URL for the plain kinds. It reports a plain-language problem and a fix command, and never a token. It runs within a 15-second limit per command and is read on `datawatch git-hosts`, `GET /api/git/hosts` and the MCP tool `git_hosts_status`. It checks the login on the datawatch machine, not a `vault` token. See [Connect datawatch to your git host](howto/connect-git-host.md#what-the-readiness-check-does).

**Credential source** — where a git host's token comes from: `host_login` (the default and recommended: the login already on the host's machine), `vault` (a secrets-vault entry named by `vault_secret`, placed in one host command's environment), or `broker` (the credential broker supplies the token to worker sessions; datawatch adds none). The source is recorded in the audit log; the token never is. See [Connect datawatch to your git host](howto/connect-git-host.md#setup).

**Vault token** — the secrets-vault entry a `vault` git host names, and the environment variable datawatch sets for one host command with its value: `GH_TOKEN` for `github`, `GITLAB_TOKEN` for `gitlab`, and `DATAWATCH_GIT_TOKEN` for `gitea`, `bitbucket` and `git`. The value is never written to the daemon's environment, the config, an error or a response. See [the vault token environment variables](howto/connect-git-host.md#the-vault-token-environment-variables).

<a id="supervisor-loop"></a>
## Supervisor loop {#supervisor-loop}

**Supervisor loop** — the daemon's standing loop that keeps every Automaton moving without an operator typing the next command. Each pass runs a set of mechanical steps (no model call) and a few model jobs, and keeps a run record for each. A model job runs as a one-shot call or a session, on the backend and model you set; when none is set it uses the decision assistant's configured model, and when there is none the job escalates to you instead of quietly falling back to a hosted model. A failed check is retried, and after enough failures the job escalates. The Supervisor loop card in the Automation view lists the jobs (name, style, backend, model, last outcome, escalation count) and the most recent runs. See [`howto/supervisor-loop.md`](howto/supervisor-loop.md). The settings are under Settings → Automata → Supervisor loop; each one has its entry below, with its help link. In the config file the keys sit under `autonomous.supervisor_loop`.

<a id="supervisor-loop-card"></a>
- **Supervisor loop card** — the card on the Events tab of the Automata view (shown only once the loop has run; the Setup sub-tab it used to sit on was removed). It lists the jobs (name, style, backend, model, last outcome, escalation count) and the most recent runs, and links to the [how-to](howto/supervisor-loop.md). Help link: [`#supervisor-loop-card`](#supervisor-loop-card).
<a id="supervisor-loop-settings-group"></a>
- **Supervisor loop settings group** — the group under Settings → Automata that holds every `supervisor_loop` setting below. Help link: [`#supervisor-loop-settings-group`](#supervisor-loop-settings-group).

<a id="supervisor-loop-enabled"></a>
- **`supervisor_loop.enabled`** — Master switch for the whole loop. On by default.
<a id="supervisor-loop-close-finished-records-enabled"></a>
- **`supervisor_loop.close_finished_records.enabled`** — Turns the close finished records step on or off. The step closes the records of Automata that have finished: completed and rejected ones are archived on the next idle pass; a cancelled one is archived once the `autonomous.archive_grace_minutes` grace has passed since its most recent cancel (the legacy [`supervisor_loop.archive_cancelled_after_minutes`](#supervisor-loop-archive-cancelled-after-minutes) key is still accepted on every surface but no longer drives the step). A finished one — completed, rejected or cancelled — whose branch holds commits that are not on the base branch is not archived by the loop: you archive it, its work is merged, or its branch is deleted; `autonomous.archive_keep_unmerged` (unset reads `true`) releases that guard for completed and rejected ones only. The loop's archives are recorded with the actor `supervisor-loop`; your own archive is unaffected and is recorded with the actor `operator`. On by default.
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
- **`supervisor_loop.archive_cancelled_after_minutes`** — The grace, in minutes, before a cancelled Automaton is archived: it is archived only once the time since its most recent cancel reaches this value (0 archives it at once). It exists so a cancelled branch can still be inspected, or re-cancelled to push it back, before it disappears. Separately from this grace, the loop never archives a finished Automaton whose branch still holds commits that are not on the base branch. Default 60. Still accepted on every surface, but the close step reads `autonomous.archive_grace_minutes` (same default and rules) instead; this key no longer drives the step.
<a id="autonomous-archive-grace-minutes"></a>
- **`autonomous.archive_grace_minutes`** — The grace, in minutes, before a cancelled Automaton is archived by the supervisor loop's close step, measured from its most recent cancel: a whole number ≥ 0 (default 60; `0` archives it at once; a negative or non-whole value is refused). Read live on every pass, so a change needs no restart. See [operations.md](operations.md#archive-grace-for-cancelled-automata-autonomousarchive_grace_minutes) for the surfaces.
<a id="autonomous-archive-keep-unmerged"></a>
- **`autonomous.archive_keep_unmerged`** — A boolean the close step reads: while it is `true` (the default; unset reads `true`) the loop keeps a completed or rejected Automaton whose branch holds commits that are not on the base branch, and keeps a cancelled one whatever it says. Set to `false` and a completed or rejected Automaton with an unmerged branch is archived in the pass it reaches its state. See [operations.md](operations.md#keep-unmerged-branch-work-on-archive-autonomousarchive_keep_unmerged) for the surfaces.
<a id="supervisor-loop-lane-count-min"></a>
- **`supervisor_loop.lane_count.min`** — The lowest lane count the loop may set.
<a id="supervisor-loop-lane-count-max"></a>
- **`supervisor_loop.lane_count.max`** — The highest lane count the loop may set. 0 means no bounds, so the lane count step moves nothing.
<a id="supervisor-loop-wake-enabled"></a>
- **`supervisor_loop.wake.enabled`** — Turns the event wake on. When on, a lifecycle event of a kind listed in [`supervisor_loop.wake.kinds`](#supervisor-loop-wake-kinds) starts a supervisor pass at once instead of waiting for the next scheduled check; the schedule stays as the fallback. Off by default, so the supervisor keeps running on its schedule alone until you switch it on.
<a id="supervisor-loop-wake-kinds"></a>
- **`supervisor_loop.wake.kinds`** — The event kinds that wake the supervisor, as a list. Each kind must be one a subscription can filter on (`datawatch autonomous event-kinds` lists them); an unknown or duplicate kind is refused, and a hand-edited config with one falls back to the default list. The default list is a blocked Automaton (`prd.blocked`), a task needing review (`task.review_needed`), a failed task (`task.failed`), a failed final gate (`task.final_gate_failed`), every judge verdict (`judge.verdict_block`, `judge.verdict_warn`, `judge.verdict_pass`), every task of an Automaton done or cancelled (`automaton.tasks_done_or_cancelled`), an Automaton stuck running (`automaton.stuck_running`), a task waiting too long for capacity (`task.capacity_wait_too_long`) and a burn guard rung change (`burn_guard.rung_changed`). Over the CLI and the comm channel the list is written comma-separated; the list may not be empty while the wake is on, and holds at most 64 kinds.
<a id="supervisor-loop-wake-coalesce-seconds"></a>
- **`supervisor_loop.wake.coalesce_seconds`** — The quiet window, in seconds, after a wake: further wake events arriving within it are handled by the same pass, so a burst of events does not start a pass per event. Between 0 and 3600; 0 wakes on every event. Default 30.
<a id="supervisor-loop-wake-fallback-interval-minutes"></a>
- **`supervisor_loop.wake.fallback_interval_minutes`** — How often, in minutes, the supervisor still runs when no event wakes it. Between 1 and 1440. The default of 20 equals the scheduled check, so turning the wake on changes nothing about the schedule.
<a id="supervisor-loop-wake-session"></a>
- **`supervisor_loop.wake.session`** — The designated supervisor session: the full or short id of the one session a wake may type into. Default empty, which designates none, so a wake runs its pass and types nothing. A launch role or the `role:supervisor` tag never makes a session the target. At most 128 characters, no spaces. Read live. The wake status shows it with the current target and why. See [Which session a wake types into](howto/supervisor-loop.md#which-session-a-wake-types-into).
<a id="supervisor-wake-set"></a>
- **`supervisor-wake` subscription set** — The built-in set of lifecycle subscriptions the daemon registers while [`supervisor_loop.wake.enabled`](#supervisor-loop-wake-enabled) is on, one per kind in [`supervisor_loop.wake.kinds`](#supervisor-loop-wake-kinds). Matching events are folded within the quiet window and typed into the [designated supervisor session](#supervisor-loop-wake-session) as a `supervisor wake: <kind> automaton=<id> task=<id>` line (a priority stall entry also carries `blocker=<code>`). An identical batch is not typed again within the fallback interval, and priority stall entries only when the stall list changes. Nothing is registered while the wake is off, and your own subscriptions are never changed. Nothing is typed when no session is designated, the designated one is not running or waiting for input, its output cannot be read, or its output ends with a completion marker. Status: `GET /api/supervisor-loop/wake`, `datawatch supervisor-loop wake`, `supervisor wake` on the comm channel, the `wake` object of the `supervisor_loop_stats` MCP tool, and **Settings → Automata → Supervisor wake**.

Terms used by the [event-driven supervisor how-to](howto/automaton-event-driven-supervisor.md):

<a id="event-kind"></a>
- **Event kind** — the name of one sort of lifecycle event, such as `task.failed` or `automaton.stuck_running`. The kinds are a closed set held in the [event kind registry](#event-kind-registry-entry); a subscription, the notify set and `supervisor_loop.wake.kinds` may name only a kind in it, and an unknown kind is refused when you save. Built-in kinds are fixed; an extension adds its own at start-up with `autonomous.RegisterEventKind` (lowercase dotted name, a one-line description of at most 200 characters, one of the groups Automaton, Task, Review, Capacity or Budget). Registering a kind makes it valid and lists it on every surface; it does not emit it. List them with `datawatch autonomous event-kinds`.
<a id="subscription"></a>
- **Subscription** — one stored rule that says "when an event matching this filter fires, deliver it to this sink". The filter names kinds and/or a level, and may narrow to one Automaton, one task or one peer. A subscription never widens what the notify set allows. It is different from the *wake*, which is a built-in set the daemon keeps for the supervisor, and from an *MCP resource subscription*, which belongs to one MCP session and stores nothing. See [lifecycle event subscription](#lifecycle-event-subscription).
<a id="sink"></a>
- **Sink** — where a subscription delivers: `comm` (a messaging channel), `session` (a datawatch session), `topic` (the push topic bus), `alert` (the in-app alert store), `mcp_queue` (a named queue a client polls), `webhook` (an outbound HTTPS call) or `peer` (another federation peer). An extension may register a further sink type with `autonomous.RegisterSinkType`; the daemon accepts and stores it, but its delivery engine does not deliver to it, so the extension must deliver the events itself.
<a id="wake"></a>
- **Wake** — starting a supervisor pass because an event arrived instead of waiting for the next scheduled check. It is controlled by `supervisor_loop.wake.enabled`, `supervisor_loop.wake.kinds`, `supervisor_loop.wake.coalesce_seconds` and `supervisor_loop.wake.fallback_interval_minutes`, and on each wake runs a supervisor loop pass at once and, when the designated supervisor session can take input, delivers a `supervisor wake: <kind> automaton=<id> task=<id>` line into it. The daemon registers the wake when `supervisor_loop.wake.enabled` turns on, within a few seconds and with no restart.
<a id="coalesce"></a>
- **Coalesce** — fold events that arrive close together into one delivery. For the wake it is the quiet window `supervisor_loop.wake.coalesce_seconds` (0 to 3600, default 30; 0 wakes on every event): events inside the window share one pass, and events that arrive during a pass wait for the next one. For a stored subscription it is the `coalesce` flag, which batches human-facing sinks (comm and alert) per quiet window.
<a id="fallback-check"></a>
- **Fallback check** — the scheduled supervisor pass that still runs when no event wakes the supervisor, so a missed or never-sent event is caught on the next pass. Its interval is `supervisor_loop.wake.fallback_interval_minutes` (1 to 1440, default 20). While the wake is on the scheduled check runs a pass only when that many minutes have passed since its last pass; with the wake off it runs on every idle tick, as before. The scheduler reads it live.
<a id="peer-event"></a>
- **Peer event** — a lifecycle event that came from a federation peer: one the peer forwarded to this daemon (it carries `peer`, the sender's registered name, and a `forward` block with the origin host and hops), or, in the pull direction that no setting switches on yet, one read from the peer's pending-events queue. A peer event feeds notification sinks, the wake and MCP resource subscriptions; it is never written to Automaton state and never forwarded on. Only lifecycle events cross a peer: session output, secrets and configuration do not. See [Peer forwarding](#lifecycle-subscriptions--outbound-webhook-and-peer-forwarding).

**Tag subtree count** — the number of distinct Automata under a tag, counting Automata that carry the tag or any tag below it. An Automaton that carries two of the tags in the subtree is counted once. It is the main count the tag browser shows. Sessions have their own subtree count, which also includes the tags a session inherits. The direct count (Automata carrying exactly that tag) is available as a tooltip when `autonomous.tags.direct_counts_visible` is on. See [howto/automata-tags.md](howto/automata-tags.md).

**List modes and group-by** — the PWA controls on the flat Automata and Automation lists. The mode (*List*, *Tiles*, *Compact*) defaults from the window width until chosen, and the remembered mode is validated against those three known values whenever it is read: a stale value from an older version or hand-edited storage falls back to the window-width default, so stale browser state cannot break the toggle; the group-by is *No grouping*, *tag*, *state*, *node*, *project* or a placement axis (`automation:`, `stage:`, `kind:`, `compute:`, `peer:`, `backend:`, `lane:`, `role:`, `project:`). A group header shows a collapse toggle, a count and a state mix, and a group pages 50 items at a time. The choices are stored in the browser's local storage per list and device, not on the daemon, so they have no setting, API, CLI or MCP surface. The Sessions list shares the group header, collapse and paging when grouped by a placement axis. On the Sessions list the *project* axis groups by the project named in the session's `project:` placement tag (an Automaton task session takes its owning Automaton's project), falling back to the working folder only when no project is known; the PWA and the grouped read resolve it the same way. The Sessions group choice is remembered on the device and survives a reload and list refreshes; the **Group by** picker is the one control for it. On the Sessions list a group header on the Automaton, project and node axes shows the record's human title with the id as secondary text, the node axis puts a federation peer's sessions under the peer's name and this daemon's under its hostname (the Automaton axis also reads peers' sessions), the project axis ignores a trailing slash on the folder, the ungrouped group always sorts last, and **Compact** is the same tile grid as Tiles drawn at a smaller size (same content and ⋯ menu, never rows). See [howto/sessions-tiles-and-grouping.md](howto/sessions-tiles-and-grouping.md) and [howto/automata-tags.md](howto/automata-tags.md).

**PWA update (service worker versioning)** — how an installed PWA picks up a new datawatch release without manual cache clearing. The service worker is registered at `/sw.js?v=<release>` — a distinct worker URL per release (the page registers the versioned URL at once from the release number stamped into its own HTML, and registers the bare URL only when no release number is known at all) — and its cache name is derived from the same version, so each release owns its own cache and the new worker's activation purges the older ones. A genuine upgrade reloads the affected page at most once, and only when the page is not mid-thing (typing in an input, the terminal focused, an unsent draft, or still active within the last 30 seconds); otherwise an "Update available — tap to reload" banner waits and reloads as soon as the page goes idle. A no-op re-activation (no stale cache, or only the cache of the same release) never reloads a tab, and the update bar and its idle reload appear only when the release the page was loaded with differs from the release the daemon reports; that idle reload is taken once per browser tab session. The manual escape hatch for a stuck cache remains `forcePWAUpdate()` in the browser console. On origins where the service worker cannot register (for example an untrusted self-signed certificate), none of this applies and a normal reload is enough. There is no setting, API, CLI or MCP surface. See the *PWA updates on a datawatch release* section of [operations.md](operations.md#pwa-updates-on-a-datawatch-release) and [pwa-setup.md](pwa-setup.md).

**Attention group** — the set of Automata that need the operator now: every Automaton whose status is in `autonomous.tags.attention_statuses` (blocked, needs review and paused by default), plus any Automaton with a task held for review. It is the pinned "Needs attention" section above the tag browser, and the view that opens by default when something is in it. See [howto/automata-tags.md](howto/automata-tags.md).

**Inherited tag** — a tag a session gets from the Automaton that started it. It is shown as a dashed chip marked *inherited*, is read-only on the session, and changes when the Automaton's tags change. A session's own tags are separate and are the only ones set on the session itself. See [howto/automata-tags.md](howto/automata-tags.md).

**Automatic tags (auto tags)** — the three tags the engine keeps on every Automaton while it runs: one for its Automation, one for the member's stage, and one for its kind. Their shape comes from `autonomous.auto_tags` — `enabled` (on by default), `automation_template` (default `automation:{automation}`), `stage_template` (default `stage:{stage}`), `kind_template` (default `kind:{kind}`) and `kind_labels` (default `feature:feature`, `bug:bug`, `other:maintenance`). The templates may use only `{automation}`, `{stage}` and `{kind}`. A change to a template or a kind label renames the affected records in place (the tag ids stay put, so a filter or count keyed on the id does not break); a name an operator tag already owns is refused rather than taken, and nothing is written. An Automation may override `enabled` and the three templates for itself; `kind_labels` stay daemon-global. Renaming a stage migrates the stage tag with it. Turning `enabled` off strips the tags from the members and reclaims the records nobody references any more — the shared kind tags are the one exception (fleet vocabulary). Because the engine owns them, they are *system tags*: recolorable, but never renamed, moved or deleted by an operator (HTTP `409`, message `tag is a system tag`). Read them by filtering the Automata list, the progress, the trend and the session list on `tag` (repeatable) with `tag_match` (`any`/`all`). See [howto/automata-auto-tags.md](howto/automata-auto-tags.md).

**System tag** — a tag the engine keeps for its own vocabulary; today that is exactly the three auto tags, and the system marker is what the registry enforces on every write path — web, CLI, MCP, REST and chat. The name and the parent are fixed, and delete is refused (`409`, message `tag is a system tag`), while a recolor stays allowed. See [howto/automata-auto-tags.md](howto/automata-auto-tags.md).

**Placement axes** (`autonomous.tags.placement`) — the ten tag axes that turn a recorded fact into a tag: `compute`, `peer`, `backend`, `lane`, `role` (facts about a session run) and `project`, `automaton`, `automation`, `stage`, `kind` (facts the objects the Automata own carry too). Every session kind listed in the [how-to](howto/automata-tags.md#which-sessions-carry-which-axes) carries the run axes; the object axes apply to the Automata and their records, and an object gets those last five, never the run axes. All ten are **on by default**; `autonomous.tags.placement.enabled` is the master switch, and each axis has `<axis>.enabled` (default `true`) and `<axis>.template` (default `<axis>:{placeholder}`, for example `compute:{node}`, `automaton:{automaton}`, `automation:{automation}`, `stage:{stage}`, `kind:{kind}`). `autonomous.tags.placement.custom` declares operator axes from a recorded field (`name`, `field`, `template` with `{value}`, `enabled`). `role` and `kind` have a fixed vocabulary — roles like `worker`, `verifier`, `planner`, `judge`, `supervisor`; kinds like `task`, `decompose`, `review`, `rejudge`, `auto-fix`. Saving a change under the block runs one tag sweep over the existing sessions and records. In the PWA's Sessions **Filters** sheet every placement tag is a single chip under its own axis group (**Kind**, **Role**, **Lane**, **Compute node**, **Project**), operator tags sit under **Your tags**, and the `automaton:`, `stage:`, `task:` and `story:` tags have no chip (an Automaton is picked from the **Find an Automaton** picker); the chips lead with the readable name (tag value, project or folder name, node name) and the id rides only as small secondary text — **unnamed** with the id when no name resolves — and the **Compute node** group is per node, built from the sessions in the list: one entry per local host under its real hostname or federated peer under its registered peer name, aliases merged, never "local" or "all" — display-only, it never changes the tags, a stored selection or the results of an active tag filter. In the PWA's session list the session cards draw these placement tags as green chips with a small ⚙️ mark, and a per-record tag whose value is a bare id (such as `automaton:1d440001`) is drawn as the axis word alone — the chip reads **Automaton** — with the full tag in the tooltip, so no card shows a raw id (a value that already reads as plain words, like `lane:gpu`, renders unchanged); the chips are present at first load: the summary rows of `GET /api/sessions?view=summary` carry each row's `system_tags`, and the chip renders from the tag name itself, so it is drawn even before the tag registry has arrived. Once shown it stays through later list refreshes and session updates; only an explicit empty `system_tags` from the server removes it. Every row shape the server sends carries `system_tags`: a session with no placement tags is sent as `system_tags: []` — never a missing key and never null — and a row that carries the key wins even when it is empty, so a page reload always agrees with the server. A session started through `POST /api/sessions/start` carries the full set of axes that apply to it: the node it ran on — for a local session that stored no node, the local node (`compute:local`) — and the backend, lane and project it records; a start with launch facts (`launch_role`, `launch_kind`, `launch_stage`, `launch_automaton`) also carries the recorded role and kind (such as `role:planner` or `role:supervisor`), the stage, and the Automaton and Automation it names. The daemon's start hook applies the same stamp on its own start paths (scheduled and comm-channel starts), and a restart of a session re-runs the stamp from the record on success — a failed restart changes no tags. A session that belongs to no Automaton carries no `automaton:` or `automation:` tag, whatever role it runs as. An existing session with no recorded launch facts keeps exactly the tags it had before, and the sweep leaves it unchanged rather than inferring missing facts. See [howto/automata-tags.md](howto/automata-tags.md#placement-tags-the-system-tags).

**Launch facts** — the placement facts a session stores on its record when it launches: the role, kind and stage it runs as (the session record's `launch_role`, `launch_kind` and `launch_stage`). A session that carries them gets its placement built from those facts plus its record: the node it ran on, the local node (so the tag reads `compute:local`) when it ran locally and stored none, then the launch role, kind and stage and the backend, lane, project and Automaton/Automation it records. Records written before the fields existed load with empty values, and their sessions keep the tags they had before. The session start records the facts a start request passes (`launch_role`, `launch_kind`, `launch_stage` and `launch_automaton` on `POST /api/sessions/start`, via REST, the MCP `start_session` tool or `datawatch session new`), so a session that started with them keeps the extra axes, and the sweep rebuilds its tags from the record (a role- or kind-name in the session title no longer decides them). A restart of a session (`RestartSession`, and the restart path that reuses a completed or failed session's ID, via the PWA Restart button, REST, MCP, CLI or comm channel) re-runs the same stamp from the record on success, so a restarted session keeps the extra axes without a sweep; a restart that fails changes no tags. See [howto/automata-tags.md](howto/automata-tags.md#stamping-re-stamping-and-the-startup-backfill).

**Tag sweep** — the one pass that applies the in-force placement axes to everything that already exists: it re-derives the system tags of every session (live and archived) and of the records the Automata own — Automata, tasks, Automations, decision records, templates, held questions — from their stored facts, writing only what changed. For a session that carries recorded launch facts the pass rebuilds the placement through the same build as at launch (with the local node for a local session that stored none, e.g. `compute:local`); for a session without launch facts the derivation it always had stays, so the sweep leaves those sessions unchanged. A restarted session is re-stamped when it restarts, so a restart never needs the sweep. It is idempotent (a repeated pass changes nothing), it removes the tags of an axis turned off and adds the ones turned on, and it never reads or writes the tags an operator assigned or that a session inherits. It runs when a `tags.placement.*` change is saved, once at daemon startup after the lease backfill (the daemon remembers the last block it swept), and on demand: `POST /api/autonomous/tags/sweep` (`?dry_run=true` or a `dry_run` body field), the MCP `autonomous_tag_sweep` tool, `datawatch autonomous tags sweep [--dry-run]` (alias `tag-sweep`), the comm verb `autonomous tags sweep [dry-run]`, and the sweep button in the PWA's **Placement tags** settings group. The answer is a per-kind tally — examined, changed, unchanged, failed — and a dry run reports it without writing a single tag. See [howto/automata-tags.md](howto/automata-tags.md).

**Tag inventory (tags in use)** — the read view of the derived tags actually present on sessions: `GET /api/sessions/tags`, the MCP `session_tag_list` tool, and the comm verb `autonomous session-tags list`. Each entry names the tag, its axis and value (the axis is empty for a plain tag) and the number of live or archived sessions carrying it, which is the quickest way to discover what values an axis has before filtering on one. See [howto/automata-tags.md](howto/automata-tags.md).

**Tag registry keeper** — the one daemon that holds the shared tag registry for a federation. The keeper is named by `autonomous.tags.registry_keeper`: on a peer it is the name of the keeper peer, and on the keeper it is the keeper's own name. A peer keeps a cached copy and forwards its tag edits to the keeper; when the keeper is unreachable the peer shows its last copy as stale. See [howto/automata-tag-registry.md](howto/automata-tag-registry.md).

### Project automation policy

**Project automation policy** — the middle tier of the three policy tiers the daemon resolves every Automaton setting through: **Automaton** (its own override) over **project** over **daemon** defaults. The project tier is the per-project entry stored under `projects.<name>.policy` in `datawatch.yaml`, and it is the only tier that travels with the repository. With no policy set, every key inherits the daemon's own value and nothing reads differently. See [howto/project-automation-policy.md](howto/project-automation-policy.md) for the walkthrough.

**Project policy panel** — the PWA Settings → Automata → **Project policy** view: one row per policy key with the effective value, the precedence badge, and a *set* / *inherit (clear)* control per row. Ask it for the project's name and optionally an Automaton's id to resolve with the top tier in play. The same object is edited through `project_upsert`'s `policy` argument on MCP or the `policy` field on `POST /api/projects`, and read back with `datawatch projects policy` on the CLI or the `project policy` comm verb. Help link target: [howto/project-automation-policy.md](howto/project-automation-policy.md).

**Precedence badge** — the chip next to each setting in the Project policy panel that names the tier the *effective* value came from: **Automaton**, **Project** or **Daemon**. A badge that says *Daemon* means the project set nothing for that key; its value is inherited, not copied. The same resolution is readable with `GET /api/projects/{name}/policy/effective?automaton=<id>`, the `project_policy_effective` MCP tool, `datawatch projects policy show <name> --effective [--automaton <id>]` on the CLI, or the `project policy <name> [automaton]` comm verb. Reading the effective value is always safe; only a write to the project tier or the Automaton tier changes anything.

**Policy field** — one row of the Project policy panel, one key of the project policy object. The panel groups the fields the same way this section does. All fields are optional: omit one and it falls through to the tier below.

- **Plan checks** — `plan_coverage_check` (`off`, `warn` or `block`; what the plan requirement-coverage gate does to a short plan), `plan_coverage_min_ratio` (the ratio a plan must reach; a number in `(0, 1]`), `plan_review_mode` (`off`, `warn` or `block`, the plan-quality review gate) and `scope_lint` (`off`, `warn` or `enforce`). These apply to every Automaton that plans inside this project before any Automaton's own settings are consulted.
- **Retry & escalation** — `retry_policy` (the per-category retry budgets, a whole number of attempts from 0 up for each of `infra`, `content`, `scope` and `policy`; a category left out of the map keeps the tier below it for that category only), `retry_ceiling_total` (a cap on the retries the daemon spends across all categories, an integer ≥ 0), `auto_fix_retries` (how many times an auto-fix run may be retried, an integer ≥ 0), `infra_retry_escalate_after` (after how many infrastructure retries a task may escalate instead of retrying, an integer ≥ 0 where 0 reads as *never, keep retrying*), `attempt_cap` (the attempts one task may be started for in all, an integer ≥ 1; it replaces `autonomous.attempt_cap.max_total` for the project) and `failure_escalation` (the escalation level the daemon reaches when a task's retries are spent: the tier, its backend/model/effort, and how many attempts each step gets).
- **Git defaults** — `default_auto_pr` (whether a task may open a pull request without asking per task), `parallel_read_only_tasks` (whether read-only tasks may run in parallel) and `parallel_worktree_per_task` (whether each parallel task gets its own worktree).
- **Lane limits** — `max_concurrent_tasks` (the project's total in-flight task cap, an integer ≥ 1), `plan_tier_max_inflight` (per plan tier, how many Automata may be in flight at once) and `lane_fill_gpu_lanes` / `lane_fill_claude_lanes` (how many GPU and Claude lanes this project may hold from the pools, integers ≥ 0 where 0 reads as *no preference, use the daemon's own cap*).
- **Other recent keys** — `uncommitted_work_policy` and `uncommitted_work_scope` (see [Uncommitted-work check](#uncommitted-work-check); set here they bind every Automaton that commits its own work inside this project), and `burn_guard_ask_on_change` (whether a burn-guard change for a task in this project is a confirmation question to the operator or a silent write).

A value the panel cannot parse — a non-whole number, a word outside the field's vocabulary, an out-of-range ratio — is refused when saved with the reason in the panel; nothing half-written is kept. Clearing a field (the *inherit (clear)* action) deletes that key from the project tier, which is not the same as setting the daemon default: it puts the tier below back in play and the badge moves.
### Protected paths

Protected paths stop an Automaton task from changing the files that hold the project's rules without the operator's decision. A worker that weakens the rules it is judged by, or edits a workflow so a check passes, makes everything after it untrustworthy, and the history does not say who did it. Three settings in Settings → General → Automata settings → **Protected paths** control it. Each help link in those rows opens a section here. The check applies to any Automaton that has a task directory, in direct mode and in worktree mode. Because every worker prompt that gets its rules and context block also carries the [Worker checkpoint rule](#worker-checkpoint-rule) — worker progress notes belong in `CHECKPOINT.md`, a marker file the uncommitted-work checks skip — the gate's most routine former hit, a progress note appended to the rules file, no longer reaches it as a question: an appended entry that is the file's entire change since the base commit is stripped out with a `protected_paths_checkpoint_reverted` decision, and a change that goes beyond the entry is still held.

**Protected paths** (`autonomous.automation.protected_paths`) — the list of paths a task may not change without a decision. Each entry is repository-relative and is a file (`CODEOWNERS`), a directory (a trailing slash or `/**`, such as `ops/`, which covers everything below it at any depth) or a glob using `*`, `?` and `[ ]` matched against the whole path (`docs/plans/*.md`; a `*` never crosses a slash). Matching is case-sensitive. The default list, used when the key is unset or empty, is the project rules file (`autonomous.rules_file`, `AGENT.md` when unset), `docs/plans/2026-10-17-supervisor-rules-and-lessons.md`, `CODEOWNERS` and `.github/workflows/`. An explicit list replaces the default entirely, so write every entry you want. An absolute entry, an entry with a `..` segment, an entry over 256 bytes and a list over 100 entries are refused when written. It is for the operator who wants the rules and the checks out of a worker's reach: keep the default, and add the directories that matter to your project.

What counts as a change is read from git alone: files the task committed, edits to tracked files that are not committed yet and new untracked files, compared with the base commit. A file that was already modified or untracked before the task started is the operator's own work and is left out, unless a commit made since the base touched it. The daemon's own write into the rules file is checked like any other change. See [howto/automaton-protected-paths.md](howto/automaton-protected-paths.md).

### Protected-paths action

**Protected-paths action** (`autonomous.automation.protected_paths_action`) — what the daemon does when a task changes a protected path the Automaton is not authorised for. `hold`, the default, parks the task as blocked with the changed paths and raises one question, labelled `protected_paths`, with the options **Approve this change** (`approve`), **Reject it** (`reject`) and **Change the spec** (`change_spec`) and the default `hold`, so nothing changes until you answer. `warn` records a decision, an audit event and a warning alert and lets the task go on; no question is raised. `block` parks the task as blocked with the changed paths and raises no question: authorise the path for the Automaton, or edit the task, then reset it. `off` does no check. Apart from the [checkpoint-entry revert](howto/automaton-protected-paths.md#the-checkpoint-entry-revert) — a worker's own checkpoint entry, stripped from a rules file when it is the file's entire change since the base commit — the gate never reverts or rewrites anything by itself. An unset key or an unknown word reads as `hold`; any other word is refused when written. It is for the operator who wants a merge to carry no unreviewed change to the rules: keep `hold`, and choose `warn` only for a project that expects those edits.

**The three answers** — **Approve this change** keeps the change and completes the task; the approval covers only the listed paths, for this task, and never changes the Automaton's authorised paths. **Reject it** puts the listed paths back to their state at the base commit in a new commit (a path the task added is removed; history is never rewritten) and runs the task again with a note that those paths must not change; it uses none of the task's automatic-retry budget. **Change the spec** touches nothing: the task stays blocked until you edit its instructions and reset it. A restore that cannot be done keeps the hold and raises a fresh question. See [howto/automaton-protected-paths.md](howto/automaton-protected-paths.md).

### Rules-review source

**Rules-review source** (`autonomous.automation.rules_review_source`) — where the rules review reads the project rules from. The rules review is the completion check that judges an Automaton's real changes against the project's rules; it runs when `rules-review` is in the Automaton's guardrail list. `base_commit`, the default, finds the commit the Automaton's work started from and gives the judge the text of the rules file and the context file as they were at that commit, so a task cannot change the rubric it is judged by, and deleting the rules file does not skip the review. If the working-tree copy differs from that text, the judge reports the difference as a finding. `worktree` tells the judge to read the files from the working directory, as before. When the base commit cannot be found, or a git read fails, the review reads the working directory for that run and records a `rules_review_source_fallback` decision on the Automaton. An unset key or an unknown word reads as `base_commit`; any other word is refused when written. It is for the operator who wants the rubric fixed before the work starts: keep the default.

### Judge time limit and judge timeout hold

**Judge time limit** — the time limit for one judge run, `autonomous.judge_timeout_seconds` (default 1800 seconds). The wait for a capacity slot is not counted. For a change above `autonomous.judge_size_threshold_lines` the limit grows by `autonomous.judge_timeout_per_thousand_lines_seconds` (default 600) for every 1,000 changed lines, to at most four times the base. Each backend the judge tries gets its own limit. See [howto/judge-policy.md](howto/judge-policy.md#the-time-limit).

**Judge timeout hold (`judge_timeout_hold`)** — the `needs_review` hold an Automaton gets when its judge hit the time limit on every backend it tried. The work was not judged: the review state reads `timed_out`, with `judged` false and a `cause` that starts "not judged:", and it lists each backend with how its attempt ended. Once every task is completed or cancelled, the rejudge or `prd-run` replays the review on the same head with no new commit, never from the verdict cache. If it times out again the hold stays. See [howto/judge-policy.md](howto/judge-policy.md#one-attempt-per-backend) and [howto/autonomous-recovery.md](howto/autonomous-recovery.md#re-judging-a-blocked-automaton-whose-guardrail-verdict-was-lost-rejudge).

### Rejudge, review head and rules fingerprint

**Rejudge** — the operator's request to run an Automaton's completion rules review again, without resetting or editing any task. It applies to a `blocked` Automaton whose tasks are all terminal (with at least one `completed`), to a held one (`needs_review` after the review retries were spent or with auto-fix off) once new commits are on its branch, to a `needs_review` Automaton held by a judge timeout (`judge_timeout_hold`) on the same head with no new commit (`prd-run` also replays that hold; a timed-out review is reported as *not judged*, `judged` false in the review state), and to a `completed` or `archived` Automaton whose work branch is not yet on the base branch (merge state `finished_not_merged`) and whose last review verdict is pending, a block, or a pass judged on an older review head (a stale pass after the work moved on) — the one that clears a stale block after the work has moved on; a pass there keeps the finished status, a block flips the record back to `blocked`. Finished Automata whose work is already landed, or whose latest pass covers the current review head, are refused. An auto-fix merge into the branch also re-runs this review on the new head by itself. It is a fresh review whenever the review head or the rules text has changed since the stored blocking verdict; it replays that verdict only when both are unchanged, the verdict recorded a rules fingerprint and the judge verdict cache is on. Every rejudge records a `guardrail_rejudge` decision that names the commit it judged (`judged_commit_sha`) and says whether a stored verdict was reused (`verdict_reused`). A new review also records the verdict and its `guardrail_verdict` decision; a reuse records neither, so the Automaton's verdict list keeps one entry per review that ran. The reply says which it was: the compact reply's `message` reads `a new review ran on commit <sha>` or `the stored verdict was reused`, and the PWA shows it in the notice after Retry or Reroute. Use it after committing a fix, or after the auto-fix merge that should have lifted a stale block. MCP `autonomous_prd_rejudge`, REST `POST /api/autonomous/prds/{id}/rejudge`, CLI `datawatch autonomous prd-rejudge <id>` (alias `rejudge`; `--backend` reroute pin), comm `autonomous rejudge <id> [backend=<name>]`. See [howto/autonomous-recovery.md](howto/autonomous-recovery.md#re-judging-the-completion-guardrail-rejudge) and [howto/judge-policy.md](howto/judge-policy.md#the-per-commit-verdict-cache).

**Review head** — the commit the completion review is anchored to: the real head of the Automaton's own working tree, so a commit made by hand counts, not only one a task attempt recorded. When that head cannot be read, the latest commit a task attempt recorded stands in. The verdict stores it as `judged_commit_sha`.

**Rules fingerprint** — a hash of the exact rules and context text the completion review reads, stored on the verdict as `rules_fingerprint` and mixed into the verdict cache key. Editing that text makes the next rejudge judge again. With the `base_commit` source it covers the files as committed at the base commit; with `worktree`, their names and contents. It is empty when the project has no rules or context file, and a verdict recorded without one is judged again once.

**Skipped story** — an Automaton story whose tasks are all cancelled. It shows as cancelled (skipped) and does not fail the Automaton. A story whose other tasks are completed counts as completed, and a story with any failed task is still failed. See [howto/autonomous-recovery.md](howto/autonomous-recovery.md#cancelled-tasks-at-completion).

**Cancelled task** — an Automaton task you stopped on purpose; a deliberate final outcome, not a failure. The Automaton skips it, so it no longer holds the Automaton in running. While the Automaton is running, a task that is not running and not queued can be removed on every surface. See [howto/autonomous-recovery.md](howto/autonomous-recovery.md#cancelled-tasks-at-completion).

**Cancelled task skipped** — a cancelled task that the completion roll-up leaves out instead of treating as unfinished. When every task of an Automaton is completed or cancelled and at least one is completed, the completion review runs and the Automaton completes or blocks on its verdict; the daemon records one `cancelled_tasks_skipped` decision naming the cancelled tasks. A story whose tasks are all cancelled shows as cancelled (skipped) and does not fail the Automaton; an Automaton with every task cancelled is not judged or completed. A rejudge accepts cancelled tasks the same way, and the stuck-running watchdog advances an Automaton held only by cancelled tasks, recording a `stuck_running_cancelled_advance` decision. A running Automaton also lets you remove a task that is not running or queued. See [howto/autonomous-recovery.md](howto/autonomous-recovery.md#cancelled-tasks-at-completion).

**Stored verdict reuse** — a stored completion verdict handed back instead of a new review, because the review head, the rules text and the judge inputs are unchanged and the judge verdict cache is on. The reused verdict keeps the time, commit and rules fingerprint it was made with and is not added to the Automaton's verdict list a second time. The decision log says so: a rejudge records `guardrail_rejudge` with `verdict_reused` true and a note that names the commit and the time of the stored verdict; the completion review the daemon runs on its own (including the one-time re-fire of the stuck-running watchdog) records a `guardrail_reuse` decision in the same way, with no second classification of the verdict. When a review did run, `verdict_reused` is false (it is left out of the JSON) and the note starts with "new review ran on commit". `judged_commit_sha` on the decision is the commit that was judged, or the commit the reused verdict was judged on.

### Authorised paths

**Authorised paths** (the Automaton's `authorized_paths`) — the list of protected paths one Automaton's tasks may change without a decision. It is set per Automaton, not in the settings, because only some Automata are meant to edit the rules or a workflow. The list you send replaces the current one; an empty list clears it. Each entry follows the same three forms as the protected list (file, directory, glob) and is repository-relative; an absolute entry, a `..` segment, an entry over 256 bytes and a list over 100 entries are refused. A path under the list passes in every action, and the first time a task uses it the Automaton records a `protected_paths_authorised` decision. Authorising does not answer a task that is already held: answer its question, or reset the task. Set it with `POST /api/autonomous/prds/{id}/set_authorized_paths`, MCP `autonomous_prd_set_authorized_paths`, `datawatch autonomous authorize-paths`, the chat verb `autonomous authorize-paths`, or the **Authorised paths** card on the Automaton's detail page. Prose says authorised; identifiers, JSON fields and commands say authorized. See [howto/automaton-protected-paths.md](howto/automaton-protected-paths.md).

### Infrastructure retry

**Infrastructure failure** — a task attempt that ended because the machinery around the task broke, not because the work was wrong. The task never got a fair attempt, so the daemon does not count it as a failed attempt. Only four classes count, and nothing else is ever treated this way:

- `spawn_error` — the worker could not be started at all.
- `storage_write` — a write failed because the disk is full, the quota is used up or the file system is read-only; the script gate's **temporary folder full** reason (a no-space exit at the gate) is one of the texts this class matches, so it takes this path rather than the normal failure path.
- `session_killed_before_delivery` — the worker session ended before the task text reached it.
- `daemon_restart` — the daemon restarted or shut down while the task was running.

A wrong answer, a failed check, a task that went out of scope or a stall that is not a launch failure is the task's own fault and follows the normal failure path. A session that never started has its own start budget and is not classified here. A throttle or usage limit from a model provider is not an infrastructure failure either, and it is never retried by this feature.

**Infrastructure retry** (`autonomous.infra_retry.enabled`, on by default) — when an infrastructure failure ends a task attempt, the Automaton waits and tries the task again instead of counting a failure against it. This is the master switch for the whole feature; with it off, every failure takes the normal path as before.

**Back-off** (`autonomous.infra_retry.max_tries`, `autonomous.infra_retry.backoff_seconds`) — how many tries a task gets (default 3, a whole number from 1 to 1000) and how long the Automaton waits before each retry, in seconds. The default delays are 60, 300 and 540: one, five and nine minutes, 15 minutes in all. When a task has more tries than delays the last delay repeats, and an empty list means retry with no delay. While a task waits, its row says *infrastructure retry 2 of 3 at 14:05:09 (storage_write)*. A retry that follows a daemon restart waits no longer than the first delay, because the cause is already gone.

**Verifier fault** — a verification that says nothing about the work: the ask call failed, the verifier session gave no output or timed out, no capacity could be had, the diff could not be read, or the reply was not a verdict (not JSON, or no `ok` field). Only the verification runs again, on the same `max_tries` and `backoff_seconds`, counted in the task's `verifier_fault_retries`; the worker is not run again and no failure is counted. When the tries are used up, whatever `exhausted_action` says, the task is held with the failure source `verifier-fault-hold`, its commit kept, one alert, and one question whose default is `hold`; the supervisor loop does not retry that hold by itself. With `enabled` off the first fault holds the task. See [howto/infrastructure-retry](howto/infrastructure-retry.md#when-the-verifier-is-at-fault).

**Retried failure classes** (`autonomous.infra_retry.categories`) — which of the four classes are retried. The default is all four; an empty list retries none, so every infrastructure failure takes the normal path.

**Retry record** — what the task keeps about its retries. Each retry is written to the task as an entry with its number, class, a short reason (the error text, cut to 500 characters), the time, the delay and the time the next try starts; the task keeps the newest 50. A separate count of consecutive retries decides when the tries are spent, and it goes back to zero when the incident ends while the entries stay as history. Each retry is also logged as an `infra_retry` decision on the Automaton. The Automata page and the Automaton detail show it as **Retry n of max** on the task, and the Automation view carries an Infrastructure retries card with the policy line and every retried or held task.

**Held task** (`autonomous.infra_retry.exhausted_action`) — what a task becomes when its tries are spent and the action is `hold` (the default). The task stops for the operator's review without a failure counted against it, so the failure escalation budget is untouched. It carries the infrastructure cause, a report that lists every retry, and one error-level alert titled *Automaton task held after infrastructure retries*. With the action set to `count`, the failure is booked as one ordinary failed attempt and the normal failure handling applies.

**Held-task question** (`autonomous.infra_retry.raise_question`, on by default) — when a task is held and the operator question channel (`autonomous.questions.enabled`) is on, the daemon raises one high-priority, blocking question for the hold. It names the task, the Automaton, the failure class and the cause, and offers two answers: **Retry the task now** (the default), which runs the task again with a fresh set of tries and holds it again if the cause persists, or **Cancel the task**, which cancels it so that tasks depending on it do not run. Answering the question has the same effect as resolving the task review. If the task is resolved some other way, the question is withdrawn. With the question switched off, or the question channel off, the alert and the review report carry the cause.

**Dependents that recover** (`autonomous.infra_retry.retry_dependents`, on by default) — a dependent whose only failure is that its dependency failed is requeued on its own, so it needs no manual `reset_task`: the moment that dependency reaches `completed`, the executor requeues the dependent in the same step, for every failure class of the dependency's own failure — infrastructure, cancellation, content or scope — returning it to pending in dependency order, so a chain recovers in one pass, and an Automaton that ended failed or blocked only because of them returns to running. A dependent that failed for its own reasons, or that still waits on another dependency that is failed, cancelled, blocked or held, is left for a manual reset. In the infrastructure chain the dependents are additionally marked as failed because of the dependency, with the task where the chain began named as the root, and they recover as soon as the root is no longer failed, cancelled, blocked or held — which can be before completion, for example when you answer "retry" on a held root; that marking and recovery are the side the setting governs, and with it off the marking and that early recovery do not happen, while the general requeue still applies. The infrastructure recovery is logged as an `infra_dependents_recovered` decision and the general requeue as a `dependents_recovered` decision.

**Model unresponsive** — a pi attempt that failed because the model never answered: the output log holds a `DATAWATCH_PI_FAILED` line and the text `Request timed out`. The daemon fails the attempt as soon as it sees the failure line rather than waiting out the silence limit. The task is then requeued after the back-off without using an attempt, at most 3 times in a row (the task's `model_unresponsive_retries`, separate from the infrastructure retry count and not part of `autonomous.infra_retry.categories`). The reason names the compute node and model.

**Launch probe** — the one-token chat request (`max_tokens` 1, 20-second limit) the daemon sends to a compute node's model before it launches a pi worker there. A failure means no session is started: the node gets an unhealthy mark and the task is deferred through the model-unresponsive requeue. A success clears the mark. It is skipped when the launch has no node or no model. Not a setting.

**Unhealthy mark** — a per-node flag, with the probe error as its reason, that makes placement skip the node as if it were full. It shows as `unhealthy` and `unhealthy_reason` on the node's pool in `GET /api/capacity`. It is cleared by a passing launch probe and by a daemon restart (it is held in memory).

**Node unresponsive alert** — the error alert "Compute node model unresponsive", raised once when 3 model-unresponsive failures in a row happen on one compute node. A success on that node resets the count; the count is in memory and restarts with the daemon.

**Pi worker failure reasons** — what the daemon reports when a pi-prompt worker stalls or its model does not answer: the failure line (`DATAWATCH_PI_FAILED`) read from the session's output log ends the attempt at once with runner evidence (exit status, last output, processes, node, model) and a "Task session failed" error alert; a failure that also says `Request timed out` is model unresponsive and is requeued without using an attempt; three in a row on one compute node raise the "Compute node model unresponsive" alert; and the launch probe before a worker starts on a node of a pooled entry (more than one compute node) marks a node that fails it unhealthy; a single-node entry is not probed. The unhealthy mark ends with a passing launch probe or a daemon restart. The drain, undrain and node health controls do not clear it, and node health does not show it (the capacity pool does). None of it is a setting. See [howto/pi-worker-failure-reasons.md](howto/pi-worker-failure-reasons.md).

**Judged by output** — a worker session that ended complete, printed the completion marker and left a commit or a stored result is judged by that output and is not retried as a launch failure, even if the delivery of the task text was never confirmed. A launch failure from the delivery check says which screen the worker was on (a startup dialog, the agent working, text already in the input box, or no input box). A held task whose worker has a completion marker can be **accepted** from review beside retry and cancel: `datawatch autonomous resolve-review <id> --task <task-id> --action accept`, MCP `autonomous_prd_resolve_task_review` with `action=accept`, REST `resolve_task_review` with `"action":"accept"`, comm `autonomous review-task <automaton> <task> accept`. Without a marker it is refused. See [howto/failure-escalation.md](howto/failure-escalation.md).

**Judged by output** — a worker session that ended complete, printed the completion marker and left a commit or a stored result is judged by that output and is not retried as a launch failure, even if the delivery of the task text was never confirmed. The delivery check records what the screen showed when its wait ended (a choice dialog still waiting for an answer, the agent working, text already in the input box, or no input box). A held task whose worker left a completion marker can be **accepted** from review beside retry and cancel: `datawatch autonomous resolve-review <id> --task <task-id> --action accept`, MCP `autonomous_prd_resolve_task_review` with `action=accept`, REST `resolve_task_review` with `"action":"accept"`, comm `autonomous review-task <automaton> <task> accept` (the web UI has no accept button yet). Without a marker it is refused. See [howto/failure-escalation.md](howto/failure-escalation.md).

The state is read with `GET /api/autonomous/infra-retries`, the `autonomous_infra_retries` MCP tool, `datawatch autonomous infra-retries` and the comm channel verb `autonomous infra-retries`. Every setting above is also a config key on all surfaces. See [howto/infrastructure-retry.md](howto/infrastructure-retry.md) for the walkthrough and [mcp.md](mcp.md) for the arguments that set the policy.


### Attempt cap

**Attempt count** (`attempts_total` on every task) — the number of attempts the daemon has started the task for, in all. One attempt is one start of a worker for the task, or one retry that continues the earlier session with the failure report. The count is part of the stored task, so a daemon restart keeps it, and no reset, cancel, review retry or housekeeping repair clears it. It is not the failure ladder's count (`escalation_failures`), which counts failed attempts since the last operator action and starts over on every reset. A start that failed on the infrastructure and is retried, a session that never started, a restart that cut an attempt short and a session adopted again after a restart are not counted. A task that ran before the cap existed is counted once, at the first start after the cap shipped, from its recorded attempt history (completed tasks too); an unfinished task already over the cap is held then, with one grouped operator question per Automaton. Every surface that shows a task shows the count: the Automaton and its task list over REST and MCP, `datawatch autonomous tasks`, the review list, and the task row in the web app.

**Attempt cap** (`autonomous.attempt_cap.enabled`, on by default; `autonomous.attempt_cap.max_total`, default 6) — the most attempts a task may be started for in all. When the count reaches it, the task is held in `review_needed` with the failure source `attempt-cap-hold` and `attempt_cap_held` set, an `attempt_cap_hold` decision is written on the Automaton, one error alert titled "Automaton task reached its attempt cap" is raised and, when the operator question channel (`autonomous.questions.enabled`) is on, one blocking question offers **Clear the count and run it again** or **Cancel the task**, with leaving it held as the default. Nothing starts the task again after that, whoever asks: a review retry is refused with a message naming the cap, and so is a force reset made by a session. Off, the daemon behaves as it did before the cap: only the failure ladder limits a task. A project may set its own cap as `attempt_cap` in its policy block.

**Attempt back-off** (`autonomous.attempt_cap.free_attempts`, default 3; `autonomous.attempt_cap.backoff_minutes`, default 5, 15, 45) — the first `free_attempts` attempts start at once; each attempt after them waits the next delay, counted from the end of the previous attempt, the last delay repeating, an empty list meaning no wait. A cancel counts as the end of an attempt, so cancel-and-retry waits too. The wait applies to automatic retries and to resets and retries made by sessions; the operator's own retry or reset starts at once (the wait owed is dropped), still counts toward the cap, and leaves the waits of the automatic retries that follow in place. The earliest start is stored on the task as `attempt_not_before`, so a restart does not shorten it. With the defaults a task runs three attempts at once, then one after 5 minutes, one after 15 and one after 45, six in all. The three is the failure ladder's length, the 5, 15, 45 grow by a factor of three so an unfixed fault gets more time each round, and six is the ladder run twice: the second run is the one retry a supervisor or the operator may ask for.

**Force reset by the operator** — the only thing that clears the attempt count. A force reset (`reset_task` with `force`) made as the operator clears the count and the hold, sets the task back to pending and writes an `attempt_cap_cleared` decision with the actor and the number of attempts cleared; the REST audit entry carries `clears_attempt_count`. The daemon decides who is calling from the credential, not from the call: a session's scoped credential names the session and a federation peer names itself, whatever the call says, and `datawatch autonomous prd-reset-task` run inside a session reports the session. A reset by a doer, a supervisor session, a housekeeping repair or the gate repair keeps the count. Answering the cap question with the force-reset option, as the operator, is the same reset; a session's answer clears nothing and is noted as `attempt_cap_answer_ignored`.

#### autonomous.attempt_cap.enabled

**Default** on (`true`). **Accepted values** `true` or `false`.

Whether the daemon counts attempts, makes later attempts wait and holds a task at the cap. Off, only the failure ladder limits a task, as before.

**How to set it** — in YAML under `autonomous:` then `attempt_cap:` as `enabled: false`; with `datawatch config set autonomous.attempt_cap.enabled false`; with `PUT /api/config` and the body `{"autonomous.attempt_cap.enabled": false}`; from a chat channel with `configure autonomous.attempt_cap.enabled=false`; from MCP with `autonomous_config_set` with `attempt_cap_enabled`; or in the Settings tab with the toggle **Attempt cap** in the **Attempt cap** group. A refused value comes back naming the key and the valid values.

#### autonomous.attempt_cap.max_total

**Default** `6`. **Accepted values** a whole number from 1 to 1000.

The attempts a task may be started for in all, before it is held for the operator. A project's policy `attempt_cap` replaces it for that project.

**How to set it** — in YAML under `autonomous:` then `attempt_cap:` as `max_total: 8`; with `datawatch config set autonomous.attempt_cap.max_total 8`; with `PUT /api/config` and the body `{"autonomous.attempt_cap.max_total": 8}`; from a chat channel with `configure autonomous.attempt_cap.max_total=8`; from MCP with `autonomous_config_set` with `attempt_cap_max_total`; or in the Settings tab in the **Attempt cap** group. A refused value comes back naming the key and the valid values.

#### autonomous.attempt_cap.free_attempts

**Default** `3`. **Accepted values** a whole number from 1 to 1000.

How many attempts start with no wait. The attempt after these waits the first delay of `autonomous.attempt_cap.backoff_minutes`.

**How to set it** — in YAML under `autonomous:` then `attempt_cap:` as `free_attempts: 2`; with `datawatch config set autonomous.attempt_cap.free_attempts 2`; with `PUT /api/config` and the body `{"autonomous.attempt_cap.free_attempts": 2}`; from a chat channel with `configure autonomous.attempt_cap.free_attempts=2`; from MCP with `autonomous_config_set` with `attempt_cap_free_attempts`; or in the Settings tab in the **Attempt cap** group.

#### autonomous.attempt_cap.backoff_minutes

**Default** `[5, 15, 45]`. **Accepted values** a list of whole minutes, each 0 to 10080, as a JSON array or a comma list; an empty list means no wait; `null` clears the key back to the default.

The wait before each attempt after the free ones, in order, the last entry repeating when there are more attempts than entries.

**How to set it** — in YAML under `autonomous:` then `attempt_cap:` as `backoff_minutes: [5, 15, 45]`; with `datawatch config set autonomous.attempt_cap.backoff_minutes 5,15,45`; with `PUT /api/config` and the body `{"autonomous.attempt_cap.backoff_minutes": [5, 15, 45]}`; from a chat channel with `configure autonomous.attempt_cap.backoff_minutes=5,15,45`; from MCP with `autonomous_config_set` with `attempt_cap_backoff_minutes`; or in the Settings tab with the field **Wait before each later attempt** in the **Attempt cap** group.

### Change record

The change record is the list of what an Automaton changed, built from the git diff of each task and merged for the Automaton. Counts are taken from the merge base with main (the same diff the merge would apply), so the record counts only the Automaton's own work — files that arrive through a merge of main are not counted — and the record keeps the base it counted from (`base_ref`) with how it found it (`base_how`): `merge_base`, or `task_start` when the merge base could not be resolved and the task-start ref was used instead. The changelog, parity and tracker drafts name the base with the line `Base: merge base with main (<short sha>)`. It lists files, packages, settings, routes, locale keys, tests, the commands that were run, and — when a ledger reading was attached — the ledger-backed cost summary (sessions, token and tool-call counts, span of time, the session ids, the API-equivalent estimate when one exists, and the ledger's window and unknown-spend notes). It is stored with the Automaton, read with `GET /api/autonomous/prds/{id}/change-record`, the `autonomous_prd_change_record` MCP tool and `datawatch autonomous change-record <id>`, and it is the only input the generated artefacts are drawn from. See [howto/generated-artefacts.md](howto/generated-artefacts.md).

### Generated artefacts

Generated artefacts are the small documents drawn from the change record when an Automaton completes: a gate report (the counts table, plus the cost section when the record carries a cost summary), a changelog draft, a mobile parity draft, testing-tracker rows and a registry row. Each starts with a generated marker and is a draft: a file a person edited is never overwritten, and `autonomous_prd_regenerate_artefact` (or `datawatch autonomous generated <id> <kind> --regenerate`) renders it again. Generated artefacts never carry the operator's machine name: session ids are printed bare (the `<host>-`/`<host>:` prefix stripped, the segment after the last separator kept) and ledger notes are scrubbed (the local hostname replaced by `host`, absolute paths reduced to their final element). Seven switches under `autonomous.generated_artefacts` (`change_record`, `gate_report`, `changelog_draft`, `parity_draft`, `tracker_rows`, `registry_row`, `combined_docs_task`) turn each piece off; unset means on. See [howto/generated-artefacts.md](howto/generated-artefacts.md).

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

**Milestone, Automation** — the `milestone` flag on an Automation record marks the plan as a milestone. When every member has finished and the project's `major_trigger` is `milestone`, the plan's merges step major; with any other trigger the flag changes nothing. An unset flag is an ordinary plan. It is set with the Mark as milestone control on the Automation, or the `milestone` field of `automation_create` and `automation_update`. See [howto/release-size-and-milestones.md](howto/release-size-and-milestones.md).

**Breaking change flag, Automaton** — the `breaking_change` flag on an Automaton record marks its change as user-breaking. Regardless of the resolved release size, the resolver reports the merge as one that must go through the major approval mode, so it is never merged quietly; the approval modes themselves land in the work that follows. See [howto/release-size-and-milestones.md](howto/release-size-and-milestones.md).

### Post-ship hooks

The post-ship hooks are the hook steps a project runs after the ship — after the merge pipeline's push and deploy have succeeded, in the order the project lists them. They are off by default. A post-ship hook is either one of the five builtins (`registry_row` updates a registry row, `parity_issue` files a parity issue through the ship runner, `tag` adds a version tag that is not there yet, `notify` sends a rendered template, `mark_completed` marks the shipped Automaton Completed and needs no target) with its built-in target, or a `command` the daemon runs in the project. Each hook has its own `enabled` switch. A failed hook records its result in the merge run record and, only for a run that belongs to an Automaton, leaves one triage note with source `post_ship`; it never changes the ship, never undoes the deploy and never stops the hooks after it. The block and the hook list are readable and writable from every settings surface: YAML, the `GET`/`PUT /api/config` and merge-pipeline reads, `autonomous_merge_pipeline_get` and `autonomous_config_set` on MCP, `datawatch autonomous merge-pipeline post-ship set|show` on the command line, `configure` and `help` on the comm channel, and the Post-ship hooks card in the PWA settings. See [howto/post-ship-hooks.md](howto/post-ship-hooks.md) for the walkthrough and [flow/post-ship-hook-flow.md](flow/post-ship-hook-flow.md) for the flow.

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

**Merge conflict model** (`autonomous.merge_pipeline.conflict_model`; an unset key reads the non-Opus daemon default name, `sonnet`) — the LLM registry entry that proposes a conflict resolution. A model the operator names, an Opus model included, is used as written; an explicit empty string means no model. The setting never adds, edits or deletes a registry entry, so the name must already exist. The proposal is worked out in a scratch worktree and tested there; the project is touched only when the test step passes.

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

**Push and tag** (`autonomous.push_and_tag`, off by default) — the two steps that publish a finished release. **Push** sends the release commit to the remote; **tag** creates the annotated release tag on that commit and sends the tag to the remote. With the setting on, the daemon does both for an Automaton's release, on the [release path](#release-path) the project chose. With it off, the release stays local: the tag is created and the outcome is recorded as `prepared`, and the operator pushes. Both steps are fast-forward only and never force; a tag that already points at another commit is never moved. Setting it does not change the approval mode of the release. See [howto/release-push-and-tag.md](howto/release-push-and-tag.md).

**Release path** (`autonomous.release_path`, `direct_push` by default) — how a finished release reaches the remote. `direct_push` fast-forwards the default branch to the release commit and pushes the tag. `pull_request` pushes the release branch, opens a pull request into the default branch, and merges it only when every check on it succeeded; a failed or unfinished check set leaves the pull request open and unmerged. Any other value is refused when it is set. The path names how the release is published; it does not choose the release size or its approval mode.

**Release CI window** (`git.release_ci_window_minutes`, 60 by default) — how many whole minutes the daemon watches a release's CI after the push, and must be greater than 0. On the pull request path it bounds how long the checks are polled before the release is left unmerged. It is also the window in which a red CI outcome triggers the fall-back to ask: a red run within the window moves that project's release approval for the release's size to `ask`, raises an alert and files a defect. A red run after the window, or a green or pending one, changes nothing. See [howto/release-push-and-tag.md](howto/release-push-and-tag.md#ci-window-and-the-fall-back-to-ask).

**Release record** — the daemon's record of one published release of a project, one per project and tag. It names the tag, the commit the tag points at on the remote, the release path, when the push was attempted, the push outcome (`pushed`, `prepared`, `refused`, `failed`, `merged`, `checks_failed` or `checks_timed_out`), the CI outcome (`pending`, `green`, `red` or `unknown`), when CI was last observed, and the release size. The newest record for a project is its last release, read by the `release_push_status` MCP tool and shown as the `Last release:` line in the release settings. The record is kept as a JSON-lines file, and the store is only read once the daemon installs it; this build does not install one yet, so no release record is read.

### Release gate

The **release gate** is where the daemon's own merge run enforces the [release approval](#release-approval) mode. It applies to every step that leaves the machine — the tag, push, deploy and post-ship-hook steps — and decides at most once per run, before the first of them runs; the verdict is cached, so the run cannot answer the push differently from the tag. What it holds back is the leaving steps only: the merge, the version bump, the changelog and the commit land on the local project as usual, and a held release is one that is not tagged, not pushed and not deployed.

With the checks passing, the mode decides: `auto` lets the leaving steps run; `ask` raises one yes/no question ending "Ship this release?" in the operator's question channel (options "yes (ship it)" and "no (do not ship)", "no" the default) and the run ships only when the current answer is yes — a re-run for the same project, Automaton and size finds that question instead of raising a second one, and a changed mind replaces the earlier answer; `manual` holds the release for the operator and raises no question. A failed check holds the release for a person in every mode and is never asked about: a "yes" against failed checks could only be honoured by shipping them. If the question cannot be raised, the run fails short of the ship rather than guess. The steps the gate holds are still in the run's record, skipped with the mode, the size (or "not read") and the reason in plain words — `pipeline_runs` of `GET /api/autonomous/merge` and the PWA's merge status view. The operator guide is [howto/merge-pipeline.md](howto/merge-pipeline.md#the-release-gate).

### Merge shadow

The four settings that tune the [shadow merge plan](#shadow-merge-plan), under `autonomous.merge_shadow`. All four are single values — there is no per-project override for this block — and a value they refuse is refused with the problem named and nothing written on every surface: a 400 on `PUT /api/config`, an error line on `datawatch config set`, on comm `configure`, and the same message on the Settings card.

**Merge shadow enabled** (`autonomous.merge_shadow.enabled`, on by default) — the master switch for the plan. Absent or `true` keeps it on; `false` is the only spelling that turns it off, from then on the plan is no longer written when an Automaton's finish is handled.

**Merge shadow gate timeout** (`autonomous.merge_shadow.gate_timeout`, `20m`) — the budget each gate of the throwaway trial merge may wait before giving up: a Go duration string (`20m`, `90s`, `2h`), at least one nanosecond, and a positive value per gate, not a cap across all of them.

**Merge shadow plan retention** (`autonomous.merge_shadow.plan_keep`, `200`) — how many plans are kept per project before the oldest are pruned; a whole number of at least 1.

**Leftover merge trial sweep** — at start the daemon removes the trial worktrees a stopped daemon left under `merge-shadow-scratch` in its data directory and records them in the audit log (`autonomous_merge_trial_sweep`). It has no setting. See [howto/merge-shadow.md](howto/merge-shadow.md).

**Agreement streak threshold** (`autonomous.merge_shadow.agreement_streak`, `5`) — the number of most-recent consecutive stored comparisons a project needs, with no major difference in any of them, before its agreement streak reads `met` (exposed as `streak` — `length`, `total`, `threshold`, `met` — on the REST, MCP, CLI and comm reads). The streak is evidence for the handover bar, not a gate: it approves nothing. A whole number of at least 1.

Set them with `datawatch config set autonomous.merge_shadow.<key> <value>` (read them with `datawatch config get autonomous.merge_shadow.<key>`), with `PUT /api/config` and `GET /api/config` as a flat `{"autonomous.merge_shadow.<key>": …}` object, with comm `configure autonomous.merge_shadow.<key>=<value>`, or in Settings > Automation > the **Merge shadow** card. The operator walkthrough, with what the settings enable, is [howto/merge-shadow.md](howto/merge-shadow.md).

### Shadow merge plan

The shadow merge plan is the record the daemon writes for a finished Automaton describing what its merge would do, in the shadow period of Phase 4 of the autonomy handover: the release size (`patch`, `minor`, `major` or `none`), the version the bump step would set, the changelog heading and body, where they came from, the README line, the registry row and the parity-issue flag, each conflict with its file class, resolution and resolver, each gate with its status (`pass`, `fail`, `timeout`, `skipped`, or `intermittent` — a test gate that failed first and passed when rerun, its failing lines kept as the finding and not blocking) and duration, and each finding with a code (`trial_merge_failed`, `conflict_unresolved`, `gate_fail`/`gate_timeout`, `changelog_missing`, `unmerged_task_branch`, `stray_root_file`, `lint_version_mismatch`, `lint_tool_unavailable`, `review_stale`) and whether it blocks. Before the trial merge, the plan step checks the Automaton's own task branches: one named after the work branch (`<branch>--tasks-…`) holding commits the work branch does not blocks the plan with `unmerged_task_branch`, a recorded review verdict that names a commit other than the branch head (or none at all) blocks it with `review_stale`, and when any blocks the trial merge and every gate are then recorded as skipped. It is a record, not a queue: every stored plan has `applied` set to `false`, the store refuses a plan marked applied, and the trial merge that finds the conflicts runs in a throwaway worktree outside the project's tree, uncommitted (`--no-commit --no-ff`), with the scratch tree removed on every exit path — no branch, tag or ref is created or moved and nothing is pushed. The changelog text comes only from the Automaton's own changelog entry task when its output has the exact shape `## [version] - YYYY-MM-DD` with a non-empty body; without one the plan carries empty heading and body and the source `missing` — nothing is invented. One plan per Automaton at `<data dir>/merge-plans/<id>.json` under the daemon's data directory, written atomically (a temporary file renamed into place); reads list the newest first, and per-project retention prunes to the newest N (N is at least 1). When the merge has landed and the shipped record is written, the daemon runs the shadow comparison in the background — time-boxed, the ship path never waits on it, a failure is logged and dropped, and absent a stored plan it does nothing: it reads the landed state from the base ref with `git show <ref>:<path>` (the working tree is never consulted) — the first `## ` heading of `CHANGELOG.md`, the `var Version` line of each pipeline version file, the `Current release` line of `README.md` matching the planned version, and the registry table row naming the project and its Automaton — grades each difference **major** (a version wrong or missing, a planned heading, README line or registry row landing nowhere, a required parity issue landing nowhere, or a gate absent from the supervisor's manual gate record when one is supplied) or **minor** (wording drift on the heading, README line or registry row, or a parity issue landing where the plan did not require one), and records each difference as a defect candidate (status `candidate`, content-derived key, idempotent by content, one file per Automaton) under `<data dir>/defect-candidates/` — the `merge-comparisons/` store exists in the code but the post-ship flow does not write to it yet, and filing a candidate into the defect queue is a separate job. The plan and the comparison (once stored) have read-only reads on every operator surface — REST `GET /api/autonomous/merge/shadow` (per project: the stored plans, the stored comparisons and per-release-size matched/different counts), the `autonomous_merge_shadow_status` MCP tool, `datawatch autonomous merge-shadow status` and `compare <automaton-id>`, the comm verb `autonomous merge-shadow`, and the Merge shadow card on the Automation Setup and policy sub-tab (rendered only when the shadow is enabled and at least one comparison is stored); the defect candidates have no read surface — they are the JSON under the data directory. The supervisor's manual merge stays the source of truth, and every difference the comparison grades is a defect to review. Four settings govern it: `autonomous.merge_shadow.enabled` (on unless set to `false`), `autonomous.merge_shadow.gate_timeout` (the Go-duration budget each trial-merge gate gets, default `20m`), `autonomous.merge_shadow.plan_keep` (how many plans are kept per project, default 200) and `autonomous.merge_shadow.agreement_streak` (the agreement streak threshold, default 5; see howto/merge-shadow.md); a value that is not a boolean, a positive Go duration, or a whole number of at least 1 is refused with the problem named and nothing is written. See [howto/merge-shadow.md](howto/merge-shadow.md) for the settings and every way they are set, [howto/merge-pipeline.md](howto/merge-pipeline.md#the-shadow-merge-plan) for the walkthrough and [plans/2026-11-03-autonomy-handover-plan.md](plans/2026-11-03-autonomy-handover-plan.md) (Phase 4) for the modes, defaults and exit criteria.

### Shadow merge checks

The checks the shadow merge applies before and inside the trial merge; each names its finding and whether it blocks. The operator view is [howto/merge-shadow.md](howto/merge-shadow.md#the-plans-checks-and-gates).

**Unmerged task branch check** (`unmerged_task_branch`, blocks) — the first step of the shadow merge plan: the Automaton's task branches, named after the work branch (`<branch>--tasks-…`). A task branch holding commits the work branch does not blocks the plan with one finding per such branch, and the trial merge is not run — every gate is recorded as skipped with the reason. It is the replay of the event where a task branch is never merged back into the work branch.

**Stray root file check** (`stray_root_file`, blocks) — a check of an Automaton branch as a merge candidate: one blocking finding per file the branch adds to the repository root that the base ref does not track in its root tree, dotfiles included. It is the replay of the event where a stray tracked file sits at the repository root.

**Intermittent gate** (`intermittent`) — a test gate status, not a finding: the `test` gate that fails on its first run is rerun once, and when the rerun passes the gate is recorded `intermittent` (detail `intermittent: passed on rerun`), the first five lines of the failing run are kept as the gate's finding, and it does not block the plan. When the rerun does not pass, the first failure stands with the rerun's status noted in the detail. A test that fails and then passes is a real intermittent failure (race, unbounded wait, shared resource, timing assumption), never load.

**Intermittent defect record** — the defect the daemon files when a test gate is recorded `intermittent`: one record per test name and commit under test, holding the test name, the failure message, the first lines of the failing output and the date, plus one alert titled with the test name. Repeated runs at the same commit do not add records. The merge is not blocked, the record has no mode and nothing to repair automatically, and it is listed under `intermittent_defects` on the housekeeping-repairs views ([howto](howto/automaton-housekeeping-repairs.md)); the fix is in the test or the code.

**No-load-patterns check** — `scripts/check-no-load-patterns.sh`, run as `make check-no-load-patterns` and inside `make lint`: fails with `file:line` hits when a test file, `testdata/` folder or test helper holds a CPU burner or stress pattern (busy loops, `yes > /dev/null`, `stress`, `stress-ng`, `cpuburn`, empty `for {}` spins, load-described `GOMAXPROCS` fan-outs). There is no allow-list. A failing test is a defect, never "load" ([testing](testing.md#no-load-patterns-in-tests)).

**Pinned lint tool** (`lint_tool_unavailable`, blocks) — when the trial merge's CI workflow pins a golangci-lint version, the lint gate runs that version from the daemon's own tools folder (`<data dir>/tools/golangci-lint/<version>/`), else the `PATH` binary when it already reports the pin, else a copy installed into the tools folder; the gate detail records which binary ran and where it came from, so the host copy does not matter. When the pinned version cannot be provided the gate fails with one blocking `lint_tool_unavailable` finding naming the reason.

**Lint version pin** (`lint_version_mismatch`, blocks) — the lint gate records the local golangci-lint version against the version the trial merge's CI workflow pins (the `version` of a `golangci/golangci-lint-action` step in `.github/workflows/ci.yaml`, a `v`-prefix aside) and fails with one blocking finding on a difference; a local version that cannot be determined counts as a mismatch. A CI workflow that pins nothing (no workflow, no such step, no `version` key) or that cannot be read leaves the gate's status unchanged — the local version, and the reason the pin could not be read, go into the gate's detail only. It is the answer to the lint that differs between local and CI.

**Verification lint gate** — the check that lints only the Go packages a branch changed against its merge base with `main` (untracked files included) by running `golangci-lint run <packages>` from the project directory. It passes when no Go package was touched or the linter exits zero; linter findings fail it, with the output kept and capped at 8 KiB; a linter that cannot start is an error, never a pass. It runs after every passing task or story verification script, before the guardrails; a failure records a `lint-gate` block verdict (severity high) and a non-retryable `content` failure reason with source `lint-gate`. It is skipped when the unit has no project directory or that directory is not a git work tree. It is the answer to a task's verification passing a branch that fails the repository lint (2026-11-15). The operator view is [howto/automaton-verification-scripts.md](howto/automaton-verification-scripts.md#7-the-verification-lint-gate).

**Plans-link gate** (`check-plans-links`) — the gate runner runs the project's make target of a given name; build, lint and test are the standard three in the trial merge, and the plans-link checker runs as a gate the same way: a broken plan link fails the gate, and its output reports only the count of broken targets, never their names. It is the answer to the broken plan link.

**Review-head check** (`review_stale`, blocks) — the step-0 check of a recorded review verdict against the branch head: a verdict that names no commit, one that names a commit the repository does not have, or one that names a commit other than the head of the Automaton's work branch blocks the plan with a single finding, on the grounds that the verdict says nothing about the work the merge would land; the trial merge and every gate are then recorded as skipped. A review of the current head produces no finding. It is the replay of the review that passed on a branch that did not yet carry its fix.

**Failing tests in the `test` gate** — when the `test` gate fails, its detail names the failing tests — parsed from the run's `go test -json` output, in the order they failed — and the first assertion line of the first failing test, kept next to the original detail; a run that produced no JSON, or none that failed, keeps its original detail unchanged. It is the replay of the stale settings-count test: a test whose expected count is out of date fails the gate with its name and its assertion in the plan.

**Gate process group** — each gate's process starts in its own process group, and every process the gate starts inherits it (unix); when the gate's timeout ends, the whole group is killed, so a hung PWA test suite — and whatever it spawned — ends with the gate and nothing it started survives it. On platforms without process groups the timeout kills the gate's process only, as before. It is the replay of the PWA suite that hangs: the gate can only end by its timeout, and afterwards the suite is gone.

**Version planner** — the planned version is derived from the default branch's version and the release size alone (a base that is not a plain `X.Y.Z` version, or a size that is not `patch`, `minor` or `major`, is refused; nothing is guessed); planning twice over the same base and size gives the same planned version, and a trial tree that already carries the bump is reported as already at planned, with the version files that differ from the planned version — or hold no version at all — listed as stale. The trial tree's version is never used to compute the bump, so a bump applied twice cannot move the version twice.

### Deploy after ship

After a release ships, the daemon can build the new binary, install it, restart itself and check it is healthy. `GET /api/autonomous/deploy` shows the settings and the last deploy. Each step is a setting, and **an empty value means nothing runs** for that step, so a daemon with no deploy settings never deploys. Every setting is a config key on all five access methods: YAML (`autonomous.deploy.<key>` in the config file), CLI (`datawatch config set autonomous.deploy.<key> <value>`; `datawatch autonomous deploy` shows the state), Web UI (Settings > Automation > Deploy), REST API (`PUT /api/config` with the key, `GET /api/autonomous/deploy` for the state) and comm channel (`configure autonomous.deploy.<key>=<value>`; the `deploy` verb shows the state).

### autonomous.deploy.build_command

**Deploy build command** (`autonomous.deploy.build_command`, empty by default) — a shell command line run in the project that builds the new binary. Empty means nothing runs for this setting. Set it in YAML under `autonomous.deploy`, with `datawatch config set autonomous.deploy.build_command`, in Settings > Automation > Deploy, through the REST config API, or from a comm channel with `configure autonomous.deploy.build_command=<value>`.

### autonomous.deploy.install_command

**Deploy install command** (`autonomous.deploy.install_command`, empty by default) — a shell command line that installs the new binary over the installed one. It needs the installed binary path; the previous binary is copied aside first. Empty means nothing runs for this setting. Set it in YAML under `autonomous.deploy`, with `datawatch config set autonomous.deploy.install_command`, in Settings > Automation > Deploy, through the REST config API, or from a comm channel with `configure autonomous.deploy.install_command=<value>`.

### autonomous.deploy.binary_path

**Deploy installed binary path** (`autonomous.deploy.binary_path`, empty by default) — the absolute path of the installed binary that the install command replaces. The previous binary is copied from here before it is replaced, so a failed deploy can go back to it. Empty means nothing runs for this setting. Set it in YAML under `autonomous.deploy`, with `datawatch config set autonomous.deploy.binary_path`, in Settings > Automation > Deploy, through the REST config API, or from a comm channel with `configure autonomous.deploy.binary_path=<value>`.

### autonomous.deploy.restart_command

**Deploy restart command** (`autonomous.deploy.restart_command`, empty by default) — a shell command line that restarts the daemon after the install. Empty means nothing runs for this setting. Set it in YAML under `autonomous.deploy`, with `datawatch config set autonomous.deploy.restart_command`, in Settings > Automation > Deploy, through the REST config API, or from a comm channel with `configure autonomous.deploy.restart_command=<value>`.

### autonomous.deploy.health_check

**Deploy health check** (`autonomous.deploy.health_check`, empty by default) — a shell command line whose exit code 0 means the daemon is healthy. Empty means nothing runs for this setting. Set it in YAML under `autonomous.deploy`, with `datawatch config set autonomous.deploy.health_check`, in Settings > Automation > Deploy, through the REST config API, or from a comm channel with `configure autonomous.deploy.health_check=<value>`.

### autonomous.deploy.drain_timeout

**Deploy drain timeout** (`autonomous.deploy.drain_timeout`, 10 minutes when unset) — a duration such as `10m`: how long the deploy waits for running final-gate tasks to finish before it is deferred. Unset reads 10 minutes; it never turns the deploy on by itself. Set it in YAML under `autonomous.deploy`, with `datawatch config set autonomous.deploy.drain_timeout`, in Settings > Automation > Deploy, through the REST config API, or from a comm channel with `configure autonomous.deploy.drain_timeout=<value>`.

### autonomous.deploy.health_timeout

**Deploy health timeout** (`autonomous.deploy.health_timeout`, 2 minutes when unset) — a duration such as `2m`: how long the deploy waits for the health check to pass. Unset reads 2 minutes; it never turns the deploy on by itself. Set it in YAML under `autonomous.deploy`, with `datawatch config set autonomous.deploy.health_timeout`, in Settings > Automation > Deploy, through the REST config API, or from a comm channel with `configure autonomous.deploy.health_timeout=<value>`.

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


### Projects sub-tab

**Projects sub-tab** — the first sub-tab of the Automata view in the web app, to the left of Automation (the sub-tab order is Projects, Automation, Automata, Events, Templates; the old Projects link and the `#projects` address open it). It shows one registered project as a whole: its plans, the Automata and sessions under each plan, progress and cost in points, an expandable Gantt timeline, the finished history and the project's events; the effective policy and merge pipeline are not drawn on the page — the link next to the project name opens them in Settings. A selected project's page is split into **page sub-tabs** (a strip labelled Project sections), one body at a time: **Overview** (header and progress), **Plans**, **Gantt**, **Unassigned**, **History** and **Events** (notes and the project's event stream; in the strip only while the project has notes). The choice is remembered per browser (`cs_projects_subtab`, not on the daemon, so no REST, MCP, CLI, chat or config form) and a page sub-tab has its own address, `#projects/<name>/<subtab>`; `#projects/<name>` opens Overview. The picker and the show-empty checkbox stay above the strip, the strip scrolls sideways on a phone while the rest of the page stays within the screen width (the toolbar wraps, long lines wrap, wide code and tables scroll in their own box), the lists and cards use the shared **Cards / Tiles / Compact** card-mode switch (per browser, `cs_projects_view_mode`), and the old Projects address opens Overview. See [howto/projects-tab.md](howto/projects-tab.md#the-page-sub-tabs). A **Manage projects** block under the picker adds, edits and removes a project (name, directory, description, default backend); the rest of the project record is changed through REST, MCP, the CLI or the comm channel. Overview in the same picker is the cross-project in-flight view; it has no page sub-tabs but shows the in-flight sub-tabs (Inbox, Automations and the rest) in a strip with the same look as the page sub-tabs, each one clickable, with the choice remembered in the same browser value (`cs_projects_subtab`) and restored after a refresh (see [howto/projects-tab.md](howto/projects-tab.md#the-overview-sub-tabs)). Empty fields, empty sections and an empty event stream are hidden by default; the **Show empty lines and sections** checkbox shows them, remembered per browser (see *Show empty lines* below). See [howto/projects-tab.md](howto/projects-tab.md#manage-projects-in-the-web-app).

**Overview sub-tabs** — the strip of in-flight sub-tabs (Inbox, Automations, Resources, Progress and cost, Housekeeping) shown on the Projects sub-tab when **Overview** is picked. It looks like the project page sub-tabs (the strip labelled Project sections), not like the Automation view's own second row, and every sub-tab in it responds to a click or tap. The choice is kept in the browser value `cs_projects_subtab`, shared with the project page sub-tabs but never read back as one of them (the names do not overlap), and is restored once per page load; an unrecognised value opens Inbox. It has no address, no setting and no daemon side, so no REST, MCP, CLI, chat or config form. See [howto/projects-tab.md](howto/projects-tab.md#the-overview-sub-tabs) and, for the operator check, [operations.md](operations.md#projects-overview-sub-tabs-web-app).

<a id="scope-selector"></a>

**Scope selector** (the header scope button) — the funnel icon in the fixed app header (`id="headerScopeBtn"`), shown only on the Automata view, that opens the **Scope** window to choose **All**, one project, or one plan of a project. It replaced the separate project and plan selectors. While a project or plan is chosen the icon carries a small dot (**Filter active** for screen readers), and the Projects pages, the Automata list, the Events sub-tab, the Inbox and the Automations list show only that scope under a **Filtered to …** banner with a **Clear** button. The **Templates** sub-tab and the **Resources**, **Progress and cost** and **Housekeeping** sub-tabs are global: they stay unfiltered and say so while a scope is active. The choice is remembered per browser (`dw_scope`, never on the daemon) and shown in the Projects address (`#projects/<name>/<subtab>?plan=<id>`); the in-page **Project** picker is the same scope seen from inside the page. It is a display rule of the web app: no setting, and REST, MCP, CLI and comm channel return everything. See [howto/projects-tab.md](howto/projects-tab.md#the-header-scope-button).

**Inbox scope** — what the Inbox badge and cards show, and what the badge counts. The badge counts only items awaiting the operator's action: the actionable Triage records (the read-only *merge blocked* card does not count), the open questions, and the open held entries (the would-be list in shadow mode never counts); the security failure report and the supervisor watch findings keep rendering but do not count. The badge and cards follow the header scope (see **Scope selector**). With a project in scope they keep only that project's items (matched by the `project:<name>` system tag first, by directory only when no tag exists; an item with neither shows only under All), with a plan in scope only the items that name the plan's Automation or one of its Automata, and the badge reads "N waiting on you to act". Under All nothing is cut and the badge reads "N waiting on you to act across all projects". It is a display rule of the web app: no setting, and REST, MCP, CLI and comm channel return every item. See [howto/automation-tab.md](howto/automation-tab.md#what-the-inbox-counts).

**Tab strip (phone width)** — a row of tabs under the header of a web app screen: the Automata sub-tab bar, the Automation second row, the Projects bar, the Templates sub-tabs, the Automaton detail tabs, the session detail tabs and the Settings tabs. At phone width (600 px or less, or a touch screen) the strip stays on one line and scrolls sideways with its scrollbar hidden; a soft fade on an edge shows that tabs are hidden on that side, and the active tab is scrolled into view. It is a layout rule of the web app only: no setting, REST, MCP, CLI or comm form. See [howto/navigation-mobile-fit.md](howto/navigation-mobile-fit.md#tab-strips-at-phone-width).

**Phone page layout** — at phone width (600 px or less, or a touch screen) every top-level page of the web app (Sessions, Automata and its sub-tabs, Projects, Alerts, Observer, Dashboard, Settings) uses the geometry of the Sessions list: a 12 px side gutter, full-width cards with 12 px 14 px padding and an 8 px gap, and no sideways page scroll. On the Dashboard the stat bar wraps onto more lines with each counter kept whole and the burn-rate summary on its own line, and the card grid is one column; filter and control rows on the pages wrap instead of running off the right edge. A layout rule of the web app only, with no setting, REST, MCP, CLI or comm form; desktop layout is unchanged. See [howto/navigation-mobile-fit.md](howto/navigation-mobile-fit.md#page-layout-at-phone-width).

**Show empty lines (Projects sub-tab)** — a per-browser checkbox on the Projects sub-tab, off by default. Off hides empty header fields, the "none recorded" plan document lines, empty Plans and Unassigned Automata sections and an empty event stream; on draws them. On the project's **Events** page sub-tab, off also folds the notes that say only that something is empty or never happened into one summary line with a count (real problems, such as a failed roll-up, are always listed one per line); on lists each note on its own line. It is stored in the browser (`cs_projects_show_empty`), not on the daemon, so it has no REST, MCP, CLI, chat or config form. See [howto/projects-tab.md](howto/projects-tab.md#the-show-empty-checkbox).

**Project (alias)** — a name mapped to an absolute directory, with an optional default backend and description. Sessions, Automata and the policy refer to a project by this name. Registering one creates an alias; deleting one does not touch the directory or stop any session. The web app's Projects sub-tab has an add, edit and remove form for aliases.

**Project description** — the free-text description that sits beside the alias's directory and default backend. Stored with the alias and shown in the **Manage projects** list (web app) and the project page Overview (hidden when empty). Set or replace it from the **Manage projects** form, REST `POST /api/projects`, MCP `project_upsert`, `datawatch projects upsert --description` (a `set` never touches it), or the comm channel's `project set <name> description <value>`; read it back from `datawatch projects get` or `show`, `project_list`, `project show` or any project read. An update without a description keeps the stored one on every surface: the **Manage projects** form pre-fills it and re-sends it, and the CLI's `projects upsert` and `projects set`, MCP `project_upsert` and the comm channel carry the stored one forward — a change of the directory, backend or another field never blanks a saved description. The one writer that still replaces it is a raw REST `POST /api/projects`: it writes the description it is sent (blank when the field is missing), as does its `default_backend`; every other record field — owner and repository among them — keeps its stored value when left out of the body. A description is cleared by sending an explicit empty one, from the form, `projects upsert --description ""`, MCP or a REST body. See [howto/projects-tab.md](howto/projects-tab.md#manage-projects-in-the-web-app).

**Project timeline** — when each plan, Automaton and task of one project started and finished, read one level at a time so a client does not fetch every task up front: plans first (the default), then the Automata under a plan, then the tasks of one Automaton, or one plan's Automata and tasks together with `focus`. Each row has a start and an end (the older `first` and `last` carry the same times); work still running has no end and is drawn to now. An Automaton row spans its first task start to its last task end; an Automaton with no started task is read from its created time to its completion time. Each row also has a status (completed, running, waiting, blocked, failed or cancelled — a shipped Automaton reads completed whatever its record says) and a release, and task rows carry the tasks they wait for (`depends_on`); Automata-level and focus replies add chain edges between Automata. A row of kind `unassigned` holds Automata that no plan holds. It is a pure read with no configuration key. REST `GET /api/projects/{name}/timeline`, MCP `project_timeline`, CLI `datawatch projects timeline`, chat `project timeline`. Not the same as the cross-project *Timeline chart* or the dashboard's 6h Gantt. The Projects sub-tab Gantt in the web app draws this read: bars coloured by status, a tooltip with status, dates and release, dependency edges (the **Edges** toggle, remembered per browser), and a default range fitted to the visible rows. See [howto/projects-tab.md](howto/projects-tab.md#the-project-timeline).

**Unassigned (project line)** — the one line named `unassigned` in a cost chart grouped by project (`group_by=project`, the **Project** choice on the Automata cost cards). Each Automaton's spend is placed under its project in this order: its own project link, then its parent Automaton's project, then the name of its project directory. An Automaton that resolves to none of these has no project, and all of its spend, however many Automata that is, is drawn on this one line. It is not a project and not an Automaton. The Unassigned page of the Projects sub-tab lists those Automata one by one. **Unattributed** is different: it is spend with no owner at all (a session with no Automaton id), with its own row under the Automaton, Automation and model groupings; under the project grouping that spend is counted on the unassigned line. The cost-report key for the line is `unassigned`. A project name is never an Automaton id: the Automaton's own id is skipped at each step (operator review: a link naming a different Automaton's id is not filtered). See [howto/automata-cost.md](howto/automata-cost.md#cost-grouped-by-project) and [howto/cost-report.md](howto/cost-report.md#grouping-by-project).

**Focus view** — the page the **Focus** button opens in the web app: one Automation's Automata and tasks on a single timeline, from its first task to its last, each end extended by the 2 percent margin, with presets, wheel zoom and drag pan, and **Back**. Its "First task … – last task …" header prints the two ends of that displayed range — not the exact dates of the first and last task, so each can read up to a day off — in the browser's local date format. The button is on a plan's row in the Projects Gantt, on the plan card and in the header of the Automation page. It reads the project timeline with `focus` (unpaged) and writes nothing. An Automation with no task yet reads as planned. See [howto/projects-tab.md](howto/projects-tab.md#focus-on-one-automation).

**Project record** — the optional fields a project alias can carry beyond its directory: owner, repository, default branch, plans, target, lane defaults, weekly share, branch retention and major trigger. An absent field keeps its stored value on the operator surfaces (form, CLI, MCP, comm channel) — the exceptions are `policy`, which a write without one clears (CLI `projects upsert` and MCP `project_upsert` send none, so they clear the tier), and a raw REST body, which also writes `description` and `default_backend` as sent (see [Project description](#project-description) and the *policy* notes in [howto/projects-tab.md](howto/projects-tab.md#troubleshooting)).

**Arc** (deprecated name of a slot in the older project plan list; a plan record replaces it, see [Plan record](#plan-record)) — one planned slot in a project's order: an Automation (its `automation_id`), a `priority` that sets its place on the page, and a `state` of `planned`, `active` or `done`. The plan list is replaced as a whole. A plan is an Automation, not an Automaton.

**Project target** — the outcome a project works toward: a name and a definition of done, the point at which the target counts as reached.

**Lane defaults** — the lane, backend and model that new work in a project starts with (lane `gpu`, `claude` or `any`). Recorded on the project; they do not yet decide where an Automaton runs.

**Weekly share** — the percentage (0 to 100) of the weekly usage budget a project should get, with the daemon-wide default `projects_defaults.weekly_share_percent`. Stored and shown; no scheduler reads it yet.

**Branch reaper** — the pass that backs up, then deletes, the finished task branches of settled Automata; `autonomous branch-reap` reads its report and `branch-reap-run` acts. Nothing with unmerged work is deleted and every deleted branch is backed up first. See [howto/automation-cleanup-passes.md](howto/automation-cleanup-passes.md).

**Worktree cleanup skip reasons** — why the worktree cleanup left an entry: `dirty`, `held`, `commits_not_on_base`, `no_base_branch`, `fresh`, `live_session`, `live_lease`, `permission_denied`, or the per-pass limit. Held entries carry the one release action that frees them. See [howto/automation-cleanup-passes.md](howto/automation-cleanup-passes.md).

**Remote reap** — the operator-run pass that deletes the remote branch of a fully merged Automaton after a verified backup. `autonomous.remote_reap_enabled` defaults to false (report only); `autonomous.remote_reap_retention_days` (default 30) is the age a merged branch must reach. See [howto/automation-cleanup-passes.md](howto/automation-cleanup-passes.md).

**Temp sweep skip** — a temporary path the sweep could not read or remove; it is counted as skipped with reason `unreadable`, not as a failure, and removed once readable. See [howto/automation-cleanup-passes.md](howto/automation-cleanup-passes.md).

**Branch retention (project)** — the days a project keeps finished Automaton branches, with the daemon-wide default `projects_defaults.branch_retention_days`. Stored and shown; the worktree cleanup does not read it yet.

**Dependency update** — the weekly Automation that applies every dependency version update (Go modules, Docker base images, GitHub Actions, build-argument image tags) in one Automaton. Dependabot no longer opens version pull requests; it is limited to security updates by `open-pull-requests-limit: 0` in `.github/dependabot.yml`, enforced by `make check-dependabot-security-only` inside `make lint`. Three settings control it: `autonomous.automation.dependency_update.enabled` (default on), `.schedule` (default `monday 05:00`) and `.max_majors` (default 5, 0-20); the MCP tool `dependency_update_status` reports them with the next and last run. Each run is reported as one `check` row on the Automata Events tab, and an alert is raised only when a run needs the operator. Dependabot's "security updates" switch in the hosting settings must stay on, and version pull requests opened before the change need closing by hand; the how-to's operator checklist lists both. See [howto/automation-dependency-update.md](howto/automation-dependency-update.md).

**Toolchain freshness check** — a development tool for the machine that builds datawatch, not a product feature: `scripts/check-toolchain.sh` compares the installed Go, `gosec`, `govulncheck`, `gitleaks`, `golangci-lint`, `trivy` and `goreleaser` with the newest stable release of each and prints a table with one status per tool: `CURRENT`, `PATCH-BEHIND` (informational), `BEHIND` (a minor or major behind), `GO-BEHIND` (Go is older than the newest stable patch of the `go.mod` line), `MISSING` (not on `PATH`) or `UNREACHABLE` (a version could not be read or parsed, including a tool built from source that prints only a placeholder such as `dev`). It exits `1` when Go is `GO-BEHIND` or any tool is `BEHIND`, and `0` otherwise, so a network error never fails it. The tool list is `toolchain.yaml` at the repository root; `scripts/check-toolchain_test.sh` tests the script offline through the `TOOLCHAIN_FIXTURE_DIR` hook. It only reads: it runs each tool's version command and makes HTTPS reads. It is run by hand: the daemon does not run it, there is no setting, and it has no REST, MCP, CLI, chat or app surface. It is different from the weekly dependency update, which updates the repository's dependencies, not the build host's tools. See [howto/toolchain-freshness-check.md](howto/toolchain-freshness-check.md) and [operations.md](operations.md#build-host-toolchain-freshness-check).

**Whole-tree local-environment leak check** — the automatic enforcement of the `AGENT.md` "No local-environment leaks in git" rule across the whole tracked tree at once: `scripts/check-no-local-leaks.sh` fails on any value tied to one operator's machine — a private or internal IP address, an absolute home-directory path, a personal email address, or a machine-specific token the operator lists in the gitignored `.leak-patterns.local` at the repository root (seeded with `make leak-patterns-init`, whose output prints counts and never a name). The four classes are `address` (private/internal/link-local IPv4, ULA and tailnet IPv6, MagicDNS hostnames), `home` (absolute home paths for any user), `email` (anything not on `scripts/leak-check.allowed-emails`) and `local` (the operator's own fixed strings); the generic patterns under `scripts/` are configuration, written self-clean so the pattern file never matches itself, and an absent or empty `.leak-patterns.local` prints `local patterns: skipped` and carries on — never an error. The report is CI-safe by design: one line per finding, `path:line: class`, then a per-class count summary, and nothing else — the matched text is never printed, so the output is safe in CI logs or a commit message. It exits `0` clean, `1` findings, `2` configuration error. It is a development tool for the repository, not a daemon feature: no setting, no REST, MCP, CLI, chat or app surface. It is wired into three gates — the pre-commit chain (`hooks/pre-commit-leak-check`, the middle step of `hooks/pre-commit-docs-sync`, refusing the commit on any finding with no escape hatch), `make lint` (via `check-leaks`), and `scripts/release-smoke.sh` before a release (skippable only with the operator-owned `DW_SKIP_LOCAL_LEAKS_CHECK=1`). It scans the tracked tree only: commit messages, issue and PR comment text are not scanned and stay the surface of the narrower last-commit diff check. `make check-leaks` runs a second scan over the untracked surface the tree check cannot see — every tracked and untracked report under `docs/plans/reports`, copied to a throwaway directory and scanned with the same four classes (`make check-generated-reports-leaks`, `scripts/check-generated-reports-leaks.sh`) — and the report generators themselves strip the host prefix from the session ids they render (a test renders every report kind with a fake host name and asserts the host appears nowhere in the output), so a generated gate report no longer prints the operator's private hostname. Note the generated reports folder scan is a step of `make check-leaks` (hence `make lint`) only: the pre-commit hook and the release smoke run the whole-tree check alone, so `make lint` or the standalone scan is the gate that covers the reports folder. Findings are fixed with the prescribed placeholders, operator config or an environment variable, or by scrubbing peer/node names with `scripts/scrub-peer-names.sh` (dry-run emits `path: N` counts only). See [howto/local-env-leak-check.md](howto/local-env-leak-check.md) and [operations.md](operations.md#whole-tree-local-environment-leak-check).

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
| Dependency update (weekly Automation, security-only Dependabot) | [`howto/automation-dependency-update.md`](howto/automation-dependency-update.md) | [`plans/2026-11-11-weekly-dependency-update.md`](plans/2026-11-11-weekly-dependency-update.md) | — |
| Toolchain freshness check (build-host development tool, run by hand; not a daemon feature) | [`howto/toolchain-freshness-check.md`](howto/toolchain-freshness-check.md) | — | — |
| Whole-tree local-environment leak check (repository development tool: pre-commit gate, `make lint`, release smoke; not a daemon feature) | [`howto/local-env-leak-check.md`](howto/local-env-leak-check.md) | — | — |
| Shadow merge plan (Phase 4, shadow mode: plans the merge, never applies; the post-ship comparison grades each difference and records a defect candidate per difference) | [`howto/merge-pipeline.md`](howto/merge-pipeline.md#the-shadow-merge-plan) | [`plans/2026-11-03-autonomy-handover-plan.md`](plans/2026-11-03-autonomy-handover-plan.md) (Phase 4) | — |
| Security failure report (stop and report, shadow first) | [`howto/automation-security-failure.md`](howto/automation-security-failure.md) | [`plans/parity-issues/security-failure-exception.md`](plans/parity-issues/security-failure-exception.md) | — |
| Automaton worktree cleanup (report first, disk alert, cache-folder sweep, no-space gate reason) | [`howto/automaton-worktree-cleanup.md`](howto/automaton-worktree-cleanup.md) | [`plans/parity-issues/2026-10-30-automation-cleanup-parity.md`](plans/parity-issues/2026-10-30-automation-cleanup-parity.md) | — |
| Automaton uncommitted-work check (hold, warn or off) | [`howto/automaton-uncommitted-work.md`](howto/automaton-uncommitted-work.md) | [`plans/parity-issues/2026-10-30-automaton-uncommitted-work-parity.md`](plans/parity-issues/2026-10-30-automaton-uncommitted-work-parity.md) | — |
| Automaton protected paths (hold, warn, block or off; rules read from the base commit) | [`howto/automaton-protected-paths.md`](howto/automaton-protected-paths.md) | — | — |
| Automaton pending items (hold or skip, attach, optional file registry) | [`howto/automaton-pending-items.md`](howto/automaton-pending-items.md) | [`plans/2026-10-28-automation-pending-item-member-proposal.md`](plans/2026-10-28-automation-pending-item-member-proposal.md) | — |
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
| Multi-server management | [`howto/multi-servers.md`](howto/multi-servers.md) | v7.2.0 | REST proxy + aggregated endpoints; picking a server from the header server icon button (Servers window) re-renders the current view in place |
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
| Model placement (missing-model policy + ask queue) | [`howto/compute-placement-ask.md`](howto/compute-placement-ask.md) | Unreleased (<a href="CHANGELOG.md#unreleased">CHANGELOG</a>) | `GET /api/compute/placements` + `POST …/approve\|deny` + `compute_placement.*` |
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
| Dedicated send_input endpoint | [`howto/sessions-deep-dive.md`](howto/sessions-deep-dive.md) | v8.12.9 | POST /api/sessions/send bypasses text-command parsing; scheduleSettleMs applies to all sources; every send waits for the paste to show, presses Enter, and retries Enter at most twice while the text is still unsent (see Message submission) |
| LLM-optional memory recall | [`docs/memory.md`](memory.md) | v8.13.3 | `Recall`/`RecallAll`/`RecallInNamespaces`/`RetrieveContext` fall back to LIKE keyword search when Ollama offline; `LazyReembed()` back-fills vectors when LLM returns; summarizer returns stub instead of error |
| FCM push payload enrichment | [`howto/push-notifications.md`](howto/push-notifications.md) | v8.13.1 | session_id/session_name/last_response in session_state_changed and waiting_input push events |
| Goose backend | [`docs/llm-backends.md`](llm-backends.md) | v8.13.36–v8.14.0 | provider/model/api_key env-var injection, MCP channel bridge, `agent-goose` container, `channel_enabled` config flag |
| Vision input system | [`docs/config-reference.yaml`](config-reference.yaml) · [`docs/mcp.md`](mcp.md) | v8.15.0 (pending) | `vision.*` config, `POST /api/vision/describe`, comms image→description injection, `vision_describe` MCP tool, `image_paths` on start_session/send_input, council `image_path`, skills `accepts_images`, `remember [image]` |
| Automaton lifecycle event subscriptions | [`howto/automaton-lifecycle-subscriptions.md`](howto/automaton-lifecycle-subscriptions.md) · [`flow/lifecycle-subscription-flow.md`](flow/lifecycle-subscription-flow.md) | Unreleased (Phases 3–4 of `plans/2026-10-13-prd-lifecycle-events.md`) | persisted encrypted store, engine with one-shot/expire/coalesce, comm/session(bridge, SendInput)/topic/alert/mcp_queue sinks, pending-events queue + polling, update + test-fire, REST + MCP + CLI + owner-scoped comm verbs + PWA Subscriptions tab, live re-attach (no restart) |
| Git hosts | [`howto/git-hosts.md`](howto/git-hosts.md) · [Git hosts](#git-hosts) | Unreleased (Phase 4 of `plans/2026-11-03-autonomy-handover-plan.md`) | `git.hosts[]` config model (name, kind, base_url, credential_source host_login/vault/broker, vault_secret, sign_tags, signing_key) validated at load; readiness check (gh/glab auth status, git ls-remote) with 15-second deadline; connection test; `GET /api/git/hosts` + `POST /api/git/hosts/{name}/test` (config-read capability) plus the MCP `git_hosts_status` / `git_hosts_test` tools, the CLI `datawatch git-hosts` list, and the comm channel `configure git.hosts list`; writes on every operator surface — REST `PUT /api/config` (`git.hosts` list or `git.hosts.<name>.<field>` field keys, one atomic judged patch — a single write can set `credential_source: vault` with its `vault_secret`), `datawatch config set git.hosts.<name>.<field>`, `configure git.hosts.<name>.<field>=...` — the host must exist; whole-list writes create or remove; token never in any response (`[redacted]`); per-host operations — gh/glab PR/MR + issue + release creation, clone/push per kind, "no forge API" on plain-git kinds; the repo CI watch lists a watched repository's runs through a `github`/`gitlab` host of yours (read-only, no `hung` state); the post-ship `parity_issue` hook files through the host when the pipeline runner carries it (today still `gh issue create`); every completed facade operation audit-logged as `host_git_operation`; the signed-tag primitive is in (a tag created against a host with `sign_tags: true` is signed with `git tag -s`; an empty `signing_key` omits `-u` and leaves the key to git's own configuration — the daemon performs no refusal) but no shipping path calls it yet; the PWA **Git hosts settings card** in Settings → General (per-host name/kind/base URL/credential source/vault secret name, the readiness row, Test connection by saved name, Add/Remove/Save as a locally-validated whole-list `PUT /api/config` — `sign_tags`/`signing_key` are preserved on save but not edited there, and `broker` is exposed as a credential source) — the release-and-merge loop is the follow-up |

Every core feature now has a dedicated how-to. Per-channel coverage on each is being expanded so the same walkthrough works across PWA / Mobile / REST / MCP / CLI / Comm / YAML — every operator workflow is reachable from every surface.

## Git hosts

A **git host** is one named git host in the `git.hosts` list that the daemon may push to: a GitHub, a GitLab, a Gitea, a Bitbucket, or a plain git remote. Each entry carries the host `kind`, its base URL (required for the self-hosted kinds — `gitea`, `bitbucket`, `git`; empty means github.com or gitlab.com for the public ones), how its token is obtained — the **credential source** — and whether the tags the daemon creates for it are signed.

**Credential sources.** `host_login` (the default) uses the login already on the machine — `gh auth login` / `glab auth login` — and adds nothing to the environment. `vault` reads the token from the secrets-vault entry named in `vault_secret` and places it in the one host command's environment as `GH_TOKEN` (github), `GITLAB_TOKEN` (gitlab) or `DATAWATCH_GIT_TOKEN` (everything else) — never in the daemon's own environment, the config, or any error message. `broker` lets the credential broker supply the token; the daemon names the source so a push or tag audit line can carry it.

**Readiness.** `GET /api/git/hosts` reads every host plus a live readiness check: for github, `gh auth status` (account, scopes, the `repo`/`workflow` scope booleans); for gitlab, `glab auth status` (the `api` scope); for the other kinds, a `git ls-remote` reachability probe. A missing login or scope is reported as a plain-language `problem` with a `fix_command`. Every external command is bounded by a 15-second deadline.

**Connection test.** `POST /api/git/hosts/{name}/test` is the readiness check plus one read-only probe (`gh api user`, `glab api user`, or `git ls-remote`) and reports the account connected as. An unknown name is a 404.

**Surfaces.** Readiness and the test are on every channel: REST (`GET /api/git/hosts`, `POST /api/git/hosts/{name}/test`), MCP (`git_hosts_status` with no arguments — the same data as the REST list; `git_hosts_test` with the host `name`), the CLI (`datawatch git-hosts` lists every configured host with its readiness, against the running daemon), the comm channel (`configure git.hosts list` — each host's name, kind, credential source and vault entry name), and the **Git hosts settings card** in PWA Settings → General ([howto](howto/connect-git-host.md)) — the same list with each host's readiness row (Ready / Not logged in / Status unknown, the account, the repo-scope status), the plain-language problem and fix command when the daemon found one, and a Test connection button that runs the test by the host's saved name. The **config write** is on every operator surface too: the `git.hosts` list or a `git.hosts.<name>.<field>` field key on REST `PUT /api/config` (a single write may set `credential_source: vault` together with its `vault_secret`), `datawatch config set git.hosts.<name>.<field> <value>`, and the comm channel `configure git.hosts.<name>.<field>=<value>`. A field key addresses an **existing** host — an unknown host is rejected and not created — and a patch is judged atomically on the load rules before anything is applied, so a bad field or value leaves every host untouched. Creating, renaming and removing a host is a whole-list write (the `hosts:` block in YAML, the `git.hosts` key on `PUT /api/config`, or the card's Add / Remove / Save — the card validates the whole list locally first and then sends the entire list in one `PUT /api/config`).

**The token rule.** No read — REST, CLI, MCP, comm, PWA — returns a token. A vault host shows its entry's **name** (`vault_secret`), and any token-shaped value or `user:password@` credential that reaches an answer is written out as `[redacted]`.

**Per-host operations (what the daemon does with a host).** The providers shell out to each host's own tooling — the same login the readiness check verifies. GitHub: `gh pr create` (PR URL), `gh issue create` (issue URL), `gh release create <tag>` (release URL), and a ref's CI runs via `gh run list --repo <owner/name> --branch <ref>` (the list the repo CI watch takes for a watched repository on this host; 15-second bound). GitLab: `glab mr create` (merge request URL), `glab issue create` (issue URL), `glab release create <tag>` (release URL) — all new; the GitLab provider was a not-implemented stub before — the ref's pipelines via `glab api projects/<owner%2Fname>/pipelines?ref=<ref>` (the CI watch's list, 15-second bound) — and clone/push, where an HTTPS remote's local git config sets the credential helper to `glab auth git-credential` so git rides the `glab` host login (an SSH remote is unchanged). The plain-git kinds (`git`, `gitea`, `bitbucket`): clone/push via plain `git`; pull requests, issues and releases return the explicit "no forge API integration; open the change in the host's web interface" answer, and the CI-run list is not attempted. Every CLI call is bounded (30-second cap) and runs with the credential source's environment slice; no token is read, stored or logged by datawatch; GitLab token minting remains unimplemented (the broker's mint flow is GitHub-only).

**Audit.** Every completed facade operation — change request (`mr`), issue, release, tag — writes one entry to the operator action log under the action `host_git_operation`: the host **name** and kind, the operation, the repository, the ref or title, and the credential source (`host_login`, `vault` or `broker`). A token is never part of the entry, and a failed audit write never fails the host operation it describes. Read it with `GET /api/audit?action=host_git_operation` ([howto/audit-logging.md](howto/audit-logging.md)).

**Live consumers.** The Automaton/project facade (PR/MR open, issue creation), the repo CI watch's run listing (read-only — no rerun, cancel, deletion; no `hung` state, the listing has no start time), and the post-ship `parity_issue` hook when the merge pipeline's runner carries the project's remote and your host list (today the daemon's supervisor path runs with a plain runner, so its issues still go out with `gh issue create`). The signed-tag primitive is in — a tag created against a host with `sign_tags: true` is signed with `git tag -s` (an empty `signing_key` omits `-u`, leaving the key to git's own configuration — the daemon performs no refusal) — but no shipping path calls it yet.

**What is not here yet.** The list is validated at config load (every problem reported together; one invalid entry fails the load), and the per-project release-and-merge loop that consumes it — and with it `sign_tags`/`signing_key` in use — is the follow-up in [plans/2026-11-03-autonomy-handover-plan.md](plans/2026-11-03-autonomy-handover-plan.md), Phase 4. How to: [howto/git-hosts.md](howto/git-hosts.md); the PWA card: [howto/connect-git-host.md](howto/connect-git-host.md); operations: [operations.md](operations.md) § Git hosts.

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
- [`howto/automata-timeline.md`](howto/automata-timeline.md) — the interval chart of the Automata across all projects: every task as a bar with its start and end, blocked and waiting intervals, the today marker and the projected finish
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
- [`howto/git-hosts.md`](howto/git-hosts.md) — git push targets: the `git.hosts` list (kind, base URL, credential source, tag signing), setup and editing on each channel, readiness + connection test over REST / MCP / CLI / comm, per-host operations (PR/MR + issue + release on GitHub/GitLab, clone/push per kind, no forge API on plain-git kinds), the token-never-returned rule, and what is not here yet
- [`howto/federated-observer.md`](howto/federated-observer.md) — push-based multi-host stats aggregation
- [`howto/multi-servers.md`](howto/multi-servers.md) — register remote instances, header server icon button (opens the Servers window, also opened by a three-finger swipe up; selecting a host re-renders the current view in place; the chosen entry is remembered per browser), all-servers aggregation
- [`howto/compute-nodes.md`](howto/compute-nodes.md) — GPU/CPU node registry, kind taxonomy, observer peer binding
- [`howto/compute-placement-ask.md`](howto/compute-placement-ask.md) — missing-model policy (`ask` / `auto_load` / `refuse`), headroom and size settings, the one-shot placement ask queue and its five surfaces
- [`howto/v7-compute-migration.md`](howto/v7-compute-migration.md) — migrate deprecated compute node kinds to ollama/openai-compat
- [`howto/llm-registry.md`](howto/llm-registry.md) — named LLM registry, per-node model lists, failover routing
- [`howto/ollama-marketplace.md`](howto/ollama-marketplace.md) — browse and pull models from the embedded Ollama catalog
- [`howto/guardrail-library.md`](howto/guardrail-library.md) — SAST/secrets/deps/LLM grader scan profiles
- [`howto/file-service.md`](howto/file-service.md) — federated file service: upload/delete files, peers/ + discussions/ subdirs, config caps (v8.3.0)
- [`howto/discussion-scopes.md`](howto/discussion-scopes.md) — discussion scopes: WAL-backed per-discussion memory, participant sync, conflict resolution (v8.4.0)
- [`howto/dashboard.md`](howto/dashboard.md) — mission control: Network work map, Lane pulse, session cards, customisable cards
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

<a id="automaton-merge-state"></a>
**Automaton merge state** — `merge_state` on the Automaton record itself, derived by the daemon on every read and never stored. It takes the value `finished_not_merged` when the Automaton has finished but its working branch still holds commits that never landed on the base branch: the branch's work is done, but it is not in the branch it belongs to. It sits beside `merged_in` (the branch the merge landed into) and in the `merge_blocked` line (why the merge cannot land yet). Every operator-facing surface shows it: the Automaton list and detail cards in the web app, the Projects cards and the Gantt row, the REST reads (`GET /api/autonomous/prds` and `GET /api/autonomous/prds/{id}`), the `merge_state:` line in `datawatch autonomous prd-get`, the `autonomous_prd_get` tool, the chat `autonomous get` reply, and the archive guard — `datawatch autonomous archive` refuses to archive such a record until the work lands on the base branch (a rebased or squashed landing counts) or its branch is deleted. In the web app the value shows as an amber *Branch not merged* badge on the Automaton; the `?` next to it opens this entry. It never changes the Automaton's status; it only tells you where the branch's work stands. The value follows git, not the Automaton store: the git comparison is cached for one minute per record, and the Projects roll-up (which is cached between Automaton writes) is rebuilt after that same minute whenever it holds a finished Automaton with a work branch, so a branch you merged by hand clears its badge within about two minutes without any Automaton being written.

**Merge blocked** — a finished task whose branch cannot merge into the Automaton branch yet. The task card shows one line with the reason and the next action, and Triage lists the Automaton as *waiting to merge* with the reason, how long it has been blocked and the action to take. Nothing is shown when no merge is blocked.

**Merge conflict policy** (`autonomous.parallel.merge_conflict_policy`) — what happens when a task branch does not merge cleanly. `additive_then_model` (the default) keeps both sides when the two changes only add lines, and asks the merge conflict model to resolve anything else. `block` holds the task at once. `triage` raises the conflict as a question for the operator.

**Merge conflict model** (`autonomous.parallel.merge_conflict_model`) — the model that resolves a merge conflict under `additive_then_model`. Empty, the default, reads the non-Opus daemon default (`sonnet`); a model the operator names, an Opus model included, is used as written.

**Merge retry interval** (`autonomous.parallel.merge_retry_seconds`, default `60`) — how many seconds the daemon waits before it tries again to merge a task branch whose merge is still `pending`. While an earlier merge is pending, the next task is held. `0` turns the timed retry off.

### Read-only tasks

A **read-only task** is a task flagged `read_only` because it changes no files, for example a task that only reads code and reports. While `autonomous.parallel.read_only_tasks` is on (the default), a read-only task skips the per-tree write lock and runs beside other tasks of the same Automaton instead of waiting for them. A task that is not flagged still takes the lock. Flag a task when you add or edit it (the `read_only` option of `autonomous_prd_add_task` and `autonomous_prd_edit_task`, or the task edit in the web app). The default is on because that is the operator's choice for this installation; other users may switch it off with `autonomous.parallel.read_only_tasks: false`, and every task then takes the lock as before.

A **report path** (`report_path`) is the one clean repo-relative file — under `.datawatch/reports/` — that a read-only task is allowed to write, so the task can still *report* (it is only read-only for everything else): the dirty check does not count that file in the `git status` selection (it is excluded from the status read itself), and it never counts the two untracked root [launcher tooling files](#uncommitted-work-check) (`worker-mcp.json`, `worker-starting-context.md`) — dropped from the status list, untracked root copies only — so any other stray change fails the task. It is only valid on a read-only task — a report path on a task that writes files is refused — and an empty value means the task writes nothing. Set it with the task's `read_only` mark (add/edit in the CLI, chat, MCP, API or web app); see [howto/automaton-parallel-tasks.md](howto/automaton-parallel-tasks.md#allow-a-read-only-task-one-report-file).

### Parallel speedup

**Parallel speedup** is how much faster an Automaton's tasks finished than they would have one at a time: the sum of the task durations divided by the time from the first task start to the last task end. It is recorded when the Automaton completes and shown with the read-only share in the Parallel execution card; see "Parallel execution metrics" above for every field. A speedup of 1 means no overlap. The default settings are the operator's choice and other users may change them; the how-to is [howto/automaton-parallel-tasks.md](howto/automaton-parallel-tasks.md).

### Planning route

A **planning route** is the choice of planner (an LLM backend and a model, plus its escalation targets) that drafts an Automaton's plan when it is decomposed. It comes from the first rule in `autonomous.planning_routing.rules` whose match fields all pass; if routing is off or no rule matches, the global planner is used, and a planner set on the Automaton itself always wins. With `shadow` on, the route is recorded (and counted as would-route in the scorecard) but not applied. Routing is off by default with no rules, and the example rules are not active. How-to: [howto/automata-planning-routing.md](howto/automata-planning-routing.md).

### Task type

A **task type** is the class a task is sorted into so the task-routing table can pick its LLM: `docs_set`, `changelog_entry`, `parity_issue_draft`, `bounded_load_step`, `version_note`, `locale_strings`, `tests`, `formatting_cleanup`, `ui_code`, `daemon_backend_code`, `mcp_api_surface`, `merge_conflict_repair`, `final_gate` or `review_repair`. It is decided by the task's own kind or template, then ordered keyword rules over the title and spec, then the default `daemon_backend_code`; a task that lists a non-test Go file under `internal/` is never a mechanical type. How-to: [howto/automata-task-routing.md](howto/automata-task-routing.md).

### Task routing table

The **task routing table** is the set of rows, kept in named profiles, that maps each task type to the effort and verification LLM a newly created task starts with. It is off by default and is read when a task is created, by the planner and by the add-task path. The planner never writes a worker backend or model onto a task: a task has its own backend only when its task JSON or its spec (a line `backend: <name>`) names one, and otherwise runs on its story's, then its Automaton's, setting. An LLM set on the task, its story or its Automaton always wins over a row. How-to: [howto/automata-task-routing.md](howto/automata-task-routing.md).

### Explicit model policy

The **explicit model policy** is the daemon rule that a `claude-code` autonomous session never starts on the backend's built-in default model (which on some installs is a premium, Opus-class model the daemon neither knows nor records). After the normal resolution chain (task, story, PRD, Automaton, Automation, global, first-attempt pair, ladder, burn-guard route, planner rule or judge pin) has run, a `claude-code` backend with no model resolves to the daemon default: `sonnet` for the worker on a normal task, `haiku` on a mechanical task type (the eight routing-table mechanical types, worker only), and `sonnet` for verifiers, guardrail judges and planners — judging and planning are never mechanical work. A model named at any level, premium included, always passes through unchanged, and every other backend resolves exactly as before. It is fixed behavior with no setting; a guardrail judge on the claude-code backend that names no model at all now starts on `sonnet` instead of being blocked with a no-guardrail-model verdict, and a pin the backend cannot serve still fails loudly. A **direct** start (CLI, chat, REST, MCP or the PWA) on the claude-code backend that names no model first resolves one from the LLM row, then the global autonomous planning model (when its backend is claude-code), and if all are empty is **refused before anything is created** — no tracker, no session record, no tmux pane, no run under the operator's account default; the daemon will not fall back to the account default or upgrade the model on its own. When a model is named, the persisted session record carries exactly the value the backend passes to the CLI as `--model`, and a restart re-injects that recorded model before relaunching, so a relaunched session keeps the same `--model` rather than the account default. How-to: [howto/explicit-model-policy.md](howto/explicit-model-policy.md).

### Task routing profile

A **task routing profile** is a named table that maps each task type to a row: worker backend and model, optional verification backend and model, effort, an optional second choice and `allow_premium_model` (needed to name an Opus or Fable model). One profile is active (`autonomous.task_routing.active_profile`); switching it changes only tasks created afterwards. Profiles can be saved, switched, deleted, exported, imported and tried with a no-write dry run from the CLI, REST, MCP and the web Settings card (the chat channel can list, switch and dry-run). Each task shows the row that decided its LLM in the Automaton detail view. A table (or a single key such as the active profile) written through the config route is validated before it is stored; a refused key is skipped and reported as `not stored` on every surface, never confirmed. The routing table is off by default, and an LLM set on the task, its story or its Automaton always wins over a row. The shipped `default` profile is the operator's starting choice and other users may change it.

### Active profile

The **active profile** is the one task routing profile whose rows are read for tasks created from now on (`autonomous.task_routing.active_profile`). It cannot be deleted while it is active. Switching it changes nothing on tasks that already exist, rewrites no task and never touches a running session; only tasks created afterwards, or tasks the router itself routed that have not started and are re-routed by a re-decompose, take the new profile's rows. Hand-set task, story and Automaton LLMs are never overwritten.

### Second choice

A **second choice** is the alternative backend and model on a task routing row, used when the row's first choice is refused. The task's routing result records whether it was used. It follows the same rules as the first choice: the backend must be known, the model must pair with it, and an Opus or Fable model needs `allow_premium_model` on the row.

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

### Trivy version pin

The **Trivy version pin** is `scripts/trivy-version.txt`, a file holding one `vX.Y.Z` tag. It is the only place the CI Trivy version is set: every workflow that installs Trivy reads it and passes it to `scripts/ci-install-trivy.sh`. `scripts/check-trivy-version-single.sh` fails when a workflow carries its own pin. See [howto/container-image-refresh.md](howto/container-image-refresh.md#how-ci-installs-trivy).

### Helm version pin

The **Helm version pin** is `scripts/helm-version.txt`, a file holding one `vX.Y.Z` tag. The kind smoke workflow reads it and passes it to `scripts/ci-install-helm.sh`, which downloads that exact Helm release with retries and installs it without piping into a shell. It is separate from the Trivy version pin, and no guard script checks it. See [howto/container-image-refresh.md](howto/container-image-refresh.md#helm-in-the-kind-smoke-workflow).

## Plan records

A **plan record** is a registry row or a finding kept as a datawatch record instead of a numbered line in `docs/plans/README.md` or a findings file. Records are read and changed through REST (`/api/plan-records`), the MCP `plan_record_*` tools, `datawatch plan-record`, the `autonomous plan-record` chat verbs and the Plan records section of the Projects page. A plan can be a [release plan](#release-plan), and registry rows and findings are items that link into a plan, never plans. See [howto/plan-records.md](howto/plan-records.md) and [operations.md](operations.md#plan-records-store-and-import).

### Plan

A **Plan** is the registry record of what you are working on. The model links down: **Plan**, then **Automation**, then **Automata**, then tasks. A **Plan** holds one or more Automations (a named chain of Automata); an **Automaton** is one unit of autonomous work with its tasks. A **finding** attaches to a Plan or to an Automaton. A Plan is not an Automation: it is the record above one. Plans are ordered per project (lowest order first); a Plan with no Automaton yet is in the backlog. The word *record* is used only for what is stored (the registry rows and findings) and for the Records sub-tab. See [Plan record](#plan-record), [Plan order](#plan-order) and [howto/projects-tab.md](howto/projects-tab.md).

### Registry

The **Registry** is the set of registry rows a project keeps: one planned or shipped work item each, with a state and optional links to a Plan and to Automata. See [Registry row record](#registry-row-record). Rows are stored records, never deleted; closing one is a state change.

### Finding

A **finding** is a problem raised by an Automaton or an operator. It attaches to a Plan or to an Automaton (a finding already attached elsewhere is refused and the holder is named). See [Finding record](#finding-record).

### Plans browser

The **Plans browser** is the body of the **Plans** page sub-tab on the Projects sub-tab: the project's Plans in priority order, each with its goal, state, progress of finished Automata and points, then a **Backlog** group of Plans with no Automaton yet. Open a Plan to see its Automations, its Automata in run order with the next marker, its tasks (read only when opened, with **Load more**) and its linked findings and registry rows. The up and down buttons reorder Plans and a Plan's Automata; **Create plan** and the attach controls link an Automaton, an Automation or a finding, through the [Plans write API](#plans-write-api). The search box covers Plan, Automaton, registry row and finding titles, the state filter narrows by Plan state, and reordering is off while either is in use. Which Plans are expanded is remembered per browser, not by the daemon. For [release plans](#release-plan) it also shows the Release badge, the Themes section, Archive and Restore with the archived filter, the Unattributed list with per-row and bulk attach, and the release and row-plan migration panels.

### Records sub-tab

The **Records** page sub-tab (third in the Projects page, after Overview and Plans) lists every registry row and finding of the project with the Plan and Automata each links to, read from `GET /api/plan-records/browse`. It has a search box, a kind filter and a multi-select state chip set (no chip picked means all states, several picked match *any* of the picked states, a second tap on a picked chip removes exactly that one, the **All** chip, active while nothing is picked, clears the whole set, and the picked chip set is remembered in this browser only, never shared with the Plans browser or the registry card), plus **Load more**; a Plan link opens the Plans sub-tab on that Plan. The Overview page sub-tab shows a summary: the active Plans with state and progress, what is next, and counts of Plans, registry rows and findings.

The **Registry and findings** section, under the Records page sub-tab, filters its Registry rows and Findings views with the same kind of multi-select state chip set: one chip per state of the shown kind, no chip picked means all states, several picked match *any* of them (sent to the daemon as `state=one,two`), and **All** clears the set. Its picks are remembered in this browser only, separately from the Records sub-tab and the Plans browser. See [howto/plan-records](howto/plan-records.md).

### Plan record

A **plan record** of kind `plan` is the registry record of what you are working on. It has a title, a goal, a state and an `order`, and it links down to Automations and Automata and holds the findings attached to it. A plan with no Automaton yet is in the *backlog*. See [howto/plan-records.md](howto/plan-records.md#plans-links-and-order).

### Plan rename and goal edit

A plan can be **renamed** and its **goal edited** with `PATCH /api/plans/{ref}` (MCP `plan_rename` and `plan_edit`, CLI `datawatch plans rename` and `edit --goal`, chat `autonomous plan rename` and `plan edit <plan> goal`, and **Rename** / **Edit goal** in the Plans browser). A title is one line of at most 100 characters and unique in the project (a clash is a 409 naming the other record); the old title stays as an alias and the plan's title is then kept as written, so a later plans migration does not overwrite it. A goal is at most 2000 characters and an empty goal clears it (not from chat). Neither changes the plan's status or `set_by_hand`. Each writes a `plan_renamed` or `plan_goal_edited` audit event. See [howto/plan-records.md](howto/plan-records.md#renaming-a-plan-and-editing-its-goal).

### Plan links

**Plan links** are the ids a plan keeps in `automations`, `automata` and `findings`. A link is never overwritten: attaching an Automaton or Automation that another plan already holds, or a finding attached elsewhere, is refused and names the holder. Detach it first.

### Set-by-hand plan

A **set-by-hand** plan is a plan whose status someone wrote by hand. The plan record carries `set_by_hand: true`; the plan roll-up never changes its status and only reports it (`set_by_hand_kept`). A plan renamed by hand carries `title_set_by_hand`, so a later plans migration keeps its name. Both markers are set by the store, not by a separate setting. See [Rolling plan status up from the work](howto/plan-records.md#rolling-plan-status-up-from-the-work) and [Renaming a plan and editing its goal](howto/plan-records.md#renaming-a-plan-and-editing-its-goal).

### Release plan

A **release plan** is a plan record with `plan_kind` `release` and a `release` label naming the release it stands for. A plan *is* a release: the release plan holds that release's Automata and Automations, grouped by [themes](#theme). A project has one release plan per label (compared like titles, so `Release 11.0` and ` release 11.0 ` are one label), and a second is refused with the plan that holds the label named. The release migration names the plans it makes `Release <label>` and uses the default labels `10.0` (current) and `11.0` (next) when it is given none. Registry rows and findings are not release plans; they are items that link into one (see [Registry row link](#registry-row-link) and [Backlog item](#backlog-item)). A release plan is created by the [release migration](#release-migration), or by hand over REST or MCP (`plan_create` with `plan_kind` `release`). See [howto/plan-records.md](howto/plan-records.md#release-plans-and-themes). Operator review: the default labels `10.0` and `11.0` are only defaults, so check the two labels in the migration preview before approving it (see [operations.md](operations.md)); Android and iPhone do not have release plans yet.

### Theme

A **theme** is a named group inside a release plan. It groups Automata and Automations the plan already holds and never adds one; an item is in at most one theme, and an item in no theme is *unplaced*. Detaching an item from the plan takes it out of its theme; removing a theme leaves its items in the plan, unplaced. A plan holds at most 50 themes, a theme name is at most 100 characters and unique in the plan (case-insensitive), and a theme is created empty. Only a release plan has themes. Themes are changed with the theme calls (`plan_theme_list`, `plan_theme_set`, `datawatch plan theme`, `autonomous plan theme`, the Themes section of the Plans sub-tab). See [Theme rules](#theme-rules).

### Theme rules

The **theme rules** are the table the release migration and the unattributed list use to choose a theme for an Automaton. Rows are tried in order and the first match decides: a tag rule matches a tag exactly (any case); an Automation-title or Automaton-title rule matches when the words of any listed phrase appear together, in order, in that title. An operator mark overrides the table, and an item no row matches is attached unplaced. An Automation (a chain) is filed by its own title as the Automation title.

| Order | Looks at | Matches | Theme |
|---|---|---|---|
| 1 | tag | `security`, `encryption`, `secrets`, `crypto`, `tls` | Security and encryption |
| 2 | tag | `mobile`, `android`, `ios`, `iphone`, `app` | Mobile and the app |
| 3 | tag | `release`, `tests`, `testing`, `e2e`, `ci` | Release tooling and tests |
| 4 | tag | `automata`, `automation`, `autonomous`, `projects`, `plans`, `core` | Core automation and projects |
| 5 | Automation title | `security`, `encryption`, `secrets`, `vault`, `tls`, `certificate` | Security and encryption |
| 6 | Automation title | `mobile`, `android`, `ios`, `iphone`, `companion app` | Mobile and the app |
| 7 | Automation title | `release`, `tests`, `testing`, `e2e`, `smoke`, `changelog` | Release tooling and tests |
| 8 | Automation title | `automata`, `automaton`, `automation`, `projects`, `project`, `plans`, `plan` | Core automation and projects |
| 9 | Automaton title | `security`, `encryption`, `encrypt`, `secret`, `secrets`, `vault`, `tls`, `cve`, `certificate` | Security and encryption |
| 10 | Automaton title | `mobile`, `android`, `ios`, `iphone`, `companion app` | Mobile and the app |
| 11 | Automaton title | `release`, `tests`, `test`, `e2e`, `smoke`, `changelog`, `goreleaser` | Release tooling and tests |
| 12 | Automaton title | `automata`, `automaton`, `automation`, `projects`, `project`, `plans`, `plan`, `decompose`, `executor` | Core automation and projects |

The four themes are created in the order *Core automation and projects*, *Security and encryption*, *Mobile and the app*, *Release tooling and tests* when a run needs them, then any theme of the operator's own that a mark names, alphabetically.

### Release migration

The **release migration** files a project's Automata and Automations under two release plans, the current release and the next one (default labels `10.0` and `11.0`), creating the plans and their themes as needed. Work shipped since the last major release, or planned and not held, goes to the current plan; work held for after it, planned for the next release or marked for it goes to the next plan; other work is skipped with a reason. An Automaton another plan already holds is a *conflict* and is never moved. It previews by default and prints a **preview id** (a stable hash of what the preview shows); a written run needs `apply` and the preview id of the preview you read, a missing id is refused (400) and a stale one (the plans changed since) is refused (409), both before anything is written. A second approved run changes nothing, and nothing is deleted. Run it with `datawatch plan migrate --release`, `POST /api/plans/migrate` with `mode` `release`, MCP `plan_migrate` with `mode` `release`, chat `autonomous plan migrate release`, or the Release migration panel of the Plans sub-tab. Audit: the store action `plan_record_release_migrated`, recorded in the audit log as `plan_record_updated` with an `op` detail. Not the same as the [plans migration](#plans-migration). See [howto/plan-records.md](howto/plan-records.md#migrating-automata-to-release-plans).

### Backlog item

A **backlog item** is a registry row or a finding with no Automaton yet that a plan holds. It is an item inside the plan, never a plan. Not the same as the plans *backlog* group, which lists plans with no Automaton. The row-plan conversion makes a backlog item of a row-plan that holds no Automaton.

### Row-plan

A **row-plan** is a plan the older plans migration made for one registry row (title `Plan: <row text>`, with a `migrated-from` link naming the row). The **row-plan conversion** replaces it with a registry row link on the release plan that holds its Automata, or a backlog item of the current release plan when it holds none, and archives it with a reason; nothing is deleted. A row-plan whose Automata are not all on a release plan yet *waits* and is left alone. Preview first; a written run needs the approved preview id; the store action is `plan_record_row_plans_converted`, recorded in the audit log as `plan_record_updated` with an `op` detail. Run it with `datawatch plan row-plans convert`, `POST /api/plans/migrate/row-plans`, MCP `plan_row_plan_list` and `plan_row_plan_convert`, or the Row-plans migration panel; it has no chat verb. See [howto/plan-records.md](howto/plan-records.md#converting-the-row-plans).

### Registry row link

A **registry row link** ties a registry row to a plan. A row can be linked from more than one plan. A record is held by a plan as a row link or as a backlog item, not both. The row-plan conversion makes a row link of a row-plan that holds Automata.

### Archived plan

An **archived plan** is a plan marked `archived` (with the time and an optional reason of at most 1000 characters). It is left out of the default plan lists, the backlog, search and overview counts, but its file, links, themes and order are kept, and **restore** brings it back in its old place. Archiving a plan does not touch its Automata, Automations, rows or findings, and neither archive nor restore ever deletes anything. Both are idempotent, only plans can be archived, and a *protected plan* named by the caller (usually the current release plan) is refused while it holds Automata. Asking for archived plans takes `archived=include` or `archived=only` on the project plans list, or the archived filter in the web app. Audit events: `plan_record_archived`, `plan_record_restored`. Not the same as an archived Automaton or the plans folder archive. See [howto/plan-records.md](howto/plan-records.md#archiving-a-plan).

### Unattributed Automaton

An **unattributed Automaton** is an Automaton that no release plan holds. The unattributed list (`GET /api/plans/unattributed`, MCP `plan_unattributed_list`, `datawatch plan unattributed`, chat `autonomous plan unattributed`, the **Unattributed** chip of the Plans sub-tab) shows each one with the release and theme the [theme rules](#theme-rules) would file it under; an Automaton held by an ordinary plan names that plan and must be detached from it first. It only reads. An Automaton is attached to a release plan one at a time (`plan_attach`, `plan attach <plan> --automaton ID`) or in bulk (`plan_attach_bulk`, `plan attach <plan> --automaton a1,a2 [--theme T]`, up to 500 ids, one result per id, a refused id never stops the others). See [howto/plan-records.md](howto/plan-records.md#the-unattributed-list).

### Plan order

**Plan order** is the `order` number of a plan: the lowest runs first. Reordering writes a `plan_record_reordered` audit event.

### Plans migration

The **plans migration** (`datawatch plans migrate [--apply]`, `POST /api/plans/migrate`, MCP `plan_migrate`, chat `autonomous plan-migrate [apply]`) builds plans from the older project plan lists (arcs) and the registry rows: one plan per plan, one plan per row that names an Automaton or Automation, one backlog plan per open row that names none — an open row with no derivable name is not created, it is listed under *needs review*. It previews unless applied, tags each plan it makes with a `migrated-from` link so a second apply changes nothing, and deletes no plan, row or setting. A shipped, done or closed row never becomes a plan: with an Automaton it is linked to the plan that holds it, without one it is listed as *history without an Automaton*. Every preview and apply prints the server summary line verbatim with the six counts — created, linked, *already present*, *history without an Automaton*, *needs review* (open rows with no derivable name, not created) and *conflicts* (each names what holds the item and what would have changed) — and the command line then lists each kind, with a section headed `Needs review (N, not made into plans):` and the existing `History without an Automaton (N, not made into plans):` section; `--json` is unchanged and carries `needs_review`. See [howto/plans-migration.md](howto/plans-migration.md) and [howto/plan-records.md](howto/plan-records.md#building-plans-from-arcs-and-rows).

### Plans write API

The **plans write API** creates, renames, orders and links plans: `POST /api/plans`, `PATCH /api/plans/{ref}` (rename a plan and/or edit its goal), `PUT /api/plans/order`, `PUT /api/plans/{ref}/automata/order`, `POST /api/plans/{ref}/attach` and `POST /api/plans/{ref}/detach`, with MCP `plan_create`, `plan_rename`, `plan_edit`, `plan_reorder`, `plan_reorder_automata`, `plan_attach` and `plan_detach`, CLI `datawatch plans`, and chat `autonomous plan`. A link held by another plan is refused with a 409 that names the holder. See [howto/plan-records.md](howto/plan-records.md#ordering-and-linking-plans).

The **plan roll-up** derives a plan's status from the Automata and Automations linked to it (any failed or blocked item makes it blocked, any running item keeps it open, all shipped makes it shipped, all finished makes it done) and never changes a status that was set by hand (`set_by_hand`). It previews with `GET /api/plans/rollup?project=&dry_run=true` and applies with `POST /api/plans/rollup`; MCP `plan_rollup` (`dry_run`), CLI `datawatch plans rollup [--project P] [--dry-run]`, and chat `autonomous plan rollup [dry-run]` do the same, and `plans list` marks a hand-set status `(by hand)`. See [howto/plan-records.md](howto/plan-records.md#rolling-plan-status-up-from-the-work).

A plan's **status** is the `state` of its plan record: one of `open`, `planned`, `in_progress`, `blocked`, `shipped`, `done`, `deferred` or `cancelled`. The roll-up writes only `open`, `blocked`, `done` and `shipped`. It reads the **link state** kept on the plan for each linked Automaton or Automation (the state seen when it was attached, and `shipped` when the merge pipeline records it as shipped), not the live Automaton, so attaching an item to the same plan again refreshes it. **Set by hand** (`set_by_hand`) marks a plan whose status was written by hand: the roll-up keeps that status and lists the plan with `set_by_hand_kept` and the status it would have had. The roll-up runs when you ask for it, once a day from daemon start, and when an Automaton is recorded as shipped through write-through. A plan's title and goal can be edited; no surface edits its status or clears `set_by_hand` yet. See the open points in [howto/plan-records.md](howto/plan-records.md#still-for-the-operator-to-review) and [operations.md](operations.md#plan-records-store-and-import).

### Plans read API

The **plans read API** shows a project as plans in priority order: `GET /api/projects/{name}/plans`, `GET /api/projects/{name}/overview-summary`, `GET /api/plans/{ref}/automata`, `GET /api/plans/{ref}/tasks` and `GET /api/plan-records/browse`. It reads the plan-record summary index and pages every list with `limit` and `offset`. The `state` filter of the `browse` route takes one state, or a comma-separated list; without `kind` a state of the registry-row or the finding vocabulary is admitted, with `kind` only the states of that kind. See [howto/plan-records.md](howto/plan-records.md#reading-plans).

### Next marker

The **next marker** on a plan's Automata points at the Automaton that runs next. It never lands on a blocked or held Automaton; those are shown and marked `skipped`.

### Registry row record

A **registry row record** (kind `registry_row`) is one planned or shipped work item. Its state is `open`, `planned`, `in_progress`, `blocked`, `shipped`, `done`, `deferred` or `cancelled`.

### Finding record

A **finding record** (kind `finding`) is a problem raised by an Automaton or an operator. Its state is `open`, `triaged`, `fixed`, `wontfix` or `duplicate`.

### Record identity

Every plan record has a short readable **id**, unique in the installation, and a **uid** that never changes. A new record never reuses a legacy row number as its id; the old number is kept as `legacy_number`, so "row 12" still finds it. The **title** is the human handle. It is unique inside one project (case-insensitive, trimmed) and may repeat across projects. A rename keeps the old title as an alias that still resolves.

### Record reference

A **record reference** is how a caller names a plan record. It is tried in this order: id, uid, `row <number>`, exact title, an old title alias, an unambiguous title prefix. An ambiguous reference returns the list of candidates so the caller can ask which one was meant. Records are never deleted; closing one is a state change. A reference in a URL writes the row form as `row-<n>`.

### Plan record import

The **plan record import** reads the numbered rows of `docs/plans/README.md` and the findings files (`docs/plans/**/*findings*.md` by default) of a registered project and creates one record for each. It only reads: the files stay in place as the read-only archive. It is idempotent, keyed by project, kind and `legacy_number` (rows) or `legacy_source` (findings), so a second run creates nothing. A dry run returns the full preview and counts and writes nothing. When a source changed after import, the run reports the record as changed and updates it only if nobody edited the record since.

### Import conflict policy

The **import conflict policy** decides what happens to a title already taken in the project, a duplicate row number or a row that cannot be parsed. `suffix` (the default) appends ` (row <n>)` or ` (<file>)` and flags the record `needs_review`; `rename` takes a new title from the caller's map; `skip` leaves the record out. A real run applies the policy to every conflict and never stops half way.

### Plan record write-through

**Plan record write-through** is the setting `autonomous.plan_records.write_through` (default off). When on, the merge pipeline's `registry_row` post-ship hook records the shipped row as a plan record (marks the matching registry row `shipped` with the Automaton id and version, or creates one) instead of editing `docs/plans/README.md`. A failed write fails the hook and never falls back to the README. It does not mirror hand edits of a record back into the plans files. See [howto/plan-records.md](howto/plan-records.md#what-happens-when-an-automaton-ships).

### Plan records switch

The **plan records switch** is `autonomous.plan_records.enabled` (default on). While off, every `/api/plan-records` route answers 404 and the Plan records section of the Projects page hides. Records on disk are kept.

### Derived project alias

The **derived project alias** is the read-only `project` field on an Automaton or Automation as returned by the REST views. It is worked out on every read from the registered project aliases (the registry behind `GET /api/projects`, alias name to directory): the explicit project profile when it names a registered alias, otherwise the longest registered alias directory that contains the record's project directory, otherwise the field is left out. Nothing is stored or rewritten. See [howto/projects-tab.md](howto/projects-tab.md#reading-the-fields).

### Pre-tag run

The **pre-tag run** is `make pre-tag` (`scripts/pre-tag.sh`): every release gate run locally in CI order, stopping at the first failure, so a code failure shows up on the build host before the tag is pushed. It is the only gate that runs `go test`: CI (`ci.yaml`, `release.yaml`) no longer does. Each run appends a line with the commit, date, step count, result and seconds to `.datawatch/pre-tag-record`. A step with a missing tool is skipped, not failed. The agent-base step scans the image it builds with Trivy when Trivy is installed, on a throwaway isolated scan cache under the operator's temp space that the script removes on every exit path — a successful scan, a failing scan, or an interrupt. See [operations.md](operations.md#pre-tag-run-make-pre-tag) and [release-checklist.md](release-checklist.md#4a-pre-tag-run-local-gates-before-the-tag).

### Final gate allowlist

The **final gate allowlist** is the list of changed files a final verification gate lets through. It must cover every path the task specs and the investigation name, plus the existing tests those changes need and the generated changelog and parity draft files — never a guessed directory list. The lint rule `verify_script_allowed_files` fails when a task spec names a file the allowlist rejects; globs and generated-folder prefixes count as coverage. See [decompose lint](howto/automaton-decompose-lint.md#3a-final-gate-allowlist-and-meaning-based-phrase-checks) and [verification scripts](howto/automaton-verification-scripts.md#5b-allowlist-coverage-and-phrase-wording).

### Local tokens

**Local tokens** are the tokens a local-model backend (pi, ollama) used. They are their own unit in the cost report and charts: never priced and never added to Claude tokens. See [howto/cost-report.md](howto/cost-report.md#local-tokens-tool-calls-and-active-seconds) and [api/cost.md](api/cost.md).

### Not reported

**Not reported** marks a session whose backend gave no token or tool-call counts. Its figures are unknown, not zero: they are left out of the local tokens and tool-call totals and counted in `tokens_not_reported_sessions` or `tool_calls_not_reported_sessions` instead.

### Active seconds

**Active seconds** are the time an Automaton's sessions spent working, with idle time taken out, summed per row, chart line and total as `active_seconds`. They are a measure of time, never a cost.

### Tool calls

**Tool calls** are the number of tool invocations a session made, counted per session and never by tool name.
