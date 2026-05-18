# BL316 — Cross-Host Session Federation + CBAC Plan

**Backlog item:** BL316  
**Version:** v7.3.0  
**Date filed:** 2026-05-18  
**GitHub issue:** #52  
**Sprint prefix:** T38  
**Test story range:** TS-564 – TS-603

---

## Goal

Enable two or more datawatch instances to know about each other, share session
visibility, route input cross-host, and enforce fine-grained capability-based
access control (CBAC) on everything a federated peer can touch — REST, MCP,
CLI, WebSocket, and comm channels.

**User requirement (verbatim):**
> "federated controls should apply through all comms channels and controls like
> cli, api, mcp, etc because federated peer may try to use any since it's driven
> by an ai that knows all connection types"

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  datawatch-A (admin)                                            │
│                                                                 │
│  serverStore (servers.json)                                     │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │  Entry{Name:"B", URL:"http://B:8080", Token:"tok-B",      │  │
│  │         Federated:true, Capabilities:["session-operator"]}│  │
│  └───────────────────────────────────────────────────────────┘  │
│                                                                 │
│  fedAuthMiddleware ──► fedCap(required) check at every handler  │
│                                                                 │
│  /api/federation/peers    CRUD for peer registry                │
│  /api/federation/groups   CRUD for custom capability groups     │
│  /api/federation/sessions fan-out: cfg.Servers + federated peers│
│                                                                 │
│  Cross-host input: "B/sess-abc" → proxy POST to B:8080          │
└───────────────────────────┬─────────────────────────────────────┘
                            │ Bearer tok-B
                            ▼
┌─────────────────────────────────────────────────────────────────┐
│  datawatch-B (peer)                                             │
│                                                                 │
│  fedAuthMiddleware recognises tok-B → tags ctx with peer-B      │
│  Every handler calls fedCap(required) — 403 if missing          │
└─────────────────────────────────────────────────────────────────┘
```

---

## Capability model

50 individual `surface:action` constants across 18 surfaces. 13 built-in
groups. Operator-defined custom groups persist to `<dataDir>/federation/groups.json`.

**Built-in groups (compiled in, immutable):**

| Group | Key capabilities |
|---|---|
| `monitor` | health:read, analytics:read, sessions/agents/alerts list |
| `session-viewer` | sessions:list/read, agents:list/read |
| `session-operator` | session-viewer + write/kill/input + pipeline start/cancel |
| `inference-admin` | llms:*, compute:* |
| `config-reader` | config:read, docs:read |
| `config-admin` | config:read/write |
| `analytics-viewer` | analytics:read, dashboard:read, audit:read |
| `autonomous-operator` | autonomous:list/read/write/run |
| `council-operator` | council:list/read/run |
| `federation-peer` | health:read, sessions:list/read/input, agents:list/read, observers:list/read, alerts:list/read, dashboard:read, federation:list/read |
| `comm-bridge` | sessions:list/read/input, comm:read/write, alerts:list/read |
| `read-only` | all :read/:list caps across every surface |
| `full-control` | all 50 capabilities |

**Enforcement points (S1 ✅ done; remaining marked):**

| Entry point | Enforcement | Status |
|---|---|---|
| REST handlers (sessions list/write/kill/input, start, MCP call) | `fedCap()` at handler top | ✅ S1 |
| WebSocket MsgCommand, MsgNewSession | `wsHasCap()` per-message | ✅ S1 |
| `/api/federation/peers` CRUD | admin-only (federated peers blocked) | ✅ S1 |
| CLI (federation subcommands) | passes admin token | S2 |
| Comm channel commands | `fedCap()` in comm router | S2 |
| All remaining REST handlers | `fedCap()` systematic sweep | S3 |

---

## Sprint plan

### S1 — CBAC foundation + peer registry REST API ✅ Shipped 2026-05-18

- `internal/federation/` package: caps, groups, Resolve/Check, GroupStore
- `multiserver.Entry` extended: `Capabilities []string`, `AuthType`, `GetByToken()`
- REST: `/api/federation/peers` CRUD + test sub-route
- REST: `/api/federation/groups` CRUD + `/builtins` read-only endpoint
- `fedAuthMiddleware` — peer tokens accepted alongside admin token
- Capability enforcement wired: sessions, start, kill, command, session-input, MCP call, WS command/new-session
- 10 unit tests (capabilities) + 12 integration tests (peers/groups API) — all pass

---

### S2 — Fan-out extension + cross-host input + 7-surface parity

**Scope:**

1. **Extend `/api/federation/sessions` fan-out** to include `serverStore.ListFederated()` entries in addition to `cfg.Servers`. Currently only YAML-seeded servers are fanned out to; runtime-registered federation peers are invisible to the aggregator.

2. **Cross-host send_input routing** — when session ID has the format `<peer_name>/<session_id>` (slash-delimited), proxy the input to that peer's `/api/sessions/{session_id}/input` endpoint using the peer's registered token. Supported entry points:
   - `POST /api/sessions/<peer>/<id>/input` (REST)
   - `POST /api/command` with routed `send: <peer>/<id>: text` syntax
   - WebSocket `MsgSendInput` with `<peer>/<id>` session ID

3. **MCP tools — 12 new tools** (proxy to REST, same pattern as `bl312_server_tools.go`):
   - `federation_peer_list` — list federation peers
   - `federation_peer_add` — register a new peer (name, url, token, capabilities)
   - `federation_peer_get` — get one peer by name
   - `federation_peer_update` — update a peer's config / capabilities
   - `federation_peer_delete` — remove a peer
   - `federation_peer_test` — ping peer's /api/health, return latency + version
   - `federation_group_list` — list builtin + custom groups
   - `federation_group_list_builtins` — list built-in groups only
   - `federation_group_add` — create a custom group
   - `federation_group_get` — get a group by name
   - `federation_group_update` — update a custom group
   - `federation_group_delete` — delete a custom group

4. **CLI subcommands** (`cmd/datawatch/cli_federation.go`):
   - `datawatch federation peer list`
   - `datawatch federation peer add <name> --url <u> --token <t> --capabilities <cap,...>`
   - `datawatch federation peer get <name>`
   - `datawatch federation peer update <name> --capabilities <cap,...>`
   - `datawatch federation peer delete <name>`
   - `datawatch federation peer test <name>`
   - `datawatch federation group list`
   - `datawatch federation group add <name> --caps <cap,...>`
   - `datawatch federation group get <name>`
   - `datawatch federation group update <name> --caps <cap,...>`
   - `datawatch federation group delete <name>`
   - `datawatch federation group builtins`

5. **Comm channel commands** (extend `router/commands.go` + `executeCommand`):
   - `federation peers` → list federated peers
   - `federation peer add <name> <url>` → register peer
   - `federation peer delete <name>` → remove peer
   - `federation peer test <name>` → ping peer
   - `federation groups` → list groups

6. **PWA — Federation Peers panel in Observer view:**
   - New card below "Federated Peers" (meta-peers) in Observer tab
   - Lists registered federation peers: name, URL, enabled badge, capabilities pill list, last-test latency
   - Add / delete / test buttons (admin only — peer tokens see read-only view)
   - "Edit capabilities" modal with group picker + individual cap toggles

7. **YAML seeding** — extend `RemoteServerConfig` to accept `capabilities` and `auth_type` fields so YAML-seeded federation peers can be declared with explicit cap grants.

8. **Locale × 5** — add keys for all new PWA strings to en/es/de/fr/ja bundles.

9. **Howto doc** — `docs/howto/federation-cbac.md`:
   - How to register a peer with limited capabilities
   - How to create custom groups
   - How to verify capability enforcement
   - Capability reference table

**Test stories (T35, TS-500 – TS-530):**

| Sprint | ID | Surface | Test |
|---|---|---|---|
| T38 | TS-564 | REST | GET /api/federation/peers returns [] on fresh install |
| T38 | TS-565 | REST | POST /api/federation/peers creates peer with federation-peer default caps |
| T38 | TS-566 | REST | POST /api/federation/peers/{name}/test returns {ok,latency_ms,version} |
| T38 | TS-567 | REST | GET /api/federation/groups returns {builtins:[13 items],custom:[]} |
| T38 | TS-568 | REST | POST /api/federation/groups creates custom group, persists across reload |
| T38 | TS-569 | REST | DELETE /api/federation/groups/monitor returns 403 (builtin protected) |
| T38 | TS-570 | REST | Peer token with sessions:list cap → GET /api/sessions returns 200 |
| T38 | TS-571 | REST | Peer token without sessions:write → POST /api/sessions/start returns 403 |
| T38 | TS-572 | REST | Peer token without comm:write → POST /api/mcp/call returns 403 |
| T38 | TS-573 | REST | Unknown token → GET /api/sessions returns 401 |
| T38 | TS-574 | REST | GET /api/federation/sessions fans out to runtime-registered federated peers |
| T38 | TS-575 | REST | POST /api/sessions/peer-alpha/sess-123/input proxies to peer's /api/sessions/sess-123/input |
| T38 | TS-576 | MCP | federation_peer_list returns [] on fresh install |
| T38 | TS-577 | MCP | federation_peer_add creates peer |
| T38 | TS-578 | MCP | federation_peer_test returns ok/latency shape |
| T38 | TS-579 | MCP | federation_group_list returns builtin groups |
| T38 | TS-580 | MCP | federation_group_add creates custom group |
| T38 | TS-581 | CLI | datawatch federation peer list exits 0 |
| T38 | TS-582 | CLI | datawatch federation peer add ... exits 0 |
| T38 | TS-583 | CLI | datawatch federation peer delete <name> exits 0 |
| T38 | TS-584 | CLI | datawatch federation group list exits 0 |
| T38 | TS-585 | CLI | datawatch federation group add exits 0 |
| T38 | TS-586 | Comm | "federation peers" in comm → returns peer list |
| T38 | TS-587 | Comm | "federation peer add name url" → registers peer |
| T38 | TS-588 | Comm | "federation groups" → returns group list |
| T38 | TS-589 | PWA | Observer tab shows Federation Peers card with list |
| T38 | TS-590 | PWA | Add peer modal creates peer and refreshes list |
| T38 | TS-591 | PWA | Peer token → Federation Peers card is read-only (no add/delete buttons) |
| T38 | TS-592 | Locale | 5 bundles contain federation_peers_title key |
| T38 | TS-593 | Locale | 5 bundles contain federation_cap_group_label key |
| T38 | TS-594 | Docs | docs_search "federation peer capabilities" returns federation-cbac.md |

---

### S3 — Systematic REST handler sweep + comm capability enforcement

**Scope:**
- Apply `fedCap()` to ALL remaining REST handlers not covered in S1:
  - `/api/agents/*` → agents:list/read/spawn/terminate
  - `/api/autonomous/*` → autonomous:list/read/write/run
  - `/api/council/*` → council:list/read/run
  - `/api/llms/*` → llms:list/read/write
  - `/api/compute/nodes/*` → compute:list/read/write
  - `/api/secrets/*` → secrets:list/read/write
  - `/api/config` → config:read/write
  - `/api/audit` → audit:read
  - `/api/observer/*` → observers:list/read/write
  - `/api/alerts/*` → alerts:list/read
  - `/api/analytics` → analytics:read
  - `/api/dashboard/*` → dashboard:read/write
  - `/api/pipelines/*` → pipelines:list/read/start/cancel
  - All comm channel command routing
- Comprehensive REST capability enforcement test coverage (TS-531–TS-549)
- Release-smoke section §cap-enforcement added

---

### S4 — SPIFFE/SPIRE AuthType implementation (deferred / future)

- `AuthType: "spiffe"` — parse SPIFFE SVIDs from TLS client cert or Authorization header
- SPIFFE claims → capability grant mapping via `spiffe_claims_map` config
- Wire-ready from S1; full implementation tracked in future BL

---

## 7-surface parity tracking

| Surface | S1 | S2 | S3 |
|---|---|---|---|
| REST API | Partial ✅ (key handlers) | Fan-out + cross-host input | Full sweep |
| MCP | Gate only (comm:write on /api/mcp/call) | 12 new tools | — |
| CLI | — | federation subcommands | — |
| Comm | — | peer/group commands | Cap enforcement |
| PWA | — | Federation Peers panel | — |
| Locale × 5 | — | Keys for all new strings | — |
| Mobile | — | datawatch-app issue | — |

---

## File map

| File | Purpose |
|---|---|
| `internal/federation/capabilities.go` | Cap constants, builtin groups, Resolve/Check |
| `internal/federation/capabilities_test.go` | 10 unit tests |
| `internal/federation/group_store.go` | Custom group CRUD + persistence |
| `internal/server/federation_cap.go` | fedAuthMiddleware, fedCap(), mustMarshal |
| `internal/server/federation_peers_api.go` | /api/federation/peers + /api/federation/groups handlers |
| `internal/server/federation_peers_api_test.go` | 12 integration tests |
| `internal/server/federation.go` | /api/federation/sessions fan-out (extend in S2) |
| `internal/server/multiserver/store.go` | Entry struct + GetByToken + ListFederated |
| `internal/mcp/bl316_federation_tools.go` | MCP tools (S2) |
| `cmd/datawatch/cli_federation.go` | CLI subcommands (S2) |
| `internal/server/web/app.js` | PWA Federation Peers panel (S2) |
| `internal/server/web/locales/*.json` | Locale keys (S2) |
| `docs/howto/federation-cbac.md` | Operator howto (S2) |

---

## Acceptance (v7.3.0 full release)

- All TS-500–TS-549 pass against live test daemon
- `go test ./...` green (1 pre-existing kubectl skip excluded)
- Smoke §federation passes
- `docs/howto/federation-cbac.md` present and indexed
- 7-surface parity table fully checked
- Mobile issue filed in datawatch-app
- No capability check bypassed at any entry point

---

## Rules audit template (fill per sprint)

| Rule | S1 | S2 | S3 |
|---|---|---|---|
| DIP | ✓ no unresolved | — | — |
| 7-surface parity | Partial | Full | Full sweep |
| Error-Filing Rule | ✓ | — | — |
| Android Sync Rule | — | Issue filed | — |
| Localization Rule | — | × 5 bundles | — |
| Smoke | ✓ build pass | §fed pass | §cap-sweep |
| No internal refs | ✓ | ✓ | ✓ |
| Howto-Coverage | — | federation-cbac.md | — |
| Cookbook | TS-500–530 planned | TS-531–549 planned | — |
| Plans folder hygiene | This doc dated 2026-05-18 | — | — |
