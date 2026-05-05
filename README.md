# datawatch

<p align="center"><img src="internal/server/web/icon-512.svg" width="180" alt="datawatch logo"/></p>

**A distributed control plane for orchestrating AI work — recursive, episodic, secure, and structured across hosts, clusters, and channels.**

[![License: Polyform NC](https://img.shields.io/badge/license-Polyform%20NC%201.0-blue)](LICENSE)
[![Go version](https://img.shields.io/badge/go-1.24%2B-00ADD8)](https://go.dev)
[![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20WSL2-lightgrey)](docs/setup.md)
[![Release](https://img.shields.io/badge/release-v6.11.0-success)](https://github.com/dmz006/datawatch/releases/tag/v6.11.0)

`datawatch` is a single-binary control plane that runs, remembers, plans, attests, and **debates** AI work — local sessions, ephemeral container workers, persistent memory, and the messaging fabric that ties them together — under one operator with one set of lifecycle, audit, and security guarantees.

It started as a daemon that bridged Signal/Telegram to AI coding sessions running in tmux. It is now a full PAI-parity (and beyond) personal AI infrastructure with structured identity, multi-phase reasoning, rubric-based grading, and multi-persona debate built in.

<p align="center"><img src="docs/tour.gif" width="300" alt="datawatch web UI tour"/></p>

---

## Current release

**v6.11.3 (2026-05-05)** — BL262 — added `"out of extra usage"` and `"you're out of"` trigger patterns to the Claude rate-limit detector. Closes the gap reported on the `"You're out of extra usage · resets 11:50am (America/New_York)"` prompt format. 2 new tests.

**v6.11.2 (2026-05-05)** — PRD → Automata user-visible string sweep. ~19 lingering "PRD" strings in modals, toasts, tooltips, settings section headers, and confirm dialogs replaced with "Automaton" / "Automata" across all 5 locale bundles. Internal identifiers (function names, DOM IDs, API paths, locale keys) unchanged.

**v6.11.1 (2026-05-05)** — Settings card placement: BL257-BL260 cards moved from Agents → Automata per operator directive. Identity / Algorithm Mode / Evals / Council Mode now sit alongside Pipeline Manager / Automata Orchestrator / Scan Framework / Skill Registries in the Automata tab. The 🤖 robot icon in the header (BL257 P2) is unchanged.

**v6.11.0 (2026-05-05)** — Council Mode (multi-persona structured debate). **Closes the BL257-BL260 PAI parity arc**: Identity / Telos (BL257), Algorithm Mode (BL258), Evals Framework (BL259), Council Mode (BL260) all now ship with full 7-surface parity.

The PAI parity arc shipped in 6 releases over ~24 hours after the operator audit on 2026-05-05:

| BL | Feature | Release |
|---|---|---|
| BL261 | Settings → Automata tab card padding | v6.7.7 |
| BL257 P1 | Identity / Telos layer + 7-surface CRUD | v6.8.0 |
| BL257 P2 | Identity Wizard + 🤖 robot-icon nav | v6.8.1 |
| BL258 | Algorithm Mode — 7-phase Observe→Improve harness | v6.9.0 |
| BL259 P1 | Evals Framework — 4 grader types | v6.10.0 |
| BL259 P2 | Algorithm-Mode → Evals bridge (closes BL259) | v6.10.1 |
| BL260 | Council Mode — multi-persona debate | v6.11.0 |

See [CHANGELOG.md](CHANGELOG.md) for full history. Implementation plan: [docs/plans/2026-05-05-bl257-260-pai-parity-plan.md](docs/plans/2026-05-05-bl257-260-pai-parity-plan.md).

---

## Why a control plane and not a bot

The same profile that drives a chat-spawned session can drive a Kubernetes-deployed worker in a remote cluster, a child agent of an existing worker, a scheduled cron job, a webhook reaction, or a cross-host fan-out — and the operator only ever interacts with one surface: the daemon's REST API. **Every feature is mirrored verbatim across 7 surfaces:** REST, MCP, CLI, Comm channels (Signal/Telegram/Matrix/Slack/Discord/etc.), PWA, mobile (Compose Multiplatform), and YAML on disk.

That uniformity is the whole point. Read once, write once, audit once.

---

## What it does

### 🧠 Operator identity (BL257) — *new in v6.8.x*

A structured operator self-description (role, north-star goals, current projects, values, current focus, context notes) loaded from `~/.datawatch/identity.yaml` and **auto-injected into the wake-up L0 layer of every spawned session**. AI work stays anchored to operator priorities. PWA → Settings → Agents → Identity card or 🤖 robot-icon wizard in the header. CLI: `datawatch identity {get,set,configure,edit}`.

### 🔁 Algorithm Mode (BL258) — *new in v6.9.0*

PAI's 7-phase structured-thinking harness as a per-session state machine: **Observe → Orient → Decide → Act → Measure → Learn → Improve**. Operator-driven advance with output captured at each gate; PWA shows a color-coded phase strip per active session. CLI: `datawatch algorithm {start,advance,edit,abort,reset,measure} <session-id>`. The Measure phase can auto-run an Evals suite (BL259 P2) and fold the verdict into the captured output.

### 📊 Evals Framework (BL259) — *new in v6.10.x*

Rubric-based grading replacing the binary verifier. Suites at `~/.datawatch/evals/<name>.yaml` with capability (~70% threshold) or regression (~99% threshold) modes. Four grader types: `string_match`, `regex_match`, `binary_test`, `llm_rubric` (stubbed). Runs persisted to `~/.datawatch/evals/runs/<id>.json`. PWA → Settings → Agents → Evals card. CLI: `datawatch evals {list,run,runs,get-run}`.

### ⚖️ Council Mode (BL260) — *new in v6.11.0*

PAI's multi-persona structured debate. 6 default personas (security-skeptic, ux-advocate, perf-hawk, simplicity-advocate, ops-realist, contrarian) editable as YAML at `~/.datawatch/council/personas/`. Modes: `debate` (3 rounds) for serious decisions, `quick` (1 round) for fast perspective checks. Synthesizer combines responses into consensus + dissent. CLI: `datawatch council {personas,run,runs,get-run}`. *(LLM responses stubbed in v6.11.0; real per-persona inference is a v6.11.x follow-up.)*

### 🛠 Skill Registries (BL255) — *new in v6.7.0*

PAI-format skill manifests with 6 datawatch extensions, synced from git registries (PAI default ships built-in). Connect → browse → sync flow lets operators preview before downloading. Resolution at session spawn copies synced files into `<projectDir>/.datawatch/skills/<name>/`. CLI: `datawatch skills {list,registry,get,load}`.

### 🔐 Secrets Manager (BL242) — *new in v6.4.x*

Centralized AES-256-GCM encrypted store with KeePass + 1Password backends. `${secret:name}` references in YAML config + plugin manifests + spawn-time env injection. Per-secret tags + scoping with caller context. Audit-logged on every read. CLI: `datawatch secrets {list,get,set,delete}`.

### 🌐 Tailscale Mesh (BL243) — *new in v6.5.x*

Tailscale k8s sidecar injected into agent pods for private overlay networking. Headscale-first (self-hosted), commercial Tailscale supported. Pre-auth keys + OAuth device flow. ACL generator with existing-node awareness. CLI: `datawatch tailscale {status,nodes,acl-push}`.

### 💬 The legacy core — still here

- **Multi-channel messaging** — Signal, Telegram, Discord, Slack, Matrix, Twilio, GitHub webhooks, generic webhooks, DNS channel; voice input via Whisper transcription
- **Pluggable LLM backends** — claude-code, aider, goose, gemini, opencode, opencode-acp, ollama, openwebui, custom shell
- **Episodic memory** — vector-indexed project knowledge; SQLite (pure Go) or PostgreSQL+pgvector; Ollama / OpenAI embeddings; XChaCha20-Poly1305 content encryption with key rotation
- **Temporal knowledge graph** — entity-relationship triples with validity windows
- **Full mempalace 6-axis spatial schema** — floor / wing / room / hall / shelf / box auto-derived at save time; +34pp retrieval improvement
- **4-layer wake-up stack** — L0 identity (incl. BL257 Telos) + L1 critical facts + L2 room recall + L3 deep search
- **PWA** — installable Android/iOS web app over Tailscale; xterm.js ANSI streaming; full Settings UI for every config knob
- **Container workers** — Docker / Kubernetes spawn with PQC bootstrap, distroless images, per-pod auth (BL251), Tailscale mesh
- **Plugin framework (BL33)** — manifest-driven hot-reload; subprocess + native plugins; declared comm verbs / CLI subcommands / MCP tools / mobile cards
- **PRD-DAG orchestrator (BL117)** — autonomous PRD decomposition with verification, multi-graph dependencies, guardrails
- **Auto rate-limit recovery** — detects rate limits, pauses session, auto-resumes with context after reset window (persisted across daemon restarts)
- **Multi-profile fallback chains** — named backend profiles with auto-switch on rate limit
- **Quality gates** — run tests before/after sessions, block on new failures
- **Channel session-start hygiene (BL218)** — Go-first MCP enforcement + per-session `.mcp.json` cleanup
- **LLM tooling lifecycle (BL219)** — per-backend setup/teardown, ignore-file hygiene, cross-backend cleanup
- **System monitoring** — CPU, memory, disk, GPU, network, per-session resource usage; eBPF per-process TCP tracking; Prometheus `/metrics`; healthz / readyz
- **Communication channel analytics** — per-channel message counts, bytes, errors, connection stats
- **Bearer token auth + TLS** — auto-generated or custom certs with dual-port HTTP+HTTPS
- **Full audit log** — every operator action recorded with actor / action / details / timestamp
- **Federation** — cross-cluster proxy mode with circuit breaker, offline queue, peer registry, observer rollup

See **[docs/architecture-overview.md](docs/architecture-overview.md)** for the one-screen Mermaid map of every interface, subsystem, and data path.

---

## Installation

### Linux (one-liner)

```bash
curl -fsSL https://raw.githubusercontent.com/dmz006/datawatch/main/install/install.sh | bash
```

Installs to `~/.local/bin` for non-root users, `/usr/local/bin` for root. Includes systemd service.

### From source

```bash
git clone https://github.com/dmz006/datawatch
cd datawatch
go build -o bin/datawatch ./cmd/datawatch
sudo mv bin/datawatch /usr/local/bin/
```

### Update an existing install

```bash
datawatch update && datawatch restart
```

Update is version-string aware. Tmux sessions survive daemon restarts.

---

## Quick start

```bash
# 1. Initialize configuration
datawatch config init

# 2. Set up a messaging backend (choose one)
datawatch setup telegram    # Telegram bot
datawatch setup discord     # Discord bot
datawatch setup slack       # Slack app
datawatch setup signal      # Signal (requires signal-cli + Java)
datawatch setup web         # Web UI only (no messaging backend needed)

# 3. Start the daemon
datawatch start

# 4. Configure your operator identity (BL257)
datawatch identity configure
# or open the PWA and click the 🤖 robot icon in the header

# 5. Verify
datawatch version            # → datawatch v6.11.0
curl -ks https://localhost:8443/api/health
```

Send `help` in the configured channel to see the command reference.

---

## Surface quick reference

Every datawatch feature is reachable from all of these surfaces:

| Surface | Example |
|---|---|
| REST | `curl https://localhost:8443/api/identity` |
| MCP | `get_identity` (via Claude Desktop / Cursor / VS Code) |
| CLI | `datawatch identity get` |
| Comm | `identity` (sent in Signal / Telegram / Matrix / etc.) |
| PWA | Settings → Agents → Identity card |
| Mobile | Mirrored via Compose Multiplatform app (`dmz006/datawatch-app`) |
| YAML | `~/.datawatch/identity.yaml` |

The mobile parity rule: every operator-visible PWA change files an issue against `dmz006/datawatch-app` so the Compose pipeline mirrors it.

---

## Core commands (messaging + CLI)

All commands work in any configured channel and as `datawatch <command>` on the CLI.

### Sessions

| Command | Description |
|---|---|
| `new: <task>` | Start a new AI coding session |
| `list` | List sessions and their current state |
| `status <id>` | Show recent output from a session |
| `tail <id> [n]` | Show last N lines of output (default 20) |
| `send <id>: <msg>` | Send input to a session waiting for input |
| `kill <id>` | Terminate a running session |
| `attach <id>` | Get the tmux attach command for SSH access |

### PAI parity (BL257-BL260)

| Verb | Purpose |
|---|---|
| `identity` / `identity show` | Print operator identity / Telos |
| `identity set <field> <value>` | Patch one identity field |
| `identity configure` | Run the 6-step interview wizard (PWA / CLI) |
| `algorithm` | List sessions in Algorithm Mode |
| `algorithm start <id>` | Register a session at Observe phase |
| `algorithm advance <id> [output...]` | Close current phase + advance |
| `algorithm measure <id> <suite>` | Measure phase via Evals suite |
| `evals` / `evals list` | List eval suites |
| `evals run <suite>` | Execute eval suite, return Run |
| `council` / `council personas` | List Council personas |
| `council run <mode> <proposal>` | Run debate (mode = quick / debate) |

### Memory + KG

| Command | Description |
|---|---|
| `remember <text>` | Save to operator memory |
| `recall <query>` | Semantic search |
| `learnings` | Distilled per-task learnings |
| `kg query <subject>` | Knowledge-graph entity lookup |
| `kg add <s> <p> <o>` | Append a temporal triple |

### Skills + Secrets + Tailscale

| Command | Description |
|---|---|
| `skills` / `skills list` | List synced skills |
| `skills registry add-default` | Add the built-in PAI registry |
| `secrets list/get <name>/set <name>` | Manage centralized secrets |
| `tailscale status/nodes` | Read mesh state |

See [docs/commands.md](docs/commands.md) for the full reference.

---

## Architecture

➡ **[docs/architecture-overview.md](docs/architecture-overview.md)** — one-screen Mermaid diagram of every interface, subsystem, and data path, with planned features called out.

For deeper drill-downs:

- [docs/architecture.md](docs/architecture.md) — package list, component diagram, session state machine, proxy mode (4 Mermaid diagrams)
- [docs/data-flow.md](docs/data-flow.md) — per-feature sequence diagrams
- [docs/plans/README.md](docs/plans/README.md) — open and planned features tracker
- [docs/plans/2026-05-05-bl257-260-pai-parity-plan.md](docs/plans/2026-05-05-bl257-260-pai-parity-plan.md) — PAI parity arc plan

---

## Documentation index

Full documentation lives in [docs/](docs/) — see [docs/README.md](docs/README.md) for a complete index with all flow diagrams.

### Getting started

| Document | Description |
|---|---|
| [docs/setup.md](docs/setup.md) | Installation, backend setup, voice input, RTK, profiles, proxy mode, encryption |
| [docs/commands.md](docs/commands.md) | Complete command reference (messaging and CLI) |
| [docs/pwa-setup.md](docs/pwa-setup.md) | PWA setup with Tailscale |

### Backends

| Document | Description |
|---|---|
| [docs/llm-backends.md](docs/llm-backends.md) | All LLM backends — claude-code, aider, goose, gemini, opencode, ollama, openwebui, shell |
| [docs/messaging-backends.md](docs/messaging-backends.md) | All messaging backends — Signal, Telegram, Discord, Slack, Matrix, Twilio, ntfy, email, webhooks, DNS |

### Interfaces

| Document | Description |
|---|---|
| [docs/mcp.md](docs/mcp.md) | MCP server — 60+ tools for Cursor, Claude Desktop, VS Code |
| [docs/api/autonomous.md](docs/api/autonomous.md) | Autonomous PRD decomposition with verification |
| [docs/api/plugins.md](docs/api/plugins.md) | Subprocess plugin framework + manifest format |
| [docs/api/orchestrator.md](docs/api/orchestrator.md) | PRD-DAG orchestrator + guardrails |
| [docs/api-mcp-mapping.md](docs/api-mcp-mapping.md) | API ↔ MCP coverage analysis |
| [docs/skills.md](docs/skills.md) | Skill Registries + manifest format (BL255) |
| [docs/howto/skills-sync.md](docs/howto/skills-sync.md) | Skills sync workflow walkthrough |
| [internal/server/web/openapi.yaml](internal/server/web/openapi.yaml) | OpenAPI 3.0 REST API specification |

### Memory & intelligence

| Document | Description |
|---|---|
| [docs/memory.md](docs/memory.md) | Episodic memory architecture + flow diagrams |
| [docs/memory-usage-guide.md](docs/memory-usage-guide.md) | Memory in development workflows + PostgreSQL setup |

### Operations & security

| Document | Description |
|---|---|
| [docs/operations.md](docs/operations.md) | Service management, upgrades, CLI, monitoring, troubleshooting |
| [docs/config-reference.yaml](docs/config-reference.yaml) | Annotated config file reference |
| [docs/encryption.md](docs/encryption.md) | Encryption at rest — XChaCha20-Poly1305 |
| [docs/multi-session.md](docs/multi-session.md) | Multi-machine configuration |
| [docs/uninstall.md](docs/uninstall.md) | Manual uninstall for all install methods |

### Source attribution

| Document | Description |
|---|---|
| [docs/plan-attribution.md](docs/plan-attribution.md) | What's borrowed from nightwire, mempalace, PAI; what was built in response |

---

## Prerequisites

| Dependency | Version | Notes |
|---|---|---|
| [signal-cli](https://github.com/AsamK/signal-cli) | ≥ 0.13 | Optional — Signal protocol bridge |
| Java | ≥ 17 | Optional — required by signal-cli |
| [tmux](https://github.com/tmux/tmux) | Any recent | Session management |
| [claude CLI](https://docs.anthropic.com/en/docs/claude-code) | Latest | Default LLM backend |
| [Tailscale](https://tailscale.com) | Any | Optional — for PWA + mesh |
| Go | 1.24+ | Only required for building from source |

---

## License

Polyform Noncommercial 1.0.0. See [LICENSE](LICENSE).

Commercial licensing inquiries: open an issue.

---

## Acknowledgements

Datawatch's design borrows heavily from three projects, with full attribution in [docs/plan-attribution.md](docs/plan-attribution.md):

- **[HackingDave/nightwire](https://github.com/HackingDave/nightwire)** — Signal-driven AI coding bot. Episodic memory + Signal-as-control-plane shape.
- **[milla-jovovich/mempalace](https://github.com/milla-jovovich/mempalace)** — Memory palace metaphor, 4-layer wake-up stack, full 6-axis spatial schema, conversation mining, repair self-check.
- **[danielmiessler/Personal_AI_Infrastructure (PAI)](https://github.com/danielmiessler/Personal_AI_Infrastructure)** — Identity / Telos, Algorithm Mode 7-phase, Skills, Evals, Council, ISA generalization. v6.7.0 + v6.8-v6.11 ship the parity arc.

---

## Contributing

Issues + PRs welcome. Read [AGENT.md](AGENT.md) for the operating rules — every commit follows the documented Pre-Execution / Versioning / Documentation / Mobile-Parity / Secrets-Store rules.
