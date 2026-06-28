# DATAWATCH-CONTEXT.md — Datawatch Context & Quick Start

**Last Updated**: 2026-06-27  
**Version**: v8.13.2  
**Load this before any session** — it contains architecture, rules, memory queries, MCP access, and common workflows.

---

## What is Datawatch?

**A distributed control plane for orchestrating AI work** — recursive, episodic, secure, and structured across hosts, clusters, and channels.

### Elevator Pitch
Single-binary daemon that:
- **Runs AI coding sessions** in tmux (Claude Code, Aider, OpenCode ACP)
- **Remembers** via episodic memory (PostgreSQL-backed, vector search)
- **Plans** autonomously with multi-phase reasoning (BL24 Automata / PRDs)
- **Debates** via multi-persona Council (12+ built-in personas + custom)
- **Orchestrates** across federation mesh (Tailscale + direct + proxy routing)
- **Bridges** messaging (Signal, Telegram, Slack, Discord, Matrix) + REST API + PWA + Android app
- **Attests** security via eBPF observer (CPU, mem, net, GPU live metrics)
- **Encrypts** operational data (sessions, discussions, configs)

### Core Use Cases
1. **Autonomous AI workflows** — spawn child PRDs, debate proposals, execute structured tasks
2. **Multi-channel messaging** — Signal/Telegram/Slack/Discord operators get notifications and can reply
3. **Session orchestration** — lifecycle, state machine, environment variables, concurrent execution
4. **Mobile companion** — Android phone, Wear OS, Android Auto with full parity to PWA
5. **Team collaboration** — discussion scopes (federated append-only WAL), multi-user access control

### Directory Structure
```
.
├── cmd/datawatch/          Main CLI + daemon startup (contains var Version)
├── internal/
│   ├── session/            Session lifecycle (Launch, Resume, Kill, state machine)
│   ├── server/             REST API, WebSocket, PWA web UI (also contains var Version)
│   ├── llm/
│   │   └── backends/       LLM backends: claude-code, aider, opencode-acp, ollama, openwebui
│   ├── messaging/          Signal, Telegram, Slack, Discord, Matrix bridges
│   ├── inference/          Automata executor, task scheduler, debate framework
│   ├── observer/           eBPF metrics (CPU, mem, GPU, net)
│   ├── council/            Multi-persona debater + persona wizard
│   ├── federation/         Cross-instance routing, proxy, Tailscale
│   ├── memory/             Episodic memory (embeddings, semantic search, WAL)
│   └── mcp/                MCP server (tools that sessions can call)
├── templates/              session-CLAUDE.md (auto-injected rules at session start)
├── docs/                   Architecture, setup, operations, release notes, plans
├── deploy/                 Kubernetes, systemd manifests
├── docker/                 Container images
├── scripts/                Build, release, validation, smoke tests
├── Makefile                Build targets: build, install, test, fmt, lint, cross
├── AGENT.md                **Read before any code changes** — all guardrails, memory, DATAWATCH-CONTEXT rules
└── CLAUDE.md               RTK instructions only (auto-managed, overwritten on session exit)
```

---

## Rules & Guardrails

**CRITICAL: Read AGENT.md before any code changes.** It contains all guardrails for:
- **Pre-execution rule** (line 9): Re-read relevant AGENT.md sections before coding
- **Scope Constraints** (line 25): Work only within `/home/dmz/workspace/datawatch/`
- **Code Quality** (line 31): Go code must compile, interfaces must remain stable
- **Testing Tracker** (line 40): Every interface needs unit tests + live connection tests
- **Git Discipline** (line 59): Conventional commits, no force-push to main
- **Versioning** (line 67): Patch (Z), Minor (Y.0), Major (X.0.0) — update BOTH `cmd/datawatch/main.go` and `internal/server/api.go`
- **Documentation** (line 117): Every behavior-changing commit MUST update docs/CHANGELOG
- **Session Rules** (line 714): Live rules in `templates/session-CLAUDE.md` (auto-injected at session start)
- **Memory Use Rule** (line 738): Use episodic memory via MCP — NOT optional, it's part of your context

**Key: Do not assume, do not auto-release, do not version-bump without explicit user request.**

---

## Load Memory Context Before Working

**CRITICAL**: The datawatch episodic memory system is your collaborator. Use it proactively via MCP:

### Current Memories Available

These are real memories from past sessions (as of 2026-05-22):

1. **[v8.0.0 Release Stats](project_v800_release_stats.md)** — E2E story counts (626), smoke sections (85), bugs found (7). Use when planning release scope.
2. **[E2E Testing Rule](feedback_e2e_no_skip_for_live_state.md)** — Tests needing live sessions/hooks/Automata must create them inline, never skip. Note: Whisper uses `/home/dmz/.datawatch/.venv`; DNS requires 2nd test instance.
3. **[External Issue Triage](feedback_external_issue_triage.md)** — Non-dmz006 issues: check Skills/Plugins first; only add to core what extensions cannot do.
4. **[No Release Without Ask](feedback_no_release_without_ask.md)** — Do NOT bump versions, write CHANGELOG, or create release notes unless user explicitly asks to release. Implementation done = stop at "build passes + committed."
5. **[SSE + Polling Pattern](feedback_streaming_sse_pattern.md)** — SSE primary, 202+polling utility, exponential backoff, Tailscale-aware low-power reconnect.

### Query Memory at Session Start

When using MCP tools in a datawatch session launched by the daemon:

```
memory_recall "recent changes bugs fixes"     # What changed recently
memory_recall "feedback rules patterns"       # Feedback from past sessions  
memory_recall "session state opencode"       # Domain-specific context
kg_query "opencode"                          # Relationships (people, modules, tools)
research_sessions "federation bugs"          # Cross-session research
```

### Save Patterns After Work

When you discover a pattern or make a key decision:

```
memory_remember "
**Description of what you learned**
- Problem: ...
- Root cause: ...  
- Solution: ...
- When to apply: ...
"

kg_add "entity1" "relationship" "entity2"
```

---

## MCP Access & Tools Available

When launched by the datawatch daemon, sessions get full MCP access to:

### Memory System (Episodic + Knowledge Graph)
```
memory_recall(query)               # Semantic search: "recent changes", "patterns", etc.
memory_remember(text)              # Save a finding or pattern for future sessions
memory_list(n, project_dir)        # List recent memories
memory_export() / memory_import()  # Backup/restore full memory
kg_query(entity)                   # Query knowledge graph: "opencode", "federation", etc.
kg_add(subject, relation, object)  # Record a relationship (e.g. "BL96 depends_on federation")
research_sessions(query)           # Cross-session research across all outputs + memories
```

### Session & Configuration Management
```
list_sessions()                    # All running sessions (state, task, backend)
start_session(task, llm, project_dir)  # Launch a new session
kill_session(session_id)           # Terminate session
get_config(section)                # Current daemon config (returns YAML structure)
get_version()                      # Daemon version
```

### Secrets Management
```
secret_list()                      # List secret names (values redacted)
secret_get(name)                   # Retrieve secret value (audit-logged)
secret_set(name, value, tags)      # Store secret
```

**How to use:** In a datawatch-spawned claude-code/aider/opencode session, MCP tools are available directly. Call them without any prefix — just use the function names above.

---

## Build, Test, Release

### Build Locally
```bash
# Using make (recommended)
make build          # Builds to ./bin/datawatch
make install        # Installs to ~/.local/bin/datawatch
make test           # Runs all tests
make fmt            # Format code (gofmt)
make lint           # Linting (golangci-lint)

# Direct go commands
go build -ldflags="-X main.Version=8.6.2" -o ./bin/datawatch ./cmd/datawatch/
go test ./...
```

### Testing Strategy
- **Unit tests**: `go test ./...` — must pass before commit; close to 100% coverage required
- **Live tests**: Create actual sessions, verify in PWA/logs (not optional for interfaces)
- **Regression**: Before any release, test key flows (session lifecycle, backends, messaging)
- **eBPF**: If modifying observer, verify CAP_BPF present and metrics flow

### Version Bumping (ONLY When User Asks to Release)

**Do NOT touch version numbers unless the user explicitly says "release" or "bump the version."**

When user asks to release:

```bash
# 1. Edit BOTH files (they must match):
#    - cmd/datawatch/main.go: var Version = "X.Y.Z"
#    - internal/server/api.go: var Version = "X.Y.Z"

# 2. Update CHANGELOG.md (move [Unreleased] to [X.Y.Z])

# 3. Create release notes: docs/RELEASE_NOTES_v8.X.Y.md

# 4. Commit: git commit -m "v8.X.Y: description of what's in this release"

# 5. Verify: ./bin/datawatch version shows the new version

# 6. User will create the GitHub release and push if needed
```

See AGENT.md § Versioning (line 67) and § Release vs Patch Discipline (line 204) for full rules.

---

## Key Directories & Files

### Architecture & Design
- **`docs/architecture.md`** — Component overview, data flow, federation mesh
- **`docs/data-flow.md`** — How messages flow: session → channel → backends → PWA
- **`docs/operations.md`** — Deployment, config, security, monitoring
- **`docs/security-model.md`** — Encryption, secrets, access control
- **`docs/plans/`** — Feature plans, architecture decisions, backlog tracker
- **`CHANGELOG.md`** — User-facing change history
- **`README.md`** — Feature showcase, quick start, platform info

### Core Packages
- **`internal/session/manager.go`** — Session lifecycle state machine
- **`internal/server/api.go`** — REST API routes + version string
- **`internal/llm/backends/`** — Backend implementations (claude-code, aider, opencode)
- **`internal/messaging/`** — Signal, Telegram, Slack, Discord, Matrix bridges
- **`internal/inference/`** — Automata executor, PRD planner, debate framework
- **`internal/memory/`** — Episodic memory, embeddings, semantic search
- **`cmd/datawatch/main.go`** — CLI entry, daemon startup, version string

### Configuration & Templates
- **`AGENT.md`** — All guardrails, Memory & Intelligence rules, and DATAWATCH-CONTEXT context loading
- **`CLAUDE.md`** — RTK instructions only (auto-managed)
- **`templates/session-CLAUDE.md`** — Injected into sessions launched by daemon
- **`.env.build`** — Build environment (golang version, etc.)

### Testing & Validation
- **`docs/testing-tracker.md`** — Interface test matrix (unit + live validation)
- **`internal/*/`** `*_test.go` — Unit tests for every package
- **`scripts/e2e/`** — End-to-end test suite

---

## Common Workflows

### 🐛 Fix a Bug (AGENT.md § Error-Filing Rule, line 1202)

1. **Query memory** — check if similar bugs were fixed before
   ```
   memory_recall "session state WebSocket cycling"
   ```

2. **Reproduce & locate** the bug via tests or real session

3. **Fix the code** — localized change, minimal scope

4. **Test** — `rtk go test ./...` must pass before commit

5. **Commit once** — one logical fix = one commit with conventional message
   ```
   git commit -m "fix(session): screen-capture path ignores FirstTick guard"
   ```

6. **Save the pattern** to memory AFTER verifying the fix works
   ```
   memory_remember "Bug: FirstTick guard must skip both detection and activity marking ..."
   ```

**Do NOT bump version, write CHANGELOG, or create release notes unless user asks to release.**

### ✨ Add a Backend (AGENT.md § New LLM backend, line 152)

1. **Check AGENT.md** for interface requirements (SignalBackend, LLMBackend are stable)

2. **Implement the interface** in `internal/llm/backends/<name>/`
   - Launch(), SendInput(), Name(), Version() methods
   - Unit tests with close to 100% coverage

3. **Add live connection test** — actually spawn the backend, send commands, verify output

4. **Document** in docs/llm-backends.md with: prerequisites, installation, config, command, interactive input support

5. **Update** docs/testing-tracker.md with test status

6. **Commit** — one commit per feature, conventional message

**Version bump is only if user explicitly asks to "release" this feature.**

### 🚀 Release (Only When User Asks)

When user says "release" or "bump the version":

1. **Update versions in BOTH files** (or they won't match):
   - `cmd/datawatch/main.go`: `var Version = "X.Y.Z"`
   - `internal/server/api.go`: `var Version = "X.Y.Z"`

2. **Update docs** — CHANGELOG (move [Unreleased]), create RELEASE_NOTES_v8.X.Y.md

3. **Build & verify** — `make build && ./bin/datawatch version`

4. **One commit** — `git commit -m "v8.X.Y: <description>"`

(See AGENT.md § Release vs Patch Discipline, line 204 for full details.)

### 🧠 Using Memory from a Session

When in a datawatch-spawned session:

```
memory_recall "bug patterns opencode"        # Search for past findings
memory_remember "New pattern: ..."           # Save what you learned
kg_query "federation"                        # Understand relationships
research_sessions "session state issues"     # Cross-session research
```

---

## OpenCode — Production Configuration (v8.7.0)

OpenCode is a first-class backend alongside claude-code. Key production details:

### LSP (Language Server Protocol)
- **What**: Real-time language intelligence (type errors, completions, diagnostics) from an installed language server binary
- **How**: Operator **selects** the language in the session-creation UI — no auto-detection
- **Config**: `lsp.servers` in daemon config defines available presets (go, typescript, python, rust, cpp)
- **Wire**: Daemon writes `<projectDir>/opencode.json` with `{"lsp": {"<lang>": {"command": [...]}}}` at session start
- **Claude Code**: Does NOT support native LSP — use shell diagnostics (`gopls check`, `tsc --noEmit`) instead
- **API**: `GET /api/lsp` → `{servers: {lang: {command, extensions}}}` populates the UI dropdown

### Model Selection (OpenCode)
- **Format**: `provider/model` — e.g. `anthropic/claude-sonnet-4-6`, `ollama/llama3`
- **API**: `GET /api/opencode/models?node=<cn>` → returns cloud + Ollama models for the given compute node
- **Wire**: Daemon writes `model` field to `<projectDir>/opencode.json` at session start
- **Ollama + multi-compute**: When an `ollama/` model is selected with a compute node, `provider.ollama.apiUrl` is set to that node's Ollama endpoint in opencode.json

### Per-Session opencode.json
Datawatch manages `<projectDir>/opencode.json` (project-scope override, merged with global `~/.config/opencode/opencode.jsonc`):
```json
{
  "model": "anthropic/claude-sonnet-4-6",
  "lsp": {
    "go": { "command": ["gopls"], "extensions": [".go"] }
  }
}
```
File is written at session start and cleaned up at session end (`tooling.BackendArtifacts["opencode"]` includes `opencode.json`).

### OpenCode Config Pattern
- Global: `~/.config/opencode/opencode.jsonc` — provider keys, permission, MCP
- Project: `<projectDir>/opencode.json` — model + LSP (written by datawatch per-session)
- Datawatch also writes `<projectDir>/.mcp.json` for the datawatch MCP channel

---

## Recent Context (as of 2026-06-27)

**Version:** v8.13.2  
**Last changes (v8.12.9–v8.13.2):**

- **v8.13.2** — `schedule spawn` overlap guard + run history (`fire_count`/`last_fire_at`/`last_fire_result`/`active_spawn_id`) + `--shell`/`--path` CLI flags (GH#128)
- **v8.13.1** — FCM/push payload enrichment: `session_id`, `session_name`, `last_response` in `session_state_changed` and `waiting_input` push events (GH#117)
- **v8.13.0** — `session.extra_mcp_servers` config: inject additional MCP servers into every spawned session `.mcp.json` (GH#118); alert dock always receives alerts in session-detail view (GH#120); `navigate('session-detail', …)` typo fix (4 sites)
- **v8.12.9** — `Manager.SendInput` now applies `scheduleSettleMs` to ALL send sources (was schedule-only); `POST /api/sessions/send` dedicated endpoint; CLI `session send` uses new endpoint
- **v8.12.x** — subprocess spawn mode for shell tasks (exit code = completion); completion pattern matching in TUI output (CR-split, non-ASCII strip, task-echo suppression)
- **v8.11.x** — `schedule spawn` command introduced: ephemeral one-shot sessions from a cron, independent of any running session
- **v8.10.21–v8.10.25** — TTS summarizer humanization, session new error surfacing, sanitizeForSpeech post-processor

### Known Good Patterns (from memory)
1. **Session state machine** — FirstTick guard must skip both detection AND activity marking
2. **Backend resume** — When a backend server restarts, create fresh session (do not reconnect old ID)
3. **Opencode specifics** — Auto-generates "ses..." IDs, does not accept custom IDs via POST /session
4. **Testing requirement** — E2E tests needing live sessions/hooks must create them inline; never skip
5. **Memory-driven development** — Always query memory before work; save patterns after discovering them
6. **LSP selection** — Operator selects language at session creation; daemon writes opencode.json. Never auto-detect
7. **CLAUDE_CONFIG_DIR** — Must be in tmux session env for claude-code sessions; set via hookEnv in manager.go. Sessions started before v8.6.3 won't have it — restart to fix
8. **SendInput settle** — `scheduleSettleMs` (default 200ms) applies to ALL send sources since v8.12.9. Set >0 in config to fix Enter not delivered to React Ink TUIs
9. **extra_mcp_servers** — `session.extra_mcp_servers` YAML field injects MCP servers into `.mcp.json` on every spawn; for non-claude-code backends via `WriteProjectMCPConfig`, for claude-code via `InjectExtrasIntoMCPConfig`
10. **schedule spawn** — `datawatch schedule spawn --shell <cmd> --cron "0 * * * *" --path <dir> --schedule-name <name>` runs shell jobs on a cron without LLM; overlap guard prevents stacked concurrent runs; `schedule list` shows FIRES/LAST-FIRE/LAST-RESULT

### Gotchas to Avoid (from AGENT.md)
- ❌ **No version bump without ask** — User must explicitly request release; implementation != release
- ❌ **Interface stability** — SignalBackend, LLMBackend are breaking-change boundaries
- ❌ **Testing is mandatory** — 100% code coverage, unit + live tests for every interface
- ❌ **Both Version strings** — `cmd/datawatch/main.go` AND `internal/server/api.go` must match
- ❌ **No internal IDs in user docs** — BL7, F11, etc. only in `docs/plans/`, not README/CHANGELOG
- ❌ **Commit frequently** — One logical change = one commit (no squashing history)
- ❌ **schedule_settle_ms** — Never name this in user docs; refer to it as "settle delay" or the `schedule_settle_ms` config key

---

## How to Load This Context Automatically

**For AI Sessions Launched by Datawatch:**
This file is referenced in `AGENT.md`. Every claude-code/aider/opencode session launched from the daemon will:
1. Have access to `DATAWATCH-CONTEXT.md` in the project root
2. Automatically query memory for recent changes at session start
3. Have full MCP access to memory tools (memory_recall, memory_remember, kg_query, etc.)

**For Manual Sessions:**
When starting work manually:
```bash
# Read this file
cat DATAWATCH-CONTEXT.md

# Query memory
mcp__datawatch__memory_recall "recent work this codebase"

# Start your session knowing the context
```

**For RTK Support:**
RTK automatically excludes `DATAWATCH-CONTEXT.md` from token optimization (it's metadata, not code). When you reference it in your work, RTK treats it as context-load and doesn't compress it.

---

## Quick Links

| Resource | Purpose |
|----------|---------|
| [README.md](README.md) | Feature showcase, quick start, platforms |
| [AGENT.md](AGENT.md) | All guardrails (read before any code changes); Memory & Intelligence rules at end |
| [CLAUDE.md](CLAUDE.md) | RTK instructions only (auto-managed, ephemeral) |
| [docs/architecture.md](docs/architecture.md) | Component overview & data flow |
| [docs/operations.md](docs/operations.md) | Deployment, config, security |
| [CHANGELOG.md](CHANGELOG.md) | User-facing change history |
| [docs/testing-tracker.md](docs/testing-tracker.md) | Test matrix (unit + live) |
| [docs/plans/](docs/plans/) | Feature plans & architecture decisions |

---

**Happy coding! Load this before you start, query memory frequently, save patterns after work. 🚀**
