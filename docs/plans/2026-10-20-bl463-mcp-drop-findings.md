# BL463 — MCP connection reliability, session isolation clarity, REST error-mode parity

Status: **Phase 0 (audit) complete; Phase 1 (read-only `get_my_scope` tool + REST mirror) implemented in this commit; the live CONNECTION_CLOSED drop incident investigated (this doc) with repro tests and permanent mitigations shipped.**
PRD `bf78e50a` (autonomous PRD backlog, PRD #5 in `docs/plans/README.md`).

---

## Incident — mid-session MCP CONNECTION_CLOSED (live report 2026-10-15)

### Symptom

A spawned session's datawatch MCP connection reported **CONNECTION_CLOSED mid-session and never recovered**, while `/api/mcp/tools` on the same daemon stayed healthy. That split is the defining evidence: the daemon's MCP *logic* was fine; what died was the *per-client transport* (SSE stream or stdio bridge process) and nothing on the client side could re-establish it.

Before this investigation there was **zero server-side evidence** of why any of our transports close:

- mcp-go v0.46.0 **silently discards write and flush errors** on its SSE main loop (`server/sse.go`) — a stream that dies on a failed write leaves no log line at all, and a mid-write death is indistinguishable from a client/middlebox kill by timing alone.
- mcp-go's `WithRecovery()` tool-panic middleware is **opt-in and was unregistered on every datawatch MCP entry point** (daemon SSE, daemon stdio, bridge) — so a single panicking tool handler killed the whole process serving that session. For a per-session stdio bridge that is *exactly* "CONNECTION_CLOSED, daemon healthy, never recovers" (`handleMessage` runs `HandleMessage` in a goroutine on the SSE path — the daemon itself was dead, with `/api/mcp/tools` served by the still-up sibling process explaining the split).
- The stdio `Listen` loop logged nothing on exit — no start line, no stop reason.
- The daemon's `http.Server` timeout profile (ReadTimeout 30s / IdleTimeout 120s / WriteTimeout unset) was set in code without any record of which of these stdlib deadlines can actually touch an in-flight SSE stream.

### What dropped — the traced drop modes

Two drop classes; one is **permanent** (matches the incident), one is recoverable:

1. **PERMANENT: panic in a tool handler killing the stdio bridge (or daemon SSE process).** The client's only transport endpoint dies; the client sees the stream close and, because bridge processes are spawned per-session and *nothing respawns them*, there is no recovery path. The daemon's REST endpoints keep working because they live in the daemon process. **Evidence:** `internal/mcp/drop_repro_test.go` `TestStdioTransportDropFromToolPanicRecovered` — the *unregistered*-recovery path is what a panic does by mcp-go default (a tool handler panic is an unrecovered panic → process death in the stdio `Listen` goroutine); with the now-mandatory recovery middleware registered, the identical panicking tool returns an `isError` JSON-RPC result (`"internal panic in tool ..."`) and the **same session** answers a second tool call with `pong` afterward. PASS in 0.00s.
2. **RECOVERABLE but silent: the SSE stream going quiet and dying mid-session.** mcp-go v0.46.0 defaults to `keepAlive=false` — a held-open SSE stream is *silent* unless pings are opted in, which is exactly what a proxy, middlebox or the client's own idle read timeout then kills on their schedule. **Evidence:** `TestSSETransportSurvivesDaemonTimeoutProfile` (control — with the 100ms test keepalive the daemon's exact timeout profile never drops the stream; the daemon now ships a 10s keepalive, `server.go ServeSSE`) and `TestSSETransportDropWhenWriteTimeoutExpires` (setting the single stdlib deadline that CAN kill an in-flight stream — `WriteTimeout` — drops the stream with the captured close log below). Both PASS in ~0.3–0.6s.

### Evidence captured by the repro tests

`go test ./internal/mcp/ -run 'Drop|Transport' -v -count=1` (2026-10-20):

```
--- PASS: TestSSETransportSurvivesDaemonTimeoutProfile   (0.30s)
--- PASS: TestSSETransportDropWhenWriteTimeoutExpires    (0.60s)
--- PASS: TestSSEObserveHandlerRecordsClientDisconnect   (0.30s)
--- PASS: TestStdioTransportDropFromToolPanicRecovered   (0.00s)
--- PASS: TestStdioTransportCloseReasonOnClientDisconnect(0.00s)
--- PASS: TestToolPanicGuardMiddlewareDirectly           (0.00s)
--- PASS: TestGuardStdioListenContextCancelLogging       (0.15s)
ok   github.com/dmz006/datawatch/internal/mcp        1.366s
```

The instrumentation now records, at close time, exactly the lines the incident lacked (captured live from the WriteTimeout drop repro):

```
mcp/sse: STREAM OPEN /sse from "127.0.0.1:42416" (session transport begins)
mcp/sse: STREAM CLOSE after 300ms — client disconnected (request context: context canceled) |
        writes=8 bytes=289 idle_at_close=0s FIRST_WRITE_ERROR="write tcp 127.0.0.1:45729->127.0.0.1:42416: i/o timeout"
```

The `FIRST_WRITE_ERROR` field is the smoking gun the incident never had: server-side write failure vs client/middlebox kill are recorded, not guessed. `TestSSEObserveHandlerRecordsClientDisconnect` proves the benign side: a client disconnect logs `client disconnected (request context: context canceled)` with the `writes=/bytes=/idle_at_close=` stream summary.

### Timeout-profile audit (which stdlib deadline can touch an in-flight SSE stream)

From the Go net/http source, verified by the two control/drop tests above:

| Deadline | Daemon value | Can kill an in-flight SSE stream? |
|---|---|---|
| `ReadTimeout` | 30s | **No** — applies only while reading a request; no read occurs on a body-less stream while its handler runs |
| `IdleTimeout` | 120s | **No while in-flight** — applies between keep-alive requests; can kill a *stalled POST* that outlives 120s |
| `WriteTimeout` | **unset (deliberately)** | **Yes** — the only one; `readRequest` wraps the handler in `SetWriteDeadline`, so in-flight event/keepalive writes fail on expiry (reproduced in 0.6s above) |
| Keepalive pings | 10s (new) | N/A — keeps the stream visibly alive against client/middlebox idle kills |
| Bridge stdio path | no HTTP | None of the above apply at all — the stdio risk class is process-panic/EOF, covered by the recovery + stop-reason logging |

`LogSSEServerTimeouts` prints this profile verbatim at serve start, so a future profile change is auditable next to the stream logs it would break.

### How this incident differs from BL444 (cross-reference only, do not conflate)

BL444 ("MCP SSE listener retries bind at boot", `docs/plans/README.md` row 47) is a **boot-time availability** failure class in the same subsystem:

| Axis | BL444 (boot bind gap) | This incident (mid-session drop) |
|---|---|---|
| When it fails | Listener (re)start at boot — bind/accept retry | Stream already established, held open in-session |
| What dies | The listener never came up (or the retry loop) | The established transport (stdio bridge process or the SSE stream) |
| Client state at failure | Connection refused / no endpoint found | `CONNECTION_CLOSED` on a live session |
| Recovery shape | Daemon restart / retry succeeds from cold | Nothing was respawning the per-session bridge — recovery required a *new* session, not a retry |
| Server evidence at time of failure | Absent listener logs | Absent close-reason logs (now fixed by the same instrumentation) |

Different failure classes, different fixes (boot-time retry vs in-session transport hardening + close-reason audit trail); both live in the MCP SSE/stdio transport path, which is why they get cross-referenced.

### Mitigations shipped (permanent)

1. **Panic recovery on every MCP entry point** — `RegisterToolPanicRecovery` (internal/mcp/transport_debug.go) wired at daemon MCP server construction (`internal/mcp/server.go:330`); `GuardStdioListen` wraps the daemon stdio path (`server.go ServeStdio`) so panics escaping the synchronous dispatch are also recovered.
2. **SSE stream open/close observability always on** — `ObserveSSEHandler` (wrapped at `server.go ServeSSE`): one `STREAM OPEN` / `STREAM CLOSE` line pair per stream with duration, write count, bytes, idle-at-close and first write/flush error (the mcp-go-discarded errors the incident lacked). `DATAWATCH_MCP_DEBUG=1` adds per-request lines.
3. **Stdio stop-reason logging** — `GuardStdioListen` logs `listener <name> started` / `stopped — stdin closed/EOF (client disconnected or shutting down)` (or `stopped with error: …` / `panic recovered: …`), so the "who closed the stream and why" question has a server-side answer.
4. **Silent-stream prevention** — SSE keepalive ping fixed at 10s (`server.go ServeSSE`, mcp-go `WithKeepAliveInterval`) so held-open streams stay visibly alive against client/middlebox idle kills.
5. **Timeout profile made intentional and printed** — ReadTimeout 30s / IdleTimeout 120s / *no* WriteTimeout retained, with the rationale logged at serve start (`LogSSEServerTimeouts`).
6. **Repro tests** — `internal/mcp/drop_repro_test.go` (7 tests, all named `…Transport…`/`Drop…`, all green; 252 passed whole-package).

No tracker IDs were added outside `docs/plans/` by this investigation: the instrumentation and tests reference the findings doc by path only (e.g. `internal/mcp/server.go:325`, `transport_debug.go` header) — the only BL/HLLM/SEC identifiers appearing in code come from the pre-existing Phase 1 `get_my_scope` work, which follows the repo's established in-code tag convention.

---

## Phase 0 — audit: how a spawned (host-mode) session's MCP channel name/scope is constructed

### 0.1 The session identity triple

A datawatch session has three related identifiers:

| Identifier | Construction | Evidence |
|---|---|---|
| `ID` | 4-char hex suffix (e.g. `ba41`) | `internal/session/store.go:49` (Session struct) |
| `FullID` | `hostname + "-" + ID` (e.g. `johnnyjohnny-ba41`) | `internal/session/manager.go:1754` — `fullID := fmt.Sprintf("%s-%s", m.hostname, id)` |
| tmux session | `cs-<sanitizedHostname>-<ID>` | `internal/session/manager.go:1759` |

`FullID` is the identifier the daemon uses for **everything channel/session-scoped**:
session token minting, MCP server naming, `.claude` settings scoping, audit attribution.

### 0.2 The two MCP server names (the core confusion source)

There are **two different MCP server names** involved for one session, and code, docs and
operator memory mix them up:

1. **`datawatch-<FullID>`** (per-session, user-scope) — the channel actually used by
   claude-code sessions.
   - `internal/channel/channel.go:181` `RegisterSessionMCP(sessionID, …)`:
     `name := "datawatch-" + sessionID`; the same literal reappears in
     `UnregisterSessionMCP` (:199) and `ChannelServerName` (:204).
   - Mints env `CLAUDE_SESSION_ID=<FullID>` and `DATAWATCH_CHANNEL_PORT=0`
     (random port per session) when registering — `channel.go:190-191`.
   - Called from `cmd/datawatch/main.go:1547` in the pre-launch hook:
     `channel.RegisterSessionMCP(sess.FullID, channelJSPath, sessionChannelEnv)`.
   - Registered with **`claude mcp add --scope user`** into claude's global
     `~/.claude.json` (or the `CLAUDE_CONFIG_DIR`/`datawatch-config` variant, BL318),
     so it lives **outside the project dir** and outside `.mcp.json`.
   - `CLAUDE_CONFIG_DIR` is deliberately NOT injected into session env (GH#128) so the
     spawned claude process finds the user-scope registration.

2. **`datawatch`** (shared, project-scope) — a legacy/global-style entry.
   - Literal `"datawatch"` is written into the project's `.mcp.json` by
     `channel.WriteProjectMCPConfig` (`internal/channel/mcp_config.go:99`
     `cfg.MCPServers["datawatch"] = spec`), and by the global `RegisterMCP`
     (`channel.go:176`).
   - For **claude-code** sessions this path is explicitly avoided: the pre-launch hook
     *removes* any `"datawatch"` entry from `.mcp.json`
     (`cmd/datawatch/main.go:1490-1496`, BL109 comment: a shared entry on the default
     port 7433 breaks multi-session setups) and uses the per-session named server
     instead (:1485-1549, `BackendFamily != "claude-code"` vs `==` branch).
   - For **non-claude-code** backends (opencode/goose/… that honour `.mcp.json`) the
     shared `datawatch` entry is the only MCP wiring those backends see.

**Key audit finding:** the mapping *session → MCP server name* is
`datawatch-<hostname>-<4-hex>` for the session that matters, but every in-code name of
that relationship is just a string-concatenation in `internal/channel`, and there is
**no tool, REST endpoint or CLI command that answers "what is MY channel's name/scope?"**
from inside a session. `get_my_session_id` (BL349) returns the session ID but says
nothing about the MCP server name, the project dir mode, tmux identity or which other
sessions' names must NOT be used.

### 0.3 Credential / capability scope (Design A3 / HLLM-001)

- `cmd/datawatch/main.go:1367-1388`: per-session `sessionChannelEnv` mints a
  **session-scoped token** (`sessionTokenStore.Mint(sess.FullID, resolved)` where
  resolved defaults to the `session-default` group, `main.go:1370-1379`) and passes it
  to the channel as `DATAWATCH_TOKEN`.
- `internal/federation/capabilities.go:357` `session-default` group: includes
  `CapHealthRead`, `CapSessionsRead`, `CapCommRead/Write`, memory, queue, results —
  excludes secrets, config writes, daemon control.
- HLLM-004 in-band scoping: when an MCP call reaches the shared daemon MCP server via
  `POST /api/mcp/call` authenticated by that token,
  `internal/server/api.go:1096-1103` resolves token → FullID and stashes it on context
  via `federation.WithCallerSessionID`, which the MCP server reads in
  `scopedCallerSessionID` (`internal/mcp/server.go:172-188`, checked before the
  process-level `s.callerSessionID`). **This is the correct, authoritative source for
  "who am I"** — a new self-scope tool must resolve through it, exactly like
  `get_my_session_id` already does.

### 0.4 Mode matrix (host / container / docker-agent)

| Mode | MCP wiring | Evidence |
|---|---|---|
| **host (tmux, claude-code)** | `claude mcp add --scope user datawatch-<FullID>` + `CLAUDE_SESSION_ID` env + random port (DATAWATCH_CHANNEL_PORT=0); `.mcp.json` `datawatch` entry removed | `main.go:1547`, `channel.go:181-196` |
| **host (tmux, non-claude-code)** | shared `datawatch` entry in project `.mcp.json` | `main.go:1485-1490` |
| **container/agent (F10)** | session record gets `AgentID` set (runs inside a worker container spawned by a parent); the parent's own host-side channel naming rules still apply | `internal/session/store.go:121-126` |
| **docker driver (agents)** | **no MCP registration at all** — `docker run` with a `callbackURL`; agents talk to the daemon over REST callbacks | `internal/agents/docker_driver.go` |

So "container mode" (in the F10 sense) is *orthogonal* to MCP wiring: a container
session is still identified by its `AgentID` on the record, and its MCP channel (if
any) is registered by the *host* daemon under the host's `FullID`-derived name.

### 0.5 Related known gap: BL467 registration race

- `docs/plans/2026-10-16-mcp-bridge-registration-race.md` — if two sessions start
  while no MCP channel is registered, the **first** to reach `/api/channel/ready` owns
  the single bridge slot (first-come, not owner-bound). The other session's channel
  callbacks silently no-op.
- `get_my_scope` (Phase 1) does not fix BL467 — it **makes the failure visible from
  inside a session**: the tool reports per-session `channel_ready` and the set of other
  active sessions' MCP names, so a dropped/collided registration is diagnosable with
  one tool call instead of grepping daemon logs + `claude mcp list`.

### 0.6 What an agent *can* and *cannot* say about its own scope today

| Question | Available tool? |
|---|---|
| "What is my session ID?" | `get_my_session_id` (BL349) / `GET /api/sessions/self` |
| "Is my channel actually ready?" | `channel_info` (v5.27.10) — but returns whatever the *first-come* bridge says (BL467) |
| "What MCP server name is **mine**?" | **No tool.** Requires `claude mcp list` + knowing your FullID |
| "Which other sessions exist and what are their MCP names (so I don't use one)?" | `list_sessions` gives IDs/names/states, not MCP names; operator must derive `datawatch-<FullID>` |
| "Am I host or container?" | `list_sessions` has `agent_id` on the record but no dedicated field/tool surfaces "my" mode |
| "What capability group am I under (session-default vs admin)?" | **No tool.** The capability check exists only at the REST chokepoint (`POST /api/mcp/call`, `federation.RequiredCapForMCPTool`) |

---

## Phase 1 — the fix: `get_my_scope` MCP tool + `GET /api/sessions/self/scope` REST mirror

### What it does

`get_my_scope` is a **read-only** tool (capability `health:read`, in the `session-default`
group — already granted to every per-session scoped credential) that answers, from
inside the calling session:

1. **Identity** — `session_id` (4-hex), `full_id` (hostname-4hex), `hostname`.
2. **MCP scope** — the **exact MCP server name this session's channel is registered
   under** (`datawatch-<FullID>` via `channel.ChannelServerName`, the same source
   `RegisterSessionMCP`/`UnregisterSessionMCP` use), and the **legacy shared name**
   `datawatch` (the `.mcp.json` entry / `RegisterMCP` global name) so the operator can
   see both names and which is authoritative for this session's backend.
3. **Mode** — `host` vs `container(agent=<AgentID>)` read from the session record's
   `AgentID` field (the F10 marker), plus the session's `State`.
4. **Project dir / tmux** — `project_dir`, `tmux_session` (from the record),
   `ephemeral_workspace` flag — the isolation surface the session actually occupies.
5. **Isolation clarity** — a `other_sessions` list: every *other* session currently
   running/waiting_input, with each one's MCP server name computed the same way, so
   the caller can see (a) its own name is distinct from every peer's, and (b) which
   names must NOT be used. This is the "session isolation clarity" half of BL463.
6. **Channel readiness** — `channel_ready` for this session (the BL467-visible bit).
7. **Credential scope** — whether the call arrived through a per-session scoped
   credential (Design A3, `scopedCallerSessionID` present) or unscoped/admin — so the
   agent knows which capability tier its own calls are running under.
8. **`resolved_via`** — `caller-scope` (HLLM-004 in-band context, authoritative when
   the session connected through its own scoped credential), `hint` (explicit
   `hint=<id>` param), or `channel-ready-fallback` (the BL349 fallback — only when no
   caller-scope or hint was available). This directly documents the "first-come"
   ambiguity of the fallback so it's visible, not silent.

### Resolution order (mirrors the HLLM-004 design exactly)

1. `hint` param, if supplied → `resolveSession(hint)`, error if not found.
2. Caller session ID from context (`federation.CallerSessionIDFromContext`) /
   process-level `s.callerSessionID` (i.e. `scopedCallerSessionID`, the same
   resolver `ownsSession`/`get_my_session_id`-adjacent checks already use) →
   `manager.GetSession(callerFullID)`.
3. Fallback: first `ChannelReady` + (Running or WaitingInput) session (the BL349
   behaviour, already established for `get_my_session_id` and `GET /api/sessions/self`)
   — only reached when neither a hint nor a scoped caller is available (e.g. an
   unscoped admin/peer call with no hint), and flagged as such in `resolved_via`.

### Parity surface

- **MCP:** `get_my_scope(hint?)` — read-only hint annotation, `health:read` capability
  (so it's callable under the default `session-default` group a spawned session
  already has — no extra grants needed).
- **REST:** `GET /api/sessions/self/scope` — same field set as JSON, same resolution
  order (hint query param, then — note: REST has no in-band caller-scope context the
  way the MCP chokepoint does, so REST's "self" is the same channel-ready-fallback as
  the pre-existing `GET /api/sessions/self` endpoint it sits next to; the MCP tool is
  the higher-fidelity surface for a session to ask about itself, per the existing
  BL349 split where the MCP tool and the REST endpoint have the same fallback but the
  MCP tool additionally consults the in-band caller context).
- **CLI:** out of scope for this PRD slice — `datawatch session self` already exists for
  the ID; operators can hit the REST endpoint directly. (Flagged, not blocked, as a
  possible follow-up in Phase 2 if it's wanted.)
- **Channel:** same as REST (the channel protocol routes through the daemon's HTTP
  routes), so no separate channel-side work.
- **Documentation:** `docs/mcp.md` (Available Tools), `docs/cursor-mcp.md` (tools
  table), `docs/datawatch-definitions.md` (self-session discovery block, next to the
  existing `get_my_session_id` entry).

### Tests (in this commit)

- `internal/mcp/my_scope_test.go` — unit tests: (a) a host-mode session can confirm its
  own scope (MCP server name = `datawatch-<FullID>`, distinct from every other
  session's name), (b) hint resolution, (c) hint that doesn't resolve → error, (d)
  no-hint/no-caller fallback picks the channel-ready active session and says so in
  `resolved_via`, (e) isolation list excludes self and includes peers' names.
- `internal/server/self_scope_test.go` — REST mirror: field parity with the MCP tool's
  payload for the same session, `hint` param, 404 on unknown hint.
- Cap coverage: the existing
  `internal/mcp/mcp_tool_cap_coverage_test.go`
  (`TestEveryUnconditionallyRegisteredToolHasAnMCPToolCapEntry`) already asserts every
  `AddTool`-registered tool has a `federation.MCPToolCap` entry — `get_my_scope`'s
  `health:read` entry in `internal/federation/mcp_tool_caps.go` is verified by that
  test, no duplicate test needed.

---

## Phase 2 — deferred (out of this PRD slice, tracked separately)

- **BL467 fix itself**: making the bridge slot owner-bound (a session that loses the
  first-come race gets an explicit error/signal in-band, not a silent no-op).
  `get_my_scope`'s `channel_ready` + `resolved_via` fields make the failure observable
  in the meantime; the actual owner-binding fix is its own PRD.
- **REST error-mode parity sweep** (the third leg of BL463's title "error-mode parity
  with REST"): auditing that every MCP tool's failure response has a matching,
  same-shape REST failure (and vice versa) is a cross-cutting audit of its own, not
  something this single-tool slice should attempt.
- **CLI `session scope` command**, if/when wanted.

## Version / release

- **Version bump: N/A on this branch.** This is a PRD worktree branch
  (`prd-worktrees/dd8c0001`), and per `AGENT.md` "Feature branches and merges" no
  branch in a merge run changes `cmd/datawatch/main.go` or `internal/server/api.go` —
  the merge script picks the next patch/feature version strictly higher than main's
  current and records one bump commit covering the branches (currently at 9.5.28).
- New MCP tool + new REST route = new feature, so the eventual merge-bump should be a
  **minor** (X.Y.0) not a patch, flagged for whoever lands it (the merge-script bump
  is the version of record either way).
- CHANGELOG `[Unreleased]` entry added (docs/testing-tracker §4.6 rule: MCP tools go in
  the changelog).
- **Documentation**: see "Parity surface" above — no silent-omission gap: new tool is
  in `docs/mcp.md`, `docs/cursor-mcp.md`, `docs/datawatch-definitions.md`; the new REST
  endpoint is in `docs/testing-tracker.md` (test coverage index for MCP tools + REST
  endpoints).
