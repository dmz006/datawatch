# datawatch — User Manual

## What is datawatch?

Datawatch is a single-binary control plane for orchestrating AI work. One operator, one daemon, one consistent surface — and underneath, a set of long-running AI sessions, ephemeral container workers, persistent memory, structured identity, multi-phase reasoning, rubric-based evaluation, and multi-persona debate, all running together with one set of lifecycle, audit, and security guarantees.

It started as a small bridge between mobile messaging apps and AI coding sessions running in `tmux` — type a message in Signal, watch the LLM work, get the answer back. That bridge is still here, but the surface around it grew. Today datawatch runs entire workflows end-to-end: it holds your operator identity (your role, your goals, your constraints) and injects it into every spawned session so the AI stays aligned. It decomposes a high-level intent into a directed graph of stories and tasks, dispatches each to the right backend, captures the output, runs it past graders, and pulls in a council of personas for structured critique when the decision is non-trivial. It remembers what was decided last week. It can spawn workers in remote Kubernetes clusters as easily as it spawns local sessions. It speaks the same REST + MCP + CLI + chat + PWA + mobile + YAML surface no matter which side you approach it from.

## Who is it for?

Operators who want **one** place to drive AI work — not a tab in five different web apps, not a notebook here and a CLI there. People who want their AI sessions to come back tomorrow remembering what was discussed today. Teams that need their AI work attributed, audited, and bounded by explicit authorization. Hobbyists who want a PAI (Personal AI Infrastructure) they can self-host without paying for SaaS layers.

## What it gives you

- **Long-lived AI sessions** that survive daemon restarts and re-attach cleanly. xterm.js streaming in the PWA, full tmux underneath, full event history captured.
- **Ephemeral container workers** in Docker or Kubernetes, spawned on demand with PQC bootstrap, distroless images, per-pod auth, and Tailscale mesh.
- **Episodic memory** — your sessions remember each other. Vector-indexed project knowledge across sessions, with the spatial schema (floor / wing / room / hall / shelf / box) that makes recall actually work.
- **Multi-channel messaging** — Signal, Telegram, Discord, Slack, Matrix, Twilio, GitHub webhooks, generic webhooks, DNS channel; voice input via Whisper.
- **Pluggable LLM backends** — claude-code, aider, goose, gemini, opencode, opencode-acp, ollama, openwebui, custom shell.
- **Operator identity** — a structured self-description you write once and the daemon injects into every spawned session as the L0 wake-up layer.
- **Algorithm Mode** — a 7-phase structured-thinking harness (Observe → Orient → Decide → Act → Measure → Learn → Improve) you can drive a session through with output captured at each gate.
- **Evals Framework** — rubric-based grading suites (string / regex / binary / LLM-rubric) with capability vs regression thresholds.
- **Council Mode** — multi-persona debate. 10 default personas (security-skeptic, ux-advocate, perf-hawk, simplicity-advocate, ops-realist, contrarian, platform-engineer, network-engineer, data-architect, privacy). Quick (1 round) for fast checks, debate (3 rounds) for serious decisions.
- **Skill registries** — git-backed PAI-format skill manifests synced into your workspaces on demand.
- **Secrets manager** — native AES-256-GCM store at `~/.datawatch/secrets.db` plus optional KeePass and 1Password backends; `${secret:name}` references resolve in YAML, plugin manifests, spawn-time env injection.
- **Federated observer** — multiple datawatch instances pushing process / network / GPU stats into one aggregated view.
- **Autonomous Automata (PRD-DAG)** — high-level intent decomposed into a directed graph of stories and tasks, executed under verification + guardrails.
- **Plugin framework** — manifest-driven hot-reload; subprocess + native plugins; declared comm verbs / CLI subcommands / MCP tools / mobile cards.

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

**Links:**
- [How sessions work](#sessions) (this section)
- [`docs/architecture-overview.md`](architecture-overview.md) — daemon lifecycle
- [`docs/howto/start-session.md`](howto/start-session.md) — TODO
- Plans: see "Sessions" in `docs/plans/README.md`

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

---

## Settings

### Settings — General

The daily-driver knobs.

- **Operator identity** — wake-up L0 layer self-description loaded from `~/.datawatch/identity.yaml`. Auto-injected into every spawned session so the LLM stays anchored to your role / north-star goals / current projects / values / context. Edit via inline form, the 🤖 wizard on the Automata page, or `datawatch identity {get,set,configure,edit}`.
- **Session templates** — named bundles of (backend, effort, model, profile, skills) saved as `~/.datawatch/session-templates/<name>.yaml`. Used when starting new sessions to skip the picker.
- **Device aliases** — friendly names for the device IDs in your federation. Cosmetic; helps observer rows / audit log read more cleanly.
- **Backend artifact lifecycle** — per-backend cleanup policy (e.g. claude `.mcp.json` removal post-session, opencode workspace teardown). Defaults are sensible; only touch if you see leftover artifacts.
- **Secrets store** — credentials, tokens, environment values. Native AES-256-GCM at `~/.datawatch/secrets.db` plus optional KeePass / 1Password backends. `${secret:name}` references in YAML/plugins/spawn-time env injection. Per-secret tags + scope. Audit-logged on every read.

### Settings — Plugins

#### Plugin Manager

Installed plugins listed with their declared surface — comm verbs (chat commands), CLI subcommands, MCP tools, mobile-app cards. Toggle enable/disable; reload re-runs the manifest. Plugins live as folders under `~/.datawatch/plugins/<name>/` with a `manifest.yaml`. Subprocess + native plugin runtimes both supported.

### Settings — Comms

#### Authentication

Bearer token controls. The **Browser token** field is the credential this PWA tab presents on every API call (stored in localStorage). The **Server bearer token** row shows whether the daemon is enforcing token auth and lets you rotate it. CA certificate download buttons retrieve the daemon's auto-generated TLS root so you can trust it on a remote device.

#### Servers

The list of remote datawatch instances this PWA can switch to (`switch server` lets you pivot between hosts without changing the URL). Add a server with its base URL + bearer token; the PWA validates by hitting `/api/health`.

#### Communication Configuration

Per-channel registries: Signal, Telegram, Discord, Slack, Matrix, Twilio, GitHub webhooks, Generic webhooks, DNS channel. Each row exposes connect/disconnect, status, and per-channel settings (e.g. Signal device link QR, Telegram bot token, Slack workspace OAuth). Channels in red are inactive; tap to reconnect.

#### Proxy Resilience

Connection pooling + circuit breaker policies for outbound HTTP from the daemon (LLM backends, webhooks, observer pushes). Settings: pool size, retry budget, breaker open threshold, breaker reset window. Defaults are conservative; tune up only if you're hitting rate limits at a layer datawatch can't auto-recover from.

#### Routing Rules

Comm-channel → backend routing. Each rule is a (sender / channel / pattern) → (backend / profile / model / effort) mapping. Used by the channel adapters to pick which LLM handles an inbound message. Empty list = all messages route to the default backend. Click a rule to edit; reorder by drag.

### Settings — LLM

#### LLM Configuration

Per-backend enable/disable + setup. Each backend card carries its own setup wizard (e.g. claude-code asks for `~/.claude.json`; ollama asks for the host URL; openwebui asks for the API key). Status row shows whether the backend is reachable + the model list it advertises.

#### Cost Rates (USD / 1K tokens)

Per-backend per-model input + output token rates the daemon multiplies session token counts by to compute `EstCostUSD`. Adjust if a backend's billing changed or you negotiated a custom rate. Values default to public list pricing on first run.

#### Detection filters

Prompt patterns + completion patterns the daemon scans tmux output for. **Prompt patterns** trigger `WaitingInput` when matched (e.g. `❯`, `$ `). **Completion patterns** trigger `Complete` (e.g. `DATAWATCH_COMPLETE:`). Per-deployment overrides; the global defaults work for most setups.

### Settings — Agents

#### Project Profiles

Named bundles describing a project workspace: directory, git policy, pre/post hooks, default backend, default skills. Used by Automata's "Workspace" picker. Edit YAML at `~/.datawatch/profiles/projects/<name>.yaml`.

#### Cluster Profiles

Named Kubernetes contexts (kubeconfig + namespace + node selector). Used when spawning container workers in a remote cluster. Operator sets credentials once; sessions reference by name.

#### Container Workers

The agent worker fleet — Docker locally OR Kubernetes-spawned per-session pods. Settings: image base (distroless default), PQC bootstrap key, pull policy, resource limits. Workers join the Tailscale mesh on spawn for private network.

#### Tailscale Mesh Status / Configuration

Headscale-first (self-hosted), commercial Tailscale supported. Status card shows current node + advertised routes; Configuration accepts pre-auth keys or OAuth device flow. ACL Generator builds a Tailscale ACL from current node tags + agent fleet membership.

#### Notifications

Per-channel preference for daemon-emitted events: state changes, needs-input, rate-limit hits, autonomous step approvals. Off by default for chatty events; on for needs-input.

### Settings — Automate

Automaton-related cards.

- **Orchestrator** — multi-graph PRD-DAG executor. Approve / hold / cancel automated runs from this card.
- **Identity / Telos** — same content as Settings → General → Operator identity, surfaced here too because Telos drives autonomous prioritization.
- **Algorithm Mode** — PAI's 7-phase per-session harness (Observe → Orient → Decide → Act → Measure → Learn → Improve). This card lists active sessions, current phase, captured output per gate. CLI: `datawatch algorithm {start,advance,edit,abort,reset,measure}`.
- **Evals** — rubric-based grading suites. Default suite types: `string_match`, `regex_match`, `binary_test`, `llm_rubric`. Run a suite from this card; results land in `~/.datawatch/evals/runs/`. Used by Algorithm Mode's Measure phase if configured.
- **Council Mode** — multi-persona debate. 10 default personas (security-skeptic, ux-advocate, perf-hawk, simplicity-advocate, ops-realist, contrarian, platform-engineer, network-engineer, data-architect, privacy). View / edit any persona's system_prompt via the "View / edit personas" button in the card. Personas live as YAML at `~/.datawatch/council/personas/<name>.yaml`; drop new ones there with `name`, `role`, `system_prompt` fields. Modes: quick (1 round) for fast checks, debate (3 rounds) for serious decisions. Synthesizer combines outputs into consensus + dissent.
- **Skill Registries** — git-backed PAI-format skill manifests. Connect a registry → browse → sync. Synced skills get copied into a session's `<projectDir>/.datawatch/skills/<name>/` at spawn time when listed in the session's Skills field.

### Settings — About

A short identity panel: this daemon's hostname + version, a link to the mobile companion app, an Orphaned Tmux Sessions maintenance row, and a single hyperlink to **System documentation & diagrams** which opens this manual in the in-app rendered viewer.

#### API

Inline links to `/api/docs` (Swagger UI), `/api/openapi.yaml` (raw OpenAPI spec), `/api/mcp/docs` (MCP tool catalogue). These are the operator-facing entry points to the daemon's REST + MCP surface — useful for scripting against datawatch from outside.

#### Mobile app pointer

GitHub link to `dmz006/datawatch-app` (the Compose Multiplatform companion). Play Store link will land here once the app is published.

#### Orphaned tmux sessions

Lists `cs-*` tmux sessions on this host that have no corresponding entry in the daemon's session store. Usually leftover from a crash or hard restart. Click a row to kill the orphan tmux session.

---

## Core feature reference matrix

Tracks which core features have how-to walkthroughs, plans, and architecture diagrams.

| Feature | How-to | Plan | Architecture / diagram |
|---|---|---|---|
| Sessions | [`howto/sessions-deep-dive.md`](howto/sessions-deep-dive.md) | covered in active backlog | [`architecture-overview.md`](architecture-overview.md) |
| Channel-driven session state engine | [`howto/channel-state-engine.md`](howto/channel-state-engine.md) | active backlog | covered in `architecture.md` |
| Automata / PRD-DAG | [`howto/autonomous-planning.md`](howto/autonomous-planning.md), [`howto/autonomous-review-approve.md`](howto/autonomous-review-approve.md), [`howto/prd-dag-orchestrator.md`](howto/prd-dag-orchestrator.md) | many plans | architecture covers it |
| Skills | [`howto/skills-sync.md`](howto/skills-sync.md) | ✓ | ✓ |
| Council Mode | [`howto/council-mode.md`](howto/council-mode.md) | ✓ | ✓ |
| Algorithm Mode | [`howto/algorithm-mode.md`](howto/algorithm-mode.md) | ✓ | ✓ |
| Evals | [`howto/evals.md`](howto/evals.md) | ✓ | ✓ |
| Identity / Telos | [`howto/identity-and-telos.md`](howto/identity-and-telos.md) | ✓ | ✓ |
| Secrets Manager | [`howto/secrets-manager.md`](howto/secrets-manager.md) | ✓ | covered in `architecture.md` |
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

Every core feature now has a dedicated how-to. Per-channel coverage on each is being expanded so the same walkthrough works across PWA / Mobile / REST / MCP / CLI / Comm / YAML — every operator workflow is reachable from every surface.

## Documentation index

### How-to walkthroughs

Sessions + state:
- [`howto/sessions-deep-dive.md`](howto/sessions-deep-dive.md) — anatomy, lifecycle, daemon-restart resume, debugging
- [`howto/channel-state-engine.md`](howto/channel-state-engine.md) — why a session is in its current state; signals + diagnostic walkthrough

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
- [`howto/mcp-tools.md`](howto/mcp-tools.md) — wire datawatch into Claude Code / Cursor / any MCP host

Automata + orchestration:
- [`howto/autonomous-planning.md`](howto/autonomous-planning.md) — submit a free-form spec, watch it decompose
- [`howto/autonomous-review-approve.md`](howto/autonomous-review-approve.md) — PRD lifecycle gate
- [`howto/prd-dag-orchestrator.md`](howto/prd-dag-orchestrator.md) — multi-PRD graphs with guardrails
- [`howto/pipeline-chaining.md`](howto/pipeline-chaining.md) — DAG pipelines with before/after gates

Infrastructure:
- [`howto/profiles.md`](howto/profiles.md) — Project + Cluster Profiles
- [`howto/container-workers.md`](howto/container-workers.md) — Docker / Kubernetes ephemeral workers
- [`howto/tailscale-mesh.md`](howto/tailscale-mesh.md) — Headscale + commercial Tailscale agent mesh
- [`howto/secrets-manager.md`](howto/secrets-manager.md) — native + KeePass + 1Password backends
- [`howto/federated-observer.md`](howto/federated-observer.md) — multi-host stats aggregation

Memory + ops:
- [`howto/cross-agent-memory.md`](howto/cross-agent-memory.md) — episodic memory + knowledge graph across sessions
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
