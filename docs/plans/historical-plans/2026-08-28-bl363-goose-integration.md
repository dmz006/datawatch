# BL363 — Goose Full Integration

**Filed:** 2026-08-28  
**Status:** Open feature  
**Source:** Operator request — match Claude Code / opencode controls for Block's Goose agent  
**Relates to:** `internal/llm/backends/goose/`, `internal/config/config.go`, `internal/inference/llm.go`

---

## Context

Goose (Block's AI coding agent, github.com/block/goose) is already partially wired into datawatch:

| Layer | Status |
|---|---|
| `KindGoose` inference kind | ✅ defined in `internal/inference/llm.go:61` |
| `GooseConfig` struct | ✅ in `internal/config/config.go:617` — enabled, binary, console size, output/input mode |
| Observer process detection | ✅ `internal/observer/types.go:438` — tracks `goose` process name |
| Cost tracking | ✅ placeholder rates in `internal/session/cost.go:38` |
| Tooling artifacts | ✅ `.goose/` in gitignore helper |
| Backend implementation | ⚠️ Stub only — `internal/llm/backends/goose/backend.go` |
| `init()` registration | ❌ backend never calls `llm.Register()` — never reachable |
| Interactive TUI mode | ❌ `SupportsInteractiveInput() false` |
| Session resume | ❌ `Resumable` interface not implemented |
| MCP / channel integration | ❌ no equivalent to Claude Code `--channels` |
| Model/provider injection | ❌ no `GooseProvider`/`GooseModel` config fields |
| Agent container | ❌ no `Dockerfile.agent-goose` |

The stub at `internal/llm/backends/goose/backend.go` runs `goose run --text 'task'` and emits a
`DATAWATCH_COMPLETE:` marker, but is never registered (no `init()`), so no session can actually
target it. This plan fixes all gaps.

---

## Goose CLI Surface (v1.43.0+)

```bash
goose session                  # interactive TUI (starts a new session)
goose session start [--name N] # start named session
goose session resume [name]    # resume a named session by name
goose run --text 'task'        # non-interactive one-shot
goose --version                # version string (semver, no prefix)
goose configure                # interactive provider setup (not usable headlessly)
```

**Environment variables Goose respects:**
- `GOOSE_PROVIDER` — provider name (`anthropic`, `openai`, `google`, `databricks`, `ollama`, …)
- `GOOSE_MODEL` — model identifier
- `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` / `GOOGLE_API_KEY` — API credentials
- `GOOSE_MCP__<NAME>__TYPE=stdio` + `GOOSE_MCP__<NAME>__CMD=...` — inline MCP server config
- `GOOSE_CLI_THEME=plain` — disables fancy rendering in non-TTY contexts

**Session storage:** `~/.config/goose/sessions/<name>.jsonl` — JSONL conversation log.

**No HTTP server mode** — Goose does not expose an ACP-style REST/SSE API (as of v1.43.0). The
integration path is env-var injection + tmux send-keys + MCP extension config.

---

## Gap Analysis vs Claude Code

| Claude Code Control | Mechanism | Goose Equivalent | Gap |
|---|---|---|---|
| Interactive TUI | launch `claude` with no task | `goose session` | T1 |
| One-shot task | `claude -p 'task'` | `goose run --text 'task'` | ✅ exists (not registered) |
| Session resume | `claude --resume UUID` | `goose session resume [name]` | T1 |
| Session naming | `claude --name <name>` | `goose session start --name <name>` | T1 |
| Binary resolution | PATH + home dir fallbacks | same pattern needed | T1 |
| `init()` registration | `llm.Register()` in init | missing | T1 |
| Model selection | `--model sonnet` | `GOOSE_MODEL=...` env var | T2 |
| Provider selection | n/a (Anthropic-only CLI) | `GOOSE_PROVIDER=...` env var | T2 |
| API key injection | `ANTHROPIC_API_KEY` | provider-specific key env var | T2 |
| MCP / channel events | `--dangerously-load-development-channels` | `GOOSE_MCP__DATAWATCH__*` env vars | T3 |
| Hook installer | `.claude/settings.json` + shell scripts | no equivalent; MCP is the hook surface | T3 |
| Completion detection | `DATAWATCH_COMPLETE:` marker | same marker (already in stub) | ✅ |
| Permission modes | `--permission-mode plan` | no equivalent (Goose has no permission system) | n/a |
| Chrome | `--chrome` | no equivalent | n/a |
| Effort level | `--effort max` | no equivalent | n/a |

---

## Implementation Plan

### T1 — Backend Parity (aider/gemini tier)

**Scope:** make Goose fully operational as a first-class session backend.

**File: `internal/llm/backends/goose/backend.go`** — rewrite stub to:

1. **Binary resolution** — same `resolveBinary()` pattern as opencode:
   - Check `PATH`
   - Check `~/.local/bin/goose`, `~/.config/goose/bin/goose`, `/usr/local/bin/goose`

2. **`init()` registration** — `llm.Register(New("goose"))` — currently missing, nothing can resolve the goose backend.

3. **Interactive TUI mode** (`SupportsInteractiveInput() true`):
   ```go
   cmd = fmt.Sprintf("cd '%s' && GOOSE_CLI_THEME=plain %s session", escapedDir, b.binary)
   ```

4. **One-shot mode** (existing, but must also be the `goose-prompt` variant):
   ```go
   cmd = fmt.Sprintf("cd '%s' && GOOSE_CLI_THEME=plain %s run --text '%s'; echo 'DATAWATCH_COMPLETE: goose done'",
       escapedDir, b.binary, escaped)
   ```

5. **`PromptBackend`** — mirror `opencode.PromptBackend` for one-shot-only sessions where a task is required.

6. **`Resumable` interface** — `LaunchResume()`:
   - If `resumeID` is a session name: `goose session resume <name>`
   - Start the named session fresh if resume fails (fallback like opencode)
   - Store the session name via `Nameable` so it can be resumed later

7. **`Nameable` interface** — `SetSessionName(name string)`:
   - Passed via `goose session start --name <name>` in interactive mode
   - Stored so `LaunchResume` can derive the same name

8. **`Version()` fix** — current version output is not prefixed with `v`:
   ```go
   // goose --version outputs "1.43.0" (no "v"); normalize
   out = "v" + strings.TrimSpace(string(out))
   ```

**Tests** — mirror `internal/llm/backends/opencode/models_test.go` pattern for:
- Binary resolution fallback
- Version normalization
- Launch command construction (table-driven: interactive, prompt, with-name, resume)

---

### T2 — Model & Provider Injection

**Scope:** expose `provider` and `model` in `GooseConfig` so operators can configure which LLM Goose uses without running `goose configure` interactively.

**`internal/config/config.go`** — add to `GooseConfig`:

```go
type GooseConfig struct {
    Enabled     bool   `yaml:"enabled"`
    Binary      string `yaml:"binary"`
    ConsoleCols int    `yaml:"console_cols,omitempty"`
    ConsoleRows int    `yaml:"console_rows,omitempty"`
    OutputMode  string `yaml:"output_mode,omitempty"`
    InputMode   string `yaml:"input_mode,omitempty"`
    // T2 additions:
    Provider    string `yaml:"provider,omitempty"`  // GOOSE_PROVIDER
    Model       string `yaml:"model,omitempty"`     // GOOSE_MODEL
    APIKeyRef   string `yaml:"api_key_ref,omitempty"` // secret name → env var
}
```

**Backend changes** — accept provider/model at construction; inject as env vars in launch command:
```go
env := ""
if b.provider != "" { env += "GOOSE_PROVIDER=" + shellQuote(b.provider) + " " }
if b.model    != "" { env += "GOOSE_MODEL=" + shellQuote(b.model) + " " }
if apiKey     != "" { env += providerKeyEnvVar(b.provider) + "=" + shellQuote(apiKey) + " " }
```

`providerKeyEnvVar()` maps provider name → key env var:
- `anthropic` → `ANTHROPIC_API_KEY`
- `openai` → `OPENAI_API_KEY`
- `google` → `GOOGLE_API_KEY`
- others → `GOOSE_API_KEY` (Goose's generic fallback)

**API surface (`internal/server/api.go`)** — add provider/model to the goose config response block (line 4702) and to the `config_set` handler.

**LLM registry** — session-backend LLM entries of `KindGoose` can store provider/model in the `Model` field (for display) and a custom field or tag for provider. Use the same `Address` field for provider (already used for binary path disambiguation in v6 backends).

**`internal/agents/spawn.go`** — if an agent spec targets a goose LLM, inject `GOOSE_PROVIDER` + `GOOSE_MODEL` into the agent container's env block (same pattern as `OPENCODE_PROVIDER_URL`/`OPENCODE_MODEL`).

---

### T3 — MCP / Channel Integration (Claude Code parity)

**Scope:** configure Goose to use datawatch as an MCP extension so session state events can flow back without relying solely on the `DATAWATCH_COMPLETE:` text marker.

**Mechanism** — Goose supports inline MCP server configuration via environment variables:

```bash
GOOSE_MCP__DATAWATCH__TYPE=stdio
GOOSE_MCP__DATAWATCH__CMD=/usr/local/bin/datawatch
GOOSE_MCP__DATAWATCH__ARGS=mcp-stdio,--session-id,<sessionID>
```

Or via a Goose profile YAML at `~/.config/goose/profiles.yaml` (generated per session).

**What this enables:**
1. Goose tool calls arrive at the datawatch MCP server → same `OnACPEvent`-style hook as opencode-acp
2. The datawatch `reply` tool works inside Goose sessions (notifications, memory, etc.)
3. State machine transitions can be driven by Goose's tool-call structure rather than text pattern matching
4. No hooking into `.claude/settings.json` equivalent needed — MCP is Goose's native extension surface

**Config field** — `GooseConfig.ChannelEnabled bool yaml:"channel_enabled"` (mirrors `ClaudeCodeConfig.ChannelEnabled`)

**Backend changes** — when `channelEnabled`, append MCP env vars to launch command:
```go
if b.channelEnabled && b.sessionFullID != "" {
    env += fmt.Sprintf(
        "GOOSE_MCP__DATAWATCH__TYPE=stdio "+
        "GOOSE_MCP__DATAWATCH__CMD=%s "+
        "GOOSE_MCP__DATAWATCH__ARGS=mcp-stdio,--session-id,%s ",
        shellQuote(b.binaryPath), b.sessionFullID)
}
```

**Session manager hook** — in `OnSessionStart`, when backend family is "goose" and channel is enabled:
- Record the session's `fullID` so the MCP tool calls can be correlated
- Wire `OnMCPToolCall(fullID, toolName, args)` → `MarkACPEvent` (same path as ACP events)
- This gives Goose the same structured state transitions as opencode-acp without needing an HTTP server

**Completion detection** — for MCP-enabled sessions, the `DATAWATCH_COMPLETE:` text marker remains as fallback. The MCP `reply` tool call (which always fires at session end via exit hooks) becomes the primary completion signal.

**Note on limitation:** Goose's MCP tool calls are `stdio` (not SSE), so we don't get streaming deltas like opencode-acp. But we get: turn start, tool calls, turn end. That's enough for the state machine.

---

### T4 — Agent Container (stretch)

**Scope:** `docker/dockerfiles/Dockerfile.agent-goose` — container for Goose-based autonomous agents, mirroring `Dockerfile.agent-aider` and `Dockerfile.agent-gemini`.

**Key differences from other agent containers:**
- Goose installs via `curl https://github.com/block/goose/releases/download/v<VER>/goose-linux_amd64.tar.gz`
- No Python venv needed (Goose is a self-contained binary)
- `~/.config/goose/` must be writable inside the container (sessions dir)
- `GOOSE_CLI_THEME=plain` prevents terminal escape bleed in pipe-pane captures

**`docker/dockerfiles/Dockerfile.agent-goose`** structure:
```dockerfile
ARG GOOSE_VERSION=1.43.0
ARG GOOSE_ARCH=amd64

RUN curl -fsSL --retry 10 --retry-delay 30 --retry-all-errors \
    "https://github.com/block/goose/releases/download/v${GOOSE_VERSION}/goose-linux_${GOOSE_ARCH}.tar.gz" \
    -o /tmp/goose.tar.gz && \
    tar -xzf /tmp/goose.tar.gz -C /usr/local/bin/ && \
    chmod +x /usr/local/bin/goose && \
    rm /tmp/goose.tar.gz

ENV GOOSE_CLI_THEME=plain
```

**CI:** Add `build-agent-goose` job to `.github/workflows/release.yaml` following the `build-agents` pattern. Add Trivy scan step.

---

## Non-Goals

- **Permission modes** — Goose has no equivalent to `--permission-mode plan`. Skip.
- **Chrome integration** — Goose has no browser automation mode. Skip.
- **Effort levels** — Goose has no `--effort` flag. Skip.
- **opencode-acp–style SSE streaming** — Goose has no HTTP server. T3 covers MCP stdio instead.
- **Goose inference adapter** — Goose is a session-backend kind, not an inference-API kind. No `adapters/goose.go` needed.

---

## Delivery Order

| Sprint | Deliverable | Unlocks |
|---|---|---|
| S1 | T1 complete + tests | Goose sessions launchable and resumable via datawatch UI |
| S2 | T2 complete | Operator can select provider/model from profile config |
| S3 | T3 complete | Session state machine driven by MCP events; `reply` tool works |
| S4 | T4 complete | Autonomous agent containers using Goose |

S1 is self-contained and safe to ship without S2–S4.

---

## Open Questions

1. **Goose session name format** — what characters are valid in `--name`? Need to sanitize the
   datawatch session ID (e.g. strip colons). Check goose source or test empirically.
2. **MCP stdio transport latency** — does `GOOSE_MCP__*` env var config load before the first tool
   call? Or is a profile YAML more reliable? Validate in T3 integration test.
3. **Goose version pinning in Dockerfile** — should `GOOSE_VERSION` track latest or pin like RTK?
   Recommendation: pin with a tracked ARG, update alongside security scan cycles.
4. **`goose session resume` fallback** — if the named session doesn't exist on a fresh container,
   does Goose start fresh or error? Need error behavior spec before implementing `LaunchResume`.
