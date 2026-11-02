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
7. **Live bridge liveness + close reason in self-scope** (added by the
   fix-verification retry) — `channel.BridgeHealth(port)`
   (`internal/channel/bridge_diagnostics.go`) probes the session bridge's
   own `/health` on loopback and classifies the failure mode: no port
   registered (never announced via `/api/channel/ready`) vs. no listener —
   bridge exited and is *not* automatically respawned, restart the session
   vs. listener wedged (timeout) vs. unhealthy HTTP answer.
   `channel.BuildSelfScope` (`internal/channel/session_scope.go`) now
   reports `bridge_alive` + `bridge_close_reason` next to `channel_ready`,
   so `get_my_scope` (MCP) and `GET /api/sessions/self/scope` (REST)
   distinguish "registered but dead" from "registered and live" — the
   stale `ChannelReady=true` left over from the 2026-10-15 drop is now
   diagnosable by the session itself, without server logs.
   Regression-tested by `internal/channel/bridge_diagnostics_test.go`.

No tracker IDs were added outside `docs/plans/` by this investigation: the instrumentation and tests reference the findings doc by path only (e.g. `internal/mcp/server.go:325`, `transport_debug.go` header) — the only BL/HLLM/SEC identifiers appearing in code come from the pre-existing Phase 1 `get_my_scope` work, which follows the repo's established in-code tag convention.

---

## Phase 0 — audit: how a spawned (host-mode) session's MCP channel name/scope is constructed

### 0.1 The session identity triple

A datawatch session has three related identifiers:

| Identifier | Construction | Evidence |
|---|---|---|
| `ID` | 4-char hex suffix (e.g. `ba41`) | `internal/session/store.go:49` (Session struct) |
| `FullID` | `hostname + "-" + ID` (e.g. `local-node-ba41`) | `internal/session/manager.go:1754` — `fullID := fmt.Sprintf("%s-%s", m.hostname, id)` |
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

## Error-mode parity (MCP vs REST) — opaque-failure audit + shared helper

The third leg of BL463's title ("REST error-mode parity") — the audit is scoped to the
MCP surface the phase sections above didn't cover, and the fix is in-tree.

### What was wrong

Many MCP handlers failed *soft*: they returned
`mcpsdk.NewToolResultText("Error: ...")` with a **nil Go error**, so the resulting
`CallToolResult` carried no `isError` flag, no tool name, and (for the REST-loopback
proxy helpers in `sx_parity.go`) no upstream status code or body except what happened
to be spliced into the message string. Consequences:

1. **Clients couldn't distinguish failure from success** — the MCP wire result is
   identical in shape to a successful call (`isError:false`). Claude Desktop, pi,
   and any other client treating `isError` as the failure bit kept going on an error
   string, exactly like the `list_templates`/`delete_template`-class drift the
   earlier BL463 slice flagged for `get_my_scope`'s neighbors.
2. **The REST mirror diverged.** `POST /api/mcp/call` (`internal/server/api.go`)
   marshals the `CallToolResult` verbatim, so a failed tool call came back as
   **HTTP 200 + `isError:false`** through the bridge while the direct REST route
   (`GET /api/templates?project=...`, `POST /api/sessions`, ...) returned a real
   **4xx + JSON body**. Same daemon, same underlying call, two different failure
   signals — the parity gap this PRD names.
3. **Upstream detail was lost or unstructured.** The loopback proxy helpers
   (`proxyGetWithHeaders`/`proxyJSON` in `sx_parity.go`) spliced the daemon's
   error body into the message without any bound — a verbose 5xx dump (config
   dump, big validation payload) went straight into the agent's context;
   memory proxies named the operation `"path"` regardless of which tool was
   running.

### Audit: opaque cases (before → now)

| Handler(s) | Before (soft failure) | After |
|---|---|---|
| `get_my_session_id`, `get_my_scope` (server.go) | hint miss / no resolution returned soft "Error: ..." text | `toolError(f)` — named, `IsError` |
| `start_session` — invalid `permission_mode` | soft "Error: invalid permission_mode..." | `toolErrorf("start_session", ...)` |
| `send_input` / `rename_session` / `kill_session` / `session_output` / `session_timeline` / `session_children` / `send_saved_command` / `schedule_add` — not-found paths | soft `"Session X not found."` (2 of 11 sites were also success-flagged) | `toolErrorf("<tool>", "session %q not found", id)` at all 11 sites |
| `list_sessions` — invalid `view` / invalid `states` combo | soft "Error: unknown view ..." | `toolErrorf("list_sessions", ...)` (legacy string contract for non-error paths unchanged) |
| `sx_parity.go` REST-loopback proxies — `ask`, `project_summary`, `template_list`/`template_upsert`/`template_delete`, `project_list`/`project_upsert`/`project_alias_delete`, `session_rollback`, `cooldown_status`/`cooldown_set`/`cooldown_clear`, `sessions_stale`, `cost_summary`/`cost_usage`/`cost_rates`, `audit_query`/`audit_access_query`, `diagnose`, `reload`, `assist`, `device_alias_*`, `splash_info`, `routing_rules_*`, `analytics` | `NewToolResultText(fmt.Sprintf("Error: %v", err))` — success-flagged, upstream status/body only inside the message, unbounded | `toolError("<tool>", err)` — `IsError` set, tool name in the message; the proxy helpers now build the error as `HTTP %d: <body>` with the body bounded to `maxToolErrorBody` (2048B) |
| memory-loopback proxies (`memory_remember`/`recall`/`list`/`forget`/`stats`/`pin`/`sweep`/`spell_check`/`extract_facts`/`import`/`export`/`learnings`) | `toolError("path", err)` — generic op name | `proxyMemoryPOST`/`proxyMemoryGET` take the tool name as the op |
| `session_reconcile`, `session_import` (session_reconcile.go) | soft "Error: ..." | `toolError(f)` |
| `bl220` MCP tools (bl220_mcp_tools.go) | `textOK("Error: ...")` soft strings | `toolErrorf("<tool>", ...)` |
| `bl241` matrix tools, `bl331` channel-routing, `bl332` discussion tools, `bl356` exit hooks, `bl357` queue, `bl358` discussion subscribe, `bl360` result store, `bl362` channel diagnostics, `agent_audit`, `v52710` channel info, `v5278` gap closures, `v6040`/`v6080` session tools, `profile_tools`, `agent_tools` not-found paths | `NewToolResultText("Error: ...")` with nil error in the not-found / validation branches | `toolError` / `toolErrorf` with the tool's registered name |

No `NewToolResultText("Error: ...")` (non-test) occurrences remain in
`internal/mcp/` except the legacy-view string helper in `list_sessions_view.go`
(which the `list_sessions` handler now wraps with `toolErrorf` rather than emitting
it directly).

### The shared helper (`internal/mcp/errors.go`)

```go
// toolError / toolErrorf → CallToolResult{IsError:true,
//                              content: "<op>: <message>"}
// toolHTTPError → CallToolResult{IsError:true,
//                     content: "<op>: HTTP <status>: <body≤2048B (…-ellipsized if longer)>",
//                     with nil-response/empty-body fallbacks ("HTTP 0" / status text)}
//
// Loopback proxy helpers (sx_parity.go) build their error as
// "HTTP %d: <body>" with the same 2048-byte cap (boundedErrorBody), and
// toolError() names the operation in front of it.
```

`maxToolErrorBody` = 2048 — a verbose daemon error body (config dump, big
validation payload) is cut to 2048 bytes with a `…` in both the proxy helpers
and `toolHTTPError` instead of flooding the client's context.

### What the REST bridge now reports

`POST /api/mcp/call` still returns the marshalled `CallToolResult` — that is now
correct by construction: failed tool calls carry `isError:true` + a named,
bounded message that includes the upstream `HTTP <status>` + body for the
loopback proxies, matching what direct REST gives (4xx + body) semantically,
without pretending the HTTP status line has to double as the MCP signal.

### Tests (this slice)

- `internal/mcp/errors_test.go` — new: `TestToolError_FlagsIsErrorWithPrefix`,
  `TestToolErrorf_FormatsArguments`, `TestToolHTTPError_CarriesStatusAndBody`,
  `TestToolHTTPError_NoBody`, `TestToolHTTPError_TruncatesOversizedBody`,
  `TestBoundedErrorBody_TrimsAndCaps` (the proxy helpers' body cap).
- Assertion updates to the new failure contract: `bl331_channel_routing_tools_test.go`
  (both tests now assert `res.IsError` + the tool-name prefix),
  `bl91_handler_test.go` (`send_input` / `rename_session` failure messages),
  `bl368_vision_describe_test.go` (file-not-found message).
- Full suites: `go test ./internal/mcp/... ./internal/server/...` — **269 + 1013
  passed, 0 failed**; `go build ./...` clean.

---

## Phase 2 — deferred (out of this PRD slice, tracked separately)

- **BL467 fix itself**: making the bridge slot owner-bound (a session that loses the
  first-come race gets an explicit error/signal in-band, not a silent no-op).
  `get_my_scope`'s `channel_ready` + `resolved_via` fields make the failure observable
  in the meantime; the actual owner-binding fix is its own PRD.
- **REST error-mode parity sweep** (the other direction): that every *REST* error
  shape has a matching MCP message — the MCP-side opaque-failure audit + shared
  helper shipped in the section above ("Error-mode parity") covers MCP→REST
  direction; the REST→MCP direction is a separate audit pass, tracked here.
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

---

## Implementation log — 2026-10-20 (smoke / scope / failures slice, v9.5.29)

This slice shipped the rest of the BL463 contract on this branch (dd8c0001):

1. **Smoke hardening (BL463 acceptance):** `scripts/release-smoke-stdio-mcp.sh`
   rewritten as a 7-probe protocol suite (initialize → tools/list incl.
   `get_my_scope` → live `memory_recall` → `get_my_scope` positive →
   `get_my_scope` negative → failing proxy call). `scripts/release-smoke.sh`
   §7r now passes `DW_BIN` / `DW_CONFIG` / `DW_SESSION_HINT` (fixture session
   when the smoke owns the daemon — shell backend preferred) and
   `DW_REQUIRE_HTTP_STATUS=1` in local mode, so the probe demands
   `isError:true` + `HTTP <status>: <body>` on a failing loopback proxy call.
   Verified: live daemon (fallback path + 404 with body), and a sandbox
   (`resolved_via=hint` asserted, fake REST 404).
2. **stdout hygiene (new finding, caught live by §7r on its first run):**
   `datawatch mcp` is a JSON-RPC-over-stdio server; session-store recovery
   logs (`internal/session/store.go` corrupt/merge messages,
   `internal/session/flock_unix.go` flock timeout) were `fmt.Printf`'d to
   stdout and corrupted the framing (`Expecting value: line 1 column 2`).
   Moved to stderr — the daemon-facing log channel — verified by repro
   (subdir-merge scenario): stdout parses strict JSON line-by-line,
   `[recovery]` lines land on stderr.
3. **Version / CHANGELOG / docs:** 9.5.28 → **9.5.29** via
   `scripts/set-release-version.sh` ("MCP held to REST's reliability bar"),
   superseding this doc's earlier "version bump N/A on this branch" note —
   the slice task instructed an explicit bump and one patch of
   `Version` is consistent across `cmd/datawatch/main.go` +
   `internal/server/api.go` (9.5.29). CHANGELOG v9.5.29 entry carries the
   three legs (connection-drop fixes, `get_my_scope` + REST mirror, failing
   contract + smoke) and closes with the reliability-bar statement.
   `docs/mcp.md` gained the failure-response semantics + stderr/log contract
   and `get_my_scope`/`get_my_session_id` in the Sessions family;
   `docs/testing-tracker.md` records the live validations above.
4. **Not included (tracked above, out of this slice):** BL467 bridge
   owner-binding, REST→MCP error-shape sweep, CLI `session scope`.

**Final verification run (release finalization, 2026-10-20):**
`go build ./...` clean; `go test ./...` — 5342 passed / 0 failed across 85
packages; `node --test internal/server/web/*.test.js` — 260 passed / 0 failed;
`scripts/check-no-internal-refs.sh` — clean on user-facing surfaces;
## Phase Completion Checklist (per AGENT.md, for the v9.5.29 slice)

- [x] **Functionality implemented** — smoke §7r suite, `get_my_scope` + REST mirror,
  MCP failure contract (isError + named + status/body), stderr/stdout split, docs.
- [x] **Unit tests added/passing** — `internal/mcp/errors_test.go`,
  `internal/mcp/error_parity_test.go`, repro tests, `drop_repro_test.go`, smoke probes.
- [x] **Live/smoke test** — `scripts/release-smoke.sh` §7r +
  `scripts/release-smoke-stdio-mcp.sh` run live against a running daemon (fallback
  path + real 404 with body) and in a sandbox harness (`resolved_via=hint`, fake REST
  404); recorded in `docs/testing-tracker.md` (PASS 2026-10-20).
- [x] **`go test ./...` + `node --test internal/server/web/*.test.js`** — re-run at
  release finalization (see the implementation log's final run note).
- [x] **General documentation checklist (8 items)** —
  - [x] `CHANGELOG.md` v9.5.29 (all three legs + reliability-bar statement)
  - [x] `docs/config-reference.yaml` — **N/A:** no new config keys; keepalive
        (10 s), timeout profile, and the `DATAWATCH_MCP_DEBUG` env var are runtime
        behavior, not config; the env var is documented in `docs/operations.md`
        (MCP transport diagnostics), where operator log-channel notes live
  - [x] `docs/operations.md` — new "MCP transport diagnostics" section
        (stream observation, keepalive, timeout profile, verbose log, stdio log/stderr
        split, failed handlers no longer kill bridges)
  - [x] `README.md` — **N/A:** the README does not enumerate individual MCP tools
        (it links `docs/mcp.md`/`docs/howto/mcp-tools.md`); the new tool + REST route
        are both covered by those linked surfaces, which are up to date
  - [x] Doc index (`README.md`/`docs/README.md`) — **N/A for new files:**
        `docs/mcp.md`, `docs/cursor-mcp.md`, `docs/datawatch-definitions.md`,
        `docs/operations.md` are existing, already-indexed files this phase updated;
        the findings doc sits under `docs/plans/` which is indexed as a backlog area
  - [x] `docs/testing-tracker.md` — §7r live lines + failure-contract lines +
        `GET /api/sessions/self/scope` (new REST endpoint)
  - [x] No leaked tracker IDs — this phase's new docs use plain language on the
        must-not-contain surfaces (`datawatch-definitions.md`, `operations.md`,
        `README.md` untouched); `scripts/check-no-internal-refs.sh` runs as part of
        release smoke (and was clean at finalization)
  - [x] Parity surface — updated above (CLI deferred to Phase 2; mobile/app surfaces
        unaffected — see below)
- [x] **Conditional docs** — `docs/datawatch-definitions.md` updated (the
  self-session block gains `get_my_scope` + the REST scope route); `docs/flow/*.md`
  and `docs/howto/*.md` **N/A:** no new operator workflow or data-flow concept was
  introduced — one read-only tool, one read-only REST mirror, diagnostic logging.
- [x] **Mobile-Parity Rule** — **N/A with note:** nothing the mobile app parses
  changed — no PWA layout/behavior change, no REST/WS route was renamed, re-shaped or
  removed; the only new REST surface is *additive*
  (`GET /api/sessions/self/scope`) and mobile-app parity for a *self-scope* query is
  not a datawatch-app use case (it is a session-side identification call, same class
  as `GET /api/sessions/self`, which is already not app-parsed for scope); MCP-side
  failure-message changes reach AI-agent clients only. No `datawatch-app` issue
  filed, per this task's rule ("file the issue only if a REST/WS contract changed").
- [x] **Localization Rule** — **N/A:** no `app.js` / locale-bundle changes in this
  slice (no PWA strings touched).
- [x] **Version bumped in both files** — 9.5.28 → 9.5.29 via
  `scripts/set-release-version.sh`, `cmd/datawatch/main.go` and
  `internal/server/api.go` in lockstep; CHANGELOG `## v9.5.29` heading present.
- [x] **Plan status lines current** — this doc's top Status line, the Version/release
  note (superseded-in-place by the implementation log), and the implementation log
  all reflect the shipped slice;
  `docs/plans/README.md` BL463 row updated in place (deferred items noted), since
  Phase 2 items make this **partially completed**, not fully moved-to-completed.
