# datawatch — Definitions

The user manual. Every tab and every card in the PWA links into this file via a `?` icon next to the card or section header. Each section explains what the card is for, what every configuration option does, and where to dig deeper (architecture, plans, how-tos).

> **Rule (operator-directed 2026-05-05):** every NEW card / page / feature added to datawatch MUST add a section to this document with the same shape (description → controls → links). PRs that add UI without the doc section get rejected.

This is the landing point for "what does this thing do?". It is intentionally long; use the table of contents to jump.

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

**Loading splash:** appears while the first pane_capture frame arrives. Always dismisses now (v6.11.23+) — even for ended sessions, the saved final frame is shown.

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

The "+" FAB launches a new automaton from a template or free-form spec. The form is wizard-style: pick template → confirm spec → pick backend / effort / model / skills → launch.

> *(v6.12.0 — full prose for this section pending the new-automaton-form layout pass; tracked in `docs/plans/README.md` as v6.12.x.)*

### Automaton detail

4-tab layout: **Overview** (PRD spec + status), **Stories** (per-story state + edit/profile/files/approve), **Decisions** (every state-changing event with expandable detail), **Scan** (Run Scan + history).

> *(v6.12.0 — full per-tab prose pending; tracked.)*

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

Browse entity-relationship triples (BL76 episodic memory). Each row is a `(subject, predicate, object, validity_window)`.

### Daemon log

Tail of `~/.datawatch/daemon.log`. For deeper investigation, tail the file directly.

---

## Settings

### Settings — General

Operator identity, session templates, device aliases, backend artifact lifecycle, secrets store. The cards in this tab are the daily-driver knobs.

> *(v6.12.0 — full per-card prose pending; tracked.)*

### Settings — Plugins

Plugin Manager — installed plugins, status, enable/disable, declared comm verbs / CLI subcommands / MCP tools / mobile cards.

### Settings — Comms

Channel registry (Signal, Telegram, Discord, Slack, Matrix, Twilio, GitHub webhooks, generic webhooks, DNS channel), CA certificate downloads, Proxy resilience (connection pooling + circuit breaker), Routing rules.

### Settings — LLM

Backend list (claude-code, aider, goose, gemini, opencode, opencode-acp, ollama, openwebui, custom), per-backend cost rates, detection filters (prompt patterns, completion patterns).

### Settings — Agents

Container agent worker config (Docker / Kubernetes), Tailscale mesh status + configuration, PQC bootstrap, distroless image policy.

### Settings — Automate

Automaton-related cards: orchestrator, identity / Telos, Algorithm Mode, Evals, Council, Skills registries (synced skills).

### Settings — About

App identity, mobile app pointer, orphaned tmux sessions maintenance affordance, system documentation & diagrams (this file's landing page).

---

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
