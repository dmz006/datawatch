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

### Federated peers

Other datawatch instances pushing observer / stats data into this one. Each peer is a row with:

- **Health dot** — green (push <15 s ago), amber (15–60 s), red (stale >60 s or never pushed).
- **Name + shape** (Agent / Standalone / Cluster) + version.
- **Last push** age.
- **📊** — last snapshot drill-down.
- **×** — remove peer (rotates token; peer auto-re-registers if it's still alive).

When ANY peer goes stale, the gear icon in the bottom nav shows a numeric badge. Click the badge to land on this card with the offending peer flashed.

### Process envelopes

Per-process aggregation by attribution kind: `session:`, `backend:`, `container:`, `system`. Snapshot of CPU / RSS / threads / FDs / network / GPU per envelope. Refreshes every 5 s.

### eBPF per-process net

Kernel-traced TCP socket activity per process (when eBPF is available — kernel ≥ 5.8 + cap_bpf + cap_sys_resource). Off → see Settings → About → eBPF status row.

### Audit log

Every operator action (config change, session start/stop, secret read, etc.) recorded with actor / action / details / timestamp. Default view shows the last 5 entries; bump the limit dropdown for more (20 / 50 / 100). Filter by actor or action substring.

### Knowledge graph

Browse entity-relationship triples from the episodic memory. Each row is a `(subject, predicate, object, validity_window)`.

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

Plugin Manager — installed plugins, status, enable/disable, declared comm verbs / CLI subcommands / MCP tools / mobile cards.

### Settings — Comms

Channel registry (Signal, Telegram, Discord, Slack, Matrix, Twilio, GitHub webhooks, generic webhooks, DNS channel), CA certificate downloads, Proxy resilience (connection pooling + circuit breaker), Routing rules.

### Settings — LLM

Backend list (claude-code, aider, goose, gemini, opencode, opencode-acp, ollama, openwebui, custom), per-backend cost rates, detection filters (prompt patterns, completion patterns).

### Settings — Agents

Container agent worker config (Docker / Kubernetes), Tailscale mesh status + configuration, PQC bootstrap, distroless image policy.

### Settings — Automate

Automaton-related cards.

- **Orchestrator** — multi-graph PRD-DAG executor. Approve / hold / cancel automated runs from this card.
- **Identity / Telos** — same content as Settings → General → Operator identity, surfaced here too because Telos drives autonomous prioritization.
- **Algorithm Mode** — PAI's 7-phase per-session harness (Observe → Orient → Decide → Act → Measure → Learn → Improve). This card lists active sessions, current phase, captured output per gate. CLI: `datawatch algorithm {start,advance,edit,abort,reset,measure}`.
- **Evals** — rubric-based grading suites. Default suite types: `string_match`, `regex_match`, `binary_test`, `llm_rubric`. Run a suite from this card; results land in `~/.datawatch/evals/runs/`. Used by Algorithm Mode's Measure phase if configured.
- **Council Mode** — multi-persona debate. 10 default personas (security-skeptic, ux-advocate, perf-hawk, simplicity-advocate, ops-realist, contrarian, platform-engineer, network-engineer, data-architect, privacy). View / edit any persona's system_prompt via the "View / edit personas" button in the card. Personas live as YAML at `~/.datawatch/council/personas/<name>.yaml`; drop new ones there with `name`, `role`, `system_prompt` fields. Modes: quick (1 round) for fast checks, debate (3 rounds) for serious decisions. Synthesizer combines outputs into consensus + dissent.
- **Skill Registries** — git-backed PAI-format skill manifests. Connect a registry → browse → sync. Synced skills get copied into a session's `<projectDir>/.datawatch/skills/<name>/` at spawn time when listed in the session's Skills field.

### Settings — About

App identity, mobile app pointer, orphaned tmux sessions maintenance affordance, system documentation & diagrams (this file's landing page).

---

## Core feature reference matrix

Tracks which core features have how-to walkthroughs, plans, and architecture diagrams.

| Feature | How-to | Plan | Architecture / diagram |
|---|---|---|---|
| Sessions | ✗ (no dedicated walkthrough) | covered in active backlog | [`architecture-overview.md`](architecture-overview.md) |
| Channel-driven session state engine | ✗ | active backlog | covered in `architecture.md` |
| Automata / PRD-DAG | [`howto/autonomous-planning.md`](howto/autonomous-planning.md), [`howto/autonomous-review-approve.md`](howto/autonomous-review-approve.md), [`howto/prd-dag-orchestrator.md`](howto/prd-dag-orchestrator.md) | many plans | architecture covers it |
| Skills | [`howto/skills-sync.md`](howto/skills-sync.md) | ✓ | ✓ |
| Council Mode | ✗ (no dedicated walkthrough) | ✓ | ✓ |
| Algorithm Mode | ✗ (no dedicated walkthrough) | ✓ | ✓ |
| Evals | ✗ (no dedicated walkthrough) | ✓ | ✓ |
| Identity / Telos | ✗ (no dedicated walkthrough — covered in setup) | ✓ | ✓ |
| Secrets Manager | ✗ (no dedicated walkthrough) | ✓ | covered in `architecture.md` |
| Container workers | [`howto/container-workers.md`](howto/container-workers.md) | ✓ | ✓ |
| Federated observer | [`howto/federated-observer.md`](howto/federated-observer.md) | ✓ | ✓ |
| Comm channels | [`howto/comm-channels.md`](howto/comm-channels.md) | ✓ | ✓ |
| Voice input | [`howto/voice-input.md`](howto/voice-input.md) | ✓ | ✓ |
| MCP tools | [`howto/mcp-tools.md`](howto/mcp-tools.md) | ✓ | ✓ |
| Pipeline chaining | [`howto/pipeline-chaining.md`](howto/pipeline-chaining.md) | ✓ | ✓ |
| Cross-agent memory | [`howto/cross-agent-memory.md`](howto/cross-agent-memory.md) | ✓ | ✓ |
| Daemon operations | [`howto/daemon-operations.md`](howto/daemon-operations.md) | ✓ | ✓ |
| Profiles | [`howto/profiles.md`](howto/profiles.md) | ✓ | ✓ |
| Tailscale mesh | ✗ (no dedicated walkthrough — covered in container-workers) | ✓ | ✓ |
| chat / LLM quickstart | [`howto/chat-and-llm-quickstart.md`](howto/chat-and-llm-quickstart.md) | ✓ | ✓ |

**Missing how-tos worth writing** (tracked as future work): Sessions deep-dive, Council Mode, Algorithm Mode, Evals, Identity / Telos, Secrets Manager, Tailscale mesh, channel-driven session state engine.

## Documentation index

Architecture & internals:
- [`architecture.md`](architecture.md) — high-level system shape
- [`architecture-overview.md`](architecture-overview.md) — daemon, backends, channels, memory
- [`backends.md`](backends.md) — LLM backend integration
- [`agents.md`](agents.md) — container worker model
- [`addons.md`](addons.md) — plugin framework

Operations:
- [`setup.md`](setup.md) — install + first run
- [`api/`](api/) — REST endpoints
- [`api-mcp-mapping.md`](api-mcp-mapping.md) — MCP ↔ REST surface map

Plans / backlog:
- [`plans/README.md`](plans/README.md) — every active plan + backlog
- [`plans/historical-plans/`](plans/historical-plans/) — archived plans (>1 week)
- [`plans/historical-releasenotes/`](plans/historical-releasenotes/) — off-minor release notes

For per-feature attribution to upstream projects, see [`plan-attribution.md`](plan-attribution.md).
