# datawatch

<p align="center"><img src="internal/server/web/icon-512.svg" width="180" alt="datawatch logo"/></p>

**A distributed control plane for orchestrating AI work — recursive, episodic, secure, and structured across hosts, clusters, and channels.**

[![License: Polyform NC](https://img.shields.io/badge/license-Polyform%20NC%201.0-blue)](LICENSE)
[![Go version](https://img.shields.io/badge/go-1.24%2B-00ADD8)](https://go.dev)
[![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20WSL2-lightgrey)](docs/setup.md)
[![Release](https://img.shields.io/badge/release-v8.6.1-success)](https://github.com/dmz006/datawatch/releases/tag/v8.6.1)

`datawatch` is a single-binary control plane that runs, remembers, plans, attests, and **debates** AI work — local sessions, ephemeral container workers, persistent memory, and the messaging fabric that ties them together — under one operator with one set of lifecycle, audit, and security guarantees.

It started as a daemon that bridged Signal/Telegram to AI coding sessions running in tmux. It now spans a full compute abstraction layer with capability-based federation access control, three compute-node routing modes (direct, docker-network, datawatch-proxy), a multi-server proxy surface, and a comprehensive E2E test suite — on top of full PAI-parity personal AI infrastructure with structured identity, multi-phase reasoning, rubric-based grading, and multi-persona debate.

<p align="center"><img src="docs/tour.gif" width="300" alt="datawatch web UI tour"/></p>

---

## 📱 Android app 1.0.0 released — alpha testers wanted!

**datawatch for Android, Android Auto, and Wear OS** is now available at **v1.0.0**. Full 7-surface parity: sessions, PRDs, discussions, files, push notifications, channel routing, and identity management — all built on Compose Multiplatform.

**We're looking for alpha testers.** If you want to help shape the mobile experience, reach out to **dmz006** on Signal, GitHub, or via the datawatch PWA comms.

**What's in 1.0:**
- Full session and automata queue on Wear (voice + gesture controls)
- Push notifications with bidirectional replies
- Discussion scopes + discussion message history
- File uploads/downloads from the mobile device
- Full Settings page parity
- Offline queue + auto-sync on reconnect

---

## 🎉 Community skills + plugins registry is live!

**[`dmz006/datawatch-community`](https://github.com/dmz006/datawatch-community)** — the official community hub for datawatch Skills and Plugins is now open.

Operators have been building incredible things on top of datawatch's extension surface — autonomous session patterns, multi-agent polity topologies, inter-agent proposal pipelines — and until now those patterns lived privately on individual machines with no way to share or discover them. That changes today.

The registry launches with seed contributions covering autonomous workflow patterns, identity, and workspace sync:

| Entry | Type | What it does |
|---|---|---|
| [`sibling-runner`](https://github.com/dmz006/datawatch-community/tree/main/skills/autonomous-patterns/sibling-runner) | Skill | Standard per-sibling autonomous session: mailbox-in, scratchpad continuity, Automata queue, structured output sections |
| [`polity-topology`](https://github.com/dmz006/datawatch-community/tree/main/skills/identity/polity-topology) | Skill | Multi-instance identity layer — tells each instance which one it is, where siblings live, how to route references |
| [`sandbox-permissions`](https://github.com/dmz006/datawatch-community/tree/main/skills/identity/sandbox-permissions) | Skill | Fixes sandbox network policy so autonomous claude-code sessions can reach the datawatch CLI and API |
| [`inbox-integrator`](https://github.com/dmz006/datawatch-community/tree/main/plugins/comms/inbox-integrator) | Plugin | `post_session_complete` hook — moves sibling INBOX proposals into the shared InFlight workspace with attribution headers |
| [`workspace-rsync-sync`](https://github.com/dmz006/datawatch-community/tree/main/plugins/sync/workspace-rsync-sync) | Plugin | Rsync workspace files to/from a remote datawatch host over SSH after session completes |
| [`workspace-git-sync`](https://github.com/dmz006/datawatch-community/tree/main/plugins/sync/workspace-git-sync) | Plugin | Commits and pushes workspace changes to a git remote; pulls latest before session starts |
| [`workspace-nfs-mount`](https://github.com/dmz006/datawatch-community/tree/main/skills/ops/workspace-nfs-mount) | Skill | Mounts an NFS share before session starts so multiple agents share a common workspace |

**To connect and sync:**

```bash
datawatch skills registry connect https://github.com/dmz006/datawatch-community
datawatch skills sync community
```

**To contribute:** fork the repo, add your Skill or Plugin directory, open a PR. The bar is intentionally low — if it works and is safe, it gets merged. See [`CONTRIBUTING.md`](https://github.com/dmz006/datawatch-community/blob/main/CONTRIBUTING.md).

---

## Current release

**[v8.6.1](https://github.com/dmz006/datawatch/releases/tag/v8.6.1) (2026-05-20)** — Patch release. Matrix backend (BL241 P1): cleartext send/receive, alias resolution, 7-surface parity, `m.datawatch.session` tag. Fixes `ValidateSecrets` ordering bug that silently disabled Matrix for all secrets-store users. Fixes instance-scoped Claude config (BL318): test daemons no longer corrupt the production `~/.claude.json` or `~/.mcp.json`.

See the [v8.6.1 release notes](docs/RELEASE_NOTES_v8.6.1.md), [v8.6.0 release notes](docs/RELEASE_NOTES_v8.6.0.md), and [CHANGELOG.md](CHANGELOG.md) for the full history.

### v8.6 highlights

- **Full operational data encryption (BL334 T43g+T43h)** — `--secure` closes all remaining coverage gaps. JSON stores: `servers.json`, `skills.json`, `compute/nodes.json`, `inference/llms.json` — all encrypted with XChaCha20-Poly1305 (DWDAT2 format). Upgrade migration runs automatically on first `--secure` startup.
- **Encrypted daemon-app.log (T43h)** — Runtime log output redirected to `secfile.EncryptedLogWriter` (DWLOG1 format) after key derivation. Append-mode on restart preserves history. Decrypt with `datawatch security logs [--tail N]`.
- **Encryption status covers all six categories** — `datawatch security encryption status` probes channel_routing, servers, skills, compute/nodes, inference/llms, and daemon-app.log.

### v8.5 highlights

- **Operational Data Encryption (BL334 T43a–T43e)** — Discussion WAL lines encrypted as `ENC:<base64(nonce24+ciphertext)>`. `participants.json` and `channel_routing.json` encrypted via DWDAT2. Migration idempotent on first `--secure` startup.
- **Secure wipe** — `datawatch security wipe-plaintext --confirm` does 3-pass overwrite (zeros/ones/random) then unlinks plaintext files.
- **Encryption status + migrate** — `GET /api/security/encryption/status`, `POST /api/security/encryption/migrate`. CLI: `datawatch security encryption {status,migrate}`.
- **`${secret:name}` config references** — API keys and tokens can live exclusively in `~/.datawatch/secrets.db` (AES-256-GCM, independent of `--secure`).

### v8.4 highlights

- **Discussion Scopes (BL332)** — Federated append-only WAL memory. Each discussion has `~/.datawatch/discussions/<id>/wal.jsonl` with entries timestamped, origin-peer-tagged, and sequence-numbered. Conflict detection (same-prefix writes from different peers within 5s), 60 writes/min rate throttle, participant sync via push fan-out.
- **REST**: full discussion CRUD + `/wal` + `/conflicts` + `/participants`. **CLI**: `datawatch memory discussion {list,write,recall,wal,participants}`. **MCP**: `memory_discussion_*`. **PWA**: Settings → General → Discussion Scopes card.

### v8.3 highlights

- **Channel Routing (BL331)** — Map inbound channel identities (e.g., `telegram:group:-1001234567890`, `signal:+1555…`) to specific federation peers with optional automata type and default project directory. `GET/PUT /api/channel/routing`. CLI: `datawatch federation peer add --channel-identity`.
- **File Service (BL333)** — Federated upload/delete/list under a configurable service root. Path-traversal guard on every write path. `POST /api/files`, `DELETE /api/files`, `GET /api/files/{peers,discussions,meta}`. CLI: `datawatch files {list,upload,delete,peer}`.
- **14th federation builtin group: `comms-channel-agent`** — sessions+comms+alerts+autonomous without full operator access.

### v8.2 highlights

- **Async PRD decompose (BL328)** — `POST /api/autonomous/prds/<id>/decompose` returns `{task_id, stream_url}` immediately. Stories stream via SSE with `Last-Event-ID` replay. CLI: `datawatch autonomous prd decompose`. MCP: `autonomous_prd_decompose`.
- **Identity POST alias (BL329)** — `POST /api/identity` aliases `PATCH` for Android compatibility. All four methods share one handler.
- **UnifiedPush (BL330)** — `GET /.well-known/unifiedpush`, register/unregister/notify endpoints. PWA: Settings → Comms → Push Notifications card.
- **Badge/chip multi-select (BL327)** — All comma-separated settings fields use badge inputs with dropdown completion and drag-to-reorder.

### v8.1 highlights

- **Compute Node routing modes (BL318–BL322)** — `direct`, `docker-network` (DockerLifecycle manages container lifecycle), `datawatch-proxy` (forward through a peer's `/api/proxy/llm/<name>`). New `gemini-api` and `opencode-api` adapter kinds.
- **Community Skills + Plugins registry (BL324–BL326)** — `dmz006/datawatch-community` is the official hub. In-app registry browser, one-click install, plugin install without restart. Connect: `datawatch skills registry connect`.
- **Mic popup (BL326)** — animated waveform recording overlay in PWA.
- **301 E2E test stories** — 142 new stories (TS-637–TS-778) across v8.2–v8.5 cohorts.

### v8.0 highlights

- **Federation CBAC** — 50 capabilities, **14 built-in groups** (admin, observer, operator, readonly, …, comms-channel-agent), `fedCap()` guards every REST handler and MCP tool.
- **Compute Node routing** — `direct`, `docker-network`, `datawatch-proxy` modes.
- **MCP SSE federation** — MCP SSE transport accepts federation peer tokens with per-tool CBAC.
- **626 E2E test stories** — full plugin, skill, and inline peer daemon coverage.

### v7.x highlights (v7.0.0 → v7.4.0)

- **v7.4.0** — MCP SSE federated auth + per-tool CBAC.
- **v7.3.0** — Systematic `fedCap()` enforcement sweep: 110+ call sites.
- **v7.0.0** — Compute Node registry, LLM Registry + dispatcher, Ollama Marketplace, Alert dock, Claude Code hooks.

### Earlier highlights (v6.0.0 → v6.22.x)

- **v6.22.0** — Docs-as-MCP-Interface: 22 curated howtos, hybrid vector+BM25 index.
- **v6.15.0** — HashiCorp Vault / OpenBao secrets backend.
- **v6.11.0** — Council Mode (multi-persona debate, 6 default personas).
- **v6.10.x** — Evals Framework with rubric-based grading.
- **v6.9.0** — Algorithm Mode: 7-phase structured-thinking harness.
- **v6.8.x** — Operator identity wake-up layer.

See [CHANGELOG.md](CHANGELOG.md) for full history.

---

## Why a control plane and not a bot

The same profile that drives a chat-spawned session can drive a Kubernetes-deployed worker in a remote cluster, a child agent of an existing worker, a scheduled cron job, a webhook reaction, or a cross-host fan-out — and the operator only ever interacts with one surface: the daemon's REST API. **Every feature is mirrored verbatim across 7 surfaces:** REST, MCP, CLI, Comm channels (Signal/Telegram/Matrix/Slack/Discord/etc.), PWA, mobile (Compose Multiplatform), and YAML on disk.

That uniformity is the whole point. Read once, write once, audit once.

---

## What it does

### 💬 Discussion Scopes — *new in v8.4*

A `discussion` memory scope shared across multiple federation peers. Entries accumulate in an append-only JSONL WAL (`~/.datawatch/discussions/<id>/wal.jsonl`), each timestamped with origin peer and sequence number. Conflict detection flags same-content-prefix writes from different peers within 5 seconds. Per-peer write throttle (60 writes/min per Bearer token). Async fan-out syncs every write to all registered participants. REST: `/api/memory/discussion/{id}` CRUD + `/wal` + `/conflicts` + `/participants`. CLI: `datawatch memory discussion {list,write,recall,wal,participants}`. MCP: `memory_discussion_*`.

### 🗂 Channel Routing + File Service — *new in v8.3*

**Channel Routing**: map inbound channel identities (Telegram groups, Signal numbers, webhook URLs) to specific federation peers with optional automata type and default project directory. `GET/PUT /api/channel/routing`. Federation peers now carry a `channel_identity[]` field. CLI: `datawatch federation peer add --channel-identity <pattern>`.

**File Service**: federated upload/delete/list under a configurable service root (`session.file_service_root` or `session.root_path`). Path-traversal guard on every write. `POST /api/files`, `DELETE /api/files`, `GET /api/files/peers/{name}`, `GET /api/files/discussions/{id}`, `GET /api/files/meta`. CLI: `datawatch files {list,upload,delete,peer}`.

### 🔔 Async PRD decompose + Push — *new in v8.2*

**Async decompose**: `POST /api/autonomous/prds/<id>/decompose` returns `{task_id, stream_url}` immediately; stories stream via SSE with `Last-Event-ID` replay. Idempotent second calls return the same task_id. PWA: inline progress panel with reconnect. CLI: `datawatch autonomous prd decompose <id>`. MCP: `autonomous_prd_decompose`.

**UnifiedPush**: `POST /api/push/register` registers an endpoint, `POST /api/push/notify` fans out to all (or one) registration. `GET /.well-known/unifiedpush` for discovery. PWA: Settings → Comms → Push Notifications card.

### 🔐 Federation CBAC — *new in v8.0*

50 capabilities organized into **14 built-in groups** (admin, observer, operator, readonly, …, comms-channel-agent). Every REST endpoint and MCP tool is gated with `fedCap()` / `mcpFedCap()`. Federated peers declare a group (or a custom capability set), and the daemon enforces it on every request — not admin-or-nothing. Groups are manageable at runtime: `POST /api/federation/groups` + `PUT /api/federation/peers/<name>`. MCP tools `federation_group_*`, `federation_peer_*`. CLI `datawatch federation group {list,get,add,update,delete}`.

### 🔀 Compute Node routing — *new in v8.0*

The `routing` field on a Compute Node separates **how the daemon reaches it** (transport) from **what API it speaks** (kind). Three modes: `direct` (existing default — daemon hits `address` directly), `docker-network` (daemon manages the LLM container lifecycle via Docker CLI — spin-up, network attach, teardown), `datawatch-proxy` (forward inference through another datawatch peer's `/api/proxy/llm/<name>` endpoint). All routing modes exposed on all 7 surfaces. New `gemini-api` and `opencode-api` adapter kinds also added.

### 🌐 Multi-server proxy + MCP SSE federation — *new in v8.0*

`GET /api/servers` enumerates Remote Server entries (formerly only visible in the Comms tab). The MCP SSE transport now accepts federation peer tokens with per-tool CBAC enforcement — matching the REST surface. A new `/api/proxy/llm/<name>` inbound endpoint accepts proxied inference from peers configured with `datawatch-proxy` routing.

### 🖥 Compute Node registry — *new in v7.0*

A hardware abstraction layer: add any host, GPU box, Kubernetes cluster, or remote datawatch peer as a **Compute Node**. Each node has a name, kind (`ollama` / `openwebui` / `remote` / `k8s`), address, declared capacity (RAM / VRAM / max concurrent models), RBAC permissions, scheduling priority, and optional maintenance windows. Nodes auto-register from datawatch-stats peer push. Live health + stats via the bound observer sidecar.

PWA → Settings → Compute → Compute Nodes → **+ Add**. CLI: `datawatch compute node {list,get,add,update,delete,health,detail}`.

### 🤖 LLM Registry + dispatcher — *new in v7.0*

Named LLM entries (e.g., `ollama`, `claude-code`, `my-gpu-llama`) each with a kind, ordered ComputeNode failover list, enabled model set, and optional API key reference. The dispatcher walks the failover list, retries one transient error per node, and surfaces final errors immediately. Four built-in adapters: **ollama**, **openwebui**, **opencode** (ollama-protocol alias), **claude** (Anthropic Messages API). Existing v6.x `cfg.ollama` / `cfg.openwebui` configs auto-migrate to `ollama-default` / `openwebui-default` LLM entries on first start — no manual migration.

Every consumer (sessions, Council, `/api/ask`, Automata) routes inference through this registry.

PWA → Settings → Compute → LLM Configuration → **+ Add LLM**. CLI: `datawatch llm {list,get,add,update,delete,test,models,in-use,reassign,force-delete}`.

### 📦 Ollama Marketplace — *new in v7.0 alpha.33*

A browseable, searchable catalog of curated models (llama3.1, qwen3, gemma3, deepseek-r1, codellama, nomic-embed-text, and more) shipped embedded in the daemon. Each model entry shows available tag variants with disk size, minimum RAM, minimum VRAM, and a **hardware-fit indicator** that checks the node's declared capacity. Pulling runs as a background goroutine with live progress in the alert dock. Delete models from the same surface.

PWA → Settings → Compute → Compute Nodes → (Ollama node) → **Browse marketplace**. CLI: `datawatch compute pull-model <node> <model:tag>`.

### 🔔 Alert dock — *new in v7.0 alpha.29–30*

An always-on header badge shows alert count on every page. Click to open the in-app alert dock: filterable by category (prompts / errors / warnings / info), session-grouped cards with attention-first sort, quick-reply select for prompt events, and 🔕 per-session mute. Background operations (model pulls, LLM probes) surface here with live progress — no more scrolling toasts.

### 📊 Claude Code hooks + Status board — *new in v7.0 alpha.34*

Three Claude Code hooks (`Stop`, `PostToolUse`, `UserPromptSubmit`) call a per-session daemon endpoint. Auto-installed at session spawn for `claude-code` backends — daemon writes `.claude/sprint/post-event.sh`, the settings entries, and a `.dw-env` credential file. The session detail **Status** tab renders a live board: current focus, sprint tree, test pass/fail counts, and git branch + dirty flag. Completion detection uses Stop hook events directly — faster and more accurate than screen-buffer pattern matching.

PWA → session detail → **Status** tab. REST: `GET /api/sessions/<id>/status`.

### 🧠 Operator identity — *new in v6.8.x*

A structured operator self-description (role, north-star goals, current projects, values, current focus, context notes) loaded from `~/.datawatch/identity.yaml` and **auto-injected into the wake-up L0 layer of every spawned session**. AI work stays anchored to operator priorities. PWA → Settings → Automata → Identity card or 🤖 robot-icon wizard. CLI: `datawatch identity {get,set,configure,edit}`.

### 🔁 Algorithm Mode — *new in v6.9.0*

PAI's 7-phase structured-thinking harness as a per-session state machine: **Observe → Orient → Decide → Act → Measure → Learn → Improve**. Operator-driven advance with output captured at each gate; PWA shows a color-coded phase strip per active session. CLI: `datawatch algorithm {start,advance,edit,abort,reset,measure} <session-id>`.

### 📊 Evals Framework — *new in v6.10.x*

Rubric-based grading replacing the binary verifier. Suites at `~/.datawatch/evals/<name>.yaml` with capability (~70% threshold) or regression (~99% threshold) modes. Four grader types: `string_match`, `regex_match`, `binary_test`, `llm_rubric`. PWA → Settings → Automata → Evals card. CLI: `datawatch evals {list,run,runs,get-run}`.

### ⚖️ Council Mode — *new in v6.11.0 / wired in v7.0*

Multi-persona structured debate. 6 default personas (security-skeptic, ux-advocate, perf-hawk, simplicity-advocate, ops-realist, contrarian) editable as YAML. Modes: `debate` (3 rounds) or `quick` (1 round). In v7.0 alpha.3+, debates run real LLM inference through the registry dispatcher with per-round parallelism (`Council.MaxParallel`). Real-time SSE event streaming (`/api/council/runs/<id>/events`): `persona_responding` / `round_completed` / `run_completed` events. CLI: `datawatch council {personas,run,cancel,runs,get-run}`.

### 🛠 Skill Registries — *new in v6.7.0*

PAI-format skill manifests with 6 datawatch extensions, synced from git registries (PAI default ships built-in). Resolution at session spawn copies synced files into `<projectDir>/.datawatch/skills/<name>/`. CLI: `datawatch skills {list,registry,get,load}`.

### 🔐 Secrets Manager — *new in v6.4.x*

Centralized native AES-256-GCM encrypted store at `~/.datawatch/secrets.db`, plus optional KeePass, 1Password, and HashiCorp Vault / OpenBao backends. `${secret:name}` references resolve from any configured backend in YAML config, plugin manifests, LLM API key fields, and spawn-time env injection. Per-secret tags + scoping with caller context. Audit-logged on every read. CLI: `datawatch secrets {list,get,set,delete}`.

### 🌐 Tailscale Mesh — *new in v6.5.x*

Tailscale k8s sidecar injected into agent pods for private overlay networking. Headscale-first (self-hosted), commercial Tailscale supported. Pre-auth keys + OAuth device flow. ACL generator with existing-node awareness. CLI: `datawatch tailscale {status,nodes,acl-push}`.

### 💬 The legacy core — still here

- **Multi-channel messaging** — Signal, Telegram, Discord, Slack, Matrix, Twilio, GitHub webhooks, generic webhooks, DNS channel; voice input via Whisper transcription
- **Pluggable LLM backends** — claude-code, aider, goose, gemini, opencode, opencode-acp, ollama, openwebui, custom shell — all routed through the v7.0 LLM registry dispatcher
- **Episodic memory** — vector-indexed project knowledge; SQLite (pure Go) or PostgreSQL+pgvector; Ollama / OpenAI embeddings; XChaCha20-Poly1305 content encryption with key rotation; **5-scope hierarchy** (persona-global → persona-in-project → project-shared → session-local → **discussion**)
- **Temporal knowledge graph** — entity-relationship triples with validity windows
- **Full mempalace 6-axis spatial schema** — floor / wing / room / hall / shelf / box auto-derived at save time; +34pp retrieval improvement
- **4-layer wake-up stack** — L0 identity (incl. Telos) + L1 critical facts + L2 room recall + L3 deep search
- **PWA** — installable Android/iOS web app over Tailscale; xterm.js ANSI streaming; full Settings UI for every config knob
- **Container workers** — Docker / Kubernetes spawn with PQC bootstrap, distroless images, per-pod auth, Tailscale mesh
- **Plugin framework** — manifest-driven hot-reload; subprocess + native plugins; declared comm verbs / CLI subcommands / MCP tools / mobile cards
- **Automata (PRD-DAG orchestrator)** — autonomous PRD decomposition with verification, multi-graph dependencies, guardrails, rubric-based grading
- **Auto rate-limit recovery** — detects rate limits, pauses session, auto-resumes with context after reset window (persisted across daemon restarts)
- **Docs-as-MCP-Interface** — 22 curated howtos searchable + executable through MCP: hybrid vector+BM25 index, plan-then-execute with approval-token round-trip, per-step risk gate
- **System monitoring** — CPU, memory, disk, GPU, network, per-session resource usage; eBPF per-process TCP tracking; Prometheus `/metrics`
- **Bearer token auth + TLS** — auto-generated or custom certs with dual-port HTTP+HTTPS
- **Full audit log** — every operator action recorded with actor / action / details / timestamp
- **Federation** — cross-cluster proxy mode with circuit breaker, offline queue, peer registry, observer rollup; channel routing (inbound message → peer mapping); capability-based access control (14 built-in groups)

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

# 4. Configure your operator identity
datawatch identity configure
# or open the PWA and click the 🤖 robot icon in the header

# 5. Review auto-migrated LLM entries and add your hardware
datawatch llm list
# → ollama-default (auto-migrated from cfg.ollama.host)
# → openwebui-default (auto-migrated from cfg.openwebui.url)

datawatch compute node list
# → datawatch-ollama  kind=ollama  address=http://localhost:11434

# 6. Pull a model and start chatting
datawatch compute pull-model datawatch-ollama llama3.1:8b
datawatch sessions start --llm ollama --model llama3.1:8b --task "Hello"

# 7. Verify
datawatch version            # → datawatch v8.0.0
curl -ks https://localhost:8443/api/health
```

Send `help` in the configured channel to see the command reference.

---

## Surface quick reference

Every datawatch feature is reachable from all of these surfaces:

| Surface | Example |
|---|---|
| REST | `curl https://localhost:8443/api/llms` |
| MCP | `llm_list` / `compute_node_list` (via Claude Code / Cursor / VS Code) |
| CLI | `datawatch llm list` / `datawatch compute node list` |
| Comm | `llm list` / `compute node list` (sent in Signal / Telegram / Matrix / etc.) |
| PWA | Settings → Compute → LLM Configuration / Compute Nodes |
| Mobile | Mirrored via Compose Multiplatform app (`dmz006/datawatch-app`) |
| YAML | `~/.datawatch/datawatch.yaml` `compute:` + `llm:` blocks |

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

### Compute Nodes + LLM Registry (v7.0)

| Command | Description |
|---|---|
| `compute node list` | List registered Compute Nodes |
| `compute node add <name> kind=ollama address=http://...` | Register a node |
| `compute node health <name>` | Check node reachability + stats |
| `compute pull-model <node> <model:tag>` | Pull a model to an Ollama node |
| `compute remove-model <node> <model:tag>` | Delete a model from a node |
| `llm list` | List LLM registry entries |
| `llm add <name> kind=ollama compute_nodes=gpu-1,gpu-2` | Add an LLM entry |
| `llm test <name>` | One-shot probe via the dispatcher |
| `llm models list <name>` | List enabled models for an LLM entry |
| `llm models add <name> model=llama3.1:8b node=gpu-1` | Enable a model |
| `llm in-use <name>` | Show active session + automata bindings |
| `llm reassign <name> --to-llm <other>` | Reassign all active bindings |

### PAI parity

| Verb | Purpose |
|---|---|
| `identity` / `identity show` | Print operator identity / Telos |
| `identity configure` | Run the 6-step interview wizard |
| `algorithm start <id>` | Register a session at Observe phase |
| `algorithm advance <id>` | Close current phase + advance |
| `evals run <suite>` | Execute eval suite |
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
| `skills list` | List synced skills |
| `skills sync community` | Sync the community registry ([dmz006/datawatch-community](https://github.com/dmz006/datawatch-community)) |
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

---

## Documentation index

Full documentation lives in [docs/](docs/) — see [docs/README.md](docs/README.md) for a complete index with all flow diagrams.

### Getting started

| Document | Description |
|---|---|
| [docs/setup.md](docs/setup.md) | Installation, backend setup, voice input, RTK, profiles, proxy mode, encryption |
| [docs/commands.md](docs/commands.md) | Complete command reference (messaging and CLI) |
| [docs/pwa-setup.md](docs/pwa-setup.md) | PWA setup with Tailscale |

### Compute + LLM (v7.0)

| Document | Description |
|---|---|
| [docs/howto/compute-nodes.md](docs/howto/compute-nodes.md) | Register, configure, and monitor Compute Nodes |
| [docs/howto/llm-registry.md](docs/howto/llm-registry.md) | Add LLM entries, set up failover, manage enabled models |
| [docs/howto/ollama-marketplace.md](docs/howto/ollama-marketplace.md) | Browse the Ollama catalog, pull models, check hardware fit |
| [docs/howto/chat-and-llm-quickstart.md](docs/howto/chat-and-llm-quickstart.md) | Fastest path from daemon to chatting with an LLM |

### Sessions + hooks

| Document | Description |
|---|---|
| [docs/howto/sessions-deep-dive.md](docs/howto/sessions-deep-dive.md) | Session anatomy — xterm, channel, stats, status tabs |
| [docs/howto/claude-hooks.md](docs/howto/claude-hooks.md) | Claude Code hooks auto-install + Status board |

### Backends

| Document | Description |
|---|---|
| [docs/llm-backends.md](docs/llm-backends.md) | All LLM backends — claude-code, aider, goose, gemini, opencode, ollama, openwebui, shell |
| [docs/messaging-backends.md](docs/messaging-backends.md) | All messaging backends — Signal, Telegram, Discord, Slack, Matrix, Twilio, ntfy, email, webhooks, DNS |

### Interfaces

| Document | Description |
|---|---|
| [docs/mcp.md](docs/mcp.md) | MCP server — 60+ tools for Cursor, Claude Desktop, VS Code |
| [docs/howto/mcp-tools.md](docs/howto/mcp-tools.md) | MCP tool catalog + usage walkthrough |
| [docs/howto/docs-as-mcp.md](docs/howto/docs-as-mcp.md) | Docs-as-MCP-Interface: search + execute howtos via MCP |
| [docs/api/autonomous.md](docs/api/autonomous.md) | Autonomous PRD decomposition with verification |
| [docs/api/plugins.md](docs/api/plugins.md) | Subprocess plugin framework + manifest format |
| [docs/api/orchestrator.md](docs/api/orchestrator.md) | PRD-DAG orchestrator + guardrails |
| [docs/api-mcp-mapping.md](docs/api-mcp-mapping.md) | API ↔ MCP coverage analysis |
| [docs/skills.md](docs/skills.md) | Skill Registries + manifest format |
| [dmz006/datawatch-community](https://github.com/dmz006/datawatch-community) | Community Skills + Plugins registry — browse and contribute |
| [internal/server/web/openapi.yaml](internal/server/web/openapi.yaml) | OpenAPI 3.0 REST API specification |

### Comms + Federation (v8.2–v8.4)

| Document | Description |
|---|---|
| [docs/howto/channel-routing.md](docs/howto/channel-routing.md) | Route inbound channel messages to specific federation peers (BL331) |
| [docs/howto/file-service.md](docs/howto/file-service.md) | Federated file upload/delete/list under service root (BL333) |
| [docs/howto/discussion-scopes.md](docs/howto/discussion-scopes.md) | Shared WAL-backed discussion memory scopes (BL332) |

### Comm channels

| Document | Description |
|---|---|
| [docs/howto/comm-channels.md](docs/howto/comm-channels.md) | Per-channel setup (Signal, Telegram, Discord, Slack, Matrix, …) |

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
| [ollama](https://ollama.com) | Any recent | Optional — local LLM inference |
| [claude CLI](https://docs.anthropic.com/en/docs/claude-code) | Latest | Optional — claude-code backend |
| [Tailscale](https://tailscale.com) | Any | Optional — for PWA + mesh |
| Go | 1.24+ | Only required for building from source |

---

## License

Polyform Noncommercial 1.0.0. See [LICENSE](LICENSE).

Commercial licensing inquiries: open an issue.

---

## Acknowledgements

Special thanks to **[Daniel Keys Moran](https://en.wikipedia.org/wiki/Daniel_Keys_Moran)** and his novel
**[The Long Run](https://www.amazon.com/Long-Run-Daniel-Keys-Moran/dp/1939888336)** — the story of Trent
the Uncatchable, a thief and hacker operating under the eye of an all-seeing AI surveillance network, sparked a
decades-long obsession with the intersection of technology, autonomy, and the systems that watch over us.
That spirit lives somewhere in this project.

> *"The DataWatch sees everything."*

If you haven't read it: [buy it on Amazon](https://www.amazon.com/Long-Run-Daniel-Keys-Moran/dp/1939888336)
(Kindle edition also available), or borrow it from the
[Internet Archive](https://archive.org/details/longruntaleofcon0000mora).
Daniel has also historically offered copies by email request via his
[blog](https://danielkeysmoran.blogspot.com).

### Additional Acknowledgements

Datawatch's design also borrows heavily from three projects, with full attribution in [docs/plan-attribution.md](docs/plan-attribution.md):

- **[HackingDave/nightwire](https://github.com/HackingDave/nightwire)** — Signal-driven AI coding bot. Episodic memory + Signal-as-control-plane shape.
- **[milla-jovovich/mempalace](https://github.com/milla-jovovich/mempalace)** — Memory palace metaphor, 4-layer wake-up stack, full 6-axis spatial schema, conversation mining, repair self-check.
- **[danielmiessler/Personal_AI_Infrastructure (PAI)](https://github.com/danielmiessler/Personal_AI_Infrastructure)** — Identity / Telos, Algorithm Mode 7-phase, Skills, Evals, Council, ISA generalization.

---

## Contributing

Issues + PRs welcome. Read [AGENT.md](AGENT.md) for the operating rules — every commit follows the documented Pre-Execution / Versioning / Documentation / Mobile-Parity / Secrets-Store rules.
