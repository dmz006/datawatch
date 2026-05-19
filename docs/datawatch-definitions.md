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
- **Multi-channel messaging** — Signal, Telegram, Discord, Slack, Matrix, Twilio, GitHub webhooks, generic webhooks, DNS channel; voice input via Whisper.
- **Pluggable LLM backends** — claude-code, aider, goose, gemini, opencode, opencode-acp, ollama, openwebui, custom shell.
- **Operator identity** — a structured self-description you write once and the daemon injects into every spawned session as the L0 wake-up layer.
- **Algorithm Mode** — a 7-phase structured-thinking harness (Observe → Orient → Decide → Act → Measure → Learn → Improve) you can drive a session through with output captured at each gate.
- **Evals Framework** — rubric-based grading suites (string / regex / binary / LLM-rubric) with capability vs regression thresholds.
- **Council Mode** — multi-persona debate. 12 default personas (security-skeptic, ux-advocate, perf-hawk, simplicity-advocate, ops-realist, contrarian, platform-engineer, network-engineer, data-architect, privacy, hacker, app-hacker). Quick (1 round) for fast checks, debate (3 rounds) for serious decisions. Async-first with SSE live-watch; AI persona wizard drafts `system_prompt` via LLM interview.
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

### Inside a session — terminal area

`Tmux` tab is the live xterm.js view of the tmux pane the LLM is attached to. Read-only by default; tap the input bar to send commands.

**Toolbar:**
- `Aa ▾` font controls — A−, A+, current size, Fit-to-width.
- `Scroll` — enter tmux scroll mode (Page Up / Page Down / ESC). Ties into tmux's own scroll-back so you see real history.

**Input bar:** sends text with Enter; the daemon routes it through tmux send-keys. State transitions back to `Running` automatically on input (and bumps the channel-event timer so the gap watcher doesn't immediately re-flip).

**Loading splash:** appears while the first pane_capture frame arrives. Always dismisses — even for ended sessions, the saved final frame is shown.

**Generating indicator:** a 3-dot wave below the output area when the session is `Running`. Sits below the channel tab too, so it's always under the visible content.

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

Per-channel registries: Signal, Telegram, Discord, Slack, Matrix, Twilio, GitHub webhooks, Generic webhooks, DNS channel. Each row exposes connect/disconnect, status, and per-channel settings (e.g. Signal device link QR, Telegram bot token, Slack workspace OAuth). Channels in red are inactive; tap to reconnect.

#### Proxy Resilience

Connection pooling + circuit breaker policies for outbound HTTP from the daemon (LLM backends, webhooks, observer pushes). Settings: pool size, retry budget, breaker open threshold, breaker reset window. Defaults are conservative; tune up only if you're hitting rate limits at a layer datawatch can't auto-recover from.

#### Routing Rules

Comm-channel → backend routing. Each rule is a (sender / channel / pattern) → (backend / profile / model / effort) mapping. Used by the channel adapters to pick which LLM handles an inbound message. Empty list = all messages route to the default backend. Click a rule to edit; reorder by drag.

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
Full verb list: `list | get | add | update | delete | detail | health | pull-model | remove-model | attach-observer | detach-observer | observer-free | observer-by-node | federation-meta-peers`

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

See [`howto/push-setup.md`](howto/push-setup.md) · [`howto/push-notifications.md`](howto/push-notifications.md).

#### Notifications

Per-channel preference for daemon-emitted events: state changes, needs-input, rate-limit hits, autonomous step approvals. Off by default for chatty events; on for needs-input.

### Settings — Automate

Automaton-related cards.

- **Orchestrator** — multi-graph PRD-DAG executor. Approve / hold / cancel automated runs from this card. The **Dashboard nav button** in the bottom navigation is only shown when `autonomous.enabled: true` in `datawatch.yaml` — keeping the nav clean for operators not using Automata.
- **Identity / Telos** — same content as Settings → General → Operator identity, surfaced here too because Telos drives autonomous prioritization.
- **Algorithm Mode** — PAI's 7-phase per-session harness (Observe → Orient → Decide → Act → Measure → Learn → Improve). This card lists active sessions, current phase, captured output per gate. CLI: `datawatch algorithm {start,advance,edit,abort,reset,measure}`.
- **Evals** — rubric-based grading suites. Default suite types: `string_match`, `regex_match`, `binary_test`, `llm_rubric`. Run a suite from this card; results land in `~/.datawatch/evals/runs/`. Used by Algorithm Mode's Measure phase if configured.
- **Council Mode** — multi-persona debate. 12 default personas (security-skeptic, ux-advocate, perf-hawk, simplicity-advocate, ops-realist, contrarian, platform-engineer, network-engineer, data-architect, privacy, hacker, app-hacker). Each run is **async** by default: `POST /api/council/run` returns `{id, events_path}` immediately; subscribe to `GET /api/council/runs/{id}/events` for SSE streaming as each persona responds round-by-round. The PWA shows collapsible live-watch cards per run. Cancel with `POST /api/council/runs/{id}/cancel`. Milestone messages (run started / round complete / consensus reached) push to all configured comm channels; `council.comm_firehose: true` also sends per-persona response previews. Config: `council.llm_ref` (which LLM to use), `council.max_parallel` (concurrent personas per round, default 2). **AI persona wizard** (v6.22.3): the + Add Persona flow can draft a `system_prompt` via LLM — answer 5 interview questions; each answer has a Refine button; result is saved to `~/.datawatch/council/personas/<name>.yaml`. Re-interview any existing persona via the 🤖 button on its row. See [`howto/council-mode.md`](howto/council-mode.md).
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
| Claude hooks | [`howto/claude-hooks.md`](howto/claude-hooks.md) | v7.0.0-alpha.34 | hook scripts + status board |
| Alerts & notifications | [`howto/alerts-and-notifications.md`](howto/alerts-and-notifications.md) | v7.0.0 | alert dock + per-channel delivery |
| Guardrail library | [`howto/guardrail-library.md`](howto/guardrail-library.md) | v7.0.0 | SAST/secrets/deps/LLM scan profiles |
| Ollama marketplace | [`howto/ollama-marketplace.md`](howto/ollama-marketplace.md) | v7.0.0-alpha.33 | embedded catalog + background pull |

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
